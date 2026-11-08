package overlay

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// O cadeado da gravação solta (ADR-025, D3; opção A, escolhida pelo Mauro em
// 07-nov-2026 olhando as três em PNG).

// TestDimensoes_SoAGravacaoSoltaTemCadeado: a pílula de gravação cresce para o
// cadeado; o processamento volta à compacta, e nenhuma mensagem muda.
func TestDimensoes_SoAGravacaoSoltaTemCadeado(t *testing.T) {
	solta := dimensoesDoQuadro(FrameState{Phase: dictation.OverlayRecording, Solta: true})
	if solta.W != CompactWidth+LarguraDoCadeado || solta.H != CompactHeight || !solta.Cadeado || !solta.ComOnda {
		t.Fatalf("gravação solta: %+v, quero %dx%d com cadeado e onda", solta, CompactWidth+LarguraDoCadeado, CompactHeight)
	}
	if d := dimensoesDoQuadro(FrameState{Phase: dictation.OverlayRecording}); d.W != CompactWidth || d.Cadeado {
		t.Fatalf("gravação de sempre: %+v, quero a compacta sem cadeado", d)
	}
	if d := dimensoesDoQuadro(FrameState{Phase: dictation.OverlayProcessing, Solta: true}); d.W != CompactWidth || d.Cadeado {
		t.Fatalf("processando com a marca ligada: %+v, quero a compacta sem cadeado", d)
	}
	for _, p := range fasesDoOverlay() {
		if p == dictation.OverlayRecording {
			continue
		}
		if com, sem := dimensoesDoQuadro(FrameState{Phase: p, Solta: true}), dimensoesDoQuadro(FrameState{Phase: p}); com != sem {
			t.Errorf("%s: a marca da gravação solta mudou a pílula (%+v, quero %+v)", nomeDaFase(p), com, sem)
		}
	}
}

// TestSuperficie_CabeAGravacaoSolta: a superfície da janela, alocada uma vez,
// cabe a pílula mais larga de gravação.
func TestSuperficie_CabeAGravacaoSolta(t *testing.T) {
	if w, h := TamanhoDaSuperficie(); w < CompactWidth+LarguraDoCadeado || h < CompactHeight {
		t.Fatalf("superfície %dx%d não cabe a gravação solta (%dx%d)", w, h, CompactWidth+LarguraDoCadeado, CompactHeight)
	}
}

// TestRender_CadeadoAntesDaOnda: na gravação solta, o cadeado fica na ponta
// esquerda, centrado no eixo; entre ele e a primeira barra só há corpo; e a
// onda não entra na ponta redonda da direita. Sem a marca, onde estaria o
// cadeado é corpo.
func TestRender_CadeadoAntesDaOnda(t *testing.T) {
	st := FrameState{Phase: dictation.OverlayRecording, Amps: ampsCheias(1.0), Solta: true}
	c := canvasDeTeste()
	Render(c, st)
	d := dimensoesDoQuadro(st)
	px, py := cantoNoCanvasDeTeste(d)
	cb, cg, cr := corCinza.bgr()
	ehCinza := func(x, y int) bool {
		b, g, r, a := c.At(x, y)
		return a == 255 && b == cb && g == cg && r == cr
	}

	// O corpo do cadeado, inteiro, e a alça em U.
	for y := py + cadeadoY + 3; y < py+cadeadoY+cadeadoH; y++ {
		for x := px + cadeadoX; x < px+cadeadoX+cadeadoW; x++ {
			if !ehCinza(x, y) {
				t.Fatalf("corpo do cadeado: pixel (%d,%d) da pílula não é cinza", x-px, y-py)
			}
		}
	}
	if !ehCinza(px+cadeadoX+3, py+cadeadoY) || ehCinza(px+cadeadoX+3, py+cadeadoY+1) {
		t.Fatal("a alça não é um U: quero o topo cheio e o miolo vazio")
	}
	if cadeadoY >= WaveAxisY || cadeadoY+cadeadoH <= WaveAxisY {
		t.Fatalf("o cadeado (y=%d..%d) não atravessa o eixo da onda (y=%d)", cadeadoY, cadeadoY+cadeadoH-1, WaveAxisY)
	}

	// Entre o cadeado e a primeira barra, nada de barra.
	primeira := px + LarguraDoCadeado + (CompactWidth-(BarCount*BarWidth+(BarCount-1)*BarGap))/2
	for x := px + cadeadoX + cadeadoW; x < primeira; x++ {
		for y := py; y < py+d.H; y++ {
			if ehPixelDeBarra(c, x, y) {
				t.Fatalf("barra em x=%d da pílula, entre o cadeado e a onda", x-px)
			}
		}
	}
	// A onda: a partir da primeira barra, e fora da ponta redonda da direita.
	barras := 0
	for y := 0; y < c.H; y++ {
		for x := primeira; x < c.W; x++ {
			if !ehPixelDeBarra(c, x, y) {
				continue
			}
			barras++
			if x >= px+d.W-CompactRadius {
				t.Fatalf("barra em x=%d da pílula, na ponta redonda da direita", x-px)
			}
		}
	}
	if barras == 0 {
		t.Fatal("a gravação solta não desenhou a onda")
	}

	// Sem a marca: onde estaria o corpo do cadeado é corpo da pílula.
	sem := canvasDeTeste()
	Render(sem, FrameState{Phase: dictation.OverlayRecording, Amps: ampsCheias(1.0)})
	spx, spy := cantoNoCanvasDeTeste(DimensoesPara(dictation.OverlayRecording))
	b, g, r, _ := sem.At(spx+cadeadoX+3, spy+cadeadoY+5)
	if wb, wg, wr := corCorpo.bgr(); b != wb || g != wg || r != wr {
		t.Fatal("a gravação de sempre desenhou algo onde fica o cadeado")
	}
}

// TestOverlay_SetGravacaoSoltaAlargaAPilula: a marca chega ao quadro que a
// janela apresenta, e sai dele quando desliga.
func TestOverlay_SetGravacaoSoltaAlargaAPilula(t *testing.T) {
	j := &janelaDeTeste{}
	o := &Overlay{win: j, geo: geometria{TelaW: 1920, TelaH: 1080}}

	o.SetGravacaoSolta(true)
	redesenhaEm(o, dictation.OverlayRecording)
	if w := j.quadros[len(j.quadros)-1].c.W; w != CompactWidth+LarguraDoCadeado {
		t.Fatalf("gravação solta: quadro de %d px, quero %d", w, CompactWidth+LarguraDoCadeado)
	}
	o.SetGravacaoSolta(false)
	redesenhaEm(o, dictation.OverlayProcessing)
	if w := j.quadros[len(j.quadros)-1].c.W; w != CompactWidth {
		t.Fatalf("depois de parar: quadro de %d px, quero a compacta (%d)", w, CompactWidth)
	}
	// O ditado seguinte, segurando, não herda o cadeado.
	redesenhaEm(o, dictation.OverlayRecording)
	if w := j.quadros[len(j.quadros)-1].c.W; w != CompactWidth {
		t.Fatalf("gravação seguinte: quadro de %d px, quero a compacta (%d), sem cadeado", w, CompactWidth)
	}
}
