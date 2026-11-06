-- 0016: a preferência "Levar o dicionário para o celular" em local_settings
-- (ADR-023, fatia D3, 05-nov-2026): liga e desliga o envio da cópia do
-- dicionario.json à nuvem.
--
-- NÃO destrutiva: só acrescenta uma coluna, com padrão, e não toca em
-- nenhuma linha existente além de preenchê-la com o padrão.
--
-- Cada coluna começa a própria linha de propósito: é assim que
-- TestSchema_NoAudioOrContextColumns reconhece uma definição de coluna.

-- dictionary_copy_enabled: 0 (padrão) = nada do dicionário sai deste PC. 1 =
-- a cópia do dicionario.json vai à nuvem (o celular a usa no ditado). É a
-- INTENÇÃO deste PC: quem é a fonte da cópia quem diz é a nuvem. Só a escolha
-- de quem usa; o termo, a variante e o domínio nunca moram aqui.
ALTER TABLE local_settings ADD COLUMN
    dictionary_copy_enabled INTEGER NOT NULL DEFAULT 0;
