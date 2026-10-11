package main

import (
	"strings"
	"unicode"
)

// Métrica "maiúscula a mais" (19-eval-plan, "Polish"; ADR-016, nota de
// 30-set-2026). No uso real o gpt-6-luna devolveu "1º DCTFWeb de Agosto, 2º
// PGDAS da Padaria e 3º Recibo da ECF": maiúscula em mês e em substantivo comum
// no MEIO da frase. A guarda não barra isso (só barra palavra TODA em
// maiúscula, e sem léxico não dá para saber o que é nome próprio); quem cuida
// é a regra de maiúscula do prompt (1.0.2), e esta métrica só mede se ela
// funcionou. Não entra na regra de escolha.
//
// Uma palavra conta quando:
//   - a saída a escreve com inicial maiúscula NO MEIO da frase: não é a
//     primeira palavra do texto, nem a que vem depois de ".", "!", "?", "…" ou
//     ":", nem a primeira de uma linha (o que inclui o item de lista, depois do
//     marcador); e
//   - o esperado, que tem as mesmas palavras, a escreve com inicial minúscula.
//
// Nome próprio, sigla e termo do dicionário que o esperado já capitaliza não
// contam: o esperado é a régua.

// inicialMaiuscula diz se a palavra abre com letra maiúscula.
func inicialMaiuscula(bruta string) bool {
	for _, r := range bruta {
		return unicode.IsUpper(r)
	}
	return false
}

// inicialMinuscula diz se a palavra abre com letra minúscula. Dígito não é
// letra: "1º" e "2026" não são minúscula nem maiúscula.
func inicialMinuscula(bruta string) bool {
	for _, r := range bruta {
		return unicode.IsLower(r)
	}
	return false
}

// noMeioDaFrase diz, para cada palavra, se ela está no meio de uma frase da
// própria saída. AbreFrase já cobre o começo do texto, o começo de linha (item
// de lista incluído, porque palavrasDe tira o marcador) e o que vem depois de
// ".", "!", "?" e "…"; aqui entra o ":" (a lista e a explicação que ele
// anuncia abrem com maiúscula).
func noMeioDaFrase(ps []palavra) []bool {
	meio := make([]bool, len(ps))
	for i, p := range ps {
		abre := p.AbreFrase
		if i > 0 && strings.ContainsRune(ps[i-1].Depois, ':') {
			abre = true
		}
		meio[i] = !abre
	}
	return meio
}

// maiusculasAMais devolve as palavras da saída que abrem com maiúscula no meio
// da frase e que o esperado escreve com minúscula, na ordem da saída, na
// grafia da saída.
//
// A comparação é palavra a palavra: as duas listas são alinhadas por igualdade
// de forma (alinharPalavras), e cada par é comparado. Uma palavra da saída sem
// par (o modelo acrescentou, trocou ou mudou de lugar) não tem o que comparar;
// nela vale só o que é certo sem alinhamento: conta se o esperado a tem e a tem
// SEMPRE com minúscula. Sem par e sem a palavra no esperado, não conta (é
// acréscimo, e a falha crítica `acrescimo` cuida dele).
func maiusculasAMais(esperado, saida string) []string {
	pe, ps := palavrasDe(esperado), palavrasDe(saida)
	meio := noMeioDaFrase(ps)
	pares := alinharPalavras(normas(pe), normas(ps))
	casada := make([]int, len(ps)) // índice da palavra do esperado, ou -1
	for j := range casada {
		casada[j] = -1
	}
	for _, p := range pares {
		casada[p.B] = p.A
	}

	// Por forma: como o esperado a escreve, para o caso de a palavra da saída não
	// ter par.
	sempreMinuscula := map[string]bool{}
	for _, p := range pe {
		anterior, visto := sempreMinuscula[p.Norm]
		sempreMinuscula[p.Norm] = (!visto || anterior) && inicialMinuscula(p.Bruta)
	}

	var extras []string
	for j, p := range ps {
		if !meio[j] || !inicialMaiuscula(p.Bruta) {
			continue
		}
		if i := casada[j]; i >= 0 {
			if inicialMinuscula(pe[i].Bruta) {
				extras = append(extras, p.Bruta)
			}
			continue
		}
		if sempreMinuscula[p.Norm] {
			extras = append(extras, p.Bruta)
		}
	}
	return extras
}
