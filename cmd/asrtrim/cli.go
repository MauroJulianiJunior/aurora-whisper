package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// run é a lógica do CLI separada de main() para ser testável sem exec, no
// mesmo padrão de cmd/asrbench/cli.go, cmd/asrrec/cli.go e
// cmd/asrfix/cli.go.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("asrtrim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", "", "caminho do manifesto do dataset (JSON, internal/benchmark/dataset)")
	outDir := fs.String("out", "", "diretório de saída para os WAVs normalizados — nunca o diretório dos originais")
	margin := fs.Duration("margin", 200*time.Millisecond, "quanto silêncio deixar de cada lado da fala detectada")

	if err := fs.Parse(args); err != nil {
		return 2 // flag já escreveu a mensagem em stderr
	}
	if *manifestPath == "" || *outDir == "" {
		fmt.Fprintln(stderr, "asrtrim: -manifest e -out são obrigatórios")
		fs.Usage()
		return 2
	}
	if *margin < 0 {
		fmt.Fprintln(stderr, "asrtrim: -margin não pode ser negativa")
		return 2
	}

	samples, err := dataset.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, "asrtrim:", err)
		return 1
	}

	absOut, err := filepath.Abs(*outDir)
	if err != nil {
		fmt.Fprintln(stderr, "asrtrim: resolvendo -out:", err)
		return 1
	}
	absOut = filepath.Clean(absOut)

	// Recusa ANTES de escrever qualquer coisa: se -out coincidir com o
	// diretório de qualquer amostra original, não há tomada parcial que já
	// tenha corrompido nada.
	for _, s := range samples {
		origDir := filepath.Clean(filepath.Dir(s.AudioPath))
		if origDir == absOut {
			fmt.Fprintf(stderr, "asrtrim: -out (%s) é o mesmo diretório dos arquivos originais (%s) — recuso sobrescrever\n", absOut, origDir)
			return 2
		}
	}

	if err := os.MkdirAll(absOut, 0o755); err != nil {
		fmt.Fprintln(stderr, "asrtrim: criando -out:", err)
		return 1
	}

	fmt.Fprintln(stdout, tableHeader())
	var results []sampleResult
	var refused []string
	var totalBefore, totalAfter, totalRemoved time.Duration

	// Processa TODAS as amostras mesmo quando alguma é recusada: o
	// relatório desta ferramenta é a tabela completa do dataset, não só o
	// prefixo até o primeiro problema. O código de saída ainda reflete a
	// recusa (abaixo).
	for _, s := range samples {
		res, err := trimSample(s, absOut, *margin)
		if err != nil {
			fmt.Fprintf(stderr, "asrtrim: %s: %v\n", s.ID, err)
			fmt.Fprintf(stdout, "%-14s | RECUSADO: %v\n", s.ID, err)
			refused = append(refused, s.ID)
			continue
		}
		results = append(results, res)
		fmt.Fprintln(stdout, formatRow(res))
		totalBefore += res.Before
		totalAfter += res.After
		totalRemoved += res.RemovedStart + res.RemovedEnd
	}

	var removedPct float64
	if totalBefore > 0 {
		removedPct = 100 * totalRemoved.Seconds() / totalBefore.Seconds()
	}
	fmt.Fprintf(stdout, "\nasrtrim: %d/%d arquivos normalizados em %s.\n", len(results), len(samples), absOut)
	fmt.Fprintf(stdout, "asrtrim: total antes %s, total depois %s, silêncio removido %s (%.1f%% do total antes).\n",
		totalBefore, totalAfter, totalRemoved, removedPct)

	if len(refused) > 0 {
		sort.Strings(refused)
		fmt.Fprintf(stderr, "asrtrim: %d arquivo(s) recusado(s): %v\n", len(refused), refused)
		return 1
	}
	return 0
}

// sampleResult é o que uma amostra normalizada imprime na tabela: as
// durações antes/depois, quanto silêncio saiu de cada lado, e quanta fala
// foi detectada.
type sampleResult struct {
	ID                       string
	Before, After            time.Duration
	RemovedStart, RemovedEnd time.Duration
	Speech                   time.Duration
}

// trimSample lê o WAV original de s, decide o corte (decideTrim) e
// escreve o resultado em <outDir>/<id>.wav via dataset.WriteWAV — nunca
// em s.AudioPath. O corte é aplicado direto sobre as amostras int16
// ORIGINAIS por fatiamento: a região preservada nunca passa por uma
// conversão de ida e volta para float, então não há ruído de quantização
// novo nem risco de os valores divergirem do arquivo de origem.
func trimSample(s dataset.Sample, outDir string, margin time.Duration) (sampleResult, error) {
	sampleRate, raw, err := dataset.ReadWAV(s.AudioPath)
	if err != nil {
		return sampleResult{}, fmt.Errorf("lendo %s: %w", s.AudioPath, err)
	}
	if sampleRate <= 0 || len(raw) == 0 {
		return sampleResult{}, fmt.Errorf("arquivo sem amostras utilizáveis (sampleRate=%d, %d amostras)", sampleRate, len(raw))
	}

	decision := decideTrim(int16ToFloat64(raw), sampleRate, margin)
	if decision.Refused {
		return sampleResult{}, fmt.Errorf("aparagem recusada: %s", decision.Reason)
	}

	trimmed := raw[decision.StartCut : len(raw)-decision.EndCut]

	outPath := filepath.Join(outDir, s.ID+".wav")
	if err := dataset.WriteWAV(outPath, sampleRate, trimmed); err != nil {
		return sampleResult{}, fmt.Errorf("escrevendo %s: %w", outPath, err)
	}

	toDuration := func(numSamples int) time.Duration {
		return time.Duration(float64(numSamples) / float64(sampleRate) * float64(time.Second))
	}

	return sampleResult{
		ID:           s.ID,
		Before:       toDuration(len(raw)),
		After:        toDuration(len(trimmed)),
		RemovedStart: toDuration(decision.StartCut),
		RemovedEnd:   toDuration(decision.EndCut),
		Speech:       toDuration(decision.SpeechEnd - decision.SpeechStart),
	}, nil
}

// int16ToFloat64 decodifica amostras int16 cruas para float64 dividindo
// pelo valor NEGATIVO máximo (2^15) — a mesma convenção de decodificação
// de convertPCM em internal/agent/audio/pcm.go (arquivo que esta tarefa
// deliberadamente não toca) e a mesma que cmd/asrfix/cli.go usa para o
// mesmo fim. Serve só para localizar a borda da fala (trim.go); o WAV
// escrito reaproveita as amostras int16 originais, sem passar por aqui.
func int16ToFloat64(samples []int16) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = float64(s) / 32768
	}
	return out
}

func tableHeader() string {
	return fmt.Sprintf("%-14s | %8s %8s | %9s %8s | %8s",
		"id", "antes", "depois", "rm-inicio", "rm-fim", "fala")
}

func formatRow(r sampleResult) string {
	return fmt.Sprintf("%-14s | %8s %8s | %9s %8s | %8s",
		r.ID,
		r.Before.Round(time.Millisecond), r.After.Round(time.Millisecond),
		r.RemovedStart.Round(time.Millisecond), r.RemovedEnd.Round(time.Millisecond),
		r.Speech.Round(time.Millisecond),
	)
}
