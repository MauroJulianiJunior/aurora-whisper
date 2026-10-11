package syncengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os percursos da varredura do aprendido (F8f3), com o dublê do PC: o
// dicionario.json de verdade (dictionary.Arquivo) numa pasta temporária, o
// historico.db de verdade em outra, e uma nuvem de roteiro. O Aprender e o
// Desfazer do painel são o Arquivo mais a versão local subindo no ato (o
// gancho da F8g, storage.Fila.SubirVersaoLocal); a mudança "à mão" grava o
// arquivo direto, sem o gancho. Nenhum teste toca o dicionário real, o
// teclado ou a área de transferência.

var agoraDaVarredura = time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC)

// nuvemDoAprendido é a nuvem de roteiro: responde cada mutação (por padrão,
// applied com a versão seguinte do id) e anota as que recebeu. O pull vem
// vazio. falhar faz o próximo push não ter resposta.
type nuvemDoAprendido struct {
	enviadas  []Mutation
	versoes   map[string]int64
	responder func(Mutation) MutationResult
	falhar    bool
}

func (n *nuvemDoAprendido) Push(_ context.Context, muts []Mutation) (PushResult, error) {
	if n.falhar {
		n.falhar = false
		n.enviadas = append(n.enviadas, muts...)
		return PushResult{}, transitorio(CodigoRede)
	}
	r := PushResult{Epoch: "e1"}
	for _, m := range muts {
		n.enviadas = append(n.enviadas, m)
		var res MutationResult
		if n.responder != nil && m.EntityType == storage.EntidadeAprendido {
			res = n.responder(m)
		} else {
			n.versoes[m.EntityID]++
			res = MutationResult{Status: StatusApplied, EntityVersion: n.versoes[m.EntityID]}
		}
		res.MutationID = m.MutationID
		r.Results = append(r.Results, res)
	}
	return r, nil
}

func (n *nuvemDoAprendido) Pull(_ context.Context, after int64, _ int) (ChangePage, error) {
	return ChangePage{LastSeq: after, Epoch: "e1"}, nil
}

// doId são as mutações que a nuvem recebeu do id.
func (n *nuvemDoAprendido) doId(id string) []Mutation {
	var out []Mutation
	for _, m := range n.enviadas {
		if m.EntityID == id {
			out = append(out, m)
		}
	}
	return out
}

// pcQueVarre é o PC da varredura: o espelho e o arquivo de verdade.
type pcQueVarre struct {
	pcDeTeste
	t     *testing.T
	arq   *dictionary.Arquivo
	motor *Motor
	nuvem *nuvemDoAprendido
	agora time.Time
	seq   int64
}

// sementeDaVarredura é o dicionário de partida: três termos, um com domínio.
func sementeDaVarredura() dictionary.Semente {
	return dictionary.Semente{
		Usuario:  "mauro",
		Dominios: map[string][]string{"contabil": {"SistemaContabil.exe"}},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "ECD", Prioridade: 90},
			{Canonica: "ECF", Prioridade: 90},
			{Canonica: "LALUR", Prioridade: 80, Dominio: "contabil"},
		},
	}
}

func novoPCQueVarre(t *testing.T) *pcQueVarre {
	t.Helper()
	p := novoPC(t)
	if _, _, err := p.fila.Preparar(context.Background(), "a1", "p1", agoraDaVarredura); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.Exec(`UPDATE sync_state SET sync_epoch = 'e1'`); err != nil {
		t.Fatal(err)
	}
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if err := sementeDaVarredura().Gravar(caminho); err != nil {
		t.Fatal(err)
	}
	pc := &pcQueVarre{pcDeTeste: p, t: t, arq: &dictionary.Arquivo{Caminho: caminho, Usuario: "mauro"},
		nuvem: &nuvemDoAprendido{versoes: map[string]int64{}}, agora: agoraDaVarredura}
	pc.motor = &Motor{Fila: p.fila, Cliente: pc.nuvem, Aparelho: "a1", PareadoEm: "p1", NovoID: storage.NewID,
		Agora: func() time.Time { return pc.agora }, Dicionario: pc.arq}
	return pc
}

func (p *pcQueVarre) varrer() ResultadoDaVarredura {
	p.t.Helper()
	r, err := p.motor.Varrer(context.Background(), "e1")
	if err != nil {
		p.t.Fatalf("a varredura: %v", err)
	}
	return r
}

func (p *pcQueVarre) enviar() {
	p.t.Helper()
	rel := novoRelatorio()
	if err := p.motor.Enviar(context.Background(), &rel); err != nil {
		p.t.Fatalf("o envio: %v", err)
	}
}

func (p *pcQueVarre) aceitar() {
	p.t.Helper()
	if _, err := p.db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_envio_aceito_em', 'x')
		ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`); err != nil {
		p.t.Fatal(err)
	}
}

// arquivo é o dicionario.json de agora.
func (p *pcQueVarre) arquivo() dictionary.Semente {
	p.t.Helper()
	s, err := p.arq.Atual()
	if err != nil {
		p.t.Fatal(err)
	}
	return s
}

func (p *pcQueVarre) bytesDoArquivo() []byte {
	p.t.Helper()
	b, err := os.ReadFile(p.arq.Caminho)
	if err != nil {
		p.t.Fatal(err)
	}
	return b
}

// aMao grava o arquivo direto, sem o painel (nem o gancho).
func (p *pcQueVarre) aMao(f func(*dictionary.Semente)) {
	p.t.Helper()
	s := p.arquivo()
	f(&s)
	if err := s.Gravar(p.arq.Caminho); err != nil {
		p.t.Fatal(err)
	}
}

// aprender e desfazer são o painel: o arquivo muda, e a versão local sobe no
// ato.
func (p *pcQueVarre) aprender(termo, variante string) string {
	p.t.Helper()
	if _, _, err := p.arq.Aprender(termo, variante); err != nil {
		p.t.Fatalf("aprender: %v", err)
	}
	id := dictionary.IDDoPar(variante)
	if err := p.fila.SubirVersaoLocal(context.Background(), []string{id}); err != nil {
		p.t.Fatal(err)
	}
	return id
}

func (p *pcQueVarre) desfazer(variante string) {
	p.t.Helper()
	if _, _, err := p.arq.Desfazer(variante); err != nil {
		p.t.Fatalf("desfazer: %v", err)
	}
	if err := p.fila.SubirVersaoLocal(context.Background(), []string{dictionary.IDDoPar(variante)}); err != nil {
		p.t.Fatal(err)
	}
}

// sincronizado é o par aprendido aqui que já subiu (com o aceite).
func (p *pcQueVarre) sincronizado(termo, variante string) string {
	p.t.Helper()
	id := p.aprender(termo, variante)
	p.varrer()
	p.enviar()
	if e := p.estado(id); e.ServerVersion == nil || e.MudouAqui() || e.Marcador != nil {
		p.t.Fatalf("o par não subiu: %+v", e)
	}
	return id
}

func (p *pcQueVarre) estado(id string) storage.EstadoDoAprendido {
	p.t.Helper()
	es, err := p.fila.Espelho(context.Background())
	if err != nil {
		p.t.Fatal(err)
	}
	for _, e := range es {
		if e.ID == id {
			return e
		}
	}
	return storage.EstadoDoAprendido{ID: id}
}

func (p *pcQueVarre) espelho() []storage.EstadoDoAprendido {
	p.t.Helper()
	es, err := p.fila.Espelho(context.Background())
	if err != nil {
		p.t.Fatal(err)
	}
	return es
}

// marcados são os ids com marcador learned.
func (p *pcQueVarre) marcados() []string {
	p.t.Helper()
	ms, err := p.fila.Marcadores(context.Background())
	if err != nil {
		p.t.Fatal(err)
	}
	var out []string
	for _, m := range ms {
		if m.EntityType == storage.EntidadeAprendido {
			out = append(out, m.EntityID)
		}
	}
	return out
}

// escritas é o total de linhas que esta conexão já mudou (o banco tem uma
// conexão só): igual antes e depois é "não escreveu".
func (p *pcQueVarre) escritas() int64 {
	p.t.Helper()
	var n int64
	if err := p.db.QueryRow(`SELECT total_changes()`).Scan(&n); err != nil {
		p.t.Fatal(err)
	}
	return n
}

func projecaoNo(s dictionary.Semente, id string) *dictionary.ParDoPainel {
	for _, par := range s.ParesDoPainel() {
		if par.ID == id {
			return &par
		}
	}
	return nil
}

// ateOArquivo são os passos 1 e 2 do pull de um par pelo arquivo de verdade
// (o PlanejarPar e a intenção antes da gravação), sem o passo 3: a página
// "caiu" depois do arquivo. Com gravar falso, cai também antes da gravação
// (a intenção foi gravada, e o arquivo não).
func (p *pcQueVarre) ateOArquivo(c Change, gravar bool) PlanoDoPar {
	p.t.Helper()
	id := strings.ToLower(c.EntityID)
	queda := errors.New("queda")
	var plano PlanoDoPar
	_, _, err := p.arq.AplicarPares(func(s dictionary.Semente) []dictionary.ParDaNuvem {
		plano = PlanejarPar(p.estado(id), c, ArquivoDoPar{Projecao: projecaoNo(s, id)})
		if !plano.Lote {
			p.t.Fatalf("o par não foi ao lote: %+v", plano)
		}
		return []dictionary.ParDaNuvem{plano.Par}
	}, func(l dictionary.Lote) error {
		a, ok := IntencaoDoPar(plano, l.Resultados[0])
		if !ok {
			p.t.Fatal("sem intenção")
		}
		if err := p.fila.MudarAprendido(context.Background(), p.agora, []string{id},
			func(storage.EstadoDoAprendido) storage.AcaoDoAprendido { return a }); err != nil {
			return err
		}
		if !gravar {
			return queda
		}
		return nil
	})
	if gravar && err != nil || !gravar && !errors.Is(err, queda) {
		p.t.Fatalf("o lote: %v", err)
	}
	if e := p.estado(id); e.Situacao != storage.EspelhoAplicando {
		p.t.Fatalf("a intenção não ficou: %+v", e)
	}
	return plano
}

// receber é o Receber (F8f4) reduzido a uma mudança, pelo arquivo de
// verdade: os três passos, com o cursor.
func (p *pcQueVarre) receber(c Change) DecisaoDoAprendido {
	p.t.Helper()
	ctx := context.Background()
	id := strings.ToLower(c.EntityID)
	var plano PlanoDoPar
	_, lote, err := p.arq.AplicarPares(func(s dictionary.Semente) []dictionary.ParDaNuvem {
		plano = PlanejarPar(p.estado(id), c, ArquivoDoPar{Projecao: projecaoNo(s, id)})
		if plano.Lote {
			return []dictionary.ParDaNuvem{plano.Par}
		}
		return nil
	}, func(l dictionary.Lote) error {
		if a, ok := IntencaoDoPar(plano, l.Resultados[0]); ok {
			return p.fila.MudarAprendido(ctx, p.agora, []string{id},
				func(storage.EstadoDoAprendido) storage.AcaoDoAprendido { return a })
		}
		return nil
	})
	var r *dictionary.ResultadoDoPar
	switch {
	case dictionary.ArquivoIndisponivel(err):
		plano = PlanejarPar(p.estado(id), c, ArquivoDoPar{Indisponivel: true})
	case err != nil:
		p.t.Fatal(err)
	case plano.Lote:
		r = &lote.Resultados[0]
	}
	p.seq++
	cursor := p.seq
	var d DecisaoDoAprendido
	if err := p.fila.AplicarComAprendido(ctx, "a1", "e1", nil, &cursor, nil, storage.RespostaDoAprendido{
		IDs: []string{id}, Agora: p.agora,
		Decidir: func(e storage.EstadoDoAprendido) storage.AcaoDoAprendido { d = DecidirPar(e, plano, r); return d.Acao },
	}); err != nil {
		p.t.Fatalf("a página: %v", err)
	}
	return d
}

// daNuvem é o par que chega pelo pull e passa a valer aqui.
func (p *pcQueVarre) daNuvem(versao int64, a *Aprendido) string {
	p.t.Helper()
	if d := p.receber(doPull(versao, a)); d.Tipo != TipoAplicado && d.Tipo != TipoTrocado {
		p.t.Fatalf("o par da nuvem não foi aplicado: %+v", d)
	}
	p.nuvem.versoes[a.ID] = versao
	return a.ID
}

// ─── O arquivo indisponível ───

// O arquivo ausente, inválido ou de outro usuário é "sem informação": a
// varredura não grava nada (nem o delete do par que a nuvem tem, nem a
// projeção), e o M não é conhecido.
func TestVarredura_OArquivoIndisponivelNuncaViraDelete(t *testing.T) {
	for nome, estragar := range map[string]func(p *pcQueVarre){
		"ausente": func(p *pcQueVarre) {
			if err := os.Remove(p.arq.Caminho); err != nil {
				t.Fatal(err)
			}
		},
		"inválido": func(p *pcQueVarre) {
			if err := os.WriteFile(p.arq.Caminho, []byte(`{"usuario": "mauro", "entradas": [`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"de outro usuário": func(p *pcQueVarre) {
			s := sementeDaVarredura()
			s.Usuario = "outra"
			if err := s.Gravar(p.arq.Caminho); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(nome, func(t *testing.T) {
			p := novoPCQueVarre(t)
			p.aceitar()
			sf := p.sincronizado("ECF", "sf")
			ecede := p.daNuvem(1, fioDoPar("ecede", "ECD", false, "", false))
			estragar(p)
			antes, escritas := p.espelho(), p.escritas()
			r := p.varrer()
			if !r.SemArquivo || r.Trocas != 0 || soma(r.Tipos) != 0 {
				t.Errorf("o resultado: %+v", r)
			}
			if p.escritas() != escritas || !reflect.DeepEqual(antes, p.espelho()) || len(p.marcados()) != 0 {
				t.Errorf("a varredura gravou com o arquivo %s", nome)
			}
			for _, id := range []string{sf, ecede} {
				if e := p.estado(id); !e.Presente() || e.MudouAqui() {
					t.Errorf("%s virou desfazer: %+v", id, e)
				}
			}
		})
	}
}

// Sem o arquivo, a rodada não grava o aceite sozinha, mesmo sem ditado (o M
// não é conhecido); com o arquivo sem nada a subir, grava.
func TestVarredura_SemArquivoORodadaNaoGravaOAceite(t *testing.T) {
	ctx := context.Background()
	p := novoPCQueVarre(t)
	if err := os.Remove(p.arq.Caminho); err != nil {
		t.Fatal(err)
	}
	rel, err := p.motor.Rodada(ctx)
	if err != nil || !rel.SemDicionario {
		t.Fatalf("a rodada: %+v, %v", rel, err)
	}
	if aceito, _ := p.fila.EnvioAceito(ctx); aceito {
		t.Fatal("o aceite foi gravado sem saber o M")
	}
	if err := sementeDaVarredura().Gravar(p.arq.Caminho); err != nil {
		t.Fatal(err)
	}
	if _, err := p.motor.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if aceito, _ := p.fila.EnvioAceito(ctx); !aceito {
		t.Fatal("N = 0 e M = 0, e o aceite não foi gravado")
	}
}

// ─── A varredura ociosa ───

// Com o arquivo igual ao que o espelho guarda, a varredura não escreve no
// SQLite nem no arquivo: o par que subiu, o que veio da nuvem (com a grafia
// do termo daqui), o pendente que continua não valendo aqui, o recusado e a
// etiqueta dentro do prazo. Antes do aceite também: o par segurado é só
// contado.
func TestVarredura_OciosaNaoEscreve(t *testing.T) {
	t.Run("depois do aceite", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		p.sincronizado("ECF", "sf")
		p.daNuvem(1, fioDoPar("ecede", "ecd", false, "", false))
		if d := p.receber(doPull(1, fioDoPar("nfe", "ECD", true, "fiscal", false))); d.Tipo != TipoNaoAplicado {
			t.Fatalf("o não aplicado: %+v", d)
		}
		p.nuvem.responder = func(Mutation) MutationResult {
			return MutationResult{Status: StatusRejected, Code: "campo_invalido"}
		}
		recusado := p.aprender("LALUR", "lalu")
		p.varrer()
		p.enviar()
		if e := p.estado(recusado); e.Situacao != storage.EspelhoRecusado {
			t.Fatalf("o recusado: %+v", e)
		}
		p.varrer()
		bytesAntes, escritas, antes := p.bytesDoArquivo(), p.escritas(), p.espelho()
		for i := 0; i < 3; i++ {
			if r := p.varrer(); soma(r.Tipos) != 0 || soma(r.Reaplicados) != 0 || r.Trocas != 0 {
				t.Errorf("a varredura %d contou mudança: %+v", i, r)
			}
		}
		if p.escritas() != escritas {
			t.Errorf("a varredura ociosa escreveu %d linhas no SQLite", p.escritas()-escritas)
		}
		if !bytes.Equal(bytesAntes, p.bytesDoArquivo()) || !reflect.DeepEqual(antes, p.espelho()) {
			t.Error("a varredura ociosa mudou o arquivo ou o espelho")
		}
	})
	t.Run("antes do aceite", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aprender("ECF", "sf")
		if r := p.varrer(); r.Trocas != 1 {
			t.Fatalf("o M: %+v", r)
		}
		escritas := p.escritas()
		if r := p.varrer(); r.Trocas != 1 || soma(r.Tipos) != 0 {
			t.Errorf("a segunda varredura: %+v", r)
		}
		if p.escritas() != escritas {
			t.Error("a varredura ociosa escreveu antes do aceite")
		}
	})
}

// ─── Antes do aceite (D3) ───

// Antes do clique, nenhum upsert nasce: nem o do par novo, nem o da troca de
// termo de um par que a nuvem tem (os dois são o M). Sai só o delete de um
// id que a nuvem tem; o par aprendido e desfeito antes do clique não gera
// nada. A rodada não grava o aceite sozinha com M > 0, e a pergunta vale com
// N = 0. Depois do clique, a varredura solta exatamente os M upserts.
func TestVarredura_AntesDoAceite(t *testing.T) {
	ctx := context.Background()
	p := novoPCQueVarre(t)
	sf := p.daNuvem(1, fioDoPar("sf", "ECF", false, "", false))
	ecede := p.daNuvem(1, fioDoPar("ecede", "ECD", false, "", false))
	lalu := p.aprender("LALUR", "lalu")
	p.desfazer("ecede")
	p.aprender("ECF", "ecede") // a troca de termo de um par que a nuvem tem
	p.desfazer("sf")           // o desfazer de um par que a nuvem tem
	xis := p.aprender("ECD", "xis")
	p.desfazer("xis") // aprendido e desfeito antes do clique: nunca subiu

	rel, err := p.motor.Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Trocas != 2 {
		t.Errorf("o M = %d, want 2 (o par novo e a troca de termo)", rel.Trocas)
	}
	if aceito, _ := p.fila.EnvioAceito(ctx); aceito {
		t.Fatal("a rodada gravou o aceite com M > 0 e N = 0")
	}
	if r, err := p.fila.Resumo(ctx, p.agora); err != nil || r.Previa != 0 || r.Aceito {
		t.Fatalf("a pergunta com N = 0: %+v, %v", r, err)
	}
	if len(p.nuvem.enviadas) != 1 {
		t.Fatalf("antes do clique subiram %d mutações, want só o delete", len(p.nuvem.enviadas))
	}
	if m := p.nuvem.enviadas[0]; m.EntityID != sf || m.Operation != OpDelete || m.BaseVersion != 1 || m.Payload != nil {
		t.Errorf("o delete antes do clique: %+v", m)
	}
	for _, id := range []string{ecede, lalu} {
		if e := p.estado(id); e.Marcador != nil || !e.MudouAqui() || !e.Presente() {
			t.Errorf("o upsert segurado de %s: %+v", id, e)
		}
	}
	if e := p.estado(xis); e.Marcador != nil || e.MudouAqui() || e.Presente() {
		t.Errorf("o par que nunca subiu: %+v", e)
	}

	// O clique solta os M upserts, e só eles.
	if _, err := p.fila.EnviarHistorico(ctx, filepath.Join(t.TempDir(), "historico.db"), p.agora); err != nil {
		t.Fatal(err)
	}
	r := p.varrer()
	if r.Trocas != 0 || r.Tipos[TipoVarreduraUpsert] != 2 {
		t.Errorf("depois do clique: %+v", r)
	}
	if got := p.marcados(); !reflect.DeepEqual(got, ordenados(ecede, lalu)) {
		t.Errorf("os marcados depois do clique: %v", got)
	}
	p.enviar()
	if n := len(p.nuvem.enviadas); n != 3 {
		t.Fatalf("subiram %d mutações no total, want 3", n)
	}
	for _, m := range p.nuvem.enviadas[1:] {
		if m.Operation != OpUpsert {
			t.Errorf("depois do clique: %+v", m)
		}
		if m.EntityID == ecede && (m.BaseVersion != 1 ||
			string(m.Payload) != `{"variant":"ecede","term":"ECF","restricted":false,"domain":null}`) {
			t.Errorf("a troca de termo: %+v %s", m, m.Payload)
		}
	}
}

func ordenados(ids ...string) []string {
	out := append([]string(nil), ids...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// ─── Adotar, e o par que está no arquivo e não sobe ───

// A variante que perdeu só a origem "painel" e continua no termo foi
// adotada: não é desfazer. Nada sobe, a linha passa a "já vale aqui", e nem o
// epoch novo a manda como delete. A mesma variante movida para outro termo,
// sem a origem, é desfazer.
func TestVarredura_AdotarNaoEDesfazer(t *testing.T) {
	ctx := context.Background()
	p := novoPCQueVarre(t)
	p.aceitar()
	sf := p.sincronizado("ECF", "sf")
	ecede := p.sincronizado("ECD", "ecede")
	p.aMao(func(s *dictionary.Semente) {
		delete(s.AliasOrigem, "sf") // adotada: continua em ECF
		delete(s.AliasOrigem, "ecede")
		s.Entradas[0].Alias = nil // "ecede" sai de ECD...
		s.Entradas[1].Alias = append(s.Entradas[1].Alias, "ecede")
	})
	r := p.varrer()
	if r.Tipos[TipoVarreduraAdotado] != 1 || r.Tipos[TipoVarreduraDelete] != 1 {
		t.Errorf("a varredura: %+v", r)
	}
	e := p.estado(sf)
	if e.Marcador != nil || e.Presente() || e.MudouAqui() || e.DesfeitoAqui() || e.Situacao != storage.EspelhoJaValeAqui {
		t.Errorf("a adotada: %+v", e)
	}
	if e := p.estado(ecede); e.Marcador == nil || e.Presente() {
		t.Errorf("a movida para outro termo: %+v", e)
	}
	p.enviar()
	if n := len(p.nuvem.doId(sf)); n != 1 {
		t.Errorf("a adotada subiu %d vezes (só o upsert de quando foi aprendida)", n)
	}
	// O epoch novo não a manda como delete.
	if err := p.fila.Recomecar(ctx, "a1", "p1", p.agora); err != nil {
		t.Fatal(err)
	}
	if e := p.estado(sf); e.Marcador != nil {
		t.Errorf("o epoch novo marcou a adotada: %+v", e)
	}
}

// O par que continua no arquivo com a origem "painel", mas não sobe
// (numa entrada restrita sem domínio, ou repetido em duas entradas), não vira
// delete, e a varredura nem escreve. Volta a subir quando o arquivo volta.
func TestVarredura_OParNoArquivoQueNaoSobeNaoViraDelete(t *testing.T) {
	for nome, mexer := range map[string]func(*dictionary.Semente){
		"restrito sem domínio": func(s *dictionary.Semente) { s.Entradas[1].Restrito = true },
		"repetido":             func(s *dictionary.Semente) { s.Entradas[0].Alias = append(s.Entradas[0].Alias, "sf") },
	} {
		t.Run(nome, func(t *testing.T) {
			p := novoPCQueVarre(t)
			p.aceitar()
			sf := p.sincronizado("ECF", "sf")
			antes := p.bytesDoArquivo()
			p.aMao(mexer)
			if projecaoNo(p.arquivo(), sf) != nil {
				t.Fatal("o par ainda sobe: o teste não prova nada")
			}
			escritas := p.escritas()
			if r := p.varrer(); soma(r.Tipos) != 0 || r.Freio != 0 {
				t.Errorf("a varredura: %+v", r)
			}
			if p.escritas() != escritas || len(p.marcados()) != 0 {
				t.Error("o par que não sobe virou mudança")
			}
			if err := os.WriteFile(p.arq.Caminho, antes, 0o600); err != nil {
				t.Fatal(err)
			}
			if r := p.varrer(); soma(r.Tipos) != 0 || p.escritas() != escritas {
				t.Errorf("de volta ao arquivo de antes: %+v", r)
			}
		})
	}
}

// ─── O freio ───

// Mais de LimiteDoFreio pares saindo do arquivo à mão numa varredura: nada é
// gravado (nem o delete, nem a projeção), e o freio é avisado; com o
// limite, os deletes saem. O Desfazer do painel (a versão local sobe no ato)
// não conta para o freio.
func TestVarredura_OFreio(t *testing.T) {
	variantes := make([]string, LimiteDoFreio+1)
	for i := range variantes {
		variantes[i] = fmt.Sprintf("v%c", 'a'+i)
	}
	semAs := func(s *dictionary.Semente, vs []string) {
		for _, v := range vs {
			sem, _, err := s.DesfazerVariante(v)
			if err != nil {
				panic(err)
			}
			*s = sem
		}
	}
	t.Run("à mão", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		for _, v := range variantes {
			p.aprender("ECF", v)
		}
		p.varrer()
		p.enviar()
		antes, escritas := p.bytesDoArquivo(), p.escritas()
		p.aMao(func(s *dictionary.Semente) { semAs(s, variantes) })
		r := p.varrer()
		if r.Freio != LimiteDoFreio+1 || soma(r.Tipos) != 0 {
			t.Fatalf("o freio: %+v", r)
		}
		if p.escritas() != escritas || len(p.marcados()) != 0 {
			t.Fatal("o freio deixou gravar")
		}
		for _, v := range variantes {
			if e := p.estado(dictionary.IDDoPar(v)); !e.Presente() || e.MudouAqui() {
				t.Errorf("%s: o freio mexeu na linha: %+v", v, e)
			}
		}
		// Com o limite exato, sai.
		if err := os.WriteFile(p.arq.Caminho, antes, 0o600); err != nil {
			t.Fatal(err)
		}
		p.aMao(func(s *dictionary.Semente) { semAs(s, variantes[:LimiteDoFreio]) })
		if r := p.varrer(); r.Freio != 0 || r.Tipos[TipoVarreduraDelete] != LimiteDoFreio {
			t.Fatalf("no limite: %+v", r)
		}
	})
	t.Run("pelo painel", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		for _, v := range variantes {
			p.aprender("ECF", v)
		}
		p.varrer()
		p.enviar()
		for _, v := range variantes {
			p.desfazer(v)
		}
		if r := p.varrer(); r.Freio != 0 || r.Tipos[TipoVarreduraDelete] != len(variantes) {
			t.Fatalf("o Desfazer do painel freou: %+v", r)
		}
	})
}

// ─── A linha "aplicando" ───

// A intenção do aplicador e o arquivo: igual à esperada, a varredura
// confirma, sem envio (o id fica no "veio da nuvem", e a adaptação deste PC
// não sobe), e o par da nuvem fica pendente até a tentativa de novo dele
// andar a base; igual à guardada (o arquivo não chegou a ser gravado), a
// intenção sai e o par volta a pendente, sem envio, e entra na varredura
// seguinte; diferente das duas, a intenção sai e o par sobe como mudado
// aqui.
func TestVarredura_Aplicando(t *testing.T) {
	ecf := fioDoPar("sf", "ecf", false, "", false) // o termo daqui é "ECF"
	t.Run("igual à esperada", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		p.ateOArquivo(doPull(1, ecf), true)
		esperada := p.estado(idSF).Esperada
		if esperada == nil || esperada.Termo != "ECF" {
			t.Fatalf("a esperada: %+v", esperada)
		}
		if r := p.varrer(); r.Tipos[TipoVarreduraConfirmado] != 1 {
			t.Errorf("a varredura: %+v", r)
		}
		// O par da nuvem guardado fica pendente, com a versão: a base anda
		// pela tentativa de novo dele, e não pela confirmação.
		e := p.estado(idSF)
		if e.Situacao != storage.EspelhoAplicado || !reflect.DeepEqual(e.Projecao, esperada) || e.Esperada != nil ||
			e.Nuvem == nil || *e.Nuvem.Versao != 1 || !TemPendente(e) || e.ServerVersion != nil || e.Marcador != nil ||
			e.MudouAqui() || !e.VeioDaNuvem {
			t.Fatalf("a confirmação: %+v", e)
		}
		// A varredura seguinte converge o pendente (a base anda), sem gravar o
		// arquivo e sem envio; a próxima é ociosa, e o pull da mesma versão é
		// eco. (O pull da mesma versão que vem antes dela converge:
		// receber_percurso_test.go, a queda.)
		bytesAntes := p.bytesDoArquivo()
		if r := p.varrer(); r.Reaplicados[TipoConvergente] != 1 {
			t.Errorf("a tentativa de novo do pendente: %+v", r)
		}
		e = p.estado(idSF)
		if e.ServerVersion == nil || *e.ServerVersion != 1 || e.Nuvem != nil || e.Situacao != storage.EspelhoAplicado ||
			!bytes.Equal(bytesAntes, p.bytesDoArquivo()) {
			t.Fatalf("o pendente convergiu: %+v", e)
		}
		escritas := p.escritas()
		p.varrer()
		p.enviar()
		if p.escritas() != escritas || len(p.nuvem.enviadas) != 0 {
			t.Error("a confirmação virou envio")
		}
		if d := p.receber(doPull(1, ecf)); d.Tipo != TipoEco {
			t.Errorf("o pull de novo: %+v", d)
		}
	})
	t.Run("igual à guardada", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		antes := p.bytesDoArquivo()
		p.ateOArquivo(doPull(1, ecf), false)
		if !bytes.Equal(antes, p.bytesDoArquivo()) {
			t.Fatal("o arquivo foi gravado")
		}
		if r := p.varrer(); r.Tipos[TipoVarreduraDeVolta] != 1 || soma(r.Reaplicados) != 0 {
			t.Errorf("a varredura: %+v", r)
		}
		e := p.estado(idSF)
		if e.Situacao != storage.EspelhoNenhuma || e.Esperada != nil || e.Nuvem == nil || *e.Nuvem.Versao != 1 ||
			!TemPendente(e) || e.Marcador != nil || e.Presente() {
			t.Fatalf("de volta a pendente: %+v", e)
		}
		// A varredura seguinte o tenta de novo, e ele entra, sem envio.
		if r := p.varrer(); r.Reaplicados[TipoAplicado] != 1 {
			t.Errorf("a tentativa de novo: %+v", r)
		}
		e = p.estado(idSF)
		if e.Situacao != storage.EspelhoAplicado || e.ServerVersion == nil || *e.ServerVersion != 1 || TemPendente(e) ||
			projecaoNo(p.arquivo(), idSF) == nil {
			t.Fatalf("aplicado: %+v", e)
		}
		p.varrer()
		p.enviar()
		if len(p.nuvem.enviadas) != 0 {
			t.Error("o par da nuvem subiu")
		}
	})
	t.Run("diferente das duas", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		p.ateOArquivo(doPull(1, ecf), true)
		p.aMao(func(s *dictionary.Semente) {
			sem, _, err := s.DesfazerVariante("sf")
			if err != nil {
				t.Fatal(err)
			}
			*s, _, err = sem.AprenderVariante("ECD", "sf")
			if err != nil {
				t.Fatal(err)
			}
		})
		p.varrer()
		e := p.estado(idSF)
		if e.Situacao != storage.EspelhoNenhuma || e.Esperada != nil || e.Projecao.Termo != "ECD" || !e.MudouAqui() ||
			e.Marcador == nil || e.VeioDaNuvem {
			t.Fatalf("mudado aqui: %+v", e)
		}
	})
}

// ─── O pendente e a adaptação ───

// O par da nuvem que não vale aqui (o domínio não existe) fica pendente, e a
// varredura não grava nada enquanto a causa não some; quando o domínio é
// criado, ela o aplica, com a adaptação deste PC (o alias_restrito, a grafia
// do termo daqui), sem envio. A adaptação nunca volta como upsert. O par
// pendente do arquivo indisponível entra quando o arquivo volta.
func TestVarredura_OPendenteReaplicadoEAAdaptacaoNaoVolta(t *testing.T) {
	t.Run("o domínio criado aqui", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		if d := p.receber(doPull(1, fioDoPar("sf", "ecd", true, "fiscal", false))); d.Tipo != TipoNaoAplicado {
			t.Fatalf("o pull: %+v", d)
		}
		escritas := p.escritas()
		if r := p.varrer(); soma(r.Reaplicados) != 0 || p.escritas() != escritas {
			t.Fatalf("a causa não sumiu, e a varredura gravou: %+v", r)
		}
		p.aMao(func(s *dictionary.Semente) {
			s.Dominios["fiscal"] = []string{"Fiscal.exe"}
			s.Entradas[0].Dominio = "fiscal"
		})
		if r := p.varrer(); r.Reaplicados[TipoAplicado] != 1 {
			t.Fatalf("o pendente: %+v", r)
		}
		quero := &dictionary.ParDoPainel{ID: idSF, Variante: "sf", Termo: "ECD", Restrito: true, Dominio: "fiscal",
			EmAliasRestrito: true}
		if got := projecaoNo(p.arquivo(), idSF); !reflect.DeepEqual(got, quero) {
			t.Fatalf("a adaptação deste PC: %+v", got)
		}
		e := p.estado(idSF)
		if e.Situacao != storage.EspelhoAplicado || !reflect.DeepEqual(e.Projecao, quero) || *e.ServerVersion != 1 ||
			!e.VeioDaNuvem || e.Nuvem != nil || e.MudouAqui() {
			t.Fatalf("o espelho: %+v", e)
		}
		escritas = p.escritas()
		p.varrer()
		p.enviar()
		if len(p.nuvem.enviadas) != 0 || p.escritas() != escritas {
			t.Errorf("a adaptação voltou: %+v", p.nuvem.enviadas)
		}
	})
	t.Run("o arquivo que volta", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		antes := p.bytesDoArquivo()
		if err := os.Remove(p.arq.Caminho); err != nil {
			t.Fatal(err)
		}
		if d := p.receber(doPull(1, fioDoPar("sf", "ECF", false, "", false))); d.Tipo != TipoPendente {
			t.Fatalf("o pull: %+v", d)
		}
		if r := p.varrer(); !r.SemArquivo {
			t.Fatalf("sem o arquivo: %+v", r)
		}
		if err := os.WriteFile(p.arq.Caminho, antes, 0o600); err != nil {
			t.Fatal(err)
		}
		if r := p.varrer(); r.Reaplicados[TipoAplicado] != 1 {
			t.Fatalf("o arquivo voltou: %+v", r)
		}
		if e := p.estado(idSF); e.Situacao != storage.EspelhoAplicado || projecaoNo(p.arquivo(), idSF) == nil {
			t.Fatalf("o espelho: %+v", e)
		}
	})
}

// ─── O par sem versão da nuvem ───

// O par que a nuvem nunca teve sai sem envio: o marcador que esperava sai, e
// a mudança é assentada (nem o epoch novo a manda). Com o envio em voo, a
// varredura só sobe a versão local, e o delete sai depois que a resposta
// traz a versão.
func TestVarredura_OParSemVersaoDaNuvemSaiSemEnvio(t *testing.T) {
	ctx := context.Background()
	t.Run("o marcador que esperava", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		sf := p.aprender("ECF", "sf")
		p.varrer()
		if len(p.marcados()) != 1 {
			t.Fatal("o upsert não esperava na fila")
		}
		p.desfazer("sf")
		if r := p.varrer(); r.Tipos[TipoVarreduraAssentado] != 1 {
			t.Errorf("a varredura: %+v", r)
		}
		e := p.estado(sf)
		if e.Marcador != nil || e.Presente() || e.MudouAqui() || e.DesfeitoAqui() {
			t.Fatalf("assentado: %+v", e)
		}
		p.enviar()
		if err := p.fila.Recomecar(ctx, "a1", "p1", p.agora); err != nil {
			t.Fatal(err)
		}
		p.varrer()
		p.enviar()
		if len(p.nuvem.enviadas) != 0 {
			t.Errorf("subiu: %+v", p.nuvem.enviadas)
		}
	})
	t.Run("em voo", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		sf := p.aprender("ECF", "sf")
		p.varrer()
		p.nuvem.falhar = true
		rel := novoRelatorio()
		if err := p.motor.Enviar(ctx, &rel); err == nil {
			t.Fatal("o push sem resposta")
		}
		p.desfazer("sf")
		if r := p.varrer(); r.Tipos[TipoVarreduraEmVoo] != 0 {
			// O Desfazer do painel já subiu a versão local: a varredura não
			// precisa subir de novo.
			t.Errorf("a varredura em voo: %+v", r)
		}
		if e := p.estado(sf); !e.Presente() || !e.Marcador.EmVoo() || !e.MudouAqui() {
			t.Fatalf("em voo: %+v", e)
		}
		p.enviar() // o reenvio, o mesmo upsert; a nuvem o aplica
		if m := p.nuvem.enviadas; len(m) != 2 || !reflect.DeepEqual(m[0], m[1]) || m[0].Operation != OpUpsert {
			t.Fatalf("o reenvio: %+v", m)
		}
		if r := p.varrer(); r.Tipos[TipoVarreduraDelete] != 1 {
			t.Fatalf("o delete depois da resposta: %+v", r)
		}
		p.enviar()
		if m := p.nuvem.enviadas[2]; m.Operation != OpDelete || m.BaseVersion != 1 {
			t.Errorf("o delete: %+v", m)
		}
	})
}

// ─── A conta nova ───

// Depois do esquecer, a conta nova recebe só o que foi aprendido aqui: o par
// que veio da nuvem esquecida (no "veio da nuvem") não sobe, nem depois do
// clique.
func TestVarredura_OVeioDaNuvemNaoSobeParaAContaNova(t *testing.T) {
	ctx := context.Background()
	p := novoPCQueVarre(t)
	if _, err := p.db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_conta', 'c1')`); err != nil {
		t.Fatal(err)
	}
	p.aceitar()
	ecede := p.daNuvem(1, fioDoPar("ecede", "ECD", false, "", false))
	sf := p.sincronizado("ECF", "sf")
	if err := p.fila.EsquecerNuvem(ctx, p.agora); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'a2'),
		('nuvem_conectado_em', 'p2'), ('nuvem_conta', 'c2')`); err != nil {
		t.Fatal(err)
	}
	p.motor.Aparelho, p.motor.PareadoEm = "a2", "p2"
	p.nuvem.enviadas = nil
	rel, err := p.motor.Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Trocas != 1 || len(p.nuvem.enviadas) != 0 {
		t.Fatalf("antes do clique na conta nova: M = %d, %+v", rel.Trocas, p.nuvem.enviadas)
	}
	if _, err := p.fila.EnviarHistorico(ctx, filepath.Join(t.TempDir(), "historico.db"), p.agora); err != nil {
		t.Fatal(err)
	}
	if _, err := p.motor.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.nuvem.doId(ecede)) != 0 {
		t.Error("o par da conta esquecida subiu para a conta nova")
	}
	if ms := p.nuvem.doId(sf); len(ms) != 1 || ms[0].Operation != OpUpsert || ms[0].BaseVersion != 0 {
		t.Errorf("o aprendido aqui na conta nova: %+v", ms)
	}
	if e := p.estado(ecede); !e.VeioDaNuvem || e.Marcador != nil {
		t.Errorf("o veio da nuvem: %+v", e)
	}
}

// ─── A recusa ───

// Uma nuvem que recusa tudo gera uma tentativa por mudança, e não uma por
// rodada: a projeção recusada não é mandada de novo; a mudança seguinte do
// par vai uma vez; e o desfazer de um par que a nuvem nunca aceitou nem sai.
func TestMotor_ANuvemQueRecusaTudoTemUmaTentativaPorMudanca(t *testing.T) {
	ctx := context.Background()
	p := novoPCQueVarre(t)
	p.aceitar()
	p.nuvem.responder = func(Mutation) MutationResult {
		return MutationResult{Status: StatusRejected, Code: "campo_invalido"}
	}
	rodadas := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			if _, err := p.motor.Rodada(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	sf := p.aprender("ECF", "sf")
	rodadas(3)
	if n := len(p.nuvem.doId(sf)); n != 1 {
		t.Fatalf("o par recusado subiu %d vezes em 3 rodadas", n)
	}
	if e := p.estado(sf); e.Situacao != storage.EspelhoRecusado || e.Motivo != "campo_invalido" {
		t.Fatalf("o recusado: %+v", e)
	}
	p.desfazer("sf")
	p.aprender("ECD", "sf")
	rodadas(3)
	if n := len(p.nuvem.doId(sf)); n != 2 {
		t.Fatalf("a mudança subiu %d vezes no total, want 2", n)
	}
	p.desfazer("sf")
	rodadas(2)
	if n := len(p.nuvem.doId(sf)); n != 2 {
		t.Errorf("o desfazer de um par que a nuvem nunca teve subiu (%d)", n)
	}
}

// ─── O Desfazer e o Aprender pelo gancho ───

// O Desfazer seguido do Aprender do mesmo par pelo painel deixa o arquivo
// como era, e é mudança daqui (a versão local subiu no ato): vira upsert,
// com a base da nuvem. Antes do aceite, é o M. A mesma volta feita à mão,
// sem o gancho, não é vista (risco aceito na ADR).
func TestVarredura_ODesfazerEOAprenderPeloGanchoViramUpsert(t *testing.T) {
	t.Run("depois do aceite", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		sf := p.sincronizado("ECF", "sf")
		antes := p.bytesDoArquivo()
		p.desfazer("sf")
		p.aprender("ECF", "sf")
		if !bytes.Equal(antes, p.bytesDoArquivo()) {
			t.Fatal("o arquivo não voltou ao que era")
		}
		if r := p.varrer(); r.Tipos[TipoVarreduraUpsert] != 1 {
			t.Errorf("a varredura: %+v", r)
		}
		p.enviar()
		if ms := p.nuvem.doId(sf); len(ms) != 2 || ms[1].Operation != OpUpsert || ms[1].BaseVersion != 1 {
			t.Errorf("o upsert: %+v", ms)
		}
	})
	t.Run("antes do aceite", func(t *testing.T) {
		p := novoPCQueVarre(t)
		sf := p.daNuvem(1, fioDoPar("sf", "ECF", false, "", false))
		p.desfazer("sf")
		p.aprender("ECF", "sf")
		if r := p.varrer(); r.Trocas != 1 || len(p.marcados()) != 0 {
			t.Fatalf("antes do clique: %+v", r)
		}
		if e := p.estado(sf); e.VeioDaNuvem {
			t.Error("a pessoa aprendeu o par aqui, e ele ficou no veio da nuvem")
		}
	})
	t.Run("à mão", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		p.sincronizado("ECF", "sf")
		p.aMao(func(s *dictionary.Semente) {
			sem, _, _ := s.DesfazerVariante("sf")
			*s, _, _ = sem.AprenderVariante("ECF", "sf")
		})
		if r := p.varrer(); soma(r.Tipos) != 0 {
			t.Errorf("a volta à mão foi vista: %+v", r)
		}
	})
}

// ─── A etiqueta ───

// D4: o Desfazer e o Aprender daqui perdem para o desfazer de outro aparelho;
// a linha ganha "desfeito em outro aparelho", e o par sai daqui. A etiqueta
// some aos 30 dias (o relógio do motor) e quando a pessoa aprende o par de
// novo; nos dois casos, o texto dela sai junto.
func TestVarredura_AEtiquetaSome(t *testing.T) {
	comEtiqueta := func(t *testing.T) (*pcQueVarre, string) {
		p := novoPCQueVarre(t)
		p.aceitar()
		sf := p.sincronizado("ECF", "sf")
		p.desfazer("sf")
		p.aprender("ECF", "sf")
		p.varrer()
		p.nuvem.responder = func(Mutation) MutationResult {
			return MutationResult{Status: StatusConflict, EntityVersion: 2,
				StateAprendido: fioDoPar("sf", "ECF", false, "", true)}
		}
		p.enviar()
		p.nuvem.responder = nil
		if r := p.varrer(); r.Reaplicados[TipoDesfeito] != 1 {
			t.Fatalf("o state do conflict: %+v", r)
		}
		e := p.estado(sf)
		if e.Situacao != storage.EspelhoDesfeitoEmOutro || e.Nuvem == nil || e.Presente() || *e.ServerVersion != 2 ||
			projecaoNo(p.arquivo(), sf) != nil {
			t.Fatalf("a etiqueta: %+v", e)
		}
		return p, sf
	}
	t.Run("aos 30 dias", func(t *testing.T) {
		p, sf := comEtiqueta(t)
		comeco := *p.estado(sf).SituacaoEm
		p.agora = comeco.Add(PrazoDaEtiqueta - time.Minute)
		if r := p.varrer(); soma(r.Tipos) != 0 || p.estado(sf).Situacao != storage.EspelhoDesfeitoEmOutro {
			t.Fatalf("antes do prazo: %+v", r)
		}
		p.agora = comeco.Add(PrazoDaEtiqueta)
		if r := p.varrer(); r.Tipos[TipoVarreduraEtiqueta] != 1 {
			t.Errorf("no prazo: %+v", r)
		}
		e := p.estado(sf)
		if e.Situacao != storage.EspelhoNenhuma || e.Nuvem != nil || e.Projecao != nil || e.Marcador != nil {
			t.Fatalf("a etiqueta vencida: %+v", e)
		}
	})
	t.Run("a pessoa aprende de novo", func(t *testing.T) {
		p, sf := comEtiqueta(t)
		p.aprender("ECF", "sf")
		p.varrer()
		e := p.estado(sf)
		if e.Situacao != storage.EspelhoNenhuma || e.Nuvem != nil || e.Marcador == nil {
			t.Fatalf("aprendeu de novo: %+v", e)
		}
		p.enviar()
		ms := p.nuvem.doId(sf)
		if m := ms[len(ms)-1]; m.Operation != OpUpsert || m.BaseVersion != 2 {
			t.Errorf("o reaprender: %+v", m)
		}
	})
}

// O prazo de 30 dias é só das etiquetas. Depois dele, o "aplicado" e o "já
// vale aqui" continuam na lista (a varredura nem escreve), e o "não aplicado"
// continua pendente: a varredura o aplica quando a causa some, mesmo meses
// depois.
func TestVarredura_AsOutrasSituacoesNaoVencem(t *testing.T) {
	alemDoPrazo := func(p *pcQueVarre, id string) {
		p.t.Helper()
		e := p.estado(id)
		if e.SituacaoEm == nil {
			p.t.Fatalf("a situação sem data: %+v", e)
		}
		p.agora = e.SituacaoEm.Add(PrazoDaEtiqueta + time.Hour)
	}
	ficam := func(t *testing.T, p *pcQueVarre, id string, s storage.SituacaoDoEspelho) {
		t.Helper()
		alemDoPrazo(p, id)
		escritas := p.escritas()
		if r := p.varrer(); soma(r.Tipos) != 0 || soma(r.Reaplicados) != 0 || p.escritas() != escritas {
			t.Errorf("a varredura gravou: %+v", r)
		}
		if e := p.estado(id); e.Situacao != s {
			t.Errorf("a situação venceu: %+v", e)
		}
	}
	t.Run("o aplicado", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		sf := p.daNuvem(1, fioDoPar("sf", "ECF", false, "", false))
		ficam(t, p, sf, storage.EspelhoAplicado)
	})
	t.Run("o já vale aqui", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		p.aMao(func(s *dictionary.Semente) { s.Entradas[0].Alias = []string{"sf"} }) // "sf" -> "ECD", à mão
		if d := p.receber(doPull(1, fioDoPar("sf", "ECD", false, "", false))); d.Tipo != TipoJaValeAqui {
			t.Fatalf("o pull: %+v", d)
		}
		ficam(t, p, idSF, storage.EspelhoJaValeAqui)
	})
	t.Run("o não aplicado pelo domínio ausente", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		if d := p.receber(doPull(1, fioDoPar("sf", "ecd", true, "fiscal", false))); d.Tipo != TipoNaoAplicado {
			t.Fatalf("o pull: %+v", d)
		}
		ficam(t, p, idSF, storage.EspelhoNaoAplicado)
		p.aMao(func(s *dictionary.Semente) {
			s.Dominios["fiscal"] = []string{"Fiscal.exe"}
			s.Entradas[0].Dominio = "fiscal"
		})
		if r := p.varrer(); r.Reaplicados[TipoAplicado] != 1 {
			t.Fatalf("o pendente não foi aplicado: %+v", r)
		}
		if e := p.estado(idSF); e.Situacao != storage.EspelhoAplicado || projecaoNo(p.arquivo(), idSF) == nil {
			t.Errorf("o espelho: %+v", e)
		}
	})
	t.Run("o não aplicado pelo arquivo indisponível", func(t *testing.T) {
		p := novoPCQueVarre(t)
		p.aceitar()
		antes := p.bytesDoArquivo()
		if err := os.Remove(p.arq.Caminho); err != nil {
			t.Fatal(err)
		}
		if d := p.receber(doPull(1, fioDoPar("sf", "ECF", false, "", false))); d.Tipo != TipoPendente {
			t.Fatalf("o pull: %+v", d)
		}
		alemDoPrazo(p, idSF)
		if err := os.WriteFile(p.arq.Caminho, antes, 0o600); err != nil {
			t.Fatal(err)
		}
		if r := p.varrer(); soma(r.Tipos) != 0 || r.Reaplicados[TipoAplicado] != 1 {
			t.Fatalf("o arquivo voltou: %+v", r)
		}
		if e := p.estado(idSF); e.Situacao != storage.EspelhoAplicado || projecaoNo(p.arquivo(), idSF) == nil {
			t.Errorf("o espelho: %+v", e)
		}
	})
}

// ─── O epoch e o pareamento ───

// O pendente só vai ao arquivo depois da leitura curta do epoch e do
// pareamento ("A ordem", passo 1): mudados, o arquivo não é tocado, a
// varredura devolve o erro, e a rodada para (o pareamento) ou segue para o
// recomeço (o epoch), com o M contado.
func TestVarredura_OEpochOuOPareamentoMudadosNaoTocamOArquivo(t *testing.T) {
	ctx := context.Background()
	for nome, c := range map[string]struct {
		mudar string
		erro  error
	}{
		"o epoch":      {`UPDATE sync_state SET sync_epoch = 'e2'`, storage.ErrEpochDiferente},
		"o pareamento": {`DELETE FROM sync_state`, storage.ErrPareamentoMudou},
	} {
		t.Run(nome, func(t *testing.T) {
			p := novoPCQueVarre(t)
			if d := p.receber(doPull(1, fioDoPar("sf", "ECD", true, "fiscal", false))); d.Tipo != TipoNaoAplicado {
				t.Fatalf("o pull: %+v", d)
			}
			p.aprender("ECF", "lalu")
			p.aMao(func(s *dictionary.Semente) {
				s.Dominios["fiscal"] = []string{"Fiscal.exe"}
				s.Entradas[0].Dominio = "fiscal"
			})
			if _, err := p.db.Exec(c.mudar); err != nil {
				t.Fatal(err)
			}
			antes := p.bytesDoArquivo()
			r, err := p.motor.Varrer(ctx, "e1")
			if !errors.Is(err, c.erro) {
				t.Fatalf("a varredura: %v, want %v", err, c.erro)
			}
			if r.Trocas != 1 {
				t.Errorf("o M: %+v", r)
			}
			if !bytes.Equal(antes, p.bytesDoArquivo()) {
				t.Error("o arquivo foi tocado com o " + nome + " mudado")
			}
			if e := p.estado(idSF); e.Situacao != storage.EspelhoNaoAplicado || !TemPendente(e) {
				t.Errorf("o pendente: %+v", e)
			}
		})
	}
}
