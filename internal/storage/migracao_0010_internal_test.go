package storage

import (
	"context"
	"reflect"
	"testing"
)

// A 0010 (o ditado que virou nota, N3) só põe uma coluna nula em
// local_transcriptions e um índice parcial. A prova segue a da 0008: um banco
// na 0009 com dados, lido tabela por tabela antes e depois.

func TestMigracao0010_SoGanhaAColunaNula(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	aplicarSo(t, db, "0008_sync_aprendido_perdido")
	aplicarSo(t, db, "0009_notas")
	// linhasDe tira a coluna virou_nota nula; aqui o "antes" não a tem, e o
	// "depois" é conferido à parte, pela pragma.
	antes := todasAsTabelas(t, db)
	if len(antes["local_transcriptions"]) != 9 {
		t.Fatalf("semeadura: %d ditados", len(antes["local_transcriptions"]))
	}

	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	depois := todasAsTabelas(t, db)
	for tb, a := range antes {
		d := depois[tb]
		if tb == "schema_migrations" {
			// A 0010 e as que vieram depois (a 0011, do gatilho da nota).
			if len(d) != len(a)+2 || d[len(a)]["version"] != "0010_ditado_virou_nota" ||
				d[len(a)+1]["version"] != "0011_local_settings_gatilho_da_nota" {
				t.Fatalf("as migrations aplicadas: %v", d)
			}
			d = d[:len(a)]
		}
		if !reflect.DeepEqual(a, d) {
			t.Errorf("%s mudou:\nantes  %v\ndepois %v", tb, a, d)
		}
	}

	// A coluna existe, é nula em todo ditado que já estava lá, e o índice nasceu.
	var existe, naoNulos int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('local_transcriptions') WHERE name = 'virou_nota'`).Scan(&existe); err != nil || existe != 1 {
		t.Fatalf("a coluna virou_nota: %d, %v", existe, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions WHERE virou_nota IS NOT NULL`).Scan(&naoNulos); err != nil || naoNulos != 0 {
		t.Errorf("%d ditados já marcados: a migration não pode esconder nada do histórico", naoNulos)
	}
	var indice int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_local_transcriptions_virou_nota'`).Scan(&indice); err != nil || indice != 1 {
		t.Errorf("o índice: %d, %v", indice, err)
	}
}
