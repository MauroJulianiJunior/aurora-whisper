package audio

import (
	"math"
	"testing"
	"time"
)

func genSineInt16(freqHz float64, amplitude float64, sampleRate, numSamples int) []int16 {
	out := make([]int16, numSamples)
	for i := 0; i < numSamples; i++ {
		v := amplitude * math.Sin(2*math.Pi*freqHz*float64(i)/float64(sampleRate))
		out[i] = floatToInt16(float32(v))
	}
	return out
}

func TestClassifyVAD_Silence(t *testing.T) {
	samples := make([]int16, SampleRate) // 1s de silêncio digital puro
	summary := ClassifyVAD(samples, SampleRate, VADConfig{})

	if summary.HasSpeech {
		t.Fatalf("HasSpeech = true para silêncio puro")
	}
	if summary.SpeechDuration != 0 {
		t.Fatalf("SpeechDuration = %s, want 0", summary.SpeechDuration)
	}
}

func TestClassifyVAD_Speech(t *testing.T) {
	// Tom alto (amplitude 0.5), 1 segundo inteiro, bem acima do limiar
	// default — simula fala normal de ditado.
	samples := genSineInt16(200, 0.5, SampleRate, SampleRate)
	summary := ClassifyVAD(samples, SampleRate, VADConfig{})

	if !summary.HasSpeech {
		t.Fatalf("HasSpeech = false para sinal claramente acima do limiar")
	}
	// Quase 1s inteiro deve ter sido classificado como fala (tolerância
	// para a última janela parcial no fim do buffer).
	if summary.SpeechDuration < 900*time.Millisecond {
		t.Fatalf("SpeechDuration = %s, want perto de 1s", summary.SpeechDuration)
	}
}

func TestClassifyVAD_ModerateBackgroundNoise(t *testing.T) {
	// Ruído de fundo moderado: amplitude bem abaixo do limiar de fala,
	// mas não silêncio digital — ex.: ventilador, ruído de linha.
	samples := genSineInt16(120, 0.01, SampleRate, SampleRate)
	summary := ClassifyVAD(samples, SampleRate, VADConfig{})

	if summary.HasSpeech {
		t.Fatalf("HasSpeech = true para ruído de fundo moderado (amplitude 0.01, abaixo do limiar default %.2f)", DefaultVADThreshold)
	}
}

func TestClassifyVAD_EmptyInput(t *testing.T) {
	summary := ClassifyVAD(nil, SampleRate, VADConfig{})
	if summary.HasSpeech || summary.SpeechDuration != 0 {
		t.Fatalf("ClassifyVAD(nil) = %+v, want zero value", summary)
	}
}

func TestClassifyVAD_PartialSpeech(t *testing.T) {
	// Metade silêncio, metade fala — a duração de fala relatada deve
	// refletir só a metade falada, não o buffer inteiro.
	silence := make([]int16, SampleRate/2)
	speech := genSineInt16(200, 0.5, SampleRate, SampleRate/2)
	samples := append(append([]int16{}, silence...), speech...)

	summary := ClassifyVAD(samples, SampleRate, VADConfig{})
	if !summary.HasSpeech {
		t.Fatalf("HasSpeech = false, want true")
	}
	if summary.SpeechDuration < 400*time.Millisecond || summary.SpeechDuration > 550*time.Millisecond {
		t.Fatalf("SpeechDuration = %s, want ~500ms", summary.SpeechDuration)
	}
}
