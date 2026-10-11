package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As rotas da etapa 5 sobre um cloudstore.Store (ADR-019, fatias F2 e F3): o
// histórico pelo CRUD e o sync por push e pull, com toda escrita passando
// pelo ponto único de gravação do Store. O mesmo código serve o Store em
// memória e o de Postgres (internal/server/pgstore). Quem monta é a tabela de
// rotas da nuvem (rotas.go), que diz quem pode chamar cada uma: o CRUD só com
// a sessão do navegador, o sync só com o token de um aparelho.
//
// Nenhum handler loga texto. Os erros respondem com texto fixo, sem o erro do
// Store dentro (nem o SQLSTATE).

func actorOf(p Principal) cloudstore.Actor {
	return cloudstore.Actor{UserID: p.UserID, DeviceID: p.DeviceID}
}

// conflictDTO é o selo: o texto que perdeu, o aparelho e quando. A escolha
// entrou na F4 (o PATCH com resolver_conflito); o PC passa a mostrá-lo na F7c.
type conflictDTO struct {
	FinalText  string    `json:"final_text"`
	DeviceID   *string   `json:"device_id"`
	ConflictAt time.Time `json:"conflict_at"`
}

// itemDTO é o ditado no JSON da etapa 5: no CRUD, no payload do pull e no
// state do conflict. Sem target_app. device_id é o aparelho onde o ditado
// nasceu; deleted_at e versao_em_conflito vêm sempre (null quando não há),
// para o PC saber que o item saiu da lixeira ou que o selo foi limpo.
type itemDTO struct {
	ID               string       `json:"id"`
	DeviceID         *string      `json:"device_id"`
	SourcePlatform   string       `json:"source_platform"`
	RawText          string       `json:"raw_text"`
	FinalText        string       `json:"final_text"`
	Mode             string       `json:"mode"`
	Language         *string      `json:"language,omitempty"`
	ASRProvider      string       `json:"asr_provider"`
	ASRModel         *string      `json:"asr_model,omitempty"`
	DurationMs       *int64       `json:"duration_ms,omitempty"`
	ProcessingMs     *int64       `json:"processing_ms,omitempty"`
	Confidence       *float32     `json:"confidence,omitempty"`
	Status           string       `json:"status"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	DeletedAt        *time.Time   `json:"deleted_at"`
	Version          int64        `json:"version"`
	VersaoEmConflito *conflictDTO `json:"versao_em_conflito"`
}

func toItemDTO(t cloudstore.Transcription) itemDTO {
	d := itemDTO{
		ID: t.ID, DeviceID: t.DeviceID, SourcePlatform: t.SourcePlatform,
		RawText: t.RawText, FinalText: t.FinalText, Mode: t.Mode, Language: t.Language,
		ASRProvider: t.ASRProvider, ASRModel: t.ASRModel, DurationMs: t.DurationMs,
		ProcessingMs: t.ProcessingMs, Confidence: t.Confidence, Status: t.Status,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, DeletedAt: t.DeletedAt, Version: t.Version,
	}
	if t.Conflict != nil {
		d.VersaoEmConflito = &conflictDTO{FinalText: t.Conflict.FinalText, DeviceID: t.Conflict.DeviceID, ConflictAt: t.Conflict.At}
	}
	return d
}

// learnedDTO é o aprendido no JSON da etapa 5 (ADR-019, "Decisões da F8"): no
// payload do pull e no state do conflict. domain e undone_at vêm sempre (null
// quando não há), para o PC saber que a restrição saiu ou que o par foi
// reaprendido. Nenhum nome coincide, com outro tipo, com um campo do itemDTO:
// um PC da F7 decodifica o aprendido no item do ditado sem erro e o conta
// como "não aplicado". Sem o normalized_variant, o user_id, as versões e as
// datas da linha: o PC não usa, e o id já é a chave.
type learnedDTO struct {
	ID         string     `json:"id"`
	Variant    string     `json:"variant"`
	Term       string     `json:"term"`
	Restricted bool       `json:"restricted"`
	Domain     *string    `json:"domain"`
	UndoneAt   *time.Time `json:"undone_at"`
}

func toLearnedDTO(l cloudstore.Learned) learnedDTO {
	return learnedDTO{
		ID: l.ID, Variant: l.Variant, Term: l.Term, Restricted: l.Restricted,
		Domain: l.Domain, UndoneAt: l.UndoneAt,
	}
}

// writeStoreError traduz um erro do Store, sempre com mensagem fixa.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloudstore.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "transcrição não encontrada")
	case errors.Is(err, cloudstore.ErrLearnedNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "troca aprendida não encontrada")
	case errors.Is(err, cloudstore.ErrVersionConflict):
		writeError(w, http.StatusConflict, codeConflict, "Entity version is stale")
	case errors.Is(err, cloudstore.ErrNotInTrash):
		writeError(w, http.StatusConflict, codeConflict, "o ditado não está na lixeira")
	case errors.Is(err, cloudstore.ErrNoConflict):
		writeError(w, http.StatusConflict, codeConflict, "o ditado não tem versão em conflito")
	case errors.Is(err, cloudstore.ErrInvalidChoice):
		writeError(w, http.StatusBadRequest, codeValidation, `resolver_conflito tem de ser "manter" ou "trocar"`)
	case errors.Is(err, cloudstore.ErrInvalidText):
		writeError(w, http.StatusBadRequest, codeValidation, "texto com caractere inválido")
	case errors.Is(err, cloudstore.ErrTextTooLong):
		writeError(w, http.StatusBadRequest, codeValidation, "texto grande demais")
	case errors.Is(err, cloudstore.ErrInvalidCursor):
		writeError(w, http.StatusBadRequest, codeBadRequest, "cursor inválido")
	case errors.Is(err, cloudstore.ErrInvalidFilter):
		writeError(w, http.StatusBadRequest, codeBadRequest, "filtro inválido")
	default:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
	}
}

// listFilterJSON são os filtros da busca (POST /transcriptions/search), os
// mesmos do GET mais o q, no corpo: o texto buscado nunca vai na URL, para não
// parar no log de acesso de um proxy (ADR-019, "Busca fora da URL").
type listFilterJSON struct {
	Q        string  `json:"q"`
	Cursor   string  `json:"cursor"`
	Limit    int     `json:"limit"`
	From     string  `json:"from"`
	To       string  `json:"to"`
	DeviceID *string `json:"device_id"`
	Mode     *string `json:"mode"`
	Trash    bool    `json:"trash"`
}

// maxCorpoBusca: o corpo da busca é pequeno (um trecho e os filtros).
const maxCorpoBusca = 16 << 10

// handleStoreList é o GET /transcriptions, sem q: um q na URL é 400 (a busca
// é o POST /transcriptions/search).
func handleStoreList(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("q") {
			writeError(w, http.StatusBadRequest, codeValidation, "a busca é por POST /api/v1/transcriptions/search, com o q no corpo")
			return
		}
		f := listFilterJSON{Cursor: q.Get("cursor"), From: q.Get("from"), To: q.Get("to"), Trash: q.Get("trash") == "true"}
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, codeValidation, "limit inválido")
				return
			}
			f.Limit = n
		}
		if v := q.Get("device_id"); v != "" {
			f.DeviceID = &v
		}
		if v := q.Get("mode"); v != "" {
			f.Mode = &v
		}
		listWith(w, r, store, f)
	}
}

// handleStoreSearch é o POST /transcriptions/search: o q e os filtros no
// corpo, e a mesma resposta do GET.
func handleStoreSearch(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, ok := decodeJSONLimit[listFilterJSON](w, r, maxCorpoBusca)
		if !ok {
			return
		}
		if f.Limit < 0 {
			writeError(w, http.StatusBadRequest, codeValidation, "limit inválido")
			return
		}
		listWith(w, r, store, f)
	}
}

func listWith(w http.ResponseWriter, r *http.Request, store cloudstore.Store, in listFilterJSON) {
	principal, _ := principalFromContext(r.Context())
	f := cloudstore.ListFilter{
		UserID: principal.UserID, Query: in.Q, Cursor: in.Cursor, Trash: in.Trash, Limit: in.Limit,
		DeviceID: in.DeviceID, Mode: in.Mode,
	}
	for _, p := range []struct {
		key, val string
		dst      **time.Time
	}{{"from", in.From, &f.From}, {"to", in.To, &f.To}} {
		if p.val != "" {
			t, err := time.Parse(time.RFC3339, p.val)
			if err != nil {
				writeError(w, http.StatusBadRequest, codeValidation, p.key+" inválido")
				return
			}
			*p.dst = &t
		}
	}
	page, err := store.List(r.Context(), f)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := make([]itemDTO, 0, len(page.Items))
	for _, t := range page.Items {
		items = append(items, toItemDTO(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": page.NextCursor})
}

func handleStoreGet(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		t, err := store.Get(r.Context(), principal.UserID, r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toItemDTO(t))
	}
}

// storeEditRequest é o corpo do PATCH: a base e, ou o texto novo, ou a escolha
// do selo (resolver_conflito: "manter" ou "trocar"; ADR-019, "Decisões da
// F4"). Os dois juntos são 400.
type storeEditRequest struct {
	BaseVersion      *int64  `json:"base_version"`
	FinalText        *string `json:"final_text"`
	ResolverConflito *string `json:"resolver_conflito"`
}

func handleStoreEdit(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeJSON[storeEditRequest](w, r)
		if !ok {
			return
		}
		var (
			t   cloudstore.Transcription
			err error
		)
		switch {
		case body.BaseVersion == nil || (body.FinalText == nil) == (body.ResolverConflito == nil):
			writeError(w, http.StatusBadRequest, codeValidation, "base_version e, ou final_text, ou resolver_conflito são obrigatórios")
			return
		case body.ResolverConflito != nil:
			if !cloudstore.EscolhaValida(*body.ResolverConflito) {
				writeError(w, http.StatusBadRequest, codeValidation, `resolver_conflito tem de ser "manter" ou "trocar"`)
				return
			}
			t, err = store.ResolveConflict(r.Context(), actorOf(principal), r.PathValue("id"), *body.BaseVersion, *body.ResolverConflito, now())
		default:
			t, err = store.Edit(r.Context(), actorOf(principal), r.PathValue("id"), *body.BaseVersion, *body.FinalText, now())
		}
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toItemDTO(t))
	}
}

func handleStoreTrash(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		if _, err := store.Trash(r.Context(), actorOf(principal), r.PathValue("id"), now()); err != nil {
			writeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleStoreRestore(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		t, err := store.Restore(r.Context(), actorOf(principal), r.PathValue("id"), now())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toItemDTO(t))
	}
}

// handleStorePurge é o "excluir definitivamente" (POST
// /transcriptions/{id}/purge): só para um ditado da lixeira (fora dela, 409),
// e deixa a lápide. 204.
func handleStorePurge(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		if err := store.Purge(r.Context(), actorOf(principal), r.PathValue("id"), now()); err != nil {
			writeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// requireDevice: push e pull só com o token de um aparelho (ADR-019). A
// sessão do navegador (o celular) não sincroniza: lê e edita pelo CRUD.
func requireDevice(w http.ResponseWriter, p Principal) bool {
	if p.DeviceID == "" {
		writeError(w, http.StatusForbidden, codeForbidden, "o sync exige o token de um aparelho")
		return false
	}
	return true
}

type storeMutationDTO struct {
	MutationID  string          `json:"mutation_id"`
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id"`
	Operation   string          `json:"operation"`
	BaseVersion int64           `json:"base_version"`
	Payload     json.RawMessage `json:"payload"`
}

// storePushRequest: o aparelho vem do token; um device_id no corpo é
// ignorado (não há campo para ele).
type storePushRequest struct {
	Mutations []storeMutationDTO `json:"mutations"`
}

// storeResultDTO é a resposta a uma mutação. State é por tipo: o itemDTO no
// conflict de um ditado, o learnedDTO no de um aprendido (F8); nil (omitido)
// fora do conflict.
type storeResultDTO struct {
	MutationID    string `json:"mutation_id"`
	Status        string `json:"status"`
	Original      string `json:"original,omitempty"`
	EntityVersion int64  `json:"entity_version,omitempty"`
	Mesclado      bool   `json:"mesclado,omitempty"`
	Code          string `json:"code,omitempty"`
	Motivo        string `json:"motivo,omitempty"`
	State         any    `json:"state,omitempty"`
}

// handlePush é o POST /sync/push, com os limites por aparelho (ADR-019,
// "Decisões da F3"): pedidos por minuto antes de ler o corpo, e mutações por
// minuto depois (o teto de 500 por lote vale antes).
func (c *Cloud) handlePush(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	if !requireDevice(w, p) || !c.permitir(w, r, c.limPush, p.DeviceID, 1) {
		return
	}
	pushWith(w, r, c.cfg.Store, c.now, func(n int) bool {
		return n == 0 || c.permitir(w, r, c.limMutacoes, p.DeviceID, n)
	})
}

func pushWith(w http.ResponseWriter, r *http.Request, store cloudstore.Store, now func() time.Time, limitar func(n int) bool) {
	principal, _ := principalFromContext(r.Context())
	body, ok := decodeJSON[storePushRequest](w, r)
	if !ok {
		return
	}
	if len(body.Mutations) > cloudstore.MaxPushMutations {
		writeError(w, http.StatusBadRequest, codeValidation, "lote grande demais: no máximo "+
			strconv.Itoa(cloudstore.MaxPushMutations)+" mutações por push")
		return
	}
	if !limitar(len(body.Mutations)) {
		return
	}
	muts := make([]cloudstore.Mutation, 0, len(body.Mutations))
	for _, m := range body.Mutations {
		muts = append(muts, cloudstore.Mutation{
			MutationID: m.MutationID, EntityType: m.EntityType, EntityID: m.EntityID,
			Operation: m.Operation, BaseVersion: m.BaseVersion, Payload: m.Payload,
		})
	}
	resp, err := store.Push(r.Context(), actorOf(principal), muts, now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	counts := map[cloudstore.Status]int{}
	results := make([]storeResultDTO, 0, len(resp.Results))
	for _, res := range resp.Results {
		counts[res.Status]++
		d := storeResultDTO{
			MutationID: res.MutationID, Status: string(res.Status), Original: string(res.Original),
			EntityVersion: res.EntityVersion, Mesclado: res.Merged, Code: res.Code, Motivo: res.Reason,
		}
		// Só um ponteiro não nulo vai para o any: um ponteiro nulo dentro de
		// uma interface não é omitido, e sairia "state": null.
		switch {
		case res.State != nil:
			st := toItemDTO(*res.State)
			d.State = &st
		case res.LearnedState != nil:
			st := toLearnedDTO(*res.LearnedState)
			d.State = &st
		}
		results = append(results, d)
	}
	anotarContagens(r.Context(), "mutacoes", len(muts),
		"applied", counts[cloudstore.StatusApplied], "duplicate", counts[cloudstore.StatusDuplicate],
		"conflict", counts[cloudstore.StatusConflict], "rejected", counts[cloudstore.StatusRejected],
		"gone", counts[cloudstore.StatusGone])
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "sync_epoch": resp.Epoch})
}

// storeChangeDTO é uma linha do pull. Payload é por tipo (entity_type): o
// itemDTO de um ditado, o learnedDTO de um aprendido (F8); nil (omitido) na
// lápide.
type storeChangeDTO struct {
	Seq           int64   `json:"seq"`
	EntityType    string  `json:"entity_type"`
	EntityID      string  `json:"entity_id"`
	Operation     string  `json:"operation"`
	EntityVersion int64   `json:"entity_version,omitempty"`
	DeviceID      *string `json:"device_id,omitempty"`
	Payload       any     `json:"payload,omitempty"`
}

func handleStorePull(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		if !requireDevice(w, principal) {
			return
		}
		q := r.URL.Query()
		var afterSeq int64
		if v := q.Get("after_seq"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, codeValidation, "after_seq inválido")
				return
			}
			afterSeq = n
		}
		limit := 0
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, codeValidation, "limit inválido")
				return
			}
			limit = n
		}
		resp, err := store.Pull(r.Context(), principal.UserID, afterSeq, limit)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		anotarContagens(r.Context(), "mudancas", len(resp.Changes))
		changes := make([]storeChangeDTO, 0, len(resp.Changes))
		for _, c := range resp.Changes {
			d := storeChangeDTO{
				Seq: c.Seq, EntityType: c.EntityType, EntityID: c.EntityID, Operation: c.Operation,
				EntityVersion: c.EntityVersion, DeviceID: c.DeviceID,
			}
			switch {
			case c.Item != nil:
				item := toItemDTO(*c.Item)
				d.Payload = &item
			case c.Learned != nil:
				pair := toLearnedDTO(*c.Learned)
				d.Payload = &pair
			}
			changes = append(changes, d)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"changes": changes, "last_seq": resp.LastSeq, "has_more": resp.HasMore, "sync_epoch": resp.Epoch,
		})
	}
}
