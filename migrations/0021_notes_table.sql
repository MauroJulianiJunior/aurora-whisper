-- Migration 0021: tabela notes.
--
-- A nota (ADR-022, "A nota" e "Sync"; fase N7): o ditado que vira anotação, com
-- lista, marcador e cor. O corpo é texto, uma linha por bloco, com um prefixo
-- que diz o tipo ("- [ ] item" é a lista de marcar). A nota NÃO tem a retenção
-- de 6 meses do histórico: fica até a pessoa apagar.
--   * id: o UUID v7 que o PC gerou; a chave primária é (user_id, id), por
--     usuário, como a do ditado (0003).
--   * titulo (até 1 KiB) e corpo (até 64 KiB, o teto do texto do ditado),
--     sempre presentes (o vazio é ''); os tetos são os do PC
--     (internal/storage, nota.go), em bytes.
--   * cor: um dos oito nomes (a cor é um nome, não um hex: cada tema tem o seu
--     tom), ou nula.
--   * fixada e arquivada: o último que escreve vence, sem selo.
--   * posicao: a ordem manual da N2b, uma chave de índice fracionário (de 1 a
--     128 caracteres de 0-9A-Za-z, sem '0' no fim). COLLATE "C": a ordem é a
--     dos bytes, a mesma do Go e do PC; com o collation do banco, "a" e "B"
--     sairiam trocados. A lista sai por fixada desc, posicao, id.
--   * ditado_de_origem: o ditado de que a nota nasceu, ou nulo. Sem chave
--     estrangeira: o ditado sai com a retenção de 6 meses, e a nota fica.
--   * created_at é a do PC; updated_at muda quando o conteúdo muda.
--     deleted_at e purge_at são a lixeira (30 dias depois de ir para ela).
--   * version, content_version e trash_version, content_device_id e
--     trash_device_id: o desenho do ditado (text_version e trash_version, na
--     0003). O conteúdo é o título e o corpo juntos.
--   * conflict_titulo, conflict_corpo, conflict_device_id e conflict_at: o
--     selo (a versão do conteúdo que perdeu um conflito). O título, o corpo e
--     a data vêm juntos ou não vêm; o aparelho só com eles (nulo quando a
--     versão não veio de um aparelho).
--   * As colunas de aparelho são chaves estrangeiras compostas para devices
--     (user_id, id): o banco recusa o aparelho de outro usuário.
--
-- Os marcadores da nota estão em note_labels (0022). Apagar de vez uma nota
-- apaga a linha (e as ligações, em cascata) e deixa a lápide em
-- note_tombstones (0023), com o evento em note_sync_events (0024).
--
-- Só aditiva (ADR-021, "As migrations"): nenhuma tabela anterior muda. O
-- "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE notes (
    id                 UUID NOT NULL,
    user_id            UUID NOT NULL REFERENCES users (id),
    titulo             TEXT NOT NULL CHECK (octet_length(titulo) <= 1024),
    corpo              TEXT NOT NULL CHECK (octet_length(corpo) <= 65536),
    cor                TEXT CHECK (cor IN ('amarelo', 'laranja', 'vermelho', 'rosa', 'roxo', 'azul', 'verde', 'cinza')),
    fixada             BOOLEAN NOT NULL,
    arquivada          BOOLEAN NOT NULL,
    posicao            TEXT COLLATE "C" NOT NULL CHECK (posicao ~ '^[0-9A-Za-z]{1,128}$' AND right(posicao, 1) <> '0'),
    ditado_de_origem   UUID,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    deleted_at         TIMESTAMPTZ,
    purge_at           TIMESTAMPTZ,
    version            BIGINT NOT NULL CHECK (version >= 1),
    content_version    BIGINT NOT NULL,
    trash_version      BIGINT NOT NULL,
    content_device_id  UUID,
    trash_device_id    UUID,
    last_seq           BIGINT NOT NULL CHECK (last_seq > 0),
    conflict_titulo    TEXT CHECK (octet_length(conflict_titulo) <= 1024),
    conflict_corpo     TEXT CHECK (octet_length(conflict_corpo) <= 65536),
    conflict_device_id UUID,
    conflict_at        TIMESTAMPTZ,
    PRIMARY KEY (user_id, id),
    UNIQUE (user_id, last_seq),
    FOREIGN KEY (user_id, content_device_id) REFERENCES devices (user_id, id),
    FOREIGN KEY (user_id, trash_device_id) REFERENCES devices (user_id, id),
    FOREIGN KEY (user_id, conflict_device_id) REFERENCES devices (user_id, id),
    CHECK (purge_at IS NULL OR deleted_at IS NOT NULL),
    CHECK (content_version BETWEEN 1 AND version),
    CHECK (trash_version BETWEEN 1 AND version),
    CHECK ((conflict_titulo IS NULL) = (conflict_at IS NULL)),
    CHECK ((conflict_corpo IS NULL) = (conflict_at IS NULL)),
    CHECK (conflict_at IS NOT NULL OR conflict_device_id IS NULL)
);

-- As notas fora da lixeira (as ativas e o arquivo), na ordem da lista.
CREATE INDEX idx_notes_list
    ON notes (user_id, arquivada, fixada DESC, posicao, id)
    WHERE deleted_at IS NULL;

-- A lixeira: a que foi para lá por último primeiro.
CREATE INDEX idx_notes_trash
    ON notes (user_id, deleted_at DESC)
    WHERE deleted_at IS NOT NULL;
