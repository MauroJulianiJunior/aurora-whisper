-- Migration 0023: tabela note_tombstones.
--
-- As lápides das notas e dos marcadores de nota (ADR-022, N7): apagar de vez
-- uma nota ou um marcador apaga a linha e deixa a lápide aqui, na mesma
-- transação, com o evento tombstone em note_sync_events (0024). O pull entrega
-- as lápides em ordem de seq, e o PC apaga a cópia dele; uma mudança para uma
-- entidade com lápide recebe "gone", e assim um PC parado não ressuscita a nota
-- apagada.
--
-- Por que uma tabela à parte, e não a tombstones (0015): o CHECK do
-- entity_type de lá só aceita 'transcription', e a 0015 já está em produção.
-- Alargar o CHECK exige um ALTER, e as migrations depois do deploy são só
-- aditivas, uma tabela por arquivo (ADR-021, "As migrations"). A lápide do
-- ditado também tem a data do ditado (a retenção conta por ela); a da nota não
-- precisa.
--
-- Vida: a lápide da nota e a do marcador NÃO vencem. A nota não tem a janela
-- de 6 meses do ditado, então nada cobriria a recusa depois que a lápide
-- saísse. São só ids, e o job de retenção não as toca.
--   * seq: o seq da mudança (único por usuário, como o last_seq das
--     entidades), para o pull ordenar as lápides junto com elas.
--
-- O "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE note_tombstones (
    user_id     UUID NOT NULL REFERENCES users (id),
    entity_type TEXT NOT NULL CHECK (entity_type IN ('note', 'label')),
    entity_id   UUID NOT NULL,
    seq         BIGINT NOT NULL CHECK (seq > 0),
    PRIMARY KEY (user_id, entity_type, entity_id),
    UNIQUE (user_id, seq)
);
