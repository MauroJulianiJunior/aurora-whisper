package resolver_test

import (
	"context"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

const testUser = "user-1"

func strPtr(s string) *string { return &s }

// putEntry monta e insere uma DictionaryEntry ativa via Put (bypass de
// Upsert), como faz internal/dictionary/repository_memory_test.go.
func putEntry(t *testing.T, repo *dictionary.InMemoryDictionaryRepository, id, userID, canonical string, priority int) dictionary.DictionaryEntry {
	t.Helper()
	e := dictionary.DictionaryEntry{
		ID:             id,
		UserID:         userID,
		CanonicalForm:  canonical,
		NormalizedForm: dictionary.Normalize(canonical),
		Priority:       priority,
		Active:         true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		Version:        1,
	}
	repo.Put(e)
	return e
}

func putAlias(t *testing.T, repo *dictionary.InMemoryDictionaryRepository, id, entryID, alias string) dictionary.DictionaryAlias {
	t.Helper()
	a := dictionary.DictionaryAlias{
		ID:              id,
		EntryID:         entryID,
		Alias:           alias,
		NormalizedAlias: dictionary.Normalize(alias),
		Source:          dictionary.SourceManual,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	repo.PutAlias(a)
	return a
}

// poisonDictionary é um espião de dictionary.DictionaryRepository que
// falha o teste (t.Fatal) se qualquer método for chamado. Usado pelo
// AC-023 para provar que LiteralResolver nunca consulta o dicionário —
// embora, por construção (LiteralResolver não tem nenhum campo), nem
// exista onde encaixar este espião em LiteralResolver: ver
// literal_test.go.
type poisonDictionary struct {
	t     *testing.T
	Calls int
}

var _ dictionary.DictionaryRepository = (*poisonDictionary)(nil)

func (p *poisonDictionary) Search(context.Context, dictionary.DictionaryQuery) ([]dictionary.DictionaryCandidate, error) {
	p.Calls++
	p.t.Fatal("poisonDictionary.Search foi chamado: um resolver semântico foi acionado onde não deveria")
	return nil, nil
}

func (p *poisonDictionary) Upsert(context.Context, dictionary.DictionaryEntry) error {
	p.Calls++
	p.t.Fatal("poisonDictionary.Upsert foi chamado: um resolver semântico tentou persistir")
	return nil
}

func (p *poisonDictionary) UpsertAlias(context.Context, dictionary.DictionaryAlias) error {
	p.Calls++
	p.t.Fatal("poisonDictionary.UpsertAlias foi chamado: um resolver semântico tentou persistir")
	return nil
}
