-- Migration 0009: tabela applied_mutations.
--
-- Base da idempotência do push (ver docs/ai-spec/05-data-model.md,
-- "AppliedMutation", e docs/adr/ADR-019-nuvem-e-celular.md, "Contrato de
-- sync, por usuário").
--
-- A chave primária é (user_id, mutation_id), POR USUÁRIO. Esta migration
-- tinha mutation_id como PK global; o motor em memória já escopava por
-- usuário, e a PK global faria o mutation_id de uma pessoa colidir com o
-- de outra, ou deixaria uma pessoa descobrir que um id existe em outra
-- conta. Corrigida no lugar pela F1 da etapa 5 (ADR-019; nunca rodou).
--
-- Reenviar uma mutação já aplicada (ex.: retry de rede após timeout no
-- ack) colide com a PK. O servidor lê a linha antiga e responde
-- "duplicate" com o MESMO resultado da primeira vez, em vez de aplicar
-- duas vezes — é essa colisão que impede uma mutação reenviada de virar
-- dois registros (meta dura de F1-D: "Sync confirmado perdido: 0").
--
-- O resultado da primeira aplicação fica em colunas, não em JSON livre, de
-- propósito: nenhuma delas carrega texto do ditado, então esta tabela não
-- guarda texto além da retenção.
--   * status: o que aconteceu na primeira vez (applied, conflict, rejected
--     ou gone). "duplicate" nunca é gravado: é a resposta a um reenvio.
--   * result_version: a versão da entidade devolvida (applied, conflict).
--   * merged: o servidor mesclou campos diferentes (o "mesclado" da
--     resposta). Só vale para applied.
--   * result_code: o código da lista fechada de rejected, ou o motivo de
--     gone (apagado, fora_da_janela).
--
-- entity_type e entity_id são a entidade que a mutação mudou, e são
-- NOT NULL. Sem eles, um mutation_id reusado para OUTRA entidade (um cliente
-- com defeito) seria respondido como "duplicate", e a mudança se perderia em
-- silêncio, contra a meta "Sync confirmado perdido: 0". Com eles, o servidor
-- compara a entidade do reenvio com a gravada e responde "rejected" quando
-- for outra (F2). O tipo é o da lista fechada da etapa 5 (transcription ou
-- learned), que a validação do push confere; o banco só exige que exista.
--
-- device_id é o aparelho que enviou, e é NOT NULL: o push só vem de um
-- aparelho pareado (o celular edita pelo CRUD, que não grava mutação).
-- (user_id, device_id) é chave estrangeira composta para devices (0002): o
-- aparelho tem de ser do mesmo usuário.
--
-- operation é a operação mandada da primeira vez: upsert ou delete. Entrou
-- com o aprendido (ADR-019, "Decisões da F8"; corrigida no lugar pela F8c,
-- pela regra da decisão 13: nunca rodou fora de um banco de teste
-- descartável, e a convenção de um arquivo por tabela não deixa um ALTER
-- noutro arquivo). O learned tem duas operações (aprender e desfazer), e o
-- reenvio com o mesmo mutation_id e OUTRA operação é "rejected"
-- (mutacao_reusada): sem a coluna, o servidor não teria de onde tirar a
-- operação da primeira vez, e o desfazer em voo reenviado como reaprender
-- perderia para o próprio desfazer. É nula só quando a operação mandada não
-- é da lista (a recusa operacao_invalida): o banco nunca guarda texto livre
-- do cliente aqui. Um banco de teste que aplicou a 0009 de antes recusa o
-- migrate por checksum: apague-o (migrations/README.md).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE applied_mutations (
    user_id        UUID NOT NULL REFERENCES users (id),
    mutation_id    UUID NOT NULL,
    device_id      UUID NOT NULL,
    entity_type    TEXT NOT NULL,
    entity_id      UUID NOT NULL,
    operation      TEXT CHECK (operation IN ('upsert', 'delete')),
    applied_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    status         TEXT NOT NULL CHECK (status IN ('applied', 'conflict', 'rejected', 'gone')),
    result_version BIGINT CHECK (result_version >= 1),
    merged         BOOLEAN NOT NULL DEFAULT false,
    result_code    TEXT,
    PRIMARY KEY (user_id, mutation_id),
    FOREIGN KEY (user_id, device_id) REFERENCES devices (user_id, id),
    CHECK (NOT merged OR status = 'applied')
);

-- Suporte à arrumação por idade (retenção dos registros de idempotência,
-- ver "Retenção" em docs/ai-spec/05-data-model.md). O acesso por usuário e
-- mutation_id é a própria chave primária.
CREATE INDEX idx_applied_mutations_applied_at ON applied_mutations (applied_at);
