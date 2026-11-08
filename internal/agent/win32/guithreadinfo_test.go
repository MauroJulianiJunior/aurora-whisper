package win32

import (
	"testing"
	"unsafe"
)

// TestGUITHREADINFO_Layout: GetGUIThreadInfo recusa a chamada se CbSize não
// for o tamanho da struct do Windows, e uma struct com os campos fora do
// lugar leria a bandeira errada. O layout é o do winuser.h: dois DWORD, seis
// HWND e um RECT, sem folga (72 bytes em 64 bits).
func TestGUITHREADINFO_Layout(t *testing.T) {
	var g GUITHREADINFO
	ptr := unsafe.Sizeof(HWND(0))
	if ptr != unsafe.Sizeof(uintptr(0)) {
		t.Fatalf("HWND com %d bytes, quero o tamanho de um ponteiro (%d)", ptr, unsafe.Sizeof(uintptr(0)))
	}
	if got, quer := unsafe.Sizeof(g), 8+6*ptr+16; got != quer {
		t.Fatalf("tamanho %d, quero %d", got, quer)
	}
	if got := unsafe.Offsetof(g.Flags); got != 4 {
		t.Fatalf("Flags no byte %d, quero 4", got)
	}
	if got, quer := unsafe.Offsetof(g.RcCaret), 8+6*ptr; got != quer {
		t.Fatalf("RcCaret no byte %d, quero %d", got, quer)
	}
}
