package update

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// O erro clássico é comparar como string: "0.9.0" > "0.10.0"
		// lexicograficamente, mas semanticamente 0.10.0 é maior.
		{"0.10.0", "0.9.0", 1},
		{"0.9.0", "0.10.0", -1},
		{"0.9.0", "0.9.0", 0},

		// Release sempre > pré-release da mesma versão base.
		{"0.9.0", "0.9.0-rc1", 1},
		{"0.9.0-rc1", "0.9.0", -1},

		// Entre pré-releases, identificador a identificador.
		{"0.9.0-rc1", "0.9.0-rc2", -1},
		{"0.9.0-rc2", "0.9.0-rc1", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha", 1},

		// "v" opcional na frente.
		{"v1.2.3", "1.2.3", 0},
	}
	for _, c := range cases {
		got, err := Compare(c.a, c.b)
		if err != nil {
			t.Fatalf("Compare(%q, %q) erro inesperado: %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCompare_InvalidVersion(t *testing.T) {
	if _, err := Compare("nao-e-versao", "1.0.0"); err == nil {
		t.Fatal("esperava erro para versão local inválida")
	}
	if _, err := Compare("1.0.0", "nao-e-versao"); err == nil {
		t.Fatal("esperava erro para versão remota inválida")
	}
	if _, err := Compare("1.2", "1.2.0"); err == nil {
		t.Fatal("esperava erro para versão sem PATCH")
	}
	if _, err := Compare("1.2.x", "1.2.0"); err == nil {
		t.Fatal("esperava erro para componente não numérico")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"0.9.0", "0.10.0", true},
		{"0.9.0", "0.9.0", false},  // igual -> sem update
		{"0.10.0", "0.9.0", false}, // servidor manda versão MENOR -> sem update, sem downgrade sozinho
		{"0.9.0-rc1", "0.9.0", true},
		{"0.9.0", "0.9.0-rc1", false},
	}
	for _, c := range cases {
		got, err := IsNewer(c.current, c.candidate)
		if err != nil {
			t.Fatalf("IsNewer(%q, %q) erro inesperado: %v", c.current, c.candidate, err)
		}
		if got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

func TestIsNewer_InvalidCandidate(t *testing.T) {
	if _, err := IsNewer("1.0.0", "versao-invalida"); err == nil {
		t.Fatal("esperava erro para versão candidata inválida")
	}
}
