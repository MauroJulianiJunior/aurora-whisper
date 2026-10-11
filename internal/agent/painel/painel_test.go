package painel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const porta = 51234

// --- bilhete ------------------------------------------------------------

func TestBilhete_ValeUmaVezSo(t *testing.T) {
	b := NovosBilhetes()
	bilhete, err := b.Emitir()
	if err != nil {
		t.Fatal(err)
	}
	if !b.Trocar(bilhete) {
		t.Fatal("bilhete recém-emitido não trocou")
	}
	if b.Trocar(bilhete) {
		t.Fatal("o mesmo bilhete trocou duas vezes — um bilhete no histórico do navegador ainda abriria o painel")
	}
}

func TestBilhete_Expira(t *testing.T) {
	agora := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	b := &Bilhetes{pendentes: map[string]pendente{}, agora: func() time.Time { return agora }}
	bilhete, _ := b.Emitir()

	agora = agora.Add(ValidadeDoBilhete)
	if b.Trocar(bilhete) {
		t.Fatal("bilhete trocou depois de expirar")
	}
}

func TestBilhete_InventadoNaoTroca(t *testing.T) {
	b := NovosBilhetes()
	_, _ = b.Emitir()
	for _, falso := range []string{"", "abc", strings.Repeat("0", 64), strings.Repeat("z", 64)} {
		if b.Trocar(falso) {
			t.Errorf("bilhete inventado %q trocou", falso)
		}
	}
}

// TestBilhete_Limitado: cada clique em "Abrir" emite um. Sem limite, o
// mapa cresceria sem fim.
func TestBilhete_Limitado(t *testing.T) {
	b := NovosBilhetes()
	var primeiro string
	for i := 0; i < maxPendentes+5; i++ {
		x, _ := b.Emitir()
		if i == 0 {
			primeiro = x
		}
	}
	if n := len(b.pendentes); n > maxPendentes {
		t.Fatalf("%d bilhetes pendentes, limite %d", n, maxPendentes)
	}
	if b.Trocar(primeiro) {
		t.Error("o bilhete mais antigo devia ter sido descartado")
	}
}

// --- rotas ----------------------------------------------------------------

func montar(t *testing.T, dir string) (http.Handler, *Bilhetes) {
	t.Helper()
	b := NovosBilhetes()
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "API:"+r.URL.Path)
	})
	return Handler(Config{Porta: porta, Token: "tok-secreto", Bilhetes: b, API: api, DirPainel: dir}), b
}

func req(metodo, caminho, host string, corpo io.Reader) *http.Request {
	r := httptest.NewRequest(metodo, caminho, corpo)
	r.Host = host
	return r
}

func dirComPainel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>PAINEL</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

var hostCerto = "127.0.0.1:51234"

// TestRotas_FluxoDoBilhete é o caminho inteiro: a página carrega sem
// token, troca o bilhete, e com o token alcança a API.
func TestRotas_FluxoDoBilhete(t *testing.T) {
	h, b := montar(t, dirComPainel(t))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/?t=qualquer", hostCerto, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PAINEL") {
		t.Fatalf("GET / = %d %q", w.Code, w.Body.String())
	}

	bilhete, _ := b.Emitir()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req("POST", "/api/sessao", hostCerto, strings.NewReader(`{"bilhete":"`+bilhete+`"}`)))
	if w.Code != 200 {
		t.Fatalf("troca do bilhete = %d %q", w.Code, w.Body.String())
	}
	var resp struct{ Token string }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Token != "tok-secreto" {
		t.Fatalf("token devolvido = %q", resp.Token)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("a resposta com o token pode ser guardada em cache")
	}

	w = httptest.NewRecorder()
	r := req("GET", "/api/history", hostCerto, nil)
	r.Header.Set("Authorization", "Bearer "+resp.Token)
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "API:/api/history" {
		t.Fatalf("API com token = %d %q", w.Code, w.Body.String())
	}
}

// TestRotas_APISemTokenRecusada: a Local API continua exigindo o token —
// abrir os arquivos do painel sem token não pode abrir a API junto.
func TestRotas_APISemTokenRecusada(t *testing.T) {
	h, _ := montar(t, dirComPainel(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/api/history", hostCerto, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("API sem token = %d, want 401", w.Code)
	}
}

// TestRotas_HostErradoRecusadoEmTudo: DNS rebinding (THR-002). Um site
// que aponte um domínio para 127.0.0.1 manda Host com o domínio dele.
// Nem os arquivos do painel podem responder.
func TestRotas_HostErradoRecusadoEmTudo(t *testing.T) {
	h, b := montar(t, dirComPainel(t))
	bilhete, _ := b.Emitir()
	for _, r := range []*http.Request{
		req("GET", "/", "evil.example:51234", nil),
		req("GET", "/assets/app.js", "evil.example:51234", nil),
		req("POST", "/api/sessao", "evil.example:51234", strings.NewReader(`{"bilhete":"`+bilhete+`"}`)),
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s com Host estranho = %d, want 403", r.Method, r.URL.Path, w.Code)
		}
	}
	// E o bilhete não foi gasto pela tentativa barrada.
	if !b.Trocar(bilhete) {
		t.Error("a tentativa com Host errado consumiu o bilhete")
	}
}

// TestRotas_OrigemEstranhaNaoTrocaBilhete: uma página de outro site
// chamando /api/sessao via fetch manda Origin do site dela.
func TestRotas_OrigemEstranhaNaoTrocaBilhete(t *testing.T) {
	h, b := montar(t, dirComPainel(t))
	bilhete, _ := b.Emitir()
	r := req("POST", "/api/sessao", hostCerto, strings.NewReader(`{"bilhete":"`+bilhete+`"}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("origem estranha trocou o bilhete")
	}
}

// TestRotas_ReferrerNuncaVaza: a página abre com o bilhete na URL.
func TestRotas_ReferrerNuncaVaza(t *testing.T) {
	h, _ := montar(t, dirComPainel(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/?t=bilhete", hostCerto, nil))
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q — o bilhete iria no Referer de qualquer recurso externo", got)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("o painel pode ser embutido em frame de outro site")
	}
}

// TestRotas_SemBuildExplica: um 404 mudo não diria à pessoa o que fazer.
func TestRotas_SemBuildExplica(t *testing.T) {
	h, _ := montar(t, t.TempDir())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/", hostCerto, nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "pnpm") {
		t.Fatalf("sem build = %d, corpo sem a instrução de build", w.Code)
	}
}

// TestRotas_SemBuildNaoTemCaminhoDeMaquina: a página dizia onde construir com
// o caminho da máquina de quem desenvolve (C:\Desenvolvimento\...), o que
// não serve a quem instalou o programa e confunde quem cola a tela num
// relato. O texto é relativo à pasta do repositório.
func TestRotas_SemBuildNaoTemCaminhoDeMaquina(t *testing.T) {
	h, _ := montar(t, t.TempDir())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/", hostCerto, nil))
	corpo := w.Body.String()
	for _, proibido := range []string{`C:\`, `C:/`, "Desenvolvimento", `\Users`, "/Users/"} {
		if strings.Contains(corpo, proibido) {
			t.Errorf("a página sem painel tem %q:\n%s", proibido, corpo)
		}
	}
	if !strings.Contains(corpo, "pnpm -C web build") {
		t.Errorf("a página sem painel perdeu a instrução relativa:\n%s", corpo)
	}
}

// TestRotas_SemBuildTemCSPQueLiberaOEstiloDela: a página 503 se formata
// com atributos style, e a CSP geral do painel (default-src 'self', sem
// style-src) os bloqueava — a página saía crua e o console registrava
// violações (revisão de 29-set-2026). A resposta 503 manda uma CSP
// própria. E a do painel de verdade não pode ter afrouxado junto.
func TestRotas_SemBuildTemCSPQueLiberaOEstiloDela(t *testing.T) {
	h, _ := montar(t, t.TempDir())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/", hostCerto, nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("sem build = %d, want 503", w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(w.Body.String(), "style=") && !strings.Contains(csp, "style-src 'unsafe-inline'") {
		t.Errorf("a página 503 usa style=, e a CSP dela os bloqueia: %q", csp)
	}
	for _, d := range []string{"default-src 'none'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP da página 503 sem %q: %q", d, csp)
		}
	}
	if strings.Contains(w.Body.String(), "<script") {
		t.Error("a página 503 ganhou script — a CSP dela foi pensada para uma página sem script")
	}

	h, _ = montar(t, dirComPainel(t))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/", hostCerto, nil))
	if csp := w.Header().Get("Content-Security-Policy"); strings.Contains(csp, "unsafe-inline") {
		t.Errorf("a CSP do painel construído afrouxou junto: %q", csp)
	}
}

// TestRotas_CaminhoDeReactVolta ao index: /historico é rota do React, não
// arquivo.
func TestRotas_CaminhoDeReactVoltaAoIndice(t *testing.T) {
	h, _ := montar(t, dirComPainel(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/historico", hostCerto, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PAINEL") {
		t.Fatalf("/historico = %d %q", w.Code, w.Body.String())
	}
}

// TestRotas_ServeOsArquivosDoBuild: a validação do caminho relativo
// (filepath.Localize, revisão de 29-set-2026) não pode barrar o que é
// legítimo — um asset numa subpasta continua saindo como arquivo, e não
// como o index da regra de página única.
func TestRotas_ServeOsArquivosDoBuild(t *testing.T) {
	h, _ := montar(t, dirComPainel(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/assets/app.js", hostCerto, nil))
	if w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("/assets/app.js = %d %q", w.Code, w.Body.String())
	}
}

// TestRotas_SwJsE404: o painel do PC nunca tem service worker (ADR-019, F5).
// O /sw.js é 404, e não o index.html da página única (que até a F5 saía com
// 200), com ou sem um sw.js na pasta (um build da nuvem copiado para
// bin\painel por engano). O resto do painel continua igual.
func TestRotas_SwJsE404(t *testing.T) {
	for _, comArquivo := range []bool{false, true} {
		dir := dirComPainel(t)
		if comArquivo {
			if err := os.WriteFile(filepath.Join(dir, "sw.js"), []byte("self.addEventListener('fetch', () => {})"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		h, _ := montar(t, dir)
		for _, c := range []string{"/sw.js", "/SW.JS", "/Sw.Js"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req("GET", c, hostCerto, nil))
			corpo := w.Body.String()
			if w.Code != http.StatusNotFound || strings.Contains(corpo, "addEventListener") || strings.Contains(corpo, "PAINEL") {
				t.Errorf("com o arquivo %v: GET %s = %d %q, quero 404 sem o sw.js nem o index", comArquivo, c, w.Code, corpo)
			}
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("GET %s: Cache-Control %q, quero no-store", c, got)
			}
			if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
				t.Errorf("GET %s sem a CSP do painel: %q", c, csp)
			}
		}
		// Atrás do Host, como os outros arquivos.
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req("GET", "/sw.js", "evil.example", nil))
		if w.Code == http.StatusOK {
			t.Errorf("GET /sw.js com Host estranho = %d", w.Code)
		}
		// O resto do painel: o índice, a rota do React e o asset.
		for c, quer := range map[string]string{"/": "PAINEL", "/historico": "PAINEL", "/sw": "PAINEL", "/assets/app.js": "console.log(1)"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req("GET", c, hostCerto, nil))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), quer) {
				t.Errorf("com o arquivo %v: GET %s = %d %q, quero 200 com %q", comArquivo, c, w.Code, w.Body.String(), quer)
			}
		}
	}
}

// TestRotas_NaoSaiDaPasta: nenhum caminho alcança arquivo fora do build.
func TestRotas_NaoSaiDaPasta(t *testing.T) {
	raiz := t.TempDir()
	dir := filepath.Join(raiz, "painel")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("PAINEL"), 0o600)
	_ = os.WriteFile(filepath.Join(raiz, "segredo.txt"), []byte("SEGREDO"), 0o600)
	h, _ := montar(t, dir)
	for _, c := range []string{"/../segredo.txt", "/..%2fsegredo.txt", "/assets/../../segredo.txt", "/..%5Csegredo.txt", "/assets%5C..%5C..%5Csegredo.txt"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req("GET", c, hostCerto, nil))
		if strings.Contains(w.Body.String(), "SEGREDO") {
			t.Errorf("%s vazou um arquivo de fora da pasta do painel", c)
		}
	}

	// Não vazar o conteúdo não basta: a resposta também não pode dizer
	// SE o arquivo existe. Até a revisão de 29-set-2026, com %5C, o
	// os.Stat rodava fora da pasta e o status entregava a resposta: 404
	// para o que existe (o http.Dir recusava a "\"), 400 para o que não
	// existe (o ServeFile do index recusava o ".."). Um oráculo de
	// existência, sem token, para qualquer arquivo do disco.
	//
	// segredo.txt EXISTE fora da pasta; naoexiste.txt não. Pela mesma
	// forma de URL, os dois precisam receber o mesmo status e o mesmo
	// corpo.
	for _, forma := range []string{"/..%5C", "/assets%5C..%5C..%5C", "/%2e%2e%5C"} {
		existe, nao := httptest.NewRecorder(), httptest.NewRecorder()
		h.ServeHTTP(existe, req("GET", forma+"segredo.txt", hostCerto, nil))
		h.ServeHTTP(nao, req("GET", forma+"naoexiste.txt", hostCerto, nil))
		if existe.Code != nao.Code || existe.Body.String() != nao.Body.String() {
			t.Errorf("%s: arquivo que existe fora da pasta = %d %q, inexistente = %d %q — a resposta diz se o arquivo existe",
				forma, existe.Code, existe.Body.String(), nao.Code, nao.Body.String())
		}
	}
}

// TestBilhete_DescarteDeterministicoNoMesmoTique trava o defeito que fez
// TestBilhete_Limitado falhar de vez em quando. Com o relógio PARADO,
// todos os bilhetes têm o mesmo horário: se a antiguidade viesse do
// horário, o descarte dependeria da ordem de iteração do mapa.
func TestBilhete_DescarteDeterministicoNoMesmoTique(t *testing.T) {
	parado := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for rodada := 0; rodada < 50; rodada++ {
		b := &Bilhetes{pendentes: map[string]pendente{}, agora: func() time.Time { return parado }}
		primeiro, _ := b.Emitir()
		for i := 0; i < maxPendentes; i++ {
			_, _ = b.Emitir()
		}
		if b.Trocar(primeiro) {
			t.Fatalf("rodada %d: com o relógio parado o primeiro bilhete sobreviveu ao descarte", rodada)
		}
	}
}

// TestRotas_RetornoDaNuvem: o GET /nuvem/retorno (F6) chega ao handler do
// retorno sem o token da Local API e com a origem da nuvem (é o navegador
// voltando da página de aprovação), mas atrás do Host como o resto: com o
// Host de outro site (DNS rebinding) é 403 e o handler nem vê o pedido.
func TestRotas_RetornoDaNuvem(t *testing.T) {
	var vistos []string
	retorno := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vistos = append(vistos, r.URL.RawQuery)
		_, _ = io.WriteString(w, "RETORNO")
	})
	h := Handler(Config{Porta: porta, Token: "tok-secreto", Bilhetes: NovosBilhetes(), DirPainel: dirComPainel(t), Retorno: retorno,
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Errorf("a API recebeu %s", r.URL.Path) })})

	for _, host := range []string{hostCerto, "localhost:51234"} {
		r := req("GET", "/nuvem/retorno?code=c&state=s", host, nil)
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.String() != "RETORNO" {
			t.Errorf("Host %s: %d %q", host, w.Code, w.Body.String())
		}
		// Os cabeçalhos de sempre valem também aqui: o code na URL não vai
		// no Referer de nada.
		if w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("Host %s: Referrer-Policy = %q", host, w.Header().Get("Referrer-Policy"))
		}
	}
	for _, host := range []string{"evil.example:51234", "127.0.0.1:1", "127.0.0.1", "[::1]:51234"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req("GET", "/nuvem/retorno?code=c&state=s", host, nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("Host %s: %d, quero 403", host, w.Code)
		}
	}
	if len(vistos) != 2 {
		t.Errorf("o retorno viu %d pedidos; quero 2 (os do Host certo)", len(vistos))
	}

	// Sem o retorno montado (um Agent sem a nuvem), o caminho é só mais um
	// do painel.
	h, _ = montar(t, dirComPainel(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/nuvem/retorno?code=c&state=s", hostCerto, nil))
	if strings.Contains(w.Body.String(), "RETORNO") {
		t.Error("o retorno respondeu sem estar montado")
	}
}
