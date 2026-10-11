package storage

import (
	"context"
	"sync"
	"time"
)

// InMemorySettingsRepository é a implementação em memória de
// SettingsRepository. Serve aos testes e é exercitada pela MESMA suíte
// da versão SQLite (settings_memory_test.go, paraCadaSettings). É segura
// para uso concorrente.
type InMemorySettingsRepository struct {
	mu      sync.Mutex
	current *Settings
}

// NewInMemorySettingsRepository constrói um repositório sem nenhuma
// configuração salva ainda.
func NewInMemorySettingsRepository() *InMemorySettingsRepository {
	return &InMemorySettingsRepository{}
}

var _ SettingsRepository = (*InMemorySettingsRepository)(nil)

func (r *InMemorySettingsRepository) Get(_ context.Context) (Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current == nil {
		return Settings{}, ErrNotFound
	}
	return *r.current, nil
}

func (r *InMemorySettingsRepository) Put(_ context.Context, s Settings, baseVersion int64) (Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var atual int64
	if r.current != nil {
		atual = r.current.Version
	}
	if baseVersion != atual {
		return Settings{}, ErrVersionConflict
	}
	s.Version = atual + 1
	s.UpdatedAt = time.Now().UTC()

	r.current = &s
	return *r.current, nil
}
