#!/usr/bin/env bash
# Tira o CADASTRO de uma pessoa da nuvem (a linha dela em users: e-mail,
# identificador do Google e nome), a pedido dela. A politica de privacidade
# promete isso (secao 8, "O cadastro"): o "Apagar meus dados" apaga tudo o que a
# pessoa fez, mas deixa o cadastro, e quem o tira e o responsavel, pelo
# contato. Quem roda e o Mauro, na VPS, como o usuario dele (sem sudo), uma vez
# por pedido:
#
#   bash ~/aurora-whisper/deploy/scripts/tirar-cadastro.sh pessoa@exemplo.com
#
# Antes de tudo, o e-mail tem de sair de CONTAS_PERMITIDAS (no
# ~/aurora-whisper/config.env) e a API tem de ser recriada com a lista nova
# (bash ~/aurora-whisper/deploy/scripts/subir.sh): se ele continuasse la, a
# pessoa poderia entrar de novo e o cadastro voltaria. O script RECUSA se o
# e-mail ainda esta no config.env ou no CONTAS_PERMITIDAS do conteiner da API
# que esta de pe.
#
# O que ele faz, em ordem (para no primeiro FALHOU, sem mudar nada):
#
#   1. confere o e-mail e o Postgres;
#   2. confere que o e-mail nao esta mais em CONTAS_PERMITIDAS;
#   3. acha o cadastro e mostra o que vai sair;
#   4. RECUSA se a pessoa ainda tem dados: toda tabela que aponta para users
#      (lidas do proprio banco, pelas chaves estrangeiras, entao uma tabela
#      nova nas migrations tambem entra na conferencia) tem de estar vazia
#      para ela, menos as duas que o "Apagar meus dados" deixa de proposito: o
#      contador de sincronizacao (user_sync_counters, com o epoch novo) e a
#      sessao que ficou (web_sessions). Se sobrou algo, o script diz em que
#      tabela e quantas linhas (nunca o conteudo): a pessoa usa o "Apagar meus
#      dados" no app e voce roda de novo;
#   5. pede "digite SIM" e, so entao, apaga, numa transacao so (tudo ou nada),
#      as sessoes, o contador e a linha de users.
#
# Fala com o Postgres pelo superusuario, de dentro do conteiner (docker exec,
# pelo socket), como o conferir-postgres.sh. O e-mail vai ao psql como variavel
# (-v), e nunca dentro do texto do SQL. Nao grava nada em arquivo e o e-mail e
# o nome so aparecem nesta tela.
#
# A copia de seguranca (cifrada, no B2) guarda o cadastro ate girar, cerca de
# 30 dias, como a politica diz (secao 7); o script lembra no fim.
set -euo pipefail
umask 077

BASE="${AW_BASE:-$HOME/aurora-whisper}"
CONFIG="$BASE/config.env"
CONTEINER_POSTGRES='aurora-whisper-postgres'
CONTEINER_API='aurora-whisper-api'
SUPERUSUARIO='postgres'
BANCO='aurora'
# O que o "Apagar meus dados" deixa de proposito (internal/server/pgstore,
# apagarDaConta): o contador da conta, com o epoch novo, e a sessao que ficou.
# Estes dois saem junto com o cadastro, na mesma transacao.
TABELAS_QUE_SOBRAM='user_sync_counters web_sessions'

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

# O SQL, em blocos (os testes de Postgres de verdade tiram estes blocos daqui e
# os rodam). Variaveis do psql: :'email' (o e-mail, em minusculas) e :'uid' (o
# id do cadastro).
IFS= read -r -d '' SQL_ACHAR << 'SQL_ACHAR' || true
SELECT id, email, to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'), coalesce(display_name, '')
FROM users
WHERE email = lower(:'email')
SQL_ACHAR

IFS= read -r -d '' SQL_RESTOS << 'SQL_RESTOS' || true
SELECT c.conrelid::regclass::text,
       (xpath('/row/n/text()', query_to_xml(
         format('SELECT count(*) AS n FROM %s WHERE %I = %L', c.conrelid::regclass, a.attname, :'uid'),
         false, true, '')))[1]::text
FROM pg_constraint c
JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
WHERE c.contype = 'f' AND c.confrelid = 'public.users'::regclass
ORDER BY 1
SQL_RESTOS

IFS= read -r -d '' SQL_TIRAR << 'SQL_TIRAR' || true
DELETE FROM web_sessions WHERE user_id = :'uid';
DELETE FROM user_sync_counters WHERE user_id = :'uid';
DELETE FROM users WHERE id = :'uid';
SQL_TIRAR

# Roda o SQL do primeiro argumento no banco da nuvem; os demais argumentos
# sao variaveis do psql (-v nome=valor). O SQL vai pela entrada do psql, que e
# onde ele troca :'variavel' pelo literal.
psql_aurora() { # sql [-v nome=valor ...]
  local sql="$1"
  shift
  printf '%s\n' "$sql" | docker exec -i "$CONTEINER_POSTGRES" psql -X -A -t -q -v ON_ERROR_STOP=1 \
    "$@" -U "$SUPERUSUARIO" -d "$BANCO" -f -
}

# --- 1. o e-mail e o Postgres -----------------------------------------------------
[ "$#" -eq 1 ] || falhou "use: bash ~/aurora-whisper/deploy/scripts/tirar-cadastro.sh pessoa@exemplo.com"
email="$(printf '%s' "$1" | tr 'A-Z' 'a-z')"
[[ "$email" =~ ^[a-z0-9._%+-]+@[a-z0-9-]+(\.[a-z0-9-]+)+$ ]] || falhou "isso nao parece um e-mail (letras, numeros, ponto, hifen, sublinhado e +, sem espaco nem aspas)"
docker info > /dev/null 2>&1 || falhou "o Docker nao responde (o seu usuario esta no grupo docker?)"
docker exec "$CONTEINER_POSTGRES" pg_isready -q -U "$SUPERUSUARIO" > /dev/null 2>&1 ||
  falhou "o conteiner $CONTEINER_POSTGRES nao responde (ele esta de pe?)"
ok "o Postgres responde"

# --- 2. o e-mail nao pode mais estar em CONTAS_PERMITIDAS -------------------------
# Na lista da config e na do conteiner da API que esta de pe (a que ela le so na
# subida): tirar do config.env e nao recriar a API deixaria a pessoa entrar.
na_lista() { # lista de e-mails separados por virgula; sucesso se $email esta nela
  local item
  local IFS=','
  for item in $1; do
    item="$(printf '%s' "$item" | tr 'A-Z' 'a-z' | tr -d '[:space:]')"
    [ "$item" = "$email" ] && return 0
  done
  return 1
}
if [ -r "$CONFIG" ]; then
  contas="$(sed -n 's/^CONTAS_PERMITIDAS=//p' "$CONFIG" | tail -n 1)"
  if na_lista "$contas"; then
    falhou "o e-mail ainda esta no CONTAS_PERMITIDAS de $CONFIG: tire-o de la, rode o subir.sh (a API le a lista so na subida) e rode este script de novo. Nada foi mudado"
  fi
  ok "o e-mail nao esta no CONTAS_PERMITIDAS de $CONFIG"
else
  printf 'AVISO: nao consegui ler %s: confira a mao que o e-mail saiu do CONTAS_PERMITIDAS, senao a pessoa entra de novo e o cadastro volta.\n' "$CONFIG"
fi
if env_da_api="$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CONTEINER_API" 2> /dev/null)"; then
  contas_da_api="$(printf '%s\n' "$env_da_api" | sed -n 's/^CONTAS_PERMITIDAS=//p' | tail -n 1)"
  if na_lista "$contas_da_api"; then
    falhou "o e-mail ainda esta no CONTAS_PERMITIDAS da API que esta de pe (ela le a lista so na subida): rode o subir.sh depois de tira-lo do config.env e rode este script de novo. Nada foi mudado"
  fi
  ok "o e-mail nao esta no CONTAS_PERMITIDAS da API que esta de pe"
else
  printf 'AVISO: o conteiner %s nao existe ou nao respondeu: a lista da API nao foi conferida.\n' "$CONTEINER_API"
fi

# --- 3. o cadastro ---------------------------------------------------------------
achado="$(psql_aurora "$SQL_ACHAR" -v "email=$email")" || falhou "nao consegui consultar o banco"
[ -n "$achado" ] || falhou "nao ha cadastro com esse e-mail (nada a tirar)"
[ "$(printf '%s\n' "$achado" | wc -l)" -eq 1 ] || falhou "achei mais de um cadastro para o e-mail: isso nao deveria acontecer (o e-mail e unico). Nada foi mudado"
IFS='|' read -r uid email_achado criado nome <<< "$achado"
[[ "$uid" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] || falhou "o id do cadastro nao e um UUID"
ok "achei o cadastro"

# --- 4. a pessoa nao pode ter mais dados -----------------------------------------
restos="$(psql_aurora "$SQL_RESTOS" -v "uid=$uid")" || falhou "nao consegui conferir as tabelas da pessoa"
[ -n "$restos" ] || falhou "nao achei nenhuma tabela que aponte para users: o banco nao e o da nuvem? Nada foi mudado"
com_dados=''
sobras=0
while IFS='|' read -r tabela n; do
  [ -n "$tabela" ] || continue
  [[ "$n" =~ ^[0-9]+$ ]] || falhou "a contagem de $tabela nao e um numero ($n)"
  tabela="${tabela#public.}"
  if [ "$n" -gt 0 ]; then
    case " $TABELAS_QUE_SOBRAM " in
      *" $tabela "*) sobras=$((sobras + 1)) ;;
      *) com_dados="$com_dados  - $tabela: $n linha(s)\n" ;;
    esac
  fi
done <<< "$restos"
if [ -n "$com_dados" ]; then
  printf 'A pessoa ainda tem dados na nuvem (so a tabela e a contagem, nunca o conteudo):\n' >&2
  printf '%b' "$com_dados" >&2
  falhou "nao tiro o cadastro de quem ainda tem dados. Peca que a pessoa use o \"Apagar meus dados\" no app e rode este script de novo. Nada foi mudado"
fi
ok "a pessoa so tem o que o \"Apagar meus dados\" deixa (o contador de sincronizacao e a sessao)"

# --- 5. o SIM, e o apagar ---------------------------------------------------------
printf '\nVai sair, para sempre (numa transacao so):\n'
printf '  o cadastro: %s (nome: %s), criado em %s;\n' "$email_achado" "${nome:-sem nome}" "$criado"
printf '  o contador de sincronizacao e as sessoes que sobraram dela.\n'
printf 'Nada mais: a pessoa nao tem outro dado. A copia de seguranca guarda o cadastro por cerca de 30 dias.\n'
printf 'Digite SIM para tirar (qualquer outra coisa para): '
resposta=''
IFS= read -r resposta || true
[ "$resposta" = SIM ] || parou "sem o SIM, nada foi tirado."

psql_aurora "$SQL_TIRAR" -1 -v "uid=$uid" > /dev/null || falhou "o banco recusou (a pessoa pode ter gravado algo agora): nada foi tirado, porque e uma transacao so. Rode de novo"
confere="$(psql_aurora "$SQL_ACHAR" -v "email=$email")" || falhou "tirei, mas nao consegui conferir; olhe a mao"
[ -z "$confere" ] || falhou "o cadastro ainda esta no banco depois de apagar"
ok "o cadastro saiu do banco"

printf '\nPronto. Para fechar o pedido:\n'
printf '  - responda a pessoa (art. 19: ate 15 dias) que o cadastro foi tirado;\n'
printf '  - o cadastro ainda vive na copia de seguranca por cerca de 30 dias (politica, secao 7);\n'
printf '  - se ela entrar de novo, e porque o e-mail voltou ao CONTAS_PERMITIDAS: o cadastro nasce de novo.\n'
