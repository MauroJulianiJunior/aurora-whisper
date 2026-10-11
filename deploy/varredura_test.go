package deploy

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A varredura de segredos (ADR-019, "Decisões da F11", F11h). O gitleaks do CI
// só roda com git push, e o push espera uma decisão à parte (D4). Até lá, este
// teste faz as vezes dele sobre o que mais corre risco de levar um segredo de
// verdade: deploy/, docs/, scripts/, migrations/ e os arquivos da raiz
// (.env.example, os .md). Ele roda em todo "go test ./...".
//
// As amostras que provam que cada padrão pega o que deve são montadas por
// partes, para o próprio arquivo de teste não parecer um segredo.

type padraoDeSegredo struct {
	nome string
	re   *regexp.Regexp
	// valor, se não for nil, diz se o trecho achado é um valor de verdade (e
	// não um exemplo como <senha> ou $SENHA, que o padrão já deixa de fora).
	valor func(m []string) bool
}

// exemplosConhecidos são os valores de mentira que a documentação usa.
var exemplosConhecidos = map[string]bool{
	"postgres": true, "password": true, "senha": true, "secret": true, "segredo": true,
	"usuario": true, "user": true, "pass": true, "changeme": true,
}

var padroesDeSegredo = []padraoDeSegredo{
	{nome: "chave privada do age", re: regexp.MustCompile(`AGE-SECRET-KEY-1[QPZRY9X8GF2TVDW0S3JN54KHCE6MUA7L]{58}`)},
	{nome: "chave privada PEM", re: regexp.MustCompile(`-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY-----`)},
	{nome: "secret de client do Google", re: regexp.MustCompile(`GOCSPX-[A-Za-z0-9_-]{20,}`)},
	{nome: "chave de API do Google", re: regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)},
	{nome: "chave da Anthropic", re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{32,}`)},
	{nome: "chave da OpenAI", re: regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9]{20}[A-Za-z0-9_-]{12,}`)},
	{nome: "token do GitHub", re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}|\bgithub_pat_[A-Za-z0-9_]{40,}`)},
	{nome: "chave da AWS", re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{nome: "token do Slack", re: regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`)},
	{nome: "application key do Backblaze B2", re: regexp.MustCompile(`\bK00[0-9][A-Za-z0-9+/]{27}\b`)},
	{
		nome: "URL de banco com senha",
		re:   regexp.MustCompile(`\b(postgres|postgresql|mysql|mongodb(\+srv)?|redis)://[^:/\s"'@<>]+:([^@\s"'<>$*{}]+)@`),
		valor: func(m []string) bool {
			s := m[3]
			return !exemplosConhecidos[strings.ToLower(s)] && !regexp.MustCompile(`^(x+|\.+|_+)$`).MatchString(s)
		},
	},
	{
		nome: "segredo atribuído com valor",
		re: regexp.MustCompile(`\b(SESSION_SECRET|DEVICE_TOKEN_PEPPER|POSTGRES_PASSWORD|B2_APPLICATION_KEY|ASR_CLOUD_API_KEY|` +
			`CONTEXT_API_KEY|GITHUB_TOKEN|CLIENT_SECRET|client_secret|RCLONE_CONFIG_B2_KEY)["']?[ \t]*[:=][ \t]*["']?([A-Za-z0-9+/_=.-]{20,})`),
	},
	{nome: "chave no rclone.conf", re: regexp.MustCompile(`(?m)^[ \t]*key[ \t]*=[ \t]*[A-Za-z0-9+/=_-]{20,}[ \t]*$`)},
}

// achados devolve os nomes dos padrões que casam com o texto.
func achados(texto string) []string {
	var out []string
	for _, p := range padroesDeSegredo {
		for _, m := range p.re.FindAllStringSubmatch(texto, -1) {
			if p.valor == nil || p.valor(m) {
				out = append(out, p.nome)
				break
			}
		}
	}
	return out
}

// pastasVarridas e extensoesBinarias: o que a varredura lê e o que ela pula.
var (
	pastasVarridas    = []string{"deploy", "docs", "scripts", "migrations"}
	extensoesBinarias = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".ico": true, ".ttf": true,
		".woff": true, ".woff2": true, ".pdf": true, ".gguf": true, ".onnx": true, ".bin": true, ".exe": true}
)

// arquivosEm devolve os arquivos de texto das pastas (sem node_modules, .git
// nem as extensões binárias).
func arquivosEm(t *testing.T, pastas ...string) []string {
	t.Helper()
	var out []string
	for _, d := range pastas {
		err := filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if e.Name() == "node_modules" || e.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !extensoesBinarias[strings.ToLower(filepath.Ext(p))] {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// varrer devolve, para cada arquivo com algo que parece segredo, "arquivo:
// nome do padrão". Só o nome: o valor achado nunca vai para o log. Um arquivo
// com byte zero no começo é binário e fica de fora.
func varrer(t *testing.T, base string, arquivos []string) []string {
	t.Helper()
	var out []string
	for _, p := range arquivos {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			continue
		}
		for _, nome := range achados(string(data)) {
			rel, _ := filepath.Rel(base, p)
			out = append(out, filepath.ToSlash(rel)+": "+nome)
		}
	}
	return out
}

func TestVarredura_NenhumSegredoNoRepositorio(t *testing.T) {
	var pastas []string
	for _, d := range pastasVarridas {
		pastas = append(pastas, caminho(d))
	}
	arquivos := arquivosEm(t, pastas...)
	raizes, err := filepath.Glob(caminho("*.md"))
	if err != nil {
		t.Fatal(err)
	}
	arquivos = append(append(arquivos, raizes...), caminho(".env.example"))
	if len(arquivos) < 50 {
		t.Fatalf("só %d arquivos para varrer: a varredura não valeu", len(arquivos))
	}
	for _, a := range varrer(t, raiz, arquivos) {
		t.Errorf("parece ter segredo: %s", a)
	}
}

// amostras de segredos de mentira, montadas por partes. Cada padrão tem de
// pegar a sua.
func amostrasDeSegredo() map[string]string {
	rep := strings.Repeat
	return map[string]string{
		"chave privada do age":            "AGE-SECRET-" + "KEY-1" + rep("QPZRY9X8GF", 5) + "2TVDW0S3",
		"chave privada PEM":               "-----BEGIN " + "OPENSSH PRIVATE" + " KEY-----",
		"secret de client do Google":      `"client_secret": "` + "GOC" + "SPX-" + rep("aB3", 9) + `"`,
		"chave de API do Google":          "AI" + "za" + rep("Sy0", 11) + "ab",
		"chave da Anthropic":              "sk-" + "ant-" + "api03-" + rep("Ab1", 12),
		"chave da OpenAI":                 "sk-" + "proj-" + rep("Ab1Cd", 8),
		"token do GitHub":                 "gh" + "p_" + rep("A1b2", 9),
		"chave da AWS":                    "AK" + "IA" + rep("Z7", 8),
		"token do Slack":                  "xo" + "xb-" + rep("1234-", 4),
		"application key do Backblaze B2": "K0" + "05" + rep("aB1/", 6) + "xyz",
		"URL de banco com senha":          "postgres" + "://aurora:" + rep("9f", 32) + "@aurora-whisper-postgres:5432/aurora",
		"segredo atribuído com valor":     "DEVICE_TOKEN" + "_PEPPER=" + rep("ab", 32),
		"chave no rclone.conf":            "[b2]\ntype = b2\nkey = " + "K0" + "05" + rep("x", 27) + "\n",
	}
}

func TestVarredura_CadaPadraoPegaASuaAmostra(t *testing.T) {
	amostras := amostrasDeSegredo()
	if len(amostras) != len(padroesDeSegredo) {
		t.Fatalf("%d amostras para %d padrões", len(amostras), len(padroesDeSegredo))
	}
	for _, p := range padroesDeSegredo {
		amostra, ok := amostras[p.nome]
		if !ok {
			t.Errorf("sem amostra para %s", p.nome)
			continue
		}
		if !strings.Contains(strings.Join(achados(amostra), "|"), p.nome) {
			t.Errorf("o padrão %q não pegou a amostra", p.nome)
		}
	}
}

// Os exemplos que a documentação usa de propósito não são achados.
func TestVarredura_ExemplosNaoSaoSegredo(t *testing.T) {
	for _, texto := range []string{
		"postgres://postgres:<senha gerada na hora>@127.0.0.1:54329/postgres?sslmode=disable",
		"postgres://usuario:senha@servidor:5432/banco",
		"postgres://$PAPEL:$senha_aurora@$HOST_DO_POSTGRES:5432/$BANCO?sslmode=disable",
		"postgres://aurora:<senha>@aurora-whisper-postgres:5432/aurora",
		"SESSION_SECRET=",
		"SESSION_SECRET=\nSESSION_SECRET_FILE=", // a variável vazia, e a de baixo não é o valor dela
		"DEVICE_TOKEN_PEPPER_FILE=/run/secrets/device-token-pepper",
		"age1" + strings.Repeat("q", 58), // a chave PÚBLICA do age não é segredo
		"key = <a application key>",
		"task-" + strings.Repeat("abc", 15), // "sk-" no meio de uma palavra
	} {
		if a := achados(texto); len(a) > 0 {
			t.Errorf("exemplo achado como segredo (%v): %q", a, texto)
		}
	}
}

// A varredura de verdade acha a amostra num arquivo: o caminho todo (ler a
// pasta, pular o binário, casar) funciona.
func TestVarredura_AchaNumArquivo(t *testing.T) {
	dir := t.TempDir()
	amostra := amostrasDeSegredo()["chave privada do age"]
	for nome, conteudo := range map[string]string{
		"docs/vazou.md":            "texto\n" + amostra + "\n",
		"docs/imagem.png":          amostra,                 // pulado pela extensão
		"docs/binario.dat":         "\x00\x01" + amostra,    // pulado pelo byte zero
		"docs/node_modules/x.js":   amostra,                 // pulado pela pasta
		"scripts/limpo.sh":         "#!/usr/bin/env bash\n", // nada a achar
		"deploy/config.env.antigo": "SESSION_SECRET=" + strings.Repeat("ab", 32) + "\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(nome))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(conteudo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var pastas []string
	for _, d := range pastasVarridas {
		pastas = append(pastas, filepath.Join(dir, d))
	}
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(varrer(t, dir, arquivosEm(t, pastas...)), "\n")
	want := "deploy/config.env.antigo: segredo atribuído com valor\ndocs/vazou.md: chave privada do age"
	if got != want {
		t.Errorf("a varredura achou:\n%s\nquero:\n%s", got, want)
	}
	if strings.Contains(got, amostra) {
		t.Error("o valor achado foi para o log")
	}
}
