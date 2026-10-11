# ADR-002 — Go + React + PostgreSQL

Status: Accepted

## Decisão
Go no Windows Agent/backend, React+TypeScript no dashboard e PostgreSQL na nuvem.

## Justificativa
Go produz binários simples e código enxuto. React permite UI local/cloud compartilhada. PostgreSQL atende integridade e sync.

## Consequências
Integrações nativas podem exigir bindings/helper, sempre atrás de interfaces.
