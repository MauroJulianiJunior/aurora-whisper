package audio

import (
	"encoding/binary"
	"math"
)

// Este arquivo não tem build tag de propósito: converter amostras é
// matemática pura, e é onde moram os erros silenciosos (escala errada,
// sinal invertido, estouro). Ficando fora de //go:build windows, roda e é
// testado também no Linux.

// formatSupported reporta se o mix format negociado com o WASAPI é um dos
// que sabemos converter.
//
// CORREÇÃO (17-set-2026): o comentário anterior aqui dizia que o suporte a
// PCM de 32 bits foi acrescentado porque "o microfone da máquina real
// reporta PCM de 32 bits inteiro". **Isso era falso, e a falsidade custou
// caro.**
//
// O microfone sempre reportou float32. Quem dizia "PCM de 32 bits" era o
// nosso próprio código: o SubFormat da WAVEFORMATEXTENSIBLE era lido no
// offset errado (ver mixformat.go), isFloat vinha false, e o formato
// aparecia como PCM 32. Ao ver o Agent recusar o formato, a resposta foi
// ACEITAR PCM 32 — o que fez o sintoma sumir e transformou uma recusa
// barulhenta em corrupção muda. A partir daí a captura passou a gravar o
// padrão de bits IEEE-754 de cada amostra no lugar do áudio, por dois
// sprints, até um dataset inteiro sair sem voz.
//
// A lição não é sobre formatos de áudio: quando o código recusa algo e a
// correção é afrouxar a recusa, primeiro é preciso provar que a recusa
// estava errada. Aqui ela estava certa — errado estava o que ela recebeu.
//
// Os ramos de 24 e 32 bits continuam porque são formatos legais que algum
// driver pode expor de verdade. O que os tornava perigosos era a detecção
// quebrada a montante, e é lá que foi consertado.
func formatSupported(isFloat bool, bits int) bool {
	if isFloat {
		return bits == 32
	}
	switch bits {
	case 16, 24, 32:
		return true
	default:
		return false
	}
}

// Escalas de conversão para [-1, 1]. Dividir pelo valor NEGATIVO máximo
// (2^(n-1)) é a convenção usual: -32768/32768 dá exatamente -1.0, e o
// positivo máximo fica um passo abaixo de 1.0. O caminho inverso
// (dividir por 32767) faria -32768 estourar para menos de -1.
const (
	scaleInt16 = 1 << 15
	scaleInt24 = 1 << 23
	scaleInt32 = 1 << 31
)

// convertPCM converte raw — amostras intercaladas no formato negociado —
// para float32 em [-1, 1]. Devolve exatamente samples valores; se raw for
// curto demais, o restante fica em zero (silêncio) em vez de entrar em
// pânico, porque um pacote truncado do driver não deve derrubar o Agent.
func convertPCM(raw []byte, isFloat bool, bits, samples int) []float32 {
	out := make([]float32, samples)
	width := bits / 8
	if width <= 0 {
		return out
	}

	for i := 0; i < samples; i++ {
		off := i * width
		if off+width > len(raw) {
			break // pacote truncado: o resto permanece silêncio
		}
		chunk := raw[off : off+width]

		switch {
		case isFloat:
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(chunk))
		case bits == 16:
			out[i] = float32(int16(binary.LittleEndian.Uint16(chunk))) / scaleInt16
		case bits == 24:
			out[i] = float32(int24LE(chunk)) / scaleInt24
		case bits == 32:
			out[i] = float32(int32(binary.LittleEndian.Uint32(chunk))) / scaleInt32
		}
	}
	return out
}

// int24LE lê 3 bytes little-endian como inteiro de 24 bits com sinal,
// estendendo o sinal para int32.
//
// O bit 23 é o de sinal. Sem a extensão, uma amostra negativa viraria um
// número positivo enorme — e o áudio sairia distorcido de um jeito que
// passa despercebido num teste que só confira o tamanho do slice.
func int24LE(b []byte) int32 {
	v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
	if v&0x800000 != 0 {
		v |= ^0xFFFFFF // estende o sinal para os 8 bits de cima
	}
	return v
}
