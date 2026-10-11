package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/harness"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/report"
)

// run é a lógica do CLI separada de main() para ser testável sem exec
// (o subcomando mockcandidate, que PRECISA ser um processo separado para
// os testes de subprocess do harness, continua indo por os.Exit em
// main.go/mockcandidate.go — só o caminho "benchmark de verdade" passa
// por aqui).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "mockcandidate" {
		return runMockCandidate(args[1:], os.Stdin, stdout, stderr)
	}

	fs := flag.NewFlagSet("asrbench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", "", "caminho do manifesto do dataset (JSON, internal/benchmark/dataset)")
	candidatesPath := fs.String("candidates", "", "caminho da configuração de candidatos (JSON, ADR-012)")
	outPath := fs.String("out", "", "caminho do relatório Markdown de saída (docs/ai-spec/19-eval-plan.md)")
	hardware := fs.String("hardware", "", "descrição do hardware, para o relatório")
	osDesc := fs.String("os", "", "descrição do SO, para o relatório")
	timeout := fs.Duration("timeout", 0, "timeout global (sobrescreve o timeout de cada candidato, se > 0)")

	if err := fs.Parse(args); err != nil {
		return 2 // flag já escreveu a mensagem em stderr
	}

	if *manifestPath == "" || *candidatesPath == "" || *outPath == "" {
		fmt.Fprintln(stderr, "asrbench: -manifest, -candidates e -out são obrigatórios")
		fs.Usage()
		return 2
	}

	samples, err := dataset.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, "asrbench:", err)
		return 1
	}

	candidates, err := loadCandidates(*candidatesPath)
	if err != nil {
		fmt.Fprintln(stderr, "asrbench:", err)
		return 1
	}
	if *timeout > 0 {
		for i := range candidates {
			candidates[i].Timeout = *timeout
		}
	}

	result, err := harness.Run(context.Background(), samples, candidates)
	if err != nil {
		fmt.Fprintln(stderr, "asrbench:", err)
		return 1
	}

	env := report.Environment{Hardware: *hardware, OS: *osDesc}
	markdown := report.Render(result, env, report.DefaultGates, time.Now())

	if err := os.WriteFile(*outPath, []byte(markdown), 0o644); err != nil {
		fmt.Fprintln(stderr, "asrbench: escrevendo relatório:", err)
		return 1
	}

	fmt.Fprintf(stdout, "asrbench: relatório escrito em %s (%d amostras, %d candidatos)\n",
		*outPath, len(samples), len(candidates))
	return 0
}
