package resolver

import (
	"sort"
	"strings"
	"unicode"
)

// normalizeWhitespace aplica a "normalização mínima de Unicode/espaços"
// pedida pelo Literal (AC-023): remove espaços nas bordas e colapsa
// qualquer sequência de espaço em branco Unicode (inclui NBSP, tabs,
// quebras de linha) a um único espaço ASCII. Não mexe em pontuação: a
// pontuação nativa do ASR é preservada como está — o Literal tem
// permissão explícita para isso, mas não para mais nada.
//
// Não faz normalização de forma Unicode (NFC/NFD): isso exigiria
// golang.org/x/text, uma dependência externa fora do escopo desta fase
// (regra: zero dependência nova, só stdlib). Registrado como "concerns"
// no relatório desta slice.
func normalizeWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	started := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if started {
				pendingSpace = true
			}
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
		started = true
	}
	return b.String()
}

// wordToken é uma sequência de letras/dígitos Unicode dentro de um texto,
// com seus offsets em RUNAS (nunca bytes, para nunca partir um caractere
// multibyte). É a unidade que o detector de incerteza e a aplicação de
// substituições usam: cada substituição troca exatamente o intervalo
// [Start,End) de um token, nunca o texto ao redor (espaços, pontuação).
type wordToken struct {
	Start, End int
	Text       string
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// tokenizeWords parte s em wordTokens, na ordem em que aparecem (Start
// crescente).
func tokenizeWords(s string) []wordToken {
	runes := []rune(s)
	var tokens []wordToken
	i := 0
	for i < len(runes) {
		if !isWordRune(runes[i]) {
			i++
			continue
		}
		start := i
		for i < len(runes) && isWordRune(runes[i]) {
			i++
		}
		tokens = append(tokens, wordToken{Start: start, End: i, Text: string(runes[start:i])})
	}
	return tokens
}

// offsetReplacement é uma substituição de texto por intervalo de runas,
// usada internamente por applyReplacements.
type offsetReplacement struct {
	Start, End int
	Text       string
}

// applyReplacements reconstrói s substituindo cada intervalo [Start,End)
// (em runas) pelo texto dado. Os intervalos não precisam vir ordenados —
// applyReplacements ordena por Start antes de aplicar. Intervalos
// sobrepostos ou fora dos limites de s nunca deveriam chegar aqui (cada
// camada que produz offsetReplacement já valida isso antes); se
// acontecerem mesmo assim, o intervalo inválido é ignorado em vez de
// corromper o texto ou entrar em pânico.
func applyReplacements(s string, reps []offsetReplacement) string {
	if len(reps) == 0 {
		return s
	}
	runes := []rune(s)
	sort.Slice(reps, func(i, j int) bool { return reps[i].Start < reps[j].Start })

	var b strings.Builder
	last := 0
	for _, r := range reps {
		if r.Start < last || r.Start < 0 || r.End > len(runes) || r.End < r.Start {
			continue
		}
		b.WriteString(string(runes[last:r.Start]))
		b.WriteString(r.Text)
		last = r.End
	}
	b.WriteString(string(runes[last:]))
	return b.String()
}

// digitsOnly devolve apenas os dígitos de s, na ordem em que aparecem —
// usado para checar preservação de números/datas (Smart regra "não mudar
// números sem evidência"; Polish regra "preservar números e datas").
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sameDigits reporta se a e b contêm exatamente a mesma sequência de
// dígitos (ignorando tudo o mais).
func sameDigits(a, b string) bool {
	return digitsOnly(a) == digitsOnly(b)
}

// diferePorMaisQueCaixa reporta se escrito e canonica diferem por algo
// além de maiúscula/minúscula.
//
// É a primeira metade de precisaCanonicalizar, que decide se um match
// EXATO do dicionário é reescrito. "Exato" lá tolera espaçamento (dictionary.CollapseSpaces
// compara "dctf web" com "dctfweb"), então um match exato pode muito bem
// estar grafado diferente da forma canônica.
//
// O critério é deliberadamente o MESMO de metrics.EntityAccuracy, que é
// como o portão do produto é medido: insensível a maiúscula, sensível a
// espaço, a acento e a dígito. Alinhar os dois evita a situação em que o
// resolver considera um texto correto e a medição o considera errado.
//
// Caixa fica de fora daqui: a exceção, o termo de grafia própria, é
// decidida em precisaCanonicalizar.
func diferePorMaisQueCaixa(escrito, canonica string) bool {
	return !strings.EqualFold(strings.TrimSpace(escrito), strings.TrimSpace(canonica))
}

// precisaCanonicalizar decide se um casamento EXATO do dicionário reescreve
// o que foi escrito: quando a grafia difere por mais que caixa, e também
// quando difere SÓ na caixa de um termo de grafia própria.
//
// Até 01-out-2026 a caixa nunca contava: o critério do portão
// (metrics.EntityAccuracy) é insensível a maiúscula, e mexer em texto certo
// pela definição do produto é o que o portão de correção falsa proíbe. No
// teste do Mauro daquele dia o ASR devolveu "ESOCIAL" e "ESOcial", e o Smart
// deixou assim, com "eSocial" no dicionário. Para quem lê, "ESOCIAL" não é a
// grafia do termo, e a caixa de "eSocial" não é a da frase: é a do nome.
//
// A regra é conservadora. Três condições, todas necessárias para mexer SÓ na
// caixa:
//
//  1. a forma canônica é de grafia própria (ver grafiaPropria);
//
//  2. o escrito tem alguma maiúscula: o ASR errou a caixa de um jeito que se
//     vê. Tudo minúsculo fica como veio, porque é a grafia correta de muito
//     código e endereço — "npm install typescript" (o npm recusa maiúscula no
//     nome do pacote), "systemctl restart postgresql", "esocial.gov.br",
//     "gov.br/esocial". Quem digita assim não errou a caixa;
//
//  3. o escrito não está colado em separador de código (colado; ver
//     colaEmCodigo): "Esocial@empresa.com", "Typescript/lib".
//
//     "ESOCIAL", "ESOcial", "Esocial" -> "eSocial"  (inclusive no começo da frase)
//     "Typescript" -> "TypeScript", "Postgresql" -> "PostgreSQL", "DCTFWEB" -> "DCTFWeb"
//     "esocial", "dctfweb" minúsculos           -> intocados (condição 2)
//     "das", "Das" com "DAS" no dicionário      -> intocados (sigla toda maiúscula)
//     "aurora", "Supabase" com "Aurora"          -> intocados (só a inicial: a frase manda)
//     "Log", "COMMIT" com "log", "commit"        -> intocados (palavra comum minúscula)
//
// O preço da condição 2: "esocial" minúsculo no meio de uma frase também fica
// como veio. É a mesma saída de antes de 01-out-2026 (não regride), e trocar
// isso por "reescreve o minúsculo também" reabre a reescrita de URL, pacote e
// comando (decisão do Mauro, ver 16-ai-behavior-spec.md, "Caixa do termo").
//
// Domínio, entrada restrita e termo do painel valem como no resto do Smart:
// a busca do dicionário (searchTop) já os aplicou antes de o casamento
// chegar aqui.
//
// colado diz se o trecho escrito encosta em separador de código no texto
// (colaEmCodigo); só a diferença de caixa respeita isso. Diferença além da
// caixa ("dctf web" -> "DCTFWeb") continua como era.
func precisaCanonicalizar(escrito, canonica string, colado bool) bool {
	if diferePorMaisQueCaixa(escrito, canonica) {
		return true
	}
	e := strings.TrimSpace(escrito)
	return grafiaPropria(canonica) && e != strings.TrimSpace(canonica) && temMaiuscula(e) && !colado
}

// temMaiuscula reporta se s tem alguma letra maiúscula.
func temMaiuscula(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// colaEmCodigo reporta se o trecho [start,end) de runes, a palavra ou frase
// que o dicionário casou, encosta num separador de código, sem espaço no meio:
// o trecho está dentro de URL, caminho, e-mail, pacote, comando ou hashtag, e
// a caixa dele não é a de uma frase.
//
//	"gov.br/Esocial/pt-br"    "Esocial@empresa.com"    "Typescript@5.4"
//	"node_modules/Typescript" "C:\Esocial\x"           "#Esocial"    "Esocial_v2"
//
// "/", "\", "@" e "#" colam sempre. "." ":" "_" e "-" só colam quando há
// letra ou dígito do outro lado (a vírgula e o ponto final de uma frase não
// são cola: "o ESOCIAL." e "ESOCIAL: caiu" seguem normais).
func colaEmCodigo(runes []rune, start, end int) bool {
	return (start > 0 && separadorDeCodigo(runes, start-1, start-2)) ||
		(end < len(runes) && separadorDeCodigo(runes, end, end+1))
}

// separadorDeCodigo reporta se runes[i] é separador de código; alem é o índice
// do outro lado do separador, que decide quando o separador é ambíguo.
func separadorDeCodigo(runes []rune, i, alem int) bool {
	switch runes[i] {
	case '/', '\\', '@', '#':
		return true
	case '.', ':', '_', '-':
		return alem >= 0 && alem < len(runes) && isWordRune(runes[alem])
	}
	return false
}

// grafiaPropria reporta se a forma canônica tem caixa mista que não é só a
// inicial maiúscula: alguma palavra dela (separada por espaço; o hífen não
// separa) tem letra maiúscula depois da primeira letra E letra minúscula.
//
//	sim: eSocial, DCTFWeb, PostgreSQL, TypeScript, EFD-Reinf, iPhone, macOS
//	não: DAS, MEI, SPED, PGDAS (toda maiúscula: muitas são palavra comum —
//	     "das", "mei" —, e a caixa delas fica com quem ditou)
//	não: Aurora, Supabase, Simples Nacional, Claude Code (só a inicial)
//	não: n8n, log, commit, webhook (nenhuma maiúscula)
//	não: SQL Server, DAS Simples (a sigla e a palavra de inicial maiúscula
//	     ficam em palavras separadas, e "das simples" é português comum)
func grafiaPropria(canonica string) bool {
	for _, palavra := range strings.Fields(canonica) {
		primeira := true
		maiusculaDepois, minuscula := false, false
		for _, r := range palavra {
			if !unicode.IsLetter(r) {
				continue
			}
			if unicode.IsLower(r) {
				minuscula = true
			}
			if !primeira && unicode.IsUpper(r) {
				maiusculaDepois = true
			}
			primeira = false
		}
		if maiusculaDepois && minuscula {
			return true
		}
	}
	return false
}
