package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

func writeManifest(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("escrevendo manifesto de teste: %v", err)
	}
	return path
}

// TestLoadManifestForRecording_AudioDoesNotNeedToExist é a diferença
// deliberada em relação a dataset.LoadManifest: o áudio referenciado NÃO
// existe no disco (nenhum arquivo é criado neste teste), e mesmo assim a
// carga tem que funcionar — é exatamente o estado inicial de
// testdata/asr-dataset/manifest.json antes de qualquer gravação.
func TestLoadManifestForRecording_AudioDoesNotNeedToExist(t *testing.T) {
	dir := t.TempDir()
	body := `{"samples":[
		{"id":"s1","audio":"audio/s1.wav","reference":"o Fisco cobrou o DCTFWeb","category":"contabilidade","entities":["Fisco","DCTFWeb"]},
		{"id":"s2","audio":"audio/s2.wav","reference":"hello world","category":"ingles","entities":[]}
	]}`
	path := writeManifest(t, dir, body)

	samples, err := loadManifestForRecording(path)
	if err != nil {
		t.Fatalf("loadManifestForRecording: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("len(samples) = %d, want 2", len(samples))
	}

	wantAudio := filepath.Join(dir, "audio", "s1.wav")
	if samples[0].AudioPath != wantAudio {
		t.Fatalf("AudioPath = %q, want %q (relativo ao diretório do manifesto)", samples[0].AudioPath, wantAudio)
	}
	if !filepath.IsAbs(samples[0].AudioPath) {
		t.Fatalf("AudioPath = %q, want caminho absoluto", samples[0].AudioPath)
	}
}

func TestLoadManifestForRecording_DerivesIDWhenMissing(t *testing.T) {
	dir := t.TempDir()
	body := `{"samples":[{"audio":"a.wav","reference":"oi","category":"ingles"}]}`
	path := writeManifest(t, dir, body)

	samples, err := loadManifestForRecording(path)
	if err != nil {
		t.Fatalf("loadManifestForRecording: %v", err)
	}
	if samples[0].ID == "" {
		t.Fatal("ID derivado está vazio")
	}
}

func TestLoadManifestForRecording_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	body := `{"samples":[
		{"id":"dup","audio":"a.wav","reference":"a","category":"ingles"},
		{"id":"dup","audio":"b.wav","reference":"b","category":"ingles"}
	]}`
	path := writeManifest(t, dir, body)

	if _, err := loadManifestForRecording(path); err == nil {
		t.Fatal("loadManifestForRecording: want erro para ID duplicado, got nil")
	}
}

func TestLoadManifestForRecording_EmptyReference(t *testing.T) {
	dir := t.TempDir()
	body := `{"samples":[{"id":"s1","audio":"a.wav","reference":"   ","category":"ingles"}]}`
	path := writeManifest(t, dir, body)

	if _, err := loadManifestForRecording(path); err == nil {
		t.Fatal("loadManifestForRecording: want erro para referência vazia, got nil")
	}
}

func TestLoadManifestForRecording_UnknownCategory(t *testing.T) {
	dir := t.TempDir()
	body := `{"samples":[{"id":"s1","audio":"a.wav","reference":"oi","category":"categoria-inventada"}]}`
	path := writeManifest(t, dir, body)

	if _, err := loadManifestForRecording(path); err == nil {
		t.Fatal("loadManifestForRecording: want erro para categoria desconhecida, got nil")
	}
}

func TestLoadManifestForRecording_MissingAudioPath(t *testing.T) {
	dir := t.TempDir()
	body := `{"samples":[{"id":"s1","reference":"oi","category":"ingles"}]}`
	path := writeManifest(t, dir, body)

	if _, err := loadManifestForRecording(path); err == nil {
		t.Fatal("loadManifestForRecording: want erro para caminho de áudio ausente, got nil")
	}
}

func TestLoadManifestForRecording_NoSamples(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{"samples":[]}`)

	if _, err := loadManifestForRecording(path); err == nil {
		t.Fatal("loadManifestForRecording: want erro para manifesto sem amostras, got nil")
	}
}

func TestLoadManifestForRecording_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{not valid json`)

	if _, err := loadManifestForRecording(path); err == nil {
		t.Fatal("loadManifestForRecording: want erro para JSON inválido, got nil")
	}
}

func TestLoadManifestForRecording_MissingFile(t *testing.T) {
	if _, err := loadManifestForRecording("does-not-exist.json"); err == nil {
		t.Fatal("loadManifestForRecording: want erro para manifesto ausente, got nil")
	}
}

// samplesFixture devolve 3 amostras num manifesto fixo, usadas por vários
// testes de pendingSamples abaixo.
func samplesFixture() []dataset.Sample {
	return []dataset.Sample{
		{ID: "s1", AudioPath: "/dataset/s1.wav", Reference: "um", Category: dataset.CategoryIngles},
		{ID: "s2", AudioPath: "/dataset/s2.wav", Reference: "dois", Category: dataset.CategoryIngles},
		{ID: "s3", AudioPath: "/dataset/s3.wav", Reference: "três", Category: dataset.CategoryIngles},
	}
}

func TestPendingSamples_SkipsExisting(t *testing.T) {
	samples := samplesFixture()
	exists := func(path string) bool {
		return path == "/dataset/s1.wav" || path == "/dataset/s3.wav"
	}

	got := pendingSamples(samples, exists, "")

	if len(got) != 1 || got[0].ID != "s2" {
		t.Fatalf("pendingSamples = %v, want só s2", idsOf(got))
	}
}

func TestPendingSamples_RedoIncludesExistingSample(t *testing.T) {
	samples := samplesFixture()
	exists := func(path string) bool { return true } // todas já gravadas

	got := pendingSamples(samples, exists, "s2")

	if len(got) != 1 || got[0].ID != "s2" {
		t.Fatalf("pendingSamples com -redo s2 = %v, want só s2", idsOf(got))
	}
}

func TestPendingSamples_RedoDoesNotDuplicateAlreadyPending(t *testing.T) {
	samples := samplesFixture()
	// s1 e s3 já existem; s2 ainda não. -redo pede s1 de novo: s2 (que já
	// era pendente) não pode aparecer duas vezes na lista.
	exists := func(path string) bool {
		return path == "/dataset/s1.wav" || path == "/dataset/s3.wav"
	}

	got := pendingSamples(samples, exists, "s1")

	if len(got) != 2 {
		t.Fatalf("len(pendingSamples) = %d, want 2 (s1 via -redo, s2 pendente); got %v", len(got), idsOf(got))
	}
	if got[0].ID != "s1" || got[1].ID != "s2" {
		t.Fatalf("pendingSamples = %v, want [s1 s2] (ordem do manifesto)", idsOf(got))
	}
}

func TestPendingSamples_NoneWhenAllExistAndNoRedo(t *testing.T) {
	samples := samplesFixture()
	exists := func(path string) bool { return true }

	got := pendingSamples(samples, exists, "")

	if len(got) != 0 {
		t.Fatalf("pendingSamples = %v, want vazio", idsOf(got))
	}
}

func idsOf(samples []dataset.Sample) []string {
	ids := make([]string, len(samples))
	for i, s := range samples {
		ids[i] = s.ID
	}
	return ids
}

func TestSampleIDExists(t *testing.T) {
	samples := samplesFixture()

	if !sampleIDExists(samples, "s2") {
		t.Fatal("sampleIDExists(s2) = false, want true")
	}
	if sampleIDExists(samples, "s99") {
		t.Fatal("sampleIDExists(s99) = true, want false")
	}
}
