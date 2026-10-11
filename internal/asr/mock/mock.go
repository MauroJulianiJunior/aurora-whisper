package mock

import (
	"context"
	"errors"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Confident é um provider mock que sempre fornece confidence e N-best.
type Confident struct {
	Text         string
	Score        float64
	Language     string
	Alternatives []asr.Alternative

	Calls int
}

// NewConfident cria um Confident configurado com o texto e o confidence
// que Transcribe deve devolver.
func NewConfident(text string, score float64) *Confident {
	return &Confident{Text: text, Score: score, Language: "pt-BR"}
}

func (p *Confident) ID() string { return "mock-confident" }

func (p *Confident) Capabilities() asr.ASRCapabilities {
	return asr.ASRCapabilities{
		Streaming:    false,
		Confidence:   true,
		Timestamps:   false,
		NBest:        true,
		Languages:    []string{"pt-BR", "en-US"},
		Acceleration: asr.AccelerationCPU,
	}
}

func (p *Confident) Warmup(ctx context.Context) error { return ctx.Err() }

func (p *Confident) Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	p.Calls++
	if err := ctx.Err(); err != nil {
		return asr.ASRResult{}, err
	}
	return asr.ASRResult{
		Text:         p.Text,
		Language:     p.Language,
		Confidence:   asr.NewConfidence(p.Score),
		Alternatives: p.Alternatives,
		Model:        "mock-confident-v1",
	}, nil
}

// Unscored é um provider mock que NUNCA fornece confidence nem N-best —
// exercita o caminho em que asr.Confidence.Present é sempre false
// (docs/ai-spec/16-ai-behavior-spec.md, "Uncertainty").
type Unscored struct {
	Text     string
	Language string

	Calls int
}

// NewUnscored cria um Unscored configurado com o texto que Transcribe deve
// devolver.
func NewUnscored(text string) *Unscored {
	return &Unscored{Text: text, Language: "pt-BR"}
}

func (p *Unscored) ID() string { return "mock-unscored" }

func (p *Unscored) Capabilities() asr.ASRCapabilities {
	return asr.ASRCapabilities{
		Streaming:    false,
		Confidence:   false,
		Timestamps:   false,
		NBest:        false,
		Languages:    []string{"pt-BR"},
		Acceleration: asr.AccelerationNone,
	}
}

func (p *Unscored) Warmup(ctx context.Context) error { return ctx.Err() }

func (p *Unscored) Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	p.Calls++
	if err := ctx.Err(); err != nil {
		return asr.ASRResult{}, err
	}
	return asr.ASRResult{
		Text:       p.Text,
		Language:   p.Language,
		Confidence: asr.NoConfidence,
		Model:      "mock-unscored-v1",
	}, nil
}

// ErrMockFailure é o erro default devolvido por Failing.Transcribe quando
// Failing.Err não é configurado.
var ErrMockFailure = errors.New("mock: provider failure")

// Failing é um provider mock que sempre falha — exercita a degradação de
// policy sem panic quando um provider dá erro.
type Failing struct {
	// Err é o erro devolvido por Transcribe. nil usa ErrMockFailure.
	Err error

	Calls int
}

func (p *Failing) ID() string { return "mock-failing" }

func (p *Failing) Capabilities() asr.ASRCapabilities {
	return asr.ASRCapabilities{Languages: []string{"pt-BR"}, Acceleration: asr.AccelerationNone}
}

func (p *Failing) Warmup(ctx context.Context) error { return ctx.Err() }

func (p *Failing) Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	p.Calls++
	err := p.Err
	if err == nil {
		err = ErrMockFailure
	}
	return asr.ASRResult{}, err
}
