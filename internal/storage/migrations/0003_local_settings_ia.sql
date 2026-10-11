-- 0002: a escolha do modo de texto e do provedor de IA em local_settings
-- (tela de Configurações do painel, 29-set-2026).
--
-- NÃO destrutiva: só acrescenta colunas, com padrão, e não toca em nenhuma
-- linha existente além de preenchê-las com o padrão.
--
-- A chave de API NÃO tem coluna, nem aqui nem em lugar nenhum do banco:
-- ela mora no Gerenciador de Credenciais do Windows (internal/agent/chaves).
-- ai_provider e ai_model são só a escolha de quem usa.
-- schema_segredo_test.go (TestSchema_NenhumaColunaDeSegredo) confere que
-- nenhuma coluna tem nome de segredo.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- text_mode: "literal" | "smart" | "polish" (16-ai-behavior-spec.md).
-- Padrão "smart" (REQ-022).
ALTER TABLE local_settings ADD COLUMN
    text_mode TEXT NOT NULL DEFAULT 'smart';

-- ai_provider: "" | "openai" | "anthropic". Vazio = nenhum escolhido.
ALTER TABLE local_settings ADD COLUMN
    ai_provider TEXT NOT NULL DEFAULT '';

-- ai_model: como o provedor nomeia o modelo. Texto livre.
ALTER TABLE local_settings ADD COLUMN
    ai_model TEXT NOT NULL DEFAULT '';
