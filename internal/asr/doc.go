// Package asr define a abstração de reconhecimento de fala (Automatic
// Speech Recognition) usada pelo Agent e pelo backend cloud. O domínio
// nunca deve ser acoplado a um modelo específico (Whisper, Parakeet ou
// outro) — ver docs/adr/ADR-003-asr-provider-abstraction.md.
//
// Fase 1 (F1-C): validação de contratos, com mocks determinísticos e
// síncronos (subpacote internal/asr/mock). Nenhum modelo real, nenhuma
// chamada de rede.
//
// # Responsabilidade
//
// Este pacote declara:
//
//   - ASRProvider (provider.go) — o contrato único que todo provider
//     implementa, conforme docs/ai-spec/18-tool-contract.md.
//   - Os tipos de apoio (types.go): AudioBuffer, ASRCapabilities,
//     ASROptions, ASRResult, Confidence, Span, Alternative.
//   - Engine (engine.go) — o motor de policy que decide quais provider(s)
//     chamar para uma Policy (local_only, local_then_cloud, cloud_only),
//     conforme docs/ai-spec/04-system-design.md (seção "ASR provider") e
//     AC-030/031/032.
//
// # Contrato
//
// Conforme docs/ai-spec/18-tool-contract.md:
//
//	type ASRProvider interface {
//	    ID() string
//	    Capabilities() ASRCapabilities
//	    Warmup(context.Context) error
//	    Transcribe(context.Context, AudioBuffer, ASROptions) (ASRResult, error)
//	}
//
// ASRCapabilities cobre streaming, confidence, timestamps, nbest,
// languages e acceleration. ASRResult contém texto, idioma, timestamps
// (spans), confidence, alternatives (N-best), model e processing time.
//
// # Confidence é opcional, de verdade
//
// Confidence é um par (Value, Present), não um float simples: "provider
// não forneceu confidence" (Present == false) precisa ser distinguível de
// "provider forneceu confidence zero" (Present == true, Value == 0). O
// sistema nunca fabrica um score e o apresenta como se tivesse vindo do
// ASR (docs/ai-spec/16-ai-behavior-spec.md, "Uncertainty") — sinais
// derivados localmente (fonético, OOV, conflito de contexto) pertencem a
// outra camada (o resolver), com procedência própria, nunca disfarçados
// de ASRResult.Confidence.
package asr
