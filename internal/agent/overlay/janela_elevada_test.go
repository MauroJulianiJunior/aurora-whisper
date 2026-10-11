package overlay

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// textoJanelaElevada é o texto de docs/ai-spec/08-ui-ux-spec.md, "Blocked
// — janela de administrador" (decisão do Mauro de 29-set-2026, item 4).
const textoJanelaElevada = "O Windows não deixa ditar em janela de administrador."

// TestLabel_JanelaElevada: o texto da spec, verbatim — e inteiro na
// pílula, em duas linhas. É a mensagem mais longa de todas.
func TestLabel_JanelaElevada(t *testing.T) {
	label := Label(FaseJanelaElevada)
	if label != textoJanelaElevada {
		t.Fatalf("Label(FaseJanelaElevada) = %q, quero %q (08-ui-ux-spec.md)", label, textoJanelaElevada)
	}
	m := ComporMensagem(FaseJanelaElevada)
	if len(m.Linhas) != 2 {
		t.Fatalf("%q saiu em %d linha(s): %q; quero duas", label, len(m.Linhas), m.Linhas)
	}
	for i, l := range m.Linhas {
		if strings.Contains(l, "…") {
			t.Errorf("linha %d %q saiu truncada", i, l)
		}
	}
	if got := strings.Join(m.Linhas, " "); got != label {
		t.Errorf("a quebra perdeu texto: %q", got)
	}
}

// TestFaseJanelaElevada_ComoUmBloqueio: aparece, some sozinha como qualquer
// desfecho, e é uma pílula de mensagem com o ponto vermelho do bloqueio e
// o mesmo raio. O tamanho é o do texto dela, como o de toda mensagem.
func TestFaseJanelaElevada_ComoUmBloqueio(t *testing.T) {
	if !Visible(FaseJanelaElevada) {
		t.Error("FaseJanelaElevada invisível")
	}
	if !isTerminalPhase(FaseJanelaElevada) {
		t.Error("FaseJanelaElevada não é terminal: a pílula ficaria na tela para sempre")
	}
	elevada, bloqueio := DimensoesPara(FaseJanelaElevada), DimensoesPara(dictation.OverlayBlocked)
	if elevada.ComOnda || elevada.Radius != bloqueio.Radius {
		t.Errorf("DimensoesPara(FaseJanelaElevada) = %+v; quero pílula de mensagem com o raio do bloqueio %+v",
			elevada, bloqueio)
	}
	corE, okE := sinalPara(FaseJanelaElevada)
	corB, okB := sinalPara(dictation.OverlayBlocked)
	if !okE || corE != corB || !okB {
		t.Errorf("sinalPara(FaseJanelaElevada) = %v, %v; quero o do bloqueio, %v, %v", corE, okE, corB, okB)
	}
}

// TestFaseJanelaElevada_ForaDoNucleo: a fase é do overlay, não do núcleo
// (ADR-011, "o núcleo não muda"). Se o núcleo um dia ganhar uma fase com
// este valor, ela seria confundida com esta — e falha aqui.
func TestFaseJanelaElevada_ForaDoNucleo(t *testing.T) {
	if s := FaseJanelaElevada.String(); s != "unknown" {
		t.Fatalf("o núcleo conhece o valor %d como %q", FaseJanelaElevada, s)
	}
	for _, p := range todasAsFases() {
		if p == FaseJanelaElevada {
			t.Fatalf("FaseJanelaElevada colide com a fase do núcleo %s", p)
		}
	}
}

// TestFaseDoBloqueio: só um bloqueio com a marca vira a mensagem de janela
// de administrador. A marca nunca transforma outra fase em bloqueio, nem
// some com um desfecho.
func TestFaseDoBloqueio(t *testing.T) {
	for _, p := range todasAsFases() {
		if got := FaseDoBloqueio(p, false); got != p {
			t.Errorf("FaseDoBloqueio(%s, false) = %v, quero %s", p, got, p)
		}
		quero := p
		if p == dictation.OverlayBlocked {
			quero = FaseJanelaElevada
		}
		if got := FaseDoBloqueio(p, true); got != quero {
			t.Errorf("FaseDoBloqueio(%s, true) = %v, quero %v", p, got, quero)
		}
	}
}
