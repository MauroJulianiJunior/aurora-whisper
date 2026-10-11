#!/usr/bin/env bash
# Cria SO o arquivo da chave de cifra das chaves da IA do celular (ADR-021,
# decisao O e C3a) numa VPS que ja esta no ar. Quem roda e o Mauro, na VPS,
# como o usuario dele (sem sudo), UMA vez, pelo roteiro do deploy:
#
#   bash ~/aurora-whisper/deploy/scripts/criar-chave-de-cifra.sh
#
# Por que um script a parte: o gerar-segredos.sh recusa rodar quando a pasta
# segredos/ ja existe (um segredo novo desconectaria todos os PCs). Este so
# ACRESCENTA um arquivo, e nunca toca nos outros.
#
# O que ele grava (nada disso vai para o repositorio, e o valor nunca aparece
# na tela):
#
#   ~/aurora-whisper/segredos/ai-keys-secret    0444, na pasta 0700: 32 bytes
#                                                de /dev/urandom em hexadecimal
#                                                (64 caracteres), o
#                                                AI_KEYS_SECRET_FILE da API
#
# A chave de cifra cifra as chaves de API que as pessoas colam no app do
# celular (a tabela ai_provider_keys). Ela nunca entra no banco nem no backup,
# e nao tem copia fora da VPS (ADR-021, escolha menor 11): perder o arquivo (ou
# a VPS) faz cada pessoa colar a chave de novo no app; nenhum ditado se perde.
#
# Recusa sobrescrever: se o arquivo ja existe, para sem mudar nada (trocar a
# chave de cifra leva toda chave guardada a "cole de novo"). Recusa tambem sem
# a pasta segredos/ (numa VPS nova, rode antes o gerar-segredos.sh) e com a
# pasta base dentro do deploy/ ou de um repositorio git. A pasta base e
# ~/aurora-whisper, ou a de AW_BASE.
#
# Por que 0444 numa pasta 0700: a imagem da API roda como nonroot (uid 65532),
# e o compose monta cada arquivo de segredo com o dono e o modo do host (o
# mesmo motivo do gerar-segredos.sh). Quem fecha o acesso dos outros usuarios
# da VPS e a pasta.
#
# A gravacao nunca e por cima: com o noclobber, o bash cria o arquivo vazio com
# O_EXCL (se ele apareceu entre a conferencia e a criacao, para sem mudar
# nada), e so depois escreve nele. Uma falha no meio apaga o que este script
# criou, e so isso.
set -euo pipefail
umask 077

BASE="${AW_BASE:-$HOME/aurora-whisper}"
NOME='ai-keys-secret'

falhou() {
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
ARQUIVO="$SEGREDOS/$NOME"
if [ -L "$SEGREDOS" ] || [ ! -d "$SEGREDOS" ]; then
  falhou "nao achei a pasta $SEGREDOS: numa VPS nova, rode antes o gerar-segredos.sh (nada foi criado)"
fi
if [ -e "$ARQUIVO" ] || [ -L "$ARQUIVO" ]; then
  falhou "ja existe $ARQUIVO: nada foi mudado (uma chave de cifra nova faz cada pessoa colar a chave da IA de novo)"
fi

# --- 2. o aleatorio -------------------------------------------------------------
aleatorio() {
  local v
  v="$(od -An -vtx1 -N32 /dev/urandom | tr -d ' \n')"
  [ "${#v}" -eq 64 ] || return 1
  printf '%s' "$v"
}
chave="$(aleatorio)" || falhou "nao consegui ler /dev/urandom"

# --- 3. gravar: o arquivo novo (O_EXCL), o valor e so entao o modo --------------
chmod 0700 -- "$SEGREDOS"
criado=0
limpar() {
  if [ "$criado" -eq 1 ]; then
    rm -f -- "$ARQUIVO"
  fi
}
trap limpar EXIT
set -o noclobber
if ! { : > "$ARQUIVO"; } 2> /dev/null; then
  falhou "nao consegui criar $ARQUIVO sem sobrescrever (ele apareceu agora?): nada foi mudado"
fi
criado=1
printf '%s\n' "$chave" >| "$ARQUIVO"
chave=''
chmod 0444 -- "$ARQUIVO"
criado=0
trap - EXIT

ok "chave de cifra em $ARQUIVO (0444, na pasta 0700, para o conteiner da API ler)"
printf '\n'
printf 'Nenhum valor foi mostrado, e nao ha copia fora desta VPS (ADR-021, escolha menor 11):\n'
printf 'perder este arquivo faz cada pessoa colar a chave da IA de novo no app do celular.\n'
printf 'Nao o troque nem rode este script de novo para conferir: ele recusa, e uma chave de\n'
printf 'cifra nova tem o mesmo efeito de perder o arquivo.\n'
