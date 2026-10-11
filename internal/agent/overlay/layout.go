package overlay

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

import (
	"fmt"
	"math"
	"time"
)

// Este arquivo não tem build tag de propósito: é a lógica de LAYOUT do
// overlay — posição na tela, tamanho da pílula, quantas barras a onda tem
// e a altura de cada uma a partir das amplitudes — pura aritmética sobre
// int/float64, sem nenhuma chamada de sistema. A composição da mensagem
// (quebra de linha, largura pelo texto) mora em mensagem.go.
// É por isso que ela é testável de verdade no runner Linux do CI (ver
// layout_test.go), ao contrário do desenho e da janela em si, que
// dependem de GDI/Win32 real (render.go continua sem build tag também,
// mas window_windows.go/window_other.go isolam a fronteira de
// plataforma — ver doc.go).

// # A janela acompanha a pílula
//
// Até 29-set-2026 a janela tinha tamanho fixo — 300x56, o da pílula de
// mensagem — e a pílula compacta de gravação era desenhada centrada dentro
// dela, com o resto transparente (ADR-010, D2: não redimensionar a cada
// troca de fase). Dava certo porque havia só dois tamanhos, e o maior era
// fixo.
//
// Com a mensagem do tamanho do texto (mensagem.go), cada desfecho tem o
// seu, e a janela passou a ter o tamanho da pílula da vez. O
// UpdateLayeredWindow que apresenta cada quadro recebe também a posição e
// o tamanho novos, e o Windows troca os três de uma vez — sem SetWindowPos
// de mover ou redimensionar à parte (o que há só reafirma o topo ao
// mostrar, sem mexer em lugar nem em tamanho), sem piscar, sem sobrar pedaço
// da pílula anterior (ver window_windows.go).
//
// O motivo de antes para não redimensionar — reabrir o DIB a cada troca —
// não se aplica: a superfície de desenho continua alocada uma vez só, no
// tamanho da maior pílula possível (TamanhoDaSuperficie), e o quadro de
// cada fase ocupa o canto dela.
const (
	// CompactWidth/CompactHeight é a pílula de gravação e processamento:
	// só a onda, sem texto.
	//
	// Encolheu de 200x32 para 128x20 em 29-set-2026, a pedido de Mauro:
	// "ainda dá pra tirar uns 30 a 40% do tamanho dela". São 36% a menos
	// na largura e 37,5% na altura — 40% da área de antes. Tamanho e cores
	// foram aprovados por ele depois ("tamanho e cores perfeitos"): a
	// mudança das mensagens, no mesmo dia, não toca aqui.
	//
	// A altura é par de propósito: a onda é espelhada, toda barra tem
	// altura par, e só com altura par o eixo cai no meio exato.
	//
	// CompactRadius é metade da altura: uma cápsula inteira, o raio
	// "pill" do design system, em vez do cantinho de 8 px de antes.
	CompactWidth  = 128
	CompactHeight = 20
	CompactRadius = CompactHeight / 2

	// BordaPilula é o contorno de 1 px em borderDark que o design system
	// põe em toda superfície que flutua sobre outra (ver corCorpo, em
	// paleta.go).
	BordaPilula = 1

	// CentroAcimaDaBase é a que altura da borda inferior da tela fica o
	// CENTRO da pílula, qualquer que seja o tamanho dela.
	//
	// 76 é onde ele sempre esteve: a janela fixa ficava a 48 px da borda e
	// tinha 56 de altura, e as pílulas eram centradas nela. Ancorar o
	// centro, e não a base, mantém a pílula compacta aprovada exatamente
	// onde estava, pixel por pixel, e faz a mensagem crescer para os dois
	// lados a partir dela — a pílula "abre" no lugar em vez de pular.
	CentroAcimaDaBase = 76
)

// Dimensoes é a geometria da pílula efetivamente desenhada numa fase.
//
// Existe para que o tamanho seja UMA decisão, tomada num lugar só, em vez
// de constantes espalhadas que o render consulta diretamente.
type Dimensoes struct {
	W, H   int
	Radius int
	// ComOnda: a fase desenha a onda em vez de texto. Gravação e
	// processamento; nenhuma outra.
	ComOnda bool
}

// DimensoesPara devolve a geometria da pílula para phase.
//
// Gravação e processamento compartilham a pílula compacta de propósito:
// são o ditado em curso, e mudar de tamanho no meio dele chamaria atenção
// para a transição justamente quando a pessoa está olhando para a onda. A
// pílula só muda no desfecho, para caber a mensagem — que é quando há o
// que ler —, e aí com o tamanho do texto dela (ComporMensagem).
func DimensoesPara(phase dictation.OverlayPhase) Dimensoes {
	switch phase {
	case dictation.OverlayRecording, dictation.OverlayProcessing:
		return Dimensoes{W: CompactWidth, H: CompactHeight, Radius: CompactRadius, ComOnda: true}
	default:
		return ComporMensagem(phase).Dim
	}
}

// TamanhoDaSuperficie é o tamanho da superfície de desenho da janela: o
// bastante para a maior pílula que qualquer fase pode pedir. É alocada uma
// vez, na criação da janela (window_windows.go), e cada quadro usa o canto
// dela.
func TamanhoDaSuperficie() (w, h int) {
	return max(CompactWidth, MensagemLarguraMax), max(CompactHeight, alturaMensagem(MaxLinhas))
}

// Geometria da onda de amplitude, em pixels dentro da pílula.
//
// A onda é ESPELHADA em torno de um eixo horizontal (WaveAxisY): cada
// barra se estende o mesmo tanto para cima e para baixo. É a forma que
// se reconhece como "estou ouvindo sua voz" — gravador de voz, Wispr
// Flow, WhatsApp — em vez de um equalizador crescendo do chão. E tem um
// efeito colateral útil: 1 px de variação de amplitude move as duas
// pontas, então a barra cresce 2 px. O mesmo sinal rende o dobro de
// movimento visível.
// BarCount/BarWidth/BarGap foram escolhidos OLHANDO, não calculando:
// seis combinações desenhadas sobre o mesmo instante de áudio real, em
// PNG, e comparadas lado a lado (ADR-014). Com 20 barras sobre a janela
// de onda, cada uma resume ~65 ms; com 44, ~29 ms — abaixo da duração de
// uma sílaba, que é o que faz aparecer o contorno da frase em vez de uma
// cerca de barras iguais.
//
// BarCount continua 33: é a RESOLUÇÃO TEMPORAL da onda, não o tamanho
// dela. Com 33 barras sobre a janela de 1,3 s cada uma resume ~39 ms, que
// é o que faz aparecer o contorno da frase. Encolher a pílula não muda a
// duração que cada barra representa, então mexer na contagem aqui
// desfaria a escolha do ADR-014 por um motivo que não é o dela.
//
// BarWidth/BarGap caíram de 5/3 para 3/2 junto com a pílula de 28-set, e
// de 3/2 para 2/1 junto com a de 29-set (128 px): 33 barras ocupam 98 px,
// e a onda fica inteira no trecho reto da cápsula, longe das pontas
// redondas.
//
// BarCount continuou 33 também em 29-set, pelo mesmo motivo de sempre: é
// a resolução temporal. Cair para ~25 barras de 3 px caberia na mesma
// largura, mas cada barra passaria a resumir 52 ms em vez de 39 — a
// direção da cerca que o ADR-014 corrigiu.
const (
	BarCount = 33
	BarWidth = 2
	BarGap   = 1

	// BarRadius é zero porque a barra tem 2 px: nessa largura não há
	// ponta redonda que se veja. A cápsula continua sendo a pílula.
	BarRadius = 0

	// waveMargem é a folga entre a onda e a borda da pílula compacta.
	//
	// Antes a onda começava em waveTop=24 porque o rótulo "Gravando"
	// ocupava o topo. Sem rótulo, ela é centrada e usa a altura inteira.
	// Com a pílula de 20 px (29-set), 3 px de folga deixam 2 px de corpo
	// entre a barra mais alta e o contorno, e a onda com 7 alturas
	// distintas em vez de 11.
	waveMargem = 3

	// WaveAxisY é o eixo de espelhamento e BarHalfMax quanto a barra mais
	// alta se estende para CADA lado dele. Ambos em coordenadas da PÍLULA
	// compacta; o render desloca para as da janela (ver drawBars).
	WaveAxisY  = CompactHeight / 2
	BarHalfMax = WaveAxisY - waveMargem
	BarHalfMin = 1
)

// Faixa de nível que a onda espalha sobre o curso das barras, em dBFS.
//
// # Por que dB, e não RMS linear
//
// Até 26-set-2026 barHeight era linear: min + amplitude*(max-min), com
// amplitude = audio.Frame.RMS em [0,1]. Parece razoável e está errado
// por dois motivos independentes.
//
// O primeiro é físico: RMS de fundo de escala 1,0 é INALCANÇÁVEL. Uma
// senoide em fundo de escala tem RMS 0,707, e fala tem fator de crista
// de 3 a 5 — fala com RMS 1,0 estaria clipada muito antes. O mapeamento
// linear reservava a maior parte do curso para amplitude que não pode
// acontecer.
//
// O segundo é perceptual: sonoridade é logarítmica. É para isso que o
// decibel existe.
//
// O resultado, MEDIDO nas 46 amostras de testdata/asr-dataset/audio,
// gravadas no microfone e no ganho reais desta máquina:
//
//	RMS dos quadros vozeados: p50 = 0,068 (-23,4 dBFS), pico = 0,224 (-13,0 dBFS)
//	altura de barra que isso produzia: 86% dos quadros em 6 ou 7 px, de 6..26
//	alturas distintas usadas: 5 de 21
//	espalhamento da onda num frame desenhado: 2 px de 20 disponíveis (p50)
//
// Dois pixels. Mauro reportou que a onda se mexia com a voz mas que "o
// movimento podia ser mais intenso" — não era preferência de gosto, era
// a onda usando 10% do curso que tinha.
//
// Com a mesma redução e mapeamento em dB nesta faixa, sobre os mesmos
// arquivos: espalhamento de 18 px de 20 (p50), 11 alturas distintas,
// distribuídas em torno de 21 px.
//
// O piso em -48 dBFS fica bem abaixo do fundo entre palavras (p50
// medido em -67 dBFS, p99 em -31), então silêncio continua lendo como
// repouso. O teto em -14 fica logo acima do pico medido (-13,0), para a
// barra encostar no máximo em sílaba forte sem saturar o tempo todo.
//
// Os dois são constante, e não configuração, de propósito: são
// propriedade de voz captada perto do microfone, não preferência de
// quem usa. Se em outra máquina a onda saturar ou ficar apagada, o
// número a mexer é este — e a medição que o justifica está acima.
const (
	WaveFloorDBFS = -48.0
	WaveCeilDBFS  = -14.0
)

// Position calcula o canto superior-esquerdo (x, y) de uma janela
// pillWidth x pillHeight centralizada na horizontal e com o CENTRO
// centroAcimaDaBase pixels acima da borda inferior de uma tela
// screenWidth x screenHeight (docs/ai-spec/08-ui-ux-spec.md: "Overlay.
// Default: centro inferior") — ver CentroAcimaDaBase para o porquê de
// ancorar o centro, e não a base. Recorta para não devolver coordenada
// negativa numa tela menor que a pílula — situação degenerada, mas que
// não deveria travar o cálculo.
func Position(screenWidth, screenHeight, pillWidth, pillHeight, centroAcimaDaBase int32) (x, y int32) {
	x = (screenWidth - pillWidth) / 2
	y = screenHeight - centroAcimaDaBase - pillHeight/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// Bars reduz amplitudes — mais recente por último, cada valor já
// normalizado em [0,1] (o mesmo formato de audio.Frame.RMS) — a
// exatamente barCount alturas de barra em pixels, cada uma recortada em
// [minHeight, maxHeight].
//
// Menos amostras que barCount preenche a sobra à ESQUERDA com
// minHeight (silêncio antes da fala começar a chegar); mais amostras
// que barCount agrupa em baldes e usa o PICO de cada balde — não a
// média — para que uma sílaba curta e alta não seja borrada pela
// suavização.
//
// A conversão de amplitude para pixel é logarítmica — ver barHeight e a
// medição registrada em WaveFloorDBFS.
func Bars(amplitudes []float64, barCount, minHeight, maxHeight int) []int {
	heights := make([]int, barCount)
	if barCount <= 0 {
		return heights[:0]
	}
	if len(amplitudes) == 0 {
		for i := range heights {
			heights[i] = minHeight
		}
		return heights
	}
	if len(amplitudes) <= barCount {
		pad := barCount - len(amplitudes)
		for i := 0; i < pad; i++ {
			heights[i] = minHeight
		}
		for i, a := range amplitudes {
			heights[pad+i] = barHeight(a, minHeight, maxHeight)
		}
		return heights
	}

	bucket := float64(len(amplitudes)) / float64(barCount)
	for i := 0; i < barCount; i++ {
		from := int(float64(i) * bucket)
		to := int(float64(i+1) * bucket)
		if to <= from {
			to = from + 1
		}
		if to > len(amplitudes) {
			to = len(amplitudes)
		}
		peak := 0.0
		for _, a := range amplitudes[from:to] {
			if a > peak {
				peak = a
			}
		}
		heights[i] = barHeight(peak, minHeight, maxHeight)
	}
	return heights
}

// barHeight converte uma amplitude RMS em [0,1] na altura em pixels
// dentro de [min, max], espalhando a faixa [WaveFloorDBFS,
// WaveCeilDBFS] sobre o curso disponível. Amplitude zero ou negativa é
// repouso; acima do teto satura em max.
func barHeight(amplitude float64, min, max int) int {
	if max < min {
		min, max = max, min
	}
	if amplitude <= 0 {
		return min
	}
	if amplitude > 1 {
		amplitude = 1
	}
	fracao := (20*math.Log10(amplitude) - WaveFloorDBFS) / (WaveCeilDBFS - WaveFloorDBFS)
	if fracao < 0 {
		fracao = 0
	}
	if fracao > 1 {
		fracao = 1
	}
	return min + int(math.Round(fracao*float64(max-min)))
}

// FormatDuration renderiza d como o "m:ss" compacto mostrado ao lado da
// onda durante a gravação (AC-005). Durações de uma hora ou mais —
// inalcançável na prática, já que DefaultMaxRecording é 120s — caem
// para "h:mm:ss" em vez de estourar o campo de minutos em silêncio.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second) / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
