package notas

import (
	"strings"
	"unicode"
)

// semAcento leva as letras acentuadas do português (e as de nomes próprios
// latinos mais comuns) à letra de base. Só minúsculas: Dobrar passa tudo para
// minúscula antes.
var semAcento = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
}

// Dobrar põe s em minúsculas e sem acento: a forma em que se compara o nome do
// marcador, se busca no texto das notas e se casa o título no "ditar para
// nota". Não mexe em pontuação nem em espaço ("C#" e "C++" continuam
// diferentes).
func Dobrar(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1 // o acento solto, de quem escreveu em NFD
		}
		r = unicode.ToLower(r)
		if f, ok := semAcento[r]; ok {
			return f
		}
		return r
	}, s)
}
