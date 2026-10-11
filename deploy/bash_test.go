package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// O bash com programas de mentira (ADR-019, "Decisões da F11": "quando der, o
// bash executado com programas de mentira num PATH só deles, sem rede").
//
// Cada teste ganha uma pasta temporária com:
//   - falsos/: os programas de mentira que o teste escreve (docker, psql,
//     pg_dump, age, rclone, flock...). Cada um anota a chamada em REGISTRO;
//   - ferramentas/: embrulhos para uma lista FECHADA de utilitários do sistema
//     (cat, mv, chmod, od, date...), e nada mais. O docker, o ssh, o psql, o
//     age, o rclone, o curl e o sudo de verdade nunca estão nessa lista: um
//     script que os chamasse sem o falso daria "command not found";
//   - casa/ e tmp/: o HOME e o TMPDIR do script.
//
// O PATH do script é só falsos:ferramentas. No Windows, o bash é o do Git
// (nunca o C:\Windows\System32\bash.exe, que é o do WSL); sem bash, o teste
// pula.

// ferramentasPermitidas são os utilitários de verdade que os scripts podem
// usar. Nenhum deles fala com a rede, com o Docker ou com um banco.
var ferramentasPermitidas = []string{
	"basename", "cat", "chmod", "cmp", "cp", "cut", "date", "dirname", "env", "find",
	"grep", "head", "id", "ls", "mkdir", "mkfifo", "mktemp", "mv", "od",
	"readlink", "realpath", "rm", "rmdir", "sed", "sleep", "sort", "stat",
	"tail", "tee", "touch", "tr", "wc",
}

// acharBash devolve o bash de verdade, ou "" se não houver um que sirva.
func acharBash() string {
	if runtime.GOOS != "windows" {
		p, err := exec.LookPath("bash")
		if err != nil {
			return ""
		}
		return p
	}
	var candidatos []string
	for _, pf := range []string{os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles")} {
		if pf != "" {
			candidatos = append(candidatos, filepath.Join(pf, "Git", "usr", "bin", "bash.exe"))
		}
	}
	if git, err := exec.LookPath("git"); err == nil {
		// <Git>\cmd\git.exe ou <Git>\mingw64\bin\git.exe
		d := filepath.Dir(git)
		candidatos = append(candidatos,
			filepath.Join(d, "..", "usr", "bin", "bash.exe"),
			filepath.Join(d, "..", "..", "usr", "bin", "bash.exe"))
	}
	for _, c := range candidatos {
		if info, err := os.Stat(c); err == nil && info.Mode().IsRegular() {
			return c
		}
	}
	return ""
}

// posix converte um caminho do sistema para o que o bash entende: no Windows,
// C:\x\y vira /c/x/y (o ponto de montagem padrão do Git Bash).
func posix(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	vol := filepath.VolumeName(p)
	if len(vol) == 2 && vol[1] == ':' {
		return "/" + strings.ToLower(vol[:1]) + filepath.ToSlash(p[2:])
	}
	return filepath.ToSlash(p)
}

type ambienteBash struct {
	t           *testing.T
	bash        string
	dir         string
	falsos      string
	ferramentas string
	casa        string
	tmp         string
	registro    string
	env         map[string]string
}

func novoAmbienteBash(t *testing.T) *ambienteBash {
	t.Helper()
	bash := acharBash()
	if bash == "" {
		t.Skip("sem bash (no Windows, o do Git): os testes com programas de mentira não rodam")
	}
	dir := t.TempDir()
	// No Windows, o chmod 0444 do script vira o atributo "somente leitura", e
	// a limpeza do TempDir não apaga esses arquivos. Esta limpeza roda antes
	// dela (as limpezas rodam da última para a primeira).
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(p, 0o777)
			}
			return nil
		})
	})
	a := &ambienteBash{
		t:        t,
		bash:     bash,
		dir:      dir,
		falsos:   filepath.Join(dir, "falsos"),
		casa:     filepath.Join(dir, "casa"),
		tmp:      filepath.Join(dir, "tmp"),
		registro: filepath.Join(dir, "registro.txt"),
		env:      map[string]string{},
	}
	for _, d := range []string{a.falsos, a.casa, a.tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(a.registro, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.ferramentas = ferramentasCompartilhadas(t, a)
	return a
}

// As ferramentas são as mesmas para todos os testes do pacote: geradas uma vez
// (no Windows, cada processo do bash custa caro) e apagadas no TestMain.
var (
	onceFerramentas sync.Once
	dirFerramentas  string
	errFerramentas  error
)

func ferramentasCompartilhadas(t *testing.T, a *ambienteBash) string {
	t.Helper()
	onceFerramentas.Do(func() {
		d, err := os.MkdirTemp("", "aurora-deploy-ferramentas-")
		if err != nil {
			errFerramentas = err
			return
		}
		dirFerramentas = d
		// Os embrulhos: gerados de dentro do bash, com o PATH do sistema (e
		// só ele), cada um um exec do utilitário de verdade pelo caminho
		// completo.
		gerar := `set -e
PATH=/usr/local/bin:/usr/bin:/bin
d="$1"; shift
for f in "$@"; do
  p="$(command -v "$f" 2>/dev/null || true)"
  case "$p" in /*) ;; *) continue ;; esac
  printf '#!%s\nexec %q "$@"\n' "$BASH" "$p" > "$d/$f"
  chmod +x "$d/$f"
done
printf '#!%s\nexec %q "$@"\n' "$BASH" "$BASH" > "$d/bash"
chmod +x "$d/bash"
`
		args := append([]string{"--noprofile", "--norc", "-c", gerar, "_", posix(d)}, ferramentasPermitidas...)
		cmd := exec.Command(a.bash, args...)
		cmd.Env = a.ambiente()
		if out, err := cmd.CombinedOutput(); err != nil {
			errFerramentas = fmt.Errorf("gerar as ferramentas: %v\n%s", err, out)
		}
	})
	if errFerramentas != nil {
		t.Fatal(errFerramentas)
	}
	return dirFerramentas
}

func TestMain(m *testing.M) {
	code := m.Run()
	if dirFerramentas != "" {
		_ = os.RemoveAll(dirFerramentas)
	}
	os.Exit(code)
}

// falso escreve um programa de mentira. O corpo é bash; antes dele, a chamada
// é anotada no REGISTRO, uma por linha: "nome [arg1] [arg2] ...".
func (a *ambienteBash) falso(nome, corpo string) {
	a.t.Helper()
	texto := "#!/usr/bin/env bash\n" +
		"{ printf '%s' " + nome + "; printf ' [%s]' \"$@\"; printf '\\n'; } >> \"$REGISTRO\"\n" +
		corpo + "\n"
	if err := os.WriteFile(filepath.Join(a.falsos, nome), []byte(texto), 0o755); err != nil {
		a.t.Fatal(err)
	}
}

// ambiente é o do processo, sem o que um script daqui leria (para o teste não
// depender da máquina), mais o do ambiente de teste.
func (a *ambienteBash) ambiente() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		ku := strings.ToUpper(k)
		if strings.HasPrefix(ku, "AW_") || strings.HasPrefix(ku, "PG") || strings.HasPrefix(ku, "RCLONE") ||
			strings.HasPrefix(ku, "AURORA") || strings.HasPrefix(ku, "POSTGRES") || strings.HasPrefix(ku, "FALSO_") ||
			ku == "HOME" || ku == "TMPDIR" || ku == "REGISTRO" || ku == "BASH_ENV" || ku == "ENV" || ku == "CDPATH" ||
			ku == "XDG_RUNTIME_DIR" {
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"HOME="+posix(a.casa),
		"TMPDIR="+posix(a.tmp),
		"REGISTRO="+posix(a.registro),
		"LC_ALL=C",
	)
	for k, v := range a.env {
		out = append(out, k+"="+v)
	}
	return out
}

type resultado struct {
	saida  string // stdout
	erro   string // stderr
	codigo int
}

func (r resultado) tudo() string { return r.saida + r.erro }

// rodar roda o script (caminho relativo à raiz do repositório) com o PATH só
// dos falsos e das ferramentas, a entrada dada e os argumentos.
func (a *ambienteBash) rodar(script string, entrada []byte, args ...string) resultado {
	a.t.Helper()
	abs := script
	if !filepath.IsAbs(script) {
		var err error
		if abs, err = filepath.Abs(caminho(filepath.FromSlash(script))); err != nil {
			a.t.Fatal(err)
		}
	}
	roda := `PATH="$1:$2"; export PATH; s="$3"; shift 3; exec "$BASH" "$s" "$@"`
	full := append([]string{"--noprofile", "--norc", "-c", roda, "_", posix(a.falsos), posix(a.ferramentas), posix(abs)}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.bash, full...)
	cmd.Env = a.ambiente()
	cmd.Dir = a.dir
	cmd.Stdin = bytes.NewReader(entrada)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ctx.Err() != nil {
		a.t.Fatalf("%s não terminou em 60 s (travou?)\nstdout:\n%s\nstderr:\n%s", script, so.String(), se.String())
	}
	codigo := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		codigo = ee.ExitCode()
	} else if err != nil {
		a.t.Fatalf("rodar %s: %v", script, err)
	}
	return resultado{so.String(), se.String(), codigo}
}

// sintaxe roda só o "bash -n" (lê o script e não executa nada).
func (a *ambienteBash) sintaxe(abs string) (string, error) {
	cmd := exec.Command(a.bash, "--noprofile", "--norc", "-n", posix(abs))
	cmd.Env = a.ambiente()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// chamadas devolve o registro dos falsos, uma chamada por linha.
func (a *ambienteBash) chamadas() []string {
	a.t.Helper()
	data, err := os.ReadFile(a.registro)
	if err != nil {
		a.t.Fatal(err)
	}
	s := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// chamou diz se alguma chamada começa com o prefixo (por exemplo
// "docker [exec]").
func (a *ambienteBash) chamou(prefixo string) bool {
	for _, c := range a.chamadas() {
		if strings.HasPrefix(c, prefixo) {
			return true
		}
	}
	return false
}

// indice devolve a posição da primeira chamada que contém o trecho, ou -1.
func (a *ambienteBash) indice(trecho string) int {
	for i, c := range a.chamadas() {
		if strings.Contains(c, trecho) {
			return i
		}
	}
	return -1
}

// TestBash_OAmbienteNaoTemOSistema: o PATH do script não alcança o docker, o
// psql, o ssh nem o curl de verdade, mesmo que a máquina os tenha.
func TestBash_OAmbienteNaoTemOSistema(t *testing.T) {
	a := novoAmbienteBash(t)
	sonda := filepath.Join(a.dir, "sonda.sh")
	texto := "#!/usr/bin/env bash\nfor p in docker psql pg_dump pg_restore age age-keygen rclone b2 ssh scp curl wget sudo flock systemctl; do\n" +
		"  if command -v \"$p\" >/dev/null 2>&1; then echo \"ACHOU $p\"; fi\ndone\necho FIM\n"
	if err := os.WriteFile(sonda, []byte(texto), 0o755); err != nil {
		t.Fatal(err)
	}
	r := a.rodar(sonda, nil)
	if !strings.Contains(r.saida, "FIM") {
		t.Fatalf("a sonda não rodou: %q %q", r.saida, r.erro)
	}
	if strings.Contains(r.saida, "ACHOU") || strings.Contains(r.saida, "SEM cat") {
		t.Errorf("o PATH dos testes alcança programa de verdade:\n%s", r.saida)
	}
}
