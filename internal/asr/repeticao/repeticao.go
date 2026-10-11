// Package repeticao acha e desfaz o laço de repetição de um ASR: a mesma
// sequência de palavras repetida em seguida muitas vezes, no lugar da fala
// que deveria vir.
//
// É o defeito conhecido do Whisper (decodificador autorregressivo que entra
// num ciclo), e foi o do incidente de 01-out-2026
// (docs/defeito-ditado-longo.md): um ditado de 59 s saiu com o começo certo e
// depois "e como cada um deles vai ser utilizado na hora de récu," doze
// vezes, e o resto da fala sumiu. O whisper.cpp tem um guarda (entropia dos
// últimos 32 tokens), mas o limiar padrão só pega laços de até 11 tokens, e
// aquele tinha uns 15 a 20.
//
// Só olha o texto, sem saber de modelo nenhum: serve a qualquer provider, e
// fica no domínio sem acoplá-lo ao Whisper (CLAUDE.md). Puro, sem I/O.
//
// # O que conta como laço
//
// Uma unidade de N palavras seguida de cópias idênticas dela (comparando sem
// maiúsculas e sem pontuação), com:
//
//   - N ≥ 10 palavras: pelo menos 4 ocorrências seguidas. Uma frase de dez
//     palavras dita quatro vezes seguidas, sem nada entre elas, é rara em
//     fala; é o falso positivo que a regra aceita, e o texto original fica
//     no raw_text do histórico (winruntime).
//   - N de 3 a 9 palavras: pelo menos 6 ocorrências seguidas, somando pelo
//     menos 60 caracteres. Frase curta repetida quatro ou cinco vezes ainda
//     é fala: ênfase ("vai dar certo", três vezes), uma aula ("Repitam
//     comigo: o RAG busca antes de gerar.", quatro vezes), itens de lista
//     iguais ditados em sequência ("Item pendente revisar contrato.",
//     quatro vezes). Laço de verdade costuma ter muitas cópias (doze no
//     incidente), e só junta poucas quando a unidade é longa. O mínimo de
//     caracteres deixa de fora contagem curta ("um dois três" ×6 já passa;
//     "é o tal" ×6 não).
//   - N de 1 a 2: pelo menos 8 ocorrências. "não, não, não", "tá, tá",
//     "muito muito", "sim sim sim sim" e "é isso, é isso, é isso" são fala;
//     a mesma palavra oito vezes seguidas é o decodificador preso.
//   - a unidade tem de ter pelo menos uma letra: números e pontuação sozinhos
//     nunca contam (valores, CPF, uma lista de números repetidos).
//
// O laço vira UMA ocorrência. Se o texto termina numa cópia pela metade da
// unidade (o decodificador cortado no meio do ciclo), ela sai também. Sem
// laço, o texto volta byte a byte igual.
package repeticao

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Os limiares do que conta como laço; ver o comentário do pacote.
const (
	// MinRepeticoesFrase é o mínimo de ocorrências seguidas de uma unidade
	// de MinPalavrasFrase a MinPalavrasFraseLonga-1 palavras.
	MinRepeticoesFrase = 6
	// MinRepeticoesFraseLonga é o mínimo de ocorrências seguidas de uma
	// unidade de MinPalavrasFraseLonga palavras ou mais.
	MinRepeticoesFraseLonga = 4
	// MinPalavrasFrase separa a frase (3 palavras ou mais) da palavra solta.
	MinPalavrasFrase = 3
	// MinPalavrasFraseLonga separa a frase longa (10 palavras ou mais).
	MinPalavrasFraseLonga = 10
	// MinCaracteresFrase é o mínimo de caracteres que as ocorrências somam.
	MinCaracteresFrase = 60
	// MinRepeticoesPalavra é o mínimo de ocorrências seguidas de uma unidade
	// de 1 ou 2 palavras.
	MinRepeticoesPalavra = 8
	// MaxPalavrasUnidade é o maior laço procurado. O do incidente tinha 12
	// palavras; o Whisper às vezes repete uma frase inteira de 20 a 30.
	MaxPalavrasUnidade = 50

	// maxCaudaGirada é o maior pedaço da unidade que a cópia pela metade no
	// fim pode deixar de fora e ainda ficar no lugar da ocorrência inteira:
	// o conector do laço achado girado ("e", "e então"). Ver Colapsar.
	maxCaudaGirada = 2
)

// Colapso é um laço desfeito: o tamanho da unidade e quantas vezes ela
// aparecia em seguida (contando a que ficou).
type Colapso struct {
	Palavras   int
	Repeticoes int
}

// Resultado é o texto sem os laços e o que foi tirado.
type Resultado struct {
	Texto    string
	Colapsos []Colapso
	// PalavrasRemovidas conta tudo o que saiu, inclusive a cópia pela metade
	// no fim.
	PalavrasRemovidas int
}

// Colapsou diz se algum laço foi desfeito.
func (r Resultado) Colapsou() bool { return len(r.Colapsos) > 0 }

// palavra é um pedaço do texto entre espaços: onde começa e termina (em
// bytes) e a forma comparável (minúsculas, só letras e dígitos).
type palavra struct {
	ini, fim int
	norm     string
	letra    bool
}

// Colapsar devolve texto com cada laço de repetição reduzido a uma
// ocorrência. O resto do texto — espaços, pontuação, maiúsculas — fica como
// estava.
func Colapsar(texto string) Resultado {
	ps := palavras(texto)
	res := Resultado{Texto: texto}
	var b strings.Builder
	copiado := 0 // até onde texto já foi para b
	for i := 0; i < len(ps); {
		n, reps := lacoEm(ps, i)
		if reps == 0 {
			i++
			continue
		}
		fim := i + n*reps // primeira palavra depois do laço
		fica := n         // palavras do laço que ficam: uma ocorrência
		// Uma cópia pela metade que termina o texto é o mesmo laço cortado.
		k := 0
		for fim+k < len(ps) && k < n && ps[fim+k].norm == ps[i+k].norm {
			k++
		}
		if k > 0 && fim+k == len(ps) {
			fim = len(ps)
			// O laço pode ter sido achado "girado": a fala antes dele não
			// tinha a primeira palavra do ciclo ("...explique como cada um
			// deles... récu," e depois "e como cada um deles... récu," muitas
			// vezes), e a unidade achada começa no meio da frase e termina
			// num "e" solto. Aí quem termina a frase é a cópia pela metade:
			// fica ela, se ela acaba em pontuação e a ocorrência inteira não,
			// e se o que ela deixa de fora é só esse conector (até
			// maxCaudaGirada palavras). Uma cauda maior é fala: numa unidade
			// sem pontuação no fim, o corte pode cair logo depois de uma
			// vírgula interna ("Quero saber como cada um deles, vai ser usado
			// na hora certa" e a cópia "Quero saber como cada um deles,"), e
			// ficar com a cópia apagaria "vai ser usado na hora certa", dito
			// uma vez de verdade.
			if !pontuada(texto, ps[i+n-1]) && pontuada(texto, ps[i+k-1]) && n-k <= maxCaudaGirada {
				fica = k
			}
		}
		res.Colapsos = append(res.Colapsos, Colapso{Palavras: n, Repeticoes: reps})
		res.PalavrasRemovidas += fim - (i + fica)

		// Fica o começo do laço; sai do resto dele até o começo do que vem
		// depois (ou até o fim do texto).
		corteIni, corteFim := ps[i+fica].ini, len(texto)
		if fim < len(ps) {
			corteFim = ps[fim].ini
		} else {
			corteIni = ps[i+fica-1].fim
		}
		b.WriteString(texto[copiado:corteIni])
		copiado = corteFim
		i = fim
	}
	if res.Colapsou() {
		b.WriteString(texto[copiado:])
		res.Texto = b.String()
	}
	return res
}

// lacoEm procura, a partir da palavra i, a menor unidade que se repete em
// seguida o bastante para ser laço. Devolve o tamanho dela e as ocorrências,
// ou reps 0.
func lacoEm(ps []palavra, i int) (n, reps int) {
	for n = 1; n <= MaxPalavrasUnidade && i+2*n <= len(ps); n++ {
		r := 1
		for i+(r+1)*n <= len(ps) && iguais(ps[i:i+n], ps[i+r*n:i+(r+1)*n]) {
			r++
		}
		if ehLaco(ps[i:i+n], r) {
			return n, r
		}
	}
	return 0, 0
}

// ehLaco aplica os limiares do pacote a uma unidade que aparece reps vezes
// seguidas.
func ehLaco(unidade []palavra, reps int) bool {
	temLetra, caracteres := false, 0
	for j, p := range unidade {
		temLetra = temLetra || p.letra
		caracteres += utf8.RuneCountInString(p.norm)
		if j > 0 {
			caracteres++ // o espaço entre as palavras
		}
	}
	if !temLetra {
		return false
	}
	if len(unidade) >= MinPalavrasFrase {
		minimo := MinRepeticoesFrase
		if len(unidade) >= MinPalavrasFraseLonga {
			minimo = MinRepeticoesFraseLonga
		}
		return reps >= minimo && reps*caracteres >= MinCaracteresFrase
	}
	return reps >= MinRepeticoesPalavra
}

// pontuada diz se a palavra termina em pontuação de fim de frase ou de
// oração.
func pontuada(texto string, p palavra) bool {
	r, _ := utf8.DecodeLastRuneInString(texto[p.ini:p.fim])
	return strings.ContainsRune(",.;:!?…", r)
}

func iguais(a, b []palavra) bool {
	for j := range a {
		if a[j].norm != b[j].norm {
			return false
		}
	}
	return true
}

// palavras quebra o texto nos espaços, guardando onde cada pedaço está.
func palavras(texto string) []palavra {
	var ps []palavra
	ini := -1
	for j, r := range texto {
		if unicode.IsSpace(r) {
			if ini >= 0 {
				ps = append(ps, nova(texto, ini, j))
				ini = -1
			}
			continue
		}
		if ini < 0 {
			ini = j
		}
	}
	if ini >= 0 {
		ps = append(ps, nova(texto, ini, len(texto)))
	}
	return ps
}

// nova monta a palavra de texto[ini:fim] com a forma comparável: minúsculas,
// só letras e dígitos ("récu," e "Récu" são a mesma).
func nova(texto string, ini, fim int) palavra {
	var b strings.Builder
	letra := false
	for _, r := range texto[ini:fim] {
		switch {
		case unicode.IsLetter(r):
			letra = true
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return palavra{ini: ini, fim: fim, norm: b.String(), letra: letra}
}
