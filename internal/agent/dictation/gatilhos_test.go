package dictation

import (
	"errors"
	"math/rand"
	"testing"
)

const ctrlWinAlt = ModControl | ModMeta | ModAlt

// passo é um evento do teclado e o que o decisor dos dois gatilhos deve
// responder a ele.
type passo struct {
	ev                     RawKeyEvent
	notify, paraNota, supr bool
}

func p(key KeyCode, mods Modifiers, down, notify, paraNota bool) passo {
	return passo{ev: ev(key, mods, down), notify: notify, paraNota: paraNota}
}

func rodarPassos(t *testing.T, g *Gatilhos, passos []passo) {
	t.Helper()
	for i, s := range passos {
		got := g.Decide(s.ev)
		if got.Notify != s.notify || got.ParaNota != s.paraNota || got.Suppress != s.supr {
			t.Errorf("passo %d (%+v): got %+v, quer Notify=%v ParaNota=%v Suppress=%v",
				i, s.ev, got, s.notify, s.paraNota, s.supr)
		}
	}
	if g.Armado() {
		t.Error("terminada a sequência, o decisor continua armado")
	}
}

// TestGatilhos_Desambiguacao é a tabela das ordens de apertar e soltar, com o
// padrão: Ctrl+Win (ditado) e Ctrl+Win+Alt (nota).
func TestGatilhos_Desambiguacao(t *testing.T) {
	casos := []struct {
		nome   string
		passos []passo
	}{
		{"Ctrl+Win sem Alt: o ditado de sempre, sem marca", []passo{
			p(ControlLeft, ModControl, true, false, false),
			p(MetaLeft, ctrlWin, true, true, false),
			p(ControlLeft, ModMeta, false, true, false),
			p(MetaLeft, 0, false, false, false),
		}},
		{"Win antes de Ctrl, solta Win primeiro: sem marca", []passo{
			p(MetaLeft, ModMeta, true, false, false),
			p(ControlLeft, ctrlWin, true, true, false),
			p(MetaLeft, ModControl, false, true, false),
			p(ControlLeft, 0, false, false, false),
		}},
		{"Alt primeiro, depois Ctrl e Win: nota desde o começo", []passo{
			p(AltLeft, ModAlt, true, false, false),
			p(ControlLeft, ModAlt|ModControl, true, false, false),
			p(MetaLeft, ctrlWinAlt, true, true, true),
			p(AltLeft, ctrlWin, false, false, false),
			p(MetaLeft, ModControl, false, true, true),
			p(ControlLeft, 0, false, false, false),
		}},
		{"Ctrl, Alt, Win: nota", []passo{
			p(ControlLeft, ModControl, true, false, false),
			p(AltLeft, ModControl|ModAlt, true, false, false),
			p(MetaLeft, ctrlWinAlt, true, true, true),
			p(ControlLeft, ModAlt|ModMeta, false, true, true),
			p(AltLeft, ModMeta, false, false, false),
			p(MetaLeft, 0, false, false, false),
		}},
		{"Alt atrasado, com a gravação já armada: a soltura leva a marca", []passo{
			p(ControlLeft, ModControl, true, false, false),
			p(MetaLeft, ctrlWin, true, true, false),
			p(AltLeft, ctrlWinAlt, true, false, false),
			p(AltLeft, ctrlWin, false, false, false),
			p(ControlLeft, ModMeta, false, true, true),
			p(MetaLeft, 0, false, false, false),
		}},
		{"soltar o Alt antes não devolve o ditado ao campo", []passo{
			p(ControlLeft, ModControl, true, false, false),
			p(MetaLeft, ctrlWin, true, true, false),
			p(AltLeft, ctrlWinAlt, true, false, false),
			p(AltLeft, ctrlWin, false, false, false),
			p(MetaLeft, ModControl, false, true, true),
			p(ControlLeft, 0, false, false, false),
		}},
		{"Alt direito também vale (o Translator junta os lados)", []passo{
			p(ControlRight, ModControl, true, false, false),
			p(AltRight, ModControl|ModAlt, true, false, false),
			p(MetaRight, ctrlWinAlt, true, true, true),
			p(MetaRight, ModControl|ModAlt, false, true, true),
			p(AltRight, ModControl, false, false, false),
			p(ControlRight, 0, false, false, false),
		}},
		{"Ctrl+Alt sem Win: nada", []passo{
			p(ControlLeft, ModControl, true, false, false),
			p(AltLeft, ModControl|ModAlt, true, false, false),
			p(KeyD, ModControl|ModAlt, true, false, false),
			p(KeyD, ModControl|ModAlt, false, false, false),
			p(AltLeft, ModControl, false, false, false),
			p(ControlLeft, 0, false, false, false),
		}},
		{"duas sessões seguidas: a marca da primeira não vaza para a segunda", []passo{
			p(ControlLeft, ModControl, true, false, false),
			p(AltLeft, ModControl|ModAlt, true, false, false),
			p(MetaLeft, ctrlWinAlt, true, true, true),
			p(MetaLeft, ModControl|ModAlt, false, true, true),
			p(AltLeft, ModControl, false, false, false),
			p(MetaLeft, ctrlWin, true, true, false),
			p(MetaLeft, ModControl, false, true, false),
			p(ControlLeft, 0, false, false, false),
		}},
		{"tecla comum no meio não quebra a sessão nem a marca", []passo{
			p(ControlLeft, ModControl, true, false, false),
			p(MetaLeft, ctrlWin, true, true, false),
			p(AltLeft, ctrlWinAlt, true, false, false),
			p(KeyD, ctrlWinAlt, true, false, false),
			p(KeyD, ctrlWinAlt, false, false, false),
			p(ControlLeft, ModMeta|ModAlt, false, true, true),
			p(AltLeft, ModMeta, false, false, false),
			p(MetaLeft, 0, false, false, false),
		}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rodarPassos(t, NovosGatilhos(NewDeciderModificadores(ctrlWin), ctrlWinAlt), c.passos)
		})
	}
}

// TestGatilhos_NuncaSuprimeModificador repete, para os dois gatilhos, a regra
// mais importante do ADR-013: nenhum modificador suprimido.
func TestGatilhos_NuncaSuprimeModificador(t *testing.T) {
	g := NovosGatilhos(NewDeciderModificadores(ctrlWin), ctrlWinAlt)
	r := rand.New(rand.NewSource(7))
	for i, e := range sequenciaAleatoria(r, 2000) {
		if got := g.Decide(e); got.Suppress {
			t.Fatalf("evento %d (%+v): Suppress = true", i, e)
		}
	}
}

// teclasDoSorteio são as teclas que a sequência aleatória aperta e solta.
var teclasDoSorteio = []KeyCode{ControlLeft, ControlRight, MetaLeft, AltLeft, AltRight, ShiftLeft, KeyD, KeyF}

// sequenciaAleatoria aperta e solta teclas ao acaso, com Modifiers montado
// como o Translator monta (o estado DEPOIS do evento, juntando os lados).
func sequenciaAleatoria(r *rand.Rand, n int) []RawKeyEvent {
	seguras := map[KeyCode]bool{}
	mods := func() Modifiers {
		var m Modifiers
		if seguras[ControlLeft] || seguras[ControlRight] {
			m |= ModControl
		}
		if seguras[MetaLeft] {
			m |= ModMeta
		}
		if seguras[AltLeft] || seguras[AltRight] {
			m |= ModAlt
		}
		if seguras[ShiftLeft] {
			m |= ModShift
		}
		return m
	}
	var out []RawKeyEvent
	for range n {
		k := teclasDoSorteio[r.Intn(len(teclasDoSorteio))]
		down := !seguras[k]
		if k.IsModifier() {
			seguras[k] = down
		} else if r.Intn(2) == 0 {
			down = true // tecla comum: o auto-repeat manda keydown de novo
		}
		out = append(out, ev(k, mods(), down))
		if !k.IsModifier() && down {
			out = append(out, ev(k, mods(), false))
		}
	}
	return out
}

// TestGatilhos_CtrlWinIgualAoDeSempre é a garantia de que o Ctrl+Win não mudou:
// para qualquer sequência de teclas, as decisões de suprimir e notificar dos
// dois gatilhos (com o padrão) são as MESMAS do Decider sozinho. Só a marca
// ParaNota é nova — a sessão é a mesma, com a pílula, o bipe e o limiar de
// sempre.
func TestGatilhos_CtrlWinIgualAoDeSempre(t *testing.T) {
	for semente := int64(1); semente <= 50; semente++ {
		r := rand.New(rand.NewSource(semente))
		so := NewDeciderModificadores(ctrlWin)
		dois := NovosGatilhos(NewDeciderModificadores(ctrlWin), ctrlWinAlt)
		for i, e := range sequenciaAleatoria(r, 400) {
			a, b := so.Decide(e), dois.Decide(e)
			if a.Suppress != b.Suppress || a.Notify != b.Notify {
				t.Fatalf("semente %d, evento %d (%+v): Decider %+v, dois gatilhos %+v", semente, i, e, a, b)
			}
			if so.Armado() != dois.Armado() {
				t.Fatalf("semente %d, evento %d: armado diverge", semente, i)
			}
			if b.ParaNota && !b.Notify {
				t.Fatalf("semente %d, evento %d: marca sem notificação", semente, i)
			}
		}
	}
}

// TestGatilhos_SemNotaEhODecider: com o gatilho da nota desligado (zero), o
// decisor é o Decider, nos dois modos (combinação e tecla de letra), e nunca
// marca nada.
func TestGatilhos_SemNotaEhODecider(t *testing.T) {
	novos := []func() *Decider{
		func() *Decider { return NewDeciderModificadores(ctrlWin) },
		func() *Decider { return NewDecider(KeyF) },
	}
	for modo, novo := range novos {
		for semente := int64(1); semente <= 30; semente++ {
			r := rand.New(rand.NewSource(semente))
			so, dois := novo(), NovosGatilhos(novo(), 0)
			for i, e := range sequenciaAleatoria(r, 300) {
				a, b := so.Decide(e), dois.Decide(e)
				if a != b {
					t.Fatalf("modo %d, semente %d, evento %d (%+v): Decider %+v, dois gatilhos %+v", modo, semente, i, e, a, b)
				}
			}
		}
	}
}

// TestGatilhos_NotaQueNaoContemODitado: com um conjunto da nota que não contém
// o do ditado (Ctrl+Shift, hoje inalcançável pela tela, mas a regra é geral),
// a sessão é armada pelo conjunto da nota sozinho, e um Decider do ditado que
// arme no meio não manda um segundo keydown ao núcleo.
func TestGatilhos_NotaQueNaoContemODitado(t *testing.T) {
	ctrlShift := ModControl | ModShift
	g := NovosGatilhos(NewDeciderModificadores(ctrlWin), ctrlShift)
	rodarPassos(t, g, []passo{
		p(ControlLeft, ModControl, true, false, false),
		p(ShiftLeft, ctrlShift, true, true, true),
		p(MetaLeft, ctrlShift|ModMeta, true, false, false), // o do ditado arma calado
		p(ShiftLeft, ctrlWin, false, true, true),           // a nota quebrou: fim, uma soltura só
		p(MetaLeft, ModControl, false, false, false),       // o desarme do ditado não vai ao núcleo
		p(ControlLeft, 0, false, false, false),
	})
}

// TestGatilhos_NotaComTeclaDeLetra: com o ditado por tecla de letra, o gatilho
// da nota continua sendo uma combinação, e a supressão da letra segue a do
// Decider.
func TestGatilhos_NotaComTeclaDeLetra(t *testing.T) {
	g := NovosGatilhos(NewDecider(KeyF), ctrlWinAlt)
	rodarPassos(t, g, []passo{
		{ev: ev(KeyF, 0, true), notify: true, supr: true},
		{ev: ev(KeyF, 0, false), notify: true, supr: true},
		p(ControlLeft, ModControl, true, false, false),
		p(MetaLeft, ctrlWin, true, false, false),
		p(AltLeft, ctrlWinAlt, true, true, true),
		p(KeyF, ctrlWinAlt, true, false, false), // F com modificador: atalho, passa
		p(KeyF, ctrlWinAlt, false, false, false),
		p(AltLeft, ctrlWin, false, true, true),
		p(MetaLeft, ModControl, false, false, false),
		p(ControlLeft, 0, false, false, false),
	})
}

// TestGatilhos_InjetadoPorNosPassa: o evento que nós mesmos injetamos nunca
// arma nem solta nada.
func TestGatilhos_InjetadoPorNosPassa(t *testing.T) {
	g := NovosGatilhos(NewDeciderModificadores(ctrlWin), ctrlWinAlt)
	e := ev(MetaLeft, ctrlWinAlt, true)
	e.Injected, e.FromThisProcess = true, true
	if got := g.Decide(e); got != (Decision{}) {
		t.Fatalf("evento injetado por nós: %+v, quer passar direto", got)
	}
	if g.Armado() {
		t.Fatal("evento injetado armou o decisor")
	}
}

// TestGatilhos_PausaRespeitaSessaoDaNota: Armado cobre a sessão armada só pelo
// conjunto da nota, para a pausa do hook não engolir a soltura dela.
func TestGatilhos_PausaRespeitaSessaoDaNota(t *testing.T) {
	g := NovosGatilhos(NewDeciderModificadores(ctrlWin), ModControl|ModShift)
	g.Decide(ev(ControlLeft, ModControl, true))
	g.Decide(ev(ShiftLeft, ModControl|ModShift, true))
	if !g.Armado() {
		t.Fatal("a sessão da nota em curso não conta como armada")
	}
}

func TestValidarGatilhoDaNota(t *testing.T) {
	padrao := Config{Key: KeyUnknown, Modifiers: ctrlWin}
	casos := []struct {
		nome string
		cfg  Config
		nota Modifiers
		erro error
	}{
		{"desligado", padrao, 0, nil},
		{"o padrão", padrao, ctrlWinAlt, nil},
		{"Ctrl+Shift, que não contém o ditado", padrao, ModControl | ModShift, nil},
		{"um modificador só", padrao, ModAlt, ErrModificadorSolto},
		{"igual ao do ditado", padrao, ctrlWin, ErrGatilhoDaNotaIgual},
		{"contido no do ditado", Config{Key: KeyUnknown, Modifiers: ctrlWinAlt}, ctrlWin, ErrGatilhoDaNotaIgual},
		{"ditado por letra", Config{Key: KeyF}, ctrlWin, nil},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := ValidarGatilhoDaNota(c.cfg, c.nota)
			if c.erro == nil && err != nil || c.erro != nil && !errors.Is(err, c.erro) {
				t.Fatalf("ValidarGatilhoDaNota = %v, quer %v", err, c.erro)
			}
		})
	}
}
