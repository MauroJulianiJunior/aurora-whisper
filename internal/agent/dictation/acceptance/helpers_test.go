package acceptance

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation/fakes"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// baseTime é o instante em que o clock.Fake de cada teste começa. É fixo e
// arbitrário só para que a saída dos testes nunca dependa do relógio de
// parede real.
var baseTime = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

// defaultConfig é a tecla F com o threshold default de 700ms (dentro da
// faixa [300ms, 2000ms] do AC-044) e beep desligado.
func defaultConfig() dictation.Config {
	return dictation.Config{Key: dictation.KeyF, HoldThreshold: 700 * time.Millisecond, BeepEnabled: false}
}

// harness conecta um Machine a um Runtime e aos seus dublês, todos
// compartilhando um único clock.Fake, prontos para um teste dirigir com
// eventos sintéticos.
type harness struct {
	t *testing.T

	Machine *dictation.Machine
	Clock   *clock.Fake
	Audio   *fakes.FakeAudioSession
	ASR     *fakes.FakeTranscriber
	Resolve *fakes.FakeResolver
	Insert  *fakes.FakeInserter
	Runtime *fakes.Runtime
}

// newHarness cria um harness novo para cfg, com o clock.Fake em baseTime.
func newHarness(t *testing.T, cfg dictation.Config) *harness {
	t.Helper()
	m, err := dictation.NewMachine(cfg)
	if err != nil {
		t.Fatalf("NewMachine(%+v): erro inesperado: %v", cfg, err)
	}
	c := clock.NewFake(baseTime)
	audio := fakes.NewFakeAudioSession()
	asr := fakes.NewFakeTranscriber()
	resolver := fakes.NewFakeResolver()
	inserter := fakes.NewFakeInserter()
	rt := fakes.NewRuntime(m, c, audio, asr, resolver, inserter)
	return &harness{t: t, Machine: m, Clock: c, Audio: audio, ASR: asr, Resolve: resolver, Insert: inserter, Runtime: rt}
}

// keyDown dispara um KeyDownEvent para focus e devolve o AttemptID que o
// Machine atribuiu — o próprio Runtime.Suppressed[a] mais recente, já que
// SuppressKeyEffect é sempre o primeiro efeito de uma tentativa não
// bloqueada. Para o caminho bloqueado (campo protegido), use
// h.Machine.CurrentAttempt() ANTES de despachar, pois a tentativa já
// termina dentro do próprio Dispatch.
func (h *harness) keyDown(focus dictation.FocusTarget) dictation.AttemptID {
	h.t.Helper()
	before := len(h.Runtime.Suppressed)
	h.Runtime.Dispatch(dictation.KeyDownEvent{Attempt: dictation.NoAttempt, Focus: focus})
	if len(h.Runtime.Suppressed) > before {
		return h.Runtime.Suppressed[len(h.Runtime.Suppressed)-1]
	}
	// Campo protegido: SuppressKeyEffect não é emitido pelo Machine (a
	// supressão física já aconteceu no Decider, antes de o evento chegar
	// aqui). O AttemptID ainda pode ser lido do RecordHistoryEffect mais
	// recente.
	if n := len(h.Runtime.History); n > 0 {
		return h.Runtime.History[n-1].Attempt
	}
	h.t.Fatalf("keyDown: não foi possível determinar o AttemptID atribuído")
	return dictation.NoAttempt
}

func (h *harness) keyUp(attempt dictation.AttemptID) []dictation.Effect {
	h.t.Helper()
	return h.Runtime.Dispatch(dictation.KeyUpEvent{Attempt: attempt})
}

// normalFocus é um campo de texto comum, não protegido.
func normalFocus() dictation.FocusTarget {
	return dictation.FocusTarget{ProcessName: "notepad.exe", ControlType: "Edit"}
}

// protectedFocus é um campo de senha (AC-036/TEST-009).
func protectedFocus() dictation.FocusTarget {
	return dictation.FocusTarget{ProcessName: "chrome.exe", ControlType: "Edit", Protected: true}
}

// lastHistory devolve o RecordHistoryEffect mais recente observado pelo
// Runtime, falhando o teste se nenhum foi emitido ainda.
func (h *harness) lastHistory() dictation.RecordHistoryEffect {
	h.t.Helper()
	if len(h.Runtime.History) == 0 {
		h.t.Fatalf("nenhum RecordHistoryEffect foi emitido")
	}
	return h.Runtime.History[len(h.Runtime.History)-1]
}
