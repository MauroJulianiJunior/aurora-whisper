package storage

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

// A 0008 (o par perdido do "trocado em outro aparelho", revisão da F8g) só põe
// duas colunas nulas na sync_aprendido. A prova segue a da 0007: um banco na
// 0007 com dados (os da 0006 e uma linha do espelho com a etiqueta), lido
// tabela por tabela antes e depois.

// bancoNa0007 é o banco da 0006 com a 0007 e uma linha do espelho.
func bancoNa0007(t *testing.T) *sql.DB {
	t.Helper()
	db := bancoNa0006(t)
	aplicarSo(t, db, "0007_sync_aprendido")
	if _, err := db.Exec(`INSERT INTO sync_aprendido (user_id, entity_id, server_version, versao_base, versao_local,
		variante, termo, nuvem_versao, nuvem_variante, nuvem_termo, nuvem_restrito, nuvem_desfeito,
		situacao, situacao_em)
		VALUES ('0199a000-0000-7000-8000-000000000001', 'id-1', 4, 2, 2, 'ecedê', 'ECD', 4, 'ecedê', 'ECD', 0, 0,
		'trocado_em_outro', '2026-10-07T12:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigracao0008_SoGanhaAsColunasNulas(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	antes := todasAsTabelas(t, db)

	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	depois := todasAsTabelas(t, db)
	for tb, a := range antes {
		d := depois[tb]
		switch tb {
		case "schema_migrations":
			// O Migrate roda até a última: a 0008 e as migrations que vieram depois.
			if len(d) != len(a)+5 || d[len(a)]["version"] != "0008_sync_aprendido_perdido" ||
				d[len(a)+1]["version"] != "0009_notas" || d[len(a)+2]["version"] != "0010_ditado_virou_nota" ||
				d[len(a)+3]["version"] != "0011_local_settings_gatilho_da_nota" ||
				d[len(a)+4]["version"] != "0012_notas_posicao" {
				t.Fatalf("as migrations aplicadas: %v", d)
			}
			d = d[:len(a)]
		case "sync_aprendido":
			for i := range d {
				for _, c := range []string{"perdido_variante", "perdido_termo"} {
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
	// A 0008 não cria tabela; as três novas são as das notas, da 0009.
	if len(depois) != len(antes)+3 {
		t.Errorf("a 0008 criou tabela: %d antes, %d depois (esperava só as 3 das notas)", len(antes), len(depois))
	}

	// O espelho lê a linha migrada: a etiqueta, sem o par perdido.
	es, err := NewSQLiteTranscriptionRepository(db).Fila("0199a000-0000-7000-8000-000000000001").Espelho(ctx)
	if err != nil || len(es) != 1 || es[0].Situacao != EspelhoTrocadoEmOutro || es[0].Perdido != nil {
		t.Fatalf("o espelho depois da 0008: %+v, %v", es, err)
	}

	migrado := todasAsTabelas(t, db)
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate de novo: %v", err)
	}
	if !reflect.DeepEqual(todasAsTabelas(t, db), migrado) {
		t.Error("migrar de novo mudou o banco")
	}
}

// A 0008 é uma transação: a coluna perdido_termo que já existe faz o segundo
// ALTER falhar, depois de o primeiro ter rodado, e nada dela fica.
func TestMigracao0008_FalhaNoMeioNaoDeixaNada(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	if _, err := db.Exec(`ALTER TABLE sync_aprendido ADD COLUMN perdido_termo TEXT`); err != nil {
		t.Fatal(err)
	}
	antes := todasAsTabelas(t, db)
	if err := NewMigrator().Migrate(ctx, db); err == nil {
		t.Fatal("a 0008 passou com a coluna ocupada")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = '0008_sync_aprendido_perdido'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a 0008 ficou registrada: %d, %v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sync_aprendido') WHERE name = 'perdido_variante'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a coluna perdido_variante da 0008 ficou: %d, %v", n, err)
	}
	if !reflect.DeepEqual(todasAsTabelas(t, db), antes) {
		t.Error("a 0008 que falhou mudou o banco")
	}
}
