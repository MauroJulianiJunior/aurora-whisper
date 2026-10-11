package overlay

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// A guarda contra o laço de repetição do ASR tirou cópias do texto
// inserido; a pílula diz isso, para a pessoa conferir (incidente de
// 01-out-2026, docs/defeito-ditado-longo.md).

func TestFaseRepeticaoRemovida_Mensagem(t *testing.T) {
	if got := Label(FaseRepeticaoRemovida); got != "Removi uma repetição. Confira o texto." {
		t.Errorf("Label = %q", got)
	}
	if !Visible(FaseRepeticaoRemovida) || !isTerminalPhase(FaseRepeticaoRemovida) {
		t.Error("tem de aparecer e sumir sozinha, como o sucesso")
	}
	// Verde: o texto entrou no campo.
	if cor, ok := sinalPara(FaseRepeticaoRemovida); !ok || cor != corVerde {
		t.Errorf("sinal = %v, %v; quero o verde do sucesso", cor, ok)
	}
}

// TestFaseDoSucesso: só um sucesso com a marca vira a mensagem da
// repetição. A marca nunca transforma erro, bloqueio ou área de
// transferência em sucesso.
func TestFaseDoSucesso(t *testing.T) {
	for _, p := range fasesDoOverlay() {
		if got := FaseDoSucesso(p, false); got != p {
			t.Errorf("FaseDoSucesso(%s, false) = %v, quero %s", nomeDaFase(p), got, nomeDaFase(p))
		}
		quero := p
		if p == dictation.OverlaySuccess {
			quero = FaseRepeticaoRemovida
		}
		if got := FaseDoSucesso(p, true); got != quero {
			t.Errorf("FaseDoSucesso(%s, true) = %v, quero %v", nomeDaFase(p), got, quero)
		}
	}
}
