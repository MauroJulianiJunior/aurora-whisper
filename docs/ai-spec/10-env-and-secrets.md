# Environment and Secrets

## Ambientes
| Ambiente | URL |
|---|---|
| local-windows | `http://127.0.0.1:<port>` |
| local-cloud | `http://localhost:<port>` |
| production | `https://exemplo.com` (o domínio real vem de `PUBLIC_BASE_URL`) |

## Variáveis
| Nome | Obrigatória | Descrição |
|---|---:|---|
| APP_ENV | Não | ambiente. **F3:** o servidor não a lê |
| PUBLIC_BASE_URL | Sim | a origem da nuvem: `https://servidor`, ou `http://localhost:<porta>` (e `127.0.0.1`) só no PC; sem caminho. O `Host` dos pedidos tem de ser o dela, o HSTS só sai com `https`, e o redirect do Google é ela + `/auth/google/callback` (F3) |
| HTTP_ADDR | Não | bind. **F3:** padrão `127.0.0.1:8080`; no binário com a build tag `logindeteste`, só loopback |
| DATABASE_URL | Sim (ou o `_FILE`) | Postgres. **ADR-019, F1:** também é a única fonte da conexão do comando `migrate` (abaixo) |
| DATABASE_URL_FILE | Não | **F11c:** o caminho de um arquivo com a `DATABASE_URL` (o segredo montado no contêiner). Ver "Segredos por arquivo", abaixo |
| SESSION_SECRET | Sim (ou o `_FILE`) | a chave do HMAC do id da sessão. **F3:** pelo menos 32 bytes, diferente de `DEVICE_TOKEN_PEPPER` |
| SESSION_SECRET_FILE | Não | **F11c:** o caminho de um arquivo com o `SESSION_SECRET` |
| DEVICE_TOKEN_PEPPER | Sim (ou o `_FILE`) | a chave do HMAC do token do PC. **F3:** pelo menos 32 bytes |
| DEVICE_TOKEN_PEPPER_FILE | Não | **F11c:** o caminho de um arquivo com o `DEVICE_TOKEN_PEPPER` |
| GOOGLE_CLIENT_SECRET_FILE | Sim | **F3:** o caminho do JSON do client Web baixado do Google Cloud (`{"web": {"client_id", "client_secret", "redirect_uris"}}`), guardado fora do repositório. Substitui `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` e `GOOGLE_REDIRECT_URL` |
| CONTAS_PERMITIDAS | Sim | **ADR-019:** e-mails que podem entrar na nuvem, separados por vírgula. **F3:** vazia, o servidor não sobe; uma entrada que não é e-mail também não. O `sub` de cada um fica fixado no primeiro login |
| PROXY_CONFIAVEL | Não | **F3:** o IP (ou a rede CIDR) do proxy que termina o HTTPS na frente do servidor; só dele o `X-Forwarded-For` (o último salto) vale. Vazia, nenhum |
| PAINEL_DIR | Sim | **F4 (ADR-019):** a pasta do build da nuvem do painel (o app), com o `index.html` (`pnpm build:nuvem`, que sai em `web/dist-nuvem`). O servidor a lê para a memória na subida. Vazia, não é pasta, ou sem o `index.html`: saída 2, sem o caminho na mensagem. No servidor de teste do PC (`teste`, build tag `logindeteste`) também é obrigatória |
| ASR_CLOUD_PROVIDER | Não | provider. **Não usada na etapa 5** (ADR-019: ASR na nuvem é "depois") |
| ASR_CLOUD_MODEL | Não | model. **Não usada na etapa 5** |
| ASR_CLOUD_API_KEY | Condicional | secret. **Não usada na etapa 5** |
| CONTEXT_PROVIDER | Não | resolver. **Não usada na etapa 5** (contexto não vai para a nuvem) |
| CONTEXT_MODEL | Não | model. **Não usada na etapa 5** |
| CONTEXT_API_KEY | Condicional | secret. **Não usada na etapa 5** |
| UPDATE_FEED_SOURCE | Não | update. **Não usada na etapa 5** (a rota `/updates` não é montada) |
| GITHUB_TOKEN | Condicional | server-side se privado. **Não usada na etapa 5** |
| LOG_LEVEL | Não | logs: `debug`, `info` (padrão), `warn` ou `error`. **F9:** a linha `retenção` do job (o batimento do alerta da F11) é `INFO`; com `warn` ou `error` ela some, mesmo com o job saudável (`11-observability.md`) |

**Etapa 5 (ADR-019, 01-out-2026):**
- `SESSION_SECRET` é a chave do HMAC do id da sessão web (o banco guarda só o HMAC);
  `DEVICE_TOKEN_PEPPER`, a do HMAC do token do PC. Os dois, e a senha do Postgres, são gerados pelo
  dono **no servidor**; nunca no chat, no repositório ou no `.env.example`.
- O client Web do Google vem do JSON baixado do projeto do dono no Google Cloud, apontado por
  `GOOGLE_CLIENT_SECRET_FILE`, sempre fora do repositório: o de desenvolvimento, numa pasta local do
  PC; o de produção, num arquivo no servidor.
- O endereço do proxy confiável (de quem o `X-Forwarded-For` vale): `PROXY_CONFIAVEL`, um IP ou uma
  rede CIDR (F3).
- Não existe variável que ligue um login de teste: ele só existe sob a build tag `logindeteste` (F3),
  e o binário com ela só sobe com `PUBLIC_BASE_URL` em `localhost` e `HTTP_ADDR` no loopback
  (`127.0.0.0/8`, `::1` ou `localhost`; `0.0.0.0` e `:8080` encerram com saída 2).
- `CONTAS_PERMITIDAS` entrou no `.env.example` da raiz na F3, vazia; `PAINEL_DIR`, na F4, vazia.
- **`RETENTION_MONTHS` e `TRASH_DAYS` saíram (F9, 08-out-2026; ADR-019, "Decisões da F9", escolha
  menor 3).** O código nunca as leu. Os prazos da retenção (os 6 meses do ditado e os 30 dias da
  lixeira) são constantes da política, `internal/retention`, iguais na recusa por janela do push, no
  job de retenção da nuvem e no PC: configurá-las faria a nuvem e os PCs divergirem na janela. O job
  roda no `serve`, na subida e de hora em hora, sem variável nenhuma.
- **O servidor de teste (F4, 05-out-2026).** `aurora-whisper-server teste`, que só existe no binário com
  a build tag `logindeteste`, lê só `PAINEL_DIR` e, opcional, `HTTP_ADDR` (padrão `127.0.0.1:8080`,
  só loopback): guarda tudo na memória, não tem o Google nem o Postgres, e gera as duas chaves dentro
  do processo, a cada subida, sem nunca imprimi-las. O atalho é `scripts/nuvem-local-teste.ps1`; o
  roteiro, `docs/teste-manual-nuvem-f4.md`.
- **O `serve` (F3, 05-out-2026).** `aurora-whisper-server serve` lê as variáveis acima. Uma que falta
  ou está errada encerra com saída 2 e uma linha `serve: <VARIÁVEL>: ...` que diz o que está errado,
  **nunca o valor** (nem o caminho do arquivo do Google, nem o conteúdo dele). O client do Google sai
  de `GOOGLE_CLIENT_SECRET_FILE`, e não de três variáveis: é o arquivo que o Google Cloud dá, o
  `client_id` e o `client_secret` não passam pelo histórico do shell, e o redirect é derivado de
  `PUBLIC_BASE_URL` e tem de estar nos `redirect_uris` dele (o servidor confere na subida). Os
  endereços do Google do arquivo (`auth_uri`, `token_uri`) são ignorados: os do código valem
  (ADR-019, "Decisões da F3"). `LOG_LEVEL` é `debug`, `info` (padrão), `warn` ou `error`. Na subida, o
  `serve` recusa um banco com migration pendente (manda rodar o `migrate`). O roteiro de
  desenvolvimento, com os comandos, está em `docs/teste-manual-nuvem-f3.md`.
- **Segredos por arquivo (F11c, 09-out-2026; ADR-019, "Decisões da F11").** `DATABASE_URL`,
  `SESSION_SECRET` e `DEVICE_TOKEN_PEPPER` também vêm de arquivo: `DATABASE_URL_FILE`,
  `SESSION_SECRET_FILE` e `DEVICE_TOKEN_PEPPER_FILE` dizem o caminho. No contêiner, o segredo é um
  arquivo montado só de leitura, e não uma variável de ambiente (que o `docker inspect` mostra e que
  vai para os processos filhos). A variável comum continua valendo; **as duas juntas** (`X` e
  `X_FILE`) são erro de configuração, saída 2. O arquivo tem de ser um arquivo comum de até 4 KiB com
  **uma linha só**: o servidor tira só o fim de linha do final (`\n` ou `\r\n`) e recusa o vazio e o
  de mais de uma linha. As regras de sempre (32 caracteres, os dois diferentes) valem depois da
  leitura. Nenhuma mensagem traz o valor nem o caminho; o erro diz o nome da variável (`X` ou
  `X_FILE`). O `migrate` e o `pos-restauracao` (F11d) leem a `DATABASE_URL` pela mesma regra. Os
  valores nascem em hex ou base64url, que não pedem escape dentro da URL do banco.
- **Na VPS (F11, Parte 2, 09-out-2026; ADR-019, "Decisões da F11").** Quem passa as variáveis ao
  `serve` é o `deploy/compose.yml`, e **nenhum segredo vai por variável de ambiente**: o Traefik da VPS
  lê o `Config.Env` de todo contêiner, e o `docker inspect` também o mostra. Os segredos são arquivos da
  pasta `~/aurora-whisper/segredos/` (0700, arquivos 0444, do `gerar-segredos.sh`), montados pelo compose
  em `/run/secrets/<nome>`: `DATABASE_URL_FILE=/run/secrets/database-url`,
  `SESSION_SECRET_FILE=/run/secrets/session-secret`,
  `DEVICE_TOKEN_PEPPER_FILE=/run/secrets/device-token-pepper` e
  `GOOGLE_CLIENT_SECRET_FILE=/run/secrets/google-client.json` (o JSON que o
  `enviar-para-servidor.ps1` manda e o `gerar-segredos.sh` confere e move). O Postgres lê
  `POSTGRES_PASSWORD_FILE` (o superusuário, só para administrar), `AURORA_SENHA_FILE` e
  `AURORA_BACKUP_SENHA_FILE` (os dois papéis, pelo init). O que não é segredo vem do
  `~/aurora-whisper/config.env` (0600, fora do repositório), que o compose lê pelo `--env-file`:
  `PUBLIC_BASE_URL`, `DOMINIO` (o `Host` da regra do Traefik), `CONTAS_PERMITIDAS` e `B2_BUCKET` (do
  `gerar-segredos.sh`) e `PROXY_CONFIAVEL` (a sub-rede da rede `web` do Traefik, que o `subir.sh` lê
  na hora do deploy e grava ali). No contêiner, `HTTP_ADDR=0.0.0.0:8080` (a rede é só a interna e a do
  Traefik; nenhuma porta é publicada), `PAINEL_DIR=/painel`, `LOG_LEVEL=info` e `GOMEMLIMIT=200MiB`
  (abaixo do limite de 256 MB do contêiner). O contêiner do backup recebe `B2_BUCKET`, a hora
  `AW_HORA_DO_BACKUP` (06, em UTC: 03:00 em São Paulo) e os arquivos `postgres-backup-senha`,
  `backup-chave-publica` e `rclone.conf`; os padrões do `backup.sh` (`PGHOST`, `PGUSER`,
  `AW_SENHA_FILE`, `AW_CHAVE_PUBLICA`, `RCLONE_CONFIG`) já são os caminhos do compose. As variáveis
  `AW_PREFIXO`, `AW_SEGREDOS` e `AW_REDE_DO_PROXY` do compose existem só para o teste do PC
  (`scripts\deploy-teste.ps1`); no servidor ficam nos padrões, e o `subir.sh` as tira do ambiente.
  Tira também as do `config.env` (`PUBLIC_BASE_URL`, `DOMINIO`, `CONTAS_PERMITIDAS`, `B2_BUCKET`,
  `PROXY_CONFIAVEL`) e as `COMPOSE_*`, porque na interpolação o compose lê o ambiente antes do
  `--env-file`, e o `COMPOSE_PROJECT_NAME` vale mais que o `name:` do arquivo (revisão da Parte 2).
- **O `healthcheck` (F11c).** `aurora-whisper-server healthcheck` faz `GET` no `/api/v1/health/live`
  em `127.0.0.1` (ou no loopback de `HTTP_ADDR`), na porta de `HTTP_ADDR`, com prazo de 3 s, sem proxy e
  sem seguir redirecionamento, e sai com 0 ou 1 (nunca 2, que o Docker reserva): a imagem não tem shell
  nem `curl`. Com `-retencao`, lê a rota `GET /api/v1/health/retencao` (que só responde a quem pede
  pelo loopback, sem cabeçalho de proxy) e sai com 1 se a última rodada da retenção terminou há mais de
  2 h, se ainda não houve rodada, ou se a última teve falha de categoria `banco` ou `outro`. Não lê
  variável nova.
- **`DATABASE_URL` e o comando `migrate` (F1, 01-out-2026).** `aurora-whisper-server migrate` lê a URL
  do Postgres **só** desta variável (`postgres://usuario:senha@servidor:5432/banco`), ou do arquivo de
  `DATABASE_URL_FILE` (F11c): nunca por opção
  de linha de comando, que levaria a senha para o histórico do shell e para a lista de processos. A
  saída do comando nunca a traz. Em produção, o `migrate` pode rodar com um usuário do banco que cria
  tabelas, e o servidor com outro, que só lê e escreve: o nome da variável é o mesmo, definido por
  invocação. Rodar o `migrate` em produção é ponto de parada (quem roda é o dono, com `migrate -status`
  antes). Ver `migrations/README.md`.
- **`AURORA_TEST_POSTGRES_URL` (F1): só para testes, nunca em produção.** A URL de um Postgres de teste
  de um usuário que pode criar bancos, **na própria máquina** (`localhost`, `127.0.0.1`, `::1` ou
  socket Unix; os testes recusam qualquer outra). Sem ela, os testes que precisam de Postgres pulam
  com a instrução de como ligar, e o `go test ./...` normal nunca toca no Docker. Cada teste cria um
  banco `aw_test_<hex>` e o apaga no fim. Quem a define é o `scripts/postgres-de-teste.ps1`, que
  imprime a linha com uma senha aleatória gerada na hora (nunca fixa, nunca no repositório): ver
  `scripts/README.md`. Não entra no `.env.example`.

## Inference opcional
**Não usada na etapa 5** (ADR-019).
- INFERENCE_ENABLED
- INFERENCE_MODEL_ID
- INFERENCE_MAX_CONCURRENCY
- INFERENCE_MEMORY_LIMIT
- INFERENCE_TIMEOUT

## `.env.example`
```env
APP_ENV=development
PUBLIC_BASE_URL=http://localhost:8080
HTTP_ADDR=127.0.0.1:8080
PROXY_CONFIAVEL=
DATABASE_URL=
DATABASE_URL_FILE=
SESSION_SECRET=
SESSION_SECRET_FILE=
DEVICE_TOKEN_PEPPER=
DEVICE_TOKEN_PEPPER_FILE=
GOOGLE_CLIENT_SECRET_FILE=
CONTAS_PERMITIDAS=
PAINEL_DIR=
ASR_CLOUD_PROVIDER=
ASR_CLOUD_MODEL=
ASR_CLOUD_API_KEY=
CONTEXT_PROVIDER=
CONTEXT_MODEL=
CONTEXT_API_KEY=
UPDATE_FEED_SOURCE=
GITHUB_TOKEN=
LOG_LEVEL=info
```

## Regras
- nunca commitar `.env`;
- CI não imprime secrets;
- Agent guarda device token no Credential Manager (alvo `AuroraWhisper/nuvem`, pelo portão
  `sysinput`; ADR-019);
- o endereço da nuvem é configuração de quem usa (Configurações do painel), vazio por padrão; não é
  variável de ambiente do Agent, e o repositório só mostra `exemplo.com` (ADR-019);
- Agent guarda a chave de API do provedor de IA **de quem usa** (OpenAI, Anthropic) no Credential
  Manager, alvo `AuroraWhisper/<provedor>`, `CRED_PERSIST_LOCAL_MACHINE` — nunca em `.env`, variável
  de ambiente, arquivo, banco, log, resposta da API local ou mensagem de erro (ver
  `09-security-privacy.md`, "Chave de API do provedor de IA");
- bundle React nunca recebe secret server-side, nem a chave de quem usa: o painel só a envia
  (`PUT /api/ia/chave`) e nunca a recebe de volta.

A chave de quem usa não é variável deste arquivo: `CONTEXT_API_KEY` e `ASR_CLOUD_API_KEY` acima são
do operador da cloud, no servidor. A de quem usa entra pela tela de Configurações do painel.
