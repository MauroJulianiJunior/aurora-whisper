package winruntime

// A mensagem própria para janela de administrador (decisão do Mauro de
// 29-set-2026, item 4) atravessa o runtime sem tocar no núcleo: o Machine
// bloqueia por Protected e emite OverlayBlocked, como sempre; o runtime,
// que leu no keydown que a proteção era por janela elevada, troca a fase
// mostrada. Estes testes rodam a tentativa inteira — keydown, o timer do
// hold vencendo, o desfecho — com o relógio falso e um overlay sem janela.

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// focoElevado é o dublê de um focusProvider que sabe da janela de
// administrador, como *uia.Tracker. Ponteiro, para o teste mudar o que ele
// diz entre um keydown e outro.
type focoElevado struct {
	alvo    dictation.FocusTarget
	elevada bool
}

func (f *focoElevado) Current() dictation.FocusTarget { return f.alvo }

func (f *focoElevado) CurrentComElevacao() (dictation.FocusTarget, bool) {
	return f.alvo, f.elevada
}

var alvoProtegido = dictation.FocusTarget{Protected: true}

// pilulaFalsa faz o papel do overlay: guarda cada fase que o runtime mandou
// mostrar, e cada amplitude que mandou para a onda.
type pilulaFalsa struct {
	fases []dictation.OverlayPhase
	amps  []float64
}

func (p *pilulaFalsa) SetPhase(f dictation.OverlayPhase) { p.fases = append(p.fases, f) }
func (p *pilulaFalsa) PushAmplitude(a float64)           { p.amps = append(p.amps, a) }

// ultima devolve a última fase mostrada.
func (p *pilulaFalsa) ultima(t *testing.T) dictation.OverlayPhase {
	t.Helper()
	if len(p.fases) == 0 {
		t.Fatal("o runtime não mostrou fase nenhuma")
	}
	return p.fases[len(p.fases)-1]
}

// runtimeComLog monta o runtime dos testes com um log que o teste lê e uma
// pílula falsa que guarda as fases.
func runtimeComLog(t *testing.T, focus focusProvider) (*Runtime, *bytes.Buffer, *pilulaFalsa) {
	t.Helper()
	rt := testRuntime(t, focus)
	var buf bytes.Buffer
	rt.log = slog.New(slog.NewTextHandler(&buf, nil))
	p := &pilulaFalsa{}
	rt.overlay = p
	return rt, &buf, p
}

// segurarAteBloquear aperta a tecla e deixa o hold vencer: num alvo
// protegido, é o que leva ao desfecho OutcomeBlocked e ao overlay.
func segurarAteBloquear(t *testing.T, rt *Runtime) {
	t.Helper()
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if got := rt.machine.State(); got != dictation.StateBlocked {
		t.Fatalf("depois do keydown: State = %v, quero StateBlocked", got)
	}
	rt.clk.(*clock.Fake).Advance(700 * time.Millisecond)
	select {
	case ev := <-rt.pending:
		rt.dispatch(ev)
	default:
		t.Fatal("o timer do hold não venceu")
	}
}

// linhaDoOverlay devolve a linha "winruntime: overlay" do bloqueio.
func linhaDoOverlay(t *testing.T, log *bytes.Buffer) string {
	t.Helper()
	for _, l := range strings.Split(log.String(), "\n") {
		if strings.Contains(l, "winruntime: overlay") && strings.Contains(l, "fase=blocked") {
			return l
		}
	}
	t.Fatalf("nenhuma linha de overlay com fase=blocked no log:\n%s", log.String())
	return ""
}

// TestJanelaElevada_BloqueioDizOMotivo: o bloqueio numa janela de
// administrador sai com o motivo; o log continua dizendo a fase do núcleo.
func TestJanelaElevada_BloqueioDizOMotivo(t *testing.T) {
	rt, log, pilula := runtimeComLog(t, &focoElevado{alvo: alvoProtegido, elevada: true})
	segurarAteBloquear(t, rt)
	if got := pilula.ultima(t); got != overlay.FaseJanelaElevada {
		t.Errorf("a pílula recebeu %v, quero a mensagem de janela de administrador", got)
	}
	if l := linhaDoOverlay(t, log); !strings.Contains(l, "motivo=janela_elevada") {
		t.Errorf("a linha do bloqueio não diz o motivo: %s", l)
	}
}

// TestJanelaElevada_SemAMarcaEOBloqueioDeSempre: sem a marca — ou com um
// provedor que nem sabe dela —, o bloqueio é o de sempre.
func TestJanelaElevada_SemAMarcaEOBloqueioDeSempre(t *testing.T) {
	for nome, focus := range map[string]focusProvider{
		"provedor que sabe, janela comum": &focoElevado{alvo: alvoProtegido},
		"provedor que não sabe":           fakeFocus{target: alvoProtegido},
	} {
		t.Run(nome, func(t *testing.T) {
			rt, log, pilula := runtimeComLog(t, focus)
			segurarAteBloquear(t, rt)
			if got := pilula.ultima(t); got != dictation.OverlayBlocked {
				t.Errorf("a pílula recebeu %v, quero o bloqueio de sempre", got)
			}
			if l := linhaDoOverlay(t, log); strings.Contains(l, "motivo=") {
				t.Errorf("um bloqueio comum saiu com motivo: %s", l)
			}
		})
	}
}

// TestJanelaElevada_KeydownNoMeioNaoRemarca: um keydown no meio de uma
// tentativa é ignorado pelo núcleo, e não pode trocar a mensagem da
// tentativa em curso com uma leitura mais nova do foco.
func TestJanelaElevada_KeydownNoMeioNaoRemarca(t *testing.T) {
	foco := &focoElevado{alvo: alvoProtegido}
	rt, log, pilula := runtimeComLog(t, foco)

	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // a tentativa começa numa janela comum
	foco.elevada = true
	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // auto-repeat: o núcleo ignora

	rt.clk.(*clock.Fake).Advance(700 * time.Millisecond)
	rt.dispatch(<-rt.pending)
	if got := pilula.ultima(t); got != dictation.OverlayBlocked {
		t.Errorf("a pílula recebeu %v: o keydown ignorado remarcou a tentativa", got)
	}
	if l := linhaDoOverlay(t, log); strings.Contains(l, "motivo=") {
		t.Errorf("o keydown ignorado remarcou a tentativa: %s", l)
	}
}

// TestJanelaElevada_MarcaSoComAlvoProtegido: uma marca num alvo livre (que
// o rastreador nunca publica, mas o runtime não confia) não marca a
// tentativa — ela não é bloqueio.
func TestJanelaElevada_MarcaSoComAlvoProtegido(t *testing.T) {
	rt := testRuntime(t, &focoElevado{alvo: dictation.FocusTarget{}, elevada: true})
	rt.audio = novaCapturaFalsa()
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if got := rt.machine.State(); got != dictation.StatePrimed {
		t.Fatalf("State = %v, quero StatePrimed (alvo livre)", got)
	}
	if rt.elevadaEm != dictation.NoAttempt {
		t.Errorf("elevadaEm = %v numa tentativa sem bloqueio", rt.elevadaEm)
	}
}

// TestJanelaElevada_ATentativaSeguinteNaoHerda: depois do bloqueio na
// janela de administrador, a tentativa seguinte, numa janela comum
// protegida, volta à mensagem de sempre.
func TestJanelaElevada_ATentativaSeguinteNaoHerda(t *testing.T) {
	foco := &focoElevado{alvo: alvoProtegido, elevada: true}
	rt, log, pilula := runtimeComLog(t, foco)
	segurarAteBloquear(t, rt)
	rt.handleRaw(dictation.RawKeyEvent{Down: false})

	foco.elevada = false
	log.Reset()
	segurarAteBloquear(t, rt)
	if got := pilula.ultima(t); got != dictation.OverlayBlocked {
		t.Errorf("a pílula recebeu %v: a tentativa seguinte herdou a marca", got)
	}
	if l := linhaDoOverlay(t, log); strings.Contains(l, "motivo=") {
		t.Errorf("a tentativa seguinte herdou a marca: %s", l)
	}
}
