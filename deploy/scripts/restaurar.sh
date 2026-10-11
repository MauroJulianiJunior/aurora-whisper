#!/usr/bin/env bash
# A restauracao de um backup da nuvem (ADR-019, "Decisoes da F11", F11f). Quem
# roda e o Mauro, na VPS, com a aprovacao que o 09-security-privacy.md exige
# para operacao destrutiva. O roteiro e o docs/restauracao-f11.md, com cada
# comando pronto (a decifra no PC, o receber, o aplicar, o subir.sh e o DROP).
#
# Antes de tudo, uma sessao ssh interativa aberta na VPS, que fica aberta ate o
# fim: o dump decifrado mora na pasta em memoria SO DO USUARIO
# (/run/user/<uid>, o XDG_RUNTIME_DIR que o systemd cria na sessao), e essa
# pasta some quando a ultima sessao dele fecha. Nao e o /dev/shm, que e de
# todos e que um conteiner com --ipc=host enxerga.
#
# Sao tres comandos, porque o dump chega pela entrada do ssh e as confirmacoes
# precisam do teclado:
#
#   receber    o dump JA DECIFRADO chega pela entrada padrao (a decifra e no
#              PC, num conteiner da imagem do backup, e a chave privada nunca
#              sai de la) e vai para um arquivo em MEMORIA (nunca o disco),
#              que e conferido com "pg_restore --list" (o indice) e lido
#              inteiro (pg_restore -f /dev/null: um dump cortado no meio da
#              transferencia e recusado aqui, e nao no meio do aplicar). Nada
#              muda no banco. No PC (pelo cmd, que passa bytes sem mexer):
#                <decifra> | ssh <vps> bash ~/aurora-whisper/deploy/scripts/restaurar.sh receber
#              (o comando inteiro, com a decifra num conteiner da imagem do
#              backup, esta no passo 4 do docs/restauracao-f11.md)
#
#   aplicar    na sessao que ficou aberta, com a API e o backup PARADOS (em
#              qualquer estado vivo: de pe, reiniciando ou pausado) e sem
#              conexao nenhuma ao banco, e depois de "digite RESTAURAR":
#                1. um dump cifrado "antes-de-restaurar" do banco atual (com a
#                   chave publica; fica em ~/aurora-whisper/antes-de-restaurar);
#                2. o dump recebido restaurado num banco NOVO (pg_restore
#                   --exit-on-error --single-transaction; sem --clean, sem DROP);
#                3. no banco NOVO, pela URL dele (uma copia da database-url com
#                   o nome trocado, na mesma pasta em memoria): o migrate -status,
#                   e o migrate depois de "digite SIM" se houver migration
#                   pendente (migration em producao e ponto de parada);
#                4. no banco NOVO: pos-restauracao -confirmo, o epoch novo de
#                   todas as contas, e todas as sessoes e todos os aparelhos
#                   revogados;
#                5. so entao, por ultimo, a troca de NOMES: o atual vira
#                   aurora_antes_AAAAMMDD e o novo, ja pronto, vira aurora. O
#                   banco "aurora" nunca e uma restauracao sem o pos: qualquer
#                   saida antes daqui deixa o atual como estava;
#                6. o dump recebido sai da memoria, e o script imprime o DROP do
#                   banco antigo para daqui a 7 dias, que e do Mauro: este script
#                   NUNCA apaga banco.
#              Com --sem-copia-antes, pula o passo 1 (para quando o banco atual
#              nem consegue ser lido; o antigo continua guardado pelo nome).
#                bash ~/aurora-whisper/deploy/scripts/restaurar.sh aplicar
#
#   descartar  apaga o dump recebido da memoria, sem restaurar nada.
#
# O dump decifrado tem prazo: o aplicar recusa e apaga um dump com mais de 2 h
# (o receber apaga o que sobrou vencido), e ele some tambem quando a ultima
# sessao ssh fecha. Uma recusa do aplicar ANTES da confirmacao (a API de pe, por
# exemplo) deixa o dump na memoria, para tentar de novo dentro do prazo; depois
# da confirmacao, de certo ou de errado, ele sai da memoria ao fim do comando.
# Se a VPS tem swap, a memoria pode ir para o disco do swap: o receber avisa, e
# o risco esta registrado na ADR.
#
# Depois de restaurar: todo PC conecta de novo, e o celular entra de novo; o que
# foi apagado depois do backup pode voltar (decisao do Mauro, 09-out-2026:
# aceitar e deixar escrito); e um ditado que o celular editou antes do backup e
# o PC editou depois pode ganhar um selo. A API sobe pelo roteiro e roda a
# retencao antes de abrir a porta.
#
# Os nomes de conteiner, de rede e de imagem sao os do compose (Parte 2).
set -euo pipefail
umask 077

BASE="${AW_BASE:-$HOME/aurora-whisper}"
SEGREDOS="$BASE/segredos"
ANTES="$BASE/antes-de-restaurar"
# AW_MEMORIA e so para os testes: com ela, a pasta nao e conferida.
RAIZ_DA_MEMORIA="${AW_MEMORIA:-${XDG_RUNTIME_DIR:-/run/user/$(id -u)}}"
MEMORIA="$RAIZ_DA_MEMORIA/aurora-whisper-restauracao"
ARQUIVO_SWAPS="${AW_PROC_SWAPS:-/proc/swaps}"
IDADE_MAXIMA_MIN=120
CONTEINER_POSTGRES='aurora-whisper-postgres'
CONTEINER_API='aurora-whisper-api'
CONTEINER_BACKUP='aurora-whisper-backup'
REDE_INTERNA='aurora-whisper-interna'
IMAGEM_API="${AW_IMAGEM_API:-aurora-whisper-server:producao}"
IMAGEM_BACKUP="${AW_IMAGEM_BACKUP:-aurora-whisper-backup:producao}"
SUPERUSUARIO='postgres'
PAPEL='aurora'
PAPEL_BACKUP='aurora_backup'
BANCO='aurora'
DIAS_DO_BANCO_ANTIGO=7

falhou() {
  printf 'FALHOU: %s\n' "$*" >&2
  exit 1
}
ok() {
  printf 'OK: %s\n' "$*"
}

# De pe e respondendo: so o "running" (o Postgres, que o script usa).
rodando() { # nome do conteiner
  [ -n "$(docker ps -q --filter "name=^/?$1\$" --filter status=running)" ]
}

# Vivo em qualquer estado que volta a abrir conexao sozinho: de pe, reiniciando
# (um restart em laco) ou pausado. O docker ps sem -a e sem o filtro de status
# mostra os tres (a API e o backup, que tem de estar parados).
vivo() { # nome do conteiner
  [ -n "$(docker ps -q --filter "name=^/?$1\$")" ]
}

# Sem -i e com a entrada fechada: o docker exec -i repassaria ao conteiner o que
# vem pela entrada deste script (o teclado, as respostas).
psql_admin() { # -c "SQL"
  docker exec "$CONTEINER_POSTGRES" psql -X -A -t -q -v ON_ERROR_STOP=1 \
    -U "$SUPERUSUARIO" -d postgres "$@" < /dev/null
}

existe_banco() {
  [ "$(psql_admin -c "SELECT 1 FROM pg_database WHERE datname = '$1'")" = 1 ]
}

# O indice do dump tem de ser o do banco da aplicacao.
conferir_lista() {
  grep -Eq ' TABLE public users( |$)' "$1" && grep -Eq ' TABLE public schema_migrations( |$)' "$1"
}

# A pasta em memoria: tmpfs (nunca o disco) e so do usuario (0700, dele).
conferir_memoria() {
  if [ -n "${AW_MEMORIA:-}" ]; then return 0; fi
  [ -d "$RAIZ_DA_MEMORIA" ] ||
    falhou "nao achei a pasta em memoria so sua ($RAIZ_DA_MEMORIA): ela existe enquanto houver uma sessao sua aberta na VPS"
  case "$(stat -f -c %T "$RAIZ_DA_MEMORIA" 2> /dev/null || true)" in
    tmpfs | ramfs) ;;
    *) falhou "$RAIZ_DA_MEMORIA nao e memoria (tmpfs): o dump decifrado nao vai para o disco" ;;
  esac
  [ "$(stat -c '%u %a' "$RAIZ_DA_MEMORIA" 2> /dev/null || true)" = "$(id -u) 700" ] ||
    falhou "$RAIZ_DA_MEMORIA nao e so sua (dona voce, modo 0700): o dump decifrado nao vai para uma pasta que outros leem"
}

expirado() {
  [ -e "$MEMORIA/dump" ] && [ -n "$(find "$MEMORIA/dump" -mmin +"$IDADE_MAXIMA_MIN")" ]
}

# A API num conteiner descartavel, na rede do Postgres, so com o arquivo da
# DATABASE_URL montado: o migrate e o pos-restauracao.
api_descartavel() { # arquivo da database-url, e o comando
  local url="$1"
  shift
  docker run --rm --network "$REDE_INTERNA" --read-only --cap-drop ALL \
    --security-opt no-new-privileges \
    -v "$url:/run/secrets/database-url:ro" \
    -e DATABASE_URL_FILE=/run/secrets/database-url \
    "$IMAGEM_API" "$@"
}

# Escreve na saida a database-url com o nome do banco trocado. A senha so passa
# por variaveis e pelo printf do bash, nunca pela linha de comando. Quem grava o
# arquivo o deixa 0444 numa pasta 0700, como os de segredos/: o conteiner da
# API roda como nonroot e nao leria um 0600 do usuario.
url_do_banco() { # banco
  local url='' base consulta
  IFS= read -r url < "$SEGREDOS/database-url" || true
  base="${url%%\?*}"
  consulta="${url#"$base"}"
  [ "${base##*/}" = "$BANCO" ] || return 1
  printf '%s/%s%s\n' "${base%/*}" "$1" "$consulta"
}

# Um dump cifrado do banco atual, com a mesma conferencia do backup.sh: o
# pg_restore --list sobre o fluxo antes da cifra e o PIPESTATUS de cada etapa.
cifrar_banco_atual() { # destino
  local destino="$1" parcial="$1.partial" fifo="$MEMORIA/fluxo" pid
  local -a st
  rm -f -- "$fifo"
  mkfifo "$fifo"
  # O grupo herda o set -e: o if guarda o codigo sem sair dali, para o .rc ser
  # gravado e o cat drenar mesmo quando o pg_restore --list falha.
  {
    if docker exec -i "$CONTEINER_POSTGRES" pg_restore --list > "$MEMORIA/lista-antes" 2> /dev/null; then
      rc=0
    else
      rc=$?
    fi
    printf '%s\n' "$rc" > "$MEMORIA/lista-antes.rc"
    cat > /dev/null
  } < "$fifo" &
  pid=$!
  set +e
  docker exec "$CONTEINER_POSTGRES" pg_dump -U "$PAPEL" -d "$BANCO" \
    --format=custom --no-owner --no-privileges 2> /dev/null \
    | tee "$fifo" \
    | docker run --rm -i --network none --read-only --cap-drop ALL \
      --security-opt no-new-privileges \
      -v "$SEGREDOS/backup-chave-publica:/run/secrets/backup-chave-publica:ro" \
      "$IMAGEM_BACKUP" age --encrypt --recipients-file /run/secrets/backup-chave-publica \
      > "$parcial" 2> /dev/null
  st=("${PIPESTATUS[@]}")
  wait "$pid"
  set -e
  rm -f -- "$fifo"
  if [ "${st[0]}" -ne 0 ] || [ "${st[1]}" -ne 0 ] || [ "${st[2]}" -ne 0 ] ||
    [ "$(cat "$MEMORIA/lista-antes.rc")" != 0 ] || ! conferir_lista "$MEMORIA/lista-antes" ||
    [ ! -s "$parcial" ]; then
    rm -f -- "$parcial"
    return 1
  fi
  mv -- "$parcial" "$destino"
}

imprimir_sql() { # SQL
  printf '  docker exec -it %s psql -U %s -d postgres -c '"'"'%s'"'"'\n' \
    "$CONTEINER_POSTGRES" "$SUPERUSUARIO" "$1"
}

imprimir_drop() { # banco
  printf '  docker exec -it %s psql -U %s -d postgres -c '"'"'DROP DATABASE %s'"'"'\n' \
    "$CONTEINER_POSTGRES" "$SUPERUSUARIO" "$1"
}

# Uma parada depois de o banco novo existir: o atual nao foi tocado, e o novo
# fica para o Mauro apagar (o DROP sai impresso).
parar_com_o_novo() { # novo, e a primeira linha (FALHOU: ... ou PAROU: ...)
  local novo="$1"
  shift
  printf '%s\n' "$*" >&2
  printf 'O banco %s nao foi tocado. O restaurado ficou em %s, e nao serve para a API (faltou o\n' "$BANCO" "$novo" >&2
  printf 'que vem depois). Para recomecar, apague-o (e do Mauro, a mao) e rode receber e aplicar de novo:\n' >&2
  imprimir_drop "$novo" >&2
  exit 1
}

# --- receber --------------------------------------------------------------------
receber() {
  conferir_memoria
  if expirado; then
    rm -rf -- "$MEMORIA"
    printf 'O dump recebido antes tinha mais de %s min e saiu da memoria.\n' "$IDADE_MAXIMA_MIN"
  fi
  [ ! -e "$MEMORIA" ] ||
    falhou "ja ha um dump recebido em $MEMORIA: rode 'aplicar' ou 'descartar' antes"
  rodando "$CONTEINER_POSTGRES" || falhou "o conteiner $CONTEINER_POSTGRES nao esta de pe"
  mkdir -m 0700 -- "$MEMORIA"
  trap 'rm -rf -- "$MEMORIA"' EXIT
  cat > "$MEMORIA/dump" ||
    falhou "nao consegui guardar o dump em $RAIZ_DA_MEMORIA (cheia? ela tem um limite, em geral 10% da RAM)"
  [ -s "$MEMORIA/dump" ] || falhou "nada chegou pela entrada (o dump decifrado vem pelo ssh)"
  docker exec -i "$CONTEINER_POSTGRES" pg_restore --list < "$MEMORIA/dump" > "$MEMORIA/lista" 2> /dev/null ||
    falhou "o pg_restore --list nao leu o que chegou (o dump veio inteiro e decifrado?)"
  conferir_lista "$MEMORIA/lista" ||
    falhou "o dump nao e do banco da aplicacao (faltam as tabelas users e schema_migrations)"
  # O --list le so o indice, do comeco do arquivo; isto le e descomprime todos
  # os blocos, sem banco nenhum.
  docker exec -i "$CONTEINER_POSTGRES" pg_restore -f /dev/null < "$MEMORIA/dump" > /dev/null 2>&1 ||
    falhou "o dump veio cortado ou estragado (o pg_restore nao o leu ate o fim): mande de novo"
  trap - EXIT
  ok "dump recebido e conferido ate o fim ($(stat -c %s "$MEMORIA/dump") bytes, $(grep -Ec '^[0-9]+;' "$MEMORIA/lista") itens no indice), em memoria"
  printf 'Ele fica em %s ate o aplicar, o descartar, o fim da sua ultima sessao ssh\n' "$MEMORIA"
  printf 'na VPS, ou %s min (depois disso o aplicar o recusa e apaga).\n' "$IDADE_MAXIMA_MIN"
  if [ -r "$ARQUIVO_SWAPS" ] && [ "$(wc -l < "$ARQUIVO_SWAPS")" -gt 1 ]; then
    printf 'AVISO: a VPS tem swap ligado: a memoria onde o dump esta pode ir para o disco do swap.\n'
    printf 'Aplique ou descarte logo (o risco esta registrado na ADR-019).\n'
  fi
  printf 'Proximo passo, na sessao ssh que ficou aberta, com a API e o backup parados:\n'
  printf '  bash ~/aurora-whisper/deploy/scripts/restaurar.sh aplicar\n'
  printf 'Para desistir: restaurar.sh descartar\n'
}

# --- aplicar --------------------------------------------------------------------
aplicar() {
  local sem_copia_antes=0
  case "${1:-}" in
    '') ;;
    --sem-copia-antes) sem_copia_antes=1 ;;
    *) falhou "opcao desconhecida: $1 (a unica e --sem-copia-antes)" ;;
  esac
  if expirado; then
    rm -rf -- "$MEMORIA"
    falhou "o dump recebido tinha mais de $IDADE_MAXIMA_MIN min e saiu da memoria: rode o receber de novo"
  fi
  [ -s "$MEMORIA/dump" ] ||
    falhou "nao ha dump recebido: rode 'receber' antes (a pasta em memoria some quando a sua ultima sessao na VPS fecha)"
  # Ate a confirmacao, uma recusa deixa o dump na memoria para tentar de novo
  # ('descartar' o apaga). Depois dela, ele nunca sobrevive a este comando.
  [ -r "$SEGREDOS/database-url" ] || falhou "nao consegui ler $SEGREDOS/database-url"
  [ -r "$SEGREDOS/backup-chave-publica" ] || falhou "nao consegui ler $SEGREDOS/backup-chave-publica"
  rodando "$CONTEINER_POSTGRES" || falhou "o conteiner $CONTEINER_POSTGRES nao esta de pe"
  if vivo "$CONTEINER_API"; then
    falhou "a API ($CONTEINER_API) esta de pe (ou reiniciando, ou pausada): pare-a antes (o roteiro diz como); nada foi feito"
  fi
  if vivo "$CONTEINER_BACKUP"; then
    falhou "o backup ($CONTEINER_BACKUP) esta de pe (ou reiniciando, ou pausado): pare-o antes; nada foi feito"
  fi

  local hoje antigo novo tem_atual=0 copia_antes='' resposta='' conexoes url_novo
  hoje="$(date -u +%Y%m%d)"
  antigo="${BANCO}_antes_$hoje"
  novo="${BANCO}_restaurado_$(date -u +%Y%m%d%H%M%S)"
  if existe_banco "$antigo"; then
    falhou "ja existe o banco $antigo (outra restauracao hoje?): nada foi feito"
  fi
  if existe_banco "$novo"; then
    falhou "ja existe o banco $novo: nada foi feito"
  fi
  if existe_banco "$BANCO"; then tem_atual=1; fi
  # Alem do nome do conteiner: ninguem conectado ao banco (uma API com outro
  # nome, um servico esquecido). A conexao do proprio psql_admin e no banco
  # postgres e nao conta.
  if [ "$tem_atual" = 1 ]; then
    conexoes="$(psql_admin -c "SELECT count(*) FROM pg_stat_activity WHERE datname = '$BANCO' AND backend_type = 'client backend'")" ||
      falhou "nao consegui ler as conexoes ao banco $BANCO: nada foi feito"
    [ "$conexoes" = 0 ] ||
      falhou "ha $conexoes conexao(oes) ao banco $BANCO (a API, o backup ou outro servico ainda esta de pe?): pare-os antes; nada foi feito"
  fi
  # A URL do banco novo so e gravada depois da confirmacao; aqui, so a forma.
  url_do_banco "$novo" > /dev/null ||
    falhou "a database-url de $SEGREDOS nao termina no banco $BANCO: nada foi feito"

  printf 'Vai restaurar o dump recebido no banco %s.\n' "$BANCO"
  if [ "$tem_atual" = 1 ]; then
    printf '  - o banco atual NAO e apagado: vira %s, por %s dias;\n' "$antigo" "$DIAS_DO_BANCO_ANTIGO"
    if [ "$sem_copia_antes" = 0 ]; then
      printf '  - antes, uma copia cifrada dele vai para %s;\n' "$ANTES"
    fi
  fi
  printf '  - TODAS as sessoes e TODOS os aparelhos sao revogados: todo PC conecta de novo,\n'
  printf '    e o celular entra de novo;\n'
  printf '  - o que foi apagado depois do backup pode voltar, e pode aparecer um selo num\n'
  printf '    ditado editado nos dois lados.\n'
  printf 'Digite RESTAURAR para seguir (qualquer outra coisa para): '
  IFS= read -r resposta || true
  trap 'rm -rf -- "$MEMORIA"' EXIT
  if [ "$resposta" != RESTAURAR ]; then
    printf '\nPAROU: nada foi feito. O dump recebido saiu da memoria.\n'
    exit 1
  fi

  # 1. a copia do banco atual
  if [ "$tem_atual" = 1 ] && [ "$sem_copia_antes" = 0 ]; then
    mkdir -p -m 0700 -- "$ANTES"
    copia_antes="$ANTES/aurora-whisper-antes-de-restaurar-$(date -u +%Y%m%dT%H%M%SZ).dump.age"
    cifrar_banco_atual "$copia_antes" ||
      falhou "nao consegui a copia cifrada do banco atual: nada foi feito (se ele nem le, rode aplicar --sem-copia-antes)"
    ok "copia cifrada do banco atual em $copia_antes"
  fi

  # 2. o banco novo. Como no init do Postgres: sem CONNECT para o PUBLIC, e com
  # ele para o papel so de leitura do backup (o pg_read_all_data le, mas nao da
  # a entrada).
  psql_admin -c "CREATE DATABASE $novo OWNER $PAPEL" > /dev/null ||
    falhou "nao consegui criar o banco $novo: o banco atual nao foi tocado"
  if ! psql_admin -c "REVOKE ALL ON DATABASE $novo FROM PUBLIC" > /dev/null ||
    ! psql_admin -c "GRANT CONNECT ON DATABASE $novo TO $PAPEL_BACKUP" > /dev/null ||
    ! docker exec -i "$CONTEINER_POSTGRES" pg_restore -U "$PAPEL" -d "$novo" \
      --no-owner --no-privileges --exit-on-error --single-transaction \
      < "$MEMORIA/dump" > /dev/null 2>&1; then
    parar_com_o_novo "$novo" "FALHOU: a restauracao no banco $novo falhou"
  fi
  ok "dump restaurado no banco novo $novo"

  # 3. as migrations, no banco novo, pela URL dele (na pasta em memoria, que o
  # trap apaga no fim)
  url_novo="$MEMORIA/database-url-$novo"
  url_do_banco "$novo" > "$url_novo" ||
    parar_com_o_novo "$novo" "FALHOU: nao consegui gravar a URL do banco $novo"
  chmod 0444 "$url_novo"
  local status
  status="$(api_descartavel "$url_novo" migrate -status)" ||
    parar_com_o_novo "$novo" "FALHOU: o migrate -status falhou no banco $novo"
  printf '%s\n' "$status"
  if printf '%s\n' "$status" | grep -q '^pendente '; then
    printf 'Ha migration pendente. Digite SIM para rodar o migrate agora: '
    resposta=''
    IFS= read -r resposta || true
    if [ "$resposta" != SIM ]; then
      printf '\n'
      parar_com_o_novo "$novo" "PAROU: sem o SIM, o migrate nao rodou"
    fi
    api_descartavel "$url_novo" migrate || parar_com_o_novo "$novo" "FALHOU: o migrate falhou no banco $novo"
  fi

  # 4. o epoch novo e tudo revogado, no banco novo
  api_descartavel "$url_novo" pos-restauracao -confirmo ||
    parar_com_o_novo "$novo" "FALHOU: o pos-restauracao falhou no banco $novo"
  ok "o banco $novo esta pronto (migrations e pos-restauracao)"

  # 5. por ultimo, a troca de nomes
  if [ "$tem_atual" = 1 ]; then
    if ! psql_admin -c "ALTER DATABASE $BANCO RENAME TO $antigo" > /dev/null; then
      printf 'FALHOU: nao consegui renomear %s para %s (ha conexao aberta?). O banco atual\n' "$BANCO" "$antigo" >&2
      printf 'continua %s, e o restaurado, pronto, ficou em %s. Pare o que conecta e troque os\n' "$BANCO" "$novo" >&2
      printf 'nomes a mao, nesta ordem:\n' >&2
      imprimir_sql "ALTER DATABASE $BANCO RENAME TO $antigo" >&2
      imprimir_sql "ALTER DATABASE $novo RENAME TO $BANCO" >&2
      exit 1
    fi
  fi
  if ! psql_admin -c "ALTER DATABASE $novo RENAME TO $BANCO" > /dev/null; then
    if [ "$tem_atual" = 1 ]; then
      psql_admin -c "ALTER DATABASE $antigo RENAME TO $BANCO" > /dev/null || true
    fi
    falhou "nao consegui dar ao banco restaurado o nome $BANCO; o atual voltou ao nome dele, e o restaurado (pronto) ficou em $novo"
  fi
  if [ "$tem_atual" = 1 ]; then
    ok "o banco restaurado agora e o $BANCO; o anterior ficou como $antigo"
  else
    ok "o banco restaurado agora e o $BANCO"
  fi

  # 6. o fim
  printf '\n'
  ok "restaurado. O dump recebido saiu da memoria."
  printf 'Agora, pelo roteiro (docs/restauracao-f11.md, passo 6): suba a API (ela roda a\n'
  printf 'retencao antes de abrir a porta) e depois o backup. Todo PC conecta de novo, e o\n'
  printf 'celular entra de novo.\n'
  if [ "$tem_atual" = 1 ]; then
    printf '\nO banco anterior fica guardado como %s. Daqui a %s dias (%s), com\n' \
      "$antigo" "$DIAS_DO_BANCO_ANTIGO" "$(date -u -d "+$DIAS_DO_BANCO_ANTIGO days" +%d/%m/%Y)"
    printf 'tudo certo, apague-o (e do Mauro, a mao; este script nunca apaga banco):\n'
    imprimir_drop "$antigo"
    if [ -n "$copia_antes" ]; then
      printf '  rm -f %s\n' "$copia_antes"
    fi
  fi
}

descartar() {
  if [ -e "$MEMORIA" ]; then
    rm -rf -- "$MEMORIA"
    ok "o dump recebido saiu da memoria"
  else
    ok "nao havia dump recebido"
  fi
}

case "${1:-}" in
  receber) receber ;;
  aplicar) shift; aplicar "$@" ;;
  descartar) descartar ;;
  *)
    printf 'uso: restaurar.sh receber | aplicar [--sem-copia-antes] | descartar\n' >&2
    exit 2
    ;;
esac
