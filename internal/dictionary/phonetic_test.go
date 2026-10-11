package dictionary_test

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

func TestPTBRSoundex_Encode_IsDeterministic(t *testing.T) {
	enc := dictionary.PTBRSoundex{}
	a := enc.Encode("Fisco")
	b := enc.Encode("Fisco")
	if a != b {
		t.Fatalf("Encode não determinístico: %q != %q", a, b)
	}
	if a == "" {
		t.Fatalf("Encode(%q) veio vazio", "Fisco")
	}
}

func TestPTBRSoundex_Encode_IgnoresCaseAccentAndSpacing(t *testing.T) {
	enc := dictionary.PTBRSoundex{}
	want := enc.Encode("dctfweb")

	variants := []string{"DCTFWeb", "DCTFWEB", "dctf web", "  dctf-web  "}
	for _, v := range variants {
		if got := enc.Encode(v); got != want {
			t.Fatalf("Encode(%q) = %q, esperava %q (igual a Encode(%q))", v, got, want, "dctfweb")
		}
	}
}

// TestPTBRSoundex_Encode_DistinguishesFiscoFromFisico prova o caso central
// pedido pela spec: "fisco" (autoridade fiscal) e "físico" (adjetivo) soam
// parecido o bastante para o ASR confundir, mas são palavras diferentes —
// o dicionário não pode fundi-las.
func TestPTBRSoundex_Encode_DistinguishesFiscoFromFisico(t *testing.T) {
	enc := dictionary.PTBRSoundex{}
	fisco := enc.Encode("fisco")
	fisico := enc.Encode("físico")

	if fisco == fisico {
		t.Fatalf(`Encode("fisco") e Encode("físico") colidiram em %q; a spec exige que fiquem distintos`, fisco)
	}
}

// TestPTBRSoundex_Encode_MatchesRealNearHomophones documenta o lado bom do
// limiar de generosidade do encoder: quase-homófonos reais (mesmo número
// de sílabas, mesma sequência de grupos fonéticos) colidem, o que é o
// comportamento desejado para sugerir o termo do dicionário.
func TestPTBRSoundex_Encode_MatchesRealNearHomophones(t *testing.T) {
	enc := dictionary.PTBRSoundex{}

	cases := []struct{ a, b string }{
		{"carro", "caro"}, // dígrafo "rr" não modelado, aceitável (quase-homófonos).
		{"dctfweb", "dctf web"},
	}
	for _, c := range cases {
		ea, eb := enc.Encode(c.a), enc.Encode(c.b)
		if ea != eb {
			t.Fatalf("Encode(%q)=%q e Encode(%q)=%q deveriam colidir (quase-homófonos)", c.a, ea, c.b, eb)
		}
	}
}

// TestPTBRSoundex_Encode_KnownLimitation documenta, sem esconder, um limite
// real do encoder: por agrupar "s" e "c" no mesmo dígito (ver
// ptbrSoundexGroup), palavras com sons de fato diferentes em PT-BR podem
// colidir. Isso é aceitável PORQUE Search nunca usa a chave fonética
// sozinha — o filtro de similaridade textual (phoneticSimilarityThreshold)
// contém o dano (ver repository_memory_test.go).
func TestPTBRSoundex_Encode_KnownLimitation(t *testing.T) {
	enc := dictionary.PTBRSoundex{}
	casa := enc.Encode("casa")
	caca := enc.Encode("caça")
	if casa != caca {
		t.Fatalf("limite documentado deixou de valer: Encode(casa)=%q, Encode(caça)=%q; atualize o comentário de PTBRSoundex se isso foi proposital", casa, caca)
	}
}

func TestPTBRSoundex_Encode_EmptyInput(t *testing.T) {
	enc := dictionary.PTBRSoundex{}
	if got := enc.Encode("   !!! "); got != "" {
		t.Fatalf("Encode de string sem letras/dígitos deveria ser vazio, veio %q", got)
	}
}
