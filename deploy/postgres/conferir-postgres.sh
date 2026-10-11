#!/usr/bin/env bash
# Confere o Postgres da nuvem ja de pe (ADR-019, "Decisoes da F11", F11e): os
# cinco ajustes de log que a ADR manda, para o log do proprio Postgres nunca
# trazer texto ditado, e o papel da aplicacao.
#
#   log_error_verbosity               = terse  (sem DETAIL "Failing row contains")
#   log_statement                     = none
#   log_min_duration_statement        = -1
#   log_parameter_max_length_on_error = 0
#   auto_explain fora de shared_preload_libraries e de session_preload_libraries
#
#   o papel aurora sem superusuario (nem CREATEDB, CREATEROLE, REPLICATION,
#   BYPASSRLS), e o banco aurora de dono dele
#
#   o papel aurora_backup, do backup, sem nenhum desses poderes, so de leitura
#   (membro do pg_read_all_data e nao do pg_write_all_data) e com a entrada no
#   banco aurora
#
# Quem roda e o Mauro, na VPS, antes do primeiro dado real (e depois de toda
# troca de imagem ou de configuracao do Postgres):
#
#   bash ~/aurora-whisper/deploy/postgres/conferir-postgres.sh
#
# So le: SHOW e SELECT pelo superusuario, de dentro do conteiner (docker exec,
# pelo socket). Imprime uma linha OK ou FALHOU por item, com o nome e o valor
# que achou (que nao e segredo), e sai com 0 so se tudo bateu.
set -euo pipefail

CONTEINER_POSTGRES='aurora-whisper-postgres'
SUPERUSUARIO='postgres'
PAPEL='aurora'
PAPEL_BACKUP='aurora_backup'
BANCO='aurora'

falhas=0

consultar() {
  docker exec "$CONTEINER_POSTGRES" psql -X -A -t -q -v ON_ERROR_STOP=1 \
    -U "$SUPERUSUARIO" -d postgres -c "$1"
}

falhou() {
  printf 'FALHOU: %s\n' "$*"
  falhas=$((falhas + 1))
}

conferir() { # nome esperado achado
  if [ "$3" = "$2" ]; then
    printf 'OK: %s = %s\n' "$1" "$3"
  else
    falhou "$1 = $3 (o esperado e $2)"
  fi
}

if ! docker exec "$CONTEINER_POSTGRES" pg_isready -q -U "$SUPERUSUARIO" >/dev/null 2>&1; then
  printf 'FALHOU: o conteiner %s nao responde (ele esta de pe?)\n' "$CONTEINER_POSTGRES"
  exit 1
fi

for par in 'log_error_verbosity terse' 'log_statement none' \
  'log_min_duration_statement -1' 'log_parameter_max_length_on_error 0'; do
  nome="${par%% *}"
  esperado="${par#* }"
  if valor="$(consultar "SHOW $nome")"; then
    conferir "$nome" "$esperado" "$valor"
  else
    falhou "nao consegui ler $nome"
  fi
done

for lista in shared_preload_libraries session_preload_libraries; do
  if valor="$(consultar "SHOW $lista")"; then
    case "$valor" in
      *auto_explain*) falhou "$lista carrega o auto_explain ($valor)" ;;
      *) printf 'OK: %s sem o auto_explain\n' "$lista" ;;
    esac
  else
    falhou "nao consegui ler $lista"
  fi
done

if valor="$(consultar "SELECT concat_ws(' ', rolsuper::text, rolcreatedb::text, rolcreaterole::text, rolreplication::text, rolbypassrls::text) FROM pg_roles WHERE rolname = '$PAPEL'")"; then
  case "$valor" in
    '') falhou "o papel $PAPEL nao existe" ;;
    'false false false false false') printf 'OK: o papel %s nao e superusuario (nem cria banco, papel ou replicacao)\n' "$PAPEL" ;;
    *) falhou "o papel $PAPEL tem poder demais (superusuario, createdb, createrole, replication, bypassrls: $valor)" ;;
  esac
else
  falhou "nao consegui ler o papel $PAPEL"
fi

# O papel do backup: sem poder, membro do pg_read_all_data e nao do
# pg_write_all_data, e com a entrada (CONNECT) no banco da aplicacao.
if valor="$(consultar "SELECT concat_ws(' ', rolsuper::text, rolcreatedb::text, rolcreaterole::text, rolreplication::text, rolbypassrls::text, pg_has_role(oid, 'pg_read_all_data', 'MEMBER')::text, pg_has_role(oid, 'pg_write_all_data', 'MEMBER')::text, (SELECT has_database_privilege(r.oid, d.oid, 'CONNECT') FROM pg_database d WHERE d.datname = '$BANCO')::text) FROM pg_roles r WHERE rolname = '$PAPEL_BACKUP'")"; then
  case "$valor" in
    '') falhou "o papel $PAPEL_BACKUP nao existe" ;;
    'false false false false false true false true') printf 'OK: o papel %s so le (pg_read_all_data) e entra no banco %s\n' "$PAPEL_BACKUP" "$BANCO" ;;
    *) falhou "o papel $PAPEL_BACKUP nao esta como deveria (superusuario, createdb, createrole, replication, bypassrls, le tudo, escreve tudo, entra no banco: $valor; o esperado e false false false false false true false true)" ;;
  esac
else
  falhou "nao consegui ler o papel $PAPEL_BACKUP"
fi

if valor="$(consultar "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = '$BANCO'")"; then
  case "$valor" in
    '') falhou "o banco $BANCO nao existe" ;;
    "$PAPEL") printf 'OK: o banco %s e do papel %s\n' "$BANCO" "$PAPEL" ;;
    *) falhou "o banco $BANCO e de $valor (o esperado e $PAPEL)" ;;
  esac
else
  falhou "nao consegui ler o dono do banco $BANCO"
fi

if [ "$falhas" -gt 0 ]; then
  printf 'FALHOU: %d item(ns) fora do esperado. Nenhum dado real sobe ate isto passar.\n' "$falhas"
  exit 1
fi
printf 'OK: os cinco ajustes de log e os papeis %s e %s conferidos\n' "$PAPEL" "$PAPEL_BACKUP"
