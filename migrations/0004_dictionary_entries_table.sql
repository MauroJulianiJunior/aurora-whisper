-- Migration 0004: tabela dictionary_entries.
--
-- Vocabulário custom do usuário (ver docs/ai-spec/05-data-model.md,
-- "DictionaryEntry"). UNIQUE (user_id, normalized_form) impede entradas
-- duplicadas da mesma forma canônica normalizada para o mesmo usuário.
--
-- version dá suporte a concorrência otimista entre dispositivos.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE dictionary_entries (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users (id),
    canonical_form  TEXT NOT NULL,
    normalized_form TEXT NOT NULL,
    language        TEXT,
    domain          TEXT,
    priority        INTEGER NOT NULL DEFAULT 0,
    active          BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    version         BIGINT NOT NULL DEFAULT 1,
    UNIQUE (user_id, normalized_form)
);
