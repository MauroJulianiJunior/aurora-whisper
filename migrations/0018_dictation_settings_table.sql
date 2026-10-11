-- Migration 0018: tabela dictation_settings.
--
-- As preferências do ditado no celular, uma linha por conta (ADR-021, decisão
-- N e C3b; docs/ai-spec/05-data-model.md, "As preferências do ditado no
-- celular"): quem transcreve (provedor e modelo), se os termos aprendidos vão
-- como dica, e se o Polish está ligado (provedor e modelo). Moram só na nuvem e
-- nunca descem aos PCs: as preferências do PC continuam no PC (ADR-021,
-- conflito 7). Sem a linha, a conta tem os padrões (cloudstore.PreferenciasPadrao):
-- nada foi escolhido ainda.
--
-- Nada aqui é segredo: a chave da IA mora em ai_provider_keys (0017), cifrada.
--   * asr_provider: quem transcreve, da lista fechada openai e groq (a
--     Anthropic não transcreve).
--   * asr_model: o modelo da transcrição, texto livre dentro de uma lista
--     curta de caracteres (as letras, os dígitos e . _ : / @ -, de 1 a 100):
--     o nome vem do provedor, e a nuvem nunca o valida contra uma lista fixa
--     (os modelos saem do ar), só contra a forma. A mesma expressão mora em
--     cloudstore (ConferirPreferencias), e um teste confere as duas.
--   * use_hint: manda os termos aprendidos da conta ao provedor como dica
--     (decisão E). Começa desligada até a C8 medir.
--   * polish_enabled: o Polish no celular (decisão N). Começa desligado.
--   * polish_provider: quem faz o Polish, da lista fechada openai e anthropic.
--     Guardado mesmo com o Polish desligado: é a escolha da pessoa, e só o
--     aceite do provedor vale quando ele liga.
--   * polish_model: o modelo do Polish. Pode ficar vazio enquanto o Polish
--     está desligado (a pessoa ainda não escolheu), nunca ligado.
--   * created_at e updated_at: do relógio de quem grava (sem DEFAULT);
--     created_at é a primeira gravação, updated_at a última.
--
-- Só aditiva (ADR-021, "As migrations"): nenhuma tabela anterior muda. O
-- "apagar meus dados" apaga a linha da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE dictation_settings (
    user_id         UUID PRIMARY KEY REFERENCES users (id),
    asr_provider    TEXT NOT NULL CHECK (asr_provider IN ('openai', 'groq')),
    asr_model       TEXT NOT NULL CHECK (asr_model ~ '^[A-Za-z0-9._:/@-]{1,100}$'),
    use_hint        BOOLEAN NOT NULL,
    polish_enabled  BOOLEAN NOT NULL,
    polish_provider TEXT NOT NULL CHECK (polish_provider IN ('openai', 'anthropic')),
    polish_model    TEXT NOT NULL CHECK (polish_model ~ '^[A-Za-z0-9._:/@-]{0,100}$'),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    CHECK (NOT polish_enabled OR polish_model <> ''),
    CHECK (updated_at >= created_at)
);
