package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ortografia"
)

// A correção de grafia na AUDITORIA (ADR-016, emenda de 18-out-2026).
//
// A guarda de palavras aceita que o modelo corrija o erro do Whisper
// ("vejante" -> "vigente"); sem esta regra, a auditoria contava a correção
// aceita como `acrescimo` depois da guarda, um falso "buraco na guarda". Como o
// resto da auditoria, a regra é escrita aqui, à parte do resolver, com as
// mesmas consultas ao dicionário (internal/ortografia): a política é a que o
// Mauro decidiu, e a auditoria não pode ser nem mais estrita nem mais frouxa.
//
// A palavra errada tem de estar na ENTRADA e não existir em português; a certa
// tem de existir como palavra comum, ter a mesma primeira letra e a escrita
// parecida pela pronúncia. Ou duas viram uma com as mesmas letras ("e mail" ->
// "e-mail"), ou uma vira duas ("derepente" -> "de repente"). Nunca em número,
// negação ou quantificação, sigla ou nome (maiúscula no meio da frase).
//
// A diferença só conta quando o modelo e o esperado discordam, e é leve nos
// dois sentidos: o modelo corrigiu o que o esperado manteve (a guarda aceita),
// ou manteve o que o esperado corrigiu (é o texto do Smart). A correção para
// OUTRA palavra que não a do esperado ("vejante" -> "vidente", quando o
// esperado diz "vigente") continua acréscimo: mudou o que a pessoa disse.

// separarGrafia acha, entre as palavras omitidas do esperado e as extras da
// saída, as que só diferem por uma correção de grafia, e as tira das duas
// listas. ok=false quando não acha nenhuma. Só pareia palavras da mesma
// lacuna do alinhamento (entre as mesmas palavras casadas), para a palavra
// apagada num lugar não "virar" a acrescentada em outro.
func separarGrafia(pe, ps, pi []palavra, pares []par, omitidas, extras []int) (restoO, restoE []int, ok bool) {
	naEntrada := contagem(normas(pi))
	lacE, lacS := lacunas(pares, len(pe), true), lacunas(pares, len(ps), false)
	livreO, livreE := conjuntoDe(omitidas), conjuntoDe(extras)

	// grupos devolve a palavra i e, se a seguinte também estiver livre, o par
	// (i, i+1): os candidatos a um lado de uma correção.
	grupos := func(i int, livre map[int]bool, lac []int) [][]int {
		g := [][]int{{i}}
		if livre[i+1] && lac[i+1] == lac[i] {
			g = append(g, []int{i, i + 1})
		}
		return g
	}
	pegar := func(ix []int, ps []palavra) []palavra {
		out := make([]palavra, len(ix))
		for k, i := range ix {
			out[k] = ps[i]
		}
		return out
	}

	for _, j := range extras {
		if !livreE[j] {
			continue
		}
	procura:
		for _, i := range omitidas {
			if !livreO[i] || lacE[i] != lacS[j] {
				continue
			}
			for _, gE := range grupos(i, livreO, lacE) {
				for _, gS := range grupos(j, livreE, lacS) {
					de, para := pegar(gE, pe), pegar(gS, ps)
					if corrigeGrafia(de, para, naEntrada) || corrigeGrafia(para, de, naEntrada) {
						for _, k := range gE {
							delete(livreO, k)
						}
						for _, k := range gS {
							delete(livreE, k)
						}
						ok = true
						break procura
					}
				}
			}
		}
	}
	if !ok {
		return omitidas, extras, false
	}
	return filtrar(omitidas, livreO), filtrar(extras, livreE), true
}

// lacunas dá, para cada palavra de um lado, quantas palavras casadas vêm
// antes dela: duas palavras não casadas estão na mesma lacuna quando têm o
// mesmo número.
func lacunas(pares []par, n int, ladoA bool) []int {
	out := make([]int, n)
	for i := range out {
		for _, p := range pares {
			k := p.B
			if ladoA {
				k = p.A
			}
			if k < i {
				out[i]++
			}
		}
	}
	return out
}

func conjuntoDe(ix []int) map[int]bool {
	m := make(map[int]bool, len(ix))
	for _, i := range ix {
		m[i] = true
	}
	return m
}

func filtrar(ix []int, ficam map[int]bool) []int {
	var out []int
	for _, i := range ix {
		if ficam[i] {
			out = append(out, i)
		}
	}
	return out
}

// corrigeGrafia diz se "para" é a correção de grafia de "de": uma palavra por
// uma, duas por uma ou uma por duas. As palavras de "de" têm de estar na
// entrada.
func corrigeGrafia(de, para []palavra, naEntrada map[string]int) bool {
	for _, p := range de {
		if naEntrada[p.Norm] == 0 || !palavraLivre(p) {
			return false
		}
	}
	for _, p := range para {
		if !palavraLivre(p) || !ortografia.ExisteComum(p.Norm) {
			return false
		}
	}
	switch {
	case len(de) == 1 && len(para) == 1:
		return !ortografia.Existe(de[0].Norm) && semSentidoNovo(de[0].Norm, para[0].Norm) &&
			escritaParecida(de[0].Norm, para[0].Norm)
	case len(de) == 2 && len(para) == 1:
		junta := de[0].Norm + de[1].Norm
		return (!ortografia.Existe(de[0].Norm) || !ortografia.Existe(de[1].Norm)) &&
			strings.ReplaceAll(para[0].Norm, "-", "") == junta && semSentidoNovo(junta, para[0].Norm)
	case len(de) == 1 && len(para) == 2:
		return !ortografia.Existe(de[0].Norm) &&
			para[0].Norm+para[1].Norm == strings.ReplaceAll(de[0].Norm, "-", "") &&
			semSentidoNovo(de[0].Norm, para[0].Norm) && semSentidoNovo(de[0].Norm, para[1].Norm)
	}
	return false
}

// palavraLivre diz se a palavra pode entrar numa correção de grafia: só
// letras (e hífen), não é número, e não é nome nem sigla (maiúscula fora do
// começo da frase; a sigla trocada já é sigla_termo). A palavra de sentido
// não precisa de regra aqui: do lado errado ela existe, e do lado certo quem a
// barra é semSentidoNovo, que deixa passar o acento que faltava ("nao" ->
// "não").
func palavraLivre(p palavra) bool {
	if p.Norm == "" || ehNumerica(p.Norm) {
		return false
	}
	if r, _ := utf8.DecodeRuneInString(p.Bruta); unicode.IsUpper(r) && !p.AbreFrase {
		return false
	}
	for _, r := range p.Norm {
		if !unicode.IsLetter(r) && r != '-' {
			return false
		}
	}
	return true
}

// semSentidoNovo diz se a palavra certa não traz negação nem quantificação
// que a errada não tinha, fora o acento que faltava ("nao" -> "não").
func semSentidoNovo(de, para string) bool {
	return !palavrasDeSentido[para] || semAcento(de) == semAcento(para)
}

// somIgual unifica as grafias de mesmo som, em que o Whisper erra: "vejante"
// e "vigente" diferem em três letras, e em duas pela pronúncia.
var somIgual = strings.NewReplacer(
	"ch", "x", "ss", "s", "ç", "s", "z", "s",
	"ge", "je", "gi", "ji", "gé", "jé", "gê", "jê", "gí", "jí",
	"ce", "se", "ci", "si", "cé", "sé", "cê", "sê", "cí", "sí",
)

// escritaParecida: pela pronúncia e sem acento, a mesma primeira letra e no
// máximo 1 letra de diferença em palavra de 4 a 6 letras, 2 a partir de 7; até
// 3 letras, só o acento ou o som.
func escritaParecida(de, para string) bool {
	a, b := []rune(semAcento(somIgual.Replace(de))), []rune(semAcento(somIgual.Replace(para)))
	if de == para || len(a) == 0 || len(b) == 0 || a[0] != b[0] {
		return false
	}
	limite := 0
	switch {
	case len(a) >= 7:
		limite = 2
	case len(a) >= 4:
		limite = 1
	}
	return levenshtein(a, b) <= limite
}

func levenshtein(a, b []rune) int {
	ant := make([]int, len(b)+1)
	atual := make([]int, len(b)+1)
	for j := range ant {
		ant[j] = j
	}
	for i := 1; i <= len(a); i++ {
		atual[0] = i
		for j := 1; j <= len(b); j++ {
			custo := 1
			if a[i-1] == b[j-1] {
				custo = 0
			}
			atual[j] = min(ant[j]+1, atual[j-1]+1, ant[j-1]+custo)
		}
		ant, atual = atual, ant
	}
	return ant[len(b)]
}
