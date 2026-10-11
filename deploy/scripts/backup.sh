#!/usr/bin/env bash
# O backup da nuvem (ADR-019, "Decisoes da F11", D2 e F11f). Roda DENTRO do
# conteiner do backup (a imagem de deploy/backup/Dockerfile: o cliente do
# Postgres 18, o age e o rclone), uma vez por chamada. Quem agenda e o proprio
# compose (Parte 2): nada de cron no host.
#
# O que faz, e o que cada passo confere:
#   1. trava contra execucao dupla (flock);
#   2. pg_dump --format=custom --no-owner --no-privileges do banco, pelo papel
#      so de leitura aurora_backup (pg_read_all_data; o init do Postgres o
#      cria): este e o unico conteiner que fala com a internet, e quem o
#      tomasse leria o banco, mas nao o apagaria. A senha vai so para o
#      ambiente do pg_dump (nem o age nem o rclone a herdam). O fluxo passa
#      por um "pg_restore --list" (por tee) ANTES
#      da cifra, e o PIPESTATUS de cada etapa e conferido: um dump que falha no
#      meio nao vira um arquivo cifrado truncado com cara de OK (este conteiner
#      so cifra e nao consegue decifrar para conferir depois). Nenhum dump em
#      claro vai para o disco: entre as etapas ha so canos e um FIFO;
#   3. cifra com o age para a chave PUBLICA (a privada so existe no PC do
#      Mauro), num arquivo .partial que so ganha o nome final depois de tudo
#      conferido;
#   4. envia ao Backblaze B2 com o rclone (copyto --immutable), com uma
#      application key restrita ao bucket, so com listBuckets, listFiles e
#      writeFiles (SEM deleteFiles): este script nunca apaga nada la, e a chave
#      nao apaga de vez o que subiu. Mas a writeFiles tambem deixa ESCONDER um
#      arquivo ou subir outra versao com o mesmo nome, e a regra de ciclo de
#      vida apaga de vez o que foi escondido: quem segura os backups contra
#      quem invadir a VPS e o Object Lock do bucket, com retencao padrao de 30
#      dias ou mais (deploy/README.md). Sem ele, a chave apaga, so que com um
#      dia de atraso. O deploy/scripts/conferir-b2.sh confere a chave antes do
#      primeiro uso. Os nomes levam a data e a hora, entao nada e sobrescrito.
#      O giro de 30 dias e a regra de ciclo de vida do proprio bucket. Confere
#      o tamanho do que subiu;
#   5. so entao grava a hora do ultimo sucesso (.ultimo-sucesso), que o
#      healthcheck do conteiner do backup le (Parte 2: unhealthy com mais de
#      26 h);
#   6. guarda o cifrado 3 dias no volume, para reenviar se o B2 falhar (o que
#      nao subiu e reenviado na chamada seguinte e nunca e apagado daqui antes
#      de subir), e apaga o que ja subiu e passou disso.
#
# Uma linha de log por chamada (backup: OK ou backup: FALHOU), sem segredo e
# sem texto ditado. A saida do pg_dump, do age e do rclone nao vai para o log.
#
# Configuracao (variaveis de ambiente; entre parenteses, o padrao do compose):
#   PGHOST (aurora-whisper-postgres), PGPORT (5432), PGUSER (aurora_backup),
#   PGDATABASE (aurora)
#   AW_SENHA_FILE       a senha do papel (/run/secrets/postgres-backup-senha)
#   AW_CHAVE_PUBLICA    a chave publica do age (/run/secrets/backup-chave-publica)
#   RCLONE_CONFIG       o rclone.conf com o remoto "b2" (/run/secrets/rclone.conf)
#   B2_BUCKET           o bucket (do config.env); obrigatoria
#   AW_PASTA_BACKUP     o volume do backup (/backup)
#   AW_PREFIXO_REMOTO   a pasta dentro do bucket (aurora-whisper)
set -euo pipefail
umask 077

PASTA="${AW_PASTA_BACKUP:-/backup}"
SENHA_FILE="${AW_SENHA_FILE:-/run/secrets/postgres-backup-senha}"
CHAVE_PUBLICA="${AW_CHAVE_PUBLICA:-/run/secrets/backup-chave-publica}"
PREFIXO="${AW_PREFIXO_REMOTO:-aurora-whisper}"
DIAS_NO_VOLUME=3
export RCLONE_CONFIG="${RCLONE_CONFIG:-/run/secrets/rclone.conf}"
export PGHOST="${PGHOST:-aurora-whisper-postgres}"
export PGPORT="${PGPORT:-5432}"
export PGUSER="${PGUSER:-aurora_backup}"
export PGDATABASE="${PGDATABASE:-aurora}"

inicio="$(date -u +%s)"

falhou() {
  printf 'backup: FALHOU (%s)\n' "$*" >&2
  exit 1
}

# --- a configuracao -------------------------------------------------------------
[ -n "${B2_BUCKET:-}" ] || falhou "B2_BUCKET nao esta definida"
[[ "$B2_BUCKET" =~ ^[A-Za-z0-9-]{6,63}$ ]] || falhou "B2_BUCKET nao e um nome de bucket"
[[ "$PREFIXO" =~ ^[A-Za-z0-9_-]+$ ]] || falhou "AW_PREFIXO_REMOTO tem de ser um nome simples"
{ [ -d "$PASTA" ] && [ -w "$PASTA" ]; } || falhou "nao da para escrever na pasta do backup"
[ -r "$CHAVE_PUBLICA" ] || falhou "nao consegui ler a chave publica"
if ! grep -Eqx 'age1[02-9ac-hj-np-z]{58}' "$CHAVE_PUBLICA" || [ "$(wc -l < "$CHAVE_PUBLICA")" -gt 1 ]; then
  falhou "a chave publica nao e uma chave do age (age1...)"
fi
[ -r "$RCLONE_CONFIG" ] || falhou "nao consegui ler o rclone.conf"
[ -r "$SENHA_FILE" ] || falhou "nao consegui ler o arquivo da senha"

# --- a trava ------------------------------------------------------------------
exec 9> "$PASTA/.trava"
flock -n 9 || falhou "outro backup esta rodando"

# A senha fica numa variavel que nao e exportada, e vai so para o ambiente do
# pg_dump (PGPASSWORD=... na frente dele): nunca para a linha de comando, e
# nunca para o tee, o age, o rclone ou outro programa daqui.
senha=''
IFS= read -r senha < "$SENHA_FILE" || true
[ -n "$senha" ] || falhou "o arquivo da senha esta vazio"
unset PGPASSWORD

trabalho="$(mktemp -d "$PASTA/.trabalho.XXXXXX")"
parcial=''
limpar() {
  rm -rf -- "$trabalho"
  if [ -n "$parcial" ]; then rm -f -- "$parcial"; fi
}
trap limpar EXIT

# --- o dump, conferido, cifrado ------------------------------------------------
nome="aurora-whisper-$(date -u +%Y%m%dT%H%M%SZ).dump.age"
[ ! -e "$PASTA/$nome" ] || falhou "ja existe um backup com este nome"
parcial="$PASTA/$nome.partial"

fifo="$trabalho/fluxo"
mkfifo "$fifo"
# O pg_restore --list le o comeco do fluxo (o cabecalho e o indice) e para; o
# cat drena o resto, para o tee nunca morrer de SIGPIPE no meio do dump. O
# grupo roda num subshell que herda o set -e: o if guarda o codigo do
# pg_restore sem sair dali, para o .rc ser gravado e o cat drenar mesmo quando
# ele falha (senao a causa que o log mostraria seria o tee).
{
  if pg_restore --list > "$trabalho/lista" 2> /dev/null; then rc=0; else rc=$?; fi
  printf '%s\n' "$rc" > "$trabalho/lista.rc"
  cat > /dev/null
} < "$fifo" &
pid_lista=$!

set +e
PGPASSWORD="$senha" pg_dump --format=custom --no-owner --no-privileges 2> /dev/null \
  | tee "$fifo" \
  | age --encrypt --recipients-file "$CHAVE_PUBLICA" --output "$parcial" 2> /dev/null
estados=("${PIPESTATUS[@]}")
wait "$pid_lista"
set -e

# Da ponta para o comeco: quando o age morre, o tee e o pg_dump morrem depois
# dele, de SIGPIPE (141), e a causa e o age.
[ "${estados[2]}" -eq 0 ] || falhou "o age falhou (codigo ${estados[2]}); nada foi enviado"
[ "${estados[1]}" -eq 0 ] || falhou "o tee falhou (codigo ${estados[1]}); nada foi enviado"
[ "${estados[0]}" -eq 0 ] || falhou "o pg_dump falhou (codigo ${estados[0]}); nada foi enviado"
[ "$(cat "$trabalho/lista.rc")" = 0 ] || falhou "o pg_restore --list nao leu o fluxo do dump; nada foi enviado"
grep -Eq ' TABLE public users( |$)' "$trabalho/lista" ||
  falhou "o indice do dump nao tem a tabela users (banco errado?); nada foi enviado"
grep -Eq ' TABLE public schema_migrations( |$)' "$trabalho/lista" ||
  falhou "o indice do dump nao tem a tabela schema_migrations; nada foi enviado"
[ -s "$parcial" ] || falhou "o arquivo cifrado ficou vazio; nada foi enviado"
mv -- "$parcial" "$PASTA/$nome"
parcial=''

# --- o envio ------------------------------------------------------------------
# A chave do backup nao tem readFiles, de proposito. No B2, a pergunta "o
# arquivo ja existe?" do rclone (o --immutable, ou o lsf de um arquivo so) e um
# HEAD de download, que pede readFiles: o B2 responde 401, e o primeiro backup
# da VPS falhou assim (10-out-2026). Por isso o envio vai com --no-check-dest (o
# nome leva a data, e o Object Lock guarda cada versao), e o tamanho e
# conferido pela listagem da pasta (listFiles).
enviar() { # arquivo local
  local arq="$1" nome pasta_remota tam_local tam_remoto
  nome="$(basename "$arq")"
  pasta_remota="b2:$B2_BUCKET/$PREFIXO/"
  rclone copyto --no-check-dest --retries 3 --low-level-retries 5 "$arq" "$pasta_remota$nome" > /dev/null 2>&1 || return 1
  tam_local="$(stat -c %s "$arq")"
  tam_remoto="$(rclone lsf --format sp --files-only "$pasta_remota" 2> /dev/null |
    while IFS=';' read -r tam n; do if [ "$n" = "$nome" ]; then printf '%s\n' "$tam"; fi; done)" || return 1
  [ -n "$tam_remoto" ] || return 1
  [ "$tam_remoto" = "$tam_local" ] || return 1
  touch -- "$arq.enviado"
}

enviar "$PASTA/$nome" || falhou "o envio ao B2 falhou; o cifrado ficou no volume e sobe na proxima vez"
date -u +%s > "$PASTA/.ultimo-sucesso.novo"
mv -f -- "$PASTA/.ultimo-sucesso.novo" "$PASTA/.ultimo-sucesso"

# O que ficou de vezes anteriores sem subir.
pendentes=0
for f in "$PASTA"/aurora-whisper-*.dump.age; do
  [ -e "$f" ] || continue
  [ -e "$f.enviado" ] && continue
  enviar "$f" || pendentes=$((pendentes + 1))
done

# O volume guarda so 3 dias do que ja subiu; o que nao subiu fica.
for f in "$PASTA"/aurora-whisper-*.dump.age; do
  [ -e "$f" ] && [ -e "$f.enviado" ] || continue
  if [ -n "$(find "$f" -mmin +$((DIAS_NO_VOLUME * 24 * 60)))" ]; then
    rm -f -- "$f" "$f.enviado"
  fi
done
# As sobras de uma chamada que morreu no meio (sem o trap): o .partial e a
# pasta de trabalho com mais de um dia.
for f in "$PASTA"/*.partial; do
  [ -e "$f" ] || continue
  if [ -n "$(find "$f" -mmin +1440)" ]; then rm -f -- "$f"; fi
done
for d in "$PASTA"/.trabalho.*; do
  [ -d "$d" ] && [ "$d" != "$trabalho" ] || continue
  if [ -n "$(find "$d" -maxdepth 0 -mmin +1440)" ]; then rm -rf -- "$d"; fi
done

tamanho="$(stat -c %s "$PASTA/$nome")"
duracao=$(($(date -u +%s) - inicio))
if [ "$pendentes" -gt 0 ]; then
  printf 'backup: OK (%s, %s bytes, %s s; %s antigo(s) ainda sem subir)\n' "$nome" "$tamanho" "$duracao" "$pendentes"
else
  printf 'backup: OK (%s, %s bytes, %s s)\n' "$nome" "$tamanho" "$duracao"
fi
