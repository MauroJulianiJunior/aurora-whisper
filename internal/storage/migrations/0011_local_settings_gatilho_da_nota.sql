-- 0011: a preferência "Ditar para nota" em local_settings (ADR-022, fase N9,
-- 26-out-2026): liga e desliga o segundo gatilho (Ctrl+Win+Alt).
--
-- NÃO destrutiva: só acrescenta uma coluna, com padrão, e não toca em
-- nenhuma linha existente além de preenchê-la com o padrão.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- note_trigger_enabled: 1 (padrão) = segurar Ctrl+Win+Alt manda o ditado para
-- uma nota, sem inserir no campo em foco. 0 = Ctrl+Win+Alt é um ditado comum
-- (o conjunto contém o Ctrl+Win). Só a escolha de quem usa.
ALTER TABLE local_settings ADD COLUMN
    note_trigger_enabled INTEGER NOT NULL DEFAULT 1;
