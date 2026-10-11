package deploy

import (
	"strings"
	"testing"
)

// Um leitor de YAML pequeno, só para os arquivos deste pacote (o compose, o
// compose do teste do PC e a configuração do Traefik de teste): o repositório
// não tem biblioteca de YAML, e uma dependência nova só para o teste não vale.
// Ele aceita o subconjunto que esses arquivos usam, e RECUSA o resto (o teste
// falha), para um arquivo escrito fora do formato não passar por engano:
//   - mapas em bloco ("chave: valor" e "chave:" com o filho mais recuado);
//   - listas em bloco ("- item"), de escalares ou de mapas;
//   - escalares simples, entre aspas duplas ou simples, e o {} e o [] vazios;
//   - comentários só em linha inteira (nada de "valor # comentário"), e nada
//     de tab, de âncora, de bloco "|" ou ">" nem de lista ou mapa em linha.

type linhaYAML struct {
	n      int
	recuo  int
	texto  string
	origem string
}

type leitorYAML struct {
	t      *testing.T
	nome   string
	linhas []linhaYAML
	i      int
}

// lerYAML lê o texto e devolve a árvore: map[string]any, []any e string (o
// {} vira mapa vazio, o [] lista vazia, e "chave:" sem filho vira nil).
func lerYAML(t *testing.T, nome, texto string) any {
	t.Helper()
	l := &leitorYAML{t: t, nome: nome}
	for i, s := range strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n") {
		limpo := strings.TrimSpace(s)
		if limpo == "" || strings.HasPrefix(limpo, "#") {
			continue
		}
		if strings.Contains(s, "\t") {
			t.Fatalf("%s, linha %d: tab no YAML", nome, i+1)
		}
		recuo := len(s) - len(strings.TrimLeft(s, " "))
		corpo := strings.TrimRight(s[recuo:], " ")
		for _, ruim := range []string{" #", "&", "*", ": |", ": >", ": [", ": {"} {
			if strings.Contains(corpo, ruim) && !(ruim == ": {" && strings.HasSuffix(corpo, ": {}")) &&
				!(ruim == ": [" && strings.HasSuffix(corpo, ": []")) {
				t.Fatalf("%s, linha %d: %q fora do subconjunto que este leitor aceita: %s", nome, i+1, ruim, corpo)
			}
		}
		l.linhas = append(l.linhas, linhaYAML{i + 1, recuo, corpo, s})
	}
	if len(l.linhas) == 0 {
		t.Fatalf("%s: YAML vazio", nome)
	}
	raiz := l.bloco(0)
	if l.i != len(l.linhas) {
		t.Fatalf("%s, linha %d: recuo inesperado", nome, l.linhas[l.i].n)
	}
	return raiz
}

func (l *leitorYAML) falhar(format string, a ...any) {
	l.t.Helper()
	n := 0
	if l.i < len(l.linhas) {
		n = l.linhas[l.i].n
	}
	l.t.Fatalf("%s, linha %d: "+format, append([]any{l.nome, n}, a...)...)
}

func (l *leitorYAML) bloco(recuo int) any {
	if l.linhas[l.i].recuo != recuo {
		l.falhar("recuo %d, esperado %d", l.linhas[l.i].recuo, recuo)
	}
	if ehItem(l.linhas[l.i].texto) {
		return l.lista(recuo)
	}
	return l.mapa(recuo)
}

func ehItem(s string) bool { return strings.HasPrefix(s, "- ") }

func (l *leitorYAML) mapa(recuo int) map[string]any {
	m := map[string]any{}
	for l.i < len(l.linhas) && l.linhas[l.i].recuo == recuo {
		if ehItem(l.linhas[l.i].texto) {
			l.falhar("item de lista no meio de um mapa")
		}
		l.par(m, recuo)
	}
	return m
}

func (l *leitorYAML) par(m map[string]any, recuo int) {
	k, v, ok := chaveValorYAML(l.linhas[l.i].texto)
	if !ok {
		l.falhar("não é chave: valor: %s", l.linhas[l.i].texto)
	}
	if _, dup := m[k]; dup {
		l.falhar("chave repetida: %s", k)
	}
	l.i++
	if v != "" {
		m[k] = escalarYAML(v)
		return
	}
	if l.i < len(l.linhas) && l.linhas[l.i].recuo > recuo {
		m[k] = l.bloco(l.linhas[l.i].recuo)
		return
	}
	m[k] = nil
}

func (l *leitorYAML) lista(recuo int) []any {
	out := []any{}
	for l.i < len(l.linhas) && l.linhas[l.i].recuo == recuo && ehItem(l.linhas[l.i].texto) {
		item := strings.TrimPrefix(l.linhas[l.i].texto, "- ")
		if k, v, ok := chaveValorYAML(item); ok {
			// Um mapa como item: o primeiro par na linha do "-", o resto
			// recuado dois espaços.
			m := map[string]any{}
			l.i++
			if v != "" {
				m[k] = escalarYAML(v)
			} else if l.i < len(l.linhas) && l.linhas[l.i].recuo > recuo+2 {
				m[k] = l.bloco(l.linhas[l.i].recuo)
			} else {
				m[k] = nil
			}
			for l.i < len(l.linhas) && l.linhas[l.i].recuo == recuo+2 && !ehItem(l.linhas[l.i].texto) {
				l.par(m, recuo+2)
			}
			out = append(out, m)
			continue
		}
		out = append(out, escalarYAML(item))
		l.i++
	}
	return out
}

// chaveValorYAML separa "chave: valor" (ou "chave:"). Um texto entre aspas
// nunca é chave.
func chaveValorYAML(s string) (string, string, bool) {
	if strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "'") {
		return "", "", false
	}
	if i := strings.Index(s, ": "); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+2:]), true
	}
	if strings.HasSuffix(s, ":") {
		return strings.TrimSuffix(s, ":"), "", true
	}
	return "", "", false
}

func escalarYAML(v string) any {
	switch {
	case v == "{}":
		return map[string]any{}
	case v == "[]":
		return []any{}
	case len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`):
		return v[1 : len(v)-1]
	case len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'"):
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// --- a navegação ------------------------------------------------------------

// noYAML devolve o nó no caminho (chaves de mapas), ou falha.
func noYAML(t *testing.T, raiz any, caminho ...string) any {
	t.Helper()
	atual := raiz
	for i, k := range caminho {
		m, ok := atual.(map[string]any)
		if !ok {
			t.Fatalf("%s não é um mapa", strings.Join(caminho[:i], "."))
		}
		if atual, ok = m[k]; !ok {
			t.Fatalf("falta %s", strings.Join(caminho[:i+1], "."))
		}
	}
	return atual
}

// temYAML diz se o caminho existe.
func temYAML(raiz any, caminho ...string) bool {
	atual := raiz
	for _, k := range caminho {
		m, ok := atual.(map[string]any)
		if !ok {
			return false
		}
		if atual, ok = m[k]; !ok {
			return false
		}
	}
	return true
}

func textoYAML(t *testing.T, raiz any, caminho ...string) string {
	t.Helper()
	v, ok := noYAML(t, raiz, caminho...).(string)
	if !ok {
		t.Fatalf("%s não é um texto", strings.Join(caminho, "."))
	}
	return v
}

func listaYAML(t *testing.T, raiz any, caminho ...string) []string {
	t.Helper()
	v, ok := noYAML(t, raiz, caminho...).([]any)
	if !ok {
		t.Fatalf("%s não é uma lista", strings.Join(caminho, "."))
	}
	out := make([]string, 0, len(v))
	for _, x := range v {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("%s tem item que não é texto", strings.Join(caminho, "."))
		}
		out = append(out, s)
	}
	return out
}

func mapaYAML(t *testing.T, raiz any, caminho ...string) map[string]any {
	t.Helper()
	v, ok := noYAML(t, raiz, caminho...).(map[string]any)
	if !ok {
		t.Fatalf("%s não é um mapa", strings.Join(caminho, "."))
	}
	return v
}

func chavesYAML(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return ordenar(out)
}

func ordenar(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestYAML_OLeitorDesteTeste(t *testing.T) {
	texto := "# comentario\nnome: x\nmapa:\n  a: \"1\"\n  b: '2'\n  vazio: {}\n  nada:\nlista:\n  - um\n  - \"dois\"\n" +
		"servidores:\n  - url: http://a:8080\n    peso: 2\n  - url: http://b\nfundo:\n  - c: d\n    e:\n      - f\n"
	raiz := lerYAML(t, "amostra", texto)
	if textoYAML(t, raiz, "nome") != "x" || textoYAML(t, raiz, "mapa", "a") != "1" || textoYAML(t, raiz, "mapa", "b") != "2" {
		t.Error("escalares")
	}
	if len(mapaYAML(t, raiz, "mapa", "vazio")) != 0 || noYAML(t, raiz, "mapa", "nada") != nil {
		t.Error("vazios")
	}
	if strings.Join(listaYAML(t, raiz, "lista"), ",") != "um,dois" {
		t.Error("lista")
	}
	sv := noYAML(t, raiz, "servidores").([]any)
	if len(sv) != 2 || sv[0].(map[string]any)["url"] != "http://a:8080" || sv[0].(map[string]any)["peso"] != "2" ||
		sv[1].(map[string]any)["url"] != "http://b" {
		t.Errorf("lista de mapas: %#v", sv)
	}
	f := noYAML(t, raiz, "fundo").([]any)[0].(map[string]any)
	if f["c"] != "d" || len(f["e"].([]any)) != 1 {
		t.Errorf("mapa com filho dentro de lista: %#v", f)
	}
	if temYAML(raiz, "mapa", "c") || !temYAML(raiz, "mapa", "a") {
		t.Error("temYAML")
	}
}
