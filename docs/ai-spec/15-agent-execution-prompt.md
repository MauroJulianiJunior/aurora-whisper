# Agent Execution Prompt

## Claude Code

Leia:
1. `CLAUDE.md`
2. `docs/ai-spec/00-index.md`
3. `docs/ai-spec/03-acceptance-criteria.md`
4. `docs/ai-spec/13-implementation-plan.md`
5. documentos técnicos da slice.

Objetivo:
Implementar Aurora Whisper por vertical slices, começando pela TASK-001 e nunca fixando ASR default antes do benchmark.

Antes de editar:
- explore o repo;
- confirme a slice;
- liste arquivos prováveis;
- identifique dependências/conflitos;
- gere plano curto.

Durante:
- preserve decisões;
- ASR desacoplado;
- não persista áudio/contexto;
- sem screenshots;
- sem push/deploy/release;
- sem credenciais reais;
- rode testes;
- atualize docs quando contrato/schema/comando mudar.

Git:
- commit local por slice verde;
- nunca push automático.

Pare se:
- benchmark falhar nos gates;
- migration destrutiva;
- deploy/DNS/servidor/secrets reais;
- mudança de privacidade;
- necessidade de persistir áudio;
- conflito entre docs.

Ao final:
- resumo;
- arquivos;
- comandos;
- testes;
- ACs;
- riscos;
- commit.

## Codex

Goal:
Implementar a vertical slice atual de `13-implementation-plan.md`.

Context:
Leia `AGENTS.md`, `00-index.md`, ACs e docs da slice.

Constraints:
- sem deploy/push/release;
- sem secrets;
- sem audio/context persistence;
- sem screenshots;
- ASR provider;
- offline + sync preservados.

Done when:
- critérios passam;
- testes/lint/build passam;
- docs atualizadas;
- commit local;
- relatório final.

## Generic Agent
Explore → planeje → implemente 1 slice → teste → revise segurança → atualize docs → commit local → reporte.
