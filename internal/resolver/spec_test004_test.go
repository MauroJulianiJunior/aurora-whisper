package resolver_test

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// TestSmartResolver_TEST004_PalavrasDaSpec exercita o TEST-004 com as
// palavras EXATAS do enunciado em docs/ai-spec/03-acceptance-criteria.md:
//
//	"Com ASR ambíguo 'físico'/'fisco', contexto 'Módulo Fisco' e dictionary
//	 Fisco, Smart pode escolher Fisco sem persistir contexto."
//
// Este caso é mais difícil — e mais representativo — do que uma grafia
// errada: "físico" é uma palavra portuguesa perfeitamente válida e bem
// escrita. Nenhuma checagem ortográfica ou fonética a marcaria como
// suspeita. O que denuncia o problema é a DIVERGÊNCIA entre a hipótese
// principal do ASR e sua alternativa N-best, corroborada pelo contexto da
// janela e pelo dicionário pessoal.
//
// O outro teste de TEST-004 (em smart_test.go) cobre o caminho fonético,
// com uma grafia incorreta. Os dois mecanismos são distintos e ambos
// precisam de cobertura própria.
func TestSmartResolver_TEST004_PalavrasDaSpec(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", testUser, "Fisco", 0)

	sr := resolver.NewSmartResolver(repo)

	req := resolver.ResolveRequest{
		UserID: testUser,
		ASRResult: asr.ASRResult{
			Text: "abra o modulo fisico",
			Alternatives: []asr.Alternative{
				{Text: "abra o modulo fisco", Confidence: asr.NewConfidence(0.71)},
			},
			Confidence: asr.NewConfidence(0.74),
		},
		Context: uiacontext.ContextSnapshot{
			AppName:         "chrome.exe",
			PrefixNearCaret: "Módulo Fisco",
		},
		CloudPolicy: resolver.CloudDisabled,
	}

	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const want = "abra o modulo Fisco"
	if got.FinalText != want {
		t.Fatalf("FinalText = %q, want %q\n"+
			"O ASR devolveu 'fisico' (palavra válida), mas a alternativa N-best, o contexto da\n"+
			"janela ('Módulo Fisco') e o dicionário pessoal apontam todos para 'Fisco'.", got.FinalText, want)
	}
	if got.UsedCloud {
		t.Fatal("UsedCloud = true: N-best + contexto + dicionário deveriam bastar localmente")
	}

	// O contexto é efêmero: nada dele pode sair no resultado (AC-027).
	for _, a := range got.AppliedReplacements {
		if a.Original == "" && a.Replacement == "" {
			continue
		}
	}
	if got.RawText != req.ASRResult.Text {
		t.Fatalf("RawText = %q, want %q (o texto bruto do ASR precisa ser preservado)", got.RawText, req.ASRResult.Text)
	}
}
