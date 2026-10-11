//go:build !windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// errWindowsOnly é o erro devolvido por toda operação deste stub. A
// captura WASAPI real (record_windows.go, sobre internal/agent/audio) só
// existe no Windows — este arquivo nem tenta: fora do Windows, Open falha
// imediatamente com uma mensagem clara em vez de deixar o comando
// prosseguir e travar mais adiante tentando ler de um microfone que não
// vai abrir.
var errWindowsOnly = errors.New("asrrec: gravação só funciona no Windows (captura WASAPI); nada a fazer nesta plataforma")

// noopRecorder satisfaz recorder fora do Windows só para que
// `GOOS=linux go build ./...` compile (CLAUDE.md, "compilação
// multiplataforma") — nunca chega a abrir nada.
type noopRecorder struct{}

func newRecorder() recorder { return noopRecorder{} }

func (noopRecorder) Open(ctx context.Context, maxRecording time.Duration) error {
	return errWindowsOnly
}

func (noopRecorder) BeginSample() <-chan float64 { return nil }

func (noopRecorder) EndSample() ([]int16, int, dictation.VADSummary, error) {
	return nil, 0, dictation.VADSummary{}, errWindowsOnly
}

func (noopRecorder) Close() error { return nil }

func (noopRecorder) DeviceReport() string { return "" }
