//go:build windows

package overlay

// Implementação real da janela nativa do overlay (ADR-010, D2). Só
// escreve o mínimo de Win32 que o pacote win32 (internal/agent/win32)
// não expõe — PostMessageW (para window.Wake/RequestQuit acordarem o
// GetMessageW bloqueado de outra thread) e GetModuleHandleW (HINSTANCE
// da janela) — no mesmo espírito de
// internal/agent/audio/wasapi_windows.go escrever seus próprios
// bindings de mmdevapi em vez de fazer crescer win32 para um único
// chamador. Tudo o mais (RegisterClassExW, CreateWindowExW,
// CreateCompatibleDC, CreateDIBSection, SelectObject,
// UpdateLayeredWindow, ShowWindow, SetWindowPos, DestroyWindow,
// DeleteObject, DeleteDC) vem de win32.
//
// A pílula é desenhada inteiramente em Go puro sobre os bits do DIB
// (canvas.go, tipografia.go, render.go) — nenhuma chamada de GDI de desenho
// (brush, pen, TextOut) é necessária, só a criação da superfície.

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

const (
	className = "AuroraWhisperOverlay"

	// DIB_RGB_COLORS (wingdi.h): bmiColors de CreateDIBSection contém
	// valores RGB literais, não índices de paleta — irrelevante aqui
	// (32bpp true color, sem paleta), mas é o valor correto a passar.
	dibRGBColors = 0
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procPostMessageW     = user32.NewProc("PostMessageW")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

// postMessageW enfileira uma mensagem na fila de hwnd e retorna sem
// esperar processamento — ao contrário de PostThreadMessageW (que exige
// saber o id da thread), o Win32 documenta PostMessageW como seguro de
// chamar de qualquer thread, o que é exatamente o que window.Wake e
// window.RequestQuit precisam.
func postMessageW(hwnd win32.HWND, msg uint32, wParam, lParam uintptr) error {
	r1, _, err := procPostMessageW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	if r1 == 0 {
		return err
	}
	return nil
}

func getModuleHandle() win32.HINSTANCE {
	r1, _, _ := procGetModuleHandleW.Call(0)
	return win32.HINSTANCE(r1)
}

// created garante que newWindow só seja bem-sucedido uma vez por
// processo. Não é uma limitação arbitrária: win32.NewWndProc (por baixo,
// windows.NewCallback) consome, para sempre, uma posição de uma tabela
// de trampolins de tamanho fixo do runtime — e RegisterClassExW só
// aceita uma classe com este nome por processo. Overlay é um singleton
// do Agent (um processo, um overlay); uma segunda chamada é um bug de
// quem está usando este pacote, não um caso a suportar.
var created int32

// newWindow cria a janela em (x, y) com width x height, e a superfície de
// desenho com supW x supH — o tamanho da maior pílula
// (TamanhoDaSuperficie). A janela muda de tamanho a cada fase; a
// superfície, nunca.
func newWindow(x, y, width, height, supW, supH int32, proc win32.WndProc) (window, error) {
	if !atomic.CompareAndSwapInt32(&created, 0, 1) {
		return nil, fmt.Errorf("overlay: newWindow já foi chamado neste processo (Overlay é singleton)")
	}

	namePtr, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return nil, fmt.Errorf("overlay: nome de classe inválido: %w", err)
	}

	instance := getModuleHandle()
	wc := win32.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win32.WNDCLASSEX{})),
		LpfnWndProc:   win32.NewWndProc(proc),
		HInstance:     instance,
		LpszClassName: namePtr,
	}
	if _, err := win32.RegisterClassExW(&wc); err != nil {
		return nil, fmt.Errorf("overlay: RegisterClassExW: %w", err)
	}

	// overlayExStyle mora em style.go, fora da build tag, para ser
	// testável na suíte normal — ver o comentário de lá.
	hwnd, err := win32.CreateWindowExW(
		overlayExStyle, namePtr, nil, win32.WSPopup,
		x, y, width, height,
		0, 0, instance, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("overlay: CreateWindowExW: %w", err)
	}

	w, err := newSurface(hwnd, supW, supH)
	if err != nil {
		win32.DestroyWindow(hwnd)
		return nil, err
	}
	return w, nil
}

// nativeWindow é a implementação real de window: um HWND layered, com
// um memory DC contendo um DIB ARGB — a superfície — do tamanho da maior
// pílula, em cujo canto cada quadro é copiado (Draw).
type nativeWindow struct {
	hwnd    win32.HWND
	hdcDst  win32.HDC // DC de tela, exigido por UpdateLayeredWindow (só para casamento de paleta; não é onde desenhamos)
	hdcSrc  win32.HDC // memory DC com o DIB selecionado
	hbitmap win32.HBITMAP
	prevObj win32.HGDIOBJ
	canvas  *Canvas // os bits do DIB: a superfície inteira, não a janela
}

func newSurface(hwnd win32.HWND, width, height int32) (*nativeWindow, error) {
	hdcDst, err := win32.CreateCompatibleDC(0)
	if err != nil {
		return nil, fmt.Errorf("overlay: CreateCompatibleDC (tela): %w", err)
	}
	hdcSrc, err := win32.CreateCompatibleDC(0)
	if err != nil {
		win32.DeleteDC(hdcDst)
		return nil, fmt.Errorf("overlay: CreateCompatibleDC (memória): %w", err)
	}

	bmi := win32.BITMAPINFO{Header: win32.BITMAPINFOHEADER{
		Size:        uint32(unsafe.Sizeof(win32.BITMAPINFOHEADER{})),
		Width:       width,
		Height:      -height, // negativo = DIB top-down: linha 0 é o topo, sem inverter Y ao desenhar
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
	}}
	hbitmap, bits, err := win32.CreateDIBSection(hdcSrc, &bmi, dibRGBColors)
	if err != nil {
		win32.DeleteDC(hdcSrc)
		win32.DeleteDC(hdcDst)
		return nil, fmt.Errorf("overlay: CreateDIBSection: %w", err)
	}
	prevObj, err := win32.SelectObject(hdcSrc, win32.HGDIOBJ(hbitmap))
	if err != nil {
		win32.DeleteObject(win32.HGDIOBJ(hbitmap))
		win32.DeleteDC(hdcSrc)
		win32.DeleteDC(hdcDst)
		return nil, fmt.Errorf("overlay: SelectObject: %w", err)
	}

	pixLen := int(width) * int(height) * 4
	canvas := &Canvas{
		W:   int(width),
		H:   int(height),
		Pix: unsafe.Slice((*byte)(bits), pixLen),
	}

	return &nativeWindow{
		hwnd: hwnd, hdcDst: hdcDst, hdcSrc: hdcSrc,
		hbitmap: hbitmap, prevObj: prevObj,
		canvas: canvas,
	}, nil
}

// Draw copia c para o canto da superfície (copiaParaSuperficie) e
// apresenta via UpdateLayeredWindow com alpha por pixel (ACSrcAlpha) — é
// isto que SetLayeredWindowAttributes não conseguiria fazer (doc.go).
//
// A mesma chamada leva a posição (pptDst) e o tamanho (psize) novos da
// janela: o Windows troca conteúdo, lugar e tamanho de uma vez, no mesmo
// quadro da composição. Não há SetWindowPos que mude posição ou tamanho
// antes nem depois (o de SetVisible só reafirma o topo, com NoMove e
// NoSize), então não existe o instante em que a janela já mudou de tamanho
// e ainda mostra a pílula anterior — nem o inverso. E como psize é exatamente c.W x c.H a
// partir do canto (pptSrc 0,0), o que a superfície guarda de uma pílula
// anterior maior, fora desse retângulo, nunca é apresentado.
func (w *nativeWindow) Draw(c *Canvas, x, y int32) error {
	if err := copiaParaSuperficie(w.canvas.Pix, w.canvas.W, w.canvas.H, c); err != nil {
		return err
	}

	dst := win32.POINT{X: x, Y: y}
	size := win32.SIZE{CX: int32(c.W), CY: int32(c.H)}
	srcPt := win32.POINT{}
	blend := win32.BLENDFUNCTION{
		BlendOp:             win32.ACSrcOver,
		SourceConstantAlpha: 255,
		AlphaFormat:         win32.ACSrcAlpha,
	}
	return win32.UpdateLayeredWindow(w.hwnd, w.hdcDst, &dst, &size, w.hdcSrc, &srcPt, 0, &blend, win32.ULWAlpha)
}

// SetVisible mostra ou esconde a pílula.
//
// Mostrar é a decisão de comoMostrar (style.go, pura e testada): ShowWindow
// sem ativar e, em seguida, SetWindowPos(HWND_TOPMOST) para reafirmar o topo
// contra uma janela topmost que tenha subido depois, também sem ativar e sem
// mexer em posição nem em tamanho. NUNCA ativa a janela nem move o foco: o
// ditado insere no campo que o tem. Uma trava na árvore sintática deste
// arquivo (style_test.go) confere que as duas chamadas usam o que
// comoMostrar devolve, e que nada aqui ativa a janela.
//
// O erro devolvido é o do SetWindowPos; a pílula já foi mostrada quando ele
// vem, só pode estar atrás de outra topmost. Quem chama (redraw) o ignora, e
// um log por quadro afogaria o do Agent.
func (w *nativeWindow) SetVisible(visible bool) error {
	if !visible {
		win32.ShowWindow(w.hwnd, swHide)
		return nil
	}
	m := comoMostrar()
	win32.ShowWindow(w.hwnd, m.CmdShow)
	return win32.SetWindowPos(w.hwnd, m.InsertAfter, 0, 0, 0, 0, m.Flags)
}

func (w *nativeWindow) Wake() error        { return postMessageW(w.hwnd, msgUpdate, 0, 0) }
func (w *nativeWindow) RequestQuit() error { return postMessageW(w.hwnd, msgQuit, 0, 0) }

func (w *nativeWindow) Close() error {
	win32.SelectObject(w.hdcSrc, w.prevObj)
	win32.DeleteObject(win32.HGDIOBJ(w.hbitmap))
	win32.DeleteDC(w.hdcSrc)
	win32.DeleteDC(w.hdcDst)
	return win32.DestroyWindow(w.hwnd)
}
