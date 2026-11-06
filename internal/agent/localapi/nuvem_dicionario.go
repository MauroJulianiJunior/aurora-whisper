package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
)

// O dicionário para o celular (ADR-023, "F. O PC: a opção, o envio e o aviso"):
// a opção "Levar o dicionário para o celular" da seção Nuvem. Quem faz o
// trabalho é o nuvem.Servico; aqui só se traduz para HTTP. As duas rotas passam
// pela mesma guarda das outras /api/nuvem* (guard.Protect, no Agent).

// DicionarioDaNuvem é o que as rotas usam do nuvem.Servico para o dicionário.
type DicionarioDaNuvem interface {
	EstadoDoDicionario(ctx context.Context) (nuvem.EstadoDoDicionario, error)
	LigarDicionario(ctx context.Context) (nuvem.EstadoDoDicionario, error)
	DesligarDicionario(ctx context.Context) (nuvem.EstadoDoDicionario, error)
}

var _ DicionarioDaNuvem = (*nuvem.Servico)(nil)

// maxCorpoDoDicionario: o corpo do POST é {"ligar":true}.
const maxCorpoDoDicionario = 1 << 10

// ligarDicionarioDTO é o corpo de POST /api/nuvem/dicionario. Ligar é um
// ponteiro para o corpo sem o campo ser um 400, e não um "desligar" por engano.
type ligarDicionarioDTO struct {
	Ligar *bool `json:"ligar"`
}

// copiaLigada responde 503 quando o Agent não montou a nuvem ou o dicionário.
func (s *Server) copiaLigada(w http.ResponseWriter) bool {
	if s.pref.Nuvem != nil && s.pref.CopiaDoDicionario != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, "A nuvem não está disponível neste Agent.")
	return false
}

// handleGetDicionarioDaNuvem atende GET /api/nuvem/dicionario: o estado do
// "Levar o dicionário para o celular", que pergunta à nuvem (por isso o painel
// o lê quando a seção abre, e não a cada poucos segundos).
//
// A resposta é sempre 200, com este JSON (os campos vazios saem da resposta):
//
//	{
//	  "ligado":        true,                    a opção deste PC (a intenção)
//	  "fonte":         "este"|"outro"|"nenhuma", quem a nuvem diz ser a fonte da cópia;
//	                                           ausente quando a nuvem não pôde ser perguntada
//	                                           (sem rede, ou o PC desconectado)
//	  "aparelho":      "NOTEBOOK",              o nome do PC que é a fonte (fonte "outro")
//	  "termos":        49,                      quantos termos tem a cópia
//	  "atualizado_em": "2026-11-05T14:02:00Z",  quando a cópia da nuvem mudou (RFC 3339, UTC)
//	  "enviado_em":    "2026-11-05T14:02:00Z",  a última vez, nesta execução do Agent, que a
//	                                           nuvem aceitou a cópia deste PC
//	  "aviso":         "..."                    a última frase do envio automático (já em português)
//	}
//
// "ligado" false com "aviso" é o PC que perdeu a fonte (ou a cópia foi apagada, ou
// a nuvem recusou): a opção foi desligada, e o aviso diz por quê. "ligado" true
// com "aviso" é uma cópia que a nuvem recusou ou que passa dos limites: a opção
// fica, e o aviso diz o motivo.
func (s *Server) handleGetDicionarioDaNuvem(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.copiaLigada(w) {
		return
	}
	e, err := s.pref.CopiaDoDicionario.EstadoDoDicionario(r.Context())
	s.responderDicionario(w, e, err)
}

// handleLigarDicionarioDaNuvem atende POST /api/nuvem/dicionario {"ligar": true|false}.
//
// Ligar monta a cópia do dicionario.json e a manda na hora; só com o 200 da nuvem
// a opção fica ligada. Desligar apaga a cópia da nuvem na hora; só com o 204 a
// opção fica desligada. O sucesso é 200 com o MESMO JSON do GET (já com o estado
// novo). A falha é o JSON de erro de sempre ({"error":{"code","message"}}), com a
// frase em português para a tela em "message" e a opção como estava:
//
//	409 CONFLICT     o PC não está conectado, a cópia passa dos limites ou a nuvem a recusou
//	                 (o 422), ou a nuvem não reconhece mais o PC
//	502 UNAVAILABLE  a nuvem não respondeu, pediu para esperar, ainda não recebe o dicionário
//	                 (o 404), ou não apagou a cópia ("Não deu para apagar a cópia da nuvem
//	                 agora. Tente de novo.": a opção continua ligada)
//	503 UNAVAILABLE  o Agent não montou a nuvem
//	400 BAD_REQUEST  o corpo não é {"ligar": true|false}
//	500 INTERNAL     o banco ou o Gerenciador de Credenciais
func (s *Server) handleLigarDicionarioDaNuvem(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.copiaLigada(w) {
		return
	}
	var body ligarDicionarioDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoDoDicionario)).Decode(&body); err != nil || body.Ligar == nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	var (
		e   nuvem.EstadoDoDicionario
		err error
	)
	if *body.Ligar {
		e, err = s.pref.CopiaDoDicionario.LigarDicionario(r.Context())
	} else {
		e, err = s.pref.CopiaDoDicionario.DesligarDicionario(r.Context())
	}
	s.responderDicionario(w, e, err)
}

// responderDicionario: o estado com 200, ou a frase do *nuvem.Erro com o status
// dele. Outro erro é defeito: uma frase sem detalhe, e o log só com a classe.
func (s *Server) responderDicionario(w http.ResponseWriter, e nuvem.EstadoDoDicionario, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, e)
		return
	}
	var ne *nuvem.Erro
	if errors.As(err, &ne) {
		if ne.Status >= 500 && ne.Status != http.StatusBadGateway && ne.Status != http.StatusServiceUnavailable {
			// O erro do Gerenciador ou do banco: só o erro de dentro, que nunca
			// leva o token, a URL ou um termo.
			s.log.Warn("localapi: dicionário da nuvem", "status", ne.Status, "err", errors.Unwrap(ne))
		}
		writeError(w, ne.Status, codigoDoStatus(ne.Status), ne.Mensagem)
		return
	}
	s.log.Warn("localapi: dicionário da nuvem: erro inesperado", "err", err)
	writeError(w, http.StatusInternalServerError, codeInternal, "Não deu para falar com a nuvem agora.")
}
