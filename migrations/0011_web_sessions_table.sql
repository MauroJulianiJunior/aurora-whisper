-- Migration 0011: tabela web_sessions.
--
-- As sessões do navegador (o painel em modo nuvem e o app do celular): ver
-- docs/ai-spec/05-data-model.md, "WebSession", e
-- docs/adr/ADR-019-nuvem-e-celular.md, "Sessão".
--
-- O id da sessão (256 bits aleatórios) só existe no cookie __Host-sessao.
-- O banco guarda APENAS o HMAC-SHA256 dele com SESSION_SECRET
-- (token_hmac, 32 bytes): quem lê o banco não ganha sessão. O id desta
-- tabela (UUID v7) é só o identificador que a lista de sessões e o
-- "derrubar sessão" usam; ele não autentica nada.
--
-- Expira em 30 dias sem uso (sugestão do plano; a F3 confirma): expires_at
-- anda para a frente a cada uso. Revogar (sair, ou derrubar uma sessão da
-- lista) é preencher revoked_at.
--
-- label é um rótulo curto do aparelho para a lista de sessões (por exemplo,
-- "Android · Chrome"), nunca o user-agent inteiro: o limite de tamanho é
-- do banco.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE web_sessions (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users (id),
    token_hmac   BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hmac) = 32),
    label        TEXT NOT NULL CHECK (char_length(label) BETWEEN 1 AND 64),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);

-- A lista de sessões de um usuário.
CREATE INDEX idx_web_sessions_user_id ON web_sessions (user_id);

-- A arrumação das sessões vencidas.
CREATE INDEX idx_web_sessions_expires_at ON web_sessions (expires_at);
