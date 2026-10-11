package report

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/harness"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

func sampleResult() harness.BenchmarkResult {
	samples := []dataset.Sample{
		{ID: "s1", Category: dataset.CategoryContabilidade, Reference: "o Fisco cobrou"},
		{ID: "s2", Category: dataset.CategoryIngles, Reference: "hello there"},
	}
	cold := harness.ModeReport{
		Supported:      true,
		Attempted:      2,
		Failed:         0,
		Latency:        metrics.Stats{N: 2, Mean: float64(300 * time.Millisecond), P50: float64(300 * time.Millisecond), P95: float64(450 * time.Millisecond)},
		RTF:            metrics.Stats{N: 2, Mean: 0.2},
		RSS:            metrics.Stats{},
		WER:            metrics.Stats{N: 2, Mean: 0.05},
		CER:            metrics.Stats{N: 2, Mean: 0.02},
		EntityAccuracy: metrics.MatchResult{Correct: 19, Total: 20},
	}
	warm := harness.ModeReport{Supported: false}

	return harness.BenchmarkResult{
		Samples:     samples,
		DatasetHash: "deadbeef",
		Candidates: []harness.CandidateReport{
			{Name: "whisper-cpp-base", Cold: cold, Warm: warm},
		},
	}
}

func TestRender_ContainsRequiredSections(t *testing.T) {
	out := Render(sampleResult(), Environment{Hardware: "Ryzen 7", OS: "Windows 11"}, DefaultGates, time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC))

	required := []string{
		"Ryzen 7",
		"Windows 11",
		"deadbeef",
		"whisper-cpp-base",
		"Decisão técnica",
		"Gates (docs/ai-spec/19-eval-plan.md)",
	}
	for _, want := range required {
		if !strings.Contains(out, want) {
			t.Errorf("relatório não contém %q", want)
		}
	}
}

func TestRender_DatasetHashAndNAppear(t *testing.T) {
	out := Render(sampleResult(), Environment{}, DefaultGates, time.Now())
	if !strings.Contains(out, "N total de amostras: 2") {
		t.Error("relatório não informa N total de amostras")
	}
}

func TestRender_UnmeasuredWarmSaysNaoMedido(t *testing.T) {
	out := Render(sampleResult(), Environment{}, DefaultGates, time.Now())
	// A seção Warm deve dizer explicitamente que não foi medida, nunca
	// mostrar um número (candidato sem WarmArgs).
	idx := strings.Index(out, "Warm (processo residente")
	if idx < 0 {
		t.Fatal("seção Warm não encontrada")
	}
	section := out[idx:]
	if !strings.Contains(section[:200], "Não medido") {
		t.Errorf("seção Warm não diz 'Não medido': %s", section[:200])
	}
}

func TestRender_GateTable_PassAndFail(t *testing.T) {
	result := sampleResult()
	// p50 = 300ms <= 500ms (default gate) -> passou.
	out := Render(result, Environment{}, DefaultGates, time.Now())
	if !strings.Contains(out, "passou") {
		t.Error("nenhum gate marcado como 'passou' com dados que deveriam passar")
	}

	// Agora força um gate apertado o bastante para falhar.
	tight := DefaultGates
	tight.P50Max = 10 * time.Millisecond
	out2 := Render(result, Environment{}, tight, time.Now())
	if !strings.Contains(out2, "falhou") {
		t.Error("gate de p50 apertado não apareceu como 'falhou'")
	}
}

func TestRender_RSSNotMeasuredShowsNaoMedidoNotZero(t *testing.T) {
	out := Render(sampleResult(), Environment{}, DefaultGates, time.Now())
	if !strings.Contains(out, "não medido") {
		t.Error("RSS não medido (RSSSamples=0) deveria aparecer como 'não medido', não como 0")
	}
}

func TestRender_EmptyCandidatesStillRenders(t *testing.T) {
	result := harness.BenchmarkResult{
		Samples:     []dataset.Sample{{ID: "s1", Category: dataset.CategoryIngles, Reference: "oi"}},
		DatasetHash: "abc",
		Candidates:  nil,
	}
	out := Render(result, Environment{}, DefaultGates, time.Now())
	if !strings.Contains(out, "Decisão técnica") {
		t.Error("relatório sem candidatos ainda deve ter a seção de decisão")
	}
}

func TestRender_WarmOnlyP50TriggersArchitectureNote(t *testing.T) {
	// Cold falha o gate de p50 (800ms > 500ms), warm passa (200ms <=
	// 500ms) — ADR-012 exige que o relatório diga, com todas as letras,
	// que isso é uma decisão de arquitetura obrigatória para o Sprint 4.
	cold := harness.ModeReport{
		Supported: true, Attempted: 1,
		Latency: metrics.Stats{N: 1, Mean: float64(800 * time.Millisecond), P50: float64(800 * time.Millisecond), P95: float64(800 * time.Millisecond)},
	}
	warm := harness.ModeReport{
		Supported: true, Attempted: 1,
		Latency: metrics.Stats{N: 1, Mean: float64(200 * time.Millisecond), P50: float64(200 * time.Millisecond), P95: float64(200 * time.Millisecond)},
	}
	result := harness.BenchmarkResult{
		Samples:     []dataset.Sample{{ID: "s1", Category: dataset.CategoryIngles, Reference: "oi"}},
		DatasetHash: "abc",
		Candidates:  []harness.CandidateReport{{Name: "cand-lento-a-frio", Cold: cold, Warm: warm}},
	}

	out := Render(result, Environment{}, DefaultGates, time.Now())
	if !strings.Contains(out, "Decisão de arquitetura necessária") {
		t.Errorf("relatório não avisa sobre a decisão de arquitetura do Sprint 4 quando só warm cumpre p50:\n%s", out)
	}
}

func TestRender_ColdOnlyCandidateGetsWarmNotMeasuredNote(t *testing.T) {
	result := sampleResult() // warm.Supported == false neste fixture
	out := Render(result, Environment{}, DefaultGates, time.Now())
	if !strings.Contains(out, "Nota (ADR-012)") {
		t.Errorf("relatório não avisa que o candidato só foi medido em cold:\n%s", out)
	}
}

// TestRender_EntityAccuracyGate_NeverSumsColdAndWarm prova a correção do
// bug "gate de entity accuracy soma cold e warm": um candidato bom em
// cold e péssimo em warm (ou vice-versa) precisa reprovar no modo ruim,
// mesmo que a SOMA das duas contagens passasse do limiar — ADR-012 e o
// comentário de harness.CandidateReport são explícitos que os dois modos
// "NUNCA são somados nem promediados juntos".
func TestRender_EntityAccuracyGate_NeverSumsColdAndWarm(t *testing.T) {
	// Cold: 10/10 (100%, passa folgado). Warm: 0/10 (0%, reprova).
	// Somados: 10/20 = 50%, que TAMBÉM reprovaria — mas por um número
	// inventado que não corresponde a nenhum modo medido de verdade.
	cold := harness.ModeReport{
		Supported:      true,
		Attempted:      10,
		EntityAccuracy: metrics.MatchResult{Correct: 10, Total: 10},
	}
	warm := harness.ModeReport{
		Supported:      true,
		Attempted:      10,
		EntityAccuracy: metrics.MatchResult{Correct: 0, Total: 10},
	}
	result := harness.BenchmarkResult{
		Samples:     []dataset.Sample{{ID: "s1", Category: dataset.CategoryIngles, Reference: "oi"}},
		DatasetHash: "abc",
		Candidates:  []harness.CandidateReport{{Name: "cand-warm-ruim", Cold: cold, Warm: warm}},
	}

	out := Render(result, Environment{}, DefaultGates, time.Now())

	// A tabela precisa ter uma linha "cold" mostrando 100% e uma linha
	// "warm" mostrando 0% — nunca uma linha só com 50%.
	if !strings.Contains(out, "100.0% (10/10)") {
		t.Errorf("gate de entity accuracy não mostra a linha cold isolada (10/10):\n%s", out)
	}
	if !strings.Contains(out, "0.0% (0/10)") {
		t.Errorf("gate de entity accuracy não mostra a linha warm isolada (0/10), ou a somou com cold:\n%s", out)
	}
	if strings.Contains(out, "50.0% (10/20)") {
		t.Errorf("gate de entity accuracy somou cold+warm num número só (10/20 = 50%%), violando ADR-012:\n%s", out)
	}
}

// TestRender_EntityAccuracyLabel_DistinguishesRawFromAfterDictionary prova
// a correção do bug "o rótulo do gate mente sobre o que foi medido": o
// gate de 19-eval-plan.md é "Entity Accuracy ≥95% APÓS dictionary", mas
// este harness nunca chama internal/dictionary — mede o texto BRUTO. O
// relatório precisa ter DUAS linhas distintas: uma para o que foi
// medido de fato (rotulada "sem dictionary") e uma para o gate real do
// eval-plan, marcada como não medida.
func TestRender_EntityAccuracyLabel_DistinguishesRawFromAfterDictionary(t *testing.T) {
	out := Render(sampleResult(), Environment{}, DefaultGates, time.Now())

	if !strings.Contains(out, "sem dictionary") {
		t.Errorf("relatório não deixa explícito que a Entity Accuracy medida é SEM dictionary:\n%s", out)
	}
	if !strings.Contains(out, "após dictionary") {
		t.Errorf("relatório não tem uma linha separada para o gate real (após dictionary):\n%s", out)
	}
	if !strings.Contains(out, "NÃO MEDIDO") {
		t.Errorf("gate 'após dictionary' precisa dizer explicitamente que não foi medido por este harness:\n%s", out)
	}
}

// TestRender_GateTable_CoversEveryEvalPlanGate prova a correção do bug
// "a tabela de gates está incompleta e não diz que está": TODA linha de
// "Gates local" (docs/ai-spec/19-eval-plan.md) precisa aparecer na
// tabela, mesmo as que este harness não mede — com status explícito, não
// silenciosamente omitidas.
func TestRender_GateTable_CoversEveryEvalPlanGate(t *testing.T) {
	out := Render(sampleResult(), Environment{}, DefaultGates, time.Now())

	evalPlanGates := []string{
		"PT-BR + inglês",
		"RSS ≤",
		"GPU opcional",
		"CPU funcional",
		"execuções estáveis (cold)",
		"execuções estáveis (warm)",
		"sem persistência",
		"Entity Accuracy",
		"após dictionary",
		"Smart false correction",
		"p50 ≤",
		"p95 ≤",
	}
	for _, want := range evalPlanGates {
		if !strings.Contains(out, want) {
			t.Errorf("tabela de gates não contém a linha esperada para %q:\n%s", want, out)
		}
	}

	// Gates que este harness não mede precisam dizer isso com todas as
	// letras — nunca aparecer em branco nem como "sem dados suficientes"
	// (que sugeriria que a medição foi tentada).
	mustSayNotApplicable := []string{"PT-BR + inglês", "GPU opcional", "CPU funcional", "sem persistência", "Smart false correction"}
	for _, gate := range mustSayNotApplicable {
		idx := strings.Index(out, gate)
		if idx < 0 {
			t.Fatalf("linha do gate %q não encontrada", gate)
		}
		line := out[idx : idx+strings.Index(out[idx:], "\n")]
		if !strings.Contains(line, "não aplicável") {
			t.Errorf("linha do gate %q não diz 'não aplicável': %s", gate, line)
		}
	}
}

// TestRender_RSSGate_UsesMaxNotMean prova a correção do bug "RSS usa a
// média dos picos": o gate de memória precisa decidir pelo MÁXIMO global
// entre as amostras, não pela média — um candidato cuja pior chamada
// estoura o limite não pode passar porque as outras compensaram.
func TestRender_RSSGate_UsesMaxNotMean(t *testing.T) {
	// Três amostras: RSS baixo, baixo, e uma bem acima do gate (1GB). A
	// MÉDIA fica abaixo de 1GB, mas o MÁXIMO estoura.
	cold := harness.ModeReport{
		Supported:  true,
		Attempted:  3,
		RSSSamples: 3,
		RSS: metrics.Stats{
			N:    3,
			Mean: (100*1024*1024 + 100*1024*1024 + 1200*1024*1024) / 3, // ~466MB, abaixo do gate
			Max:  1200 * 1024 * 1024,                                   // 1.2GB, acima do gate
		},
		Latency: metrics.Stats{N: 3},
	}
	result := harness.BenchmarkResult{
		Samples:     []dataset.Sample{{ID: "s1", Category: dataset.CategoryIngles, Reference: "oi"}},
		DatasetHash: "abc",
		Candidates:  []harness.CandidateReport{{Name: "cand-pico-alto", Cold: cold, Warm: harness.ModeReport{}}},
	}

	out := Render(result, Environment{}, DefaultGates, time.Now())

	// A linha do gate de RSS (cold) precisa aparecer como "falhou", porque
	// o MÁXIMO (1.2GB) estoura o gate de ~1GB — mesmo a média passando.
	idx := strings.Index(out, "RSS ≤")
	if idx < 0 {
		t.Fatal("linha de gate de RSS não encontrada")
	}
	line := out[idx : idx+strings.Index(out[idx:], "\n")]
	if !strings.Contains(line, "falhou") {
		t.Errorf("gate de RSS (cold) deveria falhar pelo MÁXIMO (1.2GB > 1GB), mesmo com média abaixo do limite: %s", line)
	}
}

func TestRender_DecisionSectionIsBlankForAHumanToFill(t *testing.T) {
	out := Render(sampleResult(), Environment{}, DefaultGates, time.Now())
	idx := strings.Index(out, "## Decisão técnica")
	if idx < 0 {
		t.Fatal("seção de decisão não encontrada")
	}
	section := out[idx:]
	if !strings.Contains(section, "Candidato escolhido: \n") {
		t.Error("campo 'Candidato escolhido' não está em branco para preenchimento humano")
	}
}
