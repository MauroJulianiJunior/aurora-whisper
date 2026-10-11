-- 0005: a fila do sync com UM marcador por ditado, o cursor e o epoch da
-- nuvem, e o selo de conflito (ADR-019, fatia F7a, 06-out-2026).
--
-- NÃO destrutiva. Nenhuma linha existente muda:
--   - a sync_outbox da 0001 (uma linha por mudança, com o texto copiado em
--     payload_json) nunca teve escritor e está vazia em todo banco. Ela é
--     RENOMEADA para sync_outbox_0001, intocada (com o índice dela), e a
--     tabela nova nasce no lugar. Se algum banco tiver alguma linha nela,
--     a linha fica lá;
--   - sync_state (também vazia, sem escritor) e local_transcriptions só
--     ganham colunas, todas nulas.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

ALTER TABLE sync_outbox RENAME TO sync_outbox_0001;

-- sync_outbox: "este ditado mudou e ainda não subiu". Um marcador por
-- entidade (a PK): duas edições seguidas viram um envio só. NENHUM texto
-- aqui: o conteúdo é lido da linha na hora de enviar, então o texto não
-- sobrevive à retenção dentro da fila.
CREATE TABLE sync_outbox (
    entity_type    TEXT NOT NULL,
    entity_id      TEXT NOT NULL,
    -- campos: o que mudou desde a última versão que a nuvem confirmou, em
    -- bits: 1 = o texto final, 2 = a lixeira (entrar ou sair). Criar não é
    -- bit: é o ditado com server_version nulo, que sobe inteiro.
    campos         INTEGER NOT NULL DEFAULT 0,
    -- mutation_id e versao_enviada: o envio em curso (o id da mutação e a
    -- version local que foi lida para montá-la). Nulos fora de voo. Um
    -- envio sem resposta é repetido com o MESMO mutation_id.
    mutation_id    TEXT,
    versao_enviada INTEGER,
    criado_em      TEXT NOT NULL,
    PRIMARY KEY (entity_type, entity_id)
);

-- sync_state já tem o cursor (last_server_seq) e last_success_at.
-- sync_epoch: o epoch da nuvem visto por último (nulo: adota o da próxima
-- resposta). pareado_em: o nuvem_conectado_em do pareamento a que o cursor
-- pertence; outro valor é uma reconexão, e o PC recomeça. ultimo_erro: o
-- código (lista fechada) da última rodada que falhou; nulo depois de uma
-- que deu certo.
ALTER TABLE sync_state ADD COLUMN
    sync_epoch TEXT;
ALTER TABLE sync_state ADD COLUMN
    pareado_em TEXT;
ALTER TABLE sync_state ADD COLUMN
    ultimo_erro TEXT;

-- O selo: a versão do texto que perdeu um conflito, como na nuvem
-- (versao_em_conflito). Chega pelo pull ou pela resposta do push; a escolha
-- no PC é da F7c. Os três vêm juntos, ou nenhum.
ALTER TABLE local_transcriptions ADD COLUMN
    conflito_texto TEXT;
ALTER TABLE local_transcriptions ADD COLUMN
    conflito_device_id TEXT;
ALTER TABLE local_transcriptions ADD COLUMN
    conflito_em TEXT;
