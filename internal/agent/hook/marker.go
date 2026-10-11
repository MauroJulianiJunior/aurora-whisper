package hook

// Este arquivo não tem build tag: a marcação em si é aritmética pura
// sobre uintptr, sem nenhuma chamada de sistema.
//
// Problema (ADR-007, seção 3): KBDLLHOOKSTRUCT só diz "isto foi injetado
// por SendInput" (LLKHF_INJECTED) — nunca por QUEM. Sem saber se fomos
// nós, o tap que SynthesizeTap injeta para completar o AC-001 (tecla
// solta antes do threshold digita normalmente) volta pelo nosso próprio
// hook e é reinterpretado como um keydown/keyup físico da tecla de
// ditado — um laço infinito.
//
// Solução escolhida: KEYBDINPUT.DwExtraInfo é um campo de 8 bytes de uso
// livre que o SendInput do chamador controla, e o Win32 copia esse valor,
// sem alterar, para KBDLLHOOKSTRUCT.DwExtraInfo no evento que o hook
// recebe (documentado pelo próprio Win32; não é um campo "por acaso"
// disponível — existe para propósito exatamente como este). Marcando
// DwExtraInfo com uma constante fixa deste processo em todo SendInput que
// SynthesizeTap emite, o callback do hook reconhece a origem só comparando
// um uintptr — nenhum estado compartilhado, nenhuma janela de tempo,
// nenhuma fila para sincronizar entre a goroutine que chama SynthesizeTap
// e a thread do hook. É por isso que este arquivo não precisa de mutex
// nem de build tag: markerExtraInfo é uma constante, e IsOurs é uma
// comparação pura.
//
// markerExtraInfo é um valor arbitrário, mas deliberadamente
// reconhecível e improvável de colidir com o que qualquer outro processo
// (ex. AutoHotkey) usaria por acaso em DwExtraInfo.
const markerExtraInfo uintptr = 0x4155524f52415741 // "AURORAWA" em ASCII, truncado a 8 bytes

// IsOurs reporta se extraInfo é a marca que este processo embute em todo
// SendInput emitido por SynthesizeTap. Só é significativo quando o
// próprio evento já é sabido injetado (KBDLLHOOKSTRUCT.Injected()) —
// ver Translator.Translate, que combina os dois para preencher
// dictation.RawKeyEvent.FromThisProcess.
func IsOurs(extraInfo uintptr) bool {
	return extraInfo == markerExtraInfo
}
