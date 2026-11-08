package overlay

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestFaseDoCampoNoFim: só a inserção que falhou vira a mensagem do campo
// protegido, e só com a marca; nenhuma outra fase muda.
func TestFaseDoCampoNoFim(t *testing.T) {
	for _, p := range fasesDoOverlay() {
		if got := FaseDoCampoNoFim(p, false); got != p {
			t.Errorf("FaseDoCampoNoFim(%s, false) = %v, quero %s", nomeDaFase(p), got, nomeDaFase(p))
		}
		quero := p
		if p == dictation.OverlayInsertFailed {
			quero = FaseCampoProtegidoNoHistorico
		}
		if got := FaseDoCampoNoFim(p, true); got != quero {
			t.Errorf("FaseDoCampoNoFim(%s, true) = %v, quero %s", nomeDaFase(p), got, nomeDaFase(quero))
		}
	}
}

// TestFaseCampoProtegidoNoHistorico_ComoATardia: diz onde está o texto, sem
// "tente novamente", aparece, some sozinha, e tem o ponto cinza.
func TestFaseCampoProtegidoNoHistorico_ComoATardia(t *testing.T) {
	l := Label(FaseCampoProtegidoNoHistorico)
	if !strings.Contains(l, "histórico") || strings.Contains(strings.ToLower(l), "tente") {
		t.Fatalf("mensagem %q: quero dizer que está no histórico, sem mandar tentar de novo", l)
	}
	if !Visible(FaseCampoProtegidoNoHistorico) || !isTerminalPhase(FaseCampoProtegidoNoHistorico) {
		t.Fatal("quero visível e terminal (some sozinha)")
	}
	if cor, ok := sinalPara(FaseCampoProtegidoNoHistorico); !ok || cor != corCinza {
		t.Fatalf("ponto %v (%v), quero cinza", cor, ok)
	}
}
