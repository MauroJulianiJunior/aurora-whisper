package resolver_test

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// trackingDictionary embrulha um *dictionary.InMemoryDictionaryRepository
// real (para que Search funcione de verdade) e conta chamadas de
// escrita (Upsert/UpsertAlias) — usado para provar que o Smart nunca
// persiste o contexto efêmero que consultou (TEST-004: "SEM persistir o
// contexto").
type trackingDictionary struct {
	*dictionary.InMemoryDictionaryRepository
	upsertCalls      int
	upsertAliasCalls int
}

func (t *trackingDictionary) Upsert(ctx context.Context, e dictionary.DictionaryEntry) error {
	t.upsertCalls++
	return t.InMemoryDictionaryRepository.Upsert(ctx, e)
}

func (t *trackingDictionary) UpsertAlias(ctx context.Context, a dictionary.DictionaryAlias) error {
	t.upsertAliasCalls++
	return t.InMemoryDictionaryRepository.UpsertAlias(ctx, a)
}

var _ dictionary.DictionaryRepository = (*trackingDictionary)(nil)

// TestSmartResolver_TEST004 recria o cenário TEST-004
// (docs/ai-spec/03-acceptance-criteria.md): ASR transcreveu uma palavra
// foneticamente ambígua ("comersial", que colide com "comercial" sob
// dictionary.PTBRSoundex — a mesma relação que "fisico"/"fisco" tem na
// especificação, verificada em internal/resolver/tmpcheck durante o
// desenvolvimento desta slice; "fisico"/"fisco" NÃO colidem sob o
// Soundex realmente implementado, então o par de palavras foi adaptado
// para exercitar o mesmo mecanismo com fidelidade ao código real — ver
// "concerns" do relatório desta slice), o contexto de janela menciona a
// forma correta ("Departamento Comercial") e o dicionário pessoal tem
// "Comercial" cadastrado. Smart deve escolher "Comercial" sem acionar o
// cloud e SEM persistir o contexto usado.
func TestSmartResolver_TEST004(t *testing.T) {
	repo := &trackingDictionary{InMemoryDictionaryRepository: dictionary.NewInMemoryDictionaryRepository(nil)}
	putEntry(t, repo.InMemoryDictionaryRepository, "e1", testUser, "Comercial", 0)

	sr := resolver.NewSmartResolver(repo)

	req := resolver.ResolveRequest{
		UserID: testUser,
		ASRResult: asr.ASRResult{
			Text: "abra o modulo comersial",
		},
		Context: uiacontext.ContextSnapshot{
			AppName:         "chrome.exe",
			PrefixNearCaret: "Departamento Comercial",
		},
		CloudPolicy: resolver.CloudDisabled, // não deveria precisar do cloud.
	}

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const want = "abra o modulo Comercial"
	if got.FinalText != want {
		t.Fatalf("FinalText = %q, want %q", got.FinalText, want)
	}
	if got.UsedCloud {
		t.Fatal("UsedCloud = true: a corroboração de contexto deveria ter bastado, sem precisar do cloud")
	}
	if len(got.UncertainSpans) != 0 {
		t.Fatalf("UncertainSpans = %v, want vazio (span foi resolvido localmente)", got.UncertainSpans)
	}

	foundContextReplacement := false
	for _, a := range got.AppliedReplacements {
		if a.Original == "comersial" {
			foundContextReplacement = true
			if a.Replacement != "Comercial" {
				t.Fatalf("Replacement = %q, want %q", a.Replacement, "Comercial")
			}
			if a.Reason != resolver.ReasonContext {
				t.Fatalf("Reason = %q, want %q", a.Reason, resolver.ReasonContext)
			}
		}
	}
	if !foundContextReplacement {
		t.Fatalf("nenhuma substituição de 'comersial' encontrada em %v", got.AppliedReplacements)
	}

	if repo.upsertCalls != 0 || repo.upsertAliasCalls != 0 {
		t.Fatalf("Smart persistiu no dicionário (Upsert=%d, UpsertAlias=%d): contexto/correção nunca deve ser gravado automaticamente", repo.upsertCalls, repo.upsertAliasCalls)
	}
}

// TestSmartResolver_MisleadingContextNeverWins é o teste "contexto
// enganoso": o contexto menciona um termo do dicionário sem NENHUMA
// relação com o span ambíguo. Como contextCorroborates só CORROBORA um
// candidato que o dicionário/fonética já levantaram (nunca introduz um
// candidato novo), o contexto enganoso não tem como vencer — a palavra
// ambígua permanece como o ASR a transcreveu.
func TestSmartResolver_MisleadingContextNeverWins(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", testUser, "Comercial", 0)
	putEntry(t, repo, "e2", testUser, "Financeiro", 0)

	sr := resolver.NewSmartResolver(repo)

	req := resolver.ResolveRequest{
		UserID:    testUser,
		ASRResult: asr.ASRResult{Text: "abra o modulo comersial"},
		Context: uiacontext.ContextSnapshot{
			// Contexto menciona um termo do dicionário, mas não "Comercial"
			// — não corrobora o candidato fonético levantado para
			// "comersial".
			PrefixNearCaret: "Departamento Financeiro",
		},
	}

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	const want = "abra o modulo comersial"
	if got.FinalText != want {
		t.Fatalf("FinalText = %q, want %q (contexto enganoso não pode substituir)", got.FinalText, want)
	}
	if len(got.UncertainSpans) != 1 {
		t.Fatalf("UncertainSpans = %v, want 1 span pendente (marcado, mas não resolvido)", got.UncertainSpans)
	}
	if got.UncertainSpans[0].Original != "comersial" {
		t.Fatalf("UncertainSpans[0].Original = %q, want %q", got.UncertainSpans[0].Original, "comersial")
	}
}

// TestSmartResolver_MissingContextDegradesGracefully cobre "contexto
// ausente": sem nenhum dado de contexto (zero value) e sem cloud, Smart
// não quebra — degrada para o texto literal/dicionário, marcando o span
// ambíguo como incerto mas sem tentar adivinhar.
func TestSmartResolver_MissingContextDegradesGracefully(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", testUser, "Comercial", 0)

	sr := resolver.NewSmartResolver(repo)

	req := resolver.ResolveRequest{
		UserID:    testUser,
		ASRResult: asr.ASRResult{Text: "abra o modulo comersial"},
		// Context é o zero value: ausente.
	}

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.FinalText != "abra o modulo comersial" {
		t.Fatalf("FinalText = %q, want texto literal preservado", got.FinalText)
	}
	if len(got.UncertainSpans) != 1 {
		t.Fatalf("UncertainSpans = %v, want 1 span pendente", got.UncertainSpans)
	}
}

// TestSmartResolver_BlockedContextTreatedAsAbsent garante que um
// ContextSnapshot bloqueado (AC-036) é tratado exatamente como ausente —
// nunca usado para corroborar nada, mesmo que (por engano de quem monta
// o request) os campos textuais estivessem preenchidos.
func TestSmartResolver_BlockedContextTreatedAsAbsent(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", testUser, "Comercial", 0)

	sr := resolver.NewSmartResolver(repo)

	req := resolver.ResolveRequest{
		UserID:    testUser,
		ASRResult: asr.ASRResult{Text: "abra o modulo comersial"},
		Context: uiacontext.ContextSnapshot{
			Blocked:         true,
			PrefixNearCaret: "Departamento Comercial", // não deveria importar: Blocked.
		},
	}

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.FinalText != "abra o modulo comersial" {
		t.Fatalf("FinalText = %q, want texto literal preservado (contexto bloqueado)", got.FinalText)
	}
}

// TestAC024_CorrectSentenceStaysIntact: uma frase sem nenhuma ambiguidade
// (sem colisão de dicionário, sem confidence baixo, sem N-best divergente)
// sai do Smart idêntica à normalização mínima do Literal — Smart só altera
// o que tem evidência para alterar.
func TestAC024_CorrectSentenceStaysIntact(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", testUser, "Comercial", 0) // presente, mas irrelevante para esta frase.

	sr := resolver.NewSmartResolver(repo)

	req := resolver.ResolveRequest{
		UserID: testUser,
		ASRResult: asr.ASRResult{
			Text: "bom dia como vai voce",
		},
	}

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	const want = "bom dia como vai voce"
	if got.FinalText != want {
		t.Fatalf("FinalText = %q, want %q (frase já correta deve sair intacta)", got.FinalText, want)
	}
	if len(got.AppliedReplacements) != 0 {
		t.Fatalf("AppliedReplacements = %v, want vazio", got.AppliedReplacements)
	}
	if len(got.UncertainSpans) != 0 {
		t.Fatalf("UncertainSpans = %v, want vazio", got.UncertainSpans)
	}
}

// TestAC024_AliasReplacementIsAppliedWithoutContext: um alias JÁ
// CONFIRMADO no dicionário pessoal (docs/ai-spec/16-ai-behavior-spec.md,
// "Dictionary": aplicar uma variante conhecida não é "substituir
// distantemente apenas por existir") é aplicado mesmo sem corroboração de
// contexto — ao contrário de um match puramente fonético.
func TestAC024_AliasReplacementIsAppliedWithoutContext(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", testUser, "n8n", 0)
	putAlias(t, repo, "a1", "e1", "n oito n")

	sr := resolver.NewSmartResolver(repo)

	got, err := sr.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:    testUser,
		ASRResult: asr.ASRResult{Text: "instale o n oito n"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	const want = "instale o n8n"
	if got.FinalText != want {
		t.Fatalf("FinalText = %q, want %q", got.FinalText, want)
	}
	if len(got.AppliedReplacements) != 1 || got.AppliedReplacements[0].Reason != resolver.ReasonDictionary {
		t.Fatalf("AppliedReplacements = %v, want 1 replacement com Reason=dictionary", got.AppliedReplacements)
	}
}

// TestSmartResolver_UncertaintyProvenance é o teste central do item 3 da
// tarefa: quando o provider de ASR NÃO fornece confidence, nenhum span
// pode ser marcado com origem "asr_confidence" — a incerteza detectada
// (aqui, fonética) tem que carregar sua procedência real, nunca se
// disfarçar de confidence do ASR.
func TestSmartResolver_UncertaintyProvenance(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", testUser, "Comercial", 0)
	sr := resolver.NewSmartResolver(repo)

	t.Run("provider sem confidence", func(t *testing.T) {
		got, err := sr.Resolve(context.Background(), resolver.ResolveRequest{
			UserID: testUser,
			ASRResult: asr.ASRResult{
				Text:       "abra o modulo comersial",
				Confidence: asr.NoConfidence,
			},
		})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(got.UncertainSpans) == 0 {
			t.Fatal("esperava pelo menos um UncertainSpan (match fonético sem corroboração)")
		}
		for _, span := range got.UncertainSpans {
			for _, src := range span.Sources {
				if src == resolver.SpanSourceASRConfidence {
					t.Fatalf("span %q marcado com SpanSourceASRConfidence, mas o provider não forneceu confidence (Present=false)", span.Original)
				}
			}
		}
	})

	t.Run("provider com confidence baixo", func(t *testing.T) {
		got, err := sr.Resolve(context.Background(), resolver.ResolveRequest{
			UserID: testUser,
			ASRResult: asr.ASRResult{
				Text:       "abra o modulo comersial",
				Confidence: asr.NewConfidence(0.2),
			},
		})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		found := false
		for _, span := range got.UncertainSpans {
			for _, src := range span.Sources {
				if src == resolver.SpanSourceASRConfidence {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("esperava pelo menos um span com SpanSourceASRConfidence quando o provider fornece confidence baixo")
		}
	})
}

// TestSmartResolver_SelfCorrectionEndToEnd exercita os dois lados da
// autocorreção (ver também TestSelfCorrection_* em
// selfcorrection_internal_test.go, no nível do parser) agora passando
// pelo pipeline completo do SmartResolver.
func TestSmartResolver_SelfCorrectionEndToEnd(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	sr := resolver.NewSmartResolver(repo)

	t.Run("correção real é aplicada", func(t *testing.T) {
		got, err := sr.Resolve(context.Background(), resolver.ResolveRequest{
			UserID:    testUser,
			ASRResult: asr.ASRResult{Text: "vou amanha, nao, quero dizer terca"},
		})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.FinalText != "terca" {
			t.Fatalf("FinalText = %q, want %q", got.FinalText, "terca")
		}
	})

	t.Run("negação normal não é tocada", func(t *testing.T) {
		got, err := sr.Resolve(context.Background(), resolver.ResolveRequest{
			UserID:    testUser,
			ASRResult: asr.ASRResult{Text: "nao quero ir amanha"},
		})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.FinalText != "nao quero ir amanha" {
			t.Fatalf("FinalText = %q, want texto intocado", got.FinalText)
		}
	})
}

// TestSmartResolver_NoDictionary garante que Smart nunca quebra quando é
// construído sem DictionaryRepository (fallback explícito no doc do
// tipo).
func TestSmartResolver_NoDictionary(t *testing.T) {
	sr := &resolver.SmartResolver{}
	got, err := sr.Resolve(context.Background(), resolver.ResolveRequest{
		ASRResult: asr.ASRResult{Text: "  ola  mundo  "},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.FinalText != "ola mundo" {
		t.Fatalf("FinalText = %q, want %q", got.FinalText, "ola mundo")
	}
	if len(got.Warnings) == 0 {
		t.Fatal("esperava um warning documentando a degradação sem dicionário")
	}
}
