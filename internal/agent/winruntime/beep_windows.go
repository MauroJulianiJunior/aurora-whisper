//go:build windows

package winruntime

import "golang.org/x/sys/windows"

// kernel32!Beep gera um tom simples e curto pelo dispositivo de áudio
// padrão do sistema — mais previsível e discreto que MessageBeep (que
// depende do esquema de sons configurado pelo usuário, podendo nem tocar
// nada se "Sem sons" estiver selecionado). Sem wrapper em
// golang.org/x/sys/windows nem em internal/agent/win32 (que só cobre
// user32/gdi32 — ver seu doc.go), então é resolvido aqui do mesmo jeito
// que internal/agent/hook resolve kernel32!GetTickCount:
// NewLazySystemDLL/NewProc, sem reimplementar a fiação de convenção de
// chamada por cima da stdlib (ADR-010, D3).
var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	procBeep = kernel32.NewProc("Beep")
)

// beepFreqHz e beepDurationMs escolhem um tom curto (AC-006: "só quando
// habilitado na config") — uma confirmação discreta de que a gravação
// começou, não uma notificação de sistema chamativa.
const (
	beepFreqHz     = 880
	beepDurationMs = 90
)

// beep toca o tom de confirmação de PlayBeepEffect (AC-006). Best-effort:
// PlayBeepEffect nunca espera resposta do núcleo (effects.go), então uma
// falha aqui só é logada pelo chamador (runtime.go), nunca propagada ao
// Machine.
func beep() error {
	r1, _, err := procBeep.Call(uintptr(beepFreqHz), uintptr(beepDurationMs))
	if r1 == 0 {
		return err
	}
	return nil
}
