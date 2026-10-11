// Package resolver transforma o resultado bruto do ASR no texto final a
// ser inserido, aplicando as estratégias Literal, Smart e Polish
// descritas em docs/ai-spec/04-system-design.md (seção "Resolver"),
// docs/ai-spec/16-ai-behavior-spec.md e docs/ai-spec/17-prompt-spec.md.
//
// # Contrato (docs/ai-spec/18-tool-contract.md)
//
//	type Resolver interface {
//	    Resolve(context.Context, ResolveRequest) (ResolveResult, error)
//	}
//
// Três implementações satisfazem essa interface:
//
//   - LiteralResolver: ASR + normalização mínima de Unicode/espaços +
//     pontuação nativa do ASR. Nunca toca dicionário, contexto ou cloud
//     (AC-023, prova por construção — ver literal.go).
//   - SmartResolver: normaliza → autocorreção de fala → detecta incerteza
//     (confidence quando o provider fornece, N-best, casamento
//     fonético/dicionário) → aplica o que o dicionário/contexto
//     corroboram → resolver cloud restrito só para o que sobrar incerto,
//     se a política permitir (AC-024, TEST-004).
//   - PolishResolver: Smart primeiro, depois organização do texto
//     (pontuação, parágrafos, listas) pelo provedor de IA escolhido, só
//     quando explicitamente opt-in (AC-025). A resposta do provedor só vale
//     se passar em guardas puras (polish_guardas.go): curto, JSON, formato,
//     números, siglas, termos e nomes, palavras. Qualquer falha devolve o texto do
//     Smart, com o motivo em ResolveResult.MotivoFallback. Quando é a chamada
//     ao provedor que falha, o motivo é o que o cliente classificou num
//     *ErroPolish (chave recusada, sem crédito, modelo inexistente...), sem
//     corpo de resposta e sem chave.
//
// # Regra que atravessa o pacote inteiro (THR-008)
//
// ASRResult.Text, ASRResult.Alternatives e todo o conteúdo de
// uiacontext.ContextSnapshot são DADO NÃO CONFIÁVEL, nunca instrução.
// Nenhum tipo deste pacote interpreta, obedece ou reage a texto embutido
// no transcript ou no contexto — eles só participam de comparação
// determinística de string (normalização, casamento exato/fonético,
// corroboração de candidato já levantado pelo dicionário). Ver
// injection_test.go.
//
// # Procedência de incerteza (16-ai-behavior-spec.md, "Uncertainty")
//
// Cada UncertainSpan registra de ONDE veio cada sinal (SpanSource).
// SpanSourceASRConfidence só aparece quando o provider de ASR
// efetivamente forneceu confidence (asr.Confidence.Present) — nunca é
// fabricado para disfarçar um sinal derivado localmente (fonético, OOV,
// contexto) como se fosse confidence do ASR.
package resolver
