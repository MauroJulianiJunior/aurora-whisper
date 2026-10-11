-- Migration 0002: tabela devices.
--
-- Um dispositivo (instalação do Windows Agent) por usuário (ver
-- docs/ai-spec/05-data-model.md, "Device"). UNIQUE (user_id, install_id)
-- impede que o mesmo install_id seja registrado duas vezes para o
-- mesmo usuário — install_id identifica a instalação local, não a
-- pessoa, e é gerado pelo Agent.
--
-- UNIQUE (user_id, id) é redundante com a PK de propósito (ADR-019, F1):
-- é o alvo das chaves estrangeiras compostas (user_id, device_id) das
-- tabelas que apontam para um aparelho. Com elas, o banco recusa um
-- ditado, um evento ou uma mutação do usuário A apontando para o
-- aparelho do usuário B (isolamento por usuário, AC-035, THR-004).
--
-- version dá suporte a concorrência otimista (ex.: revoke concorrente).
-- Revogar é preencher revoked_at; o token do aparelho mora em
-- device_tokens (0014), só como HMAC.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE devices (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users (id),
    install_id   TEXT NOT NULL,
    name         TEXT NOT NULL,
    platform     TEXT NOT NULL,
    app_version  TEXT,
    last_seen_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ,
    version      BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    UNIQUE (user_id, install_id),
    UNIQUE (user_id, id)
);

-- Suporte a listagem/lookup de dispositivos por usuário.
CREATE INDEX idx_devices_user_id ON devices (user_id);
