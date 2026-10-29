package storage

import (
	"context"
	"reflect"
	"testing"
)

// A 0013 (o sync das notas, N7) só põe cinco colunas nulas em local_notes (o
// selo e o corpo_nuvem). A prova segue a da 0010: um banco na 0012 com notas,
// marcadores, ligações e ditados, lido tabela por tabela antes e depois.

func TestMigracao0013_SoGanhaAsColunasNulas(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	for _, v := range []string{"0008_sync_aprendido_perdido", "0009_notas", "0010_ditado_virou_nota",
		"0011_local_settings_gatilho_da_nota", "0012_notas_posicao"} {
		aplicarSo(t, db, v)
	}
	for _, q := range []string{
		`INSERT INTO local_notes (id, user_id, titulo, corpo, cor, fixada, created_at, updated_at, version, posicao)
		 VALUES ('n1', 'u1', 'mercado', '- [ ] leite', 'azul', 1, '2026-10-01T00:00:00.000000000Z',
		   '2026-10-02T00:00:00.000000000Z', 3, 'V')`,
		`INSERT INTO local_notes (id, user_id, corpo, created_at, updated_at, deleted_at, purge_at, posicao)
		 VALUES ('n2', 'u1', 'apagada', '2026-10-01T00:00:00.000000000Z', '2026-10-01T00:00:00.000000000Z',
		   '2026-10-03T00:00:00.000000000Z', '2026-11-02T00:00:00.000000000Z', 'W')`,
		`INSERT INTO local_labels (id, user_id, nome, nome_dobrado, created_at, updated_at)
		 VALUES ('l1', 'u1', 'Casa', 'casa', '2026-10-01T00:00:00.000000000Z', '2026-10-01T00:00:00.000000000Z')`,
		`INSERT INTO local_note_labels (note_id, label_id) VALUES ('n1', 'l1')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	antes := todasAsTabelas(t, db)
	if len(antes["local_notes"]) != 2 || len(antes["local_transcriptions"]) == 0 {
		t.Fatalf("semeadura: %d notas, %d ditados", len(antes["local_notes"]), len(antes["local_transcriptions"]))
	}

	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	depois := todasAsTabelas(t, db)
	semAsColunasDa0015(t, depois)
	// A única tabela nova é a das imagens das notas, da 0014 (o Migrate roda até a
	// última), e vazia.
	if l, ok := depois["local_note_images"]; len(depois) != len(antes)+1 || !ok || len(l) != 0 {
		t.Errorf("a 0013 criou ou tirou tabela: %d antes, %d depois", len(antes), len(depois))
	}
	novas := []string{"conflito_titulo", "conflito_corpo", "conflito_device_id", "conflito_em", "corpo_nuvem"}
	for tb, a := range antes {
		d := depois[tb]
		switch tb {
		case "schema_migrations":
			if len(d) != len(a)+3 || d[len(a)]["version"] != "0013_notas_sync" || d[len(a)+1]["version"] != "0014_notas_imagens" ||
				d[len(a)+2]["version"] != "0015_notas_imagens_sync" {
				t.Fatalf("as migrations aplicadas: %v", d)
			}
			d = d[:len(a)]
		case "local_notes":
			for i := range d {
				for _, c := range novas {
					if v, ok := d[i][c]; !ok || v != nil {
						t.Errorf("a coluna nova %s = %v (presente %v); quero nula", c, v, ok)
					}
					delete(d[i], c)
				}
			}
		}
		if !reflect.DeepEqual(a, d) {
			t.Errorf("%s mudou:\nantes  %v\ndepois %v", tb, a, d)
		}
	}

	// A nota migrada é lida como antes, sem selo e sem corpo_nuvem.
	n, err := NewSQLiteNotaRepository(db).Get(ctx, "u1", "n1")
	if err != nil {
		t.Fatal(err)
	}
	if n.Titulo != "mercado" || n.Conflito != nil || n.CorpoNuvem != nil || len(n.Marcadores) != 1 {
		t.Errorf("a nota migrada: %+v", n)
	}
}
