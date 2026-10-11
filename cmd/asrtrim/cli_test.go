package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

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

// synthWAV escreve um WAV PCM16 mono de `segs` concatenados (cada
// segmento já em float64 normalizado) em path.
func synthWAV(t *testing.T, path string, rate int, segs ...[]float64) {
	t.Helper()
	x := concat(segs...)
	raw := make([]int16, len(x))
	for i, v := range x {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		raw[i] = int16(math.Round(v * 32767))
	}
	if err := dataset.WriteWAV(path, rate, raw); err != nil {
		t.Fatalf("escrevendo WAV de teste %s: %v", path, err)
	}
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

func TestRun_NegativeMarginRejected(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"-manifest", filepath.Join(dir, "manifest.json"),
		"-out", filepath.Join(dir, "out"),
		"-margin", "-1s",
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr=%s", code, stderr.String())
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
	synthWAV(t, audioPath, 16000, segmento(16000, 1.0, 0.0005), segmento(16000, 2.0, 0.3), segmento(16000, 0.5, 0.0005))
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
	if len(samples) == 0 {
		t.Fatalf("arquivo original ficou vazio depois da recusa")
	}
}

// TestRun_NormalizesSyntheticSample é o caminho feliz: uma amostra
// sintética com silêncio de borda conhecido sai mais curta, com o
// original intacto, e o resumo final soma corretamente.
func TestRun_NormalizesSyntheticSample(t *testing.T) {
	dir := t.TempDir()
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	audioPath := filepath.Join(audioDir, "geral-01.wav")
	synthWAV(t, audioPath, 16000,
		segmento(16000, 1.2, 0.0005), // silêncio inicial
		segmento(16000, 3.0, 0.3),    // fala
		segmento(16000, 0.3, 0.0005), // silêncio final
	)
	manifestPath := writeManifest(t, dir, [2]string{"geral-01", "audio/geral-01.wav"})

	outDir := filepath.Join(dir, "normalizado")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-manifest", manifestPath, "-out", outDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "1/1 arquivos normalizados") {
		t.Errorf("resumo não confere no stdout:\n%s", stdout.String())
	}

	outPath := filepath.Join(outDir, "geral-01.wav")
	rate, trimmed, err := dataset.ReadWAV(outPath)
	if err != nil {
		t.Fatalf("WAV normalizado não foi escrito: %v", err)
	}
	if rate != 16000 {
		t.Fatalf("sampleRate = %d, want 16000", rate)
	}

	_, original, err := dataset.ReadWAV(audioPath)
	if err != nil {
		t.Fatalf("relendo original: %v", err)
	}
	if len(trimmed) >= len(original) {
		t.Fatalf("len(trimmed)=%d não ficou menor que len(original)=%d", len(trimmed), len(original))
	}
}

// TestRun_RefusesAllSilentSampleButKeepsProcessingOthers prova duas
// coisas ao mesmo tempo: uma amostra sem fala nenhuma é recusada (código
// de saída diferente de zero, ID citado), e isso NÃO impede as demais
// amostras do manifesto de serem normalizadas — o relatório é a tabela
// inteira, não só o prefixo até o primeiro problema.
func TestRun_RefusesAllSilentSampleButKeepsProcessingOthers(t *testing.T) {
	dir := t.TempDir()
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	silentPath := filepath.Join(audioDir, "mudo-01.wav")
	if err := dataset.WriteWAV(silentPath, 16000, make([]int16, 3*16000)); err != nil {
		t.Fatalf("setup WAV mudo: %v", err)
	}
	goodPath := filepath.Join(audioDir, "geral-01.wav")
	synthWAV(t, goodPath, 16000, segmento(16000, 1.0, 0.0005), segmento(16000, 2.0, 0.3), segmento(16000, 0.3, 0.0005))

	manifestPath := writeManifest(t, dir,
		[2]string{"mudo-01", "audio/mudo-01.wav"},
		[2]string{"geral-01", "audio/geral-01.wav"},
	)

	outDir := filepath.Join(dir, "normalizado")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-manifest", manifestPath, "-out", outDir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "mudo-01") {
		t.Errorf("stderr não identifica o arquivo recusado: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "1/2 arquivos normalizados") {
		t.Errorf("resumo não confere (esperava 1/2 normalizados, o outro recusado):\n%s", stdout.String())
	}

	if _, err := os.Stat(filepath.Join(outDir, "geral-01.wav")); err != nil {
		t.Errorf("amostra boa não foi escrita mesmo com a outra recusada: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "mudo-01.wav")); err == nil {
		t.Errorf("amostra recusada não devia ter gerado arquivo de saída")
	}
}
