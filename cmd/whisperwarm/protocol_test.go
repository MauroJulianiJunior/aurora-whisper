package main

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// runProtocolWithTimeout chama runProtocol numa goroutine e falha o
// teste se ela não retornar dentro de 3s — prova estrutural de "sem
// travar" para os casos de entrada malformada e EOF no meio, em vez de
// só confiar na leitura visual do código.
func runProtocolWithTimeout(t *testing.T, in, out, errOut *bytes.Buffer, transcribe func(string) (string, error)) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- runProtocol(in, out, errOut, transcribe) }()

	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("runProtocol travou: não retornou dentro de 3s")
		return nil // inalcançável, t.Fatal já encerra a goroutine de teste
	}
}

func TestRunProtocol_WellFormedLine(t *testing.T) {
	calls := 0
	transcribe := func(path string) (string, error) {
		calls++
		if path != "audio.wav" {
			t.Errorf("transcribe recebeu %q, want %q", path, "audio.wav")
		}
		return "resultado da transcricao", nil
	}

	in := bytes.NewBufferString("audio.wav\n")
	var out, errOut bytes.Buffer

	if err := runProtocolWithTimeout(t, in, &out, &errOut, transcribe); err != nil {
		t.Fatalf("runProtocol: %v", err)
	}
	if calls != 1 {
		t.Fatalf("transcribe foi chamado %d vezes, want 1", calls)
	}
	if got := out.String(); got != "resultado da transcricao\n" {
		t.Fatalf("stdout = %q, want %q", got, "resultado da transcricao\n")
	}
}

func TestRunProtocol_EmptyInput_ReturnsImmediatelyWithoutCallingTranscribe(t *testing.T) {
	called := false
	transcribe := func(string) (string, error) { called = true; return "", nil }

	in := bytes.NewBufferString("")
	var out, errOut bytes.Buffer

	if err := runProtocolWithTimeout(t, in, &out, &errOut, transcribe); err != nil {
		t.Fatalf("runProtocol: %v", err)
	}
	if called {
		t.Fatal("transcribe foi chamado para entrada vazia (EOF imediato)")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want vazio", out.String())
	}
}

func TestRunProtocol_MalformedLine_EmptyLineNeverCallsTranscribeAndKeepsProtocolInSync(t *testing.T) {
	var got []string
	transcribe := func(path string) (string, error) {
		got = append(got, path)
		return "ok:" + path, nil
	}

	// Linha em branco no meio de duas linhas válidas: entrada
	// malformada que não pode travar nem desalinhar as respostas
	// seguintes.
	in := bytes.NewBufferString("primeiro.wav\n\nsegundo.wav\n")
	var out, errOut bytes.Buffer

	if err := runProtocolWithTimeout(t, in, &out, &errOut, transcribe); err != nil {
		t.Fatalf("runProtocol: %v", err)
	}

	if len(got) != 2 || got[0] != "primeiro.wav" || got[1] != "segundo.wav" {
		t.Fatalf("transcribe recebeu %v, want [primeiro.wav segundo.wav] (linha vazia não deve virar chamada)", got)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stdout tem %d linhas, want 3 (uma por linha de entrada, inclusive a vazia): %q", len(lines), out.String())
	}
	if lines[0] != "ok:primeiro.wav" {
		t.Fatalf("lines[0] = %q, want %q", lines[0], "ok:primeiro.wav")
	}
	if !strings.HasPrefix(lines[1], errorLinePrefix) {
		t.Fatalf("lines[1] = %q, want prefixo de erro %q para a linha vazia", lines[1], errorLinePrefix)
	}
	if lines[2] != "ok:segundo.wav" {
		t.Fatalf("lines[2] = %q, want %q (deve continuar processando depois do erro)", lines[2], "ok:segundo.wav")
	}
}

func TestRunProtocol_TranscribeError_WritesErrorLineAndKeepsGoing(t *testing.T) {
	call := 0
	transcribe := func(path string) (string, error) {
		call++
		if call == 1 {
			return "", errors.New("whisper-server: falha simulada")
		}
		return "sucesso:" + path, nil
	}

	in := bytes.NewBufferString("falha.wav\nsucesso.wav\n")
	var out, errOut bytes.Buffer

	if err := runProtocolWithTimeout(t, in, &out, &errOut, transcribe); err != nil {
		t.Fatalf("runProtocol: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout tem %d linhas, want 2 (uma por requisição, mesmo a que falhou): %q", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], errorLinePrefix) {
		t.Fatalf("lines[0] = %q, want prefixo de erro", lines[0])
	}
	if lines[1] != "sucesso:sucesso.wav" {
		t.Fatalf("lines[1] = %q, want %q — a falha na primeira linha não pode impedir a segunda", lines[1], "sucesso:sucesso.wav")
	}
	if !strings.Contains(errOut.String(), "falha simulada") {
		t.Fatalf("stderr = %q, want mensagem completa do erro (\"falha simulada\")", errOut.String())
	}
}

func TestRunProtocol_EOFMidStream_NoTrailingNewlineOnLastLine(t *testing.T) {
	transcribe := func(path string) (string, error) { return "ok:" + path, nil }

	// Última linha sem "\n" final — simula EOF chegando no meio de uma
	// linha em vez de logo depois de um separador. bufio.Scanner
	// entrega essa linha como o último token válido; runProtocol não
	// pode travar nem perder essa última requisição.
	in := bytes.NewBufferString("primeiro.wav\nultimo-sem-newline.wav")
	var out, errOut bytes.Buffer

	if err := runProtocolWithTimeout(t, in, &out, &errOut, transcribe); err != nil {
		t.Fatalf("runProtocol: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout tem %d linhas, want 2: %q", len(lines), out.String())
	}
	if lines[1] != "ok:ultimo-sem-newline.wav" {
		t.Fatalf("lines[1] = %q, want %q", lines[1], "ok:ultimo-sem-newline.wav")
	}
}

func TestRunProtocol_OneOutputLinePerInputLine_EvenWithManyFailures(t *testing.T) {
	// Reforça a invariante central do protocolo (1 linha lida => 1
	// linha escrita) com um volume maior e falhas intercaladas, para
	// pegar qualquer desalinhamento que só apareça depois de várias
	// iterações.
	const n = 50
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString("linha" + strconv.Itoa(i) + ".wav\n")
	}

	transcribe := func(path string) (string, error) {
		if strings.HasSuffix(path, "3.wav") { // falha ~1 a cada 10
			return "", errors.New("falha")
		}
		return "ok", nil
	}

	in := bytes.NewBufferString(sb.String())
	var out, errOut bytes.Buffer

	if err := runProtocolWithTimeout(t, in, &out, &errOut, transcribe); err != nil {
		t.Fatalf("runProtocol: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("stdout tem %d linhas, want %d (uma por linha de entrada, sem exceção)", len(lines), n)
	}
}
