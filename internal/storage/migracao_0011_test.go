package storage_test

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// TestSchema_LinhaAntesDa0011FicaComOGatilhoDaNotaLigado: uma instalação que já
// tinha preferências gravadas sai da 0011 com o "Ditar para nota" ligado (o
// padrão da ADR-022, E1), sem perder nada do que já estava gravado.
func TestSchema_LinhaAntesDa0011FicaComOGatilhoDaNotaLigado(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE local_settings DROP COLUMN note_trigger_enabled`); err != nil {
		t.Fatalf("desfazer a 0011: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = '0011_local_settings_gatilho_da_nota'`); err != nil {
		t.Fatalf("esquecer a 0011: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO local_settings
		(id, hotkey, hold_threshold_ms, beep_enabled, overlay_position, asr_policy, protected_field_policy, local_port, text_mode, microphone_always_ready, updated_at, version)
		VALUES (1, 'ctrl+win', 900, 0, '', 'local_only', 'block', 0, 'literal', 1, '2026-10-20T00:00:00.000000000Z', 4)`); err != nil {
		t.Fatalf("inserir a linha antiga: %v", err)
	}
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate com a linha antiga: %v", err)
	}
	repo := storage.NewSQLiteSettingsRepository(db)
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.NoteTriggerEnabled {
		t.Fatal("a linha antiga saiu com o gatilho da nota desligado, quero o padrão ligado")
	}
	if got.HoldThresholdMs != 900 || !got.MicrophoneAlwaysReady || got.TextMode != "literal" || got.Version != 4 {
		t.Fatalf("a migração mexeu no que já estava gravado: %+v", got)
	}

	// Desligar e ler de volta.
	got.NoteTriggerEnabled = false
	salvo, err := repo.Put(ctx, got, got.Version)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if salvo.NoteTriggerEnabled {
		t.Fatal("desligado, voltou ligado")
	}
}
