package dictionary

import (
	"strings"
	"unicode"
)

// accentFold mapeia vogais acentuadas, cedilha e til de nasalização comuns
// em PT-BR (e alguns casos de outras línguas latinas que aparecem em nomes
// próprios/estrangeirismos) para o caractere ASCII equivalente. Cobre só
// minúsculas porque Normalize sempre passa por strings.ToLower antes.
var accentFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c',
	'ñ': 'n',
}

// Normalize reduz s à forma canônica usada como chave de busca/unicidade do
// dicionário: minúsculas, sem acentos/cedilha, e com pontuação removida e
// espaços colapsados a um único espaço (sem espaço nas bordas).
//
// Exemplos:
//
//	Normalize("DCTFWeb")     == "dctfweb"
//	Normalize("Físico")      == "fisico"
//	Normalize("Fisco!")      == "fisco"
//	Normalize("  n8n   ")    == "n8n"
//	Normalize("dctf-web")    == "dctf web"
//
// Números são preservados (ex.: "n8n") porque fazem parte de vários termos
// técnicos do domínio (nomes de produto, versões). Qualquer outro símbolo
// (pontuação, hífen, underline, emoji) é tratado como separador de palavra
// e vira espaço — nunca some duas palavras numa só.
func Normalize(s string) string {
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false

	for _, r := range s {
		if folded, ok := accentFold[r]; ok {
			r = folded
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteRune(r)
			continue
		}
		// Qualquer coisa que não seja letra/dígito (espaço, pontuação,
		// hífen, símbolo) colapsa para, no máximo, um espaço.
		pendingSpace = true
	}

	return b.String()
}

// CollapseSpaces remove os espaços internos de uma forma já normalizada,
// para permitir casar variações de espaçamento de um mesmo termo composto
// (ex.: "dctf web" contra "dctfweb" — o ASR às vezes separa em duas
// palavras o que o usuário digitaria junto). Search usa isso como um
// segundo nível de comparação exata, não como substituto de Normalize:
// duas palavras genuinamente distintas não devem ser tratadas como uma só
// só porque perderam o espaço.
func CollapseSpaces(normalized string) string {
	return strings.ReplaceAll(normalized, " ", "")
}
