package tray

import (
	"errors"
	"math"
	"testing"
)

// tamanhosDoIcone são os que GetSystemMetrics(SM_CXSMICON) devolve: 100%,
// 125%, 150% e 200% de escala, e 250% e 300% dos monitores mais densos.
var tamanhosDoIcone = []int{16, 20, 24, 32, 40, 48}

type variante struct {
	tema    Tema
	pausado bool
}

func (v variante) String() string {
	s := "escuro"
	if v.tema == TemaClaro {
		s = "claro"
	}
	if v.pausado {
		return s + "/pausado"
	}
	return s + "/ativo"
}

var variantes = []variante{
	{TemaEscuro, false}, {TemaEscuro, true},
	{TemaClaro, false}, {TemaClaro, true},
}

// icone é o ícone desenhado, com leitura por pixel.
type icone struct {
	tam int
	pix []byte
}

func desenhar(tam int, v variante) icone {
	return icone{tam, PixelsDoIcone(tam, v.tema, v.pausado)}
}

func (ic icone) alfa(x, y int) uint8 { return ic.pix[(y*ic.tam+x)*4+3] }

func (ic icone) cor(x, y int) cor {
	i := (y*ic.tam + x) * 4
	return cor{ic.pix[i+2], ic.pix[i+1], ic.pix[i]}
}

// eh reporta se o pixel é da cor c, inteiro ou parcial. Um pixel de borda
// mistura uma forma só com o transparente, então a cor dele é a da forma,
// exata; só o alfa cai.
func (ic icone) eh(x, y int, c cor) bool { return ic.alfa(x, y) > 0 && ic.cor(x, y) == c }

// Cores esperadas, escritas aqui de novo, e não lidas das variáveis do
// pacote: se alguém trocar o token lá, este teste precisa discordar.
var (
	esperadoVerde    = cor{0x00, 0xE6, 0x76}
	esperadoOffWhite = cor{0xF5, 0xF7, 0xF5}
	esperadoPreto    = cor{0x07, 0x0A, 0x09}
	esperadoCinza    = cor{0x9A, 0xA4, 0xA0}

	// As barras de tarefas do Windows 11, medidas a olho nas capturas do
	// sistema: a escura fica perto de #202020, e a clara, de #F3F3F3.
	barraDeTarefasEscura = cor{0x20, 0x20, 0x20}
	barraDeTarefasClara  = cor{0xF3, 0xF3, 0xF3}
)

func corDoColchete(v variante) cor {
	if v.pausado {
		return esperadoCinza
	}
	return esperadoVerde
}

func corDaBarra(v variante) cor {
	if v.tema == TemaClaro {
		return esperadoPreto
	}
	return esperadoOffWhite
}

func barraDeTarefas(tema Tema) cor {
	if tema == TemaClaro {
		return barraDeTarefasClara
	}
	return barraDeTarefasEscura
}

// TestPaleta_CoresDoDesignSystem trava as cores nos tokens de cor da
// paleta do projeto.
func TestPaleta_CoresDoDesignSystem(t *testing.T) {
	for _, v := range variantes {
		colchete, barra := paleta(v.tema, v.pausado)
		if colchete != corDoColchete(v) {
			t.Errorf("%v: colchete %v, quer %v", v, colchete, corDoColchete(v))
		}
		if barra != corDaBarra(v) {
			t.Errorf("%v: barra %v, quer %v", v, barra, corDaBarra(v))
		}
	}
}

// TestIcone_TamanhoEFundoTransparente: tam x tam pixels BGRA, sem o
// quadrado de fundo do mark-small.svg — na bandeja, o ícone fica direto
// sobre a barra de tarefas.
func TestIcone_TamanhoEFundoTransparente(t *testing.T) {
	if PixelsDoIcone(0, TemaEscuro, false) != nil {
		t.Error("tamanho zero desenhou alguma coisa")
	}
	for _, tam := range tamanhosDoIcone {
		for _, v := range variantes {
			ic := desenhar(tam, v)
			if len(ic.pix) != tam*tam*4 {
				t.Fatalf("%d px %v: %d bytes, quer %d", tam, v, len(ic.pix), tam*tam*4)
			}
			for _, p := range [][2]int{{0, 0}, {tam - 1, 0}, {0, tam - 1}, {tam - 1, tam - 1}, {0, tam / 2}} {
				if a := ic.alfa(p[0], p[1]); a != 0 {
					t.Errorf("%d px %v: pixel (%d, %d) com alfa %d — o fundo tem de ser transparente", tam, v, p[0], p[1], a)
				}
			}
		}
	}
}

// TestIcone_BordasRetasSemMeiaIntensidade: toda borda reta cai em pixel
// inteiro. Meia-intensidade só é aceita onde o desenho é curvo: no canto
// arredondado de cada colchete e, quando a barra do meio tem ponta
// redonda, nas linhas dessa ponta.
//
// Qualquer borda deslocada meio pixel — uma divisão que arredondou para o
// lado errado, a barra sem a paridade de tam — acende uma coluna ou linha
// inteira a 50%, e o ícone sai borrado na bandeja.
func TestIcone_BordasRetasSemMeiaIntensidade(t *testing.T) {
	for _, tam := range tamanhosDoIcone {
		g := geometriaPara(tam)
		raio := int(math.Ceil(g.raioFora))
		curvo := func(x, y int) bool {
			// Para o quadrante de cima à esquerda: os quatro são o mesmo.
			if x >= tam/2 {
				x = tam - 1 - x
			}
			if y >= tam/2 {
				y = tam - 1 - y
			}
			b, tp := int(g.borda), int(g.topo)
			noCanto := x >= b && x < b+raio && y >= tp && y < tp+raio
			naPonta := g.picoRedondo && y >= int(g.pontaPico) && y < int(g.pontaPico+g.larguraPico/2)
			return noCanto || naPonta
		}
		for _, v := range variantes {
			ic := desenhar(tam, v)
			erros := 0
			for y := 0; y < tam && erros < 4; y++ {
				for x := 0; x < tam && erros < 4; x++ {
					if a := ic.alfa(x, y); a != 0 && a != 255 && !curvo(x, y) {
						t.Errorf("%d px %v: pixel (%d, %d) com alfa %d numa borda reta", tam, v, x, y, a)
						erros++
					}
				}
			}
		}
	}
}

// TestIcone_BarraInteiraNosTamanhosPequenos: em 16 e 20 px a barra tem
// 2 px de largura, e a ponta redonda seriam os dois pixels da ponta a 79%
// (π/4) — no PNG, a barra saía com as pontas cinza, como um defeito. Ali
// ela termina reta, e todo pixel dela é inteiro.
func TestIcone_BarraInteiraNosTamanhosPequenos(t *testing.T) {
	for _, tam := range []int{16, 20} {
		for _, v := range variantes {
			ic := desenhar(tam, v)
			for y := 0; y < tam; y++ {
				for x := 0; x < tam; x++ {
					if ic.eh(x, y, corDaBarra(v)) && ic.alfa(x, y) != 255 {
						t.Fatalf("%d px %v: pixel da barra em (%d, %d) com alfa %d", tam, v, x, y, ic.alfa(x, y))
					}
				}
			}
		}
	}
}

// TestIcone_Simetrico: o Pico estourado é simétrico nos dois eixos, e o
// desenho também — pixel a pixel, cor e alfa. É o que garante que os
// testes abaixo, que olham um canto só, valem para os quatro.
func TestIcone_Simetrico(t *testing.T) {
	for _, tam := range tamanhosDoIcone {
		for _, v := range variantes {
			ic := desenhar(tam, v)
			for y := 0; y < tam; y++ {
				for x := 0; x < tam; x++ {
					for _, e := range [][2]int{{tam - 1 - x, y}, {x, tam - 1 - y}} {
						if ic.alfa(x, y) != ic.alfa(e[0], e[1]) || ic.cor(x, y) != ic.cor(e[0], e[1]) {
							t.Fatalf("%d px %v: (%d, %d) difere do espelho (%d, %d)", tam, v, x, y, e[0], e[1])
						}
					}
				}
			}
		}
	}
}

// moldura é o retângulo que os colchetes ocupam, bordas incluídas.
type moldura struct{ x0, y0, x1, y1 int }

func molduraDe(t *testing.T, ic icone, colchete cor) moldura {
	t.Helper()
	m := moldura{ic.tam, ic.tam, -1, -1}
	for y := 0; y < ic.tam; y++ {
		for x := 0; x < ic.tam; x++ {
			if ic.eh(x, y, colchete) {
				m.x0, m.y0 = min(m.x0, x), min(m.y0, y)
				m.x1, m.y1 = max(m.x1, x), max(m.y1, y)
			}
		}
	}
	if m.x1 < 0 {
		t.Fatalf("%d px: nenhum pixel na cor dos colchetes %v", ic.tam, colchete)
	}
	return m
}

// TestIcone_ColchetesNosQuatroCantos: quatro colchetes de canto, cada um
// um L, e não uma moldura fechada — o meio de cada lado fica vazio.
func TestIcone_ColchetesNosQuatroCantos(t *testing.T) {
	for _, tam := range tamanhosDoIcone {
		for _, v := range variantes {
			ic := desenhar(tam, v)
			c := corDoColchete(v)
			m := molduraDe(t, ic, c)

			// O L de cima à esquerda: um braço desce pela coluna da borda, o
			// outro corre pela linha do topo. O pixel do canto em si pode
			// estar apagado — é o arredondado —, então cada braço é medido
			// do primeiro ao último pixel de colchete da sua linha, na
			// metade de cima à esquerda.
			trecho := func(em func(i int) bool) (primeiro, ultimo, n int) {
				primeiro, ultimo = -1, -1
				for i := 0; i < tam/2; i++ {
					if em(i) {
						if primeiro < 0 {
							primeiro = i
						}
						ultimo = i
						n++
					}
				}
				return primeiro, ultimo, n
			}
			v0, v1, nv := trecho(func(y int) bool { return ic.eh(m.x0, y, c) })
			h0, h1, nh := trecho(func(x int) bool { return ic.eh(x, m.y0, c) })
			if nv < 3 || nh < 3 || nv != v1-v0+1 || nh != h1-h0+1 {
				t.Errorf("%d px %v: braços do colchete com %d e %d px, interrompidos ou curtos — não é um colchete", tam, v, nv, nh)
				continue
			}
			// Os braços param antes do meio do lado: colchete de canto, e
			// não moldura fechada.
			if v1 >= tam/2-1 || h1 >= tam/2-1 {
				t.Errorf("%d px %v: braços até a linha %d e a coluna %d chegam ao meio do lado — viraram moldura", tam, v, v1, h1)
			}
			// O miolo do L é vazio: um colchete, não um quadrado cheio.
			if a := ic.alfa(h1, v1); a != 0 {
				t.Errorf("%d px %v: o miolo do colchete, em (%d, %d), está aceso (alfa %d)", tam, v, h1, v1, a)
			}
			// O de baixo à direita, pelo espelho das pontas dos braços.
			if !ic.eh(m.x1, tam-1-v1, c) || !ic.eh(tam-1-h1, m.y1, c) {
				t.Errorf("%d px %v: colchete de baixo à direita ausente", tam, v)
			}
			// O meio de cada lado da moldura não tem colchete.
			for _, p := range [][2]int{{m.x0, tam / 2}, {m.x1, tam / 2}, {tam / 2, m.y0}, {tam / 2, m.y1}} {
				if ic.eh(p[0], p[1], c) {
					t.Errorf("%d px %v: pixel de colchete no meio do lado, em (%d, %d)", tam, v, p[0], p[1])
				}
			}
		}
	}
}

// alturaDaBarra devolve a primeira e a última linha com pixel da cor da
// barra.
func alturaDaBarra(ic icone, barra cor) (y0, y1 int) {
	y0, y1 = ic.tam, -1
	for y := 0; y < ic.tam; y++ {
		for x := 0; x < ic.tam; x++ {
			if ic.eh(x, y, barra) {
				y0, y1 = min(y0, y), max(y1, y)
			}
		}
	}
	return y0, y1
}

// TestIcone_PicoFuraAMoldura: ativo, a barra do meio passa da moldura por
// cima e por baixo — é o "estourado" do nome. Pausado, nada fura: o sinal
// de pausa fica dentro dela.
func TestIcone_PicoFuraAMoldura(t *testing.T) {
	for _, tam := range tamanhosDoIcone {
		for _, v := range variantes {
			ic := desenhar(tam, v)
			m := molduraDe(t, ic, corDoColchete(v))
			y0, y1 := alturaDaBarra(ic, corDaBarra(v))
			if y1 < 0 {
				t.Errorf("%d px %v: nenhum pixel na cor da barra", tam, v)
				continue
			}
			if !v.pausado {
				// No mark-small.svg, a barra passa 4/32 do lado da moldura:
				// no mínimo 2 px já em 16 px.
				if y0 > m.y0-2 || y1 < m.y1+2 {
					t.Errorf("%d px %v: a barra vai da linha %d à %d e a moldura da %d à %d — o pico não fura a moldura", tam, v, y0, y1, m.y0, m.y1)
				}
				continue
			}
			if y0 < m.y0 || y1 > m.y1 {
				t.Errorf("%d px %v: pausado, a barra vai da linha %d à %d, fora da moldura (%d a %d)", tam, v, y0, y1, m.y0, m.y1)
			}
		}
	}
}

// barrasNoMeio conta as barras que cruzam a linha do meio do ícone: os
// trechos seguidos de pixels acesos. Só o alfa, a cor não entra — é a
// forma que se vê sem distinguir cor nenhuma.
func barrasNoMeio(ic icone) int {
	n, acesa := 0, false
	for x := 0; x < ic.tam; x++ {
		a := ic.alfa(x, ic.tam/2) > 0
		if a && !acesa {
			n++
		}
		acesa = a
	}
	return n
}

// TestIcone_PausadoInconfundivelSemCor: pausado não pode depender só de
// cor — quem não distingue verde de cinza, ou olha a barra de tarefas de
// relance, precisa ver a diferença. A silhueta muda: ativo, uma barra
// cruza o meio; pausado, duas (o sinal de pausa).
func TestIcone_PausadoInconfundivelSemCor(t *testing.T) {
	for _, tam := range tamanhosDoIcone {
		for _, tema := range []Tema{TemaEscuro, TemaClaro} {
			ativo := desenhar(tam, variante{tema, false})
			pausado := desenhar(tam, variante{tema, true})

			if n := barrasNoMeio(ativo); n != 1 {
				t.Errorf("%d px tema %d: ativo com %d barras no meio, quer 1", tam, tema, n)
			}
			if n := barrasNoMeio(pausado); n != 2 {
				t.Errorf("%d px tema %d: pausado com %d barras no meio, quer 2 — o sinal de pausa", tam, tema, n)
			}

			diferentes := 0
			for y := 0; y < tam; y++ {
				for x := 0; x < tam; x++ {
					if (ativo.alfa(x, y) > 0) != (pausado.alfa(x, y) > 0) {
						diferentes++
					}
				}
			}
			if diferentes < tam {
				t.Errorf("%d px tema %d: as silhuetas diferem em %d pixels só", tam, tema, diferentes)
			}

			// E a cor acompanha: sem verde na pausa, porque o verde afirma
			// "sistema pronto".
			for y := 0; y < tam; y++ {
				for x := 0; x < tam; x++ {
					if pausado.eh(x, y, esperadoVerde) {
						t.Fatalf("%d px tema %d: verde no ícone pausado, em (%d, %d)", tam, tema, x, y)
					}
				}
			}
		}
	}
}

// luminancia é a luminância relativa do WCAG 2.2.
func luminancia(c cor) float64 {
	canal := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*canal(c.r) + 0.7152*canal(c.g) + 0.0722*canal(c.b)
}

// contraste é a razão de contraste do WCAG 2.2, de 1 a 21.
func contraste(a, b cor) float64 {
	la, lb := luminancia(a), luminancia(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestIcone_ContrasteComABarraDeTarefas: a barra do ícone — a do meio, ou
// as duas da pausa — é o que leva o estado, e ela precisa de 3:1 contra a
// barra de tarefas (WCAG 2.2, 1.4.11, contraste de objeto gráfico), nas
// duas cores de barra de tarefas. Mede a cor que foi DESENHADA, e não a
// que a paleta diz.
//
// Os colchetes verdes na barra de tarefas clara ficam abaixo disso — ver
// o log do teste. É a decisão de marca (verde nos dois temas); o estado
// não depende deles.
func TestIcone_ContrasteComABarraDeTarefas(t *testing.T) {
	if c := contraste(cor{0, 0, 0}, cor{255, 255, 255}); math.Abs(c-21) > 0.01 {
		t.Fatalf("contraste(preto, branco) = %.2f, quer 21 — a conta está errada", c)
	}
	const minimo = 3.0
	for _, tam := range tamanhosDoIcone {
		for _, v := range variantes {
			ic := desenhar(tam, v)
			fundo := barraDeTarefas(v.tema)
			colchete := corDoColchete(v)
			achou := false
			for y := 0; y < tam; y++ {
				for x := 0; x < tam; x++ {
					if ic.alfa(x, y) != 255 || ic.cor(x, y) == colchete {
						continue
					}
					achou = true
					if c := contraste(ic.cor(x, y), fundo); c < minimo {
						t.Fatalf("%d px %v: barra %v contra a barra de tarefas %v = %.2f:1, quer %.1f:1", tam, v, ic.cor(x, y), fundo, c, minimo)
					}
				}
			}
			if !achou {
				t.Errorf("%d px %v: nenhum pixel de barra inteiro", tam, v)
			}
		}
	}
	for _, v := range variantes {
		t.Logf("%v: colchetes %.2f:1, barra %.2f:1 contra a barra de tarefas",
			v, contraste(corDoColchete(v), barraDeTarefas(v.tema)), contraste(corDaBarra(v), barraDeTarefas(v.tema)))
	}
}

// TestTamanhoDoIcone: zero é a falha de GetSystemMetrics, e 16 é o menor
// tamanho que o Windows usa.
func TestTamanhoDoIcone(t *testing.T) {
	for _, c := range []struct{ metrica, quer int }{
		{0, 16}, {-1, 16}, {12, 16}, {16, 16}, {20, 20}, {24, 24}, {32, 32}, {48, 48},
	} {
		if got := TamanhoDoIcone(c.metrica); got != c.quer {
			t.Errorf("TamanhoDoIcone(%d) = %d, quer %d", c.metrica, got, c.quer)
		}
	}
}

// TestTemaDaBarra: SystemUsesLightTheme = 1 é a barra clara; qualquer
// falha de leitura fica com a escura, o padrão do Windows.
func TestTemaDaBarra(t *testing.T) {
	falha := errors.New("o sistema não pode encontrar o arquivo especificado")
	for _, c := range []struct {
		valor uint64
		err   error
		quer  Tema
	}{
		{1, nil, TemaClaro},
		{0, nil, TemaEscuro},
		{0, falha, TemaEscuro},
		{1, falha, TemaEscuro}, // leitura que falhou não vale, mesmo com valor
	} {
		if got := TemaDaBarra(c.valor, c.err); got != c.quer {
			t.Errorf("TemaDaBarra(%d, %v) = %d, quer %d", c.valor, c.err, got, c.quer)
		}
	}
}

// TestMudouOTema: só "ImmersiveColorSet" é troca de tema; o Windows manda
// WM_SETTINGCHANGE por muita outra coisa.
func TestMudouOTema(t *testing.T) {
	for area, quer := range map[string]bool{
		"ImmersiveColorSet": true,
		"immersivecolorset": true,
		"":                  false,
		"Policy":            false,
		"intl":              false,
		"Environment":       false,
	} {
		if got := mudouOTema(area); got != quer {
			t.Errorf("mudouOTema(%q) = %v, quer %v", area, got, quer)
		}
	}
}
