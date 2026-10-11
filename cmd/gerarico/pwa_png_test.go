//go:build renderpng

package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestPWAEmPNG escreve a folha de prévia dos ícones do app instalável (ADR-019,
// F5), para alguém olhar antes de aprovar. Não afirma nada: o veredito é de quem
// olha (o mesmo motivo do icone_png_test.go da bandeja). Só escreve um arquivo.
//
//	PNGDIR=/algum/lugar go test -tags renderpng -run PWAEmPNG ./cmd/gerarico/
//
// A folha (pwa-icones.png, 1600 x 1200), de cima para baixo:
//
//  1. os "any" em tamanho real, o de 512 e o de 192, sobre um fundo claro (à
//     esquerda) e um escuro (à direita): o Iniciar do Windows, a janela do app,
//     a tela de abertura do Android;
//  2. o maskable recortado como os launchers do Android fazem, em 144 px (48 dp
//     num celular de densidade 3x, o tamanho de verdade na tela inicial): em
//     círculo, squircle e quadrado arredondado, no claro e no escuro;
//  3. o mesmo ampliado (352 px): o quadrado inteiro e os três recortes, com o
//     círculo da zona segura (raio de 40%) em magenta por cima.
func TestPWAEmPNG(t *testing.T) {
	dir := os.Getenv("PNGDIR")
	if dir == "" {
		t.Skip("defina PNGDIR")
	}
	const larg, alt, margem, vao = 1600, 1200, 32, 32
	claro := color.NRGBA{0xF1, 0xF3, 0xF4, 0xFF}
	escuro := color.NRGBA{0x20, 0x21, 0x24, 0xFF}
	cinza := color.NRGBA{0xDA, 0xDC, 0xE0, 0xFF}
	magenta := color.NRGBA{0xFF, 0x2D, 0x95, 0xFF}

	img := image.NewNRGBA(image.Rect(0, 0, larg, alt))
	pintar := func(r image.Rectangle, c color.NRGBA) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
	}

	// 1. Os "any" em tamanho real.
	alt1 := 512 + 2*margem
	pintar(image.Rect(0, 0, larg/2, alt1), claro)
	pintar(image.Rect(larg/2, 0, larg, alt1), escuro)
	for metade, fundo := range []color.NRGBA{claro, escuro} {
		ox := metade*larg/2 + margem
		colar(img, pixelsDoIcone(512), 512, ox, margem, fundo)
		colar(img, pixelsDoIcone(192), 192, ox+512+vao, margem+(512-192)/2, fundo)
	}

	// 2. O maskable recortado, em 144 px.
	maskable := pixelsDoMaskable(512)
	mascaras := []func(u, v float64) bool{circulo, squircle, quadradoArredondado}
	y2 := alt1
	alt2 := 144 + 2*margem
	pintar(image.Rect(0, y2, larg/2, y2+alt2), claro)
	pintar(image.Rect(larg/2, y2, larg, y2+alt2), escuro)
	for metade, fundo := range []color.NRGBA{claro, escuro} {
		usado := len(mascaras)*144 + (len(mascaras)-1)*vao
		ox := metade*larg/2 + (larg/2-usado)/2
		for i, m := range mascaras {
			recortar(img, maskable, 512, 144, ox+i*(144+vao), y2+margem, m, fundo, nil)
		}
	}

	// 3. Ampliado, com a zona segura.
	y3 := y2 + alt2
	pintar(image.Rect(0, y3, larg, alt), cinza)
	const grande = 352
	todos := append([]func(u, v float64) bool{func(float64, float64) bool { return true }}, mascaras...)
	usado := len(todos)*grande + (len(todos)-1)*vao
	ox := (larg - usado) / 2
	for i, m := range todos {
		recortar(img, maskable, 512, grande, ox+i*(grande+vao), y3+margem, m, cinza, &magenta)
	}

	nome := filepath.Join(dir, "pwa-icones.png")
	f, err := os.Create(nome)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("escrito %s", nome)
}

// colar põe pix (BGRA, alfa direto, tam x tam) em (ox, oy), sobre o fundo.
func colar(img *image.NRGBA, pix []byte, tam, ox, oy int, fundo color.NRGBA) {
	for y := 0; y < tam; y++ {
		for x := 0; x < tam; x++ {
			i := (y*tam + x) * 4
			a := float64(pix[i+3]) / 255
			mix := func(c, f uint8) uint8 { return uint8(math.Round(float64(c)*a + float64(f)*(1-a))) }
			img.SetNRGBA(ox+x, oy+y, color.NRGBA{mix(pix[i+2], fundo.R), mix(pix[i+1], fundo.G), mix(pix[i], fundo.B), 0xFF})
		}
	}
}

// recortar desenha o ícone src (BGRA, opaco, tamSrc) em tamOut x tamOut, em (ox,
// oy), só dentro da máscara (coordenadas de 0 a 1), sobre o fundo. Cada pixel de
// saída é a média de 8 x 8 amostras: a redução de 512 para 144 e a borda da
// máscara saem suaves, como no celular. Com zona, desenha por cima o círculo de
// raio 40% (2 px de traço).
func recortar(img *image.NRGBA, src []byte, tamSrc, tamOut, ox, oy int, mascara func(u, v float64) bool, fundo color.NRGBA, zona *color.NRGBA) {
	const sub = 8
	escala := float64(tamSrc) / float64(tamOut)
	raioZona := zonaSeguraDoMaskable * float64(tamOut)
	meio := float64(tamOut) / 2
	for y := 0; y < tamOut; y++ {
		for x := 0; x < tamOut; x++ {
			var soma [3]float64
			dentro, naZona := 0, 0
			for sy := 0; sy < sub; sy++ {
				py := float64(y) + (float64(sy)+0.5)/sub
				for sx := 0; sx < sub; sx++ {
					px := float64(x) + (float64(sx)+0.5)/sub
					if zona != nil && math.Abs(math.Hypot(px-meio, py-meio)-raioZona) <= 1 {
						naZona++
					}
					if !mascara(px/float64(tamOut), py/float64(tamOut)) {
						continue
					}
					dentro++
					sxSrc := min(tamSrc-1, int(px*escala))
					sySrc := min(tamSrc-1, int(py*escala))
					i := (sySrc*tamSrc + sxSrc) * 4
					soma[0] += float64(src[i+2])
					soma[1] += float64(src[i+1])
					soma[2] += float64(src[i])
				}
			}
			n := float64(sub * sub)
			cobre := float64(dentro) / n
			var c [3]float64
			f := [3]float64{float64(fundo.R), float64(fundo.G), float64(fundo.B)}
			for k := 0; k < 3; k++ {
				media := 0.0
				if dentro > 0 {
					media = soma[k] / float64(dentro)
				}
				c[k] = media*cobre + f[k]*(1-cobre)
			}
			if zona != nil && naZona > 0 {
				a := float64(naZona) / n
				z := [3]float64{float64(zona.R), float64(zona.G), float64(zona.B)}
				for k := 0; k < 3; k++ {
					c[k] = z[k]*a + c[k]*(1-a)
				}
			}
			img.SetNRGBA(ox+x, oy+y, color.NRGBA{uint8(math.Round(c[0])), uint8(math.Round(c[1])), uint8(math.Round(c[2])), 0xFF})
		}
	}
}

// As máscaras dos launchers, em coordenadas de 0 a 1.

func circulo(u, v float64) bool {
	return math.Hypot(u-0.5, v-0.5) <= 0.5
}

// squircle: a superelipse |x|^4 + |y|^4 <= 1, a forma de vários launchers.
func squircle(u, v float64) bool {
	x, y := math.Abs(2*u-1), math.Abs(2*v-1)
	return x*x*x*x+y*y*y*y <= 1
}

// quadradoArredondado: cantos de raio 20% do lado.
func quadradoArredondado(u, v float64) bool {
	const r = 0.2
	dx := math.Max(math.Abs(u-0.5)-(0.5-r), 0)
	dy := math.Max(math.Abs(v-0.5)-(0.5-r), 0)
	return math.Hypot(dx, dy) <= r
}
