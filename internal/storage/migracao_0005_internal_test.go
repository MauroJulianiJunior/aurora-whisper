package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A 0005 passa pelo historico.db de verdade do Mauro: ela não pode perder nem
// mudar nada. A prova é um banco num arquivo, migrado só até a 0004 (como o
// de quem tem o Agent instalado hoje), com o que um banco real tem (ditados,
// lixeira, as preferências, a instalação com o dono e a nuvem da F6, e uma
// linha na sync_outbox velha, que nunca teve escritor mas poderia ter), lido
// tabela por tabela antes e depois da 0005.

// migrarAte aplica as migrations embutidas até a versão dada (inclusive),
// como um Agent de antes faria.
func migrarAte(t *testing.T, db *sql.DB, ultima string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, schemaMigrationsTable); err != nil {
		t.Fatal(err)
	}
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.version > ultima {
			break
		}
		if err := applyMigration(ctx, db, m); err != nil {
			t.Fatalf("aplicar %s: %v", m.version, err)
		}
	}
}

// aplicarSo aplica UMA migration embutida, como o Migrator faria, num banco
// que já tem as anteriores.
func aplicarSo(t *testing.T, db *sql.DB, versao string) {
	t.Helper()
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.version == versao {
			if err := applyMigration(context.Background(), db, m); err != nil {
				t.Fatalf("aplicar %s: %v", versao, err)
			}
			return
		}
	}
	t.Fatalf("a migration %s não existe", versao)
}

// linhasDe lê a tabela inteira, cada linha como coluna -> valor, em ordem de
// rowid.
func linhasDe(t *testing.T, db *sql.DB, tabela string) []map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM "` + tabela + `" ORDER BY rowid`)
	if err != nil {
		t.Fatalf("ler %s: %v", tabela, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigracao0005_NaoPerdeNemMudaNada(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	migrarAte(t, db, "0004_local_settings_microfone")

	// O banco "de antes": ditados ativos e na lixeira (o INSERT é o da 0004,
	// sem as colunas novas), as preferências, a instalação (o dono fixo, o
	// registro da migração e a nuvem da F6), o cursor e um marcador velho.
	const dono = "0199a000-0000-7000-8000-000000000001"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	base := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		criado := base.Add(time.Duration(i) * time.Hour).Format(formatoTempo)
		var apagado, purga any
		if i%3 == 2 {
			apagado = base.Add(time.Duration(i)*time.Hour + time.Minute).Format(formatoTempo)
			purga = base.AddDate(0, 0, 30).Format(formatoTempo)
		}
		exec(`INSERT INTO local_transcriptions (id, user_id, device_id, source_platform, raw_text, final_text,
			mode, language, asr_provider, asr_model, duration_ms, processing_ms, confidence, target_app, status,
			created_at, updated_at, deleted_at, purge_at, version, sync_state, server_version, last_sync_error)
			VALUES (?, ?, NULL, 'windows', ?, ?, 'smart', NULL, 'whisper-local', 'small-q4_0', 3200, 410, NULL, NULL,
			'done', ?, ?, ?, ?, ?, 'pending', NULL, NULL)`,
			fmt.Sprintf("0199a000-0000-7000-8000-00000000010%d", i), dono,
			fmt.Sprintf("bruto %d com acento: ação", i), fmt.Sprintf("final %d com acento: ação", i),
			criado, criado, apagado, purga, i+1)
	}
	exec(`INSERT INTO local_settings (id, hotkey, hold_threshold_ms, overlay_position, asr_policy,
		protected_field_policy, local_port, updated_at, text_mode, ai_provider, ai_model, microphone_always_ready)
		VALUES (1, 'ctrl+win', 300, 'bottom', 'local_only', 'block', 0, 'x', 'polish', 'anthropic', 'claude', 1)`)
	exec(`INSERT INTO instalacao (nome, valor) VALUES ('dono_local', ?), ('dono_local_migrado_em', 'x'),
		('install_id', 'i1'), ('nuvem_url', 'http://localhost:8080'), ('nuvem_aparelho', 'a1'),
		('nuvem_conectado_em', '2026-10-06T12:00:00Z')`, dono)
	exec(`INSERT INTO sync_state (user_id, device_id, last_server_seq, last_success_at) VALUES (?, 'a1', 7, 'x')`, dono)
	exec(`INSERT INTO sync_outbox (mutation_id, entity_type, entity_id, operation, base_version, payload_json, created_at)
		VALUES ('m1', 'transcription', 't1', 'upsert', 0, '{"final_text":"velho"}', 'x')`)

	tabelasAntes := []string{"local_transcriptions", "local_settings", "instalacao", "sync_state", "sync_outbox",
		"local_dictionary_entries", "local_corrections"}
	antes := map[string][]map[string]any{}
	for _, tb := range tabelasAntes {
		antes[tb] = linhasDe(t, db, tb)
	}
	if len(antes["local_transcriptions"]) != 6 || len(antes["sync_outbox"]) != 1 {
		t.Fatalf("semeadura: %d ditados, %d marcadores", len(antes["local_transcriptions"]), len(antes["sync_outbox"]))
	}

	// Agora o Agent da F7a sobe: só a 0005 (as seguintes têm o teste delas).
	aplicarSo(t, db, "0005_sync_fila")

	colunasNovas := map[string][]string{
		"local_transcriptions": {"conflito_texto", "conflito_device_id", "conflito_em"},
		"sync_state":           {"sync_epoch", "pareado_em", "ultimo_erro"},
	}
	for _, tb := range tabelasAntes {
		nome := tb
		if tb == "sync_outbox" {
			nome = "sync_outbox_0001" // renomeada, intocada
		}
		depois := linhasDe(t, db, nome)
		if len(depois) != len(antes[tb]) {
			t.Fatalf("%s: %d linhas antes, %d depois", tb, len(antes[tb]), len(depois))
		}
		for i := range depois {
			for _, c := range colunasNovas[tb] {
				if v, ok := depois[i][c]; !ok || v != nil {
					t.Errorf("%s linha %d: a coluna nova %s = %v (presente %v); quero nula", tb, i, c, v, ok)
				}
				delete(depois[i], c)
			}
			if !reflect.DeepEqual(depois[i], antes[tb][i]) {
				t.Errorf("%s linha %d mudou:\nantes  %v\ndepois %v", tb, i, antes[tb][i], depois[i])
			}
		}
	}

	// A sync_outbox nova nasce vazia, com o desenho do marcador, e o índice
	// velho ficou com a tabela velha.
	if n := len(linhasDe(t, db, "sync_outbox")); n != 0 {
		t.Errorf("a sync_outbox nova tem %d linhas", n)
	}
	var cols []string
	rows, err := db.Query(`SELECT name FROM pragma_table_info('sync_outbox')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		cols = append(cols, c)
	}
	rows.Close()
	sort.Strings(cols)
	if got := strings.Join(cols, ","); got != "campos,criado_em,entity_id,entity_type,mutation_id,versao_enviada" {
		t.Errorf("colunas da sync_outbox nova: %s", got)
	}
	var tabelaDoIndice string
	if err := db.QueryRow(`SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = 'idx_sync_outbox_next_attempt_at'`).
		Scan(&tabelaDoIndice); err != nil || tabelaDoIndice != "sync_outbox_0001" {
		t.Errorf("o índice velho está em %q (%v); quero na sync_outbox_0001", tabelaDoIndice, err)
	}

	// E o repositório lê o banco migrado: os seis ditados, sem selo.
	repo := NewSQLiteTranscriptionRepository(db)
	for _, lixeira := range []bool{false, true} {
		p, err := repo.List(ctx, TranscriptionListFilter{UserID: dono, Trash: lixeira})
		if err != nil {
			t.Fatal(err)
		}
		quero := 4
		if lixeira {
			quero = 2
		}
		if len(p.Items) != quero {
			t.Errorf("lixeira=%v: %d ditados; quero %d", lixeira, len(p.Items), quero)
		}
		for _, it := range p.Items {
			if it.Conflito != nil || it.SyncState != SituacaoNormal || it.ServerVersion != nil {
				t.Errorf("ditado %s mudou de situação: %+v", it.ID, it)
			}
		}
	}

	// Migrar de novo não faz nada (e não falha).
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate de novo: %v", err)
	}
}

// A 0005 é uma transação: se ela falha no meio, nada dela fica (nem a
// renomeação). A falha é real: uma tabela sync_outbox_0001 que já existe faz
// a renomeação falhar.
func TestMigracao0005_FalhaNoMeioNaoDeixaNada(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	migrarAte(t, db, "0004_local_settings_microfone")
	if _, err := db.Exec(`CREATE TABLE sync_outbox_0001 (x TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sync_outbox (mutation_id, entity_type, entity_id, operation, payload_json, created_at)
		VALUES ('m1', 'transcription', 't1', 'upsert', '{}', 'x')`); err != nil {
		t.Fatal(err)
	}
	if err := NewMigrator().Migrate(ctx, db); err == nil {
		t.Fatal("a 0005 passou com o nome ocupado")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = '0005_sync_fila'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a 0005 ficou registrada: %d, %v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_outbox WHERE payload_json = '{}'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("a sync_outbox velha não está intacta: %d, %v", n, err)
	}
	if _, err := db.Exec(`SELECT conflito_texto FROM local_transcriptions`); err == nil {
		t.Error("uma coluna da 0005 ficou")
	}
}
