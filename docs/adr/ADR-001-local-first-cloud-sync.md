# ADR-001 — Local-first com sincronização cloud

Status: Accepted

## Decisão
O Windows Agent deve funcionar localmente sem cloud. Quando autenticado, sincroniza dados persistentes com backend cloud, que atua como fonte consolidada entre devices.

## Justificativa
Ditado não pode depender de internet, mas histórico/dicionário precisam ser multi-device.

## Consequências
- SQLite local.
- PostgreSQL cloud.
- Outbox/cursor/idempotência.
- Conflitos precisam ser tratados.
