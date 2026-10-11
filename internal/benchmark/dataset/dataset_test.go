package dataset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("escrevendo manifesto de teste: %v", err)
	}
	return path
}

func TestLoadManifest_ValidManifest(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 16000)

	body := `{"samples":[
		{"id":"s1","audio":"a.wav","reference":"o Fisco cobrou o DCTFWeb","category":"contabilidade","entities":["Fisco","DCTFWeb"]}
	]}`
	path := writeManifest(t, dir, body)

	samples, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	got := samples[0]
	if got.ID != "s1" || got.Category != CategoryContabilidade {
		t.Fatalf("got %+v", got)
	}
	if !filepath.IsAbs(got.AudioPath) {
		t.Fatalf("AudioPath = %q, want caminho absoluto", got.AudioPath)
	}
	if len(got.Entities) != 2 {
		t.Fatalf("Entities = %v, want 2 itens", got.Entities)
	}
}

func TestLoadManifest_DerivesIDWhenMissing(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)

	body := `{"samples":[{"audio":"a.wav","reference":"oi","category":"ingles"}]}`
	path := writeManifest(t, dir, body)

	samples, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if samples[0].ID == "" {
		t.Fatal("ID derivado está vazio")
	}
}

func TestLoadManifest_MissingAudioFile(t *testing.T) {
	dir := t.TempDir()
	body := `{"samples":[{"id":"s1","audio":"nao-existe.wav","reference":"oi","category":"ingles"}]}`
	path := writeManifest(t, dir, body)

	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("want erro para áudio ausente, got nil")
	}
	if !strings.Contains(err.Error(), "s1") {
		t.Fatalf("erro não identifica a amostra: %v", err)
	}
}

func TestLoadManifest_EmptyReference(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)
	body := `{"samples":[{"id":"s1","audio":"a.wav","reference":"   ","category":"ingles"}]}`
	path := writeManifest(t, dir, body)

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("want erro para referência vazia, got nil")
	}
}

func TestLoadManifest_UnknownCategory(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)
	body := `{"samples":[{"id":"s1","audio":"a.wav","reference":"oi","category":"categoria-que-nao-existe"}]}`
	path := writeManifest(t, dir, body)

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("want erro para categoria desconhecida, got nil")
	}
}

func TestLoadManifest_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)
	body := `{"samples":[
		{"id":"dup","audio":"a.wav","reference":"oi","category":"ingles"},
		{"id":"dup","audio":"a.wav","reference":"oi de novo","category":"ingles"}
	]}`
	path := writeManifest(t, dir, body)

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("want erro para ID duplicado, got nil")
	}
}

func TestLoadManifest_EmptyManifest(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{"samples":[]}`)

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("want erro para manifesto sem amostras, got nil")
	}
}

func TestLoadManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{not json`)

	if _, err := LoadManifest(path); err == nil {
		t.Fatal("want erro para JSON inválido, got nil")
	}
}

func TestLoadManifest_MissingManifestFile(t *testing.T) {
	if _, err := LoadManifest("does-not-exist.json"); err == nil {
		t.Fatal("want erro para manifesto ausente, got nil")
	}
}

func TestLoadManifest_AllTwelveCategoriesAccepted(t *testing.T) {
	dir := t.TempDir()
	writeTestWAV(t, dir, "a.wav", 16000, 1600)

	var b strings.Builder
	b.WriteString(`{"samples":[`)
	for i, c := range Categories {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"s` + string(rune('a'+i)) + `","audio":"a.wav","reference":"oi","category":"` + string(c) + `"}`)
	}
	b.WriteString(`]}`)
	path := writeManifest(t, dir, b.String())

	samples, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(samples) != len(Categories) {
		t.Fatalf("len(samples) = %d, want %d", len(samples), len(Categories))
	}
}
