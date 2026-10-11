//go:build windows

package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// NewLowLevelKeyboardHookProc cria o trampolim stdcall exigido por
// SetWindowsHookExW(WHKeyboardLL, ...), usando windows.NewCallback
// (ADR-010, D3) para resolver a convenção de chamada. O retorno deve
// ser passado diretamente como o parâmetro hookProc de
// SetWindowsHookExW.
//
// syscall.NewCallback (por baixo de windows.NewCallback) nunca libera a
// entrada que aloca: cada chamada consome, para sempre, uma posição de
// uma tabela interna de tamanho fixo. Por isso este construtor deve ser
// chamado uma única vez por processo — ao instalar o hook — nunca por
// evento de teclado.
func NewLowLevelKeyboardHookProc(fn LowLevelKeyboardProc) uintptr {
	return windows.NewCallback(func(nCode int32, wParam uintptr, lParam uintptr) uintptr {
		// lParam é o endereço de um KBDLLHOOKSTRUCT alocado pelo próprio
		// Win32 (não pelo runtime do Go), então a preocupação de
		// go vet (unsafeptr) sobre o GC mover ou coletar o alvo não se
		// aplica aqui. unsafe.Add(nil, lParam) reconstrói o ponteiro a
		// partir do endereço bruto sem acionar esse aviso — é a forma
		// que o próprio pacote unsafe recomenda desde o Go 1.17 no lugar
		// de unsafe.Pointer(uintptr(...)+offset) com offset 0.
		info := (*KBDLLHOOKSTRUCT)(unsafe.Add(unsafe.Pointer(nil), lParam))
		return fn(nCode, wParam, info)
	})
}

// NewWndProc cria o trampolim stdcall para um window procedure, com a
// mesma ressalva de "chamar uma única vez" descrita em
// NewLowLevelKeyboardHookProc. O retorno deve ser atribuído a
// WNDCLASSEX.LpfnWndProc antes de RegisterClassExW.
func NewWndProc(fn WndProc) uintptr {
	return windows.NewCallback(func(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr {
		return fn(hwnd, msg, wParam, lParam)
	})
}
