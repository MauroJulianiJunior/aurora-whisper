package learning

import (
	"strings"
	"unicode"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Limiares de elegibilidade a alias — ver doc.go, seção "Critério de
// 'pequena e mapeável'", para a justificativa completa.
const (
	// maxAliasChars é o teto de comprimento (em runes) de before/after
	// para que a edição seja elegível a alias.
	maxAliasChars = 30
	// maxAliasWords é o teto de palavras (tokens separados por espaço)
	// de before/after para que a edição seja elegível a alias.
	maxAliasWords = 4

	// SimilaridadeSuspeita é o ponto abaixo do qual os dois termos são
	// distantes o bastante para merecer um aviso a quem confirma — não
	// uma recusa. Ver Suggestion.Similaridade.
	SimilaridadeSuspeita = 0.5
)

// sentenceTerminators são os sinais de pontuação que tratamos como prova
// de que o texto é uma frase corrida, não um termo/expressão curta (ver
// doc.go).
const sentenceTerminators = ".!?;"

// Suggestion é o resultado de Classify: uma edição real (não vazia, não
// idêntica, não só espaço/pontuação) entre o texto original e o editado,
// pronta para ser mostrada ao usuário antes de qualquer confirmação.
type Suggestion struct {
	// Before é o final_text original (antes da edição do usuário).
	Before string
	// After é o final_text depois da edição do usuário.
	After string

	// AliasFrom e AliasTo são o TRECHO MÍNIMO que mudou entre Before e
	// After — o par que pode virar alias de dicionário.
	//
	// Separá-los de Before/After não é detalhe: até 27-set-2026 o alias
	// usava os textos INTEIROS, e como a elegibilidade limita por número
	// de palavras, uma correção de UMA palavra dentro de uma frase longa
	// nunca virava alias. O Before estourava o teto e a edição virava só
	// uma Correction.
	//
	// Isso esvaziava o fluxo do 07-flows.md na prática: editar o
	// final_text no histórico — que é o caminho previsto para ensinar o
	// dicionário — quase nunca produzia alias. Medido em
	// docs/sprint-5-portao-de-entidades.md, alias aprendido e a falta
	// dele separam 95,9%% de 70,8%% de Entity Accuracy.
	//
	// Vazios quando a edição não é uma SUBSTITUIÇÃO (inserção ou remoção
	// pura): "manda o balancete" -> "manda o balancete hoje" não mapeia
	// termo nenhum em termo nenhum, e inventar um mapeamento ali seria
	// criar uma regra a partir de nada.
	AliasFrom string
	AliasTo   string

	// Similaridade é o quanto AliasFrom e AliasTo se parecem (0 a 1, a
	// mesma medida que dictionary.Search usa para candidato fonético).
	//
	// É um AVISO para quem confirma, não um veto — e essa escolha é
	// deliberada. Medido nos pares reais deste projeto:
	//
	//	Supabaese / Supabase   0,89   erro de transcrição
	//	comite    / commit     0,67   erro de transcrição
	//	amanhã... / depois...  0,45   mudança de ideia
	//	certo     / errado     0,33   mudança de ideia
	//	n oito n  / n8n        0,25   erro de transcrição (TEST-008!)
	//
	// O TEST-008 está do lado ERRADO de qualquer limiar que separe os
	// outros quatro — "oito" e "8" são a mesma fala, e nenhuma medida
	// ortográfica ou fonética disponível aqui enxerga isso. Um limiar
	// que o acomodasse deixaria "certo" -> "errado" passar junto.
	//
	// Escolher um número que coubesse nos exemplos que eu tinha na mão
	// seria ajustar a regra de segurança aos dados. A spec já tem o
	// mecanismo certo para este julgamento: confirmação explícita
	// (AC-022). Quem confirma vê o número e decide.
	Similaridade float64

	// AliasEligible indica se, além de virar uma Correction, esta edição
	// é pequena e mapeável o bastante (ver doc.go) para também poder
	// gerar um DictionaryAlias quando confirmada. Uma edição grande
	// nunca tem AliasEligible=true (07-flows.md: "edição grande nunca
	// vira alias silenciosamente").
	AliasEligible bool

	// Reason documenta, em português, por que a edição recebeu essa
	// classificação — usado em auditoria e nos testes de diff. Nunca
	// inclui o texto ditado/editado em si além do que já está em
	// Before/After (mesma regra de não vazar conteúdo em log que rege o
	// resto do projeto — AC-027/AC-028 — ainda que Suggestion viva só em
	// memória, nunca em log).
	Reason string
}

// Classify calcula o diff entre before e after e decide se há alguma
// edição real a sugerir para aprendizado, e, quando há, se ela é elegível
// a virar alias de dicionário além de uma Correction comum.
//
// ok é false (Suggestion zero-value, nada a sugerir) quando:
//   - before ou after é vazio (ou só espaço);
//   - before e after são idênticos;
//   - a única diferença entre os dois é espaçamento e/ou pontuação (as
//     mesmas letras/dígitos, na mesma ordem).
//
// Fora esses casos, ok é true e Suggestion.AliasEligible segue o critério
// documentado em doc.go. Classify nunca devolve mais de uma Suggestion
// para o mesmo par (before, after) — uma reescrita grande ainda gera só
// uma, nunca uma por trecho.
func Classify(before, after string) (Suggestion, bool) {
	before = strings.TrimSpace(before)
	after = strings.TrimSpace(after)

	if before == "" || after == "" {
		return Suggestion{}, false
	}
	if before == after {
		return Suggestion{}, false
	}
	if lettersAndDigits(before) == lettersAndDigits(after) {
		// Só espaço/pontuação mudou: as mesmas palavras continuam lá.
		return Suggestion{}, false
	}

	de, para := trechoQueMudou(strings.Fields(before), strings.Fields(after))

	sug := Suggestion{
		Before:    before,
		After:     after,
		AliasFrom: de,
		AliasTo:   para,
	}
	if de == "" || para == "" {
		// Inserção ou remoção pura: não há termo A virando termo B.
		sug.Reason = "inserção ou remoção: não mapeia um termo em outro"
		return sug, true
	}
	sug.Similaridade = dictionary.Similaridade(
		dictionary.Normalize(de), dictionary.Normalize(para))
	sug.AliasEligible, sug.Reason = classifyAliasEligibility(
		de, para, strings.Fields(de), strings.Fields(para))
	return sug, true
}

// trechoQueMudou isola o miolo diferente entre duas sequências de
// palavras, descartando o prefixo e o sufixo comuns.
//
// Granularidade de PALAVRA, e não de caractere: um alias mapeia termo em
// termo, e um diff de caractere produziria fragmentos como "mi"->"mmi"
// no meio de "comite"/"commit" — inaplicáveis como regra e perigosos
// como substituição cega.
//
// A pontuação de borda do trecho é removida porque ela pertence à FRASE,
// não ao termo: em "...antes do dia quinze." o que mudou é "quinze", e o
// ponto final é do texto.
//
// Só devolve par quando há UM trecho contíguo diferente. Duas mudanças
// separadas na mesma edição produzem um miolo que engloba o texto entre
// elas, e aí a elegibilidade por tamanho é que barra — como deve: não há
// um alias único a extrair de duas correções distintas.
func trechoQueMudou(antes, depois []string) (string, string) {
	i := 0
	for i < len(antes) && i < len(depois) && antes[i] == depois[i] {
		i++
	}
	j := 0
	for j < len(antes)-i && j < len(depois)-i &&
		antes[len(antes)-1-j] == depois[len(depois)-1-j] {
		j++
	}
	esq, dir := aparaLigacaoDeBorda(antes[i:len(antes)-j], depois[i:len(depois)-j])
	de := strings.Join(esq, " ")
	para := strings.Join(dir, " ")
	return pontuacaoDeBorda(de), pontuacaoDeBorda(para)
}

// ligacao são as palavras de classe fechada que pertencem à FRASE, não a
// um termo: artigos, preposições e as contrações entre as duas.
//
// A lista é fechada e curta de propósito. Ela só é consultada na BORDA
// de um trecho que já mudou, e só para uma palavra que existe de um lado
// só — nenhuma delas é removida do miolo, então "Ministério da Fazenda"
// e "EFD-Reinf" ficam intactos.
var ligacao = map[string]bool{
	"o": true, "a": true, "os": true, "as": true,
	"um": true, "uma": true, "uns": true, "umas": true,
	"de": true, "do": true, "da": true, "dos": true, "das": true,
	"em": true, "no": true, "na": true, "nos": true, "nas": true,
	"ao": true, "à": true, "aos": true, "às": true,
	"pelo": true, "pela": true, "pelos": true, "pelas": true,
}

// aparaLigacaoDeBorda tira do trecho as palavras de ligação que estão na
// borda e existem de um lado só.
//
// # Por que existe
//
// trechoQueMudou é um diff de prefixo/sufixo comum, e por isso engolia o
// artigo que a pessoa acrescentava JUNTO da correção do termo. Medido em
// 28-set-2026, e sistemático:
//
//	"Whisperflow"          -> "o Wispr Flow"
//	"DCTF web"             -> "a DCTFWeb"
//	"postgres"             -> "o PostgreSQL"
//	"migracao do subabase" -> "a migracao do Supabase"
//
// O artigo entrava na forma CANÔNICA. Como um alias é aplicado a todo
// ditado futuro, o resolver passaria a escrever "Abri o o Wispr Flow" —
// e sem aviso nenhum, porque a similaridade fica alta (0,58 a 0,86),
// bem acima de SimilaridadeSuspeita.
//
// Inserção PURA já era recusada ("Manda balancete" -> "Manda o
// balancete" devolve "" -> "o", inelegível). O que faltava era a
// inserção COLADA numa substituição.
//
// # Por que aparar reexpõe contexto comum
//
// Depois de tirar a palavra de um lado, os dois lados podem voltar a
// começar (ou terminar) igual — e aí aquilo também é contexto, não
// edição. Por isso o laço continua até estabilizar: o caso
// "migracao do subabase" -> "a migracao do Supabase" só chega em
// "subabase" -> "Supabase" porque "migracao" e "do" caem depois do "a".
func aparaLigacaoDeBorda(esq, dir []string) ([]string, []string) {
	// Nunca esvazia um lado: "" de um lado é o sinal de inserção/remoção
	// pura, e fabricá-lo aqui transformaria uma edição legítima num par
	// vazio.
	podeTirar := func(s []string) bool { return len(s) > 1 }

	for mudou := true; mudou; {
		mudou = false
		if len(esq) == 0 || len(dir) == 0 {
			break
		}
		switch {
		case esq[0] == dir[0]:
			esq, dir, mudou = esq[1:], dir[1:], true
		case ligacao[strings.ToLower(esq[0])] && podeTirar(esq):
			esq, mudou = esq[1:], true
		case ligacao[strings.ToLower(dir[0])] && podeTirar(dir):
			dir, mudou = dir[1:], true
		}
		if len(esq) == 0 || len(dir) == 0 {
			break
		}
		switch {
		case esq[len(esq)-1] == dir[len(dir)-1]:
			esq, dir, mudou = esq[:len(esq)-1], dir[:len(dir)-1], true
		case ligacao[strings.ToLower(esq[len(esq)-1])] && podeTirar(esq):
			esq, mudou = esq[:len(esq)-1], true
		case ligacao[strings.ToLower(dir[len(dir)-1])] && podeTirar(dir):
			dir, mudou = dir[:len(dir)-1], true
		}
	}
	return esq, dir
}

// pontuacaoDeBorda remove pontuação das pontas, preservando o miolo:
// "n8n," vira "n8n", mas "EFD-Reinf" continua inteiro.
func pontuacaoDeBorda(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
}

// classifyAliasEligibility aplica o critério de "pequena e mapeável"
// documentado em doc.go e devolve o motivo (sempre preenchido, elegível
// ou não) para auditoria/testes.
func classifyAliasEligibility(before, after string, beforeWords, afterWords []string) (bool, string) {
	beforeLen, afterLen := runeCount(before), runeCount(after)

	if beforeLen > maxAliasChars || afterLen > maxAliasChars {
		return false, "edição grande: excede o teto de caracteres para alias"
	}
	if len(beforeWords) > maxAliasWords || len(afterWords) > maxAliasWords {
		return false, "edição grande: excede o teto de palavras para alias"
	}
	if containsSentenceTerminator(before) || containsSentenceTerminator(after) {
		return false, "edição grande: contém pontuação de frase"
	}
	if isPurelyNumeric(before) || isPurelyNumeric(after) {
		// Um alias é aplicado a TODO ditado futuro. Aprender "5" → "50"
		// faria o sistema reescrever qualquer "5" que a pessoa falasse
		// depois, mudando valores sem nenhuma evidência — justamente o que
		// o docs/ai-spec/16-ai-behavior-spec.md proíbe ("mudar números sem
		// evidência").
		//
		// A edição continua virando uma Correction: ela é um fato
		// histórico sobre AQUELE ditado, e registrá-la é útil. O que fica
		// barrado é a promoção a alias, que vale para todos os próximos.
		//
		// Note que isto não atrapalha o TEST-008 ("n oito n" → "n8n"):
		// nenhum dos dois lados é puramente numérico.
		return false, "números não viram alias: seriam aplicados a todo ditado futuro"
	}
	return true, "pequena e mapeável: dentro dos tetos de caracteres/palavras e sem pontuação de frase"
}

// isPurelyNumeric reporta se s é só um número — dígitos, com os separadores
// usuais de milhar, decimal, data ou intervalo. Exige pelo menos um dígito,
// para que algo como "-" ou "/" sozinho não conte.
func isPurelyNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	hasDigit := false
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case r == '.' || r == ',' || r == '/' || r == '-' || r == ' ' || r == ':':
			// separador aceito dentro de um número
		default:
			return false
		}
	}
	return hasDigit
}

// lettersAndDigits devolve só as runes de letra/dígito de s, na ordem em
// que aparecem, preservando maiúsculas/minúsculas — usado para detectar
// uma edição que só mudou espaçamento/pontuação (as "palavras" continuam
// as mesmas).
func lettersAndDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func containsSentenceTerminator(s string) bool {
	return strings.ContainsAny(s, sentenceTerminators)
}

func runeCount(s string) int {
	return len([]rune(s))
}
