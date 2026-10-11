package clock

import (
	"reflect"
	"testing"
	"time"
)

// start é o instante inicial usado pela maioria dos testes de mesa
// abaixo. O valor em si é arbitrário; só importa ser um ponto de
// referência fixo.
var start = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

// TestFake_Advance é a mesa de teste principal: ordem cronológica,
// empate FIFO, Advance(0) e múltiplos timers disparando ao longo de
// avanços sucessivos.
func TestFake_Advance(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(c *Fake, record func(string))
		advances []time.Duration // aplicados em sequência, cada um relativo ao Now() corrente
		want     []string
	}{
		{
			name: "ordem cronológica, independente da ordem de agendamento",
			setup: func(c *Fake, record func(string)) {
				c.AfterFunc(3*time.Second, func() { record("c") })
				c.AfterFunc(1*time.Second, func() { record("a") })
				c.AfterFunc(2*time.Second, func() { record("b") })
			},
			advances: []time.Duration{3 * time.Second},
			want:     []string{"a", "b", "c"},
		},
		{
			name: "empate de horário resolvido por ordem de agendamento (FIFO)",
			setup: func(c *Fake, record func(string)) {
				c.AfterFunc(time.Second, func() { record("first") })
				c.AfterFunc(time.Second, func() { record("second") })
				c.AfterFunc(time.Second, func() { record("third") })
			},
			advances: []time.Duration{time.Second},
			want:     []string{"first", "second", "third"},
		},
		{
			name: "Advance(0) dispara apenas o que já está vencido",
			setup: func(c *Fake, record func(string)) {
				c.AfterFunc(0, func() { record("immediate") })
				c.AfterFunc(time.Second, func() { record("later") })
			},
			advances: []time.Duration{0},
			want:     []string{"immediate"},
		},
		{
			name: "vários timers disparando ao longo de avanços sucessivos",
			setup: func(c *Fake, record func(string)) {
				c.AfterFunc(1*time.Second, func() { record("t1") })
				c.AfterFunc(2*time.Second, func() { record("t2") })
				c.AfterFunc(5*time.Second, func() { record("t5") })
			},
			advances: []time.Duration{2 * time.Second, 3 * time.Second},
			want:     []string{"t1", "t2", "t5"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewFake(start)
			var order []string
			record := func(label string) { order = append(order, label) }

			tc.setup(c, record)
			for _, d := range tc.advances {
				c.Advance(d)
			}

			if !reflect.DeepEqual(order, tc.want) {
				t.Fatalf("ordem de disparo = %v, want %v", order, tc.want)
			}
		})
	}
}

// TestFake_NowAndSet cobre Now() e Set(): o relógio só se move quando
// o teste manda.
func TestFake_NowAndSet(t *testing.T) {
	c := NewFake(start)

	if got := c.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}

	later := start.Add(5 * time.Minute)
	c.Set(later)
	if got := c.Now(); !got.Equal(later) {
		t.Fatalf("apos Set, Now() = %v, want %v", got, later)
	}
}

// TestFake_ReentrantTimerFiresInSameAdvance cobre o caso central do
// núcleo (ADR-007 §8): um callback disparado durante o Advance agenda
// um novo timer que também vence dentro da mesma janela — esse novo
// timer precisa disparar ainda dentro do mesmo Advance, antes dele
// retornar.
func TestFake_ReentrantTimerFiresInSameAdvance(t *testing.T) {
	c := NewFake(start)
	var order []string

	c.AfterFunc(1*time.Second, func() {
		order = append(order, "outer")
		// Agendado de dentro do callback: vence em 2s, dentro da
		// janela do Advance(5s) abaixo.
		c.AfterFunc(1*time.Second, func() {
			order = append(order, "inner")
		})
	})

	c.Advance(5 * time.Second)

	want := []string{"outer", "inner"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("ordem de disparo = %v, want %v", order, want)
	}

	wantNow := start.Add(5 * time.Second)
	if got := c.Now(); !got.Equal(wantNow) {
		t.Fatalf("apos Advance, Now() = %v, want %v", got, wantNow)
	}
}

// TestFake_ReentrantTimerBeyondWindowWaitsForNextAdvance garante que um
// timer reentrante só dispara quando o seu próprio vencimento é
// alcançado — não antes, mesmo que tenha sido agendado dentro de um
// Advance em curso.
func TestFake_ReentrantTimerBeyondWindowWaitsForNextAdvance(t *testing.T) {
	c := NewFake(start)
	var order []string

	c.AfterFunc(1*time.Second, func() {
		order = append(order, "outer")
		// Vence em 1+10=11s, fora da janela do primeiro Advance(2s).
		c.AfterFunc(10*time.Second, func() {
			order = append(order, "inner")
		})
	})

	c.Advance(2 * time.Second) // now = 2s; inner vence em 11s
	if want := []string{"outer"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("apos primeiro Advance, ordem = %v, want %v", order, want)
	}

	c.Advance(9 * time.Second) // now = 11s; inner deve disparar agora
	if want := []string{"outer", "inner"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("apos segundo Advance, ordem = %v, want %v", order, want)
	}
}

// TestFake_StopBeforeFire cobre Stop() antes do disparo: cancela e o
// callback nunca roda; Stop() é idempotente e retorna false na segunda
// chamada.
func TestFake_StopBeforeFire(t *testing.T) {
	c := NewFake(start)
	fired := false

	timer := c.AfterFunc(1*time.Second, func() { fired = true })

	if ok := timer.Stop(); !ok {
		t.Fatal("Stop() antes do disparo deveria retornar true")
	}

	c.Advance(5 * time.Second)

	if fired {
		t.Fatal("callback nao deveria ter disparado apos Stop()")
	}
	if ok := timer.Stop(); ok {
		t.Fatal("segundo Stop() deveria retornar false")
	}
}

// TestFake_StopAfterFire cobre Stop() depois do disparo: deve retornar
// false, sem efeito colateral.
func TestFake_StopAfterFire(t *testing.T) {
	c := NewFake(start)
	fired := false

	timer := c.AfterFunc(1*time.Second, func() { fired = true })
	c.Advance(1 * time.Second)

	if !fired {
		t.Fatal("callback deveria ter disparado")
	}
	if ok := timer.Stop(); ok {
		t.Fatal("Stop() apos o disparo deveria retornar false")
	}
}

// TestFake_CallbackStopsCompetingTimer simula a ordenação crítica
// citada na ADR-007 (watchdog vs. keyup): um callback disparado durante
// o Advance cancela outro timer ainda pendente na mesma janela, e esse
// outro timer nunca deve disparar.
func TestFake_CallbackStopsCompetingTimer(t *testing.T) {
	c := NewFake(start)
	var order []string

	watchdog := c.AfterFunc(2*time.Second, func() {
		order = append(order, "watchdog")
	})
	c.AfterFunc(1*time.Second, func() {
		order = append(order, "keyup")
		watchdog.Stop()
	})

	c.Advance(5 * time.Second)

	want := []string{"keyup"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("ordem de disparo = %v, want %v", order, want)
	}
}
