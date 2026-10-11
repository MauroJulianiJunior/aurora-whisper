-- 0007: o espelho do aprendido e o conjunto "veio da nuvem" (ADR-019,
-- "Decisões da F8", fatia F8e, 07-out-2026).
--
-- NÃO destrutiva. Só nascem duas tabelas novas, vazias. Nenhuma linha
-- existente muda, e nenhuma tabela que já existe ganha coluna. O marcador do
-- aprendido na sync_outbox (entity_type 'learned', campos 0, inicial 0) não
-- nasce aqui: é posto em tempo de execução (porMarcadorDoAprendido,
-- aprendido_sqlite.go). O cursor (sync_state.last_server_seq) NÃO volta a 0:
-- nenhuma nuvem tem aprendido antes da F8 (a escolha menor 4 da ADR, um risco
-- aceito).
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- sync_aprendido: o espelho. Uma linha por id do aprendido (o UUID v5 da
-- variante, dictionary.IDDoPar), pelo dono local. Nada daqui vai para o
-- dicionario.json, e nada do dicionario.json muda por causa dela.
CREATE TABLE sync_aprendido (
    user_id           TEXT NOT NULL,
    entity_id         TEXT NOT NULL,
    -- A base: a versão que a nuvem confirmou (nula: a nuvem não tem o par, ou
    -- o PC esqueceu a versão, no esquecer e no epoch novo) e se o par
    -- confirmado está desfeito lá. O texto da base é a projeção guardada
    -- (abaixo): a linha não guarda outra cópia do par da nuvem.
    server_version    INTEGER,
    base_desfeita     INTEGER NOT NULL DEFAULT 0 CHECK (base_desfeita IN (0, 1)),
    -- versao_local sobe a cada mudança daqui (a varredura que a vê, e o
    -- Aprender e o Desfazer do painel, no ato); versao_base é a versão local
    -- que a base cobre. versao_local maior que versao_base é "mudou aqui e a
    -- nuvem não confirmou", e o esquecer não mexe em nenhuma das duas.
    versao_base       INTEGER NOT NULL DEFAULT 0,
    versao_local      INTEGER NOT NULL DEFAULT 0,
    -- A projeção guardada (dictionary.ParDoPainel): o que o dicionario.json
    -- tinha deste id da última vez que a varredura ou o aplicador o viram.
    -- Nula (variante nula): o id fora da projeção. Durante o voo de um envio,
    -- é a do envio (a operação e o payload do reenvio saem dela); no
    -- "recusado", é a projeção recusada. dominio nulo é sem domínio.
    variante          TEXT,
    termo             TEXT,
    restrito          INTEGER CHECK (restrito IS NULL OR restrito IN (0, 1)),
    dominio           TEXT,
    em_alias_restrito INTEGER CHECK (em_alias_restrito IS NULL OR em_alias_restrito IN (0, 1)),
    -- A projeção esperada da intenção do aplicador (write-ahead), só na
    -- situação 'aplicando'. Nula, nessa situação: a intenção tira o par.
    esperada_variante TEXT,
    esperada_termo    TEXT,
    esperada_restrito INTEGER CHECK (esperada_restrito IS NULL OR esperada_restrito IN (0, 1)),
    esperada_dominio  TEXT,
    esperada_em_alias_restrito INTEGER CHECK (esperada_em_alias_restrito IS NULL OR esperada_em_alias_restrito IN (0, 1)),
    -- O par da nuvem guardado (dictionary.ParDaNuvem), com a versão dele: o
    -- que ainda não vale aqui (não aplicado, pulado, arquivo indisponível), o
    -- da intenção ('aplicando') ou o da etiqueta ('desfeito_em_outro',
    -- 'trocado_em_outro'). O esquecer apaga o texto e a versão.
    nuvem_versao      INTEGER,
    nuvem_variante    TEXT,
    nuvem_termo       TEXT,
    nuvem_restrito    INTEGER CHECK (nuvem_restrito IS NULL OR nuvem_restrito IN (0, 1)),
    nuvem_dominio     TEXT,
    nuvem_desfeito    INTEGER CHECK (nuvem_desfeito IS NULL OR nuvem_desfeito IN (0, 1)),
    -- A situação e o motivo, de listas fechadas conferidas em Go
    -- (storage.SituacaoDoEspelho; o motivo é um dictionary.MotivoDoPar no
    -- 'nao_aplicado' e o código da recusa no 'recusado'), e quando a
    -- situação começou (as etiquetas somem em 30 dias). Nula: nenhuma.
    situacao          TEXT,
    motivo            TEXT,
    situacao_em       TEXT,
    -- conta_esquecida: a conta (nuvem_conta) que o esquecer guardou na linha
    -- que depende dela (o "desfeito aqui", a linha presente mudada aqui que a
    -- nuvem tinha, e o par aprendido aqui que a nuvem confirmou). Na primeira
    -- rodada do pareamento seguinte, de volta à mesma conta, a mudança daqui
    -- continua mudada aqui e sobe; numa conta nova, o "desfeito aqui" é solto
    -- e não sobe (lá, tiraria uma troca que a conta aprendeu por conta
    -- própria), e o aprendido aqui passa a ser mudança daqui. As regras estão
    -- no Go (internal/storage/aprendido_sqlite.go).
    conta_esquecida   TEXT,
    PRIMARY KEY (user_id, entity_id)
);

-- sync_veio_da_nuvem: os ids que o aplicador escreveu no dicionario.json (o
-- par que chegou da nuvem). Só o id: nenhum texto. O "Esquecer esta nuvem"
-- e o epoch novo não a tocam: o par de uma conta esquecida nunca sobe para a
-- conta nova. Sai o id que a pessoa aprende aqui de novo.
CREATE TABLE sync_veio_da_nuvem (
    user_id   TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    PRIMARY KEY (user_id, entity_id)
);
