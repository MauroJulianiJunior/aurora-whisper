-- Migration 0001: schema inicial do SQLite local do Windows Agent.
--
-- Espelha o subconjunto sincronizável do modelo Postgres (ver
-- docs/ai-spec/05-data-model.md, seção "SQLite") mais o controle de
-- sincronização (sync_outbox, sync_state) e as preferências locais
-- (local_settings).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em internal/storage/schema_test.go.
--
-- Nota: PRAGMA foreign_keys é no-op dentro de uma transação em SQLite,
-- então NÃO é ligado aqui — o Migrator liga antes de abrir a transação
-- de migração (ver internal/storage/migrator.go).

-- local_transcriptions é o espelho local de Transcription (Postgres) mais
-- os campos de sincronização (sync_state, server_version,
-- last_sync_error). deleted_at/purge_at implementam a lixeira (30 dias)
-- e a retenção de 6 meses descritas em "Retenção".
CREATE TABLE local_transcriptions (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    device_id       TEXT,
    source_platform TEXT NOT NULL,
    raw_text        TEXT NOT NULL,
    final_text      TEXT NOT NULL,
    mode            TEXT NOT NULL,
    language        TEXT,
    asr_provider    TEXT NOT NULL,
    asr_model       TEXT,
    duration_ms     INTEGER,
    processing_ms   INTEGER,
    confidence      REAL,
    target_app      TEXT,
    status          TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT,
    purge_at        TEXT,
    version         INTEGER NOT NULL DEFAULT 1,
    sync_state      TEXT NOT NULL DEFAULT 'pending',
    server_version  INTEGER,
    last_sync_error TEXT
);

-- Histórico ativo (AC-011): mais recentes primeiro, apenas não excluídos.
CREATE INDEX idx_local_transcriptions_active
    ON local_transcriptions (user_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- Lixeira: itens excluídos, mais recentes primeiro.
CREATE INDEX idx_local_transcriptions_trash
    ON local_transcriptions (user_id, deleted_at DESC)
    WHERE deleted_at IS NOT NULL;

-- Suporte a sincronização incremental por updated_at.
CREATE INDEX idx_local_transcriptions_updated_at
    ON local_transcriptions (user_id, updated_at);

-- local_dictionary_entries espelha DictionaryEntry (Postgres).
CREATE TABLE local_dictionary_entries (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    canonical_form  TEXT NOT NULL,
    normalized_form TEXT NOT NULL,
    language        TEXT,
    domain          TEXT,
    priority        INTEGER NOT NULL DEFAULT 0,
    active          INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    sync_state      TEXT NOT NULL DEFAULT 'pending',
    server_version  INTEGER,
    last_sync_error TEXT,
    UNIQUE (user_id, normalized_form)
);

CREATE INDEX idx_local_dictionary_entries_updated_at
    ON local_dictionary_entries (user_id, updated_at);

-- local_dictionary_aliases espelha DictionaryAlias (Postgres).
CREATE TABLE local_dictionary_aliases (
    id               TEXT PRIMARY KEY,
    entry_id         TEXT NOT NULL REFERENCES local_dictionary_entries (id) ON DELETE CASCADE,
    alias            TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    phonetic_key     TEXT,
    source           TEXT NOT NULL,
    usage_count      INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    version          INTEGER NOT NULL DEFAULT 1,
    sync_state       TEXT NOT NULL DEFAULT 'pending',
    server_version   INTEGER,
    last_sync_error  TEXT,
    UNIQUE (entry_id, normalized_alias)
);

CREATE INDEX idx_local_dictionary_aliases_entry_id
    ON local_dictionary_aliases (entry_id);

-- local_corrections espelha Correction (Postgres). transcription_id usa
-- ON DELETE SET NULL: apagar uma transcrição nunca deve apagar o
-- aprendizado da correção associada (exige PRAGMA foreign_keys = ON,
-- ligado pelo Migrator — ver internal/storage/migrator.go).
CREATE TABLE local_corrections (
    id                TEXT PRIMARY KEY,
    user_id           TEXT NOT NULL,
    transcription_id  TEXT REFERENCES local_transcriptions (id) ON DELETE SET NULL,
    from_text         TEXT NOT NULL,
    to_text           TEXT NOT NULL,
    normalized_from   TEXT NOT NULL,
    normalized_to     TEXT NOT NULL,
    language          TEXT,
    confirmed_by_user INTEGER NOT NULL DEFAULT 0,
    use_count         INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    version           INTEGER NOT NULL DEFAULT 1,
    sync_state        TEXT NOT NULL DEFAULT 'pending',
    server_version    INTEGER,
    last_sync_error   TEXT
);

CREATE INDEX idx_local_corrections_transcription_id
    ON local_corrections (transcription_id);

-- local_settings guarda as preferências locais do Agent (ver
-- docs/ai-spec/05-data-model.md, "local_settings"). É uma tabela de
-- linha única: o CHECK força id = 1, então só existe uma configuração
-- local por instalação do Agent.
CREATE TABLE local_settings (
    id                     INTEGER PRIMARY KEY CHECK (id = 1),
    hotkey                 TEXT NOT NULL,
    hold_threshold_ms      INTEGER NOT NULL,
    beep_enabled           INTEGER NOT NULL DEFAULT 1,
    overlay_position       TEXT NOT NULL,
    microphone_device_id   TEXT,
    autostart              INTEGER NOT NULL DEFAULT 0,
    asr_policy             TEXT NOT NULL,
    local_model_id         TEXT,
    local_context_enabled  INTEGER NOT NULL DEFAULT 0,
    protected_field_policy TEXT NOT NULL,
    local_port             INTEGER NOT NULL,
    updated_at             TEXT NOT NULL,
    version                INTEGER NOT NULL DEFAULT 1
);

-- sync_outbox é a fila de mutações locais pendentes de envio ao backend
-- cloud (ver docs/ai-spec/04-system-design.md, "Sync").
CREATE TABLE sync_outbox (
    mutation_id      TEXT PRIMARY KEY,
    entity_type      TEXT NOT NULL,
    entity_id        TEXT NOT NULL,
    operation        TEXT NOT NULL,
    base_version     INTEGER,
    payload_json     TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    attempt_count    INTEGER NOT NULL DEFAULT 0,
    next_attempt_at  TEXT,
    last_error_code  TEXT
);

CREATE INDEX idx_sync_outbox_next_attempt_at
    ON sync_outbox (next_attempt_at);

-- sync_state guarda o cursor de sincronização por dispositivo.
CREATE TABLE sync_state (
    user_id          TEXT NOT NULL,
    device_id        TEXT NOT NULL,
    last_server_seq  INTEGER NOT NULL DEFAULT 0,
    last_success_at  TEXT,
    PRIMARY KEY (user_id, device_id)
);
