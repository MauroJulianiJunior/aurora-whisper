package smartconta

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// A meta da ADR-021 (C2): 500 pares montados e resolvidos em menos de 25 ms. Os
// pares são sintéticos, com a forma dos reais (variantes de uma a quatro
// palavras, termos de uma a três), e sempre os mesmos: a semente do gerador é
// fixa.

var (
	silabas = []string{"ba", "be", "bi", "bo", "ca", "ce", "co", "da", "de", "di", "do", "fa", "fe", "fi",
		"ga", "ge", "go", "la", "le", "li", "lo", "ma", "me", "mi", "mo", "na", "ne", "ni", "no",
		"pa", "pe", "pi", "po", "ra", "re", "ri", "ro", "sa", "se", "si", "so", "ta", "te", "ti", "to",
		"va", "ve", "vi", "vo", "xa", "xe", "za", "ze", "zu"}
	sufixos = []string{"", "", "", "x", "s", "r", "n"}
)

// palavraSintetica devolve uma palavra de 2 a 4 sílabas.
func palavraSintetica(r *rand.Rand) string {
	var b strings.Builder
	for i, n := 0, 2+r.IntN(3); i < n; i++ {
		b.WriteString(silabas[r.IntN(len(silabas))])
	}
	b.WriteString(sufixos[r.IntN(len(sufixos))])
	return b.String()
}

// paresSinteticos devolve n pares distintos, do mais recente ao mais antigo,
// como o ListLearned. A palavra de cada termo e de cada variante leva o número
// do par, para as chaves serem distintas.
func paresSinteticos(n int) []dictionary.ParDaNuvem {
	r := rand.New(rand.NewPCG(21, 10))
	pares := make([]dictionary.ParDaNuvem, 0, n)
	for i := 0; i < n; i++ {
		var variante, termo []string
		for j, k := 0, 1+r.IntN(4); j < k; j++ {
			variante = append(variante, palavraSintetica(r))
		}
		variante[len(variante)-1] += fmt.Sprintf("%d", i)
		for j, k := 0, 1+r.IntN(3); j < k; j++ {
			termo = append(termo, strings.ToUpper(palavraSintetica(r)[:2])+palavraSintetica(r))
		}
		termo[0] += fmt.Sprintf("%d", i)
		pares = append(pares, dictionary.ParDaNuvem{
			Variante: strings.Join(variante, " "),
			Termo:    strings.Join(termo, " "),
		})
	}
	return pares
}

// textoDeDitado é um ditado de uns 40 palavras, com duas variantes de pares.
func textoDeDitado(pares []dictionary.ParDaNuvem) string {
	return "eu queria falar com o time sobre " + pares[len(pares)/3].Variante +
		" e também sobre o que ficou combinado na reunião de ontem, porque ainda não está claro quem vai cuidar de " +
		pares[len(pares)/2].Variante + " nem quando isso precisa ficar pronto para a entrega do cliente"
}

// BenchmarkMontar500 é só a montagem do Smart da conta com 500 pares.
func BenchmarkMontar500(b *testing.B) {
	pares := paresSinteticos(MaxPares)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Montar("conta-1", pares); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResolver500 é só o Smart já montado resolvendo um ditado.
func BenchmarkResolver500(b *testing.B) {
	pares := paresSinteticos(MaxPares)
	c, err := Montar("conta-1", pares)
	if err != nil {
		b.Fatal(err)
	}
	texto := textoDeDitado(pares)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if d := c.Resolver(context.Background(), texto); d.SmartFalhou {
			b.Fatal("o Smart falhou")
		}
	}
}

// BenchmarkMontarEResolver500 é o que a meta da ADR mede: montar e resolver.
func BenchmarkMontarEResolver500(b *testing.B) {
	pares := paresSinteticos(MaxPares)
	texto := textoDeDitado(pares)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := Montar("conta-1", pares)
		if err != nil {
			b.Fatal(err)
		}
		if d := c.Resolver(context.Background(), texto); d.SmartFalhou {
			b.Fatal("o Smart falhou")
		}
	}
}
