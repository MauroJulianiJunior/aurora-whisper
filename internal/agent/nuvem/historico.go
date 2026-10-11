package nuvem

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O lado do histórico na seção Nuvem do painel do PC (ADR-019, "Decisões da
// F7c"): o resumo (a prévia do envio inicial, o progresso, o que não subiu,
// o que espera, a última rodada), o "Enviar o histórico" (a cópia do banco e
// os marcadores) e o "Esquecer esta nuvem". Quem faz o trabalho no banco é o
// storage (Historico; no Agent, storage.HistoricoDaNuvem); aqui fica a regra
// de quando cada um vale e a frase de cada recusa. Nada daqui fala com a
// rede.

// Historico é o que o Servico usa do histórico (storage.HistoricoDaNuvem).
type Historico interface {
	Resumo(ctx context.Context, agora time.Time) (storage.ResumoDoEnvio, error)
	Enviar(ctx context.Context, agora time.Time) (storage.ResultadoDoEnvio, error)
	Esquecer(ctx context.Context, agora time.Time) error
}

// As frases do envio e do esquecer (08-ui-ux-spec.md, "Nuvem (no PC)").
const (
	MsgSemHistorico    = "O histórico não está disponível neste Agent."
	MsgConecteAntes    = "Conecte o PC à nuvem antes de enviar o histórico."
	MsgConferindo      = "O PC ainda está conferindo a nuvem. Tente de novo em alguns segundos."
	MsgDesconecteAntes = "Desconecte antes de esquecer a nuvem."
	MsgCancelePedido   = "Cancele o pedido em curso antes de esquecer a nuvem."
	MsgCopiaFalhou     = "Não deu para copiar o histórico. Nada foi enviado."
	MsgDiscoCheio      = "Falta espaço no disco."
)

// MsgCopiasDemais diz que já há storage.MaxCopias cópias de antes do envio.
var MsgCopiasDemais = fmt.Sprintf("Já há %d cópias de antes do envio ao lado do histórico: apague as que não quiser.", storage.MaxCopias)

// ResumoDoHistorico é o que a seção Nuvem mostra do histórico (GET
// /api/nuvem). Só contagens e datas: nunca texto, id ou caminho.
type ResumoDoHistorico struct {
	// Previa: quantos ditados o "Enviar o histórico" mandaria agora.
	Previa int `json:"previa"`
	// PreviaTrocas: quantas trocas aprendidas o "Enviar o histórico" soltaria
	// (o M de "Vão subir N ditados e M trocas aprendidas", ADR-019, "Decisões
	// da F8", D3). Com Previa 0 e PreviaTrocas maior que 0, a pergunta
	// aparece. Zero depois do aceite. O nome no fio é o que a web lê (06,
	// F8h): previa_trocas.
	PreviaTrocas int `json:"previa_trocas"`
	// FreioTrocas: o aviso do freio do aprendido (06, F8h): quantos pares
	// saíram do dicionario.json de uma vez na última varredura, e que por
	// isso nada do aprendido sobe (syncengine.Laco.Freio). Só conectado, e
	// ausente sem o freio (some na varredura que o solta).
	FreioTrocas int `json:"freio_trocas,omitempty"`
	// Aceito: a pessoa já mandou enviar (ou não havia nada a enviar).
	Aceito bool `json:"aceito"`
	// Envio: o envio inicial em curso; ausente quando não há.
	Envio *Progresso `json:"envio,omitempty"`
	// NaoSubiram: ditados deste PC que a nuvem recusou ou pôs fora da janela.
	NaoSubiram int `json:"nao_subiram"`
	// Pendentes: o que ainda não subiu (marcadores na fila).
	Pendentes int `json:"pendentes"`
	// Mudancas: dos Pendentes, as mudanças feitas aqui num ditado que a
	// nuvem tem (fora o ditado novo e o histórico que ainda não subiram): o
	// número da pergunta do esquecer.
	Mudancas int `json:"mudancas"`
	// ConferidoEm: a última rodada que deu certo (RFC 3339), ou vazio.
	ConferidoEm string `json:"conferido_em,omitempty"`
	// Falhou: a última rodada não deu certo; o PC tenta de novo sozinho.
	Falhou bool `json:"falhou"`
	// OutraNuvem: este PC esqueceu uma nuvem que já tinha ditados dele.
	OutraNuvem bool `json:"outra_nuvem"`
}

// Progresso é o envio inicial em curso: feitos de total.
type Progresso struct {
	Feitos int `json:"feitos"`
	Total  int `json:"total"`
}

// resumo lê o resumo do histórico para o Estado. Sem o Historico, ou se a
// leitura falha (registrada no log, sem detalhe), nil: a seção mostra o
// resto.
func (s *Servico) resumo(ctx context.Context) *ResumoDoHistorico {
	if s.cfg.Historico == nil {
		return nil
	}
	r, err := s.cfg.Historico.Resumo(ctx, s.cfg.Agora())
	if err != nil {
		s.log.Warn("nuvem: não consegui ler o resumo do histórico", "err", err)
		return nil
	}
	out := &ResumoDoHistorico{
		Previa: r.Previa, PreviaTrocas: r.Trocas, Aceito: r.Aceito, NaoSubiram: r.NaoSubiram, Pendentes: r.Pendentes, Mudancas: r.Mudancas,
		Falhou: r.Falhou, OutraNuvem: r.OutraNuvem,
	}
	if r.Total > 0 {
		out.Envio = &Progresso{Feitos: r.Feitos, Total: r.Total}
	}
	if r.ConferidoEm != nil {
		out.ConferidoEm = r.ConferidoEm.UTC().Format(time.RFC3339)
	}
	return out
}

// EnviarHistorico é o clique em "Enviar o histórico" (D1; e a D3 da F8: o
// clique solta também as trocas aprendidas, e com N = 0 e M > 0 grava o
// aceite sem cópia, porque a cópia é do histórico e não há ditado a enviar;
// as trocas sobem pela rodada que ele acorda): só conectado, e só
// depois da primeira rodada que deu certo com esta conexão (o pull do cursor
// 0 já trouxe, e reconciliou, o que a nuvem tem: de volta à mesma conta
// depois de um esquecer, a prévia é só o que ela não tem, e nada sobe com a
// base vazia por cima de um ditado que outro aparelho mudou). Faz a cópia do
// banco e põe os ditados elegíveis na fila (storage), e acorda o laço de
// sync, que os leva em lotes. A cópia que falha não marca nada e diz por quê
// (MsgCopiaFalhou e, se der, MsgCopiasDemais ou MsgDiscoCheio).
func (s *Servico) EnviarHistorico(ctx context.Context) (Estado, error) {
	if s.cfg.Historico == nil {
		return Estado{}, &Erro{Status: http.StatusServiceUnavailable, Mensagem: MsgSemHistorico}
	}
	conectado, err := s.conectado(ctx)
	if err != nil {
		return Estado{}, err
	}
	if !conectado {
		return Estado{}, &Erro{Status: http.StatusConflict, Mensagem: MsgConecteAntes}
	}
	resumo, err := s.cfg.Historico.Resumo(ctx, s.cfg.Agora())
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	if resumo.ConferidoEm == nil {
		return Estado{}, &Erro{Status: http.StatusConflict, Mensagem: MsgConferindo}
	}
	r, err := s.cfg.Historico.Enviar(ctx, s.cfg.Agora())
	if err != nil {
		var ec *storage.ErroDaCopia
		if errors.As(err, &ec) {
			// O detalhe tem o nome do arquivo, nunca o caminho: vai para o
			// log; a tela recebe a frase.
			s.log.Warn("nuvem: a cópia do histórico antes do envio falhou; nada foi marcado",
				"motivo", motivoDaCopia(ec.Motivo), "detalhe", ec.Detalhe)
			switch {
			case errors.Is(err, storage.ErrCopiasDemais):
				return Estado{}, &Erro{Status: http.StatusConflict, Mensagem: MsgCopiaFalhou + " " + MsgCopiasDemais, causa: storage.ErrCopiasDemais}
			case errors.Is(err, storage.ErrDiscoCheio):
				return Estado{}, &Erro{Status: http.StatusInsufficientStorage, Mensagem: MsgCopiaFalhou + " " + MsgDiscoCheio, causa: storage.ErrDiscoCheio}
			}
			return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCopiaFalhou, causa: storage.ErrCopiaDoEnvio}
		}
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	s.log.Info("nuvem: o histórico entrou na fila para subir",
		"marcados", r.Marcados, "trocas", resumo.Trocas, "copiou", r.Copia != "", "ja_aceito", r.JaAceito)
	s.acordar()
	return s.Estado(ctx)
}

// motivoDaCopia é o motivo da cópia que falhou, para o log (lista fechada).
func motivoDaCopia(err error) string {
	switch {
	case errors.Is(err, storage.ErrCopiasDemais):
		return "copias_demais"
	case errors.Is(err, storage.ErrDiscoCheio):
		return "disco_cheio"
	}
	return "copia_falhou"
}

// Esquecer é o "Esquecer esta nuvem" (ADR-019, "Decisões da F7c"): só
// desconectado e sem pedido em curso. Esquece a nuvem, a conta e o que é do
// sync (storage: as chaves, o cursor, a fila e as versões da nuvem nas
// linhas) e tira o token do Gerenciador de Credenciais, se ainda estiver lá.
// Os ditados ficam, com o conteúdo de sempre; na nuvem, nada muda. Depois, a
// D3 deixa conectar a outra nuvem ou conta (nada mais está gravado), e o
// histórico só sobe com o "Enviar o histórico" de novo.
func (s *Servico) Esquecer(ctx context.Context) (Estado, error) {
	if s.cfg.Historico == nil {
		return Estado{}, &Erro{Status: http.StatusServiceUnavailable, Mensagem: MsgSemHistorico}
	}
	s.mu.Lock()
	p, _ := s.pedidoValendo()
	s.mu.Unlock()
	if p != nil {
		return Estado{}, &Erro{Status: http.StatusConflict, Mensagem: MsgCancelePedido}
	}
	conectado, err := s.conectado(ctx)
	if err != nil {
		return Estado{}, err
	}
	if conectado {
		return Estado{}, &Erro{Status: http.StatusConflict, Mensagem: MsgDesconecteAntes}
	}
	if base, ok, err := s.cfg.Instalacao.Ler(ctx, chaveURL); err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	} else if !ok || base == "" {
		// Nada a esquecer.
		return s.Estado(ctx)
	}
	if err := s.cfg.Cofre.Apagar(s.cfg.NomeNoCofre); err != nil && !errors.Is(err, chaves.ErrAusente) {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCofre, causa: err}
	}
	if err := s.cfg.Historico.Esquecer(ctx, s.cfg.Agora()); err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	s.mu.Lock()
	s.aviso, s.concluido, s.conferidoEm = "", "", time.Time{}
	s.mu.Unlock()
	s.log.Info("nuvem: este PC esqueceu a nuvem; os ditados ficam")
	s.acordar()
	return s.Estado(ctx)
}

// acordar avisa o laço de sync (AoConectar): ele reconta a fila e a bandeja.
func (s *Servico) acordar() {
	s.mu.Lock()
	f := s.aoConectar
	s.mu.Unlock()
	if f != nil {
		f()
	}
}
