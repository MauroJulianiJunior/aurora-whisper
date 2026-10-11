package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
)

// As rotas /api/nuvem* são o "Conectar à nuvem" das Configurações (ADR-019,
// "Conectar o PC"; fatia F6). Quem faz o trabalho é o nuvem.Servico; aqui só
// se traduz para HTTP.
//
// Nenhuma resposta daqui traz o token do aparelho: o Estado não o tem. E
// nenhum Conferir roda sozinho: o painel o pede ao abrir as Configurações e
// ao voltar à aba (sem laço).
//
// Desde a F7c: o GET traz o resumo do histórico (a prévia do envio inicial,
// o progresso, o que não subiu), e há o "Enviar o histórico" (POST
// /api/nuvem/enviar: a cópia do banco e a fila) e o "Esquecer esta nuvem"
// (POST /api/nuvem/esquecer). O painel relê o GET a cada 2 s só enquanto o
// envio corre e a seção está à vista: é leitura local, sem rede.
//
// Desde a S da ADR-021: GET /api/nuvem/mudancas, o contador de mudanças do
// sync (um número, sem texto), que o painel lê a cada poucos segundos com a
// aba à vista para saber quando reler a lista. A leitura é também o aviso ao
// laço de que o painel está aberto: ele busca a nuvem a cada 10 s, e não a
// cada 60 s.

// Nuvem é o que as rotas usam do nuvem.Servico.
type Nuvem interface {
	Estado(ctx context.Context) (nuvem.Estado, error)
	Conectar(ctx context.Context, url string) (nuvem.Estado, error)
	Conferir(ctx context.Context) (nuvem.Estado, error)
	Desconectar(ctx context.Context) (nuvem.Estado, error)
	EnviarHistorico(ctx context.Context) (nuvem.Estado, error)
	Esquecer(ctx context.Context) (nuvem.Estado, error)
}

var _ Nuvem = (*nuvem.Servico)(nil)

// maxCorpoNuvem: o corpo do Conectar (a URL tem até 256 caracteres).
const maxCorpoNuvem = 1 << 10

type conectarDTO struct {
	URL string `json:"url"`
}

// nuvemLigada responde 503 quando o Agent não montou a nuvem.
func (s *Server) nuvemLigada(w http.ResponseWriter) bool {
	if s.pref.Nuvem != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, "A nuvem não está disponível neste Agent.")
	return false
}

// handleGetNuvem atende GET /api/nuvem: o estado, para a tela.
func (s *Server) handleGetNuvem(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.nuvemLigada(w) {
		return
	}
	e, err := s.pref.Nuvem.Estado(r.Context())
	s.responderNuvem(w, e, err)
}

// handleConectarNuvem atende POST /api/nuvem/conectar {url}. O navegador
// que não abriu não é erro: o estado (conectando, com o aviso) vai com 200,
// e a tela mostra o "Abrir de novo".
func (s *Server) handleConectarNuvem(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.nuvemLigada(w) {
		return
	}
	var body conectarDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoNuvem)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	e, err := s.pref.Nuvem.Conectar(r.Context(), body.URL)
	if errors.Is(err, nuvem.ErrNaoAbriu) {
		writeJSON(w, http.StatusOK, e)
		return
	}
	s.responderNuvem(w, e, err)
}

// handleConferirNuvem atende POST /api/nuvem/conferir.
func (s *Server) handleConferirNuvem(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.nuvemLigada(w) {
		return
	}
	e, err := s.pref.Nuvem.Conferir(r.Context())
	s.responderNuvem(w, e, err)
}

// handleDesconectarNuvem atende POST /api/nuvem/desconectar: com um pedido
// em curso, o Cancelar; conectado, o Desconectar.
func (s *Server) handleDesconectarNuvem(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.nuvemLigada(w) {
		return
	}
	e, err := s.pref.Nuvem.Desconectar(r.Context())
	s.responderNuvem(w, e, err)
}

// handleEnviarHistorico atende POST /api/nuvem/enviar: o "Enviar o
// histórico" (só conectado). A cópia que falha volta com a frase do motivo.
func (s *Server) handleEnviarHistorico(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.nuvemLigada(w) {
		return
	}
	e, err := s.pref.Nuvem.EnviarHistorico(r.Context())
	s.responderNuvem(w, e, err)
}

// handleEsquecerNuvem atende POST /api/nuvem/esquecer: o "Esquecer esta
// nuvem" (só desconectado; conectado ou conectando, 409).
func (s *Server) handleEsquecerNuvem(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.nuvemLigada(w) {
		return
	}
	e, err := s.pref.Nuvem.Esquecer(r.Context())
	s.responderNuvem(w, e, err)
}

// MudancasDoSync é o contador de mudanças do laço de sync (ADR-021, S).
type MudancasDoSync interface {
	// LerMudancas devolve quantas rodadas do sync gravaram no histórico algo
	// que veio da nuvem, nesta execução do Agent. A leitura é também o aviso de
	// que o painel está aberto: o laço busca a nuvem mais vezes enquanto ela
	// se repete (em produção, syncengine.Laco.LerMudancas).
	LerMudancas() uint64
}

// mudancasDTO é a resposta de GET /api/nuvem/mudancas: um número e mais nada.
// Nenhum texto, id, versão, URL ou token: o painel só compara com o que leu
// da última vez e, se mudou, lê a lista pelo caminho de sempre.
type mudancasDTO struct {
	Contador uint64 `json:"contador"`
}

// handleMudancasNuvem atende GET /api/nuvem/mudancas: o contador de mudanças
// do sync, que o painel do PC lê a cada poucos segundos enquanto está à vista.
// É leitura de memória: não vai à nuvem nem ao banco. Um Agent sem o laço (sem
// histórico) responde 503, e um de antes da S responde 404.
func (s *Server) handleMudancasNuvem(w http.ResponseWriter, _ *http.Request) {
	semCache(w)
	if s.pref.Mudancas == nil {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, "A nuvem não está disponível neste Agent.")
		return
	}
	writeJSON(w, http.StatusOK, mudancasDTO{Contador: s.pref.Mudancas.LerMudancas()})
}

// responderNuvem: o estado com 200, ou a frase do *nuvem.Erro com o status
// dele. Outro erro é defeito: uma frase sem detalhe, e o log só com a classe.
func (s *Server) responderNuvem(w http.ResponseWriter, e nuvem.Estado, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, e)
		return
	}
	var ne *nuvem.Erro
	if errors.As(err, &ne) {
		if ne.Status >= 500 {
			// O erro do Gerenciador ou do banco: só o erro de dentro, que
			// nunca leva o token, a URL ou o e-mail.
			s.log.Warn("localapi: nuvem", "status", ne.Status, "err", errors.Unwrap(ne))
		}
		writeError(w, ne.Status, codigoDoStatus(ne.Status), ne.Mensagem)
		return
	}
	s.log.Warn("localapi: nuvem: erro inesperado", "err", err)
	writeError(w, http.StatusInternalServerError, codeInternal, "Não deu para falar com a nuvem agora.")
}

func codigoDoStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return codeBadRequest
	case http.StatusConflict:
		return codeConflict
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return codeUnavailable
	}
	return codeInternal
}
