package dictation

import "testing"

// TestOverlayPhaseFor_FalhaDeInsercaoNaoDizQueNaoTranscreveu é um
// defeito que Mauro encontrou usando o produto em 27-set-2026.
//
// Ele ditou sem um campo de texto em foco. A transcrição funcionou, o
// texto ficou na área de transferência (ADR-011, decisão 3: não perder o
// texto ditado), e a tela mostrou "Não consegui transcrever. Tente
// novamente." — mandando refazer um trabalho que já estava pronto.
//
// A causa: a fase era escolhida só pelo DESFECHO, e OutcomeError cobre
// coisas que pedem ações opostas.
func TestOverlayPhaseFor_FalhaDeInsercaoNaoDizQueNaoTranscreveu(t *testing.T) {
	got := overlayPhaseFor(OutcomeError, ErrorReasonInsertClipboard)
	if got == OverlayError {
		t.Fatal("falha de INSERÇÃO caiu na mensagem de falha de TRANSCRIÇÃO — " +
			"a pessoa dita de novo um texto que já está no clipboard")
	}
	if got != OverlayClipboard {
		t.Errorf("overlayPhaseFor(erro, insert_clipboard) = %v, want OverlayClipboard", got)
	}
}

// TestOverlayPhaseFor_FalhaDeInsercaoSemClipboardNaoMandaColar.
//
// Nem toda falha de inserção deixa o texto na área de transferência: na
// injeção PARCIAL o clipboard nem chega a ser tocado (ver
// inserter.Result.Partial), e quando os dois caminhos falham cedo também
// não.
//
// Mandar colar nesses casos faz a pessoa colar o que ela tinha copiado
// ANTES — um erro silencioso, porque algo aparece no campo e parece que
// funcionou.
//
// Este defeito foi introduzido por mim no mesmo dia, ao criar
// OverlayClipboard tratando TODA falha de inserção como "está no
// clipboard".
func TestOverlayPhaseFor_FalhaDeInsercaoSemClipboardNaoMandaColar(t *testing.T) {
	got := overlayPhaseFor(OutcomeError, ErrorReasonInsertFailed)
	if got == OverlayClipboard {
		t.Fatal("falha de inserção SEM clipboard retido mandaria colar — " +
			"a pessoa colaria o que tinha copiado antes")
	}
	if got != OverlayInsertFailed {
		t.Errorf("overlayPhaseFor(erro, insert_failed) = %v, want OverlayInsertFailed", got)
	}
}

// TestOverlayPhaseFor_OutrosErrosContinuamSendoErro: a correção não pode
// ter transformado toda falha em "está no clipboard" — aí a mensagem
// mentiria na direção oposta, e pior, mandaria colar algo que não
// existe.
func TestOverlayPhaseFor_OutrosErrosContinuamSendoErro(t *testing.T) {
	for _, r := range []ErrorReason{
		ErrorReasonNoSpeech,
		ErrorReasonTranscribeFailed,
		ErrorReasonResolveFailed,
		ErrorReasonTimeout,
		ErrorReasonAudioCaptureFailed,
		ErrorReasonNone,
	} {
		if got := overlayPhaseFor(OutcomeError, r); got != OverlayError {
			t.Errorf("overlayPhaseFor(erro, %v) = %v, want OverlayError", r, got)
		}
	}
}

// TestOverlayPhaseFor_SucessoEBloqueioNaoMudaram.
func TestOverlayPhaseFor_SucessoEBloqueioNaoMudaram(t *testing.T) {
	if got := overlayPhaseFor(OutcomeSuccess, ErrorReasonNone); got != OverlaySuccess {
		t.Errorf("sucesso = %v", got)
	}
	if got := overlayPhaseFor(OutcomeBlocked, ErrorReasonNone); got != OverlayBlocked {
		t.Errorf("bloqueado = %v", got)
	}
	// Sucesso nunca vira clipboard, mesmo com um motivo estranho junto.
	if got := overlayPhaseFor(OutcomeSuccess, ErrorReasonInsertFailed); got != OverlaySuccess {
		t.Errorf("sucesso com motivo de insert_failed = %v, want OverlaySuccess", got)
	}
}
