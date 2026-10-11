package server

import (
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// Deps reúne todas as dependências da API cloud. Campos podem ficar nil
// em New: quando fazem, New() os substitui pela implementação em
// memória desta fase (ver NewInMemory* em cada arquivo), exceto
// Sessions e ASRProvider, que não têm um default seguro (sem Sessions
// nenhuma rota autentica; sem ASRProvider o gateway de ASR não teria o
// que chamar) — esses dois precisam ser passados explicitamente.
type Deps struct {
	Sessions SessionStore

	// A etapa 5 (ADR-019) não passa por aqui: o servidor da nuvem, com o
	// Store da F2 e o login da F3, é o Cloud (nuvem.go). Este Server é o
	// servidor em memória da F1-D, com o contrato antigo de sync (genérico,
	// sem gone nem mescla), que só existe para os testes do cliente antigo
	// (internal/sync, internal/server/integration) até a F7a refazê-lo.

	Devices        DeviceRepository
	Transcriptions TranscriptionRepository
	Dictionary     DictionaryRepository
	Corrections    CorrectionRepository
	Settings       SettingsRepository
	Sync           *SyncEngine

	ASRProvider    asr.ASRProvider
	UpdateProvider UpdateProvider

	Clock clock.Clock

	// ReadyCheckers roda em GET /health/ready. Vazio nesta fase (nenhuma
	// dependência externa real ainda).
	ReadyCheckers []ReadyChecker
}

// Server é a API cloud HTTP (docs/ai-spec/06-api-contract.md), montada
// sobre Deps. Server.Handler devolve um http.Handler puro — quem serve
// (cmd/server) decide TLS/porta/graceful shutdown.
type Server struct {
	deps Deps
}

// New monta o Server, preenchendo com implementações em memória
// qualquer dependência opcional deixada como zero value em deps.
func New(deps Deps) *Server {
	if deps.Devices == nil {
		deps.Devices = NewInMemoryDeviceRepository()
	}
	if deps.Transcriptions == nil {
		deps.Transcriptions = NewInMemoryTranscriptionRepository()
	}
	if deps.Dictionary == nil {
		deps.Dictionary = NewInMemoryDictionaryRepository()
	}
	if deps.Corrections == nil {
		deps.Corrections = NewInMemoryCorrectionRepository()
	}
	if deps.Settings == nil {
		deps.Settings = NewInMemorySettingsRepository()
	}
	if deps.Sync == nil {
		deps.Sync = NewSyncEngine()
	}
	if deps.UpdateProvider == nil {
		deps.UpdateProvider = StaticUpdateProvider{}
	}
	if deps.Clock == nil {
		deps.Clock = clock.NewSystem()
	}
	return &Server{deps: deps}
}

// Handler monta o roteador da API cloud sob /api/v1, conforme
// docs/ai-spec/06-api-contract.md. Usa http.ServeMux nativo (padrões
// "MÉTODO /caminho/{id}", Go 1.22+) — zero dependência nova de
// roteamento (CLAUDE.md: "ZERO dependência externa nova").
//
// Toda rota, exceto health e updates (checadas sem sessão, ver seus
// handlers), passa por requireSession: é o único ponto de entrada de
// user_id no resto do código (AC-035, NFR-012, THR-004).
func (s *Server) Handler() http.Handler {
	d := s.deps
	now := d.Clock.Now
	auth := requireSession(d.Sessions)

	mux := http.NewServeMux()

	// Health — sem sessão.
	mux.HandleFunc("GET /api/v1/health/live", handleHealthLive)
	mux.HandleFunc("GET /api/v1/health/ready", handleHealthReady(d.ReadyCheckers...))

	// Updates — sem sessão.
	mux.HandleFunc("GET /api/v1/updates/windows/latest", handleUpdatesLatest(d.UpdateProvider))

	// Me.
	mux.Handle("GET /api/v1/me", auth(http.HandlerFunc(handleMe)))

	// Devices.
	mux.Handle("GET /api/v1/devices", auth(handleListDevices(d.Devices)))
	mux.Handle("PATCH /api/v1/devices/{id}", auth(handlePatchDevice(d.Devices)))
	mux.Handle("DELETE /api/v1/devices/{id}", auth(handleDeleteDevice(d.Devices, now)))

	// Transcriptions.
	mux.Handle("GET /api/v1/transcriptions", auth(handleListTranscriptions(d.Transcriptions)))
	mux.Handle("POST /api/v1/transcriptions", auth(handleCreateTranscription(d.Transcriptions, now)))
	mux.Handle("PATCH /api/v1/transcriptions/{id}", auth(handlePatchTranscription(d.Transcriptions)))
	mux.Handle("DELETE /api/v1/transcriptions/{id}", auth(handleDeleteTranscription(d.Transcriptions, now)))
	mux.Handle("POST /api/v1/transcriptions/{id}/restore", auth(handleRestoreTranscription(d.Transcriptions)))

	// Dictionary.
	mux.Handle("GET /api/v1/dictionary", auth(handleListDictionary(d.Dictionary)))
	mux.Handle("POST /api/v1/dictionary", auth(handleCreateDictionary(d.Dictionary, now)))
	mux.Handle("PATCH /api/v1/dictionary/{id}", auth(handlePatchDictionary(d.Dictionary)))
	mux.Handle("DELETE /api/v1/dictionary/{id}", auth(handleDeleteDictionary(d.Dictionary)))

	// Corrections.
	mux.Handle("POST /api/v1/corrections", auth(handleCreateCorrection(d.Corrections, now)))

	// Settings.
	mux.Handle("GET /api/v1/settings", auth(handleGetSettings(d.Settings)))
	mux.Handle("PUT /api/v1/settings", auth(handlePutSettings(d.Settings, now)))

	// Sync.
	mux.Handle("POST /api/v1/sync/push", auth(handleSyncPush(d.Sync)))
	mux.Handle("GET /api/v1/sync/pull", auth(handleSyncPull(d.Sync)))

	// ASR gateway.
	if d.ASRProvider != nil {
		mux.Handle("POST /api/v1/asr/transcribe", auth(handleASRTranscribe(d.ASRProvider)))
	}

	return mux
}
