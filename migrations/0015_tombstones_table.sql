-- Migration 0015: tabela tombstones.
--
-- As lápides (ver docs/ai-spec/05-data-model.md, "Lapide", e
-- docs/adr/ADR-019-nuvem-e-celular.md, "Lápides, gone, epoch e recomeço").
-- Apagar de vez um ditado ainda dentro da janela de 6 meses (a lixeira
-- vencida, ou "excluir definitivamente") apaga a linha de transcriptions e
-- deixa uma lápide aqui, na mesma transação, junto com um sync_event de
-- operation 'tombstone' (0008).
--
-- Para quê: o pull entrega as lápides, em ordem de seq, e o PC apaga a
-- cópia dele. Uma mudança para uma entidade com lápide recebe "gone", e
-- assim um PC parado por meses não ressuscita um ditado apagado.
--
-- Vida: a lápide vive até a DATA DO DITADO passar dos 6 meses
-- (dictated_at, o created_at que o PC mandou) e mais 24 horas (a folga do
-- relógio do servidor). Depois disso a recusa por janela cobre, e o job de
-- retenção apaga a lápide, decidida linha a linha em Go (nunca por um corte
-- de data no SQL, que prende o fim do mês) e apagada pela chave (F9;
-- ADR-019, "Decisões da F9"). A marca d'água de user_sync_counters (0010) não
-- sobe (D2). Como a retenção conta pela data do ditado, e não pela do
-- apagamento, as lápides saem fora da ordem do seq. Corrigida no lugar pela
-- F9c (só este comentário).
--
-- Só ditado deixa lápide: o aprendido (0016) é desfeito por undone_at, e
-- a linha continua, porque "desfeito em outro aparelho" precisa ser
-- lembrado. O CHECK abaixo trava isso; quem mudar essa decisão muda o
-- CHECK junto.
--   * seq: o seq da mudança (único por usuário, como o last_seq das
--     entidades), para o pull ordenar as lápides junto com elas.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE tombstones (
    user_id     UUID NOT NULL REFERENCES users (id),
    entity_type TEXT NOT NULL CHECK (entity_type = 'transcription'),
    entity_id   UUID NOT NULL,
    reason      TEXT NOT NULL CHECK (reason = 'apagado'),
    dictated_at TIMESTAMPTZ NOT NULL,
    seq         BIGINT NOT NULL CHECK (seq > 0),
    PRIMARY KEY (user_id, entity_type, entity_id),
    UNIQUE (user_id, seq)
);

-- O pré-filtro grosseiro do job de retenção: dictated_at até um corte que
-- só traz candidatos a mais (quem decide é o Go).
CREATE INDEX idx_tombstones_dictated_at ON tombstones (dictated_at);
