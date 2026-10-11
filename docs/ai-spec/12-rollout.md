# Rollout Plan — Aurora Whisper

## Estratégia
1. dev/harness local;
2. alpha Windows sem cloud;
3. alpha cloud/sync;
4. beta pessoal;
5. preparação Android.

**Etapa 5 (ADR-019, 01-out-2026):** o passo 3 vem junto com o celular Android por PWA (o passo 5 deixa
de ser só preparação). Tudo roda primeiro no PC (fatias F1 a F10, com o servidor em `localhost` e o
`bin\` com dados e conta de teste); o deploy é a F11, ponto de parada.

## Pré-requisitos
- CI verde. **Etapa 5 (ADR-019, F11h): não se cumpre até o `git push`**, que espera uma decisão à
  parte (D4). Até lá, o que faz as vezes do CI está em `deploy/README.md` ("O que substitui o CI"): o
  `go test ./...` (com os testes do `deploy/` e a varredura de segredos), o `go vet ./...`, o lint, o
  teste e o build do `web/`, e o `scripts\deploy-teste.ps1` do dono;
- benchmark ASR;
- Google OAuth;
- DNS/TLS;
- servidor escolhido;
- backup Postgres antes de dados importantes.
- **Etapa 5 (ADR-019):**
  - o diagnóstico só de leitura do servidor (`scripts/diagnostico-servidor.sh`) com RAM e disco com
    folga;
  - a revisão de LGPD, antes do primeiro dado real;
  - a retenção na nuvem e as lápides prontas (F9) antes de qualquer dado real;
  - o backup cifrado fora do servidor e um teste de restauração feito;
  - os segredos gerados pelo dono no servidor.

## Cloud
[SECURITY][BLOCKER] Deploy só com aprovação.

Passos:
1. network/volumes isolados;
2. Postgres;
3. migrations explícitas;
4. API;
5. readiness;
6. reverse proxy/TLS;
7. DNS e proxy/CDN, se houver;
8. OAuth;
9. auth/sync tests;
10. retention job;
11. inference off até benchmark.

Na etapa 5 (ADR-019): um compose só do projeto (API e Postgres numa rede própria, Postgres sem porta
publicada, limite de memória); as migrations pelo comando `migrate`, à parte da subida; sem CDN de
terceiro na frente; com as portas 80 e 443 livres, um Caddy dedicado; com um proxy já rodando,
**encostar nele é ponto de parada**. Quem roda cada passo no servidor é o dono, pelo roteiro da F11.

**F11, a D1 (confirmada pelo dono em 09-out-2026):** a nuvem mora numa VPS (datacenter no Brasil), em
`https://aurora-whisper.mjj.net.br`, atrás do Traefik v3 que já atende os outros serviços do servidor,
**só pelos rótulos do próprio contêiner** (a rede `web`, o entrypoint `websecure`, o
resolvedor `letsencrypt`, o log de acesso desligado no nosso router): nada do Traefik é editado nem
reiniciado. O roteiro é o `docs/deploy-f11.md`, na ordem:

1. o login com o Google de verdade no PC (`scripts\nuvem-local.ps1`; feito);
2. o bucket do B2 com Object Lock e a chave restrita (`docs/backup-b2-f11.md`), e a chave do backup
   (`scripts\backup-chave.ps1`);
3. o deploy inteiro no Docker do PC (`scripts\deploy-teste.ps1`: as imagens de produção, o mesmo
   compose, um Traefik de teste, o backup e a restauração com o epoch novo, derrubado no fim);
4. as imagens aprovadas para a VPS por `ssh` (`scripts\enviar-para-servidor.ps1`), com a pasta
   `deploy/` e o JSON do Google;
5. os segredos na VPS (`gerar-segredos.sh`) e a conferência da chave do B2 (`conferir-b2.sh`);
6. **a revisão de LGPD aprovada** (o primeiro `migrate` congela as migrations) e o `subir.sh`: o NTP, a
   sub-rede da rede do Traefik como `PROXY_CONFIAVEL`, o Postgres e os cinco ajustes de log, o
   `migrate -status` e o `migrate` depois de "digite SIM", "digite SIM" antes da API, a API (a retenção
   antes da porta; o log na ordem arrumação, retenção, servindo), o backup e o https pela borda;
7. a conferência de fora (`scripts\conferir-nuvem.ps1`), o login uma vez (a conta) e a recusa da
   segunda conta;
8. o primeiro backup e a restauração dele no PC (`scripts\teste-restauracao-mensal.ps1`);
9. o teste final (`docs/teste-final-f11.md`): o celular, o PC conectado, o envio do histórico, o
   aprendido pelo celular, revogar, ficar sem internet, e a palavra-sentinela nos logs.

## Windows
- build;
- installer;
- validação em máquina limpa;
- hotkey/audio/overlay/insertion;
- update checker;
- release só com aprovação.

## Feature/config flags
| Flag | Default |
|---|---|
| cloud_sync_enabled | on após login. **ADR-019:** off até quem usa conectar o PC (é a ação dela, nunca automática); on depois |
| cloud_asr_enabled | conforme config. **Depois (ADR-019):** off; a rota não é montada |
| cloud_context_resolve | on se Smart e permitido. **Depois (ADR-019):** off; contexto não vai para a nuvem na etapa 5 |
| polish_mode_enabled | on, não default: opt-in nas Configurações (`text_mode=polish`), com a chave de quem usa (ADR-016). Nunca vira default sem eval |
| server_inference_enabled | off |
| auto_update_install | off |

## Rollback
- manter imagem anterior;
- migration destrutiva exige plano específico;
- Windows permite reinstalar versão compatível.
- **Restaurar o backup da nuvem (ADR-019; F11d)** é seguido do `aurora-whisper-server
  pos-restauracao -confirmo`, com a API parada: troca o `sync_epoch` de todas as contas e revoga
  todas as sessões e todos os aparelhos (o backup guarda o `revoked_at` antigo). É um "revoke
  massivo" (`09`, "Operações destrutivas"): todo PC conecta de novo à mão e recomeça, e o celular
  entra de novo. **O que foi apagado de vez depois do backup pode voltar** (a decisão do Mauro de
  09-out-2026, "aceitar e deixar escrito"), e pode ser apagado de novo; o roteiro de restauração diz
  isso antes de o dono aprovar. A API roda a retenção antes de abrir a porta (F11c), então o que a
  janela de 6 meses já venceu não reaparece.

## Stop rollout if
- data loss/duplication;
- audio/context persisted;
- secret leaked;
- keyboard blocked;
- recurring crash;
- unresolved sync conflicts;
- memory grossly exceeds target;
- o servidor impacta serviços que já rodam nele (um dos outros serviços do servidor mais lento, ou um
  dos sites atrás do Traefik fora do ar);
- uma conta fora da lista permitida entra (ADR-019);
- texto ditado aparece em log, do servidor ou do proxy (ADR-019).

## Android futuro
Não implementar agora. Apenas garantir API/device auth/sync/ASR platform-agnostic.

**Emenda (ADR-019):** o celular Android entra na etapa 5 como PWA (o painel em modo nuvem). App
nativo, teclado (IME) e ditar no celular continuam futuros; o iPhone vem depois do MVP.
