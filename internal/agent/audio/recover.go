package audio

import "math"

// Este arquivo NÃO faz parte do caminho normal de captura/resampling
// (internal/agent/audio/pcm.go e resampler.go continuam sendo a única
// forma como o Agent processa áudio ao vivo). Ele existe para consertar
// os efeitos de um defeito específico, já corrigido, de layout da struct
// WAVEFORMATEXTENSIBLE: antes da correção, a conversão de amostra em
// pcm.go lia os 4 bytes de cada float32 capturado e os reinterpretava
// como int32 com sinal — em vez do valor real, o pipeline gravava o
// padrão de bits IEEE-754 da amostra, normalizado por 2^31. O resampler
// então converteu esse valor (já sem relação matemática direta com o
// áudio original) para int16 do jeito de sempre.
//
// Essa cadeia é, por amostra, BIJETIVA no passo bits(f)->int32/2^31 (não
// há perda de informação de sinal+expoente+mantissa, só uma
// reinterpretação), e o passo seguinte para int16 preserva os 16 bits
// mais altos desse padrão — sinal, expoente e 7 bits de mantissa, o
// suficiente para uma precisão relativa de amplitude melhor que 1%.
// RecoverBitPatternFloat32 desfaz essa cadeia inteira.
//
// As 46 gravações de testdata/asr-dataset/audio/*.wav foram feitas
// enquanto o defeito estava presente — são o único motivo desta função
// existir. cmd/asrfix é o único chamador de produção. Quando o dataset
// for regravado com o Agent já corrigido, RecoverBitPatternFloat32 e
// cmd/asrfix deixam de ter propósito e devem ser removidos; eles não são
// uma peça permanente da arquitetura de áudio.

// RecoverBitPatternFloat32 desfaz, amostra a amostra, a corrupção descrita
// acima: a partir de um int16 gravado pelo pipeline com o defeito, refaz
// x = s/32767 (a mesma convenção — dividir pelo valor POSITIVO máximo —
// que o resampler usou ao converter para int16, floatToInt16 em
// resampler.go), escala x de volta para a faixa de um int32 com sinal
// (multiplicando por 2^31, com clamp para nunca estourar o tipo) e
// reinterpreta os 32 bits resultantes como float32 — recuperando o
// padrão de bits original da amostra antes do defeito.
//
// Um resultado NaN ou infinito (só possível para padrões de bits que o
// clamp em ±2^31 força a cair exatamente no maior expoente IEEE-754, o
// reservado para NaN/Inf) vira silêncio (0) em vez de propagar um valor
// que não corresponde a nenhuma amostra de áudio real.
func RecoverBitPatternFloat32(samples []int16) []float32 {
	out := make([]float32, len(samples))
	for i, s := range samples {
		out[i] = recoverOneBitPattern(s)
	}
	return out
}

func recoverOneBitPattern(s int16) float32 {
	const int32Scale = 2147483648 // 2^31

	x := float64(s) / 32767
	scaled := math.Round(x * int32Scale)
	if scaled > 2147483647 {
		scaled = 2147483647
	}
	if scaled < -2147483648 {
		scaled = -2147483648
	}

	f := math.Float32frombits(uint32(int32(scaled)))
	if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
		return 0
	}
	return f
}

// FloatToInt16 converte v — esperado em [-1, 1], como toda amostra normal
// deste pacote — para int16 com a mesma convenção usada em todo o resto da
// captura: clamp em [-1, 1] e depois round(v*32767) (o floatToInt16 não
// exportado de resampler.go, arquivo que esta tarefa deliberadamente não
// toca). Existe para que cmd/asrfix, ao regravar em int16 o áudio já
// recuperado por RecoverBitPatternFloat32, use exatamente a mesma escala
// que o resto do pacote em vez de reinventar a constante à parte.
func FloatToInt16(v float32) int16 {
	if v > 1 {
		v = 1
	} else if v < -1 {
		v = -1
	}
	return int16(math.Round(float64(v) * 32767))
}
