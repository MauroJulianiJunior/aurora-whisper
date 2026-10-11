package audio

import (
	"math"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// VADConfig configura a detecção de atividade de voz por energia.
type VADConfig struct {
	// FrameDuration é o tamanho de cada janela de análise. Zero usa
	// DefaultVADFrameDuration.
	FrameDuration time.Duration
	// Threshold é o RMS mínimo — em fração do fundo de escala int16
	// (0.0..1.0) — acima do qual uma janela é considerada fala. Zero usa
	// DefaultVADThreshold.
	Threshold float64
}

const (
	// DefaultVADFrameDuration: 20ms é o tamanho de janela clássico para
	// VAD de voz (curto o bastante para não misturar fala e silêncio na
	// mesma janela, longo o bastante para uma estimativa de RMS estável).
	DefaultVADFrameDuration = 20 * time.Millisecond

	// DefaultVADThreshold separa ruído de fundo moderado (ventilador,
	// teclado ao longe, ruído de linha) de fala falada a volume normal de
	// ditado perto do microfone. Calibrado empiricamente em amostras
	// sintéticas: ruído de fundo moderado fica claramente abaixo de 0.03
	// RMS normalizado; fala normal, acima.
	DefaultVADThreshold = 0.03
)

func (c VADConfig) withDefaults() VADConfig {
	if c.FrameDuration <= 0 {
		c.FrameDuration = DefaultVADFrameDuration
	}
	if c.Threshold <= 0 {
		c.Threshold = DefaultVADThreshold
	}
	return c
}

// ClassifyVAD varre samples (PCM 16 kHz mono int16, mas sampleRate é
// explícito para não acoplar o cálculo à constante SampleRate) em janelas
// de FrameDuration e resume se havia fala e por quanto tempo.
//
// É deliberadamente mínimo (ADR-007, seção 4 / "O que foi rejeitado":
// "Cortar o VAD do núcleo" foi rejeitado, mas o que sobrevive é só isto —
// o dictation.VADSummary final de uma captura. Streaming/partial (amostra
// de amplitude ao vivo para o overlay) fica para depois; quando entrar,
// deve reusar rmsInt16 desta mesma janela.
func ClassifyVAD(samples []int16, sampleRate int, cfg VADConfig) dictation.VADSummary {
	cfg = cfg.withDefaults()
	if sampleRate <= 0 || len(samples) == 0 {
		return dictation.VADSummary{}
	}

	frameLen := int(cfg.FrameDuration.Seconds() * float64(sampleRate))
	if frameLen <= 0 {
		frameLen = 1
	}

	var speechDuration time.Duration
	for start := 0; start < len(samples); start += frameLen {
		end := start + frameLen
		if end > len(samples) {
			end = len(samples)
		}
		frame := samples[start:end]
		if rmsInt16(frame) >= cfg.Threshold {
			speechDuration += time.Duration(len(frame)) * time.Second / time.Duration(sampleRate)
		}
	}

	return dictation.VADSummary{
		HasSpeech:      speechDuration > 0,
		SpeechDuration: speechDuration,
	}
}

// rmsInt16 devolve o RMS de frame, normalizado para a faixa 0.0..1.0 do
// fundo de escala int16.
func rmsInt16(frame []int16) float64 {
	if len(frame) == 0 {
		return 0
	}
	var sumSquares float64
	for _, s := range frame {
		v := float64(s) / 32768
		sumSquares += v * v
	}
	return math.Sqrt(sumSquares / float64(len(frame)))
}
