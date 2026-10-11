package asr

import "context"

// ASRProvider é o contrato único que todo provedor de reconhecimento de
// fala implementa — mock, local ou cloud_gateway — conforme
// docs/ai-spec/18-tool-contract.md e ADR-003. O domínio (Engine, Resolver)
// nunca conhece o provider concreto por trás desta interface (AC-029).
type ASRProvider interface {
	// ID identifica o provider (ex.: "mock-confidence", "whisper-local",
	// "cloud-gateway"). Usado em histórico e logs — nunca inclui segredos.
	ID() string

	// Capabilities descreve o que este provider suporta. Deve ser estável
	// entre chamadas (não depende do estado da última transcrição).
	Capabilities() ASRCapabilities

	// Warmup prepara o provider (ex.: carregar modelo em memória) antes do
	// primeiro uso real. Providers sem custo de inicialização podem
	// implementar como no-op. Deve respeitar ctx.
	Warmup(ctx context.Context) error

	// Transcribe transcreve audio conforme opts. O provider NUNCA deve
	// persistir audio em disco, nem logar seu conteúdo (AC-028, NFR-010) e
	// NUNCA deve chamar audio.Release — isso é responsabilidade exclusiva
	// de quem possui o buffer (ver AudioBuffer.Release).
	Transcribe(ctx context.Context, audio AudioBuffer, opts ASROptions) (ASRResult, error)
}
