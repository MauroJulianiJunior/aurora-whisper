package tray

import "math"

// PixelsDoIcone desenha o ícone da bandeja em tam x tam pixels, BGRA com
// alfa direto (não pré-multiplicado) — o formato do bitmap de cor de 32
// bits que CreateIconIndirect espera, o mesmo dos arquivos .ico.
//
// # O desenho: o Pico estourado
//
// É o símbolo do sistema escolhido pelo Mauro em 29-set-2026
// (docs/marca/pico-estourado/mark-small.svg, a versão para 16–32 px): os
// colchetes de canto do design system do projeto emolduram a voz, e a barra do
// meio fura a moldura por cima e por baixo — uma voz alta demais para
// caber no limite.
//
// O ícone anterior — um quadrado escuro com cinco barras cinza — era
// parecido demais com o do Wispr Flow, e o Mauro não o achou na bandeja
// por causa disso.
//
// # Cores
//
// Do design system (ver paleta): colchetes verdes, porque verde ali é
// "sistema ativo"; a barra em off-white na barra de tarefas escura e em
// preto na clara. Fundo transparente — o ícone não traz quadrado próprio,
// e por isso precisa de duas variantes (Tema).
//
// # Pausado
//
// Colchetes em cinza e, no lugar da barra que estoura, o sinal de pausa:
// duas barras curtas DENTRO da moldura. Muda a forma, e não só a cor —
// quem não distingue verde de cinza vê a pausa do mesmo jeito.
//
// # Tamanho e nitidez
//
// tam vem de GetSystemMetrics(SM_CXSMICON): 16 px a 100% de escala, 20 a
// 125%, 24 a 150%, 32 a 200%. O desenho é feito no tamanho exato, e toda
// borda reta cai em coordenada inteira de pixel: ela sai com o pixel
// inteiro aceso ou inteiro apagado, nunca meio aceso. Só as curvas — o
// canto do colchete e, de 24 px para cima, a ponta redonda da barra do
// meio — ganham meia-intensidade, que é o que as deixa redondas em vez de
// em escada.
func PixelsDoIcone(tam int, tema Tema, pausado bool) []byte {
	if tam <= 0 {
		return nil
	}
	g := geometriaPara(tam)
	colchete, barra := paleta(tema, pausado)
	temBarra := g.noPico
	if pausado {
		temBarra = g.naPausa
	}

	// Supersampling: sub x sub amostras por pixel. As amostras caem no
	// meio de cada subdivisão, nunca numa coordenada inteira, e por isso
	// uma borda alinhada ao pixel dá cobertura exata de 0 ou 1.
	const sub = 8
	pix := make([]byte, tam*tam*4)
	for y := 0; y < tam; y++ {
		for x := 0; x < tam; x++ {
			nColchete, nBarra := 0, 0
			for sy := 0; sy < sub; sy++ {
				py := float64(y) + (float64(sy)+0.5)/sub
				for sx := 0; sx < sub; sx++ {
					px := float64(x) + (float64(sx)+0.5)/sub
					switch {
					case g.noColchete(px, py):
						nColchete++
					case temBarra(px, py):
						nBarra++
					}
				}
			}
			n := nColchete + nBarra
			if n == 0 {
				continue
			}
			// As formas não se tocam, então um pixel de borda mistura uma
			// forma com o transparente, e quase nunca duas formas. Quando
			// mistura, a cor é a média pesada pela cobertura de cada uma.
			mistura := func(c1, c2 uint8) byte {
				return byte((nColchete*int(c1) + nBarra*int(c2) + n/2) / n)
			}
			i := (y*tam + x) * 4
			pix[i] = mistura(colchete.b, barra.b)
			pix[i+1] = mistura(colchete.g, barra.g)
			pix[i+2] = mistura(colchete.r, barra.r)
			pix[i+3] = byte((n*255 + sub*sub/2) / (sub * sub))
		}
	}
	return pix
}

// TamanhoDoIcone normaliza o que GetSystemMetrics(SM_CXSMICON) devolveu.
// Zero é o valor de falha da chamada, e desenhar em zero pixels não
// daria ícone nenhum; 16 é o tamanho a 100% de escala, o menor que o
// Windows usa.
func TamanhoDoIcone(metrica int) int {
	if metrica < 16 {
		return 16
	}
	return metrica
}

// cor é uma cor RGB de 8 bits por canal.
type cor struct{ r, g, b uint8 }

// Cores da paleta do projeto que o ícone usa. Os nomes à direita são os
// dos tokens de cor.
var (
	corVerde    = cor{0x00, 0xE6, 0x76} // systemGreen: ação, sistema ativo
	corOffWhite = cor{0xF5, 0xF7, 0xF5} // interfaceWhite: no lugar do branco puro
	corPreto    = cor{0x07, 0x0A, 0x09} // voidBlack
	corCinza    = cor{0x9A, 0xA4, 0xA0} // steelGray: processo, repouso
)

// paleta decide a cor dos colchetes e a das barras.
//
// Os colchetes são verdes nas duas barras de tarefas: no design system,
// verde "sinaliza capacidade, progresso, ação ou estado válido", e aqui é
// exatamente isso — o ditado está pronto. Pausado, eles vão para o cinza
// de processo, e o verde some junto com o estado que ele afirmava.
//
// As barras levam o contraste: off-white na barra de tarefas escura,
// preto na clara. Trocadas, sumiriam — off-white sobre a barra clara do
// Windows 11 fica perto de 1:1 (ver icone_test.go).
func paleta(tema Tema, pausado bool) (colchete, barra cor) {
	colchete, barra = corVerde, corOffWhite
	if pausado {
		colchete = corCinza
	}
	if tema == TemaClaro {
		barra = corPreto
	}
	return colchete, barra
}

// geometria é o Pico estourado em tam x tam, em coordenadas contínuas de
// pixel: o pixel (x, y) cobre [x, x+1) × [y, y+1).
//
// As proporções vêm do mark-small.svg (viewBox 32), arredondadas para
// pixel inteiro em cada tamanho. Tudo o que é borda reta — os lados dos
// braços dos colchetes, os lados das barras — cai em coordenada inteira.
type geometria struct {
	tam float64

	// s é a espessura do traço dos colchetes.
	s float64
	// borda e topo são a borda EXTERNA da moldura, à esquerda e em cima;
	// à direita e embaixo, por simetria, tam-borda e tam-topo.
	borda, topo float64
	// braco é o alcance de cada braço do colchete, da borda externa até a
	// ponta.
	braco float64
	// raioFora é o raio da curva do canto, no lado de fora do traço; o de
	// dentro é raioFora-s.
	raioFora float64

	// larguraPico e pontaPico: a barra que estoura a moldura, centrada, da
	// linha pontaPico até tam-pontaPico, ponta incluída. picoRedondo diz se
	// a ponta é redonda, como no SVG, ou reta (ver geometriaPara).
	larguraPico, pontaPico float64
	picoRedondo            bool

	// O sinal de pausa: duas barras de larguraPausa separadas por vaoPausa,
	// centradas, de topoPausa até tam-topoPausa.
	larguraPausa, vaoPausa, topoPausa float64
}

func geometriaPara(tam int) geometria {
	// As divisões inteiras arredondam as proporções do mark-small.svg para
	// o pixel mais próximo: traço 4/32, borda 2/32, topo 6/32, braço 8/32,
	// barra de 2/32 a 30/32.
	s := max(1, (tam+3)/8) // 16→2, 20→2, 24→3, 32→4
	borda := (tam + 8) / 16
	topo := (3*tam + 8) / 16
	braco := (tam + 2) / 4

	// A barra do meio só fica centrada se a largura tiver a paridade de
	// tam; senão sobra meio pixel de um lado, e a borda cairia no meio de
	// um pixel.
	pico := s
	if (tam-pico)%2 != 0 {
		pico++
	}
	// A ponta redonda só existe com raio de 2 px ou mais. Com a barra de
	// 2 px — 16 e 20 px de ícone —, o "redondo" são os dois pixels da ponta
	// a 79% (π/4): no PNG, a barra saía com as pontas cinza, como um
	// defeito, e não redonda. Reta, ela termina limpa, como os braços.
	picoRedondo := pico >= 4

	// O sinal de pausa cabe no vão entre as pontas dos braços, com 1 px
	// de folga de cada lado, e tem a altura da moldura. Encostadas nos
	// braços, as duas barras viravam, com os colchetes, um "Ↄ C" no PNG.
	// Um terço do vão para cada barra e o resto no meio; o vão tem a
	// paridade de tam, então o sinal sai centrado.
	livre := tam - 2*(borda+braco+1) // 16→4, 20→6, 24→6, 32→10
	larguraPausa := max(1, livre/3)

	return geometria{
		tam:          float64(tam),
		s:            float64(s),
		borda:        float64(borda),
		topo:         float64(topo),
		braco:        float64(braco),
		raioFora:     math.Min(float64(braco-1), float64(s)+float64(tam)/32),
		larguraPico:  float64(pico),
		pontaPico:    float64((tam + 8) / 16),
		picoRedondo:  picoRedondo,
		larguraPausa: float64(larguraPausa),
		vaoPausa:     float64(livre - 2*larguraPausa),
		topoPausa:    float64(topo),
	}
}

// dobra leva (x, y) para o quadrante de cima à esquerda. Os quatro
// colchetes são o mesmo, espelhado; testar só um basta.
func (g geometria) dobra(x, y float64) (float64, float64) {
	if x > g.tam/2 {
		x = g.tam - x
	}
	if y > g.tam/2 {
		y = g.tam - y
	}
	return x, y
}

// noColchete reporta se o ponto está num dos quatro colchetes.
//
// O colchete de cima à esquerda é um L de traço s: o braço vertical, o
// horizontal e o canto de fora arredondado em raioFora. As pontas dos
// braços são retas, cortadas no pixel, e não redondas como no SVG: numa
// ponta redonda de 2 px, que é o traço em 16 px, os dois pixels da ponta
// ficariam a π/4 (79%) — quatro pontas meio apagadas num ícone em que o
// colchete já tem só 4 px de braço.
func (g geometria) noColchete(x, y float64) bool {
	x, y = g.dobra(x, y)
	b, t := g.borda, g.topo
	if x < b || y < t || x >= b+g.braco || y >= t+g.braco {
		return false
	}
	if x >= b+g.s && y >= t+g.s {
		return false // o miolo do L
	}
	// No quadrado do canto, o arredondado: fora do raio de fora é vazio,
	// e dentro do raio de dentro também — o traço acompanha a curva.
	cx, cy := b+g.raioFora, t+g.raioFora
	if x < cx && y < cy {
		d := math.Hypot(x-cx, y-cy)
		if d > g.raioFora {
			return false
		}
		if dentro := g.raioFora - g.s; dentro > 0 && d < dentro {
			return false
		}
	}
	return true
}

// noPico reporta se o ponto está na barra que estoura a moldura.
func (g geometria) noPico(x, y float64) bool {
	x0, h := (g.tam-g.larguraPico)/2, g.tam-2*g.pontaPico
	if !g.picoRedondo {
		return x >= x0 && x < x0+g.larguraPico && y >= g.pontaPico && y < g.pontaPico+h
	}
	return naBarra(x, y, x0, g.pontaPico, g.larguraPico, h)
}

// naPausa reporta se o ponto está numa das duas barras do sinal de pausa.
//
// As pontas são retas, ao contrário das da barra do meio: a primeira
// versão as arredondava, e duas barras curtas de ponta redonda, lado a
// lado numa moldura, viravam um par de olhos no PNG.
func (g geometria) naPausa(x, y float64) bool {
	if y < g.topoPausa || y >= g.tam-g.topoPausa {
		return false
	}
	x0 := (g.tam - 2*g.larguraPausa - g.vaoPausa) / 2
	x1 := x0 + g.larguraPausa + g.vaoPausa
	return (x >= x0 && x < x0+g.larguraPausa) || (x >= x1 && x < x1+g.larguraPausa)
}

// naBarra reporta se o ponto está na barra vertical de canto superior
// esquerdo (x0, y0), largura w e altura h, com as pontas de cima e de
// baixo arredondadas em meia-largura — as barras da onda do mark.svg.
func naBarra(x, y, x0, y0, w, h float64) bool {
	if x < x0 || x >= x0+w || y < y0 || y >= y0+h {
		return false
	}
	r := w / 2
	cx := x0 + r
	switch {
	case y < y0+r:
		return math.Hypot(x-cx, y-(y0+r)) <= r
	case y >= y0+h-r:
		return math.Hypot(x-cx, y-(y0+h-r)) <= r
	}
	return true
}
