package metrics

import (
	"errors"
)

// ErrEmptyReference é devolvido por WER e CER quando a referência, depois
// de normalizada, não tem nenhum token (string vazia ou só espaços em
// branco). A fórmula padrão é (erros) / (tokens da referência); com zero
// tokens no denominador a divisão não tem resposta matemática honesta, e
// este pacote nunca inventa um número — devolve o erro e deixa quem chama
// decidir (tipicamente: descartar a amostra do relatório).
var ErrEmptyReference = errors.New("metrics: reference is empty after normalization")

// WER calcula o Word Error Rate: (substituições + inserções + deleções) /
// (número de tokens da referência), por distância de edição a nível de
// palavra, após normalização (normalizeTokens: minúsculas, colapso de
// espaço, pontuação de borda removida — acento preservado).
//
// Teto: esta função NÃO limita o resultado a 1.0. Se a hipótese tiver
// muito mais tokens que a referência, o WER pode passar de 1.0 (ex.:
// referência "a", hipótese "a b c d" → WER = 3.0) — esse é o
// comportamento padrão de WER na literatura (ferramentas como o SCTK
// também não limitam), e limitar artificialmente esconderia um candidato
// que está inserindo lixo em excesso.
func WER(reference, hypothesis string) (float64, error) {
	refTokens := normalizeTokens(reference)
	if len(refTokens) == 0 {
		return 0, ErrEmptyReference
	}
	hypTokens := normalizeTokens(hypothesis)
	dist := editDistance(refTokens, hypTokens)
	return float64(dist) / float64(len(refTokens)), nil
}

// CER calcula o Character Error Rate: mesma fórmula do WER, mas por
// distância de edição a nível de caractere (rune) sobre o texto
// normalizado (tokens normalizados, unidos por um único espaço). Captura
// erro DENTRO de uma palavra que o WER arredondaria para "1 palavra
// errada" inteira (ex.: "DCTFWeb" transcrito "DCTFWeb" com 1 letra trocada
// é 1 palavra errada no WER, mas só uma fração de erro no CER).
//
// Mesmo teto do WER: não há limite superior de 1.0.
func CER(reference, hypothesis string) (float64, error) {
	refChars := []rune(normalizeForCER(reference))
	if len(refChars) == 0 {
		return 0, ErrEmptyReference
	}
	hypChars := []rune(normalizeForCER(hypothesis))
	dist := editDistance(refChars, hypChars)
	return float64(dist) / float64(len(refChars)), nil
}
