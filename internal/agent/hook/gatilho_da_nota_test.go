package hook

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

func modificador(k dictation.KeyCode, mods dictation.Modifiers, down bool) dictation.RawKeyEvent {
	return dictation.RawKeyEvent{Key: k, Modifiers: mods, Down: down, At: time.Now()}
}

// TestDecidir_LevaAMarcaDaNota: o evento que vai ao runtime leva a marca do
// gatilho da nota quando a sessão é dele, e só então.
func TestDecidir_LevaAMarcaDaNota(t *testing.T) {
	ctrlWin := dictation.ModControl | dictation.ModMeta
	h := New(Config{Modifiers: ctrlWin, Nota: ctrlWin | dictation.ModAlt})

	h.decidir(modificador(dictation.ControlLeft, dictation.ModControl, true))
	h.decidir(modificador(dictation.AltLeft, dictation.ModControl|dictation.ModAlt, true))
	d, ev := h.decidir(modificador(dictation.MetaLeft, ctrlWin|dictation.ModAlt, true))
	if !d.Notify || !ev.ParaNota {
		t.Fatalf("Ctrl+Alt+Win: decisão %+v, evento ParaNota=%v; quer a sessão da nota", d, ev.ParaNota)
	}
	d, ev = h.decidir(modificador(dictation.MetaLeft, dictation.ModControl|dictation.ModAlt, false))
	if !d.Notify || !ev.ParaNota {
		t.Fatalf("soltura: decisão %+v, evento ParaNota=%v", d, ev.ParaNota)
	}
	h.decidir(modificador(dictation.AltLeft, dictation.ModControl, false))

	// Ctrl+Win sozinho: sem marca.
	_, ev = h.decidir(modificador(dictation.MetaLeft, ctrlWin, true))
	if ev.ParaNota {
		t.Fatal("Ctrl+Win sem Alt saiu marcado para nota")
	}
}

// TestDecidir_LevaOToqueLimpo: a soltura que vai ao runtime leva a marca do
// toque limpo (ADR-025), e só a soltura de um toque sem outra tecla no meio.
func TestDecidir_LevaOToqueLimpo(t *testing.T) {
	ctrlWin := dictation.ModControl | dictation.ModMeta
	h := New(Config{Modifiers: ctrlWin})

	h.decidir(modificador(dictation.ControlLeft, dictation.ModControl, true))
	_, ev := h.decidir(modificador(dictation.MetaLeft, ctrlWin, true))
	if ev.Limpo {
		t.Fatal("o aperto que arma saiu marcado como limpo")
	}
	_, ev = h.decidir(modificador(dictation.MetaLeft, dictation.ModControl, false))
	if !ev.Limpo {
		t.Fatal("a soltura de Ctrl+Win sem outra tecla saiu sem a marca de limpo")
	}

	h.decidir(modificador(dictation.MetaLeft, ctrlWin, true))
	h.decidir(modificador(dictation.KeyUnknown, ctrlWin, true)) // a seta
	_, ev = h.decidir(modificador(dictation.MetaLeft, dictation.ModControl, false))
	if ev.Limpo {
		t.Fatal("a soltura de Ctrl+Win+seta saiu marcada como limpa")
	}
	h.decidir(modificador(dictation.ControlLeft, 0, false))
}

// TestDecidir_SemGatilhoDaNota: com Nota zero, nada é marcado, e Ctrl+Win+Alt é
// o ditado de sempre (o conjunto do ditado está inteiro).
func TestDecidir_SemGatilhoDaNota(t *testing.T) {
	ctrlWin := dictation.ModControl | dictation.ModMeta
	h := New(Config{Modifiers: ctrlWin})
	h.decidir(modificador(dictation.ControlLeft, dictation.ModControl, true))
	h.decidir(modificador(dictation.AltLeft, dictation.ModControl|dictation.ModAlt, true))
	d, ev := h.decidir(modificador(dictation.MetaLeft, ctrlWin|dictation.ModAlt, true))
	if !d.Notify || ev.ParaNota {
		t.Fatalf("sem gatilho da nota: decisão %+v, ParaNota=%v; quer o ditado de sempre", d, ev.ParaNota)
	}
}
