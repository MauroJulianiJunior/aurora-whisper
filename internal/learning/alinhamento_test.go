package learning

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

func TestTokenizar(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ...  ", nil},
		{"Recíbo,", []string{"recibo"}},
		{"n8n está no ar", []string{"n8n", "esta", "no", "ar"}},
		{"EFD-Reinf", []string{"efd", "reinf"}},
		{"Node.js", []string{"node", "js"}},
		{"Primeiro.\n\nSegundo!", []string{"primeiro", "segundo"}},
		{"R$ 1.500,00", []string{"r", "1", "500", "00"}},
		{"ÇÃO", []string{"cao"}},
	}
	for _, tc := range cases {
		got := chaves(tokenizar(tc.in))
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("tokenizar(%q) = %q, quer %q", tc.in, got, tc.want)
		}
	}
}

// Os deslocamentos devolvem o texto como estava escrito, com a pontuação de
// dentro e sem a das pontas.
func TestFatia(t *testing.T) {
	texto := "Usei o EFD-Reinf, hoje."
	ts := tokenizar(texto)
	if got := fatia(ts, texto, 2, 4); got != "EFD-Reinf" {
		t.Errorf("fatia = %q", got)
	}
	if got := fatia(ts, texto, 0, len(ts)); got != "Usei o EFD-Reinf, hoje" {
		t.Errorf("fatia toda = %q", got)
	}
	if got := compacta(" a \n\t b  "); got != "a b" {
		t.Errorf("compacta = %q", got)
	}
}

// lcsBruto é o tamanho da maior subsequência comum, por recursão simples,
// para comparar com o pareamento.
func lcsBruto(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if a[0] == b[0] {
		return 1 + lcsBruto(a[1:], b[1:])
	}
	x, y := lcsBruto(a[1:], b), lcsBruto(a, b[1:])
	if x > y {
		return x
	}
	return y
}

// TestPareamento_Propriedade: em sequências aleatórias pequenas, os pares
// casam palavras iguais, em ordem estritamente crescente dos dois lados, e
// são o máximo possível.
func TestPareamento_Propriedade(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alfabeto := []string{"a", "b", "c", "d"}
	sorteia := func() []string {
		out := make([]string, rng.Intn(9))
		for i := range out {
			out[i] = alfabeto[rng.Intn(len(alfabeto))]
		}
		return out
	}
	for n := 0; n < 3000; n++ {
		a, b := sorteia(), sorteia()
		pares, ok := pareamento(a, b)
		if !ok {
			t.Fatalf("pareamento(%v, %v) recusou uma entrada pequena", a, b)
		}
		ia, ib := -1, -1
		for _, p := range pares {
			if p[0] <= ia || p[1] <= ib {
				t.Fatalf("pareamento(%v, %v): pares fora de ordem: %v", a, b, pares)
			}
			if a[p[0]] != b[p[1]] {
				t.Fatalf("pareamento(%v, %v): par %v junta palavras diferentes", a, b, p)
			}
			ia, ib = p[0], p[1]
		}
		if want := lcsBruto(a, b); len(pares) != want {
			t.Fatalf("pareamento(%v, %v) achou %d pares, o máximo é %d", a, b, len(pares), want)
		}

		// Os trechos e os pares cobrem as duas sequências, sem sobra nem
		// sobreposição.
		ts := trechos(pares, len(a), len(b))
		cobreA, cobreB := len(pares), len(pares)
		for _, tr := range ts {
			cobreA += tr.a2 - tr.a1
			cobreB += tr.b2 - tr.b1
			if tr.a2 == tr.a1 && tr.b2 == tr.b1 {
				t.Fatalf("trecho vazio em %v, %v", a, b)
			}
		}
		if cobreA != len(a) || cobreB != len(b) {
			t.Fatalf("trechos de (%v, %v) cobrem %d e %d, quer %d e %d", a, b, cobreA, cobreB, len(a), len(b))
		}
	}
}

func TestPareamento_Teto(t *testing.T) {
	longa := strings.Fields(strings.Repeat("x ", 3000))
	outra := strings.Fields(strings.Repeat("y ", 3000))
	if _, ok := pareamento(longa, outra); ok {
		t.Error("9 milhões de células deveriam passar do teto")
	}
	// Prefixo e sufixo iguais saem antes da tabela: o miolo é que conta.
	a := append(append(append([]string{}, longa...), "meio"), longa...)
	b := append(append(append([]string{}, longa...), "outro"), longa...)
	pares, ok := pareamento(a, b)
	if !ok || len(pares) != 6000 {
		t.Errorf("miolo de 1x1: ok=%v, %d pares", ok, len(pares))
	}
}

// O teto é sobre (n+1)*(m+1) células: 1999 de cada lado ainda cabe, 2000 não.
func TestPareamento_TetoExato(t *testing.T) {
	mk := func(n int, w string) []string { return strings.Fields(strings.Repeat(w+" ", n)) }
	if _, ok := pareamento(mk(1999, "x"), mk(1999, "y")); !ok {
		t.Error("1999 x 1999 (4.000.000 células) deveria caber")
	}
	if _, ok := pareamento(mk(2000, "x"), mk(2000, "y")); ok {
		t.Error("2000 x 2000 (4.004.001 células) deveria passar do teto")
	}
}

// Toda palavra de ligação barra a troca, dos dois lados. A lista de diff.go
// vem da fonte; as que este arquivo acrescentou vão listadas aqui, porque a
// conferência é justamente que cada uma está na lista.
func TestLigacao_TodaPalavraBarra(t *testing.T) {
	var todas []string
	for k := range ligacao {
		todas = append(todas, k)
	}
	todas = append(todas, "por", "para", "pra", "com", "sem", "sobre", "até", "entre",
		"e", "ou", "que", "se", "mas", "nem")

	for _, w := range todas {
		// A palavra de ligação é o De, e depois o Para.
		for _, tc := range []struct{ bruto, antes, depois string }{
			{"vi " + w + " casa", "Vi " + w + " casa.", "Vi zzz casa."},
			{"vi zzz casa", "Vi zzz casa.", "Vi " + w + " casa."},
		} {
			r := Sugerir(tc.bruto, tc.antes, tc.depois, dictionary.Semente{}, nil)
			if len(r.Sugestoes) != 0 || len(r.Descartes) != 1 || r.Descartes[0] != MotivoPalavraDeLigacao {
				t.Errorf("%q -> %q: sugestões %v, descartes %v; quer barrado por palavra de ligação", tc.antes, tc.depois, r.Sugestoes, r.Descartes)
			}
		}
	}
}

// origens: cada palavra do final aponta para o trecho do bruto de onde veio.
func TestOrigens(t *testing.T) {
	bruto := []string{"abri", "o", "whisper", "flow", "agora"}
	final := []string{"abri", "o", "wispr", "flow", "agora"}
	pares, _ := pareamento(bruto, final)
	got := origens(pares, len(bruto), len(final))
	want := []origem{
		{0, 1, true}, {1, 2, true}, {2, 3, false}, {3, 4, true}, {4, 5, true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("origens = %+v, quer %+v", got, want)
	}

	// Uma palavra que substitui duas: as duas do bruto, para a mesma lacuna.
	bruto = []string{"uso", "n", "oito", "n", "hoje"}
	final = []string{"uso", "n8n", "hoje"}
	pares, _ = pareamento(bruto, final)
	got = origens(pares, len(bruto), len(final))
	want = []origem{{0, 1, true}, {1, 4, false}, {4, 5, true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("origens (2 por 1) = %+v, quer %+v", got, want)
	}

	// Palavra que o final tem e o bruto não: lacuna vazia no bruto.
	got = origens(mustPares(t, []string{"a", "c"}, []string{"a", "b", "c"}), 2, 3)
	want = []origem{{0, 1, true}, {1, 1, false}, {1, 2, true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("origens (inserção) = %+v, quer %+v", got, want)
	}
}

func mustPares(t *testing.T, a, b []string) [][2]int {
	t.Helper()
	p, ok := pareamento(a, b)
	if !ok {
		t.Fatal("pareamento recusou")
	}
	return p
}
