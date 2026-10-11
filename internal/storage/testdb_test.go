package storage_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite" // driver puro Go, só para teste (ADR-008).
)

// openTestDB abre um SQLite em memória dedicado a um único teste.
//
// SetMaxOpenConns(1) é obrigatório aqui por dois motivos independentes:
//  1. "file::memory:" sem cache compartilhado cria um banco NOVO por
//     conexão — com mais de uma conexão, cada uma veria um banco vazio
//     diferente.
//  2. PRAGMA foreign_keys é ligado por conexão (Migrator.Migrate liga
//     essa pragma). Com pool > 1, uma conexão diferente da que rodou o
//     Migrate poderia não ter a pragma ligada.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatalf("abrir sqlite em memória: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
