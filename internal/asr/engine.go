package asr

import (
	"context"
	"errors"
	"strings"
)

// Policy escolhe quais providers o Engine pode usar para uma transcrição,
// conforme docs/ai-spec/04-system-design.md ("ASR provider" → "Policies")
// e AC-030/031/032.
type Policy int

const (
	// PolicyLocalOnly usa exclusivamente o provider local. O gateway cloud
	// NUNCA é chamado, em nenhuma circunstância — nem em caso de erro ou
	// resultado inadequado (AC-030, AC-031).
	PolicyLocalOnly Policy = iota
	// PolicyLocalThenCloud tenta o provider local primeiro; se o resultado
	// for inadequado (ver AdequateFunc) e o gateway cloud estiver
	// disponível e autenticado, reaproveita o MESMO AudioBuffer em RAM na
	// chamada ao cloud (TEST-010).
	PolicyLocalThenCloud
	// PolicyCloudOnly usa exclusivamente o gateway cloud, que precisa estar
	// autenticado (AC-032). O provider local nunca é chamado.
	PolicyCloudOnly
)

var (
	// ErrNoLocalProvider é retornado quando a policy exige um provider
	// local (PolicyLocalOnly, PolicyLocalThenCloud) e Engine.Local é nil.
	ErrNoLocalProvider = errors.New("asr: policy requires a local provider, none configured")
	// ErrNoCloudProvider é retornado quando PolicyCloudOnly é usada e
	// Engine.Cloud é nil.
	ErrNoCloudProvider = errors.New("asr: policy requires a cloud gateway, none configured")
	// ErrCloudNotAuthenticated é retornado por PolicyCloudOnly quando não
	// há credencial de gateway cloud configurada (AC-032). A falha é
	// limpa: o gateway cloud nunca chega a ser chamado.
	ErrCloudNotAuthenticated = errors.New("asr: cloud gateway requires authentication, no credential configured")
	// ErrUnknownPolicy é retornado para um valor de Policy fora do
	// enumerado acima.
	ErrUnknownPolicy = errors.New("asr: unknown policy")
)

// CloudCredential é a credencial exigida para autenticar no gateway cloud
// (AC-032). Vazia (zero value) significa "sem credencial configurada".
type CloudCredential struct {
	Token string
}

func (c CloudCredential) present() bool { return c.Token != "" }

// AdequateFunc decide se um ASRResult é "adequado", isto é, se
// PolicyLocalThenCloud pode aceitá-lo sem tentar o gateway cloud. O
// critério precisa ser explícito e testável (AC-030): usar DefaultAdequate
// ou fornecer o próprio, mas nunca inventar um confidence que o provider
// não forneceu para alimentar essa decisão.
type AdequateFunc func(result ASRResult, err error) bool

// DefaultAdequate constrói o critério padrão de adequação (TEST-010,
// AC-030): um resultado é inadequado se o provider retornou erro, se o
// texto reconhecido é vazio (após aparar espaços) ou — SOMENTE quando o
// provider FORNECE confidence — se o confidence está abaixo de threshold.
// Quando o provider não fornece confidence (result.Confidence.Present ==
// false), esse sinal simplesmente não participa do critério: não fabricamos
// confidence para o provider (docs/ai-spec/16-ai-behavior-spec.md).
func DefaultAdequate(threshold float64) AdequateFunc {
	return func(result ASRResult, err error) bool {
		if err != nil {
			return false
		}
		if strings.TrimSpace(result.Text) == "" {
			return false
		}
		if result.Confidence.Present && result.Confidence.Value < threshold {
			return false
		}
		return true
	}
}

// Engine é o motor de policy: decide, a partir de uma Policy, quais
// ASRProvider(s) chamar para uma transcrição, respeitando os limites de
// AC-030/031/032.
type Engine struct {
	// Local é o provider local. Obrigatório para PolicyLocalOnly e
	// PolicyLocalThenCloud.
	Local ASRProvider
	// Cloud é o gateway cloud. Obrigatório para PolicyCloudOnly; opcional
	// para PolicyLocalThenCloud (ausência apenas desativa o fallback).
	Cloud ASRProvider
	// CloudCredential autentica chamadas ao gateway cloud. Sem ela,
	// PolicyCloudOnly falha de forma limpa (AC-032) e PolicyLocalThenCloud
	// simplesmente não tenta o fallback.
	CloudCredential CloudCredential
	// Adequate decide se o resultado local satisfaz PolicyLocalThenCloud
	// sem precisar do cloud. nil usa DefaultAdequate(0.5).
	Adequate AdequateFunc
}

func (e *Engine) adequate() AdequateFunc {
	if e.Adequate != nil {
		return e.Adequate
	}
	return DefaultAdequate(0.5)
}

// Transcribe executa a policy dada sobre audio. O AudioBuffer é liberado
// (audio.Release, se presente) exatamente uma vez, depois que todas as
// chamadas a provider(s) desta transcrição já terminaram — nunca mais de
// uma vez, mesmo quando mais de um provider é consultado.
func (e *Engine) Transcribe(ctx context.Context, policy Policy, audio AudioBuffer, opts ASROptions) (ASRResult, error) {
	defer audio.release()

	switch policy {
	case PolicyLocalOnly:
		if e.Local == nil {
			return ASRResult{}, ErrNoLocalProvider
		}
		return e.Local.Transcribe(ctx, audio, opts)

	case PolicyCloudOnly:
		if e.Cloud == nil {
			return ASRResult{}, ErrNoCloudProvider
		}
		if !e.CloudCredential.present() {
			return ASRResult{}, ErrCloudNotAuthenticated
		}
		return e.Cloud.Transcribe(ctx, audio, opts)

	case PolicyLocalThenCloud:
		if e.Local == nil {
			return ASRResult{}, ErrNoLocalProvider
		}
		localResult, localErr := e.Local.Transcribe(ctx, audio, opts)
		if e.adequate()(localResult, localErr) {
			return localResult, localErr
		}
		if e.Cloud == nil || !e.CloudCredential.present() {
			// Sem cloud disponível/autenticado: degrada para o resultado
			// local, mesmo inadequado — falha do cloud não bloqueia o uso
			// local (NFR-011).
			return localResult, localErr
		}
		// Reaproveita o MESMO audio (mesmo array subjacente em RAM) na
		// chamada ao cloud — nenhuma cópia acontece aqui (TEST-010).
		return e.Cloud.Transcribe(ctx, audio, opts)

	default:
		return ASRResult{}, ErrUnknownPolicy
	}
}
