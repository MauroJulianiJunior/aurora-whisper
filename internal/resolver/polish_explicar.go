package resolver

import (
	"fmt"
	"sort"
	"strings"
)

// Explicações das guardas para o diagnóstico do polishbench -historico, que
// mostra no terminal de quem roda POR QUE uma resposta foi recusada.
//
// PRIVACIDADE: estas funções devolvem TEXTO do ditado (a sigla, o nome, a
// palavra). Servem só ao terminal do diagnóstico; nunca vão a log, relatório,
// tela do Agent nem ao provedor (AC-048). O Agent não as chama.

// ExplicarSiglasTermos diz o que a guarda de siglas e termos viu de errado:
// cada sigla da entrada que a saída não tem o mesmo número de vezes, cada
// nome que sumiu e cada termo que não reaparece. Vazio quando a guarda passa.
func ExplicarSiglasTermos(entrada, saida string, termos []string) []string {
	var out []string
	siglasSaida := contarSiglas(saida)
	var siglas []string
	for s := range contarSiglas(entrada) {
		siglas = append(siglas, s)
	}
	sort.Strings(siglas)
	siglasEntrada := contarSiglas(entrada)
	for _, s := range siglas {
		n, m := siglasEntrada[s], siglasSaida[s]
		if n == m {
			continue
		}
		linha := fmt.Sprintf("sigla %q: %d na entrada, %d na saída", s, n, m)
		if outras := grafiasNaSaida(saida, s); len(outras) > 0 {
			linha += "; na saída, " + strings.Join(outras, ", ")
		}
		out = append(out, linha)
	}

	sai := []rune(saida)
	nomes := contarNomes(entrada)
	var lista []string
	for nome := range nomes {
		lista = append(lista, nome)
	}
	sort.Strings(lista)
	for _, nome := range lista {
		if m := contarRunas(sai, []rune(nome), false, false); m < nomes[nome] {
			linha := fmt.Sprintf("nome %q: %d na entrada, %d na saída", nome, nomes[nome], m)
			if outras := grafiasNaSaida(saida, nome); len(outras) > 0 {
				linha += "; na saída, " + strings.Join(outras, ", ")
			}
			out = append(out, linha)
		}
	}

	for _, t := range termos {
		if !guardaSiglasTermos(entrada, saida, []string{t}) {
			out = append(out, fmt.Sprintf("termo %q: não reaparece com a grafia da entrada", strings.TrimSpace(t)))
		}
	}
	return out
}

// grafiasNaSaida devolve as outras grafias da palavra na saída (sem
// distinguir caixa), com quantas vezes cada uma aparece: "SKILL" que virou
// "skill".
func grafiasNaSaida(saida, palavra string) []string {
	conta := map[string]int{}
	for _, w := range palavrasComHifen(saida) {
		if w != palavra && strings.EqualFold(w, palavra) {
			conta[w]++
		}
	}
	var out []string
	for w, n := range conta {
		out = append(out, fmt.Sprintf("%q %dx", w, n))
	}
	sort.Strings(out)
	return out
}

// CorrecaoDeGrafiaAceita diz se trocar saiu por entrou é a correção de
// grafia que a guarda de palavras aceita (polish_grafia.go): uma palavra por
// outra, duas por uma ou uma por duas. Olha só o trecho, sem a frase em volta:
// a guarda, com a frase, também recusa a palavra com maiúscula no meio dela.
func CorrecaoDeGrafiaAceita(saiu, entrou []string) bool {
	ta, _ := tokenizar(strings.Join(saiu, " "))
	tb, _ := tokenizar(strings.Join(entrou, " "))
	a, b := palavrasDe(ta), palavrasDe(tb)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	c := novasCorrecoes(a, ta, make([]marcaEstrutural, len(a)), b, tb)
	switch {
	case len(a) == 1 && len(b) == 1:
		return c.troca(0, 0)
	case len(a) == 2 && len(b) == 1:
		return c.junta(0, 0)
	case len(a) == 1 && len(b) == 2:
		return c.separa(0, 0)
	}
	return false
}
