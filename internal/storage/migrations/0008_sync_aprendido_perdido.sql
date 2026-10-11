-- 0008: o par que este PC perdeu na nuvem, para a linha de estado do
-- "trocado em outro aparelho" (ADR-019, "Decisões da F8", D4; a revisão da
-- F8g, 08-out-2026).
--
-- NÃO destrutiva. Nenhuma linha existente muda: a sync_aprendido só ganha
-- duas colunas, nulas. Nada mais é tocado, e o cursor não volta.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- perdido_variante e perdido_termo: a troca DESTE PC que perdeu na nuvem
-- (o conflict, D4: vale o primeiro), guardada com a etiqueta ('desfeito_em_outro',
-- 'trocado_em_outro'). O par que vale aqui passa a ser o da nuvem, e a linha
-- de estado mostra o que este PC tinha aprendido (06, GET /api/aprendizado).
-- Os dois juntos, ou nenhum. Só existe com a etiqueta (e na linha
-- 'aplicando' que a aplica): sai com ela (a pessoa aprende de novo, ou 30
-- dias) e no esquecer, como o texto do par da nuvem guardado.
ALTER TABLE sync_aprendido ADD COLUMN
    perdido_variante TEXT;
ALTER TABLE sync_aprendido ADD COLUMN
    perdido_termo TEXT;
