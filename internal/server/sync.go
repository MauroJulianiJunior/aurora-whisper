package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Este arquivo implementa a lógica de sync server-side descrita em
// docs/ai-spec/06-api-contract.md ("Sync") e docs/ai-spec/04-system-design.md
// ("Sync"), de propósito INDEPENDENTE dos repositórios tipados de
// transcriptions/dictionary/settings deste pacote: o motor de sync
// versiona entidades genéricas (entity_type + entity_id + payload
// opaco), exatamente como o Postgres real vai fazer com a tabela
// SyncEvent. Isso é uma escolha deliberada desta fase (ver a mensagem
// de tarefa: "não há Postgres neste ambiente... o que dá para verificar
// agora é a LÓGICA de sync, que é independente de storage").
//
// A meta dura do bloco (docs/ai-spec/02-prd.md, "Métricas iniciais"):
// "sync confirmado perdido: 0". As duas invariantes que blindam isso:
//
//  1. Idempotência de mutation_id: uma mutation cujo mutation_id já foi
//     aplicado NUNCA é reaplicada — ela devolve "duplicate" com o mesmo
//     resultado da primeira vez, então um retry de rede nunca cria um
//     segundo registro nem reexecuta um efeito.
//  2. "delete aceito vence edit antiga" (04-system-design.md, "Sync"):
//     um delete cujo base_version não é mais o corrente (porque uma
//     edição mais nova já rodou) AINDA é aceito, desde que não seja um
//     delete "do futuro" (base_version maior que o corrente). Uma vez
//     que o delete aplica, nenhuma edição antiga consegue revivê-la:
//     edits exigem base_version == corrente (igualdade estrita), então
//     uma edit anterior ao delete sempre bate em "conflict" contra o
//     tombstone. Isso vale nos dois sentidos de ordem de chegada — ver
//     sync_test.go.

// MutationStatus é o resultado de aplicar uma mutation em POST
// /sync/push, um por mutation, conforme o contrato.
type MutationStatus string

const (
	MutationApplied   MutationStatus = "applied"
	MutationDuplicate MutationStatus = "duplicate"
	MutationConflict  MutationStatus = "conflict"
	MutationRejected  MutationStatus = "rejected"
)

// Mutation é uma mutation recebida em POST /sync/push. UserID não
// existe aqui de propósito — o motor de sync recebe o userID como
// parâmetro separado de Push, vindo sempre do Principal da sessão.
type Mutation struct {
	MutationID  string
	EntityType  string
	EntityID    string
	Operation   string // "upsert" | "delete"
	BaseVersion int64
	Payload     json.RawMessage
}

// MutationResult é o resultado de UMA mutation dentro de um Push.
type MutationResult struct {
	MutationID    string
	Status        MutationStatus
	EntityVersion int64
	Reason        string
}

// SyncChange é uma linha do resultado de Pull — uma linha do "SyncEvent"
// (docs/ai-spec/05-data-model.md).
type SyncChange struct {
	Seq           int64
	EntityType    string
	EntityID      string
	Operation     string
	EntityVersion int64
	Payload       json.RawMessage
}

// PullResult é o corpo de GET /sync/pull.
type PullResult struct {
	Changes []SyncChange
	LastSeq int64
	HasMore bool
}

// entityKey identifica uma entidade versionada dentro de um usuário.
type entityKey struct {
	entityType string
	entityID   string
}

// entityRecord é o estado corrente (tombstone incluído) de uma entidade
// para um usuário.
type entityRecord struct {
	version int64
	deleted bool
	payload json.RawMessage
}

// appliedKey identifica uma mutation já aplicada, escopada por usuário
// (mutation_id é gerado pelo cliente; dois usuários distintos gerando o
// mesmo UUID por coincidência não deve fazer um vazar dedup para o
// outro).
type appliedKey struct {
	userID     string
	mutationID string
}

// DefaultPullLimit e MaxPullLimit — [ASSUMPTION]: o contrato mostra
// "limit=500" como exemplo de query string
// (docs/ai-spec/06-api-contract.md, "GET /sync/pull") mas não fixa um
// teto; usamos esse mesmo número como default e como teto, análogo ao
// teto de 100 já usado em transcriptions.
const (
	DefaultPullLimit = 500
	MaxPullLimit     = 500
)

// SyncEngine é o motor de sync server-side, em memória (o Postgres real
// chega no Sprint 6 — ver AppliedMutation/SyncEvent em
// docs/ai-spec/05-data-model.md, que é o que este tipo pré-valida).
// Seguro para uso concorrente.
type SyncEngine struct {
	mu sync.Mutex

	entities map[string]map[entityKey]entityRecord // userID -> entity -> record
	applied  map[appliedKey]MutationResult

	// seqByUser é o próximo seq a atribuir para cada usuário. seq é
	// monotônico POR USUÁRIO nesta implementação — [ASSUMPTION]: o
	// índice do data model é (user_id, seq), e o cursor de Pull só
	// precisa ser monotônico dentro do usuário da sessão (GET
	// /sync/pull não tem parâmetro de usuário — vem sempre da sessão),
	// então um contador por usuário evita que o cursor de um usuário
	// "pule" por causa de eventos de outro.
	seqByUser map[string]int64
	events    map[string][]SyncChange // userID -> eventos em ordem de seq
}

// NewSyncEngine constrói um SyncEngine vazio.
func NewSyncEngine() *SyncEngine {
	return &SyncEngine{
		entities:  make(map[string]map[entityKey]entityRecord),
		applied:   make(map[appliedKey]MutationResult),
		seqByUser: make(map[string]int64),
		events:    make(map[string][]SyncChange),
	}
}

// Push aplica mutations, na ordem recebida, para userID. Devolve um
// MutationResult por mutation, na mesma ordem.
func (e *SyncEngine) Push(_ context.Context, userID string, mutations []Mutation) []MutationResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	results := make([]MutationResult, 0, len(mutations))
	for _, m := range mutations {
		results = append(results, e.applyLocked(userID, m))
	}
	return results
}

func (e *SyncEngine) applyLocked(userID string, m Mutation) MutationResult {
	if strings.TrimSpace(m.MutationID) == "" || strings.TrimSpace(m.EntityType) == "" ||
		strings.TrimSpace(m.EntityID) == "" {
		return MutationResult{MutationID: m.MutationID, Status: MutationRejected,
			Reason: "mutation_id, entity_type e entity_id são obrigatórios"}
	}
	if m.Operation != "upsert" && m.Operation != "delete" {
		return MutationResult{MutationID: m.MutationID, Status: MutationRejected,
			Reason: `operation deve ser "upsert" ou "delete"`}
	}
	if m.BaseVersion < 0 {
		return MutationResult{MutationID: m.MutationID, Status: MutationRejected,
			Reason: "base_version não pode ser negativo"}
	}

	// Idempotência: mutation_id já aplicado com sucesso NUNCA reaplica —
	// devolve o mesmo resultado, sob o status "duplicate" (nunca cria
	// segundo registro; ver o comentário do arquivo).
	ak := appliedKey{userID: userID, mutationID: m.MutationID}
	if prev, ok := e.applied[ak]; ok {
		return MutationResult{MutationID: m.MutationID, Status: MutationDuplicate, EntityVersion: prev.EntityVersion}
	}

	ek := entityKey{entityType: m.EntityType, entityID: m.EntityID}
	if e.entities[userID] == nil {
		e.entities[userID] = make(map[entityKey]entityRecord)
	}
	current := e.entities[userID][ek] // zero value: version 0, não existe ainda.

	switch m.Operation {
	case "delete":
		// "delete aceito vence edit antiga": aceito enquanto não for um
		// delete do futuro (base_version <= corrente), mesmo que uma
		// edição mais nova já tenha rodado depois do base_version
		// informado.
		if m.BaseVersion > current.version {
			return MutationResult{MutationID: m.MutationID, Status: MutationConflict, EntityVersion: current.version}
		}
		next := entityRecord{version: current.version + 1, deleted: true, payload: current.payload}
		e.entities[userID][ek] = next
		result := MutationResult{MutationID: m.MutationID, Status: MutationApplied, EntityVersion: next.version}
		e.applied[ak] = result
		e.appendEventLocked(userID, m.EntityType, m.EntityID, "delete", next.version, nil)
		return result

	default: // "upsert"
		if m.BaseVersion != current.version {
			return MutationResult{MutationID: m.MutationID, Status: MutationConflict, EntityVersion: current.version}
		}
		next := entityRecord{version: current.version + 1, deleted: false, payload: m.Payload}
		e.entities[userID][ek] = next
		result := MutationResult{MutationID: m.MutationID, Status: MutationApplied, EntityVersion: next.version}
		e.applied[ak] = result
		e.appendEventLocked(userID, m.EntityType, m.EntityID, "upsert", next.version, m.Payload)
		return result
	}
}

// appendEventLocked grava um SyncEvent com seq monotônico para userID.
// Chamado só com e.mu já travado.
func (e *SyncEngine) appendEventLocked(userID, entityType, entityID, operation string, entityVersion int64, payload json.RawMessage) {
	e.seqByUser[userID]++
	seq := e.seqByUser[userID]
	e.events[userID] = append(e.events[userID], SyncChange{
		Seq: seq, EntityType: entityType, EntityID: entityID,
		Operation: operation, EntityVersion: entityVersion, Payload: payload,
	})
}

// Pull devolve os SyncEvents de userID com seq > afterSeq, em ordem
// crescente de seq, até limit itens. O cursor nunca pula nem repete
// evento: dois Pulls com o mesmo afterSeq sempre devolvem exatamente a
// mesma página (eventos são apend-only, nunca reescritos).
func (e *SyncEngine) Pull(_ context.Context, userID string, afterSeq int64, limit int) PullResult {
	if limit <= 0 {
		limit = DefaultPullLimit
	}
	if limit > MaxPullLimit {
		limit = MaxPullLimit
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	all := e.events[userID]
	// all já está em ordem de inserção == ordem de seq (appendEventLocked
	// só faz append). Busca binária pelo primeiro seq > afterSeq.
	start := sort.Search(len(all), func(i int) bool { return all[i].Seq > afterSeq })

	remaining := all[start:]
	hasMore := len(remaining) > limit
	if hasMore {
		remaining = remaining[:limit]
	}

	changes := make([]SyncChange, len(remaining))
	copy(changes, remaining)

	lastSeq := afterSeq
	if len(changes) > 0 {
		lastSeq = changes[len(changes)-1].Seq
	}

	return PullResult{Changes: changes, LastSeq: lastSeq, HasMore: hasMore}
}

// --- HTTP ---

type mutationDTO struct {
	MutationID  string          `json:"mutation_id"`
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id"`
	Operation   string          `json:"operation"`
	BaseVersion int64           `json:"base_version"`
	Payload     json.RawMessage `json:"payload"`
}

// pushRequest espelha POST /sync/push. Sem UserID de propósito (ver
// httpx.go, decodeJSON) — device_id é só metadado de qual device
// enviou, nunca usado para autorização (quem autoriza é a sessão).
type pushRequest struct {
	DeviceID  string        `json:"device_id"`
	Mutations []mutationDTO `json:"mutations"`
}

type mutationResultDTO struct {
	MutationID    string `json:"mutation_id"`
	Status        string `json:"status"`
	EntityVersion int64  `json:"entity_version,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func handleSyncPush(engine *SyncEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeJSON[pushRequest](w, r)
		if !ok {
			return
		}

		mutations := make([]Mutation, 0, len(body.Mutations))
		for _, m := range body.Mutations {
			mutations = append(mutations, Mutation{
				MutationID: m.MutationID, EntityType: m.EntityType, EntityID: m.EntityID,
				Operation: m.Operation, BaseVersion: m.BaseVersion, Payload: m.Payload,
			})
		}

		results := engine.Push(r.Context(), principal.UserID, mutations)

		dtos := make([]mutationResultDTO, 0, len(results))
		for _, res := range results {
			dtos = append(dtos, mutationResultDTO{
				MutationID: res.MutationID, Status: string(res.Status),
				EntityVersion: res.EntityVersion, Reason: res.Reason,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": dtos})
	}
}

type syncChangeDTO struct {
	Seq           int64           `json:"seq"`
	EntityType    string          `json:"entity_type"`
	EntityID      string          `json:"entity_id"`
	Operation     string          `json:"operation"`
	EntityVersion int64           `json:"entity_version"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

func handleSyncPull(engine *SyncEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
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

		result := engine.Pull(r.Context(), principal.UserID, afterSeq, limit)
		dtos := make([]syncChangeDTO, 0, len(result.Changes))
		for _, c := range result.Changes {
			dtos = append(dtos, syncChangeDTO{
				Seq: c.Seq, EntityType: c.EntityType, EntityID: c.EntityID,
				Operation: c.Operation, EntityVersion: c.EntityVersion, Payload: c.Payload,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"changes": dtos, "last_seq": result.LastSeq, "has_more": result.HasMore,
		})
	}
}
