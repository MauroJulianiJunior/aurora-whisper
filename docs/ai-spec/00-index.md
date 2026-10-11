# Índice da Especificação — Aurora Whisper

## Projeto
Aurora Whisper

## Objetivo
Aplicativo de ditado para Windows 10/11 x64 com hold-to-talk, ASR substituível, resolução contextual, dicionário pessoal, inserção no campo focado, dashboard web local/cloud e sincronização preparada para Android.

## Status
`Ready for implementation with tracked pending decisions`

Não há blocker para iniciar desenvolvimento local. Decisões que dependem de benchmark/deploy permanecem marcadas.

## Ordem de leitura
1. `01-intake.md`
2. `02-prd.md`
3. `03-acceptance-criteria.md`
4. `07-flows.md`
5. `04-system-design.md`
6. `05-data-model.md`
7. `06-api-contract.md`
8. `08-ui-ux-spec.md`
9. `16-ai-behavior-spec.md`
10. `17-prompt-spec.md`
11. `18-tool-contract.md`
12. `19-eval-plan.md`
13. `09-security-privacy.md`
14. `10-env-and-secrets.md`
15. `11-observability.md`
16. `12-rollout.md`
17. `13-implementation-plan.md`
18. `14-review-checklist.md`
19. `15-agent-execution-prompt.md`
20. `20-traceability.md`
21. `../../CLAUDE.md`
22. `../../AGENTS.md`
23. `../adr/*`

## Pendências não bloqueantes
- [PENDING] Provider/modelo ASR local vencedor.
- [DEPOIS, ADR-019] Provider cloud ASR: a transcrição na nuvem fica para depois do MVP da etapa 5 (a rota não é montada). **Exceção, ADR-021:** o ditado do celular tem rota própria e repassa a voz ao provedor da pessoa (a `/asr/transcribe` segue não montada).
- [DEPOIS, ADR-019] Provider/modelo do Smart cloud (resolução contextual restrita): fica para depois; contexto não vai para a nuvem na etapa 5.
- [DECIDIDO 29-set-2026, ADR-016] Polish: preservar e organizar; quem usa escolhe OpenAI ou Anthropic e usa a própria chave (BYOK). Falta só o modelo sugerido, a medir no benchmark (S3).
- [DECIDIDO 30-set-2026, ADR-017] Aprender com as edições do painel: só com um clique de quem usa (nunca sozinho; desde a F8, o clique é no PC de origem, e os outros PCs recebem o par pelo sync, com as mesmas checagens do Aprender: ADR-019, "Decisões da F8", D2), guardado no `dicionario.json` (e não no SQLite, que a spec previa) com a origem "painel", válido em qualquer aplicativo; acento ou maiúscula sozinhos não se aprendem nesta etapa. A correção que sincroniza (TEST-008) e a ida para o banco ficam para a etapa 5.
- [DECIDIDO 01-out-2026, ADR-020] Adicionar termo pelo painel: em Configurações, a pessoa põe a palavra certa, as grafias erradas e a dica para o reconhecimento de voz (o prompt do ASR, trocado em memória, sem reiniciar); a dica é a prioridade (100) e o termo leva `"origem": "adicionado"`, sem campo novo no `dicionario.json`; a dica que não cabe nas 140 runas é recusada. Revoga a decisão 7 da ADR-017. As três decisões de detalhe estão com a recomendação, até o Mauro dizer o contrário.
- [DECIDIDO 30-set-2026, ADR-018] Instalador: Inno Setup por usuário (sem administrador), programa em `%LOCALAPPDATA%\Programs\Aurora Whisper` e dados em `%LOCALAPPDATA%\AuroraWhisper` (modo portátil por arquivo marcador, para o `bin\` de desenvolvimento); modelo de voz dentro do Setup; 0.x sem assinatura e ilustração da Aurora liberada (MIT); o instalador não liga o início automático; atualização manual nas 0.x. Nada distribuído: falta a revisão de LGPD e a autorização do Mauro para publicar.
- [DECIDIDO 01-out-2026, ADR-019] Nuvem e celular (etapa 5): o celular é um PWA instalável, **Android primeiro** (iPhone e o login pelo código de 6 dígitos depois do MVP); um servidor Go com Postgres; toda gravação com o evento de sync na mesma transação (ADR-009); sync por usuário, com fila de um marcador por entidade, conflito com selo, lápides e recomeço; login Google só para as contas de `CONTAS_PERMITIDAS`; o PC conecta por retorno a `127.0.0.1` com PKCE; sobem os últimos 6 meses sem a lixeira; texto legível no servidor, com a revisão de LGPD como pré-condição do deploy. Do dicionário, só o aprendido sincroniza (o `dicionario.json` continua a fonte); preferências, transcrição e contexto na nuvem e atualização pela nuvem ficam para depois.
- [DECIDIDO 10-out-2026, ADR-021; fase 1 na `main` em 11-out-2026] Ditar no celular (passo 1, o PWA): o app grava (toque para começar e toque para parar) e o servidor repassa a voz em webm/opus ao provedor que a pessoa escolheu (OpenAI ou Groq), com a chave dela (BYOK) guardada **cifrada** no servidor; o texto passa pelo Smart da conta (o aprendido) e, se ela ligar, pelo Polish (OpenAI ou Anthropic); o ditado é gravado pelo ponto único e desce aos PCs pelo sync. Rota própria, `POST /api/v1/dictations`, só com a sessão do navegador; aceite específico e por conta antes de tudo (a voz sai do aparelho de quem dita); a voz fica só em memória e é descartada ao fim; a chave do PC continua só no PC. A chegada ao PC fica mais rápida (10 s com o painel aberto, 60 s fechado). **Nenhum áudio real sai do aparelho antes do OK do Mauro na política e no dossiê reescritos.** Falta a rota (C5b), o compose (C5c) e o deploy no aparelho (C7); as respostas de LGPD (LG) são dele. O passo 2 (app nativo Android, bolha flutuante, teclado) é outra ADR. Emenda a ADR-019 (decisões 8 e 9, D1 e "Fora do escopo"), a ADR-016 (a chave do PC), a ADR-003 e o `18-tool-contract.md` (o áudio comprimido). Ver `13-implementation-plan.md`, TASK-028.
- [PENDING] Visibilidade final do repo GitHub.
- [DECIDIDO 01-out-2026, ADR-019, com condição] VPS da primeira implantação: a atual, num compose isolado, se o diagnóstico só de leitura (`scripts/diagnostico-servidor.sh`) mostrar RAM e disco com folga. Encostar no proxy que já roda lá é ponto de parada.

## Riscos principais
- ASR local pode não cumprir simultaneamente memória/latência/qualidade.
- UI Automation não fornece contexto em todos os apps.
- Keyboard hook precisa ser testado para não interferir com atalhos.
- Sync multi-device precisa ser idempotente.
- VPS compartilhada não deve receber inference pesada sem medição.
- Com a nuvem conectada, o texto ditado fica legível no servidor de quem hospeda (ADR-019).
- No ditado do celular, a voz sai do aparelho para um provedor estrangeiro que a pessoa escolhe, passa pela memória do servidor, e uma chave de cobrança dela mora cifrada no servidor (ADR-021).

## Uso com Claude Code
Prompt sugerido:

> Leia `CLAUDE.md` e `docs/ai-spec/00-index.md`. Comece pela primeira vertical slice ainda não implementada em `13-implementation-plan.md`. Planeje antes de editar e siga os critérios de parada.

## Fonte da verdade
Em conflito:
1. decisão humana mais recente registrada;
2. PRD para escopo;
3. Security para restrições;
4. System Design para arquitetura;
5. Acceptance Criteria para comportamento verificável;
6. Implementation Plan para ordem.
