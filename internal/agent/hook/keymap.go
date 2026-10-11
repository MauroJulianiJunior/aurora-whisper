package hook

// Este arquivo não tem build tag: é só tabela estática (map literal),
// sem nenhuma chamada de sistema — compila e é testável em qualquer GOOS,
// igual a internal/agent/win32/ticks.go.
//
// Os valores de VK_* abaixo são os publicados pela documentação do Win32
// (winuser.h). Não são reexportados por internal/agent/win32 porque são
// específicos da tradução tecla-a-tecla que só este pacote precisa fazer;
// win32 permanece "burro" (ver seu doc.go).
//
// Letras 'A'-'Z' e dígitos '0'-'9' têm o mesmo valor numérico como VK_*
// e como código ASCII — não precisam de constante própria, só do laço
// abaixo.

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

const (
	vkBack   uint32 = 0x08 // VK_BACK (Backspace)
	vkTab    uint32 = 0x09 // VK_TAB
	vkReturn uint32 = 0x0D // VK_RETURN (Enter)
	vkEscape uint32 = 0x1B // VK_ESCAPE
	vkSpace  uint32 = 0x20 // VK_SPACE

	// Genéricos: o hook WH_KEYBOARD_LL normalmente já entrega a variante
	// L/R específica (ver comentário de KBDLLHOOKSTRUCT em win32/types.go),
	// mas os genéricos são mantidos como fallback defensivo — melhor
	// reconhecer "é um modificador, lado indeterminado" do que cair em
	// KeyUnknown e perder o rastreio do estado de Shift/Ctrl/Alt.
	vkShift   uint32 = 0x10 // VK_SHIFT (genérico)
	vkControl uint32 = 0x11 // VK_CONTROL (genérico)
	vkMenu    uint32 = 0x12 // VK_MENU / Alt (genérico)

	vkLShift   uint32 = 0xA0
	vkRShift   uint32 = 0xA1
	vkLControl uint32 = 0xA2
	vkRControl uint32 = 0xA3
	vkLMenu    uint32 = 0xA4
	vkRMenu    uint32 = 0xA5

	vkLWin uint32 = 0x5B
	vkRWin uint32 = 0x5C
)

// vkToKey mapeia todo VK_* que o hook pode receber e que dictation.KeyCode
// consegue representar. VK_* fora deste mapa traduz para dictation.KeyUnknown
// (ainda assim um dictation.RawKeyEvent válido: o Decider descarta com
// PassThrough por não bater com a tecla configurada — ADR-007, seção 3).
var vkToKey = buildVKToKey()

func buildVKToKey() map[uint32]dictation.KeyCode {
	m := make(map[uint32]dictation.KeyCode, 48)

	for i := 0; i < 26; i++ {
		m[uint32('A')+uint32(i)] = dictation.KeyA + dictation.KeyCode(i)
	}
	for i := 0; i < 10; i++ {
		m[uint32('0')+uint32(i)] = dictation.Key0 + dictation.KeyCode(i)
	}

	m[vkSpace] = dictation.KeySpace
	m[vkReturn] = dictation.KeyEnter
	m[vkTab] = dictation.KeyTab
	m[vkEscape] = dictation.KeyEscape
	m[vkBack] = dictation.KeyBackspace

	m[vkLShift] = dictation.ShiftLeft
	m[vkRShift] = dictation.ShiftRight
	m[vkShift] = dictation.ShiftLeft // fallback: lado indeterminado

	m[vkLControl] = dictation.ControlLeft
	m[vkRControl] = dictation.ControlRight
	m[vkControl] = dictation.ControlLeft // fallback: lado indeterminado

	m[vkLMenu] = dictation.AltLeft
	m[vkRMenu] = dictation.AltRight
	m[vkMenu] = dictation.AltLeft // fallback: lado indeterminado

	m[vkLWin] = dictation.MetaLeft
	m[vkRWin] = dictation.MetaRight

	return m
}

// keyToChar mapeia toda dictation.KeyCode elegível a ser a tecla principal
// do ditado (isto é, toda tecla não modificadora — Config.Validate já
// proíbe modificador puro como tecla principal, AC-004) para o code point
// Unicode que SynthesizeTap injeta via SendInput(KEYEVENTF_UNICODE), no
// caso de tap curto (AC-001).
//
// É best-effort para as teclas de controle (Enter/Tab/Escape/Backspace):
// KEYEVENTF_UNICODE injeta um caractere, não uma tecla virtual, e nem todo
// app trata os code points de controle (ex. U+001B para Escape) como a
// tecla física equivalente. O inserter robusto (TASK-004, fora do escopo
// do Sprint 1 — ver ADR-010, "Escopo do Sprint 1") resolve isso de vez;
// aqui o objetivo é só a fatia mínima demonstrável.
var keyToChar = buildKeyToChar()

func buildKeyToChar() map[dictation.KeyCode]uint16 {
	m := make(map[dictation.KeyCode]uint16, 42)

	for i := 0; i < 26; i++ {
		m[dictation.KeyA+dictation.KeyCode(i)] = uint16('a' + i)
	}
	for i := 0; i < 10; i++ {
		m[dictation.Key0+dictation.KeyCode(i)] = uint16('0' + i)
	}

	m[dictation.KeySpace] = uint16(' ')
	m[dictation.KeyEnter] = uint16('\r')
	m[dictation.KeyTab] = uint16('\t')
	m[dictation.KeyEscape] = 0x1B
	m[dictation.KeyBackspace] = uint16('\b')

	return m
}
