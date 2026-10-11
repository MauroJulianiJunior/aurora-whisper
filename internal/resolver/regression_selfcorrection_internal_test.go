package resolver

import "testing"

// TestRegression_SelfCorrection_LeadingNegationNeverEatsClause fixa um
// defeito encontrado por ataque adversarial no F1-C.
//
// Um marcador isolado ("não"/"no") como PRIMEIRA oração não tem nada antes
// para corrigir. A versão anterior disparava assim mesmo e consumia a
// oração seguinte, de forma que "não, tudo bem" virava "tudo bem". Isso não
// é perda de texto: é INVERSÃO do sentido do que a pessoa disse, inserida
// em silêncio no campo com foco. O raw_text continuava correto, então nem o
// histórico denunciaria o problema.
func TestRegression_SelfCorrection_LeadingNegationNeverEatsClause(t *testing.T) {
	cases := []string{
		"não, tudo bem",
		"não, obrigado",
		"não, pode ser",
		"nao, deixa quieto",
		"no, that's fine",
		"no, thank you",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := applySelfCorrection(in)
			if got.Triggered {
				t.Fatalf("applySelfCorrection(%q): Triggered = true (falso positivo) e Text = %q — o sentido da frase foi invertido", in, got.Text)
			}
			if got.Text != in {
				t.Fatalf("applySelfCorrection(%q).Text = %q, want texto intocado", in, got.Text)
			}
		})
	}
}

// TestRegression_SelfCorrection_StillWorksWithPrecedingClause garante que a
// correção acima não matou o recurso: havendo uma oração ANTES do marcador,
// a autocorreção continua disparando normalmente.
func TestRegression_SelfCorrection_StillWorksWithPrecedingClause(t *testing.T) {
	cases := []struct{ in, want string }{
		{"vou amanhã, não, quero dizer terça", "terça"},
		{"I will go monday, no, I mean Tuesday", "Tuesday"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := applySelfCorrection(tc.in)
			if !got.Triggered {
				t.Fatalf("applySelfCorrection(%q): Triggered = false, want true (corrigir a negação inicial não pode desligar o recurso)", tc.in)
			}
			if got.Text != tc.want {
				t.Fatalf("applySelfCorrection(%q).Text = %q, want %q", tc.in, got.Text, tc.want)
			}
		})
	}
}
