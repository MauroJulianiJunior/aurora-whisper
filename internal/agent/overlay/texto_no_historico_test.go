package overlay

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// A transcrição que chega depois do prazo vai para o histórico, e a pílula
// diz isso (incidente de 01-out-2026, docs/defeito-ditado-longo.md).

func TestFaseTextoNoHistorico_Mensagem(t *testing.T) {
	const quero = "O texto demorou e ficou no histórico."
	if got := Label(FaseTextoNoHistorico); got != quero {
		t.Fatalf("Label = %q, quero %q", got, quero)
	}
	if strings.Contains(strings.ToLower(Label(FaseTextoNoHistorico)), "tente") {
		t.Error("a mensagem manda tentar de novo: o texto já está salvo, ditar de novo é retrabalho")
	}
	m := ComporMensagem(FaseTextoNoHistorico)
	if got := strings.Join(m.Linhas, "|"); got != "O texto demorou e|ficou no histórico." {
		t.Errorf("linhas = %q", got)
	}
}

// TestFaseTextoNoHistorico_ComoAAreaDeTransferencia: aparece, some sozinha,
// e tem o ponto cinza — não é erro (o ditado não se perdeu) e não é sucesso
// (não foi inserido).
func TestFaseTextoNoHistorico_ComoAAreaDeTransferencia(t *testing.T) {
	if !Visible(FaseTextoNoHistorico) {
		t.Error("invisível")
	}
	if !isTerminalPhase(FaseTextoNoHistorico) {
		t.Error("não é terminal: a pílula ficaria na tela para sempre")
	}
	cor, ok := sinalPara(FaseTextoNoHistorico)
	corClip, okClip := sinalPara(dictation.OverlayClipboard)
	if !ok || cor != corClip || !okClip {
		t.Errorf("sinal = %v, %v; quero o cinza da área de transferência, %v", cor, ok, corClip)
	}
	if cor == corVerde || cor == corVermelho {
		t.Errorf("ponto %v: verde diria inserido, vermelho diria perdido", cor)
	}
}

// TestFasesSoDoOverlay_ForaDoNucleo: as fases que só o overlay conhece não
// podem colidir com uma do núcleo, nem entre si.
func TestFasesSoDoOverlay_ForaDoNucleo(t *testing.T) {
	vistas := map[dictation.OverlayPhase]bool{}
	for _, p := range todasAsFases() {
		vistas[p] = true
	}
	for _, p := range fasesSoDoOverlay() {
		if s := p.String(); s != "unknown" {
			t.Errorf("o núcleo conhece o valor %d como %q", p, s)
		}
		if vistas[p] {
			t.Errorf("%s (%d) colide com outra fase", nomeDaFase(p), p)
		}
		vistas[p] = true
	}
}
