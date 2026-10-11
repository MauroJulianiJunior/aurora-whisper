#!/usr/bin/env bash
# Confere a application key do B2 ANTES de o backup entrar em producao
# (ADR-019, "Decisoes da F11", D2, e a revisao da F11). Quem roda e o Mauro, na
# VPS, depois do gerar-segredos.sh e do docker load da imagem do backup, e de
# novo a cada chave nova:
#
#   bash ~/aurora-whisper/deploy/scripts/conferir-b2.sh
#
# Num conteiner descartavel da imagem do backup (o rclone dela), com o
# rclone.conf de segredos/ montado so para leitura:
#   1. sobe um arquivo pequeno de conferencia,
#      aurora-whisper/conferencia-da-chave-<data e hora>.txt: tem de passar
#      (writeFiles);
#   2. le o tamanho dele: tem de bater (listFiles; o backup.sh confere assim o
#      que subiu, e o copyto --immutable lista o destino antes de subir);
#   3. tenta APAGAR DE VEZ esse arquivo (deletefile --b2-hard-delete): tem de
#      FALHAR, porque a chave nao tem deleteFiles (ou porque o Object Lock o
#      segura);
#   4. confere que ele continua la.
# O arquivo de conferencia fica no bucket (esta chave nao o apaga); a regra de
# ciclo de vida o leva junto com os backups. E o unico arquivo que este script
# tenta apagar, e so para provar que nao consegue.
#
# O que isto NAO prova, e o Mauro confere no painel do B2 (o script lembra no
# fim): a writeFiles, que o backup precisa, tambem deixa ESCONDER um arquivo ou
# subir outra versao com o mesmo nome, e a regra de ciclo de vida apaga de vez
# o que foi escondido. Quem segura os backups contra quem invadir a VPS e o
# Object Lock do bucket, com retencao padrao de 30 dias ou mais; sem ele, a
# chave apaga, so que com um dia de atraso.
#
# Nenhum segredo na tela nem na linha de comando: o rclone le a chave do
# arquivo montado.
set -euo pipefail
umask 077

BASE="${AW_BASE:-$HOME/aurora-whisper}"
SEGREDOS="$BASE/segredos"
CONFIG="$BASE/config.env"
IMAGEM_BACKUP="${AW_IMAGEM_BACKUP:-aurora-whisper-backup:producao}"
PREFIXO='aurora-whisper'

falhou() {
  printf 'FALHOU: %s\n' "$*" >&2
  exit 1
}
ok() {
  printf 'OK: %s\n' "$*"
}

[ -r "$SEGREDOS/rclone.conf" ] || falhou "nao consegui ler $SEGREDOS/rclone.conf (rode antes o gerar-segredos.sh)"
[ -r "$CONFIG" ] || falhou "nao consegui ler $CONFIG (rode antes o gerar-segredos.sh)"
bucket="$(sed -n 's/^B2_BUCKET=//p' "$CONFIG")"
[[ "$bucket" =~ ^[A-Za-z0-9-]{6,63}$ ]] || falhou "o B2_BUCKET de $CONFIG nao e um nome de bucket"

# O rclone da imagem do backup, num conteiner descartavel, sem capacidades, so
# com o rclone.conf montado (so leitura). Poucas tentativas: uma recusa do B2 e
# a resposta que se espera no passo 3.
rclone_b2() { # comando do rclone e argumentos
  docker run --rm -i --cap-drop ALL --security-opt no-new-privileges \
    -v "$SEGREDOS/rclone.conf:/run/secrets/rclone.conf:ro" \
    -e RCLONE_CONFIG=/run/secrets/rclone.conf \
    -e RCLONE_RETRIES=1 -e RCLONE_LOW_LEVEL_RETRIES=2 \
    "$IMAGEM_BACKUP" rclone "$@"
}

nome="conferencia-da-chave-$(date -u +%Y%m%dT%H%M%SZ).txt"
pasta_remota="b2:$bucket/$PREFIXO/"
remoto="$pasta_remota$nome"

# O tamanho do arquivo de conferencia pela listagem da pasta (listFiles). O lsf
# de um arquivo so seria um HEAD de download, que pede readFiles, e a chave do
# backup nao a tem (o B2 responde 401; ver o backup.sh).
tamanho_na_pasta() {
  rclone_b2 lsf --format sp --files-only "$pasta_remota" < /dev/null 2> /dev/null |
    while IFS=';' read -r tam n; do if [ "$n" = "$nome" ]; then printf '%s\n' "$tam"; fi; done
}
conteudo='conferencia da application key do backup (deploy/scripts/conferir-b2.sh); pode ficar aqui'

# 1. sobe
printf '%s\n' "$conteudo" | rclone_b2 rcat "$remoto" > /dev/null 2>&1 ||
  falhou "a chave nao subiu um arquivo ao bucket $bucket (ela e deste bucket? tem writeFiles?). Nada foi apagado"
ok "a chave sobe arquivos ao bucket $bucket ($PREFIXO/$nome)"

# 2. lista
tamanho="$(tamanho_na_pasta)" ||
  falhou "a chave nao lista o bucket (falta listFiles): o backup.sh confere o que subiu listando"
[ -n "$tamanho" ] || falhou "a chave nao achou na listagem o arquivo que subiu (falta listFiles?)"
[ "$tamanho" = "$((${#conteudo} + 1))" ] || falhou "o tamanho do arquivo de conferencia no B2 nao bate ($tamanho)"
ok "a chave lista o que subiu"

# 3. nao apaga de vez
if rclone_b2 deletefile --b2-hard-delete "$remoto" < /dev/null > /dev/null 2>&1; then
  falhou "a chave APAGOU DE VEZ o arquivo de conferencia: ela tem deleteFiles e o bucket nao tem Object Lock, e quem invadir a VPS apaga os backups. Crie outra chave so com listBuckets, listFiles e writeFiles, ligue o Object Lock e rode de novo (deploy/README.md)"
fi

# 4. e o arquivo continua la
[ -n "$(tamanho_na_pasta)" ] ||
  falhou "o arquivo de conferencia sumiu, mas o rclone disse que nao apagou: confira no painel do B2 antes de seguir"
ok "a chave nao apaga de vez o que subiu"

printf '\nFalta o que o rclone nao ve com esta chave. No painel do B2, confira:\n'
printf '  - o bucket %s privado, com Object Lock ligado e retencao padrao de 30 dias ou\n' "$bucket"
printf '    mais (e o que segura os backups: a writeFiles tambem esconde arquivos);\n'
printf '  - a regra de ciclo de vida do bucket (esconde aos 30 dias; apaga o escondido depois);\n'
printf '  - a application key so deste bucket, so com listBuckets, listFiles e writeFiles\n'
printf '    (sem deleteFiles, bypassGovernance, writeFileRetentions nem writeBucketRetentions).\n'
