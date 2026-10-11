#!/usr/bin/env bash
# diagnostico-servidor.sh -- Aurora Whisper, etapa 5 (docs/adr/ADR-019-nuvem-e-celular.md).
#
# SO LEITURA. Este script:
#   - nao escreve nada em disco (nem arquivo temporario);
#   - nao acessa a internet nem chama servico externo;
#   - nao instala, nao para, nao reinicia e nao configura nada;
#   - nao usa sudo: se faltar permissao para alguma coisa, ele diz e segue;
#   - nao mostra variaveis de ambiente, labels, "docker inspect" nem logs de conteiner;
#   - troca enderecos IP especificos por <ip> (0.0.0.0, 127.0.0.1, [::] e [::1] ficam,
#     porque dizem o alcance, nao o endereco).
#
# Mostra: sistema e kernel, CPU, memoria e swap livres, disco livre, versao do Docker e do
# Compose, conteineres rodando (so nome, imagem, portas e status), memoria em uso por
# conteiner, quem escuta nas portas 80 e 443, qual proxy parece estar na frente, e os nomes
# das redes do Docker.
#
# Como rodar, sem copiar o arquivo para o servidor (o script vai pela entrada do ssh):
#   Git Bash, no PC:
#     ssh usuario@servidor 'bash -s' < /c/Desenvolvimento/aurora-whisper/scripts/diagnostico-servidor.sh
#   PowerShell, no PC:
#     cmd /c "ssh usuario@servidor bash -s < C:\Desenvolvimento\aurora-whisper\scripts\diagnostico-servidor.sh"
# Troque usuario@servidor pelo seu acesso de sempre. Depois, cole a saida inteira na conversa.
# Ver scripts/README.md.

set -u

# Saida previsivel (nomes de coluna e numeros), qualquer que seja o idioma do servidor.
LC_ALL=C
LANG=C
export LC_ALL LANG

tem() { command -v "$1" >/dev/null 2>&1; }

secao() { printf '\n== %s ==\n' "$1"; }

# Le da entrada padrao e troca IP especifico por <ip>.
mascarar_ip() {
  sed -E \
    -e 's/\[::1\]/__L6__/g' \
    -e 's/\[::\]/__T6__/g' \
    -e 's/\[[0-9A-Za-z:.%]+\]/[<ip>]/g' \
    -e 's/(^|[^0-9.])0\.0\.0\.0/\1__T4__/g' \
    -e 's/(^|[^0-9.])127(\.[0-9]{1,3}){3}/\1__L4__/g' \
    -e 's/([0-9]{1,3}\.){3}[0-9]{1,3}/<ip>/g' \
    -e 's/__L6__/[::1]/g' \
    -e 's/__T6__/[::]/g' \
    -e 's/__T4__/0.0.0.0/g' \
    -e 's/__L4__/127.0.0.1/g'
}

# Recua cada linha em dois espacos.
recuar() { sed -e 's/^/  /'; }

echo "Aurora Whisper -- diagnostico do servidor (so leitura; nada foi alterado)"
if tem date; then
  echo "Data (UTC): $(date -u '+%Y-%m-%d %H:%M')"
fi
if [ "$(id -u 2>/dev/null || echo x)" = "0" ]; then
  echo "Rodando como root: sim"
else
  echo "Rodando como root: nao (algumas informacoes podem faltar; o script diz quais)"
fi

# ---------------------------------------------------------------------------------------------
secao "Sistema"
if [ -r /etc/os-release ]; then
  nome_do_sistema=$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release | tr -d '"')
  echo "Distribuicao: ${nome_do_sistema:-desconhecida}"
else
  echo "Distribuicao: /etc/os-release nao existe ou nao pode ser lido"
fi
if tem uname; then
  echo "Kernel: $(uname -srm 2>/dev/null)"
fi
if tem systemd-detect-virt; then
  echo "Virtualizacao: $(systemd-detect-virt 2>/dev/null || echo nenhuma)"
fi
if tem uptime; then
  echo "Ligado ha: $(uptime -p 2>/dev/null || uptime 2>/dev/null)"
fi

# ---------------------------------------------------------------------------------------------
secao "CPU"
if tem nproc; then
  echo "Nucleos logicos: $(nproc 2>/dev/null)"
fi
if [ -r /proc/cpuinfo ]; then
  modelo=$(sed -n 's/^model name[[:space:]]*:[[:space:]]*//p' /proc/cpuinfo | head -n 1)
  echo "Modelo: ${modelo:-desconhecido}"
fi
if [ -r /proc/loadavg ]; then
  echo "Carga (1, 5 e 15 min): $(cut -d ' ' -f 1-3 /proc/loadavg)"
fi

# ---------------------------------------------------------------------------------------------
secao "Memoria e swap"
if tem free; then
  free -h 2>/dev/null | recuar
fi
if [ -r /proc/meminfo ]; then
  awk '
    /^MemTotal:/     { total = $2 }
    /^MemAvailable:/ { disp = $2; tem_disp = 1 }
    /^SwapTotal:/    { swt = $2 }
    /^SwapFree:/     { swl = $2 }
    END {
      d = tem_disp ? sprintf("%d MiB", disp / 1024) : "n/d (kernel sem MemAvailable)"
      printf "  Resumo: RAM total %d MiB, disponivel %s; swap total %d MiB, livre %d MiB\n",
        total / 1024, d, swt / 1024, swl / 1024
    }' /proc/meminfo
else
  echo "  /proc/meminfo nao pode ser lido"
fi

# ---------------------------------------------------------------------------------------------
secao "Disco"
if tem df; then
  # Sem os sistemas de arquivo de memoria e de conteiner; se o df nao aceitar as opcoes
  # (BusyBox, por exemplo), mostra tudo.
  discos=$(df -hT -x tmpfs -x devtmpfs -x overlay -x squashfs -x efivarfs 2>/dev/null)
  if [ -z "$discos" ]; then
    discos=$(df -h 2>/dev/null)
  fi
  printf '%s\n' "${discos:-df nao respondeu}" | recuar
else
  echo "  df nao existe"
fi

# ---------------------------------------------------------------------------------------------
secao "Docker"
docker_ok=nao
if tem docker; then
  cliente=$(docker version --format '{{.Client.Version}}' 2>/dev/null)
  echo "Cliente: ${cliente:-desconhecido}"
  servidor=$(docker version --format '{{.Server.Version}}' 2>&1)
  if [ $? -eq 0 ] && [ -n "$servidor" ]; then
    docker_ok=sim
    echo "Servidor (daemon): $servidor"
    raiz=$(docker info --format '{{.DockerRootDir}}' 2>/dev/null)
    if [ -n "$raiz" ]; then
      echo "Pasta de dados do Docker: $raiz"
      if tem df; then
        echo "Espaco nessa pasta:"
        df -h "$raiz" 2>/dev/null | recuar
      fi
    fi
  else
    case "$servidor" in
      *ermission*denied*)
        echo "Servidor (daemon): sem permissao para falar com o Docker."
        echo "  O seu usuario nao esta no grupo docker. O resto do diagnostico segue; se puder,"
        echo "  rode de novo com um usuario que tenha acesso ao Docker (sem sudo, de preferencia)."
        ;;
      *)
        echo "Servidor (daemon): nao respondeu (parado, ou outro problema)."
        ;;
    esac
  fi
  compose2=$(docker compose version --short 2>/dev/null)
  echo "Docker Compose (plugin, 'docker compose'): ${compose2:-nao encontrado}"
else
  echo "O comando docker nao existe neste servidor."
fi
if tem docker-compose; then
  compose1=$(docker-compose version --short 2>/dev/null)
  echo "docker-compose (antigo, separado): ${compose1:-instalado, versao nao lida}"
else
  echo "docker-compose (antigo, separado): nao encontrado"
fi

# ---------------------------------------------------------------------------------------------
secao "Conteineres rodando (so nome, imagem, portas e status)"
imagens=""
if [ "$docker_ok" = sim ]; then
  docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Ports}}\t{{.Status}}' 2>/dev/null \
    | mascarar_ip | recuar
  imagens=$(docker ps --format '{{.Names}}|{{.Image}}|{{.Ports}}' 2>/dev/null)
  secao "Memoria e CPU em uso por conteiner (uma leitura)"
  docker stats --no-stream --format 'table {{.Name}}\t{{.MemUsage}}\t{{.CPUPerc}}' 2>/dev/null \
    | recuar
else
  echo "  (sem acesso ao Docker: secao pulada)"
fi

# ---------------------------------------------------------------------------------------------
secao "Quem escuta nas portas 80 e 443"
# Le o que escuta (formato do ss, sem cabecalho) e mostra so as portas 80 e 443.
# So diz "ninguem escuta" quando a leitura deu certo e listou alguma porta (um servidor de verdade
# sempre escuta em alguma, o ssh no minimo). Leitura vazia ou que falhou nao e "ninguem escuta".
mostrar_portas() {
  awk '
    NF < 4 { next }
    { lidas++ }
    {
      local_ = $4
      porta = local_; sub(/.*:/, "", porta)
      if (porta != "80" && porta != "443") next
      ende = local_; sub(/:[^:]*$/, "", ende); sub(/%.*/, "", ende)
      if (ende ~ /^(0\.0\.0\.0|\*|\[::\]|::)$/)        escopo = "todas as interfaces"
      else if (ende ~ /^(127\.|\[::1\]|::1$)/)          escopo = "so loopback"
      else                                              escopo = "um IP especifico (omitido)"
      if (ende ~ /:/) escopo = escopo " (IPv6)"
      proc = ""
      for (i = 6; i <= NF; i++) proc = proc $i
      nomes = ""
      while (match(proc, /\("[^"]+"/)) {
        n = substr(proc, RSTART + 2, RLENGTH - 3)
        if (index("," nomes ",", "," n ",") == 0) nomes = nomes (nomes == "" ? "" : ",") n
        proc = substr(proc, RSTART + RLENGTH)
      }
      if (nomes == "") nomes = "(nao visivel: precisa de root para ver o processo)"
      printf "  porta %s: %s; processo: %s\n", porta, escopo, nomes
      achou = 1
    }
    END {
      if (achou) exit
      if (lidas > 0) printf "  ninguem escuta em 80 nem em 443 (a leitura listou %d porta(s), nenhuma dessas)\n", lidas
      else print "  nao deu para ler as portas: a leitura veio vazia. Isso NAO quer dizer que ninguem escuta."
    }'
}
# Imprime o que escuta (TCP) no formato do ss e sem cabecalho. Retorna 1 se nem o ss nem o netstat
# responderam. O ss e lido sem -H (o iproute2 antigo nao tem) e a primeira linha, o cabecalho, e
# tirada depois.
ler_escutas() {
  if tem ss; then
    _saida=$(ss -ltnp 2>/dev/null) && { printf '%s\n' "$_saida" | sed -e 1d; return 0; }
  fi
  if tem netstat; then
    # netstat: Proto Recv-Q Send-Q Local Foreign State PID/Program. Reordena para o formato do ss.
    _saida=$(netstat -ltnp 2>/dev/null) && {
      printf '%s\n' "$_saida" \
        | awk 'NR > 2 { p = ($7 == "-" || $7 == "") ? "" : "users:((\"" $7 "\""; print $6, $2, $3, $4, $5, p }' \
        | sed -e 's/"[0-9]*\//"/'
      return 0
    }
  fi
  return 1
}
if ! tem ss && ! tem netstat; then
  echo "  nem ss nem netstat existem: nao deu para ver as portas"
elif escutas=$(ler_escutas); then
  printf '%s\n' "$escutas" | mostrar_portas
else
  echo "  nao deu para ler as portas: o ss (ou o netstat) falhou, por versao antiga ou falta de permissao."
  echo "  Isso NAO quer dizer que ninguem escuta."
fi
echo "  (docker-proxy no lugar do processo quer dizer: um conteiner publica essa porta)"

# ---------------------------------------------------------------------------------------------
secao "Proxy na frente (o que parece ser)"
achados=""

# 1) Conteineres: pelo nome da imagem; diz se publica 80 ou 443. Por pipe, e nao por
#    here-document: o bash antigo grava o here-document num arquivo temporario.
if [ -n "$imagens" ]; then
  achados=$(printf '%s\n' "$imagens" | while IFS='|' read -r nome imagem portas; do
    [ -z "$nome" ] && continue
    img=$(printf '%s' "$imagem" | tr 'A-Z' 'a-z')
    tipo=""
    case "$img" in
      *nginx-proxy-manager*|*jc21/*) tipo="Nginx Proxy Manager" ;;
      *traefik*)                     tipo="Traefik" ;;
      *caddy*)                       tipo="Caddy" ;;
      *haproxy*)                     tipo="HAProxy" ;;
      *nginx*)                       tipo="Nginx" ;;
      *httpd*|*apache*)              tipo="Apache" ;;
    esac
    [ -z "$tipo" ] && continue
    publica=""
    case "$portas" in *:80-\>*|*:443-\>*) publica=", publica 80/443" ;; esac
    echo "  - $tipo, no conteiner $nome$publica"
  done)
fi

# 2) Servicos do proprio servidor (fora do Docker), pelo systemd.
servicos_lidos=nao
if tem systemctl; then
  servicos_lidos=sim
  for s in nginx caddy traefik haproxy apache2 httpd; do
    if [ "$(systemctl is-active "$s" 2>/dev/null)" = "active" ]; then
      achados="${achados}${achados:+
}  - servico $s ativo no servidor (fora do Docker)"
    fi
  done
fi

if [ -n "$achados" ]; then
  echo "  Parece estar na frente:"
  printf '%s\n' "$achados"
elif [ "$docker_ok" = sim ] && [ "$servicos_lidos" = sim ]; then
  echo "  Nenhum proxy conhecido (Traefik, Nginx, Caddy, Nginx Proxy Manager, HAProxy, Apache) achado"
  echo "  nos conteineres rodando nem nos servicos do systemd."
else
  echo "  Nenhum proxy conhecido achado, mas a leitura ficou incompleta (abaixo): isso NAO prova que"
  echo "  nao ha proxy."
fi
if [ "$docker_ok" != sim ]; then
  echo "  Sem acesso ao Docker: os conteineres nao foram vistos. Um proxy dentro de conteiner so"
  echo "  aparece na linha de processos abaixo e, se deu para ler, nas portas acima."
fi
if [ "$servicos_lidos" != sim ]; then
  echo "  systemctl nao existe: os servicos do sistema nao foram conferidos."
fi

# 3) Processos com nome de proxy (os de dentro de conteiner tambem aparecem aqui).
if tem ps; then
  if lista_ps=$(ps -eo comm= 2>/dev/null) && [ -n "$lista_ps" ]; then
    procs=$(printf '%s\n' "$lista_ps" \
      | awk '/^(nginx|caddy|traefik|haproxy|apache2|httpd)$/ && !visto[$0]++ { printf "%s ", $0 }')
    echo "  Processos com nome de proxy: ${procs:-nenhum}"
  else
    echo "  Processos com nome de proxy: o ps falhou ou veio vazio, nao conferido"
  fi
else
  echo "  Processos com nome de proxy: ps nao existe, nao conferido"
fi

# ---------------------------------------------------------------------------------------------
secao "Redes do Docker (so nomes)"
if [ "$docker_ok" = sim ]; then
  docker network ls --format '{{.Name}}' 2>/dev/null | recuar
else
  echo "  (sem acesso ao Docker: secao pulada)"
fi

# ---------------------------------------------------------------------------------------------
secao "Pais e regiao do servidor"
fuso=""
if tem timedatectl; then
  fuso=$(timedatectl show -p Timezone --value 2>/dev/null)
fi
if [ -z "$fuso" ] && [ -r /etc/timezone ]; then
  fuso=$(head -n 1 /etc/timezone)
fi
if [ -z "$fuso" ] && [ -L /etc/localtime ]; then
  fuso=$(readlink /etc/localtime 2>/dev/null | sed -e 's|.*/zoneinfo/||')
fi
echo "  Fuso horario do sistema: ${fuso:-desconhecido} (pista fraca: muitos servidores usam UTC)"
echo "  Sem chamar servico externo nao da para saber o pais. Informe na conversa o pais e a"
echo "  regiao do datacenter, como aparecem no painel do provedor da VPS."

printf '\nFim. Nada foi alterado neste servidor.\n'
