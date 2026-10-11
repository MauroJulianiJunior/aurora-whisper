package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// padroesDeSegredo são fragmentos de nome de coluna que indicariam um
// segredo guardado no banco. A chave de API do provedor de IA mora no
// Gerenciador de Credenciais do Windows (internal/agent/chaves), nunca
// aqui — 09-security-privacy.md e 10-env-and-secrets.md.
//
// "key" sozinho não entra: local_dictionary_aliases tem phonetic_key, que
// é a chave FONÉTICA de um termo, não segredo.
var padroesDeSegredo = []string{
	"api_key", "apikey", "chave", "secret", "segredo", "token", "senha", "password", "credential",
}

// TestSchema_NenhumaColunaDeSegredo lê as colunas do SQLite MIGRADO — o
// schema que de fato roda, com todas as migrations aplicadas — e falha se
// alguma tiver nome de segredo.
func TestSchema_NenhumaColunaDeSegredo(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	tabelas, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("listar tabelas: %v", err)
	}
	var nomes []string
	for tabelas.Next() {
		var n string
		if err := tabelas.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		nomes = append(nomes, n)
	}
	tabelas.Close()

	colunasVistas := 0
	for _, tabela := range nomes {
		cols, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, tabela)
		if err != nil {
			t.Fatalf("colunas de %s: %v", tabela, err)
		}
		for cols.Next() {
			var col string
			if err := cols.Scan(&col); err != nil {
				t.Fatalf("scan: %v", err)
			}
			colunasVistas++
			for _, p := range padroesDeSegredo {
				if strings.Contains(strings.ToLower(col), p) {
					t.Errorf("%s.%s tem nome de segredo (%q): segredo não vai para o banco", tabela, col, p)
				}
			}
		}
		cols.Close()
	}
	// Sem isto, uma consulta que não devolvesse nada passaria calada.
	if colunasVistas < 50 {
		t.Fatalf("só %d colunas lidas — a varredura não está vendo o schema", colunasVistas)
	}
}

// TestSchema_LocalSettingsTemAsColunasDaIA confere, no SQLite migrado, as
// três colunas da 0002 com o padrão certo: uma linha gravada sem elas
// (como faria um Agent anterior à 0002) sai com "smart" e vazio.
func TestSchema_LocalSettingsTemAsColunasDaIA(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO local_settings
		(id, hotkey, hold_threshold_ms, overlay_position, asr_policy, protected_field_policy, local_port, updated_at)
		VALUES (1, 'ctrl+win', 700, '', 'local_only', 'block', 0, '2026-09-29T00:00:00.000000000Z')`); err != nil {
		t.Fatalf("inserir linha sem as colunas novas: %v", err)
	}
	var modo, provedor, modelo string
	if err := db.QueryRowContext(ctx, `SELECT text_mode, ai_provider, ai_model FROM local_settings WHERE id = 1`).
		Scan(&modo, &provedor, &modelo); err != nil {
		t.Fatalf("ler colunas novas: %v", err)
	}
	if modo != "smart" || provedor != "" || modelo != "" {
		t.Fatalf("padrões = (%q, %q, %q), quero (\"smart\", \"\", \"\")", modo, provedor, modelo)
	}
}

// TestSchema_LinhaAntesDa0004FicaComOMicrofoneSobDemanda simula uma
// instalação que já existia: o banco chega na 0003 com uma linha
// gravada, e só então a 0004 roda. A linha antiga tem de sair migrada com
// microphone_always_ready = 0 (o padrão do ADR-015: o microfone só abre
// durante o ditado), sem perder nada do que já tinha, e o repositório a
// lê como false.
func TestSchema_LinhaAntesDa0004FicaComOMicrofoneSobDemanda(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Volta o banco ao estado da 0003: sem a coluna e sem o registro da 0004.
	if _, err := db.ExecContext(ctx, `ALTER TABLE local_settings DROP COLUMN microphone_always_ready`); err != nil {
		t.Fatalf("desfazer a 0004: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = '0004_local_settings_microfone'`); err != nil {
		t.Fatalf("esquecer a 0004: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO local_settings
		(id, hotkey, hold_threshold_ms, beep_enabled, overlay_position, asr_policy, protected_field_policy, local_port, text_mode, updated_at, version)
		VALUES (1, 'ctrl+win', 900, 0, '', 'local_only', 'block', 0, 'literal', '2026-09-29T00:00:00.000000000Z', 3)`); err != nil {
		t.Fatalf("inserir a linha antiga: %v", err)
	}

	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate com a linha antiga: %v", err)
	}

	got, err := storage.NewSQLiteSettingsRepository(db).Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MicrophoneAlwaysReady {
		t.Fatal("linha antiga migrada saiu com microphone_always_ready ligado, quero o padrão desligado")
	}
	if got.HoldThresholdMs != 900 || got.BeepEnabled || got.TextMode != "literal" || got.Version != 3 {
		t.Fatalf("a migração mexeu no que já estava gravado: %+v", got)
	}
}
