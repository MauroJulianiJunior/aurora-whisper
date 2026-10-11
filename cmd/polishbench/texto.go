package main

import (
	"regexp"
	"strings"
	"unicode"
)

// Utilitários de texto do benchmark. São o INSTRUMENTO de medição e, de
// propósito, não reaproveitam as guardas do resolver: as guardas decidem o que
// chega ao campo, e o benchmark precisa medir se elas acertaram. Medir com o
// mesmo código que se quer auditar daria sempre 0 falha depois da guarda.

// palavra é uma palavra do texto com o que a cerca.
type palavra struct {
	// Norm é a forma de comparação: minúscula, sem a pontuação das bordas.
	// Acento conta ("debora" e "débora" são palavras diferentes); pontuação de
	// dentro da palavra fica ("1.847,32", "efd-reinf", "meio-dia").
	Norm string
	// Bruta é a grafia original, sem a pontuação das bordas.
	Bruta string
	// Depois é a pontuação colada logo depois da palavra ("," "." "?!").
	Depois string
	// AbreFrase diz que a palavra abre uma frase: começo do texto, começo de
	// linha ou logo depois de ".", "!", "?" ou "…".
	AbreFrase bool
}

// reMarcadorDeLista casa o marcador de lista no começo da linha: "- ", "* ",
// "• ", "1. " ou "12) ". Exige espaço depois: "1.500" e "-5" não são marcador.
var reMarcadorDeLista = regexp.MustCompile(`^[ \t]*(?:[-*•]|\d{1,3}[.)])[ \t]+`)

// reMoedaColada acha a moeda colada no número ("R$1500"): palavrasDe a separa
// ("R$ 1500"), para "R$1500" e "R$ 1.500" terem a mesma palavra de moeda e o
// desvio ficar só no número.
var reMoedaColada = regexp.MustCompile(`(R\$|US\$|\$|€|£)(\d)`)

var simboloDeMoeda = map[string]bool{"R$": true, "US$": true, "$": true, "€": true, "£": true}

func ehLetraOuDigito(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// palavrasDe parte o texto em palavras, na ordem. O marcador de lista do
// começo de cada linha não é palavra. Um pedaço só de pontuação ("–", "...")
// cola na palavra anterior.
func palavrasDe(s string) []palavra {
	var out []palavra
	abre := true
	for _, linha := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if loc := reMarcadorDeLista.FindStringIndex(linha); loc != nil {
			linha = linha[loc[1]:]
		}
		abre = true // toda linha abre uma frase
		for _, campo := range strings.Fields(reMoedaColada.ReplaceAllString(linha, "$1 $2")) {
			if simboloDeMoeda[campo] {
				// "R$" e "US$" não são palavra: a troca de moeda aparece no
				// número ("R$1.500" contra "US$1.500"), não numa sigla "US".
				if n := len(out); n > 0 {
					out[n-1].Depois += campo
				}
				continue
			}
			runas := []rune(campo)
			ini, fim := 0, len(runas)
			for ini < fim && !ehLetraOuDigito(runas[ini]) {
				ini++
			}
			for fim > ini && !ehLetraOuDigito(runas[fim-1]) {
				fim--
			}
			if ini == fim {
				if n := len(out); n > 0 {
					out[n-1].Depois += campo
					if strings.ContainsAny(campo, ".!?…") {
						abre = true
					}
				}
				continue
			}
			bruta := string(runas[ini:fim])
			out = append(out, palavra{
				Norm:      strings.ToLower(bruta),
				Bruta:     bruta,
				Depois:    string(runas[fim:]),
				AbreFrase: abre,
			})
			abre = strings.ContainsAny(string(runas[fim:]), ".!?…")
		}
	}
	return out
}

// normas devolve só a forma de comparação de cada palavra.
func normas(ps []palavra) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Norm
	}
	return out
}

// sinalDepois é o sinal de pontuação que fecha a palavra: o primeiro entre
// "," "." "?" "!" ":" ";" colados nela. "…" vale como ".". Zero quando não há.
func sinalDepois(p palavra) rune {
	for _, r := range p.Depois {
		switch r {
		case ',', '.', '?', '!', ':', ';':
			return r
		case '…':
			return '.'
		}
	}
	return 0
}

// maxCelulasAlinhamento limita a tabela dos alinhamentos: um ditado real tem
// centenas de palavras. Acima disto o alinhamento devolve "nada casou".
const maxCelulasAlinhamento = 4_000_000

// par é uma palavra de a casada com uma de b.
type par struct{ A, B int }

// alinharPalavras devolve o maior alinhamento (subsequência comum mais longa)
// entre a e b, por igualdade de Norm. Nos empates prefere casar as palavras
// mais cedo.
func alinharPalavras(a, b []string) []par {
	n, m := len(a), len(b)
	if n == 0 || m == 0 || (n+1)*(m+1) > maxCelulasAlinhamento {
		return nil
	}
	w := m + 1
	d := make([]int32, (n+1)*w)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				d[i*w+j] = d[(i+1)*w+j+1] + 1
			} else if d[(i+1)*w+j] >= d[i*w+j+1] {
				d[i*w+j] = d[(i+1)*w+j]
			} else {
				d[i*w+j] = d[i*w+j+1]
			}
		}
	}
	var pares []par
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			pares = append(pares, par{i, j})
			i++
			j++
		case d[(i+1)*w+j] >= d[i*w+j+1]:
			i++
		default:
			j++
		}
	}
	return pares
}

// naoCasadas devolve os índices de 0..n-1 que não aparecem em usados, lado A
// (ladoA=true) ou lado B.
func naoCasadas(n int, pares []par, ladoA bool) []int {
	visto := make([]bool, n)
	for _, p := range pares {
		if ladoA {
			visto[p.A] = true
		} else {
			visto[p.B] = true
		}
	}
	var out []int
	for i, v := range visto {
		if !v {
			out = append(out, i)
		}
	}
	return out
}

// distanciaDePalavras é a distância de edição (Levenshtein) entre as duas
// listas de palavras: trocar, apagar e acrescentar custam 1.
func distanciaDePalavras(a, b []string) int {
	n, m := len(a), len(b)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}
	ant := make([]int, m+1)
	cur := make([]int, m+1)
	for j := range ant {
		ant[j] = j
	}
	for i := 1; i <= n; i++ {
		cur[0] = i
		for j := 1; j <= m; j++ {
			custo := 1
			if a[i-1] == b[j-1] {
				custo = 0
			}
			cur[j] = min(ant[j]+1, cur[j-1]+1, ant[j-1]+custo)
		}
		ant, cur = cur, ant
	}
	return ant[m]
}

// estrutura é o esqueleto do texto: quantos parágrafos e quantos itens de
// lista, e de que tipo são os itens.
type estrutura struct {
	Paragrafos int
	Itens      int
	// Marcador é o tipo do PRIMEIRO item: "numerado" ("1."), "traco" ("-",
	// "*", "•") ou "" sem lista.
	Marcador string
}

var reMarcadorNumerado = regexp.MustCompile(`^[ \t]*\d{1,3}[.)][ \t]+`)

// estruturaDe conta parágrafos e itens. Parágrafo é um bloco de linhas
// seguidas, separado do próximo por linha em branco; um bloco só de itens que
// vem depois de uma linha em branco continua o bloco anterior (a lista que a
// frase de apresentação anunciou não é um parágrafo novo). Item é a linha que
// abre com marcador de lista.
func estruturaDe(s string) estrutura {
	var e estrutura
	emBloco := false
	for _, linha := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(linha) == "" {
			emBloco = false
			continue
		}
		item := reMarcadorDeLista.MatchString(linha)
		if item {
			e.Itens++
			if e.Marcador == "" {
				e.Marcador = "traco"
				if reMarcadorNumerado.MatchString(linha) {
					e.Marcador = "numerado"
				}
			}
		}
		if !emBloco && (!item || e.Paragrafos == 0) {
			e.Paragrafos++
		}
		emBloco = true
	}
	return e
}

// mesmaEstrutura diz se dois textos têm o mesmo número de parágrafos e de
// itens (é o "acerto de estrutura" do plano; o tipo do marcador é medido à
// parte).
func mesmaEstrutura(a, b estrutura) bool {
	return a.Paragrafos == b.Paragrafos && a.Itens == b.Itens
}
