package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
)

// O serve CONTRA UM POSTGRES DE VERDADE. Pulam sem AURORA_TEST_POSTGRES_URL
// (pgtest.NewDB).

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func senhaDe(t *testing.T, dbURL string) string {
	t.Helper()
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := u.User.Password()
	return p
}

// Um banco sem as migrations: o serve recusa subir e manda rodar o migrate.
func TestServe_Postgres_RecusaBancoNaoMigrado(t *testing.T) {
	dbURL := pgtest.NewDB(t)
	env := with(envBom(t), envDatabaseURL, dbURL)
	// Com prazo: se o serve subisse num banco não migrado, ficaria servindo;
	// o prazo faz o teste falhar em segundos, e não no timeout do go test.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var outBuf, errBuf bytes.Buffer
	code := run(ctx, []string{"serve"}, func(k string) string { return env[k] }, &outBuf, &errBuf)
	out, errOut := outBuf.String(), errBuf.String()
	if code != 1 {
		t.Fatalf("código %d, quero 1 (stderr %q)", code, errOut)
	}
	if !strings.Contains(errOut, "migrations pendentes") || !strings.Contains(errOut, "migrate") {
		t.Errorf("stderr = %q, quero o aviso de rodar o migrate", errOut)
	}
	if pw := senhaDe(t, dbURL); pw != "" && strings.Contains(out+errOut, pw) {
		t.Error("a saída traz a senha do banco")
	}
}

// Migrado, o serve sobe, responde o health (com o Postgres) e a página, e
// desliga limpo quando o contexto acaba (o SIGINT e o SIGTERM, no main).
func TestServe_Postgres_SobeEDesligaLimpo(t *testing.T) {
	dbURL := pgtest.NewDB(t)
	env := with(envBom(t), envDatabaseURL, dbURL)
	if _, errOut, code := runCmd(t, env, "migrate"); code != 0 {
		t.Fatalf("migrate: %d %q", code, errOut)
	}

	addrCh := make(chan net.Addr, 1)
	onListen = func(a net.Addr) { addrCh <- a }
	t.Cleanup(func() { onListen = nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr safeBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"serve"}, func(k string) string { return env[k] }, &stdout, &stderr)
	}()
	var addr net.Addr
	select {
	case addr = <-addrCh:
	case code := <-done:
		t.Fatalf("o serve saiu com %d antes de escutar: %s", code, stderr.String())
	case <-time.After(30 * time.Second):
		t.Fatal("o serve não escutou em 30 s")
	}

	get := func(path, host string) (int, http.Header) {
		req, err := http.NewRequest(http.MethodGet, "http://"+addr.String()+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, resp.Header
	}
	if code, _ := get("/api/v1/health/ready", addr.String()); code != http.StatusOK {
		t.Errorf("health/ready: %d, quero 200 (o Ping no Postgres)", code)
	}
	if code, h := get("/", "localhost:8080"); code != http.StatusOK || h.Get("Content-Security-Policy") == "" {
		t.Errorf("a página: %d %v", code, h)
	}
	if code, _ := get("/", addr.String()); code != http.StatusBadRequest {
		t.Errorf("a página por 127.0.0.1: %d, quero 400 (o Host tem de ser o de PUBLIC_BASE_URL)", code)
	}
	if code, _ := get("/api/v1/me", "localhost:8080"); code != http.StatusUnauthorized {
		t.Errorf("GET /me sem sessão: %d, quero 401", code)
	}

	// A rodada da subida (a arrumação e depois a retenção) roda ANTES de a
	// porta abrir (F11c): quando o onListen chega, a linha "retenção" já saiu,
	// antes da "servindo".
	if ret, serv := strings.Index(stderr.String(), `"msg":"retenção"`), strings.Index(stderr.String(), `"msg":"servindo"`); ret < 0 || serv < 0 || ret > serv {
		t.Fatalf("a linha \"retenção\" (%d) tem de vir antes da \"servindo\" (%d):\n%s", ret, serv, stderr.String())
	}
	// A linha "servindo" diz se há proxy confiável (aqui não), e não o valor.
	if !strings.Contains(stderr.String(), `"https":false,"proxy_confiavel":false}`) {
		t.Errorf("a linha \"servindo\" não diz o proxy confiável:\n%s", stderr.String())
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("o desligamento saiu com %d: %s", code, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("o serve não desligou em 20 s")
	}
	logs := stderr.String()
	for _, want := range []string{`"msg":"servindo"`, `"msg":"desligado"`, `"rota":"GET /api/v1/health/ready"`, `"msg":"arrumação"`,
		// A retenção (F9d) roda na subida, com o Store do Postgres: a linha
		// sai, com zero, e antes de "desligado".
		`"msg":"retenção","usuarios":0,"ativos":0,"lixeira":0,"lapides_criadas":0,"lapides_vencidas":0,"eventos":0,"mutacoes":0,"falhas":0`} {
		if !strings.Contains(logs, want) {
			t.Errorf("o log não tem %s:\n%s", want, logs)
		}
	}
	if strings.Index(logs, `"msg":"retenção"`) > strings.Index(logs, `"msg":"desligado"`) {
		t.Errorf("a linha da retenção saiu depois de \"desligado\":\n%s", logs)
	}
	for _, secret := range []string{senhaDe(t, dbURL), testSessionSecret, testPepper, testClientSecret, "mauro@exemplo.test"} {
		if secret != "" && strings.Contains(logs+stdout.String(), secret) {
			t.Errorf("o log traz um segredo ou um e-mail")
		}
	}
	if _, err := net.DialTimeout("tcp", addr.String(), time.Second); err == nil {
		t.Error("a porta continua aberta depois do desligamento")
	}
}
