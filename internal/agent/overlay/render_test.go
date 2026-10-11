package overlay

import (
	"bytes"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

func opaquePixelCount(c *Canvas) int {
	n := 0
	for i := 3; i < len(c.Pix); i += 4 {
		if c.Pix[i] != 0 {
			n++
		}
	}
	return n
}

// coresDeBarra é o conjunto EXATO de cores que uma barra pode ter: uma por
// meia-altura possível (ver corDaBarra).
func coresDeBarra() map[Cor]bool {
	cores := map[Cor]bool{}
	for m := BarHalfMin; m <= BarHalfMax; m++ {
		cores[corDaBarra(m)] = true
	}
	return cores
}

// ehPixelDeBarra distingue pixel de BARRA dos outros desenhos da pílula:
// corpo, contorno, texto e ponto de estado.
//
// Distinguir por alpha não funciona (é 255 em todos) e distinguir por
// "mais claro que a pílula" também não: o texto entra. A primeira versão
// deste helper usava um limiar aberto e os testes de onda falharam
// pegando o rótulo — foi o helper que estava errado, não o desenho.
//
// Até 29-set-2026 a barra era um cinza puro entre 110 e 245 e o helper
// testava essa faixa. Com a paleta do design system a barra vai do cinza
// ao verde (corDaBarra), e o helper compara com as cores exatas que ela
// pode ter. Só vale enquanto nenhuma delas coincidir com a de outro
// desenho, o que TestRender_DiscriminadorDeBarraEhValido trava.
func ehPixelDeBarra(c *Canvas, x, y int) bool {
	b, g, r, a := c.At(x, y)
	return a == 255 && coresDeBarra()[Cor{R: r, G: g, B: b}]
}

// linhasComBarra devolve quais linhas y do intervalo [de, ate) têm ao
// menos um pixel de barra.
func linhasComBarra(c *Canvas, de, ate int) []int {
	var linhas []int
	for y := de; y < ate; y++ {
		for x := 0; x < c.W; x++ {
			if ehPixelDeBarra(c, x, y) {
				linhas = append(linhas, y)
				break
			}
		}
	}
	return linhas
}

// TestRender_DiscriminadorDeBarraEhValido trava o que ehPixelDeBarra
// supõe: nenhuma cor de barra coincide com o corpo, o contorno, o texto
// ou o vermelho. Se alguém mexer na paleta e encostar uma na outra,
// isto falha aqui em vez de fazer os testes de onda mentirem.
//
// O verde e o cinza dos PONTOS de estado são, de propósito, as pontas da
// onda (sinalPara): por isso os testes de "fase sem onda não tem barra"
// usam uma fase de ponto vermelho.
func TestRender_DiscriminadorDeBarraEhValido(t *testing.T) {
	barras := coresDeBarra()
	if len(barras) < 2 {
		t.Fatalf("a onda tem %d cor(es) — a cor não acompanha mais a altura", len(barras))
	}
	for nome, cor := range map[string]Cor{
		"corpo":    corCorpo,
		"contorno": corBorda,
		"texto":    corTexto,
		"vermelho": corVermelho,
	} {
		if barras[cor] {
			t.Errorf("a cor de %s (%v) é também cor de barra — os testes de onda "+
				"passariam a contá-la como barra", nome, cor)
		}
	}
	// E o contrário: nenhuma cor de barra passa por tinta de texto
	// (ehTinta), senão TestRender_GravacaoNaoTemTextoNenhum acusaria a onda.
	for cor := range barras {
		if ehCorDeTinta(cor) {
			t.Errorf("a cor de barra %v se confunde com tinta de texto", cor)
		}
	}
}

func ampsCheias(v float64) []float64 {
	amps := make([]float64, BarCount)
	for i := range amps {
		amps[i] = v
	}
	return amps
}

func TestRender_HiddenNaoPrecisaDeFrameMasNaoTrava(t *testing.T) {
	c := canvasDeTeste()
	Render(c, FrameState{Phase: dictation.OverlayHidden})
	// Render sempre desenha a pílula, independente da fase — é quem
	// chama (Overlay.redraw, via Visible) que decide não chamar Render
	// nem mostrar a janela quando a fase é Hidden.
	if opaquePixelCount(c) == 0 {
		t.Fatal("Render não desenhou nada")
	}
}

// TestRender_AlphaNaoDistingueBarraDePilula existe para o teste de
// barras acima não voltar a ser vacuoso.
//
// Até 26-set-2026 TestRender_RecordingDesenhaBarras procurava um pixel
// com `a == 255` na faixa da onda e declarava sucesso. Só que a pílula
// preenche essa faixa inteira, opaca — o teste passava com drawBars não
// desenhando barra nenhuma. Este teste PROVA isso: renderiza uma fase
// SEM onda e mostra que a faixa está cheia de pixel opaco de qualquer
// jeito.
//
// A fase é Error, e não mais Success, desde 29-set-2026: o ponto verde de
// "Inserido" tem a cor da barra no pico (ver sinalPara), então ali o
// critério por cor acharia "barra" legitimamente. O ponto de Error é
// vermelho, que nenhuma barra usa.
func TestRender_AlphaNaoDistingueBarraDePilula(t *testing.T) {
	c := canvasDeTeste()
	Render(c, FrameState{Phase: dictation.OverlayError}) // sem onda

	opacosNaFaixa := 0
	for y := ondaTopoNoCanvas(); y < ondaBaseNoCanvas(); y++ {
		for x := 0; x < c.W; x++ {
			if _, _, _, a := c.At(x, y); a == 255 {
				opacosNaFaixa++
			}
		}
	}
	if opacosNaFaixa == 0 {
		t.Fatal("a pílula não é opaca na faixa da onda — o critério por cor " +
			"deste arquivo pode ter deixado de ser necessário")
	}

	// E o critério por COR não se engana: sem onda, nenhuma barra.
	if linhas := linhasComBarra(c, ondaTopoNoCanvas(), ondaBaseNoCanvas()); len(linhas) != 0 {
		t.Errorf("fase sem onda tem %d linhas com pixel de barra: %v", len(linhas), linhas)
	}
}

// TestRender_OndaEhEspelhadaEmTornoDoEixo trava a forma decidida em
// 26-set-2026: a onda se estende igual para cima e para baixo de
// WaveAxisY, em vez de crescer de uma base.
func TestRender_OndaEhEspelhadaEmTornoDoEixo(t *testing.T) {
	c := canvasDeTeste()
	Render(c, FrameState{
		Phase: dictation.OverlayRecording,
		Amps:  ampsCheias(1.0),
	})

	linhas := linhasComBarra(c, 0, c.H)
	if len(linhas) == 0 {
		t.Fatal("Render(recording) não desenhou barra nenhuma")
	}

	var acima, abaixo int
	for _, y := range linhas {
		if y < eixoNoCanvas() {
			acima++
		} else {
			abaixo++
		}
	}
	if acima != abaixo {
		t.Errorf("onda não está espelhada: %d linhas acima do eixo (y<%d) e %d abaixo",
			acima, eixoNoCanvas(), abaixo)
	}
	if acima == 0 {
		t.Error("nenhuma linha acima do eixo — a onda voltou a crescer de uma base")
	}
}

// TestRender_OndaNoMaximoCabeNaPilula: com amplitude saturada a onda não
// pode invadir o texto acima nem furar a borda de baixo.
func TestRender_OndaNoMaximoCabeNaPilula(t *testing.T) {
	c := canvasDeTeste()
	Render(c, FrameState{
		Phase: dictation.OverlayRecording,
		Amps:  ampsCheias(1.0),
	})

	linhas := linhasComBarra(c, 0, c.H)
	if len(linhas) == 0 {
		t.Fatal("Render(recording) não desenhou barra nenhuma")
	}
	topo, base := linhas[0], linhas[len(linhas)-1]

	// A checagem de sobreposição com o texto saiu em 28-set-2026: não há
	// mais texto na fase de gravação. O que resta — e é o que importa
	// agora — é a onda não furar a PÍLULA COMPACTA, que é bem menor que
	// a janela: se vazasse, desenharia barra sobre o nada transparente,
	// fora da pílula, e pareceria sujeira flutuando na tela.
	if topo < pilulaTopoNoCanvas() {
		t.Errorf("a onda começa em y=%d, acima do topo da pílula (y=%d)", topo, pilulaTopoNoCanvas())
	}
	if base >= pilulaTopoNoCanvas()+CompactHeight {
		t.Errorf("a onda termina em y=%d, fora da pílula compacta (termina em y=%d)",
			base, pilulaTopoNoCanvas()+CompactHeight)
	}
	if topo < ondaTopoNoCanvas() || base >= ondaBaseNoCanvas() {
		t.Errorf("a onda ocupa y=%d..%d, fora da faixa reservada y=%d..%d",
			topo, base, ondaTopoNoCanvas(), ondaBaseNoCanvas())
	}
}

// TestRender_OndaFicaNoTrechoRetoDaCapsula: com a pílula compacta de
// 128 px (29-set-2026), a largura da onda deixou de ter folga de sobra.
// Barra nenhuma pode invadir as pontas redondas da cápsula — ali ela
// seria cortada pela curva ou desenhada sobre o nada — nem o contorno.
func TestRender_OndaFicaNoTrechoRetoDaCapsula(t *testing.T) {
	c := canvasDeTeste()
	Render(c, FrameState{Phase: dictation.OverlayRecording, Amps: ampsCheias(1.0)})

	px, _ := cantoNoCanvasDeTeste(DimensoesPara(dictation.OverlayRecording))
	retoDe, retoAte := px+CompactRadius, px+CompactWidth-CompactRadius
	topo, base := pilulaTopoNoCanvas()+BordaPilula, pilulaTopoNoCanvas()+CompactHeight-BordaPilula

	barras := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if !ehPixelDeBarra(c, x, y) {
				continue
			}
			barras++
			if x < retoDe || x >= retoAte {
				t.Fatalf("barra em x=%d, fora do trecho reto da cápsula (x=%d..%d)", x, retoDe, retoAte-1)
			}
			if y < topo || y >= base {
				t.Fatalf("barra em y=%d, sobre o contorno ou fora da pílula (miolo y=%d..%d)", y, topo, base-1)
			}
		}
	}
	if barras == 0 {
		t.Fatal("Render(recording) não desenhou barra nenhuma — o teste não prova nada")
	}
}

// TestRender_BarraEmRepousoEhCinzaEBarraNoPicoEhVerde trava a codificação
// dupla de intensidade (altura + cor) — ver corDaBarra em render.go.
//
// Substituiu, em 29-set-2026, o teste que travava os dois cinzas de antes
// (110 e 245): a paleta passou a ser a do design system, por decisão de
// Mauro. O que continua travado é o PRINCÍPIO — a barra alta se destaca
// da parada —, agora com o hue do DS: repouso no steelGray, pico no
// systemGreen.
func TestRender_BarraEmRepousoEhCinzaEBarraNoPicoEhVerde(t *testing.T) {
	if got := corDaBarra(BarHalfMin); got != corCinza {
		t.Errorf("corDaBarra(min) = %v, want corCinza %v", got, corCinza)
	}
	if got := corDaBarra(BarHalfMax); got != corVerde {
		t.Errorf("corDaBarra(max) = %v, want corVerde %v", got, corVerde)
	}
	// O meio fica estritamente entre as pontas, canal a canal: o verde
	// sobe e o vermelho desce à medida que a barra cresce.
	meio := corDaBarra((BarHalfMin + BarHalfMax) / 2)
	if !(meio.G > corCinza.G && meio.G < corVerde.G && meio.R < corCinza.R && meio.R > corVerde.R) {
		t.Errorf("corDaBarra(meio) = %v, deveria ficar entre %v e %v", meio, corCinza, corVerde)
	}
	// Fora da faixa não estoura o byte nem inverte.
	if got := corDaBarra(-100); got != corCinza {
		t.Errorf("corDaBarra(-100) = %v, want %v (recortado)", got, corCinza)
	}
	if got := corDaBarra(BarHalfMax * 10); got != corVerde {
		t.Errorf("corDaBarra(muito alto) = %v, want %v (recortado)", got, corVerde)
	}
}

// TestRender_VerdeSoOndeHaSinalMedido trava, no frame DESENHADO, a regra
// que dá sentido ao verde (ver corCinza, em paleta.go): a barra só fica
// verde com sinal medido chegando. Silêncio e processamento são cinza.
//
// TestRender_BarraEmRepousoEhCinzaEBarraNoPicoEhVerde confere corDaBarra
// sozinha. Faltava a ligação dela com o
// desenho: um drawBars que pintasse toda barra com corDaBarra(BarHalfMax),
// em vez da cor da altura de cada uma, passava a suíte inteira com a onda
// verde cheia no silêncio — achado da revisão de 29-set-2026, que é quando
// o verde passou a querer dizer "sinal medido".
//
// O processamento, além de cinza, fica no REPOUSO: o microfone já fechou,
// e onda alta ali afirmaria um sinal que não está chegando. A mesma
// revisão mostrou que trocar o repouso dele por BarHalfMax também passava:
// TestRender_ProcessandoNaoDesenhaOnda só compara o frame com e sem amps,
// e os dois ficavam iguais — verdes e cheios.
func TestRender_VerdeSoOndeHaSinalMedido(t *testing.T) {
	desenha := func(p dictation.OverlayPhase, amps []float64) *Canvas {
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p, Amps: amps})
		return c
	}

	for _, caso := range []struct {
		nome  string
		phase dictation.OverlayPhase
		amps  []float64
	}{
		{"gravando em silêncio absoluto", dictation.OverlayRecording, ampsCheias(0)},
		// -67 dBFS: o fundo medido entre palavras (ADR-014).
		{"gravando no fundo entre palavras", dictation.OverlayRecording, ampsCheias(0.00042)},
		// As amplitudes mais altas possíveis ainda na memória: não podem
		// vazar para a onda do processamento.
		{"processando depois de sinal no teto", dictation.OverlayProcessing, ampsCheias(1.0)},
	} {
		c := desenha(caso.phase, caso.amps)

		if n := contaCor(c, corVerde); n != 0 {
			t.Errorf("%s: %d pixels verdes — verde sem sinal medido", caso.nome, n)
		}
		barras, foraDoCinza := 0, 0
		for y := 0; y < c.H; y++ {
			for x := 0; x < c.W; x++ {
				if !ehPixelDeBarra(c, x, y) {
					continue
				}
				barras++
				if b, g, r, _ := c.At(x, y); (Cor{R: r, G: g, B: b}) != corCinza {
					foraDoCinza++
				}
			}
		}
		if barras == 0 {
			t.Errorf("%s: nenhuma barra — a pílula fica vazia, e vazia parece defeito", caso.nome)
		}
		if foraDoCinza != 0 {
			t.Errorf("%s: %d de %d pixels de barra fora do cinza de repouso", caso.nome, foraDoCinza, barras)
		}
		// Repouso é a barra mínima: 2*BarHalfMin linhas em volta do eixo.
		linhas := linhasComBarra(c, 0, c.H)
		if len(linhas) != 2*BarHalfMin || linhas[0] != eixoNoCanvas()-BarHalfMin {
			t.Errorf("%s: barras nas linhas %v, want só as %d em volta do eixo (y=%d)",
				caso.nome, linhas, 2*BarHalfMin, eixoNoCanvas())
		}
	}

	// O outro lado da regra, que é também a guarda de vacuidade: com sinal
	// no teto a onda É verde. Sem isto, uma onda que nunca ficasse verde
	// passaria acima.
	if contaCor(desenha(dictation.OverlayRecording, ampsCheias(1.0)), corVerde) == 0 {
		t.Error("gravando com sinal no teto e nenhum pixel verde — a cor não acompanha mais o sinal")
	}
}

// TestRender_BarraMaisAltaGuardaCorpoAteOContorno trava a folga de
// waveMargem (layout.go): com o sinal no teto, sobram ao menos 2 px de
// corpo entre a ponta da barra e o contorno, em cima e embaixo. Barra
// encostada no contorno faz a onda parecer transbordar a cápsula.
//
// Os testes de onda não pegavam isso: TestRender_OndaFicaNoTrechoRetoDaCapsula
// só acusa barra SOBRE o contorno, e TestRender_OndaNoMaximoCabeNaPilula
// mede a onda contra uma faixa calculada com o próprio waveMargem. Com
// waveMargem = 1 os dois passavam (revisão de 29-set-2026).
func TestRender_BarraMaisAltaGuardaCorpoAteOContorno(t *testing.T) {
	const corpoMinimo = 2 // "3 px de folga deixam 2 px de corpo" (waveMargem)

	c := canvasDeTeste()
	Render(c, FrameState{Phase: dictation.OverlayRecording, Amps: ampsCheias(1.0)})
	linhas := linhasComBarra(c, 0, c.H)
	if len(linhas) == 0 {
		t.Fatal("Render(recording) não desenhou barra nenhuma")
	}
	topo, base := linhas[0], linhas[len(linhas)-1]
	contornoCima := pilulaTopoNoCanvas()
	contornoBaixo := pilulaTopoNoCanvas() + CompactHeight - BordaPilula

	if corpo := topo - (contornoCima + BordaPilula); corpo < corpoMinimo {
		t.Errorf("a barra mais alta começa em y=%d: %d px de corpo até o contorno de cima, want >= %d",
			topo, corpo, corpoMinimo)
	}
	if corpo := contornoBaixo - base - 1; corpo < corpoMinimo {
		t.Errorf("a barra mais alta termina em y=%d: %d px de corpo até o contorno de baixo, want >= %d",
			base, corpo, corpoMinimo)
	}

	// E o vão é corpo de verdade, na coluna de uma barra no teto.
	x := -1
	for i := 0; i < c.W && x < 0; i++ {
		if ehPixelDeBarra(c, i, topo) {
			x = i
		}
	}
	for y := contornoCima + BordaPilula; y < topo; y++ {
		if b, g, r, _ := c.At(x, y); (Cor{R: r, G: g, B: b}) != corCorpo {
			t.Errorf("(%d,%d), entre a barra e o contorno, = %v, want o corpo %v",
				x, y, Cor{R: r, G: g, B: b}, corCorpo)
		}
	}
}

func TestRender_NaoTravaParaCadaFase(t *testing.T) {
	// Lista canônica (phase_test.go): a versão enumerada à mão aqui não
	// cobria OverlayInsertFailed nem OverlayClipboard.
	for _, p := range fasesDoOverlay() {
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p})
	}
}

// TestRender_GravacaoNaoTemTextoNenhum substituiu, em 28-set-2026, o
// teste que travava o contador de gravação.
//
// O contador foi embora junto com o rótulo. Mauro: "tirar o gravando...
// por mim o próprio wave já mostra que está gravando", e a pílula só
// pôde cair para 25% da área porque a gravação deixou de precisar
// acomodar texto.
//
// O que se trava aqui é a AUSÊNCIA: um pixel de texto na pílula de
// gravação significa que o rótulo, o contador, ou qualquer outra coisa
// voltou — e com ele volta a largura que obrigava a pílula a ser grande.
//
// Texto é tinta off-white sobre o corpo (ehTinta), que nenhuma barra usa
// (TestRender_DiscriminadorDeBarraEhValido), então a cor identifica texto
// sem ambiguidade. O segundo bloco é a guarda de vacuidade: se o desenho
// do texto parasse de funcionar, o primeiro passaria calado.
func TestRender_GravacaoNaoTemTextoNenhum(t *testing.T) {
	pixelsDeTexto := func(p dictation.OverlayPhase) int {
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p, Amps: ampsCheias(1.0)})
		return contaTinta(c)
	}

	for _, p := range []dictation.OverlayPhase{dictation.OverlayRecording, dictation.OverlayProcessing} {
		if n := pixelsDeTexto(p); n != 0 {
			t.Errorf("fase %s desenhou %d pixels de texto — a pílula compacta não comporta texto", p, n)
		}
	}
	if n := pixelsDeTexto(dictation.OverlaySuccess); n == 0 {
		t.Fatal("nenhuma fase desenha texto — o teste acima não prova nada")
	}
}

// TestRender_ProcessandoNaoDesenhaOnda: durante o processamento não entra
// áudio nenhum. A onda que sobrou na memória é do que JÁ foi gravado, e
// continuar mostrando-a sugeriria captura em curso — a mesma mentira que
// a fase existe para desfazer, só que em outro canal.
func TestRender_ProcessandoNaoDesenhaOnda(t *testing.T) {
	amps := []float64{0.9, 0.8, 0.95, 0.7, 0.85}

	comAmps := canvasDeTeste()
	Render(comAmps, FrameState{Phase: dictation.OverlayProcessing, Amps: amps})
	semAmps := canvasDeTeste()
	Render(semAmps, FrameState{Phase: dictation.OverlayProcessing})

	if !bytes.Equal(comAmps.Pix, semAmps.Pix) {
		t.Error("processando desenhou a onda: sugere microfone aberto quando ele já fechou")
	}

	// Vacuidade: em recording as mesmas amplitudes TÊM de mudar o frame.
	gravComAmps := canvasDeTeste()
	Render(gravComAmps, FrameState{Phase: dictation.OverlayRecording, Amps: amps})
	gravSemAmps := canvasDeTeste()
	Render(gravSemAmps, FrameState{Phase: dictation.OverlayRecording})
	if bytes.Equal(gravComAmps.Pix, gravSemAmps.Pix) {
		t.Fatal("Render ignora Amps até em recording — o teste acima não prova nada")
	}
}

// --- o canvas dos testes --------------------------------------------------
//
// Os testes desenham num canvas MAIOR que qualquer pílula — a superfície
// da janela mais folgaDeTeste em cada lado —, com a pílula centrada nele
// (Render centra). Em produção o canvas tem o tamanho da pílula e qualquer
// coisa que saísse dela seria recortada em silêncio pela borda; aqui,
// aparece na folga e o teste vê.

const folgaDeTeste = 16

func tamanhoDoCanvasDeTeste() (w, h int) {
	sw, sh := TamanhoDaSuperficie()
	return sw + 2*folgaDeTeste, sh + 2*folgaDeTeste
}

func canvasDeTeste() *Canvas { return NewCanvas(tamanhoDoCanvasDeTeste()) }

// cantoNoCanvasDeTeste é onde Render põe o canto da pílula d no canvas dos
// testes.
func cantoNoCanvasDeTeste(d Dimensoes) (px, py int) {
	w, h := tamanhoDoCanvasDeTeste()
	return (w - d.W) / 2, (h - d.H) / 2
}

// --- geometria da onda em coordenadas do canvas ---------------------------
//
// WaveAxisY/waveMargem são coordenadas da PÍLULA compacta; estes helpers
// convertem para as do canvas dos testes, que é onde eles leem pixel.

func pilulaTopoNoCanvas() int {
	_, py := cantoNoCanvasDeTeste(DimensoesPara(dictation.OverlayRecording))
	return py
}
func eixoNoCanvas() int     { return pilulaTopoNoCanvas() + WaveAxisY }
func ondaTopoNoCanvas() int { return pilulaTopoNoCanvas() + waveMargem }
func ondaBaseNoCanvas() int { return pilulaTopoNoCanvas() + CompactHeight - waveMargem }
