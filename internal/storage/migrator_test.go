package storage_test

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

func expectedTables() []string {
	return []string{
		"local_transcriptions",
		"local_dictionary_entries",
		"local_dictionary_aliases",
		"local_corrections",
		"local_settings",
		"sync_outbox",
		"sync_outbox_0001",
		"sync_state",
		"sync_mudou_aqui",
		"instalacao",
		"schema_migrations",
	}
}

func TestMigrator_Migrate_CreatesAllTables(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("consultar sqlite_master: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar linhas: %v", err)
	}

	for _, want := range expectedTables() {
		if !got[want] {
			t.Errorf("tabela %q não foi criada pela migration", want)
		}
	}
}

func TestMigrator_Migrate_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	m := storage.NewMigrator()
	if err := m.Migrate(ctx, db); err != nil {
		t.Fatalf("primeira Migrate: %v", err)
	}
	if err := m.Migrate(ctx, db); err != nil {
		t.Fatalf("segunda Migrate (deveria ser no-op): %v", err)
	}

	var count int
	row := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, "0001_init_schema")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("consultar schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava exatamente 1 registro de versão aplicada, achou %d (migration rodou mais de uma vez)", count)
	}

	// Se a migration tivesse rodado de novo, CREATE TABLE sem
	// IF NOT EXISTS já teria falhado a segunda chamada — o teste acima
	// só confirma que ela nem tentou.
}

func TestMigrator_Migrate_EnablesForeignKeys(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var fk int
	row := db.QueryRowContext(ctx, `PRAGMA foreign_keys`)
	if err := row.Scan(&fk); err != nil {
		t.Fatalf("consultar PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, esperava 1 (ligado) — sem isso ON DELETE SET NULL não funciona", fk)
	}
}
