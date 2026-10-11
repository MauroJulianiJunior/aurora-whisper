package metrics

import "strings"

// EntityAccuracy calcula quantas entidades esperadas (expected — nomes de
// ferramentas, siglas, nomes próprios, ex.: "DCTFWeb", "n8n", "Fisco")
// aparecem corretamente no texto transcrito (hypothesis).
//
// IMPORTANTE (19-eval-plan.md, "Gates local"): o gate do produto é
// "Entity Accuracy ≥95% APÓS dictionary" — esta função NÃO aplica
// internal/dictionary, mede o texto BRUTO do candidato como ele sai do
// ASR. O resultado daqui é "Entity Accuracy do ASR bruto (sem
// dictionary)", um número diferente e tipicamente pior do que o gate real
// exige. Ver internal/benchmark/report: a linha de gate "após dictionary"
// é reportada como NÃO MEDIDO por este harness, precisamente porque quem
// chama esta função (harness.buildOutcome) nunca passa o texto pelo
// dicionário.
//
// Comparação: CASE-INSENSITIVE ("dctfweb" conta como acerto para
// "DCTFWeb"), mas sensível a acento e a dígito, porque a normalização
// compartilhada (normalizeTokens) só faz minúsculas + pontuação de borda,
// nunca remove acento nem toca em dígito. Uma entidade só conta como
// encontrada se aparecer como a MESMA sequência de tokens — "n 8 n" (três
// tokens) NÃO conta como acerto para "n8n" (um token só), porque são
// sequências de token diferentes; o benchmark não tenta adivinhar que a
// pessoa "quis dizer" a mesma coisa.
//
// expected pode repetir uma entidade (ex.: ["Fisco", "Fisco"] quando a
// frase de referência menciona "Fisco" duas vezes) — cada ocorrência
// esperada é contada separadamente, e o número de acertos por entidade é
// limitado ao número de vezes que ela de fato aparece na hipótese (uma
// entidade repetida 2x no esperado mas presente só 1x na hipótese conta
// como 1 acerto de 2, não 2 de 2).
//
// Entidades vazias (string vazia ou só espaço) em expected são ignoradas
// — não há o que comparar, e contá-las como acerto ou erro seria
// arbitrário.
func EntityAccuracy(expected []string, hypothesis string) MatchResult {
	hypTokens := normalizeTokens(hypothesis)

	type entry struct {
		pattern  []string
		required int
	}
	order := make([]string, 0, len(expected))
	byKey := make(map[string]*entry, len(expected))

	for _, e := range expected {
		pattern := normalizeTokens(e)
		if len(pattern) == 0 {
			continue
		}
		key := strings.Join(pattern, "\x00")
		if en, ok := byKey[key]; ok {
			en.required++
			continue
		}
		byKey[key] = &entry{pattern: pattern, required: 1}
		order = append(order, key)
	}

	var result MatchResult
	for _, key := range order {
		en := byKey[key]
		result.Total += en.required

		available := countOccurrences(hypTokens, en.pattern)
		if available > en.required {
			available = en.required
		}
		result.Correct += available
	}
	return result
}
