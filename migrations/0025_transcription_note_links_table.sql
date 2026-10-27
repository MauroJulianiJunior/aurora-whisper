-- Migration 0025: tabela transcription_note_links.
--
-- O ditado que virou nota (ADR-022, "Ditado vira nota"; N7): uma linha por
-- ditado ligado, com o id da nota. É o campo virou_nota do ditado no sync: o
-- create e o patch do push gravam ou tiram a linha, sem conflito (o último
-- que escreve vence), e o pull e o GET do ditado o levam. A listagem do
-- histórico na nuvem (GET /transcriptions e a busca) esconde o ditado ligado:
-- ele está na nota.
--
-- Por que uma tabela, e não uma coluna em transcriptions: a 0003 já está em
-- produção, e as migrations depois do deploy são só aditivas, uma tabela por
-- arquivo (ADR-021, "As migrations").
--   * A chave primária é (user_id, transcription_id): no máximo uma nota por
--     ditado.
--   * (user_id, transcription_id) é chave estrangeira para transcriptions,
--     com ON DELETE CASCADE: a retenção de 6 meses do ditado continua valendo,
--     e a ligação sai com ele.
--   * note_id NÃO tem chave estrangeira para notes: a nota pode chegar depois
--     do ditado, ou ter sido apagada de vez.
--
-- O "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE transcription_note_links (
    user_id          UUID NOT NULL REFERENCES users (id),
    transcription_id UUID NOT NULL,
    note_id          UUID NOT NULL,
    PRIMARY KEY (user_id, transcription_id),
    FOREIGN KEY (user_id, transcription_id) REFERENCES transcriptions (user_id, id) ON DELETE CASCADE
);
