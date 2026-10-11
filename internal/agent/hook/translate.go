package hook

// Este arquivo não tem build tag: a tradução em si (VK + flags -> um
// dictation.RawKeyEvent) é lógica pura sobre valores já decodificados —
// nenhuma chamada de sistema. É a peça que o enunciado do sprint pede
// para "testar sem Windows, incluindo o caso do tap sintetizado voltando
// pelo hook e sendo corretamente ignorado" (ver translate_test.go).
//
// Quem decodifica o KBDLLHOOKSTRUCT de verdade e converte
// KBDLLHOOKSTRUCT.Time (ticks) em time.Time via win32.TickReference é
// hook_windows.go; aqui Translate só recebe valores já simples.

import (
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// Índices do estado de modificadores mantido por Translator. Um por
// variante L/R, para poder responder simetricamente ao keyup de cada
// lado independente (o mesmo cuidado que dictation.Decider já toma com a
// tecla principal).
const (
	idxShiftL = iota
	idxShiftR
	idxControlL
	idxControlR
	idxAltL
	idxAltR
	idxMetaL
	idxMetaR
	modifierStateSize
)

// modifierIndex devolve o índice do estado mantido por Translator para a
// variante de modificador k, e false se k não for uma tecla modificadora.
func modifierIndex(k dictation.KeyCode) (int, bool) {
	switch k {
	case dictation.ShiftLeft:
		return idxShiftL, true
	case dictation.ShiftRight:
		return idxShiftR, true
	case dictation.ControlLeft:
		return idxControlL, true
	case dictation.ControlRight:
		return idxControlR, true
	case dictation.AltLeft:
		return idxAltL, true
	case dictation.AltRight:
		return idxAltR, true
	case dictation.MetaLeft:
		return idxMetaL, true
	case dictation.MetaRight:
		return idxMetaR, true
	default:
		return 0, false
	}
}

// Translator converte eventos crus de teclado (VK + flags) em
// dictation.RawKeyEvent, mantendo entre chamadas o único estado
// necessário para isso: quais teclas modificadoras estão pressionadas
// agora, para preencher RawKeyEvent.Modifiers de um evento da tecla
// principal (que não é, ela mesma, um modificador).
//
// Não é seguro para uso concorrente — mesma regra do Decider que
// consome sua saída: um único consumidor serializado (o callback do
// hook, sempre na mesma thread do sistema).
type Translator struct {
	held [modifierStateSize]bool
}

// NewTranslator cria um Translator com nenhum modificador pressionado.
func NewTranslator() *Translator {
	return &Translator{}
}

// Translate traduz um evento cru para dictation.RawKeyEvent.
//
//   - vk é o KBDLLHOOKSTRUCT.VkCode bruto.
//   - down indica keydown (true) ou keyup (false) — já decidido a partir
//     de wParam (WM_KEYDOWN/WM_SYSKEYDOWN vs WM_KEYUP/WM_SYSKEYUP).
//   - injected é KBDLLHOOKSTRUCT.Injected() (veio de SendInput, de
//     qualquer processo).
//   - extraInfo é KBDLLHOOKSTRUCT.DwExtraInfo, comparado contra a marca
//     deste processo (marker.go) para preencher FromThisProcess — mas só
//     quando injected também é true: um DwExtraInfo que por acaso bata
//     com a marca, num evento físico não injetado, não deveria classificar
//     como "nosso" (ver ADR-007 seção 3: FromThisProcess só é significado
//     para eventos injetados).
//   - at é o instante já convertido para time.Time, ancorado via
//     win32.TickReference (ADR-007, "Pendência para o Sprint 1") — nunca
//     time.Now().
//
// Se vk não corresponder a nenhuma dictation.KeyCode conhecida, o evento
// ainda é traduzido com Key = dictation.KeyUnknown: um
// dictation.RawKeyEvent válido que o Decider descarta com PassThrough por
// não bater com a tecla configurada (ADR-007, seção 3, regra 2).
func (t *Translator) Translate(vk uint32, down bool, injected bool, extraInfo uintptr, at time.Time) dictation.RawKeyEvent {
	key := vkToKey[vk]

	if idx, ok := modifierIndex(key); ok {
		t.held[idx] = down
	}

	return dictation.RawKeyEvent{
		Key:             key,
		Modifiers:       t.modifiers(),
		Down:            down,
		At:              at,
		Injected:        injected,
		FromThisProcess: injected && IsOurs(extraInfo),
	}
}

// modifiers monta o bitmask atual a partir do estado mantido por t,
// mesclando as variantes L/R de cada modificador — dictation.Modifiers
// não distingue lado, só se o modificador está pressionado.
func (t *Translator) modifiers() dictation.Modifiers {
	var m dictation.Modifiers
	if t.held[idxShiftL] || t.held[idxShiftR] {
		m |= dictation.ModShift
	}
	if t.held[idxControlL] || t.held[idxControlR] {
		m |= dictation.ModControl
	}
	if t.held[idxAltL] || t.held[idxAltR] {
		m |= dictation.ModAlt
	}
	if t.held[idxMetaL] || t.held[idxMetaR] {
		m |= dictation.ModMeta
	}
	return m
}
