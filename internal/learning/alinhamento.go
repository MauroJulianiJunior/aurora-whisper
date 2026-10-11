package learning

import (
	"strings"
	"unicode"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Este arquivo tem as duas peças de baixo nível de Sugerir: dividir um texto
// em palavras comparáveis e alinhar duas sequências de palavras. Nenhuma
// delas conhece dicionário, regra de aprendizado ou log.

// token é uma palavra do texto: a sequência máxima de letras e dígitos.
//
// Comparar por palavra (e não por caractere, nem por trecho) é o que faz
// maiúscula, acento e pontuação desaparecerem da comparação sem nenhuma
// regra extra: "Recíbo," e "recibo" viram a mesma chave.
type token struct {
	// chave é dictionary.Normalize da palavra: minúscula, sem acento. É o
	// que se compara.
	chave string
	// ini e fim são os deslocamentos, em bytes, da palavra no texto
	// original. É o que permite devolver o trecho COMO ESTAVA escrito, e
	// não a chave.
	ini, fim int
}

// tokenizar divide s em palavras. Hífen, ponto, barra e afins separam: a
// mesma regra de dictionary.Normalize, para que "EFD-Reinf" e "EFD Reinf"
// sejam a mesma sequência de chaves.
func tokenizar(s string) []token {
	var out []token
	ini := -1
	for i, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if ini < 0 {
				ini = i
			}
			continue
		}
		if ini >= 0 {
			out = append(out, token{dictionary.Normalize(s[ini:i]), ini, i})
			ini = -1
		}
	}
	if ini >= 0 {
		out = append(out, token{dictionary.Normalize(s[ini:]), ini, len(s)})
	}
	return out
}

func chaves(ts []token) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.chave
	}
	return out
}

// fatia devolve o texto de ts[i:j] como estava escrito, do começo da
// primeira palavra ao fim da última. A pontuação de fora fica de fora (ela é
// da frase, não do termo); a de dentro ("Node.js", "EFD-Reinf") fica.
func fatia(ts []token, texto string, i, j int) string {
	return texto[ts[i].ini:ts[j-1].fim]
}

// compacta troca toda sequência de espaço e quebra de linha por um espaço.
func compacta(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// maxCelulas limita a tabela do alinhamento. Sem teto, dois textos enormes
// pediriam memória proporcional ao produto dos tamanhos. O prefixo e o
// sufixo em comum saem antes da tabela, então o que conta é só o miolo que
// mudou: um ditado de centenas de palavras com uma correção no meio custa
// quase nada, e o teto só pega reescrita de texto muito longo.
const maxCelulas = 4_000_000

// pareamento alinha a e b e devolve os pares (i, j), em ordem, de uma
// subsequência comum máxima: a[i] == b[j] para cada par. Tudo o que fica
// entre dois pares é o que mudou.
//
// ok é false quando o miolo a comparar passa de maxCelulas.
func pareamento(a, b []string) (pares [][2]int, ok bool) {
	ini := 0
	for ini < len(a) && ini < len(b) && a[ini] == b[ini] {
		pares = append(pares, [2]int{ini, ini})
		ini++
	}
	fimA, fimB := len(a), len(b)
	var sufixo [][2]int
	for fimA > ini && fimB > ini && a[fimA-1] == b[fimB-1] {
		fimA--
		fimB--
		sufixo = append(sufixo, [2]int{fimA, fimB})
	}

	n, m := fimA-ini, fimB-ini
	if (n+1)*(m+1) > maxCelulas {
		return nil, false
	}

	// tab[i][j] é o tamanho da maior subsequência comum de a[ini+i:fimA] e
	// b[ini+j:fimB]. Montada do fim para o começo, para que a leitura de
	// frente para trás case as palavras mais cedo.
	larg := m + 1
	tab := make([]int32, (n+1)*larg)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[ini+i] == b[ini+j] {
				tab[i*larg+j] = tab[(i+1)*larg+j+1] + 1
			} else if tab[(i+1)*larg+j] >= tab[i*larg+j+1] {
				tab[i*larg+j] = tab[(i+1)*larg+j]
			} else {
				tab[i*larg+j] = tab[i*larg+j+1]
			}
		}
	}
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[ini+i] == b[ini+j]:
			pares = append(pares, [2]int{ini + i, ini + j})
			i++
			j++
		case tab[(i+1)*larg+j] >= tab[i*larg+j+1]:
			i++
		default:
			j++
		}
	}

	for k := len(sufixo) - 1; k >= 0; k-- {
		pares = append(pares, sufixo[k])
	}
	return pares, true
}

// trecho é um pedaço que mudou: a[a1:a2] virou b[b1:b2]. Um dos lados vazio
// é inserção ou remoção pura.
type trecho struct{ a1, a2, b1, b2 int }

func (t trecho) troca() bool { return t.a2 > t.a1 && t.b2 > t.b1 }

// trechos devolve o que fica entre os pares de pareamento, em ordem.
func trechos(pares [][2]int, na, nb int) []trecho {
	var out []trecho
	ia, ib := 0, 0
	for _, p := range pares {
		if p[0] > ia || p[1] > ib {
			out = append(out, trecho{ia, p[0], ib, p[1]})
		}
		ia, ib = p[0]+1, p[1]+1
	}
	if na > ia || nb > ib {
		out = append(out, trecho{ia, na, ib, nb})
	}
	return out
}

// origem diz de que parte do texto bruto veio uma palavra do texto final.
type origem struct {
	// r1 e r2 delimitam as palavras do bruto de onde ela veio: [r1, r2).
	r1, r2 int
	// casou é verdadeiro quando a palavra tem a mesma chave no bruto, isto
	// é, ninguém a trocou. Falso quando o sistema (ou uma edição anterior)
	// escreveu outra coisa no lugar: todas as palavras dessa lacuna dividem
	// o mesmo [r1, r2).
	casou bool
}

// origens converte o pareamento (bruto, final) em uma origem por palavra do
// final.
func origens(pares [][2]int, nBruto, nFinal int) []origem {
	out := make([]origem, nFinal)
	ir, ifin := 0, 0
	lacuna := func(r2, f2 int) {
		for k := ifin; k < f2; k++ {
			out[k] = origem{ir, r2, false}
		}
	}
	for _, p := range pares {
		lacuna(p[0], p[1])
		out[p[1]] = origem{p[0], p[0] + 1, true}
		ir, ifin = p[0]+1, p[1]+1
	}
	lacuna(nBruto, nFinal)
	return out
}
