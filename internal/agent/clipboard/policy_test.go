package clipboard

import (
	"errors"
	"testing"
	"time"
)

// recordingSleeper é o dublê de sleep usado em todo este arquivo: nunca
// espera tempo real, só registra por quanto tempo foi "chamado" para
// esperar.
type recordingSleeper struct {
	calls []time.Duration
}

func (s *recordingSleeper) sleep(d time.Duration) {
	s.calls = append(s.calls, d)
}

func TestOpenWithRetry_SucessoNaPrimeiraTentativa(t *testing.T) {
	sleeper := &recordingSleeper{}
	calls := 0
	open := func() error {
		calls++
		return nil
	}

	if err := openWithRetry(open, DefaultRetryPolicy(), sleeper.sleep); err != nil {
		t.Fatalf("openWithRetry() = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("open chamado %d vezes, want 1", calls)
	}
	if len(sleeper.calls) != 0 {
		t.Fatalf("sleep chamado %d vezes, want 0 (nunca deveria esperar após sucesso imediato)", len(sleeper.calls))
	}
}

func TestOpenWithRetry_SucessoAposFalhas(t *testing.T) {
	sleeper := &recordingSleeper{}
	attempts := 0
	open := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("ocupado por outro processo")
		}
		return nil
	}

	policy := RetryPolicy{
		MaxAttempts: 5,
		Backoff:     func(attempt int) time.Duration { return time.Duration(attempt) * time.Millisecond },
	}

	if err := openWithRetry(open, policy, sleeper.sleep); err != nil {
		t.Fatalf("openWithRetry() = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("open chamado %d vezes, want 3", attempts)
	}
	// Espera entre as tentativas 1->2 e 2->3, nunca depois da que teve
	// sucesso.
	want := []time.Duration{1 * time.Millisecond, 2 * time.Millisecond}
	if len(sleeper.calls) != len(want) {
		t.Fatalf("sleep chamado %d vezes = %v, want %v", len(sleeper.calls), sleeper.calls, want)
	}
	for i := range want {
		if sleeper.calls[i] != want[i] {
			t.Fatalf("sleep[%d] = %v, want %v", i, sleeper.calls[i], want[i])
		}
	}
}

func TestOpenWithRetry_EsgotaTentativasSemLacoInfinito(t *testing.T) {
	sleeper := &recordingSleeper{}
	attempts := 0
	sentinel := errors.New("sempre ocupado")
	open := func() error {
		attempts++
		return sentinel
	}

	policy := RetryPolicy{
		MaxAttempts: 4,
		Backoff:     func(attempt int) time.Duration { return time.Millisecond },
	}

	err := openWithRetry(open, policy, sleeper.sleep)
	if err == nil {
		t.Fatal("openWithRetry() = nil, want erro após esgotar tentativas")
	}
	if !errors.Is(err, ErrOpenTimeout) {
		t.Fatalf("openWithRetry() = %v, want errors.Is ErrOpenTimeout", err)
	}
	if attempts != 4 {
		t.Fatalf("open chamado %d vezes, want exatamente 4 (MaxAttempts) — sem laço infinito", attempts)
	}
	// Nunca espera depois da última tentativa.
	if len(sleeper.calls) != 3 {
		t.Fatalf("sleep chamado %d vezes, want 3 (uma a menos que MaxAttempts)", len(sleeper.calls))
	}
}

func TestOpenWithRetry_MaxAttemptsMenorQueUmViraUmaTentativa(t *testing.T) {
	attempts := 0
	open := func() error {
		attempts++
		return errors.New("falha")
	}

	policy := RetryPolicy{MaxAttempts: 0}
	if err := openWithRetry(open, policy, nil); err == nil {
		t.Fatal("openWithRetry() = nil, want erro")
	}
	if attempts != 1 {
		t.Fatalf("open chamado %d vezes, want 1 (MaxAttempts<1 vira 1 tentativa, não 0 nem infinito)", attempts)
	}
}

func TestOpenWithRetry_SemBackoffNemSleepNaoEntraEmPanico(t *testing.T) {
	attempts := 0
	open := func() error {
		attempts++
		return errors.New("falha")
	}
	policy := RetryPolicy{MaxAttempts: 3, Backoff: nil}

	if err := openWithRetry(open, policy, nil); err == nil {
		t.Fatal("openWithRetry() = nil, want erro")
	}
	if attempts != 3 {
		t.Fatalf("open chamado %d vezes, want 3", attempts)
	}
}

func TestDefaultRetryPolicy_BackoffLimitado(t *testing.T) {
	policy := DefaultRetryPolicy()
	if policy.MaxAttempts < 1 {
		t.Fatalf("DefaultRetryPolicy().MaxAttempts = %d, want >= 1", policy.MaxAttempts)
	}
	if policy.Backoff == nil {
		t.Fatal("DefaultRetryPolicy().Backoff = nil, want função")
	}
	const cap = 100 * time.Millisecond
	for attempt := 1; attempt <= 50; attempt++ {
		d := policy.Backoff(attempt)
		if d < 0 {
			t.Fatalf("Backoff(%d) = %v, want >= 0", attempt, d)
		}
		if d > cap {
			t.Fatalf("Backoff(%d) = %v, want <= %v (teto documentado)", attempt, d, cap)
		}
	}
}

// TestShouldRestore cobre a política por CONTEÚDO que substituiu a
// comparação por número de sequência. O caso decisivo é o primeiro: numa
// máquina real, o Histórico da Área de Transferência do Windows reescreve
// o clipboard logo depois da nossa escrita. Pela sequência, isso parecia
// mudança externa e o restore era sempre pulado — o AC-009 nunca
// acontecia, em silêncio. Pelo conteúdo, o texto continua sendo o nosso e
// restaurar é seguro.
func TestShouldRestore(t *testing.T) {
	tests := []struct {
		name        string
		currentText string
		ourText     string
		readOK      bool
		want        bool
	}{
		{
			name:        "conteúdo ainda é o nosso — seguro restaurar",
			currentText: "texto ditado", ourText: "texto ditado", readOK: true, want: true,
		},
		{
			name:        "a pessoa copiou outra coisa — não restaurar por cima",
			currentText: "algo que ela copiou depois", ourText: "texto ditado", readOK: true, want: false,
		},
		{
			name:        "não conseguimos ler — restaura assim mesmo",
			currentText: "", ourText: "texto ditado", readOK: false, want: true,
		},
		{
			name:        "clipboard esvaziado por terceiro conta como mudança",
			currentText: "", ourText: "texto ditado", readOK: true, want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRestore(tt.currentText, tt.ourText, tt.readOK); got != tt.want {
				t.Fatalf("shouldRestore(%q, %q, readOK=%v) = %v, want %v",
					tt.currentText, tt.ourText, tt.readOK, got, tt.want)
			}
		})
	}
}
