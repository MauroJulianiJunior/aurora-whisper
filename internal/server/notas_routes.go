package server

import (
	"net/http"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As notas e os marcadores de nota na nuvem (ADR-022, N7): o formato no fio
// (o payload do pull e o state do conflict) e as duas listagens do celular,
// GET /notes e GET /labels. Só com a sessão do navegador, como o CRUD do
// histórico (o token de um PC recebe 403: o PC lê pelo pull). Nada de criar
// nem de mudar por aqui: a nota é escrita pelo sync.
//
// Nenhum handler loga texto: o log de acesso tem o padrão da rota e as
// contagens (anotarContagens), nunca o título, o corpo nem o nome.

// noteConflictDTO é o selo da nota: o título e o corpo que perderam, o aparelho
// e quando.
type noteConflictDTO struct {
	Titulo     string    `json:"titulo"`
	Corpo      string    `json:"corpo"`
	DeviceID   *string   `json:"device_id"`
	ConflictAt time.Time `json:"conflict_at"`
}

// noteDTO é a nota no JSON (o payload do pull, o state do conflict e o item do
// GET /notes). cor, ditado_de_origem, deleted_at e versao_em_conflito vêm
// sempre (null quando não há), e marcadores é sempre um array, em ordem de id.
// imagens (N10b) é sempre um array, na ordem da nota (created_at, e o id no
// empate): só os metadados das imagens vivas (imageDTO), nunca os bytes. O PC
// de antes da N10b ignora a chave.
type noteDTO struct {
	ID               string           `json:"id"`
	Titulo           string           `json:"titulo"`
	Corpo            string           `json:"corpo"`
	Cor              *string          `json:"cor"`
	Fixada           bool             `json:"fixada"`
	Arquivada        bool             `json:"arquivada"`
	Posicao          string           `json:"posicao"`
	Marcadores       []string         `json:"marcadores"`
	DitadoDeOrigem   *string          `json:"ditado_de_origem"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	DeletedAt        *time.Time       `json:"deleted_at"`
	Version          int64            `json:"version"`
	VersaoEmConflito *noteConflictDTO `json:"versao_em_conflito"`
	Imagens          []imageDTO       `json:"imagens"`
}

func toNoteDTO(n cloudstore.Note) noteDTO {
	marcadores := n.Marcadores
	if marcadores == nil {
		marcadores = []string{}
	}
	d := noteDTO{
		ID: n.ID, Titulo: n.Titulo, Corpo: n.Corpo, Cor: n.Cor, Fixada: n.Fixada, Arquivada: n.Arquivada,
		Posicao: n.Posicao, Marcadores: marcadores, DitadoDeOrigem: n.DitadoDeOrigem,
		CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, DeletedAt: n.DeletedAt, Version: n.Version,
		Imagens: toImagesDTO(n.Imagens),
	}
	if c := n.Conflict; c != nil {
		d.VersaoEmConflito = &noteConflictDTO{Titulo: c.Titulo, Corpo: c.Corpo, DeviceID: c.DeviceID, ConflictAt: c.At}
	}
	return d
}

// labelDTO é o marcador de nota no JSON (o payload do pull e o item do GET
// /labels).
type labelDTO struct {
	ID        string    `json:"id"`
	Nome      string    `json:"nome"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version"`
}

func toLabelDTO(l cloudstore.Label) labelDTO {
	return labelDTO{ID: l.ID, Nome: l.Nome, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, Version: l.Version}
}

// handleNotesList é o GET /notes?vista=ativas|arquivo|lixeira: no máximo
// cloudstore.MaxNotesList notas (e o orçamento de texto da página), com
// has_more quando passou. Sem vista, as ativas; vista fora da lista é 400.
func handleNotesList(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		vista := cloudstore.VistaAtivas
		if q := r.URL.Query(); q.Has("vista") {
			vista = q.Get("vista")
		}
		if !cloudstore.VistaValida(vista) {
			writeError(w, http.StatusBadRequest, codeValidation, `vista tem de ser "ativas", "arquivo" ou "lixeira"`)
			return
		}
		page, err := store.ListNotes(r.Context(), principal.UserID, vista, 0)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		anotarContagens(r.Context(), "itens", len(page.Items))
		items := make([]noteDTO, 0, len(page.Items))
		for _, n := range page.Items {
			items = append(items, toNoteDTO(n))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": page.HasMore})
	}
}

// handleLabelsList é o GET /labels: os marcadores pelo nome, no máximo
// cloudstore.MaxNotesList, com has_more quando passou. Não lê a query.
func handleLabelsList(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		page, err := store.ListLabels(r.Context(), principal.UserID, 0)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		anotarContagens(r.Context(), "itens", len(page.Items))
		items := make([]labelDTO, 0, len(page.Items))
		for _, l := range page.Items {
			items = append(items, toLabelDTO(l))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": page.HasMore})
	}
}
