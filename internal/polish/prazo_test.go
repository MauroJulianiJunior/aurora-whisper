package polish

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// textoDeTamanho devolve um texto de n caracteres (runas), com acento.
func textoDeTamanho(n int) string { return strings.Repeat("ã", n) }

// TestPrazo_Tabela: 3,5 s até 250 caracteres, 6 ms por caractere acima,
// até 8 s aos 1000 (decisão 5 de docs/defeito-ditado-longo.md). É a mesma
// tabela que o runtime do Agent confere (winruntime, TestPrazoPolish_Tabela):
// o prazo é o mesmo, por tamanho de texto, no PC e no servidor.
func TestPrazo_Tabela(t *testing.T) {
	for _, c := range []struct {
		caracteres int
		want       time.Duration
	}{
		// A fala curta tem os 3,5 s, ao milissegundo.
		{0, 3500 * time.Millisecond},
		{9, 3500 * time.Millisecond},
		{34, 3500 * time.Millisecond},
		{99, 3500 * time.Millisecond},
		{250, 3500 * time.Millisecond},
		{251, 3506 * time.Millisecond},
		{254, 3524 * time.Millisecond},
		// Os longos do incidente de 01-out-2026.
		{359, 4154 * time.Millisecond},
		{802, 6812 * time.Millisecond},
		{907, 7442 * time.Millisecond},
		// O teto: 8 s.
		{1000, 8 * time.Second},
		{1373, 8 * time.Second},
		{50_000, 8 * time.Second},
	} {
		if got := Prazo(textoDeTamanho(c.caracteres)); got != c.want {
			t.Errorf("Prazo(%d caracteres) = %s, quero %s", c.caracteres, got, c.want)
		}
	}
}

// TestPrazo_ContaCaracteresENaoBytes: "ç" são dois bytes. Trezentos deles
// são 300 caracteres (50 acima dos 250), e não 600 bytes.
func TestPrazo_ContaCaracteresENaoBytes(t *testing.T) {
	if got, want := Prazo(strings.Repeat("ç", 300)), 3500*time.Millisecond+300*time.Millisecond; got != want {
		t.Fatalf("Prazo(300 'ç') = %s, quero %s", got, want)
	}
	if got := Prazo(strings.Repeat("ç", 250)); got != PisoPrazo {
		t.Fatalf("Prazo(250 'ç') = %s, quero o piso %s", got, PisoPrazo)
	}
}

// TestPrazo_PisoETetoENuncaDiminui: entre 3,5 s e 8 s em qualquer tamanho,
// e mais texto nunca tem menos prazo.
func TestPrazo_PisoETetoENuncaDiminui(t *testing.T) {
	if PisoPrazo != 3500*time.Millisecond {
		t.Fatalf("PisoPrazo = %s, quero os 3,5 s da ADR-016", PisoPrazo)
	}
	anterior := Prazo("")
	for n := 0; n <= 1500; n++ {
		p := Prazo(textoDeTamanho(n))
		if p < PisoPrazo || p > 8*time.Second {
			t.Fatalf("%d caracteres: %s, fora de [3,5 s, 8 s]", n, p)
		}
		if p < anterior {
			t.Fatalf("%d caracteres: %s, menos que o de %d (%s)", n, p, n-1, anterior)
		}
		anterior = p
	}
}

// TestPrazo_FolgaAteOWatchdogDoNucleo: o prazo do Polish deixa 1,5 s até
// o watchdog da resolução do núcleo (dictation.PrazoDeResolucao), em
// QUALQUER tamanho de texto, porque os dois somam o mesmo acréscimo. Se o
// núcleo mudar o acréscimo, o Polish do servidor muda junto — e este teste,
// como o do runtime do Agent, obriga a decisão a voltar.
func TestPrazo_FolgaAteOWatchdogDoNucleo(t *testing.T) {
	for _, n := range []int{0, 1, 38, 250, 251, 359, 802, 907, 999, 1000, 1001, 1373, 3000, 50_000} {
		texto := textoDeTamanho(n)
		polish, nucleo := Prazo(texto), dictation.PrazoDeResolucao(texto)
		if folga := nucleo - polish; folga != 1500*time.Millisecond {
			t.Errorf("%d caracteres: Polish %s, watchdog %s; folga %s, quero 1,5 s", n, polish, nucleo, folga)
		}
	}
}
