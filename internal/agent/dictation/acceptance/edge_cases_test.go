package acceptance

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestEdgeMicrofoneRemovidoDuranteGravacao cobre o edge case "microfone
// removido" da spec: StopAudioCaptureEffect falha (em vez de nunca
// responder), e a tentativa deve terminar em erro sem nunca chamar o ASR.
func TestEdgeMicrofoneRemovidoDuranteGravacao(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)
	h.Audio.ExpectStopFailure(attempt, dictation.ErrorReasonAudioCaptureFailed)

	h.keyUp(attempt)

	hist := h.lastHistory()
	if hist.Outcome != dictation.OutcomeError || hist.Reason != dictation.ErrorReasonAudioCaptureFailed {
		t.Fatalf("history = %+v, want outcome=error reason=audio_capture_failed", hist)
	}
	if len(h.ASR.Calls) != 0 {
		t.Fatalf("ASR foi chamado mesmo com falha de captura: %v", h.ASR.Calls)
	}
	if !h.Audio.AllReleased() {
		t.Fatalf("audio não liberado após falha de microfone")
	}
	if got := h.Machine.State(); got != dictation.StateError {
		t.Fatalf("state = %s, want error", got)
	}
}

// TestEdgeAppBloqueiaPaste cobre o edge case "app bloqueia paste": o
// fallback por clipboard é tentado e falha, mas o clipboard do usuário
// ainda assim precisa voltar ao que era antes.
func TestEdgeAppBloqueiaPaste(t *testing.T) {
	h := newHarness(t, defaultConfig())
	h.Insert.SeedClipboard("o que o usuario tinha copiado")
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)
	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 400 * time.Millisecond})
	h.Insert.ExpectTotalFailure(attempt) // simula paste bloqueado pelo app

	h.keyUp(attempt)

	if got := h.Insert.ClipboardContent(); got != "o que o usuario tinha copiado" {
		t.Fatalf("clipboard = %q, want restaurado mesmo com paste bloqueado", got)
	}
	hist := h.lastHistory()
	if hist.Outcome != dictation.OutcomeError || hist.Reason != dictation.ErrorReasonInsertFailed {
		t.Fatalf("history = %+v, want outcome=error reason=insert_failed", hist)
	}
}

// TestEdgeGravacaoLonga cobre o edge case "gravação longa (estoura o
// máximo)". O Machine entregue para F1-A não impõe nenhum teto de duração
// de gravação (o único timer entre keydown e keyup é TimerHoldThreshold,
// que já expirou ao entrar em Recording) — este teste prova que uma
// gravação bem mais longa que o normal ainda é processada e liberada
// corretamente, sem estourar nenhum outro invariante.
func TestEdgeGravacaoLonga(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)

	longDuration := 5 * time.Minute
	h.Clock.Advance(longDuration)

	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: longDuration})
	h.ASR.ExpectSuccess(attempt, "texto de uma gravação bem longa", 0.8)

	h.keyUp(attempt)

	if got := h.Machine.State(); got != dictation.StateSuccess {
		t.Fatalf("state = %s, want success mesmo após gravação longa", got)
	}
	if !h.Audio.AllReleased() {
		t.Fatalf("audio não liberado após gravação longa")
	}
}

// TestEdgeRuntimeNuncaResponde_TimeoutSalvaSessao cobre o edge case
// "runtime que NUNCA responde -> o timeout da secao 6 tem que salvar a
// sessão". Para cada Effect que espera resposta, o próprio núcleo arma um
// StartTimerEffect de watchdog (ADR-007, seção 6); aqui provamos que, se o
// dublê correspondente nunca responde, o watchdog devolve a Machine a
// idle/error sozinho, sem nenhuma ajuda externa.
func TestEdgeRuntimeNuncaResponde_TimeoutSalvaSessao(t *testing.T) {
	t.Run("audio_stop", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopHang(attempt)

		h.keyUp(attempt)
		h.Clock.Advance(dictation.DefaultAudioStopTimeout)

		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)
	})

	t.Run("transcribe", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second})
		h.ASR.ExpectSilence(attempt)

		h.keyUp(attempt)
		h.Clock.Advance(dictation.DefaultTranscribeTimeout)

		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)
	})

	t.Run("resolve", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second})
		h.ASR.ExpectSuccess(attempt, "algo", 0.9)
		h.Resolve.ExpectSilence(attempt)

		h.keyUp(attempt)
		h.Clock.Advance(dictation.DefaultResolveTimeout)

		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)
	})

	t.Run("insert", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second})
		h.ASR.ExpectSuccess(attempt, "algo", 0.9)
		h.Resolve.ExpectSuccess(attempt, "algo")
		h.Insert.ExpectHang(attempt)

		h.keyUp(attempt)
		h.Clock.Advance(dictation.DefaultInsertTimeout)

		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)
	})
}

// TestEdgeDitadoLongo_PrazoDaTranscricaoAcompanhaOAudio é o incidente de
// 01-out-2026 (docs/defeito-ditado-longo.md): um ditado de 37 s levou 18,8 s
// no ASR, o prazo fixo de 10 s encerrou a tentativa, e o texto chegou
// depois, para ninguém. Com o prazo do tamanho do áudio, a transcrição
// lenta de um ditado longo ainda entra; e o watchdog continua salvando a
// sessão, só que no prazo do áudio.
func TestEdgeDitadoLongo_PrazoDaTranscricaoAcompanhaOAudio(t *testing.T) {
	const audio = 37200 * time.Millisecond
	prazo := dictation.PrazoDeTranscricao(audio)

	t.Run("transcricao_lenta_dentro_do_prazo_entra", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccessWithDuration(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: audio}, audio)
		h.ASR.ExpectSilence(attempt)

		h.keyUp(attempt)
		// 18,8 s: o que o ASR levou no incidente. Passou dos 10 s antigos.
		h.Clock.Advance(18800 * time.Millisecond)
		if got := h.Machine.State(); got != dictation.StateProcessing {
			t.Fatalf("state = %s aos 18,8 s, want processing: o prazo de %v para %v de áudio não podia ter vencido", got, prazo, audio)
		}

		h.Runtime.Dispatch(dictation.TranscribeResultEvent{Attempt: attempt, Result: dictation.ASRResultDTO{Text: "ditado longo"}})
		if got := h.Machine.State(); got != dictation.StateSuccess {
			t.Fatalf("state = %s, want success: a transcrição lenta tinha de entrar", got)
		}
		if hist := h.lastHistory(); hist.Outcome != dictation.OutcomeSuccess || hist.Text == "" {
			t.Fatalf("history = %+v, want success com o texto", hist)
		}
	})

	t.Run("watchdog_ainda_salva_a_sessao_no_prazo_do_audio", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccessWithDuration(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: audio}, audio)
		h.ASR.ExpectSilence(attempt)

		h.keyUp(attempt)
		h.Clock.Advance(prazo - time.Millisecond)
		if got := h.Machine.State(); got != dictation.StateProcessing {
			t.Fatalf("state = %s um instante antes do prazo, want processing", got)
		}
		h.Clock.Advance(time.Millisecond)
		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)
	})
}

// TestEdgeDitadoLongo_PrazoDaResolucaoAcompanhaOTexto é a decisão 5 de
// docs/defeito-ditado-longo.md: no ditado longo, o Polish ganha até 8 s, e o
// watchdog da resolução tem de ficar 1,5 s acima, senão a tentativa acaba
// sem texto antes de o Polish (ou o Smart, no prazo dele) responder. O
// núcleo não sabe do Polish: aqui o "Polish" é um resolver que responde
// tarde, e as corridas são as do relógio do núcleo.
func TestEdgeDitadoLongo_PrazoDaResolucaoAcompanhaOTexto(t *testing.T) {
	// Um ditado de 1 minuto: 1000 caracteres, o teto.
	longo := strings.Repeat("palavra ", 125)
	prazo := dictation.PrazoDeResolucao(longo)
	if prazo != 9500*time.Millisecond {
		t.Fatalf("pré-condição: prazo de %d caracteres = %v, quero o teto de 9,5 s", len(longo), prazo)
	}
	// O Polish desiste aos 8 s (o teto dele) e entrega o Smart.
	const tetoDoPolish = 8 * time.Second

	// ditarAteAResolucao leva uma tentativa até a resolução, que fica sem
	// resposta: o relógio da resolução começa aqui.
	ditarAteAResolucao := func(t *testing.T, h *harness, texto string) dictation.AttemptID {
		t.Helper()
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Minute})
		h.ASR.ExpectSuccess(attempt, texto, 0.9)
		h.Resolve.ExpectSilence(attempt)
		h.keyUp(attempt)
		if len(h.Resolve.Calls) == 0 || h.Resolve.Calls[len(h.Resolve.Calls)-1] != attempt {
			t.Fatalf("a resolução da tentativa %d não começou", attempt)
		}
		return attempt
	}

	t.Run("polish_no_teto_dele_ainda_entra", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := ditarAteAResolucao(t, h, longo)

		// Os 5 s de antes passaram: com o prazo fixo, o ditado se perdia aqui.
		h.Clock.Advance(dictation.DefaultResolveTimeout)
		if got := h.Machine.State(); got != dictation.StateProcessing {
			t.Fatalf("state = %s aos 5 s, want processing: o prazo de %v não podia ter vencido", got, prazo)
		}
		h.Clock.Advance(tetoDoPolish - dictation.DefaultResolveTimeout)
		h.Runtime.Dispatch(dictation.ResolveResultEvent{Attempt: attempt, Result: dictation.ResolveResultDTO{Text: "Ditado polido."}})
		if got := h.Machine.State(); got != dictation.StateSuccess {
			t.Fatalf("state = %s, want success: a resposta no teto do Polish tinha de entrar", got)
		}
		if n := len(h.Insert.Calls); n != 1 || h.Insert.Calls[0].Text != "Ditado polido." {
			t.Fatalf("inserções = %+v, want uma, com o texto da resolução", h.Insert.Calls)
		}
		if got := h.Runtime.PendingTimers(); got != 0 {
			t.Fatalf("PendingTimers() = %d, want 0", got)
		}
	})

	t.Run("watchdog_ainda_salva_a_sessao_no_prazo_do_texto", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		ditarAteAResolucao(t, h, longo)

		h.Clock.Advance(prazo - time.Millisecond)
		if got := h.Machine.State(); got != dictation.StateProcessing {
			t.Fatalf("state = %s um instante antes do prazo, want processing", got)
		}
		h.Clock.Advance(time.Millisecond)
		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)
	})

	t.Run("fala_curta_continua_com_os_5_s_de_antes", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		ditarAteAResolucao(t, h, "reunião às três")

		h.Clock.Advance(dictation.DefaultResolveTimeout - time.Millisecond)
		if got := h.Machine.State(); got != dictation.StateProcessing {
			t.Fatalf("state = %s, want processing", got)
		}
		h.Clock.Advance(time.Millisecond)
		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)
	})

	t.Run("keydown_durante_a_resolucao_longa_e_ignorado", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := ditarAteAResolucao(t, h, longo)
		h.Clock.Advance(6 * time.Second)

		// A tecla desce e sobe no meio da espera: a tentativa é a mesma, e
		// nada é suprimido, sintetizado ou gravado por isso.
		suprimidas, sintetizadas := len(h.Runtime.Suppressed), len(h.Runtime.Synthesized)
		if efeitos := h.Runtime.Dispatch(dictation.KeyDownEvent{Attempt: dictation.NoAttempt, Focus: normalFocus()}); len(efeitos) != 0 {
			t.Fatalf("keydown no meio da resolução produziu %#v", efeitos)
		}
		if efeitos := h.keyUp(attempt); len(efeitos) != 0 {
			t.Fatalf("keyup no meio da resolução produziu %#v", efeitos)
		}
		if h.Machine.CurrentAttempt() != attempt || h.Machine.State() != dictation.StateProcessing {
			t.Fatalf("tentativa %d em %s, want a %d em processing", h.Machine.CurrentAttempt(), h.Machine.State(), attempt)
		}
		if len(h.Runtime.Suppressed) != suprimidas || len(h.Runtime.Synthesized) != sintetizadas {
			t.Fatalf("a tecla no meio da resolução foi suprimida ou sintetizada")
		}

		// O resultado chega, e o ditado seguinte começa normalmente.
		h.Runtime.Dispatch(dictation.ResolveResultEvent{Attempt: attempt, Result: dictation.ResolveResultDTO{Text: "ok"}})
		if got := h.Machine.State(); got != dictation.StateSuccess {
			t.Fatalf("state = %s, want success", got)
		}
		if seguinte := h.keyDown(normalFocus()); seguinte != attempt+1 {
			t.Fatalf("ditado seguinte = tentativa %d, want %d", seguinte, attempt+1)
		}
	})

	t.Run("resposta_da_tentativa_vencida_nao_entra_na_seguinte", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		vencida := ditarAteAResolucao(t, h, longo)
		h.Clock.Advance(prazo)
		assertSessionRecovered(t, h, dictation.ErrorReasonTimeout)

		// Uma tentativa nova, já gravando, quando a resposta da vencida chega.
		seguinte := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		if got := h.Machine.State(); got != dictation.StateRecording {
			t.Fatalf("state = %s, want recording", got)
		}
		if efeitos := h.Runtime.Dispatch(dictation.ResolveResultEvent{Attempt: vencida, Result: dictation.ResolveResultDTO{Text: "tarde demais"}}); len(efeitos) != 0 {
			t.Fatalf("a resposta da tentativa vencida produziu %#v", efeitos)
		}
		if h.Machine.CurrentAttempt() != seguinte || h.Machine.State() != dictation.StateRecording {
			t.Fatalf("tentativa %d em %s, want a %d gravando", h.Machine.CurrentAttempt(), h.Machine.State(), seguinte)
		}
		if len(h.Insert.Calls) != 0 {
			t.Fatalf("inserções = %+v, want nenhuma", h.Insert.Calls)
		}
	})
}

func assertSessionRecovered(t *testing.T, h *harness, wantReason dictation.ErrorReason) {
	t.Helper()
	if got := h.Machine.State(); got != dictation.StateError {
		t.Fatalf("state = %s, want error (a sessão precisa se recuperar sozinha)", got)
	}
	if got := h.Machine.CurrentAttempt(); got != dictation.NoAttempt {
		t.Fatalf("CurrentAttempt() = %d, want NoAttempt", got)
	}
	hist := h.lastHistory()
	if hist.Outcome != dictation.OutcomeTimeout || hist.Reason != wantReason {
		t.Fatalf("history = %+v, want outcome=timeout reason=%s", hist, wantReason)
	}
	if !h.Audio.AllReleased() {
		t.Fatalf("audio não liberado após timeout: allocated=%d released=%d", h.Audio.Allocated, h.Audio.Released)
	}
	if got := h.Runtime.PendingTimers(); got != 0 {
		t.Fatalf("PendingTimers() = %d, want 0 (nenhum watchdog deveria continuar armado)", got)
	}
}

// TestEdgeCampoProtegido_BlockedSemAudioSemContexto cobre o TEST-009 /
// AC-036: "Ao focar password control, dictation é bloqueado por default e
// nenhum contexto/áudio é enviado."
func TestEdgeCampoProtegido_BlockedSemAudioSemContexto(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(protectedFocus())

	if got := h.Machine.State(); got != dictation.StateBlocked {
		t.Fatalf("state = %s, want blocked", got)
	}
	if h.Audio.HasMark(attempt) {
		t.Fatalf("MarkStart foi chamado para um campo protegido — áudio não deveria nem começar a ser cortado")
	}

	// Hold confirmado (threshold expira sem keyup): é o caminho que
	// efetivamente encerra a tentativa como blocked.
	h.Clock.Advance(defaultConfig().HoldThreshold)

	if len(h.ASR.Calls) != 0 || len(h.Resolve.Calls) != 0 || len(h.Insert.Calls) != 0 {
		t.Fatalf("alguma porta de conteúdo foi chamada para um campo protegido: asr=%v resolve=%v insert=%v", h.ASR.Calls, h.Resolve.Calls, h.Insert.Calls)
	}
	// Blocked NÃO entra no histórico: registrar "tentou ditar aqui"
	// carregaria metadado sobre uso de campo de senha, contra a postura de
	// privacidade da spec (AC-027). O usuário já recebe o aviso pelo
	// overlay (OverlayBlocked), não pelo histórico.
	if len(h.Runtime.History) != 0 {
		t.Fatalf("history = %+v, want nenhum RecordHistoryEffect para outcome=blocked (privacidade, AC-027)", h.Runtime.History)
	}
	if got := h.Machine.State(); got != dictation.StateBlocked {
		t.Fatalf("state final = %s, want blocked", got)
	}
	if got := h.Machine.CurrentAttempt(); got != dictation.NoAttempt {
		t.Fatalf("CurrentAttempt() = %d, want NoAttempt após blocked", got)
	}
	if !h.Audio.AllReleased() {
		t.Fatalf("audio não liberado após blocked")
	}
}

// TestEdgeKeyUpPerdido cobre o edge case "keyup perdido -> watchdog
// encerra e libera audio". Duas situações precisam ser distinguidas:
//
//  1. O keyup chega atrasado, mas ainda DENTRO do limite de
//     TimerMaxRecording. Nenhuma passagem de tempo por si só deveria mudar
//     o estado, e quando o keyup finalmente chega tudo continua
//     funcionando normalmente.
//
//  2. O keyup NUNCA chega de fato (perda real no hook do SO, ou tecla
//     fisicamente presa). Isto NÃO é mais um gap: TimerMaxRecording
//     (Config.MaxRecording, default 120s) é armado ao entrar em
//     StateRecording exatamente para esta rede de segurança. Quando expira,
//     a captura é encerrada pelo MESMO caminho do keyup — o VAD decide se
//     havia fala de verdade — e a tentativa termina, liberando a tecla de
//     ditado para uma tentativa nova (BLOCKER corrigido, ADR-007 seção 6).
func TestEdgeKeyUpPerdido(t *testing.T) {
	t.Run("atrasado_mas_chega_funciona_normalmente", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		if got := h.Machine.State(); got != dictation.StateRecording {
			t.Fatalf("state = %s, want recording", got)
		}

		// Atrasado, mas ainda dentro do limite de MaxRecording: nada deveria
		// disparar sozinho. defaultConfig() não define MaxRecording
		// explicitamente, então o Machine aplica dictation.DefaultMaxRecording
		// (ADR-007, seção 6) — é esse valor efetivo que o teste precisa
		// respeitar, não o campo zero de Config.
		h.Clock.Advance(dictation.DefaultMaxRecording - time.Second)
		if got := h.Machine.State(); got != dictation.StateRecording {
			t.Fatalf("state mudou sozinho durante recording (foi para %s) sem nenhum keyup ou timer legítimo", got)
		}

		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 10 * time.Minute})
		h.keyUp(attempt)
		if got := h.Machine.State(); got != dictation.StateSuccess {
			t.Fatalf("state final = %s, want success mesmo com keyup muito atrasado", got)
		}
	})

	t.Run("nunca_chega_max_recording_salva_a_sessao", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		if got := h.Machine.State(); got != dictation.StateRecording {
			t.Fatalf("state = %s, want recording", got)
		}

		// O keyup nunca chega: apenas o limite de MaxRecording pode encerrar
		// a captura agora. VAD sem fala: tecla presa em silêncio não insere
		// nada (AC-010), mas a sessão precisa se recuperar sozinha.
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: false})
		h.Clock.Advance(dictation.DefaultMaxRecording)

		if got := h.Machine.State(); got != dictation.StateError {
			t.Fatalf("state = %s, want error (MaxRecording encerrou a captura sem fala válida)", got)
		}
		if got := h.Machine.CurrentAttempt(); got != dictation.NoAttempt {
			t.Fatalf("CurrentAttempt() = %d, want NoAttempt (tentativa terminou)", got)
		}
		if !h.Audio.AllReleased() {
			t.Fatalf("audio não liberado após MaxRecording: allocated=%d released=%d", h.Audio.Allocated, h.Audio.Released)
		}

		// Prova de que a tecla não morreu: um KeyDownEvent novo é aceito.
		suppressedBefore := len(h.Runtime.Suppressed)
		h.Runtime.Dispatch(dictation.KeyDownEvent{Attempt: dictation.NoAttempt, Focus: normalFocus()})
		if len(h.Runtime.Suppressed) != suppressedBefore+1 {
			t.Fatalf("um novo KeyDownEvent não foi aceito depois que MaxRecording encerrou a tentativa anterior — a tecla morreu")
		}
		if got := h.Machine.CurrentAttempt(); got == attempt || got == dictation.NoAttempt {
			t.Fatalf("CurrentAttempt() = %d, want um novo AttemptID diferente de %d e de NoAttempt", got, attempt)
		}
	})
}
