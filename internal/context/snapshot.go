package uiacontext

import (
	"encoding/json"
	"errors"
)

// Limites explícitos de tamanho do contexto capturado (09-security-privacy.md:
// contexto deve ser "bounded/minimized"). Nenhum campo textual do
// ContextSnapshot pode crescer além destes limites — a construção do
// snapshot (ver newSnapshot) trunca antes de devolver o valor ao chamador.
const (
	// MaxNearCaretRunes limita, em runes (não bytes, para não partir um
	// caractere multibyte ao meio), o prefixo e o sufixo de texto próximos
	// ao caret.
	MaxNearCaretRunes = 200
	// MaxSelectionRunes limita o texto selecionado capturado.
	MaxSelectionRunes = 500
	// MaxAccessibleLabels limita quantos labels acessíveis o snapshot
	// carrega.
	MaxAccessibleLabels = 10
	// MaxAccessibleLabelRunes limita o tamanho de cada label acessível.
	MaxAccessibleLabelRunes = 120
)

// ErrSnapshotNotSerializable é o erro que ContextSnapshot.MarshalJSON
// sempre devolve. Existir um valor de erro nomeado (em vez de um texto
// solto) permite que chamadores façam errors.Is caso precisem distinguir
// esta falha estrutural de qualquer outra.
var ErrSnapshotNotSerializable = errors.New(
	"uiacontext: ContextSnapshot não é serializável por design; contexto é " +
		"dado efêmero e não deve ser persistido nem logado (ADR-006, AC-027)",
)

// ContextSnapshot é o retrato pontual e não persistente do controle com
// foco no momento da captura (docs/ai-spec/18-tool-contract.md,
// docs/ai-spec/04-system-design.md seção "Context Engine").
//
// # Invariante estrutural: não é serializável
//
// 18-tool-contract.md e o ADR-006/ADR-007 exigem que ContextSnapshot nunca
// implemente persistência. Isso não é um pedido educado respeitado por
// convenção — é forçado estruturalmente: ContextSnapshot implementa
// json.Marshaler devolvendo sempre erro (MarshalJSON) e fmt.Stringer
// devolvendo sempre uma representação redigida (String). Qualquer
// tentativa de logar com %v/%s ou de serializar via encoding/json — direta
// ou embutida em outro struct — falha em vez de vazar em silêncio
// (AC-027, THR-007).
//
// # Campo protegido
//
// Quando Blocked é true, o alvo era um controle protegido (senha) e nenhum
// dos demais campos foi preenchido: todos ficam no zero value, porque
// nenhuma captura foi tentada (AC-036, TEST-009).
type ContextSnapshot struct {
	// Blocked indica que o alvo focado era protegido (FocusTarget.Protected)
	// e que, portanto, nada foi capturado. Quando true, todo o restante do
	// struct permanece no zero value.
	Blocked bool

	// AppName é o nome do processo/aplicativo com foco (ex.: "notepad.exe").
	AppName string
	// ControlType é o tipo do controle focado, conforme relatado pela UI
	// Automation (ex.: "Edit", "Document").
	ControlType string
	// WindowTitle fica só em memória quando necessário
	// (04-system-design.md: "window title só em memória se necessário").
	WindowTitle string
	// Selection é o texto atualmente selecionado no controle, truncado a
	// MaxSelectionRunes.
	Selection string
	// PrefixNearCaret é o texto imediatamente anterior ao caret, truncado a
	// MaxNearCaretRunes.
	PrefixNearCaret string
	// SuffixNearCaret é o texto imediatamente posterior ao caret, truncado a
	// MaxNearCaretRunes.
	SuffixNearCaret string
	// AccessibleLabels são labels acessíveis relevantes (ex.: label do
	// campo, nome do formulário), limitados a MaxAccessibleLabels itens de
	// até MaxAccessibleLabelRunes runes cada.
	AccessibleLabels []string
}

// MarshalJSON nunca produz JSON. ContextSnapshot é dado efêmero e não deve
// ser persistido nem transitar por nenhum caminho que dependa de
// serialização (logs estruturados, banco, fila, cache). Isso vale mesmo
// quando o snapshot está embutido como campo de outro struct: o erro se
// propaga para o Marshal do struct pai, então nenhum wrapper "esquece" a
// regra por acidente.
func (ContextSnapshot) MarshalJSON() ([]byte, error) {
	return nil, ErrSnapshotNotSerializable
}

// String devolve uma representação redigida: nunca inclui o conteúdo
// capturado (app, controle, seleção, texto próximo ao caret ou labels).
// Isso garante que um %v/%s acidental em log (fmt.Println, log.Printf,
// erro envolvendo o snapshot) não vaze contexto — apenas o estado de
// bloqueio, que não é sensível.
func (s ContextSnapshot) String() string {
	if s.Blocked {
		return "uiacontext.ContextSnapshot{blocked}"
	}
	return "uiacontext.ContextSnapshot{redacted}"
}

// blockedSnapshot é o valor devolvido quando o alvo é protegido: nada foi
// capturado, então tudo fica no zero value exceto o marcador Blocked.
func blockedSnapshot() ContextSnapshot {
	return ContextSnapshot{Blocked: true}
}

// newSnapshot monta um ContextSnapshot a partir de dados brutos, aplicando
// os limites de tamanho (MaxNearCaretRunes, MaxSelectionRunes,
// MaxAccessibleLabels/MaxAccessibleLabelRunes). É o único construtor usado
// tanto pelo FakeContextProvider quanto, futuramente, por qualquer provider
// real — garantindo que o truncamento nunca seja pulado por um caminho de
// código esquecido.
func newSnapshot(app, controlType, windowTitle, selection, prefix, suffix string, labels []string) ContextSnapshot {
	return ContextSnapshot{
		AppName:          app,
		ControlType:      controlType,
		WindowTitle:      windowTitle,
		Selection:        truncateRunes(selection, MaxSelectionRunes),
		PrefixNearCaret:  truncateRunes(prefix, MaxNearCaretRunes),
		SuffixNearCaret:  truncateRunes(suffix, MaxNearCaretRunes),
		AccessibleLabels: truncateLabels(labels, MaxAccessibleLabels, MaxAccessibleLabelRunes),
	}
}

// truncateRunes corta s para, no máximo, max runes. A truncagem é por rune
// (não por byte) para nunca partir um caractere multibyte (acentuação,
// emoji) ao meio.
func truncateRunes(s string, max int) string {
	if s == "" || max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// truncateLabels limita labels a maxCount itens, cada um truncado a
// maxRunesEach runes. Devolve uma cópia nova: o slice de entrada nunca é
// retido nem mutado.
func truncateLabels(labels []string, maxCount, maxRunesEach int) []string {
	if len(labels) == 0 {
		return nil
	}
	n := len(labels)
	if n > maxCount {
		n = maxCount
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = truncateRunes(labels[i], maxRunesEach)
	}
	return out
}

// garante em tempo de compilação que ContextSnapshot satisfaz
// json.Marshaler (sempre com erro) — ver comentário de MarshalJSON acima.
var _ json.Marshaler = ContextSnapshot{}
