package winruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/paranota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// O gatilho "ditar para nota" no runtime (ADR-022, N9). Tudo com relógio falso,
// microfone falso, ASR falso, histórico falso e notas falsas: nada aqui injeta
// tecla, escreve no clipboard nem abre janela. A sessão chega com a marca
// RawKeyEvent.ParaNota, como o hook a mandaria (dictation.Gatilhos).

// notasFalsas faz o papel de paranota.Gravar: guarda o que recebeu e devolve o
// que o teste mandar.
type notasFalsas struct {
	textos, ids []string
	res         paranota.Resultado
	err         error
}

func (n *notasFalsas) gravar(_ context.Context, ditadoID, texto string) (paranota.Resultado, error) {
	n.textos = append(n.textos, texto)
	n.ids = append(n.ids, ditadoID)
	return n.res, n.err
}

// pilulaComTexto é a pílula falsa que também aceita o texto da mensagem.
type pilulaComTexto struct {
	pilulaFalsa
	textos []string
}

func (p *pilulaComTexto) SetPhaseComTexto(f dictation.OverlayPhase, texto string) {
	p.fases = append(p.fases, f)
	p.textos = append(p.textos, texto)
}

// insercaoFalsa guarda o que iria para a thread de inserção (que não roda
// aqui): prova que o texto foi, ou não, para o campo em foco.
func pedidosDeInsercao(rt *Runtime) []dictation.InsertEffect {
	var out []dictation.InsertEffect
	for {
		select {
		case e := <-rt.insercoes:
			out = append(out, e)
		default:
			return out
		}
	}
}

type bancadaDaNota struct {
	*bancada
	notas  *notasFalsas
	hist   *historicoFalso
	pilula *pilulaComTexto
}

func novaBancadaDaNota(t *testing.T, texto string) (*bancadaDaNota, *asrFalso) {
	t.Helper()
	b := &bancadaDaNota{bancada: novaBancada(t, true), notas: &notasFalsas{}, hist: &historicoFalso{}, pilula: &pilulaComTexto{}}
	b.rt.overlay = b.pilula
	b.bancada.pilula = &b.pilula.pilulaFalsa
	prov := &asrFalso{texto: texto, entrou: make(chan struct{}), liberar: make(chan struct{})}
	b.rt.asr = prov
	b.rt.UsarHistorico(b.hist, provedorDeTeste, modeloDeTeste, "mauro")
	b.rt.notas = b.notas.gravar
	b.rt.vaga, b.rt.insercoes = vagaLivre(), make(chan dictation.InsertEffect, 1)
	return b, prov
}

// ditar faz a tentativa inteira: aperta (com a marca do keydown), o hold
// vence, solta (com a marca da soltura), a captura volta, o ASR responde e a
// resolução (literal, sem resolver) leva ao InsertEffect.
func (b *bancadaDaNota) ditar(t *testing.T, prov *asrFalso, notaNoAperto, notaNaSoltura bool) {
	t.Helper()
	b.rt.handleRaw(dictation.RawKeyEvent{Down: true, ParaNota: notaNoAperto})
	b.rt.clk.(*clock.Fake).Advance(700 * time.Millisecond)
	select {
	case ev := <-b.rt.pending:
		b.rt.dispatch(ev)
	default:
		t.Fatal("o timer do hold não venceu")
	}
	attempt := b.rt.machine.CurrentAttempt()
	b.rt.handleRaw(dictation.RawKeyEvent{Down: false, ParaNota: notaNaSoltura})
	select {
	case <-b.rt.capturas:
	case <-time.After(2 * time.Second):
		t.Fatal("a captura não voltou")
	}
	b.rt.dispatch(b.rt.concluirCaptura(capturaPronta{
		attempt: attempt,
		buf:     audio.AudioBuffer{Samples: make([]int16, 16000), SampleRate: 16000},
		vad:     dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second},
	}))
	<-prov.entrou
	close(prov.liberar)
	select {
	case ev := <-b.rt.pending:
		b.rt.aoReceberPendente(ev)
	case <-time.After(2 * time.Second):
		t.Fatal("o ASR não voltou")
	}
}

const ditadoDeCompras = "Lista de compras: leite, pão e café."

func TestParaNota_VaiParaANotaENaoParaOCampo(t *testing.T) {
	b, prov := novaBancadaDaNota(t, ditadoDeCompras)
	b.notas.res = paranota.Resultado{NotaID: "nota-compras", Titulo: "Lista de compras", Linhas: 3}

	b.ditar(t, prov, true, true)

	if ins := pedidosDeInsercao(b.rt); len(ins) != 0 {
		t.Fatalf("o texto foi para a inserção no campo: %d pedido(s)", len(ins))
	}
	if len(b.notas.textos) != 1 || b.notas.textos[0] != ditadoDeCompras {
		t.Fatalf("a nota recebeu %q", b.notas.textos)
	}
	if got := b.rt.machine.State(); got != dictation.StateSuccess {
		t.Fatalf("State = %v, quero sucesso", got)
	}
	if f := b.pilula.ultima(t); f != overlay.FaseNaNota || b.pilula.textos[len(b.pilula.textos)-1] != "Lista de compras +3" {
		t.Fatalf("pílula %v %q, quero FaseNaNota \"Lista de compras +3\"", f, b.pilula.textos)
	}
	l := b.hist.unica(t)
	if l.VirouNota == nil || *l.VirouNota != "nota-compras" {
		t.Fatalf("VirouNota = %v, quero a nota (o ditado sai do histórico)", l.VirouNota)
	}
	if l.Status != "done" || l.FinalText != ditadoDeCompras {
		t.Fatalf("linha %q %q", l.Status, l.FinalText)
	}
	if l.ID != b.notas.ids[0] {
		t.Fatalf("o id da linha (%s) não é o que a nota recebeu (%s)", l.ID, b.notas.ids[0])
	}
	// Nada do texto nem do título no log.
	if s := b.log.String(); strings.Contains(s, "leite") || strings.Contains(s, "Lista de compras") {
		t.Fatalf("texto ou título no log:\n%s", s)
	}
}

func TestParaNota_NotaNova(t *testing.T) {
	b, prov := novaBancadaDaNota(t, "Ligar para o contador.")
	b.notas.res = paranota.Resultado{NotaID: "nova-1", Nova: true}
	b.ditar(t, prov, true, true)
	if f := b.pilula.ultima(t); f != overlay.FaseNotaNova {
		t.Fatalf("pílula %v, quero FaseNotaNova", f)
	}
	if l := b.hist.unica(t); l.VirouNota == nil || *l.VirouNota != "nova-1" {
		t.Fatalf("VirouNota = %v", l.VirouNota)
	}
}

// TestParaNota_AltNoMeio: o keydown veio sem a marca (Ctrl+Win), e a soltura
// com ela (o Alt foi apertado durante a gravação): vai para a nota.
func TestParaNota_AltNoMeio(t *testing.T) {
	b, prov := novaBancadaDaNota(t, ditadoDeCompras)
	b.notas.res = paranota.Resultado{NotaID: "n", Titulo: "Lista de compras", Linhas: 1}
	b.ditar(t, prov, false, true)
	if len(b.notas.textos) != 1 || len(pedidosDeInsercao(b.rt)) != 0 {
		t.Fatal("o Alt no meio da gravação não mandou o texto para a nota")
	}
}

// TestParaNota_FalhaDeixaNoHistorico: a nota não foi gravada. O texto não se
// perde: a linha fica à vista, com o texto, e a pílula diz onde ele está.
func TestParaNota_FalhaDeixaNoHistorico(t *testing.T) {
	b, prov := novaBancadaDaNota(t, ditadoDeCompras)
	b.notas.err = errors.New("disco cheio")
	b.ditar(t, prov, true, true)

	if ins := pedidosDeInsercao(b.rt); len(ins) != 0 {
		t.Fatal("a falha da nota mandou o texto para o campo em foco")
	}
	if f := b.pilula.ultima(t); f != overlay.FaseNotaNaoSalvou {
		t.Fatalf("pílula %v, quero FaseNotaNaoSalvou", f)
	}
	l := b.hist.unica(t)
	if l.VirouNota != nil {
		t.Fatalf("VirouNota = %q: o ditado que não foi para a nota sumiu do histórico", *l.VirouNota)
	}
	if l.FinalText != ditadoDeCompras || l.Status != "error" {
		t.Fatalf("linha %q %q: o texto tem de estar no histórico", l.Status, l.FinalText)
	}
}

// TestParaNota_CtrlWinSemAltInsereComoSempre: sem a marca, o InsertEffect vai
// para a thread de inserção, e as notas nem são chamadas.
func TestParaNota_CtrlWinSemAltInsereComoSempre(t *testing.T) {
	b, prov := novaBancadaDaNota(t, "texto comum")
	b.ditar(t, prov, false, false)
	ins := pedidosDeInsercao(b.rt)
	if len(ins) != 1 || ins[0].Text != "texto comum" {
		t.Fatalf("pedidos de inserção %+v, quero o texto", ins)
	}
	if len(b.notas.textos) != 0 {
		t.Fatal("o Ctrl+Win sem Alt foi para a nota")
	}
}

// TestParaNota_DesligadoInsereComoSempre: com o gatilho desligado nas
// Configurações, a sessão marcada é um ditado comum.
func TestParaNota_DesligadoInsereComoSempre(t *testing.T) {
	b, prov := novaBancadaDaNota(t, "texto comum")
	b.rt.Ajustar(Preferencias{HoldThreshold: 700 * time.Millisecond, SemGatilhoDaNota: true})
	b.ditar(t, prov, true, true)
	if len(pedidosDeInsercao(b.rt)) != 1 || len(b.notas.textos) != 0 {
		t.Fatal("desligado, o gatilho da nota não virou um ditado comum")
	}
}

// TestParaNota_MarcaNaoVazaParaATentativaSeguinte: depois de uma sessão para a
// nota, o Ctrl+Win seguinte insere no campo.
func TestParaNota_MarcaNaoVazaParaATentativaSeguinte(t *testing.T) {
	b, prov := novaBancadaDaNota(t, ditadoDeCompras)
	b.notas.res = paranota.Resultado{NotaID: "n", Titulo: "Lista de compras", Linhas: 3}
	b.ditar(t, prov, true, true)

	prov2 := &asrFalso{texto: "segundo", entrou: make(chan struct{}), liberar: make(chan struct{})}
	b.rt.asr = prov2
	b.ditar(t, prov2, false, false)
	if ins := pedidosDeInsercao(b.rt); len(ins) != 1 || ins[0].Text != "segundo" {
		t.Fatalf("a segunda tentativa não foi inserida: %+v", ins)
	}
	if len(b.notas.textos) != 1 {
		t.Fatalf("a segunda tentativa foi para a nota")
	}
	// A thread de inserção não roda aqui: a resposta dela entra à mão.
	b.rt.dispatch(dictation.InsertResultEvent{Attempt: b.rt.machine.CurrentAttempt(), Result: dictation.InsertResultDTO{Success: true}})
	if f := b.pilula.ultima(t); f != dictation.OverlaySuccess {
		t.Fatalf("pílula da segunda %v, quero \"Inserido\"", f)
	}
	if len(b.hist.linhas) != 2 || b.hist.linhas[1].VirouNota != nil {
		t.Fatal("a segunda linha herdou o virou_nota da primeira")
	}
}

// TestParaNota_SemNotasLigadasInsere: sem UsarNotas (nil), a marca é ignorada.
func TestParaNota_SemNotasLigadasInsere(t *testing.T) {
	b, prov := novaBancadaDaNota(t, "texto")
	b.rt.notas = nil
	b.ditar(t, prov, true, true)
	if len(pedidosDeInsercao(b.rt)) != 1 {
		t.Fatal("sem notas ligadas, o texto se perdeu")
	}
}

func TestFaseDaNota(t *testing.T) {
	ok := paranota.Resultado{Titulo: "Mercado", Linhas: 2}
	casos := []struct {
		fase   dictation.OverlayPhase
		res    paranota.Resultado
		falhou bool
		quer   dictation.OverlayPhase
		texto  string
	}{
		{dictation.OverlaySuccess, ok, false, overlay.FaseNaNota, "Mercado +2"},
		{dictation.OverlaySuccess, paranota.Resultado{Nova: true}, false, overlay.FaseNotaNova, ""},
		{dictation.OverlayInsertFailed, paranota.Resultado{}, true, overlay.FaseNotaNaoSalvou, ""},
		{dictation.OverlayError, ok, false, dictation.OverlayError, ""},
		{dictation.OverlayError, paranota.Resultado{}, true, dictation.OverlayError, ""},
	}
	for _, c := range casos {
		f, txt := FaseDaNota(c.fase, c.res, c.falhou)
		if f != c.quer || txt != c.texto {
			t.Errorf("FaseDaNota(%v, %+v, %v) = %v %q; quer %v %q", c.fase, c.res, c.falhou, f, txt, c.quer, c.texto)
		}
	}
}
