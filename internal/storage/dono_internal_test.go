package storage

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // driver puro Go, só para teste (ADR-008).
)

// TestCopiarBanco_ConfereACopia: a conferência é o que prova que a cópia
// serve antes de o banco ser mexido. Uma cópia que não bate com a
// contagem de antes — uma linha a mais ou a menos numa tabela, uma
// tabela a mais ou a menos — é recusada.
//
// Pela API pública a divergência não aparece (a contagem e o VACUUM INTO
// saem da mesma conexão, sem nada entre eles); daí o teste interno, que
// dá a copiarBanco um "antes" que o banco não tem.
func TestCopiarBanco_ConfereACopia(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO instalacao (nome, valor) VALUES ('a', '1'), ('b', '2')`,
		`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em)
			VALUES ('transcription', 't1', 1, 'x')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	tabelas, err := tabelasDoBanco(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	antes, err := contarLinhas(ctx, db, tabelas)
	if err != nil {
		t.Fatal(err)
	}
	if antes["instalacao"] != 2 || antes["sync_outbox"] != 1 {
		t.Fatalf("premissa: a contagem de antes é %v", antes)
	}

	mexido := func(mexer func(m map[string]int64)) map[string]int64 {
		m := maps.Clone(antes)
		mexer(m)
		return m
	}
	casos := []struct {
		nome     string
		antes    map[string]int64
		querErro bool
	}{
		{"igual ao banco", antes, false},
		{"uma linha a mais", mexido(func(m map[string]int64) { m["instalacao"]++ }), true},
		{"uma linha a menos", mexido(func(m map[string]int64) { m["sync_outbox"]-- }), true},
		{"uma tabela a mais", mexido(func(m map[string]int64) { m["tabela que a cópia não tem"] = 0 }), true},
		{"uma tabela a menos", mexido(func(m map[string]int64) { delete(m, "sync_outbox") }), true},
	}
	for i, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			destino := filepath.Join(dir, fmt.Sprintf("copia-%d.db", i))
			err := copiarBanco(ctx, db, destino, marcaDaCopia, c.antes)
			if c.querErro && err == nil {
				t.Fatal("a conferência aceitou uma cópia que não bate com o banco de antes")
			}
			if !c.querErro && err != nil {
				t.Fatalf("a conferência recusou a cópia de um banco igual: %v", err)
			}
			if _, err := os.Stat(destino); err != nil {
				t.Errorf("o VACUUM INTO não deixou a cópia em %s: %v", destino, err)
			}
		})
	}
}
