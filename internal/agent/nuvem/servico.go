package nuvem

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Cofre é onde o token do aparelho mora: o mesmo chaves.Cofre da chave de API
// (no Agent, o Gerenciador de Credenciais, atrás do portão sysinput), com o
// nome de NomeNoCofre no lugar do provedor.
type Cofre interface {
	Gravar(provedor, chave string) error
	Ler(provedor string) (string, error)
	Apagar(provedor string) error
}

// Instalacao são as chaves da nuvem na tabela instalacao do historico.db
// (storage.Instalacao).
type Instalacao interface {
	Ler(ctx context.Context, nome string) (string, bool, error)
	Gravar(ctx context.Context, valores map[string]string) error
	Apagar(ctx context.Context, nomes ...string) error
}

// O alvo do token no Gerenciador de Credenciais, por modo (ADR-019, D7): o
// bin\ de desenvolvimento (conta de teste) e o Agent instalado são do mesmo
// usuário do Windows, e sem alvos separados conectar um trocaria o token do
// outro, e o PC real passaria a usar a conta de teste.
const (
	// NomeNoCofreInstalado: AuroraWhisper/nuvem, o que o desinstalador apaga
	// na opção "apagar dados".
	NomeNoCofreInstalado = "nuvem"
	// NomeNoCofrePortatil: AuroraWhisper/nuvem-portatil. O bin\ não é
	// instalado, e o desinstalador não o toca.
	NomeNoCofrePortatil = "nuvem-portatil"
)

// NomeNoCofre é o nome do token no Cofre para o modo.
func NomeNoCofre(portatil bool) string {
	if portatil {
		return NomeNoCofrePortatil
	}
	return NomeNoCofreInstalado
}

// As chaves da tabela instalacao (storage.ChaveDaNuvem).
const (
	chaveInstallID   = "install_id"
	chaveURL         = "nuvem_url"
	chaveConta       = storage.ChaveNuvemConta
	chaveEmail       = "nuvem_email"
	chaveAparelho    = storage.ChaveNuvemAparelho
	chaveNome        = "nuvem_nome"
	chaveConectadoEm = storage.ChaveNuvemConectadoEm
)

// O estado que o painel mostra.
const (
	Desconectado = "desconectado"
	Conectando   = "conectando"
	Conectado    = "conectado"
)

// Os avisos que acompanham o estado (o painel tem a frase de cada um).
const (
	// AvisoRevogado: a nuvem respondeu 401 ao token (revogado, vencido ou
	// apagado): o PC foi desconectado lá.
	AvisoRevogado = "revogado"
	// AvisoSemResposta: o Desconectar apagou o token daqui, mas a nuvem não
	// confirmou a revogação.
	AvisoSemResposta = "sem_resposta"
	// AvisoNaoAbriu: o navegador não abriu a página de aprovação.
	AvisoNaoAbriu = "nao_abriu"
	// AvisoVencido: o pedido em curso venceu.
	AvisoVencido = "vencido"
	// AvisoOutraConta: a D3, a instalação já é de outra conta.
	AvisoOutraConta = "outra_conta"
	// AvisoFalhou: o retorno chegou, mas a troca não deu certo.
	AvisoFalhou = "falhou"
	// AvisoSemRede: a conferência não alcançou a nuvem; o PC segue conectado.
	AvisoSemRede = "sem_rede"
)

// Prazos.
const (
	// PrazoMaximoDoPedido: o pedido em curso nunca vale mais que isso, diga a
	// nuvem o que disser.
	PrazoMaximoDoPedido = 10 * time.Minute
	// IntervaloDaConferencia: o Conferir fala com a nuvem no máximo uma vez
	// nesse intervalo.
	IntervaloDaConferencia = 30 * time.Second
	// PrazoDoDesconectar: quanto o Desconectar espera a nuvem revogar.
	PrazoDoDesconectar = 5 * time.Second
	// PrazoDaRede: o prazo de cada pedido à nuvem.
	PrazoDaRede = 10 * time.Second
	// maxRespostaDaNuvem: o corpo de uma resposta da nuvem.
	maxRespostaDaNuvem = 64 << 10
)

// Estado é o que o painel local mostra (GET /api/nuvem). Nunca o token.
type Estado struct {
	Estado string `json:"estado"`
	// URL é a nuvem desta instalação (ou do pedido em curso), para preencher
	// o campo.
	URL         string `json:"url"`
	Email       string `json:"email,omitempty"`
	Nome        string `json:"nome,omitempty"`
	Conferencia string `json:"conferencia,omitempty"`
	// AbrirURL é a página de aprovação do pedido em curso (o "Abrir de
	// novo" do painel é um link para ela).
	AbrirURL string `json:"abrir_url,omitempty"`
	Aviso    string `json:"aviso,omitempty"`
	// Historico é o lado do histórico (F7c): a prévia do envio inicial, o
	// progresso, o que não subiu e a última rodada. Só com uma nuvem
	// gravada (conectado, ou desconectado com a URL) e sem pedido em curso.
	Historico *ResumoDoHistorico `json:"historico,omitempty"`
}

// Erro é uma recusa com a frase para a tela e o status da API local.
type Erro struct {
	Status   int
	Mensagem string
	causa    error
}

func (e *Erro) Error() string {
	if e.causa != nil {
		return "nuvem: " + e.Mensagem + ": " + e.causa.Error()
	}
	return "nuvem: " + e.Mensagem
}

func (e *Erro) Unwrap() error { return e.causa }

func recusa(msg string) *Erro { return &Erro{Status: http.StatusBadRequest, Mensagem: msg} }

// ErrNaoAbriu: o pedido foi criado, mas o navegador não abriu. O estado
// (conectando, com o aviso) vale; o painel mostra o "Abrir de novo".
var ErrNaoAbriu = errors.New("nuvem: o navegador não abriu a página de aprovação")

// Config é o que o Servico usa. Cofre, Abrir, Instalacao e NomeNoCofre são
// obrigatórios.
type Config struct {
	Cofre       Cofre
	NomeNoCofre string
	// Abrir abre o endereço no navegador de quem usa (painel.AbrirNoNavegador,
	// atrás do portão sysinput).
	Abrir      func(url string) error
	Instalacao Instalacao
	// HTTP fala com a nuvem; nil é NovoCliente().
	HTTP *http.Client
	// Agora é o relógio; nil é time.Now.
	Agora    func() time.Time
	Politica Politica
	// Versao é a do Agent; NomeDoPC, o que a nuvem mostra (NomeDoPC()).
	Versao   string
	NomeDoPC string
	// NovoID gera o install_id (UUID v7: storage.NewID). Obrigatório.
	NovoID func() string
	// Historico é o envio inicial, o resumo e o esquecer (F7c;
	// storage.HistoricoDaNuvem). nil: o Estado vem sem o resumo, e o
	// EnviarHistorico e o Esquecer respondem 503.
	Historico Historico
	// Log recebe só o que aconteceu, com códigos; nil descarta.
	Log *slog.Logger
}

// NovoCliente é o cliente HTTP da nuvem: prazo de PrazoDaRede e sem seguir
// redirecionamento (uma resposta 3xx é falha, e o token nunca vai para outro
// endereço).
func NovoCliente() *http.Client {
	return &http.Client{
		Timeout:       PrazoDaRede,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Servico é o "conectar à nuvem" do Agent. Seguro para uso concorrente.
type Servico struct {
	cfg Config
	log *slog.Logger

	mu sync.Mutex
	// porta é a do painel local (subirPainel): o retorno vem para ela.
	porta  int
	pedido *pedido
	aviso  string
	// conferidoEm é a última vez que o Conferir falou com a nuvem.
	conferidoEm time.Time
	// concluido é o id do pedido cuja troca deu certo: o mesmo retorno de
	// novo (o navegador que recarrega a página) mostra "PC conectado" outra
	// vez, sem trocar nada. Sai quando o PC deixa de estar conectado.
	concluido string
	// aoConectar é chamado depois de uma troca que deu certo (AoConectar).
	aoConectar func()
	// freio é o aviso do freio do aprendido (UsarFreio); nil é sem freio.
	freio func() int
}

// pedido é o pareamento em curso, só na memória.
type pedido struct {
	base        string
	id          string
	verificador string
	conferencia string
	abrirURL    string
	nome        string
	prazo       time.Time
}

// Novo monta o Servico. Faltar um obrigatório é erro de quem monta (panic,
// como localapi.New).
func Novo(cfg Config) *Servico {
	switch {
	case cfg.Cofre == nil:
		panic("nuvem: Cofre nil")
	case cfg.Abrir == nil:
		panic("nuvem: Abrir nil")
	case cfg.Instalacao == nil:
		panic("nuvem: Instalacao nil")
	case cfg.NomeNoCofre == "":
		panic("nuvem: NomeNoCofre vazio")
	case cfg.NovoID == nil:
		panic("nuvem: NovoID nil")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = NovoCliente()
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Servico{cfg: cfg, log: log}
}

// NomeNoCofre é o nome do token no Cofre (o alvo do modo, NomeNoCofre()).
func (s *Servico) NomeNoCofre() string { return s.cfg.NomeNoCofre }

// UsarPorta diz a porta do painel local, depois que ele sobe.
func (s *Servico) UsarPorta(p int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.porta = p
}

// NomeDoPC limpa o nome do computador para a nuvem: sem caractere de
// controle, sem espaço nas pontas, até 80 caracteres; vazio vira "PC".
func NomeDoPC(bruto string) string {
	var b strings.Builder
	for _, r := range bruto {
		if !unicode.IsControl(r) && r != utf8.RuneError {
			b.WriteRune(r)
		}
	}
	nome := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(nome) > 80 {
		nome = strings.TrimSpace(string([]rune(nome)[:80]))
	}
	if nome == "" {
		return "PC"
	}
	return nome
}

// ─── Estado ───

// Estado é o estado de agora. Conectado é o aparelho na instalação e o token
// no Cofre; sem o token, a instalação é limpa e o PC está desconectado.
func (s *Servico) Estado(ctx context.Context) (Estado, error) {
	s.mu.Lock()
	p, aviso := s.pedidoValendo()
	s.mu.Unlock()
	if p != nil {
		return Estado{Estado: Conectando, URL: p.base, Conferencia: p.conferencia, AbrirURL: p.abrirURL, Aviso: aviso}, nil
	}
	base, _, err := s.cfg.Instalacao.Ler(ctx, chaveURL)
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	conectado, err := s.conectado(ctx)
	if err != nil {
		return Estado{}, err
	}
	if !conectado {
		e := Estado{Estado: Desconectado, URL: base, Aviso: aviso}
		if base != "" {
			// A conta fica gravada depois do Desconectar (a D3): o "Esquecer
			// esta nuvem" diz de qual conta o PC deixa de saber.
			if e.Email, _, err = s.cfg.Instalacao.Ler(ctx, chaveEmail); err != nil {
				return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
			}
			e.Historico = s.resumo(ctx)
		}
		return e, nil
	}
	email, _, err := s.cfg.Instalacao.Ler(ctx, chaveEmail)
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	nome, _, err := s.cfg.Instalacao.Ler(ctx, chaveNome)
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	h := s.resumo(ctx)
	if h != nil {
		s.mu.Lock()
		freio := s.freio
		s.mu.Unlock()
		if freio != nil {
			if n := freio(); n > 0 {
				h.FreioTrocas = n
			}
		}
	}
	return Estado{Estado: Conectado, URL: base, Email: email, Nome: nome, Aviso: aviso, Historico: h}, nil
}

// pedidoValendo devolve o pedido em curso se ele não venceu (o vencido sai e
// deixa o aviso). Com s.mu.
func (s *Servico) pedidoValendo() (*pedido, string) {
	if s.pedido != nil && !s.cfg.Agora().Before(s.pedido.prazo) {
		s.pedido = nil
		s.aviso = AvisoVencido
	}
	return s.pedido, s.aviso
}

// conectado: o aparelho está na instalação e o token no Cofre. Sem o token
// (apagado à mão, ou pelo desinstalador), limpa o aparelho.
func (s *Servico) conectado(ctx context.Context) (bool, error) {
	aparelho, ok, err := s.cfg.Instalacao.Ler(ctx, chaveAparelho)
	if err != nil {
		return false, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	if !ok || aparelho == "" {
		return false, nil
	}
	_, err = s.cfg.Cofre.Ler(s.cfg.NomeNoCofre)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, chaves.ErrAusente):
		s.log.Info("nuvem: o token não está no Gerenciador de Credenciais; desconectado neste PC")
		if err := s.esquecerAparelho(ctx); err != nil {
			return false, err
		}
		return false, nil
	default:
		return false, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCofre, causa: err}
	}
}

// esquecerAparelho tira o aparelho da instalação. Ficam a URL, a conta e o
// e-mail: a D3 e o campo preenchido.
func (s *Servico) esquecerAparelho(ctx context.Context) error {
	s.mu.Lock()
	s.concluido = ""
	s.mu.Unlock()
	if err := s.cfg.Instalacao.Apagar(ctx, chaveAparelho, chaveNome, chaveConectadoEm); err != nil {
		return &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	return nil
}

// ─── Conectar ───

type inicioRequest struct {
	InstallID     string `json:"install_id"`
	DeviceName    string `json:"device_name"`
	Platform      string `json:"platform"`
	AppVersion    string `json:"app_version"`
	CodeChallenge string `json:"code_challenge"`
	RedirectPort  int    `json:"redirect_port"`
}

type inicioResponse struct {
	Pedido      string `json:"pedido"`
	Conferencia string `json:"conferencia"`
	ExpiresIn   int    `json:"expires_in"`
}

// Conectar pede o pareamento à nuvem e abre a página de aprovação. A URL é
// conferida antes de qualquer rede. Com o navegador que não abriu, devolve o
// estado (conectando, com o aviso) e um erro com ErrNaoAbriu.
func (s *Servico) Conectar(ctx context.Context, bruta string) (Estado, error) {
	base, err := NormalizarURL(bruta, s.cfg.Politica)
	if err != nil {
		return Estado{}, err
	}
	s.mu.Lock()
	porta := s.porta
	s.mu.Unlock()
	if porta == 0 {
		return Estado{}, &Erro{Status: http.StatusServiceUnavailable, Mensagem: MsgPainelNaoSubiu}
	}
	conectado, err := s.conectado(ctx)
	if err != nil {
		return Estado{}, err
	}
	if conectado {
		return Estado{}, &Erro{Status: http.StatusConflict, Mensagem: MsgJaConectado}
	}
	// A D3 pela URL, antes da rede: a instalação que já é de outra nuvem não
	// pede nada a esta.
	if antes, ok, err := s.cfg.Instalacao.Ler(ctx, chaveURL); err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	} else if ok && antes != base {
		return Estado{}, &Erro{Status: http.StatusConflict, Mensagem: MsgOutraConta}
	}
	installID, err := s.installID(ctx)
	if err != nil {
		return Estado{}, err
	}

	verificador := novoSegredo()
	nome := NomeDoPC(s.cfg.NomeDoPC)
	var ini inicioResponse
	status, err := s.pedir(ctx, http.MethodPost, base+"/api/v1/device-auth/start", "", inicioRequest{
		InstallID: installID, DeviceName: nome, Platform: "windows", AppVersion: s.cfg.Versao,
		CodeChallenge: desafioS256(verificador), RedirectPort: porta,
	}, &ini)
	if err != nil || status != http.StatusOK || !pareceUUID(ini.Pedido) || !pareceConferencia(ini.Conferencia) || ini.ExpiresIn <= 0 {
		s.log.Warn("nuvem: o pedido de pareamento não deu certo", "status", status, "erro_de_rede", err != nil)
		return Estado{}, &Erro{Status: http.StatusBadGateway, Mensagem: MsgSemNuvem}
	}
	prazo := time.Duration(ini.ExpiresIn) * time.Second
	if prazo > PrazoMaximoDoPedido {
		prazo = PrazoMaximoDoPedido
	}
	p := &pedido{
		base: base, id: strings.ToLower(ini.Pedido), verificador: verificador, conferencia: ini.Conferencia,
		// O endereço é montado aqui, com a origem que a pessoa digitou: o
		// auth_url da resposta é ignorado.
		abrirURL: base + "/device/authorize?" + url.Values{"pedido": {strings.ToLower(ini.Pedido)}}.Encode(),
		nome:     nome, prazo: s.cfg.Agora().Add(prazo),
	}
	s.mu.Lock()
	s.pedido = p
	s.aviso = ""
	s.mu.Unlock()
	s.log.Info("nuvem: pedido de pareamento criado; abrindo a página de aprovação")

	estado := Estado{Estado: Conectando, URL: base, Conferencia: p.conferencia, AbrirURL: p.abrirURL}
	if err := s.cfg.Abrir(p.abrirURL); err != nil {
		// O erro de quem abre não leva o endereço (painel.AbrirNoNavegador).
		s.log.Warn("nuvem: o navegador não abriu a página de aprovação", "err", err)
		s.mu.Lock()
		if s.pedido == p {
			s.aviso = AvisoNaoAbriu
		}
		s.mu.Unlock()
		estado.Aviso = AvisoNaoAbriu
		return estado, fmt.Errorf("%w: %w", ErrNaoAbriu, err)
	}
	return estado, nil
}

// installID lê o install_id da instalação ou o cria (uma vez por banco).
func (s *Servico) installID(ctx context.Context) (string, error) {
	id, ok, err := s.cfg.Instalacao.Ler(ctx, chaveInstallID)
	if err != nil {
		return "", &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	if ok && pareceUUID(id) {
		return id, nil
	}
	id = s.cfg.NovoID()
	if err := s.cfg.Instalacao.Gravar(ctx, map[string]string{chaveInstallID: id}); err != nil {
		return "", &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	return id, nil
}

// ─── Conferir ───

// Conferir pergunta à nuvem se o token ainda vale (GET /api/v1/me), no
// máximo uma vez a cada IntervaloDaConferencia, e só conectado. É ação de
// quem abriu a tela (sem laço): o painel chama ao abrir as Configurações e
// ao voltar à aba. 401: o token sai daqui, e o PC fica desconectado (e
// continua ditando). Sem rede: continua conectado, com o aviso.
func (s *Servico) Conferir(ctx context.Context) (Estado, error) {
	e, err := s.Estado(ctx)
	if err != nil || e.Estado != Conectado {
		return e, err
	}
	agora := s.cfg.Agora()
	s.mu.Lock()
	if desde := agora.Sub(s.conferidoEm); !s.conferidoEm.IsZero() && desde >= 0 && desde < IntervaloDaConferencia {
		s.mu.Unlock()
		return e, nil
	}
	s.conferidoEm = agora
	s.mu.Unlock()

	token, err := s.cfg.Cofre.Ler(s.cfg.NomeNoCofre)
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCofre, causa: err}
	}
	status, err := s.pedir(ctx, http.MethodGet, e.URL+"/api/v1/me", token, nil, nil)
	switch {
	case err == nil && status == http.StatusUnauthorized:
		s.log.Info("nuvem: a nuvem recusou o token (revogado ou vencido); desconectado neste PC")
		if err := s.desconectarPorRecusa(ctx); err != nil {
			return Estado{}, err
		}
		return s.Estado(ctx)
	case err == nil && status == http.StatusOK:
		s.mu.Lock()
		if s.aviso == AvisoSemRede {
			s.aviso = ""
		}
		s.mu.Unlock()
	default:
		s.log.Info("nuvem: a conferência não alcançou a nuvem; o PC segue conectado", "status", status, "erro_de_rede", err != nil)
		s.mu.Lock()
		s.aviso = AvisoSemRede
		s.mu.Unlock()
	}
	return s.Estado(ctx)
}

// desconectarPorRecusa é o que um 401 da nuvem faz (o do Conferir e o do
// sync): o token sai do Cofre, o aparelho sai da instalação, e o painel diz
// "Este PC foi desconectado na nuvem." O ditado segue local.
func (s *Servico) desconectarPorRecusa(ctx context.Context) error {
	if err := s.cfg.Cofre.Apagar(s.cfg.NomeNoCofre); err != nil && !errors.Is(err, chaves.ErrAusente) {
		s.log.Warn("nuvem: não consegui apagar o token recusado do Gerenciador de Credenciais", "err", err)
	}
	if err := s.esquecerAparelho(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.aviso = AvisoRevogado
	s.mu.Unlock()
	return nil
}

// ─── O sync (F7b) ───

// Credencial é o que o laço de sync precisa da conexão, lida na hora: a
// nuvem, o token (do Cofre, atrás do portão) e o pareamento (o aparelho e
// quando conectou, que é o que separa um cursor de outro).
type Credencial struct {
	// Configurada: a instalação já foi ligada a uma nuvem (a URL existe).
	Configurada bool
	// Conectado: o aparelho está na instalação e o token no Cofre.
	Conectado bool
	URL       string
	Token     string
	Aparelho  string
	PareadoEm string
}

// Credencial lê a conexão para o sync. Desconectado (ou com um pedido em
// curso), só Configurada pode ser verdadeira: o token não é lido.
func (s *Servico) Credencial(ctx context.Context) (Credencial, error) {
	base, _, err := s.cfg.Instalacao.Ler(ctx, chaveURL)
	if err != nil {
		return Credencial{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	c := Credencial{Configurada: base != "", URL: base}
	aparelho, ok, err := s.cfg.Instalacao.Ler(ctx, chaveAparelho)
	if err != nil {
		return Credencial{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	if !ok || !pareceUUID(aparelho) || base == "" {
		return c, nil
	}
	conectadoEm, _, err := s.cfg.Instalacao.Ler(ctx, chaveConectadoEm)
	if err != nil {
		return Credencial{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	token, err := s.cfg.Cofre.Ler(s.cfg.NomeNoCofre)
	switch {
	case errors.Is(err, chaves.ErrAusente):
		// Sem o token (o desinstalador, ou à mão): desconectado, como no
		// Estado.
		if _, err := s.conectado(ctx); err != nil {
			return Credencial{}, err
		}
		return c, nil
	case err != nil:
		return Credencial{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCofre, causa: err}
	}
	c.Conectado, c.Token, c.Aparelho, c.PareadoEm = true, token, strings.ToLower(aparelho), conectadoEm
	return c, nil
}

// TokenRecusado é o 401 do sync: se token ainda é o guardado, o PC fica
// desconectado como no Conferir. Um token que já não é o guardado (o PC
// reconectou enquanto o pedido estava na rede) não muda nada: o 401 era do
// pareamento anterior.
func (s *Servico) TokenRecusado(ctx context.Context, token string) error {
	atual, err := s.cfg.Cofre.Ler(s.cfg.NomeNoCofre)
	switch {
	case errors.Is(err, chaves.ErrAusente):
		return nil
	case err != nil:
		return &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCofre, causa: err}
	case atual != token:
		return nil
	}
	s.log.Info("nuvem: o sync recebeu 401 (revogado ou vencido); desconectado neste PC")
	return s.desconectarPorRecusa(ctx)
}

// UsarFreio liga o aviso do freio do aprendido (no Agent, o
// syncengine.Laco.Freio): o historico do Estado, conectado, traz o
// freio_trocas quando ele é maior que 0. Só lê memória, e não pode bloquear.
func (s *Servico) UsarFreio(f func() int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.freio = f
}

// AoConectar liga o aviso de que o PC acabou de conectar, de que o histórico
// entrou na fila ("Enviar o histórico") ou de que a nuvem foi esquecida (no
// Agent, o syncengine.Laco.Avisar: a rodada não espera o relógio, e a
// bandeja se reconta). Não pode bloquear.
func (s *Servico) AoConectar(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aoConectar = f
}

// ─── Desconectar ───

// Desconectar cancela o pedido em curso, ou desconecta o PC: tenta revogar o
// próprio aparelho na nuvem (DELETE /api/v1/devices/{id}, PrazoDoDesconectar)
// e SEMPRE apaga o token daqui e o aparelho da instalação. Sem a confirmação
// da nuvem, o aviso diz para revogar em Aparelhos.
func (s *Servico) Desconectar(ctx context.Context) (Estado, error) {
	s.mu.Lock()
	if s.pedido != nil {
		s.pedido = nil
		s.aviso = ""
		s.mu.Unlock()
		s.log.Info("nuvem: pedido de pareamento cancelado")
		return s.Estado(ctx)
	}
	s.mu.Unlock()

	conectado, err := s.conectado(ctx)
	if err != nil {
		return Estado{}, err
	}
	if !conectado {
		s.mu.Lock()
		s.aviso = ""
		s.mu.Unlock()
		return s.Estado(ctx)
	}
	base, _, err := s.cfg.Instalacao.Ler(ctx, chaveURL)
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	aparelho, _, err := s.cfg.Instalacao.Ler(ctx, chaveAparelho)
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgInstalacao, causa: err}
	}
	token, err := s.cfg.Cofre.Ler(s.cfg.NomeNoCofre)
	if err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCofre, causa: err}
	}
	revogou := s.revogar(ctx, base, aparelho, token)
	if err := s.cfg.Cofre.Apagar(s.cfg.NomeNoCofre); err != nil {
		return Estado{}, &Erro{Status: http.StatusInternalServerError, Mensagem: MsgCofre, causa: err}
	}
	if err := s.esquecerAparelho(ctx); err != nil {
		return Estado{}, err
	}
	s.mu.Lock()
	s.aviso = ""
	if !revogou {
		s.aviso = AvisoSemResposta
	}
	s.conferidoEm = time.Time{}
	s.mu.Unlock()
	s.log.Info("nuvem: desconectado neste PC", "revogado_na_nuvem", revogou)
	return s.Estado(ctx)
}

// revogar pede à nuvem que revogue o aparelho com o token dele (o próprio).
// Já revogado (401) ou já apagado (404) contam como revogado.
func (s *Servico) revogar(ctx context.Context, base, aparelho, token string) bool {
	if !pareceUUID(aparelho) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, PrazoDoDesconectar)
	defer cancel()
	status, err := s.pedir(ctx, http.MethodDelete, base+"/api/v1/devices/"+aparelho, token, nil, nil)
	return err == nil && (status == http.StatusNoContent || status == http.StatusUnauthorized || status == http.StatusNotFound)
}

// ─── A rede ───

// pedir faz um pedido à nuvem: JSON no corpo (se houver), o Bearer (se
// houver), e a resposta lida até maxRespostaDaNuvem. Um 200 com JSON que não
// é o esperado é erro. Nada daqui vai para o log.
func (s *Servico) pedir(ctx context.Context, method, endereco, token string, corpo, saida any) (int, error) {
	var body io.Reader
	if corpo != nil {
		raw, err := jsonMarshal(corpo)
		if err != nil {
			return 0, err
		}
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, endereco, body)
	if err != nil {
		return 0, err
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		return 0, errors.New("nuvem: a nuvem não respondeu")
	}
	defer resp.Body.Close()
	lido := io.LimitReader(resp.Body, maxRespostaDaNuvem)
	if saida != nil && resp.StatusCode == http.StatusOK {
		if err := jsonDecode(lido, saida); err != nil {
			return resp.StatusCode, errors.New("nuvem: resposta inválida")
		}
	}
	_, _ = io.Copy(io.Discard, lido)
	return resp.StatusCode, nil
}

// ─── Segredos e formatos ───

// novoSegredo: 256 bits em base64url sem preenchimento (43 caracteres), o
// verificador do PKCE.
func novoSegredo() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// desafioS256 é o code_challenge (RFC 7636, 4.2).
func desafioS256(verificador string) string {
	h := sha256.Sum256([]byte(verificador))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// pareceSegredo: 43 caracteres de base64url (o code e o token).
func pareceSegredo(s string) bool {
	if len(s) != 43 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// pareceUUID: 8-4-4-4-12 em hexadecimal.
func pareceUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// alfabetoDaConferencia: o mesmo do servidor (cloudauth), sem 0, 1, I, L e O.
const alfabetoDaConferencia = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

func pareceConferencia(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(alfabetoDaConferencia, rune(s[i])) {
			return false
		}
	}
	return true
}
