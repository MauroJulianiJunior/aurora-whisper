-- Migration 0010: tabela user_sync_counters.
--
-- O contador de sync de cada usuário (ver docs/ai-spec/05-data-model.md,
-- "UserSyncCounter", e docs/adr/ADR-019-nuvem-e-celular.md, "Gravação única"
-- e "Lápides, gone, epoch e recomeço"). Uma linha por usuário, criada junto
-- com o usuário, e separada de users de propósito: o contador é a linha mais
-- disputada do banco (toda mudança a trava) e não pode disputar trava com o
-- login, que lê e atualiza users.
--
-- Como o contador dá o seq: o PRIMEIRO comando da transação de qualquer
-- mudança é
--
--   UPDATE user_sync_counters SET last_seq = last_seq + 1
--   WHERE user_id = $1 RETURNING last_seq
--
-- A trava da linha fica até o commit. Duas transações do mesmo usuário se
-- enfileiram, então a ordem do seq é a ordem dos commits, sem buraco, e um
-- rollback devolve o número (o UPDATE some junto com a transação). O
-- gargalo é por usuário: irrelevante com uma pessoa só. A ordem das travas é
-- fixa (contador, entidade, evento), então não há deadlock entre elas.
--
-- Duas coisas que este contador PRESSUPÕE e que não se veem no SQL:
--
--   * READ COMMITTED, o isolamento padrão do Postgres. É nele que o segundo
--     escritor do mesmo usuário ESPERA a trava e depois lê o valor novo. Em
--     REPEATABLE READ ou SERIALIZABLE ele recebe 40001 (falha de
--     serialização) em vez de esperar: quem gravar com outro nível tem de
--     tratar o 40001 (refazer a transação).
--
--   * O pull lê um prefixo consistente de seq porque a ordem dos seqs é a
--     dos commits. Isso só vale se ele lê as entidades e as lápides num
--     snapshot só: uma consulta, ou uma transação somente leitura em
--     REPEATABLE READ. Duas consultas soltas em READ COMMITTED podem ver, de
--     uma para a outra, uma gravação confirmada no meio, e o cursor pularia
--     a mudança.
--
-- Colunas:
--   * last_seq: o maior seq já entregue. Começa em 0 (nenhuma mudança); o
--     primeiro seq é 1.
--   * sync_epoch: identifica a "época" dos dados do usuário no servidor.
--     Muda ao restaurar um backup (o roteiro de restauração troca, F11) e
--     no "apagar meus dados". O PC compara o epoch de cada resposta com o
--     que guardou: diferente, ele descarta a resposta e recomeça. É gerado
--     pela aplicação (UUID v7), sem DEFAULT.
--   * tombstone_watermark: a marca d'água das lápides. Só sobe, quem a sobe
--     usa GREATEST, e nunca passa de last_seq. Desde a F9 (ADR-019,
--     "Decisões da F9", D2, 08-out-2026), NINGUÉM A LÊ: o pull não responde
--     410 por ela (o gone impede o ressuscitar, e a releitura do 0 não
--     recuperaria a lápide perdida), e o job de retenção não a sobe ao apagar
--     uma lápide vencida. Só o "apagar meus dados" a sobe, ao last_seq. Se um
--     dia houver um leitor (a F11, com o backup restaurado, pode pedir), ela
--     é o MAIOR seq de lápide já apagada: a lápide sai pela data do ditado, e
--     não em ordem de seq. Corrigida no lugar pela F9c (só este comentário).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE user_sync_counters (
    user_id             UUID PRIMARY KEY REFERENCES users (id),
    last_seq            BIGINT NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    sync_epoch          UUID NOT NULL,
    tombstone_watermark BIGINT NOT NULL DEFAULT 0 CHECK (tombstone_watermark >= 0),
    CHECK (tombstone_watermark <= last_seq)
);
