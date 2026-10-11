package asr_test

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/mock"
)

// TestConformance_AC029 prova que os três providers mock — com
// capabilities deliberadamente diferentes entre si — são consumíveis pelo
// mesmo contrato asr.ASRProvider, sem nenhum type switch ou cast no lado
// do chamador (AC-029: "providers usam mesmo contrato").
func TestConformance_AC029(t *testing.T) {
	audio := asr.AudioBuffer{PCM: []int16{1, 2, 3}, SampleRateHz: 16000}

	providers := []asr.ASRProvider{
		mock.NewConfident("ola mundo", 0.9),
		mock.NewUnscored("ola mundo"),
		&mock.Failing{},
	}

	for _, p := range providers {
		p := p
		t.Run(p.ID(), func(t *testing.T) {
			if id := p.ID(); id == "" {
				t.Fatal("ID() vazio")
			}

			caps := p.Capabilities()

			if err := p.Warmup(context.Background()); err != nil {
				t.Fatalf("Warmup: %v", err)
			}

			result, err := p.Transcribe(context.Background(), audio, asr.ASROptions{})

			switch p.(type) {
			case *mock.Failing:
				if err == nil {
					t.Fatal("Failing provider deveria retornar erro")
				}
			default:
				if err != nil {
					t.Fatalf("Transcribe: %v", err)
				}
				// AC-029/16-ai-behavior-spec: a presença de confidence no
				// resultado precisa bater com o que Capabilities() declara.
				if result.Confidence.Present != caps.Confidence {
					t.Fatalf("Capabilities().Confidence=%v mas Confidence.Present=%v",
						caps.Confidence, result.Confidence.Present)
				}
			}
		})
	}
}

// TestConformance_CapabilitiesDiffer confirma que os três mocks realmente
// têm capabilities diferentes entre si — do contrário AC-029 não estaria
// sendo exercitado de forma significativa.
func TestConformance_CapabilitiesDiffer(t *testing.T) {
	confident := mock.NewConfident("x", 0.9).Capabilities()
	unscored := mock.NewUnscored("x").Capabilities()

	if confident.Confidence == unscored.Confidence {
		t.Fatal("Confident e Unscored deveriam divergir em Capabilities().Confidence")
	}
	if confident.NBest == unscored.NBest {
		t.Fatal("Confident e Unscored deveriam divergir em Capabilities().NBest")
	}
}
