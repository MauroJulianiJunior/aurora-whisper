//go:build !windows

package overlay

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"

// newWindow fora do Windows sempre falha com ErrNotSupported. Existe só
// para que este pacote continue compilando em CI (ubuntu-latest) — ver
// CLAUDE.md, "compilação multiplataforma". A janela real está em
// window_windows.go.
func newWindow(x, y, width, height, supW, supH int32, proc win32.WndProc) (window, error) {
	return nil, ErrNotSupported
}
