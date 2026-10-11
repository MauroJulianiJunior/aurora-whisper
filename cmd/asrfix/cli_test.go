package main

import (
	"bytes"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// syntheticSpeechF32 espelha o gerador usado em
// internal/agent/audio/recover_test.go — fala sintética determinística —
// só para os testes deste pacote terem uma amostra "recuperável" sem
// depender de nenhum arquivo real do dataset.
func syntheticSpeechF32(sampleRate int, seconds float64) []float32 {
	n := int(float64(sampleRate) * seconds)
	out := make([]float32, n)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / float64(sampleRate)
		f0 := 100 + 30*math.Sin(2*math.Pi*0.3*t)
		phase += 2 * math.Pi * f0 / float64(sampleRate)
		var s float64
		for h := 1; h <= 12; h++ {
			s += math.Sin(phase*float64(h)) / float64(h*h)
		}
		env := 0.5 * (1 - math.Cos(2*math.Pi*math.Mod(t, 0.6)/0.6))
		out[i] = float32(0.3 * s * env)
	}
	return out
}

// corruptForward aplica a MESMA corrupção descrita em
// internal/agent/audio/recover.go — bits(f) lidos como int32/2^31 e
// depois convertidos para int16 com audio.FloatToInt16 (mesma convenção
// do resampler real) — para fabricar, em teste, uma amostra "já
// corrompida" sem precisar do pipeline WASAPI de verdade.
func corruptForward(clean []float32) []int16 {
	out := make([]int16, len(clean))
	for i, f := range clean {
		bits := math.Float32bits(f)
		x := float32(int32(bits)) / 2147483648
		out[i] = audio.FloatToInt16(x)
	}
	return out
}

func writeManifest(t *testing.T, dir string, entries ...[2]string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(`{"samples":[`)
	for i, e := range entries {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"` + e[0] + `","audio":"` + e[1] + `","reference":"frase de teste","category":"ingles","entities":[]}`)
	}
	sb.WriteString(`]}`)
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("escrevendo manifesto: %v", err)
	}
	return path
}

func TestRun_MissingRequiredFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "obrigatórios") {
		t.Errorf("stderr não explica flags obrigatórias: %s", stderr.String())
	}
}

func TestRun_InvalidManifestPath(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"-manifest", filepath.Join(dir, "nao-existe.json"),
		"-out", filepath.Join(dir, "out"),
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, stderr.String())
	}
}

// TestRun_RefusesSameDirectoryAsOriginals prova a regra "nunca sobrescreve
// os arquivos originais": -out apontando pro mesmo diretório dos
// originais tem de recusar ANTES de tocar em qualquer arquivo.
func TestRun_RefusesSameDirectoryAsOriginals(t *testing.T) {
	dir := t.TempDir()
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	audioPath := filepath.Join(audioDir, "s1.wav")
	if err := dataset.WriteWAV(audioPath, 16000, make([]int16, 1600)); err != nil {
		t.Fatalf("setup WAV: %v", err)
	}
	manifestPath := writeManifest(t, dir, [2]string{"s1", "audio/s1.wav"})

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"-manifest", manifestPath,
		"-out", audioDir, // MESMO diretório dos originais
	}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "mesmo diretório") {
		t.Errorf("stderr não explica a recusa: %s", stderr.String())
	}

	_, samples, err := dataset.ReadWAV(audioPath)
	if err != nil {
		t.Fatalf("arquivo original ilegível depois da recusa: %v", err)
	}
	for _, s := range samples {
		if s != 0 {
			t.Fatalf("arquivo original foi alterado — devia continuar todo zero")
		}
	}
}

// TestRun_RecoversSyntheticSampleAndPassesSpeechDetector é o caminho
// feliz: uma amostra sintética corrompida exatamente como o defeito real
// corrompia tem de sair recuperada, com o mesmo tamanho, sem tocar no
// original, e passar no detector de fala (o mesmo gate que decide o
// código de saída).
func TestRun_RecoversSyntheticSampleAndPassesSpeechDetector(t *testing.T) {
	dir := t.TempDir()
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	clean := syntheticSpeechF32(16000, 3.0)
	corrupted := corruptForward(clean)
	audioPath := filepath.Join(audioDir, "fala-01.wav")
	if err := dataset.WriteWAV(audioPath, 16000, corrupted); err != nil {
		t.Fatalf("setup WAV: %v", err)
	}
	manifestPath := writeManifest(t, dir, [2]string{"fala-01", "audio/fala-01.wav"})

	outDir := filepath.Join(dir, "recuperado")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-manifest", manifestPath, "-out", outDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "1/1 arquivos recuperados") {
		t.Errorf("resumo não confere no stdout:\n%s", stdout.String())
	}

	outPath := filepath.Join(outDir, "fala-01.wav")
	rate, recovered, err := dataset.ReadWAV(outPath)
	if err != nil {
		t.Fatalf("WAV recuperado não foi escrito: %v", err)
	}
	if rate != 16000 {
		t.Fatalf("sampleRate recuperado = %d, want 16000", rate)
	}
	if len(recovered) != len(clean) {
		t.Fatalf("len(recuperado) = %d, want %d", len(recovered), len(clean))
	}

	_, origAfter, err := dataset.ReadWAV(audioPath)
	if err != nil {
		t.Fatalf("relendo original: %v", err)
	}
	for i := range origAfter {
		if origAfter[i] != corrupted[i] {
			t.Fatalf("arquivo original foi alterado no índice %d", i)
		}
	}
}

// TestRun_FailsWhenRecoveredSampleIsNotSpeech prova o gate: uma amostra
// que não tem estrutura de fala nenhuma — aqui, ruído branco, o mesmo
// controle negativo de internal/agent/audio/recover_test.go — tem de
// fazer asrfix sair com código de erro e nomear o arquivo que falhou.
func TestRun_FailsWhenRecoveredSampleIsNotSpeech(t *testing.T) {
	dir := t.TempDir()
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	rng := rand.New(rand.NewSource(7))
	noiseF64 := make([]float32, 16000*2)
	for i := range noiseF64 {
		noiseF64[i] = float32(rng.NormFloat64() * 0.1)
	}
	noise := make([]int16, len(noiseF64))
	for i, v := range noiseF64 {
		noise[i] = audio.FloatToInt16(v)
	}

	audioPath := filepath.Join(audioDir, "ruido-01.wav")
	if err := dataset.WriteWAV(audioPath, 16000, noise); err != nil {
		t.Fatalf("setup WAV: %v", err)
	}
	manifestPath := writeManifest(t, dir, [2]string{"ruido-01", "audio/ruido-01.wav"})

	outDir := filepath.Join(dir, "recuperado")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-manifest", manifestPath, "-out", outDir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "ruido-01") {
		t.Errorf("stderr não identifica o arquivo que falhou: %s", stderr.String())
	}
}
