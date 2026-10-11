package metrics

import "sort"

// Stats resume um conjunto de amostras: média, p50 e p95, sempre
// acompanhados de N. N precisa ir junto porque um percentil sem contexto
// de tamanho de amostra mente por omissão — p95 de 10 amostras é, na
// prática, quase o máximo (ver Aggregate), e o relatório de benchmark
// (docs/ai-spec/19-eval-plan.md) precisa poder dizer isso honestamente em
// vez de imprimir um número que parece mais sólido do que é.
type Stats struct {
	N    int
	Mean float64
	P50  float64
	P95  float64
	// Max é o maior valor observado na amostra. Existe separado de P95
	// porque, para um gate de LIMITE (ex.: RSS alvo ≤~1 GB), o que importa
	// é o pior caso — um candidato cuja pior chamada estoura o limite não
	// deve passar só porque as outras compensaram a média (ver pacote
	// report, gate de RSS: decide por Max, nunca por Mean).
	Max float64
}

// Aggregate calcula média, p50 e p95 de samples. Não modifica o slice
// recebido (ordena uma cópia internamente).
//
// Lista vazia devolve Stats{N: 0} — Mean/P50/P95 ficam no zero-value
// (0.0), e é responsabilidade de quem lê o Stats checar N antes de
// confiar nesses zeros; este pacote não inventa um valor "neutro" melhor
// que zero para "nenhuma amostra existe", e não entra em pânico com
// divisão por zero.
//
// Percentil: método nearest-rank (ordena as amostras, escolhe o índice
// ceil(p/100 * N), 1-based) — não interpolação linear. É a mesma
// convenção usada por sistemas de observabilidade para p50/p95 de
// latência: com N par, p50 pode não coincidir com a média dos dois
// valores centrais, o que é uma escolha aceitável e documentada aqui, não
// um descuido.
//
// Consequência que o comentário do 19-eval-plan.md já avisa: com N=10,
// ceil(0.95*10) = 10 → o índice do ÚLTIMO elemento (o máximo da amostra).
// Ou seja, com poucas amostras "p95" e "máximo observado" são quase a
// mesma coisa — não é bug desta função, é a definição de percentil
// aplicada a uma amostra pequena, e é exatamente por isso que Stats
// carrega N.
func Aggregate(samples []float64) Stats {
	n := len(samples)
	if n == 0 {
		return Stats{N: 0}
	}

	sorted := make([]float64, n)
	copy(sorted, samples)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	return Stats{
		N:    n,
		Mean: sum / float64(n),
		P50:  percentile(sorted, 50),
		P95:  percentile(sorted, 95),
		Max:  sorted[n-1],
	}
}

// percentile assume sorted já ordenado ascendentemente e não vazio.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	rank := int(ceilDiv100(p, n))
	idx := rank - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

// ceilDiv100 calcula ceil(p/100 * n) sem depender de math.Ceil sobre
// float para evitar surpresa de arredondamento de ponto flutuante perto
// de um inteiro exato (ex.: p=50, n=2 → deveria dar exatamente 1, não
// 0.9999999999 arredondado para baixo por erro de representação).
func ceilDiv100(p float64, n int) float64 {
	value := (p / 100) * float64(n)
	whole := float64(int(value))
	if value-whole > 1e-9 {
		return whole + 1
	}
	return whole
}
