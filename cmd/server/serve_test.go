package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
)

// Os segredos e o client dos testes são inventados aqui; nenhum é de verdade.
const (
	testSessionSecret = "SEGREDO-DE-SESSAO-DO-TESTE-0123456789abcdef"
	testPepper        = "PEPPER-DO-TOKEN-DO-TESTE-0123456789abcdefgh"
	testClientSecret  = "CLIENT-SECRET-DO-TESTE-XYZ"
	testDBPassword    = "SENHA-DO-BANCO-DO-TESTE-987"
)

// clientGoogle grava um JSON de client do Google num diretório temporário do
// teste e devolve o caminho.
func clientGoogle(t *testing.T, doc any) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "client_secret_teste.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func clientWeb(redirects ...string) map[string]any {
	return map[string]any{"web": map[string]any{
		"client_id": "cliente-de-teste.apps.googleusercontent.com", "client_secret": testClientSecret,
		"project_id": "aurora-teste", "auth_uri": "https://evil.example/auth", "token_uri": "https://evil.example/token",
		"redirect_uris": redirects,
	}}
}

// painelDir grava um build do painel de mentira (o index.html e um asset)
// num diretório temporário do teste e devolve o caminho.
func painelDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "PASTA-DO-PAINEL")
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for nome, conteudo := range map[string]string{
		"index.html":          `<!doctype html><html lang="pt-BR"><head><script type="module" src="/assets/app-1.js"></script></head><body><div id="root"></div></body></html>`,
		"assets/app-1.js":     `console.log("app")`,
		"licencas-fontes.txt": "OFL",
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(nome)), []byte(conteudo), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// envBom é uma configuração inteira e válida (o banco não existe: os testes
// sem Postgres param antes ou na conexão).
func envBom(t *testing.T) map[string]string {
	return map[string]string{
		envPainelDir:        painelDir(t),
		envPublicBaseURL:    "http://localhost:8080",
		envHTTPAddr:         "127.0.0.1:0",
		envDatabaseURL:      "postgres://aurora:" + testDBPassword + "@127.0.0.1:1/postgres?connect_timeout=2",
		envSessionSecret:    testSessionSecret,
		envDeviceTokenPep:   testPepper,
		envContasPermitidas: "mauro@exemplo.test, outra@exemplo.test",
		envGoogleSecretFile: clientGoogle(t, clientWeb("http://localhost:8080/auth/google/callback")),
	}
}

func with(env map[string]string, k, v string) map[string]string {
	out := map[string]string{}
	for key, val := range env {
		out[key] = val
	}
	if v == "" {
		delete(out, k)
	} else {
		out[k] = v
	}
	return out
}

func TestLoadConfig_Valida(t *testing.T) {
	env := envBom(t)
	cfg, err := loadConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.google.RedirectURL != "http://localhost:8080/auth/google/callback" || cfg.google.ClientSecret != testClientSecret {
		t.Errorf("client = %+v", cfg.google)
	}
	// Os endereços do Google do arquivo são ignorados: valem os do código.
	if strings.Contains(cfg.google.TokenURL, "evil") || strings.Contains(cfg.google.AuthURL, "evil") {
		t.Errorf("o token_uri do arquivo valeu: %s", cfg.google.TokenURL)
	}
	if cfg.contas.Len() != 2 || cfg.addr != "127.0.0.1:0" {
		t.Errorf("config = %+v", cfg)
	}
	// HTTP_ADDR tem padrão.
	cfg, err = loadConfig(func(k string) string { return with(env, envHTTPAddr, "")[k] })
	if err != nil || cfg.addr != "127.0.0.1:8080" {
		t.Errorf("HTTP_ADDR padrão = %q, %v", cfg.addr, err)
	}
	// https em outro servidor, com o redirect dele.
	https := with(env, envPublicBaseURL, "https://nuvem.exemplo.test")
	https = with(https, envGoogleSecretFile, clientGoogle(t, clientWeb("https://nuvem.exemplo.test/auth/google/callback")))
	_, err = loadConfig(func(k string) string { return https[k] })
	switch {
	case server.LoginDeTesteCompilado && err == nil:
		t.Error("com o login de teste compilado, o serve aceitou PUBLIC_BASE_URL fora de localhost")
	case !server.LoginDeTesteCompilado && err != nil:
		t.Errorf("https: %v", err)
	}
}

// Com o login de teste compilado (go test -tags logindeteste ./cmd/server),
// o serve só escuta no loopback: em 0.0.0.0, o POST /auth/teste ficaria
// exposto à rede local, com a URL pública em localhost ou não. Sem a tag,
// qualquer endereço vale (a VPS escuta atrás do proxy, F11).
func TestLoadConfig_LoginDeTesteSoEscutaNaMaquina(t *testing.T) {
	env := envBom(t)
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "LOCALHOST:8080", "[::1]:8080", "127.0.0.2:8080"} {
		if _, err := loadConfig(func(k string) string { return with(env, envHTTPAddr, addr)[k] }); err != nil {
			t.Errorf("HTTP_ADDR=%s: %v", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080", "192.168.0.10:8080", "meu-pc:8080"} {
		_, err := loadConfig(func(k string) string { return with(env, envHTTPAddr, addr)[k] })
		if !server.LoginDeTesteCompilado {
			if err != nil {
				t.Errorf("sem a tag, HTTP_ADDR=%s: %v", addr, err)
			}
			continue
		}
		var ce *configError
		if !errors.As(err, &ce) || ce.variavel != envHTTPAddr {
			t.Errorf("com a tag, HTTP_ADDR=%s: %v; quero o erro de %s", addr, err, envHTTPAddr)
			continue
		}
		out, errOut, code := runCmd(t, with(env, envHTTPAddr, addr), "serve")
		if code != 2 || !strings.HasPrefix(errOut, "serve: "+envHTTPAddr+": ") || strings.Contains(out+errOut, addr) {
			t.Errorf("com a tag, serve com HTTP_ADDR=%s: %d %q", addr, code, errOut)
		}
	}
}

// Cada variável que falta ou está errada encerra com 2, e a mensagem diz o
// nome da variável sem nunca mostrar o valor (nem o caminho do arquivo).
func TestServe_ConfiguracaoErrada(t *testing.T) {
	env := envBom(t)
	secretFile := env[envGoogleSecretFile]
	cases := []struct {
		name, variavel string
		env            map[string]string
	}{
		{"sem PUBLIC_BASE_URL", envPublicBaseURL, with(env, envPublicBaseURL, "")},
		{"http fora de localhost", envPublicBaseURL, with(env, envPublicBaseURL, "http://nuvem.exemplo.test")},
		{"base com caminho", envPublicBaseURL, with(env, envPublicBaseURL, "https://nuvem.exemplo.test/app")},
		{"base com senha", envPublicBaseURL, with(env, envPublicBaseURL, "https://u:"+testDBPassword+"@nuvem.exemplo.test")},
		{"base ftp", envPublicBaseURL, with(env, envPublicBaseURL, "ftp://localhost")},
		{"HTTP_ADDR sem porta", envHTTPAddr, with(env, envHTTPAddr, "localhost")},
		{"sem DATABASE_URL", envDatabaseURL, with(env, envDatabaseURL, "")},
		{"sem SESSION_SECRET", envSessionSecret, with(env, envSessionSecret, "")},
		{"SESSION_SECRET curto", envSessionSecret, with(env, envSessionSecret, "CURTO-DEMAIS-123")},
		{"sem DEVICE_TOKEN_PEPPER", envDeviceTokenPep, with(env, envDeviceTokenPep, "")},
		{"DEVICE_TOKEN_PEPPER curto", envDeviceTokenPep, with(env, envDeviceTokenPep, "CURTO-DEMAIS-456")},
		{"segredos iguais", envSessionSecret, with(env, envDeviceTokenPep, testSessionSecret)},
		{"sem CONTAS_PERMITIDAS", envContasPermitidas, with(env, envContasPermitidas, "")},
		{"CONTAS_PERMITIDAS só vírgulas", envContasPermitidas, with(env, envContasPermitidas, " , ,")},
		{"CONTAS_PERMITIDAS com lixo", envContasPermitidas, with(env, envContasPermitidas, "ok@exemplo.test,NAO-EMAIL-SECRETO")},
		{"sem GOOGLE_CLIENT_SECRET_FILE", envGoogleSecretFile, with(env, envGoogleSecretFile, "")},
		{"arquivo do Google que não existe", envGoogleSecretFile, with(env, envGoogleSecretFile, filepath.Join(t.TempDir(), "CAMINHO-SECRETO.json"))},
		{"arquivo do Google que não é JSON", envGoogleSecretFile, with(env, envGoogleSecretFile, clientGoogle(t, testClientSecret))},
		{"client do tipo computador", envGoogleSecretFile, with(env, envGoogleSecretFile, clientGoogle(t, map[string]any{"installed": map[string]any{"client_id": "x", "client_secret": testClientSecret}}))},
		{"client sem secret", envGoogleSecretFile, with(env, envGoogleSecretFile, clientGoogle(t, map[string]any{"web": map[string]any{
			"client_id": "x", "redirect_uris": []string{"http://localhost:8080/auth/google/callback"}}}))},
		{"client sem id", envGoogleSecretFile, with(env, envGoogleSecretFile, clientGoogle(t, map[string]any{"web": map[string]any{
			"client_secret": testClientSecret, "redirect_uris": []string{"http://localhost:8080/auth/google/callback"}}}))},
		{"client sem redirect_uris", envGoogleSecretFile, with(env, envGoogleSecretFile, clientGoogle(t, map[string]any{"web": map[string]any{
			"client_id": "x", "client_secret": testClientSecret}}))},
		{"redirect que não está no client", envGoogleSecretFile, with(env, envGoogleSecretFile, clientGoogle(t, clientWeb("http://localhost:9999/auth/google/callback")))},
		{"PROXY_CONFIAVEL lixo", envProxyConfiavel, with(env, envProxyConfiavel, "PROXY-SECRETO")},
		{"LOG_LEVEL lixo", envLogLevel, with(env, envLogLevel, "barulhento")},
		// O build do painel (F4): obrigatório, uma pasta, com o index.html.
		{"sem PAINEL_DIR", envPainelDir, with(env, envPainelDir, "")},
		{"PAINEL_DIR que não existe", envPainelDir, with(env, envPainelDir, filepath.Join(t.TempDir(), "PASTA-SECRETA"))},
		{"PAINEL_DIR que é um arquivo", envPainelDir, with(env, envPainelDir, filepath.Join(env[envPainelDir], "index.html"))},
		{"PAINEL_DIR sem index.html", envPainelDir, with(env, envPainelDir, t.TempDir())},
	}
	for _, c := range cases {
		out, errOut, code := runCmd(t, c.env, "serve")
		if code != 2 {
			t.Errorf("%s: código %d, quero 2 (stderr %q)", c.name, code, errOut)
			continue
		}
		if !strings.HasPrefix(errOut, "serve: "+c.variavel+": ") {
			t.Errorf("%s: a mensagem não começa pela variável %s: %q", c.name, c.variavel, errOut)
		}
		for _, secret := range []string{testSessionSecret, testPepper, testClientSecret, testDBPassword, "CURTO-DEMAIS",
			"NAO-EMAIL-SECRETO", "CAMINHO-SECRETO", secretFile, "PROXY-SECRETO", "mauro@exemplo.test", "PASTA-SECRETA", "PASTA-DO-PAINEL"} {
			if strings.Contains(out+errOut, secret) {
				t.Errorf("%s: a saída mostra um valor (%q): %q", c.name, secret, errOut)
			}
		}
	}
	if _, errOut, code := runCmd(t, env, "serve", "agora"); code != 2 || !strings.Contains(errOut, "argumento inesperado") {
		t.Errorf("serve com argumento: %d %q", code, errOut)
	}
}

// Com a configuração certa e o banco fora do ar, o serve falha com 1, sem
// mostrar a senha do banco.
func TestServe_BancoForaDoAr(t *testing.T) {
	out, errOut, code := runCmd(t, envBom(t), "serve")
	if code != 1 {
		t.Fatalf("código %d, quero 1 (stderr %q)", code, errOut)
	}
	if strings.Contains(out+errOut, testDBPassword) || strings.Contains(out+errOut, testClientSecret) {
		t.Errorf("a saída traz um segredo: %q", errOut)
	}
	if !strings.Contains(errOut, "não consegui conectar ao Postgres") {
		t.Errorf("stderr = %q", errOut)
	}
	bad := with(envBom(t), envDatabaseURL, "postgres://u:"+testDBPassword+"@127.0.0.1:porta/x")
	if _, errOut, code := runCmd(t, bad, "serve"); code != 2 || strings.Contains(errOut, testDBPassword) || !strings.Contains(errOut, envDatabaseURL) {
		t.Errorf("DATABASE_URL inválida: %d %q", code, errOut)
	}
}

func TestErroDoServidorSemIP(t *testing.T) {
	var buf strings.Builder
	w := erroDoServidor{log: newTestLogger(&buf)}
	for _, msg := range []string{
		"http: TLS handshake error from 203.0.113.9:5555: EOF\n",
		"http: panic serving 203.0.113.9:5555: segredo\n",
		"http: Accept error: x; retrying in 5ms\n",
		"qualquer coisa de 203.0.113.9\n",
	} {
		if _, err := w.Write([]byte(msg)); err != nil {
			t.Fatal(err)
		}
	}
	got := buf.String()
	if strings.Contains(got, "203.0.113.9") || strings.Contains(got, "segredo") {
		t.Fatalf("o erro do http.Server passou o IP ou o valor: %s", got)
	}
	for _, c := range []string{"categoria=tls", "categoria=panico", "categoria=accept", "categoria=outro"} {
		if !strings.Contains(got, c) {
			t.Errorf("falta %s em %s", c, got)
		}
	}
}

func newTestLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}
