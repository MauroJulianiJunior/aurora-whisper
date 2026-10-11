-- Migration 0006: tabela corrections.
--
-- Aprendizado de correções feitas pelo usuário (ver
-- docs/ai-spec/05-data-model.md, "Correction"). transcription_id usa
-- ON DELETE SET NULL: apagar (ou expirar) uma transcrição nunca deve
-- apagar o aprendizado da correção associada, só perder a referência
-- (mesmo comportamento já validado para o SQLite local em
-- internal/storage/schema_test.go).
--
-- A chave primária de transcriptions é (user_id, id) (0003), então a chave
-- estrangeira é composta: (user_id, transcription_id). O ON DELETE SET NULL
-- leva a lista de colunas, ON DELETE SET NULL (transcription_id), porque o
-- user_id é NOT NULL e não pode ser zerado. Essa lista é do PostgreSQL 15 em
-- diante (o alvo é o 18, ADR-019, D5); com a coluna nula a chave não é
-- conferida, como antes.
--
-- version dá suporte a concorrência otimista entre dispositivos.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE corrections (
    id                UUID PRIMARY KEY,
    user_id           UUID NOT NULL REFERENCES users (id),
    transcription_id  UUID,
    from_text         TEXT NOT NULL,
    to_text           TEXT NOT NULL,
    normalized_from   TEXT NOT NULL,
    normalized_to     TEXT NOT NULL,
    language          TEXT,
    confirmed_by_user BOOLEAN NOT NULL DEFAULT false,
    use_count         INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           BIGINT NOT NULL DEFAULT 1,
    FOREIGN KEY (user_id, transcription_id) REFERENCES transcriptions (user_id, id) ON DELETE SET NULL (transcription_id)
);

-- Suporte à FK ON DELETE SET NULL e a lookups por transcrição.
CREATE INDEX idx_corrections_transcription_id ON corrections (transcription_id);

-- Suporte a listagem de correções por usuário.
CREATE INDEX idx_corrections_user_id ON corrections (user_id);
