package tray

import "testing"

// TestEhAltF4: só o SC_CLOSE, com ou sem os bits internos do Windows, é
// engolido. O WM_CLOSE postado de fora e os outros comandos de sistema
// seguem o caminho de sempre.
func TestEhAltF4(t *testing.T) {
	casos := []struct {
		nome   string
		msg    uint32
		wParam uintptr
		quer   bool
	}{
		{"Alt+F4", wmSysCommand, scClose, true},
		{"Alt+F4 com os bits internos ligados", wmSysCommand, scClose | 0x2, true},
		{"WM_CLOSE de fora continua indo para Sair", wmClose, 0, false},
		{"SC_MINIMIZE não é fechar", wmSysCommand, 0xF020, false},
		{"SC_CLOSE em outra mensagem", wmClose, scClose, false},
	}
	for _, c := range casos {
		if got := ehAltF4(c.msg, c.wParam); got != c.quer {
			t.Errorf("%s: ehAltF4(0x%X, 0x%X) = %v, quer %v", c.nome, c.msg, c.wParam, got, c.quer)
		}
	}
}
