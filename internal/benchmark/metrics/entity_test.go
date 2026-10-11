package metrics

import "testing"

func TestEntityAccuracy_ExactMatch(t *testing.T) {
	r := EntityAccuracy([]string{"DCTFWeb", "n8n", "Fisco"},
		"hoje o Fisco cobrou o DCTFWeb, e o n8n disparou o alerta")
	if r.Correct != 3 || r.Total != 3 {
		t.Fatalf("got Correct=%d Total=%d, want 3/3", r.Correct, r.Total)
	}
	if !almostEqual(r.Score(), 1) {
		t.Fatalf("Score = %v, want 1", r.Score())
	}
}

func TestEntityAccuracy_CaseInsensitiveMatch(t *testing.T) {
	r := EntityAccuracy([]string{"DCTFWeb"}, "abriu o dctfweb agora")
	if r.Correct != 1 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 1/1 (case-insensitive)", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_SpaceInsideEntityIsNotAMatch(t *testing.T) {
	// "n 8 n" tokeniza em 3 tokens ("n","8","n") e NAO conta como acerto
	// para a entidade "n8n" (1 token).
	r := EntityAccuracy([]string{"n8n"}, "o n 8 n nao respondeu")
	if r.Correct != 0 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 0/1 ('n 8 n' nao e 'n8n')", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_MissingEntity(t *testing.T) {
	r := EntityAccuracy([]string{"DCTFWeb"}, "hoje nao abri nada")
	if r.Correct != 0 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 0/1", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_RepeatedEntity_BothFound(t *testing.T) {
	r := EntityAccuracy([]string{"Fisco", "Fisco"}, "o Fisco ligou, e o Fisco cobrou de novo")
	if r.Correct != 2 || r.Total != 2 {
		t.Fatalf("got Correct=%d Total=%d, want 2/2", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_RepeatedEntity_OnlyOneFound(t *testing.T) {
	// esperado 2x, hipotese so contem 1x: 1 acerto de 2, nao 2 de 2.
	r := EntityAccuracy([]string{"Fisco", "Fisco"}, "o Fisco ligou hoje de manha")
	if r.Correct != 1 || r.Total != 2 {
		t.Fatalf("got Correct=%d Total=%d, want 1/2", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_RepeatedInHypothesisDoesNotOverCount(t *testing.T) {
	// esperado 1x, hipotese contem 2x: ainda e 1 de 1 (nao pode passar de
	// Total).
	r := EntityAccuracy([]string{"Fisco"}, "Fisco, Fisco, Fisco")
	if r.Correct != 1 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 1/1 (nao ultrapassa Total)", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_AccentSensitive(t *testing.T) {
	r := EntityAccuracy([]string{"café"}, "tomou um cafe")
	if r.Correct != 0 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 0/1 (acento importa: 'café' != 'cafe')", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_DigitSensitive(t *testing.T) {
	r := EntityAccuracy([]string{"n8n"}, "abriu o n9n por engano")
	if r.Correct != 0 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 0/1 (digito importa: 'n8n' != 'n9n')", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_MultiWordEntity(t *testing.T) {
	r := EntityAccuracy([]string{"Simples Nacional"}, "ele optou pelo simples nacional este ano")
	if r.Correct != 1 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 1/1 (entidade de 2 tokens)", r.Correct, r.Total)
	}
}

func TestEntityAccuracy_EmptyExpectedListIsVacuouslyPerfect(t *testing.T) {
	r := EntityAccuracy(nil, "qualquer coisa aqui")
	if r.Total != 0 {
		t.Fatalf("Total = %d, want 0", r.Total)
	}
	if !almostEqual(r.Score(), 1) {
		t.Fatalf("Score = %v, want 1 (convencao documentada para Total=0)", r.Score())
	}
}
