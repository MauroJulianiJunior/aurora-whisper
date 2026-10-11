package main

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

func TestFormatTable_HasHeaderColumns(t *testing.T) {
	out := formatTable(nil)
	for _, want := range []string{"transporte", "tamanho", "modo", "N", "p50", "p95", "max", "falhas"} {
		if !strings.Contains(out, want) {
			t.Errorf("cabecalho nao contem %q\n%s", want, out)
		}
	}
}

func TestFormatTable_RowsWithSamples(t *testing.T) {
	rows := []Row{
		{
			Transport: "pipe (stdin/stdout)",
			SizeLabel: "1s (32.044 B)",
			Mode:      "cold",
			Stats:     metrics.Aggregate([]float64{1e6, 2e6, 3e6}), // 1-3ms em ns
			Failed:    0,
		},
	}
	out := formatTable(rows)

	for _, want := range []string{"pipe (stdin/stdout)", "1s (32.044 B)", "cold"} {
		if !strings.Contains(out, want) {
			t.Errorf("linha nao contem %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "sem amostra") {
		t.Errorf("linha com N>0 nao deveria imprimir \"sem amostra\"\n%s", out)
	}
}

func TestFormatTable_ZeroSamplesPrintsSemAmostra(t *testing.T) {
	// N=0 (todas as repetições falharam) nunca deve imprimir um zero que
	// pareça uma latência real medida. SizeLabel evita conter a
	// substring "0s" por conta própria (ex.: "10s" conteria "0s" mesmo
	// sem bug nenhum) para o assert abaixo não dar falso positivo.
	rows := []Row{
		{Transport: "tcp (loopback, cru)", SizeLabel: "3s (96.044 B)", Mode: "warm", Failed: 5},
	}
	out := formatTable(rows)

	if !strings.Contains(out, "sem amostra") {
		t.Errorf("linha com N=0 deveria imprimir \"sem amostra\"\n%s", out)
	}
	if strings.Contains(out, "0s") {
		t.Errorf("linha com N=0 nao deveria imprimir uma duracao zero\n%s", out)
	}
}
