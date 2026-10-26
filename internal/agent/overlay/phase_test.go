package overlay

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

func TestLabel_TextosVerbatimDaSpec(t *testing.T) {
	cases := []struct {
		phase dictation.OverlayPhase
		want  string
	}{
		{dictation.OverlayRecording, "Gravando"},
		{dictation.OverlaySuccess, "Inserido"},
		{dictation.OverlayError, "Não consegui transcrever. Tente novamente."},
		{dictation.OverlayBlocked, "Ditado desativado em campo protegido."},
		{dictation.OverlayHidden, ""},
	}
	for _, c := range cases {
		if got := Label(c.phase); got != c.want {
			t.Errorf("Label(%s) = %q, want %q", c.phase, got, c.want)
		}
	}
}

func TestVisible(t *testing.T) {
	if Visible(dictation.OverlayHidden) {
		t.Error("Visible(OverlayHidden) = true, want false")
	}
	for _, p := range []dictation.OverlayPhase{
		dictation.OverlayRecording, dictation.OverlaySuccess,
		dictation.OverlayError, dictation.OverlayBlocked,
	} {
		if !Visible(p) {
			t.Errorf("Visible(%s) = false, want true", p)
		}
	}
}

func TestIsTerminalPhase(t *testing.T) {
	terminal := []dictation.OverlayPhase{dictation.OverlaySuccess, dictation.OverlayError, dictation.OverlayBlocked}
	for _, p := range terminal {
		if !isTerminalPhase(p) {
			t.Errorf("isTerminalPhase(%s) = false, want true", p)
		}
	}
	notTerminal := []dictation.OverlayPhase{dictation.OverlayHidden, dictation.OverlayRecording}
	for _, p := range notTerminal {
		if isTerminalPhase(p) {
			t.Errorf("isTerminalPhase(%s) = true, want false", p)
		}
	}
}

// todasAsFases é a lista canônica de dictation.OverlayPhase.
//
// Existe porque as tabelas deste arquivo eram enumeradas à mão, cada uma
// por conta própria, e já estavam incompletas antes de "processing"
// existir: nenhuma delas cobria OverlayInsertFailed nem OverlayClipboard.
// Uma fase nova entrava no núcleo e os testes daqui seguiam verdes sem
// nunca olhar para ela.
//
// Acrescentar um caso à lista é trabalho de quem acrescenta a fase. A
// diferença é que agora é UMA lista, e os testes abaixo a percorrem.
func todasAsFases() []dictation.OverlayPhase {
	return []dictation.OverlayPhase{
		dictation.OverlayHidden,
		dictation.OverlayRecording,
		dictation.OverlayProcessing,
		dictation.OverlaySuccess,
		dictation.OverlayError,
		dictation.OverlayBlocked,
		dictation.OverlayInsertFailed,
		dictation.OverlayClipboard,
	}
}

// fasesDoOverlay é todasAsFases mais as fases que só o overlay conhece
// (FaseJanelaElevada) — tudo que a pílula pode mostrar.
//
// Os testes do que a pílula DESENHA percorrem esta lista, e não a do
// núcleo: a janela de administrador passou por eles sem glifos na fonte
// de bitmap ("indo s não deixa ditar em anela de administrador.") e sem o
// ponto de estado, porque as listas só tinham as fases do núcleo.
func fasesDoOverlay() []dictation.OverlayPhase {
	return append(todasAsFases(), fasesSoDoOverlay()...)
}

// fasesSoDoOverlay são as que o núcleo não emite: o runtime as mostra no
// lugar de uma do núcleo, ou fora de uma tentativa.
func fasesSoDoOverlay() []dictation.OverlayPhase {
	return []dictation.OverlayPhase{FaseJanelaElevada, FaseTextoNoHistorico, FaseRepeticaoRemovida, FaseMicrofoneBloqueado,
		FaseNaNota, FaseNotaNova, FaseNotaNaoSalvou}
}

// nomeDaFase é o String() da fase, que para as do overlay devolve
// "unknown" (o núcleo não as conhece).
func nomeDaFase(p dictation.OverlayPhase) string {
	switch p {
	case FaseJanelaElevada:
		return "janela-elevada"
	case FaseTextoNoHistorico:
		return "texto-no-historico"
	case FaseRepeticaoRemovida:
		return "repeticao-removida"
	case FaseMicrofoneBloqueado:
		return "microfone-bloqueado"
	case FaseNaNota:
		return "na-nota"
	case FaseNotaNova:
		return "nota-nova"
	case FaseNotaNaoSalvou:
		return "nota-nao-salvou"
	}
	return p.String()
}

// TestLabel_NenhumaFaseVisivelFicaSemTexto: uma fase sem case no switch
// cai no default e devolve "" — o overlay mostraria uma pílula vazia, sem
// dizer nada. Foi exatamente o estado de "processing" até 28-set-2026,
// com a diferença de que ele nem chegava a ser emitido.
func TestLabel_NenhumaFaseVisivelFicaSemTexto(t *testing.T) {
	for _, p := range todasAsFases() {
		if p == dictation.OverlayHidden {
			continue // esconder não tem o que dizer
		}
		if Label(p) == "" {
			t.Errorf("Label(%s) = \"\" — fase visível sem texto", p)
		}
	}
}

// TestFases_CoberturaDeVisibleEDeTerminal percorre a lista canônica e
// confere as duas classificações de uma vez, para que uma fase nova não
// entre sem que alguém decida explicitamente o que ela é.
func TestFases_CoberturaDeVisibleEDeTerminal(t *testing.T) {
	terminais := map[dictation.OverlayPhase]bool{
		dictation.OverlaySuccess:      true,
		dictation.OverlayError:        true,
		dictation.OverlayBlocked:      true,
		dictation.OverlayInsertFailed: true,
		dictation.OverlayClipboard:    true,
	}
	for _, p := range todasAsFases() {
		if got, quero := Visible(p), p != dictation.OverlayHidden; got != quero {
			t.Errorf("Visible(%s) = %v, want %v", p, got, quero)
		}
		if got := isTerminalPhase(p); got != terminais[p] {
			t.Errorf("isTerminalPhase(%s) = %v, want %v", p, got, terminais[p])
		}
	}
}

// TestIsTerminalPhase_ProcessandoNaoSeEsconde trava a consequência, não a
// classificação: fase terminal some sozinha depois de autoHideDelay. Se
// "processando" fosse terminal, o overlay sumiria no meio do ASR e
// voltaria com "Inserido" — pareceria que o ditado foi cancelado, que é
// pior do que não ter a fase.
func TestIsTerminalPhase_ProcessandoNaoSeEsconde(t *testing.T) {
	if isTerminalPhase(dictation.OverlayProcessing) {
		t.Error("processando é terminal: o overlay sumiria durante a transcrição")
	}
	if !Visible(dictation.OverlayProcessing) {
		t.Error("processando invisível: a pessoa não saberia que há trabalho em curso")
	}
	if got := Label(dictation.OverlayProcessing); got != "Processando" {
		t.Errorf("Label(processando) = %q, want %q (08-ui-ux-spec.md)", got, "Processando")
	}
}
