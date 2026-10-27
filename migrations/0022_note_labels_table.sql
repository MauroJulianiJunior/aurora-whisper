-- Migration 0022: tabela note_labels.
--
-- Os marcadores de cada nota (ADR-022, N7): uma linha por (nota, marcador). O
-- conjunto viaja inteiro com a nota (o último que escreve vence, sem selo), e
-- a gravação troca as linhas da nota quando ele muda.
--   * (user_id, note_id) é chave estrangeira para notes, com ON DELETE
--     CASCADE: apagar a nota de vez leva as ligações.
--   * label_id NÃO tem chave estrangeira para labels: o marcador pode chegar
--     depois da nota (outro PC o criou e a nota subiu primeiro). Apagar de vez
--     um marcador tira as ligações dele pelo índice abaixo, na mesma
--     transação, sem subir a versão das notas.
--
-- Só aditiva (ADR-021, "As migrations"). O "apagar meus dados" apaga as linhas
-- da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE note_labels (
    user_id  UUID NOT NULL REFERENCES users (id),
    note_id  UUID NOT NULL,
    label_id UUID NOT NULL,
    PRIMARY KEY (user_id, note_id, label_id),
    FOREIGN KEY (user_id, note_id) REFERENCES notes (user_id, id) ON DELETE CASCADE
);

-- O apagar de vez de um marcador: as ligações dele em todas as notas.
CREATE INDEX idx_note_labels_label ON note_labels (user_id, label_id);
