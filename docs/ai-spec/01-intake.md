# Intake — Aurora Whisper

## Resumo interpretado
[CONFIRMED] Criar um aplicativo Windows 10/11 x64 que use uma tecla configurável como hold-to-dictate. Ao soltar, o áudio é transcrito, ambiguidades podem ser resolvidas por contexto/dicionário, e o texto é inserido no campo com foco de teclado.

[CONFIRMED] O sistema deve ter dashboard web local, dashboard cloud, histórico sincronizado, dicionário pessoal, aprendizado por correções, modos Literal/Smart/Polish, overlay de onda sonora, beep discreto e backend preparado para futuro Android.

## Classificação
Tipo principal:
- Sistema completo novo.

Tipos secundários:
- Automação com IA.
- UI.
- Integração externa.
- Infra/deploy.
- Segurança.
- Sincronização.

## Usuário
[CONFIRMED] Inicialmente apenas o proprietário.

[DECISION] Schema e APIs serão multiusuário desde o início.

## Plataforma e domínio
- [CONFIRMED] Windows 10/11 x64.
- [CONFIRMED] Servidor próprio Linux (uma distribuição atual) + Docker.
- [CONFIRMED] Domínio de produção configurável (`https://exemplo.com` neste documento).
- [CONFIRMED] Repositório: `https://github.com/MauroJulianiJunior/aurora-whisper`.
- [DECISION] Código aberto, licença MIT.

## Stack
- [CONFIRMED] Go para Agent e backend.
- [CONFIRMED] React + TypeScript + Vite.
- [CONFIRMED] SQLite local.
- [CONFIRMED] PostgreSQL cloud.
- [CONFIRMED] Google OAuth cloud.
- [CONFIRMED] ASR selecionado por benchmark e desacoplado.

## Hardware de referência
- Dell G15.
- Intel Core i7-13650HX.
- 32 GB RAM.
- GPU dedicada com 6 GB VRAM.
- ~150 GB livres.
- Meta inicial de working set do ASR local: ~1 GB.

## Servidor
- Servidor próprio, com outros serviços já em execução.
- Um servidor menor serve de alternativa.
- Containers isolados.
- Inferência no servidor só após benchmark.

## Tecla
- configurável;
- default sugerido: F;
- Shift/Ctrl/Alt/Win não podem ser tecla principal;
- tap curto preserva função normal;
- hold ativa ditado;
- áudio começa a bufferizar no keydown;
- threshold default 700 ms [ASSUMPTION], configurável;
- com modificadores, bypass de ditado por default.

## Feedback
- overlay centro inferior;
- onda + duração;
- beep discreto configurável;
- recording/processing/success/error.

## Inserção
- alvo = campo com foco;
- UI Automation primeiro;
- fallback clipboard + paste;
- restaurar clipboard.

## Contexto
- UI Automation.
- Sem screenshots.
- App/controle/texto próximo/seleção quando disponível.
- Efêmero e não sincronizado.
- Cloud contextual permitido. **Depois (ADR-019, 01-out-2026):** na etapa 5 o contexto nunca vai para a nuvem; a rota `/resolve` não é montada.
- Campos protegidos bloqueados por default.

## Dicionário/aprendizado
- vital no MVP;
- termo canônico + aliases;
- correção explícita pode virar aprendizado;
- sync entre devices. **Depois (ADR-019, 07-out-2026):** na etapa 5 sincroniza só o aprendido do painel
  (a troca do botão Aprender, D1); o dicionário completo fica para depois (REQ-017).

## Modos
- Literal.
- Smart (default).
- Polish.

## ASR
- PT-BR + inglês.
- Code-switching avaliado.
- Meta release→insert ideal <500 ms.
- Partial/VAD durante gravação.
- Áudio descartado ao final.
- Local/cloud/servidor atrás do mesmo contrato.

## Cloud/sync
- local funciona sem login;
- cloud usa Google;
- PostgreSQL é fonte consolidada;
- SQLite é cache/offline/outbox;
- histórico/dicionário/correções/preferências compatíveis/devices sincronizam. **Depois (ADR-019):** na
  etapa 5 sincronizam o histórico, o aprendido do painel (D1 da F8) e os aparelhos; o dicionário
  completo, as correções e as preferências ficam para depois (REQ-017 e REQ-018);
- áudio/contexto não sincronizam.

## Retenção
- histórico: 6 meses;
- trash manual: 30 dias;
- áudio: não persistir;
- contexto: não persistir.

## Distribuição
- installer;
- tray;
- autostart configurável;
- model manager;
- update com confirmação;
- GitHub Releases pode ser origem, mas cliente não depende de token GitHub.

## Autonomia Claude Code
Pode: criar estrutura, dependências, migrations, Docker, CI, testes, scripts e commits locais.
Não pode sem autorização: deploy, alterar o servidor, apagar dados, usar credenciais reais, publicar release, fazer push.

## Suposições
| ID | Suposição | Validação |
|---|---|---|
| ASM-001 | Hold default 700 ms | teste UX |
| ASM-002 | Mesmo domínio serve web/API | deploy |
| ASM-003 | Update mediado por endpoint cloud | resolver repo visibility |
| ASM-004 | pnpm no frontend | bootstrap |

## Riscos
| ID | Risco | Mitigação |
|---|---|---|
| RISK-001 | ASR local não cumprir qualidade/latência/RAM | benchmark + fallback cloud |
| RISK-002 | hook conflitar com apps | bypass + testes |
| RISK-003 | UIA limitado em alguns apps | degradação graciosa |
| RISK-004 | inserção falhar | UIA + clipboard |
| RISK-005 | conflitos de sync | idempotência + versionamento |
| RISK-006 | Servidor compartilhado pressionado | inference off por default |
| RISK-007 | beep capturado | configurável + teste |
| RISK-008 | Polish alterar significado | modo separado + raw/final |
