package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// A nuvem serve o app (ADR-019, F4 e F5): o build da nuvem do painel, lido uma
// vez na subida. O / é o index.html; /assets/* tem o hash no nome e é imutável;
// os arquivos da raiz (o favicon, as licenças, e na F5 o sw.js, o manifest e
// os ícones) são conferidos a cada vez; os tipos saem de uma tabela fixa; o
// resto é o 404 das rotas não montadas (o /index.html inclusive).
func TestPainel_ServeOBuild(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	const base = "http://localhost:8080"
	c, err := server.NewCloud(server.CloudConfig{
		Store: mem, Accounts: mem, BaseURL: base,
		SessionSecret:     []byte("SEGREDO-DE-SESSAO-DO-TESTE-0123456789abcdef"),
		DeviceTokenPepper: []byte("PEPPER-DO-TOKEN-DO-TESTE-0123456789abcdefgh"),
		Contas:            contas,
		Google:            cloudauth.NovoGoogle("cliente", "secret", base+"/auth/google/callback"),
		Painel:            contracttest.PainelDeTeste(),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := c.Handler()
	pedir := func(method, path string, mudar ...func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "localhost:8080"
		for _, f := range mudar {
			f(req)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	const csp = "default-src 'self'; img-src 'self' blob:; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'"
	painel := contracttest.PainelDeTeste()

	for _, c := range []struct {
		path, arquivo, tipo, cache string
	}{
		{"/", "index.html", "text/html; charset=utf-8", "no-cache"},
		// A janela à parte de entrar de novo é o próprio app, no /, com a
		// volta do Google (a sessão que acaba com um texto não salvo).
		{"/?janela=entrar", "index.html", "text/html; charset=utf-8", "no-cache"},
		{"/?janela=entrou", "index.html", "text/html; charset=utf-8", "no-cache"},
		{"/favicon.svg", "favicon.svg", "image/svg+xml", "no-cache"},
		{"/licencas-fontes.txt", "licencas-fontes.txt", "text/plain; charset=utf-8", "no-cache"},
		// O app instalável (F5): o sw.js, o manifest e os ícones são da raiz do
		// build, públicos (o navegador busca o manifest sem cookie) e conferidos
		// a cada vez (no-cache): um sw.js novo chega na navegação seguinte.
		{"/sw.js", "sw.js", "text/javascript; charset=utf-8", "no-cache"},
		{"/manifest.webmanifest", "manifest.webmanifest", "application/manifest+json", "no-cache"},
		{"/icone-192.png", "icone-192.png", "image/png", "no-cache"},
		{"/assets/index-teste1.js", "assets/index-teste1.js", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
		{"/assets/index-teste2.css", "assets/index-teste2.css", "text/css; charset=utf-8", "public, max-age=31536000, immutable"},
		{"/assets/inter-latin-teste3.woff2", "assets/inter-latin-teste3.woff2", "font/woff2", "public, max-age=31536000, immutable"},
	} {
		rec := pedir(http.MethodGet, c.path)
		hd := rec.Header()
		if rec.Code != http.StatusOK || rec.Body.String() != string(painel[c.arquivo].Data) {
			t.Errorf("GET %s: %d %q, quero 200 com o %s", c.path, rec.Code, rec.Body.String(), c.arquivo)
			continue
		}
		if hd.Get("Content-Type") != c.tipo || hd.Get("Cache-Control") != c.cache {
			t.Errorf("GET %s: tipo %q, cache %q; quero %q, %q", c.path, hd.Get("Content-Type"), hd.Get("Cache-Control"), c.tipo, c.cache)
		}
		if hd.Get("Content-Security-Policy") != csp || hd.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s sem os cabeçalhos de segurança: %v", c.path, hd)
		}
		// O app grava o ditado do celular (ADR-021, escolha menor 18): o
		// microfone é só da própria origem, em todo arquivo dele (a página, o
		// sw.js, o manifest, os assets).
		if got := hd.Get("Permissions-Policy"); got != "microphone=(self)" {
			t.Errorf("GET %s: Permissions-Policy %q, quero microphone=(self)", c.path, got)
		}
		// O app é a casca: nenhum arquivo dele abre sessão.
		if hd.Get("Set-Cookie") != "" {
			t.Errorf("GET %s grava cookie: %q", c.path, hd.Get("Set-Cookie"))
		}
		// O navegador que já tem esta versão recebe 304, sem corpo.
		etag := hd.Get("ETag")
		if !strings.HasPrefix(etag, `"`) {
			t.Errorf("GET %s sem ETag: %q", c.path, etag)
			continue
		}
		again := pedir(http.MethodGet, c.path, func(r *http.Request) { r.Header.Set("If-None-Match", etag) })
		if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Errorf("GET %s com If-None-Match: %d, quero 304", c.path, again.Code)
		}
	}

	// Nenhum caminho vem do pedido: o que não é do build é o 404 da API.
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/index.html"}, {http.MethodGet, "/assets/nao-existe.js"}, {http.MethodGet, "/assets/"},
		{http.MethodGet, "/assets/index-teste1.js.map"}, {http.MethodGet, "/historico"}, {http.MethodGet, "/SW.JS"},
		{http.MethodGet, "/assets/sw.js"}, {http.MethodGet, "/icone-512.png"}, {http.MethodGet, "/pagina.js"},
		{http.MethodGet, "/assets/%2e%2e%2fvite.config.ts"},
		{http.MethodPost, "/"}, {http.MethodPost, "/assets/index-teste1.js"}, {http.MethodPut, "/favicon.svg"},
		{http.MethodDelete, "/licencas-fontes.txt"}, {http.MethodPost, "/sw.js"}, {http.MethodPut, "/manifest.webmanifest"},
	} {
		rec := pedir(r.method, r.path)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"rota não encontrada"`) {
			t.Errorf("%s %s: %d %q, quero o 404 em JSON", r.method, r.path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Allow") != "" {
			t.Errorf("%s %s anuncia métodos: %q", r.method, r.path, rec.Header().Get("Allow"))
		}
	}

	// O estilo das páginas fixas do login que não deu continua lá.
	if rec := pedir(http.MethodGet, "/pagina.css"); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Errorf("GET /pagina.css: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// As rotas do app estão na tabela, públicas.
	var app []string
	for _, rt := range c.Rotas() {
		switch rt.Padrao {
		case "GET /{$}", "GET /assets/{arquivo}", "GET /favicon.svg", "GET /licencas-fontes.txt",
			"GET /sw.js", "GET /manifest.webmanifest", "GET /icone-192.png":
			app = append(app, rt.Padrao)
			if rt.Acesso != server.AcessoPublico {
				t.Errorf("%s: acesso %s, quero público", rt.Padrao, rt.Acesso)
			}
		}
	}
	if got := strings.Join(app, ","); got != "GET /{$},GET /assets/{arquivo},GET /favicon.svg,GET /icone-192.png,"+
		"GET /licencas-fontes.txt,GET /manifest.webmanifest,GET /sw.js" {
		t.Errorf("as rotas do app são %v", app)
	}
}
