package pgstore

import (
	"strings"
	"testing"
	"time"
)

// As consultas das imagens das notas (N10b), lidas contra a 0026 sem Postgres:
// a soma do teto conta as imagens de TODA a conta (as das notas da lixeira
// inclusive) e não as lápides; a lista da nota é só das vivas, na ordem dela, e
// nunca leva os bytes; só a leitura dos bytes toca a coluna conteudo; e as
// colunas existem na migration. O comportamento é o da suíte de contrato (os
// dois Stores) e dos testes com Postgres.
func TestSQL_AsConsultasDasImagens(t *testing.T) {
	for nome, c := range map[string]struct {
		sql   string
		frags []string
		nunca []string
	}{
		"a soma do teto e a contagem da nota": {sqlImageCounts,
			[]string{"FROM note_images WHERE user_id = $1 AND removed_at IS NULL", "COUNT(*) FILTER (WHERE note_id = $2)", "COALESCE(SUM(bytes), 0)"},
			[]string{"deleted_at", "JOIN notes"}},
		"a lista da nota": {noteImagesOf("n"),
			[]string{"i.user_id = n.user_id AND i.note_id = n.id AND i.removed_at IS NULL", "ORDER BY i.created_at, i.id", "'[]'::json"},
			[]string{"conteudo"}},
		"a nota lida com as imagens": {sqlSelectNote, []string{"FROM note_images i"}, nil},
		"o pull das notas":           {sqlPullNotes, []string{"FROM note_images i"}, nil},
		"o pull das imagens": {sqlPullImages,
			[]string{"FROM note_images", "last_seq > $2", "ORDER BY last_seq", "LIMIT $3"}, []string{"conteudo", "removed_at IS NULL"}},
		"os metadados":    {sqlImageInfo, []string{"WHERE user_id = $1 AND id = $2"}, []string{"conteudo"}},
		"a linha travada": {sqlImageForUpdate, []string{"WHERE user_id = $1 AND id = $2 FOR UPDATE"}, []string{"conteudo"}},
		"os bytes":        {sqlImageBytes, []string{"conteudo FROM note_images", "note_id = $2 AND id = $3 AND removed_at IS NULL"}, nil},
		"a lápide":        {sqlRemoveImage, []string{"SET conteudo = NULL, removed_at = $3, last_seq = $4", "removed_at IS NULL"}, nil},
		"a lápide nova":   {sqlImageTombstone, []string{"INSERT INTO note_images (id, user_id, note_id, created_at, removed_at, last_seq)"}, nil},
		"o anexo":         {sqlInsertImage, []string{"INSERT INTO note_images (", ", conteudo)", "NULL, $9, $10"}, nil},
	} {
		for _, f := range c.frags {
			if !strings.Contains(c.sql, f) {
				t.Errorf("%s: sem %q:\n%s", nome, f, c.sql)
			}
		}
		for _, f := range c.nunca {
			if strings.Contains(c.sql, f) {
				t.Errorf("%s: com %q:\n%s", nome, f, c.sql)
			}
		}
	}
	cols := createTableColumns(t, "0026_note_images_table.sql")
	for _, col := range append(strings.Split(imageCols, ", "), "conteudo") {
		if !cols[col] {
			t.Errorf("a coluna %s não existe na 0026 (%v)", col, cols)
		}
	}
	if len(cols) != len(strings.Split(imageCols, ", "))+1 {
		t.Errorf("a 0026 tem %d colunas, o SQL usa %d", len(cols), len(strings.Split(imageCols, ", "))+1)
	}
}

// A lista imagens da nota vem do JSON da subconsulta: vazia é o array vazio, e
// a ordem é a do Go (created_at, e o id no empate).
func TestLerImagensDaNota(t *testing.T) {
	is, err := lerImagensDaNota([]byte(`[]`), "n1", "u1")
	if err != nil || is == nil || len(is) != 0 {
		t.Fatalf("o vazio = %v, %v", is, err)
	}
	if is, err = lerImagensDaNota(nil, "n1", "u1"); err != nil || is == nil {
		t.Fatalf("o nulo = %v, %v", is, err)
	}
	raw := `[{"id":"b","largura":3,"altura":4,"bytes":10,"sha256":"aa","created_at":"2026-10-28T09:00:00.000001-03:00","last_seq":5},
		{"id":"a","largura":1,"altura":2,"bytes":20,"sha256":"bb","created_at":"2026-10-28T12:00:00.000001+00:00","last_seq":4}]`
	is, err = lerImagensDaNota([]byte(raw), "n1", "u1")
	if err != nil || len(is) != 2 {
		t.Fatalf("a lista = %v, %v", is, err)
	}
	quando := time.Date(2026, 10, 28, 12, 0, 0, 1000, time.UTC)
	if is[0].ID != "a" || is[1].ID != "b" || !is[0].CreatedAt.Equal(quando) || is[0].CreatedAt.Location() != time.UTC ||
		is[0].NoteID != "n1" || is[0].UserID != "u1" || is[1].Largura != 3 || is[1].Bytes != 10 || is[1].LastSeq != 5 {
		t.Errorf("a lista = %+v", is)
	}
	if _, err := lerImagensDaNota([]byte(`{`), "n1", "u1"); err == nil || strings.Contains(err.Error(), "{") {
		t.Errorf("o JSON quebrado: %v", err)
	}
}
