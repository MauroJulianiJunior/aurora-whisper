package notas

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// A regra do "ditar para nota" (ADR-022, "Acrescentar por voz sem abrir a
// nota"; as decisões da N9): a qual nota o ditado vai, como o texto entra nela
// e o que o aviso diz. O PC a usa no gatilho Ctrl+Win+Alt
// (internal/agent/paranota) e a nuvem no botão "Ditar para nota" do celular
// (internal/server/cloudstore, N8): a mesma regra nos dois lados.

// ---------------------------------------------------------------------
// O destino: o título no começo do ditado
// ---------------------------------------------------------------------

// separadores são o que pode vir entre o título e o resto do ditado, além do
// espaço: "lista de compras: leite", "lista de compras, leite", e o ponto que o
// ASR às vezes põe ("Lista de compras. Leite"). O hífen fica de fora: o Polish
// escreve a lista em linhas "- ", e o hífen do primeiro item não é separador.
const separadores = ":,;.!?–—"

// Candidata é o que a regra precisa saber de uma nota que pode receber o
// ditado: o id, o título e as datas do desempate.
type Candidata struct {
	ID        string
	Titulo    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// palavrasDoTitulo é o título na forma em que se compara: sem caixa, sem
// acento, em palavras separadas por espaço, sem a pontuação do fim ("Compras:"
// casa com "compras"). Vazio: a nota não tem título e não é destino.
func palavrasDoTitulo(titulo string) []string {
	t := strings.TrimRight(strings.TrimSpace(Dobrar(titulo)), separadores+" ")
	return strings.Fields(t)
}

// casar diz se o ditado começa pelo título (em palavras, já dobrado) e devolve
// o resto, do jeito que foi ditado. A comparação é exata palavra a palavra, sem
// caixa e sem acento; a última palavra do título pode vir colada a um separador
// ("compras:"), e depois dela tem de haver fim de palavra ("Lista" não casa com
// "Listar"). O resto sai sem os espaços e os separadores da frente.
func casar(titulo []string, ditado string) (resto string, ok bool) {
	if len(titulo) == 0 {
		return "", false
	}
	pos := 0
	for i, palavra := range titulo {
		// Pula o espaço (inclusive a quebra de linha) até a próxima palavra.
		for pos < len(ditado) {
			r, n := utf8.DecodeRuneInString(ditado[pos:])
			if !unicode.IsSpace(r) {
				break
			}
			pos += n
		}
		ini := pos
		for pos < len(ditado) {
			r, n := utf8.DecodeRuneInString(ditado[pos:])
			if unicode.IsSpace(r) {
				break
			}
			pos += n
		}
		tok := ditado[ini:pos]
		if tok == "" {
			return "", false
		}
		if i == len(titulo)-1 {
			// A última palavra pode trazer o separador colado.
			tok = strings.TrimRight(tok, separadores)
		}
		if Dobrar(tok) != palavra {
			return "", false
		}
	}
	resto = strings.TrimLeftFunc(ditado[pos:], func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(separadores, r)
	})
	return strings.TrimRightFunc(resto, unicode.IsSpace), true
}

// Escolher acha a nota de destino do ditado entre as candidatas e devolve o
// índice dela (-1: nenhuma; o ditado vira uma nota nova) e o resto do ditado
// depois do título.
//
// As regras (ADR-022; as decisões da N9 em "Acrescentar por voz"):
//   - só nota com título é destino; nota sem título nunca casa;
//   - o título mais longo vence ("Lista de compras" ganha de "Lista" para
//     "lista de compras: leite"): é o nome que a pessoa disse inteiro;
//   - com o mesmo título, vence a nota editada por último (UpdatedAt; a criada
//     por último desempata);
//   - o ditado que é só o título, sem nada depois, não casa com ela: não há o
//     que acrescentar, e ele vira nota nova (nada se perde).
//
// Quais notas são candidatas é de quem chama: as ativas (nem arquivadas nem na
// lixeira), no PC e na nuvem.
func Escolher(candidatas []Candidata, ditado string) (indice int, resto string) {
	indice = -1
	maior := 0
	for i, n := range candidatas {
		titulo := palavrasDoTitulo(n.Titulo)
		r, ok := casar(titulo, ditado)
		if !ok {
			continue
		}
		if indice >= 0 {
			if len(titulo) < maior {
				continue
			}
			if len(titulo) == maior && !maisRecente(n, candidatas[indice]) {
				continue
			}
		}
		indice, resto, maior = i, r, len(titulo)
	}
	// O título mais longo que casou é o que a pessoa disse. Sem nada depois
	// dele, não há o que acrescentar — e um título mais curto ("Lista") não
	// pode ficar com "de compras" como se fosse o resto.
	if resto == "" {
		return -1, ""
	}
	return indice, resto
}

// maisRecente diz se a foi editada depois de b; na mesma edição, a criada
// depois; no mesmo instante, o id maior (o UUID v7 cresce com o tempo).
func maisRecente(a, b Candidata) bool {
	switch {
	case !a.UpdatedAt.Equal(b.UpdatedAt):
		return a.UpdatedAt.After(b.UpdatedAt)
	case !a.CreatedAt.Equal(b.CreatedAt):
		return a.CreatedAt.After(b.CreatedAt)
	default:
		return a.ID > b.ID
	}
}

// ---------------------------------------------------------------------
// Como o texto entra na nota
// ---------------------------------------------------------------------

// As linhas do corpo, como o painel as lê (web/src/notas/corpo.ts).
var (
	reTarefa        = regexp.MustCompile(`^- \[( |x|X)\](?: |$)`)
	reTarefaAberta  = regexp.MustCompile(`^- \[ \](?: |$)`)
	reMarcador      = regexp.MustCompile(`^- `)
	reNumerada      = regexp.MustCompile(`^\d+\. `)
	rePrefixoDeItem = regexp.MustCompile(`^(?:- \[(?: |x|X)\] ?|[-*•] |\d+[.)] )`)
)

// Forma é o jeito do corpo da nota de destino, que decide como o texto entra.
type Forma int

const (
	// FormaParagrafo: nenhuma lista. O resto entra no fim, como está.
	FormaParagrafo Forma = iota
	// FormaTarefas: há lista de marcar ("- [ ] " ou "- [x] "). Os itens entram
	// como "- [ ] ", antes dos marcados.
	FormaTarefas
	// FormaMarcadores: há lista com "- ". Os itens entram como "- ".
	FormaMarcadores
	// FormaNumerada: há lista numerada. Os itens continuam a numeração.
	FormaNumerada
)

// FormaDoCorpo diz a forma das linhas: a lista de marcar vence (uma linha
// dela basta), depois o marcador, depois a numerada.
func FormaDoCorpo(linhas []string) Forma {
	var marcador, numerada bool
	for _, l := range linhas {
		switch {
		case reTarefa.MatchString(l):
			return FormaTarefas
		case reMarcador.MatchString(l):
			marcador = true
		case reNumerada.MatchString(l):
			numerada = true
		}
	}
	switch {
	case marcador:
		return FormaMarcadores
	case numerada:
		return FormaNumerada
	default:
		return FormaParagrafo
	}
}

// linhasDe separa o texto em linhas (\n, \r\n ou \r). Texto vazio, nenhuma.
func linhasDe(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.Split(s, "\n")
}

// limparItem tira do item o prefixo de lista que o Polish ou o ASR já puseram,
// o separador das bordas e o ponto final, e põe a inicial maiúscula como a
// regra das listas.
func limparItem(s string) string {
	s = strings.TrimSpace(s)
	s = rePrefixoDeItem.ReplaceAllString(s, "")
	s = strings.TrimLeft(strings.TrimSpace(s), ",;")
	s = strings.TrimRight(s, " \t,;.!")
	return resolver.InicialDoItem(s)
}

// ItensDoResto separa o resto do ditado em itens de lista:
//   - em várias linhas (o Polish já organizou a lista), cada linha é um item;
//   - numa linha só, a regra das listas (resolver.ItensPorRegra: vírgula,
//     ponto e vírgula e o último " e ") quando ela vê uma enumeração;
//   - senão, a linha inteira é um item só.
func ItensDoResto(resto string) []string {
	var linhas []string
	for _, l := range linhasDe(resto) {
		if strings.TrimSpace(l) != "" {
			linhas = append(linhas, l)
		}
	}
	if len(linhas) == 1 {
		if itens := resolver.ItensPorRegra(strings.TrimSpace(linhas[0]), 2); itens != nil {
			linhas = itens
		}
	}
	var itens []string
	for _, l := range linhas {
		if it := limparItem(l); it != "" {
			itens = append(itens, it)
		}
	}
	return itens
}

// Acrescentar põe o resto do ditado no corpo e devolve o corpo novo e quantas
// linhas entraram (o "+N" do aviso). Nada que já estava no corpo muda de lugar
// nem de texto.
//
//   - Lista de marcar: os itens entram como "- [ ] " logo depois do último
//     item aberto (ou, se todos estão marcados, antes do primeiro marcado): os
//     marcados continuam no fim, como o painel os grava (N2).
//   - Lista com "- ": os itens entram como "- " depois do último marcador.
//   - Lista numerada: os itens continuam a numeração depois da última
//     numerada.
//   - Sem lista: o resto entra no fim, linha por linha, como foi ditado (a
//     primeira letra em maiúscula).
func Acrescentar(corpo, resto string) (novo string, n int) {
	linhas := linhasDe(corpo)
	var novas []string
	pos := len(linhas)

	switch FormaDoCorpo(linhas) {
	case FormaTarefas:
		for _, it := range ItensDoResto(resto) {
			novas = append(novas, "- [ ] "+it)
		}
		pos = depoisDaUltima(linhas, reTarefaAberta)
		if pos < 0 {
			pos = primeiraMarcada(linhas)
		}
	case FormaMarcadores:
		for _, it := range ItensDoResto(resto) {
			novas = append(novas, "- "+it)
		}
		// Sem lista de marcar (ela venceria a forma), todo "- " é marcador.
		pos = depoisDaUltima(linhas, reMarcador)
	case FormaNumerada:
		pos = depoisDaUltima(linhas, reNumerada)
		numero := 0
		for i := pos - 1; i >= 0 && reNumerada.MatchString(linhas[i]); i-- {
			numero++
		}
		for _, it := range ItensDoResto(resto) {
			numero++
			novas = append(novas, fmt.Sprintf("%d. %s", numero, it))
		}
	default:
		for _, l := range linhasDe(resto) {
			if l = strings.TrimSpace(l); l != "" {
				novas = append(novas, l)
			}
		}
		if len(novas) > 0 {
			novas[0] = resolver.InicialDoItem(novas[0])
		}
	}
	if len(novas) == 0 {
		return corpo, 0
	}
	if pos < 0 || pos > len(linhas) {
		pos = len(linhas)
	}
	saida := make([]string, 0, len(linhas)+len(novas))
	saida = append(saida, linhas[:pos]...)
	saida = append(saida, novas...)
	saida = append(saida, linhas[pos:]...)
	return strings.Join(saida, "\n"), len(novas)
}

// depoisDaUltima é o índice logo depois da última linha que casa com re, ou -1.
func depoisDaUltima(linhas []string, re *regexp.Regexp) int {
	for i := len(linhas) - 1; i >= 0; i-- {
		if re.MatchString(linhas[i]) {
			return i + 1
		}
	}
	return -1
}

// primeiraMarcada é o índice do primeiro item marcado, ou o fim.
func primeiraMarcada(linhas []string) int {
	for i, l := range linhas {
		if reTarefa.MatchString(l) && !reTarefaAberta.MatchString(l) {
			return i
		}
	}
	return len(linhas)
}

// ---------------------------------------------------------------------
// O aviso
// ---------------------------------------------------------------------

// TextoNotaNova: nenhum título casou, e nasceu uma nota com o ditado. É o que a
// pílula do PC diz (overlay.FaseNotaNova) e o aviso do celular.
const TextoNotaNova = "Nota nova"

// maxTituloNaPilula é quanto do título cabe na pílula, em letras. A pílula
// tem até duas linhas de ~30 letras (overlay.MensagemLarguraMax); 40 deixa o
// "+N" sempre à vista.
const maxTituloNaPilula = 40

// TextoDaPilula é o que a pílula mostra depois de acrescentar n linhas à nota
// de título titulo: "Lista de compras +3". O título longo é cortado com "…"
// para o número nunca sumir.
func TextoDaPilula(titulo string, n int) string {
	return fmt.Sprintf("%s +%d", TituloCurto(titulo), n)
}

// TituloCurto é o título como a pílula e o aviso o mostram: em uma linha, com
// os espaços juntos, cortado com "…" antes de 40 letras.
func TituloCurto(titulo string) string {
	t := strings.Join(strings.Fields(titulo), " ")
	if r := []rune(t); len(r) > maxTituloNaPilula {
		t = strings.TrimRight(string(r[:maxTituloNaPilula-1]), " ") + "…"
	}
	return t
}
