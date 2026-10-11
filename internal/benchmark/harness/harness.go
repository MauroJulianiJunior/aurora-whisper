// Package harness orquestra dataset + candidate + metrics + rss numa
// medição completa de benchmark de ASR (TASK-006,
// docs/ai-spec/13-implementation-plan.md), conforme
// docs/adr/ADR-012-asr-local-como-processo-externo.md.
//
// Run mede um candidato contra o dataset inteiro, nos dois modos —
// cold (candidate.RunCold, um processo por amostra) e warm
// (candidate.WarmSession, processo residente) — e devolve dois
// ModeReport independentes (BenchmarkResult.Cold, BenchmarkResult.Warm).
// Eles NUNCA são somados nem promediados juntos (ADR-012, "Dois números
// distintos, nunca somados"): cold responde "quanto custa se não
// mantivermos nada vivo", warm responde "quanto custa em uso real e
// contínuo", e confundir os dois esconde exatamente a decisão de
// arquitetura que o Sprint 4 precisa tomar se só o warm cumprir a meta.
package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/candidate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// SampleOutcome é o resultado de rodar UMA amostra contra UM candidato em
// UM modo (cold ou warm).
type SampleOutcome struct {
	SampleID string
	Category dataset.Category

	// Err, quando não nil, é a única coisa preenchida além de
	// SampleID/Category: a amostra falhou (processo saiu com erro) ou
	// travou (candidate.ErrTimeout). Falhas NÃO entram nas estatísticas
	// de latência/RTF/qualidade — entram na contagem de Failed/TimedOut
	// do ModeReport, que é o que o gate de "estabilidade"
	// (19-eval-plan.md) observa.
	Err error

	Latency       time.Duration
	AudioDuration time.Duration
	RTF           float64 // Latency / AudioDuration
	PeakRSSBytes  int64
	RSSMeasured   bool

	// WER/CER e seus Err companheiros: quando metrics.WER/CER devolve
	// ErrEmptyReference (referência vazia após normalização), o cálculo
	// não tem resposta honesta — WER/CER ficam no zero-value, mas
	// WERErr/CERErr != nil marca que esse zero NÃO é uma medição, é
	// "não calculado". aggregate() usa esses campos para excluir a
	// amostra da fatia agregada em vez de somar um 0.0 como se fosse uma
	// transcrição perfeita.
	WER    float64
	WERErr error
	CER    float64
	CERErr error

	EntityResult  metrics.MatchResult
	NumberDateRes metrics.MatchResult
}

// ModeReport agrega os SampleOutcome de um modo (cold ou warm) de um
// candidato: quantas amostras foram tentadas, quantas falharam
// (separando timeout do resto), e as estatísticas (metrics.Aggregate)
// de latência, RTF, RSS, WER e CER sobre as amostras bem-sucedidas.
type ModeReport struct {
	// Supported é false quando o modo não se aplica a este candidato
	// (warm sem WarmArgs) — ver candidate.ErrWarmNotSupported. Quando
	// false, todos os outros campos ficam no zero-value: "não medido",
	// nunca um zero fingindo ser medição.
	Supported bool

	Attempted int
	Failed    int
	TimedOut  int

	Latency    metrics.Stats
	RTF        metrics.Stats
	RSS        metrics.Stats
	RSSSamples int // quantas amostras tiveram RSS medido (pode ser < Attempted-Failed)

	WER metrics.Stats
	CER metrics.Stats
	// WERFailed/CERFailed contam amostras (dentre as bem-sucedidas, ou
	// seja, já excluindo Failed/TimedOut) cujo cálculo de WER/CER falhou
	// (metrics.ErrEmptyReference — referência vazia após normalização) e
	// por isso NÃO entraram em WER.N/CER.N nem na média. Uma amostra
	// quebrada nunca vira um 0.0 escondido na agregação — o relatório
	// (pacote report) mostra esta contagem explicitamente.
	WERFailed int
	CERFailed int

	// EntityAccuracy e NumberDateAccuracy são agregados por SOMA de
	// Correct/Total entre amostras, não por média das Score() individuais
	// — a mesma razão documentada em metrics.MatchResult.Score(): uma
	// amostra sem entidade esperada não deve pesar tanto quanto uma com
	// 5, e somar preserva isso.
	EntityAccuracy     metrics.MatchResult
	NumberDateAccuracy metrics.MatchResult

	Outcomes []SampleOutcome
}

// CandidateReport é o resultado completo de um candidato: os dois modos,
// nunca combinados.
type CandidateReport struct {
	Name string
	Cold ModeReport
	Warm ModeReport
}

// BenchmarkResult é a saída completa de Run: o dataset usado (para o
// relatório poder citar hash/categorias/N) e um CandidateReport por
// candidato configurado.
type BenchmarkResult struct {
	Samples     []dataset.Sample
	DatasetHash string
	Candidates  []CandidateReport
}

// Run mede cada cfg em candidates contra samples, nos modos cold e (se
// suportado) warm, e devolve um BenchmarkResult. Run não decide nada
// (ADR-012) — só mede e agrega; comparar com os gates é
// responsabilidade do pacote report.
//
// Run calcula o hash do dataset (dataset.Hash) uma vez, no início — os
// mesmos bytes de áudio usados durante toda a medição, para o hash do
// relatório corresponder de fato ao que foi medido.
func Run(ctx context.Context, samples []dataset.Sample, candidates []candidate.Config) (BenchmarkResult, error) {
	hash, err := dataset.Hash(samples)
	if err != nil {
		return BenchmarkResult{}, fmt.Errorf("harness: calculando hash do dataset: %w", err)
	}

	result := BenchmarkResult{
		Samples:     samples,
		DatasetHash: hash,
		Candidates:  make([]CandidateReport, 0, len(candidates)),
	}

	for _, cfg := range candidates {
		cr := CandidateReport{
			Name: cfg.Name,
			Cold: runCold(ctx, cfg, samples),
			Warm: runWarm(ctx, cfg, samples),
		}
		result.Candidates = append(result.Candidates, cr)
	}

	return result, nil
}

func runCold(ctx context.Context, cfg candidate.Config, samples []dataset.Sample) ModeReport {
	outcomes := make([]SampleOutcome, 0, len(samples))
	for _, s := range samples {
		start := time.Now()
		res, err := candidate.RunCold(ctx, cfg, s.AudioPath)
		outcomes = append(outcomes, buildOutcome(s, res, err, time.Since(start)))
	}
	return aggregate(outcomes, true)
}

func runWarm(ctx context.Context, cfg candidate.Config, samples []dataset.Sample) ModeReport {
	ws, err := candidate.StartWarmSession(ctx, cfg)
	if err != nil {
		// Warm não suportado (ou não conseguiu nem subir): reportado como
		// "não medido", não como falha de amostra por amostra — o
		// candidato não participa da comparação warm, mas isso não é o
		// mesmo que ele ter "travado" em cada amostra.
		return ModeReport{Supported: false}
	}
	defer ws.Close()

	outcomes := make([]SampleOutcome, 0, len(samples))
	for _, s := range samples {
		start := time.Now()
		res, err := ws.Transcribe(ctx, s.AudioPath)
		outcomes = append(outcomes, buildOutcome(s, res, err, time.Since(start)))
	}
	return aggregate(outcomes, true)
}

// ErrEmptyTranscription marca a amostra em que o candidato devolveu texto
// vazio. Conta como falha (Failed), nunca como transcricao de nota zero.
var ErrEmptyTranscription = errors.New("harness: candidato devolveu transcricao vazia")

func buildOutcome(s dataset.Sample, res candidate.Result, err error, wall time.Duration) SampleOutcome {
	o := SampleOutcome{SampleID: s.ID, Category: s.Category}
	if err != nil {
		o.Err = err
		return o
	}

	// Transcricao vazia e FALHA, nao resultado ruim.
	//
	// Sem isto, um candidato que nao produz nada — processo mal
	// configurado, servidor que fechou o corpo cedo, binario que leu EOF
	// em vez do audio — entra no relatorio com WER 1,0 e Entity 0/N, e le
	// como "modelo pessimo" em vez de "medicao invalida". As duas coisas
	// pedem acoes opostas: uma elimina o candidato, a outra manda
	// consertar a configuracao e medir de novo.
	//
	// O dataset nao tem amostra muda: as 46 foram verificadas uma a uma,
	// todas com fala. Entao vazio aqui so pode ser defeito.
	if strings.TrimSpace(res.Text) == "" {
		o.Err = ErrEmptyTranscription
		return o
	}

	o.Latency = res.WallTime
	if o.Latency <= 0 {
		o.Latency = wall
	}
	o.PeakRSSBytes = res.PeakRSSBytes
	o.RSSMeasured = res.RSSMeasured

	if dur, derr := dataset.Duration(s.AudioPath); derr == nil && dur > 0 {
		o.AudioDuration = dur
		o.RTF = o.Latency.Seconds() / dur.Seconds()
	}

	// Sempre registra o erro junto do valor (mesmo quando o valor fica no
	// zero-value por falha): aggregate() decide o que fazer com isso, e
	// nunca é este ponto que apaga a informação de "não calculado".
	o.WER, o.WERErr = metrics.WER(s.Reference, res.Text)
	o.CER, o.CERErr = metrics.CER(s.Reference, res.Text)
	o.EntityResult = metrics.EntityAccuracy(s.Entities, res.Text)
	o.NumberDateRes = metrics.NumberDateAccuracy(s.Reference, res.Text)

	return o
}

func aggregate(outcomes []SampleOutcome, supported bool) ModeReport {
	r := ModeReport{Supported: supported, Attempted: len(outcomes), Outcomes: outcomes}

	var latencies, rtfs, rssBytes, wers, cers []float64
	for _, o := range outcomes {
		if o.Err != nil {
			r.Failed++
			if isTimeout(o.Err) {
				r.TimedOut++
			}
			continue
		}
		latencies = append(latencies, float64(o.Latency))
		if o.AudioDuration > 0 {
			rtfs = append(rtfs, o.RTF)
		}
		if o.RSSMeasured {
			rssBytes = append(rssBytes, float64(o.PeakRSSBytes))
			r.RSSSamples++
		}
		// Falha de cálculo (referência vazia) EXCLUI a amostra da fatia —
		// nunca substitui por 0.0, que pareceria uma transcrição perfeita
		// e puxaria a média para cima por uma amostra quebrada.
		if o.WERErr == nil {
			wers = append(wers, o.WER)
		} else {
			r.WERFailed++
		}
		if o.CERErr == nil {
			cers = append(cers, o.CER)
		} else {
			r.CERFailed++
		}
		r.EntityAccuracy.Correct += o.EntityResult.Correct
		r.EntityAccuracy.Total += o.EntityResult.Total
		r.NumberDateAccuracy.Correct += o.NumberDateRes.Correct
		r.NumberDateAccuracy.Total += o.NumberDateRes.Total
	}

	// metrics.Aggregate recebe float64 puro: para Latency os valores são
	// nanossegundos (float64(time.Duration)), para RSS são bytes — a
	// conversão de volta para unidade legível é responsabilidade de quem
	// lê Stats (o pacote report), não deste agregador.
	r.Latency = metrics.Aggregate(latencies)
	r.RTF = metrics.Aggregate(rtfs)
	r.RSS = metrics.Aggregate(rssBytes)
	r.WER = metrics.Aggregate(wers)
	r.CER = metrics.Aggregate(cers)

	return r
}

func isTimeout(err error) bool {
	return errors.Is(err, candidate.ErrTimeout)
}
