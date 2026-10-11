package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

func TestParseUserAction(t *testing.T) {
	cases := []struct {
		input string
		want  userAction
	}{
		{"", actionAccept},
		{"   ", actionAccept},
		{"r", actionRedo},
		{"R", actionRedo},
		{" r ", actionRedo},
		{"s", actionSkip},
		{"S", actionSkip},
		{"q", actionQuit},
		{"Q", actionQuit},
		{"x", actionUnknown},
		{"sim", actionUnknown},
		{"rq", actionUnknown},
	}
	for _, c := range cases {
		if got := parseUserAction(c.input); got != c.want {
			t.Errorf("parseUserAction(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestFormatProgress(t *testing.T) {
	got := formatProgress(12, 36, dataset.CategoryContabilidade)
	want := "[12/36] categoria: contabilidade"
	if got != want {
		t.Fatalf("formatProgress = %q, want %q", got, want)
	}
}

func TestSampleDuration(t *testing.T) {
	cases := []struct {
		numSamples, sampleRate int
		want                   time.Duration
	}{
		{16000, 16000, time.Second},
		{8000, 16000, 500 * time.Millisecond},
		{0, 16000, 0},
		{100, 0, 0}, // taxa inválida: não pode dividir por zero
	}
	for _, c := range cases {
		got := sampleDuration(c.numSamples, c.sampleRate)
		if got != c.want {
			t.Errorf("sampleDuration(%d, %d) = %v, want %v", c.numSamples, c.sampleRate, got, c.want)
		}
	}
}

func silentSamples(n int) []int16 { return make([]int16, n) }

func TestRecordingWarnings_Silence(t *testing.T) {
	got := recordingWarnings(silentSamples(16000), dictation.VADSummary{HasSpeech: false})
	if len(got) != 1 || !strings.Contains(got[0], "silêncio") {
		t.Fatalf("recordingWarnings(silêncio) = %v, want aviso de silêncio", got)
	}
}

func TestRecordingWarnings_EmptySamples(t *testing.T) {
	got := recordingWarnings(nil, dictation.VADSummary{HasSpeech: true})
	if len(got) != 1 || !strings.Contains(got[0], "silêncio") {
		t.Fatalf("recordingWarnings(vazio) = %v, want aviso de silêncio", got)
	}
}

func TestRecordingWarnings_LowVolume(t *testing.T) {
	// Pico de 1000 é ~3% do fundo de escala (32768) — bem abaixo dos 10%
	// de lowVolumePeakRatio.
	samples := make([]int16, 16000)
	for i := range samples {
		samples[i] = 1000
	}
	got := recordingWarnings(samples, dictation.VADSummary{HasSpeech: true})

	if len(got) != 1 || !strings.Contains(got[0], "baixo") {
		t.Fatalf("recordingWarnings(baixo volume) = %v, want só aviso de volume baixo", got)
	}
}

func TestRecordingWarnings_Clipping(t *testing.T) {
	// 1% das amostras no teto (32767) — acima de clippingSampleRatio
	// (0.1%) — e o restante num nível normal, para não disparar também o
	// aviso de volume baixo.
	samples := make([]int16, 10000)
	for i := range samples {
		if i%100 == 0 {
			samples[i] = math.MaxInt16
		} else {
			samples[i] = 8000 // ~24% do fundo de escala: acima do limiar de volume baixo
		}
	}
	got := recordingWarnings(samples, dictation.VADSummary{HasSpeech: true})

	if len(got) != 1 || !strings.Contains(got[0], "clipping") {
		t.Fatalf("recordingWarnings(clipping) = %v, want só aviso de clipping", got)
	}
}

// TestRecordingWarnings_NegativeClippingDoesNotOverflow prova que o
// caminho de -32768 (o int16 mínimo, sem correspondente positivo) não
// estoura a conta de pico: negar -32768 em aritmética int16 pura
// devolveria -32768 de novo (two's complement), o que faria o pico
// aparecer ERRADO como muito baixo (ou negativo) em vez de máximo. A
// amostra em si já é clipping (bate em -32768, abaixo do teto -32760) e o
// volume claramente não é baixo — se a função errar o sinal, este teste
// falha vendo o aviso errado.
func TestRecordingWarnings_NegativeClippingDoesNotOverflow(t *testing.T) {
	samples := make([]int16, 10000)
	for i := range samples {
		if i%100 == 0 {
			samples[i] = math.MinInt16
		} else {
			// -1000 de propósito, e não um valor alto: precisa ser BAIXO o
			// bastante para, sozinho, ficar abaixo de lowVolumePeakRatio
			// (10% de 32768 = 3276,8). Assim o único jeito de não sair o
			// aviso de volume baixo é o pico vir do math.MinInt16 — que é
			// exatamente o que o overflow de sinal destruiria. Com amostras
			// de fundo altas, o teste passaria com ou sem o defeito.
			samples[i] = -1000
		}
	}
	got := recordingWarnings(samples, dictation.VADSummary{HasSpeech: true})

	if len(got) != 1 || !strings.Contains(got[0], "clipping") {
		t.Fatalf("recordingWarnings(clipping negativo) = %v, want só aviso de clipping (sem aviso de volume baixo por overflow de sinal)", got)
	}
}

func TestRecordingWarnings_NormalAudio(t *testing.T) {
	// Onda simples de amplitude ~50% do fundo de escala: nem baixa, nem
	// no teto.
	samples := make([]int16, 16000)
	for i := range samples {
		if i%2 == 0 {
			samples[i] = 16000
		} else {
			samples[i] = -16000
		}
	}
	got := recordingWarnings(samples, dictation.VADSummary{HasSpeech: true})

	if len(got) != 0 {
		t.Fatalf("recordingWarnings(áudio normal) = %v, want nenhum aviso", got)
	}
}

func TestLevelBar(t *testing.T) {
	cases := []struct {
		rms  float64
		want string
	}{
		{0, "[" + strings.Repeat(".", 30) + "]"},
		{1, "[" + strings.Repeat("#", 30) + "]"},
		{0.5, "[" + strings.Repeat("#", 15) + strings.Repeat(".", 15) + "]"},
		{-1, "[" + strings.Repeat(".", 30) + "]"}, // grampeado para 0
		{2, "[" + strings.Repeat("#", 30) + "]"},  // grampeado para 1
	}
	for _, c := range cases {
		if got := levelBar(c.rms); got != c.want {
			t.Errorf("levelBar(%v) = %q, want %q", c.rms, got, c.want)
		}
	}
}
