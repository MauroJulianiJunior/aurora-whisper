package pagina

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Markdown devolve o HTML do texto em Markdown, para a página da política de
// privacidade da instância (GET /privacidade). É um renderizador pequeno e
// puro, só do que o modelo da política usa (docs/politica-de-privacidade.md),
// e sem dependência:
//
//   - títulos de # a ######; parágrafos (a quebra de linha simples vira
//     espaço); réguas (---);
//   - listas com "-", "*", "+" e "1." (a continuação indentada, o parágrafo
//     seguinte e a lista dentro do item entram no item);
//   - citações com ">"; tabelas no estilo do GitHub (cabeçalho e linha
//     |---|, com :---: para o alinhamento);
//   - no meio do texto: **negrito**, `código` e a barra invertida antes de um
//     sinal de pontuação.
//
// TODO o texto sai escapado (html.EscapeString), e HTML cru no Markdown nunca
// passa: um <script> aparece na página como texto. O que está fora disso
// (links, imagens, itálico, blocos de código, HTML) é texto comum, escapado.
// Não há atributo "style" na saída: a CSP do servidor não admite estilo
// embutido, e o alinhamento das tabelas é uma classe do pagina.css.
func Markdown(src string) string {
	linhas := strings.Split(strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n"), "\n")
	var b strings.Builder
	blocos(&b, linhas, 0, false)
	return b.String()
}

// profundidadeMaxima corta o aninhamento (lista dentro de lista, citação
// dentro de citação): além dela o resto é parágrafo, e um arquivo cheio de ">"
// não vira uma pilha de chamadas sem fim.
const profundidadeMaxima = 6

var (
	reTitulo = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	reRegua  = regexp.MustCompile(`^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	// reMarcador: o "-", "*", "+" ou "1." (até 9 dígitos), até 3 espaços de
	// recuo e, depois, ao menos um espaço (ou o fim da linha, o item vazio).
	reMarcador  = regexp.MustCompile(`^( {0,3})([-*+]|[0-9]{1,9}[.)])(?:( +)(.*))?$`)
	reCitacao   = regexp.MustCompile(`^ {0,3}> ?`)
	reSeparador = regexp.MustCompile(`^:?-+:?$`)
	reEmBranco  = regexp.MustCompile(`^\s*$`)
)

func vazia(l string) bool { return reEmBranco.MatchString(l) }

// recuo conta os espaços do começo da linha (o tab vale 4).
func recuo(l string) int {
	n := 0
	for _, c := range l {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4 - n%4
		default:
			return n
		}
	}
	return n
}

// semRecuo tira até n colunas de espaço do começo da linha.
func semRecuo(l string, n int) string {
	col := 0
	for i, c := range l {
		if col >= n {
			return l[i:]
		}
		switch c {
		case ' ':
			col++
		case '\t':
			col += 4 - col%4
		default:
			return l[i:]
		}
	}
	return ""
}

// blocos escreve em b os blocos de linhas. Em "justa" (dentro de um item de
// lista), o parágrafo sai sem <p>.
func blocos(b *strings.Builder, linhas []string, prof int, justa bool) {
	i := 0
	for i < len(linhas) {
		l := linhas[i]
		if vazia(l) {
			i++
			continue
		}
		// Só o primeiro bloco do item sai sem <p>: um segundo parágrafo é
		// parágrafo mesmo.
		sem := justa
		justa = false
		switch {
		case prof >= profundidadeMaxima:
			i = paragrafo(b, linhas, i, sem, prof)
		case reRegua.MatchString(l):
			b.WriteString("<hr>\n")
			i++
		case reTitulo.MatchString(l):
			m := reTitulo.FindStringSubmatch(l)
			n := strconv.Itoa(len(m[1]))
			b.WriteString("<h" + n + ">" + inline(m[2]) + "</h" + n + ">\n")
			i++
		case reCitacao.MatchString(l):
			i = citacao(b, linhas, i, prof)
		case ehTabela(linhas, i):
			i = tabela(b, linhas, i)
		case reMarcador.MatchString(l):
			i = lista(b, linhas, i, prof)
		default:
			i = paragrafo(b, linhas, i, sem, prof)
		}
	}
}

// interrompe diz se a linha encerra o parágrafo que vem antes dela: título,
// régua, citação, tabela e o começo de uma lista (o "1." só interrompe como
// primeiro número, para uma linha como "2026. O ano" não virar item).
func interrompe(linhas []string, i int) bool {
	l := linhas[i]
	if vazia(l) || reRegua.MatchString(l) || reTitulo.MatchString(l) || reCitacao.MatchString(l) || ehTabela(linhas, i) {
		return true
	}
	if m := reMarcador.FindStringSubmatch(l); m != nil && m[4] != "" {
		if c := m[2][0]; c < '0' || c > '9' {
			return true
		}
		return strings.TrimRight(m[2], ".)") == "1"
	}
	return false
}

func paragrafo(b *strings.Builder, linhas []string, i int, justa bool, prof int) int {
	var partes []string
	for ; i < len(linhas); i++ {
		// A primeira linha é sempre do parágrafo (é o que o "default" e o corte
		// de profundidade trouxeram até aqui).
		if len(partes) > 0 && (interrompe(linhas, i) && prof < profundidadeMaxima) {
			break
		}
		if vazia(linhas[i]) {
			break
		}
		partes = append(partes, strings.TrimSpace(linhas[i]))
	}
	texto := inline(strings.Join(partes, " "))
	if justa {
		b.WriteString(texto + "\n")
	} else {
		b.WriteString("<p>" + texto + "</p>\n")
	}
	return i
}

func citacao(b *strings.Builder, linhas []string, i, prof int) int {
	var dentro []string
	for ; i < len(linhas) && reCitacao.MatchString(linhas[i]); i++ {
		dentro = append(dentro, reCitacao.ReplaceAllString(linhas[i], ""))
	}
	b.WriteString("<blockquote>\n")
	blocos(b, dentro, prof+1, false)
	b.WriteString("</blockquote>\n")
	return i
}

// lista escreve uma lista, do item que começa em linhas[i] até o primeiro que
// não é da mesma espécie (de "1." para "-", por exemplo), e devolve onde parou.
func lista(b *strings.Builder, linhas []string, i, prof int) int {
	m := reMarcador.FindStringSubmatch(linhas[i])
	ordenada := m[2][0] >= '0' && m[2][0] <= '9'
	if ordenada {
		if n, _ := strconv.Atoi(strings.TrimRight(m[2], ".)")); n != 1 {
			b.WriteString("<ol start=\"" + strconv.Itoa(n) + "\">\n")
		} else {
			b.WriteString("<ol>\n")
		}
	} else {
		b.WriteString("<ul>\n")
	}
	for i < len(linhas) {
		m := reMarcador.FindStringSubmatch(linhas[i])
		if m == nil || reRegua.MatchString(linhas[i]) || (m[2][0] >= '0' && m[2][0] <= '9') != ordenada {
			break
		}
		// A coluna onde o texto do item começa: o recuo, o marcador e os
		// espaços (de 1 a 4; com mais, o texto começa depois do primeiro).
		espacos := len(m[3])
		if espacos == 0 || espacos > 4 {
			espacos = 1
		}
		conteudo := len(m[1]) + len(m[2]) + espacos
		item := []string{m[4]}
		i++
		for i < len(linhas) {
			l := linhas[i]
			if vazia(l) {
				// A linha em branco fica no item se o que vem depois dela ainda é dele.
				k := i
				for k < len(linhas) && vazia(linhas[k]) {
					k++
				}
				if k < len(linhas) && recuo(linhas[k]) >= conteudo {
					item = append(item, "")
					i++
					continue
				}
				break
			}
			if recuo(l) >= conteudo {
				item = append(item, semRecuo(l, conteudo))
				i++
				continue
			}
			// Menos recuado: ou é outro item/bloco, ou a continuação preguiçosa
			// do parágrafo do item (a linha logo abaixo, sem recuo).
			if reMarcador.MatchString(l) || interrompe(linhas, i) || vazia(item[len(item)-1]) {
				break
			}
			item = append(item, strings.TrimLeft(l, " \t"))
			i++
		}
		b.WriteString("<li>")
		var dentro strings.Builder
		blocos(&dentro, item, prof+1, true)
		b.WriteString(strings.TrimRight(dentro.String(), "\n"))
		b.WriteString("</li>\n")
		// Pula as linhas em branco entre os itens se a lista continua.
		k := i
		for k < len(linhas) && vazia(linhas[k]) {
			k++
		}
		if k < len(linhas) && k > i {
			if mk := reMarcador.FindStringSubmatch(linhas[k]); mk != nil && !reRegua.MatchString(linhas[k]) && (mk[2][0] >= '0' && mk[2][0] <= '9') == ordenada {
				i = k
			}
		}
	}
	if ordenada {
		b.WriteString("</ol>\n")
	} else {
		b.WriteString("</ul>\n")
	}
	return i
}

// celulas divide uma linha de tabela nas barras que não têm barra invertida
// antes, sem as barras das pontas.
func celulas(l string) []string {
	l = strings.TrimSpace(l)
	l = strings.TrimPrefix(l, "|")
	if strings.HasSuffix(l, "|") && !strings.HasSuffix(l, `\|`) {
		l = strings.TrimSuffix(l, "|")
	}
	var out []string
	var atual strings.Builder
	for i := 0; i < len(l); i++ {
		switch {
		case l[i] == '\\' && i+1 < len(l):
			atual.WriteByte(l[i])
			atual.WriteByte(l[i+1])
			i++
		case l[i] == '|':
			out = append(out, strings.TrimSpace(atual.String()))
			atual.Reset()
		default:
			atual.WriteByte(l[i])
		}
	}
	return append(out, strings.TrimSpace(atual.String()))
}

// ehTabela: a linha i é o cabeçalho e a seguinte é a linha de traços, com o
// mesmo número de colunas.
func ehTabela(linhas []string, i int) bool {
	if i+1 >= len(linhas) || !strings.Contains(linhas[i], "|") || !strings.Contains(linhas[i+1], "-") {
		return false
	}
	cab, sep := celulas(linhas[i]), celulas(linhas[i+1])
	if len(cab) != len(sep) {
		return false
	}
	for _, c := range sep {
		if !reSeparador.MatchString(c) {
			return false
		}
	}
	return true
}

func tabela(b *strings.Builder, linhas []string, i int) int {
	cab, sep := celulas(linhas[i]), celulas(linhas[i+1])
	alinha := make([]string, len(sep))
	for n, c := range sep {
		switch {
		case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
			alinha[n] = ` class="c"`
		case strings.HasSuffix(c, ":"):
			alinha[n] = ` class="d"`
		}
	}
	linha := func(cels []string, tag string) {
		b.WriteString("<tr>")
		for n := range cab {
			t := ""
			if n < len(cels) {
				t = cels[n]
			}
			b.WriteString("<" + tag + alinha[n] + ">" + inline(t) + "</" + tag + ">")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("<div class=\"tabela\"><table>\n<thead>\n")
	linha(cab, "th")
	b.WriteString("</thead>\n<tbody>\n")
	i += 2
	for ; i < len(linhas) && !vazia(linhas[i]) && strings.Contains(linhas[i], "|"); i++ {
		linha(celulas(linhas[i]), "td")
	}
	b.WriteString("</tbody>\n</table></div>\n")
	return i
}

func ehPontuacao(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// inline escreve o texto de um parágrafo, célula ou título: negrito, código e
// a barra invertida; o resto, escapado.
func inline(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		j := strings.IndexAny(s[i:], "\\`*")
		if j < 0 {
			b.WriteString(html.EscapeString(s[i:]))
			break
		}
		b.WriteString(html.EscapeString(s[i : i+j]))
		i += j
		switch {
		case s[i] == '\\' && i+1 < len(s) && ehPontuacao(s[i+1]):
			b.WriteString(html.EscapeString(s[i+1 : i+2]))
			i += 2
		case s[i] == '`':
			n := corridaDe(s, i, '`')
			if fim := fechaCodigo(s, i+n, n); fim >= 0 {
				b.WriteString("<code>" + html.EscapeString(strings.Trim(s[i+n:fim], " ")) + "</code>")
				i = fim + n
			} else {
				b.WriteString(html.EscapeString(s[i : i+n]))
				i += n
			}
		case strings.HasPrefix(s[i:], "**") && i+2 < len(s) && s[i+2] != ' ' && s[i+2] != '\t':
			if fim := fechaNegrito(s, i+2); fim >= 0 {
				b.WriteString("<strong>" + inline(s[i+2:fim]) + "</strong>")
				i = fim + 2
			} else {
				b.WriteString("**")
				i += 2
			}
		default:
			b.WriteString(html.EscapeString(s[i : i+1]))
			i++
		}
	}
	return b.String()
}

// corridaDe conta quantas vezes c se repete a partir de i.
func corridaDe(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

// fechaCodigo acha onde fecha o código aberto por n crases, ou -1.
func fechaCodigo(s string, de, n int) int {
	for j := de; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		k := corridaDe(s, j, '`')
		if k == n {
			return j
		}
		j += k
	}
	return -1
}

// fechaNegrito acha o ** que fecha o negrito aberto antes de de: um que não
// tem espaço antes dele, fora de um código e sem barra invertida. -1, se não há.
func fechaNegrito(s string, de int) int {
	for j := de; j < len(s); {
		switch {
		case s[j] == '\\':
			j += 2
		case s[j] == '`':
			n := corridaDe(s, j, '`')
			if fim := fechaCodigo(s, j+n, n); fim >= 0 {
				j = fim + n
			} else {
				j += n
			}
		case strings.HasPrefix(s[j:], "**") && j > de && s[j-1] != ' ' && s[j-1] != '\t':
			return j
		default:
			j++
		}
	}
	return -1
}
