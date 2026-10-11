package pgmigrate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdvisoryLockKey é a chave do pg_advisory_lock que serializa dois "migrate"
// no mesmo banco: o segundo espera o primeiro terminar, vê tudo aplicado e
// não faz nada. Os bytes são "AWMIGR" mais 0x0001. Fica exportada para o teste
// provar que Run usa mesmo esta trava (segurando-a de fora), e para quem
// precisar ver quem a segura (pg_locks, tipo advisory).
const AdvisoryLockKey int64 = 0x41574D4947520001

// unlockTimeout limita o pg_advisory_unlock do fim, que roda mesmo depois de
// o contexto do chamador ter vencido.
const unlockTimeout = 5 * time.Second

const createSchemaMigrations = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    checksum   TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Result é o que um Run fez.
type Result struct {
	Applied        []Migration // as que este Run aplicou, em ordem
	AlreadyApplied int         // quantas já estavam aplicadas antes
}

// State é o que Inspect viu: o que já está aplicado e o que falta.
type State struct {
	Applied []Migration
	Pending []Migration
}

// Run aplica as migrations pendentes, em ordem, cada arquivo numa transação
// própria, e registra a versão, o nome e o checksum em schema_migrations na
// MESMA transação: ou o arquivo e o registro entram juntos, ou nenhum dos
// dois. Rodar de novo, sem arquivo novo, não faz nada.
//
// conn tem de ser uma conexão só, dedicada a este Run: a trava
// (pg_advisory_lock) é da sessão e vale enquanto ele roda. Quem a pega
// primeiro aplica; o outro espera e depois não encontra nada pendente. Se
// ctx vencer enquanto ele espera a trava, Run devolve o erro do contexto.
//
// Recusa, antes de aplicar qualquer coisa: banco que não é UTF8 (o texto
// ditado é português, e uma codificação sem acento o estragaria), e qualquer
// divergência que Plan acusa (migration alterada, ausente ou fora de ordem).
func Run(ctx context.Context, conn *pgx.Conn, migrations []Migration) (Result, error) {
	if err := requireUTF8(ctx, conn); err != nil {
		return Result{}, err
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", AdvisoryLockKey); err != nil {
		return Result{}, fmt.Errorf("pgmigrate: pegar a trava do migrate: %w", err)
	}
	defer func() {
		// A trava é da sessão: se esta chamada falhar (conexão caída), fechar
		// a conexão a solta do mesmo jeito.
		unlockCtx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", AdvisoryLockKey)
	}()

	if _, err := conn.Exec(ctx, createSchemaMigrations); err != nil {
		return Result{}, fmt.Errorf("pgmigrate: criar schema_migrations: %w", err)
	}
	applied, err := readApplied(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	pending, err := Plan(migrations, applied)
	if err != nil {
		return Result{}, err
	}

	result := Result{AlreadyApplied: len(applied)}
	for _, m := range pending {
		if err := apply(ctx, conn, m); err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, m)
	}
	return result, nil
}

// Inspect diz o que está aplicado e o que falta, sem escrever nada (não cria
// nem schema_migrations). Devolve o mesmo erro de Plan se o banco e os
// arquivos não combinam.
func Inspect(ctx context.Context, conn *pgx.Conn, migrations []Migration) (State, error) {
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return State{}, fmt.Errorf("pgmigrate: procurar schema_migrations: %w", err)
	}
	var applied []Applied
	if exists {
		var err error
		if applied, err = readApplied(ctx, conn); err != nil {
			return State{}, err
		}
	}
	pending, err := Plan(migrations, applied)
	if err != nil {
		return State{}, err
	}
	isPending := make(map[int]bool, len(pending))
	for _, m := range pending {
		isPending[m.Version] = true
	}
	state := State{Pending: pending}
	for _, m := range migrations {
		if !isPending[m.Version] {
			state.Applied = append(state.Applied, m)
		}
	}
	return state, nil
}

func requireUTF8(ctx context.Context, conn *pgx.Conn) error {
	var encoding string
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_encoding')").Scan(&encoding); err != nil {
		return fmt.Errorf("pgmigrate: ler a codificação do banco: %w", err)
	}
	if encoding != "UTF8" {
		return fmt.Errorf("pgmigrate: o banco usa a codificação %s; o Aurora Whisper exige UTF8 (texto em português)", encoding)
	}
	return nil
}

func readApplied(ctx context.Context, conn *pgx.Conn) ([]Applied, error) {
	rows, err := conn.Query(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("pgmigrate: ler schema_migrations: %w", err)
	}
	defer rows.Close()
	var applied []Applied
	for rows.Next() {
		var a Applied
		if err := rows.Scan(&a.Version, &a.Name, &a.Checksum); err != nil {
			return nil, fmt.Errorf("pgmigrate: ler uma linha de schema_migrations: %w", err)
		}
		applied = append(applied, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgmigrate: ler schema_migrations: %w", err)
	}
	return applied, nil
}

// apply roda um arquivo e registra a versão dele numa transação só. Exec sem
// argumentos usa o protocolo simples do Postgres, que aceita um texto com
// vários comandos; é por isso que o arquivo inteiro vai de uma vez.
func apply(ctx context.Context, conn *pgx.Conn, m Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgmigrate: %s: abrir a transação: %w", m.File, err)
	}
	// Depois do Commit, o Rollback não faz nada. Ele roda com um contexto
	// próprio: se ctx venceu no meio, o rollback ainda precisa acontecer.
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("pgmigrate: %s: %w", m.File, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
		m.Version, m.Name, m.Checksum); err != nil {
		return fmt.Errorf("pgmigrate: %s: registrar a versão: %w", m.File, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgmigrate: %s: confirmar a transação: %w", m.File, err)
	}
	return nil
}
