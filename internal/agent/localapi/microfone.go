package localapi

import "net/http"

// As rotas /api/microfone* avisam o painel de que a privacidade do Windows
// bloqueia o microfone e abrem a tela em que a pessoa liga a chave. O app
// não liga a permissão sozinho: só percebe e leva até lá.
//
//	GET  /api/microfone                 {"bloqueado": bool, "motivo": "<motivo>"}
//	POST /api/microfone/configuracoes   abre ms-settings:privacy-microphone; 204
//
// # Só um motivo, de uma lista fixa
//
// A resposta nunca traz caminho, título nem texto: o motivo é uma palavra
// (empresa, aparelho, conta, programas, este_app; "" quando liberado) e
// "desconhecido" quando o Agent não conseguiu ler o registro — que não conta
// como bloqueio.
//
// # O POST age no sistema real
//
// Abrir a tela do Windows é uma janela na área de trabalho de quem usa. Por
// isso é POST (nunca GET: um GET sai de uma <img> ou de um link), passa pela
// mesma cadeia do guard das outras rotas (Host, Origin, token) e a abertura
// fica atrás do portão de internal/agent/sysinput na implementação.
//
// Sem a interface montada (Preferencias.Microfone nil), as duas rotas
// respondem 503.

// Microfone é o que as rotas usam do Agent. Em produção, quem monta o Agent
// liga permissao.Consultar e painel.AbrirConfiguracoesDoMicrofone.
type Microfone interface {
	// Estado diz se o Windows bloqueia o microfone e por quê. Lê o registro
	// a cada chamada: a pessoa liga a chave e volta ao painel, e o aviso tem
	// de sumir sozinho. bloqueado=false com motivo "desconhecido" é "não deu
	// para ler".
	Estado() (bloqueado bool, motivo string)
	// AbrirConfiguracoes abre a tela de privacidade do microfone.
	AbrirConfiguracoes() error
}

// MsgMicrofoneNaoPronto é a frase do 503: o Agent não montou a consulta.
const MsgMicrofoneNaoPronto = "O Agent não consegue consultar o microfone agora."

// MsgNaoAbriuConfiguracoes é a frase de quando o Windows não abriu a tela.
const MsgNaoAbriuConfiguracoes = "Não consegui abrir as configurações do Windows."

type microfoneDTO struct {
	Bloqueado bool   `json:"bloqueado"`
	Motivo    string `json:"motivo"`
}

func (s *Server) microfonePronto(w http.ResponseWriter) bool {
	if s.pref.Microfone != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, MsgMicrofoneNaoPronto)
	return false
}

// handleGetMicrofone atende GET /api/microfone.
func (s *Server) handleGetMicrofone(w http.ResponseWriter, _ *http.Request) {
	semCache(w)
	if !s.microfonePronto(w) {
		return
	}
	bloqueado, motivo := s.pref.Microfone.Estado()
	writeJSON(w, http.StatusOK, microfoneDTO{Bloqueado: bloqueado, Motivo: motivo})
}

// handleAbrirConfiguracoesDoMicrofone atende POST /api/microfone/configuracoes.
func (s *Server) handleAbrirConfiguracoesDoMicrofone(w http.ResponseWriter, _ *http.Request) {
	semCache(w)
	if !s.microfonePronto(w) {
		return
	}
	if err := s.pref.Microfone.AbrirConfiguracoes(); err != nil {
		// O erro vai para o log do Agent, nunca para a tela.
		s.log.Warn("microfone: não abriu as configurações do Windows", "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, MsgNaoAbriuConfiguracoes)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
