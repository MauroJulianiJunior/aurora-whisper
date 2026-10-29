package storage

import (
	"context"
	"reflect"
	"testing"
)

// A 0015 (o sync das imagens das notas, N10b) só põe colunas: cinco em
// local_note_images, com o valor de "nada ainda" (a imagem com o arquivo aqui,
// que a nuvem nunca viu, sem tentativa), e uma, nula, em sync_outbox. A prova
// segue a da 0014: um banco na 0014 com notas, uma imagem, ditados e
// marcadores na fila, lido tabela por tabela antes e depois.

// colunasDa0015 são as colunas que a 0015 põe, com o valor que toda linha de
// antes ganha.
var colunasDa0015 = map[string]map[string]any{
	"local_note_images": {"na_nuvem": int64(0), "sync_state": "pending", "last_sync_error": nil, "tentativas": int64(0), "tentou_em": nil},
	"sync_outbox":       {"nota_id": nil},
}

// semAsColunasDa0015 confere e tira, das linhas lidas depois do Migrate (que roda
// até a última), as colunas da 0015: os testes das migrations de antes comparam
// as tabelas com o que eram.
func semAsColunasDa0015(t *testing.T, tabelas map[string][]map[string]any) {
	t.Helper()
	for tb, colunas := range colunasDa0015 {
		for i, linha := range tabelas[tb] {
			for c, quero := range colunas {
				if v, ok := linha[c]; !ok || !reflect.DeepEqual(v, quero) {
					t.Errorf("%s linha %d: a coluna da 0015 %s = %v (presente %v); quero %v", tb, i, c, v, ok, quero)
				}
				delete(linha, c)
			}
		}
	}
}

func TestMigracao0015_SoGanhaAsColunasDoNadaAinda(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	for _, v := range []string{"0008_sync_aprendido_perdido", "0009_notas", "0010_ditado_virou_nota",
		"0011_local_settings_gatilho_da_nota", "0012_notas_posicao", "0013_notas_sync", "0014_notas_imagens"} {
		aplicarSo(t, db, v)
	}
	for _, q := range []string{
		`INSERT INTO local_notes (id, user_id, titulo, corpo, created_at, updated_at, version, posicao)
		 VALUES ('n1', 'u1', 'mercado', '', '2026-10-01T00:00:00.000000000Z', '2026-10-02T00:00:00.000000000Z', 3, 'V')`,
		`INSERT INTO local_note_images (id, note_id, largura, altura, bytes, sha256, created_at)
		 VALUES ('i1', 'n1', 30, 20, 100, 'aa', '2026-10-04T00:00:00.000000000Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	antes := todasAsTabelas(t, db)
	if len(antes["local_note_images"]) != 1 || len(antes["sync_outbox"]) == 0 {
		t.Fatalf("semeadura: %d imagens, %d marcadores", len(antes["local_note_images"]), len(antes["sync_outbox"]))
	}

	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	depois := todasAsTabelas(t, db)
	if len(depois) != len(antes) {
		t.Fatalf("a 0015 criou ou tirou tabela: %d antes, %d depois", len(antes), len(depois))
	}
	semAsColunasDa0015(t, depois)
	for tb, a := range antes {
		d := depois[tb]
		if tb == "schema_migrations" {
			if len(d) != len(a)+1 || d[len(a)]["version"] != "0015_notas_imagens_sync" {
				t.Fatalf("as migrations aplicadas: %v", d)
			}
			d = d[:len(a)]
		}
		if !reflect.DeepEqual(a, d) {
			t.Errorf("%s mudou:\nantes  %v\ndepois %v", tb, a, d)
		}
	}

	// A imagem de antes é lida como antes: com o arquivo (sem Estado), e é a que
	// a completação do envio põe na fila (na_nuvem 0, normal).
	n, err := NewSQLiteNotaRepository(db).Get(ctx, "u1", "n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Imagens) != 1 || n.Imagens[0].ID != "i1" || n.Imagens[0].Estado != "" {
		t.Errorf("a imagem migrada: %+v", n.Imagens)
	}
}
