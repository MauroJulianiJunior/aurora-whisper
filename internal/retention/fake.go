package retention

import (
	"context"
	"sync"
	"time"
)

// Este arquivo contém implementações em memória dos três contratos de
// repositório deste pacote, usadas para validar os contratos nesta fase
// (F1-E), no mesmo espírito de InMemoryTranscriptionRepository em
// internal/storage. Não fazem parte da API de produção — servem para os
// testes deste pacote e, futuramente, dos pacotes que passarem a
// consumir Job diretamente.

// fakeTranscription é uma linha mínima de transcrição na memória do
// FakeTranscriptionRepository.
type fakeTranscription struct {
	id        string
	userID    string
	createdAt time.Time
	deletedAt *time.Time
}

// FakeTranscriptionRepository é uma implementação em memória, segura para
// concorrência, de TranscriptionRepository — mais os métodos de fixture
// (Create, SoftDelete, Restore) que um teste usa para montar o cenário
// antes de rodar o Job.
type FakeTranscriptionRepository struct {
	mu   sync.Mutex
	byID map[string]fakeTranscription
}

// NewFakeTranscriptionRepository constrói um repositório vazio.
func NewFakeTranscriptionRepository() *FakeTranscriptionRepository {
	return &FakeTranscriptionRepository{byID: make(map[string]fakeTranscription)}
}

var _ TranscriptionRepository = (*FakeTranscriptionRepository)(nil)

// Create insere uma transcrição ativa (fora da lixeira) com o CreatedAt
// informado. userID é opcional para os testes deste pacote, mas fica
// disponível para quando ele importar.
func (r *FakeTranscriptionRepository) Create(id, userID string, createdAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[id] = fakeTranscription{id: id, userID: userID, createdAt: createdAt}
}

// SoftDelete move a transcrição id para a lixeira, registrando deletedAt.
// Não calcula purge_at — quem decide "quando" é o Job, via
// EffectiveTrashPurgeAt, no momento de Run; o fake só guarda o fato
// "deletado em deletedAt", como o contrato pede.
func (r *FakeTranscriptionRepository) SoftDelete(id string, deletedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.byID[id]
	if !ok {
		return
	}
	t.deletedAt = &deletedAt
	r.byID[id] = t
}

// Restore tira a transcrição id da lixeira, se ela ainda existir (AC-018:
// "restore funciona enquanto não houver purge" — depois que o Job purga,
// o ID não existe mais aqui e Restore não tem efeito).
func (r *FakeTranscriptionRepository) Restore(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.byID[id]
	if !ok {
		return
	}
	t.deletedAt = nil
	r.byID[id] = t
}

// Exists devolve se id ainda está presente no repositório (nem purgado).
// Conveniência de asserção para os testes.
func (r *FakeTranscriptionRepository) Exists(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byID[id]
	return ok
}

// Count devolve quantas transcrições ainda existem no repositório (nem
// purgadas), somando ativas e lixeira.
func (r *FakeTranscriptionRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

func (r *FakeTranscriptionRepository) ActiveOlderThan(_ context.Context, cutoff time.Time) ([]TranscriptionRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []TranscriptionRef
	for _, t := range r.byID {
		if t.deletedAt != nil {
			continue
		}
		if t.createdAt.After(cutoff) {
			continue
		}
		out = append(out, TranscriptionRef{ID: t.id, UserID: t.userID, CreatedAt: t.createdAt})
	}
	return out, nil
}

func (r *FakeTranscriptionRepository) TrashItems(_ context.Context) ([]TrashedTranscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []TrashedTranscription
	for _, t := range r.byID {
		if t.deletedAt == nil {
			continue
		}
		out = append(out, TrashedTranscription{
			ID:        t.id,
			UserID:    t.userID,
			CreatedAt: t.createdAt,
			DeletedAt: *t.deletedAt,
		})
	}
	return out, nil
}

func (r *FakeTranscriptionRepository) Purge(_ context.Context, ids []string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	purged := 0
	for _, id := range ids {
		if _, ok := r.byID[id]; !ok {
			continue // já purgado (ou nunca existiu): idempotente, sem erro.
		}
		delete(r.byID, id)
		purged++
	}
	return purged, nil
}

// FakeCorrectionRepository é uma implementação em memória, segura para
// concorrência, de CorrectionRepository.
type FakeCorrectionRepository struct {
	mu   sync.Mutex
	byID map[string]*string // correction id -> transcription id (nil = já desvinculada)
}

// NewFakeCorrectionRepository constrói um repositório vazio.
func NewFakeCorrectionRepository() *FakeCorrectionRepository {
	return &FakeCorrectionRepository{byID: make(map[string]*string)}
}

var _ CorrectionRepository = (*FakeCorrectionRepository)(nil)

// Create insere uma correction vinculada a transcriptionID (a correção
// "aprendida" que deve sobreviver ao purge do ditado que a originou).
func (r *FakeCorrectionRepository) Create(correctionID, transcriptionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tid := transcriptionID
	r.byID[correctionID] = &tid
}

// TranscriptionIDOf devolve o TranscriptionID atual da correction (nil se
// já desvinculada). Conveniência de asserção para os testes.
func (r *FakeCorrectionRepository) TranscriptionIDOf(correctionID string) *string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[correctionID]
}

func (r *FakeCorrectionRepository) ClearTranscriptionRef(_ context.Context, transcriptionIDs []string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stale := make(map[string]struct{}, len(transcriptionIDs))
	for _, id := range transcriptionIDs {
		stale[id] = struct{}{}
	}
	cleared := 0
	for cid, tid := range r.byID {
		if tid == nil {
			continue
		}
		if _, match := stale[*tid]; !match {
			continue
		}
		r.byID[cid] = nil
		cleared++
	}
	return cleared, nil
}

// FakeSyncRepository é uma implementação em memória, segura para
// concorrência, de SyncRepository.
type FakeSyncRepository struct {
	mu        sync.Mutex
	events    map[string]time.Time
	mutations map[string]time.Time
}

// NewFakeSyncRepository constrói um repositório vazio.
func NewFakeSyncRepository() *FakeSyncRepository {
	return &FakeSyncRepository{
		events:    make(map[string]time.Time),
		mutations: make(map[string]time.Time),
	}
}

var _ SyncRepository = (*FakeSyncRepository)(nil)

// PutEvent insere um SyncEvent com o changed_at informado.
func (r *FakeSyncRepository) PutEvent(id string, changedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[id] = changedAt
}

// PutMutation insere um AppliedMutation com o applied_at informado.
func (r *FakeSyncRepository) PutMutation(id string, appliedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mutations[id] = appliedAt
}

func (r *FakeSyncRepository) PurgeSyncEventsOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	purged := 0
	for id, at := range r.events {
		if at.After(cutoff) {
			continue
		}
		delete(r.events, id)
		purged++
	}
	return purged, nil
}

func (r *FakeSyncRepository) PurgeAppliedMutationsOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	purged := 0
	for id, at := range r.mutations {
		if at.After(cutoff) {
			continue
		}
		delete(r.mutations, id)
		purged++
	}
	return purged, nil
}
