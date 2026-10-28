-- 0014: as imagens das notas (ADR-022, "As imagens (N10)", 28-out-2026).
--
-- NÃO destrutiva. Só nasce uma tabela nova e o índice dela, vazios. Nenhuma
-- linha existente muda, e nenhuma tabela que já existe ganha coluna.
--
-- OS BYTES NÃO FICAM AQUI. Cada imagem é um arquivo, <dados>\notas-imagens\<id>.jpg
-- (storage.SQLiteNotaRepository.UsarPastaDasImagens), e esta tabela guarda só o
-- que a tela e a conferência precisam. Por quê: as cópias do histórico que ficam
-- ao lado do banco (a dos 6 meses, a do envio à nuvem, a do dono) copiam o
-- arquivo do banco, e não devem levar as fotos junto. O nome original do arquivo
-- nunca é guardado.
--
-- A ordem das imagens na nota é a da criação (created_at, e o rowid desempata).
-- A imagem não muda depois de guardada: anexar é uma linha nova, e tirar da nota
-- apaga a linha e o arquivo. Ela vai e volta da lixeira com a nota (a lixeira é
-- da nota, não da imagem), e o expurgo de 30 dias e o "Apagar para sempre" a
-- apagam junto (a chave estrangeira, com cascata, é a segunda guarda: o Go apaga
-- as linhas antes da nota, e o arquivo depois do commit).
--
-- O SYNC NÃO ENTRA NESTA MIGRATION (é a segunda metade da N10): nada aqui põe
-- marcador na sync_outbox.
--
-- Roda dentro da transação do Migrator: ou tudo, ou nada.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- local_note_images: uma linha por imagem. bytes é o TAMANHO do arquivo, em
-- bytes (os bytes em si estão no arquivo); sha256 é o dos bytes do arquivo, em
-- hexadecimal, conferido a cada leitura. Os limites (2.000 px, 4 MB, 10 por nota,
-- o teto do total) são conferidos em Go (internal/imagemdanota), e não por CHECK,
-- para um limite novo não pedir migration que recria a tabela.
CREATE TABLE local_note_images (
    id         TEXT PRIMARY KEY,
    note_id    TEXT NOT NULL REFERENCES local_notes (id) ON DELETE CASCADE,
    largura    INTEGER NOT NULL CHECK (largura > 0),
    altura     INTEGER NOT NULL CHECK (altura > 0),
    bytes      INTEGER NOT NULL CHECK (bytes > 0),
    sha256     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- As imagens de uma nota, na ordem da criação.
CREATE INDEX idx_local_note_images_nota
    ON local_note_images (note_id, created_at);
