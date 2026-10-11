package syncengine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os percursos do Receber com o aplicador (F8f4), com o mesmo dublê do PC da
// varredura (varredura_percurso_test.go): o dicionario.json de verdade
// (dictionary.Arquivo) numa pasta temporária, o historico.db de verdade em
// outra, e uma nuvem de roteiro que manda o pull. As quedas e o que acontece
// "no meio" são dublês: um gancho na nuvem (entre o pull e o arquivo) e um no
// aplicador (entre o arquivo e a página). Nenhum teste toca o dicionário
// real, o teclado ou a área de transferência.

// nuvemQueManda é a nuvem de roteiro do Receber: o push como a
// nuvemDoAprendido (applied, com a versão seguinte do id, ou o responder), e
// o pull das mudancas depois do cursor, em ordem de seq (porPagina > 0 corta
// em páginas).
type nuvemQueManda struct {
	*nuvemDoAprendido
	epoch     string
	mudancas  []Change
	porPagina int
	// antes roda uma vez, quando a nuvem já respondeu o pull e a página
	// ainda não chegou ao PC: é o "entre o pull e o arquivo".
	antes func()
}

func (n *nuvemQueManda) Pull(_ context.Context, after int64, _ int) (ChangePage, error) {
	p := ChangePage{LastSeq: after, Epoch: n.epoch}
	for _, c := range n.mudancas {
		if c.Seq <= after {
			continue
		}
		if n.porPagina > 0 && len(p.Changes) == n.porPagina {
			p.HasMore = true
			break
		}
		p.Changes = append(p.Changes, c)
		p.LastSeq = c.Seq
	}
	if f := n.antes; f != nil {
		n.antes = nil
		f()
	}
	return p, nil
}

// aplicadorDeTeste é o aplicador de verdade (o Arquivo) com os dublês das
// quedas: depois roda uma vez, no primeiro lote com pares (o da varredura
// ociosa não conta), quando ele já gravou o arquivo e a página ainda não foi
// aplicada ("entre o arquivo e o espelho"); falharDepoisDaIntencao faz a
// gravação do arquivo falhar depois de a intenção ser gravada.
type aplicadorDeTeste struct {
	DicionarioDoAprendido
	depois                 func()
	falharDepoisDaIntencao error
}

func (a *aplicadorDeTeste) AplicarPares(escolher func(dictionary.Semente) []dictionary.ParDaNuvem,
	antesDeGravar func(dictionary.Lote) error) (dictionary.Semente, dictionary.Lote, error) {
	antes := antesDeGravar
	if a.falharDepoisDaIntencao != nil {
		antes = func(l dictionary.Lote) error {
			if err := antesDeGravar(l); err != nil {
				return err
			}
			return a.falharDepoisDaIntencao
		}
	}
	s, l, err := a.DicionarioDoAprendido.AplicarPares(escolher, antes)
	if f := a.depois; f != nil && len(l.Resultados) > 0 {
		a.depois = nil
		f()
	}
	return s, l, err
}

// novoPCQueRecebe é o PC da varredura com a nuvem que manda o pull.
func novoPCQueRecebe(t *testing.T) (*pcQueVarre, *nuvemQueManda) {
	t.Helper()
	p := novoPCQueVarre(t)
	n := &nuvemQueManda{nuvemDoAprendido: p.nuvem, epoch: "e1"}
	p.motor.Cliente = n
	return p, n
}

// puxar é o Receber de uma rodada.
func (p *pcQueVarre) puxar(ctx context.Context) (Relatorio, error) {
	rel := novoRelatorio()
	err := p.motor.Receber(ctx, &rel)
	return rel, err
}

func (p *pcQueVarre) exec(sql string) {
	p.t.Helper()
	if _, err := p.db.Exec(sql); err != nil {
		p.t.Fatal(err)
	}
}

// naSeq é a mudança do pull com o seq dado.
func naSeq(seq int64, c Change) Change {
	c.Seq = seq
	return c
}

// ditadoDoPull é um ditado de outro aparelho no pull.
func ditadoDoPull(seq int64, id, texto string) Change {
	return Change{Seq: seq, EntityType: storage.EntidadeDitado, EntityID: id, Operation: OpUpsert, EntityVersion: 1,
		Payload: &Item{ID: id, FinalText: texto, RawText: "b", Mode: "smart", ASRProvider: "x", Status: "done",
			SourcePlatform: "windows", CreatedAt: agoraDaVarredura, Version: 1}}
}

var idLALU = dictionary.IDDoPar("lalu")

// ─── Sem o aplicador ───

// nil mantém a F7: a mudança learned é "não aplicada", o cursor anda, o
// ditado da mesma página entra, e nem o arquivo nem o espelho são tocados.
func TestReceber_SemOAplicadorEAF7(t *testing.T) {
	ctx := context.Background()
	p, n := novoPCQueRecebe(t)
	p.motor.Dicionario = nil
	n.mudancas = []Change{naSeq(1, doPull(1, fioDoPar("sf", "ECF", false, "", false))), ditadoDoPull(2, idM1, "de outro PC")}
	antes := p.bytesDoArquivo()
	rel, err := p.puxar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Pull[TipoNaoAplicado] != 1 || rel.Pull[TipoRecebido] != 1 || soma(rel.PullDoAprendido) != 0 {
		t.Errorf("o relatório: %+v", rel)
	}
	if c, _ := p.cursor(t); c != 2 {
		t.Errorf("o cursor = %d, want 2", c)
	}
	if !bytes.Equal(antes, p.bytesDoArquivo()) || len(p.espelho()) != 0 {
		t.Error("sem o aplicador, o aprendido foi tocado")
	}
}

// ─── O par passa a valer aqui ───

// O par da nuvem entra no arquivo com a adaptação deste PC (a grafia do termo
// daqui, o alias_restrito), na mesma página do ditado, e o cursor anda. A
// adaptação nunca volta como upsert, nem depois do aceite; o pull da mesma
// versão (o cursor de volta a 0) é eco, e não toca o arquivo.
func TestReceber_OParDaNuvemPassaAValerAqui(t *testing.T) {
	ctx := context.Background()
	p, n := novoPCQueRecebe(t)
	p.aceitar()
	n.mudancas = []Change{
		naSeq(1, doPull(1, fioDoPar("sf", "ecf", false, "", false))),
		ditadoDoPull(2, idM1, "de outro PC"),
		naSeq(3, doPull(4, fioDoPar("lalu", "LALUR", true, "contabil", false))),
	}
	rel, err := p.puxar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.PullDoAprendido[TipoAplicado] != 2 || rel.Pull[TipoRecebido] != 1 || rel.Pull[TipoNaoAplicado] != 0 {
		t.Errorf("o relatório: %+v", rel)
	}
	if c, e := p.cursor(t); c != 3 || e != "e1" {
		t.Errorf("o cursor = %d, %q", c, e)
	}
	if l, err := p.repo.Get(ctx, donoDoMotor, idM1); err != nil || l.FinalText != "de outro PC" {
		t.Errorf("o ditado da página: %+v, %v", l, err)
	}
	quero := map[string]*dictionary.ParDoPainel{
		idSF:   {ID: idSF, Variante: "sf", Termo: "ECF"},
		idLALU: {ID: idLALU, Variante: "lalu", Termo: "LALUR", Restrito: true, Dominio: "contabil", EmAliasRestrito: true},
	}
	versao := map[string]int64{idSF: 1, idLALU: 4}
	for id, q := range quero {
		if got := projecaoNo(p.arquivo(), id); !reflect.DeepEqual(got, q) {
			t.Errorf("no arquivo: %+v, want %+v", got, q)
		}
		e := p.estado(id)
		if e.Situacao != storage.EspelhoAplicado || !reflect.DeepEqual(e.Projecao, q) || e.ServerVersion == nil ||
			*e.ServerVersion != versao[id] || !e.VeioDaNuvem || e.Nuvem != nil || e.Esperada != nil || e.Marcador != nil ||
			e.MudouAqui() {
			t.Errorf("o espelho de %s: %+v", id, e)
		}
	}

	// A adaptação não volta, nem depois do aceite.
	if _, err := p.motor.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.nuvem.enviadas) != 0 || len(p.marcados()) != 0 {
		t.Errorf("o que veio da nuvem subiu: %+v", p.nuvem.enviadas)
	}

	// O pull da mesma versão é eco: o arquivo não é tocado.
	if err := p.fila.ZerarCursor(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	antes, espelho := p.bytesDoArquivo(), p.espelho()
	rel, err = p.puxar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.PullDoAprendido[TipoEco] != 2 || !bytes.Equal(antes, p.bytesDoArquivo()) || !reflect.DeepEqual(espelho, p.espelho()) {
		t.Errorf("o eco: %+v", rel)
	}
}

// O primeiro pull de um pareamento (o epoch ainda nulo, sem push antes) adota
// o epoch da página, como a transação dela adotaria, e aplica o par.
func TestReceber_OPrimeiroPullAdotaOEpoch(t *testing.T) {
	p, n := novoPCQueRecebe(t)
	p.exec(`UPDATE sync_state SET sync_epoch = NULL`)
	n.mudancas = []Change{naSeq(1, doPull(1, fioDoPar("sf", "ECF", false, "", false)))}
	rel, err := p.puxar(context.Background())
	if err != nil || rel.PullDoAprendido[TipoAplicado] != 1 {
		t.Fatalf("o pull: %+v, %v", rel, err)
	}
	if c, e := p.cursor(t); c != 1 || e != "e1" {
		t.Errorf("o cursor = %d, %q", c, e)
	}
	if projecaoNo(p.arquivo(), idSF) == nil {
		t.Error("o par não entrou")
	}
}

// Uma página com o mesmo id duas vezes vale pela última mudança.
func TestReceber_OMesmoIDNaPaginaValePelaUltima(t *testing.T) {
	p, n := novoPCQueRecebe(t)
	n.mudancas = []Change{
		naSeq(1, doPull(1, fioDoPar("sf", "ECF", false, "", false))),
		naSeq(2, doPull(2, fioDoPar("sf", "ECD", false, "", false))),
	}
	rel, err := p.puxar(context.Background())
	if err != nil || rel.PullDoAprendido[TipoAplicado] != 1 {
		t.Fatalf("o pull: %+v, %v", rel, err)
	}
	if got := projecaoNo(p.arquivo(), idSF); got == nil || got.Termo != "ECD" || *p.estado(idSF).ServerVersion != 2 {
		t.Errorf("o par: %+v, %+v", got, p.estado(idSF))
	}
}

// ─── O epoch e o pareamento ───

// "A ordem", passo 1: o epoch ou o pareamento mudados entre o pull e o
// arquivo não tocam o arquivo, e nada da página é aplicado (nem o ditado, nem
// o espelho, nem o cursor). A rodada recomeça no epoch novo e aplica o par;
// no pareamento mudado, para.
func TestReceber_OEpochOuOPareamentoMudadosNaoTocamOArquivo(t *testing.T) {
	ctx := context.Background()
	for nome, c := range map[string]struct {
		mudar string
		erro  error
	}{
		"o epoch":      {`UPDATE sync_state SET sync_epoch = 'e2'`, storage.ErrEpochDiferente},
		"o pareamento": {`DELETE FROM sync_state`, storage.ErrPareamentoMudou},
	} {
		t.Run(nome, func(t *testing.T) {
			p, n := novoPCQueRecebe(t)
			p.aceitar()
			n.mudancas = []Change{ditadoDoPull(1, idM1, "de outro PC"), naSeq(2, doPull(1, fioDoPar("sf", "ECF", false, "", false)))}
			n.antes = func() { p.exec(c.mudar) }
			antes := p.bytesDoArquivo()
			if _, err := p.puxar(ctx); !errors.Is(err, c.erro) {
				t.Fatalf("o Receber: %v, want %v", err, c.erro)
			}
			if !bytes.Equal(antes, p.bytesDoArquivo()) {
				t.Fatal("o arquivo foi tocado com " + nome + " mudado")
			}
			if e := p.estado(idSF); e.TemLinha || e.VeioDaNuvem {
				t.Errorf("o espelho: %+v", e)
			}
			if _, err := p.repo.Get(ctx, donoDoMotor, idM1); err == nil {
				t.Error("o ditado da página entrou")
			}
			if c.erro != storage.ErrEpochDiferente {
				return
			}
			if cur, _ := p.cursor(t); cur != 0 {
				t.Errorf("o cursor andou: %d", cur)
			}
			// A rodada recomeça (o backup restaurado) e lê de novo: agora vale.
			rel, err := p.motor.Rodada(ctx)
			if err != nil || rel.Recomecos != 1 || rel.PullDoAprendido[TipoAplicado] != 1 {
				t.Fatalf("a rodada: %+v, %v", rel, err)
			}
			if projecaoNo(p.arquivo(), idSF) == nil || len(p.nuvem.enviadas) != 0 {
				t.Errorf("depois do recomeço: %+v", p.nuvem.enviadas)
			}
		})
	}
}

// ─── O par mudado aqui ───

// Um Aprender do painel no meio é pulado: o aplicador nunca sobrescreve uma
// mudança daqui que não subiu. O par da nuvem fica guardado como pendente, a
// mudança daqui sobe pela varredura, e a nuvem decide (D4).
func TestReceber_UmAprenderNoMeioEPulado(t *testing.T) {
	ctx := context.Background()
	t.Run("entre o pull e o arquivo", func(t *testing.T) {
		p, n := novoPCQueRecebe(t)
		p.aceitar()
		n.mudancas = []Change{naSeq(1, doPull(1, fioDoPar("sf", "ECD", false, "", false)))}
		n.antes = func() { p.aprender("ECF", "sf") }
		rel, err := p.puxar(ctx)
		if err != nil || rel.PullDoAprendido[TipoPulado] != 1 {
			t.Fatalf("o pull: %+v, %v", rel, err)
		}
		if got := projecaoNo(p.arquivo(), idSF); got == nil || got.Termo != "ECF" {
			t.Fatalf("o arquivo: %+v", got)
		}
		e := p.estado(idSF)
		if e.Nuvem == nil || *e.Nuvem.Versao != 1 || e.Nuvem.Par.Termo != "ECD" || e.ServerVersion != nil || e.VeioDaNuvem {
			t.Fatalf("o pendente: %+v", e)
		}
		if c, _ := p.cursor(t); c != 1 {
			t.Errorf("o cursor = %d", c)
		}
		// A mudança daqui sobe; a nuvem já tem o ECD e vale o primeiro (D4).
		p.nuvem.responder = func(Mutation) MutationResult {
			return MutationResult{Status: StatusConflict, EntityVersion: 1, StateAprendido: fioDoPar("sf", "ECD", false, "", false)}
		}
		p.varrer()
		p.enviar()
		if ms := p.nuvem.doId(idSF); len(ms) != 1 || ms[0].Operation != OpUpsert || ms[0].BaseVersion != 0 {
			t.Fatalf("a mudança daqui: %+v", ms)
		}
		if r := p.varrer(); r.Reaplicados[TipoTrocado] != 1 {
			t.Fatalf("o par da nuvem: %+v", r)
		}
		if got := projecaoNo(p.arquivo(), idSF); got == nil || got.Termo != "ECD" ||
			p.estado(idSF).Situacao != storage.EspelhoTrocadoEmOutro {
			t.Errorf("a D4: %+v, %+v", got, p.estado(idSF))
		}
	})
	t.Run("o Desfazer e o Aprender que deixam o arquivo igual", func(t *testing.T) {
		p, n := novoPCQueRecebe(t)
		p.aceitar()
		p.sincronizado("ECF", "sf")
		n.mudancas = []Change{naSeq(1, doPull(2, fioDoPar("sf", "ECD", false, "", false)))}
		antes := p.bytesDoArquivo()
		n.antes = func() {
			p.desfazer("sf")
			p.aprender("ECF", "sf")
		}
		rel, err := p.puxar(ctx)
		if err != nil || rel.PullDoAprendido[TipoPulado] != 1 {
			t.Fatalf("o pull: %+v, %v", rel, err)
		}
		if !bytes.Equal(antes, p.bytesDoArquivo()) {
			t.Fatal("o arquivo mudou")
		}
		e := p.estado(idSF)
		if e.Nuvem == nil || *e.Nuvem.Versao != 2 || *e.ServerVersion != 1 || !e.MudouAqui() {
			t.Fatalf("o pendente: %+v", e)
		}
		if r := p.varrer(); r.Tipos[TipoVarreduraUpsert] != 1 {
			t.Errorf("a mudança daqui não sobe: %+v", r)
		}
	})
	t.Run("entre o arquivo e a página", func(t *testing.T) {
		p, n := novoPCQueRecebe(t)
		p.aceitar()
		n.mudancas = []Change{naSeq(1, doPull(1, fioDoPar("sf", "ECD", false, "", false)))}
		p.motor.Dicionario = &aplicadorDeTeste{DicionarioDoAprendido: p.arq, depois: func() { p.desfazer("sf") }}
		rel, err := p.puxar(ctx)
		if err != nil || rel.PullDoAprendido[TipoAplicado] != 1 {
			t.Fatalf("o pull: %+v, %v", rel, err)
		}
		if projecaoNo(p.arquivo(), idSF) != nil {
			t.Fatal("o aplicador sobrescreveu o Desfazer do painel")
		}
		if e := p.estado(idSF); !e.MudouAqui() || *e.ServerVersion != 1 {
			t.Fatalf("o espelho: %+v", e)
		}
		p.motor.Dicionario = p.arq
		if r := p.varrer(); r.Tipos[TipoVarreduraDelete] != 1 {
			t.Fatalf("o Desfazer do painel: %+v", r)
		}
		p.enviar()
		if ms := p.nuvem.doId(idSF); len(ms) != 1 || ms[0].Operation != OpDelete || ms[0].BaseVersion != 1 {
			t.Errorf("o desfazer: %+v", ms)
		}
	})
}

// ─── A queda ───

// A queda entre o arquivo e o espelho converge sem envio: a linha fica
// "aplicando" com o arquivo já gravado (e o cursor não anda); na volta, a
// varredura confirma pelo arquivo, o pull da mesma versão converge, e nada
// sobe. O mesmo com o epoch mudado entre o arquivo e a página (o backup
// restaurado no meio): a rodada recomeça e converge, sem envio.
func TestReceber_AQuedaEntreOArquivoEOEspelhoConvergeSemEnvio(t *testing.T) {
	confere := func(t *testing.T, p *pcQueVarre) {
		t.Helper()
		e := p.estado(idSF)
		quero := &dictionary.ParDoPainel{ID: idSF, Variante: "sf", Termo: "ECF"}
		if e.Situacao != storage.EspelhoAplicado || !reflect.DeepEqual(e.Projecao, quero) || e.ServerVersion == nil ||
			*e.ServerVersion != 1 || !e.VeioDaNuvem || e.Marcador != nil || e.Esperada != nil || e.Nuvem != nil || e.MudouAqui() {
			t.Fatalf("convergiu: %+v", e)
		}
		if _, err := p.motor.Rodada(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(p.nuvem.enviadas) != 0 || len(p.marcados()) != 0 {
			t.Errorf("a queda virou envio: %+v", p.nuvem.enviadas)
		}
	}
	t.Run("a queda", func(t *testing.T) {
		p, n := novoPCQueRecebe(t)
		p.aceitar()
		n.mudancas = []Change{naSeq(1, doPull(1, fioDoPar("sf", "ecf", false, "", false)))}
		ctx, cair := context.WithCancel(context.Background())
		p.motor.Dicionario = &aplicadorDeTeste{DicionarioDoAprendido: p.arq, depois: cair}
		if _, err := p.puxar(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("a queda: %v", err)
		}
		e := p.estado(idSF)
		if e.Situacao != storage.EspelhoAplicando || e.Esperada == nil || e.Esperada.Termo != "ECF" ||
			projecaoNo(p.arquivo(), idSF) == nil {
			t.Fatalf("depois da queda: %+v", e)
		}
		if c, _ := p.cursor(t); c != 0 {
			t.Fatalf("o cursor andou: %d", c)
		}
		// A volta do PC: a rodada inteira, com o aplicador de verdade.
		p.motor.Dicionario = p.arq
		rel, err := p.motor.Rodada(context.Background())
		if err != nil || rel.Varredura[TipoVarreduraConfirmado] != 1 || rel.PullDoAprendido[TipoConvergente] != 1 {
			t.Fatalf("a volta: %+v, %v", rel, err)
		}
		if c, _ := p.cursor(t); c != 1 {
			t.Errorf("o cursor = %d", c)
		}
		confere(t, p)
	})
	t.Run("o epoch mudado entre o arquivo e a página", func(t *testing.T) {
		p, n := novoPCQueRecebe(t)
		p.aceitar()
		n.mudancas = []Change{naSeq(1, doPull(1, fioDoPar("sf", "ecf", false, "", false)))}
		p.motor.Dicionario = &aplicadorDeTeste{DicionarioDoAprendido: p.arq, depois: func() {
			p.exec(`UPDATE sync_state SET sync_epoch = 'e2'`)
			n.epoch = "e2"
		}}
		rel, err := p.motor.Rodada(context.Background())
		if err != nil || rel.Recomecos != 1 || rel.PullDoAprendido[TipoConvergente] != 1 {
			t.Fatalf("a rodada: %+v, %v", rel, err)
		}
		if _, e := p.cursor(t); e != "e2" {
			t.Errorf("o epoch = %q", e)
		}
		confere(t, p)
	})
}

// ─── O arquivo que não deu ───

// O arquivo inválido (ou ausente, de outro usuário, ou o lote que não
// carrega, ou a gravação que falha depois da intenção) não segura os ditados:
// a página entra, o cursor anda, e o par fica pendente aqui, com o motivo;
// quando o arquivo volta, a varredura o aplica, sem envio.
func TestReceber_OArquivoQueNaoDeuNaoSeguraOsDitados(t *testing.T) {
	ctx := context.Background()
	for nome, c := range map[string]struct {
		estragar  func(p *pcQueVarre)
		consertar func(p *pcQueVarre, original []byte)
		// aplicando: a intenção foi gravada antes da falha, e a linha fica
		// "aplicando" para a varredura reconciliar.
		aplicando bool
	}{
		"inválido": {
			estragar: func(p *pcQueVarre) {
				if err := os.WriteFile(p.arq.Caminho, []byte(`{"usuario": "mauro", "entradas": [`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		"ausente": {
			estragar: func(p *pcQueVarre) {
				if err := os.Remove(p.arq.Caminho); err != nil {
					t.Fatal(err)
				}
			},
		},
		"de outro usuário": {
			estragar: func(p *pcQueVarre) {
				s := sementeDaVarredura()
				s.Usuario = "outra"
				if err := s.Gravar(p.arq.Caminho); err != nil {
					t.Fatal(err)
				}
			},
		},
		"o lote que não carrega": {
			estragar: func(p *pcQueVarre) {
				p.arq.Validar = func(dictionary.Semente) error { return errors.New("não carrega") }
			},
			consertar: func(p *pcQueVarre, _ []byte) { p.arq.Validar = nil },
		},
		"a gravação que falha depois da intenção": {
			estragar: func(p *pcQueVarre) {
				p.motor.Dicionario = &aplicadorDeTeste{DicionarioDoAprendido: p.arq, falharDepoisDaIntencao: errors.New("disco")}
			},
			consertar: func(p *pcQueVarre, _ []byte) { p.motor.Dicionario = p.arq },
			aplicando: true,
		},
	} {
		t.Run(nome, func(t *testing.T) {
			p, n := novoPCQueRecebe(t)
			p.aceitar()
			original := p.bytesDoArquivo()
			c.estragar(p)
			n.mudancas = []Change{ditadoDoPull(1, idM1, "de outro PC"), naSeq(2, doPull(1, fioDoPar("sf", "ECF", false, "", false)))}
			antes, _ := os.ReadFile(p.arq.Caminho)
			rel, err := p.puxar(ctx)
			if err != nil {
				t.Fatalf("o arquivo %s segurou a página: %v", nome, err)
			}
			if rel.Pull[TipoRecebido] != 1 || rel.PullDoAprendido[TipoPendente] != 1 {
				t.Errorf("o relatório: %+v", rel)
			}
			if cur, _ := p.cursor(t); cur != 2 {
				t.Errorf("o cursor = %d, want 2", cur)
			}
			if l, err := p.repo.Get(ctx, donoDoMotor, idM1); err != nil || l.FinalText != "de outro PC" {
				t.Errorf("o ditado: %+v, %v", l, err)
			}
			if depois, _ := os.ReadFile(p.arq.Caminho); !bytes.Equal(antes, depois) {
				t.Error("o arquivo foi tocado")
			}
			e := p.estado(idSF)
			if e.Nuvem == nil || *e.Nuvem.Versao != 1 || e.Nuvem.Par.Termo != "ECF" || e.ServerVersion != nil || e.Presente() {
				t.Fatalf("o par da nuvem guardado: %+v", e)
			}
			switch {
			case c.aplicando:
				if e.Situacao != storage.EspelhoAplicando {
					t.Errorf("a intenção: %+v", e)
				}
			case !TemPendente(e) || e.Situacao != storage.EspelhoNaoAplicado ||
				e.Motivo != string(dictionary.MotivoArquivoIndisponivel):
				t.Errorf("o pendente: %+v", e)
			}

			// O arquivo volta: a varredura o aplica (a linha "aplicando" que
			// a gravação deixou volta a pendente antes), sem envio.
			if c.consertar != nil {
				c.consertar(p, original)
			} else if err := os.WriteFile(p.arq.Caminho, original, 0o600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2 && projecaoNo(p.arquivo(), idSF) == nil; i++ {
				p.varrer()
			}
			e = p.estado(idSF)
			if projecaoNo(p.arquivo(), idSF) == nil || e.Situacao != storage.EspelhoAplicado || *e.ServerVersion != 1 {
				t.Fatalf("o arquivo voltou: %+v", e)
			}
			p.varrer()
			p.enviar()
			if len(p.nuvem.enviadas) != 0 {
				t.Errorf("o pendente aplicado subiu: %+v", p.nuvem.enviadas)
			}
		})
	}
}

// A troca de um par que já vale aqui, com o arquivo indisponível, também fica
// pendente (e não pulada: sem o arquivo, o PC não sabe se o par mudou aqui),
// com a base de antes; quando o arquivo volta, a varredura troca o par.
func TestReceber_ATrocaComOArquivoIndisponivelFicaPendente(t *testing.T) {
	p, n := novoPCQueRecebe(t)
	p.aceitar()
	p.sincronizado("ECF", "sf")
	original := p.bytesDoArquivo()
	if err := os.Remove(p.arq.Caminho); err != nil {
		t.Fatal(err)
	}
	n.mudancas = []Change{naSeq(1, doPull(2, fioDoPar("sf", "ECD", false, "", false)))}
	rel, err := p.puxar(context.Background())
	if err != nil || rel.PullDoAprendido[TipoPendente] != 1 {
		t.Fatalf("o pull: %+v, %v", rel, err)
	}
	e := p.estado(idSF)
	if e.Situacao != storage.EspelhoNaoAplicado || e.Motivo != string(dictionary.MotivoArquivoIndisponivel) ||
		*e.ServerVersion != 1 || *e.Nuvem.Versao != 2 || e.Projecao == nil || e.Projecao.Termo != "ECF" {
		t.Fatalf("o pendente: %+v", e)
	}
	if err := os.WriteFile(p.arq.Caminho, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if r := p.varrer(); r.Reaplicados[TipoTrocado] != 1 {
		t.Fatalf("o arquivo voltou: %+v", r)
	}
	if got := projecaoNo(p.arquivo(), idSF); got == nil || got.Termo != "ECD" || *p.estado(idSF).ServerVersion != 2 {
		t.Errorf("a troca: %+v, %+v", got, p.estado(idSF))
	}
}

// A intenção que o SQLite não grava segura a página: o arquivo não é
// gravado (a intenção vem antes dele), nada da página é aplicado, e o cursor
// não anda; só o erro de E/S segura o cursor. A falha do SQLite é um gatilho
// no banco de teste que recusa a situação "aplicando".
func TestReceber_AIntencaoQueNaoGravaSeguraAPagina(t *testing.T) {
	ctx := context.Background()
	p, n := novoPCQueRecebe(t)
	p.exec(`CREATE TRIGGER sem_intencao BEFORE UPDATE OF situacao ON sync_aprendido
		WHEN NEW.situacao = 'aplicando' BEGIN SELECT RAISE(ABORT, 'disco'); END`)
	n.mudancas = []Change{ditadoDoPull(1, idM1, "de outro PC"), naSeq(2, doPull(1, fioDoPar("sf", "ECF", false, "", false)))}
	antes := p.bytesDoArquivo()
	if _, err := p.puxar(ctx); err == nil {
		t.Fatal("a intenção falhou, e a página entrou")
	}
	if !bytes.Equal(antes, p.bytesDoArquivo()) {
		t.Error("o arquivo foi gravado sem a intenção")
	}
	if c, _ := p.cursor(t); c != 0 {
		t.Errorf("o cursor andou: %d", c)
	}
	if _, err := p.repo.Get(ctx, donoDoMotor, idM1); err == nil {
		t.Error("o ditado da página entrou")
	}
	if e := p.estado(idSF); e.TemLinha {
		t.Errorf("o espelho: %+v", e)
	}
}

// ─── O não aplicado ───

// O par que não vale aqui (o domínio não existe) e a mudança que nem é um par
// (a recusa por regra) não seguram o cursor, página a página: o par fica no
// espelho, "não aplicado" com o motivo, com a base andando até a versão dele,
// e o arquivo não é tocado.
func TestReceber_ONaoAplicadoAndaOCursor(t *testing.T) {
	ctx := context.Background()
	p, n := novoPCQueRecebe(t)
	n.porPagina = 1
	n.mudancas = []Change{
		naSeq(1, doPull(3, fioDoPar("sf", "ECD", true, "fiscal", false))),
		{Seq: 2, EntityType: storage.EntidadeAprendido, EntityID: "lixo", Operation: OpUpsert, EntityVersion: 1},
		ditadoDoPull(3, idM1, "de outro PC"),
	}
	antes := p.bytesDoArquivo()
	rel, err := p.puxar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Paginas != 3 || rel.PullDoAprendido[TipoNaoAplicado] != 2 || rel.Pull[TipoRecebido] != 1 {
		t.Errorf("o relatório: %+v", rel)
	}
	if c, _ := p.cursor(t); c != 3 {
		t.Errorf("o cursor = %d, want 3", c)
	}
	if !bytes.Equal(antes, p.bytesDoArquivo()) {
		t.Error("o arquivo foi tocado")
	}
	e := p.estado(idSF)
	if e.Situacao != storage.EspelhoNaoAplicado || e.Motivo != string(dictionary.MotivoDominioAusente) ||
		*e.ServerVersion != 3 || e.Nuvem == nil || *e.Nuvem.Versao != 3 || e.VeioDaNuvem {
		t.Errorf("o não aplicado: %+v", e)
	}
	if e := p.estado("lixo"); e.TemLinha {
		t.Errorf("a mudança que não é par foi guardada: %+v", e)
	}
}
