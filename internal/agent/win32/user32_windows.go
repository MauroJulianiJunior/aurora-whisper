//go:build windows

package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procShowWindow          = user32.NewProc("ShowWindow")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procSendInput           = user32.NewProc("SendInput")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
)

// SetWindowsHookExW instala um hook global ou de thread. hookProc deve
// ser o valor devolvido por NewLowLevelKeyboardHookProc (ou equivalente)
// — nunca um ponteiro de função Go cru, que não conforma à convenção de
// chamada stdcall que o Win32 exige do callback.
func SetWindowsHookExW(idHook int32, hookProc uintptr, hmod HINSTANCE, threadID uint32) (HHOOK, error) {
	r1, _, err := procSetWindowsHookExW.Call(uintptr(idHook), hookProc, uintptr(hmod), uintptr(threadID))
	if r1 == 0 {
		return 0, err
	}
	return HHOOK(r1), nil
}

// UnhookWindowsHookEx remove um hook instalado por SetWindowsHookExW.
func UnhookWindowsHookEx(hook HHOOK) error {
	r1, _, err := procUnhookWindowsHookEx.Call(uintptr(hook))
	if r1 == 0 {
		return err
	}
	return nil
}

// CallNextHookEx repassa o evento para o próximo hook da cadeia. O
// primeiro argumento é ignorado pelo Win32 desde o Windows XP, mas é
// mantido na assinatura por fidelidade à API.
func CallNextHookEx(hook HHOOK, nCode int32, wParam, lParam uintptr) uintptr {
	r1, _, _ := procCallNextHookEx.Call(uintptr(hook), uintptr(nCode), wParam, lParam)
	return r1
}

// GetMessageW bloqueia até a próxima mensagem da fila da thread
// corrente. Retorna (0, nil) em WM_QUIT, (-1, err) em erro, e
// (valor != 0, nil) para qualquer outra mensagem — a mesma convenção de
// retorno "BOOL de três estados" do Win32.
func GetMessageW(msg *MSG, hwnd HWND, msgFilterMin, msgFilterMax uint32) (int32, error) {
	r1, _, err := procGetMessageW.Call(
		uintptr(unsafe.Pointer(msg)),
		uintptr(hwnd),
		uintptr(msgFilterMin),
		uintptr(msgFilterMax),
	)
	ret := int32(r1)
	if ret == -1 {
		return -1, err
	}
	return ret, nil
}

// TranslateMessage traduz mensagens de tecla virtual em mensagens de
// caractere, enfileirando-as. Retorna se alguma tradução ocorreu.
func TranslateMessage(msg *MSG) bool {
	r1, _, _ := procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
	return r1 != 0
}

// DispatchMessageW envia msg ao window proc da janela alvo.
func DispatchMessageW(msg *MSG) uintptr {
	r1, _, _ := procDispatchMessageW.Call(uintptr(unsafe.Pointer(msg)))
	return r1
}

// PostThreadMessageW enfileira uma mensagem na fila de mensagens da
// thread identificada por threadID, sem esperar processamento. É o
// mecanismo usado para pedir a saída do loop GetMessageW de outra
// goroutine (tipicamente via WM_QUIT ou uma mensagem WM_APP+n própria).
func PostThreadMessageW(threadID uint32, msg uint32, wParam, lParam uintptr) error {
	r1, _, err := procPostThreadMessageW.Call(uintptr(threadID), uintptr(msg), wParam, lParam)
	if r1 == 0 {
		return err
	}
	return nil
}

// PostQuitMessage indica ao sistema que a thread pediu para terminar,
// fazendo um GetMessageW subsequente devolver 0 assim que a fila for
// esvaziada. Não tem valor de retorno a verificar (void no Win32).
func PostQuitMessage(exitCode int32) {
	procPostQuitMessage.Call(uintptr(exitCode))
}

// RegisterClassExW registra uma classe de janela e devolve seu atom.
func RegisterClassExW(wc *WNDCLASSEX) (uint16, error) {
	r1, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc)))
	if r1 == 0 {
		return 0, err
	}
	return uint16(r1), nil
}

// CreateWindowExW cria uma janela. className e windowName devem ser
// ponteiros UTF-16 terminados em zero (windows.UTF16PtrFromString).
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
	r1, _, err := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		uintptr(parent), uintptr(menu), uintptr(instance),
		uintptr(param),
	)
	if r1 == 0 {
		return 0, err
	}
	return HWND(r1), nil
}

// DestroyWindow destrói uma janela criada por CreateWindowExW.
func DestroyWindow(hwnd HWND) error {
	r1, _, err := procDestroyWindow.Call(uintptr(hwnd))
	if r1 == 0 {
		return err
	}
	return nil
}

// UpdateLayeredWindow apresenta o conteúdo de hdcSrc (tipicamente um
// memory DC selecionado com um DIB section ARGB) como o conteúdo da
// janela layered hwnd, com alpha por pixel via pblend. Ver ADR-010, D2.
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
	r1, _, err := procUpdateLayeredWindow.Call(
		uintptr(hwnd),
		uintptr(hdcDst),
		uintptr(unsafe.Pointer(pptDst)),
		uintptr(unsafe.Pointer(psize)),
		uintptr(hdcSrc),
		uintptr(unsafe.Pointer(pptSrc)),
		uintptr(crKey),
		uintptr(unsafe.Pointer(pblend)),
		uintptr(dwFlags),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

// SetWindowPos reposiciona e/ou redimensiona hwnd.
func SetWindowPos(hwnd, hwndInsertAfter HWND, x, y, cx, cy int32, flags uint32) error {
	r1, _, err := procSetWindowPos.Call(
		uintptr(hwnd), uintptr(hwndInsertAfter),
		uintptr(x), uintptr(y), uintptr(cx), uintptr(cy),
		uintptr(flags),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

// ShowWindow define o estado de exibição de hwnd. O retorno é a
// visibilidade *anterior* da janela (convenção do Win32, não um
// indicador de erro), por isso não há error aqui.
func ShowWindow(hwnd HWND, cmdShow int32) bool {
	r1, _, _ := procShowWindow.Call(uintptr(hwnd), uintptr(cmdShow))
	return r1 != 0
}

// GetSystemMetrics lê uma métrica de sistema (ex.: SMCxScreen).
func GetSystemMetrics(index int32) int32 {
	r1, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(r1)
}

// SendInput injeta os eventos sintéticos de inputs na fila de input do
// sistema. Devolve quantos eventos foram efetivamente injetados; um
// valor menor que len(inputs) indica falha parcial (por exemplo, outro
// processo bloqueou a injeção), e err traz o motivo do GetLastError.
func SendInput(inputs []INPUT) (uint32, error) {
	if len(inputs) == 0 {
		return 0, nil
	}
	r1, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	n := uint32(r1)
	if n != uint32(len(inputs)) {
		return n, err
	}
	return n, nil
}

// DefWindowProcW é o window proc padrão do Win32, para mensagens que um
// WndProc próprio não trata.
func DefWindowProcW(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr {
	r1, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r1
}
