// Package agent reúne a orquestração do Windows Agent: o processo que
// roda na máquina do usuário e coordena hook de teclado, captura de
// áudio, overlay, chamada ao ASR, resolução contextual e inserção de
// texto no aplicativo focado.
//
// Sprint 0: pacote vazio de lógica (apenas este doc.go). A orquestração
// real entra em sprints futuros, conforme
// docs/ai-spec/13-implementation-plan.md.
//
// # Responsabilidade
//
// Ligar os demais componentes do Agent (internal/asr, internal/context,
// internal/dictionary, internal/resolver, internal/sync, internal/storage)
// em um fluxo único: tecla pressionada → captura → transcrição →
// resolução → inserção, conforme docs/ai-spec/04-system-design.md
// (seção "Hook").
//
// # Contratos previstos para a Fase 1
//
// Este pacote deve depender apenas de interfaces (nunca de
// implementações concretas de ASR, UI Automation, etc.), consumindo os
// contratos definidos em docs/ai-spec/18-tool-contract.md:
//   - AudioSource / AudioSession — captura de áudio em RAM, sem
//     persistência.
//   - VoiceActivityDetector — detecção de fala/silêncio.
//   - ASRProvider (via internal/asr) — transcrição.
//   - ContextProvider (via internal/context, pacote uiacontext) —
//     snapshot efêmero de contexto.
//   - Resolver (via internal/resolver) — resolução Literal/Smart/Polish.
//   - TextInserter — inserção via UI Automation com fallback de
//     clipboard.
//
// Nenhuma dessas interfaces é declarada ainda; isso acontece quando a
// lógica correspondente for implementada.
package agent
