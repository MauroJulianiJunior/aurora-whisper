package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

// TestFormatSupported_AceitaPCM32 fixa o defeito que quebrou o Agent numa
// máquina real: o microfone reportava PCM de 32 bits inteiro, e o código
// rejeitava por só esperar float32 ou PCM de 16 bits.
func TestFormatSupported_AceitaPCM32(t *testing.T) {
	cases := []struct {
		name    string
		isFloat bool
		bits    int
		want    bool
	}{
		{"float32", true, 32, true},
		{"pcm 16", false, 16, true},
		{"pcm 24", false, 24, true},
		{"pcm 32 — o caso que quebrou", false, 32, true},
		{"float de 64 bits não existe em mix format", true, 64, false},
		{"pcm 8 não é suportado", false, 8, false},
		{"bits zero", false, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSupported(tc.isFloat, tc.bits); got != tc.want {
				t.Fatalf("formatSupported(isFloat=%v, bits=%d) = %v, want %v", tc.isFloat, tc.bits, got, tc.want)
			}
		})
	}
}

const tolerance = 1e-6

func closeEnough(a, b float32) bool { return math.Abs(float64(a-b)) < tolerance }

// TestConvertPCM_Escalas prova que cada formato chega a [-1, 1] com o
// sinal certo. Testar só o tamanho do slice deixaria passar sinal
// invertido e escala errada — que é exatamente o tipo de defeito que
// produz áudio distorcido sem ninguém entender por quê.
func TestConvertPCM_Escalas(t *testing.T) {
	t.Run("int16", func(t *testing.T) {
		// Padrões de bits escritos direto: num teste de nível de byte isso
		// é mais honesto que conversão de constante, e evita a armadilha de
		// uint16(int16(-32768)) não ser expressão constante válida em Go.
		raw := make([]byte, 6)
		binary.LittleEndian.PutUint16(raw[0:], 0x0000) // 0
		binary.LittleEndian.PutUint16(raw[2:], 0x8000) // -32768, o mínimo
		binary.LittleEndian.PutUint16(raw[4:], 0x4000) // +16384, metade do positivo

		got := convertPCM(raw, false, 16, 3)
		want := []float32{0, -1, 0.5}
		for i := range want {
			if !closeEnough(got[i], want[i]) {
				t.Fatalf("amostra %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("int32 — o formato da máquina que quebrou", func(t *testing.T) {
		raw := make([]byte, 12)
		binary.LittleEndian.PutUint32(raw[0:], 0x00000000) // 0
		binary.LittleEndian.PutUint32(raw[4:], 0x80000000) // mínimo do int32
		binary.LittleEndian.PutUint32(raw[8:], 0x40000000) // metade do positivo máximo

		got := convertPCM(raw, false, 32, 3)
		want := []float32{0, -1, 0.5}
		for i := range want {
			if !closeEnough(got[i], want[i]) {
				t.Fatalf("amostra %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("int24 com extensão de sinal", func(t *testing.T) {
		// 0, -8388608 (mínimo), +4194304 (metade do positivo máximo).
		raw := []byte{
			0x00, 0x00, 0x00,
			0x00, 0x00, 0x80,
			0x00, 0x00, 0x40,
		}
		got := convertPCM(raw, false, 24, 3)
		want := []float32{0, -1, 0.5}
		for i := range want {
			if !closeEnough(got[i], want[i]) {
				t.Fatalf("amostra %d = %v, want %v — sinal provavelmente não foi estendido", i, got[i], want[i])
			}
		}
	})

	t.Run("float32 passa direto", func(t *testing.T) {
		raw := make([]byte, 12)
		binary.LittleEndian.PutUint32(raw[0:], math.Float32bits(0))
		binary.LittleEndian.PutUint32(raw[4:], math.Float32bits(-1))
		binary.LittleEndian.PutUint32(raw[8:], math.Float32bits(0.5))

		got := convertPCM(raw, true, 32, 3)
		want := []float32{0, -1, 0.5}
		for i := range want {
			if !closeEnough(got[i], want[i]) {
				t.Fatalf("amostra %d = %v, want %v", i, got[i], want[i])
			}
		}
	})
}

// TestInt24LE_ExtensaoDeSinal isola a extensão de sinal, que é o ponto
// exato onde uma implementação ingênua de 24 bits erra: sem estender o
// bit 23, toda amostra negativa vira um positivo enorme.
func TestInt24LE_ExtensaoDeSinal(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want int32
	}{
		{"zero", []byte{0x00, 0x00, 0x00}, 0},
		{"positivo máximo", []byte{0xFF, 0xFF, 0x7F}, 8388607},
		{"negativo mínimo", []byte{0x00, 0x00, 0x80}, -8388608},
		{"menos um", []byte{0xFF, 0xFF, 0xFF}, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := int24LE(tc.in); got != tc.want {
				t.Fatalf("int24LE(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestConvertPCM_PacoteTruncadoNaoEntraEmPanico: um pacote curto vindo do
// driver não pode derrubar o Agent. O que falta vira silêncio.
func TestConvertPCM_PacoteTruncadoNaoEntraEmPanico(t *testing.T) {
	raw := []byte{0x00, 0x00} // uma amostra de 16 bits, mas pedimos quatro
	got := convertPCM(raw, false, 16, 4)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	for i := 1; i < 4; i++ {
		if got[i] != 0 {
			t.Fatalf("amostra %d = %v, want 0 (silêncio) para pacote truncado", i, got[i])
		}
	}
}
