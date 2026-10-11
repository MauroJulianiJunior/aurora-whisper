package learning

import (
	"strings"
	"testing"
)

// TestRegression_NumberToNumberNeverBecomesAlias fixa um defeito encontrado
// por ataque adversarial no F1-E.
//
// Classify("5", "50") devolvia AliasEligible=true. Confirmada, essa edição
// viraria um alias de dicionário — e alias é aplicado a TODO ditado futuro.
// A pessoa falaria "5" em qualquer contexto e receberia "50". É violação
// direta do "proibido mudar números sem evidência"
// (docs/ai-spec/16-ai-behavior-spec.md).
//
// A edição continua podendo virar uma Correction: ela é um fato histórico
// sobre aquele ditado. O que fica barrado é a promoção a alias.
func TestRegression_NumberToNumberNeverBecomesAlias(t *testing.T) {
	cases := []struct{ before, after string }{
		{"5", "50"},
		{"50", "5"},
		{"1500", "1.500"},
		{"2026", "2027"},
		{"vinte", "20"},
		{"20", "vinte"},
		{"10/08", "10/09"},
		{"3,5", "35"},
	}
	for _, tc := range cases {
		t.Run(tc.before+" -> "+tc.after, func(t *testing.T) {
			s, ok := Classify(tc.before, tc.after)
			if ok && s.AliasEligible {
				t.Fatalf("Classify(%q, %q).AliasEligible = true — o dicionário passaria a aplicar essa troca em todo ditado futuro (reason: %s)",
					tc.before, tc.after, s.Reason)
			}
		})
	}
}

// TestRegression_NumberGuardDoesNotBreakTEST008 garante que a proteção
// acima não matou o caso que a spec exige. Em "n oito n" -> "n8n" nenhum
// dos dois lados é puramente numérico, então continua elegível a alias.
func TestRegression_NumberGuardDoesNotBreakTEST008(t *testing.T) {
	s, ok := Classify("n oito n", "n8n")
	if !ok {
		t.Fatal("Classify devolveu ok=false para o caso do TEST-008")
	}
	if !s.AliasEligible {
		t.Fatalf("AliasEligible = false para \"n oito n\" -> \"n8n\": a proteção contra números quebrou o TEST-008 (reason: %s)", s.Reason)
	}
}

// TestRegression_AliasThresholds_Boundaries trava os limites numéricos de
// "pequena e mapeável" nas duas bordas, uma dimensão de cada vez.
//
// Sem isto, afrouxar maxAliasWords de 4 para 7, ou maxAliasChars de 30 para
// 50, passaria por todos os testes existentes — eles usavam textos muito
// acima do limiar, que não provam onde a linha está.
func TestRegression_AliasThresholds_Boundaries(t *testing.T) {
	cases := []struct {
		name         string
		before       string
		after        string
		wantEligible bool
	}{
		{
			name:         "exatamente no teto de caracteres",
			before:       strings.Repeat("a", maxAliasChars),
			after:        strings.Repeat("b", maxAliasChars),
			wantEligible: true,
		},
		{
			name:         "um caractere acima do teto",
			before:       strings.Repeat("a", maxAliasChars+1),
			after:        strings.Repeat("b", maxAliasChars+1),
			wantEligible: false,
		},
		{
			name:         "exatamente no teto de palavras",
			before:       strings.TrimSpace(strings.Repeat("aa ", maxAliasWords)),
			after:        strings.TrimSpace(strings.Repeat("bb ", maxAliasWords)),
			wantEligible: true,
		},
		{
			name:         "uma palavra acima do teto",
			before:       strings.TrimSpace(strings.Repeat("aa ", maxAliasWords+1)),
			after:        strings.TrimSpace(strings.Repeat("bb ", maxAliasWords+1)),
			wantEligible: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ok := Classify(tc.before, tc.after)
			if !ok {
				t.Fatalf("Classify(%q, %q) devolveu ok=false", tc.before, tc.after)
			}
			if s.AliasEligible != tc.wantEligible {
				t.Fatalf("AliasEligible = %v, want %v (before=%d runes/%d palavras; reason: %s)",
					s.AliasEligible, tc.wantEligible,
					len([]rune(tc.before)), len(strings.Fields(tc.before)), s.Reason)
			}
		})
	}
}
