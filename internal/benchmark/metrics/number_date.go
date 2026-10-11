package metrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NumberDateAccuracy extrai números e datas da referência e da hipótese e
// compara os dois conjuntos, tolerando SOMENTE diferença de formatação —
// não diferença de valor. "1500" e "1.500" contam como o MESMO número
// (separador de milhar); "1500" e "1050" são números diferentes. A
// contagem é por multiconjunto: se a referência menciona "1500" duas
// vezes, a hipótese precisa mencionar o valor duas vezes para os dois
// acertos.
//
// Datas são reconhecidas separadamente e mascaradas antes da extração de
// números, para que os dígitos de uma data ("17/09/2026") não sejam
// contados de novo como três números soltos.
//
// Fora do escopo deste sprint, documentado aqui para não ser reinventado
// por engano: números negativos (sinal "-") e datas por extenso ("17 de
// setembro de 2026"). Nenhum dos dois aparece no dataset de contabilidade
// como dígito puro, que é o que este benchmark mede.
func NumberDateAccuracy(reference, hypothesis string) MatchResult {
	refItems := extractNumbersAndDates(reference)
	hypItems := extractNumbersAndDates(hypothesis)

	required := make(map[string]int, len(refItems))
	for _, item := range refItems {
		required[item]++
	}
	available := make(map[string]int, len(hypItems))
	for _, item := range hypItems {
		available[item]++
	}

	var result MatchResult
	for value, need := range required {
		result.Total += need
		have := available[value]
		if have > need {
			have = need
		}
		result.Correct += have
	}
	return result
}

func extractNumbersAndDates(s string) []string {
	dates, spans := extractDates(s)
	masked := maskSpans(s, spans)
	numbers := extractNumbers(masked)

	items := make([]string, 0, len(dates)+len(numbers))
	for _, d := range dates {
		// prefixo evita colisão acidental entre a forma canônica de uma
		// data e a de um número (defensivo: com o formato atual as duas
		// nunca colidem mesmo sem prefixo, mas não custa garantir).
		items = append(items, "date:"+d)
	}
	for _, n := range numbers {
		items = append(items, "num:"+n)
	}
	return items
}

// numberRegex casa sequências de dígitos, opcionalmente agrupadas por '.'
// ou ',' (separadores de milhar/decimal). Não inclui '/': datas usam '/'
// e são reconhecidas à parte por extractDates, para não virarem 3 números
// soltos.
var numberRegex = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

func extractNumbers(s string) []string {
	idx := numberRegex.FindAllStringIndex(s, -1)
	out := make([]string, 0, len(idx))
	for _, loc := range idx {
		if coladoEmLetra(s, loc[0], loc[1]) {
			continue
		}
		out = append(out, canonicalizeNumber(s[loc[0]:loc[1]]))
	}
	return out
}

// coladoEmLetra reporta se a sequência de dígitos em [ini,fim) tem uma
// letra imediatamente antes ou depois — caso em que ela faz parte de uma
// PALAVRA e não é um número.
//
// Achado rodando o benchmark de verdade pela primeira vez: a referência
// "O workflow do n8n está travando no nó de webhook" produzia o item
// `num:8`. O nome da ferramenta virava um número fantasma na métrica de
// Número/Data, que é uma das notas do 19-eval-plan.md.
//
// O efeito é pior do que parece. Quando referência e hipótese trazem o
// mesmo "n8n", o acerto espúrio INFLA a nota com algo que não é número.
// Quando o ASR escreve "N 8 N" ou "nan", vira erro espúrio. Os dois lados
// medem a grafia de um nome próprio dentro da métrica errada — Entity
// Accuracy já cuida disso.
//
// RE2 (a engine de regexp do Go) não tem lookaround, então a checagem é
// feita aqui, olhando o rune vizinho, em vez de no padrão.
//
// Prefixo de moeda continua valendo: em "R$1.530,40" o caractere antes do
// "1" é "$", que não é letra, então o número é extraído normalmente.
func coladoEmLetra(s string, ini, fim int) bool {
	if ini > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:ini])
		if unicode.IsLetter(r) {
			return true
		}
	}
	if fim < len(s) {
		r, _ := utf8.DecodeRuneInString(s[fim:])
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// canonicalizeNumber normaliza um número extraído por numberRegex para uma
// forma comparável, resolvendo separador de milhar vs. decimal. Regras de
// normalização (decisão de produto, documentada aqui porque muda a nota):
//
//   - ',' com UMA ocorrência é sempre separador decimal (convenção
//     PT-BR: "1.500,50" == 1500.50). Os '.' antes dela são milhar.
//   - ',' com DUAS OU MAIS ocorrências: sem convenção PT-BR razoável para
//     isso, tratadas como separador de milhar estilo en-US ("1,500,000");
//     não há parte fracionária nesse caso.
//   - sem vírgula, '.' em grupos de exatamente 3 dígitos ("1.500",
//     "12.345.678") é separador de milhar.
//   - sem vírgula, um único '.' seguido de 1 ou 2 dígitos finais ("3.5",
//     "3.50") é separador decimal (convenção en-US, comum em números de
//     versão/técnicos ditados em contexto PT-BR).
//   - qualquer outro padrão de pontos sem vírgula (ambíguo) cai no
//     fallback conservador: todos os pontos como separador de milhar.
//
// Zeros à esquerda na parte inteira e zeros à direita na parte
// fracionária são removidos, para que "1500" e "1500.0" comparem iguais.
func canonicalizeNumber(raw string) string {
	var integerPart, fracPart string

	switch strings.Count(raw, ",") {
	case 1:
		parts := strings.SplitN(raw, ",", 2)
		integerPart = strings.ReplaceAll(parts[0], ".", "")
		fracPart = parts[1]
	case 0:
		integerPart, fracPart = splitDotGroups(raw)
	default:
		integerPart = strings.ReplaceAll(strings.ReplaceAll(raw, ",", ""), ".", "")
	}

	integerPart = stripLeadingZeros(integerPart)
	fracPart = strings.TrimRight(fracPart, "0")

	if fracPart == "" {
		return integerPart
	}
	return integerPart + "." + fracPart
}

func splitDotGroups(raw string) (integerPart, fracPart string) {
	parts := strings.Split(raw, ".")
	if len(parts) == 1 {
		return parts[0], ""
	}

	thousandsGrouping := len(parts[0]) >= 1 && len(parts[0]) <= 3
	if thousandsGrouping {
		for _, p := range parts[1:] {
			if len(p) != 3 {
				thousandsGrouping = false
				break
			}
		}
	}
	if thousandsGrouping {
		return strings.Join(parts, ""), ""
	}

	if len(parts) == 2 && len(parts[1]) <= 2 {
		return parts[0], parts[1]
	}

	// fallback conservador: não encaixa em agrupamento de milhar nem em
	// decimal simples de 1-2 casas — trata todos os pontos como milhar.
	return strings.Join(parts, ""), ""
}

func stripLeadingZeros(s string) string {
	i := 0
	for i < len(s)-1 && s[i] == '0' {
		i++
	}
	return s[i:]
}

// dateRegex casa datas numéricas em formato DD/MM/AAAA, AAAA-MM-DD e
// variações com '-' ou '/' como separador. Datas por extenso ("17 de
// setembro") ficam fora do escopo, documentado no comentário de
// NumberDateAccuracy.
var dateRegex = regexp.MustCompile(`\b(\d{1,4})[/-](\d{1,2})[/-](\d{1,4})\b`)

func extractDates(s string) (canonical []string, spans [][2]int) {
	matches := dateRegex.FindAllStringSubmatchIndex(s, -1)
	for _, m := range matches {
		g1 := s[m[2]:m[3]]
		g2 := s[m[4]:m[5]]
		g3 := s[m[6]:m[7]]
		c, ok := canonicalizeDate(g1, g2, g3)
		if !ok {
			continue
		}
		canonical = append(canonical, c)
		spans = append(spans, [2]int{m[0], m[1]})
	}
	return canonical, spans
}

// canonicalizeDate resolve qual grupo é ano/mês/dia e devolve a forma
// canônica AAAA-MM-DD. Assume convenção BRASILEIRA (DD/MM/AAAA) quando o
// formato não é claramente ISO (AAAA-MM-DD) — MM/DD/AAAA (americano) NÃO
// é suportado neste sprint, decisão documentada: o produto é PT-BR.
//
// Ano de 2 dígitos é assumido no século 2000 (ex.: "17/09/26" ->
// 2026-09-17) — suficiente para o horizonte deste produto.
func canonicalizeDate(g1, g2, g3 string) (string, bool) {
	var year, month, day int
	var err error

	switch {
	case len(g1) == 4: // AAAA-MM-DD (ISO)
		year, err = strconv.Atoi(g1)
		if err != nil {
			return "", false
		}
		month, err = strconv.Atoi(g2)
		if err != nil {
			return "", false
		}
		day, err = strconv.Atoi(g3)
		if err != nil {
			return "", false
		}
	case len(g3) == 4: // DD/MM/AAAA
		day, err = strconv.Atoi(g1)
		if err != nil {
			return "", false
		}
		month, err = strconv.Atoi(g2)
		if err != nil {
			return "", false
		}
		year, err = strconv.Atoi(g3)
		if err != nil {
			return "", false
		}
	default: // DD/MM/AA
		day, err = strconv.Atoi(g1)
		if err != nil {
			return "", false
		}
		month, err = strconv.Atoi(g2)
		if err != nil {
			return "", false
		}
		yy, err2 := strconv.Atoi(g3)
		if err2 != nil {
			return "", false
		}
		year = 2000 + yy
	}

	if month < 1 || month > 12 || day < 1 || day > 31 {
		return "", false
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day), true
}

func maskSpans(s string, spans [][2]int) string {
	if len(spans) == 0 {
		return s
	}
	b := []byte(s)
	for _, sp := range spans {
		for i := sp[0]; i < sp[1]; i++ {
			b[i] = ' '
		}
	}
	return string(b)
}
