package dictionary_test

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"maiusculas", "DCTFWeb", "dctfweb"},
		{"acento agudo", "Físico", "fisico"},
		{"acento circunflexo", "vocês", "voces"},
		{"til", "não", "nao"},
		{"cedilha", "coração", "coracao"},
		{"pontuacao removida", "Fisco!", "fisco"},
		{"hifen vira separador", "dctf-web", "dctf web"},
		{"espacos nas bordas e duplicados", "  n8n   pro  ", "n8n pro"},
		{"underline vira separador", "termo_composto", "termo composto"},
		{"numeros preservados", "n8n", "n8n"},
		{"string vazia", "", ""},
		{"so pontuacao", "!!!", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dictionary.Normalize(c.in); got != c.want {
				t.Fatalf("Normalize(%q) = %q, esperava %q", c.in, got, c.want)
			}
		})
	}
}

func TestNormalize_IsIdempotent(t *testing.T) {
	in := "  DCTF-Web!! "
	once := dictionary.Normalize(in)
	twice := dictionary.Normalize(once)
	if once != twice {
		t.Fatalf("Normalize não é idempotente: %q -> %q -> %q", in, once, twice)
	}
}

func TestCollapseSpaces(t *testing.T) {
	dctfWeb := dictionary.Normalize("dctf web")
	dctfweb := dictionary.Normalize("dctfweb")

	if dctfWeb == dctfweb {
		t.Fatalf("normalized forms já eram iguais (%q) — teste não estaria provando nada", dctfWeb)
	}
	if dictionary.CollapseSpaces(dctfWeb) != dictionary.CollapseSpaces(dctfweb) {
		t.Fatalf("CollapseSpaces(%q)=%q != CollapseSpaces(%q)=%q, deveriam casar",
			dctfWeb, dictionary.CollapseSpaces(dctfWeb), dctfweb, dictionary.CollapseSpaces(dctfweb))
	}
}
