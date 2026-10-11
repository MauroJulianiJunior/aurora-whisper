#!/usr/bin/env bash
# Os papeis da aplicacao no Postgres da nuvem (ADR-019, "Decisoes da F11", F11e).
#
# A imagem oficial do Postgres roda os arquivos de /docker-entrypoint-initdb.d
# UMA vez, na criacao do volume, como o usuario postgres, com um servidor
# temporario que so escuta no socket. Este arquivo cria:
#   - o papel "aurora": LOGIN, NOSUPERUSER, NOCREATEDB, NOCREATEROLE,
#     NOREPLICATION e NOBYPASSRLS, com a senha do arquivo de segredo;
#   - o papel "aurora_backup", do backup: os mesmos NO..., com outra senha, e
#     so de leitura (pg_read_all_data). O conteiner do backup e o unico que
#     fala com a internet (o rclone para o B2); com ele, quem tomasse uma
#     imagem de base ou o rclone so leria o banco, e nao o apagaria;
#   - o banco "aurora", de dono do papel aurora (o migrate cria as tabelas como
#     dono; as migrations nao precisam de extensao nenhuma), sem CONNECT para o
#     PUBLIC e com CONNECT para o aurora_backup (o pg_read_all_data le as
#     tabelas, mas nao da a entrada no banco).
# A API e o migrate usam o papel aurora. O superusuario que a imagem cria
# (POSTGRES_USER, com a senha de POSTGRES_PASSWORD_FILE, outra senha) fica so
# para administrar: criar e renomear bancos na restauracao, conferir os
# parametros. Com o superusuario na API, um "COPY ... TO PROGRAM" daria um shell
# no conteiner.
#
# As senhas nunca vao para a linha de comando nem para a tela: o psql as le dos
# arquivos (o \set com crase) e as entrega ao CREATE ROLE como literal
# (:'senha'). Os arquivos vem de AURORA_SENHA_FILE (padrao
# /run/secrets/postgres-aurora-senha) e de AURORA_BACKUP_SENHA_FILE (padrao
# /run/secrets/postgres-backup-senha), e sao os que o
# deploy/scripts/gerar-segredos.sh grava: 64 hexadecimais numa linha so, um
# diferente do outro.
#
# POSTGRES_DB nao pode ser "aurora": a imagem criaria o banco com o dono errado.
# POSTGRES_USER nao pode ser "aurora" nem "aurora_backup": a imagem criaria o
# papel como superusuario, e o CREATE ROLE daqui falharia.
#
# A sessao do psql comeca desligando, so para ela, o log de comando: se um
# comando daqui falhar (o CREATE ROLE de um papel que ja existe, por exemplo),
# o Postgres registraria a linha STATEMENT com o comando inteiro, e com ela a
# senha em claro (o log_min_error_statement padrao e "error", e o
# log_error_verbosity terse nao corta o STATEMENT). Quem roda e o superusuario
# da imagem, que pode mudar esses ajustes na propria sessao.
#
# A imagem roda o arquivo com bash se ele e executavel, e com "." (source) se
# nao e. Por isso tudo fica num subshell: o set -euo pipefail e o exit nao
# vazam para o script da imagem, e qualquer falha daqui faz a inicializacao
# inteira falhar (o volume fica para ser apagado e criado de novo).
(
set -euo pipefail

PAPEL='aurora'
PAPEL_BACKUP='aurora_backup'
BANCO='aurora'
ARQUIVO_DA_SENHA="${AURORA_SENHA_FILE:-/run/secrets/postgres-aurora-senha}"
ARQUIVO_DA_SENHA_BACKUP="${AURORA_BACKUP_SENHA_FILE:-/run/secrets/postgres-backup-senha}"

falhou() {
  printf 'FALHOU: %s\n' "$*" >&2
  exit 1
}

if [ "${POSTGRES_DB:-}" = "$BANCO" ]; then
  falhou "POSTGRES_DB nao pode ser $BANCO: o banco da aplicacao e criado aqui, de dono $PAPEL"
fi
case "${POSTGRES_USER:-postgres}" in
  "$PAPEL" | "$PAPEL_BACKUP")
    falhou "POSTGRES_USER nao pode ser ${POSTGRES_USER}: o superusuario e outro (postgres); os papeis $PAPEL e $PAPEL_BACKUP sao criados aqui, sem poder"
    ;;
esac
# O grep -q e o wc nao imprimem a senha: so dizem se ela tem a forma certa.
conferir_senha() { # arquivo papel variavel
  if [ ! -f "$1" ] || [ ! -r "$1" ]; then
    falhou "nao consegui ler o arquivo da senha do papel $2 ($3)"
  fi
  if ! grep -Eqx '[0-9a-f]{64}' "$1" || [ "$(wc -l < "$1")" -gt 1 ]; then
    falhou "o arquivo da senha do papel $2 nao e uma linha so de 64 hexadecimais (gerar-segredos.sh)"
  fi
}
conferir_senha "$ARQUIVO_DA_SENHA" "$PAPEL" AURORA_SENHA_FILE
conferir_senha "$ARQUIVO_DA_SENHA_BACKUP" "$PAPEL_BACKUP" AURORA_BACKUP_SENHA_FILE
if grep -Fqx -f "$ARQUIVO_DA_SENHA" "$ARQUIVO_DA_SENHA_BACKUP"; then
  falhou "a senha do papel $PAPEL_BACKUP e a mesma do $PAPEL (gerar-segredos.sh gera uma para cada)"
fi

psql -v ON_ERROR_STOP=1 --no-psqlrc --quiet \
  --username "${POSTGRES_USER:-postgres}" --dbname postgres \
  --set=papel="$PAPEL" --set=papel_backup="$PAPEL_BACKUP" --set=banco="$BANCO" \
  --set=arquivo="$ARQUIVO_DA_SENHA" --set=arquivo_backup="$ARQUIVO_DA_SENHA_BACKUP" <<'SQL'
SET log_statement = none;
SET log_min_duration_statement = -1;
SET log_min_error_statement = panic;
\set senha `cat :'arquivo'`
CREATE ROLE :"papel" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD :'senha';
\unset senha
\set senha_backup `cat :'arquivo_backup'`
CREATE ROLE :"papel_backup" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD :'senha_backup';
\unset senha_backup
GRANT pg_read_all_data TO :"papel_backup";
CREATE DATABASE :"banco" OWNER :"papel";
REVOKE ALL ON DATABASE :"banco" FROM PUBLIC;
GRANT CONNECT ON DATABASE :"banco" TO :"papel_backup";
SQL

printf 'OK: papel %s criado (sem superusuario), banco %s de dono dele, e papel %s so de leitura\n' "$PAPEL" "$BANCO" "$PAPEL_BACKUP"
)
