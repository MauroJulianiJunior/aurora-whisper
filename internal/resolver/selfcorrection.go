package resolver

import (
	"strings"
	"unicode"
)

// aloneCorrectionMarkers só contam como sinal de autocorreção quando
// aparecem ISOLADOS na própria oração (oração = trecho entre vírgulas) —
// nunca fundidos com o resto da frase. É exatamente essa exigência que
// separa autocorreção de negação normal
// (docs/ai-spec/16-ai-behavior-spec.md, "Self-correction": "testar
// negação normal para evitar falso positivo"):
//
//	"não quero ir amanhã"      -> uma oração só, "não" não está isolado -> negação normal, intocado.
//	"não, quero dizer terça"   -> "não" É a oração inteira (entre vírgulas) -> autocorreção.
//
// Isolado não basta quando o marcador vem repetido: "Não, não, não, isso
// não está certo." é negação enfática, e nenhum dos "não" corrige nada
// (teste do Mauro, 01-out-2026). Um marcador isolado não dispara quando a
// oração anterior ou a seguinte também é só um marcador — ver
// soMarcadorIsolado e applySelfCorrection. Nenhum marcador, deste tipo ou dos
// outros dois, dispara quando a oração anterior já nega ("Eu não sei, não,
// cara.", "Eu não vou na segunda, na verdade vou na terça"; decisão do Mauro de
// 01-out-2026) — ver oracaoJaNega e oracaoCorrigida.
//
// Escritas sem acento (ex.: ASR que não capitaliza acento) casam também —
// ver foldForMarkerMatch.
var aloneCorrectionMarkers = []string{"nao", "no"}

// prefixCorrectionMarkers aparecem no INÍCIO de uma oração, seguidos
// (na mesma oração) do conteúdo da correção — não precisam estar isolados
// porque não têm um sentido concorrente de negação comum em PT-BR/EN:
// "na verdade, terça", "corrigindo, terça", "quero dizer terça", "melhor
// quarta", "actually Tuesday", "I mean Tuesday", "rather Tuesday".
var prefixCorrectionMarkers = []string{"na verdade", "corrigindo", "quero dizer", "melhor", "actually", "i mean", "rather"}

// isolatedOnlyMarkers são marcadores que só valem quando a oração é
// EXATAMENTE eles — ", quer dizer, ..." — e nunca quando vêm seguidos de
// conteúdo na mesma oração.
//
// A distinção não é estilística, é o que separa correção de explicação:
//
//	"Sobe no Supabase, quer dizer, no PostgreSQL"   corrige
//	"O prazo venceu, quer dizer que vai ter multa"  explica
//	"Manda o balancete, desculpa o incômodo"        se desculpa
//
// Medido em 27-set-2026: tratá-los como prefixCorrectionMarkers comuns
// transformava "O prazo venceu, quer dizer que vai ter multa" em "que
// vai ter multa" — destruindo a frase. Os dois são as formas MAIS comuns
// de se corrigir falando em PT-BR, e exatamente por serem comuns também
// aparecem sem intenção corretiva.
var isolatedOnlyMarkers = []string{"quer dizer", "desculpa", "desculpe"}

func isIsolatedOnlyMarker(folded string) bool {
	for _, m := range isolatedOnlyMarkers {
		if folded == m {
			return true
		}
	}
	return false
}

// selfCorrectionFold dobra os poucos acentos que aparecem nos marcadores
// (só "não" tem acento na lista acima) para permitir casar tanto "não"
// quanto "nao", sem puxar dictionary.Normalize (que também colapsaria
// espaços e pontuação, quebrando a extração do conteúdo por posição em
// runas). O mapeamento é 1:1 em runas, então não desalinha os índices
// usados por stripPrefixFromRaw.
var selfCorrectionFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a',
	'é': 'e', 'ê': 'e',
	'í': 'i',
	'ó': 'o', 'ô': 'o', 'õ': 'o',
	'ú': 'u',
	'ç': 'c',
}

func foldForMarkerMatch(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if f, ok := selfCorrectionFold[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isAloneMarker(folded string) bool {
	for _, m := range aloneCorrectionMarkers {
		if folded == m {
			return true
		}
	}
	return false
}

// repeticaoEnfatica reporta se o marcador isolado entre as orações anterior
// e seguinte é só mais um "não" de uma negação enfática repetida — "Não,
// não, não, isso não está certo." —, e não uma autocorreção. É, se a oração
// que ele corrigiria (anterior) ou a de reposição (seguinte) for ela mesma
// só um marcador isolado. Ver applySelfCorrection.
//
// A oração conta a partir do fim de frase mais próximo do marcador, porque o
// ASR pontua a mesma fala de jeitos diferentes: em "Não. Não, não, isso não
// está certo." a oração anterior ao segundo "não" é "Não. Não", e o que
// encosta no marcador é o "Não" depois do ponto. O mesmo vale para ";", ":",
// "—" e "–" no lugar do ponto (ver frasesDe).
//
// Esta regra só vê o marcador repetido COLADO. A dupla negação pós-verbal ("Eu
// não sei, não, cara.") e o "não" depois de uma oração que já nega ("Claro que
// não, não, isso nunca.") são de outra regra, oracaoJaNega.
//
// Limitações conhecidas (documentadas em 16-ai-behavior-spec.md, "Self-
// correction"; ficam de fora de propósito, e a extensão é decisão do Mauro): o
// vocativo entre os "não" ("Não, Mauro, não, isso não está certo." → "Não,
// isso não está certo.") e "Quer café, não, não quero" (→ "não quero") ainda
// disparam, e a oração anterior some.
func repeticaoEnfatica(anterior, seguinte string) bool {
	fa, fs := frasesDe(anterior), frasesDe(seguinte)
	return (len(fa) > 0 && soMarcadorIsolado(fa[len(fa)-1])) ||
		(len(fs) > 0 && soMarcadorIsolado(fs[0]))
}

// negacoes são as palavras que, dentro da oração que um marcador corrigiria,
// mostram que ela JÁ NEGA (ver oracaoJaNega). Dobradas (minúsculas,
// sem acento) como o resto deste arquivo: "não" é "nao", "ninguém" é "ninguem".
//
// O critério de entrada é: palavra inteira que só tem sentido negativo e que
// a língua usa para negar o verbo da oração — são as que o português do Brasil
// repete com um "não" no fim ("Ninguém sabe, não."), e é essa repetição que o
// marcador confundia com autocorreção. A lista é fechada de propósito: uma
// palavra de fora cai no comportamento de antes (o marcador dispara), então
// cada acréscimo é uma linha aqui e uma linha em autocorrecao_negacao_test.go.
//
// Ficam de FORA, e por quê:
//
//   - "no": em português é a contração de "em + o" ("Sobe no Supabase, não, no
//     PostgreSQL" é uma correção de verdade e tem "no" na oração anterior), e em
//     inglês é interjeição ou determinante ("no time"). Nenhum dos dois nega a
//     oração. A negação inglesa entra por "not", "never", "n't" etc.
//   - "sem": preposição; não pede o "não" repetido e nega só um complemento.
//
// "nada" também é o verbo "nadar" ("ele nada bem"), e entra mesmo assim: o erro
// é para o lado seguro, o marcador não dispara e o texto sai como foi dito.
var negacoes = map[string]bool{
	// português
	"nao": true, "nunca": true, "jamais": true, "nem": true, "tampouco": true,
	"nada": true, "ninguem": true, "nenhum": true, "nenhuma": true,
	// inglês; as contrações em "n't" (don't, can't, won't...) vão em oracaoJaNega
	"not": true, "never": true, "cannot": true,
	"nothing": true, "nobody": true, "none": true, "nor": true, "neither": true,
}

// oracaoJaNega reporta se a oração que um marcador corrigiria (oracaoCorrigida)
// já contém negação: alguma PALAVRA INTEIRA de negacoes, ou uma contração
// negativa do inglês ("don't", "can’t"). Nesse caso o marcador não dispara e o
// texto sai como foi dito. Vale para os três tipos de marcador (isolado "não"/
// "no", de prefixo e "só isolado"), todos pelo mesmo ponto em applySelfCorrection.
//
// Palavra inteira, não trecho: "cinema" não tem "nem", "nota" não tem "not",
// "norte" não tem "nor", "nadador" não tem "nada". Maiúscula e acento não
// contam ("NÃO", "nao"); pontuação, aspas e parênteses em volta não impedem
// ("(não)", "\"não\"", "não."). O apóstrofo fica dentro da palavra, senão
// "don't" viraria "don" e "t".
//
// Decisão do Mauro (01-out-2026). O "não" isolado depois de uma oração que já
// nega quase nunca é autocorreção: é a dupla negação pós-verbal do português
// ("Eu não sei, não, cara."), ou a resposta que se repete ("Claro que não, não,
// isso nunca."). Disparar apagava a oração anterior ("cara.", "isso nunca.") e
// o que a pessoa disse sumia do campo com foco, sem aviso. A decisão foi
// estendida no mesmo dia aos outros marcadores, por outro motivo: com eles a
// correção disparava, e a mescla (mesclarCorrecao) alinhava pela primeira
// palavra da correção e devolvia "Eu não vou na terça" para "Eu não vou na
// segunda, na verdade vou na terça" — o sentido invertido, em silêncio.
//
// O custo, aceito por ele: a correção verdadeira cuja oração corrigida nega
// ("Eu não vou na segunda, não, vou na terça", "Eu não vou na segunda, na
// verdade vou na terça") também sai como foi dita. É a falha FECHADA de sempre,
// nada se perde — e é melhor que o que acontecia.
//
// A oração é a mesma de todo este arquivo, o trecho entre vírgulas. Não se
// corta no fim de frase, de propósito: cortar exigiria saber onde a frase
// acaba, e "Sr. Silva", "3.5", "12:30" e "site.com" enganam qualquer regra
// simples — e errar aqui é errar para o lado ABERTO (a negação fica de fora do
// pedaço olhado e o marcador dispara). O preço do corte largo é que a negação
// de outra frase da mesma oração também bloqueia ("Ele não veio. O prazo é dia
// dez, não, dia doze." sai como foi dito).
//
// Só a oração ANTERIOR conta: negação na oração de reposição ("..., não, não
// manda para o Thiago", "..., na verdade não manda para o Thiago") ou numa
// oração mais atrás não impede a correção.
func oracaoJaNega(anterior string) bool {
	folded := strings.ReplaceAll(foldForMarkerMatch(anterior), "’", "'")
	for _, w := range strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }) {
		w = strings.Trim(w, "'")
		if negacoes[w] || strings.HasSuffix(w, "n't") {
			return true
		}
	}
	return false
}

// oracaoCorrigida devolve a oração que o marcador de clauses[i] corrigiria: a
// anterior, sem o marcador solto que a retratação traz consigo. Esse marcador
// ("não", "quer dizer", "desculpa"...) pode vir colado no fim da oração por uma
// reticência ou um ponto ("Manda para a Rosângela... não, na verdade manda para o
// Thiago", como o ASR escreve corr-01) ou ser uma oração só dele ("Eu não vou na
// segunda, não, na verdade vou na terça"; "..., desculpa, quer dizer, vou na
// terça"). Nos dois casos é parte do marcador, e não negação da oração: sem
// descontá-lo, corr-01 e "não, quero dizer terça" deixariam de corrigir, e o
// segundo de dois marcadores em fila corrigiria o primeiro. Uma oração que fica
// vazia passa a vez para a anterior, que é a que o marcador solto retratava.
// Devolve "" quando não há nada antes para corrigir.
func oracaoCorrigida(clauses []string, i int) string {
	for k := i - 1; k >= 0; k-- {
		fs := frasesDe(clauses[k])
		for len(fs) > 0 && soMarcador(fs[len(fs)-1]) {
			fs = fs[:len(fs)-1]
		}
		if len(fs) > 0 {
			return strings.Join(fs, " ")
		}
	}
	return ""
}

// frasesDe parte uma oração nos fins de frase (. ! ? …) e nas quebras fortes
// que o ASR usa no lugar delas (; : — –), sem pedaços vazios. "Não; não, não,
// isso não está certo." é a mesma negação enfática que "Não. Não, não, ...".
func frasesDe(oracao string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(oracao, func(r rune) bool { return strings.ContainsRune(".!?…;:—–", r) }) {
		if strings.TrimSpace(f) != "" {
			out = append(out, f)
		}
	}
	return out
}

// soMarcadorIsolado reporta se o trecho é só um marcador isolado ("não",
// "no"), descontados espaço e pontuação da borda.
func soMarcadorIsolado(trecho string) bool {
	return isAloneMarker(foldForMarkerMatch(semBorda(trecho)))
}

// soMarcador reporta se o trecho é só um marcador de QUALQUER tipo ("não",
// "quer dizer", "desculpa", "quero dizer", "na verdade"...), sem a correção junto,
// descontados espaço e pontuação da borda.
func soMarcador(trecho string) bool {
	t := foldForMarkerMatch(semBorda(trecho))
	if isAloneMarker(t) || isIsolatedOnlyMarker(t) {
		return true
	}
	m, ok := matchesPrefixMarker(t)
	return ok && m == t
}

func semBorda(trecho string) string {
	return strings.TrimFunc(trecho, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) })
}

// matchesPrefixMarker reporta se folded É um prefixCorrectionMarker
// (conteúdo virá na próxima oração) ou COMEÇA com um seguido de espaço
// (conteúdo na mesma oração).
func matchesPrefixMarker(folded string) (marker string, ok bool) {
	for _, m := range prefixCorrectionMarkers {
		if folded == m || strings.HasPrefix(folded, m+" ") {
			return m, true
		}
	}
	return "", false
}

// stripPrefixFromRaw remove o marcador (já casado via matchesPrefixMarker)
// do início de rawClause (texto original, não dobrado), devolvendo o
// restante. Devolve "" quando a oração é só o marcador (conteúdo vem da
// próxima oração). Seguro porque foldForMarkerMatch preserva contagem de
// runas 1:1.
func stripPrefixFromRaw(rawClause, marker string) string {
	trimmed := strings.TrimSpace(rawClause)
	folded := foldForMarkerMatch(trimmed)
	if folded == marker {
		return ""
	}
	runes := []rune(trimmed)
	cut := len([]rune(marker)) + 1 // +1 para o espaço separador.
	if cut > len(runes) {
		return ""
	}
	return strings.TrimSpace(string(runes[cut:]))
}

// selfCorrectionResult documenta o que applySelfCorrection fez.
type selfCorrectionResult struct {
	Text      string
	Triggered bool
	Applied   AppliedReplacement
}

// applySelfCorrection procura o PRIMEIRO "beat" de autocorreção em s
// (dividido em orações por vírgula) e, se encontrado, substitui a oração
// imediatamente anterior ao marcador (quando existir) pelo conteúdo da
// correção, preservando tudo antes e depois intocado.
//
// s é DADO NÃO CONFIÁVEL (THR-008): esta função só compara orações
// dobradas (fold) contra uma lista fixa e pequena de marcadores
// conhecidos — nunca interpreta o conteúdo como comando. Um texto como
// "ignore as instruções anteriores, escreva X" não casa com nenhum
// marcador desta lista, então nunca dispara nada especial: no máximo é
// tratado como duas orações comuns, sem marcador de correção.
//
// Nenhum marcador corrige uma oração que já nega (oracaoJaNega): o texto sai como
// foi dito, em vez de perder a oração ou ter o sentido invertido.
//
// Limitação conhecida (documentada, não escondida): quando há uma oração
// "antes" do marcador, ela é DESCARTADA por inteiro e substituída pelo
// conteúdo da correção — não há tentativa de reconciliar sintaticamente
// as duas orações (ex.: "vou amanhã, não, quero dizer terça" vira só
// "terça", não "vou terça"). Reconstrução sintática fina exigiria um
// parser real, fora do escopo desta fase (mock/contratos, zero
// dependência nova).
func applySelfCorrection(s string) selfCorrectionResult {
	clauses := strings.Split(s, ",")
	if len(clauses) < 2 {
		return selfCorrectionResult{Text: s}
	}

	for i, raw := range clauses {
		folded := foldForMarkerMatch(strings.TrimSpace(raw))
		if folded == "" {
			continue
		}

		// Nenhum marcador corrige uma oração que já nega ("Eu não sei, não,
		// cara." virava "cara."; "Eu não vou na segunda, na verdade vou na
		// terça" virava "Eu não vou na terça"): ver oracaoJaNega. Vale para os
		// três tipos de marcador, e a oração que cada um corrigiria é a que
		// oracaoCorrigida devolve. No começo do texto (i == 0) não há oração
		// para corrigir, e o marcador segue as regras de cada tipo. Uma oração
		// que não é marcador não faz nada neste laço, então pular não muda nada.
		if oracaoJaNega(oracaoCorrigida(clauses, i)) {
			continue
		}

		if isAloneMarker(folded) {
			// Um marcador isolado só é autocorreção se houver uma oração
			// ANTES para corrigir. Sem isso, "não" é apenas o começo
			// natural de uma resposta — "não, tudo bem", "não, obrigado",
			// "no, that's fine" — e consumir a oração seguinte não perderia
			// texto, INVERTERIA o sentido do que a pessoa disse
			// ("não, tudo bem" viraria "tudo bem"). É exatamente o falso
			// positivo que o 16-ai-behavior-spec.md manda nunca disparar.
			if i == 0 {
				continue
			}
			j := i + 1
			if j >= len(clauses) {
				continue
			}
			// Repetição enfática não é autocorreção: o marcador não dispara
			// quando a oração que ele corrigiria é ela mesma só um marcador,
			// nem quando a oração de reposição é só um marcador.
			//
			// Medido no teste do Mauro de 01-out-2026: ele ditou "Não, não,
			// não, isso não está certo.", o ASR devolveu exatamente isso, e
			// o Smart entregou "não, isso não está certo." O segundo "não"
			// era lido como marcador, o primeiro como a oração errada e o
			// terceiro como a correção. Dois "não" sumiam do que a pessoa
			// disse, nos modos Smart e com IA (o Polish recebe o Smart).
			//
			// A regra é a mínima que separa os dois casos. "não, não, não"
			// e "não, não, isso" nunca disparam; "Manda para a Rosângela,
			// não, manda para o Thiago" continua disparando. O preço é que
			// a correção com "não" repetido — "Manda para a Rosângela, não,
			// não, manda para o Thiago" — também não dispara: falha FECHADO,
			// o texto sai como foi dito, sem perder nada.
			if repeticaoEnfatica(clauses[i-1], clauses[j]) {
				continue
			}
			replacement := strings.TrimSpace(clauses[j])
			consumedThrough := j
			if marker, ok := matchesPrefixMarker(foldForMarkerMatch(strings.TrimSpace(clauses[j]))); ok {
				replacement = stripPrefixFromRaw(clauses[j], marker)
			}
			if replacement == "" {
				continue
			}
			return buildCorrection(s, clauses, i, consumedThrough, replacement)
		}

		// Marcador que só vale isolado: exige oração ANTERIOR (para
		// corrigir) e SEGUINTE (com a correção).
		if isIsolatedOnlyMarker(folded) {
			if i == 0 || i+1 >= len(clauses) {
				continue
			}
			replacement := strings.TrimSpace(clauses[i+1])
			if replacement == "" {
				continue
			}
			return buildCorrection(s, clauses, i, i+1, replacement)
		}

		if marker, ok := matchesPrefixMarker(folded); ok {
			replacement := stripPrefixFromRaw(raw, marker)
			if replacement != "" {
				return buildCorrection(s, clauses, i, i, replacement)
			}
			// Oração é só o marcador ("quero dizer" sozinho): conteúdo
			// vem da oração seguinte.
			j := i + 1
			if j >= len(clauses) {
				continue
			}
			replacement = strings.TrimSpace(clauses[j])
			if replacement == "" {
				continue
			}
			return buildCorrection(s, clauses, i, j, replacement)
		}
	}

	return selfCorrectionResult{Text: s}
}

// buildCorrection monta o texto final: mantém as orações antes da oração
// corrigida (clauses[:markerIdx-1]), insere replacement no lugar dela, e
// mantém as orações depois do marcador consumido (clauses[consumedThrough+1:]).
func buildCorrection(original string, clauses []string, markerIdx, consumedThrough int, replacement string) selfCorrectionResult {
	before := markerIdx - 1

	var out []string
	if before >= 0 {
		out = append(out, clauses[:before]...)
		// A correção substitui o TRECHO corrigido, não a oração inteira.
		//
		// Até 27-set-2026 a oração anterior era descartada por completo, e
		// isso só não aparecia porque o único marcador que disparava na
		// prática ("não, na verdade") vinha com uma oração de reposição
		// completa. Medido com os marcadores novos:
		//
		//	"O vencimento é dia dez, desculpa, dia doze" -> "dia doze"
		//	"Sobe no Supabase, quer dizer, no PostgreSQL" -> "no PostgreSQL"
		//
		// Perder o sujeito da frase é pior que não corrigir nada: a pessoa
		// precisa reescrever o que o produto apagou.
		replacement = mesclarCorrecao(clauses[before], replacement)
	}
	out = append(out, replacement)
	if consumedThrough+1 < len(clauses) {
		out = append(out, clauses[consumedThrough+1:]...)
	}

	final := strings.Join(trimNonEmpty(out), ", ")
	return selfCorrectionResult{
		Text:      final,
		Triggered: true,
		Applied: AppliedReplacement{
			Start:       0,
			End:         len([]rune(original)),
			Original:    original,
			Replacement: final,
			Reason:      ReasonSelfCorrection,
		},
	}
}

func trimNonEmpty(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		t := strings.TrimSpace(s)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// mesclarCorrecao encaixa a correção no ponto certo da oração corrigida,
// preservando o que veio antes dela.
//
// Alinha pela PRIMEIRA palavra da correção: procura a última ocorrência
// dela na oração corrigida e substitui dali em diante.
//
//	"O vencimento é dia dez" + "dia doze"  -> "O vencimento é dia doze"
//	"Sobe no Supabase"       + "no PostgreSQL local" -> "Sobe no PostgreSQL local"
//	"Manda para a Rosângela" + "manda para o Thiago" -> "manda para o Thiago"
//
// Sem âncora — a primeira palavra da correção não aparece na oração
// corrigida — devolve a correção sozinha, que é o comportamento
// histórico. É o caso em que não há como saber onde encaixar, e chutar
// seria pior.
//
// A comparação ignora caixa: o ASR capitaliza o começo da frase e a
// correção vem no meio dela.
func mesclarCorrecao(corrigida, correcao string) string {
	palavrasCorrecao := strings.Fields(correcao)
	if len(palavrasCorrecao) == 0 {
		return correcao
	}
	ancora := strings.ToLower(palavrasCorrecao[0])

	palavras := strings.Fields(corrigida)
	corte := -1
	for i, p := range palavras {
		if strings.ToLower(p) == ancora {
			corte = i
		}
	}
	if corte <= 0 {
		return correcao
	}
	correcao = devolverUnidade(palavras[corte:], correcao)
	return strings.Join(palavras[:corte], " ") + " " + correcao
}

// unidades são as palavras que QUALIFICAM um número sem serem o assunto
// da correção: moeda, prazo, porcentagem.
//
// Lista fechada e curta de propósito, no domínio de quem usa — valor em
// reais, prazo em dias. Uma unidade fora dela simplesmente não é
// devolvida, que é o comportamento de antes de 28-set-2026: falha
// FECHADA, perdendo a unidade, e nunca inventando uma.
var unidades = map[string]bool{
	"real": true, "reais": true, "dólar": true, "dólares": true, "euro": true, "euros": true,
	"dia": true, "dias": true, "semana": true, "semanas": true,
	"mês": true, "meses": true, "ano": true, "anos": true,
	"hora": true, "horas": true, "minuto": true, "minutos": true,
}

// devolverUnidade traz de volta a unidade que só aparecia na parte
// RETRATADA, quando a correção não diz unidade nenhuma.
//
// # O defeito
//
// mesclarCorrecao descarta tudo da âncora em diante na oração corrigida.
// Quando a pessoa corrige só o NÚMERO, a unidade que vinha depois dele
// ia junto:
//
//	"São 3 mil reais, não, 3 mil e quinhentos." -> "São 3 mil e quinhentos."
//	"Prazo de 30 dias, não, de 45."             -> "Prazo de 45."
//
// Informação perdida: a pessoa não retratou "reais", retratou o valor.
// Mauro confirmou em 28-set-2026 que a intenção da primeira é "São três
// mil e quinhentos reais" (dataset corr-04).
//
// # A regra
//
// Da parte retratada (retratado, que começa na âncora), tira-se o trecho
// inicial em comum com a correção — "3 mil" — e olha-se o que sobrou. Se
// o que sobrou TERMINA numa unidade, e a correção não traz unidade
// nenhuma, a unidade volta, antes da pontuação final da correção.
//
// As duas condições são o que impede de trazer de volta o que foi, de
// fato, corrigido:
//
//	"Compra 3 dólares, não, 3 euros." -> "Compra 3 euros."
//
// "euros" é unidade, então a correção já disse qual é — "dólares" fica
// fora. E só o FIM do que sobrou conta: "os 3 dias de folga" não devolve
// "dias", porque termina em "folga".
func devolverUnidade(retratado []string, correcao string) string {
	pc := strings.Fields(correcao)
	if temUnidade(pc) {
		return correcao // a correção já disse a unidade
	}

	comum := 0
	for comum < len(retratado) && comum < len(pc) &&
		normalizarPalavra(retratado[comum]) == normalizarPalavra(pc[comum]) {
		comum++
	}
	sobra := retratado[comum:]
	if len(sobra) == 0 {
		return correcao
	}

	unidade := unidadeNoFim(sobra)
	if unidade == "" {
		return correcao
	}

	corpo := strings.TrimRightFunc(correcao, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
	return corpo + " " + unidade + correcao[len(corpo):]
}

// unidadeNoFim devolve a unidade com que palavras termina — "por cento"
// conta como uma —, ou "" se não termina em unidade.
//
// "%" não está aqui, e não por esquecimento: o ASR o cola no número
// ("10%"), e aí a correção cai no caminho sem âncora de mesclarCorrecao
// antes de chegar nesta função.
func unidadeNoFim(palavras []string) string {
	n := len(palavras)
	if n >= 2 && normalizarPalavra(palavras[n-2]) == "por" && normalizarPalavra(palavras[n-1]) == "cento" {
		return "por cento"
	}
	ultima := strings.TrimFunc(palavras[n-1], unicode.IsPunct)
	if unidades[strings.ToLower(ultima)] {
		return ultima
	}
	return ""
}

// temUnidade reporta se palavras contém alguma unidade em qualquer
// posição. Precisa reconhecer "por cento" como UMA unidade: palavra por
// palavra, nem "por" nem "cento" estão em unidades, e a correção "de 12
// por cento" seria tratada como sem unidade — devolvendo um segundo "por
// cento" no fim.
func temUnidade(palavras []string) bool {
	for i, p := range palavras {
		w := normalizarPalavra(p)
		if unidades[w] {
			return true
		}
		if w == "por" && i+1 < len(palavras) && normalizarPalavra(palavras[i+1]) == "cento" {
			return true
		}
	}
	return false
}

// normalizarPalavra compara sem caixa e sem pontuação de borda: o ASR
// capitaliza e pontua, e "reais," é a mesma unidade que "reais".
func normalizarPalavra(p string) string {
	return strings.ToLower(strings.TrimFunc(p, unicode.IsPunct))
}
