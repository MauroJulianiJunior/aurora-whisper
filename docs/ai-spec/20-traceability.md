# Matriz de Rastreabilidade

| Requisito | AC | Documento | Tarefa |
|---|---|---|---|
| REQ-001..005 | AC-001..004 | System Design | TASK-002 |
| REQ-006..008 | AC-005..010 | System Design/UI | TASK-003 |
| REQ-009..011 | AC-007..009 | System Design | TASK-004 |
| REQ-012..013 | AC-011..013 | UI | TASK-005 |
| REQ-014..018 | AC-014..017 | API/Data | TASK-012..016 |
| REQ-019..020 | AC-021..022 (aprender no painel: ADR-017; a parte de sync do AC-021 é a F8 da ADR-019, implementada e provada ponta a ponta em `internal/server/integration/sync_aprendido_e2e_test.go`, com dois PCs e o celular sobre a nuvem em memória; o teste manual é `docs/teste-manual-nuvem-f8.md`), AC-064 (adicionar termo e a dica: ADR-020) | AI/Data | TASK-008/019 |
| REQ-021..023 | AC-023..025, AC-046..051 (Polish, ADR-016) | AI | TASK-010/011/011b |
| REQ-024..026 | AC-026..028 | Security/System | TASK-009/003 |
| REQ-027..031 | AC-029..032 (a parte da nuvem de REQ-028 e AC-029, 030 e 032: depois, ADR-019; o celular tem a rota própria da ADR-021, AC-065 e AC-068) | Eval/Contracts | TASK-006/007/017/018 (no celular: TASK-028) |
| REQ-032..034 | AC-033..034 | Data/Flows | TASK-005/015 |
| REQ-035..036 | AC-018..020 | Data | TASK-020 |
| REQ-037 | AC-035 | Data/Security | TASK-012 |
| REQ-038..041 | AC-037..039 | UI/Rollout | TASK-021..023 |
| REQ-042..043 | AC-041..042 (REQ-043 e AC-041: depois, ADR-019; no ditado do celular, AC-068 e AC-065, ADR-021) | API | TASK-017 (no celular: TASK-028) |
| REQ-044 | AC-016..017 | Sync | TASK-015 |
| REQ-045 | AC-010 | Flows | TASK-003/007 |
| REQ-046 | AC-036 | Security | TASK-009 |
| REQ-047 | AC-043 | UI/Obs | TASK-005/016 |
| REQ-048..050 (celular, contas permitidas, conectar o PC; ADR-019) | AC-052..063 | API/Data/Flows/UI/Security (ADR-019) | Etapa 5, fatias F1 a F11 (`13`) |
| REQ-051 (ditar pelo celular; ADR-021) | AC-065, AC-068..AC-077 (e AC-058, AC-041, AC-040, AC-031, emendados) | API/Data/Flows/UI/Security (ADR-021) | TASK-028 (ADR-021: as subfatias C1 a C7, e C8 opcional) |
| REQ-052 (o aceite específico; ADR-021) | AC-066, AC-078 | Security/UI (ADR-021; `docs/politica-de-privacidade.md`, `docs/lgpd-dossie.md`) | TASK-028 (ADR-021: D0, C3b e C6b) |
| REQ-053 (a chave da pessoa cifrada no servidor; ADR-021) | AC-065, AC-067 (e AC-040, emendado) | Security/API (ADR-021) | TASK-028 (ADR-021: C3a) |
| REQ-054 (a chegada mais rápida ao PC; ADR-021; `GET /api/nuvem/mudancas`, `06`) | AC-079 | Sync/UI (ADR-021) | TASK-028 (ADR-021: S) |
| ADR-022, N7: notas e marcadores no sync, **o lado do PC** (a fila na mesma transação da gravação, a capacidade `entidades`, o envio inicial, o lote, as decisões do push e do pull, o selo, a lápide local, o marcador que espera, o recomeço e o esquecer). Sem REQ numerado, como as fases N1 a N6 | AC-091. Testes: `internal/storage/nota_sync_test.go`, `internal/sync/decisao_nota_test.go`, `internal/sync/nota_motor_test.go` | Sync/Data (ADR-022, "Decisões da N7"; ADR-019, emenda da N7; `05`, `07`) | TASK-030 (N7) |
| ADR-022, N7: notas e marcadores no sync, **o lado da nuvem** (as migrations `0020` a `0025`, `DecideNote` e `DecideLabel`, a mescla da lista de marcar, a lápide que não vence, o `virou_nota` que esconde o ditado do celular, `entidades` no pull, `GET /api/v1/notes` e `/labels`) | AC-091. Testes: `internal/server/cloudstore/notas_regras_test.go`, `internal/server/cloudstore/lista_de_marcar_test.go`, `internal/server/contracttest/notas.go` (nos dois Stores), `internal/server/pgstore/notas_test.go`, `migrations/schema_test.go` e `migrations/postgres_test.go`, e a ponta a ponta `internal/server/integration/sync_notas_e2e_test.go` (os testes contra o Postgres de verdade ainda não rodaram) | API/Data (`05`, `06`) | TASK-030 (N7) |
| ADR-022, N7: o selo do conflito da nota no PC ("Versão em conflito", "Manter esta", "Usar a outra") | AC-091. Testes: `internal/agent/localapi/notas_selo_test.go`, `web/src/notasSeloNoPC.test.tsx` | UI/API local (`06`, `07`) | TASK-030 (N7) |
| ADR-022, N7: o texto das notas na nuvem sem aparecer em log, lápides só com ids, o ditado que virou nota sob os 6 meses da nuvem | AC-091 (a parte de log: `testNotasNadaDeTextoEmLog` em `internal/server/contracttest/notas.go`, que roda nos dois Stores; `migrations/schema_test.go` confere que nenhuma coluna guarda áudio nem contexto; a revisão de LGPD das notas ainda não foi feita) | Security/Obs (`09`, `11`) | TASK-030 (N7) |
| ADR-022, N7: o roteiro do Mauro (deploy e teste no PC com dois aparelhos) | AC-091, a parte que os dublês não cobrem | Rollout (`docs/deploy-notas-n7.md`, `docs/teste-manual-notas-n7.md`) | TASK-030 (N7), do Mauro |
