# A imagem do servidor da nuvem do Aurora Whisper (ADR-019, "Decisoes da F11",
# F11e). Quem faz o build e o Mauro (o agente nao roda Docker), da raiz do
# repositorio:
#
#   docker build -f deploy/Dockerfile -t aurora-whisper-server:producao .
#
# O contexto e a raiz, filtrado pelo .dockerignore, que e uma lista de
# permissao: so entram cmd/, internal/, migrations/, web/ (sem node_modules e
# sem dist), go.mod e go.sum.
#
# Tres estagios:
#   1. painel:   o app da nuvem, "pnpm build:nuvem" (sai em web/dist-nuvem; o
#                "pnpm build" comum escreve em ../bin/painel, que e o do PC);
#   2. servidor: o binario Go estatico de ./cmd/server, com CGO_ENABLED=0,
#                -trimpath e SEM build tags: o login de teste (tag
#                logindeteste) nunca entra nesta imagem;
#   3. final:    distroless static, usuario nonroot (uid 65532), com as CAs do
#                sistema (o servidor fala TLS com o Google) e sem shell: o
#                HEALTHCHECK e o proprio binario (subcomando healthcheck).
#
# As versoes sao as do repositorio, em tags exatas: o Node 24 e o pnpm 11.5.1
# do web/ (.github/workflows/ci.yml e web/pnpm-lock.yaml) e o Go do go.mod.
# deploy/deploy_test.go confere tudo isso lendo este arquivo.

# --- 1. o painel da nuvem -----------------------------------------------------
FROM node:24.14.0-slim AS painel
ENV CI=true
RUN npm install --global pnpm@11.5.1
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build:nuvem

# --- 2. o servidor ------------------------------------------------------------
FROM golang:1.27.0 AS servidor
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/aurora-whisper-server ./cmd/server

# --- 3. a imagem final --------------------------------------------------------
# A distroless nao tem tag de versao (so nonroot, latest, debug): a fixacao por
# digest (@sha256:...) e feita no primeiro build do Mauro, com o digest que ele
# vir.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=servidor /out/aurora-whisper-server /usr/local/bin/aurora-whisper-server
COPY --from=painel /src/web/dist-nuvem/ /painel/

# Os padroes da imagem. Nenhum segredo vem por ENV: DATABASE_URL_FILE,
# SESSION_SECRET_FILE, DEVICE_TOKEN_PEPPER_FILE e GOOGLE_CLIENT_SECRET_FILE
# apontam para arquivos montados pelo compose (Parte 2). LOG_LEVEL=info: com
# warn ou error, a linha "retencao" do job some.
ENV HTTP_ADDR=0.0.0.0:8080 \
    PAINEL_DIR=/painel \
    LOG_LEVEL=info

USER 65532:65532
EXPOSE 8080

# A checagem de vida (GET no health/live do loopback). O start-period cobre a
# primeira rodada de Arrumar e Reter, que roda ANTES de a porta abrir (F11c).
# O compose da API (Parte 2) usa esta mesma checagem de vida. A do job
# (healthcheck -retencao) roda por docker exec (o subir.sh e a conferencia do
# mes), e nunca como healthcheck: onde o unhealthy reinicia (o Swarm), ela
# reiniciaria a API, e o Traefik nao roteia um conteiner unhealthy.
HEALTHCHECK --interval=30s --timeout=5s --start-period=2m --retries=3 \
    CMD ["/usr/local/bin/aurora-whisper-server", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/aurora-whisper-server"]
CMD ["serve"]
