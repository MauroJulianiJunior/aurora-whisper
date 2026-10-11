//go:build !windows

package win32

import "unsafe"

// Stubs de user32.dll para GOOS != windows (ver doc.go). Nenhuma destas
// funções faz qualquer coisa além de devolver ErrNotSupported (ou um
// valor zero, quando a assinatura real não tem retorno de erro) — elas
// existem só para que pacotes que importam win32 continuem compilando e
// com testes executáveis no runner Linux do CI.

func SetWindowsHookExW(idHook int32, hookProc uintptr, hmod HINSTANCE, threadID uint32) (HHOOK, error) {
	return 0, ErrNotSupported
}

func UnhookWindowsHookEx(hook HHOOK) error {
	return ErrNotSupported
}

func CallNextHookEx(hook HHOOK, nCode int32, wParam, lParam uintptr) uintptr {
	return 0
}

func GetMessageW(msg *MSG, hwnd HWND, msgFilterMin, msgFilterMax uint32) (int32, error) {
	return -1, ErrNotSupported
}

func TranslateMessage(msg *MSG) bool {
	return false
}

func DispatchMessageW(msg *MSG) uintptr {
	return 0
}

func PostThreadMessageW(threadID uint32, msg uint32, wParam, lParam uintptr) error {
	return ErrNotSupported
}

func PostQuitMessage(exitCode int32) {}

func RegisterClassExW(wc *WNDCLASSEX) (uint16, error) {
	return 0, ErrNotSupported
}

func CreateWindowExW(
	exStyle uint32,
	className, windowName *uint16,
	style uint32,
	x, y, width, height int32,
	parent HWND,
	menu HMENU,
	instance HINSTANCE,
	param unsafe.Pointer,
) (HWND, error) {
	return 0, ErrNotSupported
}

func DestroyWindow(hwnd HWND) error {
	return ErrNotSupported
}

func UpdateLayeredWindow(
	hwnd HWND,
	hdcDst HDC,
	pptDst *POINT,
	psize *SIZE,
	hdcSrc HDC,
	pptSrc *POINT,
	crKey uint32,
	pblend *BLENDFUNCTION,
	dwFlags uint32,
) error {
	return ErrNotSupported
}

func SetWindowPos(hwnd, hwndInsertAfter HWND, x, y, cx, cy int32, flags uint32) error {
	return ErrNotSupported
}

func ShowWindow(hwnd HWND, cmdShow int32) bool {
	return false
}

func GetSystemMetrics(index int32) int32 {
	return 0
}

func SendInput(inputs []INPUT) (uint32, error) {
	if len(inputs) == 0 {
		return 0, nil
	}
	return 0, ErrNotSupported
}

func DefWindowProcW(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr {
	return 0
}
