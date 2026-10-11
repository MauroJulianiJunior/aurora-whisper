package main

import (
	"math"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/tray"
)

// O desenho do ícone do programa é o do ícone da bandeja: o Pico estourado
// (docs/marca/pico-estourado/mark-small.svg), que tray.PixelsDoIcone já
// desenha em Go, em qualquer tamanho, sem imagem de origem. Reaproveitar a
// função, e não redesenhar, garante que o executável, o Setup e a bandeja
// mostram o mesmo símbolo, e que mudar o desenho da bandeja muda o do arquivo
// no próximo `go run ./cmd/gerarico` (um teste avisa se o arquivo ficou para trás).
//
// A diferença é o ladrilho. O ícone da bandeja não tem fundo, porque fica sobre
// a barra de tarefas e a variante escolhida contrasta com ela. O ícone do
// executável aparece no Explorador, no Menu Iniciar e em "Aplicativos", sobre
// fundo claro e escuro, então leva o ladrilho preto arredondado do
// mark-small.svg (rect rx=7 de 32), com os colchetes verdes e a barra off-white
// por cima, exatamente como o arquivo SVG.

// corDoLadrilho é o voidBlack do design system (#070A09), o preenchimento do
// ladrilho do mark-small.svg. É o mesmo da barra do ícone da bandeja no tema
// claro (tray, corPreto).
var corDoLadrilho = [3]float64{0x07, 0x0A, 0x09} // R, G, B

// raioDoLadrilho é o rx do mark-small.svg, em fração do lado: 7 de 32.
const raioDoLadrilho = 7.0 / 32.0

// subamostras é o lado da grade de supersampling por pixel, como no desenho
// da bandeja: as bordas retas do ladrilho caem em pixel inteiro e só o canto
// arredondado ganha meia-intensidade.
const subamostras = 8

// pixelsDoIcone devolve o ícone em tam x tam, BGRA com alfa direto (não
// pré-multiplicado), o formato do .ico de 32 bits: o ladrilho preto
// arredondado com o Pico estourado da bandeja (tema escuro, ativo) por cima.
func pixelsDoIcone(tam int) []byte {
	marca := tray.PixelsDoIcone(tam, tray.TemaEscuro, false)
	lado := float64(tam)
	raio := raioDoLadrilho * lado

	pix := make([]byte, tam*tam*4)
	for y := 0; y < tam; y++ {
		for x := 0; x < tam; x++ {
			dentro := 0
			for sy := 0; sy < subamostras; sy++ {
				py := float64(y) + (float64(sy)+0.5)/subamostras
				for sx := 0; sx < subamostras; sx++ {
					px := float64(x) + (float64(sx)+0.5)/subamostras
					if noLadrilho(px, py, lado, raio) {
						dentro++
					}
				}
			}
			aLadrilho := float64(dentro) / (subamostras * subamostras)

			i := (y*tam + x) * 4
			aMarca := float64(marca[i+3]) / 255
			// A marca por cima do ladrilho ("over"), as duas com alfa direto.
			aSaida := aMarca + aLadrilho*(1-aMarca)
			if aSaida == 0 {
				continue
			}
			cores := [3]float64{ // B, G, R da marca
				float64(marca[i]), float64(marca[i+1]), float64(marca[i+2]),
			}
			ladrilho := [3]float64{corDoLadrilho[2], corDoLadrilho[1], corDoLadrilho[0]} // B, G, R
			for c := 0; c < 3; c++ {
				v := (cores[c]*aMarca + ladrilho[c]*aLadrilho*(1-aMarca)) / aSaida
				pix[i+c] = byte(math.Round(v))
			}
			pix[i+3] = byte(math.Round(aSaida * 255))
		}
	}
	return pix
}

// noLadrilho reporta se o ponto (x, y) está no quadrado lado x lado de cantos
// arredondados em raio.
func noLadrilho(x, y, lado, raio float64) bool {
	meio := lado / 2
	dx := math.Max(math.Abs(x-meio)-(meio-raio), 0)
	dy := math.Max(math.Abs(y-meio)-(meio-raio), 0)
	return math.Hypot(dx, dy) <= raio
}
