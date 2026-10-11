package hook

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

func mustTranslate(t *testing.T, tr *Translator, vk uint32, down, injected bool, extraInfo uintptr) dictation.RawKeyEvent {
	t.Helper()
	return tr.Translate(vk, down, injected, extraInfo, time.Unix(1_700_000_000, 0))
}

func TestTranslate_PhysicalKeydown(t *testing.T) {
	tr := NewTranslator()

	ev := mustTranslate(t, tr, uint32('F'), true, false, 0)

	if ev.Key != dictation.KeyF {
		t.Fatalf("Key = %v, want KeyF", ev.Key)
	}
	if !ev.Down {
		t.Fatal("Down = false, want true")
	}
	if ev.Injected {
		t.Fatal("Injected = true, want false")
	}
	if ev.FromThisProcess {
		t.Fatal("FromThisProcess = true, want false")
	}
	if !ev.Modifiers.IsNone() {
		t.Fatalf("Modifiers = %v, want none", ev.Modifiers)
	}
}

func TestTranslate_UnknownVKBecomesKeyUnknown(t *testing.T) {
	tr := NewTranslator()

	// VK_F13 (0x7C) não está em dictation.KeyCode — deve traduzir para
	// KeyUnknown, um RawKeyEvent ainda válido que o Decider descarta por
	// PassThrough (ADR-007, seção 3, regra 2), não um erro.
	ev := mustTranslate(t, tr, 0x7C, true, false, 0)

	if ev.Key != dictation.KeyUnknown {
		t.Fatalf("Key = %v, want KeyUnknown", ev.Key)
	}
}

func TestTranslate_ModifierHeldAcrossMainKeyEvents(t *testing.T) {
	tr := NewTranslator()

	// Shift esquerdo desce...
	shiftDown := mustTranslate(t, tr, vkLShift, true, false, 0)
	if !shiftDown.Modifiers.Has(dictation.ModShift) {
		t.Fatal("Modifiers do próprio keydown de Shift deveria incluir ModShift")
	}

	// ...a tecla principal (F) desce enquanto Shift segue pressionado.
	fDown := mustTranslate(t, tr, uint32('F'), true, false, 0)
	if !fDown.Modifiers.Has(dictation.ModShift) {
		t.Fatalf("Modifiers = %v, want ModShift presente (Shift ainda segurado)", fDown.Modifiers)
	}

	// Shift sobe...
	mustTranslate(t, tr, vkLShift, false, false, 0)

	// ...e um novo keydown de F não deveria mais carregar ModShift.
	fDown2 := mustTranslate(t, tr, uint32('F'), true, false, 0)
	if fDown2.Modifiers.Has(dictation.ModShift) {
		t.Fatalf("Modifiers = %v, want ModShift ausente (Shift já solto)", fDown2.Modifiers)
	}
}

func TestTranslate_IndependentShiftSides(t *testing.T) {
	tr := NewTranslator()

	mustTranslate(t, tr, vkLShift, true, false, 0)
	mustTranslate(t, tr, vkRShift, true, false, 0)
	mustTranslate(t, tr, vkLShift, false, false, 0) // solta só o esquerdo

	ev := mustTranslate(t, tr, uint32('F'), true, false, 0)
	if !ev.Modifiers.Has(dictation.ModShift) {
		t.Fatal("ModShift deveria continuar presente: Shift direito ainda segurado")
	}
}

// TestTranslate_SynthesizedTapLoopsBackAndIsIgnored é o caso explicitamente
// pedido pela tarefa: o tap que SynthesizeTap injeta via SendInput volta
// pelo próprio hook. Sem a marca de DwExtraInfo (marker.go), isso seria
// reinterpretado como um novo keydown/keyup físico da tecla de ditado —
// exatamente o laço infinito que ADR-007 seção 3 manda evitar. Verificamos
// a cadeia inteira: Translate (produz FromThisProcess=true) seguido de
// Decider.Decide (devolve PassThrough, não inicia nova tentativa).
func TestTranslate_SynthesizedTapLoopsBackAndIsIgnored(t *testing.T) {
	tr := NewTranslator()
	decider := dictation.NewDecider(dictation.KeyF)

	// keydown físico real: inicia uma tentativa (Suppress+Notify).
	real := mustTranslate(t, tr, uint32('F'), true, false, 0)
	if d := decider.Decide(real); !d.Suppress || !d.Notify {
		t.Fatalf("keydown físico: Decision = %+v, want Suppress+Notify", d)
	}

	// SynthesizeTap injeta keydown+keyup marcados com markerExtraInfo
	// (hook_windows.go). Eles voltam pelo hook com Injected=true e
	// DwExtraInfo=markerExtraInfo.
	synthDown := mustTranslate(t, tr, uint32('F'), true, true, markerExtraInfo)
	if !synthDown.Injected {
		t.Fatal("Injected = false, want true (veio de SendInput)")
	}
	if !synthDown.FromThisProcess {
		t.Fatal("FromThisProcess = false, want true (marca deste processo presente)")
	}
	if d := decider.Decide(synthDown); d.Suppress || d.Notify {
		t.Fatalf("tap sintetizado (keydown): Decision = %+v, want PassThrough — laço infinito não evitado", d)
	}

	synthUp := mustTranslate(t, tr, uint32('F'), false, true, markerExtraInfo)
	if !synthUp.FromThisProcess {
		t.Fatal("FromThisProcess = false, want true (marca deste processo presente)")
	}
	if d := decider.Decide(synthUp); d.Suppress || d.Notify {
		t.Fatalf("tap sintetizado (keyup): Decision = %+v, want PassThrough — laço infinito não evitado", d)
	}
}

// TestTranslate_InjectedByOtherProcessIsTreatedAsPhysical documenta o gap
// conhecido e aceito do ADR-007 seção 3: um evento injetado por outro
// processo (ex. AutoHotkey) não carrega a marca deste processo, então
// FromThisProcess fica false e o Decider o trata como físico.
func TestTranslate_InjectedByOtherProcessIsTreatedAsPhysical(t *testing.T) {
	tr := NewTranslator()
	decider := dictation.NewDecider(dictation.KeyF)

	const someOtherMarker uintptr = 0xDEAD

	ev := mustTranslate(t, tr, uint32('F'), true, true, someOtherMarker)
	if !ev.Injected {
		t.Fatal("Injected = false, want true")
	}
	if ev.FromThisProcess {
		t.Fatal("FromThisProcess = true, want false — a marca não é deste processo")
	}

	d := decider.Decide(ev)
	if !d.Suppress || !d.Notify {
		t.Fatalf("Decision = %+v, want Suppress+Notify (gap conhecido: tratado como físico)", d)
	}
}

func TestTranslate_MainKeyWithModifierBypassesDictation(t *testing.T) {
	tr := NewTranslator()
	decider := dictation.NewDecider(dictation.KeyF)

	mustTranslate(t, tr, vkLControl, true, false, 0)
	ev := mustTranslate(t, tr, uint32('F'), true, false, 0)

	if !ev.Modifiers.Has(dictation.ModControl) {
		t.Fatalf("Modifiers = %v, want ModControl (Ctrl+F)", ev.Modifiers)
	}

	d := decider.Decide(ev)
	if d.Suppress || d.Notify {
		t.Fatalf("Ctrl+F: Decision = %+v, want PassThrough (AC-045, atalho não é ditado)", d)
	}
}

func TestIsOurs(t *testing.T) {
	if !IsOurs(markerExtraInfo) {
		t.Fatal("IsOurs(markerExtraInfo) = false, want true")
	}
	if IsOurs(0) {
		t.Fatal("IsOurs(0) = true, want false")
	}
	if IsOurs(markerExtraInfo ^ 1) {
		t.Fatal("IsOurs(marker com 1 bit alterado) = true, want false")
	}
}
