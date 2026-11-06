package server

import (
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A cópia do dicionário de um PC na nuvem (ADR-023, "C. Três rotas da nuvem").
// Só com o token de um PC (AcessoAparelho; a sessão do navegador é 403), na
// família /api/v1/sync/:
//
//	GET    /api/v1/sync/dictionary   o estado da fonte (200)
//	PUT    /api/v1/sync/dictionary   sobe a cópia (200 o estado, 409 NAO_E_A_FONTE, 422 DICIONARIO_INVALIDO)
//	DELETE /api/v1/sync/dictionary   apaga a cópia, se este PC for a fonte (204, idempotente)
//
// O corpo do PUT é a cópia (Content-Type: application/json), até
// dictionary.MaxBytesDaCopia, e leva dois cabeçalhos: Dicionario-SHA256 (o
// SHA-256 dos bytes, que tem de bater) e Dicionario-Fonte (tomar ou manter,
// cloudstore.ModoDaCopia). O servidor confere a cópia como o PC confere o
// arquivo (dictionary.LerCopia: o JSON estrito, a forma e os limites) e monta o
// Smart dela como o ditado no celular montará (smartconta.SmartDaSemente, com o
// usuário trocado pelo dono da conta): a cópia que não monta é 422, e nada é
// gravado. Um corpo acima do teto também é 422 (grande_demais), e não 413: o PC
// tem um motivo fechado a mostrar, e o mesmo corpo é recusado do mesmo jeito pelo
// ValidarCopia do PC, antes de mandar.
//
// O log de acesso tem o padrão da rota e o status, e daqui sai o motivo da
// recusa (lista fechada), o tamanho em faixas e o número de termos. NUNCA um
// termo, uma variante, o nome do aparelho ou o corpo.

// Os códigos desta família (minúsculos, como o ADR-023 os fixa: o PC os lê).
const (
	// codeDicionarioInvalido: a cópia foi recusada (422), com o motivo fechado.
	codeDicionarioInvalido = "DICIONARIO_INVALIDO"
	// codeNaoEAFonte: o envio automático (manter) de um PC que não é a fonte
	// (409). O corpo leva o estado.
	codeNaoEAFonte = "NAO_E_A_FONTE"
)

// Os cabeçalhos do PUT.
const (
	cabecalhoDicionarioSHA256 = "Dicionario-SHA256"
	cabecalhoDicionarioFonte  = "Dicionario-Fonte"
)

// estadoDaCopiaDTO é o estado da cópia: o corpo do GET, do 200 do PUT e (junto do
// bloco error) do 409. fonte é "este", "outro" ou "nenhuma"; sem cópia, os outros
// campos faltam.
type estadoDaCopiaDTO struct {
	Fonte        string     `json:"fonte"`
	Aparelho     string     `json:"aparelho,omitempty"`
	Termos       *int       `json:"termos,omitempty"`
	SHA256       string     `json:"sha256,omitempty"`
	AtualizadoEm *time.Time `json:"atualizado_em,omitempty"`
}

// toEstadoDaCopia é o estado da cópia c visto do PC deviceID (nil: sem cópia).
func toEstadoDaCopia(c *cloudstore.CopiaDoDicionario, deviceID string) estadoDaCopiaDTO {
	if c == nil {
		return estadoDaCopiaDTO{Fonte: cloudstore.FonteNenhuma}
	}
	termos, quando := c.Termos, c.AtualizadaEm.UTC()
	return estadoDaCopiaDTO{Fonte: c.FontePara(deviceID), Aparelho: c.DeviceNome, Termos: &termos,
		SHA256: c.SHA256, AtualizadoEm: &quando}
}

// erroDaCopiaInvalida é o 422: o bloco error de sempre, com o motivo fechado.
type erroDaCopiaInvalida struct {
	apiError
	Motivo dictionary.MotivoDaCopia `json:"motivo"`
}

// erroNaoEAFonte é o 409: o bloco error de sempre, e o estado da cópia ao lado
// dele (a mesma forma do corpo do GET).
type erroNaoEAFonte struct {
	Error apiError `json:"error"`
	estadoDaCopiaDTO
}

// frasesDaCopia são as frases fixas de cada motivo (nunca o texto da pessoa).
var frasesDaCopia = map[dictionary.MotivoDaCopia]string{
	dictionary.MotivoCopiaJSON:            "a cópia do dicionário não tem a forma esperada",
	dictionary.MotivoCopiaGrandeDemais:    "a cópia do dicionário passou do tamanho máximo",
	dictionary.MotivoCopiaTermosDemais:    "a cópia do dicionário tem termos demais",
	dictionary.MotivoCopiaVariantesDemais: "a cópia do dicionário tem variantes demais",
	dictionary.MotivoCopiaTermoInvalido:   "a cópia do dicionário tem um termo que não vale",
}

// recusarCopia responde o 422 DICIONARIO_INVALIDO com o motivo e o anota no log.
func recusarCopia(w http.ResponseWriter, r *http.Request, motivo dictionary.MotivoDaCopia) {
	anotarMotivo(r.Context(), string(motivo))
	frase, ok := frasesDaCopia[motivo]
	if !ok {
		motivo, frase = dictionary.MotivoCopiaJSON, frasesDaCopia[dictionary.MotivoCopiaJSON]
	}
	e := erroDaCopiaInvalida{Motivo: motivo}
	e.Code, e.Message, e.RequestID = codeDicionarioInvalido, frase, storage.NewID()
	writeJSON(w, http.StatusUnprocessableEntity, struct {
		Error erroDaCopiaInvalida `json:"error"`
	}{e})
}

// faixaDoTamanho é o tamanho da cópia em faixas, o único que o log vê dele.
func faixaDoTamanho(n int) string {
	switch {
	case n <= 8<<10:
		return "ate_8k"
	case n <= 64<<10:
		return "ate_64k"
	case n <= 256<<10:
		return "ate_256k"
	}
	return "ate_512k"
}

// shaDeUmCabecalho lê um cabeçalho de SHA-256: um só, com 64 dígitos
// hexadecimais (as maiúsculas são aceitas e viram minúsculas).
func shaDeUmCabecalho(h http.Header, nome string) (string, bool) {
	v := h.Values(nome)
	if len(v) != 1 || len(v[0]) != 64 {
		return "", false
	}
	s := strings.ToLower(v[0])
	if _, err := hex.DecodeString(s); err != nil {
		return "", false
	}
	return s, true
}

// handleDicionarioGet é o GET /sync/dictionary: o estado da fonte, do ponto de
// vista do PC que pergunta.
func (c *Cloud) handleDicionarioGet(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	if !requireDevice(w, principal) {
		return
	}
	copia, err := c.cfg.Store.DictionaryCopyInfo(r.Context(), principal.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEstadoDaCopia(copia, principal.DeviceID))
}

// handleDicionarioPut é o PUT /sync/dictionary: sobe a cópia (tomar ou manter).
func (c *Cloud) handleDicionarioPut(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	if !requireDevice(w, principal) {
		return
	}
	// Os cabeçalhos primeiro (baratos): o modo e o SHA-256.
	modo := cloudstore.ModoDaCopia(r.Header.Get(cabecalhoDicionarioFonte))
	if len(r.Header.Values(cabecalhoDicionarioFonte)) != 1 || !modo.Valido() {
		anotarMotivo(r.Context(), "fonte")
		writeError(w, http.StatusBadRequest, codeValidation, "Dicionario-Fonte tem de ser tomar ou manter")
		return
	}
	sha, ok := shaDeUmCabecalho(r.Header, cabecalhoDicionarioSHA256)
	if !ok {
		anotarMotivo(r.Context(), "cabecalho")
		writeError(w, http.StatusBadRequest, codeValidation, "Dicionario-SHA256 é obrigatório: o SHA-256 dos bytes da cópia, em hexadecimal")
		return
	}
	tipo, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || tipo != "application/json" {
		anotarMotivo(r.Context(), "tipo")
		writeError(w, http.StatusUnsupportedMediaType, codeBadRequest, "mande a cópia como application/json")
		return
	}
	// O corpo, até o teto: acima dele é 422 grande_demais (não 413), sem ler o resto.
	if r.ContentLength > dictionary.MaxBytesDaCopia {
		recusarCopia(w, r, dictionary.MotivoCopiaGrandeDemais)
		return
	}
	corpo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, dictionary.MaxBytesDaCopia))
	var grande *http.MaxBytesError
	switch {
	case errors.As(err, &grande):
		recusarCopia(w, r, dictionary.MotivoCopiaGrandeDemais)
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, codeBadRequest, "não consegui ler a cópia")
		return
	}
	if dictionary.SHA256DaCopia(corpo) != sha {
		anotarMotivo(r.Context(), "sha")
		writeError(w, http.StatusBadRequest, codeValidation, "o SHA-256 não é o dos bytes da cópia")
		return
	}
	anotarContagens(r.Context(), "tamanho", faixaDoTamanho(len(corpo)))

	// Os bytes são TEXT no banco: UTF-8 válido (o decodificador do JSON troca o
	// byte inválido por U+FFFD, e o banco o recusaria depois).
	if !utf8.Valid(corpo) {
		recusarCopia(w, r, dictionary.MotivoCopiaJSON)
		return
	}
	semente, err := dictionary.LerCopia(corpo)
	if err != nil {
		var ec *dictionary.ErrCopia
		if errors.As(err, &ec) {
			recusarCopia(w, r, ec.Motivo)
		} else {
			recusarCopia(w, r, dictionary.MotivoCopiaJSON)
		}
		return
	}
	// A cópia tem de montar o Smart como o ditado no celular a montará: o
	// usuário é o dono da conta, e o termo sem forma canônica é recusado.
	semente.Usuario = principal.UserID
	if _, err := smartconta.SmartDaSemente(semente); err != nil {
		recusarCopia(w, r, dictionary.MotivoCopiaTermoInvalido)
		return
	}
	anotarContagens(r.Context(), "termos", len(semente.Entradas))

	res, err := c.cfg.Store.PutDictionaryCopy(r.Context(), actorOf(principal),
		cloudstore.NovaCopia{Conteudo: corpo, SHA256: sha, Termos: len(semente.Entradas)}, modo, c.now())
	switch {
	case errors.Is(err, cloudstore.ErrUnknownDevice):
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "credencial inválida")
		return
	case err != nil:
		writeStoreError(w, err)
		return
	}
	estado := toEstadoDaCopia(res.Atual, principal.DeviceID)
	if res.NaoEraAFonte {
		anotarMotivo(r.Context(), "nao_e_a_fonte")
		writeJSON(w, http.StatusConflict, erroNaoEAFonte{
			Error:            apiError{Code: codeNaoEAFonte, Message: "este PC não é a fonte do dicionário do celular", RequestID: storage.NewID()},
			estadoDaCopiaDTO: estado,
		})
		return
	}
	writeJSON(w, http.StatusOK, estado)
}

// handleDicionarioDelete é o DELETE /sync/dictionary: apaga a cópia só se este PC
// é a fonte. Idempotente: de outro PC, ou sem cópia, é 204 sem apagar nada.
func (c *Cloud) handleDicionarioDelete(w http.ResponseWriter, r *http.Request) {
	principal, _ := principalFromContext(r.Context())
	if !requireDevice(w, principal) {
		return
	}
	if _, err := c.cfg.Store.DeleteDictionaryCopy(r.Context(), actorOf(principal)); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
