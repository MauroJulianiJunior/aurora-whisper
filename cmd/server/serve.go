package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgstore"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// O comando "serve" (ADR-019, F3): sobe o servidor da nuvem sobre o Postgres
// de DATABASE_URL, com a configuração de config.go. Não aplica migration
// nenhuma (decisão 13): com migration pendente, recusa subir e manda rodar o
// migrate. Roda a primeira rodada da arrumação e da retenção ANTES de abrir a
// porta (F11c; abrir, abaixo). Para no SIGINT (Ctrl+C) e no SIGTERM com um
// desligamento limpo de até 15 segundos.
//
// O log é slog em JSON no stderr: o de acesso (método, padrão da rota,
// status, ms, bytes e contagens), o login recusado (só o motivo), a
// arrumação, a retenção (só contagens; F9) e os erros do http.Server (só a
// categoria). Nunca a URL do banco, um segredo, um e-mail, um IP, um id nem
// texto ditado.

// Os prazos do http.Server.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 64 << 10
	shutdownTimeout   = 15 * time.Second
	startupTimeout    = 30 * time.Second
	readyTimeout      = 2 * time.Second
)

// onListen é chamado com o endereço em que o servidor escuta (os testes sobem
// em 127.0.0.1:0 e precisam saber a porta).
var onListen func(net.Addr)

func runServe(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "serve: argumento inesperado %q (a configuração vem do ambiente; veja docs/ai-spec/10-env-and-secrets.md)\n", args[0])
		return 2
	}
	cfg, err := loadConfig(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "serve: "+err.Error())
		return 2
	}
	fail := func(code int, format string, a ...any) int {
		fmt.Fprintln(stderr, redact("serve: "+fmt.Sprintf(format, a...), cfg.databaseURL))
		return code
	}
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: cfg.logLevel}))
	if server.LoginDeTesteCompilado {
		logger.Warn("este binário tem o login de teste (build tag logindeteste): só para desenvolvimento, nunca em produção")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.databaseURL)
	if err != nil {
		return fail(2, "%s não é uma URL de conexão válida", envDatabaseURL)
	}
	startCtx, cancelStart := context.WithTimeout(ctx, startupTimeout)
	defer cancelStart()
	pool, err := pgxpool.NewWithConfig(startCtx, poolCfg)
	if err != nil {
		return fail(1, "não consegui abrir o pool do Postgres: %v", err)
	}
	defer pool.Close()

	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		return fail(1, "%v", err)
	}
	conn, err := pool.Acquire(startCtx)
	if err != nil {
		return fail(1, "não consegui conectar ao Postgres: %v", err)
	}
	state, err := pgmigrate.Inspect(startCtx, conn.Conn(), migs)
	conn.Release()
	if err != nil {
		return fail(1, "não consegui conferir as migrations: %v", err)
	}
	if len(state.Pending) > 0 {
		return fail(1, "o banco tem %d migrations pendentes: rode \"migrate\" antes de subir o servidor", len(state.Pending))
	}

	store := pgstore.New(pool)
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: store, Accounts: store, BaseURL: cfg.base.String(),
		SessionSecret: cfg.sessionSecret, DeviceTokenPepper: cfg.pepper,
		Contas: cfg.contas, Google: cfg.google, Proxy: cfg.proxy, Logger: logger, Painel: cfg.painel,
		// O ditado no celular (ADR-021, C3a): sem a chave de cifra (cfg.cofre
		// nil), as rotas das chaves da IA não sobem. O aceite é da C3b: até
		// lá, nenhuma conta tem aceite, e o PUT da chave recusa (SEM_ACEITE).
		Cofre: cfg.cofre, ChavesDeIA: store, Modelos: server.ModelosPelaRede{},
		ReadyCheckers: []server.ReadyChecker{func() error {
			c, cancel := context.WithTimeout(context.Background(), readyTimeout)
			defer cancel()
			return store.Ping(c)
		}},
	})
	if err != nil {
		return fail(2, "%v", err)
	}
	return abrir(ctx, cfg.addr, cloud, logger, fail,
		"contas_permitidas", cfg.contas.Len(), "https", cfg.base.Scheme == "https",
		"proxy_confiavel", cfg.proxy.Ligado(), "chaves_da_ia", cfg.cofre != nil)
}

// abrir é o fim da subida do serve (F11c; ADR-019, "Decisões da F11"): a
// PRIMEIRA rodada de Arrumar e Reter, inteira e em ordem, com o ctx do
// processo (e não com o startupTimeout, que cobre só o pool e as migrations),
// e só DEPOIS o net.Listen. Depois de restaurar um backup, ou de dias parado,
// nenhum pedido é atendido antes de a retenção tirar o texto vencido: a ordem
// "restaurar, reter, abrir" vale sem depender do proxy, que pode já estar no
// ar (o Traefik que encaminha assim que o contêiner existe). A linha
// "retenção" sai antes da "servindo". O tique de hora em hora segue no
// servir, sem repetir a primeira rodada.
//
// Se a primeira rodada tem falha grave (de categoria banco ou outro), o serve
// sai com 1 sem abrir a porta (escolha menor 4 da F11), como já recusa subir
// com migration pendente; o restart do contêiner tenta de novo. Se o ctx
// acaba no meio (o SIGTERM), sai com 0 sem abrir.
//
// servindo são os pares a mais da linha "servindo".
func abrir(ctx context.Context, addr string, cloud *server.Cloud, logger *slog.Logger,
	fail func(code int, format string, a ...any) int, servindo ...any) int {
	cloud.Arrumar(ctx)
	rodada := cloud.Reter(ctx)
	if ctx.Err() != nil {
		logger.Info("desligado")
		return 0
	}
	if rodada.FalhasGraves > 0 {
		return fail(1, "a primeira rodada da retenção teve %d falha(s) de banco ou outra (veja as linhas \"retenção falhou\"): "+
			"o servidor não abre a porta com texto vencido à mostra", rodada.FalhasGraves)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fail(1, "não consegui escutar em %s: %v", envHTTPAddr, err)
	}
	if onListen != nil {
		onListen(ln.Addr())
	}
	logger.Info("servindo", append([]any{"endereco", ln.Addr().String()}, servindo...)...)
	return servir(ctx, cloud, ln, logger, fail, cloud.ArrumarDeHoraEmHora)
}

// servir sobe o http.Server no ln, com os prazos, a arrumação e a retenção
// (arrumar: a de hora em hora do serve, que já fez a primeira rodada, ou a
// ArrumarSempre do servidor de teste, que a faz na subida), e o desligamento
// limpo quando o ctx acaba: o http.Server fecha, o goroutine da arrumação
// termina, e só então "desligado".
func servir(ctx context.Context, cloud *server.Cloud, ln net.Listener, logger *slog.Logger,
	fail func(code int, format string, a ...any) int, arrumar func(context.Context)) int {
	srv := &http.Server{
		Handler:           cloud.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          log.New(erroDoServidor{logger}, "", 0),
	}
	// A arrumação e a retenção (F9) rodam num goroutine e ESCREVEM no Store:
	// o servir só volta (e o pool só fecha, no serve) depois de ele terminar.
	runCtx, stop := context.WithCancel(ctx)
	arrumacao := make(chan struct{})
	go func() {
		defer close(arrumacao)
		arrumar(runCtx)
	}()
	esperarArrumacao := func() {
		stop()
		<-arrumacao
	}
	defer esperarArrumacao()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			return fail(1, "o servidor parou: %v", err)
		}
		return 0
	case <-ctx.Done():
	}
	logger.Info("desligando")
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fail(1, "o desligamento não terminou em %s: %v", shutdownTimeout, err)
	}
	esperarArrumacao()
	logger.Info("desligado")
	return 0
}

// erroDoServidor é o ErrorLog do http.Server: as mensagens dele trazem o
// endereço de quem pediu ("http: TLS handshake error from 1.2.3.4:5678"), e o
// log da nuvem nunca traz IP. Passa só a categoria.
type erroDoServidor struct{ log *slog.Logger }

func (e erroDoServidor) Write(p []byte) (int, error) {
	msg := string(p)
	categoria := "outro"
	switch {
	case strings.Contains(msg, "TLS handshake"):
		categoria = "tls"
	case strings.Contains(msg, "panic"):
		categoria = "panico"
	case strings.Contains(msg, "Accept error"):
		categoria = "accept"
	case strings.Contains(msg, "superfluous"):
		categoria = "resposta_duplicada"
	}
	e.log.Error("http.Server", "categoria", categoria)
	return len(p), nil
}
