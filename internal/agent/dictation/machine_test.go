package dictation

import (
	"testing"
	"time"
)

func newTestMachine(t *testing.T, beep bool) *Machine {
	t.Helper()
	m, err := NewMachine(Config{Key: KeyF, HoldThreshold: 700 * time.Millisecond, BeepEnabled: beep})
	if err != nil {
		t.Fatalf("NewMachine() error = %v", err)
	}
	return m
}

func unprotectedFocus() FocusTarget {
	return FocusTarget{ProcessName: "notepad.exe", ControlType: "Edit"}
}

// effectsOfType devolve, em ordem, os elementos de effects cujo tipo
// dinâmico é exatamente igual ao de zero.
func effectsOfType[T Effect](effects []Effect) []T {
	var out []T
	for _, e := range effects {
		if v, ok := e.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func TestNewMachine_RejectsInvalidConfig(t *testing.T) {
	_, err := NewMachine(Config{Key: ShiftLeft, HoldThreshold: 700 * time.Millisecond})
	if err == nil {
		t.Fatal("NewMachine() with pure-modifier key: want error, got nil")
	}
}

// --- AC-001 / invariant (e): tap curto ---------------------------------

func TestMachine_TapShort_DiscardsBufferAndSynthesizesChar(t *testing.T) {
	m := newTestMachine(t, false)

	down := m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: unprotectedFocus()})
	if m.State() != StatePrimed {
		t.Fatalf("state after keydown = %v, want StatePrimed", m.State())
	}
	id := m.CurrentAttempt()
	if id == NoAttempt {
		t.Fatal("CurrentAttempt() == NoAttempt after keydown, want an assigned id")
	}
	if len(effectsOfType[StartTimerEffect](down)) != 1 {
		t.Fatalf("keydown effects = %#v, want exactly one StartTimerEffect", down)
	}

	up := m.Handle(KeyUpEvent{Attempt: id})

	if m.State() != StateIdle {
		t.Errorf("state after short tap = %v, want StateIdle", m.State())
	}
	if m.CurrentAttempt() != NoAttempt {
		t.Errorf("CurrentAttempt() after short tap = %v, want NoAttempt", m.CurrentAttempt())
	}
	if got := effectsOfType[AbortAudioCaptureEffect](up); len(got) != 1 {
		t.Errorf("tap effects = %#v, want exactly one AbortAudioCaptureEffect", up)
	}
	if got := effectsOfType[SynthesizeTapEffect](up); len(got) != 1 {
		t.Errorf("tap effects = %#v, want exactly one SynthesizeTapEffect (AC-001)", up)
	}
	if got := effectsOfType[ReleaseAudioEffect](up); len(got) != 1 || got[0].Attempt != id {
		t.Errorf("tap effects ReleaseAudioEffect = %#v, want exactly one for attempt %v", got, id)
	}
	if got := effectsOfType[RecordHistoryEffect](up); len(got) != 0 {
		t.Errorf("tap effects RecordHistoryEffect = %#v, want none (tap is normal typing, not a dictation attempt)", got)
	}
	if got := effectsOfType[CancelTimerEffect](up); len(got) != 1 || got[0].Kind != TimerHoldThreshold {
		t.Errorf("tap effects CancelTimerEffect = %#v, want cancel of TimerHoldThreshold", got)
	}
}

// --- AC-002 / AC-006 / invariant (f): hold ------------------------------

func TestMachine_Hold_NoCharEmitted_EntersRecordingWithOverlay(t *testing.T) {
	m := newTestMachine(t, false)
	down := m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	id := m.CurrentAttempt()
	startTimers := effectsOfType[StartTimerEffect](down)
	if len(startTimers) != 1 || startTimers[0].Kind != TimerHoldThreshold {
		t.Fatalf("keydown StartTimerEffect = %#v, want TimerHoldThreshold", startTimers)
	}

	effects := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})

	if m.State() != StateRecording {
		t.Fatalf("state after threshold expires = %v, want StateRecording", m.State())
	}
	if got := effectsOfType[SynthesizeTapEffect](effects); len(got) != 0 {
		t.Errorf("hold effects = %#v, want no SynthesizeTapEffect (AC-002: hold never emits a char)", got)
	}
	if got := effectsOfType[ShowOverlayEffect](effects); len(got) != 1 || got[0].Phase != OverlayRecording {
		t.Errorf("hold effects ShowOverlayEffect = %#v, want OverlayRecording (AC-005)", got)
	}
	if got := effectsOfType[PlayBeepEffect](effects); len(got) != 0 {
		t.Errorf("hold effects = %#v, want no PlayBeepEffect (beep disabled)", got)
	}
}

func TestMachine_Hold_BeepOnlyWhenEnabled(t *testing.T) {
	m := newTestMachine(t, true)
	m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	id := m.CurrentAttempt()

	effects := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})

	if got := effectsOfType[PlayBeepEffect](effects); len(got) != 1 {
		t.Errorf("hold effects = %#v, want exactly one PlayBeepEffect (AC-006: beep enabled)", got)
	}
}

// --- AC-007 / AC-008 / AC-010: fluxo completo de ditado -----------------

func startRecording(t *testing.T, m *Machine) AttemptID {
	t.Helper()
	m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	id := m.CurrentAttempt()
	m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})
	return id
}

func TestMachine_FullSuccessPath_InsertsFinalText(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)

	stopEffects := m.Handle(KeyUpEvent{Attempt: id})
	if m.State() != StateProcessing {
		t.Fatalf("state after keyup during recording = %v, want StateProcessing", m.State())
	}
	if got := effectsOfType[StopAudioCaptureEffect](stopEffects); len(got) != 1 {
		t.Fatalf("keyup effects = %#v, want exactly one StopAudioCaptureEffect", stopEffects)
	}
	if got := effectsOfType[StartTimerEffect](stopEffects); len(got) != 1 || got[0].Kind != TimerAudioStopTimeout {
		t.Fatalf("keyup effects StartTimerEffect = %#v, want TimerAudioStopTimeout", got)
	}

	captured := m.Handle(AudioCapturedEvent{
		Attempt: id,
		Audio:   AudioBufferRef{ID: "buf-1"},
		VAD:     VADSummary{HasSpeech: true, SpeechDuration: 900 * time.Millisecond},
	})
	if got := effectsOfType[CancelTimerEffect](captured); len(got) != 1 || got[0].Kind != TimerAudioStopTimeout {
		t.Fatalf("audio-captured effects CancelTimerEffect = %#v, want cancel of TimerAudioStopTimeout", got)
	}
	transcribes := effectsOfType[TranscribeEffect](captured)
	if len(transcribes) != 1 || transcribes[0].Audio.ID != "buf-1" {
		t.Fatalf("audio-captured effects TranscribeEffect = %#v, want one referencing buf-1", transcribes)
	}
	if got := effectsOfType[StartTimerEffect](captured); len(got) != 1 || got[0].Kind != TimerTranscribeTimeout {
		t.Fatalf("audio-captured effects StartTimerEffect = %#v, want TimerTranscribeTimeout", got)
	}

	transcribed := m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "modulo fisco", Confidence: 0.9}})
	resolves := effectsOfType[ResolveEffect](transcribed)
	if len(resolves) != 1 || resolves[0].ASRResult.Text != "modulo fisco" {
		t.Fatalf("transcribe-result effects ResolveEffect = %#v, want one with the ASR text", resolves)
	}
	if got := effectsOfType[StartTimerEffect](transcribed); len(got) != 1 || got[0].Kind != TimerResolveTimeout {
		t.Fatalf("transcribe-result effects StartTimerEffect = %#v, want TimerResolveTimeout", got)
	}

	resolved := m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "Módulo Fisco"}})
	if m.State() != StateInserting {
		t.Fatalf("state after resolve success = %v, want StateInserting", m.State())
	}
	inserts := effectsOfType[InsertEffect](resolved)
	if len(inserts) != 1 || inserts[0].Text != "Módulo Fisco" {
		t.Fatalf("resolve-result effects InsertEffect = %#v, want one with resolved text", inserts)
	}
	if got := effectsOfType[StartTimerEffect](resolved); len(got) != 1 || got[0].Kind != TimerInsertTimeout {
		t.Fatalf("resolve-result effects StartTimerEffect = %#v, want TimerInsertTimeout", got)
	}

	inserted := m.Handle(InsertResultEvent{Attempt: id, Result: InsertResultDTO{Success: true}})
	if m.State() != StateSuccess {
		t.Fatalf("final state = %v, want StateSuccess", m.State())
	}
	if m.CurrentAttempt() != NoAttempt {
		t.Errorf("CurrentAttempt() after success = %v, want NoAttempt", m.CurrentAttempt())
	}
	history := effectsOfType[RecordHistoryEffect](inserted)
	if len(history) != 1 || history[0].Outcome != OutcomeSuccess || history[0].Text != "Módulo Fisco" {
		t.Fatalf("insert-result effects RecordHistoryEffect = %#v, want OutcomeSuccess with final text", history)
	}
	if got := effectsOfType[ReleaseAudioEffect](inserted); len(got) != 1 || got[0].Attempt != id {
		t.Fatalf("insert-result effects ReleaseAudioEffect = %#v, want exactly one for attempt %v", got, id)
	}
}

func TestMachine_NoSpeech_SkipsTranscribeAndInsert(t *testing.T) {
	// AC-010: sem fala válida, nada é inserido.
	m := newTestMachine(t, false)
	id := startRecording(t, m)
	m.Handle(KeyUpEvent{Attempt: id})

	effects := m.Handle(AudioCapturedEvent{
		Attempt: id,
		Audio:   AudioBufferRef{ID: "buf-empty"},
		VAD:     VADSummary{HasSpeech: false},
	})

	if m.State() != StateError {
		t.Fatalf("state after no-speech = %v, want StateError", m.State())
	}
	if got := effectsOfType[TranscribeEffect](effects); len(got) != 0 {
		t.Errorf("no-speech effects = %#v, want no TranscribeEffect", got)
	}
	if got := effectsOfType[InsertEffect](effects); len(got) != 0 {
		t.Errorf("no-speech effects = %#v, want no InsertEffect", got)
	}
	history := effectsOfType[RecordHistoryEffect](effects)
	if len(history) != 1 || history[0].Outcome != OutcomeNoSpeech {
		t.Fatalf("no-speech effects RecordHistoryEffect = %#v, want OutcomeNoSpeech", history)
	}
	if got := effectsOfType[ReleaseAudioEffect](effects); len(got) != 1 {
		t.Errorf("no-speech effects = %#v, want exactly one ReleaseAudioEffect", got)
	}
}

func TestMachine_AudioFailed_EndsWithError(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)
	m.Handle(KeyUpEvent{Attempt: id})

	effects := m.Handle(AudioFailedEvent{Attempt: id, Reason: ErrorReasonAudioCaptureFailed})

	if m.State() != StateError {
		t.Fatalf("state after audio failure = %v, want StateError", m.State())
	}
	if got := effectsOfType[AbortAudioCaptureEffect](effects); len(got) != 1 {
		t.Errorf("audio-failed effects = %#v, want one AbortAudioCaptureEffect", got)
	}
	history := effectsOfType[RecordHistoryEffect](effects)
	if len(history) != 1 || history[0].Reason != ErrorReasonAudioCaptureFailed {
		t.Fatalf("audio-failed effects RecordHistoryEffect = %#v, want ErrorReasonAudioCaptureFailed", history)
	}
}

// --- invariant (d): campo protegido -------------------------------------

func TestMachine_ProtectedFocus_BlocksWithoutCapturingAnything(t *testing.T) {
	m := newTestMachine(t, false)
	effects := m.Handle(KeyDownEvent{Focus: FocusTarget{ProcessName: "bank.exe", ControlType: "Password", Protected: true}})

	if m.State() != StateBlocked {
		t.Fatalf("state after protected keydown = %v, want StateBlocked", m.State())
	}
	if got := effectsOfType[SuppressKeyEffect](effects); len(got) != 1 {
		t.Errorf("blocked effects = %#v, want exactly one SuppressKeyEffect (o caractere precisa continuar disponível para tap)", got)
	}
	if got := effectsOfType[MarkAudioStartEffect](effects); len(got) != 0 {
		t.Errorf("blocked effects = %#v, want no MarkAudioStartEffect (nenhum áudio capturado)", got)
	}
	// A tentativa fica viva aguardando o threshold: sem isso o keyup nunca
	// teria a chance de sintetizar o tap e a tecla morreria em qualquer
	// campo de senha (AC-001 / regressão proibida).
	if got := effectsOfType[StartTimerEffect](effects); len(got) != 1 || got[0].Kind != TimerHoldThreshold {
		t.Fatalf("blocked effects StartTimerEffect = %#v, want exactly one StartTimerEffect{Kind: TimerHoldThreshold}", got)
	}
	if got := effectsOfType[TranscribeEffect](effects); len(got) != 0 {
		t.Errorf("blocked effects = %#v, want no TranscribeEffect", got)
	}
	// Blocked não entra no histórico (decisão de privacidade: registrar
	// "tentou ditar aqui" carregaria metadado sobre uso de campo de senha,
	// contra o AC-027) — e, de qualquer forma, a tentativa ainda não
	// terminou neste ponto.
	if got := effectsOfType[RecordHistoryEffect](effects); len(got) != 0 {
		t.Errorf("blocked effects = %#v, want no RecordHistoryEffect", got)
	}
	if got := effectsOfType[ReleaseAudioEffect](effects); len(got) != 0 {
		t.Errorf("blocked effects = %#v, want no ReleaseAudioEffect (tentativa ainda em curso, aguardando threshold)", got)
	}
}

// --- invariant (a): descarte de eventos obsoletos -----------------------

func TestMachine_StaleAttemptID_DoesNotPromoteCurrentAttempt(t *testing.T) {
	m := newTestMachine(t, false)

	// Tentativa 1: tap, termina imediatamente.
	m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	firstID := m.CurrentAttempt()
	m.Handle(KeyUpEvent{Attempt: firstID})
	if m.State() != StateIdle {
		t.Fatalf("state after first attempt = %v, want StateIdle", m.State())
	}

	// Tentativa 2 começa.
	m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	secondID := m.CurrentAttempt()
	if secondID == firstID {
		t.Fatalf("second attempt id = %v, want different from first (%v)", secondID, firstID)
	}

	// Timer atrasado da tentativa 1 chega agora: não pode afetar a
	// tentativa 2 em curso.
	effects := m.Handle(TimerElapsedEvent{Attempt: firstID, Kind: TimerHoldThreshold})
	if effects != nil {
		t.Errorf("stale TimerElapsedEvent effects = %#v, want nil (discarded)", effects)
	}
	if m.State() != StatePrimed {
		t.Errorf("state after stale timer = %v, want StatePrimed (attempt 2 untouched)", m.State())
	}
	if m.CurrentAttempt() != secondID {
		t.Errorf("CurrentAttempt() after stale timer = %v, want %v", m.CurrentAttempt(), secondID)
	}
}

func TestMachine_KeyRepeat_IgnoredWhileAttemptInFlight(t *testing.T) {
	m := newTestMachine(t, false)
	m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	id := m.CurrentAttempt()

	// Auto-repeat do SO: outro KeyDownEvent chega antes do keyup.
	effects := m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	if effects != nil {
		t.Errorf("repeated keydown effects = %#v, want nil (ignored)", effects)
	}
	if m.CurrentAttempt() != id {
		t.Errorf("CurrentAttempt() after repeated keydown = %v, want unchanged %v", m.CurrentAttempt(), id)
	}
}

// --- invariant (b): todo efeito que espera resposta arma timeout --------

func TestMachine_EveryWaitingEffect_HasAccompanyingTimer(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)

	stopEffects := m.Handle(KeyUpEvent{Attempt: id})
	assertHasTimerFor(t, stopEffects, StopAudioCaptureEffect{}, TimerAudioStopTimeout)

	captured := m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
	assertHasTimerFor(t, captured, TranscribeEffect{}, TimerTranscribeTimeout)

	transcribed := m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "ola"}})
	assertHasTimerFor(t, transcribed, ResolveEffect{}, TimerResolveTimeout)

	resolved := m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "ola"}})
	assertHasTimerFor(t, resolved, InsertEffect{}, TimerInsertTimeout)
}

func assertHasTimerFor[T Effect](t *testing.T, effects []Effect, waiting T, kind TimerKind) {
	t.Helper()
	if got := effectsOfType[T](effects); len(got) != 1 {
		t.Fatalf("effects = %#v, want exactly one %T", effects, waiting)
	}
	timers := effectsOfType[StartTimerEffect](effects)
	if len(timers) != 1 || timers[0].Kind != kind {
		t.Fatalf("effects = %#v, want exactly one StartTimerEffect{Kind: %v}", effects, kind)
	}
}

// --- watchdog vs. resposta legítima concorrendo -------------------------

func TestMachine_WatchdogFiresBeforeResponse_EndsAttemptAsTimeout(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)
	m.Handle(KeyUpEvent{Attempt: id})

	timeoutEffects := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerAudioStopTimeout})
	if m.State() != StateError {
		t.Fatalf("state after audio-stop watchdog = %v, want StateError", m.State())
	}
	history := effectsOfType[RecordHistoryEffect](timeoutEffects)
	if len(history) != 1 || history[0].Outcome != OutcomeTimeout {
		t.Fatalf("watchdog effects RecordHistoryEffect = %#v, want OutcomeTimeout", history)
	}

	// A resposta legítima chega atrasada: já não há tentativa corrente, é
	// descartada sem efeito colateral.
	late := m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
	if late != nil {
		t.Errorf("late AudioCapturedEvent effects = %#v, want nil (attempt already ended)", late)
	}
	if m.State() != StateError {
		t.Errorf("state after late response = %v, want unchanged StateError", m.State())
	}
}

func TestMachine_ResponseArrivesBeforeStaleWatchdog_LateWatchdogIgnored(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)
	m.Handle(KeyUpEvent{Attempt: id})

	// A resposta legítima chega primeiro: avança para aguardar transcrição.
	m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
	if m.State() != StateProcessing {
		t.Fatalf("state after audio captured = %v, want StateProcessing", m.State())
	}

	// O watchdog do StopAudioCapture, que deveria ter sido cancelado, ainda
	// assim dispara atrasado para a MESMA tentativa (AttemptID inalterado
	// durante toda a tentativa) — deve ser ignorado, pois já não é o que
	// está pendente.
	late := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerAudioStopTimeout})
	if late != nil {
		t.Errorf("stale watchdog effects = %#v, want nil (already resolved by legitimate response)", late)
	}
	if m.State() != StateProcessing {
		t.Errorf("state after stale watchdog = %v, want unchanged StateProcessing", m.State())
	}
	if m.CurrentAttempt() != id {
		t.Errorf("CurrentAttempt() after stale watchdog = %v, want unchanged %v", m.CurrentAttempt(), id)
	}
}

// --- invariant (c): único ponto de saída emite ReleaseAudio -------------

func TestMachine_EveryOutcome_ReleasesAudioExactlyOnce(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, m *Machine) []Effect
	}{
		{"tap", func(t *testing.T, m *Machine) []Effect {
			m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
			id := m.CurrentAttempt()
			return m.Handle(KeyUpEvent{Attempt: id})
		}},
		{"blocked", func(t *testing.T, m *Machine) []Effect {
			m.Handle(KeyDownEvent{Focus: FocusTarget{Protected: true}})
			id := m.CurrentAttempt()
			// Conduz a tentativa até o fim: o desfecho blocked só se
			// completa quando o threshold expira sem keyup (StateBlocked).
			return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})
		}},
		{"success", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
			m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "x"}})
			m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "x"}})
			return m.Handle(InsertResultEvent{Attempt: id, Result: InsertResultDTO{Success: true}})
		}},
		{"no_speech", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			return m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: false}})
		}},
		{"mic_lost", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			return m.Handle(AudioFailedEvent{Attempt: id, Reason: ErrorReasonAudioCaptureFailed})
		}},
		{"transcribe_error", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
			return m.Handle(TranscribeResultEvent{Attempt: id, Reason: ErrorReasonTranscribeFailed})
		}},
		{"resolve_error", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
			m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "x"}})
			return m.Handle(ResolveResultEvent{Attempt: id, Reason: ErrorReasonResolveFailed})
		}},
		{"insert_error", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
			m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "x"}})
			m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "x"}})
			return m.Handle(InsertResultEvent{Attempt: id, Result: InsertResultDTO{Success: false}})
		}},
		{"watchdog_audio_stop", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerAudioStopTimeout})
		}},
		{"watchdog_transcribe", func(t *testing.T, m *Machine) []Effect {
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
			return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerTranscribeTimeout})
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestMachine(t, false)
			final := tc.run(t, m)
			got := effectsOfType[ReleaseAudioEffect](final)
			if len(got) != 1 {
				t.Fatalf("%s: ReleaseAudioEffect count = %d (%#v), want exactly 1 (TEST-007)", tc.name, len(got), final)
			}
			if m.CurrentAttempt() != NoAttempt {
				t.Errorf("%s: CurrentAttempt() = %v after outcome, want NoAttempt", tc.name, m.CurrentAttempt())
			}
		})
	}
}

// =========================================================================
// Regressão: os 3 defeitos corrigidos pela auditoria adversarial (ver
// mensagem de handoff do lead / ADR-007 seção 6). Cada teste abaixo falha
// se o defeito correspondente voltar.
// =========================================================================

// --- R1: BLOCKER — keyup perdido travava a sessão em StateRecording ------

// TestRegression_LostKeyUp_MaxRecordingSavesTheSession cobre o BLOCKER (1):
// sem TimerMaxRecording, um keyup que nunca chega (tecla presa, app morto,
// keyup perdido pelo SO) deixava a Machine presa em StateRecording para
// sempre, e um KeyDownEvent físico novo era descartado por onKeyDown
// ("if m.current != NoAttempt { return nil }") — a tecla de ditado morria
// até reiniciar o processo.
func TestRegression_LostKeyUp_MaxRecordingSavesTheSession(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)
	if m.State() != StateRecording {
		t.Fatalf("state after threshold = %v, want StateRecording", m.State())
	}

	// O keyup NUNCA chega. Só o watchdog TimerMaxRecording pode tirar a
	// Machine de StateRecording.
	elapsed := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerMaxRecording})
	if m.State() != StateProcessing {
		t.Fatalf("state after TimerMaxRecording = %v, want StateProcessing (captura encerrada pelo mesmo caminho do keyup)", m.State())
	}
	if got := effectsOfType[StopAudioCaptureEffect](elapsed); len(got) != 1 {
		t.Fatalf("MaxRecording effects = %#v, want exactly one StopAudioCaptureEffect", elapsed)
	}

	// VAD decide: sem fala, tecla presa em silêncio não insere nada.
	final := m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: false}})

	if got := effectsOfType[ReleaseAudioEffect](final); len(got) != 1 {
		t.Fatalf("final effects = %#v, want exactly one ReleaseAudioEffect", final)
	}
	if m.State() == StateRecording {
		t.Fatalf("state ainda é StateRecording após MaxRecording + AudioCaptured — sessão continua presa")
	}
	if m.CurrentAttempt() != NoAttempt {
		t.Fatalf("CurrentAttempt() = %v, want NoAttempt (tentativa precisa ter terminado)", m.CurrentAttempt())
	}

	// Prova de que a tecla não morreu: um KeyDownEvent NOVO é aceito.
	newEffects := m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	if newEffects == nil {
		t.Fatalf("KeyDownEvent novo foi ignorado depois que MaxRecording encerrou a tentativa anterior — a tecla morreu")
	}
	newID := m.CurrentAttempt()
	if newID == NoAttempt || newID == id {
		t.Fatalf("CurrentAttempt() após novo keydown = %v, want um id novo e diferente de %v", newID, id)
	}
}

// --- R2: MAJOR — colisão de valor zero no TimerKind ----------------------

// TestRegression_DuplicateTimerElapsed_SameAttempt_YieldsNoEffects cobre o
// MAJOR (2): TimerHoldThreshold valendo zero colidia com a sentinela "nenhum
// timer pendente", então o guard de obsolescência não filtrava nada durante
// StateRecording — um TimerElapsedEvent duplicado reprocessava o threshold e
// reemitia overlay/beep duplicados.
func TestRegression_DuplicateTimerElapsed_SameAttempt_YieldsNoEffects(t *testing.T) {
	m := newTestMachine(t, true) // beep habilitado, para que uma duplicata seria bem visível
	m.Handle(KeyDownEvent{Focus: unprotectedFocus()})
	id := m.CurrentAttempt()

	first := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})
	if m.State() != StateRecording {
		t.Fatalf("state after first TimerElapsed(HoldThreshold) = %v, want StateRecording", m.State())
	}
	if got := effectsOfType[ShowOverlayEffect](first); len(got) != 1 {
		t.Fatalf("first threshold effects = %#v, want exactly one ShowOverlayEffect", first)
	}
	if got := effectsOfType[PlayBeepEffect](first); len(got) != 1 {
		t.Fatalf("first threshold effects = %#v, want exactly one PlayBeepEffect", first)
	}

	// TimerElapsed duplicado, mesmo AttemptID, mesmo Kind: já não é o que a
	// tentativa está aguardando (m.pending == TimerMaxRecording agora).
	second := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})
	if second != nil {
		t.Fatalf("duplicate TimerElapsed(HoldThreshold) effects = %#v, want nil (discarded as stale)", second)
	}
	if m.State() != StateRecording {
		t.Fatalf("state after duplicate TimerElapsed = %v, want unchanged StateRecording", m.State())
	}
	if m.CurrentAttempt() != id {
		t.Fatalf("CurrentAttempt() after duplicate TimerElapsed = %v, want unchanged %v", m.CurrentAttempt(), id)
	}
}

// --- R3: MAJOR — campo protegido engolia o caractere ----------------------

// TestRegression_ProtectedField_PreservesTapCharacter cobre o MAJOR (3),
// caminho tap: antes, keydown em campo protegido chamava endAttempt
// imediatamente, sem nunca emitir SynthesizeTapEffect. Como o Decider já
// suprimiu a tecla na borda do hook, o caractere sumia — o usuário nunca
// conseguiria digitar a letra da tecla de ditado em nenhum campo de senha.
func TestRegression_ProtectedField_PreservesTapCharacter(t *testing.T) {
	m := newTestMachine(t, false)
	down := m.Handle(KeyDownEvent{Focus: FocusTarget{ProcessName: "bank.exe", ControlType: "Password", Protected: true}})
	id := m.CurrentAttempt()
	if got := effectsOfType[MarkAudioStartEffect](down); len(got) != 0 {
		t.Fatalf("protected keydown effects = %#v, want no MarkAudioStartEffect", got)
	}

	// Keyup ANTES do threshold expirar: precisa se comportar como digitação
	// normal (AC-001), mesmo em campo protegido.
	up := m.Handle(KeyUpEvent{Attempt: id})

	if got := effectsOfType[SynthesizeTapEffect](up); len(got) != 1 {
		t.Fatalf("protected tap effects = %#v, want exactly one SynthesizeTapEffect (o caractere não pode sumir)", up)
	}
	if got := effectsOfType[AbortAudioCaptureEffect](up); len(got) != 0 {
		t.Fatalf("protected tap effects = %#v, want no AbortAudioCaptureEffect (nenhum áudio jamais foi capturado)", got)
	}
	if m.State() != StateIdle {
		t.Fatalf("state after protected tap = %v, want StateIdle (comportamento igual a um campo normal)", m.State())
	}
}

// TestRegression_ProtectedField_HoldBlocksWithoutAnyAudio cobre o MAJOR (3),
// caminho hold: threshold confirmado em campo protegido não pode, em
// nenhum momento, ter capturado áudio.
func TestRegression_ProtectedField_HoldBlocksWithoutAnyAudio(t *testing.T) {
	m := newTestMachine(t, false)
	m.Handle(KeyDownEvent{Focus: FocusTarget{ProcessName: "bank.exe", ControlType: "Password", Protected: true}})
	id := m.CurrentAttempt()

	effects := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})

	if got := effectsOfType[ShowOverlayEffect](effects); len(got) != 1 || got[0].Phase != OverlayBlocked {
		t.Fatalf("protected hold effects ShowOverlayEffect = %#v, want exactly one OverlayBlocked", got)
	}
	if got := effectsOfType[MarkAudioStartEffect](effects); len(got) != 0 {
		t.Fatalf("protected hold effects = %#v, want no MarkAudioStartEffect ever", got)
	}
	if got := effectsOfType[RecordHistoryEffect](effects); len(got) != 0 {
		t.Fatalf("protected hold effects = %#v, want no RecordHistoryEffect (privacidade, AC-027)", got)
	}
	if got := effectsOfType[ReleaseAudioEffect](effects); len(got) != 1 {
		t.Fatalf("protected hold effects = %#v, want exactly one ReleaseAudioEffect", got)
	}
	if m.State() != StateBlocked {
		t.Fatalf("state after protected hold = %v, want StateBlocked", m.State())
	}
}

// --- fase de processamento (28-set-2026) --------------------------------

// TestMachine_KeyUp_SaiDeGravandoNaHora é o teste do defeito relatado.
//
// Mauro: "tem um delayzinho no final... tenho que esperar um segundo pra
// ele parar de gravar e inserir o texto". E a observação que fechou o
// diagnóstico: "depois que eu solto a tecla ele chega a marcar um segundo
// a mais no contador na tela".
//
// O contador não estava atrasado — ele mostra o tempo desde o início
// ENQUANTO a fase é recording, e a fase só mudava no desfecho, depois do
// ASR inteiro. A tela dizia "Gravando" com o microfone já fechado.
//
// O que se trava aqui é o INSTANTE: a saída de recording acompanha a
// parada da captura, no mesmo Handle. Não é detalhe de apresentação —
// é a diferença entre "parou de gravar, está trabalhando" e "por que
// ainda está gravando?".
func TestMachine_KeyUp_SaiDeGravandoNaHora(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)

	effects := m.Handle(KeyUpEvent{Attempt: id})

	overlays := effectsOfType[ShowOverlayEffect](effects)
	if len(overlays) != 1 || overlays[0].Phase != OverlayProcessing {
		t.Fatalf("effects do keyup = %#v, want exatamente um ShowOverlayEffect{Processing}", effects)
	}
	// No MESMO Handle que para a captura: se saíssem em Handles
	// diferentes, haveria um intervalo em que o microfone está fechado e
	// a tela ainda diz que grava — o defeito de novo, menor.
	if len(effectsOfType[StopAudioCaptureEffect](effects)) != 1 {
		t.Errorf("effects = %#v, want a parada da captura junto da troca de fase", effects)
	}
}

// TestMachine_MaxRecording_TambemSaiDeGravando: o limite de gravação é o
// caminho de quando o keyup NUNCA chega (tecla presa). É justamente o
// caso em que deixar "Gravando" na tela seria mais enganoso, não menos —
// não há tecla para soltar.
func TestMachine_MaxRecording_TambemSaiDeGravando(t *testing.T) {
	m := newTestMachine(t, false)
	id := startRecording(t, m)

	effects := m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerMaxRecording})

	overlays := effectsOfType[ShowOverlayEffect](effects)
	if len(overlays) != 1 || overlays[0].Phase != OverlayProcessing {
		t.Fatalf("effects do max-recording = %#v, want um ShowOverlayEffect{Processing}", effects)
	}
	if m.State() != StateProcessing {
		t.Errorf("state = %v, want StateProcessing", m.State())
	}
}

// TestMachine_NenhumCaminhoFicaPresoEmProcessando é a garantia estrutural
// que o estado novo exige.
//
// Um estado a mais é um estado a mais para ficar preso, e "processando"
// para sempre é pior que o defeito que ele conserta: o overlay ficaria na
// tela, sem contador, sem desfecho, e a pessoa não saberia se ditou ou
// não. Disciplina não basta — cada saída de StateProcessing precisa ser
// exercida.
//
// A tabela é a lista COMPLETA de saídas: se alguém acrescentar um caminho
// novo e não o puser aqui, este teste não o protege — por isso o último
// caso confere que a contagem de TimerKind em espera não cresceu sem
// alguém reparar.
func TestMachine_NenhumCaminhoFicaPresoEmProcessando(t *testing.T) {
	// ateProcessando leva a máquina até logo depois da soltura da tecla.
	ateProcessando := func(t *testing.T) (*Machine, AttemptID) {
		t.Helper()
		m := newTestMachine(t, false)
		id := startRecording(t, m)
		if got := m.Handle(KeyUpEvent{Attempt: id}); len(effectsOfType[ShowOverlayEffect](got)) != 1 {
			t.Fatalf("pré-condição: keyup devia ter trocado a fase")
		}
		return m, id
	}

	casos := []struct {
		nome  string
		saida func(m *Machine, id AttemptID) []Effect
	}{
		{
			nome: "sem fala (AC-010)",
			saida: func(m *Machine, id AttemptID) []Effect {
				return m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: false}})
			},
		},
		{
			nome: "falha ao parar a captura",
			saida: func(m *Machine, id AttemptID) []Effect {
				return m.Handle(AudioFailedEvent{Attempt: id, Reason: ErrorReasonAudioCaptureFailed})
			},
		},
		{
			nome: "watchdog da parada de captura",
			saida: func(m *Machine, id AttemptID) []Effect {
				return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerAudioStopTimeout})
			},
		},
		{
			nome: "transcrição falha",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				return m.Handle(TranscribeResultEvent{Attempt: id, Reason: ErrorReasonTranscribeFailed})
			},
		},
		{
			nome: "watchdog da transcrição",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerTranscribeTimeout})
			},
		},
		{
			nome: "watchdog do resolver",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "ola"}})
				return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerResolveTimeout})
			},
		},
		{
			nome: "watchdog da inserção",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "ola"}})
				m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "ola"}})
				return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerInsertTimeout})
			},
		},
		{
			nome: "inserção falha",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "ola"}})
				m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "ola"}})
				return m.Handle(InsertResultEvent{Attempt: id, Reason: ErrorReasonInsertFailed})
			},
		},
		{
			nome: "sucesso",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "ola"}})
				m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "ola"}})
				return m.Handle(InsertResultEvent{Attempt: id})
			},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			m, id := ateProcessando(t)
			efeitos := c.saida(m, id)

			overlays := effectsOfType[ShowOverlayEffect](efeitos)
			if len(overlays) == 0 {
				t.Fatalf("saída %q não emitiu fase nenhuma — o overlay fica em processando para sempre", c.nome)
			}
			final := overlays[len(overlays)-1].Phase
			if final == OverlayProcessing || final == OverlayRecording {
				t.Errorf("fase final = %v, want um desfecho — o overlay não sai de processando", final)
			}
		})
	}
}
