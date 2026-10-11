#!/usr/bin/env bash
# Gera os segredos e a configuracao da nuvem na VPS (ADR-019, "Decisoes da
# F11", F11f). Quem roda e o Mauro, UMA vez, na VPS, como o usuario dele (sem
# sudo):
#
#   bash ~/aurora-whisper/deploy/scripts/gerar-segredos.sh
#
# O que ele grava (nada disso vai para o repositorio, e nenhum valor aparece na
# tela):
#
#   ~/aurora-whisper/segredos/          pasta 0700, arquivos 0444
#     postgres-superusuario-senha       o superusuario do Postgres (POSTGRES_PASSWORD_FILE),
#                                       so para administrar
#     postgres-aurora-senha             o papel da aplicacao (o init do Postgres)
#     postgres-backup-senha             o papel so de leitura do backup, aurora_backup
#                                       (o init do Postgres e o conteiner do backup)
#     database-url                      DATABASE_URL_FILE da API e do migrate, com o papel
#                                       aurora e a senha acima
#     session-secret                    SESSION_SECRET_FILE
#     device-token-pepper               DEVICE_TOKEN_PEPPER_FILE
#     ai-keys-secret                    AI_KEYS_SECRET_FILE: a chave de cifra das chaves da
#                                       IA que as pessoas colam no app do celular (ADR-021).
#                                       Numa VPS que ja esta no ar, quem a cria e o
#                                       criar-chave-de-cifra.sh (este script recusa rodar)
#     backup-chave-publica              a chave PUBLICA do age (o servidor so cifra)
#     rclone.conf                       a application key do B2 (o remoto "b2"), restrita
#                                       ao bucket, so com listBuckets, listFiles e
#                                       writeFiles (o conferir-b2.sh a confere)
#     google-client.json                GOOGLE_CLIENT_SECRET_FILE: o JSON do client Web
#                                       do Google, que o scripts\enviar-para-servidor.ps1
#                                       mandou do PC para ~/aurora-whisper/recebido/ e que
#                                       este script confere (o redirect de producao,
#                                       https://<dominio>/auth/google/callback) e MOVE
#                                       para ca, sem mostrar nada dele
#   ~/aurora-whisper/config.env         0600: PUBLIC_BASE_URL, DOMINIO, CONTAS_PERMITIDAS
#                                       e B2_BUCKET; nao sao segredo, mas tambem nao vao
#                                       para o repositorio (o subir.sh acrescenta o
#                                       PROXY_CONFIAVEL)
#
# Os valores aleatorios vem de /dev/urandom, 32 bytes cada, em hexadecimal (64
# caracteres): servem como variavel, como arquivo e dentro da URL do banco sem
# escape nenhum.
#
# Por que 0444 dentro de uma pasta 0700: a imagem da API roda como nonroot (uid
# 65532) e a do backup como postgres (uid 70), e o compose monta cada arquivo
# no conteiner com o dono e o modo do host. Um 0600 do usuario nao seria lido
# ("nao consegui abrir"). Quem fecha o acesso dos outros usuarios da VPS e a
# pasta 0700. O Docker Desktop do PC mostra todo arquivo aberto, entao esse erro
# nao aparece no teste do PC.
#
# Recusa sobrescrever: se a pasta segredos/ ou o config.env ja existem, para
# sem mudar nada (um segredo novo desconectaria todos os PCs ou trancaria o
# banco). Recusa tambem uma pasta base dentro do deploy/ ou de um repositorio
# git. A pasta base e ~/aurora-whisper, ou a de AW_BASE.
#
# Pergunta o dominio, o e-mail que pode entrar, o bucket do B2, a keyID e a
# application key do B2 e a chave publica do age (a linha age1... que o
# scripts\backup-chave.ps1 mostrou no PC). Confere tudo antes de gravar
# qualquer coisa.
#
# A keyID, a application key e a chave publica sao lidas SEM eco: o erro
# previsto e colar a coisa errada (a application key no lugar da keyID, ou o
# arquivo .key inteiro, com a chave PRIVADA, no lugar da publica), e a recusa
# viria tarde se o que foi colado ja tivesse aparecido na tela. A publica,
# que nao e segredo, aparece depois de conferida. E toda recusa descarta o que
# sobrou na entrada: colado em varias linhas, o resto iria para o bash da VPS
# depois que o script saisse, e seria executado e gravado no historico dele.
set -euo pipefail
umask 077

BASE="${AW_BASE:-$HOME/aurora-whisper}"
HOST_DO_POSTGRES='aurora-whisper-postgres'
PAPEL='aurora'
BANCO='aurora'
# O JSON do Google tem menos de 1 KiB; o servidor recusa acima de 64 KiB.
MAX_JSON_DO_GOOGLE=65536

# Le e joga fora o que ainda esta na entrada (sem eco), ate 0,2 s sem nada
# chegar. O -n le tambem uma ultima linha sem o fim de linha.
descartar_entrada() {
  while IFS= read -r -s -t 0.2 -n 4096 _; do :; done 2> /dev/null || true
}

falhou() {
  descartar_entrada
  printf 'FALHOU: %s\n' "$*" >&2
  exit 1
}
ok() {
  printf 'OK: %s\n' "$*"
}

# --- 1. onde gravar -----------------------------------------------------------
AQUI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
DEPLOY="$(dirname "$AQUI")"
BASE="$(realpath -m -- "$BASE")"
case "$BASE/" in
  "$DEPLOY/"*) falhou "a pasta base ($BASE) nao pode ficar dentro de $DEPLOY" ;;
esac
d="$BASE"
while [ -n "$d" ] && [ "$d" != "/" ] && [ "$d" != "." ]; do
  if [ -e "$d/.git" ]; then
    falhou "a pasta base ($BASE) esta dentro de um repositorio git ($d): os segredos nunca moram num repositorio"
  fi
  d="$(dirname "$d")"
done
SEGREDOS="$BASE/segredos"
CONFIG="$BASE/config.env"
RECEBIDO="$BASE/recebido/google-client.json"
if [ -e "$SEGREDOS" ] || [ -e "$CONFIG" ]; then
  falhou "ja existe $SEGREDOS ou $CONFIG: nada foi mudado (um segredo novo desconecta todos os PCs)"
fi
# O JSON do Google vem antes, do PC (o enviar-para-servidor.ps1). O conteudo
# so e conferido depois do dominio, e nunca aparece.
if [ ! -f "$RECEBIDO" ] || [ ! -r "$RECEBIDO" ]; then
  falhou "nao achei o JSON do client do Google em $RECEBIDO: mande-o antes, do PC, pelo scripts\\enviar-para-servidor.ps1 (o roteiro diz como)"
fi
if [ "$(wc -c < "$RECEBIDO")" -gt "$MAX_JSON_DO_GOOGLE" ]; then
  falhou "o arquivo $RECEBIDO e grande demais para ser o JSON do client do Google"
fi

# --- 2. o que so o Mauro sabe ---------------------------------------------------
perguntar() { # variavel pergunta
  local resposta=''
  IFS= read -r -p "$2: " resposta || true
  printf -v "$1" '%s' "$resposta"
}
perguntar_sem_eco() { # variavel pergunta
  local resposta=''
  IFS= read -r -s -p "$2 (nao aparece na tela; cole e tecle Enter): " resposta || true
  if [ -t 0 ]; then printf '\n' >&2; fi
  printf -v "$1" '%s' "$resposta"
}

perguntar dominio 'O endereco da nuvem, sem https:// (ex.: aurora.exemplo.com.br)'
[[ "$dominio" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$ ]] ||
  falhou "o endereco nao e um nome de dominio em minusculas (sem https:// e sem barra)"
# O client Web do Google com o redirect de producao. O servidor confere de
# novo na subida (e recusa subir sem ele); aqui, a recusa vem antes de gerar
# qualquer coisa. O grep -q nao imprime nada do arquivo.
if grep -Fq '"installed"' "$RECEBIDO" || ! grep -Fq '"web"' "$RECEBIDO"; then
  falhou "o $RECEBIDO nao e o JSON de um client do tipo Aplicativo da Web do Google"
fi
grep -Fq "\"https://$dominio/auth/google/callback\"" "$RECEBIDO" ||
  falhou "o client do Google em $RECEBIDO nao tem o redirect https://$dominio/auth/google/callback: acrescente-o no Google Cloud, baixe o JSON de novo e mande pelo enviar-para-servidor.ps1"
perguntar email 'O e-mail que pode entrar (o da conta Google)'
[[ "$email" =~ ^[^@[:space:],]+@[^@[:space:],]+\.[^@[:space:],]+$ ]] ||
  falhou "o e-mail nao parece um e-mail (um so, sem espaco nem virgula)"
perguntar bucket 'O nome do bucket do B2'
[[ "$bucket" =~ ^[A-Za-z0-9-]{6,63}$ ]] ||
  falhou "o nome do bucket do B2 tem de ter de 6 a 63 letras, numeros ou hifens"
# A keyID de uma application key tem 25 hexadecimais; a de 12 e a da master
# key (a conta inteira, que apaga tudo). A application key comeca com K e tres
# digitos: e o que pega uma trocada pela outra.
perguntar_sem_eco keyid 'A keyID da application key do B2 (25 letras e numeros)'
if [[ "$keyid" =~ ^[0-9a-f]{12}$ ]]; then
  falhou "essa e a keyID da master key do B2, que alcanca a conta inteira e apaga: crie uma application key so do bucket (deploy/README.md)"
fi
[[ "$keyid" =~ ^[0-9a-f]{25}$ ]] ||
  falhou "a keyID do B2 tem 25 hexadecimais (0-9 e a-f); o que foi colado nao foi mostrado nem gravado (a application key, que comeca com K, vem na pergunta seguinte)"
perguntar_sem_eco appkey 'A application key do B2 (comeca com K)'
[[ "$appkey" =~ ^K[0-9]{3}[A-Za-z0-9+/=_-]{20,60}$ ]] ||
  falhou "a application key do B2 comeca com K e tres digitos; o que foi colado nao foi mostrado nem gravado"
perguntar_sem_eco chave_publica 'A chave PUBLICA do backup (a linha age1... do scripts\backup-chave.ps1)'
if [[ "$chave_publica" == AGE-SECRET-KEY-* ]] || [[ "$chave_publica" == '#'* ]]; then
  falhou "isso e a chave PRIVADA (ou o arquivo .key inteiro): ela nao foi mostrada nem gravada, e o resto do que foi colado foi descartado. Cole so a linha age1... (a PRIVADA nunca vem para a VPS)"
fi
[[ "$chave_publica" =~ ^age1[02-9ac-hj-np-z]{58}$ ]] ||
  falhou "a chave publica nao e uma chave do age (age1 e 58 caracteres); a chave PRIVADA nunca vem para ca"
ok "a chave publica do backup: $chave_publica"

# --- 3. os aleatorios -----------------------------------------------------------
aleatorio() {
  local v
  v="$(od -An -vtx1 -N32 /dev/urandom | tr -d ' \n')"
  [ "${#v}" -eq 64 ] || return 1
  printf '%s' "$v"
}
senha_superusuario="$(aleatorio)" || falhou "nao consegui ler /dev/urandom"
senha_aurora="$(aleatorio)" || falhou "nao consegui ler /dev/urandom"
senha_backup="$(aleatorio)" || falhou "nao consegui ler /dev/urandom"
session_secret="$(aleatorio)" || falhou "nao consegui ler /dev/urandom"
pepper="$(aleatorio)" || falhou "nao consegui ler /dev/urandom"
chave_de_cifra="$(aleatorio)" || falhou "nao consegui ler /dev/urandom"
if [ "$session_secret" = "$pepper" ] || [ "$senha_superusuario" = "$senha_aurora" ]; then
  falhou "dois aleatorios iguais: rode de novo"
fi
if [ "$senha_backup" = "$senha_aurora" ] || [ "$senha_backup" = "$senha_superusuario" ]; then
  falhou "dois aleatorios iguais: rode de novo"
fi
if [ "$chave_de_cifra" = "$session_secret" ] || [ "$chave_de_cifra" = "$pepper" ]; then
  falhou "dois aleatorios iguais: rode de novo"
fi

# --- 4. gravar: tudo numa pasta temporaria, e so no fim o nome de verdade -------
mkdir -p -- "$BASE"
chmod 0700 -- "$BASE"
tmp="$(mktemp -d "$BASE/.segredos-novos.XXXXXX")"
tmpconfig="$BASE/.config.env.novo.$$"
limpar() {
  rm -rf -- "$tmp"
  rm -f -- "$tmpconfig"
}
trap limpar EXIT

gravar() { # arquivo valor
  printf '%s\n' "$2" > "$tmp/$1"
}
gravar postgres-superusuario-senha "$senha_superusuario"
gravar postgres-aurora-senha "$senha_aurora"
gravar postgres-backup-senha "$senha_backup"
gravar database-url "postgres://$PAPEL:$senha_aurora@$HOST_DO_POSTGRES:5432/$BANCO?sslmode=disable"
gravar session-secret "$session_secret"
gravar device-token-pepper "$pepper"
gravar ai-keys-secret "$chave_de_cifra"
gravar backup-chave-publica "$chave_publica"
{
  printf '[b2]\n'
  printf 'type = b2\n'
  printf 'account = %s\n' "$keyid"
  printf 'key = %s\n' "$appkey"
} > "$tmp/rclone.conf"
cp -- "$RECEBIDO" "$tmp/google-client.json"
chmod 0444 "$tmp"/*
chmod 0700 "$tmp"

{
  printf '# Gerado por deploy/scripts/gerar-segredos.sh em %s.\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '# Nao e segredo, mas nao vai para o repositorio.\n'
  printf 'PUBLIC_BASE_URL=https://%s\n' "$dominio"
  printf 'DOMINIO=%s\n' "$dominio"
  printf 'CONTAS_PERMITIDAS=%s\n' "$email"
  printf 'B2_BUCKET=%s\n' "$bucket"
} > "$tmpconfig"
chmod 0600 "$tmpconfig"

mv -T -- "$tmp" "$SEGREDOS"
mv -T -- "$tmpconfig" "$CONFIG"
trap - EXIT
# O JSON recebido ja esta em segredos/: a copia de entrada sai.
rm -f -- "$RECEBIDO"
rmdir -- "$BASE/recebido" 2> /dev/null || true

ok "segredos em $SEGREDOS (pasta 0700; arquivos 0444, para os conteineres lerem)"
ok "configuracao em $CONFIG (0600)"
printf '\n'
printf 'Nenhum valor foi mostrado. Duas copias que so voce guarda, no gerenciador de senhas:\n'
printf '  - o DEVICE_TOKEN_PEPPER: perde-lo desconecta todos os PCs. Para ve-lo, so no seu\n'
printf '    terminal (nunca no chat): cat %s/device-token-pepper\n' "$SEGREDOS"
printf '  - a chave PRIVADA do backup, que ficou no PC (o scripts\\backup-chave.ps1 disse onde).\n'
printf 'A chave de cifra das chaves da IA (ai-keys-secret) nao tem copia fora desta VPS: perde-la\n'
printf 'faz cada pessoa colar a chave da IA de novo no app do celular (nenhum ditado se perde).\n'
