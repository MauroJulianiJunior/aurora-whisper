package resolver_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// TestAC023_LiteralResolverHasNoSemanticCollaborator é a prova
// ESTRUTURAL, por construção: LiteralResolver não tem nenhum campo (nem
// DictionaryRepository, nem ContextProvider, nem CloudClient, nem um
// SmartResolver embutido). Não há ONDE encaixar um "resolver semântico" —
// não é uma questão de disciplina de runtime, é impossível de compilar.
func TestAC023_LiteralResolverHasNoSemanticCollaborator(t *testing.T) {
	typ := reflect.TypeOf(resolver.LiteralResolver{})
	if typ.NumField() != 0 {
		t.Fatalf("LiteralResolver ganhou campo(s) (%d) — verifique se algum deles permite alcançar dicionário/contexto/cloud, o que quebraria a prova por construção do AC-023", typ.NumField())
	}
}

// TestAC023_LiteralNeverCallsSemanticResolver é a prova COMPORTAMENTAL,
// complementar à estrutural acima: um espião de DictionaryRepository que
// falha o teste se for chamado (poisonDictionary) existe no teste, mas
// LiteralResolver não tem como recebê-lo — não há parâmetro de
// NewLiteralResolver, nem campo, por onde ele entraria. Mesmo com um
// input ambíguo que o SmartResolver RESOLVERIA via dicionário/contexto
// (ver TestAC024... e TestSmartResolver_TEST004), o Literal devolve o
// texto praticamente intocado (só normalização mínima de espaços).
func TestAC023_LiteralNeverCallsSemanticResolver(t *testing.T) {
	spy := &poisonDictionary{t: t}
	_ = spy // nunca passado a LiteralResolver — prova por construção (ver acima).

	lit := resolver.NewLiteralResolver()

	req := resolver.ResolveRequest{
		UserID: testUser,
		ASRResult: asr.ASRResult{
			Text: "  abra   o modulo fisico  ",
		},
		// Contexto que, no Smart, corroboraria "Fisco" — o Literal precisa
		// ignorá-lo completamente.
		Context: uiacontext.ContextSnapshot{
			AppName:         "chrome.exe",
			PrefixNearCaret: "Módulo Fisco",
		},
		CloudPolicy: resolver.CloudAllowed,
	}

	got, err := lit.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: erro inesperado: %v", err)
	}

	if got.Mode != resolver.ModeLiteral {
		t.Fatalf("Mode = %v, want ModeLiteral", got.Mode)
	}
	if got.RawText != req.ASRResult.Text {
		t.Fatalf("RawText = %q, want %q (idêntico ao ASR, sem nenhuma modificação)", got.RawText, req.ASRResult.Text)
	}
	const wantFinal = "abra o modulo fisico"
	if got.FinalText != wantFinal {
		t.Fatalf("FinalText = %q, want %q (só normalização de espaços — 'fisico' NÃO pode virar 'Fisco')", got.FinalText, wantFinal)
	}
	if len(got.UncertainSpans) != 0 {
		t.Fatalf("UncertainSpans = %v, want vazio: Literal nunca marca incerteza", got.UncertainSpans)
	}
	if len(got.AppliedReplacements) != 0 {
		t.Fatalf("AppliedReplacements = %v, want vazio", got.AppliedReplacements)
	}
	if got.UsedCloud {
		t.Fatal("UsedCloud = true, want false: Literal nunca aciona o resolver cloud")
	}
	if spy.Calls != 0 {
		t.Fatalf("poisonDictionary foi chamado %d vez(es): Literal alcançou um resolver semântico", spy.Calls)
	}
}

func TestLiteral_PreservesNativePunctuationAndUnicodeWhitespace(t *testing.T) {
	lit := resolver.NewLiteralResolver()
	raw := "Olá, mundo!\tTudo bem?"
	got, err := lit.Resolve(context.Background(), resolver.ResolveRequest{
		ASRResult: asr.ASRResult{Text: raw},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	const want = "Olá, mundo! Tudo bem?"
	if got.FinalText != want {
		t.Fatalf("FinalText = %q, want %q", got.FinalText, want)
	}
	if got.RawText != raw {
		t.Fatalf("RawText = %q, want %q", got.RawText, raw)
	}
}

func TestLiteral_RespectsCancelledContext(t *testing.T) {
	lit := resolver.NewLiteralResolver()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := lit.Resolve(ctx, resolver.ResolveRequest{ASRResult: asr.ASRResult{Text: "x"}})
	if err == nil {
		t.Fatal("Resolve: want erro com ctx cancelado")
	}
}

// Garante em tempo de compilação que os tipos usados nos testes deste
// pacote continuam satisfazendo os contratos esperados (evita testes que
// silenciosamente param de exercitar o caminho pretendido se a interface
// mudar).
var (
	_ dictionary.DictionaryRepository = (*dictionary.InMemoryDictionaryRepository)(nil)
	_ resolver.Resolver               = (*resolver.LiteralResolver)(nil)
)
