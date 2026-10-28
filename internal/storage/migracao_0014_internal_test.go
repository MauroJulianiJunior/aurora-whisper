package storage

import (
	"context"
	"reflect"
	"testing"
)

// A 0014 (as imagens das notas, N10) só cria a tabela local_note_images, vazia.
// A prova segue a da 0013: um banco na 0013 com notas, marcadores, ligações e
// ditados, lido tabela por tabela antes e depois.

func TestMigracao0014_SoNasceATabelaVazia(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	for _, v := range []string{"0008_sync_aprendido_perdido", "0009_notas", "0010_ditado_virou_nota",
		"0011_local_settings_gatilho_da_nota", "0012_notas_posicao", "0013_notas_sync"} {
		aplicarSo(t, db, v)
	}
	for _, q := range []string{
		`INSERT INTO local_notes (id, user_id, titulo, corpo, cor, fixada, created_at, updated_at, version, posicao, corpo_nuvem)
		 VALUES ('n1', 'u1', 'mercado', '- [ ] leite', 'azul', 1, '2026-10-01T00:00:00.000000000Z',
		   '2026-10-02T00:00:00.000000000Z', 3, 'V', '- [ ] leite')`,
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

	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	depois := todasAsTabelas(t, db)
	if l, ok := depois["local_note_images"]; !ok || len(l) != 0 || len(depois) != len(antes)+1 {
		t.Fatalf("a tabela nova: presente %v, %d linhas; %d tabelas antes, %d depois", ok, len(l), len(antes), len(depois))
	}
	for tb, a := range antes {
		d := depois[tb]
		if tb == "schema_migrations" {
			if len(d) != len(a)+1 || d[len(a)]["version"] != "0014_notas_imagens" {
				t.Fatalf("as migrations aplicadas: %v", d)
			}
			d = d[:len(a)]
		}
		if !reflect.DeepEqual(a, d) {
			t.Errorf("%s mudou:\nantes  %v\ndepois %v", tb, a, d)
		}
	}
	var indice int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_local_note_images_nota'`).Scan(&indice); err != nil || indice != 1 {
		t.Errorf("o índice: %d, %v", indice, err)
	}

	// A nota migrada é lida como antes, com a lista de imagens vazia.
	n, err := NewSQLiteNotaRepository(db).Get(ctx, "u1", "n1")
	if err != nil {
		t.Fatal(err)
	}
	if n.Titulo != "mercado" || n.Imagens == nil || len(n.Imagens) != 0 || len(n.Marcadores) != 1 {
		t.Errorf("a nota migrada: %+v", n)
	}

	// A cascata: apagar a nota pelo SQL leva as linhas das imagens (a segunda
	// guarda; o Go as apaga antes).
	if _, err := db.Exec(`INSERT INTO local_note_images (id, note_id, largura, altura, bytes, sha256, created_at)
		VALUES ('i1', 'n2', 10, 10, 100, 'x', '2026-10-04T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM local_notes WHERE id = 'n2'`); err != nil {
		t.Fatal(err)
	}
	var sobrou int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_note_images`).Scan(&sobrou); err != nil || sobrou != 0 {
		t.Errorf("a cascata deixou %d linhas (%v)", sobrou, err)
	}
	// E a imagem de uma nota que não existe é recusada.
	if _, err := db.Exec(`INSERT INTO local_note_images (id, note_id, largura, altura, bytes, sha256, created_at)
		VALUES ('i2', 'nenhuma', 10, 10, 100, 'x', '2026-10-04T00:00:00.000000000Z')`); err == nil {
		t.Error("a imagem sem nota foi aceita")
	}
}
