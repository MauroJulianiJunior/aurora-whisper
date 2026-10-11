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

**Ditar no celular (ADR-021, 10-out-2026):** vem depois da F11. A ordem é a da ADR: a S (a chegada mais
rápida ao PC) primeiro, a D0 (os documentos) antes de qualquer código do ditado, depois a C1 a C5c no
PC, com o Postgres e os provedores falsos, a C6a a C6c no app, e a C7 (no ar e no aparelho) por último.
**O OK do Mauro na ADR libera o código do ditado; o OK dele na política e no dossiê reescritos libera o
primeiro áudio real.** A C8 (medir a qualidade e a dica) é opcional.

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
- **Ditar no celular (ADR-021)**, além do que a F11 já pede:
  - o OK do Mauro na política e no dossiê reescritos (D0), antes do primeiro áudio real; a LG (as
    pendências L1 a L5 do dossiê) é recomendada antes dele;
  - a chave de cifra (`AI_KEYS_SECRET_FILE`) gerada pelo dono na VPS, pelo passo da C3a, com a saída
    suprimida, e as migrations 0017 a 0019, que são só aditivas;
  - o `memswap_limit`, o `ulimits: core: 0` e o `stop_grace_period` de 90 s no compose da API;
  - a leitura, só de leitura, da configuração do proxy que já roda na VPS (o `readTimeout`, o
    `writeTimeout` e os middlewares do entrypoint) e da swap da VPS, antes do primeiro áudio. Nunca
    editar o proxy;
  - a chave da OpenAI do dono, só para o celular e com limite de gasto no provedor, colada por ele no
    app, **nunca no chat**.

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
11. inference off até benchmark. **ADR-021:** o servidor nunca roda inferência de voz: no ditado do
    celular ele só repassa a voz ao provedor que a pessoa escolheu, e o whisper na própria VPS foi
    descartado (disputaria a CPU com os serviços vizinhos).

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

### Ditar no celular: a C7 (ADR-021, ponto de parada)
Depois do teste final da F11 e do OK do Mauro na política e no dossiê reescritos. Quem roda cada passo é
o dono, pelo roteiro de deploy (`docs/deploy-f11.md`, que a C5c atualiza):

1. **A chave de cifra.** O passo próprio da C3a cria só o arquivo `AI_KEYS_SECRET_FILE` numa VPS que já
   está no ar (o `gerar-segredos.sh` recusa rodar quando a pasta `segredos/` existe), com a saída
   suprimida. Sem o arquivo, as rotas do ditado, das chaves e das preferências não são montadas (`404`).
2. **As migrations 0017 a 0019**, pelo comando `migrate`, à parte da subida e com "digite SIM".
3. **O compose** com o segredo novo, o `memswap_limit`, o `ulimits` e o `stop_grace_period`.
4. **A leitura, antes do primeiro áudio:** o `readTimeout`, o `writeTimeout` e os middlewares do
   entrypoint do proxy; o `docker info` sem o aviso "No swap limit support" e o `memory.swap.max` do
   contêiner em `0`, lido no host. **Se não der `0`, para e pergunta.**
5. **No app:** o dono cola a chave dele (uma chave só do celular, com limite de gasto; a residência de
   dados e a retenção zero do provedor, se a conta tiver) e dá o aceite.
6. **No Android:** um ditado curto, um de 30 s, um de 120 s (com os bytes medidos contra o teto de
   1 MiB), um silêncio (sem envio), sem rede ("Tentar de novo"), a tela apagando, a troca de app no
   meio e uma ligação recebida. A mediana do toque final até o texto, o `docker stats` da API sem salto
   e o ditado chegando ao PC.

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
| cloud_asr_enabled | conforme config. **Depois (ADR-019):** off; a rota não é montada. **ADR-021:** continua off para o PC (`/asr/transcribe` não existe); o ditado do celular tem os interruptores abaixo |
| ditado no celular (ADR-021) | off até o arquivo da chave de cifra (`AI_KEYS_SECRET_FILE`) existir no servidor: sem ele a rota, as chaves e as preferências não são montadas (`404`). Por conta, ele só funciona depois do aceite |
| dica do aprendido no celular (ADR-021) | off, por conta, até a C8 medir; a pessoa liga nas preferências do celular |
| Polish no celular (ADR-021) | off, por conta, opt-in: a pessoa liga nas preferências do celular, com o aceite do provedor do Polish |
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
- **Ditar no celular (ADR-021):** voltar à imagem anterior desliga a rota, as chaves e as preferências
  (as tabelas das migrations 0017 a 0019 ficam, porque são só aditivas). Sem o arquivo da chave de
  cifra, as rotas caem no `404` e as chaves guardadas ficam no banco, ilegíveis. **Trocar o arquivo da
  chave de cifra leva toda chave guardada a `SEM_CHAVE`:** cada pessoa cola a dela de novo, e o servidor
  não dá erro `500`. Restaurar um backup sem a mesma chave de cifra tem o mesmo efeito. Num incidente, a
  saída é **revogar as chaves no provedor**, porque quem toma a VPS lê o arquivo da chave de cifra e o
  banco; o dossiê (`docs/lgpd-dossie.md`, seção 6) diz isso.

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
- texto ditado aparece em log, do servidor ou do proxy (ADR-019);
- áudio, chave de API da pessoa, texto do ditado do celular ou corpo de erro do provedor aparece em log,
  em resposta de erro ou no disco da API (ADR-021);
- um áudio real sai do aparelho antes do OK do Mauro na política e no dossiê reescritos, ou um provedor
  é chamado sem aceite vigente que o inclua (ADR-021);
- o `memory.swap.max` do contêiner da API não dá `0`, ou o `docker info` traz "No swap limit support",
  antes do primeiro áudio (ADR-021: para e pergunta);
- a API perde memória ou CPU de forma material com o ditado (o `docker stats` com salto), ou algum
  serviço vizinho piora (ADR-021).

## Android futuro
Não implementar agora. Apenas garantir API/device auth/sync/ASR platform-agnostic.

**Emenda (ADR-019):** o celular Android entra na etapa 5 como PWA (o painel em modo nuvem). App
nativo, teclado (IME) e ditar no celular continuam futuros; o iPhone vem depois do MVP.

**Emenda (ADR-021, 10-out-2026):** o **ditado pelo PWA** (passo 1) deixa de ser futuro e segue o plano
acima. Continuam futuros, para o passo 2 (outra ADR): o app nativo do Android com a bolha flutuante ou o
teclado, o ditado em segundo plano e com a tela apagada, e a credencial de um aparelho Android com o
escopo de ditar. A rota, o contrato e os códigos de recusa do passo 1 já ficam prontos para ele.
