package fakes

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

// resolveOutcome descreve como FakeResolver deve responder a uma tentativa
// específica. respond=false modela silêncio total do runtime, para
// exercitar o watchdog TimerResolveTimeout (ADR-007, seção 6).
type resolveOutcome struct {
	respond bool
	result  dictation.ResolveResultDTO
	reason  dictation.ErrorReason
}

// FakeResolver é o dublê da porta de resolução (Literal/Smart/Polish).
type FakeResolver struct {
	plans map[dictation.AttemptID]resolveOutcome
	// Calls registra, em ordem, cada tentativa para a qual Resolve foi
	// chamado.
	Calls []dictation.AttemptID
}

// NewFakeResolver cria um FakeResolver sem nenhum plano configurado;
// tentativas não configuradas usam o default de Resolve, que apenas
// repassa o texto bruto do ASR (equivalente à política Literal).
func NewFakeResolver() *FakeResolver {
	return &FakeResolver{plans: make(map[dictation.AttemptID]resolveOutcome)}
}

// ExpectSuccess configura Resolve(attempt) para devolver text.
func (f *FakeResolver) ExpectSuccess(attempt dictation.AttemptID, text string) {
	f.plans[attempt] = resolveOutcome{respond: true, result: dictation.ResolveResultDTO{Text: text}}
}

// ExpectFailure configura Resolve(attempt) para falhar com reason.
func (f *FakeResolver) ExpectFailure(attempt dictation.AttemptID, reason dictation.ErrorReason) {
	f.plans[attempt] = resolveOutcome{respond: true, reason: reason}
}

// ExpectSilence configura Resolve(attempt) para nunca responder.
func (f *FakeResolver) ExpectSilence(attempt dictation.AttemptID) {
	f.plans[attempt] = resolveOutcome{respond: false}
}

// Resolve é chamado pelo Runtime ao aplicar um ResolveEffect.
func (f *FakeResolver) Resolve(attempt dictation.AttemptID, asr dictation.ASRResultDTO, focus dictation.FocusTarget) (dictation.Event, bool) {
	f.Calls = append(f.Calls, attempt)

	plan, ok := f.plans[attempt]
	if !ok {
		plan = resolveOutcome{respond: true, result: dictation.ResolveResultDTO{Text: asr.Text}}
	}
	if !plan.respond {
		return nil, false
	}
	return dictation.ResolveResultEvent{Attempt: attempt, Result: plan.result, Reason: plan.reason}, true
}
