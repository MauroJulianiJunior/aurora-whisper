package overlay

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

const textoMicrofoneBloqueado = "O Windows bloqueou o microfone. Veja o painel."

// TestLabel_MicrofoneBloqueado: o texto da spec, verbatim, inteiro na
// pílula e em duas linhas como o da janela de administrador.
func TestLabel_MicrofoneBloqueado(t *testing.T) {
	label := Label(FaseMicrofoneBloqueado)
	if label != textoMicrofoneBloqueado {
		t.Fatalf("Label(FaseMicrofoneBloqueado) = %q, quero %q (08-ui-ux-spec.md)", label, textoMicrofoneBloqueado)
	}
	m := ComporMensagem(FaseMicrofoneBloqueado)
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

// TestFaseMicrofoneBloqueado_ComoUmErro: aparece, é terminal (some sozinha)
// e é pílula de mensagem com o ponto vermelho do erro.
func TestFaseMicrofoneBloqueado_ComoUmErro(t *testing.T) {
	if !Visible(FaseMicrofoneBloqueado) {
		t.Error("FaseMicrofoneBloqueado invisível")
	}
	if !isTerminalPhase(FaseMicrofoneBloqueado) {
		t.Error("FaseMicrofoneBloqueado não é terminal: a pílula ficaria na tela para sempre")
	}
	if DimensoesPara(FaseMicrofoneBloqueado).ComOnda {
		t.Error("a pílula de mensagem não tem onda")
	}
	cor, ok := sinalPara(FaseMicrofoneBloqueado)
	if !ok || cor != corVermelho {
		t.Errorf("sinalPara = %v, %v; quero vermelho", cor, ok)
	}
}

// TestFaseMicrofoneBloqueado_ForaDoNucleo: o valor não pode colidir com
// uma fase que o núcleo venha a ganhar.
func TestFaseMicrofoneBloqueado_ForaDoNucleo(t *testing.T) {
	if s := FaseMicrofoneBloqueado.String(); s != "unknown" {
		t.Fatalf("o núcleo conhece o valor %d como %q", FaseMicrofoneBloqueado, s)
	}
	for _, p := range todasAsFases() {
		if p == FaseMicrofoneBloqueado {
			t.Fatalf("colide com a fase do núcleo %s", p)
		}
	}
}

// TestFaseDoMicrofone: só um erro com a marca vira a mensagem do
// microfone; a marca nunca transforma outra fase em erro.
func TestFaseDoMicrofone(t *testing.T) {
	for _, p := range todasAsFases() {
		if got := FaseDoMicrofone(p, false); got != p {
			t.Errorf("FaseDoMicrofone(%s, false) = %v, quero %s", p, got, p)
		}
		quero := p
		if p == dictation.OverlayError {
			quero = FaseMicrofoneBloqueado
		}
		if got := FaseDoMicrofone(p, true); got != quero {
			t.Errorf("FaseDoMicrofone(%s, true) = %v, quero %v", p, got, quero)
		}
	}
}
