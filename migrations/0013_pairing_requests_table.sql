-- Migration 0013: tabela pairing_requests.
--
-- O pedido de pareamento de um PC (ver docs/ai-spec/05-data-model.md,
-- "PedidoDePareamento", e docs/adr/ADR-019-nuvem-e-celular.md, "Conectar o
-- PC"): retorno para 127.0.0.1 com PKCE (RFC 8252, §7.3), sem consulta
-- repetida.
--
-- Fluxo: o Agent chama POST /device-auth/start (sem sessão) com o desafio
-- do PKCE, o nome do PC e a porta do painel local; a linha nasce SEM
-- aprovação. A pessoa abre a página de aprovação com a sessão dela e
-- aprova: approved_by e approved_at são preenchidos junto com code_hash, o
-- SHA-256 do code de uso único que o redirecionamento leva a
-- http://127.0.0.1:<redirect_port>. No POST /device-auth/exchange, o
-- servidor confere o code_verifier contra code_challenge e marca used_at
-- no mesmo comando (uso único). O token do aparelho vai para device_tokens
-- (0014), só como HMAC.
--
-- O state do retorno (http://127.0.0.1:<porta>/nuvem/retorno?code=...&state=...)
-- é o próprio id desta linha, que o Agent recebe como "pedido" na resposta do
-- start e que o painel local confere: não há coluna de state. Quem protege a
-- troca do code é o PKCE (o code_verifier, que só o Agent que começou tem); o
-- state só liga o retorno ao pedido em curso. Proposta da F1: a F6 confirma
-- (docs/ai-spec/05-data-model.md, "PedidoDePareamento").
--
-- A linha é escrita por um endpoint SEM sessão: por isso os textos têm
-- limite de tamanho no banco, e a porta é uma porta alta (o painel local
-- do Agent escuta numa porta que o sistema escolhe, sempre >= 1024).
--   * code_challenge: o desafio do PKCE (S256), em base64url.
--   * confirmation_code: o código curto, SÓ para conferência (a pessoa
--     compara o que a página e o painel local mostram). Não protege nada
--     sozinho.
--   * install_id, device_name, platform, app_version: o que o Agent diz de
--     si; viram uma linha de devices na aprovação.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE pairing_requests (
    id                UUID PRIMARY KEY,
    code_challenge    TEXT NOT NULL CHECK (char_length(code_challenge) BETWEEN 1 AND 128),
    install_id        TEXT NOT NULL CHECK (char_length(install_id) BETWEEN 1 AND 128),
    device_name       TEXT NOT NULL CHECK (char_length(device_name) BETWEEN 1 AND 80),
    platform          TEXT NOT NULL CHECK (char_length(platform) BETWEEN 1 AND 32),
    app_version       TEXT CHECK (char_length(app_version) <= 64),
    redirect_port     INTEGER NOT NULL CHECK (redirect_port BETWEEN 1024 AND 65535),
    confirmation_code TEXT NOT NULL CHECK (char_length(confirmation_code) BETWEEN 1 AND 16),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL,
    approved_by       UUID REFERENCES users (id),
    approved_at       TIMESTAMPTZ,
    code_hash         BYTEA UNIQUE CHECK (octet_length(code_hash) = 32),
    used_at           TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    CHECK ((approved_by IS NULL) = (approved_at IS NULL)),
    CHECK ((approved_by IS NULL) = (code_hash IS NULL)),
    CHECK (used_at IS NULL OR approved_at IS NOT NULL)
);

-- A arrumação dos pedidos vencidos.
CREATE INDEX idx_pairing_requests_expires_at ON pairing_requests (expires_at);
