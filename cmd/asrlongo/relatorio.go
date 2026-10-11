package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// cabecalho é o que o relatório diz sobre como a medição foi feita.
type cabecalho struct {
	data             time.Time
	servidor, modelo string
	threads          int
	vocabularioRunas int
	hashDataset      string
	duracoes         []time.Duration
	porDuracao       int
	pausa            time.Duration
	longas, curtas   []variante
	nAmostrasCurtas  int
	intercaladaCom   string // o ID da fala curta das variantes intercaladas
	comando          string
}

// renderizar escreve o relatório Markdown. Só números e identificadores: os
// textos transcritos vão para o JSON ao lado (escreverJSON), e não para cá.
func renderizar(c cabecalho, longos, curtos []resultado) string {
	var b strings.Builder
	f := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	f("# Benchmark de áudio longo — %s\n\n", c.data.Format("2006-01-02 15:04"))
	f("Gerado por `cmd/asrlongo` (incidente de 01-out-2026, `docs/defeito-ditado-longo.md`).\n\n")
	f("- Servidor: `%s`\n- Modelo: `%s`\n- Threads: %d\n", c.servidor, c.modelo, c.threads)
	if c.vocabularioRunas > 0 {
		f("- Vocabulário (campo prompt): %d runas, do dicionário\n", c.vocabularioRunas)
	} else {
		f("- Vocabulário: nenhum\n")
	}
	f("- Dataset: hash `%s`\n", c.hashDataset)
	var ds []string
	for _, d := range c.duracoes {
		ds = append(ds, rotuloDuracao(d))
	}
	f("- Áudios longos: %s, %d por duração, amostras do dataset em ordem com %v de silêncio entre elas\n",
		strings.Join(ds, ", "), c.porDuracao, c.pausa)
	if c.intercaladaCom != "" {
		f("- Fala curta das variantes intercaladas: `%s`\n", c.intercaladaCom)
	}
	if c.comando != "" {
		f("- Comando: `%s`\n", c.comando)
	}
	f("\n")

	if len(c.longas) > 0 {
		f("## Variantes do áudio longo\n\n")
		for _, v := range c.longas {
			f("- `%s`: %s\n", v.nome, v.descricao)
		}
		f("\n## Resumo do áudio longo\n\n")
		resumo(&b, c.longas, longos, true)
		f("\n## Por áudio\n\n")
		porAudio(&b, longos)
		f("\n## A fala curta antes muda o texto do áudio longo?\n\n")
		comparacao(&b, longos)
	}

	if len(c.curtas) > 0 {
		f("\n## Fala curta: o 768 por requisição contra o -ac 750 medido\n\n")
		f("As %d amostras do dataset (todas abaixo de 12 s).\n\n", c.nAmostrasCurtas)
		for _, v := range c.curtas {
			f("- `%s`: %s\n", v.nome, v.descricao)
		}
		f("\n")
		resumo(&b, c.curtas, curtos, false)
	}

	f("\n## Como ler\n\n")
	f("- **WER** é contra a referência do dataset (as frases concatenadas). \"Com a guarda\" é depois de\n")
	f("  `internal/asr/repeticao` tirar os laços, como o Agent faz antes de resolver.\n")
	f("- **Laços** conta os áudios em que a guarda achou repetição. Zero é o esperado.\n")
	f("- **Tempo** é o que o provider mediu (`ASRResult.ProcessingTime`). **Estouraria o prazo** conta os\n")
	f("  áudios em que o tempo passou do prazo que o Agent dá (`dictation.PrazoDeTranscricao`).\n")
	f("- A comparação \"fala curta antes\" mostra se o K/V que sobra da requisição curta (768) muda o texto\n")
	f("  da longa (janela inteira, lida arredondada para 1536 com a flash attention). Texto diferente com o\n")
	f("  mesmo áudio e os mesmos parâmetros quer dizer que muda; aí a saída medida é desligar a flash\n")
	f("  attention (`-nfa`), que custou 22%% em 18-set-2026.\n")
	f("- **Razão tempo/áudio máxima** é medida com a máquina livre. O fator 0,75 do prazo é a pior razão do\n")
	f("  incidente (0,51, ainda com o -ac 750 e com o Meet aberto) com folga de 1,5×. Com a janela inteira o\n")
	f("  encoder custa mais por janela; se a razão máxima aqui passar de 0,5, o fator fica curto para a CPU\n")
	f("  disputada: rever o fator, ou medir uma vez com carga.\n")
	f("- **`prompt-fixo`** contra `por-duracao-intercalada`: as duas só diferem com vocabulário (`-dicionario`).\n")
	f("  WER menor no `prompt-fixo` quer dizer que o vocabulário saindo do contexto depois da primeira janela\n")
	f("  de 30 s pesa no ditado longo.\n")
	return b.String()
}

// resumo escreve a tabela de uma linha por variante.
func resumo(b *strings.Builder, vs []variante, rs []resultado, comPrazo bool) {
	if comPrazo {
		fmt.Fprintf(b, "| variante | áudios | falhas | WER médio | WER com a guarda | laços | tempo médio | razão tempo/áudio (média / máx) | estouraria o prazo |\n")
		fmt.Fprintf(b, "|---|---|---|---|---|---|---|---|---|\n")
	} else {
		fmt.Fprintf(b, "| variante | amostras | falhas | WER médio | tempo p50 | tempo p95 | tempo máx |\n")
		fmt.Fprintf(b, "|---|---|---|---|---|---|---|\n")
	}
	for _, v := range vs {
		var wers, wersG, tempos, razoes []float64
		n, falhas, lacos, estouros := 0, 0, 0, 0
		for _, r := range rs {
			if r.variante != v.nome {
				continue
			}
			n++
			if r.err != nil {
				falhas++
				continue
			}
			if r.werOK {
				wers = append(wers, r.wer)
				wersG = append(wersG, r.werGuarda)
			}
			tempos = append(tempos, r.tempo.Seconds())
			if r.duracao > 0 {
				razoes = append(razoes, r.tempo.Seconds()/r.duracao.Seconds())
			}
			if r.lacos > 0 {
				lacos++
			}
			if r.estouraPrazo() {
				estouros++
			}
		}
		w, wg, t, rz := metrics.Aggregate(wers), metrics.Aggregate(wersG), metrics.Aggregate(tempos), metrics.Aggregate(razoes)
		if comPrazo {
			fmt.Fprintf(b, "| `%s` | %d | %d | %s | %s | %d | %s | %s / %s | %d |\n",
				v.nome, n, falhas, num(w, w.Mean, 3), num(wg, wg.Mean, 3), lacos,
				seg(t, t.Mean), num(rz, rz.Mean, 2), num(rz, rz.Max, 2), estouros)
		} else {
			fmt.Fprintf(b, "| `%s` | %d | %d | %s | %s | %s | %s |\n",
				v.nome, n, falhas, num(w, w.Mean, 3), seg(t, t.P50), seg(t, t.P95), seg(t, t.Max))
		}
	}
}

// porAudio escreve uma linha por áudio e variante.
func porAudio(b *strings.Builder, rs []resultado) {
	fmt.Fprintf(b, "| áudio | duração | variante | audio_ctx | tempo | razão | WER | WER com a guarda | laços (palavras tiradas) | runas | erro |\n")
	fmt.Fprintf(b, "|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rs {
		if r.err != nil {
			fmt.Fprintf(b, "| %s | %.1f s | `%s` | %s | — | — | — | — | — | — | %s |\n",
				r.audio, r.duracao.Seconds(), r.variante, r.audioCtx, umaLinha(r.err.Error()))
			continue
		}
		wer, werG := "—", "—"
		if r.werOK {
			wer, werG = fmt.Sprintf("%.3f", r.wer), fmt.Sprintf("%.3f", r.werGuarda)
		}
		fmt.Fprintf(b, "| %s | %.1f s | `%s` | %s | %.2f s | %.2f | %s | %s | %d (%d) | %d | |\n",
			r.audio, r.duracao.Seconds(), r.variante, r.audioCtx, r.tempo.Seconds(),
			r.tempo.Seconds()/r.duracao.Seconds(), wer, werG, r.lacos, r.palavrasRemovidas, r.runas)
	}
}

// comparacao diz, áudio por áudio, se o texto da variante intercalada é o
// mesmo da variante "fria" (servidor só com áudio longo).
func comparacao(b *strings.Builder, rs []resultado) {
	pares := []struct{ rotulo, fria, quente string }{
		{"Com flash attention", "por-duracao", "por-duracao-intercalada"},
		{"Sem flash attention (`-nfa`)", "nfa-por-duracao", "nfa-por-duracao-intercalada"},
	}
	for _, p := range pares {
		fmt.Fprintf(b, "**%s** (`%s` contra `%s`): ", p.rotulo, p.fria, p.quente)
		iguais, diferentes, ausentes := divergencias(rs, p.fria, p.quente)
		if iguais+diferentes == 0 {
			fmt.Fprintf(b, "não medido: precisa das duas variantes, sem falha.\n\n")
			continue
		}
		fmt.Fprintf(b, "%d de %d áudios com o texto igual; %d diferentes", iguais, iguais+diferentes, diferentes)
		if ausentes > 0 {
			fmt.Fprintf(b, " (%d sem comparação, por falha)", ausentes)
		}
		fmt.Fprintf(b, ".\n\n")
		if diferentes > 0 {
			fmt.Fprintf(b, "O texto do áudio longo depende da requisição anterior. Ver \"Como ler\".\n\n")
		} else {
			fmt.Fprintf(b, "Nenhuma diferença: nesta medição, a requisição anterior não mudou o texto.\n\n")
		}
	}
}

// divergencias compara os textos de duas variantes para o mesmo áudio.
func divergencias(rs []resultado, fria, quente string) (iguais, diferentes, ausentes int) {
	textos := map[string]map[string]*resultado{}
	for i := range rs {
		r := &rs[i]
		if r.variante != fria && r.variante != quente {
			continue
		}
		if textos[r.audio] == nil {
			textos[r.audio] = map[string]*resultado{}
		}
		textos[r.audio][r.variante] = r
	}
	for _, porVariante := range textos {
		a, b := porVariante[fria], porVariante[quente]
		switch {
		case a == nil || b == nil || a.err != nil || b.err != nil:
			ausentes++
		case a.texto == b.texto:
			iguais++
		default:
			diferentes++
		}
	}
	return iguais, diferentes, ausentes
}

func num(s metrics.Stats, v float64, casas int) string {
	if s.N == 0 {
		return "—"
	}
	return fmt.Sprintf("%.*f", casas, v)
}

func seg(s metrics.Stats, v float64) string {
	if s.N == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f s", v)
}

func umaLinha(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}
