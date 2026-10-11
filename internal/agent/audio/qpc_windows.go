//go:build windows

package audio

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// QueryPerformanceCounter/Frequency não têm wrapper em
// golang.org/x/sys/windows (v0.47), então são resolvidos como o hook
// resolve GetTickCount: NewLazySystemDLL/NewProc. Só LEEM o relógio — não
// agem no sistema.
var (
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procQueryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")
	procQueryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")

	// A frequência é fixa desde o boot (documentação de
	// QueryPerformanceFrequency): lida uma vez.
	frequenciaQPC = sync.OnceValue(func() int64 {
		var f int64
		procQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&f)))
		return f
	})
)

// agoraQPC é o instante atual em unidades de 100 ns do contador de
// performance — comparável direto com pu64QPCPosition (ver relogio.go).
func agoraQPC() int64 {
	var c int64
	procQueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&c)))
	return qpcPara100ns(c, frequenciaQPC())
}
