package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// envDatabaseURL é a URL do Postgres (10-env-and-secrets.md). O migrate a lê
// SÓ do ambiente, ou do arquivo de DATABASE_URL_FILE (F11c): uma opção de
// linha de comando levaria a senha para o histórico do shell e para a lista
// de processos.
//
// Em produção, o migrate pode rodar com um usuário do banco que tem direito
// de criar tabelas, e o servidor com outro, que só lê e escreve: a variável é
// a mesma, definida por invocação.
const envDatabaseURL = "DATABASE_URL"

// lerDatabaseURL lê a URL do Postgres de DATABASE_URL ou do arquivo de
// DATABASE_URL_FILE (lerSegredo): a mesma leitura no serve, no migrate e no
// pos-restauracao. Sem nenhuma das duas, é erro de configuração.
func lerDatabaseURL(getenv func(string) string) (string, error) {
	v, err := lerSegredo(getenv, envDatabaseURL)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errConfig(envDatabaseURL, "não está definida, nem "+envDatabaseURL+sufixoArquivo+
			" (a URL do Postgres, postgres://usuario:senha@servidor:5432/banco)")
	}
	return v, nil
}

// migrateTimeout limita o comando inteiro, inclusive a espera pela trava de
// outro migrate em andamento.
const migrateTimeout = 10 * time.Minute

// connect abre a conexão do comando. É uma variável só para o teste trocá-la
// por uma que falha com a URL (e a senha) dentro da mensagem, e provar que a
// saída do comando não as repete.
var connect = pgx.Connect

// runMigrate é o comando "migrate": aplica as migrations pendentes (as
// embutidas em migrations.FS) no banco de DATABASE_URL, ou, com -status, só
// mostra o que está aplicado e o que falta.
//
// Imprime só nomes de arquivo e contagens; nunca a URL, a senha nem o corpo do
// SQL. Códigos de saída: 0 deu certo, 1 falhou, 2 uso errado ou sem DATABASE_URL.
func runMigrate(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	status := flags.Bool("status", false, "só mostra o que está aplicado e o que falta, sem aplicar nada")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "migrate: argumento inesperado %q (as opções são só -status)\n", flags.Arg(0))
		return 2
	}

	dbURL, err := lerDatabaseURL(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "migrate: "+err.Error())
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintln(stderr, redact("migrate: "+fmt.Sprintf(format, a...), dbURL))
		return 1
	}

	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		return fail("%v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	defer cancel()

	conn, err := connect(ctx, dbURL)
	if err != nil {
		return fail("não consegui conectar ao Postgres: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	}()

	if *status {
		state, err := pgmigrate.Inspect(ctx, conn, migs)
		if err != nil {
			return fail("%v", err)
		}
		printStatus(stdout, state)
		return 0
	}

	result, err := pgmigrate.Run(ctx, conn, migs)
	for _, m := range result.Applied {
		fmt.Fprintf(stdout, "migrate: aplicada %s\n", m.File)
	}
	if err != nil {
		return fail("%v", err)
	}
	if len(result.Applied) == 0 {
		fmt.Fprintf(stdout, "migrate: nada a aplicar (%d já estavam aplicadas)\n", result.AlreadyApplied)
		return 0
	}
	fmt.Fprintf(stdout, "migrate: %d aplicadas agora, %d já estavam aplicadas\n", len(result.Applied), result.AlreadyApplied)
	return 0
}

// printStatus escreve uma linha por migration, na ordem, e o total.
func printStatus(w io.Writer, state pgmigrate.State) {
	for _, m := range state.Applied {
		fmt.Fprintf(w, "aplicada  %s\n", m.File)
	}
	for _, m := range state.Pending {
		fmt.Fprintf(w, "pendente  %s\n", m.File)
	}
	fmt.Fprintf(w, "migrate: %d aplicadas, %d pendentes\n", len(state.Applied), len(state.Pending))
}

// redact tira de uma mensagem de erro a URL do banco e a senha dela. O pgx já
// não imprime a senha, mas uma mensagem de erro com a senha dentro seria
// grave demais para depender disso.
func redact(msg, dbURL string) string {
	if dbURL != "" {
		msg = strings.ReplaceAll(msg, dbURL, "<DATABASE_URL>")
	}
	if u, err := url.Parse(dbURL); err == nil && u.User != nil {
		// Senha com menos de 4 caracteres não é senha de verdade, e trocá-la
		// estragaria qualquer mensagem que tenha essas letras.
		if pw, ok := u.User.Password(); ok && len(pw) >= 4 {
			msg = strings.ReplaceAll(msg, pw, "***")
		}
	}
	return msg
}
