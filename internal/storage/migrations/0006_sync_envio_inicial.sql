-- 0006: o envio inicial do histórico e a escolha do selo feita no PC
-- (ADR-019, fatia F7c, 06-out-2026).
--
-- NÃO destrutiva. Nenhuma linha existente muda: a sync_outbox só ganha
-- colunas, com o padrão (inicial 0) ou nulas (a escolha), e nasce uma tabela
-- nova, vazia (sync_mudou_aqui). Nada mais é tocado.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- inicial: o marcador foi posto pelo "Enviar o histórico" (o envio inicial,
-- resposta 3 da ADR-019). Conta para o progresso ("enviando 120 de 800") e
-- sobe por último, depois do que está em voo, do ditado novo e das edições.
-- 0 em todo marcador que já existe: nenhum deles é do envio inicial.
ALTER TABLE sync_outbox ADD COLUMN
    inicial INTEGER NOT NULL DEFAULT 0 CHECK (inicial IN (0, 1));

-- selo_escolha e selo_em: a escolha do selo feita no painel do PC ("manter"
-- fica com o texto atual; "trocar", com o do selo) e o conflict_at do selo
-- escolhido, para a nuvem conferir que é o mesmo selo. Os dois juntos, ou
-- nenhum. Sem texto nenhum: o texto continua sendo lido da linha.
ALTER TABLE sync_outbox ADD COLUMN
    selo_escolha TEXT CHECK (selo_escolha IS NULL OR selo_escolha IN ('manter', 'trocar'));
ALTER TABLE sync_outbox ADD COLUMN
    selo_em TEXT;

-- sync_mudou_aqui: os itens que o "Esquecer esta nuvem" viu mudados aqui sem
-- a nuvem esquecida ter recebido a mudança (o marcador na fila; ou, depois
-- de um esquecer anterior, a mudança feita sem a versão da nuvem). De volta
-- à mesma conta, cada um conta como "mudou aqui" e não é sobrescrito pelo
-- pull: sobe inteiro, e a nuvem decide. Item por item, e não uma hora de
-- referência recuada até a mudança mais antiga, que pegava também o que o
-- PC recebeu da nuvem depois dela (reconferência da F7c). Só o id: nenhum
-- texto.
CREATE TABLE sync_mudou_aqui (
    entity_type TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    PRIMARY KEY (entity_type, entity_id)
);
