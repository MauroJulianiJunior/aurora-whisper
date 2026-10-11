//go:build windows

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

var (
	procGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
	procMessageBoxW      = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
)

// Estilos de MessageBoxW (winuser.h).
const (
	mbIconError       = 0x10
	mbIconInformation = 0x40
	mbSetForeground   = 0x10000
)

// temConsole reporta se o Agent está ligado a um console.
//
// Compilado como aplicativo de bandeja (-H=windowsgui), não há console: é
// o modo de uso diário, e aí stderr é um handle inválido — escrever nele
// não vai a lugar nenhum. Rodado de um terminal durante o desenvolvimento,
// há. Duas decisões dependem disto: para onde vai o log, e se a bandeja
// pode faltar (ver run).
func temConsole() bool {
	h, _, _ := procGetConsoleWindow.Call()
	return h != 0
}

// avisar diz algo a quem abriu o Agent quando não há mais onde dizer.
//
// Com console, vai para ele. Sem console — o uso diário —, a única saída
// que a pessoa vê é uma janela: um erro fatal só no agent.log é um Agent
// que some sem explicação (revisão de 28-set-2026). Passa pelo portão
// como toda janela do Agent; sob go test nada aparece.
func avisar(texto string, ehErro bool) {
	if temConsole() {
		fmt.Fprintln(os.Stderr, texto)
		return
	}
	if sysinput.Guard() != nil {
		return
	}
	estilo := uintptr(mbIconInformation | mbSetForeground)
	if ehErro {
		estilo = mbIconError | mbSetForeground
	}
	msg, err1 := windows.UTF16PtrFromString(texto)
	titulo, err2 := windows.UTF16PtrFromString(appName)
	if err1 != nil || err2 != nil {
		return
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(titulo)), estilo)
}
