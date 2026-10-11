package uiacontext

import "context"

// FakeContextProvider é o dublê determinístico de ContextProvider usado
// pelos testes deste pacote e, principalmente, pelos testes do Resolver
// (internal/resolver), que consome ContextProvider como dependência para a
// política Smart. Não faz nenhuma chamada real de UI Automation: apenas
// devolve os dados configurados nos seus próprios campos.
type FakeContextProvider struct {
	// AppName, ControlType, WindowTitle, Selection, PrefixNearCaret,
	// SuffixNearCaret e AccessibleLabels são os dados "capturados" que
	// Snapshot devolve quando o alvo não é protegido. Os limites de
	// tamanho (MaxNearCaretRunes, MaxSelectionRunes, MaxAccessibleLabels)
	// são aplicados por Snapshot como aplicaria um provider real — o
	// fake não é um atalho que pula o truncamento.
	AppName          string
	ControlType      string
	WindowTitle      string
	Selection        string
	PrefixNearCaret  string
	SuffixNearCaret  string
	AccessibleLabels []string

	// Protected, quando true, força Snapshot a devolver um snapshot
	// vazio/bloqueado (ContextSnapshot{Blocked: true}) independentemente
	// do FocusTarget recebido em cada chamada. Serve para os testes de
	// outros blocos configurarem o cenário "campo protegido" sem precisar
	// montar um FocusTarget manualmente. Já FocusTarget.Protected == true
	// passado a Snapshot também bloqueia por si só — as duas fontes
	// funcionam em OR, nunca destravando uma a outra (defesa em
	// profundidade para AC-036).
	Protected bool

	// Err, quando não nil, faz Snapshot devolver esse erro em vez de um
	// snapshot, para exercitar o caminho de falha do chamador.
	Err error

	// Calls registra, em ordem, cada FocusTarget para o qual Snapshot foi
	// chamado.
	Calls []FocusTarget
}

// Snapshot implementa ContextProvider.
func (f *FakeContextProvider) Snapshot(ctx context.Context, target FocusTarget) (ContextSnapshot, error) {
	f.Calls = append(f.Calls, target)

	if err := ctx.Err(); err != nil {
		return ContextSnapshot{}, err
	}
	if f.Err != nil {
		return ContextSnapshot{}, f.Err
	}
	if target.Protected || f.Protected {
		return blockedSnapshot(), nil
	}
	return newSnapshot(f.AppName, f.ControlType, f.WindowTitle, f.Selection, f.PrefixNearCaret, f.SuffixNearCaret, f.AccessibleLabels), nil
}

// garante em tempo de compilação que FakeContextProvider satisfaz
// ContextProvider.
var _ ContextProvider = (*FakeContextProvider)(nil)
