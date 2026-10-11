package learning

import (
	"reflect"
	"strings"
	"testing"
)

// Testes das peças de baixo nível de Sugerir, com o texto na mão: os casos de
// ponta que a tabela de sugestao_test.go só alcança por um caminho longo.

func TestApararLado(t *testing.T) {
	cases := []struct {
		texto        string
		wantI, wantJ int
	}{
		{"o sf do", 1, 2},
		{"o  sf\tdo", 1, 2}, // espaço duplicado e tabulação separam
		{"o\nsf", 1, 2},     // quebra de linha separa
		{"sf", 0, 1},
		{"NF-e", 0, 2},          // o "e" está colado pelo hífen: é do termo
		{"e-mail", 0, 2},        // idem, na esquerda
		{"S.A", 0, 2},           // o "a" está colado pelo ponto
		{"e-mail do", 0, 2},     // a direita olha o par da direita, não o da esquerda
		{"do e-mail", 1, 3},     // e a esquerda olha o par da esquerda
		{"e-mail do sf", 0, 4},  // o que está no meio não é tocado
		{"sf e mail", 0, 3},     // nenhuma ponta é palavra de ligação
		{"o a", 1, 2},           // nunca esvazia: sai uma e sobra uma
		{"de o a", 2, 3},        // só sobra a última
		{"sf de", 0, 1},         // direita
		{"sf de a", 0, 1},       // duas na direita
		{"o sf, de", 1, 2},      // pontuação seguida de espaço ainda separa
		{"o-sf-de", 0, 3},       // tudo colado: nada sai
		{"com por sobre", 2, 3}, // as três são ligação: sobra uma
	}
	for _, tc := range cases {
		ts := tokenizar(tc.texto)
		i, j := apararLado(ts, tc.texto, chaves(ts), 0, len(ts))
		if i != tc.wantI || j != tc.wantJ {
			t.Errorf("apararLado(%q) = (%d, %d), quer (%d, %d)", tc.texto, i, j, tc.wantI, tc.wantJ)
		}
	}
}

func TestAbsorvida(t *testing.T) {
	ks := func(s string) []string { return strings.Fields(s) }
	cases := []struct {
		nome  string
		lado  string // o lado aparado, inteiro
		i, j  int    // o que ficou do lado
		outro string // o outro lado, inteiro
		want  bool
	}{
		{"corte da esquerda, o resto termina o outro lado", "se nao", 1, 2, "senao", true},
		{"corte da esquerda, m vira n", "com tudo", 1, 2, "contudo", true},
		{"corte da direita, o resto começa o outro lado", "cast em", 0, 1, "castem", true},
		{"nada foi cortado", "se nao", 0, 2, "senao", false},
		{"o outro lado é igual ao que sobrou: não absorveu nada", "de sf", 1, 2, "s f", false},
		{"o outro lado é menor que o que sobrou", "o wispr flow", 1, 3, "vispeflow", false},
		{"corte da esquerda, o resto só começa o outro lado", "de sf", 1, 2, "sfx", false},
		{"corte da direita, o resto só termina o outro lado", "fim de", 0, 1, "enfim", false},
		{"o resto não aparece no outro lado", "o sf", 1, 2, "ecf", false},
		{"cortou dos dois lados e o resto fecha o outro", "de sf de", 1, 2, "desfde", false},
	}
	for _, tc := range cases {
		lado := ks(tc.lado)
		if got := absorvida(lado, 0, len(lado), tc.i, tc.j, ks(tc.outro)); got != tc.want {
			t.Errorf("%s: absorvida(%q, %d:%d, %q) = %v, quer %v", tc.nome, tc.lado, tc.i, tc.j, tc.outro, got, tc.want)
		}
	}
}

func TestInicioDeFrase(t *testing.T) {
	cases := []struct {
		texto string
		alvo  string // a palavra cuja posição se testa (a primeira com essa chave)
		want  bool
	}{
		{"Transcrição ficou", "transcricao", true},
		{"  Transcrição ficou", "transcricao", true},
		{"Uso. Transcrição", "transcricao", true},
		{"Uso! Transcrição", "transcricao", true},
		{"Uso? Transcrição", "transcricao", true},
		{"Uso… Transcrição", "transcricao", true},
		{"Uso\nTranscrição", "transcricao", true},
		{"Uso\rTranscrição", "transcricao", true},
		{"Uso.\tTranscrição", "transcricao", true}, // tabulação é espaço
		{"Uso. Transcrição", "transcricao", true},  // espaço que não quebra também
		{"Uso \n Transcrição", "transcricao", true},
		{"\"Transcrição", "transcricao", true},
		{"“Transcrição", "transcricao", true},
		{"‘Transcrição", "transcricao", true},
		{"«Transcrição", "transcricao", true},
		{"'Transcrição", "transcricao", true},
		{"(Transcrição", "transcricao", true},
		{"[Transcrição", "transcricao", true},
		{"{Transcrição", "transcricao", true},
		{"- Transcrição", "transcricao", true},
		{"– Transcrição", "transcricao", true},
		{"— Transcrição", "transcricao", true},
		{"• Transcrição", "transcricao", true},
		{"* Transcrição", "transcricao", true},
		{"1. Transcrição", "transcricao", true},
		{"Uso. (\"Transcrição", "transcricao", true},
		{"Uso, Transcrição", "transcricao", false},
		{"Uso., Transcrição", "transcricao", false}, // a vírgula não é abertura
		{"Uso; Transcrição", "transcricao", false},
		{"Uso: Transcrição", "transcricao", false},
		{"Uso a Transcrição", "transcricao", false},
		{"Uso) Transcrição", "transcricao", false},
		{"e-Social", "social", false},
		{"Uso a.b Transcrição", "transcricao", false},
		// A aspa de fechamento vem depois do ponto: a frase seguinte começa.
		{"“Uso.” Transcrição", "transcricao", true},
		{"‘Uso.’ Transcrição", "transcricao", true},
		{"«Uso.» Transcrição", "transcricao", true},
		{"\"Uso.\" Transcrição", "transcricao", true},
		{"'Uso.' Transcrição", "transcricao", true},
		{"“Uso” Transcrição", "transcricao", false}, // a aspa fecha, mas a frase não acabou
		{"‘Uso’ Transcrição", "transcricao", false},
		{"«Uso» Transcrição", "transcricao", false},
	}
	for _, tc := range cases {
		ts := tokenizar(tc.texto)
		i := -1
		for k, tk := range ts {
			if tk.chave == tc.alvo {
				i = k
				break
			}
		}
		if i < 0 {
			t.Fatalf("%q não tem a palavra %q", tc.texto, tc.alvo)
		}
		if got := inicioDeFrase(tc.texto, ts, i); got != tc.want {
			t.Errorf("inicioDeFrase(%q, %q) = %v, quer %v", tc.texto, tc.alvo, got, tc.want)
		}
	}
}

func TestEhNumero(t *testing.T) {
	cases := map[string]bool{
		"5": true, "2026": true, "1º": true, "2ª": true, "10º": true,
		"º": false, "ª": false, "ºª": false, "v2": false, "10h": false, "n8n": false, "oito": false,
		"1º1": false, "º1": false,
	}
	for k, want := range cases {
		if got := ehNumero(k); got != want {
			t.Errorf("ehNumero(%q) = %v, quer %v", k, got, want)
		}
	}
}

func TestSequenciasDeDigitos(t *testing.T) {
	cases := []struct {
		ks   []string
		want []string
	}{
		{nil, nil},
		{[]string{"sf"}, nil},
		{[]string{"v2"}, []string{"2"}},
		{[]string{"10h"}, []string{"10"}},
		{[]string{"ab1cd22"}, []string{"1", "22"}},
		{[]string{"n8n", "oito", "7"}, []string{"8", "7"}},
	}
	for _, tc := range cases {
		if got := sequenciasDeDigitos(tc.ks); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("sequenciasDeDigitos(%v) = %v, quer %v", tc.ks, got, tc.want)
		}
	}
}

func TestMudaDigito(t *testing.T) {
	cases := []struct {
		de, para []string
		want     bool
	}{
		{[]string{"sf"}, []string{"ecf"}, false},
		{[]string{"n", "oito", "n"}, []string{"n8n"}, false}, // TEST-008: o De não tem dígito
		{[]string{"v", "dois"}, []string{"v2"}, false},
		{[]string{"10h"}, []string{"11h"}, true},
		{[]string{"10h"}, []string{"10h"}, false},
		{[]string{"10h"}, []string{"dez", "horas"}, true},
		{[]string{"ab1cd2"}, []string{"ef1gh"}, true},
		{[]string{"ab1cd2"}, []string{"ef2gh1"}, true},
		{[]string{"ab1cd2"}, []string{"ef1gh2"}, false},
		{[]string{"a1", "b2"}, []string{"c1d2"}, false},
		{[]string{"ab1"}, []string{"cd1ef2"}, true},    // o Para tem mais dígitos
		{[]string{"ab1cd2"}, []string{"ef1gh3"}, true}, // só o segundo muda
	}
	for _, tc := range cases {
		if got := mudaDigito(tc.de, tc.para); got != tc.want {
			t.Errorf("mudaDigito(%v, %v) = %v, quer %v", tc.de, tc.para, got, tc.want)
		}
	}
}

func TestTemPolaridade(t *testing.T) {
	for _, k := range []string{
		"nao", "nunca", "jamais", "sim", "nada", "tudo", "nenhum", "nenhuma", "ninguem",
		"nem", "nenhuns", "nenhumas", "sempre", "so", "somente", "apenas", "mais", "menos",
		"todo", "toda", "todos", "todas",
	} {
		if !temPolaridade([]string{"x", k}) {
			t.Errorf("temPolaridade(%q) = false", k)
		}
	}
	if temPolaridade([]string{"x", "sf", "nao-e", "mas", "sexta"}) || temPolaridade(nil) {
		t.Error("temPolaridade deu verdadeiro sem palavra de polaridade")
	}
}

func TestEncosta(t *testing.T) {
	ks := func(s string) []string { return strings.Fields(s) }
	cases := []struct {
		nome  string
		lado  string // o lado aparado, inteiro
		i0, i int    // o lado começava em i0 e, aparado, em i
		outro string // o outro lado, já aparado
		want  bool
	}{
		{"a nota e anotar: a mesma letra", "a nota", 0, 1, "anotar", true},
		{"com firma e confirmar: o m vira n, a letra é a mesma", "com firma", 0, 1, "confirmar", true},
		{"a que encosta é a última retirada, não a primeira", "de a nota", 0, 2, "anotar", true},
		{"a primeira retirada não é a que encosta", "de a nota", 0, 2, "denotar", false},
		{"nada foi retirado", "a nota", 0, 0, "anotar", false},
		{"o lado já começava depois: nada foi retirado aqui", "a nota", 1, 1, "anotar", false},
		{"o outro lado é vazio", "a nota", 0, 1, "", false},
		{"letras diferentes", "o sf", 0, 1, "ecf", false},
		{"o artigo e o termo novo: a letra é a mesma e o custo é barrar", "o outlook", 0, 1, "otlook", true},
		{"letras diferentes que começam pelo mesmo byte", "я ъ", 0, 1, "ёъ", false},
		{"a mesma letra fora do ASCII", "я ъ", 0, 1, "яъ", true},
	}
	for _, tc := range cases {
		if got := encosta(ks(tc.lado), tc.i0, tc.i, ks(tc.outro)); got != tc.want {
			t.Errorf("%s: encosta(%q, %d:%d, %q) = %v, quer %v", tc.nome, tc.lado, tc.i0, tc.i, tc.outro, got, tc.want)
		}
	}
}

func TestNumeraisDe(t *testing.T) {
	// Os ordinais de primeiro a décimo, nas duas flexões, e alguns cardinais,
	// na forma de chave.
	for _, k := range strings.Fields(
		"primeiro primeira segundo segunda terceiro terceira quarto quarta quinto quinta " +
			"sexto sexta setimo setima oitavo oitava nono nona decimo decima " +
			"zero dois duas tres quatro oito dez vinte cem cento mil milhao milhoes bilhao bilhoes meio meia") {
		if got := numeraisDe([]string{"x", k}); !reflect.DeepEqual(got, []string{k}) {
			t.Errorf("numeraisDe(x, %q) = %v", k, got)
		}
	}
	// Com acento não é chave: Sugerir já entrega a chave.
	if got := numeraisDe([]string{"sf", "um", "uma", "nao", "v2", "oitenta", "nota", "oito"}); !reflect.DeepEqual(got, []string{"oitenta", "oito"}) {
		t.Errorf("numeraisDe = %v, quer [oitenta oito]: um e uma são artigos, e v2 é palavra com dígito", got)
	}
	if numeraisDe(nil) != nil {
		t.Error("numeraisDe(nil) não é vazio")
	}
}

func TestMudaNumeral(t *testing.T) {
	ks := func(s string) []string { return strings.Fields(s) }
	cases := []struct {
		nome, de, para string
		want           bool
	}{
		{"numeral vira outro", "oito", "seis", true},
		{"mil vira milhão", "mil", "milhao", true},
		{"ordinal vira ordinal", "primeiro", "terceiro", true},
		{"numeral vira palavra comum", "oito", "novas", true},
		{"numeral e conjunção viram palavra comum", "mil e quinhentos", "xpto", true},
		{"numeral vira dígito: TEST-008", "oito", "8x", false},
		{"n oito n vira n8n", "n oito n", "n8n", false},
		{"vê dois vira v2", "v dois", "v2", false},
		{"nome soletrado vira outro nome", "k oito s", "kubernetes", false},
		{"nome soletrado vira outro nome e outro numeral", "k oito s", "k sete s", true},
		{"o De não tem numeral", "ceis", "seis", false},
		{"nenhum dos dois", "sf", "ecf", false},
		{"palavra com dígito dos dois lados e numeral nos dois", "oito", "n9 dois", true},
		{"o De é só ligação e numeral, e o Para tem dígito no meio de uma palavra", "de oito", "n8", false},
		{"o De é só ligação e numeral, e o Para é só letra", "de oito", "xpto", true},
	}
	for _, tc := range cases {
		if got := mudaNumeral(ks(tc.de), ks(tc.para)); got != tc.want {
			t.Errorf("%s: mudaNumeral(%q, %q) = %v, quer %v", tc.nome, tc.de, tc.para, got, tc.want)
		}
	}
}

func TestTemDigito(t *testing.T) {
	cases := []struct {
		ks   []string
		want bool
	}{
		{nil, false},
		{[]string{"sf", "oito"}, false},
		{[]string{"sf", "v2"}, true},
		{[]string{"2"}, true},
		{[]string{"1º"}, true},
	}
	for _, tc := range cases {
		if got := temDigito(tc.ks); got != tc.want {
			t.Errorf("temDigito(%v) = %v, quer %v", tc.ks, got, tc.want)
		}
	}
}

func TestIgual(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
		{[]string{"a", "b"}, []string{"b", "a"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"a"}, false},
	}
	for _, tc := range cases {
		if got := igual(tc.a, tc.b); got != tc.want {
			t.Errorf("igual(%v, %v) = %v, quer %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestMaiuscula(t *testing.T) {
	cases := []struct {
		texto string
		want  bool
	}{
		{"Descrição", true}, {"descrição", false}, {"Éden", true}, {"éden", false},
		{"ÉDEN", true}, {"1a", false},
	}
	for _, tc := range cases {
		ts := tokenizar(tc.texto)
		if got := maiuscula(tc.texto, ts[0]); got != tc.want {
			t.Errorf("maiuscula(%q) = %v, quer %v", tc.texto, got, tc.want)
		}
	}
	// A palavra do meio do texto, não a primeira.
	texto := "uso Supabase aqui"
	if ts := tokenizar(texto); !maiuscula(texto, ts[1]) || maiuscula(texto, ts[0]) {
		t.Error("maiuscula olhou a palavra errada")
	}
}
