package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/tray"
)

// Nenhum teste aqui escreve fora de t.TempDir(), abre janela, toca o registro ou
// executa programa. Só desenham pixels, codificam bytes e os leem de volta.

// pixel devolve B, G, R, A do pixel (x, y).
func pixel(pix []byte, tam, x, y int) [4]byte {
	i := (y*tam + x) * 4
	return [4]byte{pix[i], pix[i+1], pix[i+2], pix[i+3]}
}

// As cores abaixo são escritas de novo e à mão (B, G, R, A), e não lidas das
// variáveis do pacote da bandeja: se alguém trocar um token lá, o ícone do
// programa tem de acusar.
var (
	verde    = [4]byte{0x76, 0xE6, 0x00, 0xFF} // #00E676, systemGreen
	offWhite = [4]byte{0xF5, 0xF7, 0xF5, 0xFF} // #F5F7F5, interfaceWhite
	preto    = [4]byte{0x09, 0x0A, 0x07, 0xFF} // #070A09, voidBlack
)

func TestPixelsDoIcone_LadrilhoPretoComOPicoPorCima(t *testing.T) {
	for _, tam := range tamanhosDoIcone {
		pix := pixelsDoIcone(tam)
		if len(pix) != tam*tam*4 {
			t.Fatalf("%d px: %d bytes, quero %d", tam, len(pix), tam*tam*4)
		}
		// O canto do ladrilho é arredondado: o pixel (0, 0) é transparente.
		if got := pixel(pix, tam, 0, 0); got[3] != 0 {
			t.Errorf("%d px: o canto (0, 0) = %v, quero transparente: o ladrilho é arredondado", tam, got)
		}
		// A barra do meio, que fura a moldura: off-white, opaca.
		if got := pixel(pix, tam, tam/2, tam/2); got != offWhite {
			t.Errorf("%d px: o centro = %v, quero a barra off-white %v", tam, got, offWhite)
		}
		// Entre os braços dos colchetes, na borda do ladrilho: o preto do ladrilho.
		if got := pixel(pix, tam, 1, tam/2); got != preto {
			t.Errorf("%d px: (1, meio) = %v, quero o preto do ladrilho %v", tam, got, preto)
		}
		// Os colchetes são verdes.
		verdes := 0
		for y := 0; y < tam; y++ {
			for x := 0; x < tam; x++ {
				if pixel(pix, tam, x, y) == verde {
					verdes++
				}
			}
		}
		if verdes == 0 {
			t.Errorf("%d px: nenhum pixel dos colchetes em verde %v", tam, verde)
		}
	}
}

// O desenho é o da bandeja, e não uma cópia: onde o desenho da bandeja é opaco, o
// ícone do programa tem exatamente o mesmo pixel.
func TestPixelsDoIcone_ReaproveitaODesenhoDaBandeja(t *testing.T) {
	for _, tam := range tamanhosDoIcone {
		marca := tray.PixelsDoIcone(tam, tray.TemaEscuro, false)
		pix := pixelsDoIcone(tam)
		opacos := 0
		for i := 0; i < len(marca); i += 4 {
			if marca[i+3] != 255 {
				continue
			}
			opacos++
			if !bytes.Equal(pix[i:i+4], marca[i:i+4]) {
				t.Fatalf("%d px: o pixel %d do ícone é %v e o da bandeja, opaco, é %v", tam, i/4, pix[i:i+4], marca[i:i+4])
			}
		}
		if opacos == 0 {
			t.Errorf("%d px: o desenho da bandeja não tem nenhum pixel opaco", tam)
		}
	}
}

func TestICO_Estrutura(t *testing.T) {
	imgs, err := imagensDoIcone()
	if err != nil {
		t.Fatal(err)
	}
	camadas, entradas, err := lerICO(montarICO(imgs))
	if err != nil {
		t.Fatalf("lerICO: %v", err)
	}
	if len(camadas) != len(tamanhosDoIcone) {
		t.Fatalf("%d camadas, quero %d (%v)", len(camadas), len(tamanhosDoIcone), tamanhosDoIcone)
	}
	for i, tam := range tamanhosDoIcone {
		c, e := camadas[i], entradas[i]
		if c.Tam != tam {
			t.Errorf("camada %d: %d px, quero %d", i, c.Tam, tam)
		}
		// A largura e a altura da entrada: o tamanho, e 0 quer dizer 256.
		quer := byte(tam)
		if tam == 256 {
			quer = 0
		}
		if e.largura != quer || e.altura != quer {
			t.Errorf("%d px: a entrada diz %dx%d, quero %d", tam, e.largura, e.altura, quer)
		}
		if e.planos != 1 || e.bits != 32 || e.cores != 0 || e.reservado != 0 {
			t.Errorf("%d px: planos %d, bits %d, cores %d, reservado %d; quero 1, 32, 0, 0", tam, e.planos, e.bits, e.cores, e.reservado)
		}
		// Só a camada de 256 px é PNG.
		if c.PNG != (tam == 256) {
			t.Errorf("%d px: PNG = %v, quero %v", tam, c.PNG, tam == 256)
		}
		// O que o arquivo guarda é o desenho, pixel a pixel.
		if !bytes.Equal(c.Pix, pixelsDoIcone(tam)) {
			t.Errorf("%d px: os pixels lidos do .ico não são o desenho", tam)
		}
	}
}

func TestSyso_Estrutura(t *testing.T) {
	imgs, err := imagensDoIcone()
	if err != nil {
		t.Fatal(err)
	}
	syso, err := montarSyso(imgs)
	if err != nil {
		t.Fatal(err)
	}
	camadas, err := camadasDoSyso(syso)
	if err != nil {
		t.Fatalf("camadasDoSyso: %v", err)
	}
	if len(camadas) != len(tamanhosDoIcone) {
		t.Fatalf("%d camadas no grupo, quero %d", len(camadas), len(tamanhosDoIcone))
	}
	for i, tam := range tamanhosDoIcone {
		if camadas[i].Tam != tam {
			t.Errorf("grupo, entrada %d: %d px, quero %d", i, camadas[i].Tam, tam)
		}
		// Os bytes do recurso são os da camada do .ico, e os pixels, o desenho.
		if !bytes.Equal(camadas[i].Dados, imgs[i].Dados) {
			t.Errorf("%d px: o RT_ICON não tem os mesmos bytes da camada do .ico", tam)
		}
		if !bytes.Equal(camadas[i].Pix, pixelsDoIcone(tam)) {
			t.Errorf("%d px: os pixels do recurso não são o desenho", tam)
		}
	}
}

// O arquivo é reproduzível: sem data nem nada que mude de uma execução para a
// outra, dois .syso do mesmo desenho são iguais byte a byte.
func TestSyso_Reproduzivel(t *testing.T) {
	imgs, err := imagensDoIcone()
	if err != nil {
		t.Fatal(err)
	}
	a, err := montarSyso(imgs)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := montarSyso(imgs)
	if !bytes.Equal(a, b) {
		t.Error("dois .syso do mesmo desenho diferem")
	}
}

func TestMontarSyso_SemCamadasEErro(t *testing.T) {
	if _, err := montarSyso(nil); err == nil {
		t.Error("montarSyso sem camadas = nil, quero erro")
	}
}

func TestGerar_EscreveOsArquivos(t *testing.T) {
	pasta := t.TempDir()
	ico, syso := filepath.Join(pasta, "a.ico"), filepath.Join(pasta, "b.syso")
	pwa := filepath.Join(pasta, "pwa")
	if err := os.Mkdir(pwa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gerar(ico, syso, pwa); err != nil {
		t.Fatal(err)
	}
	// Os ícones do app: um PNG por entrada, no lado dela, com o desenho dela.
	for _, ic := range iconesDoApp {
		b, err := os.ReadFile(filepath.Join(pwa, ic.Nome))
		if err != nil {
			t.Fatalf("%s não foi escrito: %v", ic.Nome, err)
		}
		c, err := decodificarCamada(b)
		if err != nil || !c.PNG || c.Tam != ic.Tam || !bytes.Equal(c.Pix, ic.pixels()) {
			t.Errorf("%s: PNG %v, %d px, erro %v; quero o PNG de %d px do desenho", ic.Nome, c.PNG, c.Tam, err, ic.Tam)
		}
	}
	if entradas, _ := os.ReadDir(pwa); len(entradas) != len(iconesDoApp) {
		t.Errorf("a pasta dos ícones do app tem %d arquivos, quero %d", len(entradas), len(iconesDoApp))
	}
	bi, err := os.ReadFile(ico)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := lerICO(bi); err != nil {
		t.Errorf("o .ico escrito não se lê: %v", err)
	}
	bs, err := os.ReadFile(syso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := camadasDoSyso(bs); err != nil {
		t.Errorf("o .syso escrito não se lê: %v", err)
	}
}

// Rodar de fora da raiz do repositório (as pastas de destino não existem) é erro,
// e o aviso diz o que fazer. Nada é criado.
func TestGerar_PastaInexistenteDizParaRodarDaRaiz(t *testing.T) {
	pasta := t.TempDir()
	err := gerar(filepath.Join(pasta, "nao", "existe", "a.ico"), filepath.Join(pasta, "b.syso"), pasta)
	if err == nil || !strings.Contains(err.Error(), "raiz do repositório") {
		t.Errorf("err = %v, quero um erro que mande rodar da raiz do repositório", err)
	}
	if entradas, _ := os.ReadDir(pasta); len(entradas) != 0 {
		t.Errorf("criou %d arquivo(s) numa pasta inexistente", len(entradas))
	}
}
