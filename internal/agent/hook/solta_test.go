package hook

import (
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// O lado do hook da gravação solta (ADR-025, D). Nada aqui instala hook: o
// Hook dos testes nunca passa por Start, e GravacaoSolta sem hook rodando só
// troca a marca, sem chamada de sistema nenhuma.

func esc(mods dictation.Modifiers, down bool) dictation.RawKeyEvent {
	return modificador(dictation.KeyEscape, mods, down)
}

// TestDecidir_EscSoDescartaNaGravacaoSolta: fora da gravação solta o Esc
// passa; nela, o aperto é engolido e vai ao runtime marcado para descartar, e
// a soltura é engolida sem ir.
func TestDecidir_EscSoDescartaNaGravacaoSolta(t *testing.T) {
	h := New(Config{Modifiers: dictation.ModControl | dictation.ModMeta})

	if d, ev := h.decidir(esc(0, true)); d.Suppress || d.Notify || ev.Descartar {
		t.Fatalf("Esc sem gravação solta: %+v, Descartar=%v; quer passar", d, ev.Descartar)
	}
	h.decidir(esc(0, false))

	if err := h.GravacaoSolta(true); err != nil {
		t.Fatalf("GravacaoSolta(true) sem hook rodando: %v", err)
	}
	d, ev := h.decidir(esc(0, true))
	if !d.Suppress || !d.Notify || !ev.Descartar {
		t.Fatalf("Esc na gravação solta: %+v, Descartar=%v; quer engolir e descartar", d, ev.Descartar)
	}
	if err := h.GravacaoSolta(false); err != nil {
		t.Fatalf("GravacaoSolta(false): %v", err)
	}
	d, ev = h.decidir(esc(0, false))
	if !d.Suppress || d.Notify || ev.Descartar {
		t.Fatalf("soltura do Esc engolido: %+v, Descartar=%v; quer engolir sem notificar", d, ev.Descartar)
	}
}

// TestDecidir_CtrlShiftEscPassaNaGravacaoSolta: o Gerenciador de Tarefas
// abre com ou sem gravação solta.
func TestDecidir_CtrlShiftEscPassaNaGravacaoSolta(t *testing.T) {
	h := New(Config{Modifiers: dictation.ModControl | dictation.ModMeta})
	_ = h.GravacaoSolta(true)
	for _, down := range []bool{true, false} {
		if d, ev := h.decidir(esc(dictation.ModControl|dictation.ModShift, down)); d.Suppress || d.Notify || ev.Descartar {
			t.Fatalf("Ctrl+Shift+Esc (down=%v): %+v; quer passar", down, d)
		}
	}
}

// TestDecidir_OToqueQueParaNaoVaiComoEsc: na gravação solta, o toque no
// Ctrl+Win chega ao runtime como o gatilho de sempre, com a marca de limpo.
func TestDecidir_OToqueQueParaNaoVaiComoEsc(t *testing.T) {
	ctrlWin := dictation.ModControl | dictation.ModMeta
	h := New(Config{Modifiers: ctrlWin})
	_ = h.GravacaoSolta(true)
	h.decidir(modificador(dictation.ControlLeft, dictation.ModControl, true))
	if d, ev := h.decidir(modificador(dictation.MetaLeft, ctrlWin, true)); !d.Notify || ev.Descartar {
		t.Fatalf("aperto do gatilho na gravação solta: %+v, Descartar=%v", d, ev.Descartar)
	}
	if d, ev := h.decidir(modificador(dictation.MetaLeft, dictation.ModControl, false)); !d.Notify || !ev.Limpo || ev.Descartar {
		t.Fatalf("soltura do gatilho na gravação solta: %+v, Limpo=%v Descartar=%v", d, ev.Limpo, ev.Descartar)
	}
}

// TestPausa_GravacaoSoltaTermina: pausar no meio da gravação solta não pode
// deixar o toque que a para passar direto, ou ela seguiria até os 2 minutos.
// A pausa vale para a próxima sessão.
func TestPausa_GravacaoSoltaTermina(t *testing.T) {
	h := New(Config{Modifiers: dictation.ModControl | dictation.ModMeta})
	_ = h.GravacaoSolta(true)
	h.Pausar(true)
	if h.deveIgnorar() {
		t.Fatal("a pausa ignorou a gravação solta em curso")
	}
	_ = h.GravacaoSolta(false)
	if !h.deveIgnorar() {
		t.Fatal("terminada a gravação solta, a pausa devia valer")
	}
}

// TestPausa_EscEngolidoSoltaEngolido: o Esc que descartou foi engolido, a
// gravação solta acabou e a pausa chegou antes da soltura. A soltura ainda
// tem de ser engolida, ou o Windows fica com o Esc preso.
func TestPausa_EscEngolidoSoltaEngolido(t *testing.T) {
	h := New(Config{Modifiers: dictation.ModControl | dictation.ModMeta})
	_ = h.GravacaoSolta(true)
	h.decidir(esc(0, true))
	_ = h.GravacaoSolta(false)
	h.Pausar(true)
	if h.deveIgnorar() {
		t.Fatal("pausado com o Esc engolido: a soltura passaria sem o aperto")
	}
	if d, _ := h.decidir(esc(0, false)); !d.Suppress {
		t.Fatalf("soltura do Esc: %+v, quer engolida", d)
	}
	if !h.deveIgnorar() {
		t.Fatal("solto o Esc, a pausa devia valer")
	}
}

// TestGravacaoSolta_DevolveAFalhaDoMouse: a falha da instalação do hook do
// mouse volta uma vez, quando a gravação solta desliga.
func TestGravacaoSolta_DevolveAFalhaDoMouse(t *testing.T) {
	h := New(Config{Modifiers: dictation.ModControl | dictation.ModMeta})
	falha := errors.New("recusado")
	h.erroDoMouse.Store(&falha)
	if err := h.GravacaoSolta(true); err != nil {
		t.Fatalf("ao ligar: %v; a falha é dita ao desligar", err)
	}
	if err := h.GravacaoSolta(false); !errors.Is(err, falha) {
		t.Fatalf("ao desligar: %v, quer a falha do mouse", err)
	}
	if err := h.GravacaoSolta(false); err != nil {
		t.Fatalf("a falha voltou duas vezes: %v", err)
	}
}
