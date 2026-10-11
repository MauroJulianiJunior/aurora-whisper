package metrics

// MatchResult é o resultado comum de Entity Accuracy e Number/Date
// Accuracy: de um total de itens esperados (Total), quantos foram
// encontrados corretamente na hipótese (Correct). As duas métricas
// compartilham o mesmo formato porque as duas são, no fundo, a mesma
// pergunta — "quantos itens de uma lista esperada aparecem no texto
// transcrito" — só que a lista é de entidades num caso e de
// números/datas no outro.
type MatchResult struct {
	Correct int
	Total   int
}

// Score devolve Correct/Total como fração em [0, 1] (quando Correct não
// excede Total, o que as funções deste pacote garantem).
//
// Quando Total é 0 (nenhum item esperado na amostra), não há divisão
// possível — por convenção este pacote devolve 1.0 (vacuously correto:
// nada foi exigido, nada foi errado). Isso NÃO deve ser usado para
// inflar uma média sem cuidado: quem agrega resultados por amostra deve
// olhar Total antes de decidir se a amostra participa da média — uma
// amostra sem nenhuma entidade esperada não diz nada sobre a qualidade de
// Entity Accuracy do candidato, e o gate do produto (19-eval-plan.md)
// espera amostras com entidades de verdade.
func (r MatchResult) Score() float64 {
	if r.Total == 0 {
		return 1
	}
	return float64(r.Correct) / float64(r.Total)
}

// countOccurrences conta ocorrências NÃO sobrepostas de pattern dentro de
// tokens, varrendo da esquerda para a direita. Não sobrepor importa para
// entidades de mais de um token: "n8n n8n" não deve contar 3 vezes um
// padrão de 2 tokens que também "encaixaria" deslizando de 1 em 1.
func countOccurrences(tokens []string, pattern []string) int {
	if len(pattern) == 0 || len(pattern) > len(tokens) {
		return 0
	}
	count := 0
	i := 0
	for i+len(pattern) <= len(tokens) {
		if tokenSliceEqual(tokens[i:i+len(pattern)], pattern) {
			count++
			i += len(pattern)
			continue
		}
		i++
	}
	return count
}

func tokenSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
