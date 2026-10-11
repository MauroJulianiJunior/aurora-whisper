package deploy

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A imagem do servidor (deploy/Dockerfile) e o .dockerignore (F11e). O build
// de verdade é o do Mauro, no teste do PC (Parte 2): estes testes leem os dois
// arquivos e conferem as promessas da ADR-019.

// instrucao é uma instrução do Dockerfile, com as linhas de continuação
// juntadas e sem os comentários.
type instrucao struct {
	cmd  string // FROM, RUN, COPY... em maiúsculas
	args string
}

func instrucoes(texto string) []instrucao {
	var out []instrucao
	var atual strings.Builder
	for _, l := range strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(l)
		if atual.Len() == 0 && (s == "" || strings.HasPrefix(s, "#")) {
			continue
		}
		if strings.HasSuffix(s, "\\") {
			atual.WriteString(strings.TrimSuffix(s, "\\"))
			atual.WriteByte(' ')
			continue
		}
		atual.WriteString(s)
		inteira := strings.TrimSpace(atual.String())
		atual.Reset()
		cmd, args, _ := strings.Cut(inteira, " ")
		out = append(out, instrucao{strings.ToUpper(cmd), strings.TrimSpace(args)})
	}
	return out
}

func dockerfile(t *testing.T) []instrucao {
	t.Helper()
	return instrucoes(ler(t, "deploy/Dockerfile"))
}

// estagios agrupa as instruções por FROM, na ordem.
func estagios(ins []instrucao) [][]instrucao {
	var out [][]instrucao
	for _, i := range ins {
		if i.cmd == "FROM" {
			out = append(out, nil)
		}
		if len(out) > 0 {
			out[len(out)-1] = append(out[len(out)-1], i)
		}
	}
	return out
}

func TestImagem_SoASCIIESemCRLF(t *testing.T) {
	texto := ler(t, "deploy/Dockerfile")
	exigirASCII(t, "deploy/Dockerfile", texto)
	exigirLF(t, "deploy/Dockerfile", texto)
}

// versoesDoRepositorio lê as versões que o repositório fixa: o Go do go.mod, o
// Node e o pnpm do CI (o pnpm exato está no comentário do passo, que diz qual
// gerou o web/pnpm-lock.yaml).
func versoesDoRepositorio(t *testing.T) (goVer, nodeMajor, pnpmVer string) {
	t.Helper()
	m := regexp.MustCompile(`(?m)^go (\d+\.\d+\.\d+)\s*$`).FindStringSubmatch(ler(t, "go.mod"))
	if m == nil {
		t.Fatal("go.mod: não achei a linha go X.Y.Z")
	}
	goVer = m[1]
	ci := ler(t, ".github/workflows/ci.yml")
	n := regexp.MustCompile(`node-version:\s*'(\d+)'`).FindStringSubmatch(ci)
	if n == nil {
		t.Fatal("ci.yml: não achei o node-version")
	}
	nodeMajor = n[1]
	p := regexp.MustCompile(`pnpm-lock\.yaml \((\d+\.\d+\.\d+)\)`).FindStringSubmatch(ci)
	if p == nil {
		t.Fatal("ci.yml: não achei o pnpm exato (o comentário do Setup pnpm)")
	}
	pnpmVer = p[1]
	if !strings.HasPrefix(goVer, strings.TrimSuffix(regexp.MustCompile(`go-version:\s*'([\d.]+)\.x'`).FindStringSubmatch(ci)[1], ".x")+".") {
		t.Fatalf("o go.mod (%s) e o go-version do CI não batem", goVer)
	}
	return goVer, nodeMajor, pnpmVer
}

func TestImagem_TresEstagiosComAsVersoesDoRepositorio(t *testing.T) {
	goVer, nodeMajor, pnpmVer := versoesDoRepositorio(t)
	es := estagios(dockerfile(t))
	if len(es) != 3 {
		t.Fatalf("a imagem deveria ter 3 estágios (painel, servidor, final), tem %d", len(es))
	}
	painel, servidor := es[0][0].args, es[1][0].args
	if m := regexp.MustCompile(`^node:(\d+)\.\d+\.\d+-slim AS painel$`).FindStringSubmatch(painel); m == nil || m[1] != nodeMajor {
		t.Errorf("o estágio do painel deveria ser node:%s.X.Y-slim AS painel (versão exata), é %q", nodeMajor, painel)
	}
	if want := "golang:" + goVer + " AS servidor"; servidor != want {
		t.Errorf("o estágio do servidor deveria ser %q (o Go do go.mod), é %q", want, servidor)
	}
	texto := ler(t, "deploy/Dockerfile")
	if !strings.Contains(texto, "npm install --global pnpm@"+pnpmVer+"\n") {
		t.Errorf("o pnpm deveria ser o %s, o que gerou o web/pnpm-lock.yaml", pnpmVer)
	}
	for _, e := range es {
		img := strings.Fields(e[0].args)[0]
		if strings.HasSuffix(img, ":latest") || !strings.Contains(img, ":") {
			t.Errorf("imagem sem versão fixa: %q", img)
		}
	}
}

func TestImagem_PainelDaNuvem(t *testing.T) {
	es := estagios(dockerfile(t))
	var runs []string
	for _, i := range es[0] {
		if i.cmd == "RUN" {
			runs = append(runs, i.args)
		}
	}
	want := []string{"npm install --global pnpm@", "pnpm install --frozen-lockfile", "pnpm build:nuvem"}
	if len(runs) != len(want) {
		t.Fatalf("o estágio do painel deveria ter %d RUN, tem %d: %q", len(want), len(runs), runs)
	}
	for i, w := range want {
		if !strings.HasPrefix(runs[i], w) {
			t.Errorf("RUN %d do painel: quero %q, achei %q", i+1, w, runs[i])
		}
	}
	// "pnpm build" (sem :nuvem) escreve o painel do PC em ../bin/painel.
	if regexp.MustCompile(`pnpm (run )?build(\s|$)`).MatchString(ler(t, "deploy/Dockerfile")) {
		t.Error("o build do painel na imagem é o pnpm build:nuvem, nunca o pnpm build")
	}
}

func TestImagem_ServidorEstaticoSemTags(t *testing.T) {
	es := estagios(dockerfile(t))
	var build string
	cgo := false
	for _, i := range es[1] {
		if i.cmd == "RUN" && strings.HasPrefix(i.args, "go build") {
			build = i.args
		}
		if i.cmd == "ENV" && strings.Contains(i.args, "CGO_ENABLED=0") {
			cgo = true
		}
	}
	if build == "" {
		t.Fatal("o estágio do servidor não tem o go build")
	}
	if !cgo && !strings.Contains(build, "CGO_ENABLED=0") {
		t.Error("o build tem de ser com CGO_ENABLED=0 (binário estático na distroless static)")
	}
	exigirContem(t, "o go build", build, "-trimpath", "./cmd/server")
	// O login de teste só existe com a tag logindeteste: nenhum -tags, em
	// lugar nenhum do Dockerfile, nem no GOFLAGS.
	texto := codigo(ler(t, "deploy/Dockerfile"))
	exigirNaoContem(t, "deploy/Dockerfile", texto, "-tags", "logindeteste", "GOFLAGS")
}

func TestImagem_FinalDistrolessNonrootComHealthcheck(t *testing.T) {
	es := estagios(dockerfile(t))
	final := es[2]
	if !regexp.MustCompile(`^gcr\.io/distroless/static-debian\d+:nonroot(@sha256:[0-9a-f]{64})?$`).MatchString(final[0].args) {
		t.Errorf("a imagem final deveria ser a distroless static em nonroot, é %q", final[0].args)
	}
	achou := map[string]string{}
	for _, i := range final {
		achou[i.cmd] = i.args
	}
	if u := achou["USER"]; u != "65532:65532" && u != "nonroot:nonroot" && u != "nonroot" {
		t.Errorf("USER deveria ser o nonroot (65532), é %q", u)
	}
	hc := achou["HEALTHCHECK"]
	if !regexp.MustCompile(`--start-period=\d+[ms]`).MatchString(hc) {
		t.Errorf("o HEALTHCHECK precisa de um start-period que cubra a primeira rodada da retenção: %q", hc)
	}
	if !strings.Contains(hc, `CMD ["/usr/local/bin/aurora-whisper-server", "healthcheck"]`) {
		t.Errorf("o HEALTHCHECK é o subcomando healthcheck, em formato exec (a imagem não tem shell): %q", hc)
	}
	if strings.Contains(hc, "-retencao") {
		t.Error("o HEALTHCHECK da imagem é o de vida; o -retencao roda por docker exec (o subir.sh), nunca como healthcheck")
	}
	if achou["ENTRYPOINT"] != `["/usr/local/bin/aurora-whisper-server"]` || achou["CMD"] != `["serve"]` {
		t.Errorf("ENTRYPOINT e CMD deveriam ser o binário e serve: %q %q", achou["ENTRYPOINT"], achou["CMD"])
	}
	env := achou["ENV"]
	exigirContem(t, "o ENV da imagem final", env, "HTTP_ADDR=0.0.0.0:8080", "PAINEL_DIR=/painel", "LOG_LEVEL=info")
	texto := ler(t, "deploy/Dockerfile")
	exigirContem(t, "deploy/Dockerfile", texto,
		"COPY --from=servidor /out/aurora-whisper-server /usr/local/bin/aurora-whisper-server",
		"COPY --from=painel /src/web/dist-nuvem/ /painel/")
}

func TestImagem_NadaPerigoso(t *testing.T) {
	// "# syntax=docker/dockerfile:1" baixaria o frontend do build por uma tag
	// que anda; o frontend que vem no Docker basta.
	if strings.Contains(ler(t, "deploy/Dockerfile"), "# syntax=") {
		t.Error("deploy/Dockerfile não usa # syntax= (o frontend por tag que anda)")
	}
	for _, i := range dockerfile(t) {
		switch i.cmd {
		case "ADD":
			t.Errorf("ADD não é usado (baixa URL e abre tar sem avisar): %q", i.args)
		case "COPY":
			f := strings.Fields(i.args)
			if len(f) >= 2 && !strings.HasPrefix(f[0], "--from=") {
				for _, src := range f[:len(f)-1] {
					if src == "." || src == "./" || src == "*" {
						t.Errorf("COPY do contexto inteiro: %q", i.args)
					}
				}
			}
		case "ENV", "ARG", "LABEL":
			for _, nome := range []string{"PASSWORD", "SECRET", "PEPPER", "TOKEN", "DATABASE_URL=", "API_KEY", "CLIENT_SECRET"} {
				if strings.Contains(strings.ToUpper(i.args), nome) {
					t.Errorf("%s com nome de segredo (%s): %q", i.cmd, nome, i.args)
				}
			}
		case "RUN":
			if regexp.MustCompile(`(curl|wget)[^|]*\|\s*(ba)?sh`).MatchString(i.args) {
				t.Errorf("baixar e rodar script: %q", i.args)
			}
		}
	}
}

// --- o .dockerignore ---------------------------------------------------------

// regraIgnore é uma linha do .dockerignore, compilada como o Docker compila
// (github.com/moby/patternmatcher): * não atravessa /, ** atravessa, ? é um
// caractere; o padrão é limpo (path.Clean, sem a / do começo).
type regraIgnore struct {
	padrao  string
	excecao bool
	re      *regexp.Regexp
}

func compilarPadrao(p string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				i++
				if i+1 < len(p) && p[i+1] == '/' {
					i++
				}
				if i+1 >= len(p) {
					b.WriteString(".*")
				} else {
					b.WriteString("(.*/)?")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func regrasDoDockerignore(t *testing.T) []regraIgnore {
	t.Helper()
	var out []regraIgnore
	for _, l := range strings.Split(strings.ReplaceAll(ler(t, ".dockerignore"), "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(l)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		exc := strings.HasPrefix(s, "!")
		if exc {
			s = strings.TrimSpace(s[1:])
		}
		s = strings.TrimPrefix(path.Clean(s), "/")
		out = append(out, regraIgnore{s, exc, compilarPadrao(s)})
	}
	return out
}

// ignorado diz se o arquivo (relativo à raiz, com barras) fica fora do
// contexto: vale a última regra que casa com ele ou com uma pasta acima dele.
func ignorado(regras []regraIgnore, arquivo string) bool {
	partes := strings.Split(arquivo, "/")
	fora := false
	for _, r := range regras {
		casa := r.re.MatchString(arquivo)
		for i := 1; !casa && i < len(partes); i++ {
			casa = r.re.MatchString(strings.Join(partes[:i], "/"))
		}
		if casa {
			fora = !r.excecao
		}
	}
	return fora
}

func TestDockerignore_ListaDePermissao(t *testing.T) {
	regras := regrasDoDockerignore(t)
	if len(regras) == 0 || regras[0].padrao != "*" || regras[0].excecao {
		t.Fatal("a primeira regra do .dockerignore tem de ser *: é uma lista de permissão")
	}
	var permitidos []string
	for _, r := range regras {
		if r.excecao {
			permitidos = append(permitidos, r.padrao)
		}
	}
	want := []string{"cmd", "internal", "migrations", "web", "go.mod", "go.sum"}
	if strings.Join(permitidos, " ") != strings.Join(want, " ") {
		t.Errorf("o que entra deveria ser só %q, é %q", want, permitidos)
	}
	for _, fora := range []string{
		"bin/historico.db", "bin/aurora-whisper.exe", "cmd/agent/agent.exe", "cmd/server/server.test", "bin/painel/index.html",
		"models/ggml-large-v3-turbo-q5_0.bin", "models/x.gguf",
		"dist/Setup.exe", "thirdparty/whisper/main.exe",
		"web/node_modules/vite/package.json", "web/dist/index.html", "internal/x/node_modules/y/package.json",
		"web/dist-nuvem/index.html", "web/dist-nuvem/assets/x.js", "web/.vite/x",
		"internal/storage/testdata/historico.db", "internal/x/historico.db-wal",
		"cmd/server/.env", "internal/x/.env.local", ".env", ".env.local", "deploy/Dockerfile",
		"deploy/scripts/gerar-segredos.sh", "docs/ai-spec/00-index.md",
		"scripts/postgres-de-teste.ps1", "testdata/x.wav", ".git/HEAD",
		".claude/worktrees/x/go.mod", "segredos/session-secret",
		"internal/x/aurora-whisper-backup.key", "web/x.dump.age",
		"config.env", "backup/aurora-whisper-20261009T030000Z.dump.age",
	} {
		if !ignorado(regras, fora) {
			t.Errorf("%s deveria ficar fora do contexto do build", fora)
		}
	}
	for _, dentro := range []string{
		"go.mod", "go.sum", "cmd/server/main.go", "internal/server/server.go",
		"internal/server/pagina/pagina.css", "migrations/0001_users_table.sql",
		"migrations/migrations.go", "web/package.json", "web/pnpm-lock.yaml",
		"web/index.html", "web/vite.config.ts", "web/src/nuvem/main.tsx",
		"web/pwa/sw.js", "web/public/favicon.svg",
	} {
		if ignorado(regras, dentro) {
			t.Errorf("%s deveria entrar no contexto do build", dentro)
		}
	}
}

// TestDockerignore_OBuildDoServidorTemTudoQuePrecisa: todo arquivo Go e todo
// arquivo embutido (go:embed) que o go build ./cmd/server usa no Linux entra
// no contexto. É o "go list" do próprio repositório, sem compilar nada.
func TestDockerignore_OBuildDoServidorTemTudoQuePrecisa(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("sem o go no PATH")
	}
	absRaiz, err := filepath.Abs(raiz)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBin, "list", "-deps", "-f",
		`{{if .Module}}{{if .Module.Main}}{{.Dir}}|{{join .GoFiles ","}}|{{join .EmbedFiles ","}}{{end}}{{end}}`,
		"./cmd/server")
	cmd.Dir = absRaiz
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	regras := regrasDoDockerignore(t)
	contados := 0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l == "" {
			continue
		}
		campos := strings.Split(l, "|")
		dir, err := filepath.Rel(absRaiz, campos[0])
		if err != nil {
			t.Fatal(err)
		}
		dir = filepath.ToSlash(dir)
		for _, lista := range campos[1:] {
			for _, f := range strings.Split(lista, ",") {
				if f == "" {
					continue
				}
				arq := dir + "/" + f
				contados++
				if ignorado(regras, arq) {
					t.Errorf("o build do servidor usa %s, que o .dockerignore deixa de fora", arq)
				}
			}
		}
	}
	if contados < 50 {
		t.Fatalf("o go list devolveu só %d arquivos: a conferência não valeu", contados)
	}
}

// TestDockerignore_ArvoreDoRepositorio anda pela árvore de verdade (no PC do
// Mauro, com o bin\, o dist\ e o web\node_modules): tudo o que entra é de uma
// das pastas permitidas e não é banco, executável, chave nem dump; e tudo o que
// o build do painel lê em web/ entra.
func TestDockerignore_ArvoreDoRepositorio(t *testing.T) {
	regras := regrasDoDockerignore(t)
	proibido := regexp.MustCompile(`(?i)(\.db|\.db-wal|\.db-shm|\.sqlite3?|\.exe|\.dll|\.key|\.pem|\.age|\.dump|\.gguf|\.onnx|\.wav)$|(^|/)\.env(\.|$)`)
	permitidaNoTopo := map[string]bool{"cmd": true, "internal": true, "migrations": true, "web": true, "go.mod": true, "go.sum": true}
	entraram := 0
	err := filepath.WalkDir(raiz, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(raiz, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fora := ignorado(regras, rel)
		if d.IsDir() {
			if fora {
				// Nenhuma exceção da lista reabre algo dentro de uma pasta que
				// ficou de fora (as exceções são todas do primeiro nível).
				return filepath.SkipDir
			}
			return nil
		}
		dentroDoWeb := strings.HasPrefix(rel, "web/") &&
			!regexp.MustCompile(`^web/(node_modules|dist[^/]*|\.vite)(/|$)`).MatchString(rel)
		if dentroDoWeb && fora {
			t.Errorf("%s fica de fora, mas o build do painel o lê", rel)
		}
		if fora {
			return nil
		}
		entraram++
		topo, _, _ := strings.Cut(rel, "/")
		if !permitidaNoTopo[topo] {
			t.Errorf("%s entra no contexto, mas não é de uma pasta permitida", rel)
		}
		if proibido.MatchString(rel) {
			t.Errorf("%s entra no contexto: banco, executável, chave ou dump", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if entraram < 100 {
		t.Fatalf("só %d arquivos entrariam no contexto: a conferência não valeu", entraram)
	}
}

// TestDockerignore_OMatcherDesteTeste: o compilador de padrões acima segue as
// regras do Docker nos casos que o .dockerignore usa.
func TestDockerignore_OMatcherDesteTeste(t *testing.T) {
	for _, c := range []struct {
		padrao, arquivo string
		casa            bool
	}{
		{"*", "bin", true},
		{"*", "bin/historico.db", false}, // * não atravessa / (a pasta acima é que casa)
		{"**/*.db", "historico.db", true},
		{"**/*.db", "internal/a/b/historico.db", true},
		{"**/*.db", "internal/a/historico.dbx", false},
		{"web/dist-*", "web/dist-nuvem", true},
		{"web/dist-*", "web/distx", false},
		{"**/.env.*", "cmd/.env.local", true},
		{"go.mod", "go.mod", true},
		{"go.mod", "x/go.mod", false},
	} {
		if got := compilarPadrao(c.padrao).MatchString(c.arquivo); got != c.casa {
			t.Errorf("%q contra %q: quero %v, deu %v", c.padrao, c.arquivo, c.casa, got)
		}
	}
	regras := []regraIgnore{{"*", false, compilarPadrao("*")}, {"cmd", true, compilarPadrao("cmd")}, {"**/*.db", false, compilarPadrao("**/*.db")}}
	if ignorado(regras, "cmd/server/main.go") || !ignorado(regras, "cmd/x.db") || !ignorado(regras, "bin/x") {
		t.Error("a última regra que casa (com o arquivo ou com uma pasta acima) tem de valer")
	}
}
