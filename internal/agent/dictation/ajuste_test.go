package dictation

import (
	"errors"
	"testing"
	"time"
)

// limiarDoKeydown devolve a duração do timer de hold que o keydown armou.
func limiarDoKeydown(t *testing.T, m *Machine) time.Duration {
	t.Helper()
	timers := effectsOfType[StartTimerEffect](m.Handle(KeyDownEvent{Focus: unprotectedFocus()}))
	if len(timers) != 1 || timers[0].Kind != TimerHoldThreshold {
		t.Fatalf("keydown StartTimerEffect = %#v, want um TimerHoldThreshold", timers)
	}
	return timers[0].Duration
}

// TestAjustarPreferencias_ValeNoKeydownSeguinte: o limiar novo é o que o
// próximo keydown arma, e o beep novo é o que o hold vencido emite.
func TestAjustarPreferencias_ValeNoKeydownSeguinte(t *testing.T) {
	m := newTestMachine(t, true)
	if err := m.AjustarPreferencias(1500*time.Millisecond, false); err != nil {
		t.Fatalf("AjustarPreferencias: %v", err)
	}
	if got := limiarDoKeydown(t, m); got != 1500*time.Millisecond {
		t.Fatalf("limiar armado = %v, quero 1500ms", got)
	}
	efeitos := m.Handle(TimerElapsedEvent{Attempt: m.CurrentAttempt(), Kind: TimerHoldThreshold})
	if m.State() != StateRecording {
		t.Fatalf("state = %v, quero StateRecording", m.State())
	}
	if got := effectsOfType[PlayBeepEffect](efeitos); len(got) != 0 {
		t.Fatalf("beep desligado e mesmo assim: %#v", got)
	}
}

// TestAjustarPreferencias_LigaOBeep: o outro sentido — beep ligado depois
// de criado sem ele.
func TestAjustarPreferencias_LigaOBeep(t *testing.T) {
	m := newTestMachine(t, false)
	if err := m.AjustarPreferencias(300*time.Millisecond, true); err != nil {
		t.Fatalf("AjustarPreferencias: %v", err)
	}
	if got := limiarDoKeydown(t, m); got != 300*time.Millisecond {
		t.Fatalf("limiar armado = %v, quero 300ms", got)
	}
	efeitos := m.Handle(TimerElapsedEvent{Attempt: m.CurrentAttempt(), Kind: TimerHoldThreshold})
	if got := effectsOfType[PlayBeepEffect](efeitos); len(got) != 1 {
		t.Fatalf("beep ligado e nada: %#v", efeitos)
	}
}

// TestAjustarPreferencias_ForaDaFaixaNaoMudaNada: valor inválido é
// recusado com o mesmo erro de NewMachine, e o que valia continua valendo
// — inclusive o beep, que veio no mesmo pedido.
func TestAjustarPreferencias_ForaDaFaixaNaoMudaNada(t *testing.T) {
	m := newTestMachine(t, true)
	for _, d := range []time.Duration{299 * time.Millisecond, 2001 * time.Millisecond, 0} {
		if err := m.AjustarPreferencias(d, false); !errors.Is(err, ErrThresholdOutOfRange) {
			t.Fatalf("AjustarPreferencias(%v): err = %v, quero ErrThresholdOutOfRange", d, err)
		}
	}
	if got := limiarDoKeydown(t, m); got != 700*time.Millisecond {
		t.Fatalf("limiar = %v depois de pedidos recusados, quero os 700ms de antes", got)
	}
	efeitos := m.Handle(TimerElapsedEvent{Attempt: m.CurrentAttempt(), Kind: TimerHoldThreshold})
	if got := effectsOfType[PlayBeepEffect](efeitos); len(got) != 1 {
		t.Fatal("o beep mudou num pedido recusado")
	}
}

// TestAjustarPreferencias_NaoMexeNaTeclaNemNoLimiteDeGravacao: só o que
// foi pedido muda.
func TestAjustarPreferencias_NaoMexeNaTeclaNemNoLimiteDeGravacao(t *testing.T) {
	m, err := NewMachine(Config{Modifiers: ModControl | ModMeta, HoldThreshold: 700 * time.Millisecond, MaxRecording: 42 * time.Second})
	if err != nil {
		t.Fatalf("NewMachine: %v", err)
	}
	if err := m.AjustarPreferencias(900*time.Millisecond, true); err != nil {
		t.Fatalf("AjustarPreferencias: %v", err)
	}
	if m.cfg.Key != KeyUnknown || m.cfg.Modifiers != ModControl|ModMeta || m.cfg.MaxRecording != 42*time.Second {
		t.Fatalf("cfg depois do ajuste = %+v", m.cfg)
	}
}
