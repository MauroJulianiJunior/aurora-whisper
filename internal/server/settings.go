package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// UserSettings é o espelho cloud de UserSettings
// (docs/ai-spec/05-data-model.md): só as preferências sincronizáveis —
// hotkey/microfone/modelo local ficam device-local por default
// (docs/ai-spec/06-api-contract.md, "Settings"). O conteúdo é opaco a
// este pacote (jsonb no Postgres real): validar CADA chave sincronizável
// é responsabilidade da camada de domínio que as define, não da API.
type UserSettings struct {
	UserID    string
	Settings  json.RawMessage
	UpdatedAt time.Time
	Version   int64
}

type SettingsRepository interface {
	Get(ctx context.Context, userID string) (UserSettings, error)
	Put(ctx context.Context, userID string, settings json.RawMessage, now time.Time) (UserSettings, error)
}

// InMemorySettingsRepository é a implementação em memória desta fase.
// Segura para uso concorrente. Get de um usuário sem settings gravados
// devolve um objeto vazio ("{}"), version 0 — nunca ErrNotFound: settings
// sempre "existe" conceitualmente (default vazio), só ainda não foi
// gravado.
type InMemorySettingsRepository struct {
	mu     sync.Mutex
	byUser map[string]UserSettings
}

func NewInMemorySettingsRepository() *InMemorySettingsRepository {
	return &InMemorySettingsRepository{byUser: make(map[string]UserSettings)}
}

var _ SettingsRepository = (*InMemorySettingsRepository)(nil)

func (r *InMemorySettingsRepository) Get(_ context.Context, userID string) (UserSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byUser[userID]
	if !ok {
		return UserSettings{UserID: userID, Settings: json.RawMessage(`{}`)}, nil
	}
	return s, nil
}

func (r *InMemorySettingsRepository) Put(_ context.Context, userID string, settings json.RawMessage, now time.Time) (UserSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.byUser[userID]
	s.UserID = userID
	s.Settings = settings
	s.UpdatedAt = now.UTC()
	s.Version++
	r.byUser[userID] = s
	return s, nil
}

// --- HTTP ---

type settingsDTO struct {
	Settings  json.RawMessage `json:"settings"`
	UpdatedAt time.Time       `json:"updated_at,omitempty"`
	Version   int64           `json:"version"`
}

func handleGetSettings(repo SettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		s, err := repo.Get(r.Context(), principal.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao carregar settings")
			return
		}
		writeJSON(w, http.StatusOK, settingsDTO{Settings: s.Settings, UpdatedAt: s.UpdatedAt, Version: s.Version})
	}
}

func handlePutSettings(repo SettingsRepository, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())

		var body json.RawMessage
		raw, ok := decodeJSON[json.RawMessage](w, r)
		if !ok {
			return
		}
		body = raw
		if len(body) == 0 {
			body = json.RawMessage(`{}`)
		}

		s, err := repo.Put(r.Context(), principal.UserID, body, now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao gravar settings")
			return
		}
		writeJSON(w, http.StatusOK, settingsDTO{Settings: s.Settings, UpdatedAt: s.UpdatedAt, Version: s.Version})
	}
}
