-- Migration 0007: tabela user_settings.
--
-- Preferências compatíveis entre dispositivos, sincronizadas na nuvem
-- (ver docs/ai-spec/05-data-model.md, "UserSettings"). user_id é PK e FK
-- ao mesmo tempo: no máximo uma linha de configurações por usuário.
--
-- settings é jsonb para acomodar preferências que evoluem sem exigir
-- migration a cada novo campo — o schema de validação vive na
-- aplicação, não no banco.
--
-- version dá suporte a concorrência otimista entre dispositivos.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE user_settings (
    user_id    UUID PRIMARY KEY REFERENCES users (id),
    settings   JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    BIGINT NOT NULL DEFAULT 1
);
