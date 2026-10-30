package pagina

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// O renderizador de Markdown da política (markdown.go): cada construção do
// modelo, o escape de tudo e, no teste que vale mais, o modelo de verdade
// (docs/politica-de-privacidade.md) sem nenhuma sintaxe sobrando na saída.

func TestMarkdown_Construcoes(t *testing.T) {
	cases := []struct {
		nome, md, quero string
	}{
		{"título 1", "# Olá mundo", "<h1>Olá mundo</h1>"},
		{"título 2", "## 1. O resumo", "<h2>1. O resumo</h2>"},
		{"título 3", "### O que cada um diz", "<h3>O que cada um diz</h3>"},
		{"título com # no fim", "## Título ##", "<h2>Título</h2>"},
		{"parágrafo", "Um texto simples.", "<p>Um texto simples.</p>"},
		{"quebra simples vira espaço", "primeira\nsegunda\n\nterceira", "<p>primeira segunda</p>\n<p>terceira</p>"},
		{"negrito", "um **forte** aqui", "<p>um <strong>forte</strong> aqui</p>"},
		{"negrito com código dentro", "**veja `x` agora**", "<p><strong>veja <code>x</code> agora</strong></p>"},
		{"negrito que atravessa a quebra", "**um\ndois**", "<p><strong>um dois</strong></p>"},
		{"código", "use `go test` já", "<p>use <code>go test</code> já</p>"},
		{"código com negrito dentro é texto", "`**x**`", "<p><code>**x**</code></p>"},
		{"régua", "antes\n\n---\n\ndepois", "<p>antes</p>\n<hr>\n<p>depois</p>"},
		{"lista de marcadores", "- um\n- dois", "<ul>\n<li>um</li>\n<li>dois</li>\n</ul>"},
		{"lista com asterisco", "* um\n* dois", "<ul>\n<li>um</li>\n<li>dois</li>\n</ul>"},
		{"lista numerada", "1. um\n2. dois", "<ol>\n<li>um</li>\n<li>dois</li>\n</ol>"},
		{"lista numerada que não começa em 1", "3. três\n4. quatro", "<ol start=\"3\">\n<li>três</li>\n<li>quatro</li>\n</ol>"},
		{"item com continuação indentada", "1. um\n   continua\n2. dois", "<ol>\n<li>um continua</li>\n<li>dois</li>\n</ol>"},
		{"item com continuação sem recuo", "- um\ncontinua\n- dois", "<ul>\n<li>um continua</li>\n<li>dois</li>\n</ul>"},
		{"itens separados por linha em branco", "1. um\n\n2. dois", "<ol>\n<li>um</li>\n<li>dois</li>\n</ol>"},
		{"item com dois parágrafos", "- um\n\n  outro", "<ul>\n<li>um\n<p>outro</p></li>\n</ul>"},
		{"lista dentro de item", "1. um:\n   - a\n   - b\n2. dois", "<ol>\n<li>um:\n<ul>\n<li>a</li>\n<li>b</li>\n</ul></li>\n<li>dois</li>\n</ol>"},
		{"lista logo depois do parágrafo", "Antes:\n- a\n- b", "<p>Antes:</p>\n<ul>\n<li>a</li>\n<li>b</li>\n</ul>"},
		{"lista de uma espécie e outra", "- a\n1. b", "<ul>\n<li>a</li>\n</ul>\n<ol>\n<li>b</li>\n</ol>"},
		{"o ano não vira lista", "Em\n2026. Foi assim", "<p>Em 2026. Foi assim</p>"},
		{"citação", "> um\n> dois\n\nfora", "<blockquote>\n<p>um dois</p>\n</blockquote>\n<p>fora</p>"},
		{"citação com negrito e lista", "> **Aviso**\n>\n> - a", "<blockquote>\n<p><strong>Aviso</strong></p>\n<ul>\n<li>a</li>\n</ul>\n</blockquote>"},
		{
			"tabela",
			"| Quem | O quê |\n|---|---|\n| A | **b** |\n| C | `d` |",
			"<div class=\"tabela\"><table>\n<thead>\n<tr><th>Quem</th><th>O quê</th></tr>\n</thead>\n<tbody>\n<tr><td>A</td><td><strong>b</strong></td></tr>\n<tr><td>C</td><td><code>d</code></td></tr>\n</tbody>\n</table></div>",
		},
		{
			"tabela sem barras nas pontas, com alinhamento",
			"a | b | c\n:--- | :---: | ---:\n1 | 2 | 3",
			"<tr><th>a</th><th class=\"c\">b</th><th class=\"d\">c</th></tr>",
		},
		{"tabela com barra escapada na célula", "| a | b |\n|---|---|\n| x \\| y | z |", "<td>x | y</td><td>z</td>"},
		{"linha de tabela curta fica com a célula vazia", "| a | b |\n|---|---|\n| só |", "<tr><td>só</td><td></td></tr>"},
		{"barra invertida", `\*não\* e \` + "`" + `não`, "<p>*não* e `não</p>"},
	}
	for _, c := range cases {
		if got := Markdown(c.md); !strings.Contains(got, c.quero) {
			t.Errorf("%s:\n%s\n--- quero (contido):\n%s", c.nome, got, c.quero)
		}
	}
}

// Igualdade exata nos casos simples: a saída não ganha nada além do esperado.
func TestMarkdown_SaidaExata(t *testing.T) {
	if got, want := Markdown("# T\n\ntexto **b**\n"), "<h1>T</h1>\n<p>texto <strong>b</strong></p>\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := Markdown(""); got != "" {
		t.Errorf("vazio: %q", got)
	}
	if got := Markdown("a\r\nb\r\n\r\nc"); got != "<p>a b</p>\n<p>c</p>\n" {
		t.Errorf("CRLF: %q", got)
	}
}

// Nada passa cru: HTML no Markdown sai como texto, e nenhuma saída tem tag,
// atributo ou esquema de URL que o texto não tenha escrito como sintaxe.
func TestMarkdown_EscapaTudo(t *testing.T) {
	for nome, md := range map[string]string{
		"script no parágrafo": "oi <script>alert(1)</script> fim",
		"script no título":    "## <script>alert(1)</script>",
		"script na lista":     "- <script>alert(1)</script>",
		"script na citação":   "> <script>alert(1)</script>",
		"script na tabela":    "| <script>alert(1)</script> |\n|---|\n| <script>alert(1)</script> |",
		"script no código":    "`<script>alert(1)</script>`",
		"script no negrito":   "**<script>alert(1)</script>**",
		"img com onerror":     `<img src=x onerror="alert(1)">`,
		"link javascript":     "[clique](javascript:alert(1))",
		"html no item":        "1. <b onclick=x>a</b>",
	} {
		got := Markdown(md)
		for _, perigo := range []string{"<script", "</script", "<img", "<b ", "<a ", "href="} {
			if strings.Contains(got, perigo) {
				t.Errorf("%s: a saída tem %q:\n%s", nome, perigo, got)
			}
		}
	}
	if got := Markdown("a <b> & \"c\" 'd'"); got != "<p>a &lt;b&gt; &amp; &#34;c&#34; &#39;d&#39;</p>\n" {
		t.Errorf("escape: %q", got)
	}
	// O que o Markdown aceitaria e o subconjunto não tem é texto: o link, a
	// imagem, o itálico, o bloco de código.
	if got := Markdown("[a](http://x.y) ![b](c.png) *i* _j_"); strings.Contains(got, "<a ") || strings.Contains(got, "<img") || strings.Contains(got, "<em>") {
		t.Errorf("fora do subconjunto: %q", got)
	}
}

// Marcação mal fechada sobra como texto, sem derrubar nem engolir o resto.
func TestMarkdown_MalFechado(t *testing.T) {
	if got := Markdown("um **aberto e `solto"); got != "<p>um **aberto e `solto</p>\n" {
		t.Errorf("got %q", got)
	}
	if got := Markdown("** vazio ** e ****"); !strings.Contains(got, "** vazio **") {
		t.Errorf("got %q", got)
	}
}

// Aninhar sem fim (um arquivo de ">" ou de listas) não estoura a pilha: além
// da profundidade máxima o resto é parágrafo.
func TestMarkdown_AninhamentoSemFim(t *testing.T) {
	got := Markdown(strings.Repeat("> ", 5000) + "fundo")
	if !strings.Contains(got, "fundo") || strings.Count(got, "<blockquote>") > profundidadeMaxima {
		t.Errorf("citações: %d blockquotes", strings.Count(got, "<blockquote>"))
	}
	var b strings.Builder
	for i := range 200 {
		b.WriteString(strings.Repeat("  ", i) + "- n\n")
	}
	if got := Markdown(b.String()); strings.Count(got, "<ul>") > profundidadeMaxima+1 {
		t.Errorf("listas: %d <ul>", strings.Count(got, "<ul>"))
	}
}

var reCampo = regexp.MustCompile(`\*\*\[A PREENCHER[^\]]*\]\*\*`)

// O modelo de verdade, as seções 1 a 13 (do começo até o título "Os textos do
// app"), com os campos preenchidos de mentira: nada da sintaxe do Markdown
// sobra na página, as tabelas viram <table> e as seções, <h2>.
func TestMarkdown_OModeloDaPolitica(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/politica-de-privacidade.md")
	if err != nil {
		t.Fatal(err)
	}
	md := strings.ReplaceAll(string(raw), "\r\n", "\n")
	corte := strings.Index(md, "\n# Os textos do app sobre privacidade")
	if corte < 0 {
		t.Fatal("o modelo não tem mais o título \"Os textos do app sobre privacidade\": o corte da página depende dele")
	}
	md = md[:corte]
	campos := len(reCampo.FindAllString(md, -1))
	md = reCampo.ReplaceAllString(md, "**texto de teste**")
	if strings.Contains(md, "[A PREENCHER") {
		t.Fatalf("sobrou um campo do modelo que o teste não sabe trocar")
	}
	if campos < 8 {
		t.Errorf("só %d campos no modelo: o teste perdeu o que cobrir", campos)
	}

	got := Markdown(md)

	for _, sintaxe := range []string{"**", "|---|", "`", "\n- ", "\n1. ", "&gt; ", "---\n"} {
		if strings.Contains(got, sintaxe) {
			i := strings.Index(got, sintaxe)
			t.Errorf("sintaxe do Markdown na página (%q): ...%s...", sintaxe, got[max(0, i-80):min(len(got), i+80)])
		}
	}
	// Texto de linha começando com # (um título que não virou título).
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") || strings.HasPrefix(strings.TrimSpace(l), "<p>#") {
			t.Errorf("linha que começa com #: %q", l)
		}
	}
	if n := strings.Count(got, "<table>"); n != 3 {
		t.Errorf("%d tabelas, quero 3 (seções 6, 6 e 7)", n)
	}
	if n := strings.Count(got, "<h2>"); n != 13 {
		t.Errorf("%d títulos <h2>, quero as 13 seções", n)
	}
	for _, h := range []string{"<h2>1. O resumo</h2>", "<h2>6. Com quem os dados passam</h2>", "<h2>13. Mudanças</h2>", "<h3>O que cada provedor diz</h3>"} {
		if !strings.Contains(got, h) {
			t.Errorf("falta %s", h)
		}
	}
	if strings.Contains(got, "<script") || strings.Contains(got, "Os textos do app") {
		t.Error("a página tem o que não devia")
	}
	// A seção 10 tem a lista numerada com a lista de marcadores dentro de um item.
	if !strings.Contains(got, "<ol>") || !strings.Contains(got, "<li>") || strings.Count(got, "<ol>") != 1 {
		t.Errorf("a seção 10 não virou uma lista numerada só (%d <ol>)", strings.Count(got, "<ol>"))
	}
	if !strings.Contains(got, "<strong>texto de teste</strong>") || !strings.Contains(got, "<blockquote>") || !strings.Contains(got, "<hr>") {
		t.Error("faltam o negrito, a citação ou a régua do modelo")
	}
	// Todo item da seção 10 virou item: seis, e os da lista dentro do 4 (seis)
	// e do 5 (três).
	sec10 := got[strings.Index(got, "<h2>10."):strings.Index(got, "<h2>11.")]
	if n := strings.Count(sec10, "<li>"); n != 6+6+3 {
		t.Errorf("seção 10: %d itens (<li>), quero 6, os 6 do item 4 e os 3 do item 5", n)
	}
}
