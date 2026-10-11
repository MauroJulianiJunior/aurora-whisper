package deploy

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// O compose da nuvem (F11i; ADR-019, "Decisões da F11", D1 confirmada pelo
// Mauro em 09-out-2026): uma VPS, atrás do Traefik que já roda, com os
// rótulos no molde dos outros serviços do servidor. O agente não roda Docker:
// estes testes leem o deploy/compose.yml e conferem cada promessa, e que os
// nomes batem com os scripts da VPS, o Dockerfile e o gerar-segredos.sh.

const composeYml = "deploy/compose.yml"

const (
	svcPostgres = "aurora-whisper-postgres"
	svcAPI      = "aurora-whisper-api"
	svcBackup   = "aurora-whisper-backup"
)

func compose(t *testing.T) any {
	t.Helper()
	texto := ler(t, composeYml)
	exigirASCII(t, composeYml, texto)
	exigirLF(t, composeYml, texto)
	return lerYAML(t, composeYml, texto)
}

// padrao troca cada ${VAR:-valor} pelo valor: o que vale no servidor, onde as
// variáveis AW_* (do teste do PC) não existem.
func padrao(s string) string {
	return regexp.MustCompile(`\$\{[A-Z_]+:-([^}]*)\}`).ReplaceAllString(s, "$1")
}

// constanteDoScript devolve o valor de NOME='valor' (ou NOME="${X:-valor}")
// num script.
func constanteDoScript(t *testing.T, script, nome string) string {
	t.Helper()
	texto := ler(t, script)
	if m := regexp.MustCompile(`(?m)^` + nome + `='([^']*)'$`).FindStringSubmatch(texto); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`(?m)^(?:export )?` + nome + `="\$\{[A-Z_]+:-([^}]*)\}"$`).FindStringSubmatch(texto); m != nil {
		return m[1]
	}
	t.Fatalf("%s não define %s", script, nome)
	return ""
}

func ambiente(t *testing.T, raiz any, svc string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for k, v := range mapaYAML(t, raiz, "services", svc, "environment") {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s.environment.%s não é texto", svc, k)
		}
		out[k] = s
	}
	return out
}

func rotulos(t *testing.T, raiz any, svc string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if !temYAML(raiz, "services", svc, "labels") {
		return out
	}
	for _, l := range listaYAML(t, raiz, "services", svc, "labels") {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			t.Fatalf("%s: rótulo sem =: %s", svc, l)
		}
		if _, dup := out[k]; dup {
			t.Fatalf("%s: rótulo repetido: %s", svc, k)
		}
		out[k] = v
	}
	return out
}

func duracao(t *testing.T, s string) time.Duration {
	t.Helper()
	d, err := time.ParseDuration(s)
	if err != nil {
		t.Fatalf("duração %q: %v", s, err)
	}
	return d
}

func TestCompose_OsTresServicos(t *testing.T) {
	raiz := compose(t)
	if got := strings.Join(chavesYAML(raiz.(map[string]any)), " "); got != "name networks secrets services volumes" {
		t.Errorf("as chaves do compose: %s", got)
	}
	if textoYAML(t, raiz, "name") != "aurora-whisper" {
		t.Error("o projeto se chama aurora-whisper (o mesmo --project-name do subir.sh e dos comandos à mão na VPS)")
	}
	if got := strings.Join(chavesYAML(mapaYAML(t, raiz, "services")), " "); got != svcAPI+" "+svcBackup+" "+svcPostgres {
		t.Errorf("os serviços: %s", got)
	}
	// O nome fixo do contêiner: o mesmo dos scripts da VPS (o restaurar.sh, o
	// conferir-postgres.sh), e o stop-first (nunca dois serve de pé).
	for svc, nome := range map[string]string{
		svcPostgres: constanteDoScript(t, restaurarSh, "CONTEINER_POSTGRES"),
		svcAPI:      constanteDoScript(t, restaurarSh, "CONTEINER_API"),
		svcBackup:   constanteDoScript(t, restaurarSh, "CONTEINER_BACKUP"),
	} {
		cn := textoYAML(t, raiz, "services", svc, "container_name")
		if cn != "${AW_PREFIXO:-aurora-whisper}-"+strings.TrimPrefix(svc, "aurora-whisper-") || padrao(cn) != nome || svc != nome {
			t.Errorf("%s: container_name %q (no servidor, %q; o script usa %q)", svc, cn, padrao(cn), nome)
		}
		if textoYAML(t, raiz, "services", svc, "restart") != "unless-stopped" {
			t.Errorf("%s: restart unless-stopped", svc)
		}
	}
	exigirContem(t, conferirPostgre, ler(t, conferirPostgre), "CONTEINER_POSTGRES='"+svcPostgres+"'")
}

func TestCompose_NenhumaPortaNemSocketNemPrivilegio(t *testing.T) {
	raiz := compose(t)
	texto := ler(t, composeYml)
	exigirNaoContem(t, composeYml, codigo(texto), "docker.sock", "/var/run/docker", "privileged", "network_mode", "ports:")
	for svc, v := range mapaYAML(t, raiz, "services") {
		m := v.(map[string]any)
		for _, proibida := range []string{"ports", "expose", "network_mode", "privileged", "pid", "ipc", "cap_add",
			"devices", "userns_mode", "deploy", "scale", "extra_hosts", "build", "env_file", "user"} {
			if _, ok := m[proibida]; ok {
				t.Errorf("%s tem %s", svc, proibida)
			}
		}
		if !temYAML(raiz, "services", svc, "security_opt") ||
			strings.Join(listaYAML(t, raiz, "services", svc, "security_opt"), " ") != "no-new-privileges:true" {
			t.Errorf("%s: security_opt no-new-privileges:true", svc)
		}
		if temYAML(raiz, "services", svc, "volumes") {
			for _, vol := range listaYAML(t, raiz, "services", svc, "volumes") {
				if strings.HasPrefix(vol, "/") {
					t.Errorf("%s monta uma pasta do host: %s", svc, vol)
				}
			}
		}
	}
}

func TestCompose_Redes(t *testing.T) {
	raiz := compose(t)
	for svc, quero := range map[string]string{
		svcPostgres: "aurora-whisper-interna",
		svcAPI:      "aurora-whisper-interna web",
		svcBackup:   "aurora-whisper-interna aurora-whisper-saida",
	} {
		if got := strings.Join(ordenar(listaYAML(t, raiz, "services", svc, "networks")), " "); got != quero {
			t.Errorf("%s nas redes %q, quero %q", svc, got, quero)
		}
	}
	if got := strings.Join(chavesYAML(mapaYAML(t, raiz, "networks")), " "); got != "aurora-whisper-interna aurora-whisper-saida web" {
		t.Errorf("as redes: %s", got)
	}
	interna := mapaYAML(t, raiz, "networks", "aurora-whisper-interna")
	if interna["internal"] != "true" || padrao(interna["name"].(string)) != constanteDoScript(t, restaurarSh, "REDE_INTERNA") {
		t.Errorf("a rede interna: internal: true, com o nome que o restaurar.sh usa: %#v", interna)
	}
	if strings.Contains(interna["name"].(string), "web") {
		t.Error("a rede interna não é a do proxy")
	}
	saida := mapaYAML(t, raiz, "networks", "aurora-whisper-saida")
	if _, ok := saida["internal"]; ok || padrao(saida["name"].(string)) != "aurora-whisper-saida" {
		t.Errorf("a rede de saída do backup: %#v", saida)
	}
	web := mapaYAML(t, raiz, "networks", "web")
	if web["external"] != "true" || web["name"] != "${AW_REDE_DO_PROXY:-web}" || len(web) != 2 {
		t.Errorf("a rede web é a do Traefik, externa (não é nossa): %#v", web)
	}
}

// Os segredos: os arquivos do gerar-segredos.sh, cada serviço só com os seus.
func TestCompose_Segredos(t *testing.T) {
	raiz := compose(t)
	gerados := map[string]bool{"rclone.conf": true, "google-client.json": true}
	for _, m := range regexp.MustCompile(`(?m)^gravar ([a-z-]+) `).FindAllStringSubmatch(ler(t, gerarSegredos), -1) {
		gerados[m[1]] = true
	}
	top := mapaYAML(t, raiz, "secrets")
	if len(top) != len(gerados) {
		t.Errorf("o compose tem %d segredos e o gerar-segredos.sh grava %d", len(top), len(gerados))
	}
	for nome := range gerados {
		if !temYAML(raiz, "secrets", nome) {
			t.Errorf("falta o segredo %s", nome)
			continue
		}
		if f := textoYAML(t, raiz, "secrets", nome, "file"); f != "${AW_SEGREDOS:-../segredos}/"+nome {
			t.Errorf("o segredo %s vem de %q", nome, f)
		}
		if len(mapaYAML(t, raiz, "secrets", nome)) != 1 {
			t.Errorf("o segredo %s tem mais que o file", nome)
		}
	}
	for svc, quero := range map[string]string{
		svcPostgres: "postgres-aurora-senha postgres-backup-senha postgres-superusuario-senha",
		svcAPI:      "ai-keys-secret database-url device-token-pepper google-client.json session-secret",
		svcBackup:   "backup-chave-publica postgres-backup-senha rclone.conf",
	} {
		if got := strings.Join(ordenar(listaYAML(t, raiz, "services", svc, "secrets")), " "); got != quero {
			t.Errorf("%s monta %q, quero %q", svc, got, quero)
		}
	}
}

// Nenhum segredo por variável de ambiente (o docker inspect a mostra, e o
// Traefik lê o Config.Env de todo contêiner): só os caminhos *_FILE, e cada um
// aponta para um segredo montado no próprio serviço.
func TestCompose_SemSegredoNoAmbiente(t *testing.T) {
	raiz := compose(t)
	sensivel := regexp.MustCompile(`PASSWORD|SECRET|PEPPER|KEY|TOKEN|SENHA|DATABASE_URL|CHAVE`)
	for _, svc := range []string{svcPostgres, svcAPI, svcBackup} {
		montados := map[string]bool{}
		for _, s := range listaYAML(t, raiz, "services", svc, "secrets") {
			montados["/run/secrets/"+s] = true
		}
		for k, v := range ambiente(t, raiz, svc) {
			if strings.Contains(v, "postgres://") || regexp.MustCompile(`[0-9a-f]{32}`).MatchString(v) {
				t.Errorf("%s.%s parece um segredo", svc, k)
			}
			if !sensivel.MatchString(k) {
				continue
			}
			if !strings.HasSuffix(k, "_FILE") {
				t.Errorf("%s: %s pelo ambiente; só o caminho (%s_FILE)", svc, k, k)
				continue
			}
			if !montados[v] {
				t.Errorf("%s: %s=%s não é um segredo montado nele", svc, k, v)
			}
		}
	}
}

func TestCompose_Postgres(t *testing.T) {
	raiz := compose(t)
	img := textoYAML(t, raiz, "services", svcPostgres, "image")
	m := regexp.MustCompile(`^postgres:(18\.\d+)-[a-z]+$`).FindStringSubmatch(img)
	if m == nil {
		t.Fatalf("o Postgres 18 em versão exata: %q", img)
	}
	// O pg_dump do backup é da mesma versão do servidor.
	if !strings.Contains(ler(t, backupImagem), "FROM postgres:"+m[1]+"-alpine\n") {
		t.Errorf("a imagem do backup tem de ser do mesmo Postgres (%s)", m[1])
	}
	if got := strings.Join(listaYAML(t, raiz, "services", svcPostgres, "command"), " "); got != "postgres"+
		" -c log_error_verbosity=terse -c log_statement=none -c log_min_duration_statement=-1"+
		" -c log_parameter_max_length_on_error=0" {
		t.Errorf("os cinco ajustes de log da ADR (o auto_explain fora): %s", got)
	}
	env := ambiente(t, raiz, svcPostgres)
	init := ler(t, "deploy/postgres/init/10-papel-da-aplicacao.sh")
	if env["POSTGRES_USER"] != "postgres" || env["POSTGRES_DB"] == "aurora" || env["POSTGRES_DB"] == "" {
		t.Errorf("o superusuário é o postgres, e o banco da imagem não é o aurora: %v", env)
	}
	if env["POSTGRES_PASSWORD_FILE"] != "/run/secrets/postgres-superusuario-senha" {
		t.Error("POSTGRES_PASSWORD_FILE")
	}
	exigirContem(t, "o init", init,
		`"${AURORA_SENHA_FILE:-`+env["AURORA_SENHA_FILE"]+`}"`,
		`"${AURORA_BACKUP_SENHA_FILE:-`+env["AURORA_BACKUP_SENHA_FILE"]+`}"`)
	vols := strings.Join(listaYAML(t, raiz, "services", svcPostgres, "volumes"), " ")
	if vols != "aurora-whisper-postgres:/var/lib/postgresql ./postgres/init:/docker-entrypoint-initdb.d:ro" {
		t.Errorf("o volume do Postgres 18 e o init só leitura: %s", vols)
	}
	if got := strings.Join(listaYAML(t, raiz, "services", svcPostgres, "healthcheck", "test"), " "); got != "CMD pg_isready -q -h 127.0.0.1 -U postgres -d postgres" {
		t.Errorf("a saúde do Postgres pelo TCP (o servidor do init só escuta no socket): %s", got)
	}
	if textoYAML(t, raiz, "volumes", "aurora-whisper-postgres", "name") != "${AW_PREFIXO:-aurora-whisper}-postgres" {
		t.Error("o nome do volume do Postgres")
	}
}

func TestCompose_API(t *testing.T) {
	raiz := compose(t)
	api := func(c ...string) string { return textoYAML(t, raiz, append([]string{"services", svcAPI}, c...)...) }
	if api("image") != constanteDoScript(t, restaurarSh, "IMAGEM_API") || api("pull_policy") != "never" {
		t.Error("a imagem da API é a que veio do PC, nunca puxada de um registro")
	}
	if !strings.Contains(ler(t, "deploy/Dockerfile"), "docker build -f deploy/Dockerfile -t "+api("image")+" .") {
		t.Error("o Dockerfile constrói a imagem com o nome que o compose usa")
	}
	if strings.Join(listaYAML(t, raiz, "services", svcAPI, "command"), " ") != "serve" {
		t.Error("a API roda o serve")
	}
	env := ambiente(t, raiz, svcAPI)
	for k, v := range map[string]string{
		"HTTP_ADDR":                 "0.0.0.0:8080",
		"PAINEL_DIR":                "/painel",
		"LOG_LEVEL":                 "info",
		"DATABASE_URL_FILE":         "/run/secrets/database-url",
		"SESSION_SECRET_FILE":       "/run/secrets/session-secret",
		"DEVICE_TOKEN_PEPPER_FILE":  "/run/secrets/device-token-pepper",
		"GOOGLE_CLIENT_SECRET_FILE": "/run/secrets/google-client.json",
		"AI_KEYS_SECRET_FILE":       "/run/secrets/ai-keys-secret",
	} {
		if env[k] != v {
			t.Errorf("%s=%q, quero %q", k, env[k], v)
		}
	}
	for _, k := range []string{"PUBLIC_BASE_URL", "CONTAS_PERMITIDAS", "PROXY_CONFIAVEL"} {
		if !strings.HasPrefix(env[k], "${"+k+":?") {
			t.Errorf("%s vem do config.env, obrigatória (${%s:?...}): %q", k, k, env[k])
		}
	}
	for k := range env {
		switch k {
		case "PUBLIC_BASE_URL", "CONTAS_PERMITIDAS", "PROXY_CONFIAVEL", "HTTP_ADDR", "PAINEL_DIR", "LOG_LEVEL",
			"DATABASE_URL_FILE", "SESSION_SECRET_FILE", "DEVICE_TOKEN_PEPPER_FILE", "GOOGLE_CLIENT_SECRET_FILE",
			"AI_KEYS_SECRET_FILE", "GOMEMLIMIT":
		default:
			t.Errorf("variável inesperada na API: %s", k)
		}
	}
	if api("read_only") != "true" || strings.Join(listaYAML(t, raiz, "services", svcAPI, "cap_drop"), " ") != "ALL" {
		t.Error("a API read_only e sem capacidades")
	}
	if api("depends_on", svcPostgres, "condition") != "service_healthy" {
		t.Error("a API espera o Postgres saudável")
	}
	// O healthcheck é só o de vida: o próprio binário (a imagem não tem
	// shell), SEM o -retencao. O provider docker do Traefik tira do
	// roteamento o contêiner que não está healthy, e a saúde do job não pode
	// derrubar o endereço (escolhas menores 4 e 7 da F11). O job se confere
	// por docker exec, no subir.sh. O start_period cobre a primeira rodada,
	// como o da imagem.
	bin := regexp.MustCompile(`ENTRYPOINT \["([^"]+)"\]`).FindStringSubmatch(ler(t, "deploy/Dockerfile"))
	if bin == nil {
		t.Fatal("o ENTRYPOINT do Dockerfile")
	}
	if got := strings.Join(listaYAML(t, raiz, "services", svcAPI, "healthcheck", "test"), " "); got != "CMD "+bin[1]+" healthcheck" {
		t.Errorf("o healthcheck da API é só o de vida (o Traefik não roteia um contêiner unhealthy): %s", got)
	}
	vida := regexp.MustCompile(`(?s)HEALTHCHECK [^\n]*\\\n\s*CMD \["([^"]+)", "healthcheck"\]`).FindStringSubmatch(ler(t, "deploy/Dockerfile"))
	if vida == nil || vida[1] != bin[1] {
		t.Error("o HEALTHCHECK da imagem é o mesmo de vida")
	}
	exigirNaoContem(t, composeYml, codigo(ler(t, composeYml)), "-retencao")
	if constanteDoScript(t, subirSh, "BINARIO_DA_API") != bin[1] {
		t.Error("o subir.sh confere o job pelo binário do ENTRYPOINT")
	}
	inicio := regexp.MustCompile(`--start-period=(\S+)`).FindStringSubmatch(ler(t, "deploy/Dockerfile"))
	if inicio == nil || duracao(t, api("healthcheck", "start_period")) < duracao(t, inicio[1]) {
		t.Error("o start_period da API cobre a primeira rodada (pelo menos o da imagem)")
	}
	// O stop_grace_period tem teste próprio (TestCompose_DesligamentoDaAPI):
	// ele cobre também a espera dos ditados em andamento.
	if api("mem_limit") != "256m" {
		t.Error("a API com ~256 MB")
	}
	if !strings.HasPrefix(env["GOMEMLIMIT"], "200") {
		t.Error("o GOMEMLIMIT abaixo do limite do contêiner")
	}
}

// A chave de cifra das chaves da IA do celular (ADR-021, decisão O e C5c): o
// segredo é declarado como os outros e montado SÓ na API, que lê o caminho por
// AI_KEYS_SECRET_FILE. Com o arquivo as rotas do ditado no celular existem; sem
// ele o servidor as deixaria em 404, e por isso o compose o exige. Os três
// lugares que sabem o nome do arquivo (o gerar-segredos.sh de uma VPS nova, o
// criar-chave-de-cifra.sh de uma VPS que já está no ar e o SEGREDOS_ESPERADOS
// do subir.sh) têm de dizer o mesmo que o compose.
func TestCompose_ChaveDeCifraDaIA(t *testing.T) {
	raiz := compose(t)
	const nome = "ai-keys-secret"
	if f := textoYAML(t, raiz, "secrets", nome, "file"); f != "${AW_SEGREDOS:-../segredos}/"+nome {
		t.Errorf("o segredo %s vem de %q", nome, f)
	}
	for _, svc := range []string{svcPostgres, svcBackup} {
		for _, s := range listaYAML(t, raiz, "services", svc, "secrets") {
			if s == nome {
				t.Errorf("%s monta a chave de cifra: só a API a lê", svc)
			}
		}
	}
	montada := false
	for _, s := range listaYAML(t, raiz, "services", svcAPI, "secrets") {
		montada = montada || s == nome
	}
	if !montada {
		t.Errorf("a API não monta o segredo %s", nome)
	}
	if got := ambiente(t, raiz, svcAPI)["AI_KEYS_SECRET_FILE"]; got != "/run/secrets/"+nome {
		t.Errorf("AI_KEYS_SECRET_FILE=%q, quero /run/secrets/%s", got, nome)
	}
	// O servidor lê esta variável (com o sufixo _FILE) e cria o cofre com ela.
	exigirContem(t, "cmd/server/config.go", ler(t, "cmd/server/config.go"), `envAIKeysSecret = "AI_KEYS_SECRET"`)
	// Os três lugares que falam do arquivo dizem o mesmo nome.
	if got := constanteDoScript(t, criarChaveDeCifra, "NOME"); got != nome {
		t.Errorf("o criar-chave-de-cifra.sh grava %q, o compose monta %q", got, nome)
	}
	exigirContem(t, gerarSegredos, ler(t, gerarSegredos), "gravar "+nome+` "$chave_de_cifra"`)
	achou := false
	for _, f := range strings.Fields(constanteDoScript(t, subirSh, "SEGREDOS_ESPERADOS")) {
		achou = achou || f == nome
	}
	if !achou {
		t.Errorf("o subir.sh não exige o %s", nome)
	}
}

// A API não manda o áudio nem a chave decifrada para o disco (ADR-021, decisão
// L): sem swap no contêiner (memswap_limit igual ao mem_limit) e sem core dump
// (ulimits core 0). O memswap_limit só vale se o kernel da VPS contar a swap
// no cgroup: quem confere isso é a leitura do roteiro, antes do primeiro
// áudio (docs/deploy-f11.md, passo 10.4); aqui se confere o que o compose pede.
func TestCompose_APISemSwapNemCoreDump(t *testing.T) {
	raiz := compose(t)
	api := func(c ...string) string { return textoYAML(t, raiz, append([]string{"services", svcAPI}, c...)...) }
	mem, swap := api("mem_limit"), api("memswap_limit")
	if !regexp.MustCompile(`^\d+m$`).MatchString(mem) || swap != mem {
		t.Errorf("memswap_limit %q, quero igual ao mem_limit (%q): memória e swap somam o mesmo teto, e a swap fica em 0", swap, mem)
	}
	ul := mapaYAML(t, raiz, "services", svcAPI, "ulimits")
	if len(ul) != 1 || ul["core"] != "0" {
		t.Errorf("ulimits da API %v, quero só core: 0", ul)
	}
	// O GOMEMLIMIT segura o heap abaixo do teto do contêiner.
	if !strings.HasPrefix(ambiente(t, raiz, svcAPI)["GOMEMLIMIT"], "200") {
		t.Error("o GOMEMLIMIT abaixo do limite do contêiner")
	}
	// A imagem não grava core por conta própria: o GOTRACEBACK=crash (que
	// pediria o core) não está no compose.
	exigirNaoContem(t, composeYml, codigo(ler(t, composeYml)), "GOTRACEBACK")
}

// O desligamento da API (ADR-021, escolha menor 22): o serve espera o Shutdown
// (15 s) e, depois, os ditados em andamento (70 s). O stop_grace_period do
// compose tem de cobrir os dois, senão o Docker mata o processo no meio de um
// ditado que o provedor já cobrou. Os dois números vêm do cmd/server/serve.go.
func TestCompose_DesligamentoDaAPI(t *testing.T) {
	raiz := compose(t)
	serve := ler(t, "cmd/server/serve.go")
	segundos := func(nome string) time.Duration {
		m := regexp.MustCompile(`(?m)\b` + nome + `\s*=\s*(\d+) \* time\.Second$`).FindStringSubmatch(serve)
		if m == nil {
			t.Fatalf("o cmd/server/serve.go não define %s em segundos", nome)
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		return time.Duration(n) * time.Second
	}
	desligar, ditados := segundos("shutdownTimeout"), segundos("esperaDosDitados")
	grace := duracao(t, textoYAML(t, raiz, "services", svcAPI, "stop_grace_period"))
	if grace != 90*time.Second {
		t.Errorf("stop_grace_period %v, quero 90 s (a escolha menor 22 da ADR-021)", grace)
	}
	if grace < desligar+ditados {
		t.Errorf("stop_grace_period %v não cobre o Shutdown (%v) mais a espera dos ditados (%v)", grace, desligar, ditados)
	}
}

// Os rótulos do Traefik, no molde dos outros serviços do servidor (a rede web,
// o entrypoint websecure, o certresolver letsencrypt; o Traefik com
// exposedbydefault=false), sem um middleware de cabeçalhos e com o log de
// acesso desligado só no nosso router.
func TestCompose_RotulosDoTraefik(t *testing.T) {
	raiz := compose(t)
	r := rotulos(t, raiz, svcAPI)
	quero := map[string]string{
		"traefik.enable":                                                    "true",
		"traefik.docker.network":                                            textoYAML(t, raiz, "networks", "web", "name"),
		"traefik.http.routers.aurora-whisper-api.rule":                      "Host(`${DOMINIO:?falta o DOMINIO no config.env}`)",
		"traefik.http.routers.aurora-whisper-api.entrypoints":               "websecure",
		"traefik.http.routers.aurora-whisper-api.tls.certresolver":          "letsencrypt",
		"traefik.http.routers.aurora-whisper-api.service":                   "aurora-whisper-api",
		"traefik.http.routers.aurora-whisper-api.observability.accesslogs":  "false",
		"traefik.http.services.aurora-whisper-api.loadbalancer.server.port": "8080",
	}
	for k, v := range quero {
		if r[k] != v {
			t.Errorf("rótulo %s=%q, quero %q", k, r[k], v)
		}
	}
	for k := range r {
		if _, ok := quero[k]; !ok {
			t.Errorf("rótulo a mais: %s", k)
		}
	}
	// A porta do serviço do Traefik é a do HTTP_ADDR.
	if !strings.HasSuffix(ambiente(t, raiz, svcAPI)["HTTP_ADDR"], ":"+r["traefik.http.services.aurora-whisper-api.loadbalancer.server.port"]) {
		t.Error("a porta do rótulo e a do HTTP_ADDR")
	}
	exigirNaoContem(t, composeYml, ler(t, composeYml), "middlewares", "sec-headers=", "accesslogs=true")
	for _, svc := range []string{svcPostgres, svcBackup} {
		if len(rotulos(t, raiz, svc)) != 0 {
			t.Errorf("%s não tem rótulo do Traefik", svc)
		}
	}
}

func TestCompose_Backup(t *testing.T) {
	raiz := compose(t)
	b := func(c ...string) string { return textoYAML(t, raiz, append([]string{"services", svcBackup}, c...)...) }
	if b("image") != constanteDoScript(t, restaurarSh, "IMAGEM_BACKUP") || b("pull_policy") != "never" {
		t.Error("a imagem do backup é a que veio do PC")
	}
	if strings.Join(listaYAML(t, raiz, "services", svcBackup, "command"), " ") != "agendar-backup.sh" {
		t.Error("o comando do backup é o agendador")
	}
	if got := strings.Join(listaYAML(t, raiz, "services", svcBackup, "healthcheck", "test"), " "); got != "CMD agendar-backup.sh saude" {
		t.Errorf("a saúde do backup: %s", got)
	}
	env := ambiente(t, raiz, svcBackup)
	if !strings.HasPrefix(env["B2_BUCKET"], "${B2_BUCKET:?") || env["AW_HORA_DO_BACKUP"] != "06" || len(env) != 2 {
		t.Errorf("o ambiente do backup: o bucket do config.env e a hora (06 UTC, 03:00 em São Paulo): %v", env)
	}
	// Os caminhos que o backup.sh usa por padrão são os montados aqui.
	pasta := constanteDoScript(t, backupSh, "PASTA")
	if got := strings.Join(listaYAML(t, raiz, "services", svcBackup, "volumes"), " "); got != "aurora-whisper-backup:"+pasta {
		t.Errorf("o volume do backup em %s: %s", pasta, got)
	}
	for _, v := range []string{"SENHA_FILE", "CHAVE_PUBLICA"} {
		caminho := constanteDoScript(t, backupSh, v)
		achou := false
		for _, s := range listaYAML(t, raiz, "services", svcBackup, "secrets") {
			achou = achou || "/run/secrets/"+s == caminho
		}
		if !achou {
			t.Errorf("o backup.sh lê %s em %s, que o compose não monta", v, caminho)
		}
	}
	exigirContem(t, backupSh, ler(t, backupSh), `export RCLONE_CONFIG="${RCLONE_CONFIG:-/run/secrets/rclone.conf}"`,
		`export PGHOST="${PGHOST:-`+svcPostgres+`}"`)
	if constanteDoScript(t, gerarSegredos, "HOST_DO_POSTGRES") != svcPostgres {
		t.Error("a DATABASE_URL do gerar-segredos.sh aponta para o serviço do Postgres")
	}
	if b("mem_limit") != "128m" || strings.Join(listaYAML(t, raiz, "services", svcBackup, "cap_drop"), " ") != "ALL" {
		t.Error("o backup pequeno e sem capacidades")
	}
}

func TestCompose_LimitesELogDeTodos(t *testing.T) {
	raiz := compose(t)
	for _, svc := range []string{svcPostgres, svcAPI, svcBackup} {
		s := func(c ...string) string { return textoYAML(t, raiz, append([]string{"services", svc}, c...)...) }
		if !regexp.MustCompile(`^\d+m$`).MatchString(s("mem_limit")) || s("cpus") == "" {
			t.Errorf("%s sem limite de memória ou de CPU", svc)
		}
		// O teto não dá a vez aos outros serviços do servidor (os três somam
		// mais que os núcleos de uma VPS pequena); o peso baixo dá. Abaixo do
		// padrão (1024).
		if n, err := strconv.Atoi(s("cpu_shares")); err != nil || n < 2 || n > 256 {
			t.Errorf("%s: o peso de CPU (cpu_shares) baixo, de 2 a 256, para os outros serviços do servidor ganharem na disputa: %q", svc, s("cpu_shares"))
		}
		if s("logging", "driver") != "json-file" || s("logging", "options", "max-size") != "10m" || s("logging", "options", "max-file") != "3" {
			t.Errorf("%s: o log do Docker com rotação", svc)
		}
		if duracao(t, s("stop_grace_period")) < 20*time.Second {
			t.Errorf("%s: stop_grace_period", svc)
		}
		if !temYAML(raiz, "services", svc, "healthcheck", "test") {
			t.Errorf("%s sem healthcheck", svc)
		}
	}
}
