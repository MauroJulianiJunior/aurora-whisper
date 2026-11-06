-- Migration 0027: tabela dictionary_copies.
--
-- A cópia do dicionário de um PC na nuvem (ADR-023, "B. A nuvem guarda uma
-- cópia por conta"): o dicionario.json do PC, recortado por
-- dictionary.Semente.ParaACopia (sem o mapa de domínios, sem os ids de amostra e
-- com o usuário local trocado por "copia"), para o ditado no celular montar o
-- Smart com ele. Só vai do PC à nuvem, só com a opção "Levar o dicionário para o
-- celular" ligada no painel do PC; o arquivo continua sendo o dono, e nada desce
-- aos PCs. As tabelas dictionary_entries e dictionary_aliases (0004 e 0005)
-- ficam como estão, vazias: eram o dicionário por linha, que a decisão 1 do
-- Mauro (05-nov-2026) descartou.
--   * user_id: a conta, PRIMARY KEY. Uma cópia só por conta: ligar a opção em
--     outro PC troca a fonte (o UPSERT do store), não cria uma segunda linha.
--   * device_id: o PC que é a fonte. A chave estrangeira composta
--     (user_id, device_id) -> devices (user_id, id) garante que o PC é da conta.
--     SEM cascata: a revogação do PC não apaga a linha do aparelho (ela só ganha
--     revoked_at), então quem apaga a cópia é o RevogarAparelho, na mesma
--     transação; e o "apagar meus dados" apaga a cópia antes dos aparelhos. A
--     leitura ainda ignora a cópia de um aparelho revogado, por garantia.
--   * content: os bytes da cópia, como TEXT, NÃO JSONB: o JSONB reordena as
--     chaves, e o SHA-256 (a versão da cópia) deixaria de bater. Até 512 KiB
--     (dictionary.MaxBytesDaCopia).
--   * sha256: o SHA-256 dos bytes, em hexadecimal minúsculo, conferido contra o
--     cabeçalho Dicionario-SHA256 do PUT antes de gravar.
--   * terms: quantos termos a cópia tem, para a linha do celular (de 0 a 2.000,
--     dictionary.MaxTermosDaCopia). Os outros limites (variantes, tamanho do
--     termo) ficam em Go, em dictionary.ValidarCopia, que o servidor roda ao
--     receber; aqui só os tetos que o banco pode conferir sem entender o JSON.
--   * updated_at: quando a cópia mudou pela última vez (vem de quem grava, sem
--     DEFAULT, como o relógio do servidor em todo o resto).
--
-- Só aditiva (ADR-021, "As migrations"): nenhuma tabela anterior muda. Sai no
-- "Apagar meus dados" (pgstore, apagarDaConta), no RevogarAparelho e no DELETE da
-- rota; o backup cifrado a leva como o resto.
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem contexto de
-- janela/seleção de outras aplicações (ver docs/ai-spec/09-security-privacy.md,
-- "Logging proibido" e AC-027/AC-028). O conteúdo é o dicionário que a pessoa
-- escreveu, nunca texto ditado. Isso é verificado em migrations/schema_test.go.
CREATE TABLE dictionary_copies (
    user_id    UUID PRIMARY KEY REFERENCES users (id),
    device_id  UUID NOT NULL,
    content    TEXT NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 524288),
    sha256     TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    terms      INTEGER NOT NULL CHECK (terms BETWEEN 0 AND 2000),
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (user_id, device_id) REFERENCES devices (user_id, id)
);
