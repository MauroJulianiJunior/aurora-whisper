package learning_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
)

// fatalOnUpsertRepo é o "repositório espião" pedido pela tarefa para
// AC-022: se Upsert for chamado, o teste falha imediatamente com
// t.Fatal, não com uma asserção depois do fato — prova que Confirm nunca
// toca no repositório antes de checar a confirmação.
type fatalOnUpsertRepo struct {
	t *testing.T
}

func (r fatalOnUpsertRepo) Upsert(context.Context, learning.Correction) (learning.Correction, error) {
	r.t.Fatal("AC-022 violado: CorrectionRepository.Upsert foi chamado sem Confirmation")
	return learning.Correction{}, nil
}

// TestConfirm_AC022_SemConfirmacaoNadaECriado prova AC-022: sem
// confirmação, nada é criado. Usa um repositório espião que falha o
// teste (t.Fatal) se Upsert acontecer.
func TestConfirm_AC022_SemConfirmacaoNadaECriado(t *testing.T) {
	spy := fatalOnUpsertRepo{t: t}
	suggestion, ok := learning.Classify("n oito n", "n8n")
	if !ok {
		t.Fatal("esperava Classify(...) ok=true")
	}

	_, err := learning.Confirm(
		context.Background(),
		learning.Deps{Corrections: spy},
		learning.ConfirmInput{UserID: "u1", Suggestion: suggestion},
		learning.Confirmation{}, // zero-value: NÃO confirmado, mesmo esquecido/omitido por engano.
	)
	if !errors.Is(err, learning.ErrNotConfirmed) {
		t.Fatalf("erro = %v, want ErrNotConfirmed", err)
	}
}

// TestConfirm_TEST008_NOitoN_ParaN8N é o TEST-008 da spec: editar
// "n oito n" para "n8n" e confirmar cria uma Correction sincronizável,
// com from_text/to_text corretos e confirmed_by_user verdadeiro.
func TestConfirm_TEST008_NOitoN_ParaN8N(t *testing.T) {
	corrections := learning.NewInMemoryCorrectionRepository()
	fakeClock := clock.NewFake(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	deps := learning.Deps{
		Corrections: corrections,
		Clock:       fakeClock,
		NewID:       sequentialIDs("corr"),
	}

	suggestion, ok := learning.Classify("n oito n", "n8n")
	if !ok {
		t.Fatal("esperava Classify(...) ok=true")
	}

	got, err := learning.Confirm(
		context.Background(),
		deps,
		learning.ConfirmInput{UserID: "user-1", Suggestion: suggestion},
		learning.ConfirmedByUser("test"),
	)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	if got.FromText != "n oito n" {
		t.Fatalf("FromText = %q, want %q", got.FromText, "n oito n")
	}
	if got.ToText != "n8n" {
		t.Fatalf("ToText = %q, want %q", got.ToText, "n8n")
	}
	if !got.ConfirmedByUser {
		t.Fatal("ConfirmedByUser = false, want true")
	}
	// "sincronizável": tem identidade e dono estáveis para o outbox
	// (internal/sync) pegar depois.
	if got.ID == "" {
		t.Fatal("ID vazio: Correction não é sincronizável")
	}
	if got.UserID != "user-1" {
		t.Fatalf("UserID = %q, want %q", got.UserID, "user-1")
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("CreatedAt/UpdatedAt zerados: Correction não é sincronizável")
	}
}

// TestConfirm_EdicaoGrande_NuncaViraAlias cobre "edição grande NUNCA vira
// alias silenciosamente": reescrever o texto inteiro gera no máximo uma
// sugestão (Classify nunca devolve uma lista, só uma Suggestion), e essa
// sugestão, mesmo confirmada, nunca cria ou toca em um DictionaryAlias —
// só a Correction.
func TestConfirm_EdicaoGrande_NuncaViraAlias(t *testing.T) {
	before := "a reunião de hoje foi muito produtiva e todos concordaram com o plano."
	after := "o encontro de hoje rendeu bastante e o time topou seguir com a proposta apresentada."

	suggestion, ok := learning.Classify(before, after)
	if !ok {
		t.Fatal("esperava Classify(...) ok=true (edição grande ainda gera uma Correction)")
	}
	if suggestion.AliasEligible {
		t.Fatalf("edição grande não deveria ser AliasEligible (reason=%q)", suggestion.Reason)
	}

	corrections := learning.NewInMemoryCorrectionRepository()
	dict := dictionary.NewInMemoryDictionaryRepository(nil)
	deps := learning.Deps{
		Corrections: corrections,
		Dictionary:  dict,
		Clock:       clock.NewFake(time.Now().UTC()),
		NewID:       sequentialIDs("large"),
	}

	got, err := learning.Confirm(
		context.Background(),
		deps,
		learning.ConfirmInput{UserID: "user-1", Suggestion: suggestion},
		learning.ConfirmedByUser("test"),
	)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !got.ConfirmedByUser {
		t.Fatal("Correction deveria ter sido gravada mesmo sem virar alias")
	}

	candidates, err := dict.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: after})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("edição grande envenenou o dicionário: %d candidato(s) criado(s): %+v", len(candidates), candidates)
	}
}

// TestConfirm_AliasSeguro_CriaEntradaEAlias cobre o caminho "quando
// seguro, cria alias de dicionário": confirmar TEST-008 com um
// DictionaryRepository injetado deve deixar "n8n" buscável tanto pela
// forma canônica quanto pelo alias aprendido "n oito n".
func TestConfirm_AliasSeguro_CriaEntradaEAlias(t *testing.T) {
	corrections := learning.NewInMemoryCorrectionRepository()
	dict := dictionary.NewInMemoryDictionaryRepository(nil)
	deps := learning.Deps{
		Corrections: corrections,
		Dictionary:  dict,
		Clock:       clock.NewFake(time.Now().UTC()),
		NewID:       sequentialIDs("alias"),
	}

	suggestion, ok := learning.Classify("n oito n", "n8n")
	if !ok {
		t.Fatal("esperava Classify(...) ok=true")
	}
	if !suggestion.AliasEligible {
		t.Fatal("esperava AliasEligible=true para 'n oito n' -> 'n8n'")
	}

	if _, err := learning.Confirm(
		context.Background(),
		deps,
		learning.ConfirmInput{UserID: "user-1", Suggestion: suggestion},
		learning.ConfirmedByUser("test"),
	); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	byAlias, err := dict.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "n oito n"})
	if err != nil {
		t.Fatalf("Search por alias: %v", err)
	}
	if len(byAlias) != 1 || byAlias[0].CanonicalForm != "n8n" {
		t.Fatalf("Search('n oito n') = %+v, want 1 candidato canônico 'n8n'", byAlias)
	}
	if byAlias[0].Reason != dictionary.MatchReasonAlias {
		t.Fatalf("Reason = %v, want MatchReasonAlias", byAlias[0].Reason)
	}
}

// TestConfirm_Idempotente_ConfirmarDuasVezes cobre a IDEMPOTÊNCIA pedida
// pela tarefa no nível do fluxo completo: confirmar a mesma correção
// duas vezes não cria duas Correction (mesmo ID, UseCount incrementado).
func TestConfirm_Idempotente_ConfirmarDuasVezes(t *testing.T) {
	corrections := learning.NewInMemoryCorrectionRepository()
	fakeClock := clock.NewFake(time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC))
	deps := learning.Deps{
		Corrections: corrections,
		Clock:       fakeClock,
		NewID:       sequentialIDs("dup"),
	}
	suggestion, ok := learning.Classify("n oito n", "n8n")
	if !ok {
		t.Fatal("esperava Classify(...) ok=true")
	}
	input := learning.ConfirmInput{UserID: "user-1", Suggestion: suggestion}

	first, err := learning.Confirm(context.Background(), deps, input, learning.ConfirmedByUser("test"))
	if err != nil {
		t.Fatalf("primeiro Confirm: %v", err)
	}

	fakeClock.Advance(time.Minute)
	second, err := learning.Confirm(context.Background(), deps, input, learning.ConfirmedByUser("test"))
	if err != nil {
		t.Fatalf("segundo Confirm: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("segundo Confirm criou uma Correction nova (ID=%q), esperava reaproveitar %q", second.ID, first.ID)
	}
	if second.UseCount != first.UseCount+1 {
		t.Fatalf("UseCount = %d, want %d", second.UseCount, first.UseCount+1)
	}
}

// sequentialIDs devolve um gerador de ID determinístico e previsível
// para teste (evita depender de storage.NewID em asserções).
func sequentialIDs(prefix string) func() string {
	n := 0
	return func() string {
		n++
		return prefix + "-" + strconv.Itoa(n)
	}
}
