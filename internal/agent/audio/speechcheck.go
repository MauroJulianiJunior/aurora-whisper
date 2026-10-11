package audio

import (
	"math"
	"sort"
)

// Este arquivo existe para UMA finalidade: dar a [RecoverBitPatternFloat32]
// (recover.go) e ao cmd/asrfix um jeito de PROVAR que um sinal recuperado
// tem estrutura de fala, em vez de só afirmar isso. Não é VAD de produção
// — isso é [ClassifyVAD] (vad.go), que mede energia por janela e roda no
// caminho real de captura. AnalyzeF0 mede outra coisa: continuidade de
// pitch por autocorrelação, que é o que sobrevive à corrupção de bits
// descrita em recover.go mas NÃO sobrevive a embaralhar as mesmas
// amostras — é essa diferença que separa "reconstruiu fala" de
// "reconstruiu uma distribuição de valores parecida com fala"
// (recover_test.go prova exatamente isso).

// Limiares de decisão abaixo — TODOS escolha de julgamento, não medição.
// Foram calibrados olhando para: (a) fala sintética limpa (~95% vozeado);
// (b) ruído branco e as mesmas amostras embaralhadas (0% vozeado); (c) os
// 46 arquivos reais do dataset, cuja fração vozeada após recuperação varia
// por categoria (fala normal ~22–39% nas amostras inspecionadas
// manualmente; "ruído moderado" e trechos muito curtos tendem a ficar mais
// baixo). Ver relatório da slice para os números medidos.
const (
	// pitchMinHz / pitchMaxHz: faixa de F0 de voz humana considerada pela
	// busca de autocorrelação — pedida explicitamente na tarefa (80–200 Hz
	// cobre vozes graves e agudas).
	pitchMinHz = 80.0
	pitchMaxHz = 200.0

	// voicedCorrelationThreshold: abaixo disso a autocorrelação não é
	// confiável o bastante para contar como pitch — valor pedido
	// explicitamente na tarefa.
	voicedCorrelationThreshold = 0.4

	// pitchFrameSamples / pitchHopSamples: janela de 32 ms com passo de
	// 16 ms a 16 kHz (512/256 amostras). Curta o bastante para não
	// misturar sílabas vizinhas, longa o bastante para caber com folga
	// mais de um período de 80 Hz (200 amostras) dentro da janela. Mesmos
	// valores usados na exploração manual que validou a recuperação
	// (zz_decide_test.go).
	pitchFrameSamples = 512
	pitchHopSamples   = 256

	// minVoicedFraction: fração mínima de quadros vozeados para o sinal
	// contar como fala. 8% fica bem acima do 0% de ruído branco/amostras
	// embaralhadas e bem abaixo dos ~22–39% observados em fala normal
	// recuperada — dando folga para categorias mais difíceis do dataset
	// (ruído moderado, trechos curtos) sem deixar passar não-fala.
	minVoicedFraction = 0.08

	// minFrameRMSRatio: um quadro so pode ser vozeado se o RMS dele passar
	// desta fracao do RMS do quadro mais alto do sinal. E a porta de
	// amplitude descrita em AnalyzeF0.
	//
	// ESCOLHA POR JULGAMENTO, nao medicao. 5% sao ~26 dB abaixo do pico de
	// fala: deixa passar silaba fraca e final de frase, e barra ruido de
	// sala, que nos arquivos medidos ficou 30 a 50x abaixo do miolo falado.
	minFrameRMSRatio = 0.05

	// maxMedianAbsDeltaHz: mediana de |ΔF0| entre quadros vozeados
	// SEGUIDOS, em Hz, acima da qual o contorno de pitch não é
	// considerado contínuo. Fala real observada ficou abaixo de 1,2 Hz;
	// 5 Hz dá folga generosa mantendo o critério com poder de
	// discriminação (pitch tracking espúrio salta muito mais que isso
	// entre quadros vizinhos).
	maxMedianAbsDeltaHz = 5.0
)

// F0Profile resume a estrutura de pitch de um sinal, medida por
// autocorrelação normalizada.
type F0Profile struct {
	// TotalFrames é o número de janelas analisadas.
	TotalFrames int
	// VoicedFrames é quantas dessas janelas passaram do limiar de
	// correlação (voicedCorrelationThreshold) em algum atraso dentro da
	// faixa pitchMinHz..pitchMaxHz.
	VoicedFrames int
	// VoicedFraction é VoicedFrames/TotalFrames (0 se TotalFrames for 0).
	VoicedFraction float64
	// MedianF0Hz é a mediana de F0 entre os quadros vozeados. NaN se
	// nenhum quadro for vozeado.
	MedianF0Hz float64
	// MedianAbsDeltaHz é a mediana de |F0(t) - F0(t-1)| entre pares de
	// quadros vozeados CONSECUTIVOS (índice de janela adjacente — um
	// quadro vozeado seguido de um não vozeado não entra no par). NaN se
	// não houver nenhum par assim.
	MedianAbsDeltaHz float64
}

// IsSpeech decide, a partir do perfil, se o sinal tem estrutura de fala:
// fração vozeada suficiente (minVoicedFraction) E contorno de F0 contínuo
// (MedianAbsDeltaHz dentro de maxMedianAbsDeltaHz). Ambos os limiares são
// escolha de julgamento — ver o comentário do bloco de constantes acima.
func (p F0Profile) IsSpeech() bool {
	if p.VoicedFraction < minVoicedFraction {
		return false
	}
	if math.IsNaN(p.MedianAbsDeltaHz) {
		// Vozeado o bastante, mas sem dois quadros vozeados seguidos —
		// não há contorno nenhum para julgar continuidade, então não dá
		// para afirmar que é fala.
		return false
	}
	return p.MedianAbsDeltaHz <= maxMedianAbsDeltaHz
}

// AnalyzeF0 varre samples (qualquer sinal float64 normalizado, mono, na
// taxa sampleRate) em janelas de pitchFrameSamples/pitchHopSamples e
// resume a estrutura de pitch encontrada por autocorrelação normalizada.
//
// sampleRate escala a faixa de busca (pitchMinHz..pitchMaxHz) para
// amostras de atraso; a 16 kHz isso dá exatamente a faixa de 80 a 200
// amostras pedida na tarefa. len(samples) menor que uma janela devolve um
// F0Profile zerado (TotalFrames == 0).
func AnalyzeF0(samples []float64, sampleRate int) F0Profile {
	if sampleRate <= 0 || len(samples) < pitchFrameSamples {
		return F0Profile{MedianF0Hz: math.NaN(), MedianAbsDeltaHz: math.NaN()}
	}

	minLag := int(float64(sampleRate) / pitchMaxHz)
	maxLag := int(float64(sampleRate) / pitchMinHz)
	if minLag < 1 {
		minLag = 1
	}

	// Porta de amplitude, por quadro. SEM ela este detector mente.
	//
	// A autocorrelacao e NORMALIZADA, logo invariante a escala: ruido de
	// sala a -74 dBFS com qualquer periodicidade pontua igual a voz. Medido
	// nos proprios arquivos do dataset: os 2 s anteriores ao ENTER, onde
	// por construcao nao pode haver fala (Session.Stop devolve a partir de
	// mark - preRollSamples, e mark e o instante do ENTER), acusavam ate
	// 52% de quadros vozeados com RMS de 0,0002.
	//
	// LIMITE DESTA PORTA, e importa para quem chamar: a referencia e o
	// quadro mais alto DESTA CHAMADA. Passar um trecho so de silencio faz
	// o proprio ruido virar referencia, e ele volta a pontuar como voz.
	// Para perguntar "ha fala NESTE trecho de um sinal maior", compare a
	// amplitude do trecho com a do sinal inteiro em vez de chamar esta
	// funcao so com o trecho — e o que cmd/asrfix/trim.go faz.
	//
	// A porta compara cada quadro com o quadro MAIS ALTO do proprio sinal,
	// e nao com um valor absoluto: o ganho do microfone varia entre
	// gravacoes, e um limiar fixo classificaria uma gravacao baixa inteira
	// como silencio.
	rmsQuadro := make([]float64, 0, len(samples)/pitchHopSamples+1)
	for start := 0; start+pitchFrameSamples <= len(samples); start += pitchHopSamples {
		rmsQuadro = append(rmsQuadro, rmsDe(samples[start:start+pitchFrameSamples]))
	}
	var rmsMax float64
	for _, r := range rmsQuadro {
		if r > rmsMax {
			rmsMax = r
		}
	}
	porta := rmsMax * minFrameRMSRatio

	type voicedFrame struct {
		windowIndex int
		f0          float64
	}
	var voiced []voicedFrame
	total := 0
	for start := 0; start+pitchFrameSamples <= len(samples); start += pitchHopSamples {
		idx := start / pitchHopSamples
		total++
		if idx < len(rmsQuadro) && rmsQuadro[idx] < porta {
			continue // silencio: nao pode ser quadro vozeado, tenha a periodicidade que tiver
		}
		f0, corr := autocorrelationF0(samples[start:start+pitchFrameSamples], sampleRate, minLag, maxLag)
		if corr > voicedCorrelationThreshold && f0 > 0 {
			voiced = append(voiced, voicedFrame{windowIndex: idx, f0: f0})
		}
	}

	profile := F0Profile{
		TotalFrames:  total,
		VoicedFrames: len(voiced),
	}
	if total > 0 {
		profile.VoicedFraction = float64(len(voiced)) / float64(total)
	}

	var deltas, f0s []float64
	for i, v := range voiced {
		f0s = append(f0s, v.f0)
		if i > 0 && v.windowIndex == voiced[i-1].windowIndex+1 {
			deltas = append(deltas, math.Abs(v.f0-voiced[i-1].f0))
		}
	}
	profile.MedianF0Hz = median(f0s)
	profile.MedianAbsDeltaHz = median(deltas)
	return profile
}

// autocorrelationF0 estima F0 de um único quadro por autocorrelação
// normalizada, buscando o atraso entre minLag e maxLag (amostras) com
// maior correlação. Devolve (0, 0) se o quadro não tiver energia
// suficiente (silêncio) ou se nenhum atraso na faixa produzir correlação
// positiva.
func autocorrelationF0(frame []float64, sampleRate, minLag, maxLag int) (f0Hz, correlation float64) {
	n := len(frame)
	var mean float64
	for _, v := range frame {
		mean += v
	}
	mean /= float64(n)

	centered := make([]float64, n)
	var energy0 float64
	for i, v := range frame {
		c := v - mean
		centered[i] = c
		energy0 += c * c
	}
	if energy0 == 0 {
		return 0, 0
	}

	bestLag := 0
	var best float64
	for lag := minLag; lag <= maxLag && lag < n; lag++ {
		var num, energyLag float64
		for i := 0; i+lag < n; i++ {
			num += centered[i] * centered[i+lag]
			energyLag += centered[i+lag] * centered[i+lag]
		}
		if energyLag <= 0 {
			continue
		}
		r := num / math.Sqrt(energy0*energyLag)
		if r > best {
			best, bestLag = r, lag
		}
	}
	if bestLag == 0 {
		return 0, 0
	}
	return float64(sampleRate) / float64(bestLag), best
}

// median devolve a mediana de values (NaN se vazio). Não modifica values
// no lugar do chamador — trabalha sobre uma cópia, porque AnalyzeF0
// reaproveita os slices de origem só para leitura.
func median(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// AmplitudeStats devolve o pico absoluto, o RMS e o fator de crista
// (pico/RMS, 0 se RMS for 0) de samples — as três métricas amplitude que
// cmd/asrfix imprime antes/depois da recuperação.
func AmplitudeStats(samples []float64) (peak, rms, crestFactor float64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	var sumSquares float64
	for _, v := range samples {
		if a := math.Abs(v); a > peak {
			peak = a
		}
		sumSquares += v * v
	}
	rms = math.Sqrt(sumSquares / float64(len(samples)))
	if rms > 0 {
		crestFactor = peak / rms
	}
	return peak, rms, crestFactor
}

// rmsDe e a raiz do valor quadratico medio do quadro.
func rmsDe(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var a float64
	for _, v := range x {
		a += v * v
	}
	return math.Sqrt(a / float64(len(x)))
}
