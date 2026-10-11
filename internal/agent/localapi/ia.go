package localapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// As rotas /api/ia/* cuidam da chave de API do provedor de IA.
//
// A chave ENTRA por PUT /api/ia/chave e não sai por rota nenhuma: GET
// devolve só se há chave e os últimos caracteres dela (chaves.Final), e o
// teste usa a chave guardada sem devolvê-la. Nenhuma mensagem de erro
// daqui carrega a chave, nem o corpo que o provedor devolveu (ver
// internal/ia), e o log recebe só o erro do Gerenciador de Credenciais.

type chaveDTO struct {
	Provedor    string `json:"provedor"`
	Configurada bool   `json:"configurada"`
	Final       string `json:"final"`
}

type putChaveDTO struct {
	Provedor string `json:"provedor"`
	Chave    string `json:"chave"`
}

type testarDTO struct {
	Provedor string `json:"provedor"`
}

// provedorOuErro valida o provedor e já responde 400 quando não serve.
func provedorOuErro(w http.ResponseWriter, p string) bool {
	if ia.Valido(p) {
		return true
	}
	writeError(w, http.StatusBadRequest, codeBadRequest, "Provedor desconhecido: use openai ou anthropic.")
	return false
}

// semCache: nada que diga respeito a segredo fica em cache do navegador.
func semCache(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

// handleGetChave atende GET /api/ia/chave?provedor=.
func (s *Server) handleGetChave(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	p := r.URL.Query().Get("provedor")
	if !provedorOuErro(w, p) {
		return
	}
	chave, err := s.pref.Chaves.Ler(p)
	switch {
	case errors.Is(err, chaves.ErrAusente):
		writeJSON(w, http.StatusOK, chaveDTO{Provedor: p})
	case err != nil:
		s.log.Warn("localapi: lendo a chave", "provedor", p, "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui ler o Gerenciador de Credenciais do Windows.")
	default:
		writeJSON(w, http.StatusOK, chaveDTO{Provedor: p, Configurada: true, Final: chaves.Final(chave)})
	}
}

// handlePutChave atende PUT /api/ia/chave. 204 sem corpo: a chave não
// volta nem na confirmação.
func (s *Server) handlePutChave(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	var body putChaveDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limiteDoCorpo)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	if !provedorOuErro(w, body.Provedor) {
		return
	}
	err := s.pref.Chaves.Gravar(body.Provedor, body.Chave)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, chaves.ErrChaveVazia):
		writeError(w, http.StatusBadRequest, codeBadRequest, "Cole a chave antes de salvar.")
	case errors.Is(err, chaves.ErrChaveLonga):
		writeError(w, http.StatusBadRequest, codeBadRequest, "Isso é longo demais para ser uma chave de API.")
	case errors.Is(err, chaves.ErrChaveInvalida):
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"A chave tem espaço, quebra de linha ou caractere estranho no meio. Copie de novo, só a chave.")
	default:
		s.log.Warn("localapi: guardando a chave", "provedor", body.Provedor, "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui guardar a chave no Gerenciador de Credenciais do Windows.")
	}
}

// handleDeleteChave atende DELETE /api/ia/chave?provedor=. Apagar a chave
// do provedor que o Polish usa não muda o modo gravado: sem chave, o
// Polish cai para o Smart (16-ai-behavior-spec.md, "Fallback").
func (s *Server) handleDeleteChave(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	p := r.URL.Query().Get("provedor")
	if !provedorOuErro(w, p) {
		return
	}
	if err := s.pref.Chaves.Apagar(p); err != nil {
		s.log.Warn("localapi: apagando a chave", "provedor", p, "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui apagar a chave do Gerenciador de Credenciais do Windows.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestarIA atende POST /api/ia/testar: pergunta ao provedor, com a
// chave guardada, quais modelos ela alcança. Falha do PROVEDOR é 200 com
// ok=false e a mensagem pronta para a tela — a chamada à API local deu
// certo; quem recusou foi o provedor. No ok=true, junto da lista vai o
// modelo_sugerido para o Polish naquele provedor ("" quando não há; ver
// polish.go): é sugestão, e a lista sai inteira, com ou sem ele.
func (s *Server) handleTestarIA(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	var body testarDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limiteDoCorpo)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	if !provedorOuErro(w, body.Provedor) {
		return
	}
	chave, err := s.pref.Chaves.Ler(body.Provedor)
	if errors.Is(err, chaves.ErrAusente) {
		falhaDoTeste(w, fmt.Sprintf("Nenhuma chave da %s guardada. Cole a chave e salve antes de testar.", ia.Nome(body.Provedor)))
		return
	}
	if err != nil {
		s.log.Warn("localapi: lendo a chave para testar", "provedor", body.Provedor, "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui ler o Gerenciador de Credenciais do Windows.")
		return
	}

	modelos, err := s.pref.Modelos.ListarModelos(r.Context(), body.Provedor, chave)
	var erroIA *ia.Erro
	switch {
	case errors.As(err, &erroIA):
		falhaDoTeste(w, erroIA.Mensagem)
	case err != nil:
		// Não é um erro traduzido: a mensagem de origem não vai para a
		// tela nem para o log, porque não se sabe o que ela carrega.
		s.log.Warn("localapi: testando a chave falhou sem tradução", "provedor", body.Provedor)
		falhaDoTeste(w, fmt.Sprintf("Não consegui testar a chave da %s.", ia.Nome(body.Provedor)))
	default:
		if modelos == nil {
			modelos = []string{}
		}
		writeJSON(w, http.StatusOK, struct {
			OK             bool     `json:"ok"`
			Modelos        []string `json:"modelos"`
			ModeloSugerido string   `json:"modelo_sugerido"`
		}{true, modelos, modeloSugerido(body.Provedor)})
	}
}

func falhaDoTeste(w http.ResponseWriter, mensagem string) {
	writeJSON(w, http.StatusOK, struct {
		OK   bool   `json:"ok"`
		Erro string `json:"erro"`
	}{false, mensagem})
}
