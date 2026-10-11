package localapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// transcriptionDTO é a representação JSON de uma transcrição exposta
// pela Local API, tanto na listagem (AC-011) quanto no detalhe.
// Contém os campos que a UI precisa mostrar (AC-043, docs/ai-spec/08-ui-ux-spec.md
// "Histórico"/"Detalhe": texto, data, device, app, modo,
// provider/model, duração, processing time, status; raw_text no
// detalhe). Nunca inclui áudio nem contexto de janela — esses campos
// nem existem em storage.Transcription.
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
	// VersaoEmConflito é o selo (ADR-019, "Conflito"; no painel do PC desde
	// a F7c): a versão do texto que perdeu, guardada até a pessoa escolher.
	// Ausente sem selo, como no painel da nuvem.
	VersaoEmConflito *seloDTO `json:"versao_em_conflito,omitempty"`
}

// seloDTO é o selo como o painel o lê (o mesmo formato da nuvem).
type seloDTO struct {
	FinalText  string    `json:"final_text"`
	DeviceID   *string   `json:"device_id"`
	ConflictAt time.Time `json:"conflict_at"`
}

func toTranscriptionDTO(t storage.Transcription) transcriptionDTO {
	var selo *seloDTO
	if c := t.Conflito; c != nil {
		selo = &seloDTO{FinalText: c.FinalText, DeviceID: c.DeviceID, ConflictAt: c.Em}
	}
	return transcriptionDTO{
		VersaoEmConflito: selo,
		ID:               t.ID,
		DeviceID:         t.DeviceID,
		SourcePlatform:   t.SourcePlatform,
		RawText:          t.RawText,
		FinalText:        t.FinalText,
		Mode:             t.Mode,
		Language:         t.Language,
		ASRProvider:      t.ASRProvider,
		ASRModel:         t.ASRModel,
		DurationMs:       t.DurationMs,
		ProcessingMs:     t.ProcessingMs,
		Confidence:       t.Confidence,
		TargetApp:        t.TargetApp,
		Status:           t.Status,
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
		DeletedAt:        t.DeletedAt,
		Version:          t.Version,
	}
}

type historyPageDTO struct {
	Items      []transcriptionDTO `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// handleListHistory atende GET /api/history (AC-011, AC-013 "busca",
// AC-043). Query params: q, from, to (RFC3339), device_id, mode,
// provider, trash ("true" para lixeira), cursor, limit.
func (s *Server) handleListHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := storage.TranscriptionListFilter{
		UserID: s.userID,
		Query:  q.Get("q"),
		Cursor: q.Get("cursor"),
		Trash:  q.Get("trash") == "true",
	}

	if v := q.Get("device_id"); v != "" {
		filter.DeviceID = &v
	}
	if v := q.Get("mode"); v != "" {
		filter.Mode = &v
	}
	if v := q.Get("provider"); v != "" {
		filter.ASRProvider = &v
	}

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "parâmetro from inválido: use RFC3339")
			return
		}
		filter.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "parâmetro to inválido: use RFC3339")
			return
		}
		filter.To = &t
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "parâmetro limit inválido: use inteiro")
			return
		}
		filter.Limit = n
	}

	page, err := s.transcriptions.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}

	items := make([]transcriptionDTO, len(page.Items))
	for i, t := range page.Items {
		items[i] = toTranscriptionDTO(t)
	}
	writeJSON(w, http.StatusOK, historyPageDTO{Items: items, NextCursor: page.NextCursor})
}

// handleGetHistory atende GET /api/history/{id}.
func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	t, err := s.transcriptions.Get(r.Context(), s.userID, id)
	if err != nil {
		writeGetOrMutateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTranscriptionDTO(t))
}

type patchHistoryRequest struct {
	BaseVersion int64   `json:"base_version"`
	FinalText   *string `json:"final_text"`
	// ResolverConflito é a escolha do selo ("manter" ou "trocar"; ADR-019,
	// F7c), no lugar do final_text: o mesmo PATCH da nuvem.
	ResolverConflito *string `json:"resolver_conflito"`
}

// maxCorpoDaEdicao é o teto do corpo do PATCH: o texto de 64 KiB, mesmo
// todo escapado no JSON (\uXXXX, 6 bytes por byte), cabe com folga.
const maxCorpoDaEdicao = 512 << 10

// handlePatchHistory atende PATCH /api/history/{id} (AC-013 "editar"):
// concorrência otimista via base_version, 409 CONFLICT quando
// desatualizado.
//
// Com o aprendizado ligado (etapa 3, ADR-017), a resposta traz também
// "sugestoes": as trocas de termo que a edição sugere aprender, de 0 a 2.
// Para achá-las o motor precisa do texto de ANTES da edição, que o PATCH
// sobrescreve e ninguém guarda: por isso ele é lido ANTES de gravar. Salvar
// nunca grava no dicionário (AC-022), e o 409 não traz sugestão.
func (s *Server) handlePatchHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body patchHistoryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoDaEdicao)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	if body.ResolverConflito != nil {
		s.resolverSelo(w, r, id, body)
		return
	}
	texto := ""
	if body.FinalText != nil {
		texto = *body.FinalText
	}
	// O PC não aceita a edição que a nuvem recusaria (ADR-019, "Decisões da
	// F2"): o teto de 64 KiB do texto e o caractere nulo (campo_invalido no
	// push). Sem isso, a edição valeria aqui e ficaria "recusada" no sync.
	if len(texto) > storage.MaxTextoBytes {
		writeError(w, http.StatusBadRequest, codeBadRequest, "texto grande demais: até 64 KiB")
		return
	}
	// (UTF-8 inválido não chega aqui: o decodificador do JSON o troca por
	// U+FFFD.)
	if strings.ContainsRune(texto, 0) {
		writeError(w, http.StatusBadRequest, codeBadRequest, "texto com caractere inválido")
		return
	}

	antes, err := s.transcriptions.Get(r.Context(), s.userID, id)
	if err != nil {
		writeGetOrMutateError(w, err)
		return
	}
	t, err := s.transcriptions.UpdateFinalText(r.Context(), s.userID, id, body.BaseVersion, texto)
	if err != nil {
		writeGetOrMutateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, editadoDTO{
		transcriptionDTO: toTranscriptionDTO(t),
		Sugestoes:        s.sugerirDaEdicao(r.Context(), antes, t),
	})
}

// resolverSelo é o PATCH com resolver_conflito: a escolha do selo no PC
// (ADR-019, "Decisões da F7c"), o mesmo contrato da nuvem. O texto e a
// escolha juntos, ou uma escolha fora de "manter" e "trocar", são 400; sem
// selo, ou com a base velha, 409 (o painel relê). A escolha entra na fila do
// sync quando a nuvem tem o ditado (storage.ResolverConflito).
func (s *Server) resolverSelo(w http.ResponseWriter, r *http.Request, id string, body patchHistoryRequest) {
	if body.FinalText != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "o texto e a escolha do selo não vão juntos")
		return
	}
	t, err := s.transcriptions.ResolverConflito(r.Context(), s.userID, id, body.BaseVersion, *body.ResolverConflito)
	if errors.Is(err, storage.ErrEscolhaInvalida) {
		writeError(w, http.StatusBadRequest, codeBadRequest, "escolha do selo inválida: use manter ou trocar")
		return
	}
	if err != nil {
		writeGetOrMutateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTranscriptionDTO(t))
}

// handleDeleteHistory atende DELETE /api/history/{id} (AC-013
// "excluir"): soft delete, nunca apaga de verdade. Repetido, responde
// 204 de novo sem mudar nada — o repositório não reapaga o que já está na
// lixeira (revisão de 29-set-2026).
func (s *Server) handleDeleteHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := s.transcriptions.SoftDelete(r.Context(), s.userID, id, time.Now()); err != nil {
		writeGetOrMutateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRestoreHistory atende POST /api/history/{id}/restore (AC-013
// "restore").
func (s *Server) handleRestoreHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := s.transcriptions.Restore(r.Context(), s.userID, id); err != nil {
		writeGetOrMutateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeGetOrMutateError traduz os erros sentinela de storage para a
// resposta HTTP do contrato: ErrNotFound -> 404, ErrVersionConflict ->
// 409, qualquer outro -> 500 (sem vazar detalhe interno na mensagem).
func writeGetOrMutateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "registro não encontrado")
	case errors.Is(err, storage.ErrVersionConflict):
		writeError(w, http.StatusConflict, codeConflict, "versão desatualizada")
	default:
		writeError(w, http.StatusInternalServerError, codeInternal, "erro interno")
	}
}
