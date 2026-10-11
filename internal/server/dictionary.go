package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// DictionaryEntry é o espelho cloud de DictionaryEntry
// (docs/ai-spec/05-data-model.md). Aliases fica embutido aqui (em vez de
// uma tabela DictionaryAlias própria) porque o contrato só expõe
// dictionary como recurso — docs/ai-spec/06-api-contract.md não declara
// rotas de alias separadas, e o corpo de POST /dictionary já traz
// "aliases" como parte do entry.
type DictionaryEntry struct {
	ID             string
	UserID         string
	CanonicalForm  string
	NormalizedForm string
	Language       *string
	Domain         *string
	Priority       int
	Active         bool
	Aliases        []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int64
}

func normalizeDictionaryForm(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ErrDuplicateEntry é devolvido por Create quando já existe um entry
// deste usuário com o mesmo normalized_form — unicidade
// `(user_id, normalized_form)` de docs/ai-spec/05-data-model.md.
var ErrDuplicateEntry = errors.New("server: dictionary entry duplicado para este usuário")

type DictionaryRepository interface {
	List(ctx context.Context, userID string) ([]DictionaryEntry, error)
	Get(ctx context.Context, userID, id string) (DictionaryEntry, error)
	Create(ctx context.Context, e DictionaryEntry) (DictionaryEntry, error)
	Update(ctx context.Context, userID, id string, patch func(*DictionaryEntry)) (DictionaryEntry, error)
	Delete(ctx context.Context, userID, id string) error
}

// InMemoryDictionaryRepository é a implementação em memória desta fase.
// Segura para uso concorrente.
type InMemoryDictionaryRepository struct {
	mu   sync.Mutex
	byID map[string]DictionaryEntry
}

func NewInMemoryDictionaryRepository() *InMemoryDictionaryRepository {
	return &InMemoryDictionaryRepository{byID: make(map[string]DictionaryEntry)}
}

var _ DictionaryRepository = (*InMemoryDictionaryRepository)(nil)

func (r *InMemoryDictionaryRepository) List(_ context.Context, userID string) ([]DictionaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]DictionaryEntry, 0, len(r.byID))
	for _, e := range r.byID {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CanonicalForm < out[j].CanonicalForm })
	return out, nil
}

func (r *InMemoryDictionaryRepository) Get(_ context.Context, userID, id string) (DictionaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byID[id]
	if !ok || e.UserID != userID {
		return DictionaryEntry{}, ErrNotFound
	}
	return e, nil
}

func (r *InMemoryDictionaryRepository) Create(_ context.Context, e DictionaryEntry) (DictionaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.byID {
		if existing.UserID == e.UserID && existing.NormalizedForm == e.NormalizedForm {
			return DictionaryEntry{}, ErrDuplicateEntry
		}
	}

	e.Active = true
	e.Version = 1
	r.byID[e.ID] = e
	return e, nil
}

func (r *InMemoryDictionaryRepository) Update(_ context.Context, userID, id string, patch func(*DictionaryEntry)) (DictionaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.byID[id]
	if !ok || e.UserID != userID {
		return DictionaryEntry{}, ErrNotFound
	}
	patch(&e)
	e.Version++
	e.UpdatedAt = time.Now().UTC()
	r.byID[id] = e
	return e, nil
}

func (r *InMemoryDictionaryRepository) Delete(_ context.Context, userID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byID[id]
	if !ok || e.UserID != userID {
		return ErrNotFound
	}
	delete(r.byID, id)
	return nil
}

// --- HTTP ---

type dictionaryDTO struct {
	ID            string    `json:"id"`
	CanonicalForm string    `json:"canonical_form"`
	Language      *string   `json:"language,omitempty"`
	Domain        *string   `json:"domain,omitempty"`
	Priority      int       `json:"priority"`
	Active        bool      `json:"active"`
	Aliases       []string  `json:"aliases,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Version       int64     `json:"version"`
}

func toDictionaryDTO(e DictionaryEntry) dictionaryDTO {
	return dictionaryDTO{
		ID: e.ID, CanonicalForm: e.CanonicalForm, Language: e.Language, Domain: e.Domain,
		Priority: e.Priority, Active: e.Active, Aliases: e.Aliases,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, Version: e.Version,
	}
}

func handleListDictionary(repo DictionaryRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		entries, err := repo.List(r.Context(), principal.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao listar dictionary")
			return
		}
		dtos := make([]dictionaryDTO, 0, len(entries))
		for _, e := range entries {
			dtos = append(dtos, toDictionaryDTO(e))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": dtos})
	}
}

type dictionaryRequest struct {
	ID            string   `json:"id"`
	CanonicalForm string   `json:"canonical_form"`
	Language      *string  `json:"language"`
	Domain        *string  `json:"domain"`
	Priority      int      `json:"priority"`
	Aliases       []string `json:"aliases"`
}

func handleCreateDictionary(repo DictionaryRepository, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeJSON[dictionaryRequest](w, r)
		if !ok {
			return
		}
		if strings.TrimSpace(body.ID) == "" || strings.TrimSpace(body.CanonicalForm) == "" {
			writeError(w, http.StatusBadRequest, codeValidation, "id e canonical_form são obrigatórios")
			return
		}

		nowUTC := now().UTC()
		e := DictionaryEntry{
			ID: body.ID, UserID: principal.UserID, CanonicalForm: body.CanonicalForm,
			NormalizedForm: normalizeDictionaryForm(body.CanonicalForm), Language: body.Language,
			Domain: body.Domain, Priority: body.Priority, Aliases: body.Aliases,
			CreatedAt: nowUTC, UpdatedAt: nowUTC,
		}

		result, err := repo.Create(r.Context(), e)
		if errors.Is(err, ErrDuplicateEntry) {
			writeError(w, http.StatusConflict, codeConflict, "já existe um dictionary entry equivalente")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao criar dictionary entry")
			return
		}
		writeJSON(w, http.StatusCreated, toDictionaryDTO(result))
	}
}

type patchDictionaryRequest struct {
	CanonicalForm *string  `json:"canonical_form"`
	Language      *string  `json:"language"`
	Domain        *string  `json:"domain"`
	Priority      *int     `json:"priority"`
	Active        *bool    `json:"active"`
	Aliases       []string `json:"aliases"`
}

func handlePatchDictionary(repo DictionaryRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		id := r.PathValue("id")

		body, ok := decodeJSON[patchDictionaryRequest](w, r)
		if !ok {
			return
		}

		e, err := repo.Update(r.Context(), principal.UserID, id, func(e *DictionaryEntry) {
			if body.CanonicalForm != nil {
				e.CanonicalForm = *body.CanonicalForm
				e.NormalizedForm = normalizeDictionaryForm(*body.CanonicalForm)
			}
			if body.Language != nil {
				e.Language = body.Language
			}
			if body.Domain != nil {
				e.Domain = body.Domain
			}
			if body.Priority != nil {
				e.Priority = *body.Priority
			}
			if body.Active != nil {
				e.Active = *body.Active
			}
			if body.Aliases != nil {
				e.Aliases = body.Aliases
			}
		})
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "dictionary entry não encontrado")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao editar dictionary entry")
			return
		}
		writeJSON(w, http.StatusOK, toDictionaryDTO(e))
	}
}

func handleDeleteDictionary(repo DictionaryRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		id := r.PathValue("id")

		err := repo.Delete(r.Context(), principal.UserID, id)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "dictionary entry não encontrado")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao apagar dictionary entry")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
