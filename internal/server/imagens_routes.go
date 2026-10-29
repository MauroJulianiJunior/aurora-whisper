package server

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As imagens das notas na nuvem (ADR-022, "Decisões da N10b"; 06, "Imagens das
// notas (nuvem)"). Duas famílias de rotas, com as MESMAS regras
// (cloudstore.DecidirAnexo e DecidirTirar) e os mesmos códigos de erro da API
// local do PC:
//
// O sync do PC, só com o token de um aparelho (o PC sobe o que anexou e baixa o
// que veio de outro lugar; o pull leva só os metadados):
//
//	PUT    /api/v1/sync/notes/{id}/images/{imagem}   sobe a imagem (201 nova, 200 a mesma de novo)
//	GET    /api/v1/sync/notes/{id}/images/{imagem}   os bytes (image/jpeg)
//	DELETE /api/v1/sync/notes/{id}/images/{imagem}   tira da nota (204, idempotente)
//
// O celular, só com a sessão do navegador (o token de um PC recebe 403):
//
//	POST   /api/v1/notes/with-image                  a nota nova só com a imagem (201, a nota)
//	POST   /api/v1/notes/{id}/images                 anexa à nota (201, a imagem; o id é do servidor)
//	GET    /api/v1/notes/{id}/images/{imagem}        os bytes (image/jpeg, Cache-Control: no-store)
//	DELETE /api/v1/notes/{id}/images/{imagem}        tira da nota (204, idempotente)
//
// O corpo de quem sobe é o JPEG cru (Content-Type: image/jpeg), até
// imagemdanota.MaxBytes: nada de multipart, e nunca o nome do arquivo. O
// servidor passa os bytes por imagemdanota.Conferir (o mesmo do Agent) ANTES de
// abrir a transação, e guarda a cópia limpa (sem EXIF nem nenhum outro
// metadado). O PUT do PC leva o SHA-256 da cópia que ele guardou
// (Imagem-SHA256), conferido contra o da cópia limpa, e a hora do anexo no PC
// (Imagem-Criada-Em, opcional).
//
// O log de acesso tem o padrão da rota e o status, nunca os bytes, o tamanho de
// uma imagem (a leitura tira os bytes da resposta da linha: ocultarBytes), um
// nome de arquivo nem o texto da nota.

// Os códigos dos erros das imagens: os quatro da API local do PC (o painel é o
// mesmo) e os três do sync.
const (
	codeImageInvalid  = "IMAGE_INVALID"
	codeImageTooLarge = "IMAGE_TOO_LARGE"
	codeTooManyImages = "TOO_MANY_IMAGES"
	codeImagesFull    = "IMAGES_FULL"
	// codeImageGone: a imagem foi tirada da nota, ou a nota foi apagada de vez
	// (410). O PC tira o envio da fila; o pull leva a lápide.
	codeImageGone = "IMAGE_GONE"
	// codeImageConflict: o id já é de outra imagem (outros bytes, ou outra
	// nota) (409).
	codeImageConflict = "IMAGE_CONFLICT"
	// codeImageShaMismatch: o SHA-256 que o PC mandou não é o da cópia limpa
	// (400): o arquivo do PC não é o que o Conferir guardaria.
	codeImageShaMismatch = "IMAGE_SHA_MISMATCH"
)

// Os cabeçalhos do PUT do PC.
const (
	// cabecalhoImagemSHA256 é o SHA-256 da cópia que o PC guardou, em
	// hexadecimal (obrigatório no PUT).
	cabecalhoImagemSHA256 = "Imagem-SHA256"
	// cabecalhoImagemCriadaEm é a hora do anexo no PC, em RFC 3339 (opcional):
	// a ordem das imagens na nota é a mesma em todo lugar.
	cabecalhoImagemCriadaEm = "Imagem-Criada-Em"
)

const (
	// tetoDoCorpoDaImagem é o teto do corpo das rotas que sobem imagem: o
	// imagemdanota.MaxBytes, o mesmo que o Conferir recusa. Só estas rotas
	// têm esse teto; as de JSON continuam no maxJSONBodyBytes (1 MiB).
	tetoDoCorpoDaImagem = imagemdanota.MaxBytes
	// prazoDoCorpoDaImagem é quanto o corpo de uma imagem pode levar para
	// chegar (o ReadTimeout de 30 s do servidor é de todo o resto): 4 MB a
	// 1 Mbit/s levam uns 32 s.
	prazoDoCorpoDaImagem = 2 * time.Minute
	// prazoDaRespostaDaImagem é o prazo da escrita da resposta de um GET de
	// imagem, pelo mesmo motivo (o WriteTimeout do servidor é de 60 s).
	prazoDaRespostaDaImagem = 2 * time.Minute
	// VagasDeImagem é quantas imagens o servidor lê e confere ao mesmo tempo
	// (o corpo na memória e as duas decodificações do Conferir, até uns 30 MB
	// cada): o contêiner da API tem 256 MB. Quem chega depois espera a vez.
	VagasDeImagem = 2
)

// imageDTO é a imagem no JSON: os metadados, nunca os bytes (o item da lista
// imagens da nota, o payload note_image do pull e a resposta de quem anexa).
type imageDTO struct {
	ID      string `json:"id"`
	NoteID  string `json:"note_id"`
	Largura int    `json:"largura"`
	Altura  int    `json:"altura"`
	// Bytes é o tamanho da cópia guardada, em bytes.
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

func toImageDTO(im cloudstore.NoteImage) imageDTO {
	return imageDTO{ID: im.ID, NoteID: im.NoteID, Largura: im.Largura, Altura: im.Altura, Bytes: im.Bytes,
		SHA256: im.SHA256, CreatedAt: im.CreatedAt}
}

// toImagesDTO é a lista imagens da nota: nunca null.
func toImagesDTO(is []cloudstore.NoteImage) []imageDTO {
	out := make([]imageDTO, 0, len(is))
	for _, im := range is {
		out = append(out, toImageDTO(im))
	}
	return out
}

// writeImageError traduz o erro de uma rota das imagens, com frase fixa; o
// resto vai para writeNoteError.
func writeImageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, imagemdanota.ErrNaoEJPEG), errors.Is(err, imagemdanota.ErrInvalida):
		writeError(w, http.StatusBadRequest, codeImageInvalid, "não é um JPEG que se abra")
	case errors.Is(err, imagemdanota.ErrLadoGrande):
		writeError(w, http.StatusBadRequest, codeImageInvalid, "a imagem passa de 2.000 px no lado maior: reduza antes de mandar")
	case errors.Is(err, imagemdanota.ErrGrandeDemais):
		writeError(w, http.StatusRequestEntityTooLarge, codeImageTooLarge, "a imagem passou do tamanho máximo, mesmo reduzida")
	case errors.Is(err, cloudstore.ErrTooManyImages):
		writeError(w, http.StatusConflict, codeTooManyImages, "a nota já tem "+strconv.Itoa(imagemdanota.MaxPorNota)+" imagens")
	case errors.Is(err, cloudstore.ErrImagesFull):
		writeError(w, http.StatusConflict, codeImagesFull, imagemdanota.FraseDoTeto())
	case errors.Is(err, cloudstore.ErrImageGone):
		writeError(w, http.StatusGone, codeImageGone, "a imagem foi tirada da nota, ou a nota foi apagada de vez")
	case errors.Is(err, cloudstore.ErrImageConflict):
		writeError(w, http.StatusConflict, codeImageConflict, "esse id já é de outra imagem")
	case errors.Is(err, cloudstore.ErrImageNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "imagem não encontrada")
	default:
		writeNoteError(w, err)
	}
}

// vagaDaImagem espera a vez de ler e conferir uma imagem (VagasDeImagem) e
// devolve como largá-la; ok falso se o pedido acabou antes.
func (c *Cloud) vagaDaImagem(ctx context.Context) (largar func(), ok bool) {
	select {
	case c.vagasDeImagem <- struct{}{}:
		return func() { <-c.vagasDeImagem }, true
	case <-ctx.Done():
		return nil, false
	}
}

// lerImagemDoCorpo lê e confere o corpo de quem sobe uma imagem: o
// Content-Type image/jpeg (415), o corpo até tetoDoCorpoDaImagem (413), sob o
// prazo dele, e o imagemdanota.Conferir. Responde o erro e devolve false.
func (c *Cloud) lerImagemDoCorpo(w http.ResponseWriter, r *http.Request) (imagemdanota.Limpa, bool) {
	tipo, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || tipo != "image/jpeg" {
		writeError(w, http.StatusUnsupportedMediaType, codeImageInvalid, "mande a imagem como image/jpeg")
		return imagemdanota.Limpa{}, false
	}
	if r.ContentLength > tetoDoCorpoDaImagem {
		writeImageError(w, imagemdanota.ErrGrandeDemais)
		return imagemdanota.Limpa{}, false
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(prazoDoCorpoDaImagem))
	corpo := http.MaxBytesReader(w, r.Body, tetoDoCorpoDaImagem)
	var b []byte
	if r.ContentLength > 0 {
		b = make([]byte, r.ContentLength)
		_, err = io.ReadFull(corpo, b)
	} else {
		b, err = io.ReadAll(corpo)
	}
	var grande *http.MaxBytesError
	switch {
	case errors.As(err, &grande):
		writeImageError(w, imagemdanota.ErrGrandeDemais)
		return imagemdanota.Limpa{}, false
	case err != nil:
		writeError(w, http.StatusBadRequest, codeBadRequest, "não consegui ler a imagem")
		return imagemdanota.Limpa{}, false
	}
	limpa, err := imagemdanota.Conferir(b)
	if err != nil {
		writeImageError(w, err)
		return imagemdanota.Limpa{}, false
	}
	return limpa, true
}

// shaDoCabecalho lê o Imagem-SHA256: 64 dígitos hexadecimais, um cabeçalho só.
func shaDoCabecalho(h http.Header) (string, bool) {
	v := h.Values(cabecalhoImagemSHA256)
	if len(v) != 1 || len(v[0]) != 64 {
		return "", false
	}
	s := strings.ToLower(v[0])
	if _, err := hex.DecodeString(s); err != nil {
		return "", false
	}
	return s, true
}

// criadaEmDoCabecalho lê o Imagem-Criada-Em: ausente é zero (a hora de agora);
// um só, em RFC 3339.
func criadaEmDoCabecalho(h http.Header) (time.Time, bool) {
	v := h.Values(cabecalhoImagemCriadaEm)
	switch len(v) {
	case 0:
		return time.Time{}, true
	case 1:
		t, err := time.Parse(time.RFC3339Nano, v[0])
		return t, err == nil
	}
	return time.Time{}, false
}

// escreverImagem responde os bytes de uma imagem: image/jpeg, sem cache em
// lugar nenhum (a decisão do Mauro: o celular nunca guarda a imagem; o PC a
// grava ele mesmo, conferida).
func escreverImagem(w http.ResponseWriter, b []byte) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(prazoDaRespostaDaImagem))
	cab := w.Header()
	cab.Set("Content-Type", "image/jpeg")
	cab.Set("X-Content-Type-Options", "nosniff")
	cab.Set("Cache-Control", "no-store")
	cab.Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// handleSyncImagePut é o PUT /sync/notes/{id}/images/{imagem}: o PC sobe a
// imagem que anexou (o sync, N10b). Idempotente: o mesmo id com os mesmos bytes
// de novo é 200 com o que está gravado, e é respondido ANTES de ler o corpo
// quando o Imagem-SHA256 já bate com a imagem gravada (o reenvio não gasta a
// banda de novo). O id com lápide, ou a nota apagada de vez, é 410: um reenvio
// atrasado não ressuscita a imagem tirada.
func (c *Cloud) handleSyncImagePut(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	if !requireDevice(w, principal) {
		return
	}
	sha, ok := shaDoCabecalho(r.Header)
	if !ok {
		writeError(w, http.StatusBadRequest, codeValidation, "Imagem-SHA256 é obrigatório: o SHA-256 da imagem, em hexadecimal")
		return
	}
	criada, ok := criadaEmDoCabecalho(r.Header)
	if !ok {
		writeError(w, http.StatusBadRequest, codeValidation, "Imagem-Criada-Em tem de ser uma data RFC 3339")
		return
	}
	noteID, id := r.PathValue("id"), r.PathValue("imagem")
	if _, ok := cloudstore.CanonicalUUID(id); !ok {
		writeImageError(w, cloudstore.ErrImageNotFound)
		return
	}
	// A conferência barata, antes do corpo: o reenvio que já valeu, e a lápide.
	if atual, existe, err := c.cfg.Store.ImageInfo(r.Context(), principal.UserID, id); err != nil {
		writeImageError(w, err)
		return
	} else if existe {
		canonNota, _ := cloudstore.CanonicalUUID(noteID)
		switch {
		case !atual.Viva():
			writeImageError(w, cloudstore.ErrImageGone)
			return
		case atual.NoteID != canonNota || atual.SHA256 != sha:
			writeImageError(w, cloudstore.ErrImageConflict)
			return
		}
		writeJSON(w, http.StatusOK, toImageDTO(atual))
		return
	}
	largar, ok := c.vagaDaImagem(r.Context())
	if !ok {
		return
	}
	defer largar()
	limpa, ok := c.lerImagemDoCorpo(w, r)
	if !ok {
		return
	}
	if limpa.SHA256 != sha {
		writeError(w, http.StatusBadRequest, codeImageShaMismatch, "o SHA-256 não é o da imagem conferida")
		return
	}
	im, created, err := c.cfg.Store.AttachImage(r.Context(), actorOf(principal),
		cloudstore.ImagemNova{ID: id, NoteID: noteID, Limpa: limpa, CriadaEm: criada}, c.now())
	if err != nil {
		writeImageError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toImageDTO(im))
}

// handleImageGet é o GET dos bytes, pelo PC (o sync) e pelo celular: a imagem
// viva da nota (também a da lixeira). 404 sem ela.
func (c *Cloud) handleImageGet(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	_, b, err := c.cfg.Store.GetImage(r.Context(), principal.UserID, r.PathValue("id"), r.PathValue("imagem"))
	if err != nil {
		writeImageError(w, err)
		return
	}
	// Os bytes da resposta seriam o tamanho da imagem: fora do log de acesso.
	ocultarBytes(r.Context())
	escreverImagem(w, b)
}

// handleImageDelete é o "Tirar da nota", pelo PC (o sync) e pelo celular:
// idempotente (tirar de novo, ou da nota apagada de vez, é 204), vale na nota da
// lixeira, e deixa a lápide mesmo de um id que a nuvem nunca teve (o PUT que
// ainda esteja na rede chega depois e recebe 410). 404 sem a nota.
func (c *Cloud) handleImageDelete(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	if err := c.cfg.Store.RemoveImage(r.Context(), actorOf(principal), r.PathValue("id"), r.PathValue("imagem"), c.now()); err != nil {
		writeImageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleNoteImageAttach é o POST /notes/{id}/images: o celular anexa (só com
// rede). O id da imagem é do servidor (UUID v7); a resposta é a imagem (201). Na
// nota da lixeira, 409 CONFLICT, como no PC.
func (c *Cloud) handleNoteImageAttach(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	largar, ok := c.vagaDaImagem(r.Context())
	if !ok {
		return
	}
	defer largar()
	limpa, ok := c.lerImagemDoCorpo(w, r)
	if !ok {
		return
	}
	im, _, err := c.cfg.Store.AttachImage(r.Context(), actorOf(principal),
		cloudstore.ImagemNova{ID: cloudstore.NewUUIDv7(), NoteID: r.PathValue("id"), Limpa: limpa}, c.now())
	if err != nil {
		writeImageError(w, err)
		return
	}
	anotarContagens(r.Context(), "itens", 1)
	writeJSON(w, http.StatusCreated, toImageDTO(im))
}

// handleNoteWithImage é o POST /notes/with-image: o ícone de imagem do "Criar uma
// nota…" no celular. A nota nasce sem texto, no topo das ativas, com a imagem
// (201, a nota com imagens).
func (c *Cloud) handleNoteWithImage(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	largar, ok := c.vagaDaImagem(r.Context())
	if !ok {
		return
	}
	defer largar()
	limpa, ok := c.lerImagemDoCorpo(w, r)
	if !ok {
		return
	}
	n, err := c.cfg.Store.CreateNoteWithImage(r.Context(), actorOf(principal), limpa, c.now())
	if err != nil {
		writeImageError(w, err)
		return
	}
	anotarContagens(r.Context(), "itens", 1)
	writeJSON(w, http.StatusCreated, toNoteDTO(n))
}
