package resolver

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Tokenização das guardas do Polish. Além das palavras, as guardas precisam
// saber o que cerca cada uma — a pontuação e a quebra de linha que o modelo
// pôs no lugar de uma palavra falada, e o marcador de lista da linha —, porque
// só assim conferem que "vírgula" sumiu PORQUE virou ",", e "primeiro" PORQUE
// virou "1.".

// reMarcadorLinha casa o marcador de lista no INÍCIO de uma linha: "1. ",
// "12) ", "- ", "* " ou "• ". Exige espaço depois, então "1.500" e "-5"
// nunca são marcador.
var reMarcadorLinha = regexp.MustCompile(`^[ \t]*(?:\d{1,3}[.)]|[-*•])[ \t]+`)

// reMarcadorNumerado casa só o marcador numerado, capturando o número.
var reMarcadorNumerado = regexp.MustCompile(`^[ \t]*(\d{1,3})[.)][ \t]+`)

// tokenTexto é uma palavra do texto com o que a cerca.
type tokenTexto struct {
	// Palavra é a forma normalizada: minúscula, sem a pontuação das bordas.
	// Acento conta; pontuação de dentro da palavra fica ("1.847,32",
	// "efd-reinf", "e/ou"): reformatar um valor ou uma sigla é edição, que é
	// o que se quer pegar.
	Palavra string
	// Bruta é a grafia original (mesma caixa), sem a pontuação das bordas.
	Bruta string
	// Antes é o texto bruto entre a palavra anterior e esta: pontuação,
	// espaços e quebras de linha, SEM o marcador de lista. É onde o modelo
	// escreve a vírgula que veio de "vírgula".
	Antes string
	// Marcador diz que esta é a primeira palavra de uma linha que abre com
	// marcador de lista ("1.", "-").
	Marcador bool
}

// tokenizar parte s em palavras (tokenTexto) na ordem em que aparecem e
// devolve também a cauda: o texto depois da última palavra.
func tokenizar(s string) (tokens []tokenTexto, cauda string) {
	var antes strings.Builder
	for i, linha := range strings.Split(s, "\n") {
		if i > 0 {
			antes.WriteByte('\n')
		}
		temMarcador := false
		if loc := reMarcadorLinha.FindStringIndex(linha); loc != nil {
			linha = linha[loc[1]:]
			temMarcador = true
		}
		primeiraDaLinha := true
		for _, campo := range strings.FieldsFunc(linha, unicode.IsSpace) {
			runas := []rune(campo)
			ini, fim := 0, len(runas)
			for ini < fim && !isWordRune(runas[ini]) {
				ini++
			}
			for fim > ini && !isWordRune(runas[fim-1]) {
				fim--
			}
			if ini == fim { // só pontuação: fica com o "antes" da próxima palavra
				antes.WriteString(campo)
				continue
			}
			antes.WriteString(string(runas[:ini]))
			bruta := string(runas[ini:fim])
			tokens = append(tokens, tokenTexto{
				Palavra:  strings.ToLower(bruta),
				Bruta:    bruta,
				Antes:    antes.String(),
				Marcador: temMarcador && primeiraDaLinha,
			})
			primeiraDaLinha = false
			antes.Reset()
			antes.WriteString(string(runas[fim:]))
		}
	}
	return tokens, antes.String()
}

// palavrasDe devolve só as palavras normalizadas dos tokens.
func palavrasDe(tokens []tokenTexto) []string {
	palavras := make([]string, len(tokens))
	for i, t := range tokens {
		palavras[i] = t.Palavra
	}
	return palavras
}

// palavrasNormalizadas é a unidade de comparação das guardas de palavras:
// tira os marcadores de lista, parte por espaço em branco, apara a
// pontuação das BORDAS de cada palavra e passa para minúsculas. Pontuação
// e caixa não contam; acento conta ("pra" -> "para" e "e" -> "é" são
// edições).
func palavrasNormalizadas(s string) []string {
	tokens, _ := tokenizar(s)
	return palavrasDe(tokens)
}

// semMarcadoresNumerados tira do começo das linhas os marcadores de lista
// que a guarda de números deixa passar — "N." ou "N)" que formam uma
// numeração 1, 2, 3... em sequência (recomeçando em 1) e os de "-", "*" e
// "•" — e devolve o texto sem eles e os números tirados, na ordem.
//
// Qualquer outro "N." no começo da linha NÃO é marcador: fica no texto, e o
// número conta na guarda de números. É o que impede "15. hoje" de esconder um
// número acrescentado.
func semMarcadoresNumerados(s string) (texto string, numeros []int) {
	linhas := strings.Split(s, "\n")
	esperado := 1
	for i, linha := range linhas {
		if m := reMarcadorNumerado.FindStringSubmatch(linha); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n == esperado || n == 1 {
				numeros = append(numeros, n)
				esperado = n + 1
				linhas[i] = linha[len(m[0]):]
			}
			continue
		}
		if loc := reMarcadorLinha.FindStringIndex(linha); loc != nil {
			linhas[i] = linha[loc[1]:]
		}
	}
	return strings.Join(linhas, "\n"), numeros
}
