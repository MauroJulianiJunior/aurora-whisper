package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/tray"
)

// Os ícones do app instalável (ADR-019, F5). Nenhum teste aqui escreve fora de
// t.TempDir().

// O manifest (web/pwa/manifest.webmanifest) aponta estes três, com estes lados
// e estes "purpose"; web/bundle.test.ts confere o outro lado (o manifest e a
// largura de cada PNG).
func TestIconesDoApp_SaoOsDoManifest(t *testing.T) {
	quer := []iconeDoApp{
		{"icone-192.png", 192, false},
		{"icone-512.png", 512, false},
		{"icone-maskable-512.png", 512, true},
	}
	if len(iconesDoApp) != len(quer) {
		t.Fatalf("%d ícones, quero %d", len(iconesDoApp), len(quer))
	}
	for i := range quer {
		if iconesDoApp[i] != quer[i] {
			t.Errorf("ícone %d = %+v, quero %+v", i, iconesDoApp[i], quer[i])
		}
	}
}

// Os "any" são o ícone do programa (o do .exe e da aba), só que maiores: o
// ladrilho arredondado com o canto transparente.
func TestIconesDoApp_AnySaoOIconeDoPrograma(t *testing.T) {
	for _, ic := range iconesDoApp {
		if ic.Maskable {
			continue
		}
		pix := ic.pixels()
		if !bytes.Equal(pix, pixelsDoIcone(ic.Tam)) {
			t.Errorf("%s não é o pixelsDoIcone(%d)", ic.Nome, ic.Tam)
		}
		if got := pixel(pix, ic.Tam, 0, 0); got[3] != 0 {
			t.Errorf("%s: o canto (0, 0) = %v, quero transparente", ic.Nome, got)
		}
	}
}

// O maskable é recortado pelo launcher no formato dele: nenhum pixel pode ser
// transparente, senão o recorte mostra um canto vazio.
func TestMaskable_TodoOpacoNaCorDoLadrilho(t *testing.T) {
	const tam = 512
	pix := pixelsDoMaskable(tam)
	if len(pix) != tam*tam*4 {
		t.Fatalf("%d bytes, quero %d", len(pix), tam*tam*4)
	}
	for i := 3; i < len(pix); i += 4 {
		if pix[i] != 255 {
			t.Fatalf("o pixel (%d, %d) tem alfa %d: o maskable é todo opaco", (i/4)%tam, (i/4)/tam, pix[i])
		}
	}
	for _, c := range [][2]int{{0, 0}, {tam - 1, 0}, {0, tam - 1}, {tam - 1, tam - 1}, {tam / 2, 2}} {
		if got := pixel(pix, tam, c[0], c[1]); got != preto {
			t.Errorf("(%d, %d) = %v, quero o preto do ladrilho %v", c[0], c[1], got, preto)
		}
	}
}

// Todo pixel da marca (o que não é o fundo) fica dentro do círculo de raio 40%,
// a zona que todo launcher preserva. Mede o canto do pixel mais longe do centro,
// e não o meio dele: nem um pedaço de pixel sai.
func TestMaskable_AMarcaCabeNaZonaSegura(t *testing.T) {
	const tam = 512
	pix := pixelsDoMaskable(tam)
	raio := zonaSeguraDoMaskable * tam
	meio := float64(tam) / 2
	daMarca, maisLonge := 0, 0.0
	for y := 0; y < tam; y++ {
		for x := 0; x < tam; x++ {
			if pixel(pix, tam, x, y) == preto {
				continue
			}
			daMarca++
			dx := math.Max(math.Abs(float64(x)-meio), math.Abs(float64(x+1)-meio))
			dy := math.Max(math.Abs(float64(y)-meio), math.Abs(float64(y+1)-meio))
			if d := math.Hypot(dx, dy); d > maisLonge {
				maisLonge = d
			}
		}
	}
	if daMarca == 0 {
		t.Fatal("o maskable não tem marca nenhuma")
	}
	if maisLonge > raio {
		t.Errorf("um pixel da marca vai a %.1f px do centro; a zona segura tem %.1f", maisLonge, raio)
	}
}

// O símbolo do maskable é o desenho da bandeja em 384 px, centrado: onde o
// desenho é opaco, o pixel é exatamente o dele.
func TestMaskable_ODesenhoDaBandejaEm384Centrado(t *testing.T) {
	const tam, lado = 512, 384
	if ladoDaMarcaNoMaskable != lado {
		t.Fatalf("o símbolo do maskable tem %d px, quero %d (a conta da zona segura é com ele)", ladoDaMarcaNoMaskable, lado)
	}
	marca := tray.PixelsDoIcone(lado, tray.TemaEscuro, false)
	pix := pixelsDoMaskable(tam)
	desloc := (tam - lado) / 2
	opacos := 0
	for y := 0; y < lado; y++ {
		for x := 0; x < lado; x++ {
			j := (y*lado + x) * 4
			if marca[j+3] != 255 {
				continue
			}
			opacos++
			if got := pixel(pix, tam, x+desloc, y+desloc); !bytes.Equal(got[:], marca[j:j+4]) {
				t.Fatalf("(%d, %d) = %v, quero o pixel da bandeja %v", x+desloc, y+desloc, got, marca[j:j+4])
			}
		}
	}
	if opacos == 0 {
		t.Fatal("o desenho da bandeja não tem pixel opaco")
	}
	// Os colchetes (verdes) e a barra (off-white) aparecem no maskable.
	verdes, brancos := 0, 0
	for y := 0; y < tam; y++ {
		for x := 0; x < tam; x++ {
			switch pixel(pix, tam, x, y) {
			case verde:
				verdes++
			case offWhite:
				brancos++
			}
		}
	}
	if verdes == 0 || brancos == 0 {
		t.Errorf("%d pixels verdes e %d off-white: falta parte do símbolo", verdes, brancos)
	}
}

// Sem a pasta dos ícones do app, o erro manda rodar da raiz do repositório.
func TestGerarIconesDoApp_PastaInexistente(t *testing.T) {
	pasta := filepath.Join(t.TempDir(), "nao-existe")
	err := gerarIconesDoApp(pasta)
	if err == nil || !strings.Contains(err.Error(), "raiz do repositório") {
		t.Errorf("err = %v, quero um erro que mande rodar da raiz do repositório", err)
	}
	if _, errStat := os.Stat(pasta); errStat == nil {
		t.Error("criou a pasta")
	}
}
