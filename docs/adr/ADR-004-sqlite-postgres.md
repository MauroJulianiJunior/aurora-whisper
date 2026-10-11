# ADR-004 — SQLite no Windows e PostgreSQL na VPS

Status: Accepted

## Decisão
SQLite mantém estado local/offline; PostgreSQL mantém estado sincronizado cloud.

## Justificativa
O usuário precisa de operação offline e histórico multi-device.

## Consequências
Sync engine, outbox, cursor e optimistic concurrency são componentes centrais.
