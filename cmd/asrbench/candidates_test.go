package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCandidatesFile(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "candidates.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("escrevendo config de candidatos: %v", err)
	}
	return path
}

func TestLoadCandidates_Valid(t *testing.T) {
	dir := t.TempDir()
	path := writeCandidatesFile(t, dir, `{"candidates":[
		{"name":"whisper","executable":"whisper-cli","coldArgs":["-f","{{audio}}"],"warmArgs":["--server"],"timeout":"10s","startupTimeout":"5s"}
	]}`)

	got, err := loadCandidates(path)
	if err != nil {
		t.Fatalf("loadCandidates: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	c := got[0]
	if c.Name != "whisper" || c.Executable != "whisper-cli" {
		t.Fatalf("got %+v", c)
	}
	if c.Timeout != 10*time.Second || c.StartupTimeout != 5*time.Second {
		t.Fatalf("timeouts = %v/%v, want 10s/5s", c.Timeout, c.StartupTimeout)
	}
}

func TestLoadCandidates_MissingExecutable(t *testing.T) {
	dir := t.TempDir()
	path := writeCandidatesFile(t, dir, `{"candidates":[{"name":"x","coldArgs":["{{audio}}"]}]}`)
	if _, err := loadCandidates(path); err == nil {
		t.Fatal("want erro para executable ausente, got nil")
	}
}

func TestLoadCandidates_MissingColdArgs(t *testing.T) {
	dir := t.TempDir()
	path := writeCandidatesFile(t, dir, `{"candidates":[{"name":"x","executable":"bin"}]}`)
	if _, err := loadCandidates(path); err == nil {
		t.Fatal("want erro para coldArgs ausente, got nil")
	}
}

func TestLoadCandidates_InvalidTimeout(t *testing.T) {
	dir := t.TempDir()
	path := writeCandidatesFile(t, dir, `{"candidates":[{"name":"x","executable":"bin","coldArgs":["{{audio}}"],"timeout":"not-a-duration"}]}`)
	if _, err := loadCandidates(path); err == nil {
		t.Fatal("want erro para timeout inválido, got nil")
	}
}

func TestLoadCandidates_EmptyList(t *testing.T) {
	dir := t.TempDir()
	path := writeCandidatesFile(t, dir, `{"candidates":[]}`)
	if _, err := loadCandidates(path); err == nil {
		t.Fatal("want erro para lista vazia, got nil")
	}
}

func TestLoadCandidates_MissingFile(t *testing.T) {
	if _, err := loadCandidates("does-not-exist.json"); err == nil {
		t.Fatal("want erro para arquivo ausente, got nil")
	}
}

func TestLoadCandidates_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeCandidatesFile(t, dir, `{not json`)
	if _, err := loadCandidates(path); err == nil {
		t.Fatal("want erro para JSON inválido, got nil")
	}
}
