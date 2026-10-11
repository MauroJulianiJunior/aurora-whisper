package main

import (
	"context"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// recorder é a porta entre a sessão interativa (cli.go, sem build tag) e a
// captura real (record_windows.go) ou seu stub fora do Windows
// (record_other.go) — o mesmo padrão que internal/agent/audio já usa
// entre Session e device (device.go, session_test.go/fakeDevice): a
// lógica de fluxo fica inteiramente testável com um dublê, sem build tag
// e sem hardware nenhum.
//
// O contrato presume uso SEQUENCIAL — nunca duas amostras em gravação ao
// mesmo tempo: Open uma vez, depois BeginSample/EndSample alternados por
// amostra, e Close no fim. cli.go é o único chamador e respeita isso por
// construção (um loop simples, uma amostra de cada vez).
type recorder interface {
	// Open abre o microfone e começa a captura contínua com pre-roll.
	// Chamado uma única vez, na inicialização do programa.
	Open(ctx context.Context, maxRecording time.Duration) error

	// BeginSample marca o início de uma nova gravação no pre-roll já em
	// captura (não abre nem reinicia nada) e devolve o canal de nível RMS
	// ao vivo desta gravação (best-effort — pode perder frame sob carga,
	// nunca é a fonte do áudio realmente capturado), para o indicador de
	// progresso no console.
	BeginSample() <-chan float64

	// EndSample encerra a gravação iniciada pelo BeginSample mais
	// recente e devolve o PCM capturado (incluindo o pre-roll anterior à
	// marca), a taxa de amostragem, e o resumo de VAD.
	EndSample() ([]int16, int, dictation.VADSummary, error)

	// Close libera o microfone. Chamado uma vez, ao encerrar o programa.
	// DeviceReport devolve uma linha de diagnostico sobre o formato
	// negociado e o nivel por canal, ou string vazia se nao houver.
	DeviceReport() string
	Close() error
}
