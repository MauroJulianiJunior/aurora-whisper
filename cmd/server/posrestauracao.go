package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgstore"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// O comando "pos-restauracao" (F11d; ADR-019, "Decisões da F11"): o passo de
// depois de restaurar um backup da nuvem, com a API parada.
//
//	aurora-whisper-server pos-restauracao -confirmo
//
// Numa transação só, troca o sync_epoch de TODAS as contas (cada PC recomeça
// pelo epoch novo) e revoga todas as sessões e todos os aparelhos (o backup
// guarda o revoked_at antigo: sem isso, um aparelho revogado depois do backup
// voltaria a valer). Todo PC conecta de novo à mão, e o celular entra de novo.
// O que foi apagado depois do backup pode voltar (a decisão do Mauro de
// 09-out-2026, "aceitar e deixar escrito").
//
// Sem -confirmo, sai com 2 e não toca em nada (nem conecta). Recusa um banco
// com migration pendente (rode o migrate antes). Lê a DATABASE_URL como o
// migrate (a variável ou o _FILE). Imprime só contagens: nunca um id, um
// e-mail, um nome nem a URL. Códigos: 0 feito, 1 falhou, 2 uso errado.

// posRestauracaoTimeout limita o comando inteiro.
const posRestauracaoTimeout = 5 * time.Minute

const avisoDaPosRestauracao = `pos-restauracao: nada foi feito.
Este comando é para DEPOIS de restaurar um backup da nuvem, com a API parada.
Ele troca o epoch de todas as contas (cada PC recomeça a sincronização) e
revoga todas as sessões e todos os aparelhos: todo PC precisa conectar de novo,
e o celular, entrar de novo. O que foi apagado depois do backup pode voltar.
Para fazer: aurora-whisper-server pos-restauracao -confirmo
`

func runPosRestauracao(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("pos-restauracao", flag.ContinueOnError)
	flags.SetOutput(stderr)
	confirmo := flags.Bool("confirmo", false, "faz de verdade (sem ela, nada é feito)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "pos-restauracao: argumento inesperado %q (a opção é só -confirmo)\n", flags.Arg(0))
		return 2
	}
	if !*confirmo {
		fmt.Fprint(stderr, avisoDaPosRestauracao)
		return 2
	}
	dbURL, err := lerDatabaseURL(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "pos-restauracao: "+err.Error())
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintln(stderr, redact("pos-restauracao: "+fmt.Sprintf(format, a...), dbURL))
		return 1
	}
	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		fmt.Fprintf(stderr, "pos-restauracao: %s não é uma URL de conexão válida\n", envDatabaseURL)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, posRestauracaoTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fail("não consegui abrir o pool do Postgres: %v", err)
	}
	defer pool.Close()

	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		return fail("%v", err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fail("não consegui conectar ao Postgres: %v", err)
	}
	state, err := pgmigrate.Inspect(ctx, conn.Conn(), migs)
	conn.Release()
	if err != nil {
		return fail("não consegui conferir as migrations: %v", err)
	}
	if len(state.Pending) > 0 {
		return fail("o banco tem %d migrations pendentes: rode \"migrate\" antes (nada foi feito)", len(state.Pending))
	}

	r, err := pgstore.New(pool).DepoisDeRestaurar(ctx, time.Now())
	if err != nil {
		return fail("não deu (nada foi feito): %v", err)
	}
	fmt.Fprintf(stdout, "pos-restauracao: %d conta(s) com o epoch novo, %d sessão(ões) derrubada(s), %d aparelho(s) revogado(s)\n",
		r.Contas, r.Sessoes, r.Aparelhos)
	fmt.Fprintln(stdout, "pos-restauracao: todo PC precisa conectar de novo, e o celular, entrar de novo")
	return 0
}
