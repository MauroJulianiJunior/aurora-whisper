package resolver

import (
	"context"
	"sort"
	"strings"
	"unicode"

	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// defaultConfidenceThreshold é usado quando SmartResolver.ConfidenceThreshold
// é zero. Mesma ordem de grandeza do default de asr.DefaultAdequate(0.5)
// — não é o MESMO sinal (aquele decide policy de provider; este marca
// incerteza para o resolver), mas usar o mesmo valor evita dois "meios"
// divergentes sem motivo.
const defaultConfidenceThreshold = 0.5

// SmartResolver implementa a política Smart (AC-024, TEST-004): normaliza
// → autocorreção de fala → detecta incerteza (confidence quando o
// provider fornece, N-best, casamento fonético/dicionário) → aplica o que
// o dicionário/contexto corroboram → resolver cloud restrito só para o
// que sobrar incerto, se a política permitir.
type SmartResolver struct {
	// Dictionary é obrigatório para qualquer resolução semântica. Sem ele,
	// Smart degrada para o texto literal + autocorreção (fallback
	// "Smart sem contexto -> dictionary/ASR", aqui ainda mais básico: sem
	// dicionário nenhum).
	Dictionary dictionary.DictionaryRepository

	// Cloud é opcional. nil desativa o resolver cloud restrito
	// independentemente de ResolveRequest.CloudPolicy (AC-030).
	Cloud CloudClient

	// ConfidenceThreshold é o limiar abaixo do qual um confidence
	// FORNECIDO pelo ASR marca as palavras como incertas
	// (SpanSourceASRConfidence). Zero usa defaultConfidenceThreshold.
	ConfidenceThreshold float64
}

// NewSmartResolver constrói um SmartResolver com o dicionário dado e
// nenhum cloud client (use o campo Cloud para habilitar o fallback cloud
// restrito).
func NewSmartResolver(repo dictionary.DictionaryRepository) *SmartResolver {
	return &SmartResolver{Dictionary: repo}
}

var _ Resolver = (*SmartResolver)(nil)

func (s *SmartResolver) threshold() float64 {
	if s.ConfidenceThreshold > 0 {
		return s.ConfidenceThreshold
	}
	return defaultConfidenceThreshold
}

// Resolve implementa Resolver.
func (s *SmartResolver) Resolve(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, err
	}

	raw := req.ASRResult.Text
	base := normalizeWhitespace(raw)
	result := ResolveResult{Mode: ModeSmart, RawText: raw}

	sc := applySelfCorrection(base)
	corrected := sc.Text
	if sc.Triggered {
		result.AppliedReplacements = append(result.AppliedReplacements, sc.Applied)
	}

	if s.Dictionary == nil {
		// Sem dicionário: nenhuma resolução semântica é possível. Degrada
		// para o texto corrigido (autocorreção não depende de dicionário)
		// sem quebrar (docs/ai-spec/16-ai-behavior-spec.md, "Fallback").
		result.FinalText = corrected
		result.Warnings = append(result.Warnings, "smart: sem DictionaryRepository configurado, degradado para literal/self-correction")
		return result, nil
	}

	signals, err := s.computeSignals(ctx, req, corrected, sc.Triggered)
	if err != nil {
		// Falha ao consultar o dicionário: degrada para o texto corrigido
		// sem substituições semânticas — nunca quebra a tentativa
		// (docs/ai-spec/16-ai-behavior-spec.md, "Fallback": "resolver
		// falhou -> local result").
		result.FinalText = corrected
		result.Warnings = append(result.Warnings, "smart: busca no dicionário falhou, degradado para literal/self-correction: "+err.Error())
		return result, nil
	}

	localText, applied, pending := applyLocalSignals(corrected, signals)
	result.AppliedReplacements = append(result.AppliedReplacements, applied...)
	result.UncertainSpans = pending

	if len(pending) == 0 || req.CloudPolicy != CloudAllowed || s.Cloud == nil {
		result.FinalText = localText
		return result, nil
	}

	finalText, cloudApplied, warnings, usedCloud := s.resolveWithCloud(ctx, req, localText, pending)
	result.FinalText = finalText
	result.AppliedReplacements = append(result.AppliedReplacements, cloudApplied...)
	result.Warnings = append(result.Warnings, warnings...)
	result.UsedCloud = usedCloud
	return result, nil
}

// signal agrega, para um wordToken, os sinais de incerteza detectados e o
// melhor candidato de substituição encontrado (quando houver).
type signal struct {
	token      wordToken
	sources    []SpanSource
	candidate  string
	reason     ReplacementReason
	score      float64
	autoApply  bool
	candidates []string // candidatos extras (ex.: alternativa N-best) para UncertainSpan.Candidates.
}

// maxPhraseWords limita quantas palavras seguidas computeSignals tenta
// como um único termo de busca no dicionário (ex.: "n oito n" → "n8n").
// Dicionário pessoal e aliases costumam ser termos curtos
// (docs/ai-spec/05-data-model.md); 4 é generoso o bastante para os casos
// reais sem custar buscas demais por token.
const maxPhraseWords = 4

// computeSignals levanta, ao longo de text, os sinais de incerteza com
// procedência explícita (SpanSource) e, quando o dicionário corrobora, o
// candidato de substituição. text já passou por normalização e
// autocorreção — é o texto que efetivamente vira a base do resultado
// Smart.
//
// Tenta primeiro casar FRASES de até maxPhraseWords palavras (do maior
// para o menor) contra o dicionário — é assim que um alias de várias
// palavras (ex.: "n oito n" para "n8n") é encontrado, já que
// dictionary.Search compara o termo inteiro recebido, não palavra a
// palavra. Quando nenhuma frase maior casa, cai para a palavra isolada.
func (s *SmartResolver) computeSignals(ctx context.Context, req ResolveRequest, text string, selfCorrected bool) ([]signal, error) {
	tokens := tokenizeWords(text)
	runes := []rune(text)
	signals := make([]signal, 0, len(tokens))

	lowConfidence := req.ASRResult.Confidence.Present && req.ASRResult.Confidence.Value < s.threshold()
	contextText := contextSearchText(req.Context)

	// Comparação N-best: melhor esforço, só quando a autocorreção não
	// reestruturou o texto (índices deixariam de bater) e a contagem de
	// palavras da melhor alternativa é igual à do texto principal.
	var altTokens []wordToken
	if !selfCorrected && len(req.ASRResult.Alternatives) > 0 {
		alt := normalizeWhitespace(req.ASRResult.Alternatives[0].Text)
		at := tokenizeWords(alt)
		if len(at) == len(tokens) {
			altTokens = at
		}
	}

	i := 0
	for i < len(tokens) {
		maxLen := maxPhraseWords
		if remain := len(tokens) - i; remain < maxLen {
			maxLen = remain
		}

		// Escolhe a MELHOR frase, não a mais longa.
		//
		// Até 27-set-2026 este laço ia da frase mais longa para a mais
		// curta e parava no primeiro acerto. O efeito, medido em
		// "transmitir a DCTF Web hoje":
		//
		//	"DCTF Web hoje" -> phonetic (0,48)  <- ganhava
		//	"DCTF Web"      -> exact    (1,00)  <- nunca era tentado
		//
		// Uma evidência FRACA sobre três palavras engolia uma evidência
		// FORTE sobre duas, e ainda consumia os tokens, impedindo a
		// correção. Fora de frase o mesmo termo era corrigido — o defeito
		// só aparecia em texto real.
		//
		// Agora a força do match vem primeiro (exato/alias são variantes
		// confirmadas; fonético é evidência fraca por decisão do
		// 16-ai-behavior-spec.md), e o comprimento só desempata DENTRO da
		// mesma força — preservando a preferência por frase longa onde
		// ela não esconde nada melhor.
		melhor, melhorJ, achou, err := s.melhorFrase(ctx, req, runes, tokens, i, maxLen)
		if err != nil {
			return nil, err
		}

		consumed := false
		if achou {
			top, j := melhor, melhorJ
			phrase := string(runes[tokens[i].Start:tokens[j].End])

			switch top.Reason {
			case dictionary.MatchReasonExact:
				// "Exato" no dicionário tolera ESPAÇAMENTO: Search
				// compara colapsando espaços (dictionary.CollapseSpaces),
				// então "DCTF Web" casa exatamente com "DCTFWeb".
				//
				// Até 27-set-2026 este ramo não fazia nada, e o efeito era
				// o resolver ser estruturalmente incapaz de consertar erro
				// de fronteira de token — "DCTF Web", "End Point", "Time
				// Out" —, que é boa parte do que o ASR erra de verdade
				// (docs/sprint-5-portao-de-entidades.md).
				//
				// Agora canonicaliza quando a forma escrita difere da
				// canônica por QUALQUER coisa que não seja caixa. O
				// critério é o mesmo de metrics.EntityAccuracy, que mede o
				// portão: insensível a maiúscula, sensível a espaço, a
				// acento e a dígito.
				//
				// A caixa sozinha só conta no termo de GRAFIA PRÓPRIA
				// (desde 01-out-2026), e só quando o ASR escreveu alguma
				// maiúscula e o trecho não está colado em separador de
				// código: "ESOCIAL" vira "eSocial", "Postgresql" vira
				// "PostgreSQL". Sigla toda maiúscula ("das" não vira
				// "DAS"), termo só com a inicial maiúscula, palavra comum
				// minúscula, "npm install typescript" e "gov.br/esocial"
				// ficam como vieram. Ver precisaCanonicalizar.
				//
				// O termo que o painel inventou (AliasOnly) nunca chega aqui:
				// searchTop o descarta. A pessoa confirmou a variante, e não a
				// forma canônica, e reescrever "está" para "esta" porque ela
				// aprendeu "ista" seria uma regra de acento que ninguém pediu.
				if precisaCanonicalizar(phrase, top.CanonicalForm, colaEmCodigo(runes, tokens[i].Start, tokens[j].End)) {
					signals = append(signals, signal{
						token:     wordToken{Start: tokens[i].Start, End: tokens[j].End, Text: phrase},
						sources:   []SpanSource{SpanSourceOOV},
						candidate: top.CanonicalForm,
						reason:    ReasonDictionary,
						score:     top.Score,
						// Auto-aplica pelo mesmo motivo do alias: é o
						// termo do dicionário da própria pessoa, casado
						// exatamente. Não há dúvida a resolver — só
						// grafia a normalizar.
						autoApply: true,
					})
				}
				// Consome a frase inteira para não tratar os tokens
				// individuais dela como incertos.
				i = j + 1
				consumed = true
			case dictionary.MatchReasonAlias:
				signals = append(signals, signal{
					token: wordToken{Start: tokens[i].Start, End: tokens[j].End, Text: phrase},
					// Alias é uma variante JÁ CONFIRMADA (docs/ai-spec/16-
					// ai-behavior-spec.md, "Dictionary"): não precisa de
					// corroboração de contexto para ser aplicado.
					sources:   []SpanSource{SpanSourceOOV},
					candidate: formaDoAlias(top, phrase),
					reason:    ReasonDictionary,
					score:     top.Score,
					autoApply: true,
				})
				i = j + 1
				consumed = true
			case dictionary.MatchReasonPhonetic:
				sig := signal{
					token:     wordToken{Start: tokens[i].Start, End: tokens[j].End, Text: phrase},
					sources:   []SpanSource{SpanSourcePhonetic, SpanSourceOOV},
					candidate: top.CanonicalForm,
					reason:    ReasonPhonetic,
					score:     top.Score,
				}
				if contextCorroborates(contextText, top.CanonicalForm) {
					sig.sources = append(sig.sources, SpanSourceContext)
					sig.reason = ReasonContext
					sig.autoApply = true
				}
				signals = append(signals, sig)
				i = j + 1
				consumed = true
			}
		}
		if consumed {
			continue
		}

		// Comprimento 1: mesma lógica, mas com N-best/confidence de ASR
		// também considerados (sinais por palavra isolada).
		tok := tokens[i]
		sig := signal{token: tok}

		if lowConfidence {
			sig.sources = append(sig.sources, SpanSourceASRConfidence)
		}
		if altTokens != nil && altTokens[i].Text != tok.Text {
			sig.sources = append(sig.sources, SpanSourceNBest)
			sig.candidates = append(sig.candidates, altTokens[i].Text)
		}

		top, ok, err := s.searchTop(ctx, req, tok.Text)
		if err != nil {
			return nil, err
		}
		if ok {
			switch top.Reason {
			case dictionary.MatchReasonAlias:
				if !strings.EqualFold(top.CanonicalForm, tok.Text) {
					sig.sources = append(sig.sources, SpanSourceOOV)
					sig.candidate = formaDoAlias(top, tok.Text)
					sig.reason = ReasonDictionary
					sig.score = top.Score
					sig.autoApply = true
				}
			case dictionary.MatchReasonPhonetic:
				sig.sources = append(sig.sources, SpanSourcePhonetic, SpanSourceOOV)
				sig.candidate = top.CanonicalForm
				sig.reason = ReasonPhonetic
				sig.score = top.Score
				// Match fonético SOZINHO é evidência fraca ("não pode
				// substituir distantemente apenas por existir"): só
				// auto-aplica quando o contexto (dado não confiável, mas
				// só usado para CORROBORAR um candidato que o dicionário
				// já levantou — nunca para introduzir um novo) confirma o
				// candidato.
				if contextCorroborates(contextText, top.CanonicalForm) {
					sig.sources = append(sig.sources, SpanSourceContext)
					sig.reason = ReasonContext
					sig.autoApply = true
				}
			case dictionary.MatchReasonExact:
				// Mesmo raciocínio do ramo de frase acima: "exato" tolera
				// espaçamento e acento, e a grafia ainda pode estar
				// diferente da canônica. Caixa só conta no termo de grafia
				// própria, com maiúscula escrita e fora de código ("ESOCIAL"
				// -> "eSocial"; "das" nunca vira "DAS"; "typescript" de
				// "npm install typescript" fica): ver precisaCanonicalizar.
				// Termo do painel (AliasOnly) não chega aqui: ver o ramo de
				// frase acima.
				if precisaCanonicalizar(tok.Text, top.CanonicalForm, colaEmCodigo(runes, tok.Start, tok.End)) {
					sig.sources = append(sig.sources, SpanSourceOOV)
					sig.candidate = top.CanonicalForm
					sig.reason = ReasonDictionary
					sig.score = top.Score
					sig.autoApply = true
				}
			}
		}

		// TEST-004: o ASR pode devolver uma palavra VÁLIDA e bem escrita que
		// ainda assim é a errada no contexto — "físico" quando a pessoa
		// disse "Fisco". Nenhuma checagem ortográfica ou fonética marcaria
		// a hipótese principal, porque não há nada de errado com ela
		// isoladamente. Quem denuncia o problema é a divergência para a
		// alternativa N-best.
		//
		// Nesses casos consultamos o dicionário pela ALTERNATIVA, não pela
		// hipótese principal. Isso não viola a regra de que o dicionário
		// "não pode substituir distantemente apenas por existir"
		// (docs/ai-spec/16-ai-behavior-spec.md): quem propôs a alternativa
		// foi o próprio ASR, não o dicionário. E o contexto segue apenas
		// CORROBORANDO um candidato já levantado — exigi-lo é o que impede
		// que qualquer alternativa presente no dicionário seja aplicada
		// automaticamente.
		if sig.candidate == "" && altTokens != nil && altTokens[i].Text != tok.Text {
			altTop, altOK, err := s.searchTop(ctx, req, altTokens[i].Text)
			if err != nil {
				return nil, err
			}
			if altOK && altTop.Reason != dictionary.MatchReasonPhonetic &&
				contextCorroborates(contextText, altTop.CanonicalForm) {
				sig.sources = append(sig.sources, SpanSourceContext)
				// O que entra é o que a variante do alias escreveria (a inicial
				// de quem abre a frase acompanha a palavra que sai, que é a da
				// hipótese principal): ver formaDoAlias.
				sig.candidate = formaDoAlias(altTop, tok.Text)
				sig.reason = ReasonContext
				sig.score = altTop.Score
				sig.autoApply = true
			}
		}

		if len(sig.sources) > 0 {
			signals = append(signals, sig)
		}
		i++
	}

	return signals, nil
}

// searchTop devolve o melhor candidato do dicionário para term (o primeiro
// que dictionary.Search devolve, já ordenado), ou ok=false quando não há
// nenhum.
//
// O casamento exato de um termo que o painel inventou (AliasOnly, ver
// DictionaryCandidate.SoPresenca) não é candidato: diz só que o termo está
// escrito, e o Smart não troca nada por causa disso. Descartá-lo AQUI, e não
// em cada ponto que usa o resultado, é o que faz o termo do painel agir só
// pela variante em todos eles: a troca por palavra, o N-best, a escolha da
// melhor frase e o veto por palavra mais forte. O candidato seguinte (se
// houver) é o que valeria se o termo não existisse, e é por isso que a busca
// não corta em 1.
func (s *SmartResolver) searchTop(ctx context.Context, req ResolveRequest, term string) (dictionary.DictionaryCandidate, bool, error) {
	candidates, err := s.Dictionary.Search(ctx, dictionary.DictionaryQuery{
		UserID:   req.UserID,
		Term:     term,
		Language: req.Language,
		Domain:   req.Domain,
	})
	if err != nil {
		return dictionary.DictionaryCandidate{}, false, err
	}
	for _, c := range candidates {
		if !c.SoPresenca() {
			return c, true, nil
		}
	}
	return dictionary.DictionaryCandidate{}, false, nil
}

// formaDoAlias é o que o Smart escreve no lugar de original quando o alias de
// top casou. É a forma canônica, exceto num termo que o painel inventou em
// minúscula ("descrição", aprendido de "transcrição"): ali a inicial
// maiúscula de quem abre a frase acompanha o que foi dito, e "Transcrição do
// áudio" vira "Descrição do áudio". Só a inicial: o painel não sabe se a
// palavra abria a frase, e um termo com maiúscula própria ("ECF", "Wispr")
// fica como está.
//
// A regra vale só para a palavra comum: a forma canônica TODA em minúscula e
// sem dígito. Uma marca que começa em minúscula e tem maiúscula ou dígito no
// meio ("iFood", "n8n", "iPhone", "macOS", "eSocial") é escrita assim em
// qualquer lugar, inclusive no começo da frase; pôr a inicial em maiúscula
// ("IFood", "N8n") seria trocar a grafia do termo.
func formaDoAlias(top dictionary.DictionaryCandidate, original string) string {
	canonica := []rune(top.CanonicalForm)
	orig := []rune(original)
	if !top.AliasOnly || len(canonica) == 0 || len(orig) == 0 {
		return top.CanonicalForm
	}
	if !soMinusculas(canonica) {
		return top.CanonicalForm
	}
	if unicode.IsLower(canonica[0]) && unicode.IsUpper(orig[0]) {
		canonica[0] = unicode.ToUpper(canonica[0])
		return string(canonica)
	}
	return top.CanonicalForm
}

// soMinusculas reporta se nenhuma letra de r é maiúscula e nenhum caractere é
// dígito: a forma canônica que é uma palavra comum, e não uma marca com grafia
// própria.
func soMinusculas(r []rune) bool {
	for _, c := range r {
		if unicode.IsUpper(c) || unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// contextSearchText concatena os campos textuais do snapshot de contexto
// num único texto normalizado, para checar corroboração de candidatos.
// Contexto é DADO NÃO CONFIÁVEL (THR-008): esta função nunca interpreta o
// conteúdo, só compara substrings normalizadas. Contexto bloqueado ou
// zero value devolve "" — Smart trata isso como "contexto ausente" e
// nunca falha por causa disso (degrada para dicionário/ASR).
func contextSearchText(snap uiacontext.ContextSnapshot) string {
	if snap.Blocked {
		return ""
	}
	parts := []string{snap.WindowTitle, snap.PrefixNearCaret, snap.SuffixNearCaret, snap.Selection}
	parts = append(parts, snap.AccessibleLabels...)
	return dictionary.Normalize(strings.Join(parts, " "))
}

// contextCorroborates reporta se candidate (forma canônica de dicionário)
// aparece no texto de contexto já normalizado. Só corrobora candidatos
// JÁ levantados pelo dicionário/fonética — nunca introduz um candidato
// novo a partir do contexto: é por isso que um contexto enganoso, que
// menciona um termo qualquer sem relação com o candidato do span, nunca
// consegue empurrar uma substituição errada (não há candidato para ele
// corroborar).
func contextCorroborates(contextText, candidate string) bool {
	if contextText == "" || candidate == "" {
		return false
	}
	needle := dictionary.CollapseSpaces(dictionary.Normalize(candidate))
	if needle == "" {
		return false
	}
	haystack := dictionary.CollapseSpaces(contextText)
	return strings.Contains(haystack, needle)
}

// applyLocalSignals aplica, em text, os sinais marcados como autoApply
// (alias confirmado, ou fonético corroborado pelo contexto), devolvendo o
// texto resultante, o registro de auditoria das substituições aplicadas e
// os spans que continuam incertos (candidatos a resolver cloud), já com
// offsets recalculados para o texto pós-aplicação local.
func applyLocalSignals(text string, signals []signal) (localText string, applied []AppliedReplacement, pending []UncertainSpan) {
	delta := 0
	reps := make([]offsetReplacement, 0, len(signals))

	for _, sig := range signals {
		origLen := sig.token.End - sig.token.Start

		if sig.autoApply && sig.candidate != "" {
			applied = append(applied, AppliedReplacement{
				Start:       sig.token.Start,
				End:         sig.token.End,
				Original:    sig.token.Text,
				Replacement: sig.candidate,
				Reason:      sig.reason,
				Confidence:  sig.score,
			})
			reps = append(reps, offsetReplacement{Start: sig.token.Start, End: sig.token.End, Text: sig.candidate})
			delta += len([]rune(sig.candidate)) - origLen
			continue
		}

		candidates := make([]string, 0, len(sig.candidates)+1)
		if sig.candidate != "" {
			candidates = append(candidates, sig.candidate)
		}
		candidates = append(candidates, sig.candidates...)

		pending = append(pending, UncertainSpan{
			Start:      sig.token.Start + delta,
			End:        sig.token.Start + delta + origLen,
			Original:   sig.token.Text,
			Sources:    sig.sources,
			Candidates: candidates,
		})
	}

	localText = applyReplacements(text, reps)
	return localText, applied, pending
}

// resolveWithCloud monta a requisição restrita (docs/ai-spec/06-api-contract.md),
// chama s.Cloud e valida a resposta (item 6 da tarefa). Erros de
// transporte e JSON inválido no envelope degradam para o resultado local
// sem alterar text.
func (s *SmartResolver) resolveWithCloud(ctx context.Context, req ResolveRequest, text string, pending []UncertainSpan) (finalText string, applied []AppliedReplacement, warnings []string, usedCloud bool) {
	cloudSpans := make([]CloudUncertainSpan, 0, len(pending))
	dictTerms := make(map[string]struct{})
	for _, p := range pending {
		cloudSpans = append(cloudSpans, CloudUncertainSpan{
			Start: p.Start, End: p.End, Original: p.Original, Candidates: p.Candidates,
		})
		for _, c := range p.Candidates {
			dictTerms[c] = struct{}{}
		}
	}
	terms := make([]string, 0, len(dictTerms))
	for t := range dictTerms {
		terms = append(terms, t)
	}
	sort.Strings(terms)

	var appName, nearby string
	if !req.Context.Blocked {
		appName = req.Context.AppName
		nearby = strings.TrimSpace(strings.Join([]string{req.Context.PrefixNearCaret, req.Context.SuffixNearCaret}, " "))
	}

	cloudReq := CloudSmartRequest{
		Mode:            "smart",
		Transcript:      text,
		UncertainSpans:  cloudSpans,
		Context:         CloudContext{App: appName, NearbyText: nearby},
		DictionaryTerms: terms,
	}

	usedCloud = true
	raw, err := s.Cloud.ResolveSmart(ctx, cloudReq)
	if err != nil {
		return text, nil, []string{"smart: cloud resolver falhou, mantido resultado local: " + err.Error()}, usedCloud
	}

	valid, rejectWarnings, parseErr := parseAndValidateCloudResponse(raw, text, pending)
	if parseErr != nil {
		return text, nil, []string{"smart: " + parseErr.Error() + "; mantido resultado local"}, usedCloud
	}

	reps := make([]offsetReplacement, 0, len(valid))
	appliedList := make([]AppliedReplacement, 0, len(valid))
	for _, v := range valid {
		reps = append(reps, offsetReplacement{Start: v.Start, End: v.End, Text: v.Replacement})
		appliedList = append(appliedList, AppliedReplacement{
			Start: v.Start, End: v.End, Original: v.original, Replacement: v.Replacement,
			Reason: ReasonCloud, Confidence: v.Confidence,
		})
	}

	return applyReplacements(text, reps), appliedList, rejectWarnings, usedCloud
}

// forcaDoMatch ordena as razões de match do dicionário por quanta
// confiança elas merecem.
//
// Exato e alias são variantes JÁ CONFIRMADAS — o termo do dicionário da
// própria pessoa, casado sem dúvida. Fonético é evidência fraca por
// decisão explícita do 16-ai-behavior-spec.md ("não pode substituir
// distantemente apenas por existir"), e é justamente por isso que ele não
// pode SOMBREAR uma evidência forte sobre um trecho menor.
func forcaDoMatch(r dictionary.MatchReason) int {
	switch r {
	case dictionary.MatchReasonExact:
		return 3
	case dictionary.MatchReasonAlias:
		return 2
	case dictionary.MatchReasonPhonetic:
		return 1
	default:
		return 0
	}
}

// melhorFrase procura, entre as frases de 2 a maxLen palavras que começam
// em tokens[i], a de MELHOR match — força primeiro, comprimento só como
// desempate dentro da mesma força.
//
// Devolve o candidato, o índice do último token da frase escolhida, e se
// houve algum match.
func (s *SmartResolver) melhorFrase(ctx context.Context, req ResolveRequest,
	runes []rune, tokens []wordToken, i, maxLen int) (dictionary.DictionaryCandidate, int, bool, error) {

	// Uma frase só existe se as palavras estiverem separadas por ESPAÇO.
	//
	// Atravessar pontuação era uma CORREÇÃO FALSA silenciosa. Medido em
	// 28-set-2026 contra o dicionário real, sem nenhuma mudança no
	// código:
	//
	//	entra: "O Claude, Code e o Aurora resolvem coisas diferentes."
	//	sai:   "O Claude Code e o Aurora resolvem coisas diferentes."
	//
	// Uma lista de TRÊS itens virou uma de dois. dictionary.Normalize
	// trata pontuação como separador, então "Claude, Code" normaliza para
	// "claude code" e casa EXATO com a canônica; a substituição então
	// reescreve o intervalo de runas inteiro, vírgula inclusa.
	//
	// O argumento para RECUSAR, e não para preservar a vírgula na
	// reescrita, é o mesmo de diferePorMaisQueCaixa: o critério é o de
	// metrics.EntityAccuracy, que é como o portão do produto mede.
	// normalizeTokens parte por espaço e apara pontuação de BORDA — então
	// "Claude, Code" JÁ conta como acerto de "Claude Code". Reescrever ali
	// não ganha entidade nenhuma e mexe em texto que está certo pela
	// definição do produto, que é exatamente o que o portão de correção
	// falsa (≤1%) proíbe.
	//
	// Preservar a pontuação seria pior: só é definível quando o span e a
	// canônica têm a mesma contagem de palavras. Para um alias que muda a
	// contagem ("n oito n" -> "n8n") não existe onde recolocar a vírgula.
	// Recusar não tem essa lacuna, e falha FECHADO — deixa o texto como
	// veio.
	maxLen = limiteDeFrase(runes, tokens, i, maxLen)
	if maxLen < 2 {
		return dictionary.DictionaryCandidate{}, 0, false, nil
	}

	var melhor dictionary.DictionaryCandidate
	melhorJ, melhorForca := 0, 0
	achou := false

	// Do mais longo para o mais curto: com força igual, o primeiro a ser
	// visto vence, o que mantém a preferência original por frase longa.
	for length := maxLen; length >= 2; length-- {
		j := i + length - 1
		phrase := string(runes[tokens[i].Start:tokens[j].End])

		top, ok, err := s.searchTop(ctx, req, phrase)
		if err != nil {
			return dictionary.DictionaryCandidate{}, 0, false, err
		}
		if !ok {
			continue
		}
		if f := forcaDoMatch(top.Reason); f > melhorForca {
			melhor, melhorJ, melhorForca, achou = top, j, f, true
		}
	}

	// Nenhuma palavra DENTRO da frase escolhida pode ter casamento mais
	// forte que a frase inteira.
	//
	// Esta é a forma real do defeito que apareceu três vezes em
	// 27-set-2026, cada vez numa posição diferente:
	//
	//	"DCTF Web hoje" (phonetic) engolia "DCTF Web" (exact)
	//	"Supabaese e"   (phonetic) engolia "Supabaese" (alias)
	//	"e o EFDRinf"   (phonetic) engolia "EFDRinf"   (alias)
	//
	// As duas primeiras correções trataram instâncias: comparar frases
	// entre si, e depois comparar com a palavra INICIAL. A terceira
	// mostrou que o token forte pode estar em qualquer posição dentro da
	// frase — no caso, duas adiante.
	//
	// Devolver achou=false entrega os tokens ao ramo de palavra única,
	// que tem a lógica completa (n-best, confidence do ASR, TEST-004).
	//
	// A verificação só acontece quando a frase NÃO é do tipo mais forte:
	// match exato é o teto, e nada dentro dele pode ganhar.
	if achou && melhorForca < forcaDoMatch(dictionary.MatchReasonExact) {
		for k := i; k <= melhorJ && k < len(tokens); k++ {
			palavra, ok, err := s.searchTop(ctx, req, tokens[k].Text)
			if err != nil {
				return dictionary.DictionaryCandidate{}, 0, false, err
			}
			if ok && forcaDoMatch(palavra.Reason) > melhorForca {
				return dictionary.DictionaryCandidate{}, 0, false, nil
			}
		}
	}
	return melhor, melhorJ, achou, nil
}

// limiteDeFrase devolve quantas palavras, a partir de tokens[i], podem
// formar uma frase sem que o span atravesse pontuação.
//
// O span de uma frase é `runes[tokens[i].Start:tokens[j].End]` — ele
// carrega tudo o que estiver ENTRE os tokens, e é esse intervalo inteiro
// que a substituição reescreve. O casamento de frase foi desenhado para o
// separador ser espaço ("DCTF Web" -> "DCTFWeb"); qualquer outra coisa no
// meio é estrutura da frase, não erro de grafia.
func limiteDeFrase(runes []rune, tokens []wordToken, i, maxLen int) int {
	for n := 1; n < maxLen; n++ {
		if !soEspaco(runes[tokens[i+n-1].End:tokens[i+n].Start]) {
			return n
		}
	}
	return maxLen
}

// soEspaco reporta se o trecho entre dois tokens é só espaço em branco.
// Nunca recebe vazio: tokenizeWords só fecha um token quando encontra uma
// runa que não é letra nem dígito, então há sempre ao menos uma runa de
// separação entre dois tokens.
func soEspaco(separador []rune) bool {
	for _, r := range separador {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
