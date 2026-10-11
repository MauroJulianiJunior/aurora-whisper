package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Transcription é o espelho cloud de Transcription
// (docs/ai-spec/05-data-model.md, "Transcription" → PostgreSQL). Não
// carrega SyncState/ServerVersion/LastSyncError — esses campos são só
// do espelho local (internal/storage.Transcription, dono do Windows
// Agent); aqui é a fonte consolidada (docs/ai-spec/04-system-design.md:
// "Backend cloud é a fonte consolidada dos dados sincronizados").
//
// Nunca carrega áudio nem contexto de janela — invariante de produto
// (AC-027, AC-028), e nem existe campo aqui para isso.
type Transcription struct {
	ID             string
	UserID         string
	DeviceID       *string
	SourcePlatform string
	RawText        string
	FinalText      string
	Mode           string
	Language       *string
	ASRProvider    string
	ASRModel       *string
	DurationMs     *int64
	ProcessingMs   *int64
	Confidence     *float64
	TargetApp      *string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
	PurgeAt        *time.Time
	Version        int64
}

// ErrVersionConflict é devolvido por Update quando base_version não bate
// com a versão atual — concorrência otimista (docs/ai-spec/06-api-contract.md,
// "version").
var ErrVersionConflict = errors.New("server: versão desatualizada (conflito de concorrência)")

// ErrAlreadyExists é devolvido por Create quando o ID já pertence a
// outro registro (não deste usuário, ou com conteúdo diferente) — ver
// handleCreateTranscription para a política de idempotência de Create.
var ErrAlreadyExists = errors.New("server: id já usado por outro registro")

// TranscriptionListFilter — mesmos critérios de docs/ai-spec/06-api-contract.md,
// "GET /transcriptions" → "Query". UserID é sempre preenchido a partir
// da sessão pelo handler, nunca pelo cliente.
type TranscriptionListFilter struct {
	UserID   string
	Query    string
	From, To *time.Time
	DeviceID *string
	Mode     *string
	Trash    bool
	Cursor   string
	Limit    int
}

const (
	DefaultTranscriptionListLimit = 50
	MaxTranscriptionListLimit     = 100
)

type TranscriptionPage struct {
	Items      []Transcription
	NextCursor string
}

// TranscriptionRepository é o contrato de persistência cloud do
// histórico, consumido pelas rotas /transcriptions.
type TranscriptionRepository interface {
	List(ctx context.Context, filter TranscriptionListFilter) (TranscriptionPage, error)
	Get(ctx context.Context, userID, id string) (Transcription, error)
	// Create insere t (ID vindo do cliente — "transcription aceita ID
	// client-side", docs/ai-spec/06-api-contract.md, "Idempotência").
	// Se um registro com o mesmo ID já existe para este usuário, Create
	// é idempotente: devolve o registro existente sem erro (retry
	// seguro). Se o ID já existe para OUTRO usuário, devolve
	// ErrAlreadyExists (nunca sobrescreve nem revela o dono real).
	Create(ctx context.Context, t Transcription) (result Transcription, existed bool, err error)
	UpdateFinalText(ctx context.Context, userID, id string, baseVersion int64, finalText string) (Transcription, error)
	SoftDelete(ctx context.Context, userID, id string, now time.Time) error
	Restore(ctx context.Context, userID, id string) error
}

// InMemoryTranscriptionRepository é a implementação em memória desta
// fase (Postgres real no Sprint 6). Segura para uso concorrente.
type InMemoryTranscriptionRepository struct {
	mu   sync.Mutex
	byID map[string]Transcription
}

func NewInMemoryTranscriptionRepository() *InMemoryTranscriptionRepository {
	return &InMemoryTranscriptionRepository{byID: make(map[string]Transcription)}
}

var _ TranscriptionRepository = (*InMemoryTranscriptionRepository)(nil)

func (r *InMemoryTranscriptionRepository) List(_ context.Context, filter TranscriptionListFilter) (TranscriptionPage, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultTranscriptionListLimit
	}
	if limit > MaxTranscriptionListLimit {
		limit = MaxTranscriptionListLimit
	}

	var cursor *listCursorKey
	if filter.Cursor != "" {
		ck, err := decodeListCursor(filter.Cursor)
		if err != nil {
			return TranscriptionPage{}, err
		}
		cursor = &ck
	}

	query := strings.ToLower(strings.TrimSpace(filter.Query))

	r.mu.Lock()
	matched := make([]Transcription, 0, len(r.byID))
	for _, t := range r.byID {
		if t.UserID != filter.UserID {
			continue
		}
		if filter.Trash && t.DeletedAt == nil {
			continue
		}
		if !filter.Trash && t.DeletedAt != nil {
			continue
		}
		if filter.DeviceID != nil && (t.DeviceID == nil || *t.DeviceID != *filter.DeviceID) {
			continue
		}
		if filter.Mode != nil && t.Mode != *filter.Mode {
			continue
		}
		if filter.From != nil && t.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && t.CreatedAt.After(*filter.To) {
			continue
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(t.RawText), query) &&
			!strings.Contains(strings.ToLower(t.FinalText), query) {
			continue
		}
		matched = append(matched, t)
	}
	r.mu.Unlock()

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].ID > matched[j].ID
		}
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	if cursor != nil {
		start := 0
		for start < len(matched) {
			k := listCursorKey{createdAt: matched[start].CreatedAt, id: matched[start].ID}
			if k.after(*cursor) {
				break
			}
			start++
		}
		matched = matched[start:]
	}

	page := TranscriptionPage{}
	if len(matched) > limit {
		page.Items = matched[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeListCursor(listCursorKey{createdAt: last.CreatedAt, id: last.ID})
	} else {
		page.Items = matched
	}
	if page.Items == nil {
		page.Items = []Transcription{}
	}
	return page, nil
}

func (r *InMemoryTranscriptionRepository) Get(_ context.Context, userID, id string) (Transcription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return Transcription{}, ErrNotFound
	}
	return t, nil
}

func (r *InMemoryTranscriptionRepository) Create(_ context.Context, t Transcription) (Transcription, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.byID[t.ID]; ok {
		if existing.UserID != t.UserID {
			return Transcription{}, false, ErrAlreadyExists
		}
		return existing, true, nil
	}

	// "done", como o Agent grava (05-data-model.md; a 0003 só aceita done e
	// error). Era "completed" até a F2 da ADR-019.
	t.Status = "done"
	t.Version = 1
	r.byID[t.ID] = t
	return t, false, nil
}

func (r *InMemoryTranscriptionRepository) UpdateFinalText(_ context.Context, userID, id string, baseVersion int64, finalText string) (Transcription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return Transcription{}, ErrNotFound
	}
	if t.Version != baseVersion {
		return Transcription{}, ErrVersionConflict
	}

	t.FinalText = finalText
	t.Version++
	t.UpdatedAt = time.Now().UTC()
	r.byID[id] = t
	return t, nil
}

func (r *InMemoryTranscriptionRepository) SoftDelete(_ context.Context, userID, id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return ErrNotFound
	}

	nowUTC := now.UTC()
	purgeAt := nowUTC.AddDate(0, 0, 30) // lixeira: 30 dias (05-data-model.md, "Retenção").
	t.DeletedAt = &nowUTC
	t.PurgeAt = &purgeAt
	t.UpdatedAt = nowUTC
	t.Version++
	r.byID[id] = t
	return nil
}

func (r *InMemoryTranscriptionRepository) Restore(_ context.Context, userID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return ErrNotFound
	}

	t.DeletedAt = nil
	t.PurgeAt = nil
	t.UpdatedAt = time.Now().UTC()
	t.Version++
	r.byID[id] = t
	return nil
}

// --- HTTP ---

type transcriptionDTO struct {
	ID             string     `json:"id"`
	DeviceID       *string    `json:"device_id,omitempty"`
	SourcePlatform string     `json:"source_platform"`
	RawText        string     `json:"raw_text"`
	FinalText      string     `json:"final_text"`
	Mode           string     `json:"mode"`
	Language       *string    `json:"language,omitempty"`
	ASRProvider    string     `json:"asr_provider"`
	ASRModel       *string    `json:"asr_model,omitempty"`
	DurationMs     *int64     `json:"duration_ms,omitempty"`
	ProcessingMs   *int64     `json:"processing_ms,omitempty"`
	Confidence     *float64   `json:"confidence,omitempty"`
	TargetApp      *string    `json:"target_app,omitempty"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	Version        int64      `json:"version"`
}

func toTranscriptionDTO(t Transcription) transcriptionDTO {
	return transcriptionDTO{
		ID: t.ID, DeviceID: t.DeviceID, SourcePlatform: t.SourcePlatform,
		RawText: t.RawText, FinalText: t.FinalText, Mode: t.Mode, Language: t.Language,
		ASRProvider: t.ASRProvider, ASRModel: t.ASRModel, DurationMs: t.DurationMs,
		ProcessingMs: t.ProcessingMs, Confidence: t.Confidence, TargetApp: t.TargetApp,
		Status: t.Status, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, DeletedAt: t.DeletedAt,
		Version: t.Version,
	}
}

func handleListTranscriptions(repo TranscriptionRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		q := r.URL.Query()

		filter := TranscriptionListFilter{
			UserID: principal.UserID,
			Query:  q.Get("q"),
			Cursor: q.Get("cursor"),
			Trash:  q.Get("trash") == "true",
		}
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				filter.Limit = n
			}
		}
		if v := q.Get("device_id"); v != "" {
			filter.DeviceID = &v
		}
		if v := q.Get("mode"); v != "" {
			filter.Mode = &v
		}
		if v := q.Get("from"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				filter.From = &t
			}
		}
		if v := q.Get("to"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				filter.To = &t
			}
		}

		page, err := repo.List(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "cursor inválido")
			return
		}
		dtos := make([]transcriptionDTO, 0, len(page.Items))
		for _, t := range page.Items {
			dtos = append(dtos, toTranscriptionDTO(t))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":       dtos,
			"next_cursor": page.NextCursor,
		})
	}
}

// createTranscriptionRequest espelha o corpo de POST /transcriptions
// (docs/ai-spec/06-api-contract.md). Sem campo UserID de propósito
// (ver httpx.go, decodeJSON).
type createTranscriptionRequest struct {
	ID             string     `json:"id"`
	DeviceID       *string    `json:"device_id"`
	SourcePlatform string     `json:"source_platform"`
	RawText        string     `json:"raw_text"`
	FinalText      string     `json:"final_text"`
	Mode           string     `json:"mode"`
	Language       *string    `json:"language"`
	ASRProvider    string     `json:"asr_provider"`
	ASRModel       *string    `json:"asr_model"`
	DurationMs     *int64     `json:"duration_ms"`
	ProcessingMs   *int64     `json:"processing_ms"`
	Confidence     *float64   `json:"confidence"`
	TargetApp      *string    `json:"target_app"`
	CreatedAt      *time.Time `json:"created_at"`
}

// handleCreateTranscription implementa POST /transcriptions. O ID vem
// do cliente e o Create é idempotente nele (docs/ai-spec/06-api-contract.md,
// "Idempotência": "transcription aceita ID client-side" e "retries não
// podem duplicar records") — reenviar o mesmo POST com o mesmo id e
// mesmo usuário devolve 200 com o registro já existente, em vez de
// criar um segundo. platform="android" é aceito sem tratamento
// especial (AC-042): source_platform é só um texto guardado, nenhuma
// validação aqui depende de Win32.
func handleCreateTranscription(repo TranscriptionRepository, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeJSON[createTranscriptionRequest](w, r)
		if !ok {
			return
		}
		if strings.TrimSpace(body.ID) == "" || strings.TrimSpace(body.SourcePlatform) == "" {
			writeError(w, http.StatusBadRequest, codeValidation, "id e source_platform são obrigatórios")
			return
		}

		createdAt := now().UTC()
		if body.CreatedAt != nil {
			createdAt = body.CreatedAt.UTC()
		}

		t := Transcription{
			ID: body.ID, UserID: principal.UserID, DeviceID: body.DeviceID,
			SourcePlatform: body.SourcePlatform, RawText: body.RawText, FinalText: body.FinalText,
			Mode: body.Mode, Language: body.Language, ASRProvider: body.ASRProvider, ASRModel: body.ASRModel,
			DurationMs: body.DurationMs, ProcessingMs: body.ProcessingMs, Confidence: body.Confidence,
			TargetApp: body.TargetApp, CreatedAt: createdAt, UpdatedAt: createdAt,
		}

		result, existed, err := repo.Create(r.Context(), t)
		if errors.Is(err, ErrAlreadyExists) {
			writeError(w, http.StatusConflict, codeConflict, "id já usado por outro registro")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao criar transcrição")
			return
		}
		status := http.StatusCreated
		if existed {
			status = http.StatusOK
		}
		writeJSON(w, status, toTranscriptionDTO(result))
	}
}

type patchTranscriptionRequest struct {
	BaseVersion int64  `json:"base_version"`
	FinalText   string `json:"final_text"`
}

func handlePatchTranscription(repo TranscriptionRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		id := r.PathValue("id")

		body, ok := decodeJSON[patchTranscriptionRequest](w, r)
		if !ok {
			return
		}

		t, err := repo.UpdateFinalText(r.Context(), principal.UserID, id, body.BaseVersion, body.FinalText)
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, codeNotFound, "transcrição não encontrada")
		case errors.Is(err, ErrVersionConflict):
			writeError(w, http.StatusConflict, codeConflict, "Entity version is stale")
		case err != nil:
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao editar transcrição")
		default:
			writeJSON(w, http.StatusOK, toTranscriptionDTO(t))
		}
	}
}

func handleDeleteTranscription(repo TranscriptionRepository, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		id := r.PathValue("id")

		err := repo.SoftDelete(r.Context(), principal.UserID, id, now())
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "transcrição não encontrada")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao apagar transcrição")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRestoreTranscription(repo TranscriptionRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		id := r.PathValue("id")

		err := repo.Restore(r.Context(), principal.UserID, id)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "transcrição não encontrada")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao restaurar transcrição")
			return
		}
		t, err := repo.Get(r.Context(), principal.UserID, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao restaurar transcrição")
			return
		}
		writeJSON(w, http.StatusOK, toTranscriptionDTO(t))
	}
}
