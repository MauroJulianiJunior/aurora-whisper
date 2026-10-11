package overlay

// Canvas é um buffer de pixels 32bpp B,G,R,A com alpha pré-multiplicado
// — o mesmo layout de bytes que CreateDIBSection produz para um DIB
// top-down BI_RGB usado com o blend ACSrcAlpha de UpdateLayeredWindow
// (win32.BLENDFUNCTION; ver docs/adr/ADR-010-camada-nativa-windows.md,
// D2). Em produção, cada quadro é desenhado num Canvas do tamanho da
// pílula (Quadro) e copiado para o canto do DIB da janela
// (copiaParaSuperficie), que é o que UpdateLayeredWindow apresenta. Pix é
// um []byte comum — o que permite testar todo o desenho (cores, forma da
// pílula, texto) sem Windows nenhum (ver canvas_test.go,
// tipografia_test.go).
//
// Nenhum método aqui usa GDI: a pílula com cantos arredondados, as
// barras da onda e o texto são todos escritos pixel a pixel em Go puro.
// ADR-010, D2 já rejeitou Direct2D por ser COM demais para poucas
// dezenas de barras a ~30 fps; a mesma lógica vale, com mais força
// ainda, contra abrir a superfície GDI de desenho de forma/texto (pen,
// brush, font, TextOut) só para uma pílula de status — ver render.go.
type Canvas struct {
	W, H int
	Pix  []byte // len == W*H*4, 4 bytes por pixel: B, G, R, A
}

// NewCanvas aloca um Canvas w x h em branco (totalmente transparente:
// todo byte, incluindo alpha, começa em zero).
func NewCanvas(w, h int) *Canvas {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return &Canvas{W: w, H: h, Pix: make([]byte, w*h*4)}
}

func (c *Canvas) offset(x, y int) (int, bool) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return 0, false
	}
	return (y*c.W + x) * 4, true
}

// At devolve a cor pré-multiplicada do pixel (x,y): b, g, r, a. Fora
// dos limites do Canvas devolve zero (transparente) em vez de entrar em
// pânico — todo método de desenho deste pacote pode escrever coordenada
// fora da tela sem checar antes (ver FillRoundedRect, que
// deliberadamente não recorta o retângulo de entrada).
func (c *Canvas) At(x, y int) (b, g, r, a byte) {
	off, ok := c.offset(x, y)
	if !ok {
		return 0, 0, 0, 0
	}
	return c.Pix[off], c.Pix[off+1], c.Pix[off+2], c.Pix[off+3]
}

// Set escreve o pixel (x,y) com a cor pré-multiplicada b, g, r, a; b, g
// e r devem ser <= a para o resultado ser um ARGB pré-multiplicado
// válido. Quase todo desenho deste pacote é opaco (a=255); o único que
// escreve alpha parcial é compoe, na borda suavizada, e ele mantém a
// invariante por construção — travada em canvas_test.go, e não
// verificada em runtime. Fora dos limites, não faz nada.
func (c *Canvas) Set(x, y int, b, g, r, a byte) {
	off, ok := c.offset(x, y)
	if !ok {
		return
	}
	c.Pix[off], c.Pix[off+1], c.Pix[off+2], c.Pix[off+3] = b, g, r, a
}

// Clear apaga o Canvas inteiro para transparente.
func (c *Canvas) Clear() {
	for i := range c.Pix {
		c.Pix[i] = 0
	}
}

// FillRect preenche, opaco (alpha 255) e sólido na cor b,g,r, o
// retângulo de canto superior-esquerdo (x,y) e tamanho w x h. Usado
// para as barras da onda (render.go).
func (c *Canvas) FillRect(x, y, w, h int, b, g, r byte) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			c.Set(x+dx, y+dy, b, g, r, 255)
		}
	}
}

// FillRoundedRect preenche, opaco e sólido na cor b,g,r, o retângulo
// (x,y,w,h) com os quatro cantos arredondados em radius pixels. Sem
// suavização de borda: cada pixel está dentro ou fora, pelo centro. É o
// que o corpo da pílula e as barras usam; a borda que encontra o papel de
// parede usa FillRoundedRectSuave.
func (c *Canvas) FillRoundedRect(x, y, w, h, radius int, b, g, r byte) {
	if w <= 0 || h <= 0 {
		return
	}
	if radius > w/2 {
		radius = w / 2
	}
	if radius > h/2 {
		radius = h / 2
	}
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			if !insideRoundedRect(dx, dy, w, h, radius) {
				continue
			}
			c.Set(x+dx, y+dy, b, g, r, 255)
		}
	}
}

// insideRoundedRect reporta se o pixel local (x,y) — coordenadas
// relativas ao canto superior-esquerdo de um retângulo w x h — está
// dentro desse retângulo com cantos arredondados em radius.
//
// A conta é a do CENTRO do pixel contra o retângulo interno
// [radius, w-radius] x [radius, h-radius]: está dentro quem fica a no
// máximo radius dele. Tudo em meios-pixels, para o centro (2x+1, 2y+1)
// ser inteiro.
//
// A versão até 29-set-2026 testava o canto do pixel e punha os centros
// dos círculos em radius e em h-radius-1 — um pixel de diferença entre o
// de cima e o de baixo. Com raio pequeno não se via. Com a cápsula da
// pílula compacta (raio = metade da altura) os dois círculos caíam em
// linhas diferentes e nenhum alcançava a coluna da ponta: a pílula saía
// 2 px mais estreita que o pedido, e o contorno de 1 px ia parar uma
// coluna para dentro. TestRender_PilulaTemContorno achou.
func insideRoundedRect(x, y, w, h, radius int) bool {
	px, py, r := 2*x+1, 2*y+1, 2*radius
	cx := min(max(px, r), 2*w-r)
	cy := min(max(py, r), 2*h-r)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

// FillRoundedRectSuave desenha a mesma forma de FillRoundedRect com a
// borda suavizada: cada pixel recebe a fração dele que a forma cobre, e
// essa fração é composta POR CIMA do que já está no Canvas (compoe).
// Pixel coberto inteiro sai na cor exata, como em FillRoundedRect; só os
// da curva mudam. Os lados retos continuam nítidos.
//
// # Por quê
//
// Até 29-set-2026 toda forma era binária (ADR-010, D2: "correto e
// legível" primeiro). Com a pílula compacta virando cápsula inteira, de
// raio 10, as pontas passaram a mostrar a escada, sobretudo com a pílula
// quase preta sobre papel de parede claro — e a janela é layered com
// alpha por pixel justamente para isso ser de graça (revisão de 29-set).
func (c *Canvas) FillRoundedRectSuave(x, y, w, h, radius int, b, g, r byte) {
	if w <= 0 || h <= 0 {
		return
	}
	radius = max(0, min(radius, w/2, h/2))
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			if n := coberturaRoundedRect(dx, dy, w, h, radius); n > 0 {
				c.compoe(x+dx, y+dy, b, g, r, n)
			}
		}
	}
}

// amostrasPorLado é a grade de amostras por pixel com que
// coberturaRoundedRect mede a fração coberta: 4x4 dá 17 níveis de alpha,
// que na borda de uma curva de raio 4 a 16 não se distinguem de mais.
const (
	amostrasPorLado = 4
	amostras        = amostrasPorLado * amostrasPorLado
)

// coberturaRoundedRect conta quantas das amostras do pixel local (x,y)
// caem dentro do retângulo w x h de cantos arredondados em radius: de 0 a
// amostras. É a conta de insideRoundedRect — distância ao retângulo
// interno — feita em cada amostra em vez de só no centro, e inteira: a
// unidade é 1/(2*amostrasPorLado) de pixel, e a amostra i do pixel fica
// em 2i+1 unidades da borda dele.
func coberturaRoundedRect(x, y, w, h, radius int) int {
	// Fora dos quatro quadrados de canto a forma cobre o pixel inteiro:
	// numa faixa reta, a distância ao retângulo interno é só a de um
	// eixo, e ela nunca passa do raio dentro de w x h.
	if (x >= radius && x < w-radius) || (y >= radius && y < h-radius) {
		return amostras
	}
	const u = 2 * amostrasPorLado
	rr := u * radius
	n := 0
	for j := 0; j < amostrasPorLado; j++ {
		sy := u*y + 2*j + 1
		dy := sy - min(max(sy, rr), u*h-rr)
		for i := 0; i < amostrasPorLado; i++ {
			sx := u*x + 2*i + 1
			dx := sx - min(max(sx, rr), u*w-rr)
			if dx*dx+dy*dy <= rr*rr {
				n++
			}
		}
	}
	return n
}

// compoe pinta a cor opaca b,g,r sobre n/amostras do pixel (x,y), por
// cima do que já está lá: saída = cor·f + atual·(1-f), nos quatro canais,
// com f = n/amostras. É o "over" do alpha pré-multiplicado, e mantém a
// invariante de Set (cor <= alpha) por construção: a cor nova é <= 255 e
// a atual já é <= o alpha atual.
func (c *Canvas) compoe(x, y int, b, g, r byte, n int) {
	c.compoeFracao(x, y, b, g, r, n, amostras)
}

// compoeCobertura é compoe para a máscara do rasterizador de fonte (ver
// tipografia.go), que mede a cobertura de 0 a 255 em vez de em amostras.
func (c *Canvas) compoeCobertura(x, y int, b, g, r, cobertura byte) {
	c.compoeFracao(x, y, b, g, r, int(cobertura), 255)
}

// compoeFracao pinta b,g,r sobre n/total do pixel (x,y). É a conta de
// compoe, com o denominador livre.
func (c *Canvas) compoeFracao(x, y int, b, g, r byte, n, total int) {
	if n <= 0 {
		return
	}
	if n >= total {
		c.Set(x, y, b, g, r, 255)
		return
	}
	ab, ag, ar, aa := c.At(x, y)
	mistura := func(nova, atual byte) byte {
		return byte((int(nova)*n + int(atual)*(total-n) + total/2) / total)
	}
	c.Set(x, y, mistura(b, ab), mistura(g, ag), mistura(r, ar), mistura(255, aa))
}
