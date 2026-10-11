package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Correction é o espelho cloud de Correction (docs/ai-spec/05-data-model.md).
// TranscriptionID é nullable porque `ON DELETE SET NULL` é a política do
// data model quando a transcrição original é apagada.
type Correction struct {
	ID              string
	UserID          string
	TranscriptionID *string
	FromText        string
	ToText          string
	Language        *string
	ConfirmedByUser bool
	CreatedAt       time.Time
}

// CorrectionRepository é o contrato de persistência de corrections.
// O contrato cloud (docs/ai-spec/06-api-contract.md, "Corrections") só
// expõe POST — sem list/patch/delete — então este contrato tem só
// Create, mais o suporte mínimo a ON DELETE SET NULL que
// TranscriptionRepository.SoftDelete/Delete de outra entidade poderia
// exigir no futuro (fora do escopo desta slice: nenhuma rota chama
// isso hoje).
type CorrectionRepository interface {
	Create(ctx context.Context, c Correction) (Correction, error)
}

type InMemoryCorrectionRepository struct {
	mu   sync.Mutex
	byID map[string]Correction
}

func NewInMemoryCorrectionRepository() *InMemoryCorrectionRepository {
	return &InMemoryCorrectionRepository{byID: make(map[string]Correction)}
}

var _ CorrectionRepository = (*InMemoryCorrectionRepository)(nil)

func (r *InMemoryCorrectionRepository) Create(_ context.Context, c Correction) (Correction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byID[c.ID]; ok && existing.UserID == c.UserID {
		return existing, nil // idempotente: mesmo id, mesmo usuário.
	}
	r.byID[c.ID] = c
	return c, nil
}

// --- HTTP ---

type correctionDTO struct {
	ID              string    `json:"id"`
	TranscriptionID *string   `json:"transcription_id,omitempty"`
	FromText        string    `json:"from_text"`
	ToText          string    `json:"to_text"`
	Language        *string   `json:"language,omitempty"`
	ConfirmedByUser bool      `json:"confirmed_by_user"`
	CreatedAt       time.Time `json:"created_at"`
}

type createCorrectionRequest struct {
	ID              string  `json:"id"`
	TranscriptionID *string `json:"transcription_id"`
	FromText        string  `json:"from_text"`
	ToText          string  `json:"to_text"`
	Language        *string `json:"language"`
	ConfirmedByUser bool    `json:"confirmed_by_user"`
}

// handleCreateCorrection implementa POST /corrections
// (docs/ai-spec/06-api-contract.md, "Corrections"; AC-022: "Aprendizado
// exige confirmação" — confirmed_by_user é gravado tal como enviado; a
// regra de só aprender com correction confirmada é do dictionary
// engine, fora do escopo desta API).
func handleCreateCorrection(repo CorrectionRepository, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeJSON[createCorrectionRequest](w, r)
		if !ok {
			return
		}
		if strings.TrimSpace(body.ID) == "" || strings.TrimSpace(body.FromText) == "" {
			writeError(w, http.StatusBadRequest, codeValidation, "id e from_text são obrigatórios")
			return
		}

		c := Correction{
			ID: body.ID, UserID: principal.UserID, TranscriptionID: body.TranscriptionID,
			FromText: body.FromText, ToText: body.ToText, Language: body.Language,
			ConfirmedByUser: body.ConfirmedByUser, CreatedAt: now().UTC(),
		}
		result, err := repo.Create(r.Context(), c)
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao criar correction")
			return
		}
		writeJSON(w, http.StatusCreated, correctionDTO{
			ID: result.ID, TranscriptionID: result.TranscriptionID, FromText: result.FromText,
			ToText: result.ToText, Language: result.Language, ConfirmedByUser: result.ConfirmedByUser,
			CreatedAt: result.CreatedAt,
		})
	}
}
