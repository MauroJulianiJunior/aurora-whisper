package resolver

import (
	"context"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
)

// Mode identifica qual política produziu um ResolveResult.
type Mode int

const (
	ModeLiteral Mode = iota
	ModeSmart
	ModePolish
)

func (m Mode) String() string {
	switch m {
	case ModeLiteral:
		return "literal"
	case ModeSmart:
		return "smart"
	case ModePolish:
		return "polish"
	default:
		return "unknown"
	}
}

// CloudPolicy decide se ESTA requisição pode acionar o resolver cloud
// restrito (AC-030: "fallback cloud só quando policy permite"). É um gate
// por requisição, independente de o SmartResolver ter ou não um
// CloudClient configurado — os dois precisam permitir para o cloud ser
// chamado (ver SmartResolver.Resolve).
type CloudPolicy int

const (
	// CloudDisabled nunca aciona o resolver cloud restrito, mesmo que
	// sobrem spans incertos sem evidência local.
	CloudDisabled CloudPolicy = iota
	// CloudAllowed permite o resolver cloud restrito quando sobrar
	// incerteza sem evidência local suficiente.
	CloudAllowed
)

// ResolveRequest é a entrada de Resolver.Resolve (docs/ai-spec/18-tool-contract.md).
type ResolveRequest struct {
	// UserID escopa a busca no dicionário pessoal (dictionary.DictionaryQuery.UserID).
	UserID string

	// ASRResult é o resultado bruto do provider de ASR. Text e
	// Alternatives são DADO NÃO CONFIÁVEL (THR-008): nenhuma implementação
	// de Resolver interpreta esse conteúdo como instrução.
	ASRResult asr.ASRResult

	// Context é o snapshot JÁ CAPTURADO do foco no momento da tentativa —
	// quem monta o ResolveRequest já chamou uiacontext.ContextProvider.Snapshot;
	// nenhum Resolver deste pacote chama Snapshot ele mesmo. Também é DADO
	// NÃO CONFIÁVEL (THR-008). Quando Context.Blocked é true ou Context é
	// o zero value, Smart/Polish tratam como "contexto ausente" e degradam
	// para dicionário/ASR sem quebrar.
	Context uiacontext.ContextSnapshot

	// Language e Domain restringem a busca no dicionário pessoal
	// (dictionary.DictionaryQuery). nil não restringe.
	Language *string
	Domain   *string

	// CloudPolicy decide se esta requisição pode acionar o resolver cloud
	// restrito (AC-030). Ver comentário de CloudPolicy.
	CloudPolicy CloudPolicy

	// PolishOptIn precisa ser true para que PolishResolver tente a
	// reescrita global via PolishClient — Polish nunca é default
	// (docs/ai-spec/16-ai-behavior-spec.md, "Polish": "não default").
	PolishOptIn bool
}

// SpanSource identifica a PROCEDÊNCIA de um sinal de incerteza. Nunca deve
// se disfarçar de confidence do ASR quando não vier do ASR
// (docs/ai-spec/16-ai-behavior-spec.md, "Uncertainty": "não inventar
// score como se viesse do ASR").
type SpanSource string

const (
	// SpanSourceASRConfidence: o provider de ASR forneceu confidence e ele
	// está abaixo do limiar configurado. Só aparece quando
	// asr.ASRResult.Confidence.Present é true — nunca fabricado quando o
	// provider não fornece confidence.
	SpanSourceASRConfidence SpanSource = "asr_confidence"
	// SpanSourceNBest: a melhor alternativa (N-best) do provider diverge
	// do texto principal nesta posição (comparação melhor-esforço,
	// palavra a palavra).
	SpanSourceNBest SpanSource = "nbest"
	// SpanSourceOOV: a grafia transcrita não corresponde à forma exata de
	// nenhum termo do dicionário pessoal, mas casa foneticamente com um —
	// sinal derivado do dicionário (internal/dictionary), nunca do ASR.
	SpanSourceOOV SpanSource = "oov"
	// SpanSourcePhonetic: casamento por chave fonética contra o
	// dicionário pessoal, já filtrado por similaridade textual em
	// dictionary.Search.
	SpanSourcePhonetic SpanSource = "phonetic"
	// SpanSourceContext: o contexto de janela (efêmero, dado não
	// confiável) corrobora um candidato JÁ levantado pelo
	// dicionário/fonética — nunca introduz um candidato novo por si só
	// (THR-008: contexto nunca vira instrução nem fonte de fato novo).
	SpanSourceContext SpanSource = "context"
)

// UncertainSpan é um trecho do texto (em runas, [Start,End)) marcado como
// incerto, com a lista de sinais que motivaram a marcação e os candidatos
// de substituição levantados. Só spans marcados podem ser substituídos
// (docs/ai-spec/17-prompt-spec.md, regra 1).
type UncertainSpan struct {
	Start, End int
	Original   string
	Sources    []SpanSource
	Candidates []string
}

// ReplacementReason é o reason_code de uma substituição aplicada, no
// mesmo vocabulário do schema cloud (docs/ai-spec/17-prompt-spec.md):
// "dictionary", "context", "phonetic", "self_correction", mais "cloud"
// para o que veio validado do resolver cloud restrito.
type ReplacementReason string

const (
	ReasonDictionary     ReplacementReason = "dictionary"
	ReasonContext        ReplacementReason = "context"
	ReasonPhonetic       ReplacementReason = "phonetic"
	ReasonSelfCorrection ReplacementReason = "self_correction"
	ReasonCloud          ReplacementReason = "cloud"
)

// AppliedReplacement documenta uma substituição efetivamente aplicada ao
// texto, para auditoria — nunca aplicada fora de um span marcado incerto
// (exceção: self_correction, que atua antes da marcação de incerteza,
// reestruturando o texto literal — ver selfcorrection.go).
type AppliedReplacement struct {
	Start, End  int
	Original    string
	Replacement string
	Reason      ReplacementReason
	// Confidence é o score do sinal que motivou a troca — derivado
	// localmente (dicionário/fonética/contexto) ou vindo do resolver
	// cloud. NUNCA é apresentado como confidence do ASR.
	Confidence float64
}

// ResolveResult é a saída de Resolver.Resolve (docs/ai-spec/18-tool-contract.md).
type ResolveResult struct {
	Mode Mode

	// RawText é sempre o texto bruto do ASR, sem nenhuma modificação —
	// idêntico a req.ASRResult.Text, em todos os modos (Literal/Smart/
	// Polish). Existe para auditoria e para satisfazer AC-025 (raw_text e
	// final_text sempre presentes juntos).
	RawText string
	// FinalText é o texto pronto para inserção, conforme a política do modo.
	FinalText string

	UncertainSpans      []UncertainSpan
	AppliedReplacements []AppliedReplacement

	// UsedCloud indica se o resolver cloud restrito foi chamado (mesmo que
	// a resposta tenha sido total ou parcialmente rejeitada na validação).
	UsedCloud bool
	// Warnings acumula avisos não fatais (ex.: replacement cloud
	// rejeitado, Polish mantido no Smart) — nunca contém o conteúdo
	// do transcript/contexto além dos índices/motivo já descritivos. O que
	// o modelo do Polish escreve fora de final_text nunca entra aqui.
	Warnings []string

	// Reescrito é true só quando o Polish foi tentado e a resposta do
	// provedor PASSOU em todas as guardas: FinalText vem dela (mesmo quando
	// ela devolveu o texto igual, por já estar organizado). É o que decide
	// se o histórico registra "polish" ou "smart". Sempre false em Literal
	// e Smart.
	Reescrito bool
	// MotivoFallback diz por que o Polish não valeu, quando foi pedido e
	// não valeu. Vazio quando Reescrito é true, ou quando o Polish nem foi
	// pedido (sem opt-in). Só o motivo, nunca texto.
	MotivoFallback MotivoFallback
	// Frases e FrasesRevertidas dizem, quando o Polish valeu POR FRASE (a
	// resposta inteira não passou, mas parte das frases sim; ADR-016, emenda
	// de 19-out-2026), quantas frases a resposta tinha e quantas voltaram ao
	// texto do Smart. Zero quando a resposta valeu inteira ou não valeu. Só
	// números: podem ir ao log.
	Frases, FrasesRevertidas int
}

// Resolver é o contrato único implementado por LiteralResolver,
// SmartResolver e PolishResolver (docs/ai-spec/18-tool-contract.md).
type Resolver interface {
	Resolve(ctx context.Context, req ResolveRequest) (ResolveResult, error)
}
