package localapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As imagens das notas na API local (ADR-022, "As imagens (N10)";
// docs/ai-spec/06-api-contract.md, "Imagens das notas"). Atrás da mesma cadeia de
// segurança de todas as rotas (guard.Protect: Host, Origin e o token): o painel
// lê a imagem por fetch com o token e a mostra por uma URL blob:, nunca por um
// <img src> que apontasse para cá (o <img> não leva o token).
//
// # Rotas
//
//	POST   /api/notas/com-imagem                  a nota nova só com a imagem (201, a nota)
//	POST   /api/notas/{id}/imagens                anexa à nota (201, a imagem)
//	GET    /api/notas/{id}/imagens/{imagem}       os bytes (image/jpeg)
//	DELETE /api/notas/{id}/imagens/{imagem}       tira da nota, de vez (204)
//
// O corpo do POST é o JPEG já reduzido pelo painel (Content-Type: image/jpeg),
// com o teto de imagemdanota.MaxBytes; nada de multipart, e nunca o nome do
// arquivo. O Agent confere e limpa (imagemdanota.Conferir) antes de guardar.
//
// Nada daqui vai para log: nem os bytes, nem o tamanho, nem o nome. Sem o
// repositório (Preferencias.Imagens nil) ou sem a pasta das imagens, 503.

// Os códigos dos erros das imagens, que o painel transforma nas frases da ADR
// (a cena 7 da prévia).
const (
	// codeImageInvalid: não é um JPEG que se abra (o tipo, a estrutura, as cores,
	// mais de 2.000 px).
	codeImageInvalid = "IMAGE_INVALID"
	// codeImageTooLarge: mais de imagemdanota.MaxBytes (413).
	codeImageTooLarge = "IMAGE_TOO_LARGE"
	// codeTooManyImages: a nota já tem imagemdanota.MaxPorNota imagens (409).
	codeTooManyImages = "TOO_MANY_IMAGES"
	// codeImagesFull: o total das imagens chegou ao teto (409); a mensagem é a
	// frase da tela, com o número (imagemdanota.FraseDoTeto).
	codeImagesFull = "IMAGES_FULL"
)

// MsgImagensNaoProntas é a frase do 503 das rotas das imagens.
const MsgImagensNaoProntas = "As imagens das notas não estão disponíveis nesta versão."

// imagemDTO é a imagem como a nota a leva no JSON: só os metadados. Os bytes vêm
// pela rota de leitura.
type imagemDTO struct {
	ID      string `json:"id"`
	Largura int    `json:"largura"`
	Altura  int    `json:"altura"`
	// Bytes é o tamanho do arquivo, em bytes.
	Bytes     int64     `json:"bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func toImagemDTO(im storage.ImagemDaNota) imagemDTO {
	return imagemDTO{ID: im.ID, Largura: im.Largura, Altura: im.Altura, Bytes: im.Bytes, CreatedAt: im.CreatedAt}
}

func toImagensDTO(is []storage.ImagemDaNota) []imagemDTO {
	out := make([]imagemDTO, len(is))
	for i, im := range is {
		out[i] = toImagemDTO(im)
	}
	return out
}

// imagensProntas devolve o repositório das imagens, ou responde 503 e devolve
// false.
func (s *Server) imagensProntas(w http.ResponseWriter) (storage.ImagemDaNotaRepository, bool) {
	if s.pref.Imagens == nil {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, MsgImagensNaoProntas)
		return nil, false
	}
	return s.pref.Imagens, true
}

// lerJPEG lê o corpo do POST: o Content-Type tem de ser image/jpeg (415), e o
// corpo, até imagemdanota.MaxBytes (413). Responde o erro e devolve false.
func lerJPEG(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	tipo, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || tipo != "image/jpeg" {
		writeError(w, http.StatusUnsupportedMediaType, codeImageInvalid, "mande a imagem como image/jpeg")
		return nil, false
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, imagemdanota.MaxBytes))
	var grande *http.MaxBytesError
	if errors.As(err, &grande) {
		writeError(w, http.StatusRequestEntityTooLarge, codeImageTooLarge, "a imagem passou do tamanho máximo, mesmo reduzida")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "não consegui ler a imagem")
		return nil, false
	}
	return b, true
}

// writeImagemError traduz os erros das imagens; o resto vai para writeNotaError.
func writeImagemError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, imagemdanota.ErrNaoEJPEG), errors.Is(err, imagemdanota.ErrInvalida):
		writeError(w, http.StatusBadRequest, codeImageInvalid, "não é um JPEG que se abra")
	case errors.Is(err, imagemdanota.ErrLadoGrande):
		writeError(w, http.StatusBadRequest, codeImageInvalid, "a imagem passa de 2.000 px no lado maior: reduza antes de mandar")
	case errors.Is(err, imagemdanota.ErrGrandeDemais):
		writeError(w, http.StatusRequestEntityTooLarge, codeImageTooLarge, "a imagem passou do tamanho máximo, mesmo reduzida")
	case errors.Is(err, storage.ErrImagensDemais):
		writeError(w, http.StatusConflict, codeTooManyImages,
			"a nota já tem "+strconv.Itoa(imagemdanota.MaxPorNota)+" imagens")
	case errors.Is(err, storage.ErrTetoDasImagens):
		writeError(w, http.StatusConflict, codeImagesFull, imagemdanota.FraseDoTeto())
	case errors.Is(err, storage.ErrImagensSemPasta):
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, MsgImagensNaoProntas)
	case errors.Is(err, storage.ErrImagemEstragada):
		writeError(w, http.StatusInternalServerError, codeInternal, "a imagem guardada não confere")
	default:
		writeNotaError(w, err)
	}
}

// handleCriarNotaComImagem atende POST /api/notas/com-imagem: o ícone de imagem
// do "Criar uma nota…". A nota nasce sem texto, com a imagem, no topo das outras.
func (s *Server) handleCriarNotaComImagem(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.imagensProntas(w)
	if !ok {
		return
	}
	b, ok := lerJPEG(w, r)
	if !ok {
		return
	}
	n, err := repo.CriarNotaComImagem(r.Context(), storage.NovaNota{UserID: s.userID}, b)
	if err != nil {
		writeImagemError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toNotaDTO(n))
}

// handleAnexarImagem atende POST /api/notas/{id}/imagens: a imagem entra no fim
// das da nota. 404 sem a nota; 409 CONFLICT na lixeira, TOO_MANY_IMAGES com 10,
// IMAGES_FULL no teto.
func (s *Server) handleAnexarImagem(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.imagensProntas(w)
	if !ok {
		return
	}
	b, ok := lerJPEG(w, r)
	if !ok {
		return
	}
	im, err := repo.AnexarImagem(r.Context(), s.userID, r.PathValue("id"), b)
	if err != nil {
		writeImagemError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toImagemDTO(im))
}

// handleLerImagem atende GET /api/notas/{id}/imagens/{imagem}: os bytes, sem
// cache (a imagem da nota é dado da pessoa; o painel a guarda na memória da aba).
func (s *Server) handleLerImagem(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.imagensProntas(w)
	if !ok {
		return
	}
	_, b, err := repo.LerImagem(r.Context(), s.userID, r.PathValue("id"), r.PathValue("imagem"))
	if err != nil {
		writeImagemError(w, err)
		return
	}
	cab := w.Header()
	cab.Set("Content-Type", "image/jpeg")
	cab.Set("X-Content-Type-Options", "nosniff")
	cab.Set("Cache-Control", "no-store")
	cab.Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// handleTirarImagem atende DELETE /api/notas/{id}/imagens/{imagem}: a imagem sai
// de vez (o Desfazer é do painel, que só pede depois de ele vencer). Vale também
// na nota da lixeira. 404 se já saiu.
func (s *Server) handleTirarImagem(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.imagensProntas(w)
	if !ok {
		return
	}
	if err := repo.TirarImagem(r.Context(), s.userID, r.PathValue("id"), r.PathValue("imagem")); err != nil {
		writeImagemError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
