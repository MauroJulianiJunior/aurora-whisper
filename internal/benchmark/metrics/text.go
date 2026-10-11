package metrics

import (
	"strings"
	"unicode"
)

// normalizeTokens aplica a normalização comum a WER, Entity Accuracy e
// Number/Date Accuracy: minúsculas (Unicode-aware — "É" vira "é", nunca
// "e"), colapso de espaço (strings.Fields já ignora runs de espaço em
// branco e as bordas) e remoção de pontuação de BORDA de cada token
// (aspas, vírgula, ponto final etc. coladas na palavra).
//
// Acento NUNCA é removido. Em PT-BR "e" e "é" são palavras diferentes, e
// apagar o acento infla artificialmente a nota de qualquer candidato que
// erre acentuação — o que é exatamente o tipo de erro que este benchmark
// existe para capturar.
//
// Pontuação NO MEIO do token é preservada (ex.: "n8n", "self-correction",
// "R$1.500,00" mantêm seu conteúdo interno intacto) — só a borda é
// aparada, porque só a borda é ambiguidade de tokenização (a vírgula em
// "Fisco," é pontuação de frase, não parte da palavra "Fisco").
func normalizeTokens(s string) []string {
	fields := strings.Fields(strings.ToLower(s))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimFunc(f, isBoundaryPunct)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// isBoundaryPunct decide se um rune conta como pontuação de borda a ser
// removida. unicode.IsPunct cobre as categorias Unicode de pontuação
// (aspas, vírgula, ponto, parênteses, etc.) sem tocar em letras
// acentuadas, que pertencem à categoria de letra, não de pontuação.
func isBoundaryPunct(r rune) bool {
	return unicode.IsPunct(r)
}

// normalizeForCER produz o texto normalizado como uma única string (tokens
// separados por um único espaço), para comparação caractere a caractere.
// Usa a mesma normalização de normalizeTokens — CER precisa ver o mesmo
// texto que WER vê, só que na granularidade de rune em vez de palavra.
func normalizeForCER(s string) string {
	return strings.Join(normalizeTokens(s), " ")
}

// editDistance calcula a distância de edição de Levenshtein (custo 1 por
// substituição, inserção ou deleção) entre duas sequências genéricas —
// usada com []string para WER (nível de palavra) e com []rune para CER
// (nível de caractere).
//
// Esta é exatamente a definição clássica de WER/CER: o mínimo de
// substituições + inserções + deleções que transforma a referência na
// hipótese. Uma substituição custa 1 (não 2, como deleção+inserção
// separadas) porque o algoritmo de programação dinâmica escolhe sempre o
// caminho de custo mínimo.
func editDistance[T comparable](a, b []T) int {
	n, m := len(a), len(b)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for j := 0; j <= m; j++ {
		prev[j] = j
	}

	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1]
				continue
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + 1
			curr[j] = minInt(del, minInt(ins, sub))
		}
		prev, curr = curr, prev
	}
	return prev[m]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
