package resolver

import "context"

// LiteralResolver implementa a política Literal (AC-023,
// docs/ai-spec/16-ai-behavior-spec.md, "Literal"): ASR + normalização
// mínima de Unicode/espaços + a pontuação que o próprio ASR produziu.
// Proibido: contexto semântico, reescrita, inferência de intenção.
//
// # Prova por construção (AC-023)
//
// LiteralResolver não tem NENHUM campo: não guarda DictionaryRepository,
// não guarda ContextProvider, não guarda um SmartResolver, um CloudClient
// ou qualquer outro colaborador "semântico". Não existe caminho de código
// dentro deste tipo que consiga alcançar dicionário, contexto ou resolver
// cloud — a garantia não depende de disciplina em tempo de execução, é
// estrutural: não há parâmetro de construtor, nem campo, por onde esse
// colaborador pudesse entrar. literal_test.go verifica isso por reflection
// (zero campos) e, como segunda prova independente, por comportamento: o
// mesmo input ambíguo que o SmartResolver resolveria via dicionário/
// contexto sai intocado do Literal, mesmo com um "espião" de dicionário
// que falharia o teste se fosse chamado — espião esse que, por não haver
// onde encaixá-lo em LiteralResolver, nunca chega a ser invocado.
type LiteralResolver struct{}

// NewLiteralResolver constrói o resolver Literal. Não recebe nenhuma
// dependência — ver comentário do tipo.
func NewLiteralResolver() *LiteralResolver { return &LiteralResolver{} }

var _ Resolver = (*LiteralResolver)(nil)

// Resolve implementa Resolver. Nunca falha por causa do CONTEÚDO do
// transcript (que é dado, não instrução — THR-008); só propaga um ctx já
// cancelado.
func (LiteralResolver) Resolve(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, err
	}
	raw := req.ASRResult.Text
	return ResolveResult{
		Mode:      ModeLiteral,
		RawText:   raw,
		FinalText: normalizeWhitespace(raw),
	}, nil
}
