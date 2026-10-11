package main

import (
	"fmt"
	"math"
	"time"
)

// Detecção de borda de fala e decisão de corte — o mesmo problema que
// cmd/asrfix/trim.go resolve (cortar margem de gravação sem comer
// sílaba), com o mesmo padrão de decisão-com-trava: preferir recusar a
// adivinhar quando a detecção não é confiável.
//
// # Por que não chamar audio.AnalyzeF0 ou audio.AmplitudeStats direto para achar a borda
//
// AnalyzeF0 (internal/agent/audio/speechcheck.go) devolve um F0Profile
// AGREGADO — fração vozeada, mediana de F0, mediana de salto entre
// quadros — e nunca expõe EM QUE QUADRO a fala começa ou termina; ela
// responde "este sinal parece fala", não "a fala começa em que amostra".
// audio.AmplitudeStats também é agregado: pico/RMS/fator de crista do
// slice inteiro, sem posição nenhuma. Nenhuma das duas serve para
// localizar borda — só para validar um sinal já isolado. Por isso o
// scanner abaixo é próprio, mas aplica o MESMO princípio que o comentário
// de AnalyzeF0 insiste em destacar: a referência de amplitude tem de ser
// o pico do ARQUIVO INTEIRO, nunca de um recorte — senão ruído de sala
// vira sua própria referência e volta a contar como fala.

const (
	// windowDur / hopDur: janela de energia usada para varrer o arquivo em
	// busca da borda da fala. Os mesmos 32 ms / 16 ms que
	// internal/agent/audio/speechcheck.go usa para AnalyzeF0
	// (pitchFrameSamples/pitchHopSamples a 16 kHz) — não por acaso:
	// dessincronizar os dois só tornaria os números menos comparáveis
	// entre asrtrim e o restante do diagnóstico de fala do projeto, sem
	// ganho nenhum.
	windowDur = 32 * time.Millisecond
	hopDur    = 16 * time.Millisecond

	// speechEnergyRatio: uma janela conta como fala se o RMS dela passar
	// desta fração do RMS da janela MAIS ALTA DO ARQUIVO INTEIRO. Mesmo
	// valor de minFrameRMSRatio em internal/agent/audio/speechcheck.go,
	// calibrado lá medindo os 46 arquivos reais deste dataset: ~26 dB
	// abaixo do pico deixa passar sílaba fraca e final de frase, e barra
	// ruído de sala, que nos arquivos medidos ficou de 30 a 50x abaixo do
	// miolo falado.
	//
	// ESCOLHA POR JULGAMENTO — herdada da medição feita em
	// speechcheck.go, não remedida aqui.
	speechEnergyRatio = 0.05

	// minSpeechFraction: a fala detectada (antes de aplicar a margem) tem
	// de ocupar ao menos esta fração da duração do arquivo. Abaixo disso a
	// detecção não é confiável o bastante para cortar — recusa em vez de
	// adivinhar.
	//
	// ESCOLHA POR JULGAMENTO, não medição. 5% fica bem abaixo de qualquer
	// amostra real do dataset (a de menor proporção de fala inspecionada,
	// curta-01, tem cerca de 49% do arquivo em fala) — isto só pega
	// detecção quebrada ou arquivo sem fala nenhuma.
	minSpeechFraction = 0.05

	// maxRemovalFraction: nenhum lado do corte pode remover mais que esta
	// fração da duração do arquivo. Espelha o mesmo padrão de guarda de
	// cmd/asrfix/trim.go (recusa quando o corte cobriria metade ou mais do
	// arquivo): o silêncio de borda real medido neste dataset é bem menor
	// — mediana de 1,16 s no início e 0,25 s no fim.
	//
	// ESCOLHA POR JULGAMENTO, não medição.
	maxRemovalFraction = 0.5
)

// speechBounds é a região de fala localizada num sinal: [Start, End) em
// amostras. Found é false quando nenhuma janela do arquivo passou do
// limiar de energia — arquivo mudo, ou curto demais para caber uma única
// janela inteira.
type speechBounds struct {
	Start, End int
	Found      bool
}

// detectSpeechEdges varre samples (mono, float64 normalizado em [-1, 1])
// em janelas de windowDur/hopDur e devolve a primeira e a última janela
// cujo RMS passa de speechEnergyRatio do RMS da janela mais alta do
// PRÓPRIO ARQUIVO.
func detectSpeechEdges(samples []float64, sampleRate int) speechBounds {
	windowSamples := int(windowDur.Seconds() * float64(sampleRate))
	hopSamples := int(hopDur.Seconds() * float64(sampleRate))
	n := len(samples)
	if windowSamples < 1 || hopSamples < 1 || n < windowSamples {
		return speechBounds{}
	}

	var windowRMS []float64
	for start := 0; start+windowSamples <= n; start += hopSamples {
		windowRMS = append(windowRMS, rms(samples[start:start+windowSamples]))
	}

	var peak float64
	for _, r := range windowRMS {
		if r > peak {
			peak = r
		}
	}
	if peak == 0 {
		return speechBounds{} // silêncio absoluto: nenhuma janela tem energia
	}
	threshold := peak * speechEnergyRatio

	firstIdx, lastIdx := -1, -1
	for i, r := range windowRMS {
		if r >= threshold {
			if firstIdx == -1 {
				firstIdx = i
			}
			lastIdx = i
		}
	}
	if firstIdx == -1 {
		return speechBounds{}
	}

	start := firstIdx * hopSamples
	end := lastIdx*hopSamples + windowSamples
	if end > n {
		end = n
	}
	return speechBounds{Start: start, End: end, Found: true}
}

func rms(x []float64) float64 {
	var sum float64
	for _, v := range x {
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(x)))
}

// trimDecision é o que decideTrim concluiu para uma amostra: quanto
// descartar de cada lado, ou por que recusou.
type trimDecision struct {
	StartCut, EndCut       int // amostras a descartar de cada lado
	SpeechStart, SpeechEnd int // borda de fala detectada, ANTES da margem
	Refused                bool
	Reason                 string
}

// decideTrim decide quanto cortar de cada lado de samples para deixar
// exatamente margin de silêncio entre a borda do arquivo e a fala
// detectada. Se já houver menos que margin de silêncio de um lado, não
// inventa silêncio: não corta nada daquele lado. Recusa (com motivo) em
// vez de cortar quando a detecção não é confiável o bastante.
func decideTrim(samples []float64, sampleRate int, margin time.Duration) trimDecision {
	n := len(samples)
	bounds := detectSpeechEdges(samples, sampleRate)
	if !bounds.Found {
		return trimDecision{Refused: true, Reason: "nenhuma fala detectável no arquivo (silêncio total, ou curto demais para uma janela de análise)"}
	}

	speechSamples := bounds.End - bounds.Start
	if float64(speechSamples) < minSpeechFraction*float64(n) {
		return trimDecision{
			Refused: true,
			Reason: fmt.Sprintf("fala detectada ocupa só %.1f%% do arquivo (%d de %d amostras) — abaixo do mínimo de %.0f%%, detecção não é confiável",
				100*float64(speechSamples)/float64(n), speechSamples, n, 100*minSpeechFraction),
		}
	}

	marginSamples := int(margin.Seconds() * float64(sampleRate))
	if marginSamples < 0 {
		marginSamples = 0
	}

	var startCut, endCut int
	if bounds.Start >= marginSamples {
		startCut = bounds.Start - marginSamples
	}
	silenceAtEnd := n - bounds.End
	if silenceAtEnd >= marginSamples {
		endCut = silenceAtEnd - marginSamples
	}

	if float64(startCut) > maxRemovalFraction*float64(n) {
		return trimDecision{
			Refused: true,
			Reason: fmt.Sprintf("corte no início removeria %d de %d amostras (%.0f%% do arquivo), acima do limite de %.0f%%",
				startCut, n, 100*float64(startCut)/float64(n), 100*maxRemovalFraction),
		}
	}
	if float64(endCut) > maxRemovalFraction*float64(n) {
		return trimDecision{
			Refused: true,
			Reason: fmt.Sprintf("corte no fim removeria %d de %d amostras (%.0f%% do arquivo), acima do limite de %.0f%%",
				endCut, n, 100*float64(endCut)/float64(n), 100*maxRemovalFraction),
		}
	}
	if startCut+endCut >= n {
		return trimDecision{Refused: true, Reason: "o corte combinado dos dois lados cobriria o arquivo inteiro"}
	}

	return trimDecision{
		StartCut: startCut, EndCut: endCut,
		SpeechStart: bounds.Start, SpeechEnd: bounds.End,
	}
}
