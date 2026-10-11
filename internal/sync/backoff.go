package syncengine

import (
	"math/rand"
	"time"
)

// BackoffPolicy calcula a espera do laço de sync depois de uma rodada que
// falhou (a rede, o prazo, um 5xx), com crescimento exponencial e jitter:
// evita que todos os aparelhos de um usuário tentem de novo no mesmo instante
// quando a rede volta.
type BackoffPolicy struct {
	// Base é a espera da primeira falha (attempt == 1). <= 0 usa
	// DefaultBackoffPolicy.Base.
	Base time.Duration

	// Max é o teto da espera, antes do jitter. <= 0 usa
	// DefaultBackoffPolicy.Max.
	Max time.Duration

	// Rand devolve um float64 em [0, 1). nil usa rand.Float64.
	// Injetável para testes determinísticos.
	Rand func() float64
}

// DefaultBackoffPolicy é a espera do laço de sync (ADR-019, F7b): de 2 s a 5
// min.
var DefaultBackoffPolicy = BackoffPolicy{Base: 2 * time.Second, Max: 5 * time.Minute}

// Next devolve a espera para a falha número attempt (attempt >= 1). O
// crescimento é exponencial (base * 2^(attempt-1)) até Max, e o valor final
// aplica "equal jitter": metade fixa + metade aleatória — cresce com attempt,
// nunca é zero, e nunca é o mesmo valor exato duas vezes seguidas.
func (b BackoffPolicy) Next(attempt int) time.Duration {
	base := b.Base
	if base <= 0 {
		base = DefaultBackoffPolicy.Base
	}
	max := b.Max
	if max <= 0 {
		max = DefaultBackoffPolicy.Max
	}
	if attempt < 1 {
		attempt = 1
	}

	d := base
	for i := 1; i < attempt; i++ {
		if d >= max {
			d = max
			break
		}
		d *= 2
		if d <= 0 || d > max { // overflow ou estourou o teto
			d = max
			break
		}
	}
	if d > max {
		d = max
	}
	if d < 0 {
		d = max
	}

	half := d / 2
	return half + time.Duration(b.rand()*float64(half))
}

func (b BackoffPolicy) rand() float64 {
	if b.Rand == nil {
		return rand.Float64()
	}
	return b.Rand()
}
