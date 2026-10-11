package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMockCandidate_Cold_WithFixture(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "a.wav")
	if err := os.WriteFile(audio+".transcript.txt", []byte("o Fisco cobrou o DCTFWeb"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runMockCandidate([]string{"cold", audio}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "o Fisco cobrou o DCTFWeb" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunMockCandidate_Cold_WithoutFixtureIsEmpty(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "sem-fixture.wav")

	var stdout, stderr bytes.Buffer
	code := runMockCandidate([]string{"cold", audio}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("stdout = %q, want vazio (sem fixture)", stdout.String())
	}
}

func TestRunMockCandidate_Warm_MultipleLines(t *testing.T) {
	dir := t.TempDir()
	a1 := filepath.Join(dir, "a1.wav")
	a2 := filepath.Join(dir, "a2.wav")
	os.WriteFile(a1+".transcript.txt", []byte("um"), 0o644)
	os.WriteFile(a2+".transcript.txt", []byte("dois"), 0o644)

	stdin := strings.NewReader(a1 + "\n" + a2 + "\n")
	var stdout, stderr bytes.Buffer
	code := runMockCandidate([]string{"warm"}, stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "um" || lines[1] != "dois" {
		t.Fatalf("linhas = %v, want [um dois]", lines)
	}
}

func TestRunMockCandidate_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runMockCandidate([]string{"nao-existe"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("want código de saída != 0 para subcomando desconhecido")
	}
}

func TestRunMockCandidate_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runMockCandidate(nil, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("want código de saída != 0 sem argumentos")
	}
}
