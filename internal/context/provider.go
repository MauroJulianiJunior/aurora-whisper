package uiacontext

import (
	"context"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// FocusTarget identifica o controle com foco no momento da captura. É o
// mesmo tipo já definido pelo núcleo do ditado (internal/agent/dictation,
// ADR-007) — não é redeclarado aqui como um struct novo e paralelo, porque
// FocusTarget já carrega exatamente o que este pacote precisa (processo,
// tipo de controle e o flag Protected que decide o bloqueio de campo
// sensível) e é o mesmo valor que o runtime do Agent vai repassar do hook
// até aqui. Duplicar o tipo criaria duas noções de "alvo com foco"
// divergentes no mesmo sistema.
type FocusTarget = dictation.FocusTarget

// ContextProvider é o contrato de captura de contexto efêmero de janela,
// conforme docs/ai-spec/18-tool-contract.md:
//
//	type ContextProvider interface {
//	    Snapshot(ctx context.Context, target FocusTarget) (ContextSnapshot, error)
//	}
//
// Implementações permitidas: app/processo, tipo de controle, seleção,
// prefixo/sufixo próximos ao caret, labels acessíveis, window title em
// memória. Nunca: screenshot, OCR, leitura arbitrária de memória
// (04-system-design.md seção "Context Engine", ADR-006).
//
// O texto devolvido (seleção, prefixo/sufixo, labels) é dado, nunca
// instrução: nenhuma implementação deste contrato pode interpretar,
// obedecer ou reagir a comandos embutidos nesse texto (THR-008,
// docs/ai-spec/17-prompt-spec.md). Cabe a quem consome o ContextSnapshot
// manter essa fronteira.
//
// Quando target.Protected é true, Snapshot não deve capturar nada: nem
// texto próximo, nem seleção, nem labels (AC-036, TEST-009). O snapshot
// devolvido deve ter Blocked == true e os demais campos no zero value.
type ContextProvider interface {
	Snapshot(ctx context.Context, target FocusTarget) (ContextSnapshot, error)
}
