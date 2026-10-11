package hook

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// nonModifierKeys enumera toda dictation.KeyCode que Config.Validate
// aceita como tecla principal (AC-004 proíbe só as modificadoras). Mantida
// aqui, não em dictation, porque é um detalhe de teste deste pacote.
func nonModifierKeys() []dictation.KeyCode {
	var keys []dictation.KeyCode
	for k := dictation.KeyA; k <= dictation.KeyBackspace; k++ {
		if !k.IsModifier() {
			keys = append(keys, k)
		}
	}
	return keys
}

func TestKeyToChar_CoversEveryEligibleMainKey(t *testing.T) {
	for _, k := range nonModifierKeys() {
		if _, ok := keyToChar[k]; !ok {
			t.Errorf("keyToChar não mapeia %v — SynthesizeTap falharia para uma tecla principal válida (AC-004 a permite)", k)
		}
	}
}

func TestVKToKey_LettersAndDigitsRoundTrip(t *testing.T) {
	for i := 0; i < 26; i++ {
		vk := uint32('A') + uint32(i)
		want := dictation.KeyA + dictation.KeyCode(i)
		if got := vkToKey[vk]; got != want {
			t.Errorf("vkToKey[%#x] = %v, want %v", vk, got, want)
		}
	}
	for i := 0; i < 10; i++ {
		vk := uint32('0') + uint32(i)
		want := dictation.Key0 + dictation.KeyCode(i)
		if got := vkToKey[vk]; got != want {
			t.Errorf("vkToKey[%#x] = %v, want %v", vk, got, want)
		}
	}
}

func TestVKToKey_ModifierFallbacksMapToAKnownSide(t *testing.T) {
	generic := map[uint32]dictation.KeyCode{
		vkShift:   dictation.ShiftLeft,
		vkControl: dictation.ControlLeft,
		vkMenu:    dictation.AltLeft,
	}
	for vk, want := range generic {
		if got := vkToKey[vk]; got != want {
			t.Errorf("vkToKey[%#x] = %v, want %v (fallback genérico)", vk, got, want)
		}
	}
}
