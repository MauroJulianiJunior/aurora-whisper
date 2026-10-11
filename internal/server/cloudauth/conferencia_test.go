package cloudauth

import (
	"strings"
	"testing"
)

// O código de conferência do pareamento (F6): 4 caracteres do alfabeto, sem
// os que se confundem, e todos os 31 aparecem (sem viés grosseiro).
func TestConferencia(t *testing.T) {
	if len(AlfabetoDaConferencia) != 31 {
		t.Fatalf("o alfabeto tem %d símbolos, quero 31", len(AlfabetoDaConferencia))
	}
	for _, c := range "01ILO" {
		if strings.ContainsRune(AlfabetoDaConferencia, c) {
			t.Errorf("o alfabeto tem %q, que se confunde na leitura", c)
		}
	}
	vistos := map[rune]int{}
	for range 2000 {
		c := NovaConferencia()
		if len(c) != TamanhoDaConferencia || !PareceConferencia(c) {
			t.Fatalf("NovaConferencia = %q", c)
		}
		for _, r := range c {
			vistos[r]++
		}
	}
	if len(vistos) != 31 {
		t.Fatalf("só %d símbolos apareceram em 8000 sorteios", len(vistos))
	}
	for r, n := range vistos {
		// Esperado: 8000/31 ≈ 258. Um viés de módulo (256 % 31 = 8) daria
		// ~9% a mais nos 8 primeiros; o teste só pega o grosseiro.
		if n < 150 || n > 380 {
			t.Errorf("%q apareceu %d vezes em 8000", r, n)
		}
	}
	for _, ruim := range []string{"", "K7Q", "K7QMX", "k7qm", "K7Q0", "K7QO", "K7Q1", "K7QI", "K7QL", "K7Q-"} {
		if PareceConferencia(ruim) {
			t.Errorf("PareceConferencia(%q) = true", ruim)
		}
	}
}
