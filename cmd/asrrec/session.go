package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// userAction é a resposta do usuário a um dos dois prompts do fluxo:
// "ENTER grava / s pula / q sai" e, depois de uma tomada,
// "ENTER aceita / r regrava / s pula / q sai". O mesmo tipo serve para os
// dois porque as respostas ("", "r", "s", "q") não mudam — o que muda é
// qual delas cada prompt aceita (ver readAction em cli.go).
type userAction int

const (
	// actionAccept é ENTER puro: no primeiro prompt significa "começar a
	// gravar"; no segundo, "aceitar a tomada e ir para a próxima".
	actionAccept userAction = iota
	actionRedo
	actionSkip
	actionQuit
	actionUnknown
)

// parseUserAction interpreta uma linha de resposta do usuário. TrimSpace +
// case-insensitive, então " R\n", "r" e "R" são equivalentes — bufio.Scanner
// já removeu o '\n' antes de chegar aqui, mas espaço em volta ainda é
// comum (Tab/espaço acidental antes do Enter). Qualquer coisa fora deste
// conjunto de quatro vira actionUnknown; cli.go pede de novo em vez de
// adivinhar a intenção.
func parseUserAction(input string) userAction {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "":
		return actionAccept
	case "r":
		return actionRedo
	case "s":
		return actionSkip
	case "q":
		return actionQuit
	default:
		return actionUnknown
	}
}

// formatProgress produz a linha de progresso mostrada antes de cada frase:
// "[índice/total] categoria: <categoria>", índice 1-based — o formato
// pedido literalmente na tarefa ("[12/36] categoria: contabilidade").
func formatProgress(index, total int, category dataset.Category) string {
	return fmt.Sprintf("[%d/%d] categoria: %s", index, total, category)
}

// sampleDuration converte uma contagem de amostras PCM em duração, dada a
// taxa de amostragem — o mesmo cálculo que dataset.Duration faz a partir
// do WAV já escrito, só que direto do slice em memória, antes de
// WriteWAV, para mostrar a duração da tomada no prompt de aceitar/regravar.
func sampleDuration(numSamples, sampleRate int) time.Duration {
	if sampleRate <= 0 {
		return 0
	}
	return time.Duration(numSamples) * time.Second / time.Duration(sampleRate)
}

// Limiares de recordingWarnings. São picos/proporções, não RMS — o VAD de
// internal/agent/audio (DefaultVADThreshold = 0.03 RMS) já decide
// HasSpeech; o que falta aqui é avisar sobre problemas de NÍVEL que o VAD
// não cobre: áudio audível mas fraco demais, e áudio tão forte que estourou.
const (
	// lowVolumePeakRatio: se a amostra de maior amplitude da gravação não
	// passar desta fração do fundo de escala int16, o sinal está baixo
	// demais para confiar no ASR — mesmo com HasSpeech=true (fala baixa,
	// mas com energia suficiente pra passar do limiar de 20ms do VAD, pode
	// ainda assim render um WAV ruim para transcrição).
	lowVolumePeakRatio = 0.10

	// clippingCeiling: amostra "encostando no teto" do fundo de escala
	// int16 (32767 positivo / -32768 negativo). Não usamos exatamente
	// ±32768/32767 porque um pico isolado ali pode ser legítimo; o que
	// indica clipping de verdade é uma FRAÇÃO de amostras nessa vizinhança
	// (ver clippingSampleRatio), não uma amostra isolada.
	clippingCeiling int16 = 32760

	// clippingSampleRatio: fração mínima de amostras no teto pra soar o
	// alarme de clipping — abaixo disso é ruído/coincidência, não ganho de
	// entrada estourando a forma de onda.
	clippingSampleRatio = 0.001
)

// recordingWarnings inspeciona o PCM de uma tomada recém-gravada e o
// VADSummary que Session.Stop devolveu, e lista avisos em português para o
// dono do produto revisar antes de decidir aceitar ou regravar. Nunca
// bloqueia o fluxo — só avisa; quem decide é a pessoa, no prompt seguinte.
func recordingWarnings(samples []int16, vad dictation.VADSummary) []string {
	if !vad.HasSpeech || len(samples) == 0 {
		return []string{"silêncio: nenhuma fala detectada"}
	}

	// peak em int32: negar o int16 mínimo (-32768) transbordaria de volta
	// para -32768 em aritmética de 16 bits (não existe +32768 em int16) —
	// o mesmo cuidado de sinal que internal/agent/audio/pcm.go documenta
	// para int24LE. Fazendo a conta em int32 isso não acontece.
	var peak int32
	var clipped int
	for _, s := range samples {
		v := int32(s)
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
		if s >= clippingCeiling || s <= -clippingCeiling {
			clipped++
		}
	}

	var warnings []string
	if float64(peak) < 32768*lowVolumePeakRatio {
		warnings = append(warnings, "áudio muito baixo (pico abaixo de 10% do fundo de escala)")
	}
	if float64(clipped)/float64(len(samples)) >= clippingSampleRatio {
		warnings = append(warnings, "possível clipping (forma de onda encostando no teto do sinal)")
	}
	return warnings
}

// levelBarWidth é a largura, em caracteres, do indicador de nível ao vivo.
const levelBarWidth = 30

// levelBar desenha uma barra de largura fixa a partir de rms (0..1, a
// mesma escala de internal/agent/audio.Frame.RMS). Grampeia rms fora da
// faixa — RMS pode passar de 1.0 num pico de clipping — porque
// strings.Repeat com contagem negativa entra em pânico, e o indicador
// visual nunca deveria derrubar a sessão de gravação por causa disso.
func levelBar(rms float64) string {
	if rms < 0 {
		rms = 0
	} else if rms > 1 {
		rms = 1
	}
	filled := int(rms * levelBarWidth)
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", levelBarWidth-filled) + "]"
}
