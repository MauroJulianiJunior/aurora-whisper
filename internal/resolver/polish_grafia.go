package resolver

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ortografia"
)

// A correção de grafia da guarda de palavras (ADR-016, emenda de
// 18-out-2026, decisão do Mauro com o diagnóstico do histórico na mão).
//
// O diagnóstico mostrou que, nos ditados longos, a maior parte das recusas
// não era o modelo estragando o ditado: era o modelo corrigindo o erro do
// Whisper ("vejante" -> "vigente", "sasonalidade" -> "sazonalidade",
// "relatorio" -> "relatório"). Sem orçamento para trocar palavra, a guarda
// jogava fora justamente a correção que a pessoa queria.
//
// A licença é estreita, e é o que separa o erro de transcrição da troca de
// sentido que a ADR-016 barra ("não pagou" -> "pagou", "pagou" -> "pegou"):
//
//   - a palavra da ENTRADA não existe em português (ortografia.Existe). A
//     que existe pode ser o que a pessoa disse, e trocá-la muda o sentido;
//     por isso "compõe" -> "compõem" e "jogar" -> "julgar" continuam
//     reprovados, mesmo quando o modelo acerta;
//   - a palavra da SAÍDA existe, como palavra comum: nem nome ("debora" ->
//     "Débora": o nome é da pessoa, com ou sem acento), nem outra palavra
//     que não existe. É o que barra a sigla ditada em minúscula virar outra
//     sigla ("irpf" -> "irpj"). O termo técnico que o VERO não tem
//     ("promptes" -> "prompts") fica para o dicionário do painel, que o
//     Smart aplica antes do Polish;
//   - uma palavra vira uma palavra de escrita parecida e com a mesma
//     primeira letra (grafiaParecida), ou duas viram uma com as mesmas
//     letras ("na quela" -> "naquela"), ou uma vira duas ("derepente" -> "de
//     repente");
//   - nunca na palavra que a entrada escreve com maiúscula fora do começo da
//     frase: é nome ou sigla ("eSocial" não vira "social");
//   - nunca em palavra protegida (negação, quantificação, numeral) nem em
//     número; e a saída não ganha uma palavra protegida que a entrada não
//     tinha, salvo o acento que faltava ("nao" -> "não");
//   - nunca na palavra estrutural (hesitação, pontuação falada, ordinal):
//     essas têm a licença própria.
//
// Apagar e acrescentar palavra continuam sem licença.

// corrigivelNaEntrada diz se a palavra da entrada pode ser corrigida: não é
// protegida nem número, só tem letras (e hífen), e não existe em português.
// A consulta ao dicionário vem por último: é a única que custa.
func corrigivelNaEntrada(w string) bool {
	if w == "" || ehProtegida(w) || !soLetras(w) {
		return false
	}
	return !ortografia.Existe(w)
}

// soLetras diz se a palavra só tem letras, com hífen no meio.
func soLetras(w string) bool {
	for _, r := range w {
		if !unicode.IsLetter(r) && r != '-' {
			return false
		}
	}
	return true
}

// saidaAceitavel diz se a palavra que entra no lugar da corrigida não traz
// sentido novo: não é número, e só é protegida quando a da entrada era a
// mesma palavra sem o acento ("nao" -> "não").
func saidaAceitavel(de, para string) bool {
	if para == "" || !soLetras(para) || ehNumeral(para) {
		return false
	}
	return !protegidas[para] || semAcento(de) == semAcento(para)
}

// comMaiusculaNoMeio marca as palavras escritas com maiúscula fora do
// começo da frase: são nome ou sigla, e nome não é grafia.
func comMaiusculaNoMeio(tokens []tokenTexto) []bool {
	nomes := make([]bool, len(tokens))
	for j, t := range tokens {
		r, _ := utf8.DecodeRuneInString(t.Bruta)
		abreFrase := j == 0 || t.Marcador || strings.ContainsAny(t.Antes, ".?!:\n")
		nomes[j] = unicode.IsUpper(r) && !abreFrase
	}
	return nomes
}

// grafiaParecida diz se "para" é a grafia corrigida de "de": medidas pela
// pronúncia (pelaPronuncia), a mesma primeira letra e a distância de edição
// no máximo 1 em palavra de 4 a 6 letras e 2 a partir de 7. Em palavra de
// até 3 letras só o acento pode mudar: "ate" -> "até" passa, "ate" -> "ato"
// não. O erro do ASR quase nunca está na primeira letra; a troca que mexe
// nela ("esocial" -> "social") é outra palavra.
func grafiaParecida(de, para string) bool {
	if de == para {
		return false
	}
	sa, sb := pelaPronuncia(de), pelaPronuncia(para)
	ra, _ := utf8.DecodeRuneInString(sa)
	rb, _ := utf8.DecodeRuneInString(sb)
	if ra != rb {
		return false
	}
	n := utf8.RuneCountInString(sa)
	limite := 0
	switch {
	case n >= 7:
		limite = 2
	case n >= 4:
		limite = 1
	}
	return distanciaDeEdicao(sa, sb) <= limite
}

// acentuadas leva cada letra acentuada do português à letra sem acento.
var acentuadas = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "ê", "e", "è", "e", "ë", "e",
	"í", "i", "î", "i", "ì", "i", "ï", "i",
	"ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
	"ú", "u", "û", "u", "ù", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// semAcento devolve a palavra minúscula sem acento nem cedilha.
func semAcento(w string) string {
	return acentuadas.Replace(strings.ToLower(w))
}

// mesmoSom troca as grafias que soam igual pela mesma letra: o Whisper erra
// pelo som, e "vejante" e "vigente" diferem em três letras, mas em duas
// pela pronúncia. Vem antes de tirar o acento, por causa do "ç".
var mesmoSom = strings.NewReplacer(
	"ch", "x", "ss", "s", "ç", "s", "z", "s",
	"ge", "je", "gi", "ji", "gé", "jé", "gê", "jê", "gí", "jí",
	"ce", "se", "ci", "si", "cé", "sé", "cê", "sê", "cí", "sí",
)

// pelaPronuncia é a palavra minúscula, com as grafias de mesmo som
// unificadas (mesmoSom) e sem acento: a forma em que grafiaParecida mede.
func pelaPronuncia(w string) string {
	return semAcento(mesmoSom.Replace(strings.ToLower(w)))
}

// semHifen tira os hífens: "e-mail" e "email" têm as mesmas letras.
func semHifen(w string) string {
	return strings.ReplaceAll(w, "-", "")
}

// distanciaDeEdicao é a distância de Levenshtein por runa.
func distanciaDeEdicao(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	ant := make([]int, len(rb)+1)
	atual := make([]int, len(rb)+1)
	for j := range ant {
		ant[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		atual[0] = i
		for j := 1; j <= len(rb); j++ {
			custo := 1
			if ra[i-1] == rb[j-1] {
				custo = 0
			}
			atual[j] = min(ant[j]+1, atual[j-1]+1, ant[j-1]+custo)
		}
		ant, atual = atual, ant
	}
	return ant[len(rb)]
}

// correcoes responde, para um par entrada/saída, que correções de grafia o
// alinhamento pode usar. O dicionário é consultado uma vez por palavra, e só
// para as que passaram pelas regras baratas.
type correcoes struct {
	a, b        []string
	marcas      []marcaEstrutural
	inexistente []bool // a[i] pode ser corrigida (ver corrigivelNaEntrada)
	nomeA       []bool // a[i] tem maiúscula no meio da frase
	nome        []bool // b[j] tem maiúscula no meio da frase
	comum       map[string]bool
}

func novasCorrecoes(a []string, ta []tokenTexto, marcas []marcaEstrutural, b []string, tb []tokenTexto) *correcoes {
	c := &correcoes{
		a: a, b: b, marcas: marcas,
		inexistente: make([]bool, len(a)),
		nomeA:       comMaiusculaNoMeio(ta),
		nome:        comMaiusculaNoMeio(tb),
		comum:       map[string]bool{},
	}
	vistas := map[string]bool{}
	for i, w := range a {
		if marcas[i].Classe != classeNenhuma || c.nomeA[i] {
			continue
		}
		pode, ok := vistas[w]
		if !ok {
			pode = corrigivelNaEntrada(w)
			vistas[w] = pode
		}
		c.inexistente[i] = pode
	}
	return c
}

// entra diz se b[j] pode tomar o lugar de "de": não traz sentido novo
// (saidaAceitavel), não está escrita como nome, e existe como palavra comum.
func (c *correcoes) entra(de string, j int) bool {
	if !saidaAceitavel(de, c.b[j]) || c.nome[j] {
		return false
	}
	w := c.b[j]
	comum, ok := c.comum[w]
	if !ok {
		comum = ortografia.Existe(w) && !ortografia.SoNomeProprio(w)
		c.comum[w] = comum
	}
	return comum
}

// livre diz se a[i] é palavra comum, não estrutural e não protegida: a que
// pode entrar numa junção. A que não existe já passou pelas mesmas regras
// (corrigivelNaEntrada); a que existe é conferida aqui.
func (c *correcoes) livre(i int) bool {
	return c.marcas[i].Classe == classeNenhuma && !c.nomeA[i] && !ehProtegida(c.a[i]) && soLetras(c.a[i])
}

// troca: a[i] vira b[j], uma palavra pela outra.
func (c *correcoes) troca(i, j int) bool {
	return c.inexistente[i] && grafiaParecida(c.a[i], c.b[j]) && c.entra(c.a[i], j)
}

// junta: a[i] e a[i+1] viram b[j], com as mesmas letras ("na quela" ->
// "naquela", "e mail" -> "e-mail"), e ao menos uma das duas não existe.
func (c *correcoes) junta(i, j int) bool {
	if i+1 >= len(c.a) || !c.livre(i) || !c.livre(i+1) {
		return false
	}
	if semHifen(c.b[j]) != c.a[i]+c.a[i+1] {
		return false
	}
	return (c.inexistente[i] || c.inexistente[i+1]) && c.entra(c.a[i]+c.a[i+1], j)
}

// separa: a[i] vira b[j] e b[j+1], com as mesmas letras ("derepente" -> "de
// repente"), e a[i] não existe.
func (c *correcoes) separa(i, j int) bool {
	if j+1 >= len(c.b) || !c.inexistente[i] {
		return false
	}
	if c.b[j]+c.b[j+1] != semHifen(c.a[i]) {
		return false
	}
	return c.entra(c.a[i], j) && c.entra(c.a[i], j+1)
}
