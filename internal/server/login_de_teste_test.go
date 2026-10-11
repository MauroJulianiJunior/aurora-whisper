//go:build logindeteste

package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O login de teste só existe com a build tag logindeteste (rode com
// go test -tags logindeteste ./internal/server/...). Sem a tag, a suíte
// authtest confere que o POST /auth/teste é 404.

func nuvemDeTeste(t *testing.T, base string, mem *cloudstore.MemoryStore) (*server.Cloud, error) {
	t.Helper()
	contas, err := cloudauth.ParseContas("teste@exemplo.test,mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	return server.NewCloud(server.CloudConfig{
		Store: mem, Accounts: mem, BaseURL: base,
		SessionSecret:     []byte("segredo-de-sessao-do-login-de-teste-0123456789"),
		DeviceTokenPepper: []byte("pepper-de-token-do-login-de-teste-0123456789ab"),
		Contas:            contas,
		Google:            cloudauth.NovoGoogle("c", "s", strings.TrimSuffix(base, "/")+"/auth/google/callback"),
		Painel:            contracttest.PainelDeTeste(),
	})
}

func TestLoginDeTeste(t *testing.T) {
	if !server.LoginDeTesteCompilado {
		t.Fatal("o arquivo com a tag compilou sem o login de teste")
	}
	mem := cloudstore.NewMemoryStore()
	if _, err := nuvemDeTeste(t, "https://nuvem.exemplo.test", mem); err == nil {
		t.Fatal("com a tag, NewCloud aceitou uma PUBLIC_BASE_URL fora de localhost")
	}
	c, err := nuvemDeTeste(t, "http://localhost:8080", mem)
	if err != nil {
		t.Fatal(err)
	}
	h := c.Handler()
	entrarDe := func(remote, email string, mudar ...func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/teste", strings.NewReader(`{"email":"`+email+`"}`))
		req.Host = "localhost:8080"
		req.RemoteAddr = remote
		for _, f := range mudar {
			f(req)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	entrar := func(email string) *httptest.ResponseRecorder { return entrarDe("127.0.0.1:5555", email) }

	// Quem não pede da própria máquina (o servidor escutando na rede, ou um
	// proxy local repassando) vê a rota como não montada: 404, nada criado.
	vazio := mem.Contar()
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"da rede local":   entrarDe("192.168.0.20:5555", "teste@exemplo.test"),
		"IPv6 da rede":    entrarDe("[fe80::1]:5555", "teste@exemplo.test"),
		"sem porta":       entrarDe("127.0.0.1", "teste@exemplo.test"),
		"X-Forwarded-For": entrarDe("127.0.0.1:5555", "teste@exemplo.test", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "192.168.0.20") }),
		"Forwarded":       entrarDe("127.0.0.1:5555", "teste@exemplo.test", func(r *http.Request) { r.Header.Set("Forwarded", "for=192.168.0.20") }),
	} {
		if rec.Code != http.StatusNotFound || len(rec.Result().Cookies()) != 0 {
			t.Errorf("login de teste %s: %d, cookies %v; quero 404 sem cookie", name, rec.Code, rec.Result().Cookies())
		}
	}
	if depois := mem.Contar(); depois != vazio {
		t.Fatalf("o login de teste de fora da máquina mudou o banco: %+v -> %+v", vazio, depois)
	}
	if rec := entrarDe("[::1]:5555", "teste@exemplo.test"); rec.Code != http.StatusNoContent {
		t.Fatalf("login de teste de ::1: %d %s", rec.Code, rec.Body.String())
	}

	rec := entrar("Teste@Exemplo.test")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("login de teste: %d %s", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "__Host-sessao" {
			cookie = ck
		}
	}
	if cookie == nil || !cookie.Secure || !cookie.HttpOnly {
		t.Fatalf("o login de teste não abriu a sessão de verdade: %+v", cookie)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Host = "localhost:8080"
	req.AddCookie(cookie)
	me := httptest.NewRecorder()
	h.ServeHTTP(me, req)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), "teste@exemplo.test") {
		t.Fatalf("GET /me com a sessão do login de teste: %d %s", me.Code, me.Body.String())
	}

	// Fora da lista: 403, nada criado.
	before := mem.Contar()
	if rec := entrar("intrusa@exemplo.test"); rec.Code != http.StatusForbidden {
		t.Fatalf("fora da lista: %d", rec.Code)
	}
	// D7: um e-mail já fixado no Google não é tomado pelo sub "teste:".
	if _, err := mem.EntrarComGoogle(context.Background(), cloudstore.Identidade{Sub: "110169484474386276334", Email: "mauro@exemplo.test"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	before = mem.Contar()
	if rec := entrar("mauro@exemplo.test"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "sub_diferente") {
		t.Fatalf("login de teste num e-mail do Google: %d %s, quero 403 sub_diferente", rec.Code, rec.Body.String())
	}
	if after := mem.Contar(); after != before {
		t.Fatalf("o login de teste recusado mudou o banco: %+v -> %+v", before, after)
	}
}

// A tela Entrar do app pergunta GET /auth/teste para saber se oferece o botão
// da conta de teste (ADR-019, F4): a primeira conta da lista e se o Google
// existe. Só da própria máquina; de fora, 404, como o POST.
func TestLoginDeTeste_ATelaPergunta(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	c, err := nuvemDeTeste(t, "http://localhost:8080", mem)
	if err != nil {
		t.Fatal(err)
	}
	perguntar := func(h http.Handler, remote string, mudar ...func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/teste", nil)
		req.Host = "localhost:8080"
		req.RemoteAddr = remote
		for _, f := range mudar {
			f(req)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := perguntar(c.Handler(), "127.0.0.1:5555")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"conta":"teste@exemplo.test","google":true}` {
		t.Fatalf("GET /auth/teste: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("GET /auth/teste sem no-store: %q", rec.Header().Get("Cache-Control"))
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"da rede local":   perguntar(c.Handler(), "192.168.0.20:5555"),
		"X-Forwarded-For": perguntar(c.Handler(), "127.0.0.1:5555", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "192.168.0.20") }),
		"Forwarded":       perguntar(c.Handler(), "127.0.0.1:5555", func(r *http.Request) { r.Header.Set("Forwarded", "for=192.168.0.20") }),
	} {
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "teste@exemplo.test") {
			t.Errorf("GET /auth/teste %s: %d %s; quero 404 sem a conta", name, rec.Code, rec.Body.String())
		}
	}

	// O servidor de teste do PC (cmd/server teste) sobe sem o Google: o
	// login do Google não existe (404), e a tela só mostra a conta de teste.
	contas, err := cloudauth.ParseContas("voce@teste.local")
	if err != nil {
		t.Fatal(err)
	}
	semGoogle, err := server.NewCloud(server.CloudConfig{
		Store: mem, Accounts: mem, BaseURL: "http://localhost:8080",
		SessionSecret:     []byte("segredo-de-sessao-do-login-de-teste-0123456789"),
		DeviceTokenPepper: []byte("pepper-de-token-do-login-de-teste-0123456789ab"),
		Contas:            contas, Painel: contracttest.PainelDeTeste(),
	})
	if err != nil {
		t.Fatalf("com a tag, NewCloud sem o Google: %v", err)
	}
	h := semGoogle.Handler()
	if rec := perguntar(h, "127.0.0.1:5555"); strings.TrimSpace(rec.Body.String()) != `{"conta":"voce@teste.local","google":false}` {
		t.Errorf("GET /auth/teste sem o Google: %d %s", rec.Code, rec.Body.String())
	}
	for _, rt := range []struct{ method, path string }{{http.MethodPost, "/auth/start"}, {http.MethodGet, "/auth/google/callback?state=x&code=y"}} {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(`{}`))
		req.Host = "localhost:8080"
		req.RemoteAddr = "127.0.0.1:5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || len(rec.Result().Cookies()) != 0 {
			t.Errorf("%s %s sem o Google: %d, cookies %v; quero 404", rt.method, rt.path, rec.Code, rec.Result().Cookies())
		}
	}
}
