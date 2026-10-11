package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain compila o binário asrbench REAL uma única vez (go build) antes
// da suíte, para o teste de ponta a ponta abaixo rodar o binário de
// verdade como subprocesso — exatamente como uma pessoa rodaria depois,
// só que apontando o candidato para o subcomando oculto "mockcandidate"
// (internal/asr/mock) em vez de um modelo de ASR de verdade. Isso é o que
// a tarefa pede explicitamente: "precisa funcionar com o provider MOCK
// [...] para ser testável de ponta a ponta sem nenhum modelo instalado".
var asrbenchBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "asrbench-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: MkdirTemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	name := "asrbench"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	asrbenchBin = filepath.Join(dir, name)

	build := exec.Command("go", "build", "-o", asrbenchBin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: go build ./cmd/asrbench falhou: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func writeE2EWAV(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("criando WAV: %v", err)
	}
	defer f.Close()

	const sampleRateHz, numSamples, bitsPerSample = 16000, 1600, 16
	blockAlign := bitsPerSample / 8
	byteRate := sampleRateHz * blockAlign
	dataSize := numSamples * blockAlign
	le32 := func(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
	le16 := func(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }

	f.WriteString("RIFF")
	f.Write(le32(uint32(36 + dataSize)))
	f.WriteString("WAVE")
	f.WriteString("fmt ")
	f.Write(le32(16))
	f.Write(le16(1))
	f.Write(le16(1))
	f.Write(le32(sampleRateHz))
	f.Write(le32(uint32(byteRate)))
	f.Write(le16(uint16(blockAlign)))
	f.Write(le16(bitsPerSample))
	f.WriteString("data")
	f.Write(le32(uint32(dataSize)))
	f.Write(make([]byte, dataSize))
}

func TestEndToEnd_MockCandidate_ProducesReport(t *testing.T) {
	dir := t.TempDir()

	audioPath := filepath.Join(dir, "s1.wav")
	writeE2EWAV(t, audioPath)
	if err := os.WriteFile(audioPath+".transcript.txt", []byte("o Fisco cobrou o DCTFWeb"), 0o644); err != nil {
		t.Fatalf("setup fixture: %v", err)
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	manifest := fmt.Sprintf(`{"samples":[
		{"id":"s1","audio":%q,"reference":"o Fisco cobrou o DCTFWeb","category":"contabilidade","entities":["Fisco","DCTFWeb"]}
	]}`, audioPath)
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("setup manifest: %v", err)
	}

	candidatesPath := filepath.Join(dir, "candidates.json")
	// coldArgs/warmArgs apontam para o subcomando oculto deste MESMO
	// binário (executable == asrbenchBin) — "mockcandidate cold {{audio}}"
	// e "mockcandidate warm" — provando o fluxo ADR-012 (processo
	// externo) de ponta a ponta com internal/asr/mock, sem nenhum modelo.
	candidatesJSON := fmt.Sprintf(`{"candidates":[
		{"name":"mock","executable":%q,"coldArgs":["mockcandidate","cold","{{audio}}"],"warmArgs":["mockcandidate","warm"],"timeout":"10s"}
	]}`, asrbenchBin)
	if err := os.WriteFile(candidatesPath, []byte(candidatesJSON), 0o644); err != nil {
		t.Fatalf("setup candidates: %v", err)
	}

	outPath := filepath.Join(dir, "report.md")

	cmd := exec.Command(asrbenchBin,
		"-manifest", manifestPath,
		"-candidates", candidatesPath,
		"-out", outPath,
		"-hardware", "teste e2e",
		"-os", runtime.GOOS,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("asrbench falhou: %v\n%s", err, out)
	}

	reportBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("relatório não foi escrito: %v", err)
	}
	report := string(reportBytes)

	for _, want := range []string{
		"teste e2e",
		"mock",
		"Decisão técnica",
		"N total de amostras: 1",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("relatório não contém %q:\n%s", want, report)
		}
	}

	// A amostra transcreve exatamente a referência (fixture ==
	// reference), então Entity Accuracy deve ser 100% (2/2) tanto em
	// cold quanto em warm.
	if !strings.Contains(report, "Correct=2, Total=2") {
		t.Errorf("relatório não mostra Entity Accuracy 2/2 esperado:\n%s", report)
	}
}

func TestEndToEnd_MissingRequiredFlags(t *testing.T) {
	cmd := exec.Command(asrbenchBin)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("asrbench sem flags devia sair com erro; saída: %s", out)
	}
	if !strings.Contains(string(out), "obrigatórios") {
		t.Errorf("saída não explica quais flags são obrigatórias: %s", out)
	}
}

func TestEndToEnd_InvalidManifestPath(t *testing.T) {
	dir := t.TempDir()
	candidatesPath := filepath.Join(dir, "candidates.json")
	os.WriteFile(candidatesPath, []byte(`{"candidates":[{"name":"x","executable":"bin","coldArgs":["{{audio}}"]}]}`), 0o644)

	cmd := exec.Command(asrbenchBin,
		"-manifest", filepath.Join(dir, "nao-existe.json"),
		"-candidates", candidatesPath,
		"-out", filepath.Join(dir, "out.md"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("asrbench com manifesto ausente devia sair com erro; saída: %s", out)
	}
}
