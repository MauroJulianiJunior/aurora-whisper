package cloudstore

import "strings"

// A mescla da lista de marcar (ADR-022, "Sync"; N7). O caso típico é o
// mercado: a pessoa marca itens no celular enquanto o PC mexe na mesma nota.
// Sem esta mescla, os dois lados mudaram o conteúdo e o do PC iria para o
// selo; com ela, se os dois só marcaram e desmarcaram itens, o servidor junta
// as duas listas, sem selo.
//
// # O que é a lista
//
// O corpo da nota é texto, uma linha por bloco (ADR-022, "A nota"). A linha de
// tarefa é "- [ ] texto" ou "- [x] texto" ("[X]" também é marcada; "- [ ]"
// sozinho é a tarefa vazia), como o painel a lê (web/src/notas/corpo.ts). Toda
// outra linha (parágrafo, título, marcador, numerada, vazia) é "fixa". O fim de
// linha pode ser \n, \r\n ou \r, e as linhas vazias do fim não contam (o painel
// as tira ao gravar): a comparação ignora as duas coisas.
//
// # Quando mescla
//
// Os três corpos (o da base, o da nuvem e o do PC) têm de ter:
//
//   - as mesmas linhas fixas, na mesma ordem, com o mesmo texto; e
//   - os mesmos itens de tarefa, pelo texto, contados como um multiconjunto do
//     corpo inteiro (o mesmo texto duas vezes são dois itens).
//
// A posição dos itens não conta. É de propósito: marcar um item no painel o
// leva para o fim do corpo inteiro (normalizarMarcados, que junta os marcados
// depois de todas as linhas, inclusive de um parágrafo que estava abaixo), e
// desmarcar o devolve para o fim dos abertos. Comparar a posição faria toda
// marca parecer uma edição. A consequência: um item arrastado de lugar num lado
// perde para a ordem da nuvem (nenhum texto se perde; só a ordem).
//
// Qualquer diferença de texto numa linha é um conflito normal (o selo), e não
// mescla: um item editado, um item acrescentado ou tirado, um parágrafo mudado,
// uma linha fixa acrescentada, tirada ou movida. O título, quem compara é quem
// chama (DecideNote: só tenta a mescla com o mesmo título dos dois lados).
//
// # Como mescla
//
// Item a item, pela regra de três vias de um booleano: se a nuvem não mudou o
// estado desde a base, vale o do PC; senão, vale o da nuvem (o PC não mudou, ou
// mudou para o mesmo). "O mesmo item marcado de um jeito num lado e de outro no
// outro" não existe nessa regra: o estado tem dois valores, então, se os dois
// lados o mudaram desde a base, os dois o levaram ao mesmo valor (o contrário
// do da base). Por isso a mescla de um item nunca falha.
//
// Com o texto repetido (dois "leite"), a regra vale para o número de marcados
// daquele texto: se só um lado mudou o número, vale o dele; se os dois o
// mudaram para o mesmo, vale esse; se os dois o mudaram para números
// diferentes, é conflito normal (não dá para saber qual "leite" cada um quis).
//
// # O resultado
//
// É o corpo da nuvem, com o estado mesclado de cada item. Quando o número de
// marcados de um texto muda, mudam as primeiras ocorrências dele na ordem da
// nuvem (as abertas, para marcar; as marcadas, para desmarcar). Cada sequência
// contínua de tarefas em que algum item mudou é reescrita como o painel a
// deixaria: os abertos primeiro, na ordem da nuvem, e depois os marcados, na
// ordem da nuvem. As sequências em que nada mudou ficam como estão, e, se nada
// mudou, o corpo volta igual ao da nuvem, byte a byte. As linhas reescritas
// saem com "\n" e na forma "- [ ] texto" / "- [x] texto".

// linhaDoCorpo é uma linha do corpo: o texto cru e, na tarefa, o estado e o
// texto do item.
type linhaDoCorpo struct {
	raw    string
	tarefa bool
	feito  bool
	texto  string
}

// lerTarefa lê uma linha de tarefa ("- [ ] texto", "- [x] texto", "- [X]
// texto", ou o colchete sozinho, sem texto).
func lerTarefa(l string) (feito bool, texto string, ok bool) {
	if len(l) < 5 || !strings.HasPrefix(l, "- [") || l[4] != ']' {
		return false, "", false
	}
	switch l[3] {
	case ' ':
	case 'x', 'X':
		feito = true
	default:
		return false, "", false
	}
	if len(l) == 5 {
		return feito, "", true
	}
	if l[5] != ' ' {
		return false, "", false
	}
	return feito, l[6:], true
}

// lerCorpo separa o corpo em linhas (\n, \r\n ou \r) e devolve, à parte,
// quantas linhas vazias havia no fim (a comparação as ignora; o resultado
// devolve as da nuvem).
func lerCorpo(corpo string) (linhas []linhaDoCorpo, vaziasNoFim int) {
	if corpo == "" {
		return nil, 0
	}
	corpo = strings.ReplaceAll(corpo, "\r\n", "\n")
	corpo = strings.ReplaceAll(corpo, "\r", "\n")
	partes := strings.Split(corpo, "\n")
	for len(partes) > 0 && partes[len(partes)-1] == "" {
		partes = partes[:len(partes)-1]
		vaziasNoFim++
	}
	linhas = make([]linhaDoCorpo, len(partes))
	for i, p := range partes {
		linhas[i].raw = p
		linhas[i].feito, linhas[i].texto, linhas[i].tarefa = lerTarefa(p)
	}
	return linhas, vaziasNoFim
}

// esqueleto são as linhas fixas, em ordem; itens, o multiconjunto dos textos
// de tarefa; marcados, quantos de cada texto estão marcados.
func esqueleto(linhas []linhaDoCorpo) (fixas []string, itens, marcados map[string]int) {
	itens, marcados = map[string]int{}, map[string]int{}
	for _, l := range linhas {
		if !l.tarefa {
			fixas = append(fixas, l.raw)
			continue
		}
		itens[l.texto]++
		if l.feito {
			marcados[l.texto]++
		}
	}
	return fixas, itens, marcados
}

func mesmasFixas(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mesmosItens(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, n := range a {
		if b[k] != n {
			return false
		}
	}
	return true
}

// MesclarListaDeMarcar junta as marcas de dois lados que só marcaram e
// desmarcaram itens desde base: nuvem é o corpo que a nuvem tem, e pc, o que o
// PC mandou. ok=false: não é uma mescla de lista de marcar (alguma linha
// mudou de texto, ou o mesmo texto repetido foi marcado de jeitos diferentes),
// e quem chama segue para o conflito normal. Pura.
func MesclarListaDeMarcar(base, nuvem, pc string) (string, bool) {
	lb, _ := lerCorpo(base)
	ln, vaziasDaNuvem := lerCorpo(nuvem)
	lp, _ := lerCorpo(pc)
	fb, ib, mb := esqueleto(lb)
	fn, in, mn := esqueleto(ln)
	fp, ip, mp := esqueleto(lp)
	if !mesmasFixas(fb, fn) || !mesmasFixas(fb, fp) || !mesmosItens(ib, in) || !mesmosItens(ib, ip) {
		return "", false
	}

	// Quantos marcados de cada texto ficam: a regra de três vias, sobre o
	// número (que, sem repetição, é o próprio estado: 0 ou 1).
	quer := make(map[string]int, len(in))
	for texto := range in {
		kb, kn, kp := mb[texto], mn[texto], mp[texto]
		switch {
		case kn == kb:
			quer[texto] = kp
		case kp == kb || kp == kn:
			quer[texto] = kn
		default:
			// Só com o texto repetido: os dois lados mudaram o número de
			// marcados, para números diferentes.
			return "", false
		}
	}

	// Muda as primeiras ocorrências de cada texto, na ordem da nuvem.
	falta := make(map[string]int, len(quer))
	for texto, r := range quer {
		falta[texto] = r - mn[texto]
	}
	mudou := make([]bool, len(ln))
	algum := false
	for i, l := range ln {
		if !l.tarefa {
			continue
		}
		switch d := falta[l.texto]; {
		case d > 0 && !l.feito:
			ln[i].feito, mudou[i], algum = true, true, true
			falta[l.texto]--
		case d < 0 && l.feito:
			ln[i].feito, mudou[i], algum = false, true, true
			falta[l.texto]++
		}
	}
	if !algum {
		return nuvem, true
	}

	// Reescreve as sequências de tarefas em que algum item mudou: os abertos
	// primeiro, depois os marcados, cada grupo na ordem da nuvem.
	out := make([]string, 0, len(ln)+vaziasDaNuvem)
	for i := 0; i < len(ln); {
		if !ln[i].tarefa {
			out = append(out, ln[i].raw)
			i++
			continue
		}
		j := i
		mexida := false
		for j < len(ln) && ln[j].tarefa {
			mexida = mexida || mudou[j]
			j++
		}
		if !mexida {
			for k := i; k < j; k++ {
				out = append(out, ln[k].raw)
			}
			i = j
			continue
		}
		for _, feito := range []bool{false, true} {
			for k := i; k < j; k++ {
				if ln[k].feito != feito {
					continue
				}
				if !mudou[k] {
					out = append(out, ln[k].raw)
					continue
				}
				marca := " "
				if feito {
					marca = "x"
				}
				out = append(out, "- ["+marca+"] "+ln[k].texto)
			}
		}
		i = j
	}
	for range vaziasDaNuvem {
		out = append(out, "")
	}
	return strings.Join(out, "\n"), true
}
