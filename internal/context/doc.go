// Package uiacontext captura o contexto efêmero da aplicação em foco
// (via UI Automation) para alimentar o Resolver, conforme
// docs/ai-spec/04-system-design.md (seção "Context Engine") e
// docs/adr/ADR-006-context-without-screenshots.md.
//
// # Por que o pacote se chama "uiacontext" e não "context"
//
// O diretório é internal/context/, porque esse é o layout de pacotes
// especificado em CLAUDE.md e deve ser respeitado literalmente. No
// entanto, o nome do pacote Go declarado aqui dentro é uiacontext, não
// context: se fosse "context", qualquer arquivo que precisasse importar
// tanto este pacote quanto o pacote context da biblioteca padrão (o que
// é praticamente garantido, já que toda função deste domínio recebe um
// context.Context) teria uma colisão de identificador no import,
// forçando alias em todo call site. Nomear o pacote uiacontext evita
// esse atrito de forma permanente e ainda deixa claro que o contexto
// aqui é especificamente o de UI Automation, e não o context.Context da
// stdlib.
//
// F1-C (fase de validação de contratos, com mocks): o contrato
// ContextProvider e o ContextSnapshot estão implementados nos arquivos
// deste pacote (provider.go, snapshot.go, fake.go). Nenhuma implementação
// real de UI Automation existe ainda — apenas o contrato e um
// FakeContextProvider determinístico, que é o que os demais blocos (em
// especial o Resolver) usam em teste.
//
// # Responsabilidade
//
// Fornecer um snapshot pontual e não persistente do estado da aplicação
// focada: nome do app/processo, tipo de controle, seleção,
// prefixo/sufixo próximos ao caret e labels acessíveis. Window title
// pode ficar em memória quando necessário. Está fora do escopo deste
// pacote: screenshot, OCR, leitura arbitrária de memória e qualquer
// leitura de controle protegido/senha (ver TestNoPixelCaptureOrTextRecognition, que
// varre o pacote em busca de import/símbolo proibido, AC-026).
//
// # Contrato (docs/ai-spec/18-tool-contract.md)
//
//	type ContextProvider interface {
//	    Snapshot(ctx context.Context, target FocusTarget) (ContextSnapshot, error)
//	}
//
// FocusTarget é um alias para dictation.FocusTarget
// (internal/agent/dictation) — o mesmo tipo que o núcleo do ditado já usa
// para descrever o controle com foco no keydown (ADR-007). ContextSnapshot
// não implementa persistência: isso é estrutural, não uma convenção.
// ContextSnapshot.MarshalJSON sempre devolve erro e ContextSnapshot.String
// nunca revela o conteúdo capturado (AC-027).
//
// Texto próximo ao caret, seleção e labels são sempre dado, nunca
// instrução: nenhum código deste pacote interpreta o conteúdo capturado
// (THR-008).
package uiacontext
