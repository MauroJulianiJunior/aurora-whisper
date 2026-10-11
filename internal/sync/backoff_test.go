package syncengine_test

import (
	"testing"
	"time"

	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

func TestBackoffPolicy_Next_GrowsWithAttemptUntilCap(t *testing.T) {
	p := syncengine.BackoffPolicy{Base: time.Second, Max: 32 * time.Second, Rand: func() float64 { return 0 }}

	// Rand=0 -> Next devolve exatamente a metade fixa (o piso da faixa),
	// que cresce monotonicamente até o teto.
	prev := time.Duration(0)
	for attempt := 1; attempt <= 8; attempt++ {
		got := p.Next(attempt)
		if got < prev {
			t.Fatalf("attempt=%d: backoff não pode diminuir (prev=%v, got=%v)", attempt, prev, got)
		}
		prev = got
	}
	if prev != 16*time.Second { // Max/2 com Rand=0, já saturado no teto
		t.Fatalf("esperava saturar em Max/2=16s com Rand=0, achou %v", prev)
	}
}

func TestBackoffPolicy_Next_RespectsMax(t *testing.T) {
	p := syncengine.BackoffPolicy{Base: time.Second, Max: 10 * time.Second, Rand: func() float64 { return 0.999 }}
	for attempt := 1; attempt <= 20; attempt++ {
		got := p.Next(attempt)
		if got > p.Max {
			t.Fatalf("attempt=%d: backoff %v excedeu Max=%v", attempt, got, p.Max)
		}
	}
}

func TestBackoffPolicy_Next_NeverZero(t *testing.T) {
	p := syncengine.BackoffPolicy{Base: time.Second, Max: time.Minute, Rand: func() float64 { return 0 }}
	got := p.Next(1)
	if got <= 0 {
		t.Fatalf("backoff nunca deveria ser <= 0 (evita retry imediato em loop), achou %v", got)
	}
}

func TestBackoffPolicy_Next_HasJitter(t *testing.T) {
	base := time.Second
	max := time.Minute
	pLow := syncengine.BackoffPolicy{Base: base, Max: max, Rand: func() float64 { return 0 }}
	pHigh := syncengine.BackoffPolicy{Base: base, Max: max, Rand: func() float64 { return 0.999 }}

	for attempt := 1; attempt <= 5; attempt++ {
		low := pLow.Next(attempt)
		high := pHigh.Next(attempt)
		if !(high > low) {
			t.Fatalf("attempt=%d: esperava jitter variar o resultado (low=%v, high=%v)", attempt, low, high)
		}
	}
}

func TestBackoffPolicy_Next_DefaultsWhenZeroValue(t *testing.T) {
	var p syncengine.BackoffPolicy // Base/Max zerados -> usa DefaultBackoffPolicy
	got := p.Next(1)
	if got <= 0 || got > syncengine.DefaultBackoffPolicy.Max {
		t.Fatalf("backoff zero value deveria usar DefaultBackoffPolicy, achou %v", got)
	}
}
