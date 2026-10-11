-- Migration 0003: tabela transcriptions.
--
-- Fonte consolidada do histórico de ditados (ver
-- docs/ai-spec/05-data-model.md, "Transcription"). Guarda apenas texto
-- (raw_text/final_text) e metadados — nunca áudio, que só existe em RAM
-- durante o processamento (docs/ai-spec/09-security-privacy.md, "Áudio";
-- AC-028, AC-041) — e nunca contexto de janela/seleção de outras
-- aplicações, que é efêmero por design (AC-027).
--
-- Corrigida no lugar pela F1 da etapa 5 (ADR-019; esta migration nunca
-- rodou em lugar nenhum):
--   * target_app SAI. O nome do app em foco é contexto (REQ-025), o Agent
--     nunca o grava, e o servidor recusa o campo. migrations/schema_test.go
--     proíbe a volta dele.
--   * created_at NÃO tem DEFAULT: é a data do ditado, a do PC, e é por ela
--     que a retenção conta (6 meses no ativo, 30 dias na lixeira). Um
--     DEFAULT now() deixaria a hora em que a nuvem recebeu passar por data
--     do ditado sem ninguém ver.
--   * last_seq é o seq (por usuário, ver 0010 e 0008) da última mudança da
--     linha: o pull devolve o estado atual das linhas com last_seq maior
--     que o cursor. É único por usuário, porque cada mudança toma um seq
--     novo do contador do usuário e grava uma entidade só.
--   * conflict_final_text, conflict_device_id e conflict_at são a versão
--     que perdeu um conflito (o texto, o aparelho e quando): o selo. Ficam
--     no próprio item até a pessoa escolher uma versão; escolher limpa os
--     três. conflict_device_id é nulo quando a versão veio do celular, que
--     não é um aparelho pareado.
--   * (user_id, device_id) é chave estrangeira composta para devices
--     (0002): o aparelho tem de ser do mesmo usuário.
--   * A chave primária é (user_id, id), POR USUÁRIO, como a do aprendido
--     (0016), a das lápides (0015) e a das mutações (0009). O id do ditado é
--     o UUID v7 que o PC gerou: vem do cliente. Com a PK global, o mesmo
--     historico.db ligado a duas contas (a real e a de teste), ou um cliente
--     com defeito, faria o push de uma conta colidir com a linha de outra, e
--     o erro (ou a resposta) mostraria que o id existe em outra conta, o que
--     a ADR-019 já recusou para o mutation_id. Por usuário, o mesmo id em duas
--     contas são duas linhas, e o upsert do push é por (user_id, id) (a F2
--     lê a linha FOR UPDATE e faz o INSERT ou o UPDATE, sem ON CONFLICT).
--     Confirmado pelo Mauro em 05-out-2026 (o 05 dizia "id uuid PK";
--     ADR-019, "Decisões da F1").
--   * text_version e trash_version (corrigida no lugar pela F2, 05-out-2026;
--     vale até o deploy, F11): a versão em que o texto final mudou por
--     último e a versão em que a lixeira mudou por último (entrar ou sair).
--     O push precisa delas para saber o que o servidor mudou DEPOIS da base
--     que o PC conhecia: com elas, a lixeira de um lado e o texto do outro
--     são uma mescla (applied, mesclado), e só o texto dos dois lados é
--     conflito (ADR-019, "Conflito" e "Eco e mescla"). Sem elas, o servidor
--     só saberia que "alguma coisa" mudou e acusaria conflito à toa. Nunca
--     passam de version.
--   * text_device_id e trash_device_id (corrigida no lugar de novo pela F2,
--     na revisão de 05-out-2026): o aparelho que fez a última mudança do
--     texto e a da lixeira (nulos quando veio do celular). Depois de um
--     applied mesclado, o PC continua com a base velha (ADR-019, "Eco e
--     mescla"); sem saber quem mudou o campo por último, o servidor tomava
--     a mudança do próprio PC por uma de outro aparelho, e o restaurar
--     seguinte dele perdia para a lixeira dele mesmo. O campo cuja última
--     mudança veio do aparelho que manda a mutação conta como conhecido por
--     ele. Chaves estrangeiras compostas, como as outras colunas de aparelho.
--
-- deleted_at/purge_at implementam a lixeira (30 dias) e a retenção de
-- 6 meses descritas em "Retenção". version dá suporte a concorrência
-- otimista entre dispositivos sincronizando a mesma transcrição.
--
-- status segue o Agent: done ou error. mode é o que o Agent grava
-- (literal, smart ou polish; o histórico diz o que aconteceu, não o que
-- foi escolhido).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações. Isso é verificado em
-- migrations/schema_test.go.
CREATE TABLE transcriptions (
    id                  UUID NOT NULL,
    user_id             UUID NOT NULL REFERENCES users (id),
    device_id           UUID,
    source_platform     TEXT NOT NULL,
    raw_text            TEXT NOT NULL,
    final_text          TEXT NOT NULL,
    mode                TEXT NOT NULL CHECK (mode IN ('literal', 'smart', 'polish')),
    language            TEXT,
    asr_provider        TEXT NOT NULL,
    asr_model           TEXT,
    duration_ms         INTEGER CHECK (duration_ms >= 0),
    processing_ms       INTEGER CHECK (processing_ms >= 0),
    confidence          REAL CHECK (confidence >= 0 AND confidence <= 1),
    status              TEXT NOT NULL CHECK (status IN ('done', 'error')),
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ,
    purge_at            TIMESTAMPTZ,
    version             BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    text_version        BIGINT NOT NULL DEFAULT 1,
    trash_version       BIGINT NOT NULL DEFAULT 1,
    text_device_id      UUID,
    trash_device_id     UUID,
    last_seq            BIGINT NOT NULL CHECK (last_seq > 0),
    conflict_final_text TEXT,
    conflict_device_id  UUID,
    conflict_at         TIMESTAMPTZ,
    PRIMARY KEY (user_id, id),
    FOREIGN KEY (user_id, device_id) REFERENCES devices (user_id, id),
    FOREIGN KEY (user_id, conflict_device_id) REFERENCES devices (user_id, id),
    FOREIGN KEY (user_id, text_device_id) REFERENCES devices (user_id, id),
    FOREIGN KEY (user_id, trash_device_id) REFERENCES devices (user_id, id),
    UNIQUE (user_id, last_seq),
    CHECK (purge_at IS NULL OR deleted_at IS NOT NULL),
    CHECK (text_version BETWEEN 1 AND version),
    CHECK (trash_version BETWEEN 1 AND version),
    CHECK ((conflict_final_text IS NULL) = (conflict_at IS NULL)),
    CHECK (conflict_final_text IS NOT NULL OR conflict_device_id IS NULL)
);

-- Histórico ativo: mais recentes primeiro, apenas não excluídos.
CREATE INDEX idx_transcriptions_active
    ON transcriptions (user_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- Lixeira: itens excluídos, mais recentes primeiro.
CREATE INDEX idx_transcriptions_trash
    ON transcriptions (user_id, deleted_at DESC)
    WHERE deleted_at IS NOT NULL;

-- Suporte a sincronização incremental por updated_at.
CREATE INDEX idx_transcriptions_updated_at
    ON transcriptions (user_id, updated_at);
