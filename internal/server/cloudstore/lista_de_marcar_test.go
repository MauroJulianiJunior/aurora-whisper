package cloudstore

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// A mescla da lista de marcar (lista_de_marcar.go), linha a linha da tabela.
// As linhas com ok=false são as do conflito normal: uma diferença de texto em
// alguma linha (ou o mesmo texto repetido marcado de jeitos diferentes), e quem
// chama manda o conteúdo do PC para o selo.
func TestMesclarListaDeMarcar_Tabela(t *testing.T) {
	cases := []struct {
		name            string
		base, nuvem, pc string
		want            string
		ok              bool
	}{
		{
			name:  "itens diferentes marcados em cada lado: mescla, os marcados no fim",
			base:  "- [ ] leite\n- [ ] pão\n- [ ] café",
			nuvem: "- [ ] pão\n- [ ] café\n- [x] leite",
			pc:    "- [ ] leite\n- [ ] café\n- [x] pão",
			want:  "- [ ] café\n- [x] pão\n- [x] leite",
			ok:    true,
		},
		{
			name:  "o mesmo item marcado dos dois lados: mescla, nada muda na nuvem",
			base:  "- [ ] leite\n- [ ] pão",
			nuvem: "- [ ] pão\n- [x] leite",
			pc:    "- [ ] pão\n- [x] leite",
			want:  "- [ ] pão\n- [x] leite",
			ok:    true,
		},
		{
			name:  "um lado desmarca, o outro marca outro item",
			base:  "- [ ] pão\n- [x] leite",
			nuvem: "- [ ] pão\n- [ ] leite",
			pc:    "- [x] leite\n- [x] pão",
			want:  "- [ ] leite\n- [x] pão",
			ok:    true,
		},
		{
			name:  "só o PC mudou: vale o do PC",
			base:  "- [ ] a\n- [ ] b",
			nuvem: "- [ ] a\n- [ ] b",
			pc:    "- [ ] b\n- [x] a",
			want:  "- [ ] b\n- [x] a",
			ok:    true,
		},
		{
			name:  "o PC igual à base: fica a nuvem, byte a byte",
			base:  "- [ ] a\r\n- [ ] b",
			nuvem: "- [x] a\r\n- [ ] b",
			pc:    "- [ ] a\n- [ ] b",
			want:  "- [x] a\r\n- [ ] b",
			ok:    true,
		},
		{
			name:  "o painel leva o marcado para o fim do corpo, passando um parágrafo",
			base:  "- [ ] a\nObs: fecha às 22h\n- [ ] b",
			nuvem: "Obs: fecha às 22h\n- [ ] b\n- [x] a",
			pc:    "- [ ] a\nObs: fecha às 22h\n- [x] b",
			want:  "Obs: fecha às 22h\n- [x] b\n- [x] a",
			ok:    true,
		},
		{
			name:  "dois blocos com o título no meio: só o bloco mexido é reordenado",
			base:  "# Mercado\n- [ ] leite\n- [ ] pão\n# Farmácia\n- [x] dipirona\n- [ ] gaze",
			nuvem: "# Mercado\n- [ ] leite\n- [ ] pão\n# Farmácia\n- [x] dipirona\n- [ ] gaze",
			pc:    "# Mercado\n- [ ] pão\n- [x] leite\n# Farmácia\n- [x] dipirona\n- [ ] gaze",
			want:  "# Mercado\n- [ ] pão\n- [x] leite\n# Farmácia\n- [x] dipirona\n- [ ] gaze",
			ok:    true,
		},
		{
			name:  "o texto repetido: os dois marcam um leite, o número é o mesmo",
			base:  "- [ ] leite\n- [ ] leite",
			nuvem: "- [ ] leite\n- [x] leite",
			pc:    "- [x] leite\n- [ ] leite",
			want:  "- [ ] leite\n- [x] leite",
			ok:    true,
		},
		{
			name:  "o texto repetido: só a nuvem mudou o número",
			base:  "- [ ] leite\n- [ ] leite\n- [ ] pão",
			nuvem: "- [ ] pão\n- [x] leite\n- [x] leite",
			pc:    "- [ ] leite\n- [ ] leite\n- [x] pão",
			want:  "- [x] pão\n- [x] leite\n- [x] leite",
			ok:    true,
		},
		{
			name:  "o X maiúsculo é marcado, e a linha que não muda fica como está",
			base:  "- [X] a\n- [ ] b",
			nuvem: "- [X] a\n- [ ] b",
			pc:    "- [x] a\n- [x] b",
			want:  "- [X] a\n- [x] b",
			ok:    true,
		},
		{
			name:  "CRLF e a quebra de linha do fim não contam; a nuvem guarda as dela",
			base:  "- [ ] a\r\n- [ ] b\r\n",
			nuvem: "- [ ] a\n- [ ] b\n\n",
			pc:    "- [ ] b\r\n- [x] a",
			want:  "- [ ] b\n- [x] a\n\n",
			ok:    true,
		},
		{
			name:  "a tarefa vazia, sem o espaço depois do colchete",
			base:  "- [ ]\n- [ ] a",
			nuvem: "- [ ]\n- [ ] a",
			pc:    "- [x]\n- [ ] a",
			want:  "- [ ] a\n- [x] ",
			ok:    true,
		},
		{
			name:  "base vazia e os dois vazios: nada a mesclar",
			base:  "",
			nuvem: "",
			pc:    "",
			want:  "",
			ok:    true,
		},
		// Os conflitos normais.
		{
			name:  "base vazia e a lista nos dois lados (itens acrescentados)",
			base:  "",
			nuvem: "- [ ] leite",
			pc:    "- [x] leite",
		},
		{
			name:  "um item editado",
			base:  "- [ ] leite\n- [ ] pão",
			nuvem: "- [ ] pão\n- [x] leite",
			pc:    "- [ ] leite integral\n- [ ] pão",
		},
		{
			name:  "um item acrescentado",
			base:  "- [ ] leite",
			nuvem: "- [x] leite",
			pc:    "- [ ] leite\n- [ ] pão",
		},
		{
			name:  "um item tirado",
			base:  "- [ ] leite\n- [ ] pão",
			nuvem: "- [ ] pão\n- [x] leite",
			pc:    "- [ ] leite",
		},
		{
			name:  "um parágrafo mudado",
			base:  "Mercado do sábado\n- [ ] leite",
			nuvem: "Mercado do sábado\n- [x] leite",
			pc:    "Mercado de domingo\n- [ ] leite",
		},
		{
			name:  "uma linha fixa a mais",
			base:  "- [ ] leite",
			nuvem: "- [x] leite",
			pc:    "- [ ] leite\nObs",
		},
		{
			name:  "uma tarefa que virou marcador",
			base:  "- [ ] leite\n- [ ] pão",
			nuvem: "- [ ] pão\n- [x] leite",
			pc:    "- leite\n- [ ] pão",
		},
		{
			name:  "o texto repetido marcado de jeitos diferentes nos dois lados",
			base:  "- [ ] leite\n- [ ] leite",
			nuvem: "- [ ] leite\n- [x] leite",
			pc:    "- [x] leite\n- [x] leite",
		},
	}
	for _, c := range cases {
		got, ok := MesclarListaDeMarcar(c.base, c.nuvem, c.pc)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%s:\n  = %q, %v\n  quero %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// A propriedade: uma lista qualquer (com textos repetidos, parágrafos e
// títulos no meio), marcada e desmarcada ao acaso dos dois lados (cada lado
// com a reordenação do painel, que leva os marcados para o fim), sempre mescla
// quando não há texto repetido mudado dos dois lados; o resultado tem todas as
// linhas da base (nenhum item se perde nem aparece), as fixas na mesma ordem, e
// cada item com o estado da regra de três vias. Determinístico (a semente é
// fixa).
func TestMesclarListaDeMarcar_Propriedade(t *testing.T) {
	rng := rand.New(rand.NewSource(22))
	textos := []string{"leite", "pão", "café", "ovos", "maçã", "arroz", "feijão", "sabão"}
	type linha struct {
		fixa  string
		texto string
		feito bool
	}
	escrever := func(ls []linha) string {
		out := make([]string, len(ls))
		for i, l := range ls {
			switch {
			case l.fixa != "":
				out[i] = l.fixa
			case l.feito:
				out[i] = "- [x] " + l.texto
			default:
				out[i] = "- [ ] " + l.texto
			}
		}
		return strings.Join(out, "\n")
	}
	// painel marca e desmarca ao acaso e leva os marcados para o fim do corpo,
	// como normalizarMarcados.
	painel := func(ls []linha) []linha {
		mud := append([]linha(nil), ls...)
		for i := range mud {
			if mud[i].fixa == "" && rng.Intn(3) == 0 {
				mud[i].feito = !mud[i].feito
			}
		}
		var abertos, marcados []linha
		for _, l := range mud {
			if l.fixa == "" && l.feito {
				marcados = append(marcados, l)
			} else {
				abertos = append(abertos, l)
			}
		}
		return append(abertos, marcados...)
	}
	contar := func(ls []linha) (fixas []string, itens, marcados map[string]int) {
		itens, marcados = map[string]int{}, map[string]int{}
		for _, l := range ls {
			if l.fixa != "" {
				fixas = append(fixas, l.fixa)
				continue
			}
			itens[l.texto]++
			if l.feito {
				marcados[l.texto]++
			}
		}
		return fixas, itens, marcados
	}
	mesclou := 0
	for rodada := 0; rodada < 3000; rodada++ {
		var base []linha
		for i, n := 0, 1+rng.Intn(10); i < n; i++ {
			if rng.Intn(5) == 0 {
				base = append(base, linha{fixa: fmt.Sprintf("# seção %d", i)})
				continue
			}
			base = append(base, linha{texto: textos[rng.Intn(len(textos))], feito: rng.Intn(2) == 0})
		}
		nuvem, pc := painel(base), painel(base)
		got, ok := MesclarListaDeMarcar(escrever(base), escrever(nuvem), escrever(pc))

		fb, ib, mb := contar(base)
		_, _, mn := contar(nuvem)
		_, _, mp := contar(pc)
		conflitoDeRepetido := false
		for texto := range ib {
			if mn[texto] != mb[texto] && mp[texto] != mb[texto] && mn[texto] != mp[texto] {
				conflitoDeRepetido = true
			}
		}
		if ok == conflitoDeRepetido {
			t.Fatalf("rodada %d: ok=%v com conflito de repetido=%v\nbase  %q\nnuvem %q\npc    %q",
				rodada, ok, conflitoDeRepetido, escrever(base), escrever(nuvem), escrever(pc))
		}
		if !ok {
			continue
		}
		mesclou++
		lg, _ := lerCorpo(got)
		var res []linha
		for _, l := range lg {
			if !l.tarefa {
				res = append(res, linha{fixa: l.raw})
				continue
			}
			res = append(res, linha{texto: l.texto, feito: l.feito})
		}
		fr, ir, mr := contar(res)
		if fmt.Sprint(fr) != fmt.Sprint(fb) || !mesmosItens(ir, ib) {
			t.Fatalf("rodada %d: o resultado perdeu ou ganhou linha\nbase %q\ngot  %q", rodada, escrever(base), got)
		}
		for texto := range ib {
			quer := mp[texto]
			if mn[texto] != mb[texto] {
				quer = mn[texto]
			}
			if mr[texto] != quer {
				t.Fatalf("rodada %d: %q com %d marcados, quero %d\nbase  %q\nnuvem %q\npc    %q\ngot   %q",
					rodada, texto, mr[texto], quer, escrever(base), escrever(nuvem), escrever(pc), got)
			}
		}
		// A mescla de novo, com o resultado no lugar da nuvem, não muda nada
		// (o PC que reenviasse a mesma coisa).
		if again, ok := MesclarListaDeMarcar(escrever(base), got, escrever(pc)); !ok || again != got {
			t.Fatalf("rodada %d: mesclar de novo deu %q, %v; quero %q", rodada, again, ok, got)
		}
	}
	if mesclou < 2000 {
		t.Fatalf("só %d das 3000 rodadas mesclaram: o gerador não exercita a mescla", mesclou)
	}
}

// As linhas de tarefa que o painel escreve, e as que não são tarefa.
func TestLerTarefa(t *testing.T) {
	for l, want := range map[string]string{
		"- [ ] leite": "aberto:leite", "- [x] leite": "feito:leite", "- [X] leite": "feito:leite",
		"- [ ]": "aberto:", "- [x] ": "feito:", "- [ ]  dois espaços": "aberto: dois espaços",
		"- [ ]x": "", "- [y] a": "", "-[ ] a": "", "- leite": "", "* [ ] a": "", "  - [ ] a": "", "": "",
	} {
		feito, texto, ok := lerTarefa(l)
		got := ""
		if ok {
			got = map[bool]string{false: "aberto", true: "feito"}[feito] + ":" + texto
		}
		if got != want {
			t.Errorf("lerTarefa(%q) = %q, quero %q", l, got, want)
		}
	}
	// A ordem dos itens não conta: o mesmo multiconjunto em outra ordem.
	a, _ := lerCorpo("- [ ] a\n- [x] b")
	b, _ := lerCorpo("- [x] b\n- [ ] a")
	_, ia, _ := esqueleto(a)
	_, ib, _ := esqueleto(b)
	ks := func(m map[string]int) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if !mesmosItens(ia, ib) || fmt.Sprint(ks(ia)) != "[a b]" {
		t.Errorf("os itens: %v e %v", ia, ib)
	}
}
