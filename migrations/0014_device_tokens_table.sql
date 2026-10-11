-- Migration 0014: tabela device_tokens.
--
-- O token de um aparelho pareado (ver docs/ai-spec/05-data-model.md, "Token
-- do aparelho", e docs/adr/ADR-019-nuvem-e-celular.md, "Token do
-- dispositivo"). O token é opaco, de 256 bits, e o banco guarda APENAS o
-- HMAC-SHA256 dele com DEVICE_TOKEN_PEPPER (token_hmac, 32 bytes): quem lê
-- o banco não ganha um token.
--
-- Fica numa tabela ao lado de devices (0002), e não em colunas dela, por
-- três motivos: o material secreto nunca vai junto num SELECT de aparelho
-- (o que a lista de aparelhos devolve); last_used_at é escrito a cada
-- chamada autenticada e não deve mexer na version do aparelho (a
-- concorrência otimista do renomear); e uma troca de token não toca no
-- aparelho.
--
-- Um token por aparelho (device_id UNIQUE): parear de novo o mesmo PC
-- (mesmo install_id) reaproveita a linha de devices e substitui o token. A
-- revogação é o revoked_at do aparelho: o token de um aparelho revogado
-- recebe 401. last_used_at alimenta a expiração dos tokens sem uso (o
-- prazo é da F6; sugestão: 180 dias).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE device_tokens (
    token_hmac   BYTEA PRIMARY KEY CHECK (octet_length(token_hmac) = 32),
    device_id    UUID NOT NULL UNIQUE REFERENCES devices (id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);
