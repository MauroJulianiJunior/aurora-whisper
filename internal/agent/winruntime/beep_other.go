//go:build !windows

package winruntime

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"

// beep fora do Windows sempre falha com win32.ErrNotSupported. Existe só
// para que este pacote continue compilando em CI (ubuntu-latest) — ver
// CLAUDE.md, "compilação multiplataforma". O beep real está em
// beep_windows.go.
func beep() error {
	return win32.ErrNotSupported
}
