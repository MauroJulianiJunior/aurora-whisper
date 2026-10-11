package win32

// Este arquivo não tem build tag: é apenas memória e constantes, sem
// nenhuma chamada de sistema, então compila e é utilizável (para
// montar/inspecionar valores em teste) em qualquer GOOS. O layout de
// cada struct espelha exatamente o correspondente Win32 em Windows
// x64 — a mesma plataforma alvo do produto (CLAUDE.md, "Windows 10/11
// x64") — incluindo o padding que o compilador C insere para alinhar
// campos de 8 bytes (ponteiros/uintptr). O alinhamento automático do Go
// para tipos de tamanho equivalente reproduz esse padding sem precisar
// de campos "_" explícitos, exceto onde comentado.

// Handle é o tipo base de todo handle Win32 (HWND, HDC, HBITMAP, ...):
// um valor opaco do tamanho de um ponteiro. Os tipos nomeados abaixo
// existem só para deixar as assinaturas de função autoexplicativas —
// não têm métodos nem comportamento.
type Handle uintptr

type (
	HWND      Handle // handle de janela
	HDC       Handle // handle de device context
	HGDIOBJ   Handle // handle de objeto GDI genérico (retorno de SelectObject)
	HBITMAP   Handle // handle de bitmap (subtipo de HGDIOBJ)
	HHOOK     Handle // handle de hook instalado por SetWindowsHookExW
	HINSTANCE Handle // handle de instância do módulo
	HMENU     Handle // handle de menu de janela
	HICON     Handle // handle de ícone
	HCURSOR   Handle // handle de cursor
	HBRUSH    Handle // handle de brush (subtipo de HGDIOBJ)
)

// POINT espelha a struct Win32 POINT (8 bytes).
type POINT struct {
	X int32
	Y int32
}

// SIZE espelha a struct Win32 SIZE (8 bytes).
type SIZE struct {
	CX int32
	CY int32
}

// RECT espelha a struct Win32 RECT (16 bytes).
type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// MSG espelha a struct Win32 MSG usada por GetMessageW/TranslateMessage/
// DispatchMessageW. O compilador insere 4 bytes de padding antes de
// WParam para alinhá-lo a 8 bytes, exatamente como o cabeçalho Win32.
type MSG struct {
	HWnd    HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// WNDCLASSEX espelha a struct Win32 WNDCLASSEXW usada por
// RegisterClassExW. LpfnWndProc recebe o valor devolvido por
// NewWndProc. LpszMenuName/LpszClassName são ponteiros UTF-16
// (windows.StringToUTF16Ptr ou equivalente) — nil é válido para
// LpszMenuName.
type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     HINSTANCE
	HIcon         HICON
	HCursor       HCURSOR
	HbrBackground HBRUSH
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       HICON
}

// BLENDFUNCTION espelha a struct Win32 BLENDFUNCTION usada por
// UpdateLayeredWindow (4 bytes, um por campo, sem padding).
type BLENDFUNCTION struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

// KEYBDINPUT espelha a struct Win32 KEYBDINPUT, a variante do union
// INPUT usada para injeção de teclado via SendInput.
type KEYBDINPUT struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

// INPUT espelha a struct Win32 INPUT already fixada na variante de
// teclado. O union real (MOUSEINPUT | KEYBDINPUT | HARDWAREINPUT) tem
// 32 bytes em Windows x64 (o tamanho de MOUSEINPUT, a maior variante);
// como só usamos a variante de teclado (24 bytes), o campo pad
// completa os 8 bytes restantes para que unsafe.Sizeof(INPUT{}) bata
// exatamente com o sizeof(INPUT) que SendInput valida via cbSize —
// se não bater, SendInput falha silenciosamente (devolve 0 injetados).
type INPUT struct {
	Type uint32
	Ki   KEYBDINPUT
	pad  [8]byte
}

// BITMAPINFOHEADER espelha a struct Win32 BITMAPINFOHEADER (40 bytes).
type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// BITMAPINFO espelha a struct Win32 BITMAPINFO para o caso de uso do
// overlay: DIB de 32 bpp sem paleta (BI_RGB), em que bmiColors não é
// consultado por CreateDIBSection — o único elemento existe apenas
// para casar o layout de memória com o header do Win32.
type BITMAPINFO struct {
	Header BITMAPINFOHEADER
	Colors [1]uint32
}

// KBDLLHOOKSTRUCT espelha a struct Win32 KBDLLHOOKSTRUCT entregue como
// lParam de um hook WH_KEYBOARD_LL.
//
// Fronteira deliberada (ADR-007 seção 3, ADR-010 ponto 1 do sprint):
// Flags só diz "este evento foi injetado" (LLKHFInjected) e, dentro
// disso, "por um processo de integridade mais baixa" (Lower Integrity
// Level) (LLKHFLowerILInjected). A struct não sabe — e não pode saber —
// se o injetor fomos nós (Aurora Whisper via SynthesizeTapEffect) ou
// outro processo (ex.: AutoHotkey). Só a camada do hook sabe o que ela
// mesma acabou de injetar (ex.: comparando contra um marcador em
// DwExtraInfo, ou contra uma janela de tempo/fila de eventos pendentes
// de auto-injeção); é ela quem deve preencher
// dictation.RawKeyEvent.FromThisProcess. Este pacote só expõe o bit
// bruto do Win32, nunca decide autoria.
type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// Injected reporta o bit LLKHFInjected: o evento veio de SendInput, de
// algum processo — não necessariamente este. Ver a fronteira descrita
// no comentário de KBDLLHOOKSTRUCT.
func (k *KBDLLHOOKSTRUCT) Injected() bool {
	return k.Flags&LLKHFInjected != 0
}

// LowerILInjected reporta o bit LLKHFLowerILInjected: o evento foi
// injetado por um processo de integridade mais baixa que o receptor do
// hook. Ver a fronteira descrita no comentário de KBDLLHOOKSTRUCT.
func (k *KBDLLHOOKSTRUCT) LowerILInjected() bool {
	return k.Flags&LLKHFLowerILInjected != 0
}

// LowLevelKeyboardProc é a assinatura Go do callback de um hook
// WH_KEYBOARD_LL, já com lParam decodificado como *KBDLLHOOKSTRUCT.
// nCode e wParam mantêm o significado bruto do Win32: quando nCode < 0,
// o callback deve apenas repassar para CallNextHookEx sem inspecionar
// info (regra do próprio Win32, não deste pacote). O retorno != 0
// suprime o evento para o resto do sistema.
type LowLevelKeyboardProc func(nCode int32, wParam uintptr, info *KBDLLHOOKSTRUCT) uintptr

// WndProc é a assinatura Go de um window procedure (o valor atribuído a
// WNDCLASSEX.LpfnWndProc através de NewWndProc).
type WndProc func(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr

// Constantes Win32 usadas pelo hook de teclado e pelo overlay. Os
// valores são os publicados pela documentação do Win32 (winuser.h);
// nenhum é inventado por este pacote.
const (
	// WHKeyboardLL é o idHook de um hook de teclado de baixo nível
	// (WH_KEYBOARD_LL), instalado via SetWindowsHookExW.
	WHKeyboardLL int32 = 13

	// Mensagens de teclado entregues a um hook WH_KEYBOARD_LL.
	WMKeyDown    uint32 = 0x0100
	WMKeyUp      uint32 = 0x0101
	WMSysKeyDown uint32 = 0x0104
	WMSysKeyUp   uint32 = 0x0105

	// Bits de KBDLLHOOKSTRUCT.Flags. Ver o comentário de KBDLLHOOKSTRUCT
	// para a fronteira entre "injetado" e "injetado por nós".
	LLKHFLowerILInjected uint32 = 0x00000002
	LLKHFInjected        uint32 = 0x00000010

	// Estilos estendidos de janela usados pelo overlay (ADR-010, D2):
	// layered (alpha por pixel via UpdateLayeredWindow) + noactivate
	// (não rouba foco) + transparent (clique passa para a janela de
	// baixo) + toolwindow (não aparece em Alt+Tab nem na taskbar).
	WSExLayered     uint32 = 0x00080000
	WSExTransparent uint32 = 0x00000020
	WSExToolWindow  uint32 = 0x00000080
	WSExNoActivate  uint32 = 0x08000000

	// WSExTopmost (WS_EX_TOPMOST, winuser.h) mantém a janela acima de
	// todas as não-topmost, inclusive da que tem o foco.
	//
	// Sem ele, uma janela com WSExNoActivate + SW_SHOWNOACTIVATE nunca
	// sobe na pilha: ela é desenhada e mostrada, e fica ATRÁS da janela
	// em foco. Para um overlay de status isso é o pior caso possível,
	// porque a janela em foco é justamente onde a pessoa está olhando.
	WSExTopmost uint32 = 0x00000008

	// WSPopup é o estilo de janela sem moldura usado pelo overlay.
	WSPopup uint32 = 0x80000000

	// Flags de SetWindowPos (winuser.h) que o overlay usa para reafirmar a
	// posição no topo sem mexer em mais nada (overlay/style.go, comoMostrar).
	//
	// SWPNoZOrder ficou de fora de propósito: com ele, SetWindowPos ignora
	// o hwndInsertAfter, e "reafirmar o topo" viraria uma chamada que não
	// faz nada.
	SWPNoSize        uint32 = 0x0001 // mantém o tamanho (cx e cy são ignorados)
	SWPNoMove        uint32 = 0x0002 // mantém a posição (x e y são ignorados)
	SWPNoZOrder      uint32 = 0x0004 // mantém a ordem na pilha (hwndInsertAfter é ignorado)
	SWPNoActivate    uint32 = 0x0010 // NÃO ativa a janela: não rouba o foco do teclado
	SWPShowWindow    uint32 = 0x0040 // mostra a janela
	SWPHideWindow    uint32 = 0x0080 // esconde a janela
	SWPNoOwnerZOrder uint32 = 0x0200 // não mexe na ordem da dona (a pílula não tem dona)

	// HWNDTopmost é o HWND_TOPMOST de winuser.h, ((HWND)-1): como
	// hwndInsertAfter, põe a janela acima de todas as que não são topmost e
	// no alto das que são. Não é um handle de janela de verdade.
	HWNDTopmost HWND = ^HWND(0)

	// ULWAlpha é a flag de UpdateLayeredWindow para composição por
	// canal alfa (necessária junto de BLENDFUNCTION.AlphaFormat =
	// ACSrcAlpha para alpha por pixel, em vez de colorkey).
	ULWAlpha uint32 = 0x00000002

	// Valores de BLENDFUNCTION para alpha por pixel pré-multiplicado.
	ACSrcOver  byte = 0x00
	ACSrcAlpha byte = 0x01

	// Flags de KEYBDINPUT.DwFlags usadas pela inserção mínima via
	// SendInput (ADR-010, escopo do Sprint 1): KeyEventFUnicode injeta
	// um caractere Unicode (WVk ignorado, WScan é o code point) em vez
	// de uma tecla virtual; KeyEventFKeyUp marca o evento como keyup
	// (ausente, é keydown).
	KeyEventFKeyUp   uint32 = 0x0002
	KeyEventFUnicode uint32 = 0x0004

	// InputKeyboard é o INPUT.Type para eventos de teclado em SendInput.
	InputKeyboard uint32 = 1

	// Índices de GetSystemMetrics usados para dimensionar o overlay à
	// tela.
	SMCxScreen int32 = 0
	SMCyScreen int32 = 1
)
