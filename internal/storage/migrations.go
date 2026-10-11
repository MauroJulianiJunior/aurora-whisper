package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// migrationFS embute as migrations SQLite (internal/storage/migrations/*.sql)
// no binário do Agent, para que elas viajem junto com ele e rodem no
// startup sem depender de arquivos externos no disco do usuário (ver
// docs/ai-spec/05-data-model.md, "Integridade": "SQLite migrations
// transacionais no startup").
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// schemaMigrationsTable é a tabela de controle que registra, por versão,
// quais migrations já foram aplicadas neste banco.
const schemaMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL
);`

// migration é uma migration SQLite carregada do embed.FS, identificada
// pelo prefixo numérico do nome do arquivo (ex.: "0001_init_schema").
type migration struct {
	version string
	sql     string
}

// loadMigrations lê internal/storage/migrations/*.sql do binário e as
// devolve ordenadas pelo nome do arquivo (portanto pela numeração:
// 0001, 0002, ...).
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("storage: ler diretório de migrations: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	migrations := make([]migration, 0, len(names))
	for _, name := range names {
		content, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			return nil, fmt.Errorf("storage: ler migration %q: %w", name, err)
		}
		migrations = append(migrations, migration{
			version: strings.TrimSuffix(name, ".sql"),
			sql:     string(content),
		})
	}
	return migrations, nil
}

// Migrator aplica as migrations SQLite embutidas no binário.
type Migrator struct{}

// NewMigrator constrói um Migrator pronto para uso. Não há estado a
// configurar: as migrations vêm sempre do embed.FS deste pacote.
func NewMigrator() *Migrator {
	return &Migrator{}
}

// Migrate aplica, em ORDEM e de forma transacional, todas as migrations
// ainda não registradas na tabela de controle schema_migrations.
//
// É seguro chamar Migrate mais de uma vez no mesmo *sql.DB (idempotente):
// migrations já aplicadas são identificadas pela versão e puladas: nunca
// executadas de novo, nunca duplicadas.
//
// Liga PRAGMA foreign_keys = ON antes de aplicar qualquer migration —
// necessário para que local_corrections.transcription_id ON DELETE SET
// NULL funcione de verdade (ver docs/adr/ADR-008 e
// docs/ai-spec/05-data-model.md, seção SQLite). Em SQLite essa pragma é
// no-op dentro de uma transação, por isso é executada aqui, fora de
// qualquer BEGIN.
//
// db deve ser efetivamente uma única conexão lógica para todo o processo
// (ex.: db.SetMaxOpenConns(1) para bancos em memória) — do contrário
// PRAGMA foreign_keys pode não valer para a conexão do pool usada depois
// pelo chamador, já que a pragma é por conexão em SQLite.
func (m *Migrator) Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		return fmt.Errorf("storage: ligar PRAGMA foreign_keys: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaMigrationsTable); err != nil {
		return fmt.Errorf("storage: criar tabela schema_migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, mig := range migrations {
		applied, err := isApplied(ctx, db, mig.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, mig); err != nil {
			return fmt.Errorf("storage: aplicar migration %q: %w", mig.version, err)
		}
	}

	return nil
}

func isApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var count int
	row := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version)
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("storage: verificar migration aplicada %q: %w", version, err)
	}
	return count > 0, nil
}

func applyMigration(ctx context.Context, db *sql.DB, mig migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op após Commit bem-sucedido.

	if _, err := tx.ExecContext(ctx, mig.sql); err != nil {
		return fmt.Errorf("executar SQL: %w", err)
	}

	const insertVersion = `INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`
	if _, err := tx.ExecContext(ctx, insertVersion, mig.version); err != nil {
		return fmt.Errorf("registrar versão aplicada: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
