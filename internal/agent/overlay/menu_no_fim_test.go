package overlay

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestFaseDoMenuNoFim: só a inserção que falhou vira a mensagem do menu, e
// só com a marca; nenhuma outra fase muda.
func TestFaseDoMenuNoFim(t *testing.T) {
	for _, p := range fasesDoOverlay() {
		if got := FaseDoMenuNoFim(p, false); got != p {
			t.Errorf("FaseDoMenuNoFim(%s, false) = %v, quero %s", nomeDaFase(p), got, nomeDaFase(p))
		}
		quero := p
		if p == dictation.OverlayInsertFailed {
			quero = FaseMenuNoHistorico
		}
		if got := FaseDoMenuNoFim(p, true); got != quero {
			t.Errorf("FaseDoMenuNoFim(%s, true) = %v, quero %s", nomeDaFase(p), got, nomeDaFase(quero))
		}
	}
}

// TestFaseMenuNoHistorico_ComoADoCampo: diz onde está o texto, sem "tente
// novamente", some sozinha, e tem o ponto cinza.
func TestFaseMenuNoHistorico_ComoADoCampo(t *testing.T) {
	l := Label(FaseMenuNoHistorico)
	if !strings.Contains(l, "Menu") || !strings.Contains(l, "histórico") || strings.Contains(strings.ToLower(l), "tente") {
		t.Fatalf("mensagem %q: quero dizer que havia um menu e que o texto está no histórico", l)
	}
	if !Visible(FaseMenuNoHistorico) || !isTerminalPhase(FaseMenuNoHistorico) {
		t.Fatal("quero visível e terminal (some sozinha)")
	}
	if cor, ok := sinalPara(FaseMenuNoHistorico); !ok || cor != corCinza {
		t.Fatalf("ponto %v (%v), quero cinza", cor, ok)
	}
}
