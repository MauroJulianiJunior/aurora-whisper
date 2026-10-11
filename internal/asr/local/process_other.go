//go:build !windows

package local

import "context"

// otherServerProcess implementa serverProcess fora do Windows: nunca sobe
// nada, porque não há whisper-server.exe para subir. O Agent é um produto
// Windows (ADR-010) — este arquivo existe só para o pacote compilar e ser
// testável no Linux, seguindo a convenção de docs/CLAUDE.md ("Pacote novo
// precisa de par _windows.go / _other.go SE tiver código específico de
// plataforma").
type otherServerProcess struct{}

func newPlatformProcess(cfg Config) serverProcess { return otherServerProcess{} }

func (otherServerProcess) Start(ctx context.Context) (string, error) { return "", ErrNotSupported }

func (otherServerProcess) Alive() bool { return false }

func (otherServerProcess) Stop() error { return nil }
