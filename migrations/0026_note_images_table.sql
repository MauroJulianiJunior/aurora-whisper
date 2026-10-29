-- Migration 0026: tabela note_images.
--
-- As imagens das notas (ADR-022, "As imagens (N10)" e "Decisões da N10b"): a
-- foto anexada à nota, no PC ou no celular, guardada NO BANCO (a decisão do
-- Mauro de 28-out-2026), para entrar sozinha no backup cifrado de todo dia, na
-- restauração e no "apagar meus dados". O teto do total da conta (400 MB,
-- imagemdanota.TetoDoTotal) é conferido em Go, porque a restauração abre o
-- dump decifrado num tmpfs da VPS e as imagens quase não comprimem.
--
-- A imagem é imutável: nasce (o PUT do PC, o POST do celular) e sai (o "Tirar
-- da nota"), e nunca muda. Anexar e tirar NÃO sobem a versão da nota.
--   * id: o UUID v7 de onde a imagem foi anexada (o PC, ou o servidor no
--     celular), guardado como veio. A chave primária é (user_id, id).
--   * note_id: a nota. (user_id, note_id) é chave estrangeira para notes, com
--     ON DELETE CASCADE: o expurgo de 30 dias, o "Apagar para sempre", a lápide
--     que vem do PC e o "apagar meus dados" levam as imagens junto. A lápide da
--     nota (note_tombstones) cobre o PUT atrasado de qualquer imagem dela.
--   * largura e altura (pixels), bytes (o tamanho de conteudo) e sha256 (o dos
--     bytes, em hexadecimal minúsculo): os metadados que o pull leva. Os bytes
--     já chegam limpos (imagemdanota.Conferir: só JPEG, sem EXIF nem nenhum
--     outro metadado, até 2.000 px e 4 MB); os limites ficam em Go, e não em
--     CHECK, para um limite novo não pedir migration (como na 0014 do PC).
--   * conteudo: os bytes do JPEG. STORAGE EXTERNAL: fora da linha (TOAST) e
--     sem a tentativa de compressão, que num JPEG só gasta CPU (PostgreSQL 16
--     ou mais novo; o alvo é o 18).
--   * created_at: a hora do anexo (a do PC, dentro da folga para o futuro, ou a
--     do servidor). A ordem das imagens na nota é created_at, e o id desempata.
--   * removed_at: a LÁPIDE, na própria linha. Tirar da nota apaga os bytes
--     (conteudo nulo) e grava removed_at com um seq novo; um PUT atrasado do
--     mesmo id recebe 410 e não ressuscita a imagem. A lápide de um id que a
--     nuvem nunca teve (o PC mandou o PUT, não ouviu a resposta e tirou a
--     imagem) nasce sem os metadados. As lápides não vencem: saem com a nota.
--   * last_seq: o seq da última mudança (o anexo, ou a lápide), único por
--     usuário, do mesmo contador (user_sync_counters): o pull lê as imagens
--     que mudaram depois do cursor, como as notas. Sem evento em tabela
--     própria: a mudança não tem aparelho de origem no pull.
--
-- Só aditiva (ADR-021, "As migrations"): nenhuma tabela anterior muda. O
-- "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE note_images (
    id         UUID NOT NULL,
    user_id    UUID NOT NULL REFERENCES users (id),
    note_id    UUID NOT NULL,
    largura    INTEGER CHECK (largura > 0),
    altura     INTEGER CHECK (altura > 0),
    bytes      INTEGER CHECK (bytes > 0),
    sha256     TEXT CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    conteudo   BYTEA STORAGE EXTERNAL CHECK (octet_length(conteudo) = bytes),
    created_at TIMESTAMPTZ NOT NULL,
    removed_at TIMESTAMPTZ,
    last_seq   BIGINT NOT NULL CHECK (last_seq > 0),
    PRIMARY KEY (user_id, id),
    UNIQUE (user_id, last_seq),
    FOREIGN KEY (user_id, note_id) REFERENCES notes (user_id, id) ON DELETE CASCADE,
    CHECK ((removed_at IS NULL) = (conteudo IS NOT NULL)),
    CHECK (removed_at IS NOT NULL OR (largura IS NOT NULL AND altura IS NOT NULL AND bytes IS NOT NULL AND sha256 IS NOT NULL))
);

-- As imagens vivas de uma nota, na ordem dela (a lista da nota, a contagem por
-- nota) e as da conta (a soma do teto).
CREATE INDEX idx_note_images_note
    ON note_images (user_id, note_id, created_at, id)
    WHERE removed_at IS NULL;
