// Package hook instala e opera o hook global de teclado (WH_KEYBOARD_LL)
// do Windows Agent (ADR-010, escopo do Sprint 1) e alimenta o Decider
// puro que já existe em internal/agent/dictation (ADR-007, seção 2).
//
// O pacote resolve três problemas, cada um em sua própria peça:
//
//   - keymap.go traduz códigos de tecla virtual (VK_*) do Win32 para
//     dictation.KeyCode e de volta (para sintetizar o tap via SendInput).
//     Dado puro, sem build tag.
//   - marker.go define como reconhecer, de forma síncrona e sem estado
//     compartilhado, um evento que fomos NÓS quem injetamos via SendInput
//     (ADR-007, seção 3). Puro, sem build tag.
//   - translate.go traduz um evento cru do hook (VK, down/up, injected,
//     extra info, instante) em dictation.RawKeyEvent, mantendo o estado
//     mínimo de quais modificadores estão pressionados. Puro, sem build
//     tag — testável sem Windows.
//   - hook.go define Hook, o tipo que amarra tudo isso; seus campos não
//     dependem de nenhuma chamada de sistema (win32.HHOOK e
//     win32.TickReference são só memória). Sem build tag.
//   - hook_windows.go implementa Start/Stop/SynthesizeTap de verdade:
//     instala o hook numa thread própria travada com
//     runtime.LockOSThread(), com o loop GetMessage/DispatchMessage que o
//     WH_KEYBOARD_LL exige (ADR-010, "Modelo de threads"), e sintetiza o
//     tap via SendInput(KEYEVENTF_UNICODE), marcando o evento como nosso
//     antes de injetar.
//   - hook_other.go stub para GOOS != windows, devolvendo
//     win32.ErrNotSupported — para que o pacote continue compilando e
//     testável (a parte pura) no runner Linux do CI.
//
// O ponto que mais importa deste pacote inteiro: o callback do hook roda
// numa thread do sistema com orçamento de ~300ms (LowLevelHooksTimeout).
// Só traduz o evento, chama Decider.Decide (puro) e manda o resultado por
// canal de forma não bloqueante (select/default). Nunca faz I/O, nunca
// aloca pesado, nunca espera lock, nunca chama UI Automation. Estourar o
// orçamento faz o Windows remover o hook silenciosamente — o ditado para
// de funcionar sem nenhum erro visível.
package hook
