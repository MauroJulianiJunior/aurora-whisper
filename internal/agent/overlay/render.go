package overlay

import (
	"math"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// Este arquivo não tem build tag: monta o frame completo (pílula +
// onda + texto) em cima de canvas.go e tipografia.go, puro Go — testável sem
// Windows (ver render_test.go). Só window_windows.go, que copia o
// resultado para o DIB real e chama UpdateLayeredWindow, depende do
// Win32 de verdade.

// A paleta mora em paleta.go: é a paleta do projeto, desde
// 29-set-2026. Até ali era a provisória do ADR-010, D2 — cinza sobre
// quase-preto —, que nunca tinha sido trocada.

// BrilhoRepouso/BrilhoPico são os dois cinzas da onda de ANTES de
// 29-set-2026 (110 e 245). A onda do overlay não os usa mais: agora ela
// vai de corCinza a corVerde (ver corDaBarra).
//
// Ficam só porque o ícone da bandeja (internal/agent/tray) ainda desenha
// a onda em miniatura com eles, e a bandeja é de outra frente de trabalho.
// Apagar daqui quebraria a compilação dela; quando ela migrar para o
// design system, estes dois saem.
const (
	BrilhoRepouso byte = 110
	BrilhoPico    byte = 245
)

// CorPilula devolve a cor do corpo da pílula, em B, G, R. Função, e não
// variável exportada, para ninguém de fora poder reatribuí-la. A bandeja
// usa para o fundo do ícone, pelo mesmo motivo de BrilhoRepouso acima.
func CorPilula() (b, g, r byte) { return corCorpo.bgr() }

// FrameState é tudo que Render precisa para desenhar um frame: a fase
// atual do overlay, a onda de amplitude capturada até agora (só usada
// enquanto Phase == dictation.OverlayRecording) e há quanto tempo a
// gravação atual está rodando.
type FrameState struct {
	Phase dictation.OverlayPhase
	Amps  []float64
}

// Quadro devolve o frame de state num canvas do tamanho exato da pílula
// da fase (DimensoesPara) — é o que a janela apresenta, e o tamanho que
// ela assume (ver Overlay.redraw).
func Quadro(state FrameState) *Canvas {
	d := DimensoesPara(state.Phase)
	c := NewCanvas(d.W, d.H)
	Render(c, state)
	return c
}

// Render desenha state em c, com a pílula CENTRADA em c. Sempre limpa c
// primeiro; quem chama decide se vale a pena chamar Render ou
// simplesmente deixar a janela escondida (ver Visible, em phase.go —
// dictation.OverlayHidden não precisa de frame nenhum).
//
// Em produção c tem o tamanho da pílula (Quadro) e a centragem não
// desloca nada. Ela existe para os testes: desenhando num canvas maior,
// qualquer coisa que saísse da pílula apareceria na folga em volta, em
// vez de ser recortada em silêncio pela borda do canvas.
func Render(c *Canvas, state FrameState) {
	c.Clear()

	if d := DimensoesPara(state.Phase); d.ComOnda {
		px, py := (c.W-d.W)/2, (c.H-d.H)/2
		desenhaPilula(c, px, py, d)
		// Gravação e processamento não têm texto nenhum. O rótulo
		// "Gravando" saiu a pedido de Mauro — "por mim o próprio wave já
		// mostra que está gravando" — e com ele saiu também o contador,
		// que era o que denunciava a fase de processamento inexistente.
		//
		// Sem texto, a onda fica com a altura inteira da pílula.
		drawBars(c, state.Amps, px, py, state.Phase)
		return
	}

	m := ComporMensagem(state.Phase)
	px, py := (c.W-m.Dim.W)/2, (c.H-m.Dim.H)/2
	desenhaPilula(c, px, py, m.Dim)
	desenhaMensagem(c, px, py, m)
}

// desenhaPilula desenha o corpo da pílula com o contorno de BordaPilula
// px do design system: a forma inteira na cor da borda, e por cima a
// mesma forma, recuada, na cor do corpo.
//
// Só a forma de fora tem a borda suavizada: é ela que encontra o papel de
// parede, onde a escada das pontas aparecia. O corpo fica binário — entre
// ele e o contorno o contraste é 1,49:1 e o degrau não se vê —, e é isso
// que deixa o contorno com 1 px exato em toda a volta
// (TestRender_ContornoTemAMesmaEspessuraNasCurvas). Todo pixel do corpo
// cai onde a forma de fora já cobre o pixel inteiro, então o corpo nunca
// pinta por cima de uma borda parcial.
func desenhaPilula(c *Canvas, px, py int, d Dimensoes) {
	bb, bg, br := corBorda.bgr()
	c.FillRoundedRectSuave(px, py, d.W, d.H, d.Radius, bb, bg, br)
	sb, sg, sr := corCorpo.bgr()
	c.FillRoundedRect(px+BordaPilula, py+BordaPilula,
		d.W-2*BordaPilula, d.H-2*BordaPilula, d.Radius-BordaPilula, sb, sg, sr)
}

// desenhaMensagem desenha, na pílula com canto em px/py, o ponto de estado
// (sinalPara) e, ao lado dele, as linhas da mensagem alinhadas à esquerda
// — o componente "status" e o "alert" do design system, que põem o sinal
// antes do texto. Onde vai cada coisa foi decidido em ComporMensagem.
//
// O ponto fica na altura da PRIMEIRA linha, centrado nela — como no
// .alert do DS, cujo sinal abre o texto em cima. Uma versão anterior o
// centrava no bloco, e nas mensagens de duas linhas ele caía no vão entre
// elas, sem acompanhar nenhuma.
// TestRender_MensagemEhCentradaEPontoAbreAPrimeiraLinha trava as posições.
func desenhaMensagem(c *Canvas, px, py int, m Mensagem) {
	if m.TemSinal {
		b, g, r := m.Sinal.bgr()
		// Suavizado: em 8 px, o círculo binário é um octógono.
		c.FillRoundedRectSuave(px+m.SinalX, py+m.SinalY, SinalDiametro, SinalDiametro, SinalDiametro/2, b, g, r)
	}
	t := tipo()
	for i, linha := range m.Linhas {
		t.desenha(c, px+m.TextoX, py+m.Bases[i], linha, corTexto)
	}
}

// drawBars desenha a onda espelhada em torno de WaveAxisY: Bars devolve
// a MEIA altura de cada barra, e cada uma se estende esse tanto para
// cima e para baixo do eixo (ver a geometria em layout.go).
// px/py é o canto da pílula dentro do canvas: a geometria da onda
// (WaveAxisY, BarHalfMax) é em coordenadas da PÍLULA, e é aqui que ela
// vira coordenada de janela.
//
// # Processamento desenha onda de repouso, não a última onda
//
// A spec pede "onda congelada". Congelar de verdade exigiria fotografar
// as amplitudes na troca de fase — o que chega aqui é uma janela
// deslizante de 1,3 s (ver JanelaDaOnda), que ESCOAria durante os ~665 ms
// do ASR: a onda encolheria sozinha, parecendo que o sinal está morrendo,
// que é pior que congelada.
//
// A onda de repouso resolve por outro caminho: barras constantes no
// mínimo, ignorando amps. Distingue-se da gravação exatamente onde
// importa — ela não se MEXE — e não depende de estado que já está
// escoando.
func drawBars(c *Canvas, amps []float64, px, py int, phase dictation.OverlayPhase) {
	metades := Bars(amps, BarCount, BarHalfMin, BarHalfMax)
	if phase == dictation.OverlayProcessing {
		metades = make([]int, BarCount)
		for i := range metades {
			metades[i] = BarHalfMin
		}
	}
	totalWidth := BarCount*BarWidth + (BarCount-1)*BarGap
	x := px + (WaveLarguraDisponivel()-totalWidth)/2
	eixo := py + WaveAxisY
	for _, metade := range metades {
		b, g, r := corDaBarra(metade).bgr()
		c.FillRoundedRect(x, eixo-metade, BarWidth, 2*metade, BarRadius, b, g, r)
		x += BarWidth + BarGap
	}
}

// WaveLarguraDisponivel é a largura da pílula em que a onda é desenhada.
func WaveLarguraDisponivel() int { return CompactWidth }

// corDaBarra interpola a cor da barra entre corCinza (repouso) e corVerde
// (pico) conforme a fração do curso que ela ocupa.
//
// É a codificação dupla de intensidade do ADR-014 — altura e cor dizem a
// mesma coisa, de propósito, para a onda parecer viva —, agora com o hue
// do design system em vez de dois cinzas. A barra em silêncio é neutra, e
// fica tanto mais verde quanto mais sinal medido ela carrega (ver
// corCinza, em paleta.go, para o porquê de o repouso não ser verde).
func corDaBarra(metade int) Cor {
	if BarHalfMax <= BarHalfMin {
		return corVerde
	}
	if metade < BarHalfMin {
		metade = BarHalfMin
	}
	if metade > BarHalfMax {
		metade = BarHalfMax
	}
	f := float64(metade-BarHalfMin) / float64(BarHalfMax-BarHalfMin)
	mistura := func(de, ate byte) byte {
		return byte(math.Round(float64(de) + f*(float64(ate)-float64(de))))
	}
	return Cor{
		R: mistura(corCinza.R, corVerde.R),
		G: mistura(corCinza.G, corVerde.G),
		B: mistura(corCinza.B, corVerde.B),
	}
}
