package overlay

import (
	"bytes"
	"math"
	"testing"
)

func TestNewCanvas_ComecaTransparente(t *testing.T) {
	c := NewCanvas(4, 4)
	if len(c.Pix) != 4*4*4 {
		t.Fatalf("len(Pix) = %d, want %d", len(c.Pix), 4*4*4)
	}
	for i, v := range c.Pix {
		if v != 0 {
			t.Fatalf("Pix[%d] = %d, want 0 (canvas novo deve ser transparente)", i, v)
		}
	}
}

func TestCanvas_SetAt(t *testing.T) {
	c := NewCanvas(3, 3)
	c.Set(1, 1, 10, 20, 30, 255)
	b, g, r, a := c.At(1, 1)
	if b != 10 || g != 20 || r != 30 || a != 255 {
		t.Fatalf("At(1,1) = (%d,%d,%d,%d), want (10,20,30,255)", b, g, r, a)
	}
}

func TestCanvas_SetAtForaDosLimitesNaoTravaEDevolveTransparente(t *testing.T) {
	c := NewCanvas(2, 2)
	c.Set(-1, 0, 1, 1, 1, 1) // não deve entrar em pânico
	c.Set(0, 5, 1, 1, 1, 1)
	b, g, r, a := c.At(-1, 0)
	if b != 0 || g != 0 || r != 0 || a != 0 {
		t.Fatalf("At fora dos limites = (%d,%d,%d,%d), want (0,0,0,0)", b, g, r, a)
	}
}

func TestCanvas_Clear(t *testing.T) {
	c := NewCanvas(2, 2)
	c.FillRect(0, 0, 2, 2, 1, 2, 3)
	c.Clear()
	for i, v := range c.Pix {
		if v != 0 {
			t.Fatalf("Pix[%d] = %d após Clear, want 0", i, v)
		}
	}
}

func TestCanvas_FillRect(t *testing.T) {
	c := NewCanvas(5, 5)
	c.FillRect(1, 1, 2, 2, 100, 150, 200)
	for y := 1; y < 3; y++ {
		for x := 1; x < 3; x++ {
			b, g, r, a := c.At(x, y)
			if b != 100 || g != 150 || r != 200 || a != 255 {
				t.Fatalf("At(%d,%d) = (%d,%d,%d,%d), want (100,150,200,255)", x, y, b, g, r, a)
			}
		}
	}
	// fora do retângulo continua transparente
	if _, _, _, a := c.At(0, 0); a != 0 {
		t.Fatalf("At(0,0) fora do FillRect: alpha = %d, want 0", a)
	}
}

func TestCanvas_FillRoundedRect_CentroOpacoCantoTransparente(t *testing.T) {
	c := NewCanvas(40, 40)
	c.FillRoundedRect(0, 0, 40, 40, 10, 1, 2, 3)

	// centro do retângulo: sempre dentro, mesmo com cantos arredondados.
	if _, _, _, a := c.At(20, 20); a != 255 {
		t.Fatalf("centro do rounded rect: alpha = %d, want 255", a)
	}
	// canto extremo (0,0): fora do círculo de raio 10 centrado em (10,10) -> transparente.
	if _, _, _, a := c.At(0, 0); a != 0 {
		t.Fatalf("canto (0,0) do rounded rect: alpha = %d, want 0 (fora do raio)", a)
	}
	// meio de uma borda reta (não é região de canto): dentro.
	if _, _, _, a := c.At(20, 0); a != 255 {
		t.Fatalf("borda reta (20,0): alpha = %d, want 255", a)
	}
}

// TestCanvas_FillRoundedRect_CapsulaAlcancaAsPontasESimetrica trava o
// defeito de geometria achado em 29-set-2026: com raio de metade da
// altura, os centros dos círculos de cima e de baixo ficavam em linhas
// diferentes, nenhum alcançava a coluna da ponta, e a forma saía torta
// em 1 px. A pílula compacta é exatamente essa cápsula.
func TestCanvas_FillRoundedRect_CapsulaAlcancaAsPontasESimetrica(t *testing.T) {
	for _, tam := range [][2]int{{128, 20}, {40, 12}, {9, 9}, {8, 8}} {
		w, h := tam[0], tam[1]
		c := NewCanvas(w, h)
		c.FillRoundedRect(0, 0, w, h, h/2, 1, 2, 3)

		alpha := func(x, y int) byte { _, _, _, a := c.At(x, y); return a }
		if alpha(0, h/2) != 255 || alpha(w-1, h/2) != 255 {
			t.Errorf("%dx%d raio %d: a cápsula não alcança as pontas na altura do meio", w, h, h/2)
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if a := alpha(x, y); a != alpha(w-1-x, y) || a != alpha(x, h-1-y) {
					t.Fatalf("%dx%d raio %d: forma assimétrica em (%d,%d)", w, h, h/2, x, y)
				}
			}
		}
	}
}

// formasDeTeste são as formas que o overlay de fato suaviza — a cápsula
// compacta, as pílulas de mensagem de uma e de duas linhas e o ponto de
// estado — e uma de raio zero.
var formasDeTeste = []struct{ w, h, r int }{
	{CompactWidth, CompactHeight, CompactRadius},
	{MensagemLarguraMin, alturaMensagem(1), PillRadius},
	{MensagemLarguraMax, alturaMensagem(MaxLinhas), PillRadius},
	{SinalDiametro, SinalDiametro, SinalDiametro / 2},
	{40, 12, 0},
}

// TestCanvas_FillRoundedRectSuave_CobreAAreaDaForma: a soma do alpha, em
// pixels, é a área da forma — w·h menos o que a curva tira dos quatro
// cantos, (4-π)·r² —, calculada pela geometria, não pelo desenho. É o que
// prova que cada pixel recebe a fração que a forma cobre dele, e não um
// degradê qualquer na borda.
func TestCanvas_FillRoundedRectSuave_CobreAAreaDaForma(t *testing.T) {
	for _, f := range formasDeTeste {
		c := NewCanvas(f.w, f.h)
		c.FillRoundedRectSuave(0, 0, f.w, f.h, f.r, 200, 220, 240)
		soma := 0.0
		for y := 0; y < f.h; y++ {
			for x := 0; x < f.w; x++ {
				_, _, _, a := c.At(x, y)
				soma += float64(a) / 255
			}
		}
		area := float64(f.w*f.h) - (4-math.Pi)*float64(f.r*f.r)
		// 4x4 amostras por pixel erram a curva em fração de pixel — medido:
		// 0,95% no ponto de 8 px, centésimos de 1% nas pílulas. 2% da área
		// cobre isso com folga.
		if math.Abs(soma-area) > 0.02*area {
			t.Errorf("%dx%d raio %d: alpha soma %.2f px², a forma tem %.2f px²", f.w, f.h, f.r, soma, area)
		}
	}
}

// TestCanvas_FillRoundedRectSuave_SoACurvaFicaParcial: pixel coberto
// inteiro sai na cor exata e opaco; alpha parcial só existe nos quadrados
// de canto — os lados retos continuam nítidos —, a forma é simétrica, e o
// alpha nunca diminui de fora para dentro (a forma é convexa: quanto mais
// para dentro, mais do pixel ela cobre). Com raio zero não há pixel
// parcial nenhum: é o FillRoundedRect.
func TestCanvas_FillRoundedRectSuave_SoACurvaFicaParcial(t *testing.T) {
	for _, f := range formasDeTeste {
		c := NewCanvas(f.w, f.h)
		c.FillRoundedRectSuave(0, 0, f.w, f.h, f.r, 200, 220, 240)
		alpha := func(x, y int) byte { _, _, _, a := c.At(x, y); return a }

		parciais := 0
		for y := 0; y < f.h; y++ {
			for x := 0; x < f.w; x++ {
				b, g, r, a := c.At(x, y)
				if a != alpha(f.w-1-x, y) || a != alpha(x, f.h-1-y) {
					t.Fatalf("%dx%d raio %d: forma assimétrica em (%d,%d)", f.w, f.h, f.r, x, y)
				}
				if x < f.w/2 && alpha(x+1, y) < a {
					t.Fatalf("%dx%d raio %d: o alpha cai de %d para %d indo para dentro, em (%d,%d)",
						f.w, f.h, f.r, a, alpha(x+1, y), x, y)
				}
				if a == 255 && (b != 200 || g != 220 || r != 240) {
					t.Fatalf("%dx%d raio %d: pixel inteiro (%d,%d) = (%d,%d,%d), want a cor exata",
						f.w, f.h, f.r, x, y, b, g, r)
				}
				if a == 0 || a == 255 {
					continue
				}
				parciais++
				noCanto := (x < f.r || x >= f.w-f.r) && (y < f.r || y >= f.h-f.r)
				if !noCanto {
					t.Fatalf("%dx%d raio %d: alpha parcial %d em (%d,%d), fora dos cantos",
						f.w, f.h, f.r, a, x, y)
				}
			}
		}
		if f.r > 0 && parciais == 0 {
			t.Errorf("%dx%d raio %d: nenhum pixel parcial — a borda não foi suavizada", f.w, f.h, f.r)
		}
		if f.r == 0 {
			binario := NewCanvas(f.w, f.h)
			binario.FillRoundedRect(0, 0, f.w, f.h, 0, 200, 220, 240)
			if !bytes.Equal(c.Pix, binario.Pix) {
				t.Errorf("%dx%d raio 0: difere do FillRoundedRect", f.w, f.h)
			}
		}
	}
}

// TestCanvas_FillRoundedRectSuave_CompoeSobreOQueJaEsta trava o "over"
// do alpha pré-multiplicado, que é o que UpdateLayeredWindow espera:
//
//   - sobre transparente, a cor sai multiplicada pelo alpha — nenhum
//     canal passa dele, e desfeita a multiplicação ela volta a ser a cor;
//   - sobre opaco, o pixel continua opaco e fica entre as duas cores.
func TestCanvas_FillRoundedRectSuave_CompoeSobreOQueJaEsta(t *testing.T) {
	const w, h, raio = 24, 24, 12

	c := NewCanvas(w, h)
	c.FillRoundedRectSuave(0, 0, w, h, raio, 200, 220, 240)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			b, g, r, a := c.At(x, y)
			if b > a || g > a || r > a {
				t.Fatalf("sobre transparente, (%d,%d) = (%d,%d,%d,%d): canal acima do alpha", x, y, b, g, r, a)
			}
			if a > 0 && a < 255 {
				if volta := int(r) * 255 / int(a); volta < 240-8 || volta > 240+8 {
					t.Errorf("sobre transparente, (%d,%d): o vermelho desmultiplicado dá %d, want ~240", x, y, volta)
				}
			}
		}
	}

	c = NewCanvas(w, h)
	c.FillRect(0, 0, w, h, 10, 10, 10)
	c.FillRoundedRectSuave(0, 0, w, h, raio, 200, 220, 240)
	mistos := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			b, g, r, a := c.At(x, y)
			if a != 255 {
				t.Fatalf("sobre opaco, (%d,%d) ficou com alpha %d", x, y, a)
			}
			if b < 10 || b > 200 || g < 10 || g > 220 || r < 10 || r > 240 {
				t.Fatalf("sobre opaco, (%d,%d) = (%d,%d,%d), fora das duas cores", x, y, b, g, r)
			}
			if b != 10 && b != 200 {
				mistos++
			}
		}
	}
	if mistos == 0 {
		t.Error("sobre opaco, nenhum pixel misturado — a borda não foi composta")
	}
}

// TestCanvas_CompoeCobertura trava a composição da máscara da fonte
// (tipografia.go): cobertura 0 não toca o pixel, 255 pinta a cor exata, e
// no meio o pixel fica na fração certa entre as duas cores — sobre opaco
// continua opaco, e sobre transparente sai pré-multiplicado, sem canal
// acima do alpha.
func TestCanvas_CompoeCobertura(t *testing.T) {
	c := NewCanvas(4, 1)
	c.FillRect(0, 0, 3, 1, 10, 20, 30) // o quarto pixel fica transparente

	c.compoeCobertura(0, 0, 250, 240, 230, 0)
	c.compoeCobertura(1, 0, 250, 240, 230, 255)
	c.compoeCobertura(2, 0, 250, 240, 230, 64)
	c.compoeCobertura(3, 0, 250, 240, 230, 128)

	for _, caso := range []struct {
		x          int
		b, g, r, a byte
	}{
		{0, 10, 20, 30, 255},
		{1, 250, 240, 230, 255},
		// 10 + (250-10)·64/255 = 70,2; 20 + 220·64/255 = 75,2; 30 + 200·64/255 = 80,2
		{2, 70, 75, 80, 255},
		// sobre transparente: cor·128/255 e alpha 128
		{3, 125, 120, 115, 128},
	} {
		b, g, r, a := c.At(caso.x, 0)
		if b != caso.b || g != caso.g || r != caso.r || a != caso.a {
			t.Errorf("pixel %d = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				caso.x, b, g, r, a, caso.b, caso.g, caso.r, caso.a)
		}
	}
}

func TestCanvas_FillRoundedRect_RaioMaiorQueMetadeDasDimensoesNaoEstoura(t *testing.T) {
	c := NewCanvas(10, 6)
	c.FillRoundedRect(0, 0, 10, 6, 100, 1, 1, 1) // radius >> w/2 e h/2
	// não deve entrar em pânico, e o centro continua opaco.
	if _, _, _, a := c.At(5, 3); a != 255 {
		t.Fatalf("centro com radius exagerado: alpha = %d, want 255", a)
	}
}
