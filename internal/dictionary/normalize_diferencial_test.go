package dictionary

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"
)

// normalizeReferencia é o Normalize de antes do caminho rápido (ADR-021, C2),
// sem mudança de lógica: strings.ToLower na string inteira, depois o accentFold
// e o teste de letra ou dígito, runa a runa.
func normalizeReferencia(s string) string {
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false

	for _, r := range s {
		if folded, ok := accentFold[r]; ok {
			r = folded
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteRune(r)
			continue
		}
		pendingSpace = true
	}

	return b.String()
}

// pedacosDoDiferencial cobre o que o Normalize pode encontrar: ASCII em caixa
// alta e baixa, dígitos, espaços e quebras, pontuação, vogais acentuadas em caixa
// alta e baixa, letras de outros alfabetos (com a caixa que muda de forma:
// Σ, o K do Kelvin, o İ turco), emoji, marcas combinantes, o que o accentFold
// não cobre (ß, ø), números que não são ASCII, e bytes que não são UTF-8.
var pedacosDoDiferencial = []string{
	"a", "b", "z", "A", "B", "Z", "k", "K", "n8n", "N8N", "0", "7", "9",
	" ", " ", " ", "  ", "\t", "\n", "\r\n", "-", "_", ".", ",", "!", "?", "/", "\\", ":", "'", "\"", "(", ")", "+", "#", "@",
	"á", "à", "â", "ã", "ä", "é", "è", "ê", "ë", "í", "ì", "î", "ï", "ó", "ò", "ô", "õ", "ö", "ú", "ù", "û", "ü", "ç", "ñ",
	"Á", "À", "Â", "Ã", "Ä", "É", "È", "Ê", "Ë", "Í", "Ó", "Ô", "Õ", "Ö", "Ú", "Ü", "Ç", "Ñ",
	"ß", "ø", "Ø", "æ", "œ", "ÿ", "ŋ", "ǅ", "ǈ", "ſ",
	"Σ", "σ", "ς", "Ω", "α", "Ж", "ж", "я", "中", "文", "日本", "한",
	"İ", "ı", "K", "Å", "ẞ",
	"٣", "①", "²", "½", "Ⅷ",
	string(rune(0x0301)), string(rune(0x0303)), "e" + string(rune(0x0301)), "a" + string(rune(0x0303)),
	string(rune(0x1F600)), string(rune(0x1F680)), string(rune(0x200b)), string(rune(0x00a0)),
	string(rune(0x2028)), string(rune(0xfeff)), string(rune(0xfffd)),
	"\xff", "\xc3", "\xe2\x82", "\x00", "\x7f", "\x80",
}

func TestNormalize_IgualAoDeAntes(t *testing.T) {
	r := rand.New(rand.NewPCG(10, 21))
	for i := 0; i < 200_000; i++ {
		var b strings.Builder
		for j, n := 0, r.IntN(9); j < n; j++ {
			b.WriteString(pedacosDoDiferencial[r.IntN(len(pedacosDoDiferencial))])
		}
		s := b.String()
		if got, quer := Normalize(s), normalizeReferencia(s); got != quer {
			t.Fatalf("Normalize(%q) = %q, o de antes dava %q", s, got, quer)
		}
	}
}

// Cada pedaço sozinho e cada par de pedaços, para nenhum canto ficar de fora
// do sorteio.
func TestNormalize_IgualAoDeAntes_PedacosEPares(t *testing.T) {
	for _, a := range pedacosDoDiferencial {
		if got, quer := Normalize(a), normalizeReferencia(a); got != quer {
			t.Fatalf("Normalize(%q) = %q, o de antes dava %q", a, got, quer)
		}
		for _, b := range pedacosDoDiferencial {
			s := a + b
			if got, quer := Normalize(s), normalizeReferencia(s); got != quer {
				t.Fatalf("Normalize(%q) = %q, o de antes dava %q", s, got, quer)
			}
			s = " " + a + " " + b + " "
			if got, quer := Normalize(s), normalizeReferencia(s); got != quer {
				t.Fatalf("Normalize(%q) = %q, o de antes dava %q", s, got, quer)
			}
		}
	}
}

// TestNormalize_TodaRunaDeBMP: toda runa válida, sozinha e entre duas letras.
func TestNormalize_TodaRunaValida(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue // substituto: string(r) vira U+FFFD, que o sorteio já cobre
		}
		for _, s := range []string{string(r), "a" + string(r) + "b", string(r) + "A"} {
			if got, quer := Normalize(s), normalizeReferencia(s); got != quer {
				t.Fatalf("Normalize(%q) (U+%04X) = %q, o de antes dava %q", s, r, got, quer)
			}
		}
	}
}

// TestNormalize_JaNormalizadaNaoAloca: o texto que já é a forma normalizada
// volta sem alocar.
func TestNormalize_JaNormalizadaNaoAloca(t *testing.T) {
	for _, s := range []string{"", "n8n", "dctf web", "a b c 1 2", "fisco"} {
		if got := Normalize(s); got != s {
			t.Errorf("Normalize(%q) = %q, quero o mesmo texto", s, got)
		}
		if n := testing.AllocsPerRun(100, func() { _ = Normalize(s) }); n != 0 {
			t.Errorf("Normalize(%q) alocou %v vezes, quero 0", s, n)
		}
	}
	for _, s := range []string{" a", "a ", "a  b", "A", "á", "a-b", "a\tb"} {
		if got := Normalize(s); got == s {
			t.Errorf("Normalize(%q) devolveu o mesmo texto, mas ele não está normalizado", s)
		}
	}
}

func BenchmarkNormalize(b *testing.B) {
	for nome, s := range map[string]string{
		"normalizada":  "dctf web n8n",
		"ascii":        "DCTF-Web N8N!",
		"com acento":   "Metalúrgica Toffanetti & Cia. — Ação",
		"sem alocação": "fisco",
	} {
		b.Run(nome, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = Normalize(s)
			}
		})
	}
}
