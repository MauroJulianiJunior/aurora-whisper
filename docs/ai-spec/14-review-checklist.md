# Review Checklist — Aurora Whisper

## Produto
- [ ] Hold-to-dictation.
- [ ] Tap curto.
- [ ] Overlay/beep.
- [ ] Inserção no foco.
- [ ] Histórico local/cloud.
- [ ] Dicionário/aprendizado.
- [ ] Literal/Smart/Polish.
- [ ] Offline.

## Arquitetura
- [ ] ASR desacoplado.
- [ ] SQLite/Postgres.
- [ ] React compartilhado.
- [ ] UIA sem screenshot.
- [ ] secrets do projeto cloud-only (exceção: a chave de quem usa, BYOK, no Gerenciador de Credenciais; ADR-016).

## ASR
- [ ] benchmark real.
- [ ] provider vencedor documentado.
- [ ] RSS.
- [ ] PT-BR.
- [ ] inglês.
- [ ] code-switching.
- [ ] Entity Accuracy.
- [ ] release→insert.

## Dados
- [ ] multiuser.
- [ ] IDs globais.
- [ ] optimistic concurrency.
- [ ] outbox/idempotency.
- [ ] 6m retention.
- [ ] 30d trash.
- [ ] sem áudio/contexto DB.

## API
- [ ] web auth.
- [ ] device auth.
- [ ] CRUD.
- [ ] sync.
- [ ] ASR gateway.
- [ ] resolve.
- [ ] update feed.
- [ ] rate limits.
- [ ] errors.

## UI
- [ ] Home.
- [ ] History.
- [ ] Detail.
- [ ] Dictionary.
- [ ] Corrections.
- [ ] Settings.
- [ ] Models.
- [ ] Devices.
- [ ] Trash.
- [ ] loading/empty/error/offline.

## Segurança
- [ ] loopback only.
- [ ] local token.
- [ ] Host/Origin.
- [ ] CSRF.
- [ ] Credential Manager.
- [ ] server-side secrets (a chave de quem usa não é um deles: fica no Credential Manager do Agent).
- [ ] Polish: opt-in, só o texto sai da máquina, nenhum texto nem chave no log (ADR-016).
- [ ] protected fields.
- [ ] sem keylogging.
- [ ] sem audio/context logs.
- [ ] CSP.
- [ ] secret scan.

## Windows
- [ ] Win10 x64.
- [ ] Win11 x64.
- [ ] hook não quebra atalhos.
- [ ] clipboard restore.
- [ ] tray.
- [ ] autostart.
- [ ] installer.
- [ ] update confirm.

## Cloud
- [ ] Compose isolado.
- [ ] Postgres não público.
- [ ] healthchecks.
- [ ] limits.
- [ ] retention jobs.
- [ ] inference off default.
- [ ] sem impacto nos serviços que já rodam no servidor.

## Agent
- [ ] CLAUDE.md.
- [ ] AGENTS.md.
- [ ] commits por slice.
- [ ] sem push/deploy.
- [ ] relatório por slice.

## Pendências
- [ ] repo visibility antes de release.
- [ ] cloud ASR provider.
- [ ] Smart cloud provider.
- [x] Polish provider: decidido na ADR-016 (quem usa escolhe OpenAI ou Anthropic, com a própria chave). Falta só o modelo sugerido, a medir no benchmark (S3).
- [ ] servidor final.
