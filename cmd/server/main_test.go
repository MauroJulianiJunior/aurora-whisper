package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// runCmd chama run com um ambiente falso e devolve a saída e o código.
func runCmd(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, func(k string) string { return env[k] }, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestRun_SemArgumentosImprimeNomeEVersao(t *testing.T) {
	out, errOut, code := runCmd(t, nil)
	if code != 0 || errOut != "" {
		t.Fatalf("código %d, stderr %q; quero 0 e vazio", code, errOut)
	}
	if want := appName + " " + version + "\n"; out != want {
		t.Errorf("stdout = %q, quero %q", out, want)
	}
}

func TestRun_AjudaEComandoDesconhecido(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		out, _, code := runCmd(t, nil, arg)
		if code != 0 || !strings.Contains(out, "migrate") {
			t.Errorf("%s: código %d, stdout %q; quero 0 e a lista de comandos", arg, code, out)
		}
	}

	out, errOut, code := runCmd(t, nil, "subir")
	if code != 2 {
		t.Errorf("comando desconhecido: código %d, quero 2", code)
	}
	if out != "" || !strings.Contains(errOut, `"subir"`) || !strings.Contains(errOut, "migrate") || !strings.Contains(errOut, "serve") {
		t.Errorf("comando desconhecido: stdout %q, stderr %q; quero o erro e a lista de comandos no stderr", out, errOut)
	}
}

func TestMigrate_SemDatabaseURLEhErroDeUso(t *testing.T) {
	out, errOut, code := runCmd(t, map[string]string{}, "migrate")
	if code != 2 {
		t.Fatalf("código %d, quero 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, quero vazio", out)
	}
	if !strings.Contains(errOut, "DATABASE_URL") {
		t.Errorf("a mensagem não diz qual variável falta: %q", errOut)
	}
}

func TestMigrate_OpcoesInvalidas(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://u:senha-nunca-usada@127.0.0.1:5432/x"}
	for name, args := range map[string][]string{
		"argumento solto":    {"migrate", "agora"},
		"opção desconhecida": {"migrate", "-force"},
		"opção com valor":    {"migrate", "-status=talvez"},
	} {
		_, errOut, code := runCmd(t, env, args...)
		if code != 2 {
			t.Errorf("%s: código %d, quero 2 (stderr %q)", name, code, errOut)
		}
		if strings.Contains(errOut, "senha-nunca-usada") {
			t.Errorf("%s: o stderr traz a senha: %q", name, errOut)
		}
	}
}

func TestMigrate_URLInvalidaNaoVazaASenha(t *testing.T) {
	// Porta que não é número: falha ao ler a URL, sem abrir conexão nenhuma.
	const secret = "SENHA_SECRETA_123"
	env := map[string]string{"DATABASE_URL": "postgres://usuario:" + secret + "@127.0.0.1:porta/banco"}
	out, errOut, code := runCmd(t, env, "migrate")
	if code != 1 {
		t.Fatalf("código %d, quero 1 (stderr %q)", code, errOut)
	}
	if strings.Contains(out+errOut, secret) {
		t.Errorf("a saída traz a senha: stdout %q, stderr %q", out, errOut)
	}
	if !strings.HasPrefix(errOut, "migrate: ") {
		t.Errorf("stderr deveria começar por \"migrate: \": %q", errOut)
	}
}

// O erro do pgx já esconde a senha, então o teste acima não prova que ESTE
// comando a esconde. Aqui a conexão falha com a URL e a senha dentro da
// mensagem, como um driver menos cuidadoso faria.
func TestMigrate_ErroDeConexaoNaoRepeteAURLNemASenha(t *testing.T) {
	const secret = "SENHA_SECRETA_123"
	dbURL := "postgres://usuario:" + secret + "@db.exemplo.com:5432/banco"

	original := connect
	t.Cleanup(func() { connect = original })
	connect = func(_ context.Context, url string) (*pgx.Conn, error) {
		return nil, fmt.Errorf("falha ao conectar em %s (senha %s)", url, secret)
	}

	out, errOut, code := runCmd(t, map[string]string{"DATABASE_URL": dbURL}, "migrate")
	if code != 1 {
		t.Fatalf("código %d, quero 1 (stderr %q)", code, errOut)
	}
	if strings.Contains(out+errOut, secret) || strings.Contains(out+errOut, dbURL) {
		t.Errorf("a saída traz a URL ou a senha: stdout %q, stderr %q", out, errOut)
	}
	if !strings.Contains(errOut, "não consegui conectar") {
		t.Errorf("stderr deveria dizer que a conexão falhou: %q", errOut)
	}
}

func TestRedact(t *testing.T) {
	const dbURL = "postgres://usuario:SENHA_SECRETA_123@db.exemplo.com:5432/banco?sslmode=require"
	cases := map[string]string{
		"a URL inteira":       "falhou em " + dbURL + " de novo",
		"só a senha":          "autenticação falhou para a senha SENHA_SECRETA_123",
		"a URL e a senha":     dbURL + " / SENHA_SECRETA_123",
		"sem nada a esconder": "falhou por outro motivo",
	}
	for name, msg := range cases {
		got := redact(msg, dbURL)
		if strings.Contains(got, "SENHA_SECRETA_123") {
			t.Errorf("%s: a senha sobrou: %q", name, got)
		}
		if name == "sem nada a esconder" && got != msg {
			t.Errorf("%s: a mensagem mudou: %q", name, got)
		}
	}
	if got := redact("erro", ""); got != "erro" {
		t.Errorf("sem URL a mensagem não pode mudar, veio %q", got)
	}
	// Senha curta demais não é trocada: estragaria qualquer texto com essas letras.
	if got := redact("banco pg", "postgres://u:pg@h/db"); got != "banco pg" {
		t.Errorf("senha de 2 caracteres foi trocada: %q", got)
	}
}
