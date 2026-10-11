package audio

import (
	"math"
	"testing"
)

func TestResampler_StereoToMono_NoRateChange(t *testing.T) {
	// srcRate == SampleRate: ratio 1.0, então cada amostra de saída deve
	// bater exatamente com a média L/R de entrada (sem interpolação
	// borrando o valor), o que torna a verificação de downmix exata.
	r := NewResampler(SampleRate, 2)

	in := []float32{
		1.0, -1.0, // média 0.0
		0.5, 0.5, // média 0.5
		-0.25, 0.75, // média 0.25
		0, 0, // amostra extra só para "destravar" a interpolação da anterior
	}
	out := r.Process(in)

	want := []int16{floatToInt16(0), floatToInt16(0.5), floatToInt16(0.25)}
	if len(out) < len(want) {
		t.Fatalf("len(out) = %d, want pelo menos %d (out=%v)", len(out), len(want), out)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d] = %d, want %d", i, out[i], want[i])
		}
	}
}

func TestResampler_ClampsBeyondFullScale(t *testing.T) {
	r := NewResampler(SampleRate, 1)
	out := r.Process([]float32{2.0, -2.0, 0.0})

	if out[0] != math.MaxInt16-0 && out[0] != 32767 {
		t.Errorf("out[0] = %d, want clamp to 32767", out[0])
	}
	if out[1] != -32767 {
		t.Errorf("out[1] = %d, want clamp to -32767 (não -32768, pois clamp é em ±1.0 antes de *32767)", out[1])
	}
	if out[0] > 32767 || out[0] < -32768 || out[1] > 32767 || out[1] < -32768 {
		t.Fatalf("amostra fora da faixa de int16: %v", out)
	}
}

// sineFrequencyHz estima a frequência de um sinal mono int16 por
// cruzamento de zero — suficiente para confirmar que a reamostragem não
// deslocou grosseiramente a frequência de um tom conhecido.
func sineFrequencyHz(samples []int16, sampleRate int) float64 {
	crossings := 0
	for i := 1; i < len(samples); i++ {
		if (samples[i-1] < 0) != (samples[i] < 0) {
			crossings++
		}
	}
	seconds := float64(len(samples)) / float64(sampleRate)
	return float64(crossings) / 2 / seconds
}

func genSine(freqHz float64, sampleRate, channels, numFrames int) []float32 {
	out := make([]float32, numFrames*channels)
	for i := 0; i < numFrames; i++ {
		v := float32(math.Sin(2 * math.Pi * freqHz * float64(i) / float64(sampleRate)))
		for c := 0; c < channels; c++ {
			out[i*channels+c] = v
		}
	}
	return out
}

func testSineRoundTrip(t *testing.T, srcRate int, freqHz float64) {
	t.Helper()

	const durationSeconds = 0.2
	numFrames := int(float64(srcRate) * durationSeconds)
	src := genSine(freqHz, srcRate, 2, numFrames)

	r := NewResampler(srcRate, 2)

	// Alimenta em pedaços de tamanho irregular, como pacotes reais do
	// WASAPI chegariam, para provar que a continuidade entre chamadas
	// (r.pos/r.prevMono) não introduz descontinuidade audível.
	chunkSizes := []int{97, 211, 500, 333, 64}
	var out []int16
	pos := 0
	ci := 0
	for pos < numFrames {
		n := chunkSizes[ci%len(chunkSizes)]
		ci++
		if pos+n > numFrames {
			n = numFrames - pos
		}
		in := src[pos*2 : (pos+n)*2]
		out = append(out, r.Process(in)...)
		pos += n
	}

	got := sineFrequencyHz(out, SampleRate)
	tolerance := freqHz * 0.05
	if math.Abs(got-freqHz) > tolerance {
		t.Fatalf("frequência medida na saída = %.1f Hz, want ~%.1f Hz (±%.1f)", got, freqHz, tolerance)
	}
}

func TestResampler_Sine_48kTo16k(t *testing.T) {
	testSineRoundTrip(t, 48000, 440)
}

func TestResampler_Sine_44100To16k(t *testing.T) {
	testSineRoundTrip(t, 44100, 440)
}

func TestResampler_OutputRateApproximatelyCorrect(t *testing.T) {
	// 48000 -> 16000 é ratio exato 3:1; 1 segundo de origem deve virar
	// bem perto de 16000 amostras de saída.
	r := NewResampler(48000, 1)
	in := make([]float32, 48000)
	out := r.Process(in)
	if out == nil {
		t.Fatal("out é nil")
	}
	if diff := abs(len(out) - 16000); diff > 2 {
		t.Fatalf("len(out) = %d, want ~16000 (±2)", len(out))
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
