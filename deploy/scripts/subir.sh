#!/usr/bin/env bash
# Sobe a nuvem na VPS (ADR-019, "Decisoes da F11", Parte 2, F11j). Quem roda e
# o Mauro, na VPS, como o usuario dele (sem sudo), depois do
# gerar-segredos.sh e do conferir-b2.sh, pelo roteiro (docs/deploy-f11.md):
#
#   bash ~/aurora-whisper/deploy/scripts/subir.sh
#
# Serve tambem para subir uma imagem nova depois (o mesmo comando, depois do
# enviar-para-servidor.ps1) e para trocar o client do Google (o mesmo
# comando, depois do JSON novo em recebido/). Em ordem, cada passo confere o anterior e o
# script para no primeiro FALHOU:
#
#   1. o que tem de existir: o Docker e o compose, as duas imagens que vieram
#      do PC (aurora-whisper-server e aurora-whisper-backup, por docker load),
#      a pasta segredos/ (0700, com os nove arquivos) e o config.env. Um JSON
#      do Google em recebido/ (para trocar o client, pelo roteiro) e
#      conferido como no gerar-segredos.sh: se for o mesmo de segredos/, sai;
#      se for outro, troca o atual no passo 8, depois do SIM, com a API
#      recriada (--force-recreate);
#   2. o relogio: o NTP sincronizado (timedatectl). O login do Google confere
#      a hora do token, e as sessoes, os aparelhos e o backup dependem dela;
#   3. a sub-rede da rede "web" do Traefik (docker network inspect, so
#      leitura), gravada no config.env como PROXY_CONFIAVEL: so dela o
#      X-Forwarded-For vale. O servidor avisa (um WARN) se ele chegar de fora;
#   4. o compose conferido (docker compose config, so leitura), com a regra
#      do Traefik igual ao DOMINIO do config.env;
#   5. o Postgres de pe e saudavel, e conferido pelo
#      deploy/postgres/conferir-postgres.sh (os cinco ajustes de log e os dois
#      papeis), ANTES de qualquer dado;
#   6. as migrations: o migrate -status e, se houver pendente, o migrate
#      depois de "digite SIM" (migration em producao e ponto de parada). O
#      migrate roda num conteiner descartavel da imagem da API, o mesmo
#      binario, na rede interna e SEM os rotulos do Traefik (um "docker
#      compose run" os levaria, e o Traefik o poria no rodizio da API). Com a
#      API de pe (uma imagem nova), ela para antes do migrate;
#   7. "digite SIM" antes de subir a API, que abre a nuvem para a internet;
#   8. a API: o compose a sobe (ou a troca pela imagem nova: o nome fixo do
#      conteiner faz o compose parar a velha antes). O proprio servidor roda a
#      primeira arrumacao e a retencao ANTES de abrir a porta; aqui se espera
#      o healthcheck (o de vida), se confere no log DESTA subida (o docker
#      logs --since a hora dela) que a ordem foi arrumacao, retencao, servindo,
#      e que o proxy confiavel esta ligado (com a API nem recriada nem
#      reiniciada, nada mudou nela e a ordem nao se confere de novo), e se
#      confere a saude do job por docker exec (healthcheck -retencao). Ela nunca e o
#      healthcheck do compose: o Traefik nao roteia um conteiner unhealthy, e
#      uma rodada da retencao com falha tiraria o endereco do ar;
#   9. o backup (o agendador, todo dia as 03:00 de Sao Paulo);
#  10. a borda: o https pelo Traefik, de dentro da VPS, com o certificado
#      conferido pelo curl (o primeiro do Let's Encrypt leva alguns segundos).
#
# Nada do Traefik nem dos outros servicos do servidor e tocado: daqui, so a
# leitura da rede web. Nenhum segredo aparece na tela.
set -euo pipefail
umask 077

# Os padroes do compose sao os nomes do servidor; as variaveis AW_* sao so do
# teste do PC.
unset AW_PREFIXO AW_SEGREDOS AW_REDE_DO_PROXY
# Na interpolacao, o compose le o ambiente ANTES do --env-file: uma variavel
# exportada na sessao (um runbook de outro site, o .bashrc) valeria mais que o
# config.env, que e o que este script confere. E o COMPOSE_PROJECT_NAME vale
# mais que o name: do compose.yml. So o config.env e os nomes daqui valem.
unset PUBLIC_BASE_URL DOMINIO CONTAS_PERMITIDAS B2_BUCKET PROXY_CONFIAVEL
unset COMPOSE_PROJECT_NAME COMPOSE_FILE COMPOSE_PROFILES COMPOSE_ENV_FILES

BASE="${AW_BASE:-$HOME/aurora-whisper}"
AQUI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
DEPLOY="$(dirname "$AQUI")"
SEGREDOS="$BASE/segredos"
CONFIG="$BASE/config.env"
# O JSON do client do Google que o PC manda para ca (o enviar-para-servidor.ps1
# na primeira vez, ou o scp do roteiro para trocar o client depois).
RECEBIDO="$BASE/recebido/google-client.json"
# O JSON do Google tem menos de 1 KiB; o servidor recusa acima de 64 KiB.
MAX_JSON_DO_GOOGLE=65536
COMPOSE="$DEPLOY/compose.yml"
PROJETO='aurora-whisper'
REDE_DO_PROXY='web'
REDE_INTERNA='aurora-whisper-interna'
CONTEINER_POSTGRES='aurora-whisper-postgres'
CONTEINER_API='aurora-whisper-api'
CONTEINER_BACKUP='aurora-whisper-backup'
IMAGEM_API='aurora-whisper-server:producao'
IMAGEM_BACKUP='aurora-whisper-backup:producao'
# O binario da imagem da API (o ENTRYPOINT): o docker exec da saude do job.
BINARIO_DA_API='/usr/local/bin/aurora-whisper-server'
# As esperas, em segundos. As variaveis AW_ESPERA_* sao so para os testes (o
# docker de mentira nunca fica saudavel quando o teste pede isso).
ESPERA_POSTGRES="${AW_ESPERA_POSTGRES:-120}"
ESPERA_API="${AW_ESPERA_API:-300}"
ESPERA_HTTPS="${AW_ESPERA_HTTPS:-180}"
SEGREDOS_ESPERADOS='postgres-superusuario-senha postgres-aurora-senha postgres-backup-senha database-url session-secret device-token-pepper google-client.json backup-chave-publica rclone.conf'

falhou() {
  printf 'FALHOU: %s\n' "$*" >&2
  exit 1
}
parou() {
  printf '\nPAROU: %s\n' "$*" >&2
  exit 1
}
ok() {
  printf 'OK: %s\n' "$*"
}

# O compose do projeto, sempre com o config.env.
dc() {
  docker compose --project-name "$PROJETO" --env-file "$CONFIG" -f "$COMPOSE" "$@"
}

# Vivo em qualquer estado que volta sozinho (de pe, reiniciando, pausado).
vivo() { # conteiner
  [ -n "$(docker ps -q --filter "name=^/?$1\$")" ]
}

# Espera o healthcheck do conteiner dizer healthy.
esperar_saudavel() { # conteiner segundos
  local i=0 estado=''
  while [ "$i" -lt "$2" ]; do
    estado="$(docker inspect -f '{{.State.Health.Status}}' "$1" 2> /dev/null || true)"
    case "$estado" in
      healthy) return 0 ;;
      unhealthy) return 1 ;;
    esac
    sleep 2
    i=$((i + 2))
  done
  return 1
}

# O valor de NOME no config.env, sem executar o arquivo (nunca "source").
do_config() { # nome
  sed -n "s/^$1=//p" "$CONFIG" | tail -n 1
}

# Grava (ou troca) NOME=valor no config.env, de uma vez (arquivo novo e mv).
gravar_no_config() { # nome valor
  local novo="$CONFIG.novo.$$"
  { grep -v "^$1=" "$CONFIG" || true; printf '%s=%s\n' "$1" "$2"; } > "$novo"
  chmod 0600 "$novo"
  mv -f -- "$novo" "$CONFIG"
}

# A API num conteiner descartavel, na rede do Postgres, so com o arquivo da
# DATABASE_URL montado: o mesmo binario, o subcomando migrate.
api_descartavel() { # comando e argumentos
  docker run --rm --network "$REDE_INTERNA" --read-only --cap-drop ALL \
    --security-opt no-new-privileges \
    -v "$SEGREDOS/database-url:/run/secrets/database-url:ro" \
    -e DATABASE_URL_FILE=/run/secrets/database-url \
    "$IMAGEM_API" "$@"
}

# "<id> <hora da subida>" do conteiner da API, ou vazio se ele nao existe.
subida_da_api() {
  docker inspect -f '{{.Id}} {{.State.StartedAt}}' "$CONTEINER_API" 2> /dev/null || true
}

# A linha do log da API (a primeira) que tem o trecho, ou vazio.
linha_do_log() { # texto do log, trecho
  printf '%s\n' "$1" | grep -n -F -- "$2" | head -n 1 | cut -d: -f1 || true
}

# --- 1. o que tem de existir ----------------------------------------------------
[ "$(realpath -m -- "$DEPLOY")" = "$(realpath -m -- "$BASE/deploy")" ] ||
  falhou "o subir.sh roda da copia em $BASE/deploy (a que o enviar-para-servidor.ps1 manda), e nao de $DEPLOY"
[ -r "$COMPOSE" ] || falhou "nao achei $COMPOSE"
docker info > /dev/null 2>&1 || falhou "o Docker nao responde (o seu usuario esta no grupo docker?)"
docker compose version > /dev/null 2>&1 || falhou "o plugin do compose (docker compose) nao responde"
for img in "$IMAGEM_API" "$IMAGEM_BACKUP"; do
  docker image inspect "$img" > /dev/null 2>&1 ||
    falhou "a imagem $img nao esta na VPS: rode no PC o scripts\\enviar-para-servidor.ps1 (o roteiro diz como)"
done
ok "o Docker, o compose e as duas imagens que vieram do PC"
[ -d "$SEGREDOS" ] || falhou "nao achei $SEGREDOS: rode antes o gerar-segredos.sh"
[ "$(stat -c %a "$SEGREDOS")" = 700 ] ||
  falhou "a pasta $SEGREDOS nao esta 0700 (os arquivos 0444 dentro dela ficariam visiveis para os outros usuarios)"
for f in $SEGREDOS_ESPERADOS; do
  [ -f "$SEGREDOS/$f" ] && [ -r "$SEGREDOS/$f" ] || falhou "falta $SEGREDOS/$f (rode o gerar-segredos.sh pelo roteiro)"
done
[ -r "$CONFIG" ] || falhou "nao achei $CONFIG: rode antes o gerar-segredos.sh"
url="$(do_config PUBLIC_BASE_URL)"
dominio="$(do_config DOMINIO)"
contas="$(do_config CONTAS_PERMITIDAS)"
bucket="$(do_config B2_BUCKET)"
[[ "$url" =~ ^https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+)$ ]] ||
  falhou "o PUBLIC_BASE_URL de $CONFIG nao e https://<dominio>"
if [ -z "$dominio" ]; then
  # Um config.env de antes da Parte 2 nao tinha o DOMINIO: sai do endereco.
  dominio="${url#https://}"
  gravar_no_config DOMINIO "$dominio"
fi
[ "$url" = "https://$dominio" ] || falhou "o DOMINIO e o PUBLIC_BASE_URL de $CONFIG nao batem"
[[ "$contas" =~ ^[^@[:space:],]+@[^@[:space:],]+\.[^@[:space:],]+(,[^@[:space:],]+@[^@[:space:],]+\.[^@[:space:],]+)*$ ]] ||
  falhou "o CONTAS_PERMITIDAS de $CONFIG nao e uma lista de e-mails"
[[ "$bucket" =~ ^[A-Za-z0-9-]{6,63}$ ]] || falhou "o B2_BUCKET de $CONFIG nao e um nome de bucket"
ok "os segredos (pasta 0700) e o config.env de https://$dominio"

# Um JSON do Google em recebido/ depois da primeira vez (a primeira o
# gerar-segredos.sh ja moveu): um client NOVO entra no lugar do atual depois
# do SIM do passo 7, com a API recriada (o compose nao a recria quando so o
# conteudo de um segredo muda, e a API le o JSON so na subida); o MESMO sai
# daqui. Nunca fica uma copia esquecida: um JSON recusado para o script ate
# alguem tira-lo. As conferencias sao as do gerar-segredos.sh, e nada do
# arquivo aparece.
troca_google=0
if [ -e "$RECEBIDO" ]; then
  tirar="(para desistir da troca: rm $RECEBIDO)"
  { [ -f "$RECEBIDO" ] && [ -r "$RECEBIDO" ]; } || falhou "nao consegui ler $RECEBIDO $tirar. Nada foi mudado"
  [ "$(wc -c < "$RECEBIDO")" -le "$MAX_JSON_DO_GOOGLE" ] ||
    falhou "o $RECEBIDO e grande demais para ser o JSON do client do Google $tirar. Nada foi mudado"
  if grep -Fq '"installed"' "$RECEBIDO" || ! grep -Fq '"web"' "$RECEBIDO"; then
    falhou "o $RECEBIDO nao e o JSON de um client do tipo Aplicativo da Web do Google $tirar. Nada foi mudado"
  fi
  grep -Fq "\"https://$dominio/auth/google/callback\"" "$RECEBIDO" ||
    falhou "o client do Google em $RECEBIDO nao tem o redirect https://$dominio/auth/google/callback $tirar. Nada foi mudado"
  if cmp -s -- "$RECEBIDO" "$SEGREDOS/google-client.json"; then
    rm -f -- "$RECEBIDO"
    rmdir -- "$BASE/recebido" 2> /dev/null || true
    ok "o JSON do Google em recebido/ e o mesmo de segredos/ (nada a trocar): a copia de entrada saiu"
  else
    troca_google=1
    ok "um client do Google NOVO em recebido/, conferido (o redirect https://$dominio/auth/google/callback): ele troca o atual depois do SIM, com a API recriada"
  fi
fi

# --- 2. o relogio ---------------------------------------------------------------
command -v timedatectl > /dev/null 2>&1 ||
  falhou "nao achei o timedatectl para conferir o NTP; confira a hora da VPS antes de seguir"
ntp="$(timedatectl show -p NTPSynchronized --value 2> /dev/null || true)"
[ "$ntp" = yes ] ||
  falhou "o relogio da VPS nao esta sincronizado (NTPSynchronized=${ntp:-?}): o login do Google, as sessoes e o backup dependem da hora certa. Nada foi mudado"
ok "o relogio sincronizado (NTP)"

# --- 3. a sub-rede da rede do Traefik --------------------------------------------
redes="$(docker network inspect "$REDE_DO_PROXY" --format '{{range .IPAM.Config}}{{.Subnet}} {{end}}' 2> /dev/null)" ||
  falhou "nao achei a rede $REDE_DO_PROXY do Traefik (ele esta de pe?). Nada foi mudado"
sub=''
for s in $redes; do
  if [[ "$s" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$ ]]; then
    [ -z "$sub" ] || falhou "a rede $REDE_DO_PROXY tem mais de uma sub-rede IPv4 ($sub e $s): o PROXY_CONFIAVEL aceita uma so"
    sub="$s"
  elif [[ "$s" == *:* ]]; then
    printf 'AVISO: a rede %s tem tambem IPv6 (%s); so a IPv4 e confiavel. Se o Traefik falar com a API\n' "$REDE_DO_PROXY" "$s"
    printf '       pelo IPv6, o servidor avisa (um WARN do X-Forwarded-For) no log da API.\n'
  fi
done
[ -n "$sub" ] || falhou "nao achei a sub-rede IPv4 da rede $REDE_DO_PROXY"
gravar_no_config PROXY_CONFIAVEL "$sub"
ok "PROXY_CONFIAVEL=$sub (a sub-rede da rede $REDE_DO_PROXY), gravado em $CONFIG"

# --- 4. o compose ---------------------------------------------------------------
cfg="$(dc config 2> /dev/null)" || falhou "o docker compose recusou o $COMPOSE com o $CONFIG (rode: docker compose --project-name $PROJETO --env-file $CONFIG -f $COMPOSE config --quiet)"
# A regra do Traefik e a do config.env conferido, e so ela: um DOMINIO de
# outro lugar faria o nosso conteiner disputar a rota de outro site.
regras="$(printf '%s\n' "$cfg" | grep -o 'Host(`[^`]*`)' | sort -u || true)"
[ "$regras" = "Host(\`$dominio\`)" ] ||
  falhou "a regra do Traefik que o compose montou nao e a do config.env (Host(\`$dominio\`)). Nada subiu"
ok "o compose conferido, com a regra Host(\`$dominio\`)"

# --- 5. o Postgres ---------------------------------------------------------------
dc up -d "$CONTEINER_POSTGRES" > /dev/null 2>&1 || falhou "o compose nao subiu o $CONTEINER_POSTGRES"
esperar_saudavel "$CONTEINER_POSTGRES" "$ESPERA_POSTGRES" ||
  falhou "o $CONTEINER_POSTGRES nao ficou saudavel em $ESPERA_POSTGRES s (veja: docker logs --tail 30 $CONTEINER_POSTGRES)"
ok "o Postgres de pe e saudavel"
bash "$DEPLOY/postgres/conferir-postgres.sh" ||
  falhou "o Postgres nao passou na conferencia (acima). A API nao subiu"

# --- 6. as migrations -------------------------------------------------------------
status="$(api_descartavel migrate -status)" || falhou "o migrate -status falhou (o banco responde? a database-url esta certa?)"
printf '%s\n' "$status"
if printf '%s\n' "$status" | grep -q '^pendente '; then
  printf '\nHa migration pendente (acima). Rodar o migrate em producao e ponto de parada.\n'
  if vivo "$CONTEINER_API"; then
    printf 'A API esta de pe: ela para antes do migrate e sobe de novo depois do SIM do passo 7.\n'
  fi
  printf 'Digite SIM para rodar o migrate agora (qualquer outra coisa para): '
  resposta=''
  IFS= read -r resposta || true
  [ "$resposta" = SIM ] || parou "sem o SIM, o migrate nao rodou. O Postgres ficou de pe; a API nao mudou."
  if vivo "$CONTEINER_API"; then
    dc stop "$CONTEINER_API" > /dev/null 2>&1 || falhou "nao consegui parar a API antes do migrate"
    ok "a API parou para o migrate"
  fi
  api_descartavel migrate || falhou "o migrate falhou (acima). A API nao subiu"
  status="$(api_descartavel migrate -status)" || falhou "o migrate -status falhou depois do migrate"
  if printf '%s\n' "$status" | grep -q '^pendente '; then
    falhou "ainda ha migration pendente depois do migrate"
  fi
fi
ok "as migrations em dia"

# --- 7. o SIM para abrir --------------------------------------------------------
printf '\nTudo pronto para abrir a nuvem em https://%s:\n' "$dominio"
printf '  - o Postgres conferido, as migrations em dia, PROXY_CONFIAVEL=%s;\n' "$sub"
printf '  - o Traefik passa a atender %s assim que a API ficar saudavel;\n' "$dominio"
printf '  - nenhum dado real sobe antes do OK da revisao de LGPD (o dossie e a politica).\n'
if [ "$troca_google" = 1 ]; then
  printf '  - o client do Google e TROCADO pelo JSON novo de recebido/, e a API e recriada com ele.\n'
fi
printf 'Digite SIM para subir a API (qualquer outra coisa para): '
resposta=''
IFS= read -r resposta || true
if [ "$resposta" != SIM ]; then
  if [ "$troca_google" = 1 ]; then
    parou "sem o SIM, a API nao subiu e o client do Google nao foi trocado (o JSON novo continua em recebido/). O Postgres ficou de pe (rode o subir.sh de novo quando quiser)."
  fi
  parou "sem o SIM, a API nao subiu. O Postgres ficou de pe (rode o subir.sh de novo quando quiser)."
fi

# --- 8. a API -------------------------------------------------------------------
# O conteiner e a hora da subida, antes e depois do compose: so o log da
# subida de agora prova a ordem (o docker logs inteiro traria a subida de
# semanas atras, ou nada dela, se a rotacao do log ja a levou).
api_antes="$(subida_da_api)"
if [ "$troca_google" = 1 ]; then
  # O arquivo novo, 0444 como os outros, no lugar do atual de uma vez (mv).
  # O conteiner que ja roda continua com o arquivo velho montado, e a API so
  # le o JSON na subida: por isso o --force-recreate.
  novo_google="$SEGREDOS/.google-client.json.novo.$$"
  if ! cp -- "$RECEBIDO" "$novo_google" || ! chmod 0444 "$novo_google" ||
    ! mv -f -- "$novo_google" "$SEGREDOS/google-client.json"; then
    rm -f -- "$novo_google"
    falhou "nao consegui trocar o $SEGREDOS/google-client.json (o atual ficou); a API nao foi mexida"
  fi
  rm -f -- "$RECEBIDO"
  rmdir -- "$BASE/recebido" 2> /dev/null || true
  ok "o client do Google trocado em $SEGREDOS (0444); a copia de entrada saiu"
  dc up -d --force-recreate "$CONTEINER_API" > /dev/null 2>&1 || falhou "o compose nao recriou a API com o client novo"
else
  dc up -d "$CONTEINER_API" > /dev/null 2>&1 || falhou "o compose nao subiu a API"
fi
if ! esperar_saudavel "$CONTEINER_API" "$ESPERA_API"; then
  printf 'As ultimas linhas do log da API (so nomes de variavel e contagens, nunca texto):\n' >&2
  docker logs --tail 20 "$CONTEINER_API" >&2 2>&1 || true
  falhou "a API nao ficou saudavel em $ESPERA_API s"
fi
api_agora="$(subida_da_api)"
inicio="${api_agora#* }"
[ -n "$api_agora" ] && [ "$inicio" != "$api_agora" ] && [ -n "$inicio" ] ||
  falhou "nao consegui ler a hora da subida da API (docker inspect)"
if [ -n "$api_antes" ] && [ "$api_antes" = "$api_agora" ]; then
  # Nem recriada nem reiniciada: a mesma imagem e a mesma configuracao (o
  # mesmo PROXY_CONFIAVEL). A ordem valeu na subida dela, e o log daquela
  # subida pode ja ter saido da rotacao.
  ok "a API saudavel; ela nao foi recriada nem reiniciada agora (nada mudou nela), e a ordem do log ficou conferida na subida dela"
else
  log="$(docker logs --since "$inicio" "$CONTEINER_API" 2>&1 || true)"
  n_arrumacao="$(linha_do_log "$log" '"logins_pendentes"')"
  n_retencao="$(linha_do_log "$log" '"lapides_criadas"')"
  n_servindo="$(linha_do_log "$log" '"msg":"servindo"')"
  if [ -z "$n_arrumacao" ] || [ -z "$n_retencao" ] || [ -z "$n_servindo" ] ||
    [ "$n_arrumacao" -ge "$n_retencao" ] || [ "$n_retencao" -ge "$n_servindo" ]; then
    falhou "o log desta subida da API nao mostra a ordem arrumacao, retencao, servindo (veja: docker logs --since $inicio $CONTEINER_API)"
  fi
  printf '%s\n' "$log" | sed -n "${n_servindo}p" | grep -q '"proxy_confiavel":true' ||
    falhou "a linha servindo da API diz que o proxy confiavel esta desligado"
  ok "a API saudavel: arrumacao, retencao e so entao servindo, com o proxy confiavel ligado"
fi
# A saude do job, por fora do roteamento. A saida e uma linha so, sem valor
# de configuracao (cmd/server/healthcheck.go).
if ! job="$(docker exec "$CONTEINER_API" "$BINARIO_DA_API" healthcheck -retencao 2>&1)"; then
  printf '%s\n' "$job" >&2
  falhou "a API esta de pe (o Traefik ja a atende), mas a saude do job de retencao falhou (acima). O backup nao subiu"
fi
ok "a saude do job de retencao (healthcheck -retencao)"

# --- 9. o backup ----------------------------------------------------------------
dc up -d "$CONTEINER_BACKUP" > /dev/null 2>&1 || falhou "o compose nao subiu o $CONTEINER_BACKUP"
[ "$(docker inspect -f '{{.State.Status}}' "$CONTEINER_BACKUP" 2> /dev/null || true)" = running ] ||
  falhou "o $CONTEINER_BACKUP nao ficou de pe (veja: docker logs $CONTEINER_BACKUP)"
ok "o agendador do backup de pe (todo dia as 03:00 de Sao Paulo)"

# --- 10. a borda ----------------------------------------------------------------
if command -v curl > /dev/null 2>&1; then
  i=0
  codigo=''
  while [ "$i" -lt "$ESPERA_HTTPS" ]; do
    codigo="$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 10 "https://$dominio/api/v1/health/live" 2> /dev/null || true)"
    if [ "$codigo" = 200 ]; then break; fi
    sleep 5
    i=$((i + 5))
  done
  [ "$codigo" = 200 ] ||
    falhou "a API esta de pe, mas https://$dominio nao respondeu 200 em $ESPERA_HTTPS s (o DNS aponta para esta VPS? O Traefik emitiu o certificado? So para ler: docker logs --since 10m traefik)"
  ok "https://$dominio responde pelo Traefik, com o certificado valido"
else
  printf 'AVISO: sem o curl na VPS; o https se confere pelo scripts\\conferir-nuvem.ps1, no PC.\n'
fi

printf '\nOK: a nuvem esta no ar em https://%s.\n' "$dominio"
printf 'Agora, pelo roteiro (docs/deploy-f11.md):\n'
printf '  1. no PC, a conferencia de fora: scripts\\conferir-nuvem.ps1;\n'
printf '  2. aqui, o primeiro backup: docker exec %s backup.sh\n' "$CONTEINER_BACKUP"
printf '  3. se ainda nao fez, a copia do DEVICE_TOKEN_PEPPER no gerenciador de senhas.\n'
