package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O servidor da nuvem da etapa 5 (ADR-019, fatia F3): o histórico e o sync
// sobre um cloudstore.Store (F2), o login Google, as sessões e o token do
// aparelho sobre um cloudstore.Accounts, os cabeçalhos de segurança, o CSRF, o
// limite de tentativas e o log de acesso. O mesmo código serve o Store em
// memória e o do Postgres.
//
// O Server antigo (server.go, sem a nuvem) continua para os testes do cliente
// de sync antigo (internal/sync, internal/server/integration), até a F7a
// refazê-lo.

// Prazos e limites da F3 (ADR-019, "Decisões da F3"; decisões 1 e 2 do plano,
// com a recomendação como padrão).
const (
	// SessaoSemUso: a sessão vence depois de 30 dias sem uso.
	SessaoSemUso = 30 * 24 * time.Hour
	// SessaoTeto: e nunca passa de 90 dias desde o login.
	SessaoTeto = 90 * 24 * time.Hour
	// PrazoDoLogin: o login pendente (o state, o __Host-pre) vale 5 minutos.
	PrazoDoLogin = 5 * time.Minute
	// IntervaloDaArrumacao: de quanto em quanto tempo a rotina apaga os
	// logins pendentes e as sessões vencidas, e roda a retenção (F9; ADR-019,
	// "Decisões da F9", escolha menor 2: de hora em hora, no lugar do
	// "diário" da spec, e na subida).
	IntervaloDaArrumacao = time.Hour
)

// Limites são os limites de tentativas, por minuto. Zero em um campo é o
// padrão (LimitesPadrao).
type Limites struct {
	// LoginPorIP: o POST /auth/start e o callback, juntos, por IP.
	LoginPorIP int
	// InicioTotal: o POST /auth/start de todos os IPs juntos (ele grava no
	// banco sem sessão nenhuma).
	InicioTotal int
	// PushPedidos e PushMutacoes: o push, por aparelho.
	PushPedidos  int
	PushMutacoes int
	// PareamentoPorIP: o POST /device-auth/start e o /exchange, juntos, por
	// IP (F6).
	PareamentoPorIP int
	// PareamentoTotal: o POST /device-auth/start de todos os IPs juntos (ele
	// grava no banco sem sessão nenhuma).
	PareamentoTotal int
}

// LimitesPadrao: 10 tentativas de login por minuto por IP e 100 inícios no
// total; no push, 60 pedidos e 5.000 mutações por minuto por aparelho (o
// envio inicial de 10 mil ditados leva uns 2 minutos); no pareamento (F6),
// 10 por minuto por IP e 100 inícios no total.
var LimitesPadrao = Limites{
	LoginPorIP: 10, InicioTotal: 100, PushPedidos: 60, PushMutacoes: 5000,
	PareamentoPorIP: 10, PareamentoTotal: 100,
}

// CloudConfig é tudo o que o servidor da nuvem precisa. Quem lê o ambiente e
// o arquivo do Google é o cmd/server (config.go); aqui chegam os valores.
type CloudConfig struct {
	Store    cloudstore.Store
	Accounts cloudstore.Accounts

	// BaseURL é PUBLIC_BASE_URL: https, ou http só para localhost e
	// 127.0.0.1. O Host dos pedidos tem de ser o dela, e o HSTS só sai com
	// https.
	BaseURL string
	// SessionSecret e DeviceTokenPepper são as chaves do HMAC do id da
	// sessão e do token do aparelho: pelo menos 32 bytes, e diferentes.
	SessionSecret     []byte
	DeviceTokenPepper []byte
	// Contas é CONTAS_PERMITIDAS. Vale no login e em TODA checagem de
	// credencial: tirar um e-mail da lista e reiniciar corta as sessões e os
	// PCs dele.
	Contas cloudauth.Contas
	// Google é o client OAuth. O RedirectURL tem de ser BaseURL +
	// /auth/google/callback. Obrigatório, menos no binário com o login de
	// teste (build tag logindeteste), que sobe sem ele no servidor de teste
	// do PC (cmd/server teste); sem ele, /auth/start e o callback são 404.
	Google *cloudauth.Google
	// Painel é o build da nuvem do painel (PAINEL_DIR: web/dist-nuvem), lido
	// uma vez, na subida (painel.go). Obrigatório: o index.html tem de estar
	// nele.
	Painel fs.FS
	// Proxy é PROXY_CONFIAVEL.
	Proxy cloudauth.ProxyConfiavel

	// Now é o relógio (nil é time.Now).
	Now func() time.Time
	// Logger recebe o log de acesso e o login recusado (nil é slog.Default).
	Logger *slog.Logger
	// ReadyCheckers rodam no GET /api/v1/health/ready (o Ping do Postgres).
	ReadyCheckers []ReadyChecker
	// Limites; zero é LimitesPadrao.
	Limites Limites
	// IntervaloDaArrumacao é de quanto em quanto tempo o ArrumarSempre roda
	// a arrumação e a retenção depois da rodada da subida; zero é a constante
	// IntervaloDaArrumacao (1 h). Só os testes o mudam, para provar o tique.
	IntervaloDaArrumacao time.Duration
}

// Cloud é o servidor da nuvem montado.
type Cloud struct {
	cfg      CloudConfig
	now      func() time.Time
	log      *slog.Logger
	host     string
	https    bool
	politica cloudstore.PoliticaDeSessao
	csrf     *http.CrossOriginProtection
	rotas    []rota
	painel   *painel

	limLogin           *cloudauth.Limite
	limInicio          *cloudauth.Limite
	limPush            *cloudauth.Limite
	limMutacoes        *cloudauth.Limite
	limPareamento      *cloudauth.Limite
	limPareamentoTotal *cloudauth.Limite
	callbackPath       string
	intervalo          time.Duration

	// saude é a última rodada do Reter, para a rota da saúde do job (F11c;
	// saude.go).
	saude saudeDoJob
	// xffAvisado: o WARN do X-Forwarded-For de quem não é o proxy confiável
	// já saiu (uma vez por processo; saude.go).
	xffAvisado atomic.Bool
}

// minSegredo é o tamanho mínimo de SESSION_SECRET e DEVICE_TOKEN_PEPPER.
const minSegredo = 32

// NewCloud confere a configuração e monta o servidor. Os erros dizem o que
// falta, nunca um valor.
func NewCloud(cfg CloudConfig) (*Cloud, error) {
	if cfg.Store == nil || cfg.Accounts == nil {
		return nil, errors.New("server: a nuvem precisa do Store e do Accounts")
	}
	base, err := ParseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if LoginDeTesteCompilado && !hostLocal(base.Hostname()) {
		return nil, errors.New("server: o login de teste (build tag logindeteste) só sobe com PUBLIC_BASE_URL em localhost")
	}
	if len(cfg.SessionSecret) < minSegredo || len(cfg.DeviceTokenPepper) < minSegredo {
		return nil, errors.New("server: SESSION_SECRET e DEVICE_TOKEN_PEPPER precisam de pelo menos 32 bytes")
	}
	if string(cfg.SessionSecret) == string(cfg.DeviceTokenPepper) {
		return nil, errors.New("server: SESSION_SECRET e DEVICE_TOKEN_PEPPER precisam ser diferentes")
	}
	if cfg.Contas.Len() == 0 {
		return nil, errors.New("server: CONTAS_PERMITIDAS está vazia")
	}
	switch {
	case cfg.Google == nil && LoginDeTesteCompilado:
		// O servidor de teste do PC: só o login de teste.
	case cfg.Google == nil || cfg.Google.ClientID == "" || cfg.Google.ClientSecret == "":
		return nil, errors.New("server: falta o client do Google")
	case cfg.Google.RedirectURL != RedirectURL(base):
		return nil, errors.New("server: o redirect do Google não é PUBLIC_BASE_URL + /auth/google/callback")
	}
	app, err := carregarPainel(cfg.Painel)
	if err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.IntervaloDaArrumacao <= 0 {
		cfg.IntervaloDaArrumacao = IntervaloDaArrumacao
	}
	lim := cfg.Limites
	def := LimitesPadrao
	for _, p := range []struct {
		v *int
		d int
	}{
		{&lim.LoginPorIP, def.LoginPorIP}, {&lim.InicioTotal, def.InicioTotal},
		{&lim.PushPedidos, def.PushPedidos}, {&lim.PushMutacoes, def.PushMutacoes},
		{&lim.PareamentoPorIP, def.PareamentoPorIP}, {&lim.PareamentoTotal, def.PareamentoTotal},
	} {
		if *p.v <= 0 {
			*p.v = p.d
		}
	}
	c := &Cloud{
		cfg:          cfg,
		now:          cfg.Now,
		log:          cfg.Logger,
		host:         normalHost(base.Host, base.Scheme),
		https:        base.Scheme == "https",
		politica:     cloudstore.PoliticaDeSessao{SemUso: SessaoSemUso, Teto: SessaoTeto},
		limLogin:     cloudauth.NovoLimite(lim.LoginPorIP, cfg.Now),
		limInicio:    cloudauth.NovoLimite(lim.InicioTotal, cfg.Now),
		limPush:      cloudauth.NovoLimite(lim.PushPedidos, cfg.Now),
		limMutacoes:  cloudauth.NovoLimite(lim.PushMutacoes, cfg.Now),
		callbackPath: callbackPath,
		painel:       app,
		intervalo:    cfg.IntervaloDaArrumacao,

		limPareamento:      cloudauth.NovoLimite(lim.PareamentoPorIP, cfg.Now),
		limPareamentoTotal: cloudauth.NovoLimite(lim.PareamentoTotal, cfg.Now),
	}
	c.csrf = http.NewCrossOriginProtection()
	c.csrf.SetDenyHandler(http.HandlerFunc(c.negarOrigemCruzada))
	c.rotas = c.tabela()
	return c, nil
}

// callbackPath é o caminho do retorno do Google.
const callbackPath = "/auth/google/callback"

// RedirectURL é o redirect do client do Google para uma PUBLIC_BASE_URL.
func RedirectURL(base *url.URL) string {
	return base.Scheme + "://" + base.Host + callbackPath
}

// ParseBaseURL confere PUBLIC_BASE_URL: https, ou http só para localhost e
// 127.0.0.1 (o desenvolvimento no PC); sem usuário, caminho, query nem
// fragmento. O erro nunca traz o valor.
func ParseBaseURL(s string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSuffix(s, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("server: PUBLIC_BASE_URL não é uma origem (esperado https://servidor ou http://localhost:porta)")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !hostLocal(u.Hostname()) {
			return nil, errors.New("server: PUBLIC_BASE_URL em http só vale para localhost e 127.0.0.1")
		}
	default:
		return nil, errors.New("server: PUBLIC_BASE_URL tem de ser https (ou http em localhost)")
	}
	return u, nil
}

func hostLocal(h string) bool {
	return strings.EqualFold(h, "localhost") || h == "127.0.0.1"
}

// HostLoopback diz se o host de um endereço (o de HTTP_ADDR, sem a porta) é
// a própria máquina: localhost ou um IP de loopback (127.0.0.0/8 e ::1). O
// vazio e o 0.0.0.0 (todas as interfaces) não são.
func HostLoopback(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && a.Unmap().IsLoopback()
}

// normalHost tira a porta padrão do esquema e põe em minúsculas, para o Host
// dos pedidos ser comparado com o da PUBLIC_BASE_URL.
func normalHost(host, scheme string) string {
	host = strings.ToLower(host)
	switch {
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		return strings.TrimSuffix(host, ":443")
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		return strings.TrimSuffix(host, ":80")
	}
	return host
}

// Handler é o servidor inteiro: o aviso do X-Forwarded-For por fora (F11c),
// depois o log de acesso, a guarda de pânico, os cabeçalhos, a conferência do
// Host, a proteção contra CSRF e, por dentro, o roteador montado a partir da
// tabela de rotas.
func (c *Cloud) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range c.rotas {
		mux.Handle(rt.padrao, c.proteger(rt))
	}
	var h http.Handler = mux
	h = c.csrf.Handler(h)
	h = c.conferirHost(h)
	h = c.cabecalhos(h)
	h = c.recuperar(h)
	h = c.registrarAcesso(h)
	h = c.avisarXFF(h)
	return h
}

// Arrumar apaga os logins pendentes, as sessões e os pedidos de pareamento
// vencidos agora, e registra só as contagens.
func (c *Cloud) Arrumar(ctx context.Context) {
	a, err := c.cfg.Accounts.Arrumar(ctx, c.now())
	if err != nil {
		c.log.Error("arrumação falhou", "erro", err.Error())
		return
	}
	c.log.Info("arrumação", "logins_pendentes", a.LoginsPendentes, "sessoes", a.Sessoes, "pedidos", a.Pedidos)
}

// ArrumarSempre roda Arrumar e Reter agora e a cada IntervaloDaArrumacao (o
// da CloudConfig), até ctx acabar. Quem a chama num goroutine espera ela
// voltar antes de fechar o Store: a retenção escreve. É a do servidor de
// teste do PC; o serve roda a primeira rodada antes de abrir a porta e depois
// só ArrumarDeHoraEmHora (F11c).
func (c *Cloud) ArrumarSempre(ctx context.Context) {
	c.Arrumar(ctx)
	c.Reter(ctx)
	c.ArrumarDeHoraEmHora(ctx)
}

// ArrumarDeHoraEmHora roda Arrumar e Reter a cada IntervaloDaArrumacao, até
// ctx acabar, SEM a rodada de agora: o serve já a rodou antes de abrir a
// porta (F11c; ADR-019, "Decisões da F11": a ordem "restaurar, reter, abrir"
// sem depender do proxy).
func (c *Cloud) ArrumarDeHoraEmHora(ctx context.Context) {
	t := time.NewTicker(c.intervalo)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Arrumar(ctx)
			c.Reter(ctx)
		}
	}
}

// RodadaDaRetencao é o resumo de uma rodada do Reter: os usuários
// percorridos, as falhas, e quantas delas são graves (de categoria banco ou
// outro: o job roda e não apaga, o texto vencido fica além do prazo). A de
// categoria contexto (o desligamento no meio da rodada) não é grave: a rodada
// da subida seguinte refaz o trabalho (11-observability.md, o alerta duplo).
type RodadaDaRetencao struct {
	Usuarios     int
	Falhas       int
	FalhasGraves int
}

// Reter roda a retenção de cada usuário com contador (ADR-019, "Decisões da
// F9"; cloudstore.Store.Reter), com o relógio da nuvem, e registra UMA linha
// INFO "retenção" por rodada, só com contagens e mesmo com zero: é o
// batimento do alerta "retention job missed" (F11). Um usuário que falha não
// para os outros: cada falha sai numa linha "retenção falhou", só com a
// categoria (contexto, outro, ou a operação e o SQLSTATE do banco), nunca o
// id, o texto, o e-mail nem o erro cru do driver, e conta em falhas. Com o
// ctx cancelado (o desligamento), para entre um usuário e outro.
//
// O resumo volta para quem chamou (o serve decide se abre a porta) e fica
// guardado para a rota da saúde do job (SaudeDaRetencao, F11c).
func (c *Cloud) Reter(ctx context.Context) RodadaDaRetencao {
	inicio := time.Now()
	now := c.now()
	var total cloudstore.Retido
	var rodada RodadaDaRetencao
	falhou := func(etapa string, err error) {
		grave, attrs := categoriaDoErro(ctx, err)
		rodada.Falhas++
		if grave {
			rodada.FalhasGraves++
		}
		c.log.Error("retenção falhou", append([]any{"etapa", etapa}, attrs...)...)
	}
	users, err := c.cfg.Store.UsuariosDaRetencao(ctx)
	if err != nil {
		falhou("usuarios", err)
	}
	for _, u := range users {
		if ctx.Err() != nil {
			break
		}
		rodada.Usuarios++
		r, err := c.cfg.Store.Reter(ctx, u, now)
		total.Somar(r)
		if err != nil {
			falhou("usuario", err)
		}
	}
	c.log.Info("retenção", "usuarios", rodada.Usuarios, "ativos", total.Ativos, "lixeira", total.Lixeira,
		"lapides_criadas", total.LapidesCriadas, "lapides_vencidas", total.LapidesVencidas,
		"eventos", total.Eventos, "mutacoes", total.Mutacoes, "falhas", rodada.Falhas,
		"ms", time.Since(inicio).Milliseconds())
	c.saude.anotar(c.now(), rodada)
	return rodada
}

// erroDoBanco é o que o erro do Store do Postgres (pgstore.DBError) diz de
// si sem valor nenhum da linha: a operação e o SQLSTATE.
type erroDoBanco interface {
	Operacao() string
	SQLState() string
}

// categoriaDoErro é o que o log da retenção pode dizer de um erro: contexto
// (o desligamento ou o prazo), banco (com a operação e o SQLSTATE) ou outro.
// Nunca o texto do erro: o do driver pode trazer valores, e o de rede, o
// endereço do banco. grave é banco ou outro (RodadaDaRetencao).
func categoriaDoErro(ctx context.Context, err error) (grave bool, attrs []any) {
	var db erroDoBanco
	switch {
	case ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return false, []any{"categoria", "contexto"}
	case errors.As(err, &db):
		return true, []any{"categoria", "banco", "operacao", db.Operacao(), "sqlstate", db.SQLState()}
	}
	return true, []any{"categoria", "outro"}
}
