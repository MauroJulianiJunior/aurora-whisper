package resolver

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// O aceite por frase (ADR-016, emenda de 19-out-2026, decisão do Mauro).
//
// O diagnóstico do histórico mostrou que, nos ditados longos, a resposta
// recusada tinha 1 a 3 pontos ruins (uma palavra apagada, uma acrescentada)
// e o resto bem organizado; o tudo ou nada jogava fora o texto inteiro. Agora,
// quando a resposta inteira não passa, cada frase dela é conferida com o
// trecho do ditado que ela cobre: a frase que passa fica como o Polish
// escreveu, a que não passa volta ao texto do Smart, palavra por palavra.
//
// A garantia das guardas não muda: o texto montado passa, INTEIRO, pelas
// mesmas guardas de números, siglas e termos, palavras e caixa que a
// resposta inteira teria de passar. A conta por frase só escolhe o que fica;
// quem decide o que chega ao campo continua sendo a conta do texto todo.
//
// Fica de fora, e cai inteiro no Smart como antes:
//   - a resposta com trecho novo (MotivoTrechoNovo): o modelo acrescentou
//     conteúdo, e isso é sinal de que ele respondeu ou obedeceu ao ditado;
//   - a resposta com quebra de linha ou lista: misturar um trecho do Smart
//     dentro de uma lista do Polish quebraria a estrutura;
//   - a resposta em que mais da metade das frases cai: aí o modelo reescreveu;
//   - a resposta de uma frase só, que é o tudo ou nada de sempre.

// sinaisDeFimDeFrase são os sinais que fecham uma frase.
const sinaisDeFimDeFrase = ".?!…"

// posicoes devolve, para cada token, o byte em que a palavra começa em s.
// As palavras aparecem em ordem, e o que fica entre elas (tokenTexto.Antes)
// não tem letra nem dígito, então a primeira ocorrência depois da anterior é
// a dela. ok=false se uma não for achada (não deve acontecer).
func posicoes(s string, tokens []tokenTexto) (pos []int, ok bool) {
	pos = make([]int, len(tokens))
	cursor := 0
	for i, t := range tokens {
		k := strings.Index(s[cursor:], t.Bruta)
		if k < 0 {
			return nil, false
		}
		pos[i] = cursor + k
		cursor = pos[i] + len(t.Bruta)
	}
	return pos, true
}

// ancorar alinha as palavras da saída às da entrada pela distância de edição
// e devolve, para cada palavra da saída, a palavra da entrada IGUAL com que
// ela casou, ou -1 (palavra trocada ou acrescentada). ok=false quando a
// tabela passaria de polishMaxCelulas.
func ancorar(a, b []string) (par []int, ok bool) {
	n, m := len(a), len(b)
	if (n+1)*(m+1) > polishMaxCelulas {
		return nil, false
	}
	largura := m + 1
	d := make([]int32, (n+1)*largura)
	for j := 0; j <= m; j++ {
		d[j] = int32(j)
	}
	for i := 1; i <= n; i++ {
		d[i*largura] = int32(i)
		for j := 1; j <= m; j++ {
			melhor := min(d[(i-1)*largura+j], d[i*largura+j-1]) + 1
			sub := d[(i-1)*largura+j-1] + 1
			if a[i-1] == b[j-1] {
				sub--
			}
			d[i*largura+j] = min(melhor, sub)
		}
	}
	par = make([]int, m)
	for j := range par {
		par[j] = -1
	}
	i, j := n, m
	for i > 0 && j > 0 {
		atual := d[i*largura+j]
		switch {
		case a[i-1] == b[j-1] && atual == d[(i-1)*largura+j-1]:
			par[j-1] = i - 1
			i--
			j--
		case atual == d[(i-1)*largura+j-1]+1:
			i--
			j--
		case atual == d[(i-1)*largura+j]+1:
			i--
		default:
			j--
		}
	}
	return par, true
}

// acrescentaNasBordas diz se a resposta tem, antes da primeira palavra que
// casou ou depois da última, mais palavras que o ditado: um prefixo ("Texto
// organizado:") ou uma frase no fim ("Dia quinze.", "Obrigado!"). É o modelo
// respondendo ou falando com a pessoa, e aí a resposta inteira cai, mesmo que
// a frase acrescentada pudesse ser descartada: a regra das bordas da guarda
// de palavras vale também aqui.
func acrescentaNasBordas(n int, par []int) bool {
	primeira, ultima := -1, -1
	for j, i := range par {
		if i < 0 {
			continue
		}
		if primeira < 0 {
			primeira = j
		}
		ultima = j
	}
	if primeira < 0 {
		return true
	}
	antesNaSaida, antesNaEntrada := primeira, par[primeira]
	depoisNaSaida, depoisNaEntrada := len(par)-1-ultima, n-1-par[ultima]
	return antesNaSaida > antesNaEntrada || depoisNaSaida > depoisNaEntrada
}

// frase é um pedaço da resposta e o trecho do ditado que ele cobre, em
// índices de palavra: entrada a[IniA:FimA], saída b[IniB:FimB].
type frase struct {
	IniA, FimA, IniB, FimB int
}

// dividirEmFrases corta a resposta nas frases dela e o ditado nos mesmos
// pontos. Um corte só vale onde as duas palavras em volta dele são as mesmas
// na entrada e na saída, em sequência (a âncora): assim o trecho do ditado de
// cada frase é exato. A frase cujo começo não tem âncora fica junto da
// anterior.
func dividirEmFrases(a, b []string, tb []tokenTexto, par []int) []frase {
	var cortes [][2]int // {palavra da entrada, palavra da saída} onde começa cada frase
	for j := 1; j < len(b); j++ {
		if !strings.ContainsAny(tb[j].Antes, sinaisDeFimDeFrase) {
			continue
		}
		if par[j] < 0 || par[j-1] < 0 || par[j-1] != par[j]-1 {
			continue
		}
		cortes = append(cortes, [2]int{par[j], j})
	}
	frases := make([]frase, 0, len(cortes)+1)
	iniA, iniB := 0, 0
	for _, c := range cortes {
		frases = append(frases, frase{IniA: iniA, FimA: c[0], IniB: iniB, FimB: c[1]})
		iniA, iniB = c[0], c[1]
	}
	return append(frases, frase{IniA: iniA, FimA: len(a), IniB: iniB, FimB: len(b)})
}

// trecho devolve o texto de s das palavras [ini, fim): do começo da palavra
// ini (do começo do texto, para a primeira) ao começo da palavra fim (ao fim
// do texto, para a última). Fica com a pontuação e o espaço que vêm depois da
// última palavra: é onde está o ponto final da frase.
func trecho(s string, pos []int, ini, fim int) string {
	de, ate := 0, len(s)
	if ini > 0 {
		de = pos[ini]
	}
	if fim < len(pos) {
		ate = pos[fim]
	}
	return s[de:ate]
}

// passaNasGuardas diz se a saída passa nas guardas que olham as palavras:
// números, siglas e termos, palavras e caixa. É a mesma sequência de Polir
// depois do formato, e devolve o primeiro motivo de recusa.
func passaNasGuardas(entrada, saida string, opts PolirOpcoes) MotivoFallback {
	if !guardaNumeros(entrada, saida) {
		return MotivoNumeros
	}
	if !guardaSiglasTermos(entrada, saida, opts.Termos) {
		return MotivoSiglasTermos
	}
	if motivo := guardaPalavras(entrada, saida, opts.maxEdicao()); motivo != MotivoNenhum {
		return motivo
	}
	if !guardaCaixa(entrada, saida) {
		return MotivoCaixa
	}
	return MotivoNenhum
}

// polirPorFrase tenta salvar, frase a frase, a resposta que não passou
// inteira (ver o começo do arquivo). Devolve o texto montado, quantas frases
// voltaram ao Smart e quantas a resposta tem; ok=false quando não dá, e aí
// fica o Smart inteiro, como antes.
func polirPorFrase(entrada, saida string, opts PolirOpcoes) (texto string, revertidas, total int, ok bool) {
	if strings.ContainsAny(entrada, "\n\r") || strings.ContainsAny(saida, "\n\r") {
		return "", 0, 0, false
	}
	ta, _ := tokenizar(entrada)
	tb, _ := tokenizar(saida)
	for _, t := range tb {
		if t.Marcador {
			return "", 0, 0, false
		}
	}
	a, b := palavrasDe(ta), palavrasDe(tb)
	posA, okA := posicoes(entrada, ta)
	posB, okB := posicoes(saida, tb)
	if !okA || !okB || len(a) == 0 || len(b) == 0 {
		return "", 0, 0, false
	}
	par, ok := ancorar(a, b)
	if !ok || acrescentaNasBordas(len(a), par) {
		return "", 0, 0, false
	}
	// Uma frase do meio que o ditado não tem não chega aqui: o corte só vale
	// com âncora dos dois lados, então todo trecho do ditado tem palavra, e a
	// frase acrescentada fica junto da vizinha, que cai.
	frases := dividirEmFrases(a, b, tb, par)
	if len(frases) < 2 { // atalho: uma frase só é a resposta inteira, que já caiu
		return "", 0, 0, false
	}

	pedacos := make([]string, len(frases))
	doSmart := make([]bool, len(frases))
	for k, f := range frases {
		de := trecho(entrada, posA, f.IniA, f.FimA)
		para := trecho(saida, posB, f.IniB, f.FimB)
		if passaNasGuardas(de, para, opts) == MotivoNenhum {
			pedacos[k] = para
			continue
		}
		pedacos[k], doSmart[k] = de, true
		revertidas++
	}
	if revertidas*2 > len(frases) {
		return "", 0, 0, false
	}

	var montado strings.Builder
	for k, p := range pedacos {
		if doSmart[k] {
			if k == 0 || !doSmart[k-1] { // abre o texto, ou vem depois de um ponto do Polish
				p = abrirFrase(p)
			}
			if k+1 < len(pedacos) && !doSmart[k+1] {
				p = fecharFrase(p)
			}
		}
		montado.WriteString(p)
	}
	texto = montado.String()
	// A conta que decide: o texto montado, inteiro, contra o ditado inteiro.
	// Nos casos conhecidos ela dá o mesmo que a soma das frases (a mutação que
	// a tira não derruba teste); fica porque é ela que garante que o que vai
	// ao campo passou nas guardas como texto, e não só em pedaços.
	if passaNasGuardas(entrada, texto, opts) != MotivoNenhum {
		return "", 0, 0, false
	}
	return texto, revertidas, len(frases), true
}

// abrirFrase põe a inicial maiúscula no trecho do Smart que abre o texto ou
// vem depois de uma frase do Polish (que fechou com ponto). A sigla fica como está ("eSocial"
// não vira "ESocial").
func abrirFrase(p string) string {
	i := strings.IndexFunc(p, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
	if i < 0 {
		return p
	}
	fim := i
	for fim < len(p) {
		r, tam := utf8.DecodeRuneInString(p[fim:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		fim += tam
	}
	if ehSigla(p[i:fim]) {
		return p
	}
	return p[:i] + comInicialMaiuscula(p[i:])
}

// fecharFrase garante que o trecho do Smart que vem antes de uma frase do
// Polish termina a frase: tira a vírgula, o ponto e vírgula ou os dois
// pontos do fim, põe o ponto quando não há sinal de fim, e um espaço antes da
// frase seguinte.
func fecharFrase(p string) string {
	nucleo := strings.TrimRightFunc(p, unicode.IsSpace)
	nucleo = strings.TrimRight(nucleo, ",;:")
	if r, _ := utf8.DecodeLastRuneInString(nucleo); !strings.ContainsRune(sinaisDeFimDeFrase, r) {
		nucleo += "."
	}
	return nucleo + " "
}
