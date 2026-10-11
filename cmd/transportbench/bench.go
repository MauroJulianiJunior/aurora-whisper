package main

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// sizeSpec descreve um dos três tamanhos derivados de áudio real (ver
// payload.go): rótulo para a tabela e os bytes do payload, construídos
// uma única vez e reutilizados em toda repetição — o benchmark mede
// transporte, não alocação de payload.
type sizeSpec struct {
	label   string
	payload []byte
}

// sizeSpecs são os três tamanhos que docs/benchmarks/candidatos-2026-09-17.md
// pede: 1s, 3s e 10s de PCM16 mono 16 kHz com cabeçalho WAV — a fala mais
// longa que a spec prevê.
func sizeSpecs() []sizeSpec {
	return []sizeSpec{
		{label: "1s (32.044 B)", payload: buildWAVPayload(1)},
		{label: "3s (96.044 B)", payload: buildWAVPayload(3)},
		{label: "10s (320.044 B)", payload: buildWAVPayload(10)},
	}
}

// transports são os três transportes medidos — ver main.go sobre por
// que named pipe do Windows não entra.
func transports() []roundTripper {
	return []roundTripper{pipeTransport{}, tcpTransport{}, httpTransport{}}
}

// runBenchmark mede cold e warm para cada transporte × tamanho, mais o
// controle in-process por tamanho, e devolve as linhas prontas para
// formatTable. exePath é o caminho do próprio binário transportbench
// (reexecutado como processo de eco — ver echo_*.go e transport_*.go);
// progress recebe uma linha por etapa (tipicamente stderr), para quem
// está rodando saber que não travou — a medição cold sozinha já soma
// dezenas de spawns de processo.
func runBenchmark(exePath string, coldReps, warmReps, controlReps int, progress io.Writer) []Row {
	var rows []Row

	for _, sz := range sizeSpecs() {
		for _, tr := range transports() {
			fmt.Fprintf(progress, "transportbench: medindo %s x %s (cold, %d repeticoes)...\n", tr.name(), sz.label, coldReps)
			rows = append(rows, measureCold(tr, exePath, sz, coldReps))

			fmt.Fprintf(progress, "transportbench: medindo %s x %s (warm, %d repeticoes)...\n", tr.name(), sz.label, warmReps)
			rows = append(rows, measureWarm(tr, exePath, sz, warmReps))
		}

		fmt.Fprintf(progress, "transportbench: medindo controle in-process x %s (%d repeticoes)...\n", sz.label, controlReps)
		rows = append(rows, measureControl(sz, controlReps))
	}

	return rows
}

// measureCold roda reps rodadas de cold.coldRoundTrip e agrega as
// durações bem-sucedidas. Uma rodada cujo eco não bate byte a byte com o
// payload enviado, ou que devolve erro de transporte, é descartada da
// amostra de latência e contada em Failed — nunca vira um número de
// tempo que parece medição válida (docs/benchmarks/candidatos-2026-09-17.md:
// "um transporte que corrompe ou trunca e ainda assim é rápido não serve
// de nada").
func measureCold(tr roundTripper, exePath string, sz sizeSpec, reps int) Row {
	samples := make([]float64, 0, reps)
	failed := 0
	for i := 0; i < reps; i++ {
		elapsed, echoed, err := tr.coldRoundTrip(exePath, sz.payload)
		if err != nil || !bytes.Equal(echoed, sz.payload) {
			failed++
			continue
		}
		samples = append(samples, float64(elapsed))
	}
	return Row{Transport: tr.name(), SizeLabel: sz.label, Mode: "cold", Stats: metrics.Aggregate(samples), Failed: failed}
}

// measureWarm abre UMA sessão warm (custo de start excluído da amostra)
// e roda reps idas-e-voltas nela. Se a sessão nem abre, todas as reps
// contam como falha e Stats fica vazio (N=0) — formatStat imprime "sem
// amostra" em vez de inventar um zero.
func measureWarm(tr roundTripper, exePath string, sz sizeSpec, reps int) Row {
	session, err := tr.startWarm(exePath)
	if err != nil {
		return Row{Transport: tr.name(), SizeLabel: sz.label, Mode: "warm", Failed: reps}
	}
	defer session.close()

	samples := make([]float64, 0, reps)
	failed := 0
	for i := 0; i < reps; i++ {
		elapsed, echoed, err := session.roundTrip(sz.payload)
		if err != nil || !bytes.Equal(echoed, sz.payload) {
			failed++
			continue
		}
		samples = append(samples, float64(elapsed))
	}
	return Row{Transport: tr.name(), SizeLabel: sz.label, Mode: "warm", Stats: metrics.Aggregate(samples), Failed: failed}
}

// measureControl mede a "chamada de função no mesmo processo, sem
// transporte nenhum" — o piso que diz quanto do tempo medido acima é
// overhead do próprio harness (alocação, cópia, time.Now()) e não do
// transporte de verdade.
func measureControl(sz sizeSpec, reps int) Row {
	samples := make([]float64, 0, reps)
	failed := 0
	for i := 0; i < reps; i++ {
		start := time.Now()
		echoed := echoInProcess(sz.payload)
		elapsed := time.Since(start)
		if !bytes.Equal(echoed, sz.payload) {
			failed++
			continue
		}
		samples = append(samples, float64(elapsed))
	}
	return Row{Transport: "in-process (controle)", SizeLabel: sz.label, Mode: "controle", Stats: metrics.Aggregate(samples), Failed: failed}
}

// echoInProcess copia payload e devolve — sem pipe, sem socket, sem
// processo. É deliberadamente trivial: o controle só existe para medir o
// que já está pago antes de qualquer transporte.
func echoInProcess(payload []byte) []byte {
	out := make([]byte, len(payload))
	copy(out, payload)
	return out
}
