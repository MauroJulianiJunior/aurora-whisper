package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/expurgo"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As rotas /api/retencao* são o bloco Histórico das Configurações (ADR-019,
// "Decisões da F10", F10d e F10e): quantos ditados têm mais de 6 meses, o OK
// que liga a regra, o Desligar e o botão "Apagar as cópias". Quem faz o
// trabalho é o ciclo do expurgo (internal/agent/expurgo), que é o dono do mutex
// que a rodada, o Ligar, o Desligar e o apagar das cópias compartilham; aqui só
// se traduz para HTTP.
//
// # Só números
//
// Nenhuma resposta traz texto de ditado, id nem caminho: as contagens, os
// booleanos e uma frase fixa por recusa. O erro que vem de dentro nunca vai
// para a tela (pode levar o nome de um arquivo); vai para o log do Agent, onde
// o ciclo já escreveu só contagens.
//
// # Sem OK, nada é apagado
//
// O GET só lê. O Ligar é o único jeito de gravar o OK, e o painel só o chama
// depois da pergunta na própria linha; o Agent recusa uma contagem que não é a
// que a tela mostrou. O Desligar e o apagar das cópias não apagam ditado.
// Nenhuma rota apaga cópia sem o clique: o ciclo nunca as apaga sozinho.
//
// # Rotas
//
//	GET  /api/retencao                a contagem, ligada, pausada, as cópias e a dos 6 meses
//	POST /api/retencao/ligar          {"previa": N}: o OK; devolve o estado, apagados e incompleto
//	POST /api/retencao/desligar       tira o OK; devolve o estado
//	POST /api/retencao/copias/apagar  apaga as cópias; devolve o estado, apagadas e ficaram
//
// Sem a interface montada (Preferencias.Retencao nil), 503. Um Agent de antes
// da F10 não tem as rotas e responde 404: o painel mostra uma frase.

// Retencao é o que as rotas usam do ciclo do expurgo (*expurgo.Ciclo). Os
// erros voltam como o storage os dá: quem chama escolhe a frase.
type Retencao interface {
	// Estado é a contagem do que o OK apagaria agora, se a regra está ligada
	// ou pausada, e quantas cópias há ao lado do banco. Só leitura.
	Estado(ctx context.Context) (storage.EstadoDosSeisMeses, error)
	// Ligar é o OK (previa é a contagem que a tela mostrou).
	Ligar(ctx context.Context, previa int) (storage.ResultadoDoLigar, error)
	// Desligar tira o OK.
	Desligar(ctx context.Context) error
	// ApagarCopias apaga as cópias do histórico ao lado do banco (F10e).
	ApagarCopias(ctx context.Context) (storage.ResultadoDasCopias, error)
}

var _ Retencao = (*expurgo.Ciclo)(nil)

// maxCorpoRetencao: o corpo do Ligar é {"previa": N}.
const maxCorpoRetencao = 1 << 10

// As frases das recusas (08-ui-ux-spec.md, "Histórico"). Todas fixas: nenhuma
// leva dado do histórico.
const (
	MsgRetencaoNaoPronta    = "O histórico deste PC ainda não está pronto para isso. Reinicie o Agent."
	MsgNadaAApagar          = "Não há nada a apagar agora."
	MsgPreviaMudou          = "A contagem mudou. Nada foi apagado."
	MsgFaltouAPrevia        = "Faltou a contagem que a tela mostrou."
	MsgCopiaDoLigarFalhou   = "Não deu para copiar o histórico. Nada foi apagado."
	MsgCopiaDoLigarDisco    = "Falta espaço no disco."
	MsgLigarFalhou          = "Não deu para apagar agora. Tente de novo."
	MsgDesligarFalhou       = "Não deu para desligar agora. Tente de novo."
	MsgCopiasNaoSairamTodas = "Nem todas as cópias saíram. O que saiu não volta."
	MsgCopiasFalhou         = "Não deu para apagar as cópias agora."
	msgLerRetencaoFalhou    = "Não deu para ler o histórico agora."
)

// MsgCopiasDemaisDoLigar diz que já há storage.MaxCopias cópias de antes dos 6
// meses; o botão "Apagar as cópias" resolve.
var MsgCopiasDemaisDoLigar = fmt.Sprintf("Já há %d cópias de antes dos 6 meses ao lado do histórico: apague as que não quiser.", storage.MaxCopias)

// retencaoDTO é o GET /api/retencao (06-api-contract.md, "Retenção no PC"):
// só números e booleanos.
type retencaoDTO struct {
	// Previa: quantos ditados do dono o OK apagaria agora (ativos e na lixeira).
	Previa int `json:"previa"`
	// NaLixeira, ComSelo e ViraramNota: subconjuntos da Previa (o que virou
	// nota não aparece no histórico; sai o ditado, e a nota fica).
	NaLixeira   int `json:"na_lixeira"`
	ComSelo     int `json:"com_selo"`
	ViraramNota int `json:"viraram_nota"`
	// DeOutroDono: os fora da janela de outro dono, que ficam e não entram na Previa.
	DeOutroDono int `json:"de_outro_dono"`
	// Ligada: o OK está gravado.
	Ligada bool `json:"ligada"`
	// Pausada: o freio pausou a regra; a pergunta volta.
	Pausada bool `json:"pausada"`
	// Copias: quantos arquivos de cópia há ao lado do banco.
	Copias int `json:"copias"`
	// CopiasApagaveis: das Copias, as que o botão apaga agora (todas menos a
	// do envio à nuvem que ainda não terminou).
	CopiasApagaveis int `json:"copias_apagaveis"`
	// CopiaDosSeisMeses: a cópia de um OK anterior está ao lado do banco; o
	// próximo OK não copia de novo, e o que nasceu depois dela não está nela.
	CopiaDosSeisMeses bool `json:"copia_dos_6_meses"`
}

func retencaoDe(e storage.EstadoDosSeisMeses) retencaoDTO {
	return retencaoDTO{
		Previa: e.Total, NaLixeira: e.NaLixeira, ComSelo: e.ComSelo, ViraramNota: e.ViraramNota, DeOutroDono: e.DeOutroDono,
		Ligada: e.Ligada, Pausada: e.Pausada, Copias: e.Copias,
		CopiasApagaveis: e.CopiasApagaveis, CopiaDosSeisMeses: e.CopiaDosSeisMeses,
	}
}

// ligouDTO é a resposta do Ligar: o estado e quantos ditados saíram agora.
type ligouDTO struct {
	retencaoDTO
	Apagados int `json:"apagados"`
	// Incompleto: o OK foi gravado e parte saiu, mas um lote seguinte falhou
	// (storage.ErrLigadoIncompleto). A regra está ligada, e o resto (a previa
	// do estado) sai na rodada seguinte do ciclo, em até 1 hora.
	Incompleto bool `json:"incompleto"`
}

// copiasApagadasDTO é a resposta do apagar das cópias: o estado, quantas
// saíram e quantas ficaram.
type copiasApagadasDTO struct {
	retencaoDTO
	Apagadas int `json:"apagadas"`
	Ficaram  int `json:"ficaram"`
}

// recusaComPreviaDTO é o 409 do Ligar quando a contagem mudou: o envelope de
// erro e, ao lado dele, o número novo.
type recusaComPreviaDTO struct {
	Error  apiError `json:"error"`
	Previa int      `json:"previa"`
}

type ligarDTO struct {
	// Previa é ponteiro para distinguir "não veio" de zero.
	Previa *int `json:"previa"`
}

// retencaoLigada responde 503 quando o Agent não montou a retenção.
func (s *Server) retencaoLigada(w http.ResponseWriter) bool {
	if s.pref.Retencao != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, MsgRetencaoNaoPronta)
	return false
}

// estadoDaRetencao lê o estado; se não dá, responde e devolve false.
func (s *Server) estadoDaRetencao(w http.ResponseWriter, r *http.Request) (retencaoDTO, bool) {
	e, err := s.pref.Retencao.Estado(r.Context())
	if err != nil {
		s.recusaDaRetencao(w, err, http.StatusInternalServerError, msgLerRetencaoFalhou)
		return retencaoDTO{}, false
	}
	return retencaoDe(e), true
}

// handleGetRetencao atende GET /api/retencao.
func (s *Server) handleGetRetencao(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.retencaoLigada(w) {
		return
	}
	if dto, ok := s.estadoDaRetencao(w, r); ok {
		writeJSON(w, http.StatusOK, dto)
	}
}

// handleLigarRetencao atende POST /api/retencao/ligar {previa}: o OK. O Agent
// reconta; só apaga se a contagem for a que a tela mostrou.
func (s *Server) handleLigarRetencao(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.retencaoLigada(w) {
		return
	}
	var body ligarDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoRetencao)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	if body.Previa == nil || *body.Previa < 0 {
		writeError(w, http.StatusBadRequest, codeBadRequest, MsgFaltouAPrevia)
		return
	}
	res, err := s.pref.Retencao.Ligar(r.Context(), *body.Previa)
	if errors.Is(err, storage.ErrLigadoIncompleto) {
		// O OK foi gravado e parte saiu: não é recusa (o ciclo já tira esse
		// erro; aqui é a segunda guarda). A tela diz o que saiu e que o resto
		// sai sozinho.
		s.log.Warn("localapi: retencao: os 6 meses ligaram incompletos", "err", err)
		res.Incompleto, err = true, nil
	}
	if err != nil {
		s.recusaDoLigar(w, err)
		return
	}
	dto, ok := s.estadoDaRetencao(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, ligouDTO{retencaoDTO: dto, Apagados: res.Apagados, Incompleto: res.Incompleto})
}

// recusaDoLigar traduz o erro do Ligar: o status e a frase de cada recusa.
func (s *Server) recusaDoLigar(w http.ResponseWriter, err error) {
	var mudou *storage.ErrPreviaMudou
	var copia *storage.ErroDaCopia
	switch {
	case errors.As(err, &mudou):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(recusaComPreviaDTO{
			Error:  apiError{Code: codeConflict, Message: MsgPreviaMudou, RequestID: storage.NewID()},
			Previa: mudou.Previa,
		})
	case errors.Is(err, storage.ErrNadaAApagar):
		writeError(w, http.StatusConflict, codeConflict, MsgNadaAApagar)
	case errors.Is(err, storage.ErrSemDonoFixo):
		writeError(w, http.StatusConflict, codeConflict, MsgRetencaoNaoPronta)
	case errors.As(err, &copia):
		// O detalhe tem o nome do arquivo, nunca o caminho, e já foi para o
		// log pelo ciclo; a tela recebe a frase.
		switch {
		case errors.Is(err, storage.ErrCopiasDemais):
			writeError(w, http.StatusConflict, codeConflict, MsgCopiaDoLigarFalhou+" "+MsgCopiasDemaisDoLigar)
		case errors.Is(err, storage.ErrDiscoCheio):
			writeError(w, http.StatusInsufficientStorage, codeInternal, MsgCopiaDoLigarFalhou+" "+MsgCopiaDoLigarDisco)
		default:
			writeError(w, http.StatusInternalServerError, codeInternal, MsgCopiaDoLigarFalhou)
		}
	default:
		s.recusaDaRetencao(w, err, http.StatusInternalServerError, MsgLigarFalhou)
	}
}

// recusaDaRetencao responde o erro que não é uma recusa conhecida: uma frase
// sem detalhe, e o log só com o erro de dentro (nunca o corpo do pedido).
func (s *Server) recusaDaRetencao(w http.ResponseWriter, err error, status int, frase string) {
	if errors.Is(err, expurgo.ErrParado) || errors.Is(err, expurgo.ErrSemSeisMeses) {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, MsgRetencaoNaoPronta)
		return
	}
	s.log.Warn("localapi: retencao: erro inesperado", "err", err)
	writeError(w, status, codeInternal, frase)
}

// handleDesligarRetencao atende POST /api/retencao/desligar: tira o OK (volta
// ao de antes: o PC guarda tudo, e o que saiu não volta). Idempotente.
func (s *Server) handleDesligarRetencao(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.retencaoLigada(w) {
		return
	}
	if err := s.pref.Retencao.Desligar(r.Context()); err != nil {
		s.recusaDaRetencao(w, err, http.StatusInternalServerError, MsgDesligarFalhou)
		return
	}
	if dto, ok := s.estadoDaRetencao(w, r); ok {
		writeJSON(w, http.StatusOK, dto)
	}
}

// handleApagarCopias atende POST /api/retencao/copias/apagar: apaga as cópias
// do histórico ao lado do banco (menos a do envio à nuvem que ainda não
// terminou) e devolve quantas saíram e quantas ficaram. A pergunta é da tela; o
// Agent nunca apaga as cópias sozinho.
func (s *Server) handleApagarCopias(w http.ResponseWriter, r *http.Request) {
	semCache(w)
	if !s.retencaoLigada(w) {
		return
	}
	res, err := s.pref.Retencao.ApagarCopias(r.Context())
	if err != nil {
		frase := MsgCopiasFalhou
		if res.Apagadas > 0 {
			frase = MsgCopiasNaoSairamTodas
		}
		s.recusaDaRetencao(w, err, http.StatusInternalServerError, frase)
		return
	}
	dto, ok := s.estadoDaRetencao(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, copiasApagadasDTO{retencaoDTO: dto, Apagadas: res.Apagadas, Ficaram: res.Ficaram})
}
