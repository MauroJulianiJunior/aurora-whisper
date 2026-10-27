-- Migration 0020: tabela labels.
--
-- O marcador de nota (ADR-022, "A nota" e "Sync"; fase N7): uma etiqueta que a
-- pessoa põe nas notas ("Mercado", "Trabalho"), entidade própria para existir
-- sem nota, ser renomeada num lugar só e sincronizar. As ligações com as notas
-- estão em note_labels (0022).
--   * id: o UUID v7 que o PC gerou. A chave primária é (user_id, id), por
--     usuário, como a do ditado (0003): o id vem do cliente.
--   * nome: de 1 a 50 caracteres, já limpo pelo servidor (as bordas sem
--     espaço, cada sequência de espaços virando um; cloudstore.NomeDoMarcador,
--     a mesma regra do PC). A nuvem NÃO impõe nome único: dois PCs podem criar
--     "Mercado" sem rede, e quem resolve é o PC, ao gravar o que veio.
--   * created_at: a data do PC; updated_at: quando o nome mudou por último
--     (o relógio do servidor). Sem DEFAULT: as datas vêm de quem grava.
--   * version e last_seq: como no ditado (0003). O nome do PC vence, sem selo
--     (não há conflito de marcador).
--
-- Apagar de vez um marcador apaga a linha e deixa a lápide em note_tombstones
-- (0023), com o evento em note_sync_events (0024).
--
-- Só aditiva (ADR-021, "As migrations"): nenhuma tabela anterior muda. O
-- "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE labels (
    id         UUID NOT NULL,
    user_id    UUID NOT NULL REFERENCES users (id),
    nome       TEXT NOT NULL CHECK (char_length(nome) BETWEEN 1 AND 50),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    version    BIGINT NOT NULL CHECK (version >= 1),
    last_seq   BIGINT NOT NULL CHECK (last_seq > 0),
    PRIMARY KEY (user_id, id),
    UNIQUE (user_id, last_seq)
);
