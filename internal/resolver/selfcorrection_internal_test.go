package resolver

import "testing"

// Testes de caixa-branca do parser de autocorreção (unexported): cobrem
// os dois lados exigidos pela tarefa — autocorreção real e negação normal
// que NÃO pode ser confundida com correção
// (docs/ai-spec/16-ai-behavior-spec.md, "Self-correction": "testar
// negação normal para evitar falso positivo").
func TestSelfCorrection_TriggersOnMarkedCorrection(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"nao isolado + quero dizer", "não, quero dizer terça", "terça"},
		{"antes + nao isolado + quero dizer", "vou amanhã, não, quero dizer terça", "terça"},
		// Marcadores de prefixo (na verdade/corrigindo/melhor/actually/
		// rather) também substituem a oração anterior por inteiro — mesma
		// regra dos marcadores isolados (não/no), documentada em
		// applySelfCorrection: sem reconciliação sintática fina.
		{"na verdade prefixo", "vou amanhã, na verdade terça", "terça"},
		{"corrigindo prefixo", "vou amanhã, corrigindo, terça", "terça"},
		{"melhor prefixo", "vou amanhã, melhor quarta", "quarta"},
		{"no isolado en", "no, I mean Tuesday", "Tuesday"},
		{"actually en", "I will go monday, actually tuesday", "tuesday"},
		{"rather en", "book a table, rather cancel it", "cancel it"},
		{"sem acento nao", "nao, quero dizer terca", "terca"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applySelfCorrection(tc.in)
			if !got.Triggered {
				t.Fatalf("applySelfCorrection(%q): Triggered = false, want true", tc.in)
			}
			if got.Text != tc.want {
				t.Fatalf("applySelfCorrection(%q).Text = %q, want %q", tc.in, got.Text, tc.want)
			}
			if got.Applied.Reason != ReasonSelfCorrection {
				t.Fatalf("Applied.Reason = %q, want %q", got.Applied.Reason, ReasonSelfCorrection)
			}
		})
	}
}

// TestSelfCorrection_NormalNegationNeverTriggers é o teste central pedido
// pela tarefa: "não quero ir amanhã" é uma frase comum, não uma correção
// — "não" está fundido na mesma oração que o resto da frase, nunca
// isolado por vírgula.
func TestSelfCorrection_NormalNegationNeverTriggers(t *testing.T) {
	cases := []string{
		"não quero ir amanhã",
		"eu não sei",
		"no thanks",
		"I do not want that",
		"melhor não ir",       // "melhor" fundido com o resto, não é oração própria.
		"não sei, vou amanhã", // "não sei" não é oração-marcador exata.
		"na verdade eu gosto disso",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := applySelfCorrection(in)
			if got.Triggered {
				t.Fatalf("applySelfCorrection(%q): Triggered = true (falso positivo!), Text = %q", in, got.Text)
			}
			if got.Text != in {
				t.Fatalf("applySelfCorrection(%q).Text = %q, want texto intocado", in, got.Text)
			}
		})
	}
}

func TestSelfCorrection_NoCommaNeverTriggers(t *testing.T) {
	got := applySelfCorrection("não quero ir amanhã de jeito nenhum")
	if got.Triggered {
		t.Fatal("Triggered = true sem nenhuma vírgula no texto")
	}
}
