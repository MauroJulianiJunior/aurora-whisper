//go:build windows

package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

// CreateCompatibleDC cria um memory device context compatível com hdc
// (hdc pode ser 0 para compatível com a tela).
func CreateCompatibleDC(hdc HDC) (HDC, error) {
	r1, _, err := procCreateCompatibleDC.Call(uintptr(hdc))
	if r1 == 0 {
		return 0, err
	}
	return HDC(r1), nil
}

// CreateDIBSection cria um bitmap com acesso direto aos bits (usado
// pelo overlay para desenhar o ARGB apresentado via
// UpdateLayeredWindow). Devolve o handle do bitmap e um ponteiro para o
// início dos bits, do tamanho de bmi.Header.Width * Height * 4 bytes
// para o caso de uso de 32 bpp deste pacote.
func CreateDIBSection(hdc HDC, bmi *BITMAPINFO, usage uint32) (HBITMAP, unsafe.Pointer, error) {
	var bits unsafe.Pointer
	r1, _, err := procCreateDIBSection.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(bmi)),
		uintptr(usage),
		uintptr(unsafe.Pointer(&bits)),
		0, // hSection: não usamos file mapping compartilhado
		0, // dwOffset
	)
	if r1 == 0 {
		return 0, nil, err
	}
	return HBITMAP(r1), bits, nil
}

// SelectObject seleciona obj em hdc, devolvendo o objeto previamente
// selecionado (para restaurar depois, se necessário).
func SelectObject(hdc HDC, obj HGDIOBJ) (HGDIOBJ, error) {
	r1, _, err := procSelectObject.Call(uintptr(hdc), uintptr(obj))
	if r1 == 0 {
		return 0, err
	}
	return HGDIOBJ(r1), nil
}

// DeleteObject libera um objeto GDI (ex.: o HBITMAP de CreateDIBSection).
// obj não deve estar mais selecionado em nenhum DC no momento da chamada.
func DeleteObject(obj HGDIOBJ) error {
	r1, _, err := procDeleteObject.Call(uintptr(obj))
	if r1 == 0 {
		return err
	}
	return nil
}

// DeleteDC libera um memory DC criado por CreateCompatibleDC.
func DeleteDC(hdc HDC) error {
	r1, _, err := procDeleteDC.Call(uintptr(hdc))
	if r1 == 0 {
		return err
	}
	return nil
}
