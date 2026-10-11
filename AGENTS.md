# AGENTS.md

## Project overview
Aurora Whisper é um sistema de ditado por voz para Windows com sync cloud e preparação para Android. Consulte `docs/ai-spec/00-index.md` antes de implementar.

## Layout
- `cmd/agent/`: Windows Agent.
- `cmd/server/`: backend cloud.
- `internal/`: domínio/módulos Go.
- `web/`: dashboard React/TypeScript.
- `migrations/`: PostgreSQL.
- `deploy/`: Docker/Compose.
- `scripts/`: automações, montagem do instalador e benchmark.
- `installer/`: script do instalador (Inno Setup; ADR-018). Nunca execute o Setup nem o Agent.
- `testdata/`: fixtures/dataset.
- `docs/ai-spec/`: especificação.
- `docs/adr/`: ADRs.

## Commands
```bash
go test ./...
go vet ./...
go build ./cmd/agent
go build ./cmd/server
cd web && pnpm install && pnpm lint && pnpm test && pnpm build
```

## Conventions
- Go idiomático e interfaces somente em fronteiras úteis.
- React com TypeScript estrito.
- Migrations versionadas.
- APIs versionadas em `/api/v1`.
- IDs globais para suportar offline/sync.
- Sync idempotente.
- Contexto e áudio nunca persistidos por padrão.

## Constraints
- ASR não pode ser acoplado a modelo específico.
- Sem screenshot context.
- Sem deploy/push/release sem autorização.
- Sem alteração de serviços que já rodam no servidor.
- Sem secrets/logging sensível.
- Login não é obrigatório para uso local.

## Done
- critérios atendidos;
- testes/lint/build passam;
- docs atualizadas;
- sem secrets;
- relatório final completo.

## Start here
- `docs/ai-spec/00-index.md`
- `docs/ai-spec/03-acceptance-criteria.md`
- `docs/ai-spec/13-implementation-plan.md`
