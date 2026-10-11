package learning

import (
	"context"
	"errors"
	"fmt"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// ErrNotConfirmed é devolvido por Confirm quando confirmation não é uma
// Confirmation "sim" (ConfirmedByUser). Nenhum repositório é tocado antes
// dessa checagem — é a garantia estrutural de AC-022.
var ErrNotConfirmed = errors.New("learning: aprendizado exige confirmação explícita do usuário (AC-022)")

// ErrMissingUserID é devolvido por Confirm quando input.UserID está
// vazio.
var ErrMissingUserID = errors.New("learning: UserID é obrigatório")

// ErrEmptySuggestion é devolvido por Confirm quando input.Suggestion não
// veio de um Classify bem-sucedido (Before/After vazios).
var ErrEmptySuggestion = errors.New("learning: Suggestion vazia")

// Confirmation representa uma confirmação explícita do usuário para
// aprender uma correção (AC-022). O único campo é não exportado de
// propósito: nenhum outro pacote consegue montar uma Confirmation com
// "sim" sem passar pelo construtor ConfirmedByUser, e o zero-value
// (Confirmation{}, variável esquecida, campo de struct não preenchido)
// é sempre "não confirmado" — ver doc.go, seção AC-022, para o
// raciocínio completo.
type Confirmation struct {
	confirmed bool
	// source é só para auditoria (ex.: "dashboard.history.edit_dialog")
	// — nunca texto do usuário (AC-027/AC-028: nada de conteúdo
	// ditado/editado em log).
	source string
}

// ConfirmedByUser é o único construtor de uma Confirmation "sim". Deve
// ser chamado apenas no ponto exato em que a UI recebeu a confirmação
// explícita do usuário (docs/ai-spec/16-ai-behavior-spec.md,
// "Human-in-the-loop": "aprender correção" está na lista do que exige
// confirmação). source identifica esse ponto para auditoria (ex.: nome
// da tela/diálogo) — não deve conter o texto corrigido.
func ConfirmedByUser(source string) Confirmation {
	return Confirmation{confirmed: true, source: source}
}

// Deps agrupa as dependências injetadas de Confirm, no mesmo espírito de
// internal/clock (ADR-007, seção 8): nada aqui chama time.Now() direto,
// nem gera ID sem passar pelo gerador injetado, para que o fluxo inteiro
// seja determinístico em teste.
type Deps struct {
	// Corrections é obrigatório.
	Corrections CorrectionRepository

	// Dictionary é opcional. Quando nil, Confirm nunca tenta criar ou
	// atualizar um DictionaryAlias, mesmo que a Suggestion seja
	// AliasEligible — grava só a Correction. Isso permite usar este
	// pacote sem acoplar quem só quer o histórico de correções ao
	// dicionário.
	Dictionary dictionary.DictionaryRepository

	// Clock é o relógio injetável (internal/clock). Nil usa
	// clock.System{} — o mesmo default de produção do resto do núcleo.
	Clock clock.Clock

	// NewID gera o ID de novas entidades (Correction, e, quando
	// necessário, DictionaryEntry/DictionaryAlias). Nil usa
	// storage.NewID.
	NewID func() string
}

func (d Deps) clock() clock.Clock {
	if d.Clock != nil {
		return d.Clock
	}
	return clock.System{}
}

func (d Deps) newID() func() string {
	if d.NewID != nil {
		return d.NewID
	}
	return storage.NewID
}

// ConfirmInput descreve o que está sendo confirmado: a Suggestion
// devolvida por Classify, mais o contexto (usuário, transcrição de
// origem, idioma) necessário para persistir a Correction.
type ConfirmInput struct {
	UserID string

	// TranscriptionID é opcional (a edição pode não ter uma transcrição
	// de origem rastreável). Correction.TranscriptionID usa
	// `ON DELETE SET NULL` no data model — este pacote só passa o valor
	// adiante, não impõe a FK.
	TranscriptionID *string

	// Language é opcional (docs/ai-spec/05-data-model.md,
	// Correction.language "nullable").
	Language *string

	// Suggestion deve vir de um Classify(before, after) que devolveu
	// ok=true. Confirm rejeita uma Suggestion zero-value (ver
	// ErrEmptySuggestion).
	Suggestion Suggestion
}

// Confirm é o único caminho deste pacote que grava dado. Ele:
//
//  1. exige uma Confirmation "sim" (AC-022) antes de tocar em qualquer
//     repositório — devolve ErrNotConfirmed caso contrário, e um
//     repositório espião pode prová-lo falhando o teste se Upsert for
//     chamado nesse caso;
//  2. grava a Correction (sempre com ConfirmedByUser=true — este pacote
//     nunca escreve uma Correction não confirmada), de forma idempotente
//     (confirmar o mesmo par (from, to) de novo incrementa UseCount, não
//     duplica — ver InMemoryCorrectionRepository.Upsert);
//  3. quando deps.Dictionary não é nil e input.Suggestion.AliasEligible é
//     true, tenta também criar/atualizar o DictionaryAlias
//     correspondente (Alias = Suggestion.Before, apontando para o
//     DictionaryEntry de CanonicalForm = Suggestion.After) — nunca ao
//     contrário: uma Suggestion não elegível (edição grande) jamais gera
//     alias, mesmo confirmada.
//
// Devolve a Correction efetivamente persistida. Se a etapa de alias
// falhar, a Correction já gravada é devolvida junto com o erro — o
// aprendizado do histórico não fica refém de o dicionário aceitar o
// alias.
func Confirm(ctx context.Context, deps Deps, input ConfirmInput, confirmation Confirmation) (Correction, error) {
	if !confirmation.confirmed {
		return Correction{}, ErrNotConfirmed
	}
	if deps.Corrections == nil {
		return Correction{}, errors.New("learning: Deps.Corrections é obrigatório")
	}
	if input.UserID == "" {
		return Correction{}, ErrMissingUserID
	}
	if input.Suggestion.Before == "" || input.Suggestion.After == "" {
		return Correction{}, ErrEmptySuggestion
	}

	newID := deps.newID()
	now := deps.clock().Now()

	saved, err := deps.Corrections.Upsert(ctx, Correction{
		ID:              newID(),
		UserID:          input.UserID,
		TranscriptionID: input.TranscriptionID,
		FromText:        input.Suggestion.Before,
		ToText:          input.Suggestion.After,
		Language:        input.Language,
		ConfirmedByUser: true,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return Correction{}, fmt.Errorf("learning: gravar correction: %w", err)
	}

	if deps.Dictionary == nil || !input.Suggestion.AliasEligible {
		return saved, nil
	}

	if err := upsertLearnedAlias(ctx, deps.Dictionary, newID, input.UserID, input.Language, input.Suggestion); err != nil {
		return saved, fmt.Errorf("learning: correction gravada, mas alias falhou: %w", err)
	}
	return saved, nil
}

// upsertLearnedAlias garante que existe um DictionaryEntry cuja
// CanonicalForm é suggestion.After (criando um novo se preciso) e
// registra suggestion.Before como DictionaryAlias(source=learned) dele.
// Só é chamada por Confirm depois de uma Confirmation "sim" e com
// AliasEligible=true (ver Confirm).
func upsertLearnedAlias(ctx context.Context, repo dictionary.DictionaryRepository, newID func() string, userID string, language *string, s Suggestion) error {
	// O alias é o TRECHO que mudou, não o texto inteiro.
	//
	// Até 27-set-2026 usava s.Before/s.After, e como AliasEligible
	// limita por número de palavras, uma correção de uma palavra dentro
	// de uma frase longa nunca chegava aqui. Ver Suggestion.AliasFrom.
	entryID, err := resolveCanonicalEntryID(ctx, repo, newID, userID, language, s.AliasTo)
	if err != nil {
		return err
	}

	return repo.UpsertAlias(ctx, dictionary.DictionaryAlias{
		ID:      newID(),
		EntryID: entryID,
		Alias:   s.AliasFrom,
		Source:  dictionary.SourceLearned,
	})
}

// resolveCanonicalEntryID acha o DictionaryEntry cuja CanonicalForm é
// exatamente canonical (para este usuário/idioma), criando-o se ainda não
// existir. DictionaryRepository.Upsert não devolve a entrada persistida,
// e uma entrada já existente com a mesma chave de negócio
// (UserID, NormalizedForm) pode manter um ID diferente do gerado aqui —
// por isso a busca é refeita depois do Upsert, em vez de assumir que o ID
// gerado é o que valeu.
func resolveCanonicalEntryID(ctx context.Context, repo dictionary.DictionaryRepository, newID func() string, userID string, language *string, canonical string) (string, error) {
	if id, ok, err := findExactEntry(ctx, repo, userID, language, canonical); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}

	if err := repo.Upsert(ctx, dictionary.DictionaryEntry{
		ID:            newID(),
		UserID:        userID,
		CanonicalForm: canonical,
		Language:      language,
		Active:        true,
	}); err != nil {
		return "", err
	}

	id, ok, err := findExactEntry(ctx, repo, userID, language, canonical)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("learning: entrada de dicionário %q não encontrada após upsert", canonical)
	}
	return id, nil
}

func findExactEntry(ctx context.Context, repo dictionary.DictionaryRepository, userID string, language *string, canonical string) (string, bool, error) {
	candidates, err := repo.Search(ctx, dictionary.DictionaryQuery{
		UserID:   userID,
		Term:     canonical,
		Language: language,
		Limit:    10,
	})
	if err != nil {
		return "", false, err
	}
	for _, c := range candidates {
		if c.Reason == dictionary.MatchReasonExact && c.CanonicalForm == canonical {
			return c.EntryID, true, nil
		}
	}
	return "", false, nil
}
