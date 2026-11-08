package dictation

import "testing"

// passoEsc é um evento e se a gravação solta estava ligada nele, com o que o
// Esc deve responder.
type passoEsc struct {
	ev             RawKeyEvent
	solta          bool
	supr, notifica bool
}

func rodarEsc(t *testing.T, passos []passoEsc) {
	t.Helper()
	var e Esc
	for i, s := range passos {
		got := e.Decide(s.ev, s.solta)
		if got.Suppress != s.supr || got.Notify != s.notifica {
			t.Errorf("passo %d (%+v, solta=%v): %+v, quer Suppress=%v Notify=%v",
				i, s.ev, s.solta, got, s.supr, s.notifica)
		}
	}
	if e.preso {
		t.Error("terminada a sequência, o Esc continua preso")
	}
}

func TestEsc_ForaDaSoltaPassa(t *testing.T) {
	rodarEsc(t, []passoEsc{
		{ev: ev(KeyEscape, 0, true)},
		{ev: ev(KeyEscape, 0, false)},
	})
}

func TestEsc_NaSoltaDescartaEmPar(t *testing.T) {
	rodarEsc(t, []passoEsc{
		{ev: ev(KeyEscape, 0, true), solta: true, supr: true, notifica: true},
		// O descarte desliga a solta; as repetições e a soltura do mesmo
		// aperto continuam engolidas, ou o Windows ficaria com o Esc preso.
		{ev: ev(KeyEscape, 0, true), supr: true},
		{ev: ev(KeyEscape, 0, true), supr: true},
		{ev: ev(KeyEscape, 0, false), supr: true},
		// O aperto seguinte, sem gravação solta, passa.
		{ev: ev(KeyEscape, 0, true)},
		{ev: ev(KeyEscape, 0, false)},
	})
}

func TestEsc_ApertadoAntesDaSoltaSoltaNormal(t *testing.T) {
	rodarEsc(t, []passoEsc{
		{ev: ev(KeyEscape, 0, true)},
		{ev: ev(KeyEscape, 0, false), solta: true},
	})
}

func TestEsc_ComModificadorPassa(t *testing.T) {
	rodarEsc(t, []passoEsc{
		// Ctrl+Shift+Esc, o Gerenciador de Tarefas.
		{ev: ev(KeyEscape, ModControl|ModShift, true), solta: true},
		{ev: ev(KeyEscape, ModControl|ModShift, false), solta: true},
		// Ctrl+Esc, o Iniciar.
		{ev: ev(KeyEscape, ModControl, true), solta: true},
		{ev: ev(KeyEscape, ModControl, false), solta: true},
	})
}

func TestEsc_ModificadorDepoisDoApertoNaoQuebraOPar(t *testing.T) {
	rodarEsc(t, []passoEsc{
		{ev: ev(KeyEscape, 0, true), solta: true, supr: true, notifica: true},
		{ev: ev(KeyEscape, ModControl, false), supr: true},
	})
}

func TestEsc_OutrasTeclasEInjetadoPorNosPassam(t *testing.T) {
	nosso := ev(KeyEscape, 0, true)
	nosso.Injected, nosso.FromThisProcess = true, true
	rodarEsc(t, []passoEsc{
		{ev: ev(KeyA, 0, true), solta: true},
		{ev: ev(KeyA, 0, false), solta: true},
		{ev: nosso, solta: true},
	})
}

// TestEsc_Preso: preso do aperto engolido até a soltura dele, e só então.
func TestEsc_Preso(t *testing.T) {
	var e Esc
	e.Decide(ev(KeyEscape, 0, true), false)
	if e.Preso() {
		t.Fatal("Esc que passou ficou preso")
	}
	e.Decide(ev(KeyEscape, 0, false), false)
	e.Decide(ev(KeyEscape, 0, true), true)
	if !e.Preso() {
		t.Fatal("Esc engolido não está preso")
	}
	e.Decide(ev(KeyEscape, 0, true), false) // repetição
	if !e.Preso() {
		t.Fatal("a repetição soltou o Esc")
	}
	e.Decide(ev(KeyEscape, 0, false), false)
	if e.Preso() {
		t.Fatal("a soltura não soltou o Esc")
	}
}
