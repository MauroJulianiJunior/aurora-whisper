//go:build !windows

package win32

import "unsafe"

// Stubs de gdi32.dll para GOOS != windows (ver doc.go).

func CreateCompatibleDC(hdc HDC) (HDC, error) {
	return 0, ErrNotSupported
}

func CreateDIBSection(hdc HDC, bmi *BITMAPINFO, usage uint32) (HBITMAP, unsafe.Pointer, error) {
	return 0, nil, ErrNotSupported
}

func SelectObject(hdc HDC, obj HGDIOBJ) (HGDIOBJ, error) {
	return 0, ErrNotSupported
}

func DeleteObject(obj HGDIOBJ) error {
	return ErrNotSupported
}

func DeleteDC(hdc HDC) error {
	return ErrNotSupported
}
