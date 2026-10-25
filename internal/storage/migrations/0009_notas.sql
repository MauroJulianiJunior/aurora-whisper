-- 0009: as notas e os marcadores locais (ADR-022, "Notas", fase N1,
-- 25-out-2026).
--
-- NÃO destrutiva. Só nascem três tabelas novas e os índices delas, vazias.
-- Nenhuma linha existente muda, e nenhuma tabela que já existe ganha coluna.
--
-- O SYNC NÃO ENTRA NESTA FASE. As colunas sync_state, server_version e
-- last_sync_error existem para a fase N7 (sync das notas e marcadores), mas o
-- motor de sync (internal/sync) ainda não conhece estas entidades: as linhas
-- ficam com sync_state = 'pending' e NENHUM marcador é posto em sync_outbox
-- para nota ou marcador (um marcador de tipo desconhecido poderia travar o
-- sync que já roda). A N7 faz o envio inicial a partir destas linhas.
--
-- A nota NÃO tem a retenção de 6 meses do histórico (ADR-022, "A nota"): só a
-- lixeira de 30 dias (purge_at), expurgada no mesmo ciclo dos ditados
-- (internal/agent/expurgo).
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- local_notes: a nota. O corpo é texto, uma linha por bloco, com um prefixo
-- que diz o tipo ("# ", "- ", "- [ ] ", "- [x] ", "1. "); o banco não
-- interpreta nada disso. A cor é um de oito nomes fixos (amarelo, laranja,
-- vermelho, rosa, roxo, azul, verde, cinza) ou nula; a lista fechada é
-- conferida em Go (storage.CoresDaNota), e não por CHECK, para uma cor nova
-- não pedir migration que recria a tabela.
CREATE TABLE local_notes (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    titulo           TEXT NOT NULL DEFAULT '',
    corpo            TEXT NOT NULL DEFAULT '',
    cor              TEXT,
    fixada           INTEGER NOT NULL DEFAULT 0 CHECK (fixada IN (0, 1)),
    arquivada        INTEGER NOT NULL DEFAULT 0 CHECK (arquivada IN (0, 1)),
    -- O ditado de que a nota nasceu ("Virar nota", N3). Só o id, sem chave
    -- estrangeira: o ditado pode sair do histórico sem levar a nota.
    ditado_de_origem TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    deleted_at       TEXT,
    purge_at         TEXT,
    version          INTEGER NOT NULL DEFAULT 1,
    sync_state       TEXT NOT NULL DEFAULT 'pending',
    server_version   INTEGER,
    last_sync_error  TEXT
);

-- A lista das notas (ativas e Arquivo): fixadas primeiro, depois a última
-- edição.
CREATE INDEX idx_local_notes_lista
    ON local_notes (user_id, arquivada, fixada DESC, updated_at DESC)
    WHERE deleted_at IS NULL;

-- A lixeira: o que foi apagado por último vem primeiro.
CREATE INDEX idx_local_notes_lixeira
    ON local_notes (user_id, deleted_at DESC)
    WHERE deleted_at IS NOT NULL;

-- O expurgo: o que já venceu na lixeira.
CREATE INDEX idx_local_notes_purge
    ON local_notes (purge_at)
    WHERE purge_at IS NOT NULL;

-- local_labels: o marcador, entidade própria (existe sem nota, é renomeado
-- num lugar só). nome_dobrado é o nome sem diferenciar maiúscula nem acento,
-- e o índice único abaixo garante um nome por pessoa entre os não apagados;
-- quem o calcula é o Go (storage.nomeDoMarcador). Apagar o marcador é soft
-- (deleted_at): tira o marcador das notas e não apaga nota.
CREATE TABLE local_labels (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL,
    nome           TEXT NOT NULL,
    nome_dobrado     TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    deleted_at     TEXT,
    version        INTEGER NOT NULL DEFAULT 1,
    sync_state     TEXT NOT NULL DEFAULT 'pending',
    server_version INTEGER
);

CREATE UNIQUE INDEX idx_local_labels_nome
    ON local_labels (user_id, nome_dobrado)
    WHERE deleted_at IS NULL;

-- local_note_labels: quais marcadores cada nota tem. Sai junto com a nota
-- (expurgo, apagar de vez) e com o marcador (o soft delete do marcador
-- apaga as linhas na própria transação).
CREATE TABLE local_note_labels (
    note_id  TEXT NOT NULL REFERENCES local_notes (id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES local_labels (id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, label_id)
);

-- Notas de um marcador (o filtro da lateral).
CREATE INDEX idx_local_note_labels_label
    ON local_note_labels (label_id);
