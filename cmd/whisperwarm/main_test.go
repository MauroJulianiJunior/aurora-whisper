package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_MissingRequiredFlags_ExitsWithUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-lang", "pt"}, strings.NewReader(""), &stdout, &stderr)

	if code != 2 {
		t.Fatalf("code = %d, want 2 (erro de uso)", code)
	}
	if !strings.Contains(stderr.String(), "-server") || !strings.Contains(stderr.String(), "-model") {
		t.Fatalf("stderr = %q, want mensagem citando -server e -model como obrigatórios", stderr.String())
	}
}

func TestRun_ServerBinaryDoesNotExist_ExitsWithStartupError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(
		[]string{"-server", "caminho/que/nao/existe/whisper-server.exe", "-model", "modelo.bin", "-ready-timeout", "200ms"},
		strings.NewReader(""), &stdout, &stderr,
	)

	if code != 1 {
		t.Fatalf("code = %d, want 1 (falha ao iniciar whisper-server)", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want vazio quando o servidor nunca sobe", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr vazio, want mensagem de erro sobre falha ao iniciar o whisper-server")
	}
}
