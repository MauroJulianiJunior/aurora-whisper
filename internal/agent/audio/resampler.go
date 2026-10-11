package audio

import "math"

// SampleRate é a taxa de saída fixa de todo áudio que sai deste pacote:
// 16 kHz mono int16 (ADR-010 D1). É o formato que internal/asr consome.
const SampleRate = 16000

// Resampler converte um fluxo de áudio PCM float32 intercalado, numa taxa
// e contagem de canais arbitrárias (tipicamente o mix format do WASAPI:
// float32 estéreo a 44,1 ou 48 kHz), para 16 kHz mono int16.
//
// É stateful de propósito: chamadas sucessivas de Process, alimentadas com
// pedaços consecutivos do mesmo fluxo, produzem saída contínua, sem clique
// nem amostra perdida na fronteira entre um []float32 e o próximo — é
// assim que o WASAPI entrega áudio, um pacote por vez.
//
// Reamostragem é por interpolação linear entre as duas amostras mono mais
// próximas. É matemática pura, deliberadamente simples: não há filtro
// anti-aliasing, o que é uma simplificação aceita para fala capturada de
// microfone (ADR-010: "matemática pura, inteiramente testável"). Um
// upgrade para um filtro FIR fica para quando/se a qualidade percebida do
// ASR pedir.
//
// Não é seguro para uso concorrente.
type Resampler struct {
	srcRate  int
	channels int
	ratio    float64 // amostras de origem por amostra de saída

	pos      float64 // posição fracionária de leitura, carregada entre chamadas
	prevMono float32
	havePrev bool
}

// NewResampler cria um Resampler para um fluxo de origem com a taxa e
// contagem de canais dadas. channels menor que 1 é tratado como 1 (mono).
func NewResampler(srcRate, channels int) *Resampler {
	if channels < 1 {
		channels = 1
	}
	if srcRate < 1 {
		srcRate = SampleRate
	}
	return &Resampler{
		srcRate:  srcRate,
		channels: channels,
		ratio:    float64(srcRate) / float64(SampleRate),
	}
}

// Process converte in (amostras float32 intercaladas, r.channels canais,
// r.srcRate Hz) para 16 kHz mono int16, com clamp em [-1.0, 1.0] antes da
// conversão para a escala int16 — nenhuma amostra de origem fora dessa
// faixa consegue estourar o tipo de saída.
//
// Cada chamada sempre segura a amostra mais recente para interpolar com o
// início do próximo pacote (é o que garante a continuidade descrita
// acima), então a saída fica até 1 amostra "atrás" do que já foi
// alimentado enquanto nenhum pacote novo chega. Para um fluxo contínuo
// (que é o único caso real de uso aqui — o dispositivo nunca para de
// mandar pacotes) isso é irrelevante: a amostra retida aparece assim que
// o próximo Process roda.
func (r *Resampler) Process(in []float32) []int16 {
	if len(in) == 0 || r.channels == 0 {
		return nil
	}
	n := len(in) / r.channels
	if n == 0 {
		return nil
	}

	mono := make([]float32, n)
	for i := 0; i < n; i++ {
		var sum float32
		base := i * r.channels
		for c := 0; c < r.channels; c++ {
			sum += in[base+c]
		}
		mono[i] = sum / float32(r.channels)
	}

	// extended prepende a última amostra do lote anterior, para que a
	// interpolação na primeira amostra nova seja contínua com o fim do
	// lote anterior, em vez de reiniciar do zero a cada chamada.
	var extended []float32
	if r.havePrev {
		extended = make([]float32, n+1)
		extended[0] = r.prevMono
		copy(extended[1:], mono)
	} else {
		extended = mono
	}

	m := len(extended) - 1
	if m <= 0 {
		r.prevMono = extended[len(extended)-1]
		r.havePrev = true
		return nil
	}

	var out []int16
	pos := r.pos
	for {
		i0 := int(pos)
		if i0+1 > m {
			break
		}
		frac := float32(pos - float64(i0))
		s := extended[i0] + frac*(extended[i0+1]-extended[i0])
		out = append(out, floatToInt16(s))
		pos += r.ratio
	}

	// O que sobrou de pos, medido a partir da última amostra conhecida
	// (extended[m]), vira o ponto de partida da próxima chamada — é isso
	// que faz a costura entre chamadas não perder nem duplicar amostra.
	r.pos = pos - float64(m)
	r.prevMono = extended[m]
	r.havePrev = true
	return out
}

// Reset descarta o estado de continuidade entre chamadas — usar ao trocar
// de fluxo de origem (ex.: dispositivo reaberto) sem recriar o Resampler.
func (r *Resampler) Reset() {
	r.pos = 0
	r.prevMono = 0
	r.havePrev = false
}

func floatToInt16(s float32) int16 {
	if s > 1 {
		s = 1
	} else if s < -1 {
		s = -1
	}
	return int16(math.Round(float64(s) * 32767))
}
