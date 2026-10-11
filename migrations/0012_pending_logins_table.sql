-- Migration 0012: tabela pending_logins.
--
-- O login Google em andamento (ver docs/ai-spec/05-data-model.md,
-- "LoginPendente", e docs/adr/ADR-019-nuvem-e-celular.md, "Login").
--
-- O state do OAuth fica NO SERVIDOR, com o verificador do PKCE e o nonce, e
-- vale uma vez. O app chama POST /auth/start; a resposta grava o cookie
-- __Host-pre (5 min, HttpOnly, Secure, SameSite=Lax) e devolve a URL do
-- Google. No callback, o servidor acha a linha pelo state, confere o cookie
-- e marca used_at no MESMO comando que a lê
-- (UPDATE ... SET used_at = now() WHERE state_hash = $1 AND used_at IS NULL
-- AND expires_at > now() RETURNING ...): uso único, sem corrida.
--
-- Esta linha existe ANTES de o usuário ser conhecido, por isso não tem
-- user_id.
--   * state_hash: SHA-256 do state (32 bytes). O state em claro só vai ao
--     Google e volta no callback.
--   * pre_hash: SHA-256 do valor do cookie __Host-pre (32 bytes): o vínculo
--     entre o navegador que começou o login e o que o termina.
--   * code_verifier e nonce: efêmeros (5 minutos), em claro, porque o
--     servidor precisa deles inteiros para trocar o código e validar o ID
--     token. Vencem e são apagados pela arrumação.
--
--   * return_to: para onde o callback leva a pessoa quando o login termina
--     (por exemplo, a página de aprovação do pareamento, que exige sessão e
--     manda entrar antes). Fica aqui, no servidor, e não em cookie nem no
--     state, junto do resto do login em andamento.
--     É um CAMINHO da própria origem, nunca uma URL: começa com uma barra,
--     não com duas (que o navegador leria como outro site), e só tem
--     caracteres de caminho e de query, sem barra invertida, espaço nem
--     quebra de linha. O CHECK é a segunda guarda: a primeira é a
--     validação do POST /auth/start (F3), que é quem recusa o resto. O
--     padrão é "/", o app.
--
-- Guardar o state aqui, e não só em cookie, deixa entrar depois o código de
-- 6 dígitos do iPhone sem refazer o login (ADR-019, "iPhone, depois do MVP").
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE pending_logins (
    state_hash    BYTEA PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    pre_hash      BYTEA NOT NULL CHECK (octet_length(pre_hash) = 32),
    code_verifier TEXT NOT NULL,
    nonce         TEXT NOT NULL,
    return_to     TEXT NOT NULL DEFAULT '/' CHECK (char_length(return_to) BETWEEN 1 AND 256 AND return_to ~ '^/[A-Za-z0-9._~/?&=%:@+-]*$' AND return_to NOT LIKE '//%'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);

-- A arrumação dos logins vencidos.
CREATE INDEX idx_pending_logins_expires_at ON pending_logins (expires_at);
