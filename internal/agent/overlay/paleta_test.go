package overlay

import (
	"math"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// contaCor conta os pixels opacos de c exatamente na cor cor.
func contaCor(c *Canvas, cor Cor) int {
	n := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if b, g, r, a := c.At(x, y); a == 255 && (Cor{R: r, G: g, B: b}) == cor {
				n++
			}
		}
	}
	return n
}

// contraste é a razão de contraste do WCAG 2.x entre duas cores sRGB.
func contraste(a, b Cor) float64 {
	lin := func(v byte) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	lum := func(c Cor) float64 { return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B) }
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestContraste_ReferenciasDoWCAG confere o próprio medidor: com ele
// errado, o teste de contraste abaixo aprovaria qualquer paleta.
func TestContraste_ReferenciasDoWCAG(t *testing.T) {
	preto, branco := Cor{0, 0, 0}, Cor{255, 255, 255}
	if got := contraste(preto, branco); math.Abs(got-21) > 0.01 {
		t.Errorf("contraste(preto, branco) = %.2f, want 21", got)
	}
	if got := contraste(branco, branco); got != 1 {
		t.Errorf("contraste(branco, branco) = %.2f, want 1", got)
	}
	// #767676 sobre branco é o cinza mais claro que passa 4,5:1 — o
	// exemplo clássico da regra.
	if got := contraste(Cor{0x76, 0x76, 0x76}, branco); got < 4.5 || got > 4.6 {
		t.Errorf("contraste(#767676, branco) = %.2f, want ~4,54", got)
	}
}

// TestPaleta_EhADoDesignSystem trava os tokens.
//
// Substituiu, em 29-set-2026, a paleta provisória (pílula 32,32,32, texto
// branco puro, barras cinza 110..245) por decisão de projeto: a paleta do
// projeto vale para tudo, sem logo nem nome pessoal. Um "ajuste" de
// cor aqui é uma decisão de identidade, não de implementação — por isso
// falha em vez de passar calado.
func TestPaleta_EhADoDesignSystem(t *testing.T) {
	for _, c := range []struct {
		nome       string
		got, token Cor
	}{
		{"corpo (voidBlack #070A09)", corCorpo, Cor{0x07, 0x0A, 0x09}},
		{"contorno (borderDark #24322D)", corBorda, Cor{0x24, 0x32, 0x2D}},
		{"texto (interfaceWhite #F5F7F5)", corTexto, Cor{0xF5, 0xF7, 0xF5}},
		{"verde (systemGreen #00E676)", corVerde, Cor{0x00, 0xE6, 0x76}},
		{"cinza (steelGray #9AA4A0)", corCinza, Cor{0x9A, 0xA4, 0xA0}},
		{"vermelho (machineRed #FF3B3B)", corVermelho, Cor{0xFF, 0x3B, 0x3B}},
	} {
		if c.got != c.token {
			t.Errorf("%s = %v, want %v", c.nome, c.got, c.token)
		}
	}
	if b, g, r := CorPilula(); (Cor{R: r, G: g, B: b}) != corCorpo {
		t.Errorf("CorPilula() = %d,%d,%d (B,G,R), não é o corpo da pílula", b, g, r)
	}
}

// TestPaleta_CorpoEscolhidoPeloContraste é a medição que escolheu o corpo
// da pílula entre os dois pretos do DS — o voidBlack (#070A09) e o
// surfaceRaised (#111916) — e trava os números que o comentário de
// corCorpo cita.
//
// O que fica atrás da pílula é o papel de parede ou o aplicativo de quem
// usa, então é contra ELES que se mede. Os fundos escuros são o preto
// puro, o #0C0C0C do Terminal e o cinza-escuro do modo escuro do Windows
// e dos aplicativos (#1F1F1F, #202124, #2B2B2B).
func TestPaleta_CorpoEscolhidoPeloContraste(t *testing.T) {
	surfaceRaised := Cor{0x11, 0x19, 0x16}

	// 1. Sobre fundo claro não há o que decidir: os dois passam de 15:1.
	for _, fundo := range []Cor{{0xFF, 0xFF, 0xFF}, {0xF0, 0xF0, 0xF0}} {
		for _, corpo := range []Cor{corCorpo, surfaceRaised} {
			if c := contraste(corpo, fundo); c < 15 {
				t.Errorf("corpo %v sobre fundo claro %v: %.2f:1, want >= 15", corpo, fundo, c)
			}
		}
	}

	// 2. Sobre o cinza-escuro do modo escuro, que é o fundo escuro mais
	// comum atrás da pílula, o corpo escolhido recorta mais.
	for _, fundo := range []Cor{{0x1F, 0x1F, 0x1F}, {0x20, 0x21, 0x24}, {0x2B, 0x2B, 0x2B}} {
		if escolhido, outro := contraste(corCorpo, fundo), contraste(surfaceRaised, fundo); escolhido <= outro {
			t.Errorf("sobre %v o corpo escolhido dá %.2f:1 e o surfaceRaised %.2f:1 — "+
				"a conta que decidiu o corpo virou", fundo, escolhido, outro)
		}
	}

	// 3. Perto do preto — o puro e o #0C0C0C do Terminal — nenhum corpo
	// aparece; o contorno é o que recorta, e tem de recortar mais que o
	// corpo.
	for _, fundo := range []Cor{{}, {0x0C, 0x0C, 0x0C}} {
		if borda, corpo := contraste(corBorda, fundo), contraste(corCorpo, fundo); borda <= corpo {
			t.Errorf("sobre %v o contorno dá %.2f:1 e o corpo %.2f:1 — o contorno não recorta mais", fundo, borda, corpo)
		}
	}
	// 3b. No cinza-escuro é o contrário: ali o contorno quase some (1,06:1
	// sobre #2B2B2B) e quem recorta é o corpo. Um cobre onde o outro some —
	// é o que o comentário de TestRender_PilulaTemContorno afirma.
	cinzaEscuro := Cor{0x2B, 0x2B, 0x2B}
	if borda, corpo := contraste(corBorda, cinzaEscuro), contraste(corCorpo, cinzaEscuro); corpo <= borda {
		t.Errorf("sobre %v o corpo dá %.2f:1 e o contorno %.2f:1 — o corpo não recorta mais", cinzaEscuro, corpo, borda)
	}

	// 4. O contorno se lê como contorno: contrasta mais com o corpo
	// escolhido do que contrastaria com o surfaceRaised.
	if escolhido, outro := contraste(corBorda, corCorpo), contraste(corBorda, surfaceRaised); escolhido <= outro {
		t.Errorf("contorno sobre o corpo: %.2f:1, sobre o surfaceRaised: %.2f:1", escolhido, outro)
	}
}

// TestPaleta_ContrasteSobreAPilula é a conta que decidiu o repouso da
// onda em cinza, e não no verde profundo #0F3D2E (1,6:1 sobre a pílula).
//
// Texto pede 4,5:1 (WCAG 1.4.3). Barra e ponto de estado são elemento
// gráfico que informa, e pedem 3:1 (WCAG 1.4.11) — TODAS as cores que a
// barra pode ter, não só as pontas: a barra em repouso é o que diz
// "estou ouvindo" no silêncio, e se ela sumir a pílula parece vazia.
func TestPaleta_ContrasteSobreAPilula(t *testing.T) {
	if c := contraste(Cor{0x0F, 0x3D, 0x2E}, corCorpo); c >= 3 {
		t.Errorf("o verde profundo deu %.2f:1 sobre a pílula — o motivo de o repouso ser cinza "+
			"(corCinza, em paleta.go) deixou de valer", c)
	}
	if c := contraste(corTexto, corCorpo); c < 4.5 {
		t.Errorf("texto sobre a pílula: %.2f:1, WCAG 1.4.3 pede 4,5:1", c)
	}
	for m := BarHalfMin; m <= BarHalfMax; m++ {
		if c := contraste(corDaBarra(m), corCorpo); c < 3 {
			t.Errorf("barra de meia-altura %d (%v) sobre a pílula: %.2f:1, WCAG 1.4.11 pede 3:1",
				m, corDaBarra(m), c)
		}
	}
	for _, p := range fasesDoOverlay() {
		if cor, ok := sinalPara(p); ok {
			if c := contraste(cor, corCorpo); c < 3 {
				t.Errorf("ponto de %s (%v) sobre a pílula: %.2f:1, WCAG 1.4.11 pede 3:1", p, cor, c)
			}
		}
	}
}

// TestRender_PilulaTemContorno: as duas pílulas, a compacta e a de
// mensagem, têm o contorno de 1 px em borderDark, com o corpo logo dentro
// dele.
//
// Sobre papel de parede escuro, quem recorta a pílula do fundo depende do
// tom, e nenhum dos dois recorta muito (TestPaleta_CorpoEscolhidoPeloContraste):
//
//   - perto do preto é o CONTORNO: sobre #000 dá 1,57:1 contra 1,06 do
//     corpo, e sobre o #0C0C0C do Terminal, 1,46 contra 1,02;
//   - no cinza-escuro do modo escuro é o CORPO: sobre #2B2B2B dá 1,40:1, e
//     o contorno, 1,06; em #1F1F1F os dois empatam (1,21 e 1,23).
//
// Um cobre a faixa em que o outro some; por isso as duas coisas, e não só
// o corpo. (A versão anterior deste comentário dizia que sobre fundo
// escuro quem recorta é sempre o contorno — a medição só confirma isso
// perto do preto.)
func TestRender_PilulaTemContorno(t *testing.T) {
	for _, p := range []dictation.OverlayPhase{dictation.OverlayRecording, dictation.OverlaySuccess, dictation.OverlayError} {
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p})
		d := DimensoesPara(p)
		px, py := cantoNoCanvasDeTeste(d)
		meioY, meioX := py+d.H/2, px+d.W/2

		for _, borda := range [][2]int{
			{px, meioY}, {px + d.W - 1, meioY}, // laterais, na altura do meio
			{meioX, py}, {meioX, py + d.H - 1}, // topo e base, no meio da largura
		} {
			b, g, r, a := c.At(borda[0], borda[1])
			if got := (Cor{R: r, G: g, B: b}); a != 255 || got != corBorda {
				t.Errorf("fase %s: (%d,%d) = %v (alpha %d), want o contorno %v",
					p, borda[0], borda[1], got, a, corBorda)
			}
		}
		b, g, r, _ := c.At(px+BordaPilula, meioY)
		if got := (Cor{R: r, G: g, B: b}); got != corCorpo {
			t.Errorf("fase %s: logo dentro do contorno = %v, want o corpo %v", p, got, corCorpo)
		}
		if _, _, _, a := c.At(px-1, meioY); a != 0 {
			t.Errorf("fase %s: pixel opaco fora da pílula, à esquerda do contorno", p)
		}
	}
}

// TestSinal_CadaDesfechoTemOSeuPonto trava o mapeamento de sinalPara —
// e, renderizando, que o ponto de fato aparece na cor dita.
//
// A decisão que mais importa aqui é a da área de transferência: cinza, e
// não verde. O texto NÃO foi inserido; verde diria que foi, e a pessoa
// bateria o olho e não colaria.
//
// Percorre fasesDoOverlay, e não só as do núcleo: a janela de
// administrador ficou sem ponto nenhum enquanto este teste olhava só
// todasAsFases (ver sinalPara).
func TestSinal_CadaDesfechoTemOSeuPonto(t *testing.T) {
	quer := map[dictation.OverlayPhase]Cor{
		dictation.OverlaySuccess:      corVerde,
		dictation.OverlayClipboard:    corCinza,
		dictation.OverlayError:        corVermelho,
		dictation.OverlayBlocked:      corVermelho,
		dictation.OverlayInsertFailed: corVermelho,
		FaseJanelaElevada:             corVermelho,
		FaseTextoNoHistorico:          corCinza,
		FaseRepeticaoRemovida:         corVerde,
	}
	for _, p := range fasesDoOverlay() {
		cor, ok := sinalPara(p)
		want, deveTer := quer[p]
		if ok != deveTer || cor != want {
			t.Errorf("sinalPara(%s) = %v, %v; want %v, %v", nomeDaFase(p), cor, ok, want, deveTer)
			continue
		}
		if !deveTer {
			continue
		}
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p})
		if contaCor(c, want) == 0 {
			t.Errorf("fase %s: nenhum pixel na cor do ponto %v", nomeDaFase(p), want)
		}
	}
}

// TestSinal_VermelhoSoEmFalhaERecusa trava as duas regras do design
// system para o vermelho, no frame desenhado:
//
//  1. só aparece quando algo falhou ou foi recusado — nunca na gravação,
//     no processamento, no sucesso nem na área de transferência;
//  2. nunca aparece sozinho: onde há vermelho há texto off-white, e é o
//     texto que diz o que houve.
func TestSinal_VermelhoSoEmFalhaERecusa(t *testing.T) {
	comVermelho := map[dictation.OverlayPhase]bool{
		dictation.OverlayError:        true,
		dictation.OverlayBlocked:      true,
		dictation.OverlayInsertFailed: true,
		FaseJanelaElevada:             true,
	}
	for _, p := range fasesDoOverlay() {
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p, Amps: ampsCheias(1.0)})
		vermelho := contaCor(c, corVermelho)

		if comVermelho[p] {
			if vermelho == 0 {
				t.Errorf("fase %s: falha sem o sinal vermelho", nomeDaFase(p))
			}
			if contaTinta(c) == 0 {
				t.Errorf("fase %s: vermelho sem texto — a cor sozinha não diz o que houve", nomeDaFase(p))
			}
		} else if vermelho != 0 {
			t.Errorf("fase %s: %d pixels vermelhos — vermelho é só para falha e recusa", nomeDaFase(p), vermelho)
		}
	}
}

// profundidade diz o quanto o CENTRO do pixel local (x,y) está para dentro
// da borda de um retângulo w x h de cantos arredondados em radius —
// negativo fora. É a distância com sinal da forma, em float: uma régua
// independente de insideRoundedRect, para o teste não medir o desenho com
// a mesma conta que o fez.
func profundidade(x, y, w, h, radius int) float64 {
	cx, cy := float64(x)+0.5, float64(y)+0.5
	mw, mh, r := float64(w)/2, float64(h)/2, float64(radius)
	qx := math.Abs(cx-mw) - (mw - r)
	qy := math.Abs(cy-mh) - (mh - r)
	fora := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	dentro := math.Min(math.Max(qx, qy), 0)
	return r - fora - dentro
}

// TestRender_ContornoTemAMesmaEspessuraNasCurvas: o contorno tem
// BordaPilula px em toda a volta, nas curvas também. Todo pixel de
// contorno fica a no máximo BordaPilula da borda de fora, e todo pixel de
// corpo a pelo menos isso.
//
// É o que obriga o corpo a ter raio d.Radius-BordaPilula: com o mesmo raio
// da forma de fora, o corpo recuado deixa de ser concêntrico e o contorno
// engrossa nos cantos da pílula de mensagem (na cápsula compacta o raio é
// recortado pela altura e o frame não muda). Mutante que sobreviveu à
// revisão de 29-set-2026.
func TestRender_ContornoTemAMesmaEspessuraNasCurvas(t *testing.T) {
	const eps = 1e-9
	for _, p := range []dictation.OverlayPhase{dictation.OverlayRecording, dictation.OverlaySuccess, dictation.OverlayError} {
		d := DimensoesPara(p)
		px, py := cantoNoCanvasDeTeste(d)
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p})

		contorno, corpo := 0, 0
		for y := py; y < py+d.H; y++ {
			for x := px; x < px+d.W; x++ {
				b, g, r, a := c.At(x, y)
				if a != 255 {
					continue
				}
				fundo := profundidade(x-px, y-py, d.W, d.H, d.Radius)
				switch (Cor{R: r, G: g, B: b}) {
				case corBorda:
					contorno++
					if fundo > BordaPilula+eps {
						t.Errorf("fase %s: contorno em (%d,%d), %.2f px para dentro da borda — "+
							"o contorno engrossou ali", p, x, y, fundo)
					}
				case corCorpo:
					corpo++
					if fundo < BordaPilula-eps {
						t.Errorf("fase %s: corpo em (%d,%d), só %.2f px para dentro da borda — "+
							"o contorno afinou ali", p, x, y, fundo)
					}
				}
			}
		}
		if contorno == 0 || corpo == 0 {
			t.Fatalf("fase %s: %d pixels de contorno e %d de corpo — o teste não prova nada", p, contorno, corpo)
		}
	}
}

// TestRender_PontasDaPilulaSaoSuaves: a silhueta da pílula — onde ela
// encontra o papel de parede — tem alpha parcial nas curvas, e só nelas.
// Binária, a cápsula de raio 10 mostrava a escada nas pontas, sobretudo
// sobre fundo claro (revisão de 29-set-2026). Os lados retos, as barras e
// o texto continuam opacos e nítidos.
func TestRender_PontasDaPilulaSaoSuaves(t *testing.T) {
	for _, p := range []dictation.OverlayPhase{dictation.OverlayRecording, dictation.OverlaySuccess, dictation.OverlayError} {
		d := DimensoesPara(p)
		px, py := cantoNoCanvasDeTeste(d)
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p, Amps: ampsCheias(1.0)})

		parciais := 0
		for y := 0; y < c.H; y++ {
			for x := 0; x < c.W; x++ {
				_, _, _, a := c.At(x, y)
				if a == 0 || a == 255 {
					continue
				}
				parciais++
				lx, ly := x-px, y-py
				noCanto := (lx < d.Radius || lx >= d.W-d.Radius) && (ly < d.Radius || ly >= d.H-d.Radius)
				if !noCanto {
					t.Errorf("fase %s: alpha parcial %d em (%d,%d), fora das curvas da pílula", p, a, x, y)
				}
			}
		}
		if parciais == 0 {
			t.Errorf("fase %s: nenhum pixel de alpha parcial — as pontas da pílula voltaram a ser escada", p)
		}
	}
}
