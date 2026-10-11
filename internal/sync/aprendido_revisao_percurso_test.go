package syncengine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os percursos da revisão da F8f (07-out-2026), rodada a rodada pelo Motor,
// com o mesmo dublê do PC da varredura (varredura_percurso_test.go: o
// dicionario.json de verdade num t.TempDir(), o historico.db de verdade) e
// uma nuvem de roteiro com memória (nuvemQueLembra), que responde o push pela
// tabela da nuvem e manda o pull do que mudou. Nenhum teste toca o dicionário
// real, o teclado ou a área de transferência.

// nuvemQueLembra é a nuvem do aprendido com memória: um par por id, com a
// versão e o seq da última mudança, a tabela de cloudstore.DecideLearned (na
// ordem da ADR, "As regras da nuvem") e o duplicate pelo mutation_id. O pull
// manda, em ordem de seq, o estado atual de cada id que mudou depois do
// cursor. perderResposta faz o próximo push ser aplicado e a resposta se
// perder (o timeout). Só o aprendido passa por ela.
type nuvemQueLembra struct {
	t              *testing.T
	pares          map[string]*parQueANuvemTem
	vistas         map[string]MutationResult
	operacoes      map[string]string
	seq            int64
	enviadas       []Mutation
	perderResposta bool
}

type parQueANuvemTem struct {
	versao, seq int64
	par         Aprendido
}

func novaNuvemQueLembra(t *testing.T) *nuvemQueLembra {
	return &nuvemQueLembra{t: t, pares: map[string]*parQueANuvemTem{}, vistas: map[string]MutationResult{},
		operacoes: map[string]string{}}
}

func (n *nuvemQueLembra) Push(_ context.Context, muts []Mutation) (PushResult, error) {
	r := PushResult{Epoch: "e1"}
	for _, m := range muts {
		n.enviadas = append(n.enviadas, m)
		r.Results = append(r.Results, n.decidir(m))
	}
	if n.perderResposta {
		n.perderResposta = false
		return PushResult{}, transitorio(CodigoRede)
	}
	return r, nil
}

// decidir é a tabela da nuvem para uma mutação do aprendido.
func (n *nuvemQueLembra) decidir(m Mutation) MutationResult {
	if v, ok := n.vistas[m.MutationID]; ok {
		if n.operacoes[m.MutationID] != m.Operation {
			return MutationResult{MutationID: m.MutationID, Status: StatusRejected, Code: CodigoMutacaoReusada}
		}
		return MutationResult{MutationID: m.MutationID, Status: StatusDuplicate, Original: v.Status,
			EntityVersion: v.EntityVersion, Code: v.Code}
	}
	res := MutationResult{MutationID: m.MutationID}
	cur := n.pares[m.EntityID]
	gravar := func(p Aprendido) {
		n.seq++
		v := int64(1)
		if cur != nil {
			v = cur.versao + 1
		}
		n.pares[m.EntityID] = &parQueANuvemTem{versao: v, seq: n.seq, par: p}
		res.Status, res.EntityVersion = StatusApplied, v
	}
	if m.Operation == OpDelete {
		switch {
		case cur == nil:
			res.Status, res.Code = StatusRejected, CodigoNadaADesfazer
		case cur.par.UndoneAt == nil:
			p := cur.par
			p.UndoneAt = &desfeitoEm
			gravar(p)
		default:
			res.Status, res.EntityVersion = StatusApplied, cur.versao
		}
	} else {
		var corpo payloadAprendido
		if err := json.Unmarshal(m.Payload, &corpo); err != nil {
			n.t.Fatalf("o payload do upsert: %v", err)
		}
		p := Aprendido{ID: m.EntityID, Variant: corpo.Variant, Term: corpo.Term, Restricted: corpo.Restricted,
			Domain: corpo.Domain}
		switch {
		case cur == nil && m.BaseVersion == 0:
			gravar(p)
		case cur == nil || m.BaseVersion > cur.versao:
			res.Status, res.Code = StatusRejected, CodigoBaseInvalida
		case cur.par.UndoneAt == nil && mesmoConteudoNaNuvem(cur.par, p):
			res.Status, res.EntityVersion = StatusApplied, cur.versao
		case m.BaseVersion == cur.versao:
			gravar(p)
		default:
			st := cur.par
			res.Status, res.EntityVersion, res.StateAprendido = StatusConflict, cur.versao, &st
		}
	}
	if res.Status != StatusRejected || res.Code == CodigoNadaADesfazer {
		n.vistas[m.MutationID], n.operacoes[m.MutationID] = res, m.Operation
	}
	return res
}

// mesmoConteudoNaNuvem: a nuvem compara o termo como veio, a restrição e o
// domínio.
func mesmoConteudoNaNuvem(a, b Aprendido) bool {
	if (a.Domain == nil) != (b.Domain == nil) || a.Domain != nil && *a.Domain != *b.Domain {
		return false
	}
	return a.Term == b.Term && a.Restricted == b.Restricted
}

func (n *nuvemQueLembra) Pull(_ context.Context, after int64, _ int) (ChangePage, error) {
	p := ChangePage{LastSeq: after, Epoch: "e1"}
	var ids []string
	for id, par := range n.pares {
		if par.seq > after {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return n.pares[ids[i]].seq < n.pares[ids[j]].seq })
	for _, id := range ids {
		par := n.pares[id]
		estado := par.par
		p.Changes = append(p.Changes, Change{Seq: par.seq, EntityType: storage.EntidadeAprendido, EntityID: id,
			Operation: OpUpsert, EntityVersion: par.versao, PayloadAprendido: &estado})
		p.LastSeq = par.seq
	}
	return p, nil
}

// porOutroAparelho é a mudança que outro aparelho (o celular, outro PC) fez
// na nuvem: o par passa a ser p, com a versão seguinte.
func (n *nuvemQueLembra) porOutroAparelho(p *Aprendido) {
	n.seq++
	v := int64(1)
	if cur := n.pares[p.ID]; cur != nil {
		v = cur.versao + 1
	}
	n.pares[p.ID] = &parQueANuvemTem{versao: v, seq: n.seq, par: *p}
}

func (n *nuvemQueLembra) doId(id string) []Mutation {
	var out []Mutation
	for _, m := range n.enviadas {
		if m.EntityID == id {
			out = append(out, m)
		}
	}
	return out
}

// pcComNuvem é o PC da varredura ligado à nuvemQueLembra.
func pcComNuvem(t *testing.T) (*pcQueVarre, *nuvemQueLembra) {
	t.Helper()
	p := novoPCQueVarre(t)
	n := novaNuvemQueLembra(t)
	p.motor.Cliente = n
	return p, n
}

// rodadas roda o Motor k vezes, cada uma até o fim (sem erro).
func (p *pcQueVarre) rodadas(k int) {
	p.t.Helper()
	for i := 0; i < k; i++ {
		if _, err := p.motor.Rodada(context.Background()); err != nil {
			p.t.Fatalf("a rodada %d: %v", i, err)
		}
	}
}

// ─── A reconexão no meio de um envio ───

// "Um desfazer sem resposta seguido de um reaprender termina reaprendido na
// nuvem, sem etiqueta nenhuma" (ADR-019, "As regras da nuvem"), também com
// uma reconexão (Desconectar e Conectar, a mesma conta) no meio: o primeiro
// envio do aparelho novo repete o desfazer (com outro mutation_id), e só
// depois o reaprender sobe, com a base que a resposta trouxe. Sem isso, o
// reaprender subia com a base velha, conflitava com o próprio desfazer deste
// PC, e saía do arquivo com a etiqueta falsa "desfeito em outro aparelho".
func TestReconexao_ODesfazerSemRespostaSeguidoDoReaprender(t *testing.T) {
	ctx := context.Background()
	p, n := pcComNuvem(t)
	p.aceitar()
	sf := p.aprender("ECF", "sf")
	p.rodadas(1)
	if c := n.pares[sf]; c == nil || c.versao != 1 {
		t.Fatalf("o par não subiu: %+v", c)
	}

	// O desfazer: a nuvem o aplica (versão 2), e a resposta se perde.
	p.desfazer("sf")
	n.perderResposta = true
	if _, err := p.motor.Rodada(ctx); err == nil {
		t.Fatal("o push sem resposta")
	}
	if c := n.pares[sf]; c.versao != 2 || c.par.UndoneAt == nil {
		t.Fatalf("a nuvem não aplicou o desfazer: %+v", c)
	}
	if e := p.estado(sf); e.Marcador == nil || !e.Marcador.EmVoo() {
		t.Fatalf("o desfazer não ficou em voo: %+v", e)
	}

	// A reconexão (outro pareamento desta instalação), e o reaprender.
	p.motor.PareadoEm = "p2"
	p.aprender("ECF", "sf")
	p.rodadas(3)

	if c := n.pares[sf]; c.par.UndoneAt != nil || c.par.Term != "ECF" {
		t.Errorf("o reaprender não chegou à nuvem: %+v", c)
	}
	e := p.estado(sf)
	if projecaoNo(p.arquivo(), sf) == nil || e.Situacao != storage.EspelhoNenhuma || e.Marcador != nil ||
		e.MudouAqui() || e.ServerVersion == nil || *e.ServerVersion != n.pares[sf].versao {
		t.Errorf("no fim: no arquivo %+v, o espelho %+v", projecaoNo(p.arquivo(), sf), e)
	}
	ms := n.doId(sf)
	if len(ms) < 4 || ms[2].Operation != OpDelete || ms[2].MutationID == ms[1].MutationID || ms[3].Operation != OpUpsert ||
		ms[3].BaseVersion != 2 {
		t.Errorf("os envios: %+v", ms)
	}
}

// ─── O esquecer e a volta ───

// esquecerEConectar é o "Esquecer esta nuvem" e o pareamento seguinte, com a
// conta dada (o aparelho e o pareamento novos).
func (p *pcQueVarre) esquecerEConectar(conta string) {
	p.t.Helper()
	if err := p.fila.EsquecerNuvem(context.Background(), p.agora); err != nil {
		p.t.Fatal(err)
	}
	p.exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'a2'), ('nuvem_conectado_em', 'p2'),
		('nuvem_conta', '` + conta + `') ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`)
	p.motor.Aparelho, p.motor.PareadoEm = "a2", "p2"
}

// O Desfazer que a varredura não viu antes do esquecer (o PC desconectado, ou
// o par com um envio em voo) não se perde de volta à mesma conta: a projeção
// guardada ainda era a do par, e o esquecer guarda a conta também nessa linha
// (a mudança daqui que a nuvem tinha). Na volta, a varredura vê o par sair e
// não o assenta: espera o pull trazer a versão, e o desfazer sobe. Sem isso,
// a varredura assentava (sem versão da nuvem, depois do esquecer), e o pull
// do cursor 0 punha o par de volta no arquivo.
func TestEsquecer_ODesfazerQueAVarreduraNaoViuVoltaAMesmaConta(t *testing.T) {
	for nome, desfazer := range map[string]func(p *pcQueVarre, n *nuvemQueLembra){
		"desconectado": func(p *pcQueVarre, _ *nuvemQueLembra) { p.desfazer("sf") },
		"com o envio em voo": func(p *pcQueVarre, n *nuvemQueLembra) {
			// A troca de termo sobe (a nuvem a aplica, e a resposta se
			// perde); o Desfazer vem com ela em voo.
			p.desfazer("sf")
			p.aprender("ECD", "sf")
			n.perderResposta = true
			if _, err := p.motor.Rodada(context.Background()); err == nil {
				t.Fatal("o push sem resposta")
			}
			p.desfazer("sf")
		},
	} {
		t.Run(nome, func(t *testing.T) {
			p, n := pcComNuvem(t)
			p.exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_conta', 'c1')`)
			p.aceitar()
			sf := p.aprender("ECF", "sf")
			p.rodadas(1)
			desfazer(p, n)
			if e := p.estado(sf); !e.Presente() || !e.MudouAqui() {
				t.Fatalf("a varredura viu o Desfazer: %+v", e)
			}

			p.esquecerEConectar("c1")
			p.rodadas(3)
			if got := projecaoNo(p.arquivo(), sf); got != nil {
				t.Fatalf("o par desfeito aqui voltou ao arquivo: %+v", got)
			}
			if c := n.pares[sf]; c.par.UndoneAt == nil {
				t.Errorf("o desfazer não subiu: %+v", c)
			}
			if e := p.estado(sf); e.MudouAqui() || e.Marcador != nil || !e.BaseDesfeita {
				t.Errorf("o espelho no fim: %+v", e)
			}
		})
	}
}

// O par aprendido aqui e confirmado pela conta esquecida, numa conta nova que
// tem a mesma variante (o id é o da variante, o mesmo em toda conta), desfeita
// ou com outro termo: antes do clique, o pull da conta nova não o tira nem o
// troca (ele é mudança daqui para ela, e o M o conta); depois do clique, ele
// sobe com a base 0, e a nuvem decide (D4: vale o primeiro, e este PC, que
// perdeu, segue a nuvem com a etiqueta). Sem isso, a varredura o contava no M
// e o segurava para o clique, mas o pull o tirava (ou trocava) em silêncio.
func TestEsquecer_OAprendidoAquiNaContaNovaEsperaOClique(t *testing.T) {
	ctx := context.Background()
	for nome, c := range map[string]struct {
		naContaNova *Aprendido
		etiqueta    storage.SituacaoDoEspelho
		termo       string
	}{
		"desfeito lá":     {fioDoPar("sf", "ECF", false, "", true), storage.EspelhoDesfeitoEmOutro, ""},
		"com outro termo": {fioDoPar("sf", "ECD", false, "", false), storage.EspelhoTrocadoEmOutro, "ECD"},
	} {
		t.Run(nome, func(t *testing.T) {
			p, _ := pcComNuvem(t)
			p.exec(`INSERT INTO instalacao (nome, valor) VALUES ('nuvem_conta', 'c1')`)
			p.aceitar()
			sf := p.aprender("ECF", "sf")
			p.rodadas(1)

			p.esquecerEConectar("c2")
			c2 := novaNuvemQueLembra(t)
			c2.porOutroAparelho(fioDoPar("sf", "ECF", false, "", false))
			c2.porOutroAparelho(c.naContaNova)
			p.motor.Cliente = c2
			rel, err := p.motor.Rodada(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got := projecaoNo(p.arquivo(), sf); got == nil || got.Termo != "ECF" {
				t.Fatalf("antes do clique, o pull da conta nova mexeu no par aprendido aqui: %+v", got)
			}
			if rel.Trocas != 1 || len(c2.enviadas) != 0 {
				t.Fatalf("antes do clique: M = %d, %+v", rel.Trocas, c2.enviadas)
			}

			if _, err := p.fila.EnviarHistorico(ctx, filepath.Join(t.TempDir(), "historico.db"), p.agora); err != nil {
				t.Fatal(err)
			}
			p.rodadas(3)
			if ms := c2.doId(sf); len(ms) != 1 || ms[0].Operation != OpUpsert || ms[0].BaseVersion != 0 {
				t.Fatalf("o aprendido aqui na conta nova: %+v", ms)
			}
			got := projecaoNo(p.arquivo(), sf)
			if c.termo == "" && got != nil || c.termo != "" && (got == nil || got.Termo != c.termo) {
				t.Errorf("a nuvem decide (D4): no arquivo %+v", got)
			}
			if e := p.estado(sf); e.Situacao != c.etiqueta || e.MudouAqui() || e.Marcador != nil {
				t.Errorf("a etiqueta do PC que perdeu: %+v", e)
			}
		})
	}
}

// ─── A confirmação da linha "aplicando" ───

// gravaEFalha é o aplicador de verdade que, uma vez, devolve um erro depois
// de gravar o arquivo: a troca do dicionário em memória que falha (o Agent,
// na F8g, troca o dicionário e o prompt depois de gravar).
type gravaEFalha struct {
	DicionarioDoAprendido
	erro error
}

func (g *gravaEFalha) AplicarPares(escolher func(dictionary.Semente) []dictionary.ParDaNuvem,
	antesDeGravar func(dictionary.Lote) error) (dictionary.Semente, dictionary.Lote, error) {
	s, l, err := g.DicionarioDoAprendido.AplicarPares(escolher, antesDeGravar)
	if err == nil && l.Mudou && g.erro != nil {
		e := g.erro
		g.erro = nil
		return s, l, e
	}
	return s, l, err
}

// A linha "aplicando" cujo arquivo foi gravado, e cuja página (ou transação
// dos pendentes) não terminou, quando o cursor já andou: a intenção veio da
// varredura tentando de novo um pendente (o pull o guardou com o arquivo
// indisponível), ou do Receber cujo lote gravou e devolveu erro. A varredura
// confirma pelo arquivo, e o par da nuvem guardado (desta conta e deste
// epoch: o esquecer e o recomeço apagam a versão dele) fica pendente até a
// base andar. Sem isso, a confirmação o tirava sem gravar a base, e o pull
// nunca mais o traria: o reaprender daqui subia com a base velha, conflitava
// com o desfazer que este PC já tinha aplicado, e saía do arquivo com a
// etiqueta falsa "desfeito em outro aparelho".
func TestAplicando_AConfirmacaoAndaABaseQuandoOCursorJaAndou(t *testing.T) {
	ctx := context.Background()
	for nome, cair := range map[string]func(p *pcQueVarre){
		"a queda na varredura": func(p *pcQueVarre) {
			arquivoIndisponivelNoPull(p)
			cctx, cancelar := context.WithCancel(ctx)
			p.motor.Dicionario = &aplicadorDeTeste{DicionarioDoAprendido: p.arq, depois: cancelar}
			if _, err := p.motor.Varrer(cctx, "e1"); !errors.Is(err, context.Canceled) {
				p.t.Fatalf("a queda: %v", err)
			}
		},
		"a troca em memória que falha na varredura": func(p *pcQueVarre) {
			arquivoIndisponivelNoPull(p)
			p.motor.Dicionario = &gravaEFalha{DicionarioDoAprendido: p.arq, erro: errors.New("a troca")}
			if r, err := p.motor.Varrer(ctx, "e1"); err != nil || !r.LoteFalhou {
				p.t.Fatalf("o lote que falhou: %+v, %v", r, err)
			}
		},
		"a troca em memória que falha no Receber": func(p *pcQueVarre) {
			p.motor.Dicionario = &gravaEFalha{DicionarioDoAprendido: p.arq, erro: errors.New("a troca")}
			p.rodadas(1)
		},
	} {
		t.Run(nome, func(t *testing.T) {
			p, n := pcComNuvem(t)
			p.aceitar()
			sf := p.aprender("ECF", "sf")
			p.rodadas(1)
			n.porOutroAparelho(fioDoPar("sf", "ECF", false, "", true)) // o celular desfaz: versão 2

			cair(p)
			if e := p.estado(sf); e.Situacao != storage.EspelhoAplicando || projecaoNo(p.arquivo(), sf) != nil {
				t.Fatalf("depois da queda: %+v", e)
			}
			if c, _ := p.cursor(t); c != n.pares[sf].seq {
				t.Fatalf("o cursor não andou: %d", c)
			}

			p.motor.Dicionario = p.arq
			p.rodadas(2)
			e := p.estado(sf)
			if e.ServerVersion == nil || *e.ServerVersion != 2 || !e.BaseDesfeita || e.Nuvem != nil ||
				e.Situacao != storage.EspelhoNenhuma || e.MudouAqui() {
				t.Fatalf("a base não andou: %+v", e)
			}

			// Dias depois, a pessoa aprende o par de novo aqui.
			p.aprender("ECF", "sf")
			p.rodadas(2)
			if c := n.pares[sf]; c.par.UndoneAt != nil || c.versao != 3 {
				t.Errorf("o reaprender não chegou à nuvem: %+v", c)
			}
			if e := p.estado(sf); projecaoNo(p.arquivo(), sf) == nil || e.Situacao != storage.EspelhoNenhuma {
				t.Errorf("o reaprender perdeu para o próprio desfazer: %+v", e)
			}
		})
	}
}

// arquivoIndisponivelNoPull é o pull do desfazer com o dicionario.json
// ausente (o par fica pendente, e o cursor anda), e a volta do arquivo.
func arquivoIndisponivelNoPull(p *pcQueVarre) {
	p.t.Helper()
	antes := p.bytesDoArquivo()
	if err := os.Remove(p.arq.Caminho); err != nil {
		p.t.Fatal(err)
	}
	rel, err := p.motor.Rodada(context.Background())
	if err != nil || rel.PullDoAprendido[TipoPendente] != 1 {
		p.t.Fatalf("o pull sem o arquivo: %+v, %v", rel, err)
	}
	if err := os.WriteFile(p.arq.Caminho, antes, 0o600); err != nil {
		p.t.Fatal(err)
	}
}

// ─── O pull mais novo na linha "aplicando" ───

// A linha "aplicando" que sobrou de uma página (a gravação falhou depois da
// intenção, ou a rodada caiu depois de gravar o arquivo) recebe, numa página
// seguinte, um par mais novo do mesmo id que não vai ao lote (o arquivo
// indisponível): o par mais novo fica guardado no lugar do da intenção, e é
// ele que vale quando o arquivo volta (a varredura devolve a intenção, ou a
// confirma, e o aplica). Sem isso, o cursor andava sem guardar nada, e a
// varredura aplicava o par velho da intenção (ou o perdia na confirmação): o
// PC ficava num estado velho da nuvem (aqui, um desfazer que outro PC já
// tinha revertido), e nunca se corrigia.
func TestAplicando_OParMaisNovoDoPullNaoSePerde(t *testing.T) {
	for nome, c := range map[string]struct {
		cair    func(p *pcQueVarre)
		gravado bool
	}{
		"a gravação que falha depois da intenção": {cair: func(p *pcQueVarre) {
			p.motor.Dicionario = &aplicadorDeTeste{DicionarioDoAprendido: p.arq, falharDepoisDaIntencao: errors.New("preso")}
			p.rodadas(1)
		}},
		"a queda depois da gravação": {gravado: true, cair: func(p *pcQueVarre) {
			ctx, cancelar := context.WithCancel(context.Background())
			p.motor.Dicionario = &aplicadorDeTeste{DicionarioDoAprendido: p.arq, depois: cancelar}
			if _, err := p.motor.Rodada(ctx); !errors.Is(err, context.Canceled) {
				p.t.Fatalf("a queda: %v", err)
			}
		}},
	} {
		t.Run(nome, func(t *testing.T) {
			p, n := pcComNuvem(t)
			p.aceitar()
			sf := p.aprender("ECF", "sf")
			p.rodadas(1)
			enviadas := len(n.enviadas)

			// O celular desfaz (versão 2), e a página do pull não termina.
			n.porOutroAparelho(fioDoPar("sf", "ECF", false, "", true))
			c.cair(p)
			p.motor.Dicionario = p.arq
			if e := p.estado(sf); e.Situacao != storage.EspelhoAplicando || (projecaoNo(p.arquivo(), sf) == nil) != c.gravado {
				t.Fatalf("a intenção que sobrou: %+v", e)
			}

			// Outro PC aprende de novo (versão 3), com o arquivo indisponível
			// aqui.
			antes := p.bytesDoArquivo()
			if err := os.Remove(p.arq.Caminho); err != nil {
				t.Fatal(err)
			}
			n.porOutroAparelho(fioDoPar("sf", "ECF", false, "", false))
			p.rodadas(1)
			if cur, _ := p.cursor(t); cur != n.pares[sf].seq {
				t.Fatalf("o cursor = %d", cur)
			}
			if e := p.estado(sf); e.Situacao != storage.EspelhoAplicando || e.Nuvem == nil || *e.Nuvem.Versao != 3 {
				t.Fatalf("o par mais novo não ficou guardado: %+v", e)
			}

			// O arquivo volta: vale a versão 3 (o par está aqui), e nada sobe.
			if err := os.WriteFile(p.arq.Caminho, antes, 0o600); err != nil {
				t.Fatal(err)
			}
			p.rodadas(3)
			e := p.estado(sf)
			if projecaoNo(p.arquivo(), sf) == nil || e.ServerVersion == nil || *e.ServerVersion != 3 || e.BaseDesfeita ||
				e.Nuvem != nil || e.Situacao == storage.EspelhoAplicando {
				t.Errorf("o PC ficou num estado velho da nuvem: no arquivo %+v, o espelho %+v", projecaoNo(p.arquivo(), sf), e)
			}
			if len(n.enviadas) != enviadas {
				t.Errorf("subiu: %+v", n.enviadas[enviadas:])
			}
		})
	}
}
