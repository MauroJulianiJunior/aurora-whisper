#!/usr/bin/env bash
# O agendador do backup da nuvem (ADR-019, "Decisoes da F11", escolha menor 8:
# o agendador e um conteiner do proprio compose, nada de cron no host). Roda
# DENTRO do conteiner aurora-whisper-backup (a imagem de
# deploy/backup/Dockerfile), como o comando dele no deploy/compose.yml:
#
#   agendar-backup.sh          espera ate a hora do backup (AW_HORA_DO_BACKUP,
#                              em UTC; padrao 06, que e 03:00 em Sao Paulo),
#                              roda o backup.sh e volta a esperar, todo dia.
#                              Uma falha do backup.sh ja sai no log dele (uma
#                              linha "backup: FALHOU"); o agendador segue, e o
#                              healthcheck abaixo e quem acusa.
#   agendar-backup.sh saude    o healthcheck do conteiner: sai com 0 se o
#                              ultimo backup que SUBIU ao B2 (a hora que o
#                              backup.sh grava em .ultimo-sucesso, so depois
#                              do envio conferido) tem no maximo 26 h, e com 1
#                              se tem mais, se ainda nao houve nenhum, ou se o
#                              arquivo nao e uma hora. No compose comum o
#                              unhealthy nao reinicia nada: aparece no docker ps.
#
# O primeiro backup e a mao, pelo roteiro (docs/deploy-f11.md), depois do
# conferir-b2.sh: o agendador nao roda nada ao subir (um reinicio nao vira um
# backup a mais).
#
# Sem date -d (a imagem e alpine, com o date do busybox): a espera e conta de
# segundos desde a meia-noite UTC.
set -euo pipefail
umask 077

PASTA="${AW_PASTA_BACKUP:-/backup}"
HORA="${AW_HORA_DO_BACKUP:-06}"
MAX_HORAS=26

falhou() {
  printf 'agendador: FALHOU (%s)\n' "$*" >&2
  exit 1
}

[[ "$HORA" =~ ^([01][0-9]|2[0-3])$ ]] || falhou "AW_HORA_DO_BACKUP tem de ser uma hora de 00 a 23, com dois digitos"

saude() {
  local ultimo='' agora idade
  if [ ! -r "$PASTA/.ultimo-sucesso" ]; then
    printf 'backup: nenhum backup subiu ao B2 ainda (rode o primeiro pelo roteiro)\n' >&2
    exit 1
  fi
  IFS= read -r ultimo < "$PASTA/.ultimo-sucesso" || true
  if ! [[ "$ultimo" =~ ^[0-9]+$ ]]; then
    printf 'backup: a hora do ultimo sucesso nao e uma hora\n' >&2
    exit 1
  fi
  agora="$(date -u +%s)"
  idade=$((agora - ultimo))
  if [ "$idade" -gt $((MAX_HORAS * 3600)) ]; then
    printf 'backup: o ultimo que subiu ao B2 e de %s h atras (mais de %s h)\n' "$((idade / 3600))" "$MAX_HORAS" >&2
    exit 1
  fi
  printf 'backup: o ultimo que subiu ao B2 e de %s h atras\n' "$((idade / 3600))"
  exit 0
}

# Os segundos ate a proxima HORA:00 UTC (nunca zero: na hora exata, o dia
# seguinte).
segundos_ate_a_hora() {
  local agora no_dia alvo espera
  agora="$(date -u +%s)"
  no_dia=$((agora % 86400))
  alvo=$((10#$HORA * 3600))
  espera=$(((alvo - no_dia + 86400) % 86400))
  if [ "$espera" -eq 0 ]; then espera=86400; fi
  printf '%s\n' "$espera"
}

agendar() {
  # O docker stop manda SIGTERM ao PID 1 (este bash): o "sleep & wait" deixa o
  # trap rodar na hora, sem esperar o fim do sono.
  trap 'printf "agendador: parado\n"; exit 0' TERM INT
  printf 'agendador: um backup por dia, as %s:00 UTC\n' "$HORA"
  while :; do
    sleep "$(segundos_ate_a_hora)" &
    wait "$!"
    backup.sh || true
  done
}

case "${1:-}" in
  '') agendar ;;
  saude) saude ;;
  *)
    printf 'uso: agendar-backup.sh [saude]\n' >&2
    exit 1
    ;;
esac
