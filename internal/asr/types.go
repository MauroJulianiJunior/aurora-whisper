package asr

import "time"

// AudioBuffer é o áudio capturado, mantido inteiramente em RAM (nunca em
// disco — ver docs/ai-spec/09-security-privacy.md e AC-028). PCM é mono,
// conforme docs/ai-spec/04-system-design.md ("Áudio").
//
// AudioBuffer é passado POR VALOR a cada chamada de ASRProvider.Transcribe,
// mas o slice PCM compartilha o array subjacente entre todas as cópias: em
// uma policy local_then_cloud, o mesmo buffer em RAM é reaproveitado na
// chamada ao provider local e, se necessário, na chamada ao gateway cloud,
// sem nenhuma cópia (TEST-010).
type AudioBuffer struct {
	// PCM contém as amostras mono de 16 bits, em ordem temporal.
	PCM []int16
	// SampleRateHz é a taxa de amostragem das amostras em PCM.
	SampleRateHz int

	// Release, quando não nil, libera o buffer em RAM subjacente. Apenas o
	// dono do buffer (quem o criou e o repassou ao Engine) deve chamá-lo, e
	// exatamente uma vez, depois que TODAS as chamadas a provider(s) para
	// esta tentativa de transcrição já terminaram — nunca um ASRProvider
	// individual. O Engine (engine.go) respeita esta regra.
	Release func()
}

// release chama buf.Release, se presente, tolerando um AudioBuffer sem
// função de liberação (ex.: buffers criados em testes que não a exercitam).
func (buf AudioBuffer) release() {
	if buf.Release != nil {
		buf.Release()
	}
}

// Acceleration identifica o tipo de aceleração de hardware usada (ou
// disponível) por um provider de ASR.
type Acceleration string

const (
	AccelerationNone Acceleration = "none"
	AccelerationCPU  Acceleration = "cpu"
	AccelerationGPU  Acceleration = "gpu"
)

// ASRCapabilities descreve o que um ASRProvider suporta, conforme
// docs/ai-spec/18-tool-contract.md. O motor de policy e o resolver usam
// isso para decidir o que pedir a um provider — nunca assumem suporte.
type ASRCapabilities struct {
	// Streaming indica suporte a resultados parciais durante a captura.
	Streaming bool
	// Confidence indica se este provider FORNECE confidence em ASRResult.
	// Quando false, ASRResult.Confidence virá sempre ausente (Present ==
	// false) — nunca um valor fabricado (docs/ai-spec/16-ai-behavior-spec.md,
	// "Uncertainty").
	Confidence bool
	// Timestamps indica suporte a spans com marcação temporal.
	Timestamps bool
	// NBest indica suporte a alternativas (N-best) além do texto principal.
	NBest bool
	// Languages lista os códigos de idioma suportados (BCP-47). Vazio
	// significa "não declarado pelo provider" — não necessariamente
	// "nenhum".
	Languages []string
	// Acceleration é o tipo de aceleração de hardware usado pelo provider.
	Acceleration Acceleration
	// Vocabulary indica se este provider USA ASROptions.Vocabulary. Quando
	// false, o vocabulário passado é ignorado em silêncio — e é por isso
	// que a capability existe: quem monta o vocabulário paga para montá-lo
	// (consulta ao dicionário, orçamento de latência) e precisa poder
	// saber que esse trabalho não teria efeito nenhum.
	Vocabulary bool
}

// ASROptions são os parâmetros de uma chamada de transcrição.
type ASROptions struct {
	// Language é uma dica de idioma (BCP-47, ex. "pt-BR"). Vazio pede
	// detecção automática quando o provider suportar.
	Language string
	// MaxAlternatives pede até N alternativas (N-best) quando o provider
	// suportar (Capabilities().NBest). Zero significa "apenas a melhor".
	MaxAlternatives int

	// Vocabulary é uma lista de termos que enviesa o decodificador em
	// direção a eles — no whisper.cpp, o "initial prompt". É uma DICA,
	// não uma restrição: o ASR continua livre para transcrever qualquer
	// coisa, e termo que não foi falado não aparece por estar aqui.
	//
	// # Por que isto existe
	//
	// Medido em 26-set-2026 sobre as 46 amostras do dataset de
	// referência (docs/sprint-5-portao-de-entidades.md): passar os termos
	// técnicos de quem usa levou a Entity Accuracy de 61,2% para 73,5%
	// com 13 termos, e para 79,6% com 36. Nenhum código de dicionário
	// envolvido — o ganho acontece na DECODIFICAÇÃO, antes de existir
	// texto para corrigir, que é mais confiável que casar string depois.
	//
	// # E por que ele tem um custo que o chamador precisa conhecer
	//
	// O decodificador processa estes termos, então eles custam latência:
	// +40 ms com 13 termos, +146 ms com 36, na máquina de referência.
	// Como o p50 do produto já estava em 565 ms contra meta de 500
	// (docs/benchmarks/asr-decisao-2026-09-18.md), quem monta este campo
	// tem de limitá-lo por orçamento — ver dictionary.Vocabulario.
	//
	// Provider que não suporta ignora (Capabilities().Vocabulary).
	Vocabulary string
}

// Confidence representa o confidence score OPCIONAL de um resultado de
// ASR. É um par (valor, presente) em vez de um *float64: evita alocação
// extra a cada resultado, não tem estado "ponteiro nil vs valor zero"
// ambíguo para quem só olha o valor por engano, e deixa Present como campo
// nomeado e explícito no call site (r.Confidence.Present) — mais legível
// que checar r.Confidence != nil espalhado pelo código.
//
// A distinção importa: "provider não forneceu confidence" (Present ==
// false) é diferente de "provider forneceu confidence zero" (Present ==
// true, Value == 0). Ver docs/ai-spec/16-ai-behavior-spec.md, "Uncertainty"
// — nunca fabricar um score e apresentá-lo como se tivesse vindo do ASR.
type Confidence struct {
	Value   float64
	Present bool
}

// NoConfidence é o valor zero de Confidence: "provider não forneceu
// confidence". Nomeado para deixar a intenção explícita nos providers mock.
var NoConfidence = Confidence{}

// NewConfidence constrói um Confidence presente com o valor dado.
func NewConfidence(value float64) Confidence {
	return Confidence{Value: value, Present: true}
}

// Span é um trecho do texto reconhecido com marcação temporal opcional.
type Span struct {
	Text  string
	Start time.Duration
	End   time.Duration
}

// Alternative é uma hipótese alternativa de transcrição (N-best).
type Alternative struct {
	Text       string
	Confidence Confidence
}

// ASRResult é o resultado de uma transcrição, conforme
// docs/ai-spec/18-tool-contract.md: "texto, idioma, timestamps,
// confidence, spans, alternatives (N-best), model, processing time".
//
// ASRResult carrega SOMENTE o que o provider de ASR reportou. Sinais
// derivados localmente (fonético, OOV, conflito de contexto — camada de
// uncertainty do resolver) não pertencem a este tipo; misturá-los aqui
// disfarçaria sua procedência como se fossem confidence do ASR, o que a
// spec proíbe explicitamente.
type ASRResult struct {
	Text           string
	Language       string
	Confidence     Confidence
	Spans          []Span
	Alternatives   []Alternative
	Model          string
	ProcessingTime time.Duration
}
