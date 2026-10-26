-- 0010: o ditado que virou nota (ADR-022, "Notas", fase N3, 25-out-2026).
--
-- NÃO destrutiva. Só nasce uma coluna nula em local_transcriptions e um
-- índice parcial, vazio. Nenhuma linha existente muda: todo ditado de hoje
-- fica com virou_nota nulo, ou seja, continua no histórico como estava.
--
-- O SYNC NÃO ENTRA. O motor (internal/sync) ainda não conhece notas (N7), e
-- a coluna é só deste PC: mudá-la não põe marcador na sync_outbox (não passa
-- por emitir) e a nuvem continua vendo o ditado normalmente. Quando a N7
-- chegar, decide-se se "virou nota" sobe.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- virou_nota: o id da nota em que o ditado virou ("Virar nota"). Nulo é o
-- ditado comum. Preenchido, o ditado sai da lista do histórico SEM ir para a
-- lixeira (não é expurgado em 30 dias nem aparece como apagado) e a
-- retenção de 6 meses não o alcança (o texto vive na nota). Só o id, sem
-- chave estrangeira: apagar a nota de vez não devolve o ditado ao histórico,
-- e o id fica solto de propósito.
ALTER TABLE local_transcriptions ADD COLUMN
    virou_nota TEXT;

-- Os ditados que viraram nota: a consulta do Desfazer, e o filtro "não
-- virou" das listas lê o próprio histórico, que já tem o índice dele.
CREATE INDEX idx_local_transcriptions_virou_nota
    ON local_transcriptions (virou_nota)
    WHERE virou_nota IS NOT NULL;
