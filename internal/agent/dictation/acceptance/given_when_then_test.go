package acceptance

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestTEST001_FCurtoProduzUmFSemGravacao cobre o TEST-001: "Dado F e
// threshold 700ms, quando F dura 120ms, então um único f é emitido e
// nenhuma gravação é criada."
func TestTEST001_FCurtoProduzUmFSemGravacao(t *testing.T) {
	cfg := dictation.Config{Key: dictation.KeyF, HoldThreshold: 700 * time.Millisecond}
	h := newHarness(t, cfg)

	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(120 * time.Millisecond)
	h.keyUp(attempt)

	if got := len(h.Runtime.Synthesized); got != 1 {
		t.Fatalf("caracteres sintetizados = %d, want exatamente 1", got)
	}
	if h.Audio.Allocated != 0 {
		t.Fatalf("buffers de gravação alocados = %d, want 0", h.Audio.Allocated)
	}
	if len(h.Runtime.Overlays) != 0 {
		t.Fatalf("overlays = %+v, want nenhum (tap não deve mostrar overlay de gravação)", h.Runtime.Overlays)
	}
}

// TestTEST002_FAlemDoThresholdSemCaractereOverlayRecording cobre o
// TEST-002: "Dado F, quando F passa de 700ms, então caractere não é
// inserido e overlay entra em recording."
func TestTEST002_FAlemDoThresholdSemCaractereOverlayRecording(t *testing.T) {
	cfg := dictation.Config{Key: dictation.KeyF, HoldThreshold: 700 * time.Millisecond}
	h := newHarness(t, cfg)

	h.keyDown(normalFocus())
	h.Clock.Advance(701 * time.Millisecond)

	if len(h.Runtime.Synthesized) != 0 {
		t.Fatalf("synthesized = %v, want nenhum caractere", h.Runtime.Synthesized)
	}
	if len(h.Runtime.Overlays) == 0 || h.Runtime.Overlays[len(h.Runtime.Overlays)-1].Phase != dictation.OverlayRecording {
		t.Fatalf("overlays = %+v, want overlay em recording", h.Runtime.Overlays)
	}
	if got := h.Machine.State(); got != dictation.StateRecording {
		t.Fatalf("state = %s, want recording", got)
	}
}

// TestTEST003_FalaValidaSoltarInsereTextoFinal cobre o TEST-003: "Com fala
// válida, ao soltar, captura encerra, estado vira processing e texto final
// é inserido."
//
// O ASR é deliberadamente configurado para não responder de imediato
// (ExpectSilence), o que permite observar o estado processing parado no
// meio do caminho — em vez de o runtime síncrono cascatear direto até
// success sem deixar o estado intermediário observável. A resposta do ASR
// é então entregue manualmente via Runtime.Dispatch, exatamente como um
// runtime real entregaria uma resposta assíncrona chegando depois.
func TestTEST003_FalaValidaSoltarInsereTextoFinal(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)

	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 800 * time.Millisecond})
	h.ASR.ExpectSilence(attempt)

	effects := h.keyUp(attempt)

	var sawStop bool
	for _, e := range effects {
		if se, ok := e.(dictation.StopAudioCaptureEffect); ok && se.Attempt == attempt {
			sawStop = true
		}
	}
	if !sawStop {
		t.Fatalf("keyUp não encerrou a captura: %+v", effects)
	}
	if got := h.Machine.State(); got != dictation.StateProcessing {
		t.Fatalf("state = %s, want processing enquanto aguarda a transcrição", got)
	}

	// A resposta do ASR chega agora (Resolver e Inserter seguem com seus
	// defaults de sucesso, equivalentes à política Literal).
	h.Runtime.Dispatch(dictation.TranscribeResultEvent{
		Attempt: attempt,
		Result:  dictation.ASRResultDTO{Text: "bom dia equipe", Confidence: 0.92},
	})

	if len(h.Insert.Calls) != 1 || h.Insert.Calls[0].Text != "bom dia equipe" {
		t.Fatalf("Insert.Calls = %+v, want texto final \"bom dia equipe\"", h.Insert.Calls)
	}
	if got := h.Machine.State(); got != dictation.StateSuccess {
		t.Fatalf("state final = %s, want success", got)
	}
	if !h.Audio.AllReleased() {
		t.Fatalf("audio não liberado: allocated=%d released=%d", h.Audio.Allocated, h.Audio.Released)
	}
}

// TestTEST007_BufferLiberadoAoTerminarOuFalhar cobre o TEST-007: "Ao
// terminar/falhar a tentativa, buffer é liberado e nenhum arquivo de áudio
// permanece." Exercita todos os caminhos de saída que passam por
// endAttempt (ADR-007, seção 7): é isso que torna o critério verificável
// por teste, e não uma promessa em comentário.
func TestTEST007_BufferLiberadoAoTerminarOuFalhar(t *testing.T) {
	t.Run("sucesso", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second})
		h.keyUp(attempt)
		assertAudioFullyReleased(t, h)
		if h.Audio.Allocated == 0 {
			t.Fatalf("teste não prova nada: nenhum buffer chegou a ser alocado")
		}
	})

	t.Run("falha_na_insercao", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second})
		h.Insert.ExpectTotalFailure(attempt)
		h.keyUp(attempt)
		assertAudioFullyReleased(t, h)
	})

	t.Run("timeout_de_transcricao", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second})
		h.ASR.ExpectSilence(attempt)
		h.keyUp(attempt)
		h.Clock.Advance(dictation.DefaultTranscribeTimeout)
		assertAudioFullyReleased(t, h)
		if got := h.Machine.State(); got != dictation.StateError {
			t.Fatalf("state = %s, want error após timeout", got)
		}
	})

	t.Run("tap", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(120 * time.Millisecond)
		h.keyUp(attempt)
		assertAudioFullyReleased(t, h)
	})

	t.Run("campo_protegido", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		h.keyDown(protectedFocus())
		// Conduz a tentativa até o fim: o desfecho blocked só se completa
		// quando o threshold expira sem keyup (StateBlocked).
		h.Clock.Advance(defaultConfig().HoldThreshold)
		assertAudioFullyReleased(t, h)
	})

	t.Run("microfone_removido", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopFailure(attempt, dictation.ErrorReasonAudioCaptureFailed)
		h.keyUp(attempt)
		assertAudioFullyReleased(t, h)
	})
}

// assertAudioFullyReleased confere o invariante do TEST-007: nenhum buffer
// alocado pode ficar sem liberar, e a tentativa não pode continuar
// "presa" na Machine.
func assertAudioFullyReleased(t *testing.T, h *harness) {
	t.Helper()
	if !h.Audio.AllReleased() {
		t.Fatalf("audio não totalmente liberado: allocated=%d released=%d", h.Audio.Allocated, h.Audio.Released)
	}
	if got := h.Machine.CurrentAttempt(); got != dictation.NoAttempt {
		t.Fatalf("CurrentAttempt() = %d, want NoAttempt (tentativa deveria ter terminado)", got)
	}
}
