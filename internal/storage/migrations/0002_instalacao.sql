-- Migration 0002: o que é desta instalação, e de mais nenhuma.
--
-- Uma tabela nome/valor pequena. Hoje guarda o dono fixo do banco local
-- (nome 'dono_local') e o registro da migração que levou os ditados
-- antigos para ele (ver internal/storage/dono.go e
-- docs/ai-spec/05-data-model.md, "instalacao").
--
-- Separada de local_settings de propósito: aquela é preferência de quem
-- usa, editável pelo painel; esta é identidade do banco, que nada deve
-- editar depois de gravada.
--
-- Não destrutiva: só cria. Nenhuma linha existente é tocada aqui — a
-- migração dos user_id é do Agent, depois de uma cópia do banco.
CREATE TABLE IF NOT EXISTS instalacao (
    -- "nome", e nao "chave": o schema_segredo_test proibe coluna com
    -- nome de segredo, e aqui "chave" seria so o nome do campo.
    nome TEXT PRIMARY KEY,
    valor TEXT NOT NULL
);
