//go:build !windows

package hook

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"

// Stubs para GOOS != windows (ver doc.go deste pacote e o doc.go de
// internal/agent/win32). Nenhuma destas funções instala hook nem injeta
// input de verdade — elas existem só para que este pacote, e tudo que o
// importa, continue compilando e com a parte pura testável (translate.go,
// marker.go, keymap.go) executável no runner Linux do CI.

// Start sempre devolve win32.ErrNotSupported fora do Windows.
func (h *Hook) Start() error {
	return win32.ErrNotSupported
}

// Stop é um no-op fora do Windows: não há hook instalado para encerrar.
func (h *Hook) Stop() error {
	return nil
}

// SynthesizeTap sempre devolve win32.ErrNotSupported fora do Windows.
func (h *Hook) SynthesizeTap() error {
	return win32.ErrNotSupported
}
