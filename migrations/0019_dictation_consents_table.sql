-- Migration 0019: tabela dictation_consents.
--
-- O aceite do ditado no celular (ADR-021, decisão H e C3b;
-- docs/ai-spec/05-data-model.md, "O aceite do ditado no celular"). Quem dita
-- pelo celular aceita um aviso específico, antes de colar a chave: a voz de
-- cada ditado vai ao provedor da transcrição que a pessoa escolheu, e, com o
-- Polish, o texto vai ao provedor do Polish. Provar o consentimento é do
-- controlador (art. 8º, §2º, da LGPD), então CADA aceite é uma linha, e nenhuma
-- some quando termina: a linha ganha a data do fim e o motivo.
--   * id: UUID v7 gerado pela aplicação.
--   * notice_version: a versão do texto do aviso que a pessoa viu (inteiro a
--     partir de 1). O aceite só vale enquanto a versão for a do servidor
--     (cloudstore.VersaoDoAviso): mudar o texto do aviso pede um aceite novo.
--   * asr_provider e polish_provider: os provedores que a pessoa aceitou, o da
--     transcrição (openai ou groq) e o do Polish (openai ou anthropic; nulo
--     quando o aviso que ela viu não incluía o Polish). O aceite cobre quem
--     está em qualquer um dos dois.
--   * accepted_at: quando a pessoa aceitou, do relógio do servidor.
--   * ended_at e end_reason: quando o aceite terminou e por quê, sempre juntos.
--     'revogado': a pessoa desligou o ditado no celular (e as chaves dela
--     foram apagadas na mesma transação). 'substituido': ela aceitou de novo
--     (outro provedor, o Polish com um provedor novo, ou outra versão do
--     aviso), e a linha nova tomou o lugar.
--   * O índice único parcial garante que cada conta tem no máximo UM aceite em
--     aberto (o vigente, se a versão for a atual): duas gravações ao mesmo
--     tempo não deixam dois.
--
-- Só aditiva (ADR-021, "As migrations"): nenhuma tabela anterior muda. O
-- "apagar meus dados" apaga as linhas da conta (pgstore, apagarDaConta).
--
-- Invariante auditada: nenhuma coluna aqui pode conter áudio nem
-- contexto de janela/seleção de outras aplicações (ver
-- docs/ai-spec/09-security-privacy.md, "Logging proibido" e AC-027/AC-028).
-- Isso é verificado em migrations/schema_test.go.
CREATE TABLE dictation_consents (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users (id),
    notice_version  INTEGER NOT NULL CHECK (notice_version >= 1),
    asr_provider    TEXT NOT NULL CHECK (asr_provider IN ('openai', 'groq')),
    polish_provider TEXT CHECK (polish_provider IN ('openai', 'anthropic')),
    accepted_at     TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ,
    end_reason      TEXT CHECK (end_reason IN ('revogado', 'substituido')),
    CHECK ((ended_at IS NULL) = (end_reason IS NULL)),
    CHECK (ended_at IS NULL OR ended_at >= accepted_at)
);

-- No máximo um aceite em aberto por conta; também é a busca do vigente.
CREATE UNIQUE INDEX idx_dictation_consents_open ON dictation_consents (user_id) WHERE ended_at IS NULL;
