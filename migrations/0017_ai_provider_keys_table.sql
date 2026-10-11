-- Migration 0017: tabela ai_provider_keys.
--
-- A chave de API do provedor de IA que a pessoa cola no app do celular, para
-- ditar pelo celular (ADR-021, decisão G e C3a; docs/ai-spec/05-data-model.md,
-- "As chaves da IA"). É a chave DA PESSOA (BYOK), nunca uma chave do projeto, e
-- não é a chave do PC: essa continua no Gerenciador de Credenciais do Windows.
--
-- A chave só entra cifrada. O servidor cifra com AES-256-GCM, com uma subchave
-- derivada por HKDF da chave de cifra do servidor (AI_KEYS_SECRET_FILE, um
-- arquivo, nunca o banco nem o backup), e com o usuário, o provedor e o id da
-- chave de cifra no AAD (internal/server/cofre): o cifrado copiado para outra
-- linha não decifra.
--   * provider: a lista fechada dos provedores do ditado no celular (a
--     transcrição: OpenAI e Groq; o Polish: OpenAI e Anthropic). Uma chave por
--     provedor e por pessoa: a chave primária é (user_id, provider), e trocar
--     é gravar por cima.
--   * cipher_key_id: os 8 primeiros bytes de um HMAC da chave de cifra que
--     cifrou a linha, e não um número de versão. Trocar o arquivo da chave de
--     cifra leva a linha a "cole de novo" (SEM_CHAVE), sem tentar decifrar.
--   * nonce: os 12 bytes do GCM, aleatórios a cada gravação.
--   * ciphertext: o texto cifrado, com a etiqueta de 16 bytes do GCM no fim;
--     o teto é o da chave (2560 bytes, o tamanho máximo de
--     internal/chavedeapi) mais a etiqueta.
--   * key_suffix: o fim da chave que a tela mostra ("Chave terminada em
--     WXYZ"), até 4 caracteres e nunca mais que um quarto dela
--     (chavedeapi.Final), para a tela não precisar decifrar nada.
--   * created_at e updated_at: do relógio de quem grava (sem DEFAULT);
--     created_at é a primeira chave do provedor, updated_at a última troca.
--
-- Só aditiva (ADR-021, "As migrations"): nenhuma tabela anterior muda. O
-- "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE ai_provider_keys (
    user_id       UUID NOT NULL REFERENCES users (id),
    provider      TEXT NOT NULL CHECK (provider IN ('openai', 'groq', 'anthropic')),
    cipher_key_id BYTEA NOT NULL CHECK (octet_length(cipher_key_id) = 8),
    nonce         BYTEA NOT NULL CHECK (octet_length(nonce) = 12),
    ciphertext    BYTEA NOT NULL CHECK (octet_length(ciphertext) BETWEEN 17 AND 2576),
    key_suffix    TEXT NOT NULL CHECK (key_suffix ~ '^[!-~]{0,4}$'),
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, provider),
    CHECK (updated_at >= created_at)
);
