package resolver

import (
	"strings"
	"unicode"
)

// O aceite por linha (ADR-016, emenda de 24-out-2026, pedido do Mauro).
//
// O aceite por frase deixava de fora a resposta com lista ou quebra de linha:
// misturar o texto corrido do Smart numa lista do Polish quebraria a
// estrutura. Aqui a unidade é a LINHA da resposta (o item da lista, ou o
// parágrafo): a linha que passa fica como o Polish escreveu, e a que não passa
// troca só as palavras pelas do Smart. O que fica entre as linhas — a
// pontuação do fim da linha, a quebra, o marcador "1." ou "-" — é sempre o do
// Polish, então a lista continua lista.
//
// A garantia é a mesma do aceite por frase: o texto montado passa, INTEIRO,
// pelas guardas de formato, números, siglas e termos, palavras e caixa, contra
// o ditado inteiro. As licenças da lista (o "e" e o ordinal que viraram o
// marcador) são conferidas nessa conta, porque só ela vê a lista toda.
//
// Cai inteiro no Smart, como no aceite por frase: a resposta com palavra a
// mais nas bordas, a de uma linha só e a em que mais da metade das linhas
// cai. E também a que tem uma linha sem nenhuma palavra do ditado ("Qualquer
// dúvida, me avise."): é o modelo falando com a pessoa. Dentro de uma linha
// não há corte por frase: o parágrafo que não passa volta inteiro.

// ehEstrutural diz se a palavra i do ditado é das que a lista tem licença
// para tirar: o "e" do último item e as palavras estruturais (o ordinal,
// "item um", a hesitação, a pontuação falada). A licença de cada uma é
// conferida depois, no texto inteiro.
func ehEstrutural(a []string, marcas []marcaEstrutural, i int) bool {
	return a[i] == "e" || marcas[i].Classe != classeNenhuma
}

// dividirEmLinhas corta a resposta nas linhas dela e o ditado nos mesmos
// pontos. O corte vale onde a palavra que abre a linha e a que fecha a
// anterior casaram com palavras do ditado, em ordem. O que o ditado tem entre
// elas fica assim:
//   - as palavras estruturais do fim ("e", "terceiro") são a passagem de um
//     item para o outro e não ficam em linha nenhuma;
//   - as de antes delas ("do mês passado e o recibo", com "passado" apagado
//     pelo modelo) ficam com a linha de cima, que então cai;
//   - sem palavra estrutural no fim ("o extrato e você manda", com "você"
//     apagado), não se sabe de que linha a palavra é, e as duas linhas ficam
//     juntas.
//
// Cada linha é uma frase (o tipo): o trecho do ditado e o da resposta.
func dividirEmLinhas(a, b []string, tb []tokenTexto, par []int, marcas []marcaEstrutural) []frase {
	var linhas []frase
	iniA, iniB := 0, 0
	for j := 1; j < len(b); j++ {
		if !strings.Contains(tb[j].Antes, "\n") {
			continue
		}
		if par[j] < 0 || par[j-1] < 0 { // ancorar casa em ordem: par[j-1] < par[j]
			continue
		}
		passagem := par[j] // onde começam as palavras estruturais do fim
		for passagem > par[j-1]+1 && ehEstrutural(a, marcas, passagem-1) {
			passagem--
		}
		if passagem > par[j-1]+1 && passagem == par[j] {
			continue // palavra apagada sem passagem: não se sabe de que linha é
		}
		linhas = append(linhas, frase{IniA: iniA, FimA: passagem, IniB: iniB, FimB: j})
		iniA, iniB = par[j], j
	}
	return append(linhas, frase{IniA: iniA, FimA: len(a), IniB: iniB, FimB: len(b)})
}

// linhaSemAncora diz se alguma linha da resposta não tem nenhuma palavra que
// casou com o ditado.
func linhaSemAncora(tb []tokenTexto, par []int) bool {
	ancorada := true
	for j, t := range tb {
		if j > 0 && strings.Contains(t.Antes, "\n") {
			if !ancorada {
				return true
			}
			ancorada = false
		}
		if par[j] >= 0 {
			ancorada = true
		}
	}
	return !ancorada
}

// corpo devolve o texto de s da primeira palavra [ini] ao fim da última
// [fim-1], sem a pontuação e o espaço de volta.
func corpo(s string, pos []int, tokens []tokenTexto, ini, fim int) string {
	return s[pos[ini] : pos[fim-1]+len(tokens[fim-1].Bruta)]
}

// polirPorLinha é o aceite por frase da resposta com quebra de linha (ver o
// começo do arquivo). Devolve o texto montado, quantas linhas voltaram ao
// Smart e quantas a resposta tem; ok=false quando não dá.
func polirPorLinha(entrada, saida string, opts PolirOpcoes) (texto string, revertidas, total int, ok bool) {
	ta, _ := tokenizar(entrada)
	tb, _ := tokenizar(saida)
	a, b := palavrasDe(ta), palavrasDe(tb)
	posA, okA := posicoes(entrada, ta)
	posB, okB := posicoes(saida, tb)
	if !okA || !okB || len(a) == 0 || len(b) == 0 {
		return "", 0, 0, false
	}
	par, ok := ancorar(a, b)
	if !ok || acrescentaNasBordas(len(a), par) || linhaSemAncora(tb, par) {
		return "", 0, 0, false
	}
	linhas := dividirEmLinhas(a, b, tb, par, marcarEstruturais(a))
	if len(linhas) < 2 { // atalho: uma linha só é a resposta inteira, que já caiu
		return "", 0, 0, false
	}

	var montado strings.Builder
	montado.WriteString(saida[:posB[0]]) // o que abre a resposta: o marcador da primeira linha
	for k, l := range linhas {
		de := corpo(entrada, posA, ta, l.IniA, l.FimA)
		para := corpo(saida, posB, tb, l.IniB, l.FimB)
		if passaNasGuardas(de, para, opts) == MotivoNenhum {
			montado.WriteString(para)
		} else {
			if comecaComMaiuscula(para) {
				de = abrirFrase(de)
			}
			montado.WriteString(de)
			revertidas++
		}
		fim := posB[l.FimB-1] + len(tb[l.FimB-1].Bruta)
		ate := len(saida)
		if k+1 < len(linhas) {
			ate = posB[linhas[k+1].IniB]
		}
		montado.WriteString(saida[fim:ate]) // o fim da linha, a quebra e o marcador da seguinte
	}
	if revertidas*2 > len(linhas) {
		return "", 0, 0, false
	}
	texto = montado.String()
	// A conta que decide: o texto montado, inteiro, contra o ditado inteiro.
	if !guardaFormato(entrada, texto) || passaNasGuardas(entrada, texto, opts) != MotivoNenhum {
		return "", 0, 0, false
	}
	return texto, revertidas, len(linhas), true
}

// comecaComMaiuscula diz se a primeira letra do trecho é maiúscula.
func comecaComMaiuscula(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return unicode.IsUpper(r)
		}
	}
	return false
}
