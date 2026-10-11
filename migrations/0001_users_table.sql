-- Migration 0001: tabela users.
--
-- Base de identidade do backend cloud (ver docs/ai-spec/05-data-model.md,
-- seção "PostgreSQL" -> "User"). Toda outra tabela deste diretório
-- referencia user_id para isolar dados por usuário (AC-035, NFR-012,
-- ameaça THR-004/IDOR): user_id nunca deve vir do payload da requisição,
-- sempre da sessão autenticada — essa é uma regra de aplicação, não de
-- schema, mas o FK aqui garante que todo user_id gravado corresponde a
-- um usuário real.
--
-- Login (ADR-019, F3): a chave do usuário é o google_sub, nunca o e-mail. O
-- sub fica fixado no primeiro login de um e-mail de CONTAS_PERMITIDAS, e o
-- mesmo e-mail com outro sub é recusado (o UNIQUE de email é a segunda
-- guarda; a primeira é o servidor). O e-mail é guardado em minúsculas, como
-- o servidor o compara com a lista: o CHECK impede que "Mauro@" e "mauro@"
-- virem duas linhas. O sub do Google tem até 255 caracteres (OpenID Connect
-- Core, 2: "MUST NOT exceed 255 ASCII characters"), e vazio não é sub.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE users (
    id           UUID PRIMARY KEY,
    google_sub   TEXT NOT NULL UNIQUE CHECK (char_length(google_sub) BETWEEN 1 AND 255),
    email        TEXT NOT NULL UNIQUE CHECK (email = lower(email)),
    display_name TEXT,
    avatar_url   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at  TIMESTAMPTZ
);
