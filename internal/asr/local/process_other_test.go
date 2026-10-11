//go:build !windows

package local

import (
	"context"
	"errors"
	"testing"
)

// TestNewPlatformProcess_ForaDoWindows trava o contrato: fora do Windows
// não existe whisper-server para subir, e newPlatformProcess nunca deve
// fingir que subiu algo. Roda no CI do Linux (docs/CLAUDE.md).
func TestNewPlatformProcess_ForaDoWindows(t *testing.T) {
	proc := newPlatformProcess(Config{ServerPath: "x", ModelPath: "y"})

	baseURL, err := proc.Start(context.Background())
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Start: err = %v, want que embrulhe ErrNotSupported", err)
	}
	if baseURL != "" {
		t.Errorf("Start: baseURL = %q, want vazio quando Start falha", baseURL)
	}
	if proc.Alive() {
		t.Error("Alive() = true, want false — nada foi iniciado")
	}
	if err := proc.Stop(); err != nil {
		t.Errorf("Stop() = %v, want nil — nada foi iniciado, então não há o que falhar ao parar", err)
	}
}
