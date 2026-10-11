package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// O deploy/scripts/subir.sh (F11j) com o docker, o timedatectl, o curl, o stat
// e o sleep de mentira, numa VPS de mentira (a pasta base com o deploy/
// copiado, os segredos e o config.env que o gerar-segredos.sh grava). Nada
// fala com o Docker, a rede ou o Traefik de verdade.

// dockerDoSubir imita o que o subir.sh pede ao Docker. O "exec" é o do
// conferir-postgres.sh (o dockerDoPostgres), chamado pelo subir.sh.
const dockerDoSubir = `
if [ "$1" = exec ] && [ "$2" = aurora-whisper-api ]; then
  if [ "$#" = 5 ] && [ "$3 $4 $5" = "/usr/local/bin/aurora-whisper-server healthcheck -retencao" ]; then
    if [ "${FALSO_JOB:-ok}" = ok ]; then printf 'healthcheck: OK (retencao ha 3 s)\n'; exit 0; fi
    printf 'healthcheck: FALHOU (a ultima rodada da retencao teve 1 falha(s) de banco ou outra)\n' >&2; exit 1
  fi
  printf 'docker exec de mentira na API: inesperado\n' >&2; exit 93
fi
if [ "$1" = exec ]; then
` + dockerDoPostgres + `
exit 0
fi
estado="$REGISTRO.estado"
sub="$1"; shift
case "$sub" in
  info) if [ "${FALSO_DOCKER_FORA:-}" = 1 ]; then exit 1; fi ;;
  image)
    for f in ${FALSO_SEM_IMAGEM:-}; do if [ "$f" = "$2" ]; then exit 1; fi; done ;;
  network)
    if [ "$1" != inspect ] || [ "$2" != web ] || [ "${FALSO_SEM_WEB:-}" = 1 ]; then exit 1; fi
    printf '%s\n' "${FALSO_SUBREDES-172.18.0.0/16 }" ;;
  compose)
    if [ "$1" = version ]; then exit 0; fi
    envfile=''
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --env-file) envfile="$2"; shift 2 ;;
        --project-name|-f) shift 2 ;;
        *) break ;;
      esac
    done
    acao="$1"; shift
    case "$acao" in
      config)
        if [ "${FALSO_CONFIG_FALHA:-}" = 1 ]; then exit 1; fi
        # Como o compose de verdade: o ambiente vale mais que o --env-file.
        d="${DOMINIO:-$(sed -n 's/^DOMINIO=//p' "$envfile" | tail -n 1)}"
        printf 'name: aurora-whisper\nservices:\n  aurora-whisper-api:\n    labels:\n'
        # \140 e a crase (esta constante do Go e um texto entre crases).
        printf '      traefik.http.routers.aurora-whisper-api.rule: Host(\140%s\140)\n' "${FALSO_HOST_DO_CONFIG:-$d}" ;;
      up) servico="${@: -1}"; printf 'de pe %s\n' "$servico" >> "$estado"
          if [ "${FALSO_UP_FALHA:-}" = "$servico" ]; then exit 1; fi ;;
      stop) printf 'parado %s\n' "${@: -1}" >> "$estado" ;;
      *) printf 'compose de mentira: %s\n' "$acao" >&2; exit 95 ;;
    esac ;;
  inspect)
    case "$2" in
      '{{.State.Health.Status}}')
        case "$3" in
          aurora-whisper-postgres) printf '%s\n' "${FALSO_SAUDE_PG:-healthy}" ;;
          aurora-whisper-api) printf '%s\n' "${FALSO_SAUDE_API:-healthy}" ;;
          *) exit 1 ;;
        esac ;;
      '{{.State.Status}}') printf '%s\n' "${FALSO_ESTADO_BACKUP:-running}" ;;
      # A API: depois do "up" do compose, a subida nova (a menos que o
      # compose nao a recrie); antes, a velha se ela ja existia.
      '{{.Id}} {{.State.StartedAt}}')
        [ "$3" = aurora-whisper-api ] || exit 1
        if grep -qx 'de pe aurora-whisper-api' "$estado" 2> /dev/null && [ "${FALSO_API_NAO_RECRIA:-}" != 1 ]; then
          printf 'c0ffee2 %s\n' "$INICIO_DA_API"
        elif [ "${FALSO_API_EXISTIA:-}" = 1 ]; then
          printf 'c0ffee1 2026-08-01T00:00:00.5Z\n'
        else
          printf 'Error: No such object: %s\n' "$3" >&2; exit 1
        fi ;;
      *) exit 1 ;;
    esac ;;
  ps)
    nome=''
    for a in "$@"; do
      case "$a" in 'name=^/?'*'$') nome="${a#'name=^/?'}"; nome="${nome%'$'}" ;; esac
    done
    for r in ${FALSO_RODANDO:-}; do
      if [ "$r" = "$nome" ] && ! grep -qx "parado $nome" "$estado" 2> /dev/null; then printf 'c0ffee\n'; fi
    done ;;
  run)
    junto=" $* "
    case "$junto" in
      *" migrate -status "*)
        if [ "${FALSO_STATUS_FALHA:-}" = 1 ]; then exit 1; fi
        printf 'aplicada  0001_users_table.sql\n'
        if [ "${FALSO_PENDENTE:-}" = 1 ] && [ ! -e "$REGISTRO.migrou" ]; then
          printf 'pendente  0016_learned_pairs_table.sql\nmigrate: 15 aplicadas, 1 pendentes\n'
        else
          printf 'migrate: 16 aplicadas, 0 pendentes\n'
        fi ;;
      *" migrate "*)
        if [ "${FALSO_MIGRATE_FALHA:-}" = 1 ]; then exit 1; fi
        if [ "${FALSO_MIGRATE_NAO_APLICA:-}" != 1 ]; then : > "$REGISTRO.migrou"; fi
        printf 'migrate: 1 aplicadas agora, 15 ja estavam aplicadas\n' ;;
      *) printf 'docker run de mentira: inesperado\n' >&2; exit 94 ;;
    esac ;;
  # Com --since a hora da subida nova, so o log dela; sem, o de uma subida
  # antiga (se o teste der um) e o dela.
  logs)
    if [ "$1" = --since ]; then
      if [ "$2" != "$INICIO_DA_API" ]; then printf 'logs de mentira: --since %s inesperado\n' "$2" >&2; exit 92; fi
    elif [ -n "${FALSO_LOG_API_ANTIGO:-}" ]; then
      cat "$FALSO_LOG_API_ANTIGO"
    fi
    cat "$FALSO_LOG_API" ;;
  *) printf 'docker de mentira: %s\n' "$sub" >&2; exit 99 ;;
esac
exit 0
`

// O stat de mentira responde ao modo da pasta dos segredos (o NTFS do
// Windows não tem 0700) e repassa o resto ao de verdade.
const statDoSubir = `
if [ "$1" = -c ] && [ "$2" = %a ]; then printf '%s\n' "${FALSO_MODO:-700}"; exit 0; fi
PATH="${PATH#*:}" exec stat "$@"
`

// O log da API na ordem do servidor (F11c): arrumação, retenção, servindo.
const logDaAPIEmOrdem = `{"time":"2026-10-09T12:00:00Z","level":"INFO","msg":"arrumação","logins_pendentes":0,"sessoes":0,"pedidos":0}
{"time":"2026-10-09T12:00:00Z","level":"INFO","msg":"retenção","usuarios":0,"ativos":0,"lixeira":0,"lapides_criadas":0,"lapides_vencidas":0,"eventos":0,"mutacoes":0,"falhas":0,"ms":3}
{"time":"2026-10-09T12:00:01Z","level":"INFO","msg":"servindo","endereco":"[::]:8080","contas_permitidas":1,"https":true,"proxy_confiavel":true}
`

// inicioDaAPI é a hora da subida nova da API (o StartedAt do docker inspect,
// com os nanossegundos).
const inicioDaAPI = "2026-10-09T12:00:00.123456789Z"

type subirDeTeste struct {
	a        *ambienteBash
	base     string
	script   string
	segredos map[string]string
}

// configDoGerar é o config.env que o gerar-segredos.sh grava.
const configDoGerar = "# Gerado por deploy/scripts/gerar-segredos.sh em 2026-10-09T12:00:00Z.\n" +
	"# Nao e segredo, mas nao vai para o repositorio.\n" +
	"PUBLIC_BASE_URL=https://aurora.exemplo.com.br\nDOMINIO=aurora.exemplo.com.br\n" +
	"CONTAS_PERMITIDAS=mauro@exemplo.com\nB2_BUCKET=bucket-de-teste\n"

func novoSubir(t *testing.T) *subirDeTeste {
	t.Helper()
	a := novoAmbienteBash(t)
	s := &subirDeTeste{a: a, base: filepath.Join(a.dir, "vps", "aurora-whisper"), segredos: map[string]string{}}
	deploy := filepath.Join(s.base, "deploy")
	for orig, dest := range map[string]string{
		subirSh:         filepath.Join(deploy, "scripts", "subir.sh"),
		composeYml:      filepath.Join(deploy, "compose.yml"),
		conferirPostgre: filepath.Join(deploy, "postgres", "conferir-postgres.sh"),
	} {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte(ler(t, orig)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.script = filepath.Join(deploy, "scripts", "subir.sh")
	seg := filepath.Join(s.base, "segredos")
	if err := os.MkdirAll(seg, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range strings.Fields("postgres-superusuario-senha postgres-aurora-senha postgres-backup-senha session-secret device-token-pepper") {
		s.segredos[n] = hexAleatorio(t, 32)
	}
	s.segredos["database-url"] = "postgres://aurora:" + s.segredos["postgres-aurora-senha"] + "@aurora-whisper-postgres:5432/aurora?sslmode=disable"
	s.segredos["google-client.json"] = strings.TrimSpace(jsonDoGoogleDeTeste("aurora.exemplo.com.br"))
	s.segredos["backup-chave-publica"] = chavePublicaDeTeste
	s.segredos["rclone.conf"] = "[b2]\ntype = b2\naccount = " + keyIDDeTeste + "\nkey = " + appKeyDeTeste
	for n, v := range s.segredos {
		if err := os.WriteFile(filepath.Join(seg, n), []byte(v+"\n"), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	s.escreverConfig(t, configDoGerar)
	logAPI := filepath.Join(a.dir, "log-da-api.txt")
	if err := os.WriteFile(logAPI, []byte(logDaAPIEmOrdem), 0o644); err != nil {
		t.Fatal(err)
	}
	a.env["AW_BASE"] = posix(s.base)
	a.env["FALSO_LOG_API"] = posix(logAPI)
	a.env["INICIO_DA_API"] = inicioDaAPI
	// As esperas curtas: o sleep é de mentira, mas cada volta chama o docker
	// de mentira, e no Windows cada processo do bash custa caro.
	a.env["AW_ESPERA_POSTGRES"] = "6"
	a.env["AW_ESPERA_API"] = "6"
	a.env["AW_ESPERA_HTTPS"] = "15"
	// O ambiente do PC de teste não pode vazar para o servidor.
	a.env["AW_PREFIXO"] = "aurora-whisper-teste"
	a.env["AW_SEGREDOS"] = "/outro/lugar"
	a.falso("docker", dockerDoSubir)
	a.falso("timedatectl", `if [ "$*" = "show -p NTPSynchronized --value" ]; then printf '%s\n' "${FALSO_NTP:-yes}"; exit 0; fi; exit 1`)
	a.falso("curl", `printf '%s' "${FALSO_HTTPS:-200}"`)
	a.falso("stat", statDoSubir)
	a.falso("sleep", "exit 0")
	return s
}

func (s *subirDeTeste) escreverConfig(t *testing.T, texto string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.base, "config.env"), []byte(texto), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (s *subirDeTeste) config(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.base, "config.env"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (s *subirDeTeste) rodar(respostas string) resultado {
	return s.a.rodar(s.script, []byte(respostas))
}

// semSegredo: nenhum valor de segredo na saída nem numa linha de comando.
func (s *subirDeTeste) semSegredo(t *testing.T, r resultado) {
	t.Helper()
	tudo := r.tudo() + strings.Join(s.a.chamadas(), "\n")
	for n, v := range s.segredos {
		if n == "backup-chave-publica" {
			continue
		}
		for _, linha := range strings.Split(v, "\n") {
			if len(linha) >= 16 && strings.Contains(tudo, linha) {
				t.Errorf("o valor de %s aparece na saída ou numa chamada", n)
			}
		}
	}
	if strings.Contains(tudo, appKeyDeTeste) || strings.Contains(tudo, segredoDoClientDeTeste) {
		t.Error("segredo na saída")
	}
}

// migrateSemStatus é a chamada do migrate que aplica (e não o -status): o
// "re:" diz a ordem que é uma expressão.
const migrateSemStatus = `re:\[aurora-whisper-server:producao\] \[migrate\]$`

// ordem confere que cada trecho aparece numa chamada, na ordem dada (com
// "re:" na frente, uma expressão regular).
func (s *subirDeTeste) ordem(t *testing.T, trechos ...string) {
	t.Helper()
	ultimo := -1
	for _, tr := range trechos {
		i := s.a.indice(tr)
		if expr, ok := strings.CutPrefix(tr, "re:"); ok {
			i = -1
			for j, c := range s.a.chamadas() {
				if regexp.MustCompile(expr).MatchString(c) {
					i = j
					break
				}
			}
		}
		if i < 0 {
			t.Fatalf("faltou a chamada %q:\n%s", tr, strings.Join(s.a.chamadas(), "\n"))
		}
		if i <= ultimo {
			t.Fatalf("a chamada %q veio fora de ordem:\n%s", tr, strings.Join(s.a.chamadas(), "\n"))
		}
		ultimo = i
	}
}

func TestSubir_PrimeiroDeployComMigrationPendente(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	s.a.env["FALSO_PENDENTE"] = "1"
	r := s.rodar("SIM\nSIM\n")
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	s.ordem(t,
		"docker [info]",
		"docker [image] [inspect] [aurora-whisper-server:producao]",
		"docker [image] [inspect] [aurora-whisper-backup:producao]",
		"timedatectl [show] [-p] [NTPSynchronized] [--value]",
		"docker [network] [inspect] [web]",
		"[config]",
		"[up] [-d] [aurora-whisper-postgres]",
		"docker [inspect] [-f] [{{.State.Health.Status}}] [aurora-whisper-postgres]",
		"docker [exec] [aurora-whisper-postgres] [pg_isready]",
		"[SHOW log_statement]",
		"[migrate] [-status]",
		migrateSemStatus,
		"docker [inspect] [-f] [{{.Id}} {{.State.StartedAt}}] [aurora-whisper-api]",
		"[up] [-d] [aurora-whisper-api]",
		"docker [inspect] [-f] [{{.State.Health.Status}}] [aurora-whisper-api]",
		// Só o log desta subida.
		"docker [logs] [--since] ["+inicioDaAPI+"] [aurora-whisper-api]",
		"docker [exec] [aurora-whisper-api] [/usr/local/bin/aurora-whisper-server] [healthcheck] [-retencao]",
		"[up] [-d] [aurora-whisper-backup]",
		"curl [--silent]",
	)
	// O PROXY_CONFIAVEL lido da rede web, gravado no config.env (o resto
	// igual), 0600.
	if want := configDoGerar + "PROXY_CONFIAVEL=172.18.0.0/16\n"; s.config(t) != want {
		t.Errorf("o config.env:\n%s\nquero:\n%s", s.config(t), want)
	}
	if sobras, _ := filepath.Glob(filepath.Join(s.base, "config.env.novo.*")); len(sobras) > 0 {
		t.Errorf("sobrou: %v", sobras)
	}
	// O compose sempre com o projeto, o config.env e o compose da VPS.
	compose := "docker [compose] [--project-name] [aurora-whisper] [--env-file] [" + posix(filepath.Join(s.base, "config.env")) +
		"] [-f] [" + posix(filepath.Join(s.base, "deploy", "compose.yml")) + "]"
	for _, c := range s.a.chamadas() {
		if strings.HasPrefix(c, "docker [compose] [--") && !strings.HasPrefix(c, compose) {
			t.Errorf("compose sem o projeto, o config.env ou o compose da VPS: %s", c)
		}
		// O migrate: um contêiner descartável, na rede interna, só leitura,
		// sem capacidades, só com a database-url, e SEM os rótulos do
		// Traefik (nada de "compose run").
		if strings.HasPrefix(c, "docker [run]") {
			exigirContem(t, "o docker run", c, "[--rm] [--network] [aurora-whisper-interna] [--read-only] [--cap-drop] [ALL]",
				"[-v] ["+posix(filepath.Join(s.base, "segredos", "database-url"))+":/run/secrets/database-url:ro]",
				"[-e] [DATABASE_URL_FILE=/run/secrets/database-url] [aurora-whisper-server:producao] [migrate]")
			exigirNaoContem(t, "o docker run", c, "--label", "traefik", "-p]", "--publish")
		}
		if strings.Contains(c, "[compose]") && strings.Contains(c, "[run]") {
			t.Errorf("compose run levaria os rótulos do Traefik: %s", c)
		}
	}
	exigirContem(t, "a saída", r.saida,
		"OK: o Docker, o compose e as duas imagens", "OK: o relogio sincronizado",
		"OK: PROXY_CONFIAVEL=172.18.0.0/16", "OK: o Postgres de pe e saudavel",
		"OK: os cinco ajustes de log", "Ha migration pendente", "OK: as migrations em dia",
		"Digite SIM para subir a API", "OK: a API saudavel: arrumacao, retencao e so entao servindo",
		"OK: a saude do job de retencao (healthcheck -retencao)", "OK: o agendador do backup de pe", "OK: https://aurora.exemplo.com.br responde",
		"OK: a nuvem esta no ar em https://aurora.exemplo.com.br", "docker exec aurora-whisper-backup backup.sh")
	s.semSegredo(t, r)
	// Nada de PAROU nem FALHOU.
	if strings.Contains(r.tudo(), "FALHOU") || strings.Contains(r.tudo(), "PAROU") {
		t.Errorf("FALHOU com tudo certo: %q", r.tudo())
	}
}

func TestSubir_SemMigrationPendenteSoUmSIM(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	r := s.rodar("SIM\n")
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	if strings.Contains(r.saida, "Ha migration pendente") {
		t.Error("sem pendente, o migrate não roda nem pergunta")
	}
	for _, c := range s.a.chamadas() {
		if regexp.MustCompile(`\[aurora-whisper-server:producao\] \[migrate\]$`).MatchString(c) {
			t.Errorf("rodou o migrate sem pendente: %s", c)
		}
	}
	s.ordem(t, "[migrate] [-status]", "[up] [-d] [aurora-whisper-api]")
}

// Sem o SIM, para: no migrate, o Postgres fica e nada mais; na API, as
// migrations rodaram e a API não sobe.
func TestSubir_ParaSemOSIM(t *testing.T) {
	t.Parallel()
	t.Run("no migrate", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		s.a.env["FALSO_PENDENTE"] = "1"
		r := s.rodar("sim\n")
		if r.codigo != 1 || !strings.Contains(r.erro, "PAROU: sem o SIM, o migrate nao rodou") {
			t.Fatalf("deveria parar: %d %q", r.codigo, r.tudo())
		}
		for _, c := range s.a.chamadas() {
			if regexp.MustCompile(`\[migrate\]$`).MatchString(c) || strings.Contains(c, "[aurora-whisper-api]") || strings.Contains(c, "[aurora-whisper-backup]") {
				t.Errorf("seguiu depois do PAROU: %s", c)
			}
		}
	})
	t.Run("na API", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		r := s.rodar("\n")
		if r.codigo != 1 || !strings.Contains(r.erro, "PAROU: sem o SIM, a API nao subiu") {
			t.Fatalf("deveria parar: %d %q", r.codigo, r.tudo())
		}
		if s.a.chamou("docker [compose]") && s.a.indice("[up] [-d] [aurora-whisper-api]") >= 0 {
			t.Error("subiu a API sem o SIM")
		}
		if s.a.indice("[up] [-d] [aurora-whisper-postgres]") < 0 {
			t.Error("o Postgres sobe antes da pergunta")
		}
	})
	t.Run("sem entrada nenhuma", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		r := s.rodar("")
		if r.codigo != 1 || !strings.Contains(r.erro, "PAROU") || s.a.indice("[up] [-d] [aurora-whisper-api]") >= 0 {
			t.Fatalf("deveria parar sem subir a API: %d %q", r.codigo, r.tudo())
		}
	})
}

// Uma imagem nova com migration pendente e a API de pé: a API para antes do
// migrate e sobe de novo pelo compose.
func TestSubir_ImagemNovaParaAAPIAntesDoMigrate(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	s.a.env["FALSO_PENDENTE"] = "1"
	s.a.env["FALSO_RODANDO"] = "aurora-whisper-api aurora-whisper-postgres"
	r := s.rodar("SIM\nSIM\n")
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	s.ordem(t, "[migrate] [-status]", "[stop] [aurora-whisper-api]", migrateSemStatus,
		"[up] [-d] [aurora-whisper-api]")
	exigirContem(t, "a saída", r.saida, "A API esta de pe: ela para antes do migrate", "OK: a API parou para o migrate")
}

// A ordem do log é a da subida de AGORA (o docker logs --since a hora dela),
// e não a da primeira subida do contêiner: depois de um "compose stop", o
// contêiner volta com o log velho; e, semanas depois, a rotação do log já
// levou a subida.
func TestSubir_LogSoDaSubidaAtual(t *testing.T) {
	t.Parallel()
	escrever := func(t *testing.T, s *subirDeTeste, nome, texto string) string {
		t.Helper()
		p := filepath.Join(s.a.dir, nome)
		if err := os.WriteFile(p, []byte(texto), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("a API que voltou de um stop: a subida antiga não conta", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		s.a.env["FALSO_API_EXISTIA"] = "1"
		// A subida antiga, em ordem; a de agora, sem a retenção.
		s.a.env["FALSO_LOG_API_ANTIGO"] = posix(escrever(t, s, "log-antigo.txt", logDaAPIEmOrdem))
		linhas := strings.Split(logDaAPIEmOrdem, "\n")
		escrever(t, s, "log-da-api.txt", linhas[0]+"\n"+linhas[2]+"\n")
		r := s.rodar("SIM\n")
		if r.codigo != 1 || !strings.Contains(r.erro, "FALHOU: o log desta subida da API nao mostra a ordem") {
			t.Fatalf("quero o FALHOU da ordem desta subida: %d %q", r.codigo, r.tudo())
		}
		if s.a.indice("[aurora-whisper-backup]") >= 0 {
			t.Error("subiu o backup depois da falha")
		}
	})
	t.Run("a API que voltou de um stop, em ordem", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		s.a.env["FALSO_API_EXISTIA"] = "1"
		r := s.rodar("SIM\n")
		if r.codigo != 0 || !strings.Contains(r.saida, "OK: a API saudavel: arrumacao, retencao e so entao servindo") {
			t.Fatalf("deveria passar: %d %q", r.codigo, r.tudo())
		}
		s.ordem(t, "[up] [-d] [aurora-whisper-api]", "docker [logs] [--since] ["+inicioDaAPI+"] [aurora-whisper-api]")
	})
	t.Run("a API que o compose não recriou: a ordem já valeu na subida dela", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		s.a.env["FALSO_API_EXISTIA"] = "1"
		s.a.env["FALSO_API_NAO_RECRIA"] = "1"
		// O log da subida dela já saiu da rotação.
		escrever(t, s, "log-da-api.txt", "")
		r := s.rodar("SIM\n")
		if r.codigo != 0 || !strings.Contains(r.saida, "ela nao foi recriada nem reiniciada agora") {
			t.Fatalf("deveria seguir e dizer que a ordem não se confere de novo: %d %q", r.codigo, r.tudo())
		}
		if s.a.chamou("docker [logs]") {
			t.Errorf("leu o log de uma subida antiga: %q", s.a.chamadas())
		}
		s.ordem(t, "[up] [-d] [aurora-whisper-api]", "[healthcheck] [-retencao]", "[up] [-d] [aurora-whisper-backup]")
	})
}

// receber põe um JSON do Google em recebido/, como o scp do roteiro (ou o
// enviar-para-servidor.ps1) faz.
func (s *subirDeTeste) receber(t *testing.T, texto string) string {
	t.Helper()
	p := filepath.Join(s.base, "recebido", "google-client.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(texto), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (s *subirDeTeste) googleAtual(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.base, "segredos", "google-client.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Trocar o client do Google depois: o JSON novo em recebido/ e o subir.sh. O
// compose não recria a API quando só o conteúdo de um segredo muda, e a API
// lê o JSON só na subida: a troca recria a API, e só depois do SIM.
func TestSubir_TrocaDoClientDoGoogle(t *testing.T) {
	t.Parallel()
	novo := strings.Replace(jsonDoGoogleDeTeste("aurora.exemplo.com.br"), "123-abc", "456-def", 1)
	t.Run("um client novo troca o de segredos/ e recria a API", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		recebido := s.receber(t, novo)
		r := s.rodar("SIM\n")
		if r.codigo != 0 {
			t.Fatalf("falhou: %d %q", r.codigo, r.tudo())
		}
		if s.googleAtual(t) != novo {
			t.Error("o segredos/google-client.json não é o JSON novo")
		}
		if info, err := os.Stat(filepath.Join(s.base, "segredos", "google-client.json")); err != nil || info.Mode().Perm()&0o222 != 0 {
			t.Errorf("o JSON novo não ficou só leitura (0444): %v", err)
		}
		if _, err := os.Stat(recebido); !os.IsNotExist(err) {
			t.Error("a cópia de entrada ficou em recebido/")
		}
		if sobras, _ := filepath.Glob(filepath.Join(s.base, "segredos", ".google-client.json.novo.*")); len(sobras) > 0 {
			t.Errorf("sobrou: %v", sobras)
		}
		exigirContem(t, "a saída", r.saida, "um client do Google NOVO em recebido/", "o client do Google e TROCADO",
			"OK: o client do Google trocado", "OK: a nuvem esta no ar")
		s.ordem(t, "[migrate] [-status]", "[up] [-d] [--force-recreate] [aurora-whisper-api]",
			"docker [logs] [--since] ["+inicioDaAPI+"] [aurora-whisper-api]", "[up] [-d] [aurora-whisper-backup]")
		s.semSegredo(t, r)
	})
	t.Run("o mesmo JSON sai de recebido/, sem trocar nem recriar", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		atual := s.googleAtual(t)
		recebido := s.receber(t, atual)
		r := s.rodar("SIM\n")
		if r.codigo != 0 || !strings.Contains(r.saida, "e o mesmo de segredos/ (nada a trocar)") {
			t.Fatalf("deveria seguir sem trocar: %d %q", r.codigo, r.tudo())
		}
		if _, err := os.Stat(recebido); !os.IsNotExist(err) {
			t.Error("a cópia igual ficou em recebido/")
		}
		if s.googleAtual(t) != atual {
			t.Error("o segredos/google-client.json mudou")
		}
		for _, c := range s.a.chamadas() {
			if strings.Contains(c, "--force-recreate") {
				t.Errorf("recriou a API sem troca: %s", c)
			}
		}
	})
	t.Run("sem o SIM, o client não troca", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		atual := s.googleAtual(t)
		recebido := s.receber(t, novo)
		r := s.rodar("nao\n")
		if r.codigo != 1 || !strings.Contains(r.erro, "o client do Google nao foi trocado") {
			t.Fatalf("deveria parar: %d %q", r.codigo, r.tudo())
		}
		if s.googleAtual(t) != atual {
			t.Error("trocou o client sem o SIM")
		}
		if _, err := os.Stat(recebido); err != nil {
			t.Error("o JSON novo deveria continuar em recebido/")
		}
		if s.a.indice("[aurora-whisper-api]") >= 0 && s.a.indice("[up] [-d] [--force-recreate]") >= 0 {
			t.Error("recriou a API sem o SIM")
		}
	})
	for _, c := range []struct{ nome, json, quero string }{
		{"um client instalado", `{"installed":{"client_id":"x","client_secret":"` + segredoDoClientDeTeste + `","redirect_uris":["https://aurora.exemplo.com.br/auth/google/callback"]}}`,
			"nao e o JSON de um client do tipo Aplicativo da Web"},
		{"sem o redirect de produção", jsonDoGoogleDeTeste("outro.exemplo.com.br"), "nao tem o redirect https://aurora.exemplo.com.br/auth/google/callback"},
		{"grande demais", `{"web":{}}` + strings.Repeat(" ", 70000), "grande demais"},
	} {
		t.Run("recusa "+c.nome, func(t *testing.T) {
			t.Parallel()
			s := novoSubir(t)
			atual := s.googleAtual(t)
			recebido := s.receber(t, c.json)
			r := s.rodar("SIM\nSIM\n")
			if r.codigo != 1 || !strings.Contains(r.erro, "FALHOU") || !strings.Contains(r.erro, c.quero) ||
				!strings.Contains(r.erro, "rm "+posix(recebido)) {
				t.Fatalf("quero FALHOU com %q e o rm: %d %q", c.quero, r.codigo, r.tudo())
			}
			if s.googleAtual(t) != atual {
				t.Error("o segredos/google-client.json mudou numa recusa")
			}
			if _, err := os.Stat(recebido); err != nil {
				t.Error("a recusa apagou o que veio (é do Mauro decidir)")
			}
			for _, ch := range s.a.chamadas() {
				if strings.Contains(ch, "[up]") || strings.HasPrefix(ch, "docker [run]") {
					t.Errorf("subiu algo antes de recusar: %s", ch)
				}
			}
			s.semSegredo(t, r)
		})
	}
}

// Cada recusa antes de subir qualquer coisa: nada sobe, e o config.env não
// muda.
func TestSubir_RecusaAntesDeSubir(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		nome  string
		prep  func(s *subirDeTeste)
		quero string
	}{
		{"o Docker fora", func(s *subirDeTeste) { s.a.env["FALSO_DOCKER_FORA"] = "1" }, "o Docker nao responde"},
		{"sem a imagem da API", func(s *subirDeTeste) { s.a.env["FALSO_SEM_IMAGEM"] = "aurora-whisper-server:producao" }, "a imagem aurora-whisper-server:producao nao esta na VPS"},
		{"sem a imagem do backup", func(s *subirDeTeste) { s.a.env["FALSO_SEM_IMAGEM"] = "aurora-whisper-backup:producao" }, "a imagem aurora-whisper-backup:producao nao esta na VPS"},
		{"a pasta dos segredos aberta", func(s *subirDeTeste) { s.a.env["FALSO_MODO"] = "755" }, "nao esta 0700"},
		{"sem o JSON do Google", func(s *subirDeTeste) {
			p := filepath.Join(s.base, "segredos", "google-client.json")
			_ = os.Chmod(p, 0o644)
			_ = os.Remove(p)
		}, "google-client.json"},
		{"sem o config.env", func(s *subirDeTeste) { _ = os.Remove(filepath.Join(s.base, "config.env")) }, "nao achei"},
		{"o endereço em http", func(s *subirDeTeste) {
			s.escreverConfig(s.a.t, strings.Replace(configDoGerar, "https://", "http://", 1))
		}, "nao e https://<dominio>"},
		{"o domínio que não bate", func(s *subirDeTeste) {
			s.escreverConfig(s.a.t, strings.Replace(configDoGerar, "DOMINIO=aurora.", "DOMINIO=outra.", 1))
		}, "nao batem"},
		{"sem conta", func(s *subirDeTeste) {
			s.escreverConfig(s.a.t, strings.Replace(configDoGerar, "CONTAS_PERMITIDAS=mauro@exemplo.com", "CONTAS_PERMITIDAS=", 1))
		}, "CONTAS_PERMITIDAS"},
		{"sem bucket", func(s *subirDeTeste) {
			s.escreverConfig(s.a.t, strings.Replace(configDoGerar, "B2_BUCKET=bucket-de-teste\n", "", 1))
		}, "B2_BUCKET"},
		{"o relógio sem NTP", func(s *subirDeTeste) { s.a.env["FALSO_NTP"] = "no" }, "o relogio da VPS nao esta sincronizado"},
		{"sem o timedatectl", func(s *subirDeTeste) { _ = os.Remove(filepath.Join(s.a.falsos, "timedatectl")) }, "nao achei o timedatectl"},
		{"sem a rede web", func(s *subirDeTeste) { s.a.env["FALSO_SEM_WEB"] = "1" }, "nao achei a rede web"},
		{"duas sub-redes IPv4", func(s *subirDeTeste) { s.a.env["FALSO_SUBREDES"] = "172.18.0.0/16 172.19.0.0/16 " }, "mais de uma sub-rede IPv4"},
		{"sem sub-rede IPv4", func(s *subirDeTeste) { s.a.env["FALSO_SUBREDES"] = "fd00::/64 " }, "nao achei a sub-rede IPv4"},
		{"o compose recusado", func(s *subirDeTeste) { s.a.env["FALSO_CONFIG_FALHA"] = "1" }, "o docker compose recusou"},
		{"a regra do Traefik de outro domínio", func(s *subirDeTeste) { s.a.env["FALSO_HOST_DO_CONFIG"] = "outro-site.exemplo.com.br" },
			"a regra do Traefik que o compose montou nao e a do config.env (Host(`aurora.exemplo.com.br`))"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			t.Parallel()
			s := novoSubir(t)
			c.prep(s)
			antes := ""
			if data, err := os.ReadFile(filepath.Join(s.base, "config.env")); err == nil {
				antes = string(data)
			}
			r := s.rodar("SIM\nSIM\n")
			if r.codigo != 1 || !strings.Contains(r.erro, "FALHOU") || !strings.Contains(r.erro, c.quero) {
				t.Fatalf("quero FALHOU com %q: %d %q", c.quero, r.codigo, r.tudo())
			}
			for _, ch := range s.a.chamadas() {
				if strings.Contains(ch, "[up]") || strings.HasPrefix(ch, "docker [run]") {
					t.Errorf("subiu algo antes de recusar: %s", ch)
				}
			}
			// O PROXY_CONFIAVEL é gravado no passo 3, antes do compose.
			if c.nome != "o compose recusado" && c.nome != "a regra do Traefik de outro domínio" {
				if data, err := os.ReadFile(filepath.Join(s.base, "config.env")); err == nil && string(data) != antes {
					t.Errorf("o config.env mudou numa recusa:\n%s", data)
				}
			}
			s.semSegredo(t, r)
		})
	}
}

// Fora da pasta base (uma cópia do deploy/ em outro lugar, ou o repositório):
// recusa.
func TestSubir_SoDaCopiaDaPastaBase(t *testing.T) {
	t.Parallel()
	s := novoSubir(t)
	s.a.env["AW_BASE"] = posix(filepath.Join(s.a.dir, "outra"))
	r := s.rodar("SIM\nSIM\n")
	if r.codigo != 1 || !strings.Contains(r.erro, "o subir.sh roda da copia em") {
		t.Fatalf("deveria recusar: %d %q", r.codigo, r.tudo())
	}
	if s.a.chamou("docker") {
		t.Error("chamou o docker antes de recusar")
	}
}

// Falhas depois de começar: o passo que falhou para tudo o que vem depois.
func TestSubir_FalhaNoMeioParaOResto(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		nome, quero, naoChama string
		env                   map[string]string
		log                   string
	}{
		{"o Postgres não fica saudável", "o aurora-whisper-postgres nao ficou saudavel", "[migrate]", map[string]string{"FALSO_SAUDE_PG": "unhealthy"}, ""},
		{"o Postgres fora do esperado", "o Postgres nao passou na conferencia", "[migrate]", map[string]string{"FALSO_log_statement": "all"}, ""},
		{"o migrate -status falha", "o migrate -status falhou", "[up] [-d] [aurora-whisper-api]", map[string]string{"FALSO_STATUS_FALHA": "1"}, ""},
		{"o migrate falha", "o migrate falhou", "[up] [-d] [aurora-whisper-api]", map[string]string{"FALSO_PENDENTE": "1", "FALSO_MIGRATE_FALHA": "1"}, ""},
		{"o migrate não aplica", "ainda ha migration pendente", "[up] [-d] [aurora-whisper-api]", map[string]string{"FALSO_PENDENTE": "1", "FALSO_MIGRATE_NAO_APLICA": "1"}, ""},
		{"a API não fica saudável", "a API nao ficou saudavel", "[aurora-whisper-backup]", map[string]string{"FALSO_SAUDE_API": "starting"}, ""},
		{"a API abre antes da retenção", "nao mostra a ordem arrumacao, retencao, servindo", "[aurora-whisper-backup]", nil,
			strings.Join([]string{strings.Split(logDaAPIEmOrdem, "\n")[0], strings.Split(logDaAPIEmOrdem, "\n")[2], strings.Split(logDaAPIEmOrdem, "\n")[1]}, "\n") + "\n"},
		{"a API sem a retenção", "nao mostra a ordem", "[aurora-whisper-backup]", nil,
			strings.Split(logDaAPIEmOrdem, "\n")[0] + "\n" + strings.Split(logDaAPIEmOrdem, "\n")[2] + "\n"},
		{"o proxy confiável desligado", "o proxy confiavel esta desligado", "[aurora-whisper-backup]", nil,
			strings.Replace(logDaAPIEmOrdem, `"proxy_confiavel":true`, `"proxy_confiavel":false`, 1)},
		{"a saúde do job falha", "a saude do job de retencao falhou", "[aurora-whisper-backup]", map[string]string{"FALSO_JOB": "falha"}, ""},
		{"o backup não fica de pé", "o aurora-whisper-backup nao ficou de pe", "curl", map[string]string{"FALSO_ESTADO_BACKUP": "restarting"}, ""},
		{"o https não responde", "nao respondeu 200", "", map[string]string{"FALSO_HTTPS": "000"}, ""},
	} {
		t.Run(c.nome, func(t *testing.T) {
			t.Parallel()
			s := novoSubir(t)
			for k, v := range c.env {
				s.a.env[k] = v
			}
			if c.log != "" {
				p := filepath.Join(s.a.dir, "log-da-api.txt")
				if err := os.WriteFile(p, []byte(c.log), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			r := s.rodar("SIM\nSIM\n")
			if r.codigo != 1 || !strings.Contains(r.erro, "FALHOU") || !strings.Contains(r.erro, c.quero) {
				t.Fatalf("quero FALHOU com %q: %d %q", c.quero, r.codigo, r.tudo())
			}
			if c.naoChama != "" && s.a.indice(c.naoChama) >= 0 {
				t.Errorf("chamou %q depois da falha:\n%s", c.naoChama, strings.Join(s.a.chamadas(), "\n"))
			}
			if strings.Contains(r.tudo(), "OK: a nuvem esta no ar") {
				t.Error("disse que está no ar depois de falhar")
			}
			s.semSegredo(t, r)
		})
	}
}

func TestSubir_CasosQueSeguem(t *testing.T) {
	t.Parallel()
	t.Run("um config.env sem o DOMINIO ganha o dele", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		s.escreverConfig(t, strings.Replace(configDoGerar, "DOMINIO=aurora.exemplo.com.br\n", "", 1))
		r := s.rodar("SIM\n")
		if r.codigo != 0 {
			t.Fatalf("falhou: %d %q", r.codigo, r.tudo())
		}
		exigirContem(t, "o config.env", s.config(t), "DOMINIO=aurora.exemplo.com.br\n", "PROXY_CONFIAVEL=172.18.0.0/16\n")
	})
	t.Run("a rede web com IPv6 também avisa e usa a IPv4", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		s.a.env["FALSO_SUBREDES"] = "fd00:1::/64 172.18.0.0/16 "
		r := s.rodar("SIM\n")
		if r.codigo != 0 || !strings.Contains(r.saida, "AVISO: a rede web tem tambem IPv6") {
			t.Fatalf("quero o AVISO e seguir: %d %q", r.codigo, r.tudo())
		}
		exigirContem(t, "o config.env", s.config(t), "PROXY_CONFIAVEL=172.18.0.0/16\n")
	})
	t.Run("o PROXY_CONFIAVEL de antes é trocado, não repetido", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		s.escreverConfig(t, configDoGerar+"PROXY_CONFIAVEL=10.0.0.0/8\n")
		r := s.rodar("SIM\n")
		if r.codigo != 0 {
			t.Fatalf("falhou: %d %q", r.codigo, r.tudo())
		}
		if got := s.config(t); strings.Count(got, "PROXY_CONFIAVEL=") != 1 || !strings.Contains(got, "PROXY_CONFIAVEL=172.18.0.0/16\n") {
			t.Errorf("o config.env:\n%s", got)
		}
	})
	t.Run("sem o curl na VPS", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		_ = os.Remove(filepath.Join(s.a.falsos, "curl"))
		r := s.rodar("SIM\n")
		if r.codigo != 0 || !strings.Contains(r.saida, "AVISO: sem o curl na VPS") {
			t.Fatalf("quero o AVISO e seguir: %d %q", r.codigo, r.tudo())
		}
	})
	// O compose lê o ambiente antes do --env-file, e o COMPOSE_PROJECT_NAME
	// antes do name: do arquivo. Nada do que a sessão da VPS exportou chega a
	// ele: as AW_* do teste do PC, as do config.env (que é o conferido) e as
	// COMPOSE_*.
	t.Run("o ambiente da sessão não vale mais que o config.env", func(t *testing.T) {
		t.Parallel()
		s := novoSubir(t)
		for k, v := range map[string]string{
			"AW_REDE_DO_PROXY": "outra-rede", "DOMINIO": "outro-site.exemplo.com.br",
			"PUBLIC_BASE_URL": "https://outro-site.exemplo.com.br", "CONTAS_PERMITIDAS": "intruso@exemplo.com",
			"B2_BUCKET": "bucket-de-outro", "PROXY_CONFIAVEL": "0.0.0.0/0", "COMPOSE_PROJECT_NAME": "outro-projeto",
			"COMPOSE_FILE": "/outro/compose.yml", "COMPOSE_PROFILES": "outro", "COMPOSE_ENV_FILES": "/outro/.env",
		} {
			s.a.env[k] = v
		}
		s.a.falso("docker", `if [ "$1" = compose ]; then
  for v in `+strings.Join(variaveisDoAmbienteDoCompose, " ")+`; do if [ -n "${!v:-}" ]; then printf 'herdou %s\n' "$v" >> "$REGISTRO"; fi; done
fi
`+strings.TrimPrefix(dockerDoSubir, "\n"))
		r := s.rodar("SIM\n")
		if r.codigo != 0 {
			t.Fatalf("falhou: %d %q", r.codigo, r.tudo())
		}
		if s.a.chamou("herdou") {
			t.Errorf("o compose recebeu variáveis da sessão: %q", s.a.chamadas())
		}
		exigirContem(t, "a saída", r.saida, "OK: o compose conferido, com a regra Host(`aurora.exemplo.com.br`)")
	})
}

// variaveisDoAmbienteDoCompose são as que o subir.sh tira do ambiente: as que
// o compose.yml interpola e as COMPOSE_* que mudam o projeto, o arquivo, os
// perfis ou o env-file.
var variaveisDoAmbienteDoCompose = []string{
	"AW_PREFIXO", "AW_SEGREDOS", "AW_REDE_DO_PROXY", "PUBLIC_BASE_URL", "DOMINIO", "CONTAS_PERMITIDAS",
	"B2_BUCKET", "PROXY_CONFIAVEL", "COMPOSE_PROJECT_NAME", "COMPOSE_FILE", "COMPOSE_PROFILES", "COMPOSE_ENV_FILES",
}

// Toda variável que o compose.yml interpola está na lista (e o subir.sh a
// tira do ambiente): uma nova no compose sem o unset quebra este teste.
func TestSubir_TiraDoAmbienteTudoOQueOComposeLe(t *testing.T) {
	t.Parallel()
	lista := map[string]bool{}
	for _, v := range variaveisDoAmbienteDoCompose {
		lista[v] = true
	}
	for _, m := range regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)`).FindAllStringSubmatch(ler(t, composeYml), -1) {
		if !lista[m[1]] {
			t.Errorf("o compose.yml interpola %s, que o subir.sh não tira do ambiente", m[1])
		}
	}
	unsets := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^unset (.+)$`).FindAllStringSubmatch(codigo(ler(t, subirSh)), -1) {
		for _, v := range strings.Fields(m[1]) {
			unsets[v] = true
		}
	}
	for _, v := range variaveisDoAmbienteDoCompose {
		if !unsets[v] {
			t.Errorf("o subir.sh não tira %s do ambiente", v)
		}
	}
}

func TestSubir_NoTexto(t *testing.T) {
	t.Parallel()
	c := codigo(ler(t, subirSh))
	exigirContem(t, subirSh, c,
		"unset AW_PREFIXO AW_SEGREDOS AW_REDE_DO_PROXY",
		`docker compose --project-name "$PROJETO" --env-file "$CONFIG" -f "$COMPOSE" "$@"`,
		"timedatectl show -p NTPSynchronized --value",
		`docker network inspect "$REDE_DO_PROXY" --format '{{range .IPAM.Config}}{{.Subnet}} {{end}}'`,
		`gravar_no_config PROXY_CONFIAVEL "$sub"`,
		`bash "$DEPLOY/postgres/conferir-postgres.sh"`,
		"api_descartavel migrate -status", "api_descartavel migrate ||",
		`[ "$resposta" = SIM ]`,
		`'"lapides_criadas"'`, `'"msg":"servindo"'`, `'"proxy_confiavel":true'`,
		`docker exec "$CONTEINER_API" "$BINARIO_DA_API" healthcheck -retencao 2>&1`,
		`dc up -d --force-recreate "$CONTEINER_API"`,
	)
	// O JSON do Google de uma troca é conferido como o da primeira vez.
	for _, s := range []string{subirSh, gerarSegredos} {
		exigirContem(t, s, codigo(ler(t, s)),
			`RECEBIDO="$BASE/recebido/google-client.json"`, "MAX_JSON_DO_GOOGLE=65536",
			`grep -Fq '"installed"' "$RECEBIDO" || ! grep -Fq '"web"' "$RECEBIDO"`,
			`grep -Fq "\"https://$dominio/auth/google/callback\"" "$RECEBIDO"`)
	}
	// Nunca executa o config.env, nunca mexe no Traefik, nunca "compose run"
	// (levaria os rótulos), nunca apaga volume.
	exigirNaoContem(t, subirSh, c, "source ", ". \"$CONFIG\"", "docker restart", "docker stop traefik", "compose run",
		"down -v", "volume rm", "network rm", "network create", "network connect", "docker rm")
	// Os nomes batem com o compose e com os outros scripts.
	for _, n := range []string{"CONTEINER_POSTGRES", "CONTEINER_API", "CONTEINER_BACKUP", "REDE_INTERNA"} {
		if constanteDoScript(t, subirSh, n) != constanteDoScript(t, restaurarSh, n) {
			t.Errorf("%s do subir.sh e do restaurar.sh", n)
		}
	}
	if constanteDoScript(t, subirSh, "IMAGEM_API") != constanteDoScript(t, restaurarSh, "IMAGEM_API") ||
		constanteDoScript(t, subirSh, "IMAGEM_BACKUP") != constanteDoScript(t, restaurarSh, "IMAGEM_BACKUP") {
		t.Error("as imagens do subir.sh e do restaurar.sh")
	}
	raiz := compose(t)
	if constanteDoScript(t, subirSh, "PROJETO") != textoYAML(t, raiz, "name") {
		t.Error("o projeto do subir.sh é o name do compose")
	}
	if constanteDoScript(t, subirSh, "REDE_DO_PROXY") != padrao(textoYAML(t, raiz, "networks", "web", "name")) {
		t.Error("a rede do proxy do subir.sh é a web do compose")
	}
	var nomes []string
	for _, n := range chavesYAML(mapaYAML(t, raiz, "secrets")) {
		nomes = append(nomes, n)
	}
	if got := strings.Join(ordenar(strings.Fields(constanteDoScript(t, subirSh, "SEGREDOS_ESPERADOS"))), " "); got != strings.Join(nomes, " ") {
		t.Errorf("os segredos que o subir.sh confere (%s) são os do compose (%s)", got, strings.Join(nomes, " "))
	}
}
