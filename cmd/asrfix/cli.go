package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// run é a lógica do CLI separada de main() para ser testável sem exec,
// no mesmo padrão de cmd/asrbench/cli.go e cmd/asrrec/cli.go.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("asrfix", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", "", "caminho do manifesto do dataset (JSON, internal/benchmark/dataset)")
	outDir := fs.String("out", "", "diretório de saída para os WAVs recuperados — nunca o diretório dos originais")
	trimStart := fs.Duration("trim-start", 0, "descarta esta duração do início de cada gravação (pre-roll anterior ao ENTER); recusa cortar se houver fala no trecho")

	if err := fs.Parse(args); err != nil {
		return 2 // flag já escreveu a mensagem em stderr
	}
	if *manifestPath == "" || *outDir == "" {
		fmt.Fprintln(stderr, "asrfix: -manifest e -out são obrigatórios")
		fs.Usage()
		return 2
	}

	samples, err := dataset.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, "asrfix:", err)
		return 1
	}

	absOut, err := filepath.Abs(*outDir)
	if err != nil {
		fmt.Fprintln(stderr, "asrfix: resolvendo -out:", err)
		return 1
	}
	absOut = filepath.Clean(absOut)

	// Recusa ANTES de escrever qualquer coisa: se -out coincidir com o
	// diretório de qualquer amostra original, não há tomada parcial que
	// já tenha corrompido nada.
	for _, s := range samples {
		origDir := filepath.Clean(filepath.Dir(s.AudioPath))
		if origDir == absOut {
			fmt.Fprintf(stderr, "asrfix: -out (%s) é o mesmo diretório dos arquivos originais (%s) — recuso sobrescrever\n", absOut, origDir)
			return 2
		}
	}

	if err := os.MkdirAll(absOut, 0o755); err != nil {
		fmt.Fprintln(stderr, "asrfix: criando -out:", err)
		return 1
	}

	fmt.Fprintln(stdout, tableHeader())
	results := make([]sampleResult, 0, len(samples))
	for _, s := range samples {
		res, err := recoverSample(s, absOut, *trimStart)
		if err != nil {
			fmt.Fprintf(stderr, "asrfix: %s: %v\n", s.ID, err)
			return 1
		}
		results = append(results, res)
		fmt.Fprintln(stdout, formatRow(res))
	}

	var failedIDs []string
	for _, r := range results {
		if !r.After.Speech.IsSpeech() {
			failedIDs = append(failedIDs, r.ID)
		}
	}
	sort.Strings(failedIDs)

	fmt.Fprintf(stdout, "\nasrfix: %d/%d arquivos recuperados em %s com característica de fala.\n",
		len(results)-len(failedIDs), len(results), absOut)

	if len(failedIDs) > 0 {
		fmt.Fprintf(stderr, "asrfix: %d arquivo(s) recuperado(s) NÃO passaram no detector de fala: %v\n",
			len(failedIDs), failedIDs)
		return 1
	}
	return 0
}

// stageStats resume amplitude e estrutura de pitch de um sinal — usado
// tanto para o "antes" (amostras cruas, ainda corrompidas) quanto para o
// "depois" (amostras já recuperadas) de cada arquivo.
type stageStats struct {
	Peak, RMS, CrestFactor float64
	Speech                 audio.F0Profile
}

func analyzeStage(samples []float64, sampleRate int) stageStats {
	peak, rms, crest := audio.AmplitudeStats(samples)
	return stageStats{
		Peak:        peak,
		RMS:         rms,
		CrestFactor: crest,
		Speech:      audio.AnalyzeF0(samples, sampleRate),
	}
}

// sampleResult é o que uma amostra recuperada imprime na tabela: as
// estatísticas antes e depois da recuperação.
type sampleResult struct {
	ID     string
	Before stageStats
	After  stageStats
}

// recoverSample lê o WAV original de s, aplica
// audio.RecoverBitPatternFloat32, escreve o resultado em
// <outDir>/<id>.wav (via dataset.WriteWAV — nunca em s.AudioPath) e
// devolve as estatísticas antes/depois para a tabela.
func recoverSample(s dataset.Sample, outDir string, trimStart time.Duration) (sampleResult, error) {
	sampleRate, raw, err := dataset.ReadWAV(s.AudioPath)
	if err != nil {
		return sampleResult{}, fmt.Errorf("lendo %s: %w", s.AudioPath, err)
	}

	before := analyzeStage(int16ToFloat64(raw), sampleRate)

	recoveredF32 := audio.RecoverBitPatternFloat32(raw)
	after := analyzeStage(float32ToFloat64(recoveredF32), sampleRate)

	recoveredI16 := make([]int16, len(recoveredF32))
	for i, v := range recoveredF32 {
		recoveredI16[i] = audio.FloatToInt16(v)
	}

	// A aparagem roda sobre o áudio JÁ RECUPERADO: no áudio corrompido o
	// silêncio não é silêncio (o padrão de bits de uma amostra minúscula
	// vira um valor grande), então medir energia antes de recuperar
	// decidiria em cima de um sinal que não existe.
	trim := decidirTrim(float32ToFloat64(recoveredF32), sampleRate, trimStart)
	if trim.Recusado {
		return sampleResult{}, fmt.Errorf("aparagem recusada: %s", trim.Motivo)
	}
	if trim.Cortar > 0 {
		recoveredI16 = recoveredI16[trim.Cortar:]
		after = analyzeStage(float32ToFloat64(recoveredF32[trim.Cortar:]), sampleRate)
	}

	outPath := filepath.Join(outDir, s.ID+".wav")
	if err := dataset.WriteWAV(outPath, sampleRate, recoveredI16); err != nil {
		return sampleResult{}, fmt.Errorf("escrevendo %s: %w", outPath, err)
	}

	return sampleResult{ID: s.ID, Before: before, After: after}, nil
}

// int16ToFloat64 decodifica amostras int16 cruas para float64 dividindo
// pelo valor NEGATIVO máximo (2^15) — a mesma convenção de decodificação
// de convertPCM em internal/agent/audio/pcm.go (arquivo que esta tarefa
// deliberadamente não toca). Serve só para medir "antes" da recuperação;
// nenhum áudio de verdade sai daqui.
func int16ToFloat64(samples []int16) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = float64(s) / 32768
	}
	return out
}

func float32ToFloat64(samples []float32) []float64 {
	out := make([]float64, len(samples))
	for i, v := range samples {
		out[i] = float64(v)
	}
	return out
}

func tableHeader() string {
	return fmt.Sprintf("%-12s | %-6s %-8s %-7s %-6s | %-6s %-8s %-7s %-6s",
		"id",
		"pico-A", "rms-A", "crist-A", "voz-A",
		"pico-D", "rms-D", "crist-D", "voz-D")
}

func formatRow(r sampleResult) string {
	return fmt.Sprintf("%-12s | %6.4f %8.5f %7.2f %5.1f%% | %6.4f %8.5f %7.2f %5.1f%%",
		r.ID,
		r.Before.Peak, r.Before.RMS, r.Before.CrestFactor, 100*r.Before.Speech.VoicedFraction,
		r.After.Peak, r.After.RMS, r.After.CrestFactor, 100*r.After.Speech.VoicedFraction,
	)
}
