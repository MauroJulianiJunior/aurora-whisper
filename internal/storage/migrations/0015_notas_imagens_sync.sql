-- 0015: o sync das imagens das notas (ADR-022, "Decisões da N10b",
-- 28-out-2026).
--
-- NÃO destrutiva. Só nascem colunas, todas com o valor de "nada ainda": cinco
-- em local_note_images e uma em sync_outbox. Nenhuma coluna, índice ou tabela
-- existente é tirado ou recriado, e nenhuma linha muda de sentido: toda imagem
-- de hoje fica com o arquivo aqui (sync_state 'pending', a SituacaoNormal de
-- todas as tabelas do sync), a nuvem sem saber dela (na_nuvem 0) e sem
-- tentativa; a completação do envio (Fila.CompletarImagens) a põe na fila
-- depois do aceite do "Enviar o histórico" e da capacidade da nuvem, como fez
-- com as notas na N7.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- na_nuvem: 1 quando a nuvem tem a imagem, ou PODE ter: ela veio da nuvem, ou
-- este PC já mandou o PUT dela pelo menos uma vez (gravado ANTES da rede). É o
-- que decide se tirar da nota manda o DELETE: com 0, a nuvem nunca a viu, e o
-- marcador sai sem nada subir. Só o "Esquecer esta nuvem" volta a 0.
ALTER TABLE local_note_images ADD COLUMN
    na_nuvem INTEGER NOT NULL DEFAULT 0;

-- sync_state: 'pending' (a SituacaoNormal: o arquivo está aqui), 'aguardando'
-- (a imagem veio da nuvem pelo pull e o arquivo ainda não baixou: a linha sem
-- arquivo que a varredura respeita, e que a API local mostra como "a_caminho")
-- ou 'recusado' (a nuvem recusou a imagem de vez: ela fica só neste PC).
-- last_sync_error é o código da recusa, ou da última espera.
ALTER TABLE local_note_images ADD COLUMN
    sync_state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE local_note_images ADD COLUMN
    last_sync_error TEXT;

-- tentativas e tentou_em: a espera de uma imagem que não subiu por um motivo
-- que pode passar (a nota da nuvem com 10, o teto, a nota da lixeira na nuvem)
-- ou que não baixou (a rede no meio, os bytes que não conferem). A próxima
-- tentativa espera a BackoffPolicy das imagens a partir de tentou_em. Voltam a
-- zero quando a imagem sobe ou baixa.
ALTER TABLE local_note_images ADD COLUMN
    tentativas INTEGER NOT NULL DEFAULT 0;
ALTER TABLE local_note_images ADD COLUMN
    tentou_em TEXT;

-- nota_id: a nota de um marcador note_image. O marcador da imagem tirada da
-- nota (a lápide local) não tem mais a linha da imagem, e o DELETE da nuvem
-- leva a nota no caminho. Nulo em todo outro marcador.
ALTER TABLE sync_outbox ADD COLUMN
    nota_id TEXT;
