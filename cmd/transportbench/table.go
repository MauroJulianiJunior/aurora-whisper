package main

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// Row é uma linha da tabela final: um (transporte, tamanho, modo) e as
// estatísticas de latência de ida-e-volta medidas para essa combinação
// (internal/benchmark/metrics.Aggregate, reaproveitado em vez de
// estatística nova — mesma convenção de internal/benchmark/report).
type Row struct {
	Transport string        // ex.: "pipe (stdin/stdout)", "in-process (controle)"
	SizeLabel string        // ex.: "1s (32.044 B)"
	Mode      string        // "cold" | "warm" | "controle"
	Stats     metrics.Stats // amostras em nanossegundos (float64(time.Duration))
	Failed    int           // repetições descartadas por eco incorreto ou erro de transporte
}

// formatTable renderiza rows como uma tabela de texto alinhada
// (text/tabwriter, stdlib — sem dependência nova). Lógica pura: não olha
// relógio, não faz I/O, só formata dados já medidos — testável em
// qualquer SO sem build tag.
func formatTable(rows []Row) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 2, 2, ' ', 0)

	fmt.Fprintln(tw, "transporte\ttamanho\tmodo\tN\tp50\tp95\tmax\tfalhas")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%d\n",
			r.Transport, r.SizeLabel, r.Mode, r.Stats.N,
			formatStat(r.Stats, "p50"), formatStat(r.Stats, "p95"), formatStat(r.Stats, "max"),
			r.Failed)
	}

	_ = tw.Flush()
	return b.String()
}

// formatStat devolve which ("p50"/"p95"/"max") de s formatado como
// time.Duration legível (ex.: "1.2ms") — a mesma convenção de
// internal/benchmark/report.formatDurationStat. "sem amostra" quando
// N==0: nunca imprime um zero que pareça uma medição real de latência
// zero.
func formatStat(s metrics.Stats, which string) string {
	if s.N == 0 {
		return "sem amostra"
	}
	var v float64
	switch which {
	case "p50":
		v = s.P50
	case "p95":
		v = s.P95
	case "max":
		v = s.Max
	}
	return time.Duration(v).String()
}
