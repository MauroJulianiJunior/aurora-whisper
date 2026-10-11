-- O historico.db do bin\ do Mauro como ele está hoje (o Agent da F7a/b, a
-- migration 0005), com dados INVENTADOS: nenhum ditado de verdade. É o
-- fixture da prova de que o envio inicial e o "Esquecer esta nuvem" da F7c
-- não perdem nem mudam nada do histórico (internal/server/integration,
-- sync_historico_de_verdade_e2e_test.go). O esquema vem das migrations de
-- verdade (0001 a 0005); este arquivo só põe os dados.
--
-- As datas são relativas ao momento em que o teste monta o banco (strftime
-- com 'now'), para os "últimos 6 meses" valerem em qualquer dia. O formato é
-- o do repositório (formatoTempo: nove dígitos de fração, UTC, 'Z').
--
-- O que tem, e por quê:
--   - a instalação de um bin\ que passou pela F6 e foi revogado: o dono fixo
--     (com o registro da cópia de antes do dono), o install_id, a URL, a
--     conta e o e-mail da nuvem de teste, sem o aparelho;
--   - as preferências;
--   - 200 ditados dos últimos ~108 dias, de três modos, com acento, aspas e
--     cedilha; um em cada dez na lixeira; um em cada 25 que falhou (sem o
--     texto final); um em cada sete sem idioma, modelo, duração e confiança;
--   - 15 ditados de mais de 6 meses (três na lixeira);
--   - um ditado com emoji; um com o texto acima do teto da nuvem (70 mil
--     caracteres: o PC o recusa sem mandar); um com a data três dias no
--     futuro (a nuvem o recusa);
--   - três linhas de outro dono (de um Agent anterior ao dono fixo: ficam
--     fora do painel e não são tocadas);
--   - uma entrada do dicionário antigo e uma correção ligada a um ditado.

INSERT INTO instalacao (nome, valor) VALUES
    ('dono_local', '0199a000-0000-7000-8000-00000000d0d0'),
    ('dono_local_migrado_em', '2026-09-29T10:15:00.000000000Z'),
    ('dono_local_copia', 'C:\Desenvolvimento\aurora-whisper\bin\historico.db.antes-do-dono-20260929-101500.db'),
    ('install_id', '0199a000-0000-7000-8000-0000000001d1'),
    ('nuvem_url', 'http://localhost:8080'),
    ('nuvem_conta', '00000000-7e57-7000-8000-000000000001'),
    ('nuvem_email', 'voce@teste.local');

INSERT INTO local_settings (id, hotkey, hold_threshold_ms, beep_enabled, overlay_position, microphone_device_id,
    autostart, asr_policy, local_model_id, local_context_enabled, protected_field_policy, local_port, updated_at,
    version, text_mode, ai_provider, ai_model, microphone_always_ready)
VALUES (1, 'ctrl+win', 300, 1, 'bottom', NULL, 1, 'local_only', 'small-q4_0', 0, 'block', 0,
    '2026-10-01T09:00:00.000000000Z', 7, 'polish', 'anthropic', 'claude-haiku', 1);

-- Os 200 dos últimos ~108 dias.
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 200)
INSERT INTO local_transcriptions (id, user_id, device_id, source_platform, raw_text, final_text, mode, language,
    asr_provider, asr_model, duration_ms, processing_ms, confidence, target_app, status, created_at, updated_at,
    deleted_at, purge_at, version, sync_state, server_version, last_sync_error)
SELECT
    printf('0199a000-0000-7000-8000-%012d', i),
    '0199a000-0000-7000-8000-00000000d0d0',
    NULL,
    'windows',
    CASE WHEN i % 25 = 0 THEN '' ELSE 'bruto ' || i || ' com acento: ação, coração e "aspas"' END,
    CASE WHEN i % 25 = 0 THEN '' ELSE 'Final ' || i || ', com acento: ação, coração e "aspas".' END,
    CASE i % 3 WHEN 0 THEN 'smart' WHEN 1 THEN 'literal' ELSE 'polish' END,
    CASE WHEN i % 7 = 0 THEN NULL ELSE 'pt' END,
    'whisper-local',
    CASE WHEN i % 7 = 0 THEN NULL ELSE 'small-q4_0' END,
    CASE WHEN i % 7 = 0 THEN NULL ELSE 1000 + i * 37 END,
    300 + i,
    CASE WHEN i % 7 = 0 THEN NULL ELSE 0.5 + (i % 50) / 100.0 END,
    NULL,
    CASE WHEN i % 25 = 0 THEN 'error' ELSE 'done' END,
    strftime('%Y-%m-%dT%H:%M:%S', 'now', printf('-%d hours', i * 13)) || '.' || printf('%09d', i * 1000) || 'Z',
    strftime('%Y-%m-%dT%H:%M:%S', 'now', printf('-%d hours', i * 13 - 1)) || '.' || printf('%09d', i * 1000) || 'Z',
    CASE WHEN i % 10 = 0 THEN strftime('%Y-%m-%dT%H:%M:%S', 'now', printf('-%d hours', i * 13 - 2)) || '.000000000Z' END,
    CASE WHEN i % 10 = 0 THEN strftime('%Y-%m-%dT%H:%M:%S', 'now', printf('-%d hours', i * 13 - 2), '+30 days') || '.000000000Z' END,
    1 + i % 4,
    'pending',
    NULL,
    NULL
FROM n;

-- Os 15 de mais de 6 meses (três na lixeira).
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 15)
INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode, language, asr_provider,
    asr_model, duration_ms, processing_ms, confidence, status, created_at, updated_at, deleted_at, purge_at, version,
    sync_state)
SELECT
    printf('0199a000-0000-7000-8000-%012d', 1000 + i),
    '0199a000-0000-7000-8000-00000000d0d0',
    'windows', 'antigo ' || i, 'Antigo ' || i || '.', 'smart', 'pt', 'whisper-local', 'small-q4_0', 2000, 400, 0.9,
    'done',
    strftime('%Y-%m-%dT%H:%M:%S', 'now', printf('-%d days', 199 + i)) || '.000000000Z',
    strftime('%Y-%m-%dT%H:%M:%S', 'now', printf('-%d days', 199 + i)) || '.000000000Z',
    CASE WHEN i % 5 = 0 THEN strftime('%Y-%m-%dT%H:%M:%S', 'now', '-2 days') || '.000000000Z' END,
    CASE WHEN i % 5 = 0 THEN strftime('%Y-%m-%dT%H:%M:%S', 'now', '+28 days') || '.000000000Z' END,
    1, 'pending'
FROM n;

-- Um com emoji, um acima do teto da nuvem e um no futuro.
INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode, asr_provider, status,
    created_at, updated_at, version, sync_state)
VALUES
    ('0199a000-0000-7000-8000-000000002001', '0199a000-0000-7000-8000-00000000d0d0', 'windows',
        'ditado com emoji', 'Ditado com emoji 🎙️ e acentuação: coração.', 'smart', 'whisper-local', 'done',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '-30 minutes') || '.000000000Z',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '-30 minutes') || '.000000000Z', 1, 'pending'),
    ('0199a000-0000-7000-8000-000000002002', '0199a000-0000-7000-8000-00000000d0d0', 'windows',
        'texto enorme', hex(zeroblob(35000)), 'literal', 'whisper-local', 'done',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '-40 minutes') || '.000000000Z',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '-40 minutes') || '.000000000Z', 2, 'pending'),
    ('0199a000-0000-7000-8000-000000002003', '0199a000-0000-7000-8000-00000000d0d0', 'windows',
        'relógio adiantado', 'Relógio adiantado.', 'smart', 'whisper-local', 'done',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '+3 days') || '.000000000Z',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '+3 days') || '.000000000Z', 1, 'pending');

-- Três de outro dono (um Agent anterior ao dono fixo, sobre o mesmo banco).
INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode, asr_provider, status,
    created_at, updated_at, version, sync_state)
VALUES
    ('0199a000-0000-7000-8000-000000003001', 'mauro', 'windows', 'de outro dono 1', 'De outro dono 1.', 'smart',
        'whisper-local', 'done', strftime('%Y-%m-%dT%H:%M:%S', 'now', '-1 days') || '.000000000Z',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '-1 days') || '.000000000Z', 1, 'pending'),
    ('0199a000-0000-7000-8000-000000003002', 'mauro', 'windows', 'de outro dono 2', 'De outro dono 2.', 'smart',
        'whisper-local', 'done', strftime('%Y-%m-%dT%H:%M:%S', 'now', '-2 days') || '.000000000Z',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '-2 days') || '.000000000Z', 1, 'pending'),
    ('0199a000-0000-7000-8000-000000003003', 'local', 'windows', 'de outro dono 3', 'De outro dono 3.', 'smart',
        'whisper-local', 'done', strftime('%Y-%m-%dT%H:%M:%S', 'now', '-3 days') || '.000000000Z',
        strftime('%Y-%m-%dT%H:%M:%S', 'now', '-3 days') || '.000000000Z', 1, 'pending');

INSERT INTO local_dictionary_entries (id, user_id, canonical_form, normalized_form, language, domain, priority,
    active, created_at, updated_at)
VALUES ('e1', '0199a000-0000-7000-8000-00000000d0d0', 'ECF', 'ecf', 'pt', 'contabil', 50, 1,
    '2026-09-20T10:00:00.000000000Z', '2026-09-20T10:00:00.000000000Z');

INSERT INTO local_corrections (id, user_id, transcription_id, from_text, to_text, normalized_from, normalized_to,
    language, confirmed_by_user, use_count, created_at, updated_at)
VALUES ('c1', '0199a000-0000-7000-8000-00000000d0d0', '0199a000-0000-7000-8000-000000000003', 'SF', 'ECF', 'sf', 'ecf',
    'pt', 1, 2, '2026-09-21T10:00:00.000000000Z', '2026-09-21T10:00:00.000000000Z');
