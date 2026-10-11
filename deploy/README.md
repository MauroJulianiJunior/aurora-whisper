# Deploy da nuvem

Os arquivos que levam o servidor da nuvem do Aurora Whisper para a VPS (ADR-019, "Decisões da F11").
**Nada daqui foi rodado de verdade pelo agente:** o build das imagens, os scripts da VPS e os do PC são
do Mauro, pelo roteiro. O que prova cada arquivo são os testes deste pacote (`go test ./deploy/`), que
leem o texto e rodam o bash com programas de mentira.

A F11 anda em duas partes. A **Parte 1** não depende de onde a nuvem mora. A **Parte 2** (o compose, a
borda que atende o https, o roteiro do deploy) segue a D1, confirmada pelo Mauro em 09-out-2026: a nuvem
mora numa **VPS Linux com datacenter no Brasil**, em `https://aurora-whisper.mjj.net.br`, atrás de um
**Traefik v3 que já roda lá** e atende os outros serviços do servidor. Nada do Traefik nem dos outros
serviços é editado ou reiniciado: a nuvem entra só com os rótulos do próprio contêiner.

Esse endereço é o da **instância do mantenedor**, fechada às contas de `CONTAS_PERMITIDAS`: não é um
serviço aberto. Quem sobe a própria nuvem usa o seu domínio, que o `gerar-segredos.sh` pergunta e grava
no `config.env`, e o troca nos comandos dos roteiros.

**O roteiro do deploy é o `docs/deploy-f11.md`** (um passo por vez, com o porquê e o comando); o do
B2, o `docs/backup-b2-f11.md`; o teste final, o `docs/teste-final-f11.md`; o da restauração, num desastre,
o `docs/restauracao-f11.md`.

## O que está aqui

| Arquivo | O que é |
|---|---|
| `compose.yml` | A nuvem na VPS: o `aurora-whisper-postgres` (só na rede `aurora-whisper-interna`, `internal: true`), a `aurora-whisper-api` (na interna e na `web` do Traefik, com os rótulos dele) e o `aurora-whisper-backup` (o agendador, na interna e numa rede de saída só dele). Segredos por arquivo (dez, com a chave de cifra das chaves da IA), nenhuma porta publicada, nenhum socket do Docker, limites de memória e CPU, e a API sem swap e sem core dump. Detalhes abaixo, em "O compose". |
| `compose.teste.yml` e `teste/traefik-dinamico.yml` | Só do teste do PC (`scripts\deploy-teste.ps1`): por cima do `compose.yml`, um Traefik de teste da mesma versão, só em `127.0.0.1`, com os roteadores por arquivo (sem o socket do Docker), iguais aos rótulos. Nunca no servidor. |
| `scripts/subir.sh` | Na VPS, para subir (e para cada imagem nova, e para trocar o client do Google): os dez segredos (sem o `ai-keys-secret` ele para e aponta o `criar-chave-de-cifra.sh`), o NTP, a sub-rede da rede `web` gravada como `PROXY_CONFIAVEL`, o Postgres conferido, o **`migrate` depois de "digite SIM"** (ele já roda as migrations pendentes, as 0017 a 0019 inclusive; não há comando à parte), "digite SIM" de novo antes da API, a ordem arrumação, retenção, servindo no log da API, o backup e o https pelo Traefik. |
| `scripts/agendar-backup.sh` | Dentro do contêiner do backup: o `backup.sh` todo dia às 06 UTC (03:00 em São Paulo), e o `saude` do healthcheck (o último backup que subiu tem no máximo 26 h). Sem cron no host. |
| `Dockerfile` | A imagem do servidor: o painel da nuvem (`pnpm build:nuvem`), o binário Go estático sem build tags, e a final distroless `nonroot` com o `HEALTHCHECK` pelo subcomando `healthcheck`. |
| `../.dockerignore` | O contexto do build (a raiz) como **lista de permissão**: só `cmd/`, `internal/`, `migrations/`, `web/` (sem `node_modules` e `dist`), `go.mod` e `go.sum`. O `bin\`, com o `historico.db` de verdade, nunca entra. |
| `backup/Dockerfile` | A imagem do backup: o cliente do Postgres 18, o `age` e o `rclone`. É a do contêiner do backup na VPS e a dos scripts do PC (a chave e o teste do mês). |
| `postgres/init/10-papel-da-aplicacao.sh` | Roda uma vez, na criação do volume do Postgres: cria o papel `aurora` sem superusuário, dono do banco `aurora`, e o papel `aurora_backup`, só de leitura (`pg_read_all_data`, com a entrada no banco). A API e o `migrate` usam o `aurora`; o backup, o `aurora_backup`. A sessão do init não registra comando no log (a senha nunca vai para o log, nem num erro). |
| `postgres/conferir-postgres.sh` | Na VPS, antes do primeiro dado real: confere os cinco ajustes de log (o log do Postgres sem texto ditado) e os dois papéis. Só lê. |
| `scripts/gerar-segredos.sh` | Na VPS, uma vez (numa VPS **nova**): gera as senhas do Postgres, o `SESSION_SECRET`, o `DEVICE_TOKEN_PEPPER` e a chave de cifra das chaves da IA (`ai-keys-secret`) de `/dev/urandom`, pergunta o domínio, o e-mail, o bucket e a chave do B2 e a chave pública do backup, confere o JSON do Google que o `enviar-para-servidor.ps1` mandou (o redirect de produção) e o move para `segredos/`, e grava tudo fora do repositório, sem mostrar nada. Recusa sobrescrever. |
| `scripts/criar-chave-de-cifra.sh` | Na VPS que **já está no ar**, uma vez, **antes do `subir.sh`** (ADR-021, C3a e C5c; numa VPS nova o `gerar-segredos.sh` já cria o arquivo, e este recusa): cria **só** `segredos/ai-keys-secret`, a chave de cifra das chaves da IA que as pessoas colam no app do celular (32 bytes de `/dev/urandom` em hexadecimal, o `AI_KEYS_SECRET_FILE` da API), `0444` na pasta `0700`, sem mostrar o valor e sem tocar nos outros arquivos. Recusa sobrescrever, e recusa sem a pasta `segredos/` (numa VPS nova, o `gerar-segredos.sh` vem antes). Detalhes abaixo, em "A chave de cifra das chaves da IA". |
| `scripts/backup.sh` | Dentro do contêiner do backup: `pg_dump` conferido, cifrado com o `age` para a chave pública e enviado ao Backblaze B2 por uma chave sem `deleteFiles`, num bucket com Object Lock (é ele que segura os backups: abaixo). |
| `scripts/conferir-b2.sh` | Na VPS, antes do primeiro backup e a cada chave nova: num contêiner da imagem do backup, sobe um arquivo de conferência, confere que a chave lista e que **não** consegue apagá-lo de vez. Lembra o que só o painel do B2 mostra (o Object Lock, a regra, as capacidades). |
| `scripts/restaurar.sh` | Na VPS, num desastre: `receber` (o dump decifrado no PC chega à memória só do usuário e é lido inteiro), `aplicar` (restaura num banco novo, roda nele o `migrate` e o `pos-restauracao`, e só por último troca os nomes) e `descartar`. Nunca apaga banco. |

Os nomes que os scripts usam (os contêineres `aurora-whisper-postgres`, `aurora-whisper-api` e
`aurora-whisper-backup`, a rede `aurora-whisper-interna`, as imagens `aurora-whisper-server:producao`
e `aurora-whisper-backup:producao`) são os do compose, e o `deploy/compose_test.go` confere que batem.

Os scripts do PC da Parte 2 (em `scripts\`, PowerShell 5.1): `deploy-teste.ps1` (o deploy inteiro no
Docker do PC, em `aurora-whisper.exemplo.test`, só em `127.0.0.1`, derrubado no fim; anota as imagens
aprovadas), `enviar-para-servidor.ps1` (só as imagens aprovadas, por `docker save | ssh docker load`,
mais a pasta `deploy\` e o JSON do Google) e `conferir-nuvem.ps1` (de fora, só leitura).

## Onde mora cada coisa na VPS

```text
~/aurora-whisper/              0700
  deploy/                      esta pasta, copiada do PC (o enviar-para-servidor.ps1)
  recebido/                    o JSON do Google, até o gerar-segredos.sh (a primeira vez) ou o
                               subir.sh (uma troca de client) o tirar de lá
  segredos/                    0700; os arquivos 0444 (o gerar-segredos.sh)
    postgres-superusuario-senha  POSTGRES_PASSWORD_FILE (só para administrar)
    postgres-aurora-senha        o papel da aplicação (o init)
    postgres-backup-senha        o papel só de leitura do backup (o init e o backup)
    database-url                 DATABASE_URL_FILE da API e do migrate
    session-secret               SESSION_SECRET_FILE
    device-token-pepper          DEVICE_TOKEN_PEPPER_FILE
    google-client.json           GOOGLE_CLIENT_SECRET_FILE (o client Web do Google)
    backup-chave-publica         a chave PÚBLICA do age
    rclone.conf                  a application key do B2 (só listBuckets, listFiles e writeFiles)
    ai-keys-secret               AI_KEYS_SECRET_FILE: a chave de cifra das chaves da IA do celular
                                 (o gerar-segredos.sh numa VPS nova; o criar-chave-de-cifra.sh numa
                                 VPS que já está no ar; ADR-021, C3a e C5c)
  config.env                   0600: PUBLIC_BASE_URL, DOMINIO, CONTAS_PERMITIDAS, B2_BUCKET e o
                               PROXY_CONFIAVEL (este, do subir.sh)
  antes-de-restaurar/          a cópia cifrada do banco antes de uma restauração
```

## O compose

- **As redes.** O Postgres só na `aurora-whisper-interna` (`internal: true`: sem saída, e nenhum vizinho
  da VPS o alcança). A API na interna e na `web` (a do Traefik, externa, criada pelo compose dele). O
  backup na interna e na `aurora-whisper-saida` (o `rclone` fala com o B2). Os apelidos de DNS levam o
  prefixo `aurora-whisper-*`, para não entrar no rodízio de um nome que os outros serviços da `web` já
  usam.
- **A borda.** Os rótulos do Traefik na API, no molde comum dos outros serviços do servidor
  (`traefik.enable`, a rede `web`, o entrypoint `websecure`, o resolvedor `letsencrypt`; o Traefik roda
  com `exposedbydefault=false`), mais
  `traefik.http.routers.aurora-whisper-api.observability.accesslogs=false`: o log de acesso do
  Traefik liga para todos os sites, e gravaria o `code` e o `state` do callback do Google e o IP de cada
  pedido; a observabilidade por router do Traefik v3 o desliga só no nosso (a sintaxe conferida na
  documentação do Traefik v3; o `deploy-teste.ps1` prova com um Traefik de verdade, na versão fixada no
  `compose.teste.yml`, por arquivo). **Sem um middleware de cabeçalhos** dos outros sites: a CSP e o
  HSTS são os do próprio servidor, e uma segunda CSP se somaria à dele.
- **Os segredos** são arquivos (`secrets:` do compose, montados em `/run/secrets/<nome>`), lidos pelas
  variáveis `*_FILE`; **nenhum segredo vai por variável de ambiente**: o provider docker do Traefik lê
  o `Config.Env` de todo contêiner, e o `docker inspect` também o mostra. São dez: a API monta o
  `ai-keys-secret` (`AI_KEYS_SECRET_FILE=/run/secrets/ai-keys-secret`) além dos quatro de antes, e é
  **obrigatório** (ADR-021, C5c): sem o arquivo, o `docker compose up` falha, e o `subir.sh` para antes,
  apontando o `criar-chave-de-cifra.sh`. É o arquivo que liga as rotas do ditado no celular (sem ele, no
  binário, elas cairiam no `404`).
- **Os limites.** A API com 256 MB (e o `GOMEMLIMIT` de 200 MiB) e 1 CPU; o Postgres com 512 MB e 1 CPU;
  o backup com 128 MB e meia CPU. O teto de CPU sozinho não dá a vez a ninguém (os três somam 2,5 CPUs,
  mais do que uma VPS pequena tem): o que dá a vez aos outros serviços do servidor na disputa é o
  **peso** (`cpu_shares: 256` nos três, um quarto do padrão que os outros contêineres têm). A API `read_only`, sem capacidades; todos com `no-new-privileges`, o
  log do Docker com rotação (10 MB, 3 arquivos), `restart: unless-stopped` e um healthcheck (a API pelo
  `healthcheck` de vida; o backup pelo `agendar-backup.sh saude`). O nome fixo de cada contêiner faz
  o compose parar o velho antes de criar o novo (nunca dois `serve` de pé).
- **A API não manda o áudio do celular para o disco** (ADR-021, decisão L; só no contêiner da API):
  `memswap_limit: 256m`, igual ao `mem_limit`, tira a swap dele (nem o heap nem o `/tmp` vão para ela, se
  a VPS tiver uma); `ulimits: core: 0`, a segunda trava contra um core dump que levaria o áudio e a chave
  decifrada ao disco (o Go não grava core no padrão); e `stop_grace_period: 90s`, que cobre o `Shutdown`
  de 15 s mais a espera de até 70 s pelos ditados em andamento (o `cmd/server/serve.go` espera os dois; o
  `compose_test` amarra os números). **O `memswap_limit` só vale se o kernel da VPS contar a swap no
  cgroup** (num cgroup v1 sem `swapaccount=1` o Docker o descarta e só escreve "No swap limit support"),
  e o Traefik pode ter um prazo curto ou um middleware de buffering: nenhum dos dois se prova pelo
  texto do compose. O roteiro (`docs/deploy-f11.md`, passos 10.3 e 10.4) manda ler, **só lendo**, antes do
  primeiro áudio: o `readTimeout`, o `writeTimeout` e os middlewares do entrypoint do Traefik, o
  `docker info` sem o aviso de swap, o `docker inspect` com a memória e a swap iguais e o `core` em 0, e
  o `memory.swap.max` do contêiner da API em `0`. **Se não der `0`, para** (a alternativa é uma VPS sem
  swap, vista no `swapon --show`); o Traefik nunca é editado.
- **A saúde do job de retenção não é o healthcheck da API.** O provider docker do Traefik tira do
  roteamento todo contêiner que não está `healthy`: com o `healthcheck -retencao` no compose, uma
  rodada da retenção com falha de banco tiraria o endereço do ar por até 1 h (e um pedido sem router
  entra no log de acesso do Traefik). O job se confere por `docker exec`, por fora do roteamento: o
  `subir.sh` o faz a cada subida, e a conferência do mês (`docs/deploy-f11.md`, "Uma vez por mês") o
  repete.
- **O `PROXY_CONFIAVEL`** é a sub-rede da rede `web`, que o `subir.sh` lê na hora do deploy e grava no
  `config.env`. O IP do Traefik nela muda quando ele é recriado; a sub-rede não.
- **As variáveis `AW_*`** (o prefixo, a pasta dos segredos, o nome da rede do proxy) só existem no
  teste do PC; no servidor ficam nos padrões, e o `subir.sh` as limpa do ambiente.
- **O ambiente da sessão não vale.** Na interpolação, o compose lê o ambiente **antes** do
  `--env-file`: um `DOMINIO` ou um `CONTAS_PERMITIDAS` exportado na sessão `ssh` valeria mais que o
  `config.env` conferido. O `subir.sh` tira do ambiente todas as variáveis que o `compose.yml` lê, e as
  `COMPOSE_*` (o `COMPOSE_PROJECT_NAME` vale mais que o `name:`), e confere na saída do `docker compose
  config` que a regra do Traefik é a do `DOMINIO` do `config.env`.

**O que fica de risco, e está aceito ou registrado na ADR:** quem divide a rede do proxy (os outros
contêineres na `web`) alcança a porta 8080 da API direto, sem passar pelo Traefik, e o
`X-Forwarded-For` que vier dessa rede é aceito (o `PROXY_CONFIAVEL` é a sub-rede inteira): o login, as
sessões e os tokens valem igual, mas um contêiner comprometido nessa rede pode contornar o limite de
tentativas por IP. Onde der, prefira uma rede só da API com o Traefik e o `PROXY_CONFIAVEL` só com o
IP dele. Numa VPS que divide a CPU com outros serviços, a nuvem tem limite, mas pode responder mais
devagar nos picos deles.

## Na VPS, depois do deploy

- **Parar** (o Traefik deixa de atender o endereço; nada é apagado):
  `docker compose --project-name aurora-whisper --env-file ~/aurora-whisper/config.env -f ~/aurora-whisper/deploy/compose.yml stop`
  (sempre com o `--project-name`: um `COMPOSE_PROJECT_NAME` exportado vale mais que o `name:` do arquivo).
  Voltar: o `subir.sh`. **Nunca `down -v`**: o `-v` apaga o volume do Postgres.
- **Uma imagem nova:** `deploy-teste.ps1` e `enviar-para-servidor.ps1` (sem o `-ArquivoGoogle`) no PC,
  e o `subir.sh` na VPS. **Numa VPS que já estava no ar antes do ditado no celular**, o
  `criar-chave-de-cifra.sh` vem antes do `subir.sh` (ele recusa sem o arquivo), e o `subir.sh` aplica as
  migrations 0017 a 0019 depois do "digite SIM" do `migrate` (`docs/deploy-f11.md`, passos 10.1 e 10.2).
- **Trocar o client do Google:** o JSON novo, sozinho, em `~/aurora-whisper/recebido/google-client.json`
  (o `scp` do roteiro, "Se algo der errado"), e o `subir.sh`: ele o confere como o
  `gerar-segredos.sh`, pede o SIM, troca o de `segredos/` (0444) e recria a API com `--force-recreate`
  (o compose não recria um serviço quando só o conteúdo de um segredo muda, e a API lê o JSON só na
  subida). Um JSON igual ao de `segredos/` sai de `recebido/` sem troca; um recusado para o `subir.sh`
  até alguém tirá-lo. O `gerar-segredos.sh` não serve para isso: rodado de novo, ele recusa.

Os arquivos de segredo são `0444` porque a imagem da API roda como `nonroot` (uid 65532) e a do backup
como `postgres` (uid 70), e o compose monta cada arquivo com o dono e o modo do host; quem fecha o
acesso dos outros usuários é a pasta `0700`.

### A chave de cifra das chaves da IA (ADR-021, C3a)

Para ditar pelo celular, a pessoa cola no app a chave de API do provedor de IA dela, e o servidor a
guarda **cifrada** no banco (a `ai_provider_keys`). A chave de cifra é um arquivo da VPS,
`segredos/ai-keys-secret`, que nunca entra no banco nem no backup. Numa VPS que já está no ar, o
`gerar-segredos.sh` não serve (ele recusa quando `segredos/` existe), e o passo é um script próprio:

```bash
bash ~/aurora-whisper/deploy/scripts/criar-chave-de-cifra.sh
```

- **O que ele faz:** confere a pasta base (fora do `deploy/` e de um repositório git) e que
  `segredos/` existe; gera 32 bytes de `/dev/urandom` em hexadecimal (64 caracteres, como os outros
  segredos); cria o arquivo sem nunca ir por cima (o `noclobber` do bash o cria com `O_EXCL`), escreve
  o valor, e o deixa `0444` na pasta `0700`. Nenhum valor aparece na tela; uma falha no meio apaga o
  arquivo que ele criou, e só ele.
- **O que ele recusa:** o arquivo que já existe (trocar a chave de cifra leva toda chave guardada a
  "cole de novo"; nada é mudado) e a VPS sem `segredos/` (numa VPS nova, o `gerar-segredos.sh` vem
  antes).
- **Sem cópia fora da VPS** (ADR-021, escolha menor 11): perder o arquivo, ou a VPS, faz cada pessoa
  colar a chave da IA de novo no app; nenhum ditado se perde.
- **Numa VPS nova:** o `gerar-segredos.sh` já cria o `ai-keys-secret` junto dos outros, e este script
  recusa ("ja existe"), sem mudar nada.
- **Quando a API passa a usá-lo:** desde a C5c o compose declara o segredo e o `AI_KEYS_SECRET_FILE`, e o
  `subir.sh` o exige (está no `SEGREDOS_ESPERADOS`): **o script vem antes do `subir.sh`** numa VPS que já
  estava no ar, senão o `subir.sh` para no começo e aponta este passo. Com o arquivo montado, as rotas
  do ditado no celular existem; com ele malformado, a API não sobe, e a mensagem diz o nome da
  variável, nunca o conteúdo. A imagem sem o arquivo (um compose antigo com a imagem nova) continua
  subindo, sem as rotas (`404`).

## O backup (D2)

`pg_dump -Fc` pelo papel só de leitura `aurora_backup` (o contêiner do backup é o único que fala com
a internet; quem o tomasse leria o banco, mas não o apagaria; a senha vai só para o ambiente do
`pg_dump`), conferido por um `pg_restore --list` sobre o fluxo antes da
cifra (com o `PIPESTATUS` de cada etapa), cifrado com o `age` para a chave **pública** (a privada só
existe no PC do Mauro e no gerenciador de senhas dele), num `.partial` renomeado só no fim, e enviado
ao **Backblaze B2** com o `rclone copyto --immutable`, por uma application key restrita ao bucket e
**sem `deleteFiles`**. O giro de **30 dias** é a regra de ciclo de vida do próprio bucket. O cifrado
fica **3 dias** no volume para reenviar se o B2 falhar. Nenhum dump em claro vai para o disco.

**A chave sem `deleteFiles` sozinha não segura os backups.** Para subir arquivos, ela precisa da
`writeFiles`, e a `writeFiles` também deixa **esconder** um arquivo (`b2_hide_file`, que é o que o
`rclone delete` faz no B2) e subir outra versão com o mesmo nome; a regra de ciclo de vida apaga de
vez o que foi escondido. Sem mais nada, quem invadir a VPS lê o `rclone.conf` e apaga os 30 backups,
só que com um dia de atraso. O que segura é o **Object Lock** do bucket:

### O bucket e a chave do B2 (o Mauro, no painel e na CLI do B2)

1. **O bucket:** privado, com **Object Lock ligado** (na criação do bucket; ligá-lo num bucket que já
   existe, se o painel deixar, a conferir) e **retenção padrão de 30 dias**, no modo **governance**
   (proposta, a confirmar: no governance, a chave mestra dele ainda consegue apagar antes do prazo,
   para um pedido de apagar da LGPD ou um engano; no compliance, ninguém consegue). Com ele, nenhuma
   versão sai antes de 30 dias da subida, nem pela regra de ciclo de vida: o que for escondido ou
   sobrescrito continua lá, e o Mauro o recupera apagando o marcador de esconder (no painel, "mostrar
   versões") com a chave mestra.
2. **A regra de ciclo de vida:** esconde aos 30 dias e apaga o escondido 1 dia depois
   (`daysFromUploadingToHiding: 30`, `daysFromHidingToDeleting: 1`). Com o Object Lock, o apagar só
   acontece depois do fim da retenção. **Sem o Object Lock** (se o B2 não deixar ligá-lo), o mínimo é
   um `daysFromHidingToDeleting` de 30 dias ou mais, e um alarme: aí a chave apaga, com atraso.
3. **A application key:** só deste bucket, e **só com `listBuckets`, `listFiles` e `writeFiles`** (o
   `copyto --immutable` lista o destino antes de subir, e o `backup.sh` confere o tamanho do que subiu
   listando). **Sem** `deleteFiles`, `readFiles`, `bypassGovernance`, `writeFileRetentions` nem
   `writeBucketRetentions`. Pelo que se sabe, o painel só oferece conjuntos prontos ("Read and Write",
   que traz o `deleteFiles`; "Write Only", que não lista), então ela nasce pela CLI do B2
   (`b2 key create --bucket <bucket> aurora-whisper-backup listBuckets,listFiles,writeFiles`, na
   sintaxe da versão 4; a conferir na versão em uso) ou pela API (`b2_create_key`), com a chave
   mestra. A chave vai direto ao servidor (o `gerar-segredos.sh` a pergunta, sem eco), nunca ao chat.
4. **A conferência, na VPS, antes do primeiro backup e a cada chave nova:**
   `bash ~/aurora-whisper/deploy/scripts/conferir-b2.sh`. Ele sobe um arquivo de conferência, confere
   que a chave lista e que **não** consegue apagá-lo de vez (sem `deleteFiles`, ou com o Object Lock
   segurando). O Object Lock, a regra e as capacidades exatas ele não vê com esta chave: confira no
   painel (o script lembra).

O que ainda fica: quem invadir a VPS pode **esconder** os backups e parar o backup. Cada cópia fica
guardada até o fim da retenção dela; o escondido não aparece no painel sem "mostrar versões", e o
teste do mês, que baixa o backup mais recente, é o que percebe.

## A restauração

**O roteiro é o `docs/restauracao-f11.md`** (um passo por vez, com o comando: a decifra no PC pelo
`cmd`, ligada ao `restaurar.sh receber` pelo `ssh`; o `aplicar`; o `subir.sh`; o `DROP` dos 7 dias).

O que foi apagado depois do backup **pode voltar** ao restaurar (decisão do Mauro, 09-out-2026:
"aceitar e deixar escrito"), e todas as sessões e todos os aparelhos são revogados (todo PC conecta
de novo). O banco anterior fica **7 dias** com outro nome (`aurora_antes_AAAAMMDD`); o `DROP` sai
impresso, pronto, e é do Mauro.

Antes de começar, uma sessão `ssh` interativa na VPS, aberta até o fim: o dump decifrado mora na pasta
em memória **só do usuário** (`/run/user/<uid>`, o `XDG_RUNTIME_DIR`), e não no `/dev/shm`, que é de
todos e que um contêiner com `--ipc=host` enxerga. Essa pasta some quando a última sessão fecha, e o
`aplicar` recusa (e apaga) um dump com mais de **2 h**. Se a VPS tem swap, o `receber` avisa: a
memória pode ir para o disco do swap. O `receber` lê o dump inteiro (`pg_restore -f /dev/null`), e
não só o índice: um dump cortado na transferência é recusado ali.

O `aplicar` recusa a API e o backup em qualquer estado vivo (de pé, reiniciando ou pausados) e
qualquer conexão ao banco `aurora` (`pg_stat_activity`). O `migrate` e o `pos-restauracao` rodam no
banco **novo**, por uma cópia da `database-url` com o nome dele (na mesma pasta em memória); a troca
de nomes é o **último** passo. Qualquer parada antes dela deixa o `aurora` como estava, e o banco
novo fica com o `DROP` impresso, para recomeçar.

## O que substitui o CI enquanto não há push

O CI do GitHub (e o gitleaks dele) só roda com `git push`, que espera uma decisão à parte (D4). Até
lá, o que faz as vezes dele é, no PC: `go test ./...` (que inclui estes testes e a varredura de
segredos de `deploy/varredura_test.go` sobre `deploy/`, `docs/`, `scripts/` e `migrations/`),
`go vet ./...`, e no `web/` o `pnpm lint`, o `pnpm test` e o `pnpm build`; e o
`scripts\deploy-teste.ps1` (o `docker build` das duas imagens e o deploy inteiro no Docker do PC), que é
do Mauro.
