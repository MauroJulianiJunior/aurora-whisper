// Package overlay implementa o overlay de estado do ditado descrito em
// docs/ai-spec/08-ui-ux-spec.md ("Overlay") e decidido em
// docs/adr/ADR-010-camada-nativa-windows.md, seção D2: uma janela Win32
// layered — WS_EX_LAYERED | WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW |
// WS_EX_TRANSPARENT, estilo WS_POPUP — desenhada em memory DC ARGB
// (CreateDIBSection) e apresentada via UpdateLayeredWindow, mostrando a
// fase atual de uma tentativa de ditado (dictation.OverlayPhase) mais a
// onda de amplitude ao vivo durante a gravação.
//
// # Por que cada flag de janela
//
//   - WS_EX_NOACTIVATE: o overlay nunca deve roubar o foco de teclado do
//     campo em que a pessoa está digitando (senão a inserção final iria
//     para o lugar errado).
//   - WS_EX_TRANSPARENT: cliques atravessam o overlay para a janela de
//     baixo — o overlay nunca deve bloquear a interação com o app real.
//   - WS_EX_TOOLWINDOW: o overlay não aparece no Alt+Tab nem na
//     taskbar — ele não é uma janela de aplicação.
//   - WS_EX_LAYERED + UpdateLayeredWindow (em vez de
//     SetLayeredWindowAttributes): alpha por pixel, necessário para
//     cantos arredondados sem serrilhado quadrado — SetLayeredWindowAttributes
//     sozinho só dá colorkey ou alpha constante para a janela inteira.
//
// Ninguém deve "limpar" essas flags sem reler esta lista: cada uma
// resolve um requisito específico da spec, não é enfeite.
//
// # Modelo de threads
//
// A janela nativa vive inteiramente numa goroutine dedicada, travada à
// sua thread do SO com runtime.LockOSThread(), rodando seu próprio loop
// GetMessageW/DispatchMessageW — mensagem de janela só chega na thread
// que a criou (ADR-010, "Modelo de threads"). SetPhase e PushAmplitude
// são chamados de qualquer outra goroutine (o runtime é o único
// consumidor serializado do dictation.Machine); eles só escrevem em
// campos protegidos por mutex e acordam a janela via PostMessageW, que é
// seguro entre threads por desenho do próprio Win32 — nenhuma goroutine
// além da que criou a janela toca HWND ou handle de GDI.
//
// # Divisão de arquivos
//
// A lógica de layout (posição na tela, tamanho da pílula, quantidade e
// altura das barras da onda a partir das amplitudes, quebra da mensagem)
// não tem build tag e é testada de verdade, inclusive no runner Linux do
// CI — ver layout.go e mensagem.go. O mesmo vale para o desenho (canvas
// ARGB e a IBM Plex Mono embutida, rasterizada em Go puro): render.go,
// canvas.go e tipografia.go escrevem pixels sem nenhuma chamada de
// sistema, e por isso também são testáveis fora do Windows — ver
// canvas_test.go e tipografia_test.go. A cópia do quadro para a
// superfície da janela também (superficie.go). Só a
// janela nativa em si (window_windows.go, atrás de window, a fronteira
// de plataforma) e o que ela precisa do Win32 além do que o pacote
// win32 já expõe (window_windows.go escreve seus próprios bindings
// finos de PostMessageW/GetModuleHandleW, no mesmo espírito de
// internal/agent/audio/wasapi_windows.go para mmdevapi) ficam atrás de
// //go:build windows, com o stub correspondente em window_other.go.
package overlay
