-- 0004: a preferência "microfone sempre pronto" em local_settings
-- (tela de Configurações do painel, ADR-015, 29-set-2026).
--
-- NÃO destrutiva: só acrescenta uma coluna, com padrão, e não toca em
-- nenhuma linha existente além de preenchê-la com o padrão.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- microphone_always_ready: 1 = o microfone fica aberto o tempo todo e a
-- captura leva 300 ms de antes das teclas. 0 (padrão) = ele só abre
-- durante o ditado. É só a escolha de quem usa: nenhum áudio é guardado.
ALTER TABLE local_settings ADD COLUMN
    microphone_always_ready INTEGER NOT NULL DEFAULT 0;
