// Package report renderiza um harness.BenchmarkResult como Markdown, no
// formato exigido por docs/ai-spec/19-eval-plan.md ("Relatório") e
// docs/adr/ADR-012-asr-local-como-processo-externo.md: hardware, SO,
// runtime/model/version, quantização, config, hash do dataset, todas as
// métricas, memória, latência, e um espaço para a decisão técnica que
// fica EM BRANCO — o benchmark não decide sozinho (ADR-012).
package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/harness"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// Environment é o contexto de execução do benchmark que
// docs/ai-spec/19-eval-plan.md exige registrar e que NENHUMA parte deste
// pacote pode medir sozinha (hardware e SO variam por máquina, e
// runtime/model/version/quantização são metadados do candidato que só
// quem configurou o benchmark sabe) — por isso é passado de fora, nunca
// inferido.
type Environment struct {
	Hardware string // ex.: "Ryzen 7 5800X, 32GB RAM"
	OS       string // ex.: "Windows 11 Pro 10.0.26200"
	// CandidateNotes mapeia o nome do candidato (harness.CandidateReport.Name)
	// para uma descrição livre de runtime/model/version/quantização/config
	// — o que 19-eval-plan.md pede por candidato. Um candidato sem entrada
	// aqui ainda aparece no relatório, só sem essa linha de metadados.
	CandidateNotes map[string]string
}

// Gates são os limiares de docs/ai-spec/19-eval-plan.md ("Gates local")
// contra os quais o relatório confronta o medido. Ficam parametrizados
// (não hardcoded) para o relatório poder registrar exatamente contra o
// que comparou, e para testes poderem usar limiares triviais sem
// depender do valor de produto.
type Gates struct {
	RSSBytesMax       int64         // "RSS alvo ≤~1 GB"
	EntityAccuracyMin float64       // "Entity Accuracy ≥95% após dictionary"
	P50Max            time.Duration // "p50 ≤500 ms"
	P95Max            time.Duration // "p95 ≤1.2 s"
	MinStableSamples  int           // "100 execuções estáveis"
}

// DefaultGates são os valores de docs/ai-spec/19-eval-plan.md.
var DefaultGates = Gates{
	RSSBytesMax:       1 << 30, // ~1 GB
	EntityAccuracyMin: 0.95,
	P50Max:            500 * time.Millisecond,
	P95Max:            1200 * time.Millisecond,
	MinStableSamples:  100,
}

// Render produz o Markdown completo do relatório de benchmark.
func Render(result harness.BenchmarkResult, env Environment, gates Gates, generatedAt time.Time) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Benchmark de ASR — %s\n\n", generatedAt.Format("2006-01-02"))

	writeEnvironment(&b, env)
	writeDataset(&b, result)

	for _, cr := range result.Candidates {
		writeCandidate(&b, cr, env, gates)
	}

	writeDecisionSection(&b)

	return b.String()
}

func writeEnvironment(b *strings.Builder, env Environment) {
	b.WriteString("## Ambiente\n\n")
	fmt.Fprintf(b, "- Hardware: %s\n", orNotSet(env.Hardware))
	fmt.Fprintf(b, "- SO: %s\n", orNotSet(env.OS))
	b.WriteString("\n")
}

func orNotSet(s string) string {
	if strings.TrimSpace(s) == "" {
		return "não informado"
	}
	return s
}

func writeDataset(b *strings.Builder, result harness.BenchmarkResult) {
	b.WriteString("## Dataset\n\n")
	fmt.Fprintf(b, "- N total de amostras: %d\n", len(result.Samples))
	fmt.Fprintf(b, "- Hash do dataset (sha256): `%s`\n", result.DatasetHash)
	b.WriteString("\n### Amostras por categoria\n\n")
	b.WriteString("| Categoria | N |\n|---|---|\n")

	counts := make(map[dataset.Category]int)
	for _, s := range result.Samples {
		counts[s.Category]++
	}
	for _, c := range dataset.Categories {
		fmt.Fprintf(b, "| %s | %d |\n", c, counts[c])
	}
	b.WriteString("\n")
}

func writeCandidate(b *strings.Builder, cr harness.CandidateReport, env Environment, gates Gates) {
	fmt.Fprintf(b, "## Candidato: %s\n\n", cr.Name)
	if note, ok := env.CandidateNotes[cr.Name]; ok && note != "" {
		fmt.Fprintf(b, "%s\n\n", note)
	}

	writeModeSection(b, "Cold (processo novo por transcrição)", cr.Cold)
	writeModeSection(b, "Warm (processo residente, modelo carregado)", cr.Warm)

	writeGateTable(b, cr, gates)

	if cr.Cold.Supported && !cr.Warm.Supported {
		b.WriteString("> **Nota (ADR-012):** este candidato só foi medido em modo cold. " +
			"Sem medição warm, não é possível saber se ele cumpriria os gates de latência " +
			"em uso contínuo — reportar isso como \"reprovado\" ou \"aprovado\" seria inventar " +
			"uma medição que não existe.\n\n")
	} else if cr.Warm.Supported && isTrue(meetsP50(cr.Warm, gates)) && !isTrue(meetsP50(cr.Cold, gates)) {
		b.WriteString("> **Decisão de arquitetura necessária (ADR-012):** só o modo warm cumpre " +
			"a meta de p50 deste candidato. Isso exige manter um processo residente com " +
			"protocolo próprio no Sprint 4, em vez de iniciar um processo por transcrição — " +
			"não é uma falha deste sprint, é o benchmark cumprindo sua função.\n\n")
	}
}

func writeModeSection(b *strings.Builder, title string, m harness.ModeReport) {
	fmt.Fprintf(b, "### %s\n\n", title)
	if !m.Supported {
		b.WriteString("Não medido (candidato não declara suporte a este modo).\n\n")
		return
	}

	fmt.Fprintf(b, "- Amostras tentadas: %d\n", m.Attempted)
	fmt.Fprintf(b, "- Falhas: %d (das quais timeout: %d)\n", m.Failed, m.TimedOut)
	fmt.Fprintf(b, "- Latência p50: %s (N=%d)\n", formatDurationStat(m.Latency, statP50), m.Latency.N)
	fmt.Fprintf(b, "- Latência p95: %s (N=%d)\n", formatDurationStat(m.Latency, statP95), m.Latency.N)
	fmt.Fprintf(b, "- Latência média: %s (N=%d)\n", formatDurationStat(m.Latency, statMean), m.Latency.N)
	fmt.Fprintf(b, "- RTF médio: %s (N=%d)\n", formatFloatOrNA(m.RTF.Mean, m.RTF.N), m.RTF.N)
	if m.RSSSamples > 0 {
		fmt.Fprintf(b, "- RSS de pico: máximo %s, média %s (N=%d de %d tentadas). "+
			"O gate compara com o MÁXIMO — a média esconde a pior chamada.\n",
			formatBytes(m.RSS.Max), formatBytes(m.RSS.Mean), m.RSSSamples, m.Attempted)
	} else {
		b.WriteString("- RSS de pico: não medido neste ambiente (N=0)\n")
	}
	fmt.Fprintf(b, "- WER médio: %s (N=%d)\n", formatFloatOrNA(m.WER.Mean, m.WER.N), m.WER.N)
	fmt.Fprintf(b, "- CER médio: %s (N=%d)\n", formatFloatOrNA(m.CER.Mean, m.CER.N), m.CER.N)
	if m.WERFailed > 0 || m.CERFailed > 0 {
		fmt.Fprintf(b, "- Amostras EXCLUÍDAS da média de WER/CER por falha de cálculo "+
			"(referência vazia após normalização, metrics.ErrEmptyReference): WER=%d, CER=%d "+
			"— excluídas, nunca contadas como 0.0\n", m.WERFailed, m.CERFailed)
	}
	fmt.Fprintf(b, "- Entity Accuracy do ASR bruto (sem dictionary): %s (Correct=%d, Total=%d)\n",
		formatFloatOrNA(m.EntityAccuracy.Score(), m.EntityAccuracy.Total),
		m.EntityAccuracy.Correct, m.EntityAccuracy.Total)
	fmt.Fprintf(b, "- Number/Date Accuracy: %s (Correct=%d, Total=%d)\n\n",
		formatFloatOrNA(m.NumberDateAccuracy.Score(), m.NumberDateAccuracy.Total),
		m.NumberDateAccuracy.Correct, m.NumberDateAccuracy.Total)
}

// writeGateTable cobre TODA linha de "Gates local" do 19-eval-plan.md, uma
// por uma — nunca só as que este harness mede. Um gate que este harness
// não mede (ou não é um limiar por-candidato) ainda ganha uma linha, com
// status explícito ("não aplicável a este harness" / "NÃO MEDIDO") e uma
// frase dizendo onde ele será verificado de fato. Quem lê o relatório ao
// lado do eval-plan precisa distinguir "considerado e fora de escopo" de
// "esquecido" — as duas coisas nunca podem parecer iguais.
func writeGateTable(b *strings.Builder, cr harness.CandidateReport, gates Gates) {
	b.WriteString("### Gates (docs/ai-spec/19-eval-plan.md)\n\n")
	b.WriteString("| Gate | Medido | Passou? |\n|---|---|---|\n")

	writeGateInfoRow(b, "PT-BR + inglês",
		"gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês)")

	writeGateRow(b, fmt.Sprintf("RSS ≤ %s (cold, pico máximo)", formatBytes(float64(gates.RSSBytesMax))),
		rssRowValue(cr.Cold), rssPasses(cr.Cold, gates))
	writeGateRow(b, fmt.Sprintf("RSS ≤ %s (warm, pico máximo)", formatBytes(float64(gates.RSSBytesMax))),
		rssRowValue(cr.Warm), rssPasses(cr.Warm, gates))

	writeGateInfoRow(b, "GPU opcional",
		"não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU")

	writeGateInfoRow(b, "CPU funcional",
		"não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas)")

	writeGateRow(b, fmt.Sprintf("≥%d execuções estáveis (cold)", gates.MinStableSamples),
		fmt.Sprintf("%d tentativas, %d falhas", cr.Cold.Attempted, cr.Cold.Failed),
		stablePasses(cr.Cold, gates))
	writeGateRow(b, fmt.Sprintf("≥%d execuções estáveis (warm)", gates.MinStableSamples),
		fmt.Sprintf("%d tentativas, %d falhas", cr.Warm.Attempted, cr.Warm.Failed),
		stablePasses(cr.Warm, gates))

	writeGateInfoRow(b, "sem persistência (áudio não fica em disco/nuvem)",
		"invariante de código (AC-028, CLAUDE.md \"Áudio não deve ser persistido\"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness")

	writeGateRow(b, "Entity Accuracy do ASR bruto (sem dictionary) — cold",
		entityRowValue(cr.Cold), entityPasses(cr.Cold, gates))
	writeGateRow(b, "Entity Accuracy do ASR bruto (sem dictionary) — warm",
		entityRowValue(cr.Warm), entityPasses(cr.Warm, gates))
	writeGateInfoRow(b, fmt.Sprintf("Entity Accuracy ≥ %.0f%% após dictionary (gate real do eval-plan)", gates.EntityAccuracyMin*100),
		"NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir \"ASR + dictionary\", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint")

	writeGateInfoRow(b, "Smart false correction ≤ 1%",
		"não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção \"Eval contextual\"), não à comparação de candidatos de ASR")

	writeGateRow(b, fmt.Sprintf("p50 ≤ %s (cold)", gates.P50Max), durationRowValue(cr.Cold, statP50), meetsP50(cr.Cold, gates))
	writeGateRow(b, fmt.Sprintf("p50 ≤ %s (warm)", gates.P50Max), durationRowValue(cr.Warm, statP50), meetsP50(cr.Warm, gates))

	writeGateRow(b, fmt.Sprintf("p95 ≤ %s (cold)", gates.P95Max), durationRowValue(cr.Cold, statP95), meetsP95(cr.Cold, gates))
	writeGateRow(b, fmt.Sprintf("p95 ≤ %s (warm)", gates.P95Max), durationRowValue(cr.Warm, statP95), meetsP95(cr.Warm, gates))

	b.WriteString("\n")
}

// writeGateInfoRow registra um gate do eval-plan que este harness NÃO
// decide sozinho como passou/falhou — nunca some da tabela, nunca finge
// "sem dados suficientes" (que sugeriria que se tentou medir e faltou
// amostra). note diz, com todas as letras, onde esse gate É verificado.
func writeGateInfoRow(b *strings.Builder, gate, note string) {
	fmt.Fprintf(b, "| %s | não aplicável a este harness | não aplicável — %s |\n", gate, note)
}

// isTrue trata "sem dados suficientes" (pass == nil) como falso — usado
// só para decidir se a nota especial de warm-only-p50 aparece, nunca para
// preencher a tabela de gates (que usa writeGateRow e distingue os três
// estados explicitamente).
func isTrue(pass *bool) bool {
	return pass != nil && *pass
}

func writeGateRow(b *strings.Builder, gate, measured string, pass *bool) {
	status := "sem dados suficientes"
	if pass != nil {
		if *pass {
			status = "passou"
		} else {
			status = "falhou"
		}
	}
	fmt.Fprintf(b, "| %s | %s | %s |\n", gate, measured, status)
}

func rssRowValue(m harness.ModeReport) string {
	if !m.Supported || m.RSSSamples == 0 {
		return "não medido"
	}
	return fmt.Sprintf("%s (máx.), %s (média)", formatBytes(m.RSS.Max), formatBytes(m.RSS.Mean))
}

// rssPasses decide pelo MÁXIMO global, não pela média: para um limite de
// memória o pior caso é o que importa — um candidato cuja pior chamada
// estoura o gate não deve passar porque as outras compensaram a média.
func rssPasses(m harness.ModeReport, gates Gates) *bool {
	if !m.Supported || m.RSSSamples == 0 {
		return nil
	}
	pass := m.RSS.Max <= float64(gates.RSSBytesMax)
	return &pass
}

func durationRowValue(m harness.ModeReport, which statSelector) string {
	if !m.Supported || m.Latency.N == 0 {
		return "não medido"
	}
	return formatDurationStat(m.Latency, which)
}

func meetsP50(m harness.ModeReport, gates Gates) *bool {
	if !m.Supported || m.Latency.N == 0 {
		return nil
	}
	pass := time.Duration(m.Latency.P50) <= gates.P50Max
	return &pass
}

func meetsP95(m harness.ModeReport, gates Gates) *bool {
	if !m.Supported || m.Latency.N == 0 {
		return nil
	}
	pass := time.Duration(m.Latency.P95) <= gates.P95Max
	return &pass
}

// entityRowValue e entityPasses operam num ÚNICO ModeReport (cold OU
// warm), nunca na soma dos dois: ADR-012 e o comentário de
// harness.CandidateReport são explícitos — cold e warm "NUNCA são somados
// nem promediados juntos", porque são respostas a perguntas diferentes
// ("quanto custa sem nada vivo" vs. "quanto custa em uso contínuo"), e
// somar Entity Accuracy entre os dois esconderia exatamente uma
// divergência de qualidade entre os dois protocolos que seria sinal de
// problema no candidato.
func entityRowValue(m harness.ModeReport) string {
	if !m.Supported || m.EntityAccuracy.Total == 0 {
		return "não medido (nenhuma amostra com entidades esperadas)"
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", 100*m.EntityAccuracy.Score(), m.EntityAccuracy.Correct, m.EntityAccuracy.Total)
}

func entityPasses(m harness.ModeReport, gates Gates) *bool {
	if !m.Supported || m.EntityAccuracy.Total == 0 {
		return nil
	}
	pass := m.EntityAccuracy.Score() >= gates.EntityAccuracyMin
	return &pass
}

func stablePasses(m harness.ModeReport, gates Gates) *bool {
	if !m.Supported || m.Attempted == 0 {
		return nil
	}
	pass := m.Attempted >= gates.MinStableSamples && m.Failed == 0
	return &pass
}

func writeDecisionSection(b *strings.Builder) {
	b.WriteString("## Decisão técnica\n\n")
	b.WriteString("_Este benchmark não escolhe sozinho (ADR-012). Preencher depois de revisar " +
		"os números acima e os gates de docs/ai-spec/19-eval-plan.md:_\n\n")
	b.WriteString("- Candidato escolhido: \n")
	b.WriteString("- Justificativa: \n")
	b.WriteString("- Modo (cold/warm) assumido em produção: \n")
	b.WriteString("- Consequência para o Sprint 4 (se aplicável): \n")
}

type statSelector int

const (
	statMean statSelector = iota
	statP50
	statP95
)

func formatDurationStat(s metrics.Stats, which statSelector) string {
	if s.N == 0 {
		return "não medido"
	}
	var v float64
	switch which {
	case statP50:
		v = s.P50
	case statP95:
		v = s.P95
	default:
		v = s.Mean
	}
	return time.Duration(v).String()
}

func formatFloatOrNA(v float64, n int) string {
	if n == 0 {
		return "não medido"
	}
	return fmt.Sprintf("%.4f", v)
}

func formatBytes(v float64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
		gb = 1 << 30
	)
	switch {
	case v >= gb:
		return fmt.Sprintf("%.2f GB", v/gb)
	case v >= mb:
		return fmt.Sprintf("%.2f MB", v/mb)
	case v >= kb:
		return fmt.Sprintf("%.2f KB", v/kb)
	default:
		return fmt.Sprintf("%.0f B", v)
	}
}
