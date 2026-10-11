package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SQLiteSettingsRepository persiste as preferências na tabela de linha
// única local_settings (id = 1).
//
// É a implementação de produção do mesmo contrato que
// InMemorySettingsRepository define, e as duas passam pela MESMA suíte
// (settings_memory_test.go, paraCadaSettings).
type SQLiteSettingsRepository struct{ db *sql.DB }

// NewSQLiteSettingsRepository monta o repositório sobre db, que já deve
// ter passado pelo Migrator.
func NewSQLiteSettingsRepository(db *sql.DB) *SQLiteSettingsRepository {
	if db == nil {
		panic("storage: db nil")
	}
	return &SQLiteSettingsRepository{db: db}
}

var _ SettingsRepository = (*SQLiteSettingsRepository)(nil)

const colunasSettings = `hotkey, hold_threshold_ms, beep_enabled, overlay_position,
	microphone_device_id, autostart, asr_policy, local_model_id, local_context_enabled,
	protected_field_policy, local_port, text_mode, ai_provider, ai_model,
	microphone_always_ready, updated_at, version`

func (r *SQLiteSettingsRepository) Get(ctx context.Context) (Settings, error) {
	var (
		s                    Settings
		microfone, modelo    sql.NullString
		atualizado           string
		beep, auto, contexto int64
		semprePronto         int64
	)
	err := r.db.QueryRowContext(ctx, `SELECT `+colunasSettings+` FROM local_settings WHERE id = 1`).Scan(
		&s.Hotkey, &s.HoldThresholdMs, &beep, &s.OverlayPosition,
		&microfone, &auto, &s.ASRPolicy, &modelo, &contexto,
		&s.ProtectedFieldPolicy, &s.LocalPort, &s.TextMode, &s.AIProvider, &s.AIModel,
		&semprePronto, &atualizado, &s.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, ErrNotFound
	}
	if err != nil {
		return Settings{}, fmt.Errorf("storage: lendo local_settings: %w", err)
	}
	s.BeepEnabled, s.Autostart, s.LocalContextEnabled = beep != 0, auto != 0, contexto != 0
	s.MicrophoneAlwaysReady = semprePronto != 0
	s.MicrophoneDeviceID, s.LocalModelID = deTexto(microfone), deTexto(modelo)
	if s.UpdatedAt, err = deTempo(atualizado); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// Put grava com concorrência otimista. A versão vai no próprio comando —
// no WHERE do UPDATE, ou no conflito de id do INSERT —, e não num Get
// anterior: entre um e outro, outro escritor pode ter passado.
func (r *SQLiteSettingsRepository) Put(ctx context.Context, s Settings, baseVersion int64) (Settings, error) {
	agora := emTexto(time.Now())
	valores := []any{
		s.Hotkey, s.HoldThresholdMs, s.BeepEnabled, s.OverlayPosition,
		textoNulo(s.MicrophoneDeviceID), s.Autostart, s.ASRPolicy, textoNulo(s.LocalModelID),
		s.LocalContextEnabled, s.ProtectedFieldPolicy, s.LocalPort,
		s.TextMode, s.AIProvider, s.AIModel, s.MicrophoneAlwaysReady, agora,
	}

	var res sql.Result
	var err error
	if baseVersion == 0 {
		// Primeira gravação. "ON CONFLICT(id) DO NOTHING", e não "INSERT OR
		// IGNORE": o OR IGNORE engoliria também uma violação de NOT NULL, e
		// um dado inválido sairia daqui como conflito de versão.
		res, err = r.db.ExecContext(ctx, `INSERT INTO local_settings (id, `+colunasSettings+`)
			VALUES (1, ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?, 1)
			ON CONFLICT(id) DO NOTHING`, valores...)
	} else {
		res, err = r.db.ExecContext(ctx, `UPDATE local_settings SET
			hotkey = ?, hold_threshold_ms = ?, beep_enabled = ?, overlay_position = ?,
			microphone_device_id = ?, autostart = ?, asr_policy = ?, local_model_id = ?,
			local_context_enabled = ?, protected_field_policy = ?, local_port = ?,
			text_mode = ?, ai_provider = ?, ai_model = ?, microphone_always_ready = ?,
			updated_at = ?, version = version + 1
			WHERE id = 1 AND version = ?`, append(valores, baseVersion)...)
	}
	if err != nil {
		return Settings{}, fmt.Errorf("storage: gravando local_settings: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return Settings{}, fmt.Errorf("storage: gravando local_settings: %w", err)
	} else if n == 0 {
		return Settings{}, ErrVersionConflict
	}
	return r.Get(ctx)
}
