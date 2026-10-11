//go:build renderpng

// Este arquivo fica atrás de build tag própria porque não AFIRMA nada:
// ele desenha. O veredito é de quem olha o PNG, não do runner.
//
// Existe porque em 28-set-2026 ele achou, em um minuto, um defeito que a
// suíte inteira não achava: oito caracteres das mensagens de
// OverlayClipboard e OverlayInsertFailed não tinham glifo, e a rede de
// segurança do DrawText os desenhava como espaço em branco. A tela de
// quem usa mostrava "Te to na rea de trans er ncia: tr .". O teste de
// cobertura da fonte passava porque listava as fases à mão e parou nas
// quatro primeiras.
//
// Nenhuma métrica pega isso. Olhar pega.
//
// Achou de novo em 29-set-2026, na mesma família: a mensagem de janela de
// administrador saía "indo s não deixa ditar em anela de administrador."
// — a fase não estava na lista de casos daqui, nem na do teste de
// cobertura, porque é do overlay e não do núcleo. Os dois a percorrem
// desde a troca da fonte de bitmap pela IBM Plex Mono.
//
// Só escreve arquivo: não abre janela, não chama Win32. Cada fase sai
// sobre fundo CLARO, ESCURO e PRETO — a pílula aparece sobre o papel de
// parede de quem usa, seja ele qual for —, em tamanho real (folha-1x) e
// numa folha de contato ampliada (folha-3x), para dar para ver pixel.
//
// Rodar:
//
//	PNGDIR=/algum/lugar go test -tags renderpng -run PilulaEmPNG ./internal/agent/overlay/

package overlay

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// falaSintetica é uma onda plausível: sílabas com vales quase mudos entre
// elas, sob o envelope de uma frase. pico escala o todo.
//
// Referência (ADR-014, medido no microfone real): fala normal tem RMS p50
// de 0,068 e pico de 0,224; a faixa mapeada é -48..-14 dBFS, então acima
// de ~0,2 a barra satura no teto.
func falaSintetica(pico float64) []float64 {
	amps := make([]float64, 130) // 1,3 s em pacotes de 10 ms
	for i := range amps {
		t := float64(i)
		silaba := math.Pow(math.Abs(math.Sin(t*0.42)), 2.2)
		frase := 0.55 + 0.45*math.Sin(t*0.07)
		amps[i] = math.Max(0.0015, pico*silaba*frase)
	}
	return amps
}

func constante(v float64) []float64 {
	amps := make([]float64, 130)
	for i := range amps {
		amps[i] = v
	}
	return amps
}

type fundoPNG struct {
	nome   string
	c1, c2 uint8 // quadriculado, para a transparência da janela aparecer
}

func TestPilulaEmPNG(t *testing.T) {
	dir := os.Getenv("PNGDIR")
	if dir == "" {
		t.Skip("defina PNGDIR com o diretório de saída")
	}
	casos := []struct {
		nome string
		st   FrameState
	}{
		// -67 dBFS: o fundo medido entre palavras. Tem de ler como repouso.
		{"gravando-silencio", FrameState{Phase: dictation.OverlayRecording, Amps: constante(0.00042)}},
		{"gravando-fala-baixa", FrameState{Phase: dictation.OverlayRecording, Amps: falaSintetica(0.05)}},
		{"gravando-fala-normal", FrameState{Phase: dictation.OverlayRecording, Amps: falaSintetica(0.13)}},
		{"gravando-fala-forte", FrameState{Phase: dictation.OverlayRecording, Amps: falaSintetica(0.4)}},
		{"gravando-saturada", FrameState{Phase: dictation.OverlayRecording, Amps: constante(1.0)}},
		{"processando", FrameState{Phase: dictation.OverlayProcessing, Amps: falaSintetica(0.13)}},
		{"inserido", FrameState{Phase: dictation.OverlaySuccess}},
		{"bloqueado", FrameState{Phase: dictation.OverlayBlocked}},
		{"erro", FrameState{Phase: dictation.OverlayError}},
		{"falha-insercao", FrameState{Phase: dictation.OverlayInsertFailed}},
		{"area-de-transferencia", FrameState{Phase: dictation.OverlayClipboard}},
		{"janela-elevada", FrameState{Phase: FaseJanelaElevada}},
		{"texto-no-historico", FrameState{Phase: FaseTextoNoHistorico}},
		{"repeticao-removida", FrameState{Phase: FaseRepeticaoRemovida}},
	}
	fundos := []fundoPNG{
		{"claro", 245, 225},
		{"escuro", 30, 16},
		{"preto", 0, 0}, // o pior caso para o contorno da pílula
	}

	// Cada célula é um pedaço da tela em volta da pílula: do tamanho da
	// maior pílula, com respiro, e a pílula da fase centrada — o centro é o
	// mesmo em toda fase, como na tela (CentroAcimaDaBase). Assim a folha
	// mostra também quanto cada pílula ocupa perto das outras.
	const respiro = 8
	supW, supH := TamanhoDaSuperficie()
	celW, celH := supW+2*respiro, supH+2*respiro

	for _, zoom := range []int{1, 3} {
		folha := image.NewRGBA(image.Rect(0, 0, celW*zoom*len(fundos), celH*zoom*len(casos)))
		for i, c := range casos {
			cv := NewCanvas(celW, celH)
			Render(cv, c.st)
			for j, f := range fundos {
				img := compoe(cv, f, zoom)
				if zoom == 1 {
					salvaPNG(t, filepath.Join(dir, fmt.Sprintf("%s-%02d-%s.png", f.nome, i+1, c.nome)), img)
				}
				x0, y0 := j*celW*zoom, i*celH*zoom
				for y := 0; y < img.Bounds().Dy(); y++ {
					for x := 0; x < img.Bounds().Dx(); x++ {
						folha.Set(x0+x, y0+y, img.At(x, y))
					}
				}
			}
		}
		salvaPNG(t, filepath.Join(dir, fmt.Sprintf("folha-%dx.png", zoom)), folha)
	}

	t.Logf("superficie %dx%d | pilula compacta %dx%d | fonte IBM Plex Mono Medium %d px",
		supW, supH, CompactWidth, CompactHeight, FonteTamanho)
	for _, c := range casos {
		if d := DimensoesPara(c.st.Phase); !d.ComOnda {
			t.Logf("%-22s %3dx%-2d %q", c.nome, d.W, d.H, ComporMensagem(c.st.Phase).Linhas)
		}
	}
}

func fundoEm(f fundoPNG, x, y int) color.RGBA {
	v := f.c1
	if (x/8+y/8)%2 == 0 {
		v = f.c2
	}
	return color.RGBA{R: v, G: v, B: v, A: 255}
}

// compoe põe o canvas (BGRA pré-multiplicado, como o DIB) sobre o fundo,
// ampliando cada pixel em zoom x zoom sem interpolar.
func compoe(cv *Canvas, f fundoPNG, zoom int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, cv.W*zoom, cv.H*zoom))
	for y := 0; y < cv.H*zoom; y++ {
		for x := 0; x < cv.W*zoom; x++ {
			b, g, r, a := cv.At(x/zoom, y/zoom)
			bg := fundoEm(f, x/zoom, y/zoom).R
			resto := 1 - float64(a)/255
			img.Set(x, y, color.RGBA{
				R: uint8(math.Round(float64(r) + float64(bg)*resto)),
				G: uint8(math.Round(float64(g) + float64(bg)*resto)),
				B: uint8(math.Round(float64(b) + float64(bg)*resto)),
				A: 255,
			})
		}
	}
	return img
}

func salvaPNG(t *testing.T, caminho string, img image.Image) {
	t.Helper()
	f, err := os.Create(caminho)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
