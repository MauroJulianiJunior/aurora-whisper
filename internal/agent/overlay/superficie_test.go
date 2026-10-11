package overlay

import (
	"bytes"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// apresentado devolve o retângulo w x h do canto da superfície — o que o
// UpdateLayeredWindow mostra na tela (psize = w x h, pptSrc = 0,0) — como
// um Canvas, para comparar com o quadro.
func apresentado(pix []byte, largura, w, h int) *Canvas {
	c := NewCanvas(w, h)
	for y := 0; y < h; y++ {
		copy(c.Pix[y*w*4:(y+1)*w*4], pix[y*largura*4:y*largura*4+w*4])
	}
	return c
}

// TestCopiaParaSuperficie_QuadroMenorVaiParaOCanto: cada linha do quadro
// vai para o começo de uma linha da superfície, com o passo da superfície
// — não o do quadro. Com o passo errado, a pílula sai inclinada.
func TestCopiaParaSuperficie_QuadroMenorVaiParaOCanto(t *testing.T) {
	const largura, altura = 7, 5
	pix := make([]byte, largura*altura*4)
	c := NewCanvas(3, 2)
	for i := range c.Pix {
		c.Pix[i] = byte(i + 1)
	}
	if err := copiaParaSuperficie(pix, largura, altura, c); err != nil {
		t.Fatal(err)
	}
	if got := apresentado(pix, largura, c.W, c.H); !bytes.Equal(got.Pix, c.Pix) {
		t.Fatalf("o canto da superfície não é o quadro:\n got %v\nwant %v", got.Pix, c.Pix)
	}
	// Fora do canto, nada foi escrito.
	for y := 0; y < altura; y++ {
		for x := 0; x < largura; x++ {
			if x < c.W && y < c.H {
				continue
			}
			if o := (y*largura + x) * 4; !bytes.Equal(pix[o:o+4], []byte{0, 0, 0, 0}) {
				t.Fatalf("(%d,%d), fora do quadro, foi escrito: %v", x, y, pix[o:o+4])
			}
		}
	}
}

// TestCopiaParaSuperficie_SemRastroDaPilulaAnterior: a superfície guarda o
// que uma pílula maior deixou, mas o retângulo apresentado depois de um
// quadro menor é EXATAMENTE o quadro menor — nenhum pixel da mensagem
// anterior aparece em volta da onda.
func TestCopiaParaSuperficie_SemRastroDaPilulaAnterior(t *testing.T) {
	sw, sh := TamanhoDaSuperficie()
	pix := make([]byte, sw*sh*4)
	for _, p := range []dictation.OverlayPhase{
		FaseJanelaElevada, dictation.OverlayRecording, dictation.OverlayError,
		dictation.OverlaySuccess, dictation.OverlayProcessing,
	} {
		q := Quadro(FrameState{Phase: p, Amps: ampsCheias(0.2)})
		if err := copiaParaSuperficie(pix, sw, sh, q); err != nil {
			t.Fatalf("%s: %v", nomeDaFase(p), err)
		}
		if got := apresentado(pix, sw, q.W, q.H); !bytes.Equal(got.Pix, q.Pix) {
			t.Fatalf("%s: o que a janela apresenta não é o quadro da fase", nomeDaFase(p))
		}
	}
}

// TestCopiaParaSuperficie_RecusaOQueNaoCabe: quadro maior que a superfície,
// ou superfície menor do que diz ser, é erro — nunca escrita fora dela.
func TestCopiaParaSuperficie_RecusaOQueNaoCabe(t *testing.T) {
	pix := make([]byte, 4*4*4)
	if err := copiaParaSuperficie(pix, 4, 4, NewCanvas(5, 1)); err == nil {
		t.Error("quadro mais largo que a superfície foi aceito")
	}
	if err := copiaParaSuperficie(pix, 4, 4, NewCanvas(1, 5)); err == nil {
		t.Error("quadro mais alto que a superfície foi aceito")
	}
	if err := copiaParaSuperficie(pix[:10], 4, 4, NewCanvas(1, 1)); err == nil {
		t.Error("superfície curta foi aceita")
	}
	if err := copiaParaSuperficie(pix, 4, 4, NewCanvas(4, 4)); err != nil {
		t.Errorf("quadro do tamanho exato foi recusado: %v", err)
	}
}

// TestQuadro_TemOTamanhoDaPilula: o quadro que a janela apresenta tem o
// tamanho exato da pílula da fase — é ele que dá o tamanho à janela — e é
// o mesmo desenho que Render faz num canvas maior, só sem a folga.
func TestQuadro_TemOTamanhoDaPilula(t *testing.T) {
	for _, p := range fasesDoOverlay() {
		st := FrameState{Phase: p, Amps: ampsCheias(0.3)}
		q := Quadro(st)
		d := DimensoesPara(p)
		if q.W != d.W || q.H != d.H {
			t.Errorf("%s: quadro %dx%d, pílula %dx%d", nomeDaFase(p), q.W, q.H, d.W, d.H)
			continue
		}
		c := canvasDeTeste()
		Render(c, st)
		px, py := cantoNoCanvasDeTeste(d)
		for y := 0; y < q.H; y++ {
			o := ((py+y)*c.W + px) * 4
			if !bytes.Equal(q.Pix[y*q.W*4:(y+1)*q.W*4], c.Pix[o:o+q.W*4]) {
				t.Fatalf("%s: a linha %d do quadro difere do desenho no canvas dos testes", nomeDaFase(p), y)
			}
		}
	}
}
