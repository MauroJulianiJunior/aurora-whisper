package dictation

import (
	"errors"
	"fmt"
)

// Este arquivo é o segundo gatilho, "ditar para nota" (ADR-022, "Acrescentar
// por voz sem abrir a nota"; emenda da ADR-013). Puro como o Decider: nenhuma
// chamada de sistema, seguro na thread do hook.
//
// # Como os dois gatilhos convivem
//
// O padrão do gatilho da nota é Ctrl+Win+Alt, um SUPERCONJUNTO do Ctrl+Win do
// ditado de sempre. O Decider do ditado já arma com Ctrl+Win+Alt (ele confere
// "tem o conjunto", e Ctrl+Win+Alt tem). Por isso a escolha do gatilho não
// muda nada na borda do hook: as decisões de suprimir e notificar continuam
// as do Decider, evento por evento, e a sessão é UMA só. O que muda é uma
// marca, ParaNota, que vai junto com o evento até o runtime.
//
// A marca é decidida pelo PICO da sessão: se em algum momento entre armar e
// soltar o conjunto da nota esteve inteiro (Ctrl, Win e Alt seguros ao mesmo
// tempo), a sessão é para nota. A soltura leva a marca final. Assim:
//
//   - a ordem de apertar não importa: Alt antes, no meio ou depois de Ctrl e
//     Win, contanto que os três estejam seguros juntos antes de soltar;
//   - a ordem de soltar também não importa: soltar o Alt primeiro não
//     "devolve" o ditado para o campo — a intenção já foi mostrada;
//   - o Ctrl+Win sem Alt nunca leva a marca, e segue exatamente como antes
//     (a mesma pílula, o mesmo bipe, o mesmo limiar).
//
// O Alt apertado DEPOIS de a gravação começar também vale (a pessoa lembrou
// no meio da fala). A alternativa, decidir só no começo, faria o Alt atrasado
// em 50 ms mandar a lista de compras para o campo em foco, sem aviso.

// ErrGatilhoDaNotaIgual: o gatilho da nota é o mesmo conjunto do ditado, ou
// está contido nele. Contido, toda vez que o ditado armasse o da nota também
// estaria inteiro, e o ditado de sempre deixaria de existir.
var ErrGatilhoDaNotaIgual = errors.New("dictation: o gatilho da nota não pode ser o do ditado nem estar contido nele")

// ValidarGatilhoDaNota confere o conjunto do gatilho da nota contra a
// configuração do ditado. Zero é "sem gatilho da nota" e sempre vale. O
// gatilho da nota é sempre uma combinação de modificadores, com a mesma regra
// do ditado: pelo menos dois (REQ-004, ADR-013).
func ValidarGatilhoDaNota(principal Config, nota Modifiers) error {
	if nota == 0 {
		return nil
	}
	if contarModificadores(nota) < 2 {
		return fmt.Errorf("%w: modifiers=%d", ErrModificadorSolto, nota)
	}
	if principal.Key == KeyUnknown && principal.Modifiers.Has(nota) {
		return fmt.Errorf("%w: principal=%d nota=%d", ErrGatilhoDaNotaIgual, principal.Modifiers, nota)
	}
	return nil
}

// sessaoDosGatilhos diz quem armou a sessão em curso.
type sessaoDosGatilhos int

const (
	semSessao sessaoDosGatilhos = iota
	// sessaoDoDitado: armada pelo Decider do ditado. É o caso de sempre, e o
	// único com o padrão (Ctrl+Win+Alt contém Ctrl+Win).
	sessaoDoDitado
	// sessaoDaNota: armada só pelo conjunto da nota, quando ele NÃO contém o
	// do ditado (outra configuração, ou o ditado por tecla de letra).
	sessaoDaNota
)

// Gatilhos é o decisor do hook com os dois gatilhos: o do ditado (o Decider de
// sempre, intocado) e o "ditar para nota" (um conjunto de modificadores).
//
// Mesmas regras do Decider: puro, síncrono, um consumidor só (a thread do
// hook), e NUNCA suprime um modificador — o gatilho da nota não acrescenta
// supressão nenhuma; quando suprime, é o Decider do ditado por tecla de letra,
// pelos motivos dele.
type Gatilhos struct {
	ditado *Decider
	nota   Modifiers

	sessao sessaoDosGatilhos
	// notaVista: o conjunto da nota esteve inteiro em algum momento da sessão
	// em curso (o pico).
	notaVista bool
}

// NovosGatilhos monta o decisor dos dois gatilhos. nota zero desliga o da nota:
// as decisões são exatamente as de ditado (TestGatilhos_SemNotaEhODecider).
// nota normalmente já passou por ValidarGatilhoDaNota.
func NovosGatilhos(ditado *Decider, nota Modifiers) *Gatilhos {
	return &Gatilhos{ditado: ditado, nota: nota}
}

// Armado reporta se há uma sessão em curso, de qualquer um dos gatilhos — é o
// que a pausa do hook respeita (ver Decider.Armado).
func (g *Gatilhos) Armado() bool {
	return g.sessao != semSessao || g.ditado.Armado()
}

// Decide decide o evento. Suppress é sempre o do Decider do ditado; Notify sai
// uma vez ao armar e uma vez ao soltar, de qualquer um dos gatilhos; ParaNota
// vai nas duas notificações: ao armar, se o conjunto da nota já está inteiro;
// ao soltar, se ele esteve inteiro em algum momento da sessão.
func (g *Gatilhos) Decide(ev RawKeyEvent) Decision {
	if ev.Injected && ev.FromThisProcess {
		return passThrough
	}
	d := g.ditado.Decide(ev)
	notaInteira := g.nota != 0 && ev.Modifiers.Has(g.nota)

	switch g.sessao {
	case sessaoDoDitado:
		if notaInteira {
			g.notaVista = true
		}
		// O fim da sessão é o desarme do Decider: com a soltura notificada,
		// ou com o desarme calado do modo de letra (keyup com modificador),
		// que o Decider já trata do jeito dele.
		if !g.ditado.Armado() {
			g.sessao = semSessao
			if d.Notify {
				d.ParaNota = g.notaVista
			}
		}
		return d

	case sessaoDaNota:
		// O Decider do ditado pode armar no meio (o conjunto do ditado ficou
		// inteiro também): a sessão continua uma só, e nada mais vai ao núcleo.
		if notaInteira {
			return Decision{Suppress: d.Suppress}
		}
		g.sessao = semSessao
		return Decision{Suppress: d.Suppress, Notify: true, ParaNota: true}
	}

	// Sem sessão.
	if d.Notify && ev.Down && g.ditado.Armado() {
		g.sessao, g.notaVista = sessaoDoDitado, notaInteira
		d.ParaNota = notaInteira
		return d
	}
	if notaInteira {
		g.sessao, g.notaVista = sessaoDaNota, true
		return Decision{Suppress: d.Suppress, Notify: true, ParaNota: true}
	}
	// Uma soltura do Decider do ditado que ficou armado depois de uma sessão da
	// nota acabar: a sessão dele já foi encerrada, e o núcleo não recebe um
	// segundo keyup.
	return Decision{Suppress: d.Suppress}
}
