package storage_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TestSchema_AllTablesAndIndexesExist prova (contra um SQLite de
// verdade, não só a leitura do SQL) que a migration cria todas as
// tabelas e todos os índices exigidos por docs/ai-spec/05-data-model.md
// (seção SQLite) e pela tarefa F1-B.
func TestSchema_AllTablesAndIndexesExist(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	wantTables := []string{
		"local_transcriptions",
		"local_dictionary_entries",
		"local_dictionary_aliases",
		"local_corrections",
		"local_settings",
		"sync_outbox",
		"sync_outbox_0001",
		"sync_state",
		"sync_mudou_aqui",
		"sync_aprendido",
		"sync_veio_da_nuvem",
		"instalacao",
	}
	wantIndexes := []string{
		"idx_local_transcriptions_active",
		"idx_local_transcriptions_trash",
		"idx_local_transcriptions_updated_at",
		"idx_local_dictionary_entries_updated_at",
		"idx_local_dictionary_aliases_entry_id",
		"idx_local_corrections_transcription_id",
		"idx_sync_outbox_next_attempt_at",
	}

	for _, name := range wantTables {
		assertSQLiteMasterHas(t, db, "table", name)
	}
	for _, name := range wantIndexes {
		assertSQLiteMasterHas(t, db, "index", name)
	}
}

func assertSQLiteMasterHas(t *testing.T, db *sql.DB, kind, name string) {
	t.Helper()
	var count int
	row := db.QueryRowContext(context.Background(),
		`SELECT COUNT(1) FROM sqlite_master WHERE type = ? AND name = ?`, kind, name)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("consultar sqlite_master por %s %q: %v", kind, name, err)
	}
	if count != 1 {
		t.Errorf("%s %q não existe no schema aplicado", kind, name)
	}
}

// TestSchema_DictionaryUniqueConstraintRejectsDuplicate prova que
// UNIQUE (user_id, normalized_form) em local_dictionary_entries é
// aplicado de verdade pelo SQLite, não só declarado no SQL.
func TestSchema_DictionaryUniqueConstraintRejectsDuplicate(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	const insert = `
		INSERT INTO local_dictionary_entries
			(id, user_id, canonical_form, normalized_form, priority, active, created_at, updated_at, version, sync_state)
		VALUES (?, 'user-1', 'DCTFWeb', 'dctfweb', 0, 1, '2026-09-17T00:00:00Z', '2026-09-17T00:00:00Z', 1, 'pending')`

	if _, err := db.ExecContext(ctx, insert, "entry-1"); err != nil {
		t.Fatalf("primeiro insert deveria ter sucesso: %v", err)
	}

	_, err := db.ExecContext(ctx, insert, "entry-2")
	if err == nil {
		t.Fatal("segundo insert com mesmo (user_id, normalized_form) deveria falhar por UNIQUE, mas teve sucesso")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
		t.Fatalf("esperava erro de UNIQUE constraint, achou: %v", err)
	}
}

// TestSchema_CorrectionTranscriptionIDSetNullOnDelete prova ON DELETE
// SET NULL em local_corrections.transcription_id: apagar a transcrição
// não apaga a correção nem falha por violação de FK — ela só perde a
// referência. Isso só funciona com PRAGMA foreign_keys ligado, que é
// responsabilidade do Migrator (ver TestMigrator_Migrate_EnablesForeignKeys).
func TestSchema_CorrectionTranscriptionIDSetNullOnDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	const insertTranscription = `
		INSERT INTO local_transcriptions
			(id, user_id, source_platform, raw_text, final_text, mode, asr_provider, status,
			 created_at, updated_at, version, sync_state)
		VALUES ('tr-1', 'user-1', 'windows', 'raw', 'final', 'smart', 'local', 'done',
			'2026-09-17T00:00:00Z', '2026-09-17T00:00:00Z', 1, 'pending')`
	if _, err := db.ExecContext(ctx, insertTranscription); err != nil {
		t.Fatalf("inserir transcription: %v", err)
	}

	const insertCorrection = `
		INSERT INTO local_corrections
			(id, user_id, transcription_id, from_text, to_text, normalized_from, normalized_to,
			 confirmed_by_user, use_count, created_at, updated_at, version, sync_state)
		VALUES ('corr-1', 'user-1', 'tr-1', 'de', 'para', 'de', 'para', 1, 1,
			'2026-09-17T00:00:00Z', '2026-09-17T00:00:00Z', 1, 'pending')`
	if _, err := db.ExecContext(ctx, insertCorrection); err != nil {
		t.Fatalf("inserir correction: %v", err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM local_transcriptions WHERE id = 'tr-1'`); err != nil {
		t.Fatalf("apagar transcription: %v", err)
	}

	var transcriptionID *string
	row := db.QueryRowContext(ctx, `SELECT transcription_id FROM local_corrections WHERE id = 'corr-1'`)
	if err := row.Scan(&transcriptionID); err != nil {
		t.Fatalf("consultar correction após delete: %v", err)
	}
	if transcriptionID != nil {
		t.Fatalf("esperava transcription_id NULL após ON DELETE SET NULL, achou %q", *transcriptionID)
	}

	var count int
	row = db.QueryRowContext(ctx, `SELECT COUNT(1) FROM local_corrections WHERE id = 'corr-1'`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("recontar correction: %v", err)
	}
	if count != 1 {
		t.Fatalf("a correction não deveria ter sido apagada junto (esperava SET NULL, não CASCADE), achou count=%d", count)
	}
}

// forbiddenColumnPatterns lista fragmentos de nome de coluna que
// indicariam áudio ou contexto de janela/seleção vazando para o schema
// (invariante de produto: AC-027, AC-028,
// docs/ai-spec/09-security-privacy.md "Logging proibido").
var forbiddenColumnPatterns = []string{
	"audio", "pcm", "wav", "buffer", "samples", "frames",
	"context", "nearby", "selection", "window_text", "clipboard",
}

// allowedColumnExceptions são colunas cujo nome bate com um padrão
// proibido acima, mas que a especificação (docs/ai-spec/05-data-model.md,
// "local_settings") exige de propósito e que não guardam áudio nem
// conteúdo de contexto — só uma política/flag sobre o recurso.
//
// local_context_enabled é boolean: liga/desliga a resolução de contexto
// local (ver docs/ai-spec/09-security-privacy.md, "Contexto"). O
// conteúdo do contexto em si é efêmero, só em RAM (ADR-006), e nunca
// tem coluna própria — é essa ausência que o teste protege.
var allowedColumnExceptions = map[string]bool{
	"local_context_enabled": true,
}

// TestSchema_NoAudioOrContextColumns varre o SQL real de TODAS as
// migrations (não um snapshot em memória) e falha se qualquer definição
// de coluna contiver um nome proibido. Um schema que nunca rodou é um
// schema não verificado — por isso as asserções fortes acima rodam
// contra SQLite de verdade, e esta varre o texto fonte que alimentou
// aquele SQLite.
func TestSchema_NoAudioOrContextColumns(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations/*.sql: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("nenhum arquivo de migration encontrado em migrations/*.sql")
	}

	columnLine := linhaDeColuna

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("ler %s: %v", file, err)
		}

		for _, line := range strings.Split(string(content), "\n") {
			m := columnLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			columnName := strings.ToLower(m[1])
			if allowedColumnExceptions[columnName] {
				continue
			}
			for _, pattern := range forbiddenColumnPatterns {
				if strings.Contains(columnName, pattern) {
					t.Errorf("%s: coluna %q contém padrão proibido %q (áudio/contexto não pode ser persistido)", file, columnName, pattern)
				}
			}
		}
	}
}

// linhaDeColuna reconhece uma definição de coluna numa migration: o nome no
// começo da linha, seguido do tipo.
var linhaDeColuna = regexp.MustCompile(`(?im)^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s+[A-Z]`)

// TestSchema_VarreduraVeAsColunasDa0007 prova que a varredura de nomes
// proibidos (TestSchema_NoAudioOrContextColumns) lê de fato as colunas do
// espelho do aprendido (ADR-019, F8e): uma migration escrita num formato que
// o padrão não reconhece passaria calada. Cada coluna do SQLite migrado
// dessas tabelas tem de aparecer como linha de coluna no arquivo da 0007, ou
// no da 0008 (o par perdido, da revisão da F8g).
func TestSchema_VarreduraVeAsColunasDa0007(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	vistas := map[string]bool{}
	for _, arquivo := range []string{"0007_sync_aprendido.sql", "0008_sync_aprendido_perdido.sql"} {
		conteudo, err := os.ReadFile(filepath.Join("migrations", arquivo))
		if err != nil {
			t.Fatal(err)
		}
		for _, linha := range strings.Split(string(conteudo), "\n") {
			if m := linhaDeColuna.FindStringSubmatch(linha); m != nil {
				vistas[strings.ToLower(m[1])] = true
			}
		}
	}
	total := 0
	for _, tabela := range []string{"sync_aprendido", "sync_veio_da_nuvem"} {
		cols, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, tabela)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var c string
			if err := cols.Scan(&c); err != nil {
				t.Fatal(err)
			}
			total++
			if !vistas[c] {
				t.Errorf("%s.%s não é vista pela varredura de nomes proibidos", tabela, c)
			}
		}
		cols.Close()
	}
	if total < 25 {
		t.Fatalf("só %d colunas nas tabelas da 0007", total)
	}
}
