package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// A F7c de ponta a ponta (ADR-019, "Decisões da F7c"), contra a nuvem de
// verdade sobre o Store em memória: o envio inicial (só o que está nos 6
// meses, fora da lixeira, nascido neste PC e sem recusa; uma vez; com a
// cópia antes; sem mudar nenhuma linha fora do sync; sem duplicar num
// reinício no meio; com o ditado novo passando na frente), o selo escolhido
// no PC, e o "Esquecer esta nuvem" seguido de outra conta ou da mesma.

// ─── Ajudas ───

// semente é um ditado de antes da conexão, gravado direto no banco do PC.
type sementeE2E struct {
	texto    string
	criado   time.Time
	lixeira  bool
	aparelho string
	situacao string
	elegivel bool
}

// semearHistorico grava, numa transação, os ditados de antes da conexão (o
// histórico que o Mauro tem no bin\), e devolve os ids na ordem.
func (pc *pcE2E) semearHistorico(sementes []sementeE2E) []string {
	pc.t.Helper()
	tx, err := pc.db.Begin()
	if err != nil {
		pc.t.Fatal(err)
	}
	ids := make([]string, len(sementes))
	for i, s := range sementes {
		ids[i] = storage.NewID()
		criado := s.criado.UTC().Format("2006-01-02T15:04:05.000000000Z")
		var apagado, purga, aparelho, erro any
		if s.lixeira {
			d := s.criado.Add(time.Minute).UTC()
			apagado = d.Format("2006-01-02T15:04:05.000000000Z")
			purga = d.AddDate(0, 0, 30).Format("2006-01-02T15:04:05.000000000Z")
		}
		if s.aparelho != "" {
			aparelho = s.aparelho
		}
		situacao := s.situacao
		if situacao == "" {
			situacao = storage.SituacaoNormal
		}
		if situacao == storage.SituacaoRecusado {
			erro = "texto_grande"
		}
		if _, err := tx.Exec(`INSERT INTO local_transcriptions (id, user_id, device_id, source_platform, raw_text,
			final_text, mode, language, asr_provider, asr_model, duration_ms, processing_ms, status, created_at,
			updated_at, deleted_at, purge_at, version, sync_state, last_sync_error)
			VALUES (?, ?, ?, 'windows', ?, ?, 'smart', 'pt', 'whisper-local', 'small-q4_0', 3200, 410, 'done', ?, ?, ?, ?, 1, ?, ?)`,
			ids[i], pc.dono, aparelho, "bruto: "+s.texto, s.texto, criado, criado, apagado, purga, situacao, erro); err != nil {
			pc.t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		pc.t.Fatal(err)
	}
	return ids
}

// linhasDoBanco lê local_transcriptions inteira, cada linha como coluna ->
// valor, por id.
func linhasDoBanco(t *testing.T, pc *pcE2E) map[string]map[string]any {
	t.Helper()
	rows, err := pc.db.Query(`SELECT * FROM local_transcriptions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := map[string]map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			m[c] = vals[i]
		}
		out[m["id"].(string)] = m
	}
	return out
}

// tudoNaConta é o estado da conta do token, lido pelo pull do cursor 0 com o
// cliente de verdade do PC.
func (n *nuvemE2E) tudoNaConta(token string) map[string]syncengine.Change {
	n.t.Helper()
	c := syncengine.NovoCliente(n.base, token, syncengine.NovoHTTP())
	out := map[string]syncengine.Change{}
	var cursor int64
	for {
		p, err := c.Pull(context.Background(), cursor, syncengine.LimiteDoPull)
		if err != nil {
			n.t.Fatalf("pull do 0: %v", err)
		}
		for _, ch := range p.Changes {
			if _, repetido := out[ch.EntityID]; repetido {
				n.t.Errorf("o pull trouxe %s duas vezes", ch.EntityID)
			}
			out[ch.EntityID] = ch
		}
		if !p.HasMore {
			return out
		}
		cursor = p.LastSeq
	}
}

// novaConta cria outra conta na mesma nuvem (a "outra conta" do esquecer).
func (n *nuvemE2E) novaConta(sub, email string) string {
	n.t.Helper()
	u, err := n.mem.EntrarComGoogle(context.Background(), cloudstore.Identidade{Sub: sub, Email: email, Nome: "Outra"}, time.Now())
	if err != nil {
		n.t.Fatal(err)
	}
	return u.ID
}

// conectarNa é um pareamento novo desta instalação com a conta (depois do
// esquecer: a URL e a conta gravadas de novo). mesmoAparelho: a nuvem de
// verdade reaproveita o aparelho pelo install_id ao parear de novo na mesma
// conta (F6); numa conta nova, ele é outro.
func (pc *pcE2E) conectarNa(conta, email string, mesmoAparelho bool) {
	pc.t.Helper()
	n := pc.nuvem
	if !mesmoAparelho {
		pc.aparelho = cloudstore.NewUUIDv7()
	}
	pc.token = cloudauth.NovoSegredo()
	n.mem.AddDevice(conta, pc.aparelho)
	n.mem.AddDeviceToken(pc.aparelho, cloudauth.HMAC(n.pepper, pc.token))
	if err := pc.cofre.Gravar(nuvem.NomeNoCofrePortatil, pc.token); err != nil {
		pc.t.Fatal(err)
	}
	if err := storage.NovaInstalacao(pc.db).Gravar(context.Background(), map[string]string{
		"nuvem_url": n.base, "nuvem_aparelho": pc.aparelho, "nuvem_conta": conta, "nuvem_email": email,
		"nuvem_nome": "PC de teste", "nuvem_conectado_em": time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano),
	}); err != nil {
		pc.t.Fatal(err)
	}
}

// esquecer é o "Desconectar" e o "Esquecer esta nuvem" do painel.
func (pc *pcE2E) esquecer() {
	pc.t.Helper()
	pc.desconectar()
	e, err := pc.servico.Esquecer(context.Background())
	if err != nil || e.URL != "" {
		pc.t.Fatalf("esquecer: %+v, %v", e, err)
	}
}

func (pc *pcE2E) enviarHistorico() nuvem.Estado {
	pc.t.Helper()
	e, err := pc.servico.EnviarHistorico(context.Background())
	if err != nil {
		pc.t.Fatalf("enviar o histórico: %v\n%s", err, pc.log.String())
	}
	return e
}

func (pc *pcE2E) estado() nuvem.Estado {
	pc.t.Helper()
	e, err := pc.servico.Estado(context.Background())
	if err != nil {
		pc.t.Fatal(err)
	}
	return e
}

func (pc *pcE2E) copias() []string {
	pc.t.Helper()
	c, err := filepath.Glob(pc.caminho + ".antes-da-nuvem-*.db")
	if err != nil {
		pc.t.Fatal(err)
	}
	return c
}

func chavesDe[T any](m map[string]T) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func ordenados(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

// historicoComum são n ditados elegíveis, de antes da conexão.
func historicoComum(n int, prefixo string) []sementeE2E {
	agora := time.Now()
	s := make([]sementeE2E, n)
	for i := range s {
		s[i] = sementeE2E{texto: fmt.Sprintf("%s %d", prefixo, i), criado: agora.Add(-time.Duration(n-i) * time.Hour)}
	}
	return s
}

// ─── O envio inicial ───

// 1.000 ditados de antes da conexão: 700 elegíveis, 100 na lixeira, 100 com
// mais de 6 meses, 50 de outro PC e 50 recusados. Sem o clique, nada sobe;
// com ele, a cópia (com os 1.000) vem antes, e a nuvem fica com EXATAMENTE os
// 700, cada um na versão 1, uma vez só; nenhuma linha do PC muda fora das
// colunas do sync, e só as dos 700 ganham a versão da nuvem.
func TestSyncE2E_EnvioInicialSobeSoOCerto(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	agora := time.Now()
	var sementes []sementeE2E
	for i := 0; i < 1000; i++ {
		s := sementeE2E{texto: fmt.Sprintf("segredo %d, com acento: ação", i), criado: agora.Add(-time.Duration(i+1) * 3 * time.Hour)}
		switch {
		case i < 700:
			s.elegivel = true
		case i < 800:
			s.lixeira = true
		case i < 900:
			s.criado = agora.AddDate(0, -7, 0).Add(-time.Duration(i) * time.Minute)
		case i < 950:
			s.aparelho = cloudstore.NewUUIDv7()
		default:
			s.situacao = storage.SituacaoRecusado
		}
		sementes = append(sementes, s)
	}
	ids := pc.semearHistorico(sementes)
	var elegiveis []string
	for i, s := range sementes {
		if s.elegivel {
			elegiveis = append(elegiveis, ids[i])
		}
	}
	antes := linhasDoBanco(t, pc)

	// Conectado, a primeira rodada: nada do histórico sobe sem o clique.
	pc.rodadaOK()
	if got := n.tudoNaConta(pc.token); len(got) != 0 {
		t.Fatalf("sem o clique, subiram %d ditados", len(got))
	}
	e := pc.estado()
	if h := e.Historico; h == nil || h.Previa != 700 || h.Aceito || h.Envio != nil || h.ConferidoEm == "" {
		t.Fatalf("a prévia: %+v", e.Historico)
	}

	// O clique: a cópia, e o histórico na fila.
	e = pc.enviarHistorico()
	if h := e.Historico; h == nil || !h.Aceito || h.Envio == nil || h.Envio.Feitos != 0 || h.Envio.Total != 700 {
		t.Fatalf("depois do clique: %+v", e.Historico)
	}
	copias := pc.copias()
	if len(copias) != 1 {
		t.Fatalf("as cópias: %v", copias)
	}
	c, err := storage.AbrirSomenteLeitura(copias[0])
	if err != nil {
		t.Fatal(err)
	}
	var naCopia int
	if err := c.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&naCopia); err != nil || naCopia != 1000 {
		t.Fatalf("a cópia tem %d ditados (%v)", naCopia, err)
	}
	c.Close()

	pc.rodadaOK()
	e = pc.estado()
	if h := e.Historico; h == nil || h.Envio != nil || h.Pendentes != 0 || h.Previa != 0 || h.NaoSubiram != 50 {
		t.Fatalf("depois da rodada: %+v", e.Historico)
	}

	naNuvem := n.tudoNaConta(pc.token)
	if !reflect.DeepEqual(chavesDe(naNuvem), ordenados(elegiveis)) {
		t.Fatalf("a nuvem tem %d ditados; quero os %d elegíveis", len(naNuvem), len(elegiveis))
	}
	for id, ch := range naNuvem {
		if ch.EntityVersion != 1 || ch.Payload == nil || ch.Payload.DeviceID == nil || *ch.Payload.DeviceID != pc.aparelho ||
			ch.Payload.DeletedAt != nil || ch.Payload.FinalText != antes[id]["final_text"] {
			t.Errorf("%s na nuvem: %+v", id, ch)
		}
	}
	_, pushes := n.porteiro.visto()
	total := 0
	for _, p := range pushes {
		total += len(p)
	}
	if total != 700 {
		t.Errorf("%d mutações no push; quero 700, uma por ditado", total)
	}

	depois := linhasDoBanco(t, pc)
	if len(depois) != 1000 {
		t.Fatalf("o PC tem %d ditados", len(depois))
	}
	eleito := map[string]bool{}
	for _, id := range elegiveis {
		eleito[id] = true
	}
	for id, linha := range depois {
		quero := map[string]any{}
		for k, v := range antes[id] {
			quero[k] = v
		}
		if eleito[id] {
			quero["server_version"] = int64(1)
		}
		if !reflect.DeepEqual(linha, quero) {
			t.Errorf("a linha %s mudou além do sync:\nantes  %v\ndepois %v", id, quero, linha)
		}
	}
	if pc.naFila() != 0 {
		t.Errorf("a fila ficou com %d", pc.naFila())
	}
	for _, proibido := range []string{"segredo", "ação", filepath.Dir(pc.caminho), n.base, elegiveis[0], pc.token} {
		if strings.Contains(pc.log.String(), proibido) || strings.Contains(n.log.String(), proibido) {
			t.Errorf("o log tem %q", proibido)
		}
	}
	if !strings.Contains(pc.log.String(), "marcados=700") {
		t.Errorf("o log do PC não diz quantos entraram na fila:\n%s", pc.log.String())
	}
}

// O reinício no meio: a resposta do primeiro lote se perde (a nuvem aplicou),
// e o Agent reinicia. Na volta, o lote em voo vai primeiro, com os MESMOS
// mutation_id (a nuvem responde duplicate), e o resto segue. Nada duplica, e
// todo ditado fica na versão 1.
func TestSyncE2E_EnvioInicialReinicioNoMeioNaoDuplica(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	ids := pc.semearHistorico(historicoComum(1200, "do reinício"))
	pc.rodadaOK()
	pc.enviarHistorico()

	n.porteiro.mexer(func(p *porteiro) { p.perderRespostas = 1 })
	if err := pc.rodada(); err == nil {
		t.Fatal("a rodada com a resposta perdida não falhou")
	}
	_, pushes := n.porteiro.visto()
	perdido := pushes[len(pushes)-1]
	if len(perdido) != 500 {
		t.Fatalf("o lote perdido tem %d", len(perdido))
	}
	pc.reiniciar()
	n.porteiro.zerar()
	pc.rodadaOK()

	_, pushes = n.porteiro.visto()
	if len(pushes) != 3 || !reflect.DeepEqual(pushes[0], perdido) {
		t.Fatalf("na volta, %d lotes; o primeiro é o em voo, com os mesmos ids: %v", len(pushes), len(pushes[0]))
	}
	naNuvem := n.tudoNaConta(pc.token)
	if !reflect.DeepEqual(chavesDe(naNuvem), ordenados(ids)) {
		t.Fatalf("a nuvem tem %d; quero %d", len(naNuvem), len(ids))
	}
	for id, ch := range naNuvem {
		if ch.EntityVersion != 1 {
			t.Errorf("%s na versão %d", id, ch.EntityVersion)
		}
	}
	for _, id := range ids {
		if l := pc.linha(id); l.ServerVersion == nil || *l.ServerVersion != 1 {
			t.Errorf("%s ficou sem a versão da nuvem: %+v", id, l.ServerVersion)
		}
	}
	if pc.naFila() != 0 || pc.estado().Historico.Envio != nil {
		t.Errorf("sobrou na fila: %d", pc.naFila())
	}
}

// Ditar durante o envio: o push do primeiro lote do histórico fica preso na
// rede; o ditado novo grava na hora (o laço não segura o banco durante a
// rede) e sobe no lote seguinte, na frente do resto do histórico.
func TestSyncE2E_DitarDuranteOEnvioPassaNaFrente(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	pc.semearHistorico(historicoComum(700, "antes"))
	pc.rodadaOK()
	pc.enviarHistorico()
	n.porteiro.zerar()

	travar := make(chan struct{})
	n.porteiro.mexer(func(p *porteiro) { p.travarPush = travar })
	fim := make(chan error, 1)
	go func() { fim <- pc.laco.RodadaAgora(context.Background()) }()
	limite := time.Now().Add(10 * time.Second)
	for {
		if pedidos, _ := n.porteiro.visto(); len(pedidos) > 0 {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("o primeiro push não chegou")
		}
		time.Sleep(5 * time.Millisecond)
	}
	inicio := time.Now()
	novo := pc.ditar("o ditado de agora")
	if d := time.Since(inicio); d > time.Second {
		t.Errorf("o ditado esperou %v pelo push preso", d)
	}
	n.porteiro.mexer(func(p *porteiro) { p.travarPush = nil })
	close(travar)
	if err := <-fim; err != nil {
		t.Fatal(err)
	}
	lotes := n.porteiro.entidadesVistas()
	if len(lotes) != 2 || len(lotes[0]) != 500 || len(lotes[1]) != 201 || lotes[1][0] != novo {
		t.Fatalf("os lotes: %d (o segundo, com %d, começa por %v; o novo é %s)", len(lotes), len(lotes[1]), lotes[1][:1], novo)
	}
	for _, id := range lotes[0] {
		if id == novo {
			t.Error("o ditado novo foi no lote que já estava em voo")
		}
	}
	if got := len(n.tudoNaConta(pc.token)); got != 701 {
		t.Errorf("a nuvem tem %d; quero 701", got)
	}
}

// A lixeira e a edição do histórico com o PC conectado (revisão da F7c): antes
// do clique, nada do histórico sobe (nem a edição, nem a lixeira, nem o
// restaurar); durante o envio, o ditado posto na lixeira antes de o lote dele
// sair não sobe, e o editado sobe com o texto novo. A lixeira da nuvem só
// recebe o que ela já tinha (a lixeira feita aqui depois do envio).
func TestSyncE2E_LixeiraDoHistoricoConectadoNaoSobe(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	s := historicoComum(5, "histórico")
	s = append(s, sementeE2E{texto: "estava na lixeira", criado: time.Now().Add(-time.Hour), lixeira: true})
	ids := pc.semearHistorico(s)
	editadoAntes, lixeiraAntes, lixeiraDurante, editadoDurante, intacto, restaurado := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	pc.rodadaOK()

	// Antes do clique.
	pc.editar(editadoAntes, "editado antes do envio")
	pc.lixeira(lixeiraAntes)
	pc.restaurar(restaurado)
	pc.rodadaOK()
	if got := n.tudoNaConta(pc.token); len(got) != 0 {
		t.Fatalf("antes do clique, subiram %d ditados do histórico", len(got))
	}
	if pc.naFila() != 0 {
		t.Fatalf("antes do clique, a fila tem %d", pc.naFila())
	}
	if h := pc.estado().Historico; h == nil || h.Previa != 5 {
		t.Fatalf("a prévia: %+v; quero 5 (sem o que foi para a lixeira, com o restaurado)", h)
	}

	// O clique, e a lixeira e a edição antes de o lote sair.
	pc.enviarHistorico()
	pc.lixeira(lixeiraDurante)
	pc.editar(editadoDurante, "editado durante o envio")
	pc.rodadaOK()
	naNuvem := n.tudoNaConta(pc.token)
	if got, quero := chavesDe(naNuvem), ordenados([]string{editadoAntes, editadoDurante, intacto, restaurado}); !reflect.DeepEqual(got, quero) {
		t.Fatalf("a nuvem tem %v; quero %v", got, quero)
	}
	for id, ch := range naNuvem {
		if ch.Payload.DeletedAt != nil {
			t.Errorf("%s subiu na lixeira", id)
		}
	}
	if a, d := naNuvem[editadoAntes].Payload.FinalText, naNuvem[editadoDurante].Payload.FinalText; a != "editado antes do envio" || d != "editado durante o envio" {
		t.Errorf("os textos na nuvem: %q, %q", a, d)
	}
	if h := pc.estado().Historico; h == nil || h.Envio != nil || h.Pendentes != 0 || h.Previa != 0 {
		t.Errorf("depois do envio: %+v", h)
	}

	// Depois do envio, a lixeira de um ditado que a nuvem tem sobe.
	pc.lixeira(intacto)
	pc.rodadaOK()
	if c := n.celItem(intacto); c.DeletedAt == nil {
		t.Error("a lixeira de um ditado que a nuvem tem não subiu")
	}
}

// A lixeira de um ditado do envio inicial cujo lote ficou em voo (a sonda A2
// da reconferência da F7c; risco aceito, ADR-019, "A reconferência da F7c"):
// o marcador em voo fica (a nuvem pode já ter o ditado), e o reenvio, com o
// mesmo mutation_id, lê a linha na hora e leva a lixeira. Se o primeiro envio
// não chegou (a nuvem fora do ar), a nuvem cria o ditado já na lixeira, na
// versão 1: ele nunca aparece no histórico do app. Se chegou (a resposta se
// perdeu), ela responde duplicate, e a lixeira sobe como mudança, na versão
// 2. Os dois terminam iguais: o ditado na lixeira da nuvem, o resto do lote
// fora dela, e nada na fila. (Tirar a lixeira do criar faria o primeiro caso
// igual ao segundo: o ditado passaria pelo histórico do app antes de ir para
// a lixeira.)
func TestSyncE2E_LixeiraDoEnvioInicialEmVoo(t *testing.T) {
	for _, c := range []struct {
		nome   string
		falha  func(p *porteiro)
		versao int64
	}{
		{"a nuvem fora do ar: o primeiro envio não chegou", func(p *porteiro) { p.fora = true }, 1},
		{"a resposta perdida: o primeiro envio chegou", func(p *porteiro) { p.perderRespostas = 1 }, 2},
	} {
		t.Run(c.nome, func(t *testing.T) {
			n := novaNuvemE2E(t, server.Limites{})
			pc := n.novoPC()
			ids := pc.semearHistorico(historicoComum(3, "em voo"))
			b := ids[1]
			pc.rodadaOK()
			pc.enviarHistorico()
			n.porteiro.mexer(c.falha)
			if err := pc.rodada(); err == nil {
				t.Fatal("a rodada deu certo com a nuvem falhando")
			}
			n.porteiro.mexer(func(p *porteiro) { p.fora = false })
			ms, err := pc.repo.Fila(pc.dono).Marcadores(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range ms {
				if !m.EmVoo() || !m.Inicial {
					t.Fatalf("depois da falha, um marcador não está em voo: %+v", m)
				}
			}
			pc.lixeira(b)
			if len(ms) != 3 || pc.naFila() != 3 {
				t.Fatalf("a fila: %d antes da lixeira, %d depois; quero os 3, com o em voo", len(ms), pc.naFila())
			}

			pc.rodadaOK()
			naNuvem := n.tudoNaConta(pc.token)
			if got := chavesDe(naNuvem); !reflect.DeepEqual(got, ordenados(ids)) {
				t.Fatalf("a nuvem tem %v", got)
			}
			for _, x := range ids {
				if na := naNuvem[x].Payload.DeletedAt != nil; na != (x == b) {
					t.Errorf("%s na lixeira da nuvem: %v", x, na)
				}
			}
			if v := naNuvem[b].EntityVersion; v != c.versao {
				t.Errorf("o ditado posto na lixeira está na versão %d da nuvem; quero %d", v, c.versao)
			}
			if l := pc.linha(b); l.DeletedAt == nil || l.ServerVersion == nil || *l.ServerVersion != c.versao {
				t.Errorf("no PC: lixeira %v, versão da nuvem %v", l.DeletedAt != nil, l.ServerVersion)
			}
			if pc.naFila() != 0 {
				t.Errorf("sobrou na fila: %d", pc.naFila())
			}
		})
	}
}

// O risco que a revisão da F2 deixou para a F7c (D3): depois de um backup
// restaurado (epoch novo), a nuvem pode ter fora da lixeira um ditado que o
// PC pôs lá. O recomeço não sobe a lixeira, o pull do 0 traz o ditado ativo,
// e o PC, sem a versão da nuvem, não o sobrescreve: a reconciliação sobe a
// lixeira (apagar vence), e o ditado não volta ao histórico. (O "backup" é o
// celular restaurando o ditado logo antes de o epoch trocar: a nuvem fica
// com ele ativo, numa versão que o PC não viu.)
func TestSyncE2E_EpochNovoNaoTiraDaLixeira(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	x := pc.ditar("vai para a lixeira")
	fica := pc.ditar("fica no histórico")
	pc.rodadaOK()
	pc.lixeira(x)
	pc.rodadaOK()
	n.celRestaurar(x)
	n.mem.TrocarEpoch(n.conta)
	pc.rodadaOK()
	if l := pc.linha(x); l.DeletedAt == nil {
		t.Fatal("o recomeço tirou da lixeira o ditado que o PC pôs lá")
	}
	if c := n.celItem(x); c.DeletedAt == nil || c.VersaoEmConflito != nil {
		t.Errorf("a nuvem: %+v (quero na lixeira, sem selo)", c)
	}
	if l := pc.linha(fica); l.DeletedAt != nil || l.ServerVersion == nil {
		t.Errorf("o outro ditado: %+v", l)
	}
	if pc.naFila() != 0 {
		t.Errorf("sobrou na fila: %d", pc.naFila())
	}
}

// A reconciliação converge quando só a lixeira difere e quem está na lixeira
// é a NUVEM (revisão da F7c): o PC sem a versão da nuvem, que mexeu no ditado
// (ou que, no epoch novo, conta tudo como mexido), com ele fora da lixeira e
// o mesmo texto; a nuvem com ele na lixeira, posto pelo celular. Subir o
// ditado não muda nada lá (apagar vence), e antes o PC reconciliava a cada
// rodada: a nuvem respondia mesclado, com versão nova, o PC não guardava a
// versão, e o pull seguinte via a diferença de novo (a versão subia sem fim,
// e os outros aparelhos baixavam o ditado a cada minuto). Agora vale o da
// nuvem, e a versão para na primeira rodada.
func TestSyncE2E_ReconciliacaoSoDaLixeiraConverge(t *testing.T) {
	conferir := func(t *testing.T, n *nuvemE2E, pc *pcE2E, x string) {
		t.Helper()
		var vs []int64
		for i := 0; i < 4; i++ {
			pc.rodadaOK()
			vs = append(vs, n.celItem(x).Version)
		}
		for _, v := range vs[1:] {
			if v != vs[0] {
				t.Fatalf("a versão da nuvem não para de subir: %v", vs)
			}
		}
		if l := pc.linha(x); l.DeletedAt == nil || l.ServerVersion == nil || *l.ServerVersion != vs[0] || l.Conflito != nil {
			t.Errorf("o PC: lixeira %v, versão %v (a nuvem está na %d), selo %+v", l.DeletedAt != nil, l.ServerVersion, vs[0], l.Conflito)
		}
		if c := n.celItem(x); c.DeletedAt == nil || c.VersaoEmConflito != nil {
			t.Errorf("a nuvem: %+v (quero na lixeira, sem selo)", c)
		}
		if pc.naFila() != 0 {
			t.Errorf("sobrou na fila: %d", pc.naFila())
		}
	}

	t.Run("o epoch novo", func(t *testing.T) {
		n := novaNuvemE2E(t, server.Limites{})
		pc := n.novoPC()
		x := pc.ditar("o celular põe na lixeira")
		pc.rodadaOK()
		n.celLixeira(x)
		n.mem.TrocarEpoch(n.conta)
		conferir(t, n, pc, x)
	})

	t.Run("de volta à mesma conta, depois de pôr e tirar da lixeira aqui", func(t *testing.T) {
		n := novaNuvemE2E(t, server.Limites{})
		pc := n.novoPC()
		x := pc.semearHistorico(historicoComum(1, "volta"))[0]
		pc.rodadaOK()
		pc.enviarHistorico()
		pc.rodadaOK()
		pc.esquecer()
		time.Sleep(20 * time.Millisecond) // o que muda daqui em diante é "depois do esquecer"
		n.celLixeira(x)
		pc.lixeira(x)
		pc.restaurar(x)
		pc.conectarNa(n.conta, emailDoE2E, true)
		conferir(t, n, pc, x)
	})
}

// Esquecer no meio de uma rodada: o push está preso na rede quando o PC
// desconecta e esquece a nuvem. A resposta que chega depois não é aplicada, e
// a rodada só termina: nada de recomeço com o aparelho velho (que recriaria o
// cursor dele e apagaria a referência do esquecer).
func TestSyncE2E_EsquecerNoMeioDaRodada(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	pc.semearHistorico(historicoComum(3, "no meio"))
	pc.rodadaOK()
	pc.enviarHistorico()
	n.porteiro.zerar()
	travar := make(chan struct{})
	n.porteiro.mexer(func(p *porteiro) { p.travarPush = travar })
	fim := make(chan error, 1)
	go func() { fim <- pc.laco.RodadaAgora(context.Background()) }()
	limite := time.Now().Add(10 * time.Second)
	for {
		if pedidos, _ := n.porteiro.visto(); len(pedidos) > 0 {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("o push não chegou")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A nuvem não responde ao Desconectar (o aparelho não é revogado lá, e o
	// push preso será aplicado); o PC desconecta e esquece do mesmo jeito.
	n.porteiro.mexer(func(p *porteiro) { p.fora = true })
	pc.esquecer()
	n.porteiro.mexer(func(p *porteiro) { p.fora, p.travarPush = false, nil })
	close(travar)
	if err := <-fim; err != nil {
		t.Fatalf("a rodada depois do esquecer: %v", err)
	}
	var cursores, comVersao int
	if err := pc.db.QueryRow(`SELECT COUNT(*) FROM sync_state`).Scan(&cursores); err != nil || cursores != 0 {
		t.Errorf("o cursor voltou depois do esquecer: %d (%v)", cursores, err)
	}
	if err := pc.db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions WHERE server_version IS NOT NULL`).Scan(&comVersao); err != nil || comVersao != 0 {
		t.Errorf("a resposta velha gravou versões: %d (%v)", comVersao, err)
	}
	if v, ok, _ := storage.NovaInstalacao(pc.db).Ler(context.Background(), storage.ChaveNuvemEsquecidaEm); !ok || v == "" {
		t.Error("a referência do esquecer sumiu")
	}
	if pc.naFila() != 0 {
		t.Errorf("a fila voltou: %d", pc.naFila())
	}
}

// ─── O selo no PC ───

// selar faz o mesmo ditado mudar no celular e no PC 1 antes da rodada: o
// celular chega primeiro, e o texto do PC 1 vai para o selo, que chega a
// todos.
func selar(t *testing.T, n *nuvemE2E, pc1, pc2 *pcE2E, prefixo string) string {
	t.Helper()
	id := pc1.ditar(prefixo + " original")
	pc1.rodadaOK()
	pc2.rodadaOK()
	n.celEditar(id, prefixo+" do celular")
	pc1.editar(id, prefixo+" do PC 1")
	pc1.rodadaOK()
	l := pc1.linha(id)
	if l.Conflito == nil || l.Conflito.FinalText != prefixo+" do PC 1" || l.FinalText != prefixo+" do celular" {
		t.Fatalf("o selo no PC 1: %q / %+v", l.FinalText, l.Conflito)
	}
	pc2.rodadaOK()
	if pc2.linha(id).Conflito == nil {
		t.Fatal("o selo não chegou ao PC 2")
	}
	return id
}

func TestSyncE2E_SeloEscolhidoNoPC(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()

	// O PC 1 fica com a outra (o texto dele): o selo sai em todos, e o
	// texto do PC 1 vale nos três.
	x := selar(t, n, pc1, pc2, "x")
	if _, err := pc1.repo.ResolverConflito(ctx, pc1.dono, x, pc1.linha(x).Version, storage.EscolhaTrocar); err != nil {
		t.Fatal(err)
	}
	pc1.rodadaOK()
	cel := n.celItem(x)
	if cel.FinalText != "x do PC 1" || cel.VersaoEmConflito != nil {
		t.Fatalf("o celular depois da escolha do PC: %+v", cel)
	}
	pc2.rodadaOK()
	if l := pc2.linha(x); l.FinalText != "x do PC 1" || l.Conflito != nil {
		t.Errorf("o PC 2 depois da escolha do PC 1: %q / %+v", l.FinalText, l.Conflito)
	}
	if l := pc1.linha(x); l.FinalText != "x do PC 1" || l.Conflito != nil || l.ServerVersion == nil || *l.ServerVersion != cel.Version {
		t.Errorf("o PC 1: %q / %+v / v%v (a nuvem está na %d)", l.FinalText, l.Conflito, l.ServerVersion, cel.Version)
	}

	// O celular escolhe antes ("ficar com esta"): a escolha do PC 1, feita
	// sem saber, cai; vale a do celular, nos três, sem selo.
	y := selar(t, n, pc1, pc2, "y")
	cy := n.celItem(y)
	if st, b := n.celular(http.MethodPatch, "/api/v1/transcriptions/"+y, map[string]any{"base_version": cy.Version, "resolver_conflito": "manter"}); st != http.StatusOK {
		t.Fatalf("a escolha do celular: %d %s", st, b)
	}
	if _, err := pc1.repo.ResolverConflito(ctx, pc1.dono, y, pc1.linha(y).Version, storage.EscolhaTrocar); err != nil {
		t.Fatal(err)
	}
	pc1.rodadaOK()
	if l := pc1.linha(y); l.FinalText != "y do celular" || l.Conflito != nil || pc1.naFila() != 0 {
		t.Errorf("o PC 1 depois da escolha que caiu: %q / %+v (fila %d)", l.FinalText, l.Conflito, pc1.naFila())
	}
	if c := n.celItem(y); c.FinalText != "y do celular" || c.VersaoEmConflito != nil {
		t.Errorf("o celular: %+v", c)
	}

	// O texto mudou depois (o celular editou com o selo aberto): a escolha
	// do PC 1 cai, e o selo volta a ele, com o texto que perdeu; nada se
	// perde.
	z := selar(t, n, pc1, pc2, "z")
	if _, err := pc1.repo.ResolverConflito(ctx, pc1.dono, z, pc1.linha(z).Version, storage.EscolhaManter); err != nil {
		t.Fatal(err)
	}
	n.celEditar(z, "z do celular de novo")
	pc1.rodadaOK()
	l := pc1.linha(z)
	if l.FinalText != "z do celular de novo" || l.Conflito == nil || l.Conflito.FinalText != "z do PC 1" {
		t.Errorf("o PC 1 depois da escolha que caiu sobre o texto novo: %q / %+v", l.FinalText, l.Conflito)
	}
	if c := n.celItem(z); c.VersaoEmConflito == nil || c.VersaoEmConflito.FinalText != "z do PC 1" {
		t.Errorf("o celular perdeu o selo: %+v", c)
	}
}

// ─── Esquecer esta nuvem ───

// Esquecer e conectar a OUTRA conta: a nuvem antiga fica intacta; na nova,
// nada sobe sem o clique (a pergunta diz que os ditados já foram para outra
// nuvem); com ele, uma cópia nova e só o que nasceu neste PC (o que veio do
// PC 2 pela conta antiga nunca sobe daqui).
func TestSyncE2E_EsquecerEConectarAOutraConta(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	daqui := pc1.semearHistorico(historicoComum(3, "daqui"))
	pc1.rodadaOK()
	pc1.enviarHistorico()
	pc1.rodadaOK()
	doPC2 := pc2.ditar("do PC 2")
	pc2.rodadaOK()
	pc1.rodadaOK()
	if !pc1.temLinha(doPC2) {
		t.Fatal("o ditado do PC 2 não chegou ao PC 1")
	}
	contaA := n.tudoNaConta(pc2.token)

	pc1.esquecer()
	if !pc1.temLinha(doPC2) || len(pc1.todos()) != 4 {
		t.Fatalf("o esquecer mexeu nos ditados: %d", len(pc1.todos()))
	}
	contaB := n.novaConta("sub-conta-b", emailDaOutraConta)
	pc1.conectarNa(contaB, emailDaOutraConta, false)
	pc1.rodadaOK()
	if got := n.tudoNaConta(pc1.token); len(got) != 0 {
		t.Fatalf("na conta nova, sem o clique, subiram %d", len(got))
	}
	if h := pc1.estado().Historico; h == nil || h.Previa != 3 || h.Aceito || !h.OutraNuvem {
		t.Fatalf("a pergunta na conta nova: %+v", h)
	}
	// A cópia tem o segundo no nome: a desta conta não pode cair no mesmo
	// segundo da da conta antiga (na vida real, são minutos).
	time.Sleep(1100 * time.Millisecond)
	pc1.enviarHistorico()
	pc1.rodadaOK()
	if got := chavesDe(n.tudoNaConta(pc1.token)); !reflect.DeepEqual(got, ordenados(daqui)) {
		t.Errorf("a conta nova tem %v; quero só os 3 daqui", got)
	}
	if len(pc1.copias()) != 2 {
		t.Errorf("as cópias: %v (quero uma por conta)", pc1.copias())
	}
	if got := n.tudoNaConta(pc2.token); !reflect.DeepEqual(got, contaA) {
		t.Errorf("a conta antiga mudou: %d ditados antes, %d depois", len(contaA), len(got))
	}
	// A edição do ditado do PC 2, agora, fica só aqui.
	pc1.editar(doPC2, "editado no PC 1")
	if pc1.naFila() != 0 {
		t.Error("o ditado do PC 2 entrou na fila da conta nova")
	}
}

// A mudança feita desconectado, que estava na fila na hora do esquecer, não se
// perde na volta à mesma conta (revisão da F7c). Antes, a referência do
// esquecer era o instante dele, e essas linhas, mudadas antes, contavam como
// "o ditado velho": o pull gravava o estado da nuvem por cima, a edição sumia
// de todo lugar e a lixeira voltava, sem selo nem cópia. Agora elas sobem com
// a base vazia, e a nuvem decide: a edição vale (o texto da nuvem era deste
// PC), a lixeira vale (apagar vence) e, quando o celular também mudou o
// texto, os dois ficam no selo.
func TestSyncE2E_MudancaAntesDoEsquecerNaoSePerde(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	ids := pc.semearHistorico(historicoComum(4, "antes do esquecer"))
	editado, naLixeira, dosDois, intacto := ids[0], ids[1], ids[2], ids[3]
	pc.rodadaOK()
	pc.enviarHistorico()
	pc.rodadaOK()

	pc.desconectar()
	pc.editar(editado, "editado desconectado")
	pc.lixeira(naLixeira)
	pc.editar(dosDois, "dos dois: o do PC")
	if pc.naFila() != 3 {
		t.Fatalf("desconectado, a fila tem %d; quero as 3 mudanças", pc.naFila())
	}
	if h := pc.estado().Historico; h == nil || h.Mudancas != 3 {
		t.Errorf("a pergunta do esquecer contaria: %+v; quero as 3 mudanças", h)
	}
	n.celEditar(dosDois, "dos dois: o do celular")
	pc.esquecer()
	pc.conectarNa(n.conta, emailDoE2E, true)
	pc.rodadaOK()
	pc.rodadaOK()

	quero := map[string]struct {
		texto, selo string
		lixeira     bool
	}{
		editado:   {texto: "editado desconectado"},
		naLixeira: {texto: "antes do esquecer 1", lixeira: true},
		dosDois:   {texto: "dos dois: o do celular", selo: "dos dois: o do PC"},
		intacto:   {texto: "antes do esquecer 3"},
	}
	naNuvem := n.tudoNaConta(pc.token)
	for id, w := range quero {
		l := pc.linha(id)
		selo := ""
		if l.Conflito != nil {
			selo = l.Conflito.FinalText
		}
		if l.FinalText != w.texto || selo != w.selo || (l.DeletedAt != nil) != w.lixeira || l.ServerVersion == nil {
			t.Errorf("%s no PC: %q, selo %q, lixeira %v, com a versão da nuvem %v; quero %+v", id, l.FinalText, selo, l.DeletedAt != nil, l.ServerVersion != nil, w)
		}
		ch, ok := naNuvem[id]
		if !ok {
			t.Errorf("%s sumiu da nuvem", id)
			continue
		}
		seloNaNuvem := ""
		if ch.Payload.VersaoEmConflito != nil {
			seloNaNuvem = ch.Payload.VersaoEmConflito.FinalText
		}
		if ch.Payload.FinalText != w.texto || seloNaNuvem != w.selo || (ch.Payload.DeletedAt != nil) != w.lixeira {
			t.Errorf("%s na nuvem: %q, selo %q, lixeira %v; quero %+v", id, ch.Payload.FinalText, seloNaNuvem, ch.Payload.DeletedAt != nil, w)
		}
	}
	if len(naNuvem) != 4 || pc.naFila() != 0 {
		t.Errorf("a nuvem tem %d; a fila, %d", len(naNuvem), pc.naFila())
	}
}

// Esquecer e VOLTAR à mesma conta (o mesmo aparelho, como a nuvem faz ao
// parear de novo): o que mudou aqui depois do esquecer sobe (a edição, sem
// selo; a lixeira, que não volta da nuvem); o que mudou no celular nesse
// tempo vale aqui, sem selo à toa; nada duplica; e o ditado feito depois do
// esquecer sobe com o clique.
func TestSyncE2E_EsquecerEVoltarAMesmaConta(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	ids := pc1.semearHistorico(historicoComum(5, "mesma"))
	p, q, r, tt, u := ids[0], ids[1], ids[2], ids[3], ids[4]
	pc1.rodadaOK()
	pc1.enviarHistorico()
	pc1.rodadaOK()
	pc1.lixeira(u)
	pc1.rodadaOK()
	pc2.rodadaOK()

	pc1.esquecer()
	time.Sleep(20 * time.Millisecond) // o que muda daqui em diante é "depois do esquecer"
	pc1.editar(q, "q editado depois do esquecer")
	pc1.lixeira(tt)
	n.celEditar(r, "r editado no celular")
	v := pc1.ditar("v, depois do esquecer")

	pc1.conectarNa(n.conta, emailDoE2E, true)
	pc1.rodadaOK()
	if h := pc1.estado().Historico; h == nil || h.Previa != 1 || h.Aceito || !h.OutraNuvem {
		t.Fatalf("a pergunta de volta à mesma conta: %+v", h)
	}
	time.Sleep(1100 * time.Millisecond) // a cópia nova não cai no segundo da primeira
	pc1.enviarHistorico()
	pc1.rodadaOK()
	pc2.rodadaOK()

	naNuvem := n.tudoNaConta(pc2.token)
	if !reflect.DeepEqual(chavesDe(naNuvem), ordenados([]string{p, q, r, tt, u, v})) {
		t.Fatalf("a nuvem tem %v", chavesDe(naNuvem))
	}
	for id, ch := range naNuvem {
		if ch.Payload.VersaoEmConflito != nil {
			t.Errorf("%s ganhou um selo à toa: %+v", id, ch.Payload.VersaoEmConflito)
		}
	}
	quero := map[string]struct {
		texto   string
		lixeira bool
	}{
		p: {"mesma 0", false}, q: {"q editado depois do esquecer", false}, r: {"r editado no celular", false},
		tt: {"mesma 3", true}, u: {"mesma 4", true}, v: {"v, depois do esquecer", false},
	}
	for id, w := range quero {
		ch := naNuvem[id]
		if ch.Payload.FinalText != w.texto || (ch.Payload.DeletedAt != nil) != w.lixeira {
			t.Errorf("%s na nuvem: %q, lixeira %v; quero %q, %v", id, ch.Payload.FinalText, ch.Payload.DeletedAt != nil, w.texto, w.lixeira)
		}
		for _, pc := range []*pcE2E{pc1, pc2} {
			l := pc.linha(id)
			if l.FinalText != w.texto || (l.DeletedAt != nil) != w.lixeira || l.Conflito != nil {
				t.Errorf("%s no PC: %q, lixeira %v, selo %+v", id, l.FinalText, l.DeletedAt != nil, l.Conflito)
			}
		}
	}
	semRepetidos(t, "PC 1", pc1.todos(), func(l storage.Transcription) string { return l.ID })
	if pc1.naFila() != 0 {
		t.Errorf("sobrou na fila: %d", pc1.naFila())
	}
}
