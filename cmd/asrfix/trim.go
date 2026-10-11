package main

import (
	"fmt"
	"math"
	"time"
)

// Aparagem do pre-roll das gravações do dataset.
//
// # O que está errado nas gravações
//
// Quando estas gravações foram feitas, audio.Config usava DefaultPreRoll
// de 2 SEGUNDOS, dimensionado para o ditado: lá a pessoa apertava a tecla
// já falando, e o áudio anterior ao gatilho era o começo da frase
// (AC-003). Hoje o padrão é 300 ms, e só no modo "sempre pronto" — no
// ditado, o microfone só liga no keydown e o pre-roll é zero (ADR-015); o
// cmd/asrrec pede 300 ms explicitamente. O cmd/asrrec herdou aquele default,
// mas a interação dele é a oposta — a pessoa aperta ENTER e SÓ ENTÃO lê a
// frase da tela.
//
// O efeito não é o que parecia à primeira vista. A captura é CONTÍNUA
// entre gravações: o mesmo audio.Session fica aberto do começo ao fim da
// sessão, e cada amostra chama MarkStart/Stop sobre o mesmo ring buffer.
// Então os 2 s anteriores ao ENTER da gravação N são, com frequência, o
// FINAL DA FRASE ANTERIOR.
//
// Medido nos 46 arquivos: 26 dos 45 pares consecutivos compartilham
// amostras IDÊNTICAS, amostra a amostra, de até 1,01 s. Não é ruído de
// sala nem silêncio — é a voz da pessoa terminando a frase de antes.
//
// Isso é pior do que áudio inútil. Um ASR transcreve essas palavras, elas
// não estão na referência, e o WER sobe por um defeito da gravação que
// seria creditado ao modelo.
//
// # Por que cortar é seguro
//
// Session.Stop devolve a partir de `mark - preRollSamples`, e `mark` é o
// instante do ENTER. A pessoa lê a frase DEPOIS de apertar. Logo, por
// construção, os 2 s iniciais não podem conter a frase deste arquivo.
//
// A primeira versão desta aparagem recusava cortar quando havia som alto
// no trecho, presumindo que som alto ali era fala desta frase. A premissa
// era falsa, e o resultado era recusar exatamente os arquivos MAIS
// contaminados. Som alto no pre-roll é a evidência de que o corte é
// necessário, não o motivo para não cortar.
//
// # O que ainda precisa de trava
//
// O ring buffer grampeia `from` quando a gravação começa com menos de 2 s
// de sessão aberta — o caso da primeira amostra de cada sessão. Aí o
// arquivo tem menos pre-roll que o nominal, e um corte fixo comeria
// sílaba. É contra ISSO que a verificação abaixo existe: ela olha o que
// sobra DEPOIS do corte e recusa se a fala já começar colada no ponto de
// corte, que é a assinatura de ter cortado no meio dela.

// trimDecision é o que decidirTrim concluiu para uma amostra.
type trimDecision struct {
	Cortar          int     // amostras a descartar do início (0 = não cortar)
	PicoNoTrecho    float64 // pico do trecho cortado, escala 0..1
	PicoNoArquivo   float64 // pico do arquivo inteiro, para comparação
	ContaminadoFrac float64 // pico do trecho ÷ pico do arquivo
	Recusado        bool
	Motivo          string
}

// margemPosCorte é quanto de áudio se olha de cada lado do ponto de corte
// para decidir se há fala atravessando o corte. 80 ms é cerca de uma
// sílaba.
//
// ESCOLHA POR JULGAMENTO, não medição.
const margemPosCorte = 80 * time.Millisecond

// fracaoDeFalaNaMargem: uma margem conta como "falando" se o pico dela
// passar desta fração do pico do que sobrou depois do corte.
//
// ESCOLHA POR JULGAMENTO, não medição.
const fracaoDeFalaNaMargem = 0.35

// decidirTrim decide se dá para descartar `dur` do início de samples.
func decidirTrim(samples []float64, sampleRate int, dur time.Duration) trimDecision {
	// dur <= 0 cai aqui junto com dur pequeno demais para render uma
	// amostra: n vira zero ou negativo nos dois casos.
	n := int(dur.Seconds() * float64(sampleRate))
	if n <= 0 {
		return trimDecision{}
	}
	if n >= len(samples)/2 {
		return trimDecision{
			Recusado: true,
			Motivo: fmt.Sprintf("aparagem de %s cobriria %d de %d amostras (metade ou mais do arquivo)",
				dur, n, len(samples)),
		}
	}

	picoTrecho := picoAbs(samples[:n])
	picoArquivo := picoAbs(samples)
	resto := samples[n:]

	// A trava olha os DOIS lados do corte, e não só o de depois.
	//
	// Olhar só depois não discrimina: se a fala começa exatamente no ponto
	// de corte, isso tanto pode ser "cortei no instante certo" quanto
	// "cortei no meio dela", e recusar os dois casos produz recusa falsa.
	//
	// O que distingue é o lado de ANTES. Se o corte caiu no meio de uma
	// fala, há som dos dois lados, contínuo. Se caiu num limite natural,
	// o fim do trecho cortado está em silêncio — que é o caso real: a
	// pessoa parou de falar, revisou a tomada, e só então apertou ENTER.
	m := int(margemPosCorte.Seconds() * float64(sampleRate))
	if m > 0 && m < len(resto) && m <= n {
		picoAntes := picoAbs(samples[n-m : n])
		picoDepois := picoAbs(resto[:m])
		picoResto := picoAbs(resto)
		if picoResto > 0 &&
			picoAntes > picoResto*fracaoDeFalaNaMargem &&
			picoDepois > picoResto*fracaoDeFalaNaMargem {
			return trimDecision{
				PicoNoTrecho:  picoTrecho,
				PicoNoArquivo: picoArquivo,
				Recusado:      true,
				Motivo: fmt.Sprintf("há fala contínua atravessando o ponto de corte "+
					"(pico %.5f nos %s antes e %.5f nos %s depois, contra %.5f no resto) — "+
					"o corte entrou no meio da frase", picoAntes, margemPosCorte, picoDepois, margemPosCorte, picoResto),
			}
		}
	}

	d := trimDecision{Cortar: n, PicoNoTrecho: picoTrecho, PicoNoArquivo: picoArquivo}
	if picoArquivo > 0 {
		d.ContaminadoFrac = picoTrecho / picoArquivo
	}
	return d
}

func picoAbs(x []float64) float64 {
	var p float64
	for _, v := range x {
		if a := math.Abs(v); a > p {
			p = a
		}
	}
	return p
}
