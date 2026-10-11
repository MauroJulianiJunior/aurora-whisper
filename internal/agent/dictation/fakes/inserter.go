package fakes

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

// ClipboardOp registra um passo da sequência snapshot -> set -> paste ->
// restore que FakeInserter executa no caminho de fallback via clipboard
// (AC-009).
type ClipboardOp string

const (
	ClipSnapshot ClipboardOp = "snapshot"
	ClipSet      ClipboardOp = "set"
	ClipPaste    ClipboardOp = "paste"
	ClipRestore  ClipboardOp = "restore"
)

// insertOutcome descreve como FakeInserter.Insert deve se comportar para
// uma tentativa específica.
type insertOutcome int

const (
	// insertViaUIA: sucesso direto (SetValue/TextPattern), sem tocar no
	// clipboard.
	insertViaUIA insertOutcome = iota
	// insertViaClipboard: UIA falhou, o fallback por clipboard é usado e
	// tem sucesso.
	insertViaClipboard
	// insertTotalFailure: nem UIA nem o fallback por clipboard conseguem
	// inserir (ex.: app que bloqueia paste); o clipboard ainda assim
	// precisa ser restaurado.
	insertTotalFailure
	// insertHang: o runtime nunca responde, para exercitar o watchdog
	// TimerInsertTimeout (ADR-007, seção 6).
	insertHang
)

// InsertCall registra uma chamada a Insert, para os testes verificarem
// exatamente o que foi pedido ao campo focado (AC-008).
type InsertCall struct {
	Attempt dictation.AttemptID
	Text    string
	Focus   dictation.FocusTarget
}

// FakeInserter é o dublê da porta de inserção de texto. Mantém um
// clipboard simulado e registra cada operação sobre ele, para que os
// testes possam provar que o conteúdo anterior do usuário é sempre
// restaurado — inclusive quando a inserção falha (AC-009).
type FakeInserter struct {
	clipboard string
	ops       []ClipboardOp
	plans     map[dictation.AttemptID]insertOutcome

	// Calls registra, em ordem, cada chamada a Insert.
	Calls []InsertCall
}

// NewFakeInserter cria um FakeInserter com clipboard vazio; tentativas não
// configuradas usam o default de sucesso direto via UIA.
func NewFakeInserter() *FakeInserter {
	return &FakeInserter{plans: make(map[dictation.AttemptID]insertOutcome)}
}

// SeedClipboard define o que o clipboard do sistema "já continha" antes de
// qualquer inserção, para que um teste possa depois verificar que foi
// restaurado.
func (f *FakeInserter) SeedClipboard(content string) { f.clipboard = content }

// ClipboardContent devolve o conteúdo atual do clipboard simulado.
func (f *FakeInserter) ClipboardContent() string { return f.clipboard }

// Ops devolve a sequência de operações de clipboard registradas, em ordem.
func (f *FakeInserter) Ops() []ClipboardOp {
	return append([]ClipboardOp(nil), f.ops...)
}

// ExpectDirectSuccess configura Insert(attempt) para ter sucesso via UIA,
// sem tocar no clipboard.
func (f *FakeInserter) ExpectDirectSuccess(attempt dictation.AttemptID) {
	f.plans[attempt] = insertViaUIA
}

// ExpectClipboardFallbackSuccess configura Insert(attempt) para simular
// uma falha de UIA cujo fallback via clipboard tem sucesso.
func (f *FakeInserter) ExpectClipboardFallbackSuccess(attempt dictation.AttemptID) {
	f.plans[attempt] = insertViaClipboard
}

// ExpectTotalFailure configura Insert(attempt) para falhar mesmo após
// tentar o fallback via clipboard (ex.: app que bloqueia paste — edge
// case). O clipboard ainda é restaurado ao final.
func (f *FakeInserter) ExpectTotalFailure(attempt dictation.AttemptID) {
	f.plans[attempt] = insertTotalFailure
}

// ExpectHang configura Insert(attempt) para nunca responder.
func (f *FakeInserter) ExpectHang(attempt dictation.AttemptID) {
	f.plans[attempt] = insertHang
}

// Insert é chamado pelo Runtime ao aplicar um InsertEffect.
func (f *FakeInserter) Insert(attempt dictation.AttemptID, text string, focus dictation.FocusTarget) (dictation.Event, bool) {
	f.Calls = append(f.Calls, InsertCall{Attempt: attempt, Text: text, Focus: focus})

	outcome, ok := f.plans[attempt]
	if !ok {
		outcome = insertViaUIA
	}

	switch outcome {
	case insertViaUIA:
		return dictation.InsertResultEvent{Attempt: attempt, Result: dictation.InsertResultDTO{Success: true}}, true

	case insertViaClipboard:
		f.pasteViaClipboard(text)
		return dictation.InsertResultEvent{Attempt: attempt, Result: dictation.InsertResultDTO{Success: true}}, true

	case insertTotalFailure:
		f.pasteViaClipboard(text)
		return dictation.InsertResultEvent{Attempt: attempt, Result: dictation.InsertResultDTO{Success: false}}, true

	case insertHang:
		return nil, false

	default:
		return dictation.InsertResultEvent{Attempt: attempt, Result: dictation.InsertResultDTO{Success: false}}, true
	}
}

// pasteViaClipboard executa a sequência snapshot -> set -> paste ->
// restore contra o clipboard simulado. É chamada tanto no caminho de
// sucesso via fallback quanto no de falha total: o restore acontece
// sempre, porque colar (com sucesso ou não) nunca deve ser o motivo de o
// usuário perder o que já tinha copiado (AC-009).
func (f *FakeInserter) pasteViaClipboard(text string) {
	previous := f.clipboard
	f.ops = append(f.ops, ClipSnapshot)
	f.ops = append(f.ops, ClipSet)
	f.clipboard = text
	f.ops = append(f.ops, ClipPaste)
	f.ops = append(f.ops, ClipRestore)
	f.clipboard = previous
}
