-- Migration 0016: tabela learned_pairs.
--
-- O aprendido: o par curto que o painel do PC aprendeu, uma variante que o
-- ASR escreve ("sf") e o termo certo ("ECF") (ver
-- docs/ai-spec/05-data-model.md, "Aprendido", e
-- docs/adr/ADR-019-nuvem-e-celular.md, "O aprendido" e decisão 7). É a
-- única parte do dicionário que sincroniza na etapa 5: o dicionario.json
-- continua a fonte da verdade em cada PC, e a Correction com a frase
-- inteira não sobe (ADR-017).
--
-- O id é um UUID derivado do hash da variante normalizada: o MESMO em todo
-- PC, sem depender do usuario do arquivo. Por isso a chave primária é
-- (user_id, id): dois usuários que aprendem a mesma variante têm o mesmo id.
-- normalized_variant repete essa identidade em texto, com UNIQUE por
-- usuário, para o banco recusar duas linhas para a mesma variante mesmo que
-- um id saia diferente.
--   * variant e term: o par. Até 80 caracteres (o teto de integridade do
--     dicionario.json; a regra de "até 4 palavras" é do motor de sugestão).
--   * restricted e domain: a restrição que a variante herdou do termo e o
--     NOME do domínio. Restrito sem domínio nunca age e é recusado pelo
--     dicionário, e o CHECK impede o banco de guardar isso. O mapa de
--     domínios para executáveis NÃO sobe: ele diz que programas a pessoa
--     usa (ADR-019, "O que nunca vai").
--   * undone_at: quando foi desfeito. A linha continua (desfazer em outro
--     aparelho remove o par, e um reaprendizado offline depois do desfazer
--     precisa ser reconhecido). Reaprender é a mesma linha, com undone_at
--     limpo e a versão mais um.
--   * version e last_seq: como em transcriptions (0003).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE learned_pairs (
    id                 UUID NOT NULL,
    user_id            UUID NOT NULL REFERENCES users (id),
    variant            TEXT NOT NULL CHECK (char_length(variant) BETWEEN 1 AND 80),
    normalized_variant TEXT NOT NULL CHECK (char_length(normalized_variant) BETWEEN 1 AND 80),
    term               TEXT NOT NULL CHECK (char_length(term) BETWEEN 1 AND 80),
    restricted         BOOLEAN NOT NULL DEFAULT false,
    domain             TEXT CHECK (char_length(domain) BETWEEN 1 AND 80),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    undone_at          TIMESTAMPTZ,
    version            BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    last_seq           BIGINT NOT NULL CHECK (last_seq > 0),
    PRIMARY KEY (user_id, id),
    UNIQUE (user_id, normalized_variant),
    UNIQUE (user_id, last_seq),
    CHECK (NOT restricted OR domain IS NOT NULL)
);
