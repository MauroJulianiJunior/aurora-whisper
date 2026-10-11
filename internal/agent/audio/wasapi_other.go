//go:build !windows

package audio

import "context"

// openDevice em qualquer SO que não seja Windows sempre falha com
// ErrNotSupported. Existe só para que este pacote continue compilando em
// CI (ubuntu-latest) — ver AGENTS/CLAUDE.md, "compilação multiplataforma".
// A captura WASAPI real está em wasapi_windows.go.
func openDevice(ctx context.Context) (device, error) {
	return nil, ErrNotSupported
}
