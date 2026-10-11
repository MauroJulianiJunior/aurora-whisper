package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Device é o espelho cloud de Device (docs/ai-spec/05-data-model.md,
// "Device"). Platform aceita qualquer plataforma cliente válida —
// "windows" hoje, "android" no futuro (AC-042, REQ-042): nenhum campo
// aqui nem nenhuma regra de negócio deste pacote assume Win32.
type Device struct {
	ID         string
	UserID     string
	InstallID  string
	Name       string
	Platform   string
	AppVersion *string
	LastSeenAt *time.Time
	CreatedAt  time.Time
	RevokedAt  *time.Time
	Version    int64
}

// ErrNotFound é devolvido pelos repositórios deste pacote quando a
// entidade pedida não existe ou pertence a outro usuário — os dois
// casos são indistinguíveis de propósito (AC-035, THR-004: um 404
// idêntico para "não existe" e para "existe, mas não é seu" evita
// vazar por enumeração se o registro de outro usuário existe).
var ErrNotFound = errors.New("server: registro não encontrado")

// DeviceRepository é o contrato de persistência de devices cloud.
type DeviceRepository interface {
	List(ctx context.Context, userID string) ([]Device, error)
	Get(ctx context.Context, userID, id string) (Device, error)
	Rename(ctx context.Context, userID, id, name string) (Device, error)
	Revoke(ctx context.Context, userID, id string, now time.Time) error
}

// InMemoryDeviceRepository é a implementação em memória usada nesta
// fase (o Postgres real chega no Sprint 6). Segura para uso concorrente.
type InMemoryDeviceRepository struct {
	mu   sync.Mutex
	byID map[string]Device
}

func NewInMemoryDeviceRepository() *InMemoryDeviceRepository {
	return &InMemoryDeviceRepository{byID: make(map[string]Device)}
}

var _ DeviceRepository = (*InMemoryDeviceRepository)(nil)

// Put insere ou substitui um device diretamente, sem passar pelas
// regras de Rename/Revoke. Existe só para seed de fixtures em testes
// (mesmo papel do Put de storage.InMemoryTranscriptionRepository) — em
// produção, devices só existem via o fluxo de device-auth (fora do
// escopo desta slice).
func (r *InMemoryDeviceRepository) Put(d Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[d.ID] = d
}

func (r *InMemoryDeviceRepository) List(_ context.Context, userID string) ([]Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Device, 0, len(r.byID))
	for _, d := range r.byID {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (r *InMemoryDeviceRepository) Get(_ context.Context, userID, id string) (Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	if !ok || d.UserID != userID {
		return Device{}, ErrNotFound
	}
	return d, nil
}

func (r *InMemoryDeviceRepository) Rename(_ context.Context, userID, id, name string) (Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	if !ok || d.UserID != userID {
		return Device{}, ErrNotFound
	}
	d.Name = name
	d.Version++
	r.byID[id] = d
	return d, nil
}

func (r *InMemoryDeviceRepository) Revoke(_ context.Context, userID, id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	if !ok || d.UserID != userID {
		return ErrNotFound
	}
	revokedAt := now.UTC()
	d.RevokedAt = &revokedAt
	d.Version++
	r.byID[id] = d
	return nil
}

// --- HTTP ---

type deviceDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	AppVersion *string    `json:"app_version,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	Version    int64      `json:"version"`
}

func toDeviceDTO(d Device) deviceDTO {
	return deviceDTO{
		ID: d.ID, Name: d.Name, Platform: d.Platform, AppVersion: d.AppVersion,
		LastSeenAt: d.LastSeenAt, CreatedAt: d.CreatedAt, RevokedAt: d.RevokedAt, Version: d.Version,
	}
}

// handleListDevices implementa GET /devices — sempre escopado a
// principal.UserID (AC-035): a query nunca aceita user_id.
func handleListDevices(repo DeviceRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		devices, err := repo.List(r.Context(), principal.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao listar devices")
			return
		}
		dtos := make([]deviceDTO, 0, len(devices))
		for _, d := range devices {
			dtos = append(dtos, toDeviceDTO(d))
		}
		writeJSON(w, http.StatusOK, map[string]any{"devices": dtos})
	}
}

type patchDeviceRequest struct {
	Name string `json:"name"`
}

// handlePatchDevice implementa PATCH /devices/{id}. O id vem da rota
// (r.PathValue), nunca do corpo — patchDeviceRequest não tem campo id
// nem user_id.
func handlePatchDevice(repo DeviceRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		id := r.PathValue("id")

		body, ok := decodeJSON[patchDeviceRequest](w, r)
		if !ok {
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			writeError(w, http.StatusBadRequest, codeValidation, "name é obrigatório")
			return
		}

		d, err := repo.Rename(r.Context(), principal.UserID, id, body.Name)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "device não encontrado")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao atualizar device")
			return
		}
		writeJSON(w, http.StatusOK, toDeviceDTO(d))
	}
}

// handleDeleteDevice implementa DELETE /devices/{id}: revoga o device
// (RevokedAt), conforme docs/ai-spec/05-data-model.md ("Device.revoked_at")
// e docs/ai-spec/09-security-privacy.md ("THR-003": "Credential Manager
// + revoke"). Não apaga o histórico associado.
func handleDeleteDevice(repo DeviceRepository, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		id := r.PathValue("id")

		err := repo.Revoke(r.Context(), principal.UserID, id, now())
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "device não encontrado")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha ao revogar device")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// newID reexporta storage.NewID para os arquivos deste pacote que
// precisam gerar IDs (seed de fixtures, resposta com request_id já usa
// storage.NewID diretamente em httpx.go).
func newID() string { return storage.NewID() }
