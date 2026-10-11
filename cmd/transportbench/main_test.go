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

// TestMain compila o binário transportbench REAL uma única vez (go
// build) antes da suíte, para o teste de ponta a ponta abaixo rodar o
// binário de verdade como subprocesso — exatamente como uma pessoa
// rodaria depois, com repetições pequenas para o teste ficar rápido.
// Mesmo padrão de cmd/asrbench/main_test.go.
var transportbenchBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "transportbench-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: MkdirTemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	name := "transportbench"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	transportbenchBin = filepath.Join(dir, name)

	build := exec.Command("go", "build", "-o", transportbenchBin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: go build ./cmd/transportbench falhou: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// TestEndToEnd_RunsAllTransportsAndPrintsTable roda o binário de verdade
// com repetições mínimas (rápido o bastante para a suíte normal) e
// verifica que a saída cobre: o aviso de escopo (named pipe do Windows
// fora), as repetições configuradas, os três transportes, os três
// tamanhos, cold/warm/controle, e nenhuma falha de eco — o benchmark
// inteiro passando pelos subcomandos ocultos echo-pipe/echo-tcp/echo-http
// como subprocessos de verdade.
func TestEndToEnd_RunsAllTransportsAndPrintsTable(t *testing.T) {
	cmd := exec.Command(transportbenchBin,
		"-reps-cold", "2",
		"-reps-warm", "3",
		"-reps-control", "5",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("transportbench falhou: %v\n%s", err, out)
	}
	output := string(out)

	for _, want := range []string{
		"Named pipe do Windows NAO entra",
		"cold=2 warm=3 controle=5",
		"pipe (stdin/stdout)",
		"tcp (loopback, cru)",
		"http POST multipart (loopback)",
		"in-process (controle)",
		"1s (32.044 B)",
		"3s (96.044 B)",
		"10s (320.044 B)",
		"cold",
		"warm",
		"controle",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("saida nao contem %q\n--- saida completa ---\n%s", want, output)
		}
	}

	// Nenhuma repeticao deveria falhar num echo local em loopback: uma
	// falha aqui teria de aparecer como "falhas" != 0 numa coluna, o que
	// este teste nao verifica campo a campo (a saida e texto formatado),
	// mas o exit code != 0 acima ja teria capturado erro de transporte.
}
