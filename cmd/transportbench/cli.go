package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// scopeNote é impresso em toda execução real do benchmark (nunca só no
// relatório escrito depois) — a omissão do named pipe do Windows precisa
// aparecer na PRÓPRIA saída do comando, porque quem só olha o terminal
// (sem ler o relatório à parte) tem que ver a mesma ressalva.
const scopeNote = "transportbench mede pipe (stdin/stdout), TCP cru em loopback e HTTP POST multipart em loopback.\n" +
	"Named pipe do Windows NAO entra nesta medicao: exigiria codigo Windows-only sem cobertura de\n" +
	"teste no Linux. Omissao DECLARADA (docs/benchmarks/candidatos-2026-09-17.md), nao esquecimento."

// run é o ponto de entrada testável do CLI. Dois caminhos:
//
//   - subcomandos ocultos echo-pipe / echo-tcp / echo-http: o próprio
//     binário reexecutado como processo de eco (ver echo_*.go). Quem
//     spawna isso é este mesmo binário, via os.Executable() + exec.Command
//     (transport_*.go) — nunca uma pessoa na linha de comando.
//   - nenhum subcomando reconhecido: roda a medição de verdade e imprime
//     a tabela.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "echo-pipe":
			return runEchoPipeSubcommand(os.Stdin, stdout)
		case "echo-tcp":
			return runEchoTCPSubcommand(os.Stdin, stdout)
		case "echo-http":
			return runEchoHTTPSubcommand(os.Stdin, stdout)
		}
	}

	fs := flag.NewFlagSet("transportbench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	coldReps := fs.Int("reps-cold", 0, "repeticoes do modo cold por (transporte x tamanho); 0 = default do autor deste benchmark (ver reps.go)")
	warmReps := fs.Int("reps-warm", 0, "repeticoes do modo warm por (transporte x tamanho); 0 = default do autor deste benchmark (ver reps.go)")
	controlReps := fs.Int("reps-control", 0, "repeticoes do controle in-process por tamanho; 0 = default do autor deste benchmark (ver reps.go)")
	if err := fs.Parse(args); err != nil {
		return 2 // flag já escreveu a mensagem em stderr
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "transportbench:", err)
		return 1
	}

	cReps := repsFor("cold", *coldReps)
	wReps := repsFor("warm", *warmReps)
	ctrlReps := repsFor("control", *controlReps)

	fmt.Fprintln(stdout, scopeNote)
	fmt.Fprintf(stdout, "\nrepeticoes: cold=%d warm=%d controle=%d por combinacao "+
		"(escolha do autor deste benchmark, marcada em reps.go — nao e medicao)\n\n", cReps, wReps, ctrlReps)

	started := time.Now()
	rows := runBenchmark(exePath, cReps, wReps, ctrlReps, stderr)
	fmt.Fprintf(stderr, "transportbench: medicao completa em %s\n", time.Since(started))

	fmt.Fprintln(stdout, formatTable(rows))
	return 0
}
