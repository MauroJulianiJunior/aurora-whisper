package fakes

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

// transcribeOutcome descreve como FakeTranscriber deve responder a uma
// tentativa específica. respond=false modela silêncio total do runtime,
// para exercitar o watchdog TimerTranscribeTimeout (ADR-007, seção 6).
type transcribeOutcome struct {
	respond bool
	result  dictation.ASRResultDTO
	reason  dictation.ErrorReason
}

// FakeTranscriber é o dublê da porta de ASR: configurável por tentativa
// para devolver um resultado, falhar, ou nunca responder.
type FakeTranscriber struct {
	plans map[dictation.AttemptID]transcribeOutcome
	// Calls registra, em ordem, cada tentativa para a qual Transcribe foi
	// chamado — usado para provar que o ASR nunca é acionado quando não
	// há fala válida (AC-010).
	Calls []dictation.AttemptID
}

// NewFakeTranscriber cria um FakeTranscriber sem nenhum plano configurado;
// tentativas não configuradas usam o default de sucesso em Transcribe.
func NewFakeTranscriber() *FakeTranscriber {
	return &FakeTranscriber{plans: make(map[dictation.AttemptID]transcribeOutcome)}
}

// ExpectSuccess configura Transcribe(attempt) para devolver text/confidence.
func (f *FakeTranscriber) ExpectSuccess(attempt dictation.AttemptID, text string, confidence float64) {
	f.plans[attempt] = transcribeOutcome{respond: true, result: dictation.ASRResultDTO{Text: text, Confidence: confidence}}
}

// ExpectFailure configura Transcribe(attempt) para falhar com reason.
func (f *FakeTranscriber) ExpectFailure(attempt dictation.AttemptID, reason dictation.ErrorReason) {
	f.plans[attempt] = transcribeOutcome{respond: true, reason: reason}
}

// ExpectSilence configura Transcribe(attempt) para nunca responder.
func (f *FakeTranscriber) ExpectSilence(attempt dictation.AttemptID) {
	f.plans[attempt] = transcribeOutcome{respond: false}
}

// Transcribe é chamado pelo Runtime ao aplicar um TranscribeEffect.
func (f *FakeTranscriber) Transcribe(attempt dictation.AttemptID, audio dictation.AudioBufferRef) (dictation.Event, bool) {
	f.Calls = append(f.Calls, attempt)

	plan, ok := f.plans[attempt]
	if !ok {
		plan = transcribeOutcome{respond: true, result: dictation.ASRResultDTO{Text: "resultado padrao", Confidence: 0.9}}
	}
	if !plan.respond {
		return nil, false
	}
	return dictation.TranscribeResultEvent{Attempt: attempt, Result: plan.result, Reason: plan.reason}, true
}
