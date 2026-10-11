package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// segmento monta um trecho de teste: `dur` segundos de um tom senoidal de
// 120 Hz (grave, estável, fácil de prever) na amplitude `amp`. amp baixo
// simula silêncio/ruído de sala; amp alto simula fala.
func segmento(rate int, dur, amp float64) []float64 {
	n := int(dur * float64(rate))
	out := make([]float64, n)
	for i := range out {
		out[i] = amp * math.Sin(2*math.Pi*120*float64(i)/float64(rate))
	}
	return out
}

func concat(segs ...[]float64) []float64 {
	var n int
	for _, s := range segs {
		n += len(s)
	}
	out := make([]float64, 0, n)
	for _, s := range segs {
		out = append(out, s...)
	}
	return out
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// TestDecideTrim_MargemBateComAlvo cobre o caso central: bordas de
// silêncio conhecidas nos dois lados, e a margem que sobra no resultado
// tem de bater com -margin. A tolerância declarada é UMA janela de
// análise (windowDur) — a granularidade da detecção.
func TestDecideTrim_MargemBateComAlvo(t *testing.T) {
	rate := 16000
	x := concat(
		segmento(rate, 1.0, 0.0005), // 1,0s de silêncio (ruído baixo)
		segmento(rate, 3.0, 0.3),    // 3,0s de "fala"
		segmento(rate, 0.5, 0.0005), // 0,5s de silêncio
	)

	margin := 200 * time.Millisecond
	d := decideTrim(x, rate, margin)
	if d.Refused {
		t.Fatalf("recusou sinal com bordas de silêncio conhecidas: %s", d.Reason)
	}

	marginSamples := int(margin.Seconds() * float64(rate))
	tol := int(windowDur.Seconds() * float64(rate)) // 1 janela de tolerância, declarada

	gotStartMargin := d.SpeechStart - d.StartCut
	if diff := absInt(gotStartMargin - marginSamples); diff > tol {
		t.Errorf("margem resultante no início = %d amostras, want %d ± %d (janela de análise)", gotStartMargin, marginSamples, tol)
	}
	gotEndMargin := (len(x) - d.EndCut) - d.SpeechEnd
	if diff := absInt(gotEndMargin - marginSamples); diff > tol {
		t.Errorf("margem resultante no fim = %d amostras, want %d ± %d (janela de análise)", gotEndMargin, marginSamples, tol)
	}
}

// TestDecideTrim_ComecaFalandoNaoCortaInicio cobre o sinal que já começa
// com fala, sem silêncio inicial nenhum: não pode cortar nada do início,
// e não pode quebrar (índice negativo, etc.).
func TestDecideTrim_ComecaFalandoNaoCortaInicio(t *testing.T) {
	rate := 16000
	x := concat(
		segmento(rate, 3.0, 0.3),    // fala desde a amostra 0
		segmento(rate, 0.5, 0.0005), // 0,5s de silêncio no fim
	)

	d := decideTrim(x, rate, 200*time.Millisecond)
	if d.Refused {
		t.Fatalf("recusou sinal que começa falando: %s", d.Reason)
	}
	if d.StartCut != 0 {
		t.Errorf("StartCut = %d, want 0 (sinal já começa com fala, nada de silêncio para cortar)", d.StartCut)
	}
}

// TestDecideTrim_SilencioTotalRecusa cobre o sinal todo silencioso: tem
// de recusar, nunca devolver um corte que produza arquivo vazio.
func TestDecideTrim_SilencioTotalRecusa(t *testing.T) {
	rate := 16000
	x := make([]float64, 3*rate) // 3s de silêncio absoluto (zero exato)

	d := decideTrim(x, rate, 200*time.Millisecond)
	if !d.Refused {
		t.Fatalf("não recusou sinal todo silencioso (StartCut=%d EndCut=%d) — arriscaria devolver arquivo vazio ou quase", d.StartCut, d.EndCut)
	}
}

// TestDecideTrim_NaoComeSilabaFracaNoFim é o teste que importa: uma
// sílaba de amplitude baixa (mas real) no fim da gravação, seguida de
// silêncio de sala de verdade. A sílaba fica de propósito perto do
// limiar (~6,7% do pico contra o limiar de 5%) para ser um caso difícil,
// não um caso óbvio.
func TestDecideTrim_NaoComeSilabaFracaNoFim(t *testing.T) {
	rate := 16000
	loud := segmento(rate, 2.0, 0.3)       // 2,0s de fala "normal"
	weak := segmento(rate, 0.15, 0.02)     // 150ms de sílaba fraca: ~6,7% do pico
	silence := segmento(rate, 0.3, 0.0003) // 300ms de silêncio de sala de verdade
	x := concat(loud, weak, silence)
	weakEnd := len(loud) + len(weak) // amostra onde a sílaba fraca termina

	d := decideTrim(x, rate, 200*time.Millisecond)
	if d.Refused {
		t.Fatalf("recusou sinal com sílaba fraca no fim: %s", d.Reason)
	}

	// A detecção tem de ter reconhecido a sílaba fraca como fala — não só
	// "por sorte" ter sobrado por causa da margem.
	if d.SpeechEnd < len(loud)+len(weak)/2 {
		t.Errorf("SpeechEnd = %d, esperava que a detecção alcançasse a sílaba fraca (que vai até %d)", d.SpeechEnd, weakEnd)
	}

	keptEnd := len(x) - d.EndCut
	if keptEnd < weakEnd {
		t.Fatalf("cortou dentro da sílaba fraca: manteve até a amostra %d, mas ela vai até %d (comeu %d amostras dela)",
			keptEnd, weakEnd, weakEnd-keptEnd)
	}
}

// TestTrimSample_EscreveWAVQueRelidoBateAmostraAAmostra é a ida e volta:
// o WAV escrito por trimSample (via dataset.WriteWAV), relido por
// dataset.ReadWAV, tem de bater amostra a amostra com o que decideTrim
// decidiu manter — nenhuma conversão de ida e volta pode introduzir
// divergência na região preservada.
func TestTrimSample_EscreveWAVQueRelidoBateAmostraAAmostra(t *testing.T) {
	rate := 16000
	x := concat(
		segmento(rate, 1.0, 0.0005),
		segmento(rate, 2.0, 0.3),
		segmento(rate, 0.4, 0.0005),
	)
	raw := make([]int16, len(x))
	for i, v := range x {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		raw[i] = int16(math.Round(v * 32767))
	}

	dir := t.TempDir()
	audioPath := filepath.Join(dir, "s1.wav")
	if err := dataset.WriteWAV(audioPath, rate, raw); err != nil {
		t.Fatalf("setup WAV: %v", err)
	}
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("setup outDir: %v", err)
	}

	margin := 200 * time.Millisecond
	s := dataset.Sample{ID: "s1", AudioPath: audioPath}
	res, err := trimSample(s, outDir, margin)
	if err != nil {
		t.Fatalf("trimSample: %v", err)
	}

	decision := decideTrim(int16ToFloat64(raw), rate, margin)
	if decision.Refused {
		t.Fatalf("setup: decideTrim recusou o próprio fixture: %s", decision.Reason)
	}
	wantTrimmed := raw[decision.StartCut : len(raw)-decision.EndCut]

	_, gotSamples, err := dataset.ReadWAV(filepath.Join(outDir, "s1.wav"))
	if err != nil {
		t.Fatalf("relendo WAV escrito: %v", err)
	}
	if len(gotSamples) != len(wantTrimmed) {
		t.Fatalf("len(relido) = %d, want %d", len(gotSamples), len(wantTrimmed))
	}
	for i := range wantTrimmed {
		if gotSamples[i] != wantTrimmed[i] {
			t.Fatalf("amostra %d diverge: relido=%d, want=%d", i, gotSamples[i], wantTrimmed[i])
		}
	}

	wantAfter := time.Duration(float64(len(wantTrimmed)) / float64(rate) * float64(time.Second))
	if res.After != wantAfter {
		t.Errorf("res.After = %s, want %s (duração das amostras efetivamente escritas)", res.After, wantAfter)
	}
}
