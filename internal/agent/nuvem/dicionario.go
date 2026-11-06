package nuvem

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// O dicionário para o celular na seção Nuvem do painel (ADR-023, "F. O PC: a
// opção, o envio e o aviso"): ligar, desligar e o estado. Quem manda a cópia
// sozinha, a cada mudança do arquivo, é a fase do sync (syncengine.Motor.
// EnviarDicionario); aqui ficam o clique, que fala com a nuvem NA HORA para o
// painel mostrar o resultado sem esperar a rodada, a opção gravada, e o aviso
// que o motor deixa para o painel.
//
// A opção (local_settings.dictionary_copy_enabled) é a intenção deste PC: só é
// gravada ligada com o 200 do PUT, e só é gravada desligada com o 204 do DELETE
// (ou ao desconectar, ou quando a nuvem diz que a fonte passou a ser outra).
//
// Nenhuma resposta nem log daqui leva um termo, uma variante ou o corpo da
// cópia: só contagens, a hora, o nome do outro PC (que é do próprio dono) e uma
// frase de lista fechada.

// OpcaoDoDicionario é onde a opção mora (no Agent, local_settings, pelo
// storage.OpcaoDaCopiaDoDicionario).
type OpcaoDoDicionario interface {
	Ligada(ctx context.Context) (bool, error)
	Gravar(ctx context.Context, ligada bool) error
}

// FonteDoDicionario é o dicionario.json deste PC (no Agent, o
// *aprendiz.Aplicador): o dicionário de agora, sem tocar no arquivo.
type FonteDoDicionario interface {
	Dicionario(ctx context.Context) (dictionary.Semente, error)
}

// As frases do dicionário (08-ui-ux-spec.md, "Nuvem (no PC)").
const (
	MsgDicionarioIndisponivel = "O dicionário não está disponível neste Agent."
	MsgDicionarioConecte      = "Conecte o PC à nuvem antes de levar o dicionário para o celular."
	MsgDicionarioNaoLeu       = "Não deu para ler o dicionário deste PC. Nada foi enviado."
	MsgDicionarioSemRede      = "Não deu para falar com a nuvem agora. O dicionário não foi para o celular."
	MsgDicionarioEspere       = "A nuvem pediu para esperar. Tente de novo em instantes."
	MsgDicionarioRecusouPC    = "A nuvem não reconhece mais este PC. Conecte de novo."
	MsgDicionarioNaoApagou    = "Não deu para apagar a cópia da nuvem agora. Tente de novo."
	MsgDicionarioNaoGravou    = "Não deu para guardar a opção neste PC. Nada foi enviado."
)

// EstadoDoDicionario é o que o painel mostra do dicionário para o celular (GET e
// POST /api/nuvem/dicionario). Só contagens, datas, o nome do PC que é a fonte e
// uma frase: nunca um termo.
type EstadoDoDicionario struct {
	// Ligado é a opção deste PC (a intenção).
	Ligado bool `json:"ligado"`
	// Fonte é quem a nuvem diz ser a fonte da cópia: "este", "outro" ou
	// "nenhuma". Vazio quando a nuvem não pôde ser perguntada (sem rede, ou o PC
	// desconectado).
	Fonte string `json:"fonte,omitempty"`
	// Aparelho é o nome do PC que é a fonte, quando é outro.
	Aparelho string `json:"aparelho,omitempty"`
	// Termos e AtualizadoEm (RFC 3339, UTC) são os da cópia que a nuvem guarda.
	Termos       int    `json:"termos,omitempty"`
	AtualizadoEm string `json:"atualizado_em,omitempty"`
	// EnviadoEm é a última vez, nesta execução do Agent, que a nuvem aceitou a
	// cópia deste PC (RFC 3339, UTC).
	EnviadoEm string `json:"enviado_em,omitempty"`
	// Aviso é a última frase que a fase do sync deixou para o painel.
	Aviso string `json:"aviso,omitempty"`
}

// dicionarioDoServico é a memória do Servico sobre o dicionário. Com s.mu.
type dicionarioDoServico struct {
	aviso     string
	enviadoEm time.Time
	// termos é quantos termos tinha a cópia na última vez que a nuvem a aceitou.
	termos int
}

// UsarDicionario liga o dicionário para o celular ao serviço: onde a opção mora
// e o dicionario.json de onde a cópia sai (no Agent, a storage.
// OpcaoDaCopiaDoDicionario e o aprendizado). Chamado uma vez, na montagem, antes
// de o painel e o laço subirem; sem ele, as rotas do dicionário respondem 503 e
// a fase do sync não faz nada.
func (s *Servico) UsarDicionario(opcao OpcaoDoDicionario, fonte FonteDoDicionario) {
	s.cfg.OpcaoDoDicionario, s.cfg.FonteDoDicionario = opcao, fonte
}

// ─── Estado ───

// EstadoDoDicionario é a opção deste PC, a fonte que a nuvem diz ter, e o último
// aviso. Desconectado, é só a opção (que o desconectar já desligou). Conectado,
// pergunta à nuvem (GET): sem rede, a fonte fica vazia, e o painel diz o que
// sabe.
//
// Se a opção está ligada e a nuvem diz que a fonte é outro PC (ou que não há
// cópia), este PC perdeu a fonte sem saber (o 409 só vem no próximo envio): a
// opção é desligada aqui, com o mesmo aviso, para o painel não mostrar ligado o
// que não está.
func (s *Servico) EstadoDoDicionario(ctx context.Context) (EstadoDoDicionario, error) {
	if s.cfg.OpcaoDoDicionario == nil {
		return EstadoDoDicionario{}, &Erro{Status: http.StatusServiceUnavailable, Mensagem: MsgDicionarioIndisponivel}
	}
	ligada, err := s.cfg.OpcaoDoDicionario.Ligada(ctx)
	if err != nil {
		return EstadoDoDicionario{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	e := s.estadoLocalDoDicionario(ligada)
	cli, err := s.clienteDoDicionario(ctx)
	if err != nil {
		var ne *Erro
		if errors.As(err, &ne) && ne.Status >= 500 {
			return EstadoDoDicionario{}, err
		}
		// Desconectado: só o que se sabe aqui.
		return e, nil
	}
	resp, err := s.perguntar(ctx, func(ctx context.Context) (syncengine.RespostaDoDicionario, error) {
		return cli.EstadoDoDicionario(ctx)
	})
	if errors.Is(err, syncengine.ErrNaoAutorizado) {
		// A nuvem não reconhece mais o PC: desconectado, como no Conferir.
		_ = s.TokenRecusado(ctx, cli.token)
		return s.estadoLocalDoDicionario(false), nil
	}
	if err != nil || resp.Status != http.StatusOK || resp.Estado == nil {
		// Sem rede, o 404 de uma nuvem de antes, um 5xx: a fonte fica vazia.
		s.log.Info("nuvem: não consegui perguntar a fonte do dicionário", "status", resp.Status, "erro_de_rede", err != nil)
		return e, nil
	}
	est := resp.Estado
	e.Fonte = est.Fonte
	if est.Fonte != syncengine.FonteNenhuma {
		e.Aparelho = syncengine.NomeDoAparelhoParaOAviso(est.Aparelho)
		e.Termos = est.Termos
		if !est.AtualizadoEm.IsZero() {
			e.AtualizadoEm = est.AtualizadoEm.UTC().Format(time.RFC3339)
		}
	}
	if ligada && est.Fonte != syncengine.FonteEste {
		aviso := syncengine.AvisoDeQuemEAFonte(est)
		if err := s.cfg.OpcaoDoDicionario.Gravar(ctx, false); err != nil {
			return EstadoDoDicionario{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
		}
		s.mu.Lock()
		s.dic.aviso = aviso
		s.mu.Unlock()
		e.Ligado, e.Aviso = false, aviso
	}
	return e, nil
}

// estadoLocalDoDicionario é o que se sabe sem a nuvem.
func (s *Servico) estadoLocalDoDicionario(ligada bool) EstadoDoDicionario {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := EstadoDoDicionario{Ligado: ligada, Aviso: s.dic.aviso, Termos: s.dic.termos}
	if !s.dic.enviadoEm.IsZero() {
		e.EnviadoEm = s.dic.enviadoEm.UTC().Format(time.RFC3339)
	}
	return e
}

// ─── Ligar ───

// LigarDicionario é o clique em "Levar o dicionário para o celular": monta a
// cópia do arquivo de agora, confere os limites e manda o PUT ... tomar NA HORA.
// Só com o 200 a opção é gravada ligada. Sem rede, com a cópia recusada (a nuvem
// ou os limites daqui) ou a nuvem de antes desta versão, a opção continua
// desligada, e o erro traz a frase para o painel.
func (s *Servico) LigarDicionario(ctx context.Context) (EstadoDoDicionario, error) {
	if s.cfg.OpcaoDoDicionario == nil || s.cfg.FonteDoDicionario == nil {
		return EstadoDoDicionario{}, &Erro{Status: http.StatusServiceUnavailable, Mensagem: MsgDicionarioIndisponivel}
	}
	s.dicMu.Lock()
	defer s.dicMu.Unlock()

	cli, err := s.clienteDoDicionario(ctx)
	if err != nil {
		return EstadoDoDicionario{}, err
	}
	semente, err := s.cfg.FonteDoDicionario.Dicionario(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return EstadoDoDicionario{}, ctx.Err()
		}
		return EstadoDoDicionario{}, &Erro{Status: http.StatusConflict, Mensagem: MsgDicionarioNaoLeu}
	}
	copia, sha, err := semente.BytesDaCopia()
	if err != nil {
		var ec *dictionary.ErrCopia
		if errors.As(err, &ec) {
			return EstadoDoDicionario{}, &Erro{Status: http.StatusConflict, Mensagem: syncengine.FraseDoMotivoDaCopia(string(ec.Motivo))}
		}
		return EstadoDoDicionario{}, &Erro{Status: http.StatusConflict, Mensagem: MsgDicionarioNaoLeu}
	}
	resp, err := s.perguntar(ctx, func(ctx context.Context) (syncengine.RespostaDoDicionario, error) {
		return cli.SubirDicionario(ctx, copia, sha, syncengine.EnvioTomar)
	})
	if err != nil {
		return EstadoDoDicionario{}, s.erroDoPedidoDoDicionario(ctx, err, cli.token)
	}
	switch resp.Status {
	case http.StatusOK:
	case http.StatusUnprocessableEntity:
		return EstadoDoDicionario{}, &Erro{Status: http.StatusConflict, Mensagem: syncengine.FraseDoMotivoDaCopia(resp.Motivo)}
	case http.StatusNotFound:
		return EstadoDoDicionario{}, &Erro{Status: http.StatusBadGateway, Mensagem: syncengine.AvisoDicionarioSemRota}
	default:
		s.log.Info("nuvem: a nuvem não aceitou a cópia do dicionário", "status", resp.Status, "codigo", resp.Codigo)
		return EstadoDoDicionario{}, &Erro{Status: http.StatusBadGateway, Mensagem: MsgDicionarioSemRede}
	}
	if err := s.cfg.OpcaoDoDicionario.Gravar(ctx, true); err != nil {
		// A nuvem tem a cópia e a opção não ficou ligada: a cópia sai (melhor
		// esforço), para não ficar na nuvem o que o PC não pediu.
		_, _ = s.perguntar(ctx, func(ctx context.Context) (syncengine.RespostaDoDicionario, error) {
			return cli.ApagarDicionario(ctx)
		})
		return EstadoDoDicionario{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgDicionarioNaoGravou, causa: err}
	}
	s.mu.Lock()
	s.dic = dicionarioDoServico{enviadoEm: s.cfg.Agora(), termos: len(semente.Entradas)}
	s.mu.Unlock()
	s.log.Info("nuvem: dicionário levado para o celular", "termos", len(semente.Entradas))
	return s.EstadoDoDicionario(ctx)
}

// ─── Desligar ───

// DesligarDicionario é o desligar: o DELETE NA HORA. Com o 204 (e o 404 de uma
// nuvem que nem tem a rota), a opção é gravada desligada. Sem rede, ou com outra
// resposta, a opção CONTINUA LIGADA e o erro diz "Não deu para apagar a cópia da
// nuvem agora. Tente de novo.": desligar sem apagar daria a impressão de que a
// cópia saiu. Desconectado, só grava desligada (não há cópia que este PC possa
// apagar: a revogação já a apagou).
func (s *Servico) DesligarDicionario(ctx context.Context) (EstadoDoDicionario, error) {
	if s.cfg.OpcaoDoDicionario == nil {
		return EstadoDoDicionario{}, &Erro{Status: http.StatusServiceUnavailable, Mensagem: MsgDicionarioIndisponivel}
	}
	s.dicMu.Lock()
	defer s.dicMu.Unlock()

	cli, err := s.clienteDoDicionario(ctx)
	switch {
	case err == nil:
		resp, err := s.perguntar(ctx, func(ctx context.Context) (syncengine.RespostaDoDicionario, error) {
			return cli.ApagarDicionario(ctx)
		})
		if err != nil {
			if errors.Is(err, syncengine.ErrNaoAutorizado) {
				// A nuvem não reconhece mais o PC: ele fica desconectado, e a
				// cópia saiu junto com o aparelho dela.
				_ = s.TokenRecusado(ctx, cli.token)
				break
			}
			return EstadoDoDicionario{}, &Erro{Status: http.StatusBadGateway, Mensagem: MsgDicionarioNaoApagou}
		}
		if resp.Status != http.StatusNoContent && resp.Status != http.StatusNotFound && resp.Status != http.StatusOK {
			s.log.Info("nuvem: a nuvem não apagou a cópia do dicionário", "status", resp.Status, "codigo", resp.Codigo)
			return EstadoDoDicionario{}, &Erro{Status: http.StatusBadGateway, Mensagem: MsgDicionarioNaoApagou}
		}
	default:
		var ne *Erro
		if errors.As(err, &ne) && ne.Status >= 500 {
			return EstadoDoDicionario{}, err
		}
		// Desconectado: nada a apagar na nuvem.
	}
	if err := s.cfg.OpcaoDoDicionario.Gravar(ctx, false); err != nil {
		return EstadoDoDicionario{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	s.mu.Lock()
	s.dic = dicionarioDoServico{}
	s.mu.Unlock()
	s.log.Info("nuvem: dicionário tirado do celular")
	return s.EstadoDoDicionario(ctx)
}

// ─── A rede ───

// clienteDoDicionario é o cliente do sync com o token de agora, ou, sem a
// conexão, o *Erro 409 MsgDicionarioConecte (e o 500 do Cofre ou da instalação).
func (s *Servico) clienteDoDicionario(ctx context.Context) (*clienteComToken, error) {
	cred, err := s.Credencial(ctx)
	if err != nil {
		return nil, err
	}
	if !cred.Conectado {
		return nil, &Erro{Status: http.StatusConflict, Mensagem: MsgDicionarioConecte}
	}
	return &clienteComToken{Cliente: syncengine.NovoCliente(cred.URL, cred.Token, s.cfg.HTTP), token: cred.Token}, nil
}

// clienteComToken guarda o token junto com o cliente, só para o TokenRecusado.
type clienteComToken struct {
	*syncengine.Cliente
	token string
}

// perguntar roda um pedido do dicionário com o prazo de PrazoDoPedidoDoDicionario.
func (s *Servico) perguntar(ctx context.Context, f func(context.Context) (syncengine.RespostaDoDicionario, error)) (syncengine.RespostaDoDicionario, error) {
	ctx, cancel := context.WithTimeout(ctx, PrazoDoPedidoDoDicionario)
	defer cancel()
	return f(ctx)
}

// PrazoDoPedidoDoDicionario é quanto o clique espera a nuvem.
const PrazoDoPedidoDoDicionario = 30 * time.Second

// erroDoPedidoDoDicionario traduz a falha de um pedido (o 401, o 429, a rede)
// numa frase para o painel. O 401 desconecta o PC, como no Conferir.
func (s *Servico) erroDoPedidoDoDicionario(ctx context.Context, err error, token string) error {
	var esp *syncengine.ErrEspere
	switch {
	case errors.Is(err, syncengine.ErrNaoAutorizado):
		_ = s.TokenRecusado(ctx, token)
		return &Erro{Status: http.StatusConflict, Mensagem: MsgDicionarioRecusouPC}
	case errors.As(err, &esp):
		return &Erro{Status: http.StatusBadGateway, Mensagem: MsgDicionarioEspere}
	case ctx.Err() != nil:
		return ctx.Err()
	}
	return &Erro{Status: http.StatusBadGateway, Mensagem: MsgDicionarioSemRede}
}

// desligarOpcaoDoDicionario grava a opção desligada e esquece o que o painel
// sabia (o aviso e a última hora): o PC foi desconectado, ou vai ser conectado
// de novo, e reconectar não manda o dicionário sem um clique novo. Melhor
// esforço: o desconectar nunca falha por isso.
func (s *Servico) desligarOpcaoDoDicionario(ctx context.Context) {
	s.mu.Lock()
	s.dic = dicionarioDoServico{}
	s.mu.Unlock()
	if s.cfg.OpcaoDoDicionario == nil {
		return
	}
	if err := s.cfg.OpcaoDoDicionario.Gravar(ctx, false); err != nil {
		s.log.Warn("nuvem: não consegui desligar a opção do dicionário", "err", err)
	}
}

// ─── O que o sync usa ───

// OpcaoDaCopia é a opção como a fase do dicionário do sync a usa
// (syncengine.OpcaoDaCopia): ler, desligar com o aviso, avisar e contar o envio.
func (s *Servico) OpcaoDaCopia() syncengine.OpcaoDaCopia { return opcaoDaCopia{s} }

type opcaoDaCopia struct{ s *Servico }

func (o opcaoDaCopia) Ligada(ctx context.Context) (bool, error) {
	if o.s.cfg.OpcaoDoDicionario == nil {
		return false, nil
	}
	return o.s.cfg.OpcaoDoDicionario.Ligada(ctx)
}

func (o opcaoDaCopia) Desligar(ctx context.Context, aviso string) error {
	if o.s.cfg.OpcaoDoDicionario != nil {
		if err := o.s.cfg.OpcaoDoDicionario.Gravar(ctx, false); err != nil {
			return err
		}
	}
	o.s.mu.Lock()
	o.s.dic = dicionarioDoServico{aviso: aviso}
	o.s.mu.Unlock()
	return nil
}

func (o opcaoDaCopia) Avisar(aviso string) {
	o.s.mu.Lock()
	o.s.dic.aviso = aviso
	o.s.mu.Unlock()
}

func (o opcaoDaCopia) Enviou(termos int, quando time.Time) {
	o.s.mu.Lock()
	o.s.dic.aviso, o.s.dic.enviadoEm, o.s.dic.termos = "", quando, termos
	o.s.mu.Unlock()
}
