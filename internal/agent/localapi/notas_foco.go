package localapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Este arquivo é o aviso "o cursor está dentro de uma nota" (ADR-022, "Ditado
// feito dentro da nota"; 06-api-contract.md):
//
//	PUT    /api/notas/foco    {"nota": "<id>"} ou {"token": "<uuid>"}   204
//	DELETE /api/notas/foco    corpo opcional {"nota"|"token", "descartar"}  204
//
// O painel manda o PUT enquanto o título ou uma linha de uma nota tem o foco
// (e a janela do navegador também), renova a cada ~20 s e manda o DELETE quando
// o foco sai. O Agent guarda isto SÓ na memória, com validade de 60 s
// (internal/agent/notafoco), e o consulta ao gravar cada ditado: com aviso
// ativo, o ditado nasce escondido do histórico.
//
// # Contexto efêmero
//
// Saber em que nota a pessoa está é contexto: não vai a disco, não vai a log
// (nenhum slog neste arquivo recebe o id nem o token) e não entra no sync.
// Os ids que chegam só são validados e entregues ao marcador.
//
// Sem o marcador montado (Preferencias.Foco nil) ou sem o repositório das
// notas, 503: o painel trata como "sem aviso" e o ditado fica no histórico.

// focoRequest é o corpo do PUT e do DELETE. Exatamente um entre Nota e Token no
// PUT; no DELETE, nenhum (limpa o aviso, qualquer que seja) ou um (limpa só se
// o aviso ainda for aquele: um DELETE atrasado da nota antiga não apaga o aviso
// da nova).
type focoRequest struct {
	Nota  string `json:"nota"`
	Token string `json:"token"`
	// Descartar só vale com Token no DELETE: a caixa "Criar uma nota" fechou sem
	// criar a nota, e os ditados feitos nela voltam ao histórico.
	Descartar bool `json:"descartar"`
}

func (s *Server) focoPronto(w http.ResponseWriter) (storage.NotaRepository, bool) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return nil, false
	}
	if s.pref.Foco == nil {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, MsgNotasNaoProntas)
		return nil, false
	}
	return repo, true
}

// handlePutFoco atende PUT /api/notas/foco.
func (s *Server) handlePutFoco(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.focoPronto(w)
	if !ok {
		return
	}
	var body focoRequest
	if !lerCorpoDasNotas(w, r, &body) {
		return
	}
	if body.Descartar || (body.Nota == "") == (body.Token == "") {
		writeError(w, http.StatusBadRequest, codeBadRequest, "informe a nota ou o token, um dos dois")
		return
	}
	if body.Token != "" {
		if !s.pref.Foco.DefinirNova(body.Token) {
			writeError(w, http.StatusBadRequest, codeBadRequest, "token inválido")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// A nota tem de existir e estar fora da lixeira: o aviso de uma nota que não
	// existe escondia ditados de graça.
	n, err := repo.Get(r.Context(), s.userID, body.Nota)
	if err != nil {
		writeNotaError(w, err)
		return
	}
	if n.DeletedAt != nil {
		writeError(w, http.StatusConflict, codeConflict, "a nota está na lixeira: restaure antes de mexer nela")
		return
	}
	if !s.pref.Foco.DefinirNota(body.Nota) {
		writeError(w, http.StatusBadRequest, codeBadRequest, "id inválido")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteFoco atende DELETE /api/notas/foco. O corpo é opcional.
func (s *Server) handleDeleteFoco(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.focoPronto(w)
	if !ok {
		return
	}
	var body focoRequest
	if err := lerCorpoOpcional(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	if body.Nota != "" && body.Token != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "informe a nota ou o token, um dos dois")
		return
	}
	if body.Descartar && body.Token == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "descartar vale só com o token")
		return
	}
	switch {
	case body.Token != "":
		if body.Descartar {
			if _, err := repo.SoltarDitadosDaNotaNova(r.Context(), s.userID, body.Token); err != nil {
				writeNotaError(w, err)
				return
			}
		}
		s.pref.Foco.LimparNova(body.Token)
	case body.Nota != "":
		s.pref.Foco.LimparNota(body.Nota)
	default:
		s.pref.Foco.Limpar()
	}
	w.WriteHeader(http.StatusNoContent)
}

// lerCorpoOpcional lê o corpo JSON se houver um; sem corpo (DELETE puro), não
// muda v. Mesmo teto e mesmas regras de lerCorpoDasNotas.
func lerCorpoOpcional(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoDaEdicao))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
