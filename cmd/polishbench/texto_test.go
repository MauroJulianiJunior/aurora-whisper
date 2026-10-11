package main

import (
	"reflect"
	"testing"
)

func TestPalavrasDe(t *testing.T) {
	ps := palavrasDe("Os pendentes são:\n1. A DCTFWeb\n2. O PGDAS.")
	got := normas(ps)
	want := []string{"os", "pendentes", "são", "a", "dctfweb", "o", "pgdas"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normas = %v, quero %v", got, want)
	}
	if ps[2].Depois != ":" || ps[6].Depois != "." {
		t.Errorf("pontuação colada: %q e %q", ps[2].Depois, ps[6].Depois)
	}
	// O marcador de lista não é palavra; a palavra depois dele abre frase.
	abrem := []bool{true, false, false, true, false, true, false}
	for i, p := range ps {
		if p.AbreFrase != abrem[i] {
			t.Errorf("palavra %q: AbreFrase = %v, quero %v", p.Bruta, p.AbreFrase, abrem[i])
		}
	}
	// Bruta guarda a caixa; Norm não.
	if ps[4].Bruta != "DCTFWeb" || ps[4].Norm != "dctfweb" {
		t.Errorf("Bruta/Norm = %q/%q", ps[4].Bruta, ps[4].Norm)
	}
}

func TestPalavrasDe_Casos(t *testing.T) {
	casos := []struct {
		nome, texto string
		quero       []string
	}{
		{"vazio", "", nil},
		{"só espaço e pontuação", "  ...  ", nil},
		{"pontuação de borda sai, a de dentro fica", "(1.847,32), efd-reinf e meio-dia!", []string{"1.847,32", "efd-reinf", "e", "meio-dia"}},
		{"acento conta", "Débora debora", []string{"débora", "debora"}},
		{"marcadores de traço, asterisco, bolinha e número", "- a\n* b\n• c\n12) d\n3. e", []string{"a", "b", "c", "d", "e"}},
		{"número com ponto colado não é marcador", "1.500 reais", []string{"1.500", "reais"}},
		{"a moeda não é palavra, colada ou solta", "R$1500 e US$ 20 e €3 e $ 4", []string{"1500", "e", "20", "e", "3", "e", "4"}},
		{"CRLF", "a\r\nb", []string{"a", "b"}},
	}
	for _, c := range casos {
		if got := normas(palavrasDe(c.texto)); !reflect.DeepEqual(got, c.quero) && !(len(got) == 0 && len(c.quero) == 0) {
			t.Errorf("%s: %q -> %v, quero %v", c.nome, c.texto, got, c.quero)
		}
	}
}

func TestPalavrasDe_PontuacaoSoCola(t *testing.T) {
	ps := palavrasDe("guia – valor ... Fim")
	if got := normas(ps); !reflect.DeepEqual(got, []string{"guia", "valor", "fim"}) {
		t.Fatalf("normas = %v", got)
	}
	if ps[0].Depois != "–" || ps[1].Depois != "..." {
		t.Errorf("Depois = %q e %q", ps[0].Depois, ps[1].Depois)
	}
	if ps[2].AbreFrase != true {
		t.Error("a palavra depois de \"...\" tem de abrir frase")
	}
	if palavrasDe("a – b")[1].AbreFrase {
		t.Error("um travessão não abre frase")
	}
}

func TestAbreFrase(t *testing.T) {
	ps := palavrasDe("Acabou. Agora sim! e depois? Fim…\nLinha nova")
	quero := map[string]bool{"acabou": true, "agora": true, "sim": false, "e": true, "depois": false, "fim": true, "linha": true, "nova": false}
	for _, p := range ps {
		if p.AbreFrase != quero[p.Norm] {
			t.Errorf("%q: AbreFrase = %v, quero %v", p.Norm, p.AbreFrase, quero[p.Norm])
		}
	}
}

func TestSinalDepois(t *testing.T) {
	casos := []struct {
		depois string
		quero  rune
	}{
		{"", 0}, {",", ','}, {".", '.'}, {"?", '?'}, {"!", '!'}, {":", ':'}, {";", ';'},
		{"...", '.'}, {"…", '.'}, {"?!", '?'}, {")", 0}, {"$", 0}, {"),", ','}, {"\"", 0}, {"–", 0},
	}
	for _, c := range casos {
		if got := sinalDepois(palavra{Depois: c.depois}); got != c.quero {
			t.Errorf("sinalDepois(%q) = %q, quero %q", c.depois, got, c.quero)
		}
	}
}

func TestAlinharPalavras(t *testing.T) {
	casos := []struct {
		a, b  []string
		quero []par
	}{
		{[]string{"a", "b", "c", "d"}, []string{"a", "c", "d", "e"}, []par{{0, 0}, {2, 1}, {3, 2}}},
		{[]string{"a", "b"}, []string{"a", "b"}, []par{{0, 0}, {1, 1}}},
		{[]string{"a", "b"}, []string{"c", "d"}, nil},
		{nil, []string{"a"}, nil},
		{[]string{"a"}, nil, nil},
		// Repetição: casa da esquerda para a direita.
		{[]string{"a", "a", "b"}, []string{"a", "b"}, []par{{0, 0}, {2, 1}}},
	}
	for i, c := range casos {
		got := alinharPalavras(c.a, c.b)
		if !reflect.DeepEqual(got, c.quero) && !(len(got) == 0 && len(c.quero) == 0) {
			t.Errorf("caso %d: %v, quero %v", i, got, c.quero)
		}
	}
	// Ordem trocada: casa uma palavra só (qualquer das duas serve, mas só uma).
	a, b := []string{"x", "y"}, []string{"y", "x"}
	if got := alinharPalavras(a, b); len(got) != 1 || a[got[0].A] != b[got[0].B] {
		t.Errorf("ordem trocada: %v", got)
	}
	if got := naoCasadas(4, []par{{0, 0}, {2, 1}}, true); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Errorf("naoCasadas lado A = %v", got)
	}
	if got := naoCasadas(3, []par{{0, 0}, {2, 1}}, false); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("naoCasadas lado B = %v", got)
	}
}

func TestDistanciaDePalavras(t *testing.T) {
	casos := []struct {
		a, b  []string
		quero int
	}{
		{[]string{"a", "b", "c"}, []string{"a", "b", "c"}, 0},
		{[]string{"a", "b", "c"}, []string{"a", "x", "c"}, 1},
		{[]string{"a", "b", "c"}, []string{"a", "c"}, 1},
		{[]string{"a", "c"}, []string{"a", "b", "c"}, 1},
		{[]string{"a", "b", "c"}, nil, 3},
		{nil, []string{"a", "b"}, 2},
		{nil, nil, 0},
		{[]string{"a", "b", "c", "d"}, []string{"d", "c", "b", "a"}, 4},
	}
	for i, c := range casos {
		if got := distanciaDePalavras(c.a, c.b); got != c.quero {
			t.Errorf("caso %d: %d, quero %d", i, got, c.quero)
		}
	}
}

func TestEstruturaDe(t *testing.T) {
	casos := []struct {
		nome, texto string
		quero       estrutura
	}{
		{"vazio", "", estrutura{}},
		{"um parágrafo", "Só uma frase.", estrutura{Paragrafos: 1}},
		{"duas linhas do mesmo parágrafo", "Linha um\nlinha dois", estrutura{Paragrafos: 1}},
		{"dois parágrafos", "Um.\n\nDois.", estrutura{Paragrafos: 2}},
		{"três parágrafos com mais linhas em branco", "Um.\n\n\n\nDois.\n\nTrês.", estrutura{Paragrafos: 3}},
		{"lista numerada", "Os itens:\n1. a\n2. b", estrutura{Paragrafos: 1, Itens: 2, Marcador: "numerado"}},
		{"lista com traço", "Os itens:\n- a\n- b\n- c", estrutura{Paragrafos: 1, Itens: 3, Marcador: "traco"}},
		{"lista com bolinha e asterisco", "• a\n* b", estrutura{Paragrafos: 1, Itens: 2, Marcador: "traco"}},
		{"lista com parêntese", "1) a\n2) b", estrutura{Paragrafos: 1, Itens: 2, Marcador: "numerado"}},
		{"a lista depois de linha em branco continua o bloco", "Os itens:\n\n- a\n- b", estrutura{Paragrafos: 1, Itens: 2, Marcador: "traco"}},
		{"lista, parágrafo depois", "Os itens:\n- a\n- b\n\nDepois disso, fim.", estrutura{Paragrafos: 2, Itens: 2, Marcador: "traco"}},
		{"um item só", "- a", estrutura{Paragrafos: 1, Itens: 1, Marcador: "traco"}},
		{"o tipo é o do primeiro item", "- a\n1. b", estrutura{Paragrafos: 1, Itens: 2, Marcador: "traco"}},
		{"número com ponto colado não é item", "1.500 reais\nfim", estrutura{Paragrafos: 1}},
		{"CRLF", "Um.\r\n\r\nDois.", estrutura{Paragrafos: 2}},
	}
	for _, c := range casos {
		if got := estruturaDe(c.texto); got != c.quero {
			t.Errorf("%s: %+v, quero %+v", c.nome, got, c.quero)
		}
	}
	if !mesmaEstrutura(estrutura{Paragrafos: 2, Itens: 3, Marcador: "traco"}, estrutura{Paragrafos: 2, Itens: 3, Marcador: "numerado"}) {
		t.Error("o tipo do marcador não conta para o acerto de estrutura")
	}
	if mesmaEstrutura(estrutura{Paragrafos: 2, Itens: 3}, estrutura{Paragrafos: 1, Itens: 3}) {
		t.Error("parágrafos diferentes")
	}
	if mesmaEstrutura(estrutura{Paragrafos: 2, Itens: 3}, estrutura{Paragrafos: 2, Itens: 2}) {
		t.Error("itens diferentes")
	}
}
