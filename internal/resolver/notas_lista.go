package resolver

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Converter texto corrido em itens de lista (ADR-022, "Formatar: converter
// trecho", decisão 2 do Mauro: "Converter um trecho em lista é com IA, com
// guarda"; fase N5).
//
// O caminho: o trecho vai ao provedor do Polish, com a chave da pessoa, pelo
// prompt notas-lista@1 (notas_lista_prompt.go); a resposta passa pelas guardas
// de guardarItens; recusada (ou sem cliente, sem chave, sem rede, prazo,
// erro do provedor), a conversão POR REGRA assume (ItensPorRegra, a mesma de
// itensDaEnumeracao do painel: vírgula, ponto e vírgula e o último " e ";
// nunca dentro de "1,5"). Se nem a regra separa, nada muda e a tela avisa.
//
// # A garantia
//
// O modelo só decide ONDE cortar. O texto de cada item que volta para a nota
// é recortado do próprio trecho, entre a primeira e a última palavra que a
// guarda casou com aquele item. Nada que o modelo escreveu (uma vírgula a
// mais, um acento trocado, um caractere invisível) chega à nota: no pior
// caso, o corte fica num lugar ruim, e o Desfazer da tela devolve a linha.
// As guardas existem para RECUSAR a resposta que não é só um corte (e para o
// benchmark contar por quê), não para limpar o que ela trouxe.

// ComoSeparou diz de onde vieram os itens.
type ComoSeparou string

const (
	// SeparouComIA: a resposta do modelo passou pelas guardas.
	SeparouComIA ComoSeparou = "ia"
	// SeparouPorRegra: a IA não valeu (ou não foi chamada) e a regra separou.
	SeparouPorRegra ComoSeparou = "regra"
	// SeparouNenhum: nem a IA nem a regra acharam itens. O texto fica como está.
	SeparouNenhum ComoSeparou = "nenhum"
)

// MotivoDaLista diz por que a IA não valeu num trecho. São os MotivoFallback do
// Polish (prazo, sem_chave, numeros, palavras...) mais os dois que só a lista
// tem. Tipo à parte, e não MotivoFallback, porque os do Polish viram frase na
// tela de Configurações ("o último ditado saiu sem o Polish"), e estes não
// falam de ditado nenhum. Como o MotivoFallback, é só o enum: pode ir ao log.
type MotivoDaLista string

const (
	// ListaUmItem: o modelo devolveu um item só (ou nenhum): para ele, o
	// trecho não é uma lista. Não é defeito do modelo; a regra ainda tenta.
	ListaUmItem MotivoDaLista = "um_item"
	// ListaItens: um item sem palavra, com quebra de linha, ou itens demais.
	ListaItens MotivoDaLista = "itens"
	// ListaMultilinha: o trecho tem mais de uma linha; nem vai ao modelo (o
	// painel só manda uma linha, e a API local recusa antes).
	ListaMultilinha MotivoDaLista = "multilinha"
)

// motivoDaLista converte um MotivoFallback do Polish.
func motivoDaLista(m MotivoFallback) MotivoDaLista { return MotivoDaLista(m) }

const (
	// NotasListaMaxCaracteres é o maior trecho que vai ao provedor. Uma linha
	// de nota com mais que isto não é uma enumeração de mercado; a API local
	// recusa antes (400).
	NotasListaMaxCaracteres = 2000
	// notasListaMaxItens limita a resposta: mais itens que isto é anomalia.
	notasListaMaxItens = 100
	// notasListaMinPalavras: com menos palavras não há o que separar, e a
	// chamada nem acontece.
	notasListaMinPalavras = 2
)

// ResultadoDaLista é o que SepararItens devolve. Itens é vazio só em
// SeparouNenhum. Motivo é MotivoNenhum ("") quando a IA valeu; com
// SeparouPorRegra ou SeparouNenhum, é por que a IA não valeu.
type ResultadoDaLista struct {
	Itens  []string
	Como   ComoSeparou
	Motivo MotivoDaLista
}

// SepararItens converte um trecho de UMA linha em itens: com o cliente (o do
// Polish da pessoa), pela IA com as guardas; sem ele, ou quando a IA não vale,
// pela regra. Nunca devolve erro: o pior caso é SeparouNenhum, com o texto
// intacto do lado de quem chamou.
//
// O prazo é o do ctx de quem chama. O trecho NUNCA vai a log: quem loga um
// resultado leva Como e Motivo, nunca Itens.
func SepararItens(ctx context.Context, cliente PolishClient, trecho string) ResultadoDaLista {
	trecho = strings.TrimSpace(trecho)
	if strings.ContainsAny(trecho, "\r\n") {
		return ResultadoDaLista{Como: SeparouNenhum, Motivo: ListaMultilinha}
	}
	motivo := motivoDaLista(MotivoSemCliente)
	switch {
	case cliente == nil:
	case len(palavrasNormalizadas(trecho)) < notasListaMinPalavras:
		motivo = motivoDaLista(MotivoCurto)
	default:
		bruto, err := cliente.Polish(ctx, PolishRequest{
			Text:       trecho,
			Instrucoes: NotasListaPrompt,
			Mensagem:   NotasListaMensagemUsuario(trecho),
			Schema:     NotasListaSchemaSaida(),
			SchemaNome: NotasListaSchemaNome,
			PromptRef:  NotasListaPromptRef,
		})
		if err != nil {
			motivo = motivoDaLista(motivoDoErro(ctx, err))
			break
		}
		itens, m := GuardarItens(trecho, bruto)
		if m == "" {
			return ResultadoDaLista{Itens: itens, Como: SeparouComIA}
		}
		motivo = m
	}
	if itens := ItensPorRegra(trecho, 2); itens != nil {
		return ResultadoDaLista{Itens: itens, Como: SeparouPorRegra, Motivo: motivo}
	}
	return ResultadoDaLista{Como: SeparouNenhum, Motivo: motivo}
}

// ---------------------------------------------------------------------
// As guardas da resposta
// ---------------------------------------------------------------------

// palavraNoTrecho é uma palavra com a posição (em bytes) do campo inteiro que
// a contém: o campo é o pedaço entre espaços, com a pontuação colada ("R$",
// "6%", "carne,"). É de onde o texto do item é recortado.
type palavraNoTrecho struct {
	palavra string // minúscula, sem a pontuação das bordas
	bruta   string // a grafia original, sem a pontuação das bordas
	ini     int    // começo do campo
	fim     int    // fim do campo
}

// palavrasComPosicao parte s como tokenizar (campos entre espaços, a
// pontuação das bordas aparada; o campo só de pontuação não é palavra), sem
// tirar marcador de lista, e guarda onde cada campo está.
func palavrasComPosicao(s string) []palavraNoTrecho {
	var ps []palavraNoTrecho
	i := 0
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			i += n
			continue
		}
		ini := i
		for i < len(s) {
			r, n := utf8.DecodeRuneInString(s[i:])
			if unicode.IsSpace(r) {
				break
			}
			i += n
		}
		runas := []rune(s[ini:i])
		a, b := 0, len(runas)
		for a < b && !isWordRune(runas[a]) {
			a++
		}
		for b > a && !isWordRune(runas[b-1]) {
			b--
		}
		if a == b {
			continue
		}
		bruta := string(runas[a:b])
		ps = append(ps, palavraNoTrecho{palavra: strings.ToLower(bruta), bruta: bruta, ini: ini, fim: i})
	}
	return ps
}

// conector é a única palavra que a resposta pode deixar de fora: o "e" logo
// antes de um item que não é o primeiro.
const conector = "e"

// GuardarItens confere a resposta do modelo para o trecho e devolve os itens
// RECORTADOS DO TRECHO, ou o motivo da recusa. Na ordem:
//
//  1. json: a resposta é {"itens": [string, ...]} (campo extra é ignorado,
//     como no Polish); nenhum item tem caractere proibido (controle,
//     invisível, U+FFFD: ehCaractereProibido).
//  2. um_item: menos de dois itens.
//  3. itens: item sem palavra, item com quebra de linha, ou mais de 100.
//  4. formato: item que abre com marcador de lista ("- ", "1. ", "• "), ou a
//     resposta, com os itens um por linha, com o que formatoDaLista recusa
//     (preâmbulo de assistente, markdown, símbolo ou JSON que o trecho não
//     tinha: o "[ ]" da lista de marcar cai aqui).
//  5. numeros: os números dos itens (lidos item a item), em ordem, são os do
//     trecho, cada um inteiro (tokensNumericos): "1,5 kg" partido em "1" e
//     "5 kg", "R$" separado do valor, "doze" virando "12" ou um valor trocado
//     reprovam.
//  6. palavras: juntando os itens na ordem, as palavras (sem caixa e sem a
//     pontuação das bordas) são as do trecho, na mesma ordem, uma a uma; a
//     ÚNICA que pode faltar é um "e" do trecho logo antes do começo de um
//     item que não é o primeiro. Palavra a mais, trocada, apagada (inclusive
//     o "e" de dentro de um item, "pão e manteiga" -> "pão manteiga"), item
//     fora de ordem e item repetido reprovam.
//  7. siglas_termos e caixa: cada palavra tem a grafia do trecho, com
//     maiúscula e acento. A única folga é a inicial da palavra que abre
//     cada item ("leite" -> "Leite"), e nunca numa sigla ("eSocial" ->
//     "ESocial" reprova). Sigla ou nome próprio do trecho com outra caixa
//     é siglas_termos; palavra comum, caixa.
//
// Passou: o texto de cada item é o trecho do começo do campo da primeira
// palavra do item ao fim do campo da última, sem a vírgula, o ponto e
// vírgula e o ponto das bordas, com a inicial maiúscula (inicialDoItem).
func GuardarItens(trecho string, bruto []byte) ([]string, MotivoDaLista) {
	var resp struct {
		Itens *[]*string `json:"itens"`
	}
	if err := json.Unmarshal(bruto, &resp); err != nil || resp.Itens == nil {
		return nil, motivoDaLista(MotivoJSON)
	}
	var itens []string
	for _, p := range *resp.Itens {
		if p == nil {
			return nil, motivoDaLista(MotivoJSON)
		}
		for _, r := range *p {
			if ehCaractereProibido(r) {
				return nil, motivoDaLista(MotivoJSON)
			}
		}
		itens = append(itens, strings.TrimSpace(*p))
	}

	if len(itens) < 2 {
		return nil, ListaUmItem
	}
	if len(itens) > notasListaMaxItens {
		return nil, ListaItens
	}
	porItem := make([][]palavraNoTrecho, len(itens))
	for k, it := range itens {
		if strings.Contains(it, "\n") {
			return nil, ListaItens
		}
		porItem[k] = palavrasComPosicao(it)
		if len(porItem[k]) == 0 {
			return nil, ListaItens
		}
	}

	junto := strings.Join(itens, "\n")
	for _, it := range itens {
		if reMarcadorLinha.MatchString(it) {
			return nil, motivoDaLista(MotivoFormato)
		}
	}
	if !formatoDaLista(trecho, junto) {
		return nil, motivoDaLista(MotivoFormato)
	}

	// Os números de cada item, item a item: juntar os itens num texto só
	// deixaria "R$" no fim de um item e "6%" no começo do outro virarem um
	// valor ("R$6%") que o trecho não tem.
	var numeros []string
	for _, it := range itens {
		numeros = append(numeros, tokensNumericos(it)...)
	}
	if !mesmosNumeros(tokensNumericos(trecho), numeros) {
		return nil, motivoDaLista(MotivoNumeros)
	}

	entrada := palavrasComPosicao(trecho)
	inicios, ok := alinharItens(entrada, porItem)
	if !ok {
		return nil, motivoDaLista(MotivoPalavras)
	}
	for k, ws := range porItem {
		for j, w := range ws {
			if m := motivoDaGrafia(entrada[inicios[k]+j].bruta, w.bruta, j == 0); m != "" {
				return nil, m
			}
		}
	}

	saida := make([]string, len(itens))
	for k, ini := range inicios {
		fim := ini + len(porItem[k]) - 1
		saida[k] = inicialDoItem(aparaSeparadores(trecho[entrada[ini].ini:entrada[fim].fim]))
	}
	return saida, ""
}

// alinharItens acha onde cada item começa no trecho (o índice da primeira
// palavra dele em entrada): as palavras do item, sem caixa, são as do trecho a
// partir dali, uma a uma; o item seguinte começa logo depois, ou depois de UM
// "e" do trecho que a resposta tirou (só antes de um item que não é o
// primeiro); e o último item acaba na última palavra do trecho. false: não há
// como.
//
// Pode haver mais de um jeito de tirar o "e" ("R$ e e duas" com o item "e
// duas": qual "e" saiu?), então a busca volta atrás quando um caminho não
// fecha. As falhas ficam guardadas por (item, posição), e a busca é
// polinomial: no máximo itens x palavras estados.
func alinharItens(entrada []palavraNoTrecho, porItem [][]palavraNoTrecho) ([]int, bool) {
	inicios := make([]int, len(porItem))
	falhou := map[[2]int]bool{}
	var casar func(k, i int) bool
	casar = func(k, i int) bool {
		if k == len(porItem) {
			return i == len(entrada)
		}
		if falhou[[2]int{k, i}] {
			return false
		}
		opcoes := []int{i}
		if k > 0 && i < len(entrada) && entrada[i].palavra == conector {
			opcoes = append(opcoes, i+1) // o "e" entre itens, que a resposta tirou
		}
		for _, de := range opcoes {
			ws := porItem[k]
			if de+len(ws) > len(entrada) {
				continue
			}
			igual := true
			for j, w := range ws {
				if entrada[de+j].palavra != w.palavra {
					igual = false
					break
				}
			}
			if igual && casar(k+1, de+len(ws)) {
				inicios[k] = de
				return true
			}
		}
		falhou[[2]int{k, i}] = true
		return false
	}
	return inicios, casar(0, 0)
}

// formatoDaLista é a parte de guardaFormato (ADR-016, guarda 4) que vale para
// a lista: preâmbulo de assistente que o trecho não tinha, "**" e "#" a mais e
// símbolo a mais (ehSimboloProibido: cerca de código, crase, emoji, "_",
// colchete, chave, "<", ">", "|"). Ficam de fora as duas que não fazem sentido
// aqui: o texto inteiro entre aspas ou parênteses (o primeiro item que abre com
// "(" e o último que fecha com ")" são um corte honesto de "— (com IPTU ...
// juros)", e o texto que volta é recortado do trecho de qualquer jeito) e a
// palavra "ditado" (o envelope do Polish; o desta lista, "itens", é palavra que
// o trecho não tem e cai na guarda de palavras, e as chaves do JSON caem nos
// símbolos).
func formatoDaLista(trecho, junto string) bool {
	pIn := semHesitacaoNoInicio(palavrasNormalizadas(trecho))
	pOut := palavrasNormalizadas(junto)
	for _, p := range preambulos {
		if comecaCom(pOut, p) && !comecaCom(pIn, p) {
			return false
		}
	}
	for _, marca := range []string{"**", "#"} {
		if strings.Count(junto, marca) > strings.Count(trecho, marca) {
			return false
		}
	}
	return !simbolosAMais(trecho, junto)
}

// mesmosNumeros compara duas sequências de números.
func mesmosNumeros(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// motivoDaGrafia confere a grafia de uma palavra que já casou sem caixa: ""
// quando é a mesma; quando difere, siglas_termos se a do trecho é sigla ou
// nome próprio, caixa no resto ("dúzias" -> "DÚZIAS"). A palavra que abre um item pode
// ganhar ou perder a inicial maiúscula (e só ela), a não ser que seja sigla.
func motivoDaGrafia(doTrecho, daResposta string, abreItem bool) MotivoDaLista {
	if doTrecho == daResposta {
		return ""
	}
	if abreItem && !ehSigla(doTrecho) && !ehSigla(daResposta) {
		rt, nt := utf8.DecodeRuneInString(doTrecho)
		rr, nr := utf8.DecodeRuneInString(daResposta)
		if doTrecho[nt:] == daResposta[nr:] && unicode.ToLower(rt) == unicode.ToLower(rr) {
			return ""
		}
	}
	if ehSigla(doTrecho) || ehNomeProprio(doTrecho) {
		return motivoDaLista(MotivoSiglasTermos)
	}
	return motivoDaLista(MotivoCaixa)
}

// aparaSeparadores tira das bordas do item o que separava os itens: espaço,
// vírgula, ponto e vírgula e, no fim, o ponto e a exclamação ("ovos." ->
// "ovos"; como a regra faz com o fim do trecho).
func aparaSeparadores(s string) string {
	s = strings.TrimLeft(s, " \t,;")
	return strings.TrimRight(s, " \t,;.!")
}

// inicialDoItem põe a primeira letra do item em maiúscula, como a regra
// (itensDaEnumeracao no painel). Não mexe quando a primeira palavra é sigla
// ("eSocial", "iPhone") nem quando o item não começa por letra minúscula.
func inicialDoItem(s string) string {
	ps := palavrasComPosicao(s)
	if len(ps) > 0 && ehSigla(ps[0].bruta) {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	if !unicode.IsLower(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// ---------------------------------------------------------------------
// A regra
// ---------------------------------------------------------------------

// reConjuncaoDaRegra: o item que começa por conjunção mostra que o texto é uma
// frase com vírgulas, não uma lista ("mas a padaria estava fechada").
var reConjuncaoDaRegra = regexp.MustCompile(`(?i)^(mas|porém|porque|pois|que|quando|se|então|depois|embora|enquanto)\b`)

// reUltimoE parte a última parte no ÚLTIMO " e " (o (.*) é guloso).
var reUltimoE = regexp.MustCompile(`^(.*\S)\s+e\s+(\S.*)$`)

// ItensPorRegra é a conversão por regra da ADR-022, a MESMA de
// itensDaEnumeracao em web/src/notas/corpo.ts (os dois lados rodam os casos
// de testdata/notas-lista/regra.json): quebra na vírgula (nunca entre dois
// dígitos, "1,5 kg"), no ponto e vírgula e no último " e ". nil quando o texto
// não é uma enumeração: sem vírgula nem ponto e vírgula, com "?" ou ":", com
// menos de minimo itens, item vazio, item de mais de cinco palavras ou que
// começa por conjunção.
func ItensPorRegra(texto string, minimo int) []string {
	t := strings.TrimRight(strings.TrimSpace(texto), ".!")
	if !strings.ContainsAny(t, ",;") || strings.ContainsAny(t, "?:") {
		return nil
	}
	var partes []string
	runas := []rune(t)
	ini := 0
	for i, r := range runas {
		corta := r == ';'
		if r == ',' {
			antesDigito := i > 0 && unicode.IsDigit(runas[i-1])
			depoisDigito := i+1 < len(runas) && unicode.IsDigit(runas[i+1])
			corta = !(antesDigito && depoisDigito)
		}
		if corta {
			partes = append(partes, strings.TrimSpace(string(runas[ini:i])))
			ini = i + 1
		}
	}
	partes = append(partes, strings.TrimSpace(string(runas[ini:])))
	if m := reUltimoE.FindStringSubmatch(partes[len(partes)-1]); m != nil {
		partes = append(partes[:len(partes)-1], m[1], m[2])
	}
	if len(partes) < minimo {
		return nil
	}
	for _, p := range partes {
		if p == "" || len(strings.Fields(p)) > 5 || reConjuncaoDaRegra.MatchString(p) {
			return nil
		}
	}
	for i, p := range partes {
		partes[i] = inicialDoItem(p)
	}
	return partes
}
