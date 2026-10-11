# A imagem do backup da nuvem (ADR-019, "Decisoes da F11", D2 e F11f): o
# cliente do Postgres 18 (o mesmo major do servidor), o age e o rclone, em
# versoes exatas. E a imagem do conteiner do backup na VPS (o backup.sh,
# agendado pelo agendar-backup.sh, que e o comando do servico no
# deploy/compose.yml) e a do PC (scripts\backup-chave.ps1 e
# scripts\teste-restauracao-mensal.ps1 geram a chave e decifram DENTRO de um
# conteiner dela: o age nao esta no Windows, e nada e instalado).
#
# Quem faz o build e o Mauro (o agente nao roda Docker), da raiz do
# repositorio:
#
#   docker build -f deploy/backup/Dockerfile -t aurora-whisper-backup:producao deploy/scripts
#
# O contexto e deploy/scripts, filtrado por deploy/backup/Dockerfile.dockerignore:
# so o backup.sh e o agendar-backup.sh entram.
#
# O Postgres e o 18.6, a mesma versao do servico aurora-whisper-postgres do
# compose (o pg_dump tem de ser do mesmo major do servidor, ou mais novo).
#
# Por que o rclone (com o backend b2), e nao a CLI do B2: e um binario unico e
# estatico, sem Python nem pip numa VPS compartilhada; o "copyto" de um nome
# novo so precisa de listar e escrever, que e o que a application key restrita
# (sem deleteFiles) tem; e ele confere o que subiu. A CLI do B2 e Python, mais
# pesada, e nao faz aqui nada que o rclone nao faca.
#
# Por que o age por "go install": o go install confere o modulo contra a base
# de checksums do Go (sum.golang.org), entao a versao fixada e verificada
# criptograficamente, sem baixar binario solto. O rclone vem da imagem oficial,
# na versao exata.
#
# Nenhuma dependencia nova no servidor Go: e ferramenta de operacao, numa
# imagem a parte. deploy/scripts_test.go confere este arquivo.

# --- o age ----------------------------------------------------------------------
FROM golang:1.27.0 AS age
ENV CGO_ENABLED=0 GOBIN=/out
RUN go install -trimpath filippo.io/age/cmd/age@v1.2.1 \
    && go install -trimpath filippo.io/age/cmd/age-keygen@v1.2.1

# --- o rclone -------------------------------------------------------------------
# O 1.73 (jan-2026) foi o primeiro a aceitar as chaves restritas a bucket que o
# B2 cria hoje: o 1.70 recebia "not currently supported on API version number
# 1" no primeiro backup da VPS (10-out-2026). O 1.75.2 traz tambem a correcao
# de seguranca do --b2-download-url.
FROM rclone/rclone:1.75.2 AS rclone

# --- a imagem do backup -----------------------------------------------------------
# A alpine da imagem oficial ja traz o bash (o script dela usa) e o flock.
FROM postgres:18.6-alpine
COPY --from=age /out/age /out/age-keygen /usr/local/bin/
COPY --from=rclone /usr/local/bin/rclone /usr/local/bin/rclone
COPY backup.sh /usr/local/bin/backup.sh
COPY agendar-backup.sh /usr/local/bin/agendar-backup.sh
RUN set -eux; \
    chmod 0555 /usr/local/bin/backup.sh /usr/local/bin/agendar-backup.sh; \
    mkdir -p /backup; \
    chown postgres:postgres /backup; \
    chmod 0700 /backup; \
    command -v bash; \
    command -v flock; \
    pg_dump --version; \
    pg_restore --version; \
    age --version; \
    age-keygen --version; \
    rclone version
USER postgres
CMD ["backup.sh"]
