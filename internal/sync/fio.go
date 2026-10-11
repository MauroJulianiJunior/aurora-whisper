package syncengine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O contrato de fio com a nuvem (06-api-contract.md, "Sync", como a F2 o
// fechou; ADR-019, "Contrato de sync, por usuário"). Os nomes do JSON são os
// do servidor (internal/server/store_routes.go); a suíte de ponta a ponta
// (internal/server/integration) prova que os dois lados falam o mesmo.
//
// Na F8 (ADR-019, "Decisões da F8"), o fio passa a ser por tipo: o payload do
// pull e o state do conflict são o Item no ditado e o Aprendido no aprendido
// (learned), e o aprendido ganha a operação delete. O ditado continua
// exatamente como na F7 (abaixo, "A leitura pelo tipo").

// Os status de uma mutação no push.
const (
	StatusApplied   = "applied"
	StatusDuplicate = "duplicate"
	StatusConflict  = "conflict"
	StatusRejected  = "rejected"
	StatusGone      = "gone"
)

// Os motivos de gone.
const (
	MotivoApagado      = "apagado"
	MotivoForaDaJanela = "fora_da_janela"
)

// Os códigos de rejected que o PC trata de um jeito próprio (os outros
// marcam o ditado como recusado, com o código).
const (
	CodigoMutacaoReusada = "mutacao_reusada"
	CodigoBaseInvalida   = "base_invalida"
	// CodigoNadaADesfazer é a resposta ao delete de um aprendido que a nuvem
	// não tem (F8). Só o aprendido o recebe, e o PC o trata como sucesso: o
	// marcador sai, e o desfazer fica confirmado (a decisão é da F8f2).
	CodigoNadaADesfazer = "nada_a_desfazer"
)

// Operações.
const (
	OpUpsert    = "upsert"
	OpTombstone = "tombstone"
	// OpDelete é o desfazer do aprendido (F8), só no push. Vai sem payload
	// (Mutation.Payload nil, que sai como "payload": null), não olha a base e
	// só o aprendido a aceita (no ditado, a nuvem responde operacao_invalida:
	// a lixeira é o deleted_at). O pull nunca a traz: o par desfeito chega
	// como upsert, com undone_at, porque o aprendido não tem lápide.
	OpDelete = "delete"
)

// Mutation é uma mutação do push. Não tem user_id nem device_id: os dois vêm
// do token.
type Mutation struct {
	MutationID  string `json:"mutation_id"`
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	Operation   string `json:"operation"`
	BaseVersion int64  `json:"base_version"`
	// Payload é o corpo da mutação, já em JSON. nil no delete do aprendido
	// (sai como null, que a nuvem aceita como o payload vazio).
	Payload json.RawMessage `json:"payload"`
}

// MutationResult é a resposta a uma mutação.
type MutationResult struct {
	MutationID string `json:"mutation_id"`
	Status     string `json:"status"`
	// Original é o status da primeira aplicação; só no duplicate.
	Original string `json:"original,omitempty"`
	// EntityVersion: a versão nova (applied), a atual (conflict) ou a da
	// primeira vez (duplicate).
	EntityVersion int64 `json:"entity_version,omitempty"`
	// Mesclado: a base era velha e o servidor juntou a mudança com as de
	// outro aparelho. O PC NÃO guarda essa versão (ADR-019, "Eco e mescla").
	// O applied do aprendido nunca é mesclado.
	Mesclado bool   `json:"mesclado,omitempty"`
	Code     string `json:"code,omitempty"`
	Motivo   string `json:"motivo,omitempty"`
	// State é o estado atual do ditado, só no conflict (nunca no duplicate).
	State *Item `json:"state,omitempty"`
	// StateAprendido é o estado atual do aprendido, só no conflict de uma
	// mutação learned (nunca no duplicate). Só o Cliente o preenche: o state
	// não diz o tipo, que é o da mutação do mesmo lugar no lote.
	StateAprendido *Aprendido `json:"-"`
}

// PushResult é a resposta do push: um resultado por mutação, na ordem, e o
// epoch.
type PushResult struct {
	Results []MutationResult `json:"results"`
	Epoch   string           `json:"sync_epoch"`
}

// Item é o ditado como a nuvem o devolve (no pull e no state do conflict).
// deleted_at e versao_em_conflito vêm sempre, null quando não há.
type Item struct {
	ID               string     `json:"id"`
	DeviceID         *string    `json:"device_id"`
	SourcePlatform   string     `json:"source_platform"`
	RawText          string     `json:"raw_text"`
	FinalText        string     `json:"final_text"`
	Mode             string     `json:"mode"`
	Language         *string    `json:"language,omitempty"`
	ASRProvider      string     `json:"asr_provider"`
	ASRModel         *string    `json:"asr_model,omitempty"`
	DurationMs       *int64     `json:"duration_ms,omitempty"`
	ProcessingMs     *int64     `json:"processing_ms,omitempty"`
	Confidence       *float64   `json:"confidence,omitempty"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at"`
	Version          int64      `json:"version"`
	VersaoEmConflito *Selo      `json:"versao_em_conflito"`
}

// Selo é a versão que perdeu um conflito.
type Selo struct {
	FinalText  string    `json:"final_text"`
	DeviceID   *string   `json:"device_id"`
	ConflictAt time.Time `json:"conflict_at"`
}

// Aprendido é o par aprendido como a nuvem o devolve (ADR-019, "Decisões da
// F8"; o learnedDTO do servidor): no pull e no state do conflict. domain e
// undone_at vêm sempre, null quando não há. O par desfeito chega com
// undone_at (o aprendido não tem lápide). domain é o NOME do domínio. Nenhum
// nome coincide, com outro tipo, com um campo do Item: um PC da F7 o lê no
// Item sem erro, e o conta como "não aplicado".
type Aprendido struct {
	ID         string     `json:"id"`
	Variant    string     `json:"variant"`
	Term       string     `json:"term"`
	Restricted bool       `json:"restricted"`
	Domain     *string    `json:"domain"`
	UndoneAt   *time.Time `json:"undone_at"`
}

// Change é uma linha do pull: o estado atual de uma entidade que mudou depois
// do cursor (upsert) ou uma lápide do ditado (tombstone, sem payload).
type Change struct {
	Seq           int64  `json:"seq"`
	EntityType    string `json:"entity_type"`
	EntityID      string `json:"entity_id"`
	Operation     string `json:"operation"`
	EntityVersion int64  `json:"entity_version,omitempty"`
	// DeviceID é o aparelho da última mudança. Serve para mostrar de onde
	// veio; NUNCA para descartar o eco, que é reconhecido pela versão.
	DeviceID *string `json:"device_id,omitempty"`
	// Payload é o ditado (o upsert de um transcription; nil na lápide).
	Payload *Item `json:"payload,omitempty"`
	// PayloadAprendido é o par (o upsert de um learned). Só o Cliente o
	// preenche (a leitura pelo tipo).
	PayloadAprendido *Aprendido `json:"-"`
}

// ChangePage é uma página do pull.
type ChangePage struct {
	Changes []Change `json:"changes"`
	LastSeq int64    `json:"last_seq"`
	HasMore bool     `json:"has_more"`
	Epoch   string   `json:"sync_epoch"`
}

// SyncClient é o contrato de fio com a nuvem (18-tool-contract.md,
// "SyncClient", como a F7b o deixou). A implementação de produção é o
// Cliente (HTTPS com o token do aparelho). Os erros são os de cliente.go:
// ErrNaoAutorizado (401), ErrRecomeco (410), *ErrEspere (429), e os de
// ErrTransitorio e ErrContrato.
type SyncClient interface {
	// Push manda um lote. Com erro, NENHUM resultado vale: o lote inteiro
	// fica em voo, e o próximo envio o repete com os mesmos mutation_id.
	Push(ctx context.Context, muts []Mutation) (PushResult, error)
	// Pull lê as mudanças depois de afterSeq (exclusivo), em ordem de seq.
	Pull(ctx context.Context, afterSeq int64, limit int) (ChangePage, error)
}

// A leitura pelo tipo (F8f1).
//
// O payload do pull e o state do conflict mudam de forma com o entity_type:
// o aprendido (learned) é o Aprendido; o ditado, e qualquer tipo que este PC
// não conhece, é o Item, como na F7. O pull traz o entity_type em cada
// mudança; a resposta do push não: o state é do tipo da mutação do mesmo
// lugar no lote (a resposta vem na ordem, e o Cliente confere os ids).
//
// A leitura direta (json.Unmarshal num ChangePage ou num PushResult) continua
// a da F7: o payload e o state sempre no Item, e PayloadAprendido e
// StateAprendido nunca preenchidos. É de propósito: a suíte de contrato
// (internal/server/contracttest, testLearnedDecodesOnF7PC) a usa para provar
// que um PC da F7 lê a nuvem da F8 sem erro. O Cliente lê pelo tipo.
//
// Para o ditado, a leitura pelo tipo dá o mesmo que a leitura direta para
// qualquer JSON, e não só para o que o servidor manda (fio_test.go confere
// as duas lado a lado): as structs do fio embutem as do PC, e só a chave do
// payload (ou do state) fica crua, com cada ocorrência guardada na ordem; o
// tipo certo as decodifica depois, uma a uma, no mesmo valor, como o
// encoding/json faria no campo (a chave repetida por cima da anterior, e o
// null o zera).

// cru guarda, na ordem, cada ocorrência de uma chave do fio ainda sem
// decodificar.
type cru []json.RawMessage

// UnmarshalJSON guarda uma ocorrência (o null inclusive: o encoding/json o
// passa para quem tem UnmarshalJSON).
func (c *cru) UnmarshalJSON(b []byte) error {
	*c = append(*c, append(json.RawMessage(nil), b...))
	return nil
}

// lerPorTipo decodifica as ocorrências no tipo de entityType: no par, para o
// aprendido; no item, para todo o resto.
func (c cru) lerPorTipo(entityType string, item **Item, par **Aprendido) error {
	for _, o := range c {
		var err error
		if entityType == storage.EntidadeAprendido {
			err = json.Unmarshal(o, par)
		} else {
			err = json.Unmarshal(o, item)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// mudancaDoFio é a Change com o payload cru. O Payload daqui esconde o da
// Change embutida (no encoding/json, o campo mais raso vence). A Change não
// pode ganhar UnmarshalJSON: ele seria promovido e leria tudo sozinho.
type mudancaDoFio struct {
	Change
	Payload cru `json:"payload"`
}

// paginaDoFio é a ChangePage como chega.
type paginaDoFio struct {
	ChangePage
	Changes []mudancaDoFio `json:"changes"`
}

// pagina é a página com cada payload no tipo da mudança.
func (w paginaDoFio) pagina() (ChangePage, error) {
	p := w.ChangePage // o Changes dela é nil: o de fora o esconde
	if w.Changes != nil {
		p.Changes = make([]Change, len(w.Changes))
	}
	for i, m := range w.Changes {
		c := m.Change
		if err := m.Payload.lerPorTipo(c.EntityType, &c.Payload, &c.PayloadAprendido); err != nil {
			return ChangePage{}, err
		}
		p.Changes[i] = c
	}
	return p, nil
}

// resultadoDoFio é o MutationResult com o state cru.
type resultadoDoFio struct {
	MutationResult
	State cru `json:"state"`
}

// respostaDoPush é o PushResult como chega.
type respostaDoPush struct {
	PushResult
	Results []resultadoDoFio `json:"results"`
}

// resultado é a resposta com o state de cada resultado no tipo da mutação do
// mesmo lugar no lote. Um resultado a mais que o lote (fora do contrato: o
// Cliente o recusa logo depois) fica no Item, como na F7.
func (w respostaDoPush) resultado(muts []Mutation) (PushResult, error) {
	out := w.PushResult // o Results dele é nil: o de fora o esconde
	if w.Results != nil {
		out.Results = make([]MutationResult, len(w.Results))
	}
	for i, r := range w.Results {
		res := r.MutationResult
		tipo := ""
		if i < len(muts) {
			tipo = muts[i].EntityType
		}
		if err := r.State.lerPorTipo(tipo, &res.State, &res.StateAprendido); err != nil {
			return PushResult{}, err
		}
		out.Results[i] = res
	}
	return out, nil
}
