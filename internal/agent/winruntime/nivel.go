package winruntime

import (
	"math"
	"time"
)

// Este arquivo não tem build tag: é aritmética sobre PCM, testável no
// runner Linux do CI como qualquer outra lógica pura deste pacote.

// silencioDBFS é o valor reportado para um buffer sem energia nenhuma.
// -inf não se lê bem num log estruturado e atrapalha comparação; -120
// dBFS está muito abaixo de qualquer coisa audível e serve de piso.
const silencioDBFS = -120.0

// nivelDBFS devolve o pico e o RMS de pcm em dBFS — decibéis relativos
// ao fundo de escala, onde 0 dBFS é a amplitude máxima que um int16
// representa.
//
// É a leitura que transforma "no_speech" em diagnóstico: microfone mudo
// dá pico perto de -90; fala normal perto do microfone dá pico entre -20
// e -6 (ver a medição registrada em overlay.WaveFloorDBFS).
func nivelDBFS(pcm []int16) (pico, rms float64) {
	if len(pcm) == 0 {
		return silencioDBFS, silencioDBFS
	}
	var maxAbs float64
	var somaQuadrados float64
	for _, s := range pcm {
		v := float64(s) / 32768
		if v < 0 {
			v = -v
		}
		if v > maxAbs {
			maxAbs = v
		}
		somaQuadrados += v * v
	}
	return paraDBFS(maxAbs), paraDBFS(math.Sqrt(somaQuadrados / float64(len(pcm))))
}

func paraDBFS(amplitude float64) float64 {
	if amplitude <= 0 {
		return silencioDBFS
	}
	db := 20 * math.Log10(amplitude)
	if db < silencioDBFS {
		return silencioDBFS
	}
	// Uma casa decimal basta para ler; mais vira ruído no log.
	return math.Round(db*10) / 10
}

// duracaoDe devolve quanto tempo de áudio as amostras contêm. Taxa zero
// — buffer malformado — devolve 0 em vez de dividir por zero.
func duracaoDe(pcm []int16, sampleRate int) time.Duration {
	if sampleRate <= 0 {
		return 0
	}
	return time.Duration(len(pcm)) * time.Second / time.Duration(sampleRate)
}

// dicaDeNivel traduz o pico medido na próxima coisa a verificar. Existe
// porque o log é lido por quem está com o problema na frente, não por
// quem escreveu o VAD: "pico_dbfs=-91,4" só ajuda quem já sabe o que
// esperar.
//
// Os cortes saem da medição de fala real desta máquina (46 amostras,
// ver overlay.WaveFloorDBFS): fala normal perto do microfone tem pico
// entre -20 e -6 dBFS, e o fundo entre palavras fica abaixo de -60.
func dicaDeNivel(picoDBFS float64) string {
	switch {
	case picoDBFS <= -80:
		return "silencio absoluto: microfone mudo, desativado no Windows, ou outro app com uso exclusivo"
	case picoDBFS <= -50:
		return "quase nada chegou: confira se o dispositivo de entrada certo esta selecionado e o volume nao esta no minimo"
	case picoDBFS <= -30:
		return "chegou som fraco: aumente o ganho do microfone ou fale mais perto"
	case picoDBFS >= -1:
		return "sinal saturado (clipping): reduza o ganho do microfone"
	default:
		return "o nivel parece normal — se havia fala, a recusa veio do detector e nao do microfone"
	}
}
