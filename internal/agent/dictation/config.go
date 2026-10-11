package dictation

import (
	"errors"
	"fmt"
	"time"
)

// MinHoldThreshold e MaxHoldThreshold delimitam a faixa aceita para
// Config.HoldThreshold (AC-044).
const (
	MinHoldThreshold = 300 * time.Millisecond
	MaxHoldThreshold = 2000 * time.Millisecond

	// DefaultMaxRecording é o limite de gravação sugerido pela spec
	// (07-flows.md, "Timeouts"). Usado quando Config.MaxRecording é zero.
	DefaultMaxRecording = 120 * time.Second
)

// Erros de validação de Config, testáveis com errors.Is.
var (
	// ErrPureModifierKey: a tecla principal não pode ser um modificador
	// puro (Shift/Ctrl/Alt/Win, qualquer variante L/R) — AC-004.
	ErrPureModifierKey = errors.New("dictation: main key cannot be a pure modifier key")
	// ErrThresholdOutOfRange: o threshold de hold precisa estar entre
	// MinHoldThreshold e MaxHoldThreshold — AC-044.
	ErrThresholdOutOfRange = errors.New("dictation: hold threshold out of range [300ms, 2000ms]")

	// ErrModificadorSolto: um gatilho por combinação precisa de pelo menos
	// DOIS modificadores. Um só ficaria pressionado o tempo todo em uso
	// normal e dispararia o ditado sem parar — que é exatamente o que o
	// REQ-004 existe para impedir.
	ErrModificadorSolto = errors.New("dictation: gatilho por modificadores exige pelo menos dois")
)

// contarModificadores conta quantos modificadores distintos estão em m.
func contarModificadores(m Modifiers) int {
	n := 0
	for _, bit := range []Modifiers{ModShift, ModControl, ModAlt, ModMeta} {
		if m.Has(bit) {
			n++
		}
	}
	return n
}

// Config são os parâmetros configuráveis do ditado (ADR-005, AC-044).
type Config struct {
	// Key é a tecla principal do ditado. KeyUnknown significa que o
	// gatilho é a COMBINAÇÃO em Modifiers, sem tecla principal.
	Key KeyCode
	// Modifiers é o conjunto exigido quando Key == KeyUnknown (ex.:
	// ModControl|ModMeta para Ctrl+Win). Ignorado quando há tecla
	// principal — nesse caso o gatilho é a tecla SEM modificadores, e
	// qualquer modificador vira bypass (AC-045).
	Modifiers Modifiers
	// HoldThreshold é a duração mínima de hold para iniciar gravação
	// (default: 700ms). Abaixo disso, tap normal (AC-001).
	HoldThreshold time.Duration
	// BeepEnabled controla se PlayBeepEffect é emitido ao entrar em
	// gravação (AC-006).
	BeepEnabled bool
	// MaxRecording limita a duração de uma gravação. Zero significa
	// DefaultMaxRecording. É a rede de segurança contra keyup perdido:
	// sem ela a sessão nunca voltaria de StateRecording.
	MaxRecording time.Duration
}

// withDefaults devolve uma cópia de c com os campos opcionais preenchidos.
// Mantém Config retrocompatível: quem não informa MaxRecording recebe o
// default da spec em vez de um limite zero (que desarmaria a proteção).
func (c Config) withDefaults() Config {
	if c.MaxRecording <= 0 {
		c.MaxRecording = DefaultMaxRecording
	}
	return c
}

// Validate aplica as regras de configuração exigidas pelos critérios de
// aceite AC-004 e AC-044. É pura: nenhuma dependência externa, apenas os
// campos de c.
func (c Config) Validate() error {
	if c.Key == KeyUnknown {
		// Gatilho por combinação de modificadores.
		//
		// O REQ-004 diz, como Must, que "modificadores puros não podem ser
		// tecla principal". A intenção dele é clara e continua valendo:
		// Ctrl sozinho fica pressionado o tempo todo em uso normal, e o
		// ditado dispararia sem parar.
		//
		// Uma COMBINAÇÃO de dois ou mais não tem esse problema — ninguém
		// segura Ctrl+Win por 700 ms sem querer — e resolve um defeito que
		// a tecla de letra não resolve: com letra, o ditado precisa engolir
		// e devolver o caractere, e as teclas seguintes chegam antes da
		// devolvida ("freio" saía "refio"). Ver
		// docs/adr/ADR-013-gatilho-por-combinacao-de-modificadores.md.
		//
		// Por isso a regra aqui é "pelo menos dois", não "nenhum": preserva
		// o que o REQ-004 protege e libera o que ele não previu.
		if contarModificadores(c.Modifiers) < 2 {
			return fmt.Errorf("%w: modifiers=%d", ErrModificadorSolto, c.Modifiers)
		}
		if c.HoldThreshold < MinHoldThreshold || c.HoldThreshold > MaxHoldThreshold {
			return fmt.Errorf("%w: got %s", ErrThresholdOutOfRange, c.HoldThreshold)
		}
		return nil
	}
	if c.Key.IsModifier() {
		return fmt.Errorf("%w: key=%d", ErrPureModifierKey, c.Key)
	}
	if c.HoldThreshold < MinHoldThreshold || c.HoldThreshold > MaxHoldThreshold {
		return fmt.Errorf("%w: got %s", ErrThresholdOutOfRange, c.HoldThreshold)
	}
	return nil
}
