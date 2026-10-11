package server

import (
	"net/http"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As rotas do celular para o aprendido (ADR-019, "Decisões da F8", F8d; 06,
// "Learned"): a lista do que os PCs aprenderam e o desfazer. Só com a sessão
// (o token do PC recebe 403: o PC desfaz pelo sync). O celular não aprende:
// não há rota de criar nem de mudar.
//
// O texto (a variante, o termo, o domínio) nunca vai na URL: as duas rotas não
// têm parâmetro de caminho, o GET não lê a query, e o id do desfazer vai no
// corpo. O log de acesso só tem o padrão da rota e contagens (acesso_log.go).

// learnedItemDTO é o item do celular (06): o par com o nome em português da
// tela. dominio vem sempre ("" sem domínio; o domínio tem de 1 a 80 runas, e
// então "" não se confunde com um nome).
type learnedItemDTO struct {
	ID       string `json:"id"`
	Termo    string `json:"termo"`
	Variante string `json:"variante"`
	Restrito bool   `json:"restrito"`
	Dominio  string `json:"dominio"`
}

func toLearnedItemDTO(l cloudstore.Learned) learnedItemDTO {
	d := learnedItemDTO{ID: l.ID, Termo: l.Term, Variante: l.Variant, Restrito: l.Restricted}
	if l.Domain != nil {
		d.Dominio = *l.Domain
	}
	return d
}

// maxCorpoDesfazer: o corpo do desfazer é um id (36 caracteres).
const maxCorpoDesfazer = 1 << 10

// handleLearnedList é o GET /learned: só os não desfeitos, do mais recente
// para o mais antigo, até o teto fixo (cloudstore.MaxLearnedList), com
// has_more quando passa dele. Não lê a query.
func handleLearnedList(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		page, err := store.ListLearned(r.Context(), principal.UserID, 0)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		anotarContagens(r.Context(), "itens", len(page.Items))
		items := make([]learnedItemDTO, 0, len(page.Items))
		for _, l := range page.Items {
			items = append(items, toLearnedItemDTO(l))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": page.HasMore})
	}
}

// learnedUndoRequest é o corpo do desfazer. O dono vem da sessão, nunca do
// corpo (não há campo para ele).
type learnedUndoRequest struct {
	ID *string `json:"id"`
}

// handleLearnedUndo é o POST /learned/undo: pelo ponto único de gravação, com
// o evento (o desfazer chega aos PCs pelo pull como upsert com undone_at).
// 200 com o item; o já desfeito também dá 200, sem gravar nem gastar seq. O
// id que não é UUID, o que não existe e o de outra conta dão o mesmo 404, com
// a frase do aprendido. Sem o id no corpo é 400.
func handleLearnedUndo(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeJSONLimit[learnedUndoRequest](w, r, maxCorpoDesfazer)
		if !ok {
			return
		}
		if body.ID == nil {
			writeError(w, http.StatusBadRequest, codeValidation, "id é obrigatório")
			return
		}
		l, err := store.UndoLearned(r.Context(), actorOf(principal), *body.ID, now())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toLearnedItemDTO(l))
	}
}
