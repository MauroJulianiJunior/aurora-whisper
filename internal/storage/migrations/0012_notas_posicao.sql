-- 0012: a ordem manual das notas (ADR-022, "Ordem manual (N2b)", 26-out-2026).
--
-- NÃO destrutiva. Só nasce uma coluna em local_notes e um índice novo; nenhuma
-- coluna, índice ou tabela existente é tirado ou recriado. O único dado que
-- muda é a própria posicao, preenchida abaixo para manter a ordem de hoje.
--
-- O SYNC NÃO ENTRA. A nota ainda não sobe (N7); quando subir, a posicao viaja
-- com ela, como qualquer outro campo.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- posicao: o índice fracionário da nota (internal/storage/posicao.go): um
-- texto que se ordena por bytes, e a lista sai por fixada DESC, posicao ASC
-- (a menor chave é o topo). Mover uma nota grava só a chave dela. Vazio é só
-- o valor transitório da própria migration: o preenchimento logo abaixo dá
-- uma chave a toda nota, e o Go nunca grava vazio.
ALTER TABLE local_notes ADD COLUMN
    posicao TEXT NOT NULL DEFAULT '';

-- O preenchimento mantém a ordem que a pessoa vê hoje: fixadas primeiro, depois
-- a última edição, e a criação mais recente desempata (a mesma ordem da lista
-- antes da 0012). Cada nota ganha o lugar dela na fila (por pessoa) escrito em
-- três dígitos de base 61, no alfabeto sem o '0': nenhuma chave termina em '0'
-- (a regra de PosicaoValida) e a comparação de bytes é a ordem da fila. Três
-- dígitos dão 226 mil notas por pessoa.
UPDATE local_notes
SET posicao =
    substr(f.alfabeto, (f.r0 / 3721) % 61 + 1, 1)
    || substr(f.alfabeto, (f.r0 / 61) % 61 + 1, 1)
    || substr(f.alfabeto, f.r0 % 61 + 1, 1)
FROM (
    SELECT id AS nid,
           '123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz' AS alfabeto,
           ROW_NUMBER() OVER (
               PARTITION BY user_id ORDER BY fixada DESC, updated_at DESC, rowid DESC
           ) - 1 AS r0
    FROM local_notes
) AS f
WHERE f.nid = local_notes.id;

-- A lista: fixadas primeiro, e dentro de cada grupo a posição (a menor é o topo).
CREATE INDEX idx_local_notes_ordem
    ON local_notes (user_id, arquivada, fixada DESC, posicao)
    WHERE deleted_at IS NULL;
