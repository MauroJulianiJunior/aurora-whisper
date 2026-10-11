//go:build renderpng

package tray

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestIconeEmPNG desenha o ícone da bandeja nos quatro tamanhos que o
// Windows usa, ativo e pausado, sobre a barra de tarefas CLARA e a
// ESCURA — cada uma com a variante que o Agent escolhe para ela (Tema).
//
// Cada PNG tem duas faixas: em cima, os quatro tamanhos em tamanho real,
// que é como alguém os vê na bandeja; embaixo, os mesmos ampliados 8x,
// para conferir pixel a pixel as bordas retas sem meia-intensidade.
//
// Não afirma nada: o veredito é de quem olha. Mesmo motivo do
// render_png_test.go do overlay. Não abre janela nem põe ícone na
// bandeja: só escreve arquivos.
//
//	PNGDIR=/algum/lugar go test -tags renderpng -run IconeEmPNG ./internal/agent/tray/
func TestIconeEmPNG(t *testing.T) {
	dir := os.Getenv("PNGDIR")
	if dir == "" {
		t.Skip("defina PNGDIR")
	}
	const zoom = 8
	tams := []int{16, 20, 24, 32}
	for _, fundo := range []struct {
		nome string
		tema Tema
		v    uint8 // a barra de tarefas do Windows 11: ~#202020 e ~#F3F3F3
	}{{"escuro", TemaEscuro, 0x20}, {"claro", TemaClaro, 0xF3}} {
		for _, pausado := range []bool{false, true} {
			larg := 16
			for _, tam := range tams {
				larg += tam*zoom + 16
			}
			faixaReal := 32 + 16
			img := image.NewRGBA(image.Rect(0, 0, larg, faixaReal+32*zoom+16))
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < larg; x++ {
					img.Set(x, y, color.RGBA{fundo.v, fundo.v, fundo.v, 255})
				}
			}
			desenha := func(pix []byte, tam, ox, oy, escala int) {
				for y := 0; y < tam; y++ {
					for x := 0; x < tam; x++ {
						i := (y*tam + x) * 4
						a := float64(pix[i+3]) / 255
						mix := func(c uint8) uint8 { return uint8(float64(c)*a + float64(fundo.v)*(1-a) + 0.5) }
						cor := color.RGBA{mix(pix[i+2]), mix(pix[i+1]), mix(pix[i]), 255}
						for dy := 0; dy < escala; dy++ {
							for dx := 0; dx < escala; dx++ {
								img.Set(ox+x*escala+dx, oy+y*escala+dy, cor)
							}
						}
					}
				}
			}
			oxReal, oxZoom := 16, 16
			for _, tam := range tams {
				pix := PixelsDoIcone(tam, fundo.tema, pausado)
				desenha(pix, tam, oxReal, 8+(32-tam)/2, 1)
				desenha(pix, tam, oxZoom, faixaReal, zoom)
				oxReal += tam + 24
				oxZoom += tam*zoom + 16
			}
			estado := "ativo"
			if pausado {
				estado = "pausado"
			}
			nome := filepath.Join(dir, fmt.Sprintf("icone-%s-%s.png", estado, fundo.nome))
			f, err := os.Create(nome)
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
