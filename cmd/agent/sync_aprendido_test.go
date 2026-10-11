package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// A ligação do aprendido do painel com o sync na montagem de verdade do Agent
// (montarSync, F8g), sob go test: o dicionario.json e o historico.db em
// t.TempDir(), o runtime falso, e a nuvem atrás do portão sysinput (que recusa
// sob go test): nenhum pedido sai, e o laço nem roda.
//
// O gancho: o Aprender e o Desfazer do painel sobem a versão local do id no
// espelho no ato, pelo mesmo aprendizado que o laço recebe. O Desfazer seguido
// do Aprender do mesmo par, que deixa o arquivo como era, continua sendo
// mudança daqui (duas versões acima da que a varredura viu).
func TestSync_GanchoDoPainelSobeAVersaoLocal(t *testing.T) {
	var buf logTravado
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hist, prefs, dono, fechar, err := abrirHistoricoEm(logger, filepath.Join(t.TempDir(), "historico.db"), "", clock.NewSystem())
	if err != nil {
		t.Fatal(err)
	}
	defer fechar()
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	n := montarNuvem(prefs.Instalacao(), nil, pastas.ModoPortatil, logger)
	montarSync(hist, dono, n, m.a, logger)
	fila := hist.Fila(dono)
	ctx := context.Background()

	if _, err := m.a.Aprender(ctx, "ECF", "esse efe"); err != nil {
		t.Fatal(err)
	}
	id := dictionary.IDDoPar("esse efe")
	versao := func() int64 {
		t.Helper()
		es, err := fila.Espelho(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range es {
			if e.ID == id {
				return e.VersaoLocal
			}
		}
		return -1
	}
	// O id sem linha não ganha linha: a varredura o vê como par novo.
	if v := versao(); v != -1 {
		t.Fatalf("o gancho criou a linha do par novo (versão %d)", v)
	}
	// A varredura viu o par (a projeção e a versão local 1).
	depoisDoAprender := m.bytes(t)
	var proj *dictionary.ParDoPainel
	for _, p := range mustSemente(t, string(depoisDoAprender)).ParesDoPainel() {
		if p.ID == id {
			p := p
			proj = &p
		}
	}
	if proj == nil {
		t.Fatal("o par não está na projeção do arquivo")
	}
	if err := fila.MudarAprendido(ctx, time.Now(), []string{id}, func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Projecao: &storage.NovaProjecao{Par: proj}, MudouAqui: true}
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.a.Desfazer(ctx, "esse efe"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.a.Aprender(ctx, "ECF", "esse efe"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.bytes(t), depoisDoAprender) {
		t.Fatal("pré-condição: o Desfazer e o Aprender deixam o arquivo como era")
	}
	if v := versao(); v != 3 {
		t.Errorf("a versão local é %d; quero 3 (a da varredura e mais uma de cada clique)", v)
	}
	for _, proibido := range []string{"esse efe", "ECF"} {
		if strings.Contains(buf.String()+m.log.String(), proibido) {
			t.Errorf("o log traz %q", proibido)
		}
	}
}

// ─── A rodada pela configuração do Agent ───

// conexaoConectada é a conexão de um PC conectado, sem o Gerenciador de
// Credenciais: o token é de mentira, e só vai ao cliente de roteiro abaixo.
type conexaoConectada struct{}

func (conexaoConectada) Credencial(context.Context) (syncengine.Credencial, error) {
	return syncengine.Credencial{Configurada: true, Conectado: true, URL: "https://nuvem.exemplo.test",
		Token: "token-de-mentira", Aparelho: "0192f0a1-0000-7000-8000-0000000a9a1e", PareadoEm: "2026-10-08T12:00:00Z"}, nil
}

func (conexaoConectada) TokenRecusado(context.Context, string) error { return nil }

// nuvemVazia é a nuvem de uma conta sem nada: o pull devolve a página vazia,
// e nenhum envio é esperado (o histórico do teste está vazio, e antes do
// aceite nenhuma troca sobe).
type nuvemVazia struct {
	t      *testing.T
	pushes atomic.Int32
	// mudancas é o que o pull traz na primeira página.
	mudancas []syncengine.Change
}

func (n *nuvemVazia) Push(_ context.Context, muts []syncengine.Mutation) (syncengine.PushResult, error) {
	n.pushes.Add(1)
	n.t.Errorf("um envio de %d mutação(ões) saiu antes do aceite", len(muts))
	return syncengine.PushResult{}, errors.New("nenhum envio esperado")
}

func (n *nuvemVazia) Pull(_ context.Context, after int64, _ int) (syncengine.ChangePage, error) {
	if after == 0 && len(n.mudancas) > 0 {
		return syncengine.ChangePage{Changes: n.mudancas, LastSeq: int64(len(n.mudancas)), Epoch: "epoch-1"}, nil
	}
	return syncengine.ChangePage{LastSeq: after, Epoch: "epoch-1"}, nil
}

// rodadaPelaConfigDoAgent roda UMA rodada com a configuração que o Agent monta
// (configDoSync), trocando só a conexão e o cliente da nuvem pelos dublês: o
// resto (a fila, o aplicador) é o do montarSync.
func rodadaPelaConfigDoAgent(t *testing.T, hist *storage.SQLiteTranscriptionRepository, dono string, ap *aprendizadoDoAgent, nv syncengine.SyncClient) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&logTravado{}, nil))
	cfg := configDoSync(hist.Fila(dono), nil, ap, logger)
	cfg.Conexao = conexaoConectada{}
	cfg.Cliente = func(string, string) syncengine.SyncClient { return nv }
	if err := syncengine.NovoLaco(cfg).RodadaAgora(context.Background()); err != nil {
		t.Fatalf("rodada: %v", err)
	}
}

func historicoVazio(t *testing.T) (*storage.SQLiteTranscriptionRepository, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&logTravado{}, nil))
	hist, _, dono, fechar, err := abrirHistoricoEm(logger, filepath.Join(t.TempDir(), "historico.db"), "", clock.NewSystem())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fechar)
	return hist, dono
}

// A configuração do laço leva o aplicador do Agent, com dicionário ou sem
// (o desvio 37 revisto); sem o aprendizado (os testes da F7), nada.
func TestSync_ConfigDoLacoLevaOAplicador(t *testing.T) {
	hist, dono := historicoVazio(t)
	for _, usuario := range []string{"mauro", ""} {
		m := montarAprendizado(t, dicionarioDoAprendizado, usuario)
		cfg := configDoSync(hist.Fila(dono), nil, m.a, nil)
		if cfg.Dicionario != syncengine.DicionarioDoAprendido(m.a.Aplicador) {
			t.Errorf("usuário %q: o Dicionario do laço é %v; quero o aplicador do Agent", usuario, cfg.Dicionario)
		}
	}
	if cfg := configDoSync(hist.Fila(dono), nil, nil, nil); cfg.Dicionario != nil {
		t.Errorf("sem o aprendizado, o Dicionario é %v", cfg.Dicionario)
	}
}

// Com dicionário, a rodada do Agent varre o arquivo: o par aprendido no painel
// é o M da pergunta ("Vão subir N ditados e M trocas"), o aceite NÃO é gravado
// sozinho com o histórico vazio, e nada sobe antes do clique. Sem o aplicador
// no laço, a rodada não varreria, o M seria 0 e o aceite seria gravado.
func TestSync_ComDicionarioARodadaContaOM(t *testing.T) {
	hist, dono := historicoVazio(t)
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	if _, err := m.a.Aprender(context.Background(), "ECF", "esse efe"); err != nil {
		t.Fatal(err)
	}
	nv := &nuvemVazia{t: t}
	rodadaPelaConfigDoAgent(t, hist, dono, m.a, nv)

	fila := hist.Fila(dono)
	ctx := context.Background()
	if aceito, err := fila.EnvioAceito(ctx); err != nil || aceito {
		t.Errorf("o aceite foi gravado sozinho (%v, %v) com uma troca a subir", aceito, err)
	}
	r, err := fila.Resumo(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.Previa != 0 || r.Trocas != 1 {
		t.Errorf("a pergunta: N = %d, M = %d; quero 0 e 1", r.Previa, r.Trocas)
	}
}

// Sem dicionário na subida (a criação do vazio falhou), o aplicador sem
// arquivo vai ao laço: o M é desconhecido, e o aceite NÃO é gravado sozinho
// (o desvio 22: as trocas de um dicionário reposto depois subiriam sem a
// pergunta). O par que o pull traz fica "não aplicado neste PC"
// (arquivo_indisponivel), guardado com a versão, para valer quando o
// dicionário voltar, e o cursor anda.
func TestSync_SemDicionarioOAceiteNaoEGravadoSozinho(t *testing.T) {
	hist, dono := historicoVazio(t)
	m := montarAprendizado(t, dicionarioDoAprendizado, "")
	if m.a.TemArquivo() {
		t.Fatal("o aprendizado sem usuário tem arquivo")
	}
	id := dictionary.IDDoPar("esse efe")
	nv := &nuvemVazia{t: t, mudancas: []syncengine.Change{{Seq: 1, EntityType: storage.EntidadeAprendido, EntityID: id,
		Operation: syncengine.OpUpsert, EntityVersion: 3,
		PayloadAprendido: &syncengine.Aprendido{ID: id, Variant: "esse efe", Term: "ECF"}}}}
	rodadaPelaConfigDoAgent(t, hist, dono, m.a, nv)

	fila := hist.Fila(dono)
	ctx := context.Background()
	if aceito, err := fila.EnvioAceito(ctx); err != nil || aceito {
		t.Errorf("o aceite foi gravado sozinho (%v, %v) sem saber o M", aceito, err)
	}
	es, err := fila.Espelho(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 || es[0].Situacao != storage.EspelhoNaoAplicado || es[0].Motivo != string(dictionary.MotivoArquivoIndisponivel) ||
		es[0].Nuvem == nil || es[0].Nuvem.Versao == nil || *es[0].Nuvem.Versao != 3 {
		t.Fatalf("o espelho: %+v; quero o par da nuvem pendente, não aplicado por arquivo indisponível", es)
	}
	if _, err := m.a.Aprender(ctx, "ECF", "esse efe"); err != dictionary.ErrSemArquivo {
		t.Errorf("Aprender = %v", err)
	}
}

// O aviso do freio do aprendido chega ao painel pelo laço que o Agent liga
// (ligarSync): uma rodada cuja varredura segura mais de LimiteDoFreio pares
// saindo do arquivo à mão põe o número no historico.freio_trocas do GET
// /api/nuvem (nuvem.Servico.Estado, conectado). Sem o UsarFreio na ligação, o
// freio só chegaria ao log do laço, e o painel nunca avisaria.
func TestSync_OFreioChegaAoPainel(t *testing.T) {
	hist, prefs, dono := historicoComPrefs(t)
	ctx := context.Background()
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")

	// Pares que a nuvem confirmou e que o arquivo não tem mais, sem o
	// Desfazer do painel (a versão local é a da base): a remoção à mão.
	sairam := syncengine.LimiteDoFreio + 1
	var ids []string
	projecoes := map[string]*dictionary.ParDoPainel{}
	for i := 0; i < sairam; i++ {
		variante := "variante " + string(rune('a'+i))
		id := dictionary.IDDoPar(variante)
		ids = append(ids, id)
		projecoes[id] = &dictionary.ParDoPainel{ID: id, Variante: variante, Termo: "ECF"}
	}
	if err := hist.Fila(dono).MudarAprendido(ctx, time.Now(), ids, func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{Base: &storage.BaseDoAprendido{ServerVersion: 1}, Projecao: &storage.NovaProjecao{Par: projecoes[e.ID]}}
	}); err != nil {
		t.Fatal(err)
	}

	// O nuvem.Servico de um PC conectado (o aparelho na instalação e o token
	// no cofre de memória), com o resumo do histórico do Agent.
	cofre := chaves.NovaMemoria()
	if err := cofre.Gravar(nuvem.NomeNoCofre(true), "token-de-mentira"); err != nil {
		t.Fatal(err)
	}
	if err := prefs.Instalacao().Gravar(ctx, map[string]string{
		storage.ChaveNuvemAparelho: "0192f0a1-0000-7000-8000-0000000a9a1e", "nuvem_url": "https://nuvem.exemplo.test",
	}); err != nil {
		t.Fatal(err)
	}
	n := nuvem.Novo(nuvem.Config{Cofre: cofre, NomeNoCofre: nuvem.NomeNoCofre(true), Abrir: func(string) error { return nil },
		Instalacao: prefs.Instalacao(), NovoID: storage.NewID, Historico: historicoDaNuvem(hist, dono, ""),
		Log: slog.New(slog.DiscardHandler)})
	freio := func() int {
		t.Helper()
		e, err := n.Estado(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if e.Estado != nuvem.Conectado || e.Historico == nil {
			t.Fatalf("o estado não é o de um PC conectado com o resumo: %+v", e)
		}
		return e.Historico.FreioTrocas
	}
	if f := freio(); f != 0 {
		t.Fatalf("antes da rodada, o freio já é %d", f)
	}

	cfg := configDoSync(hist.Fila(dono), n, m.a, slog.New(slog.DiscardHandler))
	cfg.Conexao = conexaoConectada{}
	nv := &nuvemVazia{t: t}
	cfg.Cliente = func(string, string) syncengine.SyncClient { return nv }
	laco := ligarSync(cfg, hist, n, m.a)
	if err := laco.RodadaAgora(ctx); err != nil {
		t.Fatalf("rodada: %v", err)
	}
	if f := freio(); f != sairam {
		t.Errorf("o freio_trocas do painel é %d; quero %d (o laço diz %d)", f, sairam, laco.Freio())
	}
}

// historicoComPrefs abre um historico.db em t.TempDir() pela montagem do
// Agent, com as preferências (a tabela instalacao).
func historicoComPrefs(t *testing.T) (*storage.SQLiteTranscriptionRepository, *storage.SQLiteSettingsRepository, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&logTravado{}, nil))
	hist, prefs, dono, fechar, err := abrirHistoricoEm(logger, filepath.Join(t.TempDir(), "historico.db"), "", clock.NewSystem())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fechar)
	return hist, prefs, dono
}

// O painel que o run() monta (ligarAprendidoAoPainel) lê as linhas de estado
// do espelho do mesmo banco e do mesmo dono do laço: a troca da nuvem que não
// vale aqui aparece depois da lista do arquivo. Sem o espelho, o GET voltaria
// a ser só o arquivo, em silêncio.
func TestSync_PainelLeOEspelhoDoLaco(t *testing.T) {
	hist, dono := historicoVazio(t)
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	ctx := context.Background()
	if _, err := m.a.Aprender(ctx, "ECF", "esse efe"); err != nil {
		t.Fatal(err)
	}
	v := int64(2)
	id := dictionary.IDDoPar("efedê")
	if err := hist.Fila(dono).MudarAprendido(ctx, time.Now(), []string{id}, func(storage.EstadoDoAprendido) storage.AcaoDoAprendido {
		return storage.AcaoDoAprendido{
			Base:     &storage.BaseDoAprendido{ServerVersion: v},
			Nuvem:    &storage.ParGuardado{Versao: &v, Par: dictionary.ParDaNuvem{Variante: "efedê", Termo: "EFD", Dominio: "fiscal"}},
			Situacao: &storage.NovaSituacao{Situacao: storage.EspelhoNaoAplicado, Motivo: string(dictionary.MotivoDominioAusente)},
		}
	}); err != nil {
		t.Fatal(err)
	}

	p := ligarAprendidoAoPainel(localapi.Preferencias{Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{}},
		hist, dono, m.a)
	srv := localapi.New(dono, hist, storage.NewInMemorySettingsRepository(), p)
	w := chamar(t, srv.Handler(), http.MethodGet, "/api/aprendizado", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Items []map[string]string `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	quer := []map[string]string{
		{"termo": "ECF", "variante": "esse efe"},
		{"termo": "EFD", "variante": "efedê", "situacao": "nao_aplicado", "motivo": "dominio_ausente"},
	}
	if !reflect.DeepEqual(got.Items, quer) {
		t.Errorf("a lista do painel:\n%v\nquero:\n%v", got.Items, quer)
	}
}
