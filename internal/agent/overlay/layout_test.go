package overlay

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestPosition_CentroInferior: centrada na horizontal, e com o CENTRO da
// pílula centroAcimaDaBase pixels acima da borda de baixo.
func TestPosition_CentroInferior(t *testing.T) {
	x, y := Position(1920, 1080, 400, 80, 76)
	wantX := int32((1920 - 400) / 2)
	wantY := int32(1080 - 76 - 80/2)
	if x != wantX || y != wantY {
		t.Fatalf("Position(1920,1080,400,80,76) = (%d,%d), want (%d,%d)", x, y, wantX, wantY)
	}
}

// TestPosition_PilulaCompactaNaoSaiDoLugar trava o "NÃO mexa nela": a
// pílula compacta aprovada por Mauro em 29-set-2026 fica exatamente onde
// estava quando era desenhada centrada na janela fixa de 300x56, a 48 px
// da borda — (896, 994) numa tela de 1920x1080, e o equivalente em
// qualquer outra.
func TestPosition_PilulaCompactaNaoSaiDoLugar(t *testing.T) {
	const janelaW, janelaH, margem = 300, 56, 48 // a janela fixa de antes
	for _, tela := range [][2]int32{{1920, 1080}, {1366, 768}, {2560, 1440}, {3840, 2160}} {
		antesX := (tela[0]-janelaW)/2 + (janelaW-CompactWidth)/2
		antesY := tela[1] - janelaH - margem + (janelaH-CompactHeight)/2
		x, y := Position(tela[0], tela[1], CompactWidth, CompactHeight, CentroAcimaDaBase)
		if x != antesX || y != antesY {
			t.Errorf("tela %dx%d: a pílula compacta foi para (%d,%d); estava em (%d,%d)",
				tela[0], tela[1], x, y, antesX, antesY)
		}
	}
}

func TestPosition_TelaMenorQuePilulaNaoFicaNegativa(t *testing.T) {
	x, y := Position(100, 50, 400, 80, 48)
	if x < 0 || y < 0 {
		t.Fatalf("Position numa tela menor que a pílula devolveu negativo: (%d,%d)", x, y)
	}
}

func TestBars_MenosAmostrasQueBarCountPreencheEsquerdaComMinimo(t *testing.T) {
	heights := Bars([]float64{1.0}, 5, 2, 20)
	if len(heights) != 5 {
		t.Fatalf("len(heights) = %d, want 5", len(heights))
	}
	for i := 0; i < 4; i++ {
		if heights[i] != 2 {
			t.Errorf("heights[%d] = %d, want minHeight=2 (silêncio de padding)", i, heights[i])
		}
	}
	if heights[4] != 20 {
		t.Errorf("heights[4] = %d, want maxHeight=20 (amplitude 1.0)", heights[4])
	}
}

func TestBars_SemAmostrasUsaMinimoEmTodasAsBarras(t *testing.T) {
	heights := Bars(nil, 4, 3, 15)
	for i, h := range heights {
		if h != 3 {
			t.Errorf("heights[%d] = %d, want 3 (sem amostra nenhuma)", i, h)
		}
	}
}

func TestBars_MaisAmostrasQueBarCountUsaPicoDoBalde(t *testing.T) {
	// 6 amostras em 3 barras -> baldes de 2. O segundo balde (índices
	// 2,3) tem um pico isolado de 1.0 no meio de ruído baixo — o pico
	// não pode ser diluído pela média.
	amps := []float64{0.1, 0.1, 0.1, 1.0, 0.1, 0.1}
	heights := Bars(amps, 3, 0, 100)
	if heights[1] != 100 {
		t.Fatalf("heights[1] = %d, want 100 (pico do balde, não a média)", heights[1])
	}
}

func TestBars_AmplitudeForaDeZeroUmEhRecortada(t *testing.T) {
	heights := Bars([]float64{-5, 5}, 2, 1, 10)
	if heights[0] != 1 {
		t.Errorf("amplitude negativa: heights[0] = %d, want minHeight=1", heights[0])
	}
	if heights[1] != 10 {
		t.Errorf("amplitude > 1: heights[1] = %d, want maxHeight=10", heights[1])
	}
}

func TestBars_BarCountZeroDevolveVazio(t *testing.T) {
	heights := Bars([]float64{1, 2, 3}, 0, 1, 10)
	if len(heights) != 0 {
		t.Fatalf("len(heights) = %d, want 0", len(heights))
	}
}

// TestBarHeight_FalaRealUsaOCursoDaBarra é o teste que faltava, e é o
// motivo de o mapeamento quebrado ter sobrevivido a 1.025 testes verdes.
//
// Os testes de Bars acima cobrem os EXTREMOS (amplitude 0 -> min, 1.0 ->
// max, fora da faixa recortada) e a escolha de pico por balde. Nenhum
// olhava o MEIO do mapeamento. Com o mapeamento linear antigo todos
// passavam — porque fala real nunca chega perto de 1,0, e o teste que
// usava 1,0 confirmava o único ponto em que linear e log concordam.
//
// Os RMS abaixo são medidos em testdata/asr-dataset/audio, no microfone
// e ganho reais desta máquina (ver WaveFloorDBFS). As frações mínimas
// são o que um mapeamento útil TEM de entregar: com o linear antigo,
// fala de p50 usava 5% do curso e as três linhas falhariam.
func TestBarHeight_FalaRealUsaOCursoDaBarra(t *testing.T) {
	const min, max = BarHalfMin, BarHalfMax
	curso := float64(max - min)

	casos := []struct {
		nome      string
		rms       float64
		minFracao float64
	}{
		{"fala baixa (p5 medido, -29 dBFS)", 0.0341, 0.40},
		{"fala normal (p50 medido, -23 dBFS)", 0.0676, 0.60},
		{"sílaba forte (pico medido, -13 dBFS)", 0.2240, 0.95},
	}
	for _, c := range casos {
		h := barHeight(c.rms, min, max)
		fracao := float64(h-min) / curso
		if fracao < c.minFracao {
			t.Errorf("%s: barHeight(%.4f) = %d px, usa %.0f%% do curso [%d,%d]; "+
				"queria pelo menos %.0f%% — onda que não se mexe não informa nada",
				c.nome, c.rms, h, 100*fracao, min, max, 100*c.minFracao)
		}
	}
}

// TestBarHeight_SilencioFicaEmRepouso: o outro lado da moeda. Se o
// fundo entre palavras subisse junto, a onda ficaria alta o tempo todo e
// perderia o contraste que a torna legível.
func TestBarHeight_SilencioFicaEmRepouso(t *testing.T) {
	// p50 medido dos quadros NÃO vozeados das 46 amostras: -67 dBFS.
	if h := barHeight(0.00042, BarHalfMin, BarHalfMax); h != BarHalfMin {
		t.Errorf("fundo entre palavras (-67 dBFS) = %d px, want %d (repouso)", h, BarHalfMin)
	}
	if h := barHeight(0, BarHalfMin, BarHalfMax); h != BarHalfMin {
		t.Errorf("silêncio absoluto = %d px, want %d", h, BarHalfMin)
	}
}

// TestBarHeight_EhMonotonico: mais alto nunca pode desenhar mais baixo.
func TestBarHeight_EhMonotonico(t *testing.T) {
	anterior := -1
	for rms := 0.0; rms <= 1.0; rms += 0.005 {
		h := barHeight(rms, BarHalfMin, BarHalfMax)
		if h < anterior {
			t.Fatalf("barHeight(%.3f) = %d, menor que a amplitude anterior (%d)", rms, h, anterior)
		}
		anterior = h
	}
}

// TestBarHeight_FaixaInvertidaNaoEstoura: min > max é erro de chamador,
// mas não deve devolver altura negativa nem entrar em pânico.
func TestBarHeight_FaixaInvertidaNaoEstoura(t *testing.T) {
	h := barHeight(0.5, 20, 4)
	if h < 4 || h > 20 {
		t.Errorf("barHeight(0.5, 20, 4) = %d, fora de [4,20]", h)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{3 * time.Second, "0:03"},
		{65 * time.Second, "1:05"},
		{119*time.Second + 999*time.Millisecond, "2:00"}, // arredonda pro segundo, não trunca
		{-5 * time.Second, "0:00"},                       // negativo não deveria acontecer, mas não deve estourar
		{3661 * time.Second, "1:01:01"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.d); got != c.want {
			t.Errorf("FormatDuration(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestDimensoes_PilulaCompactaEncolheuDe30a40PorCento trava o pedido de
// Mauro de 29-set-2026 — "ainda dá pra tirar uns 30 a 40% do tamanho
// dela" — sobre a pílula de gravação e processamento, que era 200x32.
//
// É uma faixa, e não um número, porque o pedido foi uma faixa: quem
// ajustar a pílula dentro dela não precisa mexer aqui; quem sair dela
// está desfazendo uma decisão dele, e tem de saber disso.
func TestDimensoes_PilulaCompactaEncolheuDe30a40PorCento(t *testing.T) {
	const antesW, antesH = 200, 32
	for _, p := range []dictation.OverlayPhase{dictation.OverlayRecording, dictation.OverlayProcessing} {
		d := DimensoesPara(p)
		for _, c := range []struct {
			nome         string
			agora, antes int
		}{
			{"largura", d.W, antesW},
			{"altura", d.H, antesH},
		} {
			reducao := 1 - float64(c.agora)/float64(c.antes)
			if reducao < 0.30 || reducao > 0.40 {
				t.Errorf("%s: %s da pílula: %d px, %.0f%% menor que os %d de antes — o pedido foi de 30 a 40%%",
					p, c.nome, c.agora, 100*reducao, c.antes)
			}
		}
	}
	// E dentro da faixa, o número exato que ele aprovou ("tamanho e cores
	// perfeitos"). A mudança das mensagens, no mesmo dia, não toca nela.
	if d := DimensoesPara(dictation.OverlayRecording); d.W != 128 || d.H != 20 {
		t.Errorf("pílula compacta = %dx%d, want os 128x20 aprovados", d.W, d.H)
	}
}

// TestDimensoes_PilulaCompactaEhCapsula: raio de metade da altura é o
// raio "pill" do design system. A altura par é o que põe o eixo da onda
// espelhada no meio exato — com altura ímpar ela sairia 1 px torta.
func TestDimensoes_PilulaCompactaEhCapsula(t *testing.T) {
	d := DimensoesPara(dictation.OverlayRecording)
	if d.Radius != d.H/2 {
		t.Errorf("raio da pílula compacta = %d, want %d (metade da altura: cápsula)", d.Radius, d.H/2)
	}
	if d.H%2 != 0 {
		t.Errorf("altura da pílula compacta = %d, ímpar: o eixo da onda não cai no meio", d.H)
	}
}

// TestDimensoes_RaiosSaoDoDesignSystem: toda pílula desenhada usa um raio
// do design system — 6, 10, 16 ou 24 px, ou a cápsula (metade da altura).
// Até 29-set-2026 a de mensagem tinha 12, que não é nenhum deles.
func TestDimensoes_RaiosSaoDoDesignSystem(t *testing.T) {
	tokens := map[int]bool{6: true, 10: true, 16: true, 24: true}
	for _, p := range fasesDoOverlay() {
		d := DimensoesPara(p)
		if !tokens[d.Radius] && d.Radius != d.H/2 {
			t.Errorf("%s: raio %d px na pílula %dx%d — não é raio do design system (6, 10, 16, 24 ou pílula)",
				nomeDaFase(p), d.Radius, d.W, d.H)
		}
	}
}

// TestDimensoes_TodaPilulaCabeNaSuperficie: a superfície da janela é
// alocada uma vez, no tamanho de TamanhoDaSuperficie, e cada quadro é
// copiado para o canto dela. Um quadro maior não cabe — Draw recusa, e a
// pílula não aparece. Toda fase, com texto ou onda, tem de caber.
func TestDimensoes_TodaPilulaCabeNaSuperficie(t *testing.T) {
	sw, sh := TamanhoDaSuperficie()
	maiorH := 0
	for _, p := range fasesDoOverlay() {
		d := DimensoesPara(p)
		if d.W > sw || d.H > sh {
			t.Errorf("%s: pílula %dx%d não cabe na superfície %dx%d", nomeDaFase(p), d.W, d.H, sw, sh)
		}
		maiorH = max(maiorH, d.H)
	}
	// E a superfície não é inventada: a altura é a da maior pílula que
	// existe, e a largura, a máxima que a composição admite.
	if maiorH != sh {
		t.Errorf("a pílula mais alta tem %d px e a superfície %d", maiorH, sh)
	}
	if sw != MensagemLarguraMax {
		t.Errorf("superfície com %d px de largura, want MensagemLarguraMax = %d", sw, MensagemLarguraMax)
	}
}
