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

// A política de privacidade da instância (politica.go): o GET /privacidade, só
// com a cópia na configuração, público, em HTML montado pelo servidor.

// politicaDeTeste é o que a cópia da instância diz: acentos, uma tabela e uma
// marca com cara de HTML, para provar que o servidor não repassa nada cru.
const politicaDeTeste = "# Política de privacidade\n\nO responsável é **Fulano de Tal**.\n\n" +
	"| Quem | Onde |\n|---|---|\n| A VPS | Brasil |\n\n<script>alert(1)</script>\n"

func novaNuvemComPolitica(t *testing.T, politica *server.PoliticaDePrivacidade) *server.Cloud {
	t.Helper()
	mem := cloudstore.NewMemoryStore()
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	const base = "http://localhost:8080"
	c, err := server.NewCloud(server.CloudConfig{
		Store: mem, Accounts: mem, BaseURL: base,
		SessionSecret:         []byte("SEGREDO-DE-SESSAO-DO-TESTE-0123456789abcdef"),
		DeviceTokenPepper:     []byte("PEPPER-DO-TOKEN-DO-TESTE-0123456789abcdefgh"),
		Contas:                contas,
		Google:                cloudauth.NovoGoogle("cliente", "secret", base+"/auth/google/callback"),
		Painel:                contracttest.PainelDeTeste(),
		PoliticaDePrivacidade: politica,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func nuvemComPolitica(t *testing.T, politica *server.PoliticaDePrivacidade) http.Handler {
	t.Helper()
	return novaNuvemComPolitica(t, politica).Handler()
}

func pedirPolitica(h http.Handler, method, path string, cabecalhos map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Host = "localhost:8080"
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func politicaPronta(t *testing.T, texto string) *server.PoliticaDePrivacidade {
	t.Helper()
	p, err := server.NovaPoliticaDePrivacidade([]byte(texto))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPolitica_ServeAPaginaEmHTML(t *testing.T) {
	h := nuvemComPolitica(t, politicaPronta(t, politicaDeTeste))
	const csp = "default-src 'self'; img-src 'self' blob:; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'"

	// Qualquer Accept (o navegador, o curl, o fetch): a mesma página.
	for nome, cab := range map[string]map[string]string{
		"sem Accept": nil,
		"navegador":  {"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
		"markdown":   {"Accept": "text/markdown"},
	} {
		rec := pedirPolitica(h, http.MethodGet, "/privacidade", cab)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d, quero 200", nome, rec.Code)
			continue
		}
		corpo := rec.Body.String()
		for _, quero := range []string{
			"<!doctype html>", `<html lang="pt-BR">`, `href="/pagina.css"`, "<title>Política de privacidade · Aurora Whisper</title>",
			"<h1>Política de privacidade</h1>", "<strong>Fulano de Tal</strong>", "<table>", "<td>Brasil</td>",
		} {
			if !strings.Contains(corpo, quero) {
				t.Errorf("%s: a página não tem %q", nome, quero)
			}
		}
		// O que o Markdown escreveu como HTML aparece como texto, e a página
		// não tem script nem estilo embutido (a CSP não deixaria).
		if strings.Contains(corpo, "<script") || !strings.Contains(corpo, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("%s: o <script> do Markdown não saiu escapado", nome)
		}
		if strings.Contains(corpo, " style=") || strings.Contains(corpo, "<style") || strings.Contains(corpo, "<link rel=\"stylesheet\" href=\"http") {
			t.Errorf("%s: estilo embutido ou de fora", nome)
		}
		hd := rec.Header()
		if hd.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Errorf("%s: Content-Type %q", nome, hd.Get("Content-Type"))
		}
		// Conferida a cada vez (ETag, 304), como o index.html: a cópia nova,
		// depois de reiniciar a nuvem, vale na próxima leitura.
		if hd.Get("Cache-Control") != "no-cache" || !strings.HasPrefix(hd.Get("ETag"), `"`) {
			t.Errorf("%s: Cache-Control %q, ETag %q", nome, hd.Get("Cache-Control"), hd.Get("ETag"))
		}
		// A CSP de sempre, inteira, e nada de cookie.
		if hd.Get("Content-Security-Policy") != csp || hd.Get("X-Content-Type-Options") != "nosniff" ||
			hd.Get("Referrer-Policy") != "no-referrer" || hd.Get("Permissions-Policy") != "microphone=(self)" {
			t.Errorf("%s sem os cabeçalhos de segurança: %v", nome, hd)
		}
		if hd.Get("Set-Cookie") != "" {
			t.Errorf("%s grava cookie: %q", nome, hd.Get("Set-Cookie"))
		}
	}

	// O navegador que já tem esta versão recebe 304, sem corpo.
	etag := pedirPolitica(h, http.MethodGet, "/privacidade", nil).Header().Get("ETag")
	again := pedirPolitica(h, http.MethodGet, "/privacidade", map[string]string{"If-None-Match": etag})
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Errorf("com If-None-Match: %d (%d bytes), quero 304 sem corpo", again.Code, again.Body.Len())
	}

	// HEAD: o mesmo, sem corpo (é como os apps descobrem se a página existe).
	head := pedirPolitica(h, http.MethodHead, "/privacidade", nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("HEAD: %d %q", head.Code, head.Header().Get("Content-Type"))
	}

	// A busca na URL não muda nada, e o Host de outro nome é o 400 de sempre.
	if rec := pedirPolitica(h, http.MethodGet, "/privacidade?x=1", nil); rec.Code != http.StatusOK {
		t.Errorf("com busca: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/privacidade", nil)
	req.Host = "outro.exemplo.test"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Host de outro nome: %d, quero 400", rec.Code)
	}
}

// A página é pública: nenhuma credencial é pedida nem olhada, e o app, o
// pagina.css e o favicon que ela usa continuam onde estavam.
func TestPolitica_PublicaESemSessao(t *testing.T) {
	h := nuvemComPolitica(t, politicaPronta(t, politicaDeTeste))
	for nome, cab := range map[string]map[string]string{
		"Authorization de lixo": {"Authorization": "Bearer lixo"},
		"cookie de lixo":        {"Cookie": "__Host-sessao=lixo"},
	} {
		if rec := pedirPolitica(h, http.MethodGet, "/privacidade", cab); rec.Code != http.StatusOK {
			t.Errorf("%s: %d, quero 200 (pública)", nome, rec.Code)
		}
	}
	for _, p := range []string{"/", "/pagina.css", "/favicon.svg"} {
		if rec := pedirPolitica(h, http.MethodGet, p, nil); rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
}

// Sem a cópia, a rota não existe: o 404 em JSON das rotas não montadas, e nada
// mais muda no app.
func TestPolitica_SemACopiaNaoHaRota(t *testing.T) {
	h := nuvemComPolitica(t, nil)
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec := pedirPolitica(h, m, "/privacidade", nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, quero 404", m, rec.Code)
		}
		if m == http.MethodGet && !strings.Contains(rec.Body.String(), `"rota não encontrada"`) {
			t.Errorf("%s: %q, quero o 404 em JSON", m, rec.Body.String())
		}
	}
	if rec := pedirPolitica(h, http.MethodGet, "/", nil); rec.Code != http.StatusOK {
		t.Errorf("o app em /: %d", rec.Code)
	}
}

// Com a cópia, a rota entra na tabela, pública, e só ela: outros métodos, a
// barra no fim e outra caixa continuam sendo o 404 (nenhum caminho responde 405).
func TestPolitica_NaTabelaPublicaESoOGET(t *testing.T) {
	c := novaNuvemComPolitica(t, politicaPronta(t, politicaDeTeste))
	var achou []server.Rota
	for _, rt := range c.Rotas() {
		if strings.Contains(rt.Padrao, "privacidade") {
			achou = append(achou, rt)
		}
	}
	if len(achou) != 1 || achou[0].Padrao != "GET "+server.CaminhoDaPolitica || achou[0].Acesso != server.AcessoPublico {
		t.Fatalf("a tabela tem %v; quero só GET /privacidade, público", achou)
	}
	h := c.Handler()
	for _, r := range []struct{ method, path string }{
		{http.MethodPost, "/privacidade"}, {http.MethodPut, "/privacidade"}, {http.MethodDelete, "/privacidade"},
		{http.MethodPatch, "/privacidade"}, {http.MethodGet, "/privacidade/"}, {http.MethodGet, "/Privacidade"},
		{http.MethodGet, "/privacidade.md"}, {http.MethodGet, "/privacidade/x"},
	} {
		rec := pedirPolitica(h, r.method, r.path, nil)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d, quero 404 (ou o 403 da proteção contra CSRF, no que muda estado)", r.method, r.path, rec.Code)
		}
		if rec.Header().Get("Allow") != "" {
			t.Errorf("%s %s anuncia métodos: %q", r.method, r.path, rec.Header().Get("Allow"))
		}
	}
}

func TestNovaPoliticaDePrivacidade_Conferencias(t *testing.T) {
	servido := func(t *testing.T, raw string) string {
		t.Helper()
		p, err := server.NovaPoliticaDePrivacidade([]byte(raw))
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		return pedirPolitica(nuvemComPolitica(t, p), http.MethodGet, "/privacidade", nil).Body.String()
	}

	t.Run("normaliza o que o Windows grava", func(t *testing.T) {
		for nome, c := range map[string]struct{ raw, quero string }{
			"BOM":                {"\xEF\xBB\xBF# Título\n", "<h1>Título</h1>"},
			"CRLF":               {"# Título\r\n\r\nTexto.\r\n", "<h1>Título</h1>\n<p>Texto.</p>"},
			"CR solto":           {"Linha 1\rLinha 2\n", "<p>Linha 1 Linha 2</p>"},
			"sem o fim de linha": {"# Título", "<h1>Título</h1>"},
			"acentos e emoji":    {"Ação, coração e 🙂\n", "<p>Ação, coração e 🙂</p>"},
			"tab":                {"a\tb\n", "<p>a\tb</p>"},
			"HTML vira texto":    {"<b>oi</b> & <img src=x onerror=alert(1)>\n", "<p>&lt;b&gt;oi&lt;/b&gt; &amp; &lt;img src=x onerror=alert(1)&gt;</p>"},
			"no limite exato":    {strings.Repeat("a", server.MaxPolitica-1) + "\n", "<p>" + strings.Repeat("a", server.MaxPolitica-1) + "</p>"},
		} {
			if got := servido(t, c.raw); !strings.Contains(got, c.quero) {
				t.Errorf("%s: servido %q..., quero conter %q", nome, got[:min(len(got), 300)], c.quero)
			}
		}
	})

	// A parte interna do modelo, a partir do título "Os textos do app sobre
	// privacidade", nunca é publicada, nem entra na conferência dos campos.
	t.Run("corta os textos internos do modelo", func(t *testing.T) {
		got := servido(t, "# Política\n\nPública.\n\n---\n\n# Os textos do app sobre privacidade\n\nINTERNO [A PREENCHER: x]\n\n## A folha do aceite\n\nINTERNO-2\n")
		if !strings.Contains(got, "Pública.") || strings.Contains(got, "INTERNO") || strings.Contains(got, "folha do aceite") || strings.Contains(got, "Os textos do app") {
			t.Errorf("a página tem o que é interno: %q", got)
		}
		// Só o título exato, numa linha só, corta: citá-lo no meio do texto não.
		got = servido(t, "# Política\n\nVeja \"# Os textos do app sobre privacidade\" no modelo.\n\nFim-do-texto.\n")
		if !strings.Contains(got, "Fim-do-texto.") {
			t.Error("cortou numa citação do título")
		}
	})

	t.Run("recusa o que não é uma política", func(t *testing.T) {
		const segredo = "CONTEUDO-SECRETO-DO-ARQUIVO"
		for nome, c := range map[string]struct {
			raw  string
			erro error
		}{
			"vazio":                  {"", server.ErrPoliticaVazia},
			"só espaços e quebras":   {" \n\t\r\n  \n", server.ErrPoliticaVazia},
			"só o BOM":               {"\xEF\xBB\xBF\n", server.ErrPoliticaVazia},
			"só os textos internos":  {"# Os textos do app sobre privacidade\n\nINTERNO\n", server.ErrPoliticaVazia},
			"passa do limite":        {strings.Repeat("a", server.MaxPolitica+1), server.ErrPoliticaGrande},
			"passa do limite (BOM)":  {"\xEF\xBB\xBF" + strings.Repeat("a", server.MaxPolitica), server.ErrPoliticaGrande},
			"Latin-1":                {segredo + " a\xe7\xe3o\n", server.ErrPoliticaNaoUTF8},
			"byte solto":             {segredo + "\xff\n", server.ErrPoliticaNaoUTF8},
			"UTF-16":                 {"\xff\xfe#\x00 \x00", server.ErrPoliticaNaoUTF8},
			"NUL":                    {segredo + "\x00\n", server.ErrPoliticaBinaria},
			"ESC":                    {segredo + "\x1b[31m\n", server.ErrPoliticaBinaria},
			"DEL":                    {segredo + "\x7f\n", server.ErrPoliticaBinaria},
			"controle C1":            {segredo + "\u0085\n", server.ErrPoliticaBinaria},
			"campo do modelo":        {segredo + " **[A PREENCHER: nome do responsável]**\n", server.ErrPoliticaModelo},
			"campo, se for o caso":   {segredo + " **[A PREENCHER, se for o caso: servidor]**\n", server.ErrPoliticaModelo},
			"nota do topo do modelo": {segredo + " preencha os campos **[A PREENCHER]**\n", server.ErrPoliticaModelo},
			"campo do modelo, longe": {segredo + "\n\n" + strings.Repeat("x\n", 1000) + "[A PREENCHER: o e-mail]\n", server.ErrPoliticaModelo},
		} {
			p, err := server.NovaPoliticaDePrivacidade([]byte(c.raw))
			if p != nil || err != c.erro {
				t.Errorf("%s: %v, %v; quero o erro %v", nome, p, err, c.erro)
				continue
			}
			// A mensagem diz o problema, nunca o conteúdo.
			if strings.Contains(err.Error(), segredo) || strings.Contains(err.Error(), "responsável") {
				t.Errorf("%s: a mensagem traz o conteúdo: %q", nome, err)
			}
		}
	})
}
