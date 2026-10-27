-- Migration 0024: tabela note_sync_events.
--
-- O log de eventos das notas e dos marcadores de nota (ADR-022, N7): o mesmo
-- desenho da sync_events (0008), uma linha por mudança, gravada na mesma
-- transação da entidade, com o seq tirado do MESMO contador do usuário
-- (user_sync_counters, 0010). Um seq é de uma mudança só, numa das duas
-- tabelas: o pull junta as duas listas por seq.
--
-- Por que uma tabela à parte, e não a sync_events: o CHECK do entity_type de
-- lá só aceita 'transcription' e 'learned', e a 0008 já está em produção.
-- Alargar o CHECK exige um ALTER, e as migrations depois do deploy são só
-- aditivas, uma tabela por arquivo (ADR-021, "As migrations").
--
--   * device_id é o aparelho de ORIGEM da mudança (nulo quando não veio de um
--     aparelho). O pull junta a nota ou o marcador ao evento do last_seq dele
--     para mostrar de onde veio. Como na 0008, a origem só existe enquanto o
--     evento existir: o job de retenção só apaga o evento órfão vencido (90
--     dias) cujo seq nenhuma nota nem marcador vivo guarda como last_seq.
--   * Sem payload: o pull lê o estado atual das entidades e as lápides
--     (note_tombstones, 0023), não o evento.
--   * changed_at é o relógio de quem gravou, sem DEFAULT.
--
-- O "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE note_sync_events (
    user_id        UUID NOT NULL REFERENCES users (id),
    seq            BIGINT NOT NULL CHECK (seq > 0),
    entity_type    TEXT NOT NULL CHECK (entity_type IN ('note', 'label')),
    entity_id      UUID NOT NULL,
    operation      TEXT NOT NULL CHECK (operation IN ('upsert', 'tombstone')),
    entity_version BIGINT NOT NULL CHECK (entity_version >= 1),
    device_id      UUID,
    changed_at     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, seq),
    FOREIGN KEY (user_id, device_id) REFERENCES devices (user_id, id)
);

-- A arrumação por idade do job de retenção, por usuário (como a da 0008).
CREATE INDEX idx_note_sync_events_changed_at ON note_sync_events (user_id, changed_at);
