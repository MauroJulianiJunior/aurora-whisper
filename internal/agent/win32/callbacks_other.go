//go:build !windows

package win32

// Stubs dos trampolins de callback para GOOS != windows (ver doc.go). O
// retorno 0 nunca é um hookProc/WndProc válido para o Win32 real; estas
// funções só existem para manter o pacote compilável fora do Windows.

func NewLowLevelKeyboardHookProc(fn LowLevelKeyboardProc) uintptr {
	return 0
}

func NewWndProc(fn WndProc) uintptr {
	return 0
}
