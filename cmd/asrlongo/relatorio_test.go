package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRenderizar_NumerosSimTextosNao(t *testing.T) {
	const segredo = "texto transcrito que não pode ir ao relatório"
	longos := []resultado{
		{variante: "ac750", audio: "longo-60s-1", duracao: 60 * time.Second, audioCtx: "750 (-ac)", tempo: 18 * time.Second,
			wer: 0.9, werGuarda: 0.6, werOK: true, lacos: 1, palavrasRemovidas: 132, runas: 900, texto: segredo},
		{variante: "por-duracao", audio: "longo-60s-1", duracao: 60 * time.Second, audioCtx: "0", tempo: 6 * time.Second,
			wer: 0.2, werGuarda: 0.2, werOK: true, texto: segredo},
		{variante: "por-duracao-intercalada", audio: "longo-60s-1", duracao: 60 * time.Second, audioCtx: "0", tempo: 6 * time.Second,
			wer: 0.25, werGuarda: 0.25, werOK: true, texto: segredo + " diferente"},
		{variante: "entropia-2.8", audio: "longo-60s-1", duracao: 60 * time.Second, audioCtx: "0", err: errors.New("servidor caiu\ncom | barra")},
	}
	curtos := []resultado{
		{variante: "ac750", audio: "geral-01", duracao: 4 * time.Second, tempo: 500 * time.Millisecond, wer: 0.1, werOK: true},
		{variante: "por-duracao", audio: "geral-01", duracao: 4 * time.Second, tempo: 520 * time.Millisecond, wer: 0.1, werOK: true},
	}
	md := renderizar(cabecalho{
		data: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), servidor: "srv.exe", modelo: "m.bin", threads: 12,
		duracoes: []time.Duration{60 * time.Second}, porDuracao: 1, pausa: 500 * time.Millisecond,
		longas: variantesLongas, curtas: variantesCurtas, nAmostrasCurtas: 1, intercaladaCom: "geral-01",
	}, longos, curtos)

	if strings.Contains(md, segredo) {
		t.Error("o texto transcrito foi para o relatório")
	}
	for _, trecho := range []string{
		"# Benchmark de áudio longo — 2026-10-02 09:00",
		// ac750: 1 áudio, laço, 18 s de 60 s = 0,30, estoura o prazo de 50 s? não: 18 < 50.
		"| `ac750` | 1 | 0 | 0.900 | 0.600 | 1 | 18.00 s | 0.30 / 0.30 | 0 |",
		"| `entropia-2.8` | 1 | 1 | — | — | 0 | — | — / — | 0 |",
		"| longo-60s-1 | 60.0 s | `ac750` | 750 (-ac) | 18.00 s | 0.30 | 0.900 | 0.600 | 1 (132) | 900 | |",
		"servidor caiu com / barra",
		"0 de 1 áudios com o texto igual; 1 diferentes",
		"O texto do áudio longo depende da requisição anterior.",
		"**Sem flash attention (`-nfa`)** (`nfa-por-duracao` contra `nfa-por-duracao-intercalada`): não medido",
		"| `por-duracao` | 1 | 0 | 0.100 | 0.52 s | 0.52 s | 0.52 s |",
		"geral-01",
	} {
		if !strings.Contains(md, trecho) {
			t.Errorf("o relatório não tem %q\n%s", trecho, md)
		}
	}
}

func TestRenderizar_PrazoEstouradoESemComparacao(t *testing.T) {
	longos := []resultado{
		// 37 s de áudio têm 32,75 s de prazo; 40 s estoura.
		{variante: "ac750", audio: "longo-37s-1", duracao: 37 * time.Second, tempo: 40 * time.Second, werOK: true},
	}
	md := renderizar(cabecalho{longas: variantesLongas[:1]}, longos, nil)
	if !strings.Contains(md, "| `ac750` | 1 | 0 | 0.000 | 0.000 | 0 | 40.00 s | 1.08 / 1.08 | 1 |") {
		t.Errorf("o resumo não contou o prazo estourado:\n%s", md)
	}
	if !strings.Contains(md, "não medido") {
		t.Errorf("sem as duas variantes, a comparação tinha de dizer que não mediu:\n%s", md)
	}
	if strings.Contains(md, "Fala curta: o 768") {
		t.Error("a seção das curtas apareceu sem medição de curtas")
	}
}
