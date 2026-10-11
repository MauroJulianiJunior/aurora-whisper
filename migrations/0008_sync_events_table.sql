-- Migration 0008: tabela sync_events.
--
-- Log append-only de mutações aplicadas, uma linha por mudança de entidade
-- (ver docs/ai-spec/05-data-model.md, "SyncEvent", e
-- docs/adr/ADR-019-nuvem-e-celular.md, "Gravação única" e "Contrato de
-- sync, por usuário"). Toda mudança grava a entidade e o evento na mesma
-- transação (ADR-009).
--
-- Corrigida no lugar pela F1 da etapa 5 (ADR-019; esta migration nunca
-- rodou em lugar nenhum):
--
--   * O seq é POR USUÁRIO: a chave primária é (user_id, seq), e o número
--     sai de user_sync_counters (0010) pelo primeiro comando da transação:
--     UPDATE user_sync_counters SET last_seq = last_seq + 1
--     WHERE user_id = $1 RETURNING last_seq. A trava da linha do contador
--     fica até o commit, então a ordem do seq é a ordem dos commits, sem
--     buraco, e um rollback devolve o número.
--
--   * O BIGSERIAL global SAI. Esta migration dizia que ele bastava como
--     cursor do pull ("cada linha nova recebe um seq estritamente
--     crescente, garantido pelo próprio Postgres"). Isso é verdade para a
--     atribuição do número, e é falso para a VISIBILIDADE: uma sequence
--     entrega o número antes do commit e não devolve no rollback, então,
--     com transações concorrentes, o evento de seq 11 pode ficar visível
--     antes do de seq 10, e um cliente com cursor em 11 pularia o 10 para
--     sempre. O contador por usuário trava até o commit e não tem esse
--     defeito.
--
--   * device_id é o aparelho de ORIGEM da mudança (nulo quando veio do
--     celular, da retenção ou do "apagar meus dados"). Serve para mostrar
--     de que aparelho veio o ditado: o pull junta a entidade ao evento dela
--     por (user_id, last_seq) e devolve este device_id. NÃO serve para
--     descartar o eco no PC: isso se decide pela versão da entidade
--     (ADR-019, "Eco e mescla"). Atenção: a origem só existe enquanto o
--     evento existir. A entidade viva aponta para o seu evento por
--     (user_id, last_seq), e o learned_pairs não tem coluna de aparelho. Se
--     a arrumação por idade apagasse o evento que é o last_seq de uma
--     entidade viva, a origem sumiria, e nada no esquema impede isso: é regra
--     do job de retenção (F9c; ADR-019, "Decisões da F9") só apagar o evento
--     cujo seq nenhuma entidade viva guarda como last_seq (o NOT EXISTS nos
--     dois UNIQUE (user_id, last_seq), o do aprendido desfeito inclusive).
--
--   * Sem payload: o pull lê o estado atual das entidades e as lápides,
--     não o evento. Por isso a idade do evento não importa para o sync, e
--     o job de retenção apaga o evento órfão com changed_at de mais de 90
--     dias (F9c), com a ressalva do device_id acima: o evento que é o
--     last_seq de uma entidade viva fica. Os de lápide e os de entidade que
--     o job apagou são órfãos e saem. changed_at é o relógio de quem gravou
--     (o servidor passa o dele; o DEFAULT só vale para um INSERT à mão).
--
-- operation é upsert (a entidade existe, no estado atual, inclusive na
-- lixeira) ou tombstone (a entidade foi apagada de vez e deixou lápide,
-- 0015). entity_type é a lista fechada da etapa 5: transcription ou
-- learned (0016).
--
-- Esta tabela não tem version: é um log imutável, não uma entidade
-- editável — não há concorrência otimista para aplicar aqui.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE sync_events (
    user_id        UUID NOT NULL REFERENCES users (id),
    seq            BIGINT NOT NULL CHECK (seq > 0),
    entity_type    TEXT NOT NULL CHECK (entity_type IN ('transcription', 'learned')),
    entity_id      UUID NOT NULL,
    operation      TEXT NOT NULL CHECK (operation IN ('upsert', 'tombstone')),
    entity_version BIGINT NOT NULL CHECK (entity_version >= 1),
    device_id      UUID,
    changed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, seq),
    FOREIGN KEY (user_id, device_id) REFERENCES devices (user_id, id)
);

-- A chave primária (user_id, seq) é o índice da junção do pull (a entidade e
-- o evento do last_seq dela); o pull não lê esta tabela por nenhum outro
-- caminho. O índice abaixo é o da arrumação por idade do job de retenção
-- (F9c; ADR-019, "Decisões da F9"), por usuário: o DELETE ... WHERE user_id =
-- $1 AND changed_at <= $2. Corrigida no lugar pela F9c (a regra da decisão 13
-- vale até o deploy, F11): um banco persistente que já aplicou a 0008 de antes
-- recusa o migrate por checksum, e a saída é recriá-lo.
CREATE INDEX idx_sync_events_changed_at ON sync_events (user_id, changed_at);
