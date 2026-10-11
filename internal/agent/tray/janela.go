package tray

// Mensagens que decidem se a janela oculta da bandeja fecha. Moram fora
// de tray_windows.go, sem build tag, para a decisão ser testável no
// runner Linux. Valores de winuser.h; nenhum inventado.
const (
	wmClose      = 0x0010
	wmSysCommand = 0x0112
	scClose      = 0xF060
)

// ehAltF4 reporta se a mensagem é o pedido de fechar que o próprio
// Windows gera quando a janela ativa recebe Alt+F4: WM_SYSCOMMAND com
// SC_CLOSE, que o DefWindowProc transforma em WM_CLOSE.
//
// A janela da bandeja fica ativa depois do menu (mostrarMenu faz
// SetForegroundWindow, e nada devolve o foco), então o Alt+F4 de quem
// acha que está fechando a janela onde estava chega nela. Repassado ao
// DefWindowProc, virava WM_CLOSE e encerrava o Agent sem aviso (revisão
// de 29-set-2026).
//
// Os quatro bits de baixo do wParam são de uso interno do Windows
// (documentação do WM_SYSCOMMAND): comparar sem a máscara deixaria
// passar um SC_CLOSE que chegasse com eles ligados.
func ehAltF4(msg uint32, wParam uintptr) bool {
	return msg == wmSysCommand && wParam&0xFFF0 == scClose
}
