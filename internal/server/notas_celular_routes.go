package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As escritas das notas e dos marcadores pelo celular (ADR-022, N8; 06,
// "Notas e marcadores (nuvem)"):
//
//	GET    /api/v1/notes/{id}                  a nota (também a da lixeira)
//	POST   /api/v1/notes                       cria (201)
//	PATCH  /api/v1/notes/{id}                  edita, ou escolhe o selo (base_version obrigatório)
//	DELETE /api/v1/notes/{id}                  para a lixeira (204)
//	POST   /api/v1/notes/{id}/restore          tira da lixeira (200, a nota)
//	POST   /api/v1/notes/{id}/move             arrasta: {antes, depois} (200, a nota)
//	POST   /api/v1/notes/{id}/purge            apaga de vez, só da lixeira (204)
//	PUT    /api/v1/notes/{id}/labels/{label}   põe o marcador (200, a nota)
//	DELETE /api/v1/notes/{id}/labels/{label}   tira o marcador (200, a nota)
//	POST   /api/v1/labels                      cria o marcador (201)
//	PATCH  /api/v1/labels/{id}                 renomeia (200)
//	DELETE /api/v1/labels/{id}                 apaga e tira das notas (204)
//	POST   /api/v1/transcriptions/{id}/undo-note   o Desfazer do "Ditar para nota" (200)
//
// Só com a sessão do navegador, como o CRUD do histórico: o token de um PC
// recebe 403 (o PC escreve pelo push). Cada escrita passa pelas regras da N7 e
// grava o evento para os PCs (cloudstore.ChangeNote e as outras). O texto vai
// e vem no corpo JSON, nunca na URL. Nenhum handler loga texto, e nenhuma
// resposta de erro traz o que veio no pedido.

// codeNameInUse é o 409 de um nome de marcador que já existe, o mesmo código
// da API local do PC (o painel é o mesmo).
const codeNameInUse = "NAME_IN_USE"

// noteWriteDTO é a nota que volta do PATCH, com o que a gravação fez quando
// não foi só gravar o que veio: "mesclada" (a lista de marcar foi juntada com a
// de outro aparelho: o que ficou não é o que o celular mandou) ou "no_selo" (o
// texto mandado perdeu para o de outro aparelho e ficou no selo).
type noteWriteDTO struct {
	noteDTO
	Gravacao string `json:"gravacao,omitempty"`
}

func toNoteWriteDTO(g cloudstore.NotaGravada) noteWriteDTO {
	d := noteWriteDTO{noteDTO: toNoteDTO(g.Nota)}
	switch {
	case g.NoSelo:
		d.Gravacao = "no_selo"
	case g.Mesclada:
		d.Gravacao = "mesclada"
	}
	return d
}

// writeNoteError traduz o erro de uma escrita das notas, com frase fixa.
func writeNoteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloudstore.ErrNoteNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "nota não encontrada")
	case errors.Is(err, cloudstore.ErrLabelNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "marcador não encontrado")
	case errors.Is(err, cloudstore.ErrNoteInTrash):
		writeError(w, http.StatusConflict, codeConflict, "a nota está na lixeira: restaure antes de mexer nela")
	case errors.Is(err, cloudstore.ErrNoteNotInTrash):
		writeError(w, http.StatusConflict, codeConflict, "só se apaga de vez a nota que está na lixeira")
	case errors.Is(err, cloudstore.ErrNameInUse):
		writeError(w, http.StatusConflict, codeNameInUse, "já existe um marcador com esse nome")
	case errors.Is(err, cloudstore.ErrInvalidLabelName):
		writeError(w, http.StatusBadRequest, codeValidation, "nome de marcador inválido: de 1 a 50 caracteres, em uma linha")
	case errors.Is(err, cloudstore.ErrNeighbors):
		writeError(w, http.StatusConflict, codeConflict, "as notas vizinhas mudaram (ou não estão no mesmo grupo): a lista será lida de novo")
	case errors.Is(err, cloudstore.ErrNoNeighbor):
		writeError(w, http.StatusBadRequest, codeValidation, "mover pede a nota de antes ou a de depois")
	case errors.Is(err, cloudstore.ErrEmptyNote):
		writeError(w, http.StatusBadRequest, codeValidation, "a nota está vazia")
	case errors.Is(err, cloudstore.ErrNothingToChange):
		writeError(w, http.StatusBadRequest, codeValidation, "nada a mudar: mande titulo, corpo, cor, fixada ou arquivada")
	case errors.Is(err, cloudstore.ErrInvalidNote):
		writeError(w, http.StatusBadRequest, codeValidation, "nota inválida: o texto passa do tamanho, ou um campo não vale")
	case errors.Is(err, cloudstore.ErrNotLinked):
		writeError(w, http.StatusConflict, codeConflict, "o ditado já não está numa nota")
	case errors.Is(err, cloudstore.ErrNoConflict):
		writeError(w, http.StatusConflict, codeConflict, "a nota não tem versão em conflito")
	case errors.Is(err, cloudstore.ErrDeviceNotAllowed):
		writeError(w, http.StatusForbidden, codeForbidden, "esta rota exige a sessão do navegador")
	default:
		writeStoreError(w, err)
	}
}

// handleNoteGet é o GET /notes/{id}: a nota, também a da lixeira.
func handleNoteGet(store cloudstore.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		n, err := store.GetNote(r.Context(), principal.UserID, r.PathValue("id"))
		if err != nil {
			writeNoteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toNoteDTO(n))
	}
}

// campoCor distingue "cor" ausente (não muda) de null (tira a cor) e de um
// nome, que um *string sozinho não distingue.
type campoCor struct {
	Presente bool
	Valor    *string
}

func (c *campoCor) UnmarshalJSON(b []byte) error {
	c.Presente = true
	return json.Unmarshal(b, &c.Valor)
}

type noteCreateRequest struct {
	Titulo     string   `json:"titulo"`
	Corpo      string   `json:"corpo"`
	Cor        *string  `json:"cor"`
	Fixada     bool     `json:"fixada"`
	Arquivada  bool     `json:"arquivada"`
	Marcadores []string `json:"marcadores"`
}

// handleNoteCreate é o POST /notes: a nota nova, no topo do grupo dela (201).
// Um marcador que não existe é 400, como na API local.
func handleNoteCreate(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeEstrito[noteCreateRequest](w, r, maxJSONBodyBytes)
		if !ok {
			return
		}
		n, err := store.CreateNote(r.Context(), actorOf(principal), cloudstore.NovaNotaDoCelular{
			Titulo: body.Titulo, Corpo: body.Corpo, Cor: body.Cor, Fixada: body.Fixada, Arquivada: body.Arquivada,
			Marcadores: body.Marcadores,
		}, now())
		if errors.Is(err, cloudstore.ErrLabelNotFound) {
			writeError(w, http.StatusBadRequest, codeValidation, "marcador inexistente")
			return
		}
		if err != nil {
			writeNoteError(w, err)
			return
		}
		anotarContagens(r.Context(), "itens", 1)
		writeJSON(w, http.StatusCreated, toNoteDTO(n))
	}
}

// notePatchRequest é o corpo do PATCH /notes/{id}: base_version sempre, e ou
// a edição (só o que mudou; corpo_base é o corpo da versão base, para a mescla
// da lista de marcar), ou a escolha do selo (resolver_conflito), nunca os dois.
type notePatchRequest struct {
	BaseVersion      *int64   `json:"base_version"`
	Titulo           *string  `json:"titulo"`
	Corpo            *string  `json:"corpo"`
	CorpoBase        *string  `json:"corpo_base"`
	Cor              campoCor `json:"cor"`
	Fixada           *bool    `json:"fixada"`
	Arquivada        *bool    `json:"arquivada"`
	ResolverConflito *string  `json:"resolver_conflito"`
}

// handleNotePatch é o PATCH /notes/{id}. A base velha não é 409: as regras da
// N7 decidem (o campo que só o celular mudou vale; os dois mudando o texto, o
// do celular vai para o selo; a lista de marcar marcada dos dois lados se
// junta), e a resposta diz o que ficou (gravacao). 409 só para a base que o
// servidor nunca deu e para a escolha do selo sobre outra versão (ou sem selo).
func handleNotePatch(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeEstrito[notePatchRequest](w, r, maxJSONBodyBytes)
		if !ok {
			return
		}
		if body.BaseVersion == nil {
			writeError(w, http.StatusBadRequest, codeValidation, "base_version é obrigatório")
			return
		}
		p := cloudstore.PedidoDaNota{Op: cloudstore.NotaEditar, BaseVersion: *body.BaseVersion}
		if body.ResolverConflito != nil {
			if body.Titulo != nil || body.Corpo != nil || body.CorpoBase != nil || body.Cor.Presente || body.Fixada != nil || body.Arquivada != nil {
				writeError(w, http.StatusBadRequest, codeValidation, "resolver_conflito vai sozinho, com base_version")
				return
			}
			p.Op, p.Escolha = cloudstore.NotaEscolher, *body.ResolverConflito
		} else {
			if body.CorpoBase != nil && body.Titulo == nil && body.Corpo == nil {
				writeError(w, http.StatusBadRequest, codeValidation, "corpo_base só vai com o titulo ou o corpo")
				return
			}
			p.Titulo, p.Corpo, p.CorpoBase = body.Titulo, body.Corpo, body.CorpoBase
			p.MudarCor, p.Cor = body.Cor.Presente, body.Cor.Valor
			p.Fixada, p.Arquivada = body.Fixada, body.Arquivada
		}
		g, err := store.ChangeNote(r.Context(), actorOf(principal), r.PathValue("id"), p, now())
		if err != nil {
			writeNoteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toNoteWriteDTO(g))
	}
}

// handleNoteOp é uma escrita sem corpo: a lixeira (DELETE, 204), restaurar
// (200, a nota), apagar de vez (204), marcar e desmarcar (200, a nota).
func handleNoteOp(store cloudstore.Store, now func() time.Time, op cloudstore.OperacaoNaNota) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		p := cloudstore.PedidoDaNota{Op: op, Marcador: r.PathValue("label")}
		g, err := store.ChangeNote(r.Context(), actorOf(principal), r.PathValue("id"), p, now())
		if err != nil {
			writeNoteError(w, err)
			return
		}
		switch op {
		case cloudstore.NotaLixeira, cloudstore.NotaApagarDeVez:
			w.WriteHeader(http.StatusNoContent)
		default:
			writeJSON(w, http.StatusOK, toNoteDTO(g.Nota))
		}
	}
}

type noteMoveRequest struct {
	// Antes e Depois são as vizinhas DEPOIS de soltar; null é a ponta.
	Antes  *string `json:"antes"`
	Depois *string `json:"depois"`
}

// handleNoteMove é o POST /notes/{id}/move: o arrastar (N2b), como o da API
// local. 409 se as vizinhas não valem; 400 sem nenhuma.
func handleNoteMove(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeEstrito[noteMoveRequest](w, r, 4<<10)
		if !ok {
			return
		}
		p := cloudstore.PedidoDaNota{Op: cloudstore.NotaMover}
		if body.Antes != nil {
			p.Antes = *body.Antes
		}
		if body.Depois != nil {
			p.Depois = *body.Depois
		}
		g, err := store.ChangeNote(r.Context(), actorOf(principal), r.PathValue("id"), p, now())
		if err != nil {
			writeNoteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toNoteDTO(g.Nota))
	}
}

type labelRequest struct {
	BaseVersion *int64 `json:"base_version"`
	Nome        string `json:"nome"`
}

// handleLabelCreate é o POST /labels (201). O nome já usado, sem diferenciar
// maiúscula nem acento, é 409 NAME_IN_USE.
func handleLabelCreate(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeEstrito[labelRequest](w, r, 4<<10)
		if !ok {
			return
		}
		if body.BaseVersion != nil {
			writeError(w, http.StatusBadRequest, codeValidation, "criar o marcador não leva base_version")
			return
		}
		l, err := store.CreateLabel(r.Context(), actorOf(principal), body.Nome, now())
		if err != nil {
			writeNoteError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toLabelDTO(l))
	}
}

// handleLabelRename é o PATCH /labels/{id}: renomeia. O nome do celular vence,
// como o do PC (sem selo); o nome de outro marcador é 409 NAME_IN_USE.
func handleLabelRename(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeEstrito[labelRequest](w, r, 4<<10)
		if !ok {
			return
		}
		if body.BaseVersion == nil {
			writeError(w, http.StatusBadRequest, codeValidation, "base_version é obrigatório")
			return
		}
		l, err := store.RenameLabel(r.Context(), actorOf(principal), r.PathValue("id"), *body.BaseVersion, body.Nome, now())
		if err != nil {
			writeNoteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toLabelDTO(l))
	}
}

// handleLabelDelete é o DELETE /labels/{id}: tira o marcador das notas, não
// apaga nota, e deixa a lápide (204).
func handleLabelDelete(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		if err := store.DeleteLabel(r.Context(), actorOf(principal), r.PathValue("id"), now()); err != nil {
			writeNoteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type undoNoteRequest struct {
	// Versao: a versão da nota logo depois do ditado (a da resposta dele).
	Versao int64 `json:"versao"`
	// CorpoAntes: o corpo de antes do ditado (a resposta dele o trouxe).
	CorpoAntes *string `json:"corpo_antes"`
}

// undoNoteDTO é a resposta do Desfazer: o ditado de volta ao histórico e se a
// nota saiu (a nota nova intacta).
type undoNoteDTO struct {
	Ditado      itemDTO `json:"ditado"`
	NotaApagada bool    `json:"nota_apagada"`
}

// handleDictationUndoNote é o POST /transcriptions/{id}/undo-note: o Desfazer do
// aviso do "Ditar para nota". O ditado volta ao histórico (em todo aparelho);
// a nota volta ao que era se ninguém mexeu nela desde o ditado. 409 se o ditado
// já não está numa nota.
func handleDictationUndoNote(store cloudstore.Store, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, _ := principalFromContext(r.Context())
		body, ok := decodeEstrito[undoNoteRequest](w, r, maxJSONBodyBytes)
		if !ok {
			return
		}
		t, apagada, err := store.UndoParaNota(r.Context(), actorOf(principal), r.PathValue("id"),
			cloudstore.PedidoDeDesfazer{Versao: body.Versao, CorpoAntes: body.CorpoAntes}, now())
		if err != nil {
			writeNoteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, undoNoteDTO{Ditado: toItemDTO(t), NotaApagada: apagada})
	}
}
