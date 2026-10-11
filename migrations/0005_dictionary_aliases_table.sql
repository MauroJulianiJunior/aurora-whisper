-- Migration 0005: tabela dictionary_aliases.
--
-- Variações/apelidos reconhecidos para uma dictionary_entries (ver
-- docs/ai-spec/05-data-model.md, "DictionaryAlias"). ON DELETE CASCADE
-- em entry_id: um alias não tem sentido sem a entrada canônica à qual
-- pertence, então some junto quando a entrada é apagada.
--
-- UNIQUE (entry_id, normalized_alias) impede o mesmo alias normalizado
-- duplicado na mesma entrada.
--
-- Nota: a especificação não lista coluna version para esta entidade
-- (diferente de DictionaryEntry) — não foi adicionada aqui.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE dictionary_aliases (
    id               UUID PRIMARY KEY,
    entry_id         UUID NOT NULL REFERENCES dictionary_entries (id) ON DELETE CASCADE,
    alias            TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    phonetic_key     TEXT,
    source           TEXT NOT NULL,
    usage_count      INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entry_id, normalized_alias)
);

-- Suporte a lookup/cascade por entrada.
CREATE INDEX idx_dictionary_aliases_entry_id ON dictionary_aliases (entry_id);
