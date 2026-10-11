package dictionary

import "strings"

// PhoneticEncoder reduz um termo a uma chave curta que agrupa palavras com
// som parecido em PT-BR, para permitir que Search sugira o termo do
// dicionário mesmo quando o ASR transcreveu uma grafia diferente da
// esperada. A spec pede explicitamente que essa peça seja plugável
// (docs/ai-spec/13-implementation-plan.md, TASK-008: "phonetic key
// pluggable") — outra implementação (ex.: baseada em fonemas de um
// dicionário de pronúncia) pode substituir PTBRSoundex sem tocar em
// Search.
type PhoneticEncoder interface {
	// Encode devolve a chave fonética de s. Implementações devem ser
	// determinísticas e não precisam receber s já normalizado — mas
	// devolvem chave estável para qualquer variação de maiúsculas/acento
	// que Normalize também colapsaria.
	Encode(s string) string
}

// ptbrSoundexGroup mapeia consoantes foneticamente aparentadas em PT-BR ao
// mesmo dígito, no espírito do Soundex clássico (Odell–Russell) adaptado:
// letras fora do mapa (vogais, h, w, y) não geram código, só servem para
// decidir se dois códigos iguais adjacentes devem ser fundidos.
//
//  1. b, f, p, v — oclusiva/fricativa labial
//  2. c, g, j, k, q, s, x, z — sibilante/gutural (agrupa "c" duro e "s"
//     surdo; não distingue "c" de "ç"/"s" antes de e/i, que soam iguais em
//     PT-BR, nem "c" duro de "k"/"q")
//  3. d, t — oclusiva alveolar
//  4. l — lateral
//  5. m, n — nasal
//  6. r — vibrante
var ptbrSoundexGroup = map[rune]byte{
	'b': '1', 'f': '1', 'p': '1', 'v': '1',
	'c': '2', 'g': '2', 'j': '2', 'k': '2', 'q': '2', 's': '2', 'x': '2', 'z': '2',
	'd': '3', 't': '3',
	'l': '4',
	'm': '5', 'n': '5',
	'r': '6',
}

// PTBRSoundex é a implementação default de PhoneticEncoder: um Soundex
// adaptado a PT-BR (ver ptbrSoundexGroup) — primeira letra preservada,
// consoantes seguintes reduzidas a um dígito de grupo, vogais/h/w/y
// ignoradas, e o resultado limitado a "letra + 3 dígitos".
//
// # Limites conhecidos (documentados de propósito, não escondidos)
//
//   - Não distingue nasalização (ã/ão) nem ditongos — "não" e "nao" caem
//     no mesmo código, o que é desejável, mas por acidente de perder a
//     acentuação em Normalize, não por tratamento fonético real.
//   - Não modela dígrafos (ch, lh, nh, rr, ss) como um único som — "carro"
//     e "caro" tendem a colidir (aceitável: são quase-homófonos reais).
//   - Agrupa "s" e "c/ç" (ambos código 2) mesmo quando "c" antecede a/o/u
//     e soaria como /k/, não /s/ — ex.: "casa" e "caça" colidem. Isso é
//     uma decisão consciente de recall alto sobre precisão; Search nunca
//     usa esse código sozinho para decidir substituição (ver
//     phoneticSimilarityThreshold), exatamente para conter esse ruído.
//   - Só cobre o alfabeto latino; dígitos são ignorados (== vogal), então
//     "n8n" e "nn" colidem. Termos técnicos com número costumam ser
//     resolvidos por casamento exato/alias, não fonético, então isso raramente
//     importa na prática.
//   - Não colide palavras de comprimento/estrutura muito diferentes por
//     construção (o código já satura em 4 caracteres), mas quando o texto é
//     curto (1-2 letras úteis) duas palavras distintas podem coincidir —
//     de novo, contido pelo filtro de similaridade de Search.
type PTBRSoundex struct{}

var _ PhoneticEncoder = PTBRSoundex{}

func (PTBRSoundex) Encode(s string) string {
	normalized := Normalize(s)
	letters := make([]rune, 0, len(normalized))
	for _, r := range normalized {
		if r == ' ' {
			continue
		}
		letters = append(letters, r)
	}
	if len(letters) == 0 {
		return ""
	}

	var code strings.Builder
	code.WriteRune(letters[0])

	lastGroup := ptbrSoundexGroup[letters[0]] // 0 se a primeira letra não tem grupo (é vogal).
	for _, r := range letters[1:] {
		group, ok := ptbrSoundexGroup[r]
		if !ok {
			// Vogal (ou h/w/y): não gera dígito, mas quebra a fusão de
			// dígitos repetidos adjacentes (regra clássica do Soundex:
			// só funde quando a repetição é adjacente na letra, sem
			// vogal entre elas).
			lastGroup = 0
			continue
		}
		if group != lastGroup {
			code.WriteByte(group)
		}
		lastGroup = group
		if code.Len() >= 4 {
			break
		}
	}

	for code.Len() < 4 {
		code.WriteByte('0')
	}
	return code.String()[:4]
}

// levenshtein devolve a distância de edição (inserção/remoção/substituição,
// custo 1 cada) entre a e b. Implementação O(len(a)*len(b)) em memória
// O(min(len(a),len(b))), suficiente para os termos curtos de dicionário —
// não há necessidade de dependência externa para isto.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) > len(rb) {
		ra, rb = rb, ra
	}

	prev := make([]int, len(ra)+1)
	for i := range prev {
		prev[i] = i
	}
	curr := make([]int, len(ra)+1)

	for i := 1; i <= len(rb); i++ {
		curr[0] = i
		for j := 1; j <= len(ra); j++ {
			cost := 1
			if rb[i-1] == ra[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			min := del
			if ins < min {
				min = ins
			}
			if sub < min {
				min = sub
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}
	return prev[len(ra)]
}

// textSimilarity devolve um score em [0,1]: 1 quando a == b, 0 quando não
// sobra nada em comum. É a base do filtro de contenção fonética em
// Search — ver phoneticSimilarityThreshold.
// Similaridade é textSimilarity exportado: 1 menos a distância de
// Levenshtein normalizada pelo comprimento da maior das duas strings.
//
// Exportado para que internal/learning use A MESMA medida de "estas duas
// palavras são confundíveis" que o Search usa para propor candidato
// fonético. Se as duas partes do produto discordassem sobre isso, o
// dicionário aprenderia pares que o resolver nunca aplicaria — ou pior,
// o contrário.
func Similaridade(a, b string) float64 { return textSimilarity(a, b) }

func textSimilarity(a, b string) float64 {
	if a == "" && b == "" {
		return 1
	}
	maxLen := len([]rune(a))
	if bl := len([]rune(b)); bl > maxLen {
		maxLen = bl
	}
	if maxLen == 0 {
		return 1
	}
	dist := levenshtein(a, b)
	return 1 - float64(dist)/float64(maxLen)
}
