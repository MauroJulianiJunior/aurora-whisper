package dictionary

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// accentFold mapeia vogais acentuadas, cedilha e til de nasalização comuns
// em PT-BR (e alguns casos de outras línguas latinas que aparecem em nomes
// próprios/estrangeirismos) para o caractere ASCII equivalente. Cobre só
// minúsculas porque Normalize sempre passa a runa para minúscula antes.
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
	// Normalize é chamada a cada comparação do dicionário (o Smart da conta
	// monta e consulta o dicionário a cada ditado), e a maior parte do que
	// chega já está normalizada ou é ASCII. O resultado é o de sempre, runa a
	// runa: strings.ToLower é unicode.ToLower de cada runa (o byte inválido vira
	// U+FFFD, que não é letra, e aqui também), seguido do accentFold. O teste
	// diferencial (normalize_diferencial_test.go) compara com a versão antiga.
	if jaNormalizada(s) {
		return s
	}

	out := make([]byte, 0, len(s))
	pendingSpace := false

	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			i++
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') {
				if pendingSpace && len(out) > 0 {
					out = append(out, ' ')
				}
				pendingSpace = false
				out = append(out, c)
				continue
			}
			// Qualquer coisa que não seja letra/dígito (espaço, pontuação,
			// hífen, símbolo) colapsa para, no máximo, um espaço.
			pendingSpace = true
			continue
		}

		r, tamanho := utf8.DecodeRuneInString(s[i:])
		i += tamanho
		r = unicode.ToLower(r)
		if folded, ok := accentFold[r]; ok {
			r = folded
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSpace && len(out) > 0 {
				out = append(out, ' ')
			}
			pendingSpace = false
			out = utf8.AppendRune(out, r)
			continue
		}
		pendingSpace = true
	}

	return string(out)
}

// jaNormalizada diz se s já é o que Normalize devolveria dele mesmo: só a-z e
// 0-9, com no máximo um espaço entre as palavras e nenhum nas bordas. Nesse
// caso Normalize devolve s, sem alocar.
func jaNormalizada(s string) bool {
	depoisDeEspaco := true // o começo conta: um espaço inicial não é normalizado
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9':
			depoisDeEspaco = false
		case c == ' ':
			if depoisDeEspaco {
				return false
			}
			depoisDeEspaco = true
		default:
			return false
		}
	}
	return len(s) == 0 || !depoisDeEspaco
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
