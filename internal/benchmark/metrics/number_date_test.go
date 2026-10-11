package metrics

import "testing"

func TestNumberDateAccuracy_ThousandsSeparatorIsEquivalent(t *testing.T) {
	r := NumberDateAccuracy("o valor foi 1500 reais", "o valor foi 1.500 reais")
	if r.Correct != 1 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 1/1 ('1500' == '1.500')", r.Correct, r.Total)
	}
}

func TestNumberDateAccuracy_DifferentNumbersDoNotMatch(t *testing.T) {
	r := NumberDateAccuracy("o valor foi 1500 reais", "o valor foi 1050 reais")
	if r.Correct != 0 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 0/1 ('1500' != '1050')", r.Correct, r.Total)
	}
}

func TestNumberDateAccuracy_DecimalCommaEquivalence(t *testing.T) {
	r := NumberDateAccuracy("total de 1.500,50", "total de 1500.50")
	if r.Correct != 1 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 1/1 (1.500,50 == 1500.50)", r.Correct, r.Total)
	}
}

func TestNumberDateAccuracy_DateDifferentFormats(t *testing.T) {
	r := NumberDateAccuracy("vence em 17/09/2026", "vence em 2026-09-17")
	if r.Correct != 1 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 1/1 (mesma data, formatos distintos)", r.Correct, r.Total)
	}
}

func TestNumberDateAccuracy_DateDoesNotDoubleCountAsNumbers(t *testing.T) {
	// a data "17/09/2026" nao deve tambem contar como 3 numeros soltos
	// (17, 09, 2026) alem de contar como 1 data.
	r := NumberDateAccuracy("vence em 17/09/2026", "vence em 17/09/2026")
	if r.Total != 1 {
		t.Fatalf("Total = %d, want 1 (so a data, nao 3 numeros + 1 data)", r.Total)
	}
	if r.Correct != 1 {
		t.Fatalf("Correct = %d, want 1", r.Correct)
	}
}

func TestNumberDateAccuracy_DifferentDatesDoNotMatch(t *testing.T) {
	r := NumberDateAccuracy("vence em 17/09/2026", "vence em 18/09/2026")
	if r.Correct != 0 || r.Total != 1 {
		t.Fatalf("got Correct=%d Total=%d, want 0/1", r.Correct, r.Total)
	}
}

func TestNumberDateAccuracy_RepeatedNumberRequiresRepeatedMatch(t *testing.T) {
	r := NumberDateAccuracy("1500 e depois mais 1500", "1500 e depois mais 2000")
	if r.Total != 2 {
		t.Fatalf("Total = %d, want 2", r.Total)
	}
	if r.Correct != 1 {
		t.Fatalf("Correct = %d, want 1 (so uma das duas ocorrencias de 1500 bateu)", r.Correct)
	}
}

func TestNumberDateAccuracy_NoNumbersIsVacuouslyPerfect(t *testing.T) {
	r := NumberDateAccuracy("nada numerico aqui", "tambem nada aqui")
	if r.Total != 0 {
		t.Fatalf("Total = %d, want 0", r.Total)
	}
	if !almostEqual(r.Score(), 1) {
		t.Fatalf("Score = %v, want 1", r.Score())
	}
}

func TestCanonicalizeNumber_Table(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"1500", "1500"},
		{"1.500", "1500"},
		{"12.345.678", "12345678"},
		{"1.500,50", "1500.5"},
		{"1500,50", "1500.5"},
		{"1500.50", "1500.5"}, // um unico ponto com 2 casas finais: decimal
		{"3.5", "3.5"},
		{"0050", "50"},
		{"1500,00", "1500"}, // zero fracionario e removido
	}
	for _, c := range cases {
		got := canonicalizeNumber(c.raw)
		if got != c.want {
			t.Errorf("canonicalizeNumber(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestExtract_DigitoDentroDePalavraNaoEhNumero trava o defeito achado na
// primeira rodada real do benchmark: a referência "O workflow do n8n está
// travando no nó de webhook" produzia o item `num:8`, transformando o nome
// de uma ferramenta em número fantasma na métrica de Número/Data.
//
// O efeito é pior do que parece. Com "n8n" nos dois lados, o acerto
// espúrio INFLA a nota com algo que não é número; com o ASR escrevendo
// "N 8 N", vira erro espúrio. Os dois medem grafia de nome próprio dentro
// da métrica errada — Entity Accuracy já cobre isso.
func TestExtract_DigitoDentroDePalavraNaoEhNumero(t *testing.T) {
	casos := []struct {
		texto string
		want  []string
	}{
		{"O workflow do n8n está travando no nó de webhook.", nil},
		{"versão 3.5 do n8n com 12 itens", []string{"num:3.5", "num:12"}},
		// Prefixo de moeda NÃO pode ser confundido com letra colada: o
		// caractere antes do dígito é "$", e o número tem de sair.
		{"O valor é de R$1.530,40", []string{"num:1530.4"}},
		{"O valor é de R$ 1.530,40 e vence em 03/10/2026.", []string{"date:2026-10-03", "num:1530.4"}},
	}
	for _, c := range casos {
		got := extractNumbersAndDates(c.texto)
		if len(got) != len(c.want) {
			t.Errorf("extractNumbersAndDates(%q) = %q (%d itens), want %q (%d)",
				c.texto, got, len(got), c.want, len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("extractNumbersAndDates(%q)[%d] = %q, want %q", c.texto, i, got[i], c.want[i])
			}
		}
	}
}
