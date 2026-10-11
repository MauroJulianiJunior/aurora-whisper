package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/tray"
)

// Os ícones do app instalável da nuvem (ADR-019, F5): os PNGs que o manifest
// (web/pwa/manifest.webmanifest) aponta e que o build da nuvem copia para a
// raiz do app. O desenho é o mesmo do executável, da aba e da bandeja (decisão
// do Mauro, 05-out-2026): o ladrilho escuro com o Pico estourado.
//
//   - icone-192.png e icone-512.png ("any"): o pixelsDoIcone de sempre, o
//     ladrilho arredondado com os cantos transparentes. É o que o Chrome do PC
//     põe no Iniciar e na janela do app, e o Android usa na tela de abertura.
//   - icone-maskable-512.png ("maskable"): o launcher do Android recorta o
//     ícone no formato dele (círculo, squircle, quadrado arredondado...), então
//     este não tem canto transparente nenhum: é o quadrado cheio da cor do
//     ladrilho, com o símbolo menor no meio.

// iconeDoApp é um PNG de web/pwa: o nome, o lado e o desenho.
type iconeDoApp struct {
	Nome     string
	Tam      int
	Maskable bool
}

// iconesDoApp são os três PNGs do manifest, na ordem dele.
var iconesDoApp = []iconeDoApp{
	{"icone-192.png", 192, false},
	{"icone-512.png", 512, false},
	{"icone-maskable-512.png", 512, true},
}

// pixels desenha o ícone (BGRA, alfa direto).
func (i iconeDoApp) pixels() []byte {
	if i.Maskable {
		return pixelsDoMaskable(i.Tam)
	}
	return pixelsDoIcone(i.Tam)
}

// ladoDaMarcaNoMaskable é o lado do símbolo dentro do maskable de 512 px.
//
// Por que 384: todo launcher preserva, no mínimo, o círculo de raio 40% do
// ícone (a zona segura do maskable, 204,8 px em 512). O ponto do símbolo mais
// longe do centro é o lado de fora do arco de um colchete, a uns 15,3/32 do
// lado: 184 px em 384, dentro do círculo, com folga. E 384 = 12 × 32, então a
// grade do desenho (que vem de um SVG de 32) cai em pixel inteiro, e as bordas
// retas saem nítidas, como na bandeja.
const ladoDaMarcaNoMaskable = 384

// zonaSeguraDoMaskable é o raio da zona segura, em fração do lado (W3C,
// "Icon masks"; web.dev, "Adaptive icon support in PWAs with maskable icons").
const zonaSeguraDoMaskable = 0.40

// pixelsDoMaskable devolve o ícone maskable em tam x tam: o quadrado cheio, todo
// opaco, da cor do ladrilho, com o Pico estourado da bandeja (tema escuro, ativo)
// centrado por cima. Só existe em 512 px (o lado do símbolo é fixo).
func pixelsDoMaskable(tam int) []byte {
	lado := ladoDaMarcaNoMaskable
	marca := tray.PixelsDoIcone(lado, tray.TemaEscuro, false)
	desloc := (tam - lado) / 2
	fundo := [3]float64{corDoLadrilho[2], corDoLadrilho[1], corDoLadrilho[0]} // B, G, R

	pix := make([]byte, tam*tam*4)
	for y := 0; y < tam; y++ {
		for x := 0; x < tam; x++ {
			i := (y*tam + x) * 4
			mx, my := x-desloc, y-desloc
			aMarca := 0.0
			var cores [3]float64
			if mx >= 0 && my >= 0 && mx < lado && my < lado {
				j := (my*lado + mx) * 4
				aMarca = float64(marca[j+3]) / 255
				cores = [3]float64{float64(marca[j]), float64(marca[j+1]), float64(marca[j+2])}
			}
			// A marca por cima do fundo opaco: o resultado é sempre opaco.
			for c := 0; c < 3; c++ {
				pix[i+c] = byte(math.Round(cores[c]*aMarca + fundo[c]*(1-aMarca)))
			}
			pix[i+3] = 255
		}
	}
	return pix
}

// gerarIconesDoApp escreve os PNGs de iconesDoApp na pasta (que tem de existir).
func gerarIconesDoApp(pasta string) error {
	for _, ic := range iconesDoApp {
		dados, err := codificarPNG(ic.Tam, ic.pixels())
		if err != nil {
			return fmt.Errorf("%s: %w", ic.Nome, err)
		}
		if err := os.WriteFile(filepath.Join(pasta, ic.Nome), dados, 0o644); err != nil {
			return fmt.Errorf("%w (rode da raiz do repositório)", err)
		}
	}
	return nil
}
