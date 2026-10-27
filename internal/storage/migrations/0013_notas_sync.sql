-- 0013: o sync das notas e dos marcadores (ADR-022, "Sync", fase N7,
-- 26-out-2026).
--
-- NÃO destrutiva. Só nascem cinco colunas nulas em local_notes; nenhuma
-- coluna, índice ou tabela existente é tirado ou recriado, e nenhuma linha
-- existente muda: toda nota de hoje fica sem selo e sem corpo_nuvem (o PC
-- ainda não sabe nada da nuvem dela).
--
-- local_labels não ganha coluna: o "marcador que espera" (o id que uma nota
-- da nuvem cita e que o PC ainda não recebeu) é uma linha com deleted_at
-- preenchido e sync_state = 'aguardando', invisível nas listas.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- O selo: o título e o corpo que perderam um conflito de conteúdo na nuvem
-- (versao_em_conflito), o aparelho que os escreveu e a hora do conflito. Os
-- quatro vêm juntos, ou nenhum, como o selo do ditado (0005). A escolha no
-- PC (manter ou trocar) vai pela sync_outbox (selo_escolha, selo_em).
ALTER TABLE local_notes ADD COLUMN
    conflito_titulo TEXT;
ALTER TABLE local_notes ADD COLUMN
    conflito_corpo TEXT;
ALTER TABLE local_notes ADD COLUMN
    conflito_device_id TEXT;
ALTER TABLE local_notes ADD COLUMN
    conflito_em TEXT;

-- corpo_nuvem: o corpo da última versão que a nuvem confirmou. Vai no envio
-- da mudança de conteúdo como corpo_base, e é com ele que a nuvem mescla a
-- lista de marcar quando os dois lados mexeram. Nulo quando o PC não sabe
-- (a nota nunca subiu, ou o envio dela teve mudança no voo).
ALTER TABLE local_notes ADD COLUMN
    corpo_nuvem TEXT;
