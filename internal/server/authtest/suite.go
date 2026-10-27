package authtest

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Run roda a suíte contra o Backend. Cada caso monta o próprio mundo, com
// e-mails únicos, sobre o mesmo Backend.
func Run(t *testing.T, b Backend) {
	cases := []struct {
		name string
		fn   func(t *testing.T, b Backend)
	}{
		{"Login_CaminhoFeliz", testCaminhoFeliz},
		{"Login_ForaDaListaNaoCriaNada", testForaDaLista},
		{"Login_SubDiferenteNaoCriaNada", testSubDiferente},
		{"Login_EmailNaoVerificadoNaoCriaNada", testEmailNaoVerificado},
		{"Login_StateReusadoVencidoEPre", testStateEPre},
		{"Login_ErrosDoGoogleEDoIDToken", testErrosDoGoogle},
		{"Login_Volta", testVolta},
		{"Login_CorpoMalFormadoTemFraseFixa", testCorpoMalFormado},
		{"Login_ContaDesativada", testContaDesativada},
		{"Sessao_NovaACadaLogin", testSessaoNovaACadaLogin},
		{"Sessao_SairListarEDerrubar", testSairListarDerrubar},
		{"Sessao_SemUsoETeto", testSemUsoETeto},
		{"Limite_LoginPorIPETotal", testLimiteDoLogin},
		{"Limite_Push", testLimiteDoPush},
		{"Lista_TirarOEmailCortaOAcesso", testTirarDaLista},
		{"Escopo_TabelaDeRotas", testEscopo},
		{"Escopo_MeComTokenEComSessao", testMe},
		{"CSRF_OutraOrigemENenhumCORS", testCSRF},
		{"Cabecalhos", testCabecalhos},
		{"Host_Estranho", testHost},
		{"RotasNaoMontadas_404", testNaoMontadas},
		{"ApagarMeusDados", testApagarDados},
		{"Log_SemSegredoNemEmail", testLog},
		{"Pagina", testPagina},
		// F6: o pareamento de um PC e os aparelhos (pareamento.go).
		{"Pareamento_CaminhoFeliz", testParearCaminhoFeliz},
		{"Pareamento_LinkCopiadoNaoConecta", testParearLinkCopiado},
		{"Pareamento_CodigoUsadoVencidoOuNaoAprovado", testParearCodigoQueNaoVale},
		{"Pareamento_AprovarDuasVezesOuDepoisDoPrazo", testParearAprovar},
		{"Pareamento_InicioValidadoSemGravar", testParearInicioValidado},
		{"Pareamento_Limites", testParearLimites},
		{"Pareamento_ContaForaDaListaNaTroca", testParearForaDaLista},
		{"Aparelhos_EscopoDoToken", testAparelhosEscopo},
		{"Aparelhos_RevogarEParearDeNovo", testAparelhosRevogar},
		{"Aparelhos_TokenSemUso180Dias", testAparelhos180Dias},
		{"Pareamento_ArrumarEApagarMeusDados", testParearArrumar},
		{"Pareamento_LogSemSegredo", testParearLog},
		{"Pareamento_PaginaDeAprovar", testParearPagina},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, b) })
	}
}

const uaAndroid = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36"
const uaWindows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

func sub() string { return "sub-" + contracttest.NewID() }

// semMudanca confere que um login recusado não criou usuário, contador nem
// sessão.
func semMudanca(t *testing.T, b Backend, antes Contagem) {
	t.Helper()
	if depois := b.Contar(t); depois != antes {
		t.Fatalf("o login recusado mudou o banco: antes %+v, depois %+v", antes, depois)
	}
}

func recusado(t *testing.T, m *mundo, n *navegador, res resposta, status int, motivo string) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status %d, quero %d: %s", res.Code, status, res.Body.String())
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "text/html") {
		t.Errorf("a recusa não é a página fixa: %q", res.Header().Get("Content-Type"))
	}
	if n.cookies["__Host-sessao"] != "" {
		t.Error("o login recusado deixou um cookie de sessão")
	}
	if c, ok := res.cookies["__Host-pre"]; !ok || c.MaxAge >= 0 {
		t.Error("o callback não apagou o __Host-pre")
	}
	if !strings.Contains(m.logs.String(), "login_recusado motivo="+motivo) {
		t.Errorf("o log não tem login_recusado motivo=%s:\n%s", motivo, m.logs.String())
	}
}

func testCaminhoFeliz(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	antes := b.Contar(t)
	n := m.navegador(uaAndroid)

	res := n.pedir(http.MethodPost, "/auth/start", map[string]any{})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /auth/start: %d %s", res.Code, res.Body.String())
	}
	pre := res.cookies["__Host-pre"]
	if pre == nil || pre.Path != "/" || !pre.HttpOnly || !pre.Secure || pre.SameSite != http.SameSiteLaxMode ||
		pre.MaxAge != 300 || pre.Domain != "" || !cloudauth.PareceSegredo(pre.Value) {
		t.Fatalf("cookie __Host-pre = %+v", pre)
	}
	var start struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &start)
	code, state := m.g.Autorizar(t, start.URL, m.g.ClaimsBoas(sub(), strings.ToUpper(m.emailA)))

	res = n.voltar(url.Values{"code": {code}, "state": {state}})
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d Location %q: %s", res.Code, res.Header().Get("Location"), res.Body.String())
	}
	sess := res.cookies["__Host-sessao"]
	if sess == nil || sess.Path != "/" || !sess.HttpOnly || !sess.Secure || sess.SameSite != http.SameSiteLaxMode ||
		sess.Domain != "" || sess.MaxAge != int(server.SessaoTeto.Seconds()) || !cloudauth.PareceSegredo(sess.Value) {
		t.Fatalf("cookie __Host-sessao = %+v", sess)
	}
	if c := res.cookies["__Host-pre"]; c == nil || c.MaxAge >= 0 {
		t.Fatal("o callback não apagou o __Host-pre")
	}
	if trocas, recusas := m.g.Contagens(); trocas != 1 || recusas != 0 {
		t.Fatalf("o Google falso viu %d trocas e recusou %d (o verificador, o client e o redirect têm de bater)", trocas, recusas)
	}

	// O usuário, o contador e a sessão nasceram juntos.
	if depois := b.Contar(t); depois.Usuarios != antes.Usuarios+1 || depois.Contadores != antes.Contadores+1 || depois.Sessoes != antes.Sessoes+1 {
		t.Fatalf("antes %+v, depois %+v: quero um usuário, um contador e uma sessão a mais", antes, depois)
	}
	if status, email := n.me(); status != http.StatusOK || email != m.emailA {
		t.Fatalf("GET /me = %d %q, quero 200 e o e-mail em minúsculas", status, email)
	}
	if ep, _ := m.epoch(n.userID()); len(ep) != 36 || ep[14] != '7' {
		t.Errorf("sync_epoch = %q, quero UUID v7", ep)
	}

	// O valor do cookie (e o do pre, e o state) não está em lugar nenhum do
	// banco: nem em claro, nem os bytes, nem o SHA-256 puro (o banco guarda o
	// HMAC da sessão e o SHA-256 só do state e do pre).
	dump := b.Despejo(t)
	raw, _ := base64.RawURLEncoding.DecodeString(sess.Value)
	for name, v := range map[string]string{
		"o cookie": sess.Value, "os bytes do cookie": hex.EncodeToString(raw),
		"o SHA-256 do cookie": hex.EncodeToString(cloudauth.Hash(sess.Value)),
		"o pre":               pre.Value, "o state": state, "o code": code,
	} {
		if strings.Contains(dump, v) {
			t.Errorf("%s está no banco", name)
		}
	}
	if !strings.Contains(dump, hex.EncodeToString(cloudauth.HMAC(segredoDeSessao, sess.Value))) {
		t.Error("o HMAC da sessão não está no banco (o despejo não vê a tabela?)")
	}
	// O mesmo login de novo não cria outro usuário.
	n2 := m.navegador(uaWindows)
	n2.entrarComo(sub(), m.emailB)
	if c := b.Contar(t); c.Usuarios != antes.Usuarios+2 {
		t.Fatalf("usuários = %d, quero %d", c.Usuarios, antes.Usuarios+2)
	}
}

func testForaDaLista(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	antes := b.Contar(t)
	n := m.navegador(uaAndroid)
	res := n.entrar(m.g.ClaimsBoas(sub(), m.emailFora))
	recusado(t, m, n, res, http.StatusForbidden, "fora_da_lista")
	semMudanca(t, b, antes)
	if strings.Contains(res.Body.String(), m.emailFora) {
		t.Error("a página de recusa ecoa o e-mail")
	}
	if status, _ := n.me(); status != http.StatusUnauthorized {
		t.Errorf("GET /me depois da recusa: %d, quero 401", status)
	}
}

func testSubDiferente(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	first := m.navegador(uaAndroid)
	first.entrarComo(sub(), m.emailA)
	antes := b.Contar(t)
	n := m.navegador(uaWindows)
	res := n.entrar(m.g.ClaimsBoas(sub(), m.emailA))
	recusado(t, m, n, res, http.StatusForbidden, "sub_diferente")
	semMudanca(t, b, antes)
}

func testEmailNaoVerificado(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	antes := b.Contar(t)
	n := m.navegador(uaAndroid)
	c := m.g.ClaimsBoas(sub(), m.emailA)
	c["email_verified"] = false
	recusado(t, m, n, n.entrar(c), http.StatusForbidden, "email_nao_verificado")
	semMudanca(t, b, antes)
}

func testStateEPre(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	antes := b.Contar(t)

	// O state reusado: o primeiro callback entra, o segundo (o mesmo code e
	// state, de novo) é recusado.
	n := m.navegador(uaAndroid)
	authURL := n.comecar("")
	code, state := m.g.Autorizar(t, authURL, m.g.ClaimsBoas(sub(), m.emailA))
	pre := n.cookies["__Host-pre"]
	if res := n.voltar(url.Values{"code": {code}, "state": {state}}); res.Code != http.StatusSeeOther {
		t.Fatalf("primeiro callback: %d", res.Code)
	}
	again := m.navegador(uaAndroid)
	again.cookies["__Host-pre"] = pre
	recusado(t, m, again, again.voltar(url.Values{"code": {code}, "state": {state}}), http.StatusBadRequest, "state_invalido")
	antes = b.Contar(t)

	// Vencido: 5 minutos depois do início.
	n = m.navegador(uaAndroid)
	code, state = m.g.Autorizar(t, n.comecar(""), m.g.ClaimsBoas(sub(), m.emailB))
	m.clk.andar(server.PrazoDoLogin)
	recusado(t, m, n, n.voltar(url.Values{"code": {code}, "state": {state}}), http.StatusBadRequest, "state_invalido")

	// Sem o __Host-pre (outro navegador termina o login que este começou: o
	// login CSRF).
	n = m.navegador(uaAndroid)
	code, state = m.g.Autorizar(t, n.comecar(""), m.g.ClaimsBoas(sub(), m.emailB))
	delete(n.cookies, "__Host-pre")
	recusado(t, m, n, n.voltar(url.Values{"code": {code}, "state": {state}}), http.StatusBadRequest, "state_invalido")

	// O __Host-pre de outro login (trocado).
	n = m.navegador(uaAndroid)
	other := m.navegador(uaAndroid)
	other.comecar("")
	code, state = m.g.Autorizar(t, n.comecar(""), m.g.ClaimsBoas(sub(), m.emailB))
	n.cookies["__Host-pre"] = other.cookies["__Host-pre"]
	recusado(t, m, n, n.voltar(url.Values{"code": {code}, "state": {state}}), http.StatusBadRequest, "state_invalido")

	// State que nunca existiu, vazio ou lixo.
	for _, s := range []string{cloudauth.NovoSegredo(), "", "curto", strings.Repeat("a", 5000)} {
		n = m.navegador(uaAndroid)
		n.comecar("")
		recusado(t, m, n, n.voltar(url.Values{"code": {"x"}, "state": {s}}), http.StatusBadRequest, "state_invalido")
	}
	semMudanca(t, b, antes)
	// Nenhum desses chegou a trocar o code no Google, fora o primeiro.
	if trocas, _ := m.g.Contagens(); trocas != 1 {
		t.Errorf("%d trocas no Google, quero 1: um state recusado não troca o code", trocas)
	}

	// Quem obtém o state de outra pessoa (a URL de retorno vazada) e o manda
	// sem o __Host-pre dela, ou com o pre de um login seu, é recusado sem
	// queimar o login dela: o pre está na busca do login pendente, e nada é
	// consumido. Ela ainda entra.
	vitima := m.navegador(uaAndroid)
	code, state = m.g.Autorizar(t, vitima.comecar(""), m.g.ClaimsBoas(sub(), m.emailB))
	atacante := m.navegador(uaWindows)
	atacante.ip = "198.51.100.66:4444"
	recusado(t, m, atacante, atacante.voltar(url.Values{"code": {code}, "state": {state}}), http.StatusBadRequest, "state_invalido")
	atacante.comecar("")
	recusado(t, m, atacante, atacante.voltar(url.Values{"code": {code}, "state": {state}}), http.StatusBadRequest, "state_invalido")
	if res := vitima.voltar(url.Values{"code": {code}, "state": {state}}); res.Code != http.StatusSeeOther || vitima.cookies["__Host-sessao"] == "" {
		t.Fatalf("a dona do login, depois das tentativas sem o pre dela: %d %s; quero 303 e a sessão", res.Code, res.Body.String())
	}
}

func testErrosDoGoogle(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	antes := b.Contar(t)

	// A pessoa cancelou no Google.
	n := m.navegador(uaAndroid)
	_, state := m.g.Autorizar(t, n.comecar(""), nil)
	recusado(t, m, n, n.voltar(url.Values{"error": {"access_denied"}, "state": {state}}), http.StatusBadRequest, "cancelado")

	// O endpoint de token falha.
	m.g.Falhar(true)
	n = m.navegador(uaAndroid)
	recusado(t, m, n, n.entrar(m.g.ClaimsBoas(sub(), m.emailA)), http.StatusBadGateway, "google_falhou")
	m.g.Falhar(false)

	// Sem code.
	n = m.navegador(uaAndroid)
	_, state = m.g.Autorizar(t, n.comecar(""), nil)
	recusado(t, m, n, n.voltar(url.Values{"state": {state}}), http.StatusBadGateway, "google_falhou")

	// Cada claim errada.
	for name, mudar := range map[string]func(c map[string]any){
		"nonce": func(c map[string]any) { c["nonce"] = "outro-nonce" },
		"aud":   func(c map[string]any) { c["aud"] = "outro-client" },
		"azp":   func(c map[string]any) { c["azp"] = "outro-client" },
		"iss":   func(c map[string]any) { c["iss"] = "https://evil.example" },
		"exp":   func(c map[string]any) { c["exp"] = m.clk.Now().Add(-10 * time.Minute).Unix() },
		"iat":   func(c map[string]any) { c["iat"] = m.clk.Now().Add(10 * time.Minute).Unix() },
		"sub":   func(c map[string]any) { c["sub"] = "" },
	} {
		c := m.g.ClaimsBoas(sub(), m.emailA)
		mudar(c)
		n = m.navegador(uaAndroid)
		res := n.entrar(c)
		if res.Code != http.StatusBadRequest || n.cookies["__Host-sessao"] != "" {
			t.Errorf("claim %s errada: %d, quero 400 sem sessão", name, res.Code)
		}
	}
	if !strings.Contains(m.logs.String(), "motivo=token_invalido") {
		t.Error("o log não tem motivo=token_invalido")
	}
	semMudanca(t, b, antes)
}

func testVolta(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	const volta = "/device/authorize?pedido=0190b3a0-0000-7000-8000-00000000f001"
	code, state := m.g.Autorizar(t, n.comecar(volta), m.g.ClaimsBoas(sub(), m.emailA))
	res := n.voltar(url.Values{"code": {code}, "state": {state}})
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != volta {
		t.Fatalf("callback com volta: %d Location %q", res.Code, res.Header().Get("Location"))
	}
	for _, bad := range []string{"//evil.example", "https://evil.example", "/\\evil.example", "evil.example",
		"/x y", "/x\r\nSet-Cookie: a=b", "/x#y", "/" + strings.Repeat("a", 256)} {
		res := m.navegador(uaAndroid).pedir(http.MethodPost, "/auth/start", map[string]any{"volta": bad})
		if res.Code != http.StatusBadRequest || res.cookies["__Host-pre"] != nil {
			t.Errorf("volta %q: %d, quero 400 sem cookie", bad, res.Code)
		}
	}
	// Corpo grande demais, ou quebrado.
	if res := m.navegador(uaAndroid).pedir(http.MethodPost, "/auth/start", `{"volta":"/`+strings.Repeat("a", 2000)+`"}`); res.Code != http.StatusBadRequest {
		t.Errorf("corpo de 2 KiB: %d, quero 400", res.Code)
	}
	if res := m.navegador(uaAndroid).pedir(http.MethodPost, "/auth/start", `{`); res.Code != http.StatusBadRequest {
		t.Errorf("corpo quebrado: %d, quero 400", res.Code)
	}
	// Sem corpo nenhum, vale (a volta é opcional).
	if res := m.navegador(uaAndroid).pedir(http.MethodPost, "/auth/start", nil); res.Code != http.StatusOK {
		t.Errorf("sem corpo: %d, quero 200", res.Code)
	}
}

// testCorpoMalFormado: o 400 de um corpo que não decodifica, no POST /auth/start
// (público, sem sessão), é SEMPRE a frase fixa. A mensagem do decodificador do
// Go traz o nome de um tipo interno (authStartRequest.volta) ou um caractere do
// próprio pedido; o corpo é de quem pede, e a resposta não o ecoa, nem o log.
func testCorpoMalFormado(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	const marca = "MARCA-DO-CORPO-9F3A"
	for _, c := range []struct{ nome, corpo string }{
		{"JSON quebrado", `{`},
		{"texto que não é JSON, com a marca", marca},
		{"cortado no meio de um texto, com a marca", `{"volta":"/` + marca},
		{"tipo errado no campo", `{"volta": 5}`},
		{"objeto onde devia ser texto, com a marca", `{"volta": {"` + marca + `": 1}}`},
		{"corpo grande demais", `{"volta":"/` + strings.Repeat("a", 2000) + `"}`},
	} {
		res := m.navegador(uaAndroid).pedir(http.MethodPost, "/auth/start", c.corpo)
		if res.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, quero 400", c.nome, res.Code)
			continue
		}
		var out struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Errorf("%s: a resposta não é o bloco de erro: %v", c.nome, err)
			continue
		}
		if out.Error.Code != "BAD_REQUEST" || out.Error.Message != server.FraseDoCorpoInvalido {
			t.Errorf("%s: %q %q, quero BAD_REQUEST com a frase fixa %q", c.nome, out.Error.Code, out.Error.Message, server.FraseDoCorpoInvalido)
		}
		for _, nao := range []string{marca, "authStartRequest", "json:", "invalid character", "unexpected end", "request body too large"} {
			if strings.Contains(res.Body.String(), nao) {
				t.Errorf("%s: a resposta traz %q: %s", c.nome, nao, res.Body.String())
			}
		}
		if res.cookies["__Host-pre"] != nil {
			t.Errorf("%s: o 400 pôs o cookie do login pendente", c.nome)
		}
	}
	if strings.Contains(m.logs.String(), marca) {
		t.Error("o log traz um pedaço do corpo do pedido")
	}
}

func testContaDesativada(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	s := sub()
	n.entrarComo(s, m.emailA)
	userID := n.userID()
	_, tok := m.aparelho(userID)
	b.DisableUser(t, userID)
	if status, _ := n.me(); status != http.StatusUnauthorized {
		t.Errorf("sessão de conta desativada: %d, quero 401", status)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", tok, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("token de conta desativada: %d, quero 401", res.Code)
	}
	again := m.navegador(uaAndroid)
	recusado(t, m, again, again.entrar(m.g.ClaimsBoas(s, m.emailA)), http.StatusForbidden, "conta_desativada")
}

func testSessaoNovaACadaLogin(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	s := sub()
	n.entrarComo(s, m.emailA)
	old := n.cookies["__Host-sessao"]
	antes := b.Contar(t)
	n.entrarComo(s, m.emailA)
	if n.cookies["__Host-sessao"] == old {
		t.Fatal("o login de novo manteve o mesmo id de sessão")
	}
	if c := b.Contar(t); c.Sessoes != antes.Sessoes {
		t.Fatalf("sessões %d, quero %d: a sessão que o navegador trouxe é trocada, não somada", c.Sessoes, antes.Sessoes)
	}
	stale := m.navegador(uaAndroid)
	stale.cookies["__Host-sessao"] = old
	if status, _ := stale.me(); status != http.StatusUnauthorized {
		t.Fatalf("a sessão anterior ainda vale: %d", status)
	}
	if status, _ := n.me(); status != http.StatusOK {
		t.Fatalf("a sessão nova não vale: %d", status)
	}
}

type sessaoJSON struct {
	ID     string `json:"id"`
	Rotulo string `json:"rotulo"`
	Atual  bool   `json:"atual"`
}

func listar(t *testing.T, n *navegador) []sessaoJSON {
	t.Helper()
	res := n.pedir(http.MethodGet, "/api/v1/sessions", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /sessions: %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Items []sessaoJSON `json:"items"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Items
}

func testSairListarDerrubar(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	cel, pc := m.navegador(uaAndroid), m.navegador(uaWindows)
	s := sub()
	cel.entrarComo(s, m.emailA)
	m.clk.andar(time.Minute)
	pc.entrarComo(s, m.emailA)
	outra := m.navegador(uaAndroid)
	outra.entrarComo(sub(), m.emailB)

	list := listar(t, cel)
	if len(list) != 2 {
		t.Fatalf("sessões = %+v, quero 2 (só as da conta)", list)
	}
	var celID, pcID string
	for _, s := range list {
		switch {
		case s.Atual && s.Rotulo == "Android · Chrome":
			celID = s.ID
		case !s.Atual && s.Rotulo == "Windows · Chrome":
			pcID = s.ID
		}
	}
	if celID == "" || pcID == "" {
		t.Fatalf("sessões = %+v: quero a atual (Android) e a outra (Windows)", list)
	}
	raw := cel.pedir(http.MethodGet, "/api/v1/sessions", nil).Body.String()
	for _, banned := range []string{"token", "hmac", "user_id", "expires"} {
		if strings.Contains(raw, banned) {
			t.Errorf("a lista de sessões expõe %q: %s", banned, raw)
		}
	}

	// Derrubar a sessão de outro usuário, ou uma que não existe: 404.
	outraID := listar(t, outra)[0].ID
	for _, id := range []string{outraID, contracttest.NewID(), "nao-e-uuid"} {
		if res := cel.pedir(http.MethodDelete, "/api/v1/sessions/"+id, nil); res.Code != http.StatusNotFound {
			t.Errorf("derrubar %q: %d, quero 404", id, res.Code)
		}
	}
	if status, _ := outra.me(); status != http.StatusOK {
		t.Fatal("a sessão do outro usuário caiu")
	}
	// Derrubar a do PC pelo celular: o PC recebe 401.
	if res := cel.pedir(http.MethodDelete, "/api/v1/sessions/"+pcID, nil); res.Code != http.StatusNoContent {
		t.Fatalf("derrubar a outra sessão: %d", res.Code)
	}
	if status, _ := pc.me(); status != http.StatusUnauthorized {
		t.Fatalf("a sessão derrubada ainda vale: %d", status)
	}
	if list := listar(t, cel); len(list) != 1 || list[0].ID != celID {
		t.Fatalf("depois de derrubar: %+v", list)
	}
	// Sair: apaga no servidor (o cookie velho não vale mais) e o cookie.
	old := cel.cookies["__Host-sessao"]
	res := cel.pedir(http.MethodPost, "/auth/logout", nil)
	if res.Code != http.StatusNoContent || cel.cookies["__Host-sessao"] != "" {
		t.Fatalf("sair: %d, cookie %q", res.Code, cel.cookies["__Host-sessao"])
	}
	stale := m.navegador(uaAndroid)
	stale.cookies["__Host-sessao"] = old
	if status, _ := stale.me(); status != http.StatusUnauthorized {
		t.Fatalf("depois de sair, o cookie velho vale: %d", status)
	}
	// Sair sem sessão também é 204.
	if res := m.navegador(uaAndroid).pedir(http.MethodPost, "/auth/logout", nil); res.Code != http.StatusNoContent {
		t.Errorf("sair sem sessão: %d", res.Code)
	}
	// Derrubar a própria sessão apaga o cookie.
	cel.entrarComo(s, m.emailA)
	me := listar(t, cel)
	for _, s := range me {
		if s.Atual {
			res := cel.pedir(http.MethodDelete, "/api/v1/sessions/"+strings.ToUpper(s.ID), nil)
			if res.Code != http.StatusNoContent || cel.cookies["__Host-sessao"] != "" {
				t.Fatalf("derrubar a atual: %d, cookie %q", res.Code, cel.cookies["__Host-sessao"])
			}
		}
	}
}

func testSemUsoETeto(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailA)
	// Usada a cada 29 dias, vale até o teto de 90 desde o login.
	for range 3 {
		m.clk.andar(29 * 24 * time.Hour)
		if status, _ := n.me(); status != http.StatusOK {
			t.Fatalf("depois de 29 dias sem uso: %d, quero 200", status)
		}
	}
	m.clk.andar(3 * 24 * time.Hour) // 90 dias desde o login
	if status, _ := n.me(); status != http.StatusUnauthorized || n.cookies["__Host-sessao"] != "" {
		t.Fatalf("depois do teto: %d (cookie %q), quero 401 e o cookie apagado", status, n.cookies["__Host-sessao"])
	}

	// 30 dias sem uso.
	n = m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailB)
	m.clk.andar(server.SessaoSemUso)
	if status, _ := n.me(); status != http.StatusUnauthorized {
		t.Fatalf("30 dias sem uso: %d, quero 401", status)
	}
}

func testLimiteDoLogin(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{limites: server.Limites{LoginPorIP: 3, InicioTotal: 5, PushPedidos: 1000, PushMutacoes: 100000}})
	start := func(ip string, xff ...string) resposta {
		n := m.navegador(uaAndroid)
		n.ip = ip
		return n.pedir(http.MethodPost, "/auth/start", map[string]any{}, func(r *http.Request) {
			for _, x := range xff {
				r.Header.Add("X-Forwarded-For", x)
			}
		})
	}
	for i := range 3 {
		if res := start("203.0.113.1:1000"); res.Code != http.StatusOK {
			t.Fatalf("início %d: %d", i+1, res.Code)
		}
	}
	res := start("203.0.113.1:2000")
	if res.Code != http.StatusTooManyRequests || res.Header().Get("Retry-After") != "20" {
		t.Fatalf("4º início do mesmo IP: %d, Retry-After %q; quero 429 e 20 s", res.Code, res.Header().Get("Retry-After"))
	}
	// Sem proxy confiável, o X-Forwarded-For não muda o IP.
	if res := start("203.0.113.1:3000", "198.51.100.7"); res.Code != http.StatusTooManyRequests {
		t.Fatalf("X-Forwarded-For sem proxy confiável: %d, quero 429", res.Code)
	}
	// O callback conta no mesmo balde.
	n := m.navegador(uaAndroid)
	n.ip = "203.0.113.1:4000"
	if res := n.voltar(url.Values{"state": {"x"}}); res.Code != http.StatusTooManyRequests {
		t.Fatalf("callback do IP limitado: %d, quero 429", res.Code)
	}
	// Outro IP passa, até o total (5) acabar.
	for i := range 2 {
		if res := start("203.0.113.2:1000"); res.Code != http.StatusOK {
			t.Fatalf("outro IP, início %d: %d", i+1, res.Code)
		}
	}
	if res := start("203.0.113.3:1000"); res.Code != http.StatusTooManyRequests {
		t.Fatalf("depois do total: %d, quero 429", res.Code)
	}
	// Um minuto depois, volta.
	m.clk.andar(time.Minute)
	if res := start("203.0.113.1:1000"); res.Code != http.StatusOK {
		t.Fatalf("um minuto depois: %d", res.Code)
	}

	// Com proxy confiável: o último salto do X-Forwarded-For é quem pede.
	p := novoMundo(t, b, opcoes{proxy: "192.0.2.1", limites: server.Limites{LoginPorIP: 1, InicioTotal: 100}})
	via := func(xff string) int {
		n := p.navegador(uaAndroid)
		n.ip = "192.0.2.1:443"
		return n.pedir(http.MethodPost, "/auth/start", map[string]any{}, func(r *http.Request) {
			r.Header.Set("X-Forwarded-For", xff)
		}).Code
	}
	if via("1.1.1.1, 198.51.100.1") != http.StatusOK || via("198.51.100.2") != http.StatusOK {
		t.Fatal("dois clientes atrás do proxy: o primeiro pedido de cada um devia passar")
	}
	if via("9.9.9.9, 198.51.100.1") != http.StatusTooManyRequests {
		t.Fatal("o mesmo último salto: devia contar no mesmo balde (o primeiro endereço é do cliente e não vale)")
	}
	// Atrás do proxy, o IPv6 também conta por /64.
	if via("2001:db8:aa:1::1") != http.StatusOK || via("2001:db8:aa:1::2") != http.StatusTooManyRequests {
		t.Fatal("atrás do proxy, dois endereços do mesmo /64 deviam contar no mesmo balde")
	}

	// IPv6: quem gira endereços dentro do próprio /64 conta num balde só; um
	// /64 vizinho é outro.
	v6 := novoMundo(t, b, opcoes{limites: server.Limites{LoginPorIP: 3, InicioTotal: 100}})
	start6 := func(remote string) int {
		n := v6.navegador(uaAndroid)
		n.ip = remote
		return n.pedir(http.MethodPost, "/auth/start", map[string]any{}).Code
	}
	for i, remote := range []string{"[2001:db8:1:2::1]:1000", "[2001:db8:1:2::2]:1000", "[2001:db8:1:2:ffff:ab::]:1000"} {
		if code := start6(remote); code != http.StatusOK {
			t.Fatalf("IPv6, início %d: %d", i+1, code)
		}
	}
	if code := start6("[2001:db8:1:2:abcd::9]:2000"); code != http.StatusTooManyRequests {
		t.Fatalf("4º início do mesmo /64 por outro endereço: %d, quero 429", code)
	}
	if code := start6("[2001:db8:1:3::1]:1000"); code != http.StatusOK {
		t.Fatalf("o /64 vizinho: %d, quero 200", code)
	}
}

func testLimiteDoPush(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{limites: server.Limites{LoginPorIP: 100, InicioTotal: 100, PushPedidos: 3, PushMutacoes: 10}})
	n := m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailA)
	_, tok := m.aparelho(n.userID())
	push := func(k int) resposta {
		muts := make([]map[string]any, k)
		for i := range muts {
			muts[i] = criar(contracttest.NewID(), "texto", m.clk.Now())
		}
		return m.comToken(http.MethodPost, "/api/v1/sync/push", tok, map[string]any{"mutations": muts})
	}
	if res := push(6); res.Code != http.StatusOK {
		t.Fatalf("push de 6: %d %s", res.Code, res.Body.String())
	}
	if res := push(6); res.Code != http.StatusTooManyRequests || res.Header().Get("Retry-After") == "" {
		t.Fatalf("12 mutações com limite de 10: %d, quero 429 com Retry-After", res.Code)
	}
	if res := push(4); res.Code != http.StatusOK {
		t.Fatalf("push de 4 (cabe): %d", res.Code)
	}
	if res := push(0); res.Code != http.StatusTooManyRequests {
		t.Fatalf("4º pedido com limite de 3: %d, quero 429", res.Code)
	}
	// O teto de 500 por lote vale antes do limite.
	m.clk.andar(time.Minute)
	if res := push(501); res.Code != http.StatusBadRequest {
		t.Fatalf("lote de 501: %d, quero 400", res.Code)
	}
}

func testTirarDaLista(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailA)
	_, tok := m.aparelho(n.userID())
	if res := m.comToken(http.MethodGet, "/api/v1/me", tok, nil); res.Code != http.StatusOK {
		t.Fatalf("token antes: %d", res.Code)
	}
	m.montar(m.emailB) // reinicia sem o e-mail A
	if status, _ := n.me(); status != http.StatusUnauthorized {
		t.Errorf("sessão de e-mail fora da lista: %d, quero 401", status)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", tok, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("token de e-mail fora da lista: %d, quero 401", res.Code)
	}
	m.montar("") // volta: desfaz-se reincluindo
	n.entrarComo(sub()+"-nao-usado", m.emailB)
}

// rotaConcreta troca os {id} do padrão por um UUID (e o {provedor} das chaves
// da IA por um da lista) e separa o método. Os padrões de resto (sem método)
// devolvem o método vazio.
func rotaConcreta(padrao string) (method, path string) {
	method, path, ok := strings.Cut(padrao, " ")
	if !ok {
		return "", ""
	}
	path = strings.ReplaceAll(strings.TrimSuffix(path, "{$}"), "{id}", contracttest.NewID())
	return method, strings.ReplaceAll(path, "{provedor}", "openai")
}

func testEscopo(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailA)
	userID := n.userID()
	_, tok := m.aparelho(userID)
	revokedID, revoked := m.aparelho(userID)
	b.RevokeDevice(t, revokedID)

	seen := map[server.Acesso]int{}
	porPadrao := map[string]server.Acesso{}
	for _, rt := range m.cloud.Rotas() {
		method, path := rotaConcreta(rt.Padrao)
		if method == "" {
			continue // os padrões de resto
		}
		seen[rt.Acesso]++
		porPadrao[rt.Padrao] = rt.Acesso
		body := any(map[string]any{})
		if method == http.MethodGet {
			body = nil
		}
		switch rt.Acesso {
		case server.AcessoSessao, server.AcessoDitar:
			// A rota de ditar (ADR-021, C5b): no passo 1, o token de um PC é
			// 403, como nas rotas só da sessão.
			if res := m.comToken(method, path, tok, body); res.Code != http.StatusForbidden {
				t.Errorf("%s com o token do PC: %d, quero 403", rt.Padrao, res.Code)
			}
		case server.AcessoAparelho:
			if res := n.pedir(method, path, body); res.Code != http.StatusForbidden {
				t.Errorf("%s com a sessão: %d, quero 403", rt.Padrao, res.Code)
			}
		}
		if rt.Acesso == server.AcessoPublico {
			continue
		}
		if res := m.serve(m.req(method, path, body)); res.Code != http.StatusUnauthorized {
			t.Errorf("%s sem credencial: %d, quero 401", rt.Padrao, res.Code)
		}
		// Bearer inválido com um cookie válido: 401, sem cair no cookie.
		for _, auth := range []string{"Bearer " + cloudauth.NovoSegredo(), "Bearer lixo", "Basic dTpw", "Bearer", ""} {
			res := n.pedir(method, path, body, func(r *http.Request) { r.Header.Set("Authorization", auth) })
			if res.Code != http.StatusUnauthorized {
				t.Errorf("%s com Authorization %q e cookie válido: %d, quero 401", rt.Padrao, auth, res.Code)
			}
		}
		if res := m.comToken(method, path, revoked, body); res.Code != http.StatusUnauthorized {
			t.Errorf("%s com aparelho revogado: %d, quero 401", rt.Padrao, res.Code)
		}
	}
	if seen[server.AcessoSessao] < 15 || seen[server.AcessoAparelho] != 2 || seen[server.AcessoQualquer] != 2 {
		t.Errorf("a tabela tem %v rotas por acesso; quero o CRUD, as sessões, a lista de aparelhos, a aprovação "+
			"de um PC e o aprendido do celular só com sessão, o sync com aparelho, e o /me e o revogar com qualquer", seen)
	}
	// O aprendido do celular (F8d): só a sessão, e sem parâmetro de caminho (o
	// texto nunca vai na URL; o id do desfazer vai no corpo).
	for _, padrao := range []string{"GET /api/v1/learned", "POST /api/v1/learned/undo"} {
		if acesso, ok := porPadrao[padrao]; !ok || acesso != server.AcessoSessao {
			t.Errorf("%s na tabela: %v (montada: %v), quero só a sessão", padrao, acesso, ok)
		}
		if strings.ContainsAny(padrao, "{}?") {
			t.Errorf("%s tem parâmetro na URL", padrao)
		}
	}
	// As notas e os marcadores do celular (ADR-022, N7): só a sessão, e sem
	// parâmetro de caminho (a vista vai na query, e nenhum texto vai na URL).
	for _, padrao := range []string{"GET /api/v1/notes", "GET /api/v1/labels"} {
		if acesso, ok := porPadrao[padrao]; !ok || acesso != server.AcessoSessao {
			t.Errorf("%s na tabela: %v (montada: %v), quero só a sessão", padrao, acesso, ok)
		}
		if strings.ContainsAny(padrao, "{}?") {
			t.Errorf("%s tem parâmetro na URL", padrao)
		}
	}
	// As chaves da IA do celular (ADR-021, C3a): só a sessão, e percorridas
	// acima com o token, sem credencial e com o Bearer inválido.
	for _, padrao := range []string{"PUT /api/v1/ai-keys/{provedor}", "DELETE /api/v1/ai-keys/{provedor}", "GET /api/v1/ai-keys/{provedor}/models"} {
		if acesso, ok := porPadrao[padrao]; !ok || acesso != server.AcessoSessao {
			t.Errorf("%s na tabela: %v (montada: %v), quero só a sessão", padrao, acesso, ok)
		}
	}
	// O aceite e as preferências do ditado no celular (ADR-021, C3b): só a
	// sessão, sem parâmetro de caminho, percorridas acima do mesmo jeito.
	for _, padrao := range []string{
		"GET /api/v1/dictation", "PUT /api/v1/dictation/settings", "POST /api/v1/dictation/consent", "DELETE /api/v1/dictation/consent",
	} {
		if acesso, ok := porPadrao[padrao]; !ok || acesso != server.AcessoSessao {
			t.Errorf("%s na tabela: %v (montada: %v), quero só a sessão", padrao, acesso, ok)
		}
		if strings.ContainsAny(padrao, "{}?") {
			t.Errorf("%s tem parâmetro na URL", padrao)
		}
	}
	// A rota de ditar (ADR-021, C5b): o AcessoDitar, que nunca é o
	// AcessoQualquer (ele admitiria o token de um PC), sem parâmetro de caminho
	// (o id vai num cabeçalho), percorrida acima com o token, sem credencial e
	// com o Bearer inválido.
	if acesso, ok := porPadrao["POST /api/v1/dictations"]; !ok || acesso != server.AcessoDitar || acesso == server.AcessoQualquer {
		t.Errorf("POST /api/v1/dictations na tabela: %v (montada: %v), quero o AcessoDitar", acesso, ok)
	}
	if seen[server.AcessoDitar] != 1 {
		t.Errorf("%d rotas com o AcessoDitar, quero só a de ditar", seen[server.AcessoDitar])
	}
	for padrao := range porPadrao {
		if strings.Contains(padrao, "/learned") && padrao != "GET /api/v1/learned" && padrao != "POST /api/v1/learned/undo" {
			t.Errorf("%s: o celular não aprende nem muda o aprendido, só lista e desfaz", padrao)
		}
	}
	// As rotas do token: o sync e o /me.
	if res := m.comToken(http.MethodGet, "/api/v1/sync/pull?after_seq=0", tok, nil); res.Code != http.StatusOK {
		t.Errorf("pull com o token: %d", res.Code)
	}
	// Dois cabeçalhos Authorization: 401.
	req := m.req(http.MethodGet, "/api/v1/me", nil)
	req.Header.Add("Authorization", "Bearer "+tok)
	req.Header.Add("Authorization", "Bearer "+tok)
	if res := m.serve(req); res.Code != http.StatusUnauthorized {
		t.Errorf("dois Authorization: %d, quero 401", res.Code)
	}
	// Um cookie que não é um segredo: 401 sem ir ao banco.
	bad := m.navegador(uaAndroid)
	bad.cookies["__Host-sessao"] = "x"
	if status, _ := bad.me(); status != http.StatusUnauthorized {
		t.Errorf("cookie lixo: %d", status)
	}
}

func testMe(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailA)
	userID := n.userID()
	devID, tok := m.aparelho(userID)
	var me struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Aparelho *struct {
			ID         string `json:"id"`
			Plataforma string `json:"plataforma"`
		} `json:"aparelho"`
	}
	res := m.comToken(http.MethodGet, "/api/v1/me", tok, nil)
	if err := json.Unmarshal(res.Body.Bytes(), &me); err != nil || res.Code != http.StatusOK {
		t.Fatalf("GET /me com token: %d %s", res.Code, res.Body.String())
	}
	if me.ID != userID || me.Email != m.emailA || me.Aparelho == nil || me.Aparelho.ID != devID || me.Aparelho.Plataforma != "windows" {
		t.Fatalf("GET /me com token = %+v", me)
	}
	if strings.Contains(n.pedir(http.MethodGet, "/api/v1/me", nil).Body.String(), "aparelho") {
		t.Error("GET /me com sessão traz um aparelho")
	}
}

func testCSRF(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailA)
	antes := b.Contar(t)
	visto := map[string]bool{}
	for _, rt := range m.cloud.Rotas() {
		method, path := rotaConcreta(rt.Padrao)
		if method == "" || method == http.MethodGet {
			continue
		}
		visto[rt.Padrao] = true
		for name, mudar := range map[string]func(*http.Request){
			"Sec-Fetch-Site cross-site": func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
			"Sec-Fetch-Site same-site":  func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") },
			"Origin de outro site": func(r *http.Request) {
				r.Header.Del("Sec-Fetch-Site")
				r.Header.Set("Origin", "https://evil.example")
			},
		} {
			res := n.pedir(method, path, map[string]any{"confirmar": "apagar"}, mudar)
			if res.Code != http.StatusForbidden {
				t.Errorf("%s com %s: %d, quero 403", rt.Padrao, name, res.Code)
			}
		}
	}
	// O desfazer do aprendido (F8d) é um POST da sessão: o CSRF o cobre como cobre
	// o resto da tabela.
	if !visto["POST /api/v1/learned/undo"] {
		t.Error("o POST /learned/undo não foi conferido contra o pedido de outra origem")
	}
	// O PUT e o DELETE das chaves da IA (ADR-021, C3a) também.
	if !visto["PUT /api/v1/ai-keys/{provedor}"] || !visto["DELETE /api/v1/ai-keys/{provedor}"] {
		t.Error("as rotas das chaves da IA não foram conferidas contra o pedido de outra origem")
	}
	// O PUT, o POST e o DELETE do aceite e das preferências (C3b) também, e o
	// POST de ditar (C5b).
	for _, padrao := range []string{"PUT /api/v1/dictation/settings", "POST /api/v1/dictation/consent", "DELETE /api/v1/dictation/consent", "POST /api/v1/dictations"} {
		if !visto[padrao] {
			t.Errorf("%s não foi conferida contra o pedido de outra origem", padrao)
		}
	}
	if status, _ := n.me(); status != http.StatusOK {
		t.Fatal("um pedido de outra origem derrubou a sessão")
	}
	if depois := b.Contar(t); depois != antes {
		t.Fatalf("um pedido de outra origem mudou o banco: %+v -> %+v", antes, depois)
	}
	// Nenhum CORS: o preflight cai no 404, e nada traz Access-Control-*.
	pre := m.req(http.MethodOptions, "/api/v1/transcriptions", nil)
	pre.Header.Set("Origin", "https://evil.example")
	pre.Header.Set("Access-Control-Request-Method", "DELETE")
	res := m.serve(pre)
	if res.Code != http.StatusNotFound {
		t.Errorf("preflight: %d, quero 404", res.Code)
	}
	for _, r := range []resposta{res, n.pedir(http.MethodGet, "/api/v1/me", nil, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })} {
		for k := range r.Header() {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Errorf("resposta com %s", k)
			}
		}
	}
}

func testCabecalhos(t *testing.T, b Backend) {
	const cspWant = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'"
	bases := []string{"https://nuvem.exemplo.test", "http://localhost:8080"}
	if server.LoginDeTesteCompilado {
		bases = bases[1:] // com a tag, o servidor só sobe em localhost
	}
	for _, base := range bases {
		m := novoMundo(t, b, opcoes{base: base})
		n := m.navegador(uaAndroid)
		n.entrarComo(sub(), m.emailA)
		for _, c := range []struct {
			method, path string
			noStore      bool
		}{
			{http.MethodGet, "/", false}, {http.MethodGet, "/assets/index-teste1.js", false}, {http.MethodGet, "/favicon.svg", false},
			// F5: o app instalável, com os cabeçalhos de sempre (a CSP do sw.js
			// o prende à mesma origem).
			{http.MethodGet, "/sw.js", false}, {http.MethodGet, "/manifest.webmanifest", false}, {http.MethodGet, "/icone-192.png", false},
			{http.MethodGet, "/api/v1/me", true}, {http.MethodGet, "/api/v1/sessions", true},
			// F6: a página de aprovação é o app; a lista de aparelhos, a API.
			{http.MethodGet, "/device/authorize?pedido=" + contracttest.NewID(), false}, {http.MethodGet, "/api/v1/devices", true},
			{http.MethodGet, "/api/v1/nada", true}, {http.MethodGet, "/api/v1/health/live", true},
			{http.MethodPost, "/auth/start", true}, {http.MethodGet, "/auth/google/callback", true},
			{http.MethodGet, "/nada", false},
		} {
			res := n.pedir(c.method, c.path, nil)
			h := res.Header()
			if h.Get("Content-Security-Policy") != cspWant || h.Get("X-Content-Type-Options") != "nosniff" ||
				h.Get("Referrer-Policy") != "no-referrer" || h.Get("Cross-Origin-Opener-Policy") != "same-origin" {
				t.Errorf("%s %s (%s): cabeçalhos %v", c.method, c.path, base, h)
			}
			// ADR-021 (escolha menor 18): o app grava o ditado do celular, e o
			// microfone é só da própria origem, em toda resposta.
			if got := h.Get("Permissions-Policy"); got != "microphone=(self)" {
				t.Errorf("%s %s (%s): Permissions-Policy %q, quero microphone=(self)", c.method, c.path, base, got)
			}
			if got := h.Get("Strict-Transport-Security"); (got == "max-age=31536000") != strings.HasPrefix(base, "https") {
				t.Errorf("%s %s (%s): HSTS %q (só com https)", c.method, c.path, base, got)
			}
			if (h.Get("Cache-Control") == "no-store") != c.noStore {
				t.Errorf("%s %s: Cache-Control %q, no-store=%v", c.method, c.path, h.Get("Cache-Control"), c.noStore)
			}
		}
		// Até a resposta barrada antes da rota traz os cabeçalhos.
		req := m.req(http.MethodGet, "/api/v1/me", nil)
		req.Host = "evil.example"
		if h := m.serve(req).Header(); h.Get("Content-Security-Policy") != cspWant || h.Get("Cache-Control") != "no-store" || h.Get("Permissions-Policy") != "microphone=(self)" {
			t.Errorf("Host recusado sem os cabeçalhos: %v", h)
		}
	}
}

// hostRecusado confere que a resposta é a da guarda do Host (o 400 em JSON
// "host inválido"), e não um 400 de outra coisa que a rota faria sozinha (o
// callback sem state, o PATCH sem corpo).
func hostRecusado(res resposta) bool {
	return res.Code == http.StatusBadRequest && strings.HasPrefix(res.Header().Get("Content-Type"), "application/json") &&
		strings.Contains(res.Body.String(), `"host inválido"`)
}

func testHost(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{base: "http://localhost:8080"})
	estranhos := []string{"127.0.0.1:8080", "evil.example", "localhost:8081", "localhost", ""}

	// Toda rota da tabela, menos o health, recusa o Host estranho antes de
	// tudo: com a sessão válida, do loopback (o login de teste, com a tag, só
	// atende assim) e com o corpo que faria efeito se a rota rodasse. É a
	// defesa contra o DNS rebinding: um site que aponta o próprio nome para
	// 127.0.0.1 chega com o Host dele e lê a resposta como da origem dele.
	n := m.navegador(uaAndroid)
	n.ip = "127.0.0.1:5555"
	n.entrarComo(sub(), m.emailA)
	userID := n.userID()
	epoch, _ := m.epoch(userID)
	// Um login em curso, com o state, o code e o __Host-pre de verdade: com o
	// Host estranho, o callback não pode entrar nem consumir o login.
	login := m.navegador(uaAndroid)
	code, state := m.g.Autorizar(t, login.comecar(""), m.g.ClaimsBoas(sub(), m.emailB))
	antes := b.Contar(t)
	rotas := 0
	conferidas := map[string]bool{}
	for _, rt := range m.cloud.Rotas() {
		method, path := rotaConcreta(rt.Padrao)
		if method == "" {
			method, path = http.MethodGet, rt.Padrao+"nada" // um caminho de cada padrão de resto
		}
		if strings.HasPrefix(path, "/api/v1/health/") {
			continue
		}
		rotas++
		conferidas[rt.Padrao] = true
		var body any
		switch {
		case method == http.MethodGet:
		case path == "/auth/teste":
			body = map[string]any{"email": m.emailA}
		default:
			body = map[string]any{"confirmar": "apagar", "final_text": "trocado", "base_version": 1}
		}
		quem := n
		if path == "/auth/google/callback" {
			quem, path = login, path+"?"+url.Values{"code": {code}, "state": {state}}.Encode()
		}
		for _, host := range estranhos {
			res := quem.pedir(method, path, body, func(r *http.Request) { r.Host = host })
			if !hostRecusado(res) {
				t.Errorf("%s com Host %q: %d %s; quero o 400 \"host inválido\"", rt.Padrao, host, res.Code, res.Body.String())
			}
		}
	}
	if rotas < 20 {
		t.Errorf("só %d rotas conferidas", rotas)
	}
	for _, padrao := range []string{
		"GET /api/v1/learned", "POST /api/v1/learned/undo",
		"GET /api/v1/dictation", "PUT /api/v1/dictation/settings", "POST /api/v1/dictation/consent", "DELETE /api/v1/dictation/consent",
		"POST /api/v1/dictations",
		"GET /api/v1/notes", "GET /api/v1/labels",
	} {
		if !conferidas[padrao] {
			t.Errorf("%s não foi conferida com o Host estranho", padrao)
		}
	}
	semMudanca(t, b, antes)
	if ep, _ := m.epoch(userID); ep != epoch {
		t.Error("um pedido com Host estranho trocou o epoch (o apagar meus dados rodou)")
	}
	if status, _ := n.me(); status != http.StatusOK {
		t.Errorf("um pedido com Host estranho derrubou a sessão: %d", status)
	}
	if trocas, _ := m.g.Contagens(); trocas != 1 {
		t.Errorf("%d trocas no Google, quero 1 (a do login do começo)", trocas)
	}
	// O login em curso não foi consumido: com o Host certo, entra.
	if res := login.voltar(url.Values{"code": {code}, "state": {state}}); res.Code != http.StatusSeeOther {
		t.Errorf("o callback com o Host certo, depois dos estranhos: %d %s", res.Code, res.Body.String())
	}

	for _, host := range estranhos {
		req := m.req(http.MethodGet, "/", nil)
		req.Host = host
		if res := m.serve(req); res.Code != http.StatusBadRequest {
			t.Errorf("Host %q: %d, quero 400", host, res.Code)
		}
		req = m.req(http.MethodGet, "/api/v1/health/live", nil)
		req.Host = host
		if res := m.serve(req); res.Code != http.StatusOK {
			t.Errorf("health com Host %q: %d, quero 200", host, res.Code)
		}
	}
	for _, host := range []string{"localhost:8080", "LOCALHOST:8080"} {
		req := m.req(http.MethodGet, "/", nil)
		req.Host = host
		if res := m.serve(req); res.Code != http.StatusOK {
			t.Errorf("Host %q: %d, quero 200", host, res.Code)
		}
	}
	if server.LoginDeTesteCompilado {
		return // com a tag, o servidor só sobe em localhost
	}
	// A porta padrão não importa.
	h := novoMundo(t, b, opcoes{base: "https://nuvem.exemplo.test"})
	for _, host := range []string{"nuvem.exemplo.test", "nuvem.exemplo.test:443"} {
		req := h.req(http.MethodGet, "/", nil)
		req.Host = host
		if res := h.serve(req); res.Code != http.StatusOK {
			t.Errorf("Host %q: %d, quero 200", host, res.Code)
		}
	}
}

func testNaoMontadas(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	n.entrarComo(sub(), m.emailA)
	_, tok := m.aparelho(n.userID())
	routes := []string{
		"POST /api/v1/transcriptions", "POST /api/v1/asr/transcribe", "POST /api/v1/resolve",
		"GET /api/v1/updates/windows/latest", "GET /api/v1/settings", "PUT /api/v1/settings",
		"GET /api/v1/dictionary", "POST /api/v1/dictionary", "PATCH /api/v1/dictionary/" + contracttest.NewID(),
		"POST /api/v1/corrections", "PATCH /api/v1/devices/" + contracttest.NewID(),
		// F6: o renomear (PATCH) fica para depois; o pareamento só tem os
		// métodos da tabela, e a aprovação é pela API, não por formulário.
		"POST /api/v1/devices", "PUT /api/v1/devices/" + contracttest.NewID(), "GET /api/v1/device-auth/start",
		"GET /api/v1/device-auth/exchange", "POST /api/v1/device-auth/requests/" + contracttest.NewID(),
		"GET /api/v1/device-auth/requests/" + contracttest.NewID() + "/approve", "DELETE /api/v1/device-auth/requests/" + contracttest.NewID(),
		"POST /device/authorize", "GET /device/authorize/x", "GET /device",
		"GET /auth/start", "GET /auth/logout",
		"PUT /api/v1/transcriptions/" + contracttest.NewID(), "DELETE /api/v1/me", "GET /api/v1/me/data",
		"POST /api/v1/sync/pull", "GET /api/v1/sync/push", "GET /qualquer", "GET /assets/x.js",
		// ADR-021, C5b: a rota de ditar só tem o POST, e sem nada no caminho.
		"GET /api/v1/dictations", "PUT /api/v1/dictations", "DELETE /api/v1/dictations",
		"POST /api/v1/dictations/" + contracttest.NewID(), "GET /api/v1/dictations/" + contracttest.NewID(),
		// Um método que a página não tem: sem o padrão "/" de resto, o mux
		// responderia 405 com Allow (e o 404 dele em texto puro no resto).
		"POST /", "DELETE /", "POST /pagina.js", "PUT /pagina.css", "GET /qualquer-coisa", "GET /favicon.ico",
		// F4: a página provisória saiu, e o app só tem o que está no build.
		"GET /pagina.js", "GET /index.html", "POST /assets/index-teste1.js", "PUT /favicon.svg", "GET /assets/nao-existe.js",
		"OPTIONS /", "OPTIONS /api/v1/me", "OPTIONS /auth/start",
	}
	if !server.LoginDeTesteCompilado {
		// Sem a build tag, o login de teste não existe: nem o POST que
		// entra, nem o GET que a tela Entrar pergunta.
		routes = append(routes, "POST /auth/teste", "GET /auth/teste")
	}
	for _, r := range routes {
		method, path, _ := strings.Cut(r, " ")
		var body any
		if method != http.MethodGet {
			body = map[string]any{}
		}
		for name, res := range map[string]resposta{
			"sessão":         n.pedir(method, path, body),
			"token":          m.comToken(method, path, tok, body),
			"sem credencial": m.serve(m.req(method, path, body)),
		} {
			if res.Code != http.StatusNotFound {
				t.Errorf("%s com %s: %d, quero 404", r, name, res.Code)
			}
			// O 404 é o da API, em JSON, e nada anuncia métodos nem CORS.
			if !strings.HasPrefix(res.Header().Get("Content-Type"), "application/json") || !strings.Contains(res.Body.String(), `"rota não encontrada"`) {
				t.Errorf("%s com %s: o 404 não é o JSON da API: %q %s", r, name, res.Header().Get("Content-Type"), res.Body.String())
			}
			for k := range res.Header() {
				if k == "Allow" || strings.HasPrefix(strings.ToLower(k), "access-control-") {
					t.Errorf("%s com %s: resposta com %s", r, name, k)
				}
			}
		}
	}
}

func testApagarDados(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	a1, a2, bb := m.navegador(uaAndroid), m.navegador(uaWindows), m.navegador(uaAndroid)
	subA := sub()
	a1.entrarComo(subA, m.emailA)
	a2.entrarComo(subA, m.emailA)
	bb.entrarComo(sub(), m.emailB)
	userA, userB := a1.userID(), bb.userID()
	_, tokA := m.aparelho(userA)
	_, tokB := m.aparelho(userB)
	for _, x := range []struct {
		tok string
		n   int
	}{{tokA, 3}, {tokB, 2}} {
		muts := make([]map[string]any, x.n)
		for i := range muts {
			muts[i] = criar(contracttest.NewID(), "ditado", m.clk.Now())
		}
		if res := m.comToken(http.MethodPost, "/api/v1/sync/push", x.tok, map[string]any{"mutations": muts}); res.Code != http.StatusOK {
			t.Fatalf("push: %d", res.Code)
		}
	}
	epochA, lastA := m.epoch(userA)
	epochB, _ := m.epoch(userB)

	// Sem a confirmação: 400 e nada muda. Com o token do PC: 403.
	for _, body := range []any{nil, map[string]any{}, map[string]any{"confirmar": "sim"}, `{"confirmar":`} {
		if res := a1.pedir(http.MethodDelete, "/api/v1/me/data", body); res.Code != http.StatusBadRequest {
			t.Errorf("sem confirmação (%v): %d, quero 400", body, res.Code)
		}
	}
	if res := m.comToken(http.MethodDelete, "/api/v1/me/data", tokA, map[string]any{"confirmar": "apagar"}); res.Code != http.StatusForbidden {
		t.Errorf("com o token do PC: %d, quero 403", res.Code)
	}
	if ep, _ := m.epoch(userA); ep != epochA {
		t.Fatal("o epoch mudou sem a confirmação")
	}

	if res := a1.pedir(http.MethodDelete, "/api/v1/me/data", map[string]any{"confirmar": "apagar"}); res.Code != http.StatusNoContent {
		t.Fatalf("apagar: %d %s", res.Code, res.Body.String())
	}
	// A sessão atual continua; a outra sessão e o PC de A caem.
	if status, _ := a1.me(); status != http.StatusOK {
		t.Fatalf("a sessão atual caiu: %d", status)
	}
	if status, _ := a2.me(); status != http.StatusUnauthorized {
		t.Errorf("a outra sessão de A: %d, quero 401", status)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/sync/pull?after_seq=0", tokA, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("o PC de A: %d, quero 401", res.Code)
	}
	if list := a1.pedir(http.MethodGet, "/api/v1/transcriptions", nil).Body.String(); !strings.Contains(list, `"items":[]`) {
		t.Errorf("o histórico de A depois de apagar: %s", list)
	}
	ep, _ := m.epoch(userA)
	if ep == epochA || len(ep) != 36 || ep[14] != '7' {
		t.Errorf("epoch depois de apagar = %q (antes %q), quero outro UUID v7", ep, epochA)
	}
	// B intacto.
	if ep, _ := m.epoch(userB); ep != epochB {
		t.Error("o epoch de B mudou")
	}
	if status, _ := bb.me(); status != http.StatusOK {
		t.Error("a sessão de B caiu")
	}
	var listB struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(bb.pedir(http.MethodGet, "/api/v1/transcriptions", nil).Body.Bytes(), &listB)
	if len(listB.Items) != 2 {
		t.Errorf("B tem %d ditados, quero 2", len(listB.Items))
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", tokB, nil); res.Code != http.StatusOK {
		t.Error("o PC de B caiu")
	}
	// O seq continua: um PC novo de A grava acima do último seq de antes.
	_, tokA2 := m.aparelho(userA)
	if res := m.comToken(http.MethodPost, "/api/v1/sync/push", tokA2, map[string]any{"mutations": []any{criar(contracttest.NewID(), "depois", m.clk.Now())}}); res.Code != http.StatusOK {
		t.Fatalf("push depois de apagar: %d", res.Code)
	}
	if _, last := m.epoch(userA); last <= lastA {
		t.Errorf("seq depois de apagar = %d, quero acima de %d (o seq não volta a 0)", last, lastA)
	}
}

func testLog(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	authURL := n.comecar("/volta-secreta")
	pre := n.cookies["__Host-pre"]
	code, state := m.g.Autorizar(t, authURL, m.g.ClaimsBoas(sub(), m.emailA))
	n.voltar(url.Values{"code": {code}, "state": {state}})
	sess := n.cookies["__Host-sessao"]
	n.pedir(http.MethodGet, "/api/v1/sessions", nil)
	_, tok := m.aparelho(n.userID())
	m.comToken(http.MethodGet, "/api/v1/me", tok, nil)
	m.comToken(http.MethodGet, "/api/v1/sync/pull?after_seq=0&limit=7", tok, nil)
	// Recusas.
	fora := m.navegador(uaAndroid)
	fora.entrar(m.g.ClaimsBoas(sub(), m.emailFora))
	m.navegador(uaAndroid).voltar(url.Values{"code": {"code-secreto-123"}, "state": {"state-secreto-456"}})
	// Um método qualquer: o log não o escreve.
	n.pedir("SEGREDO-NO-METODO", "/api/v1/me", nil)

	logs := m.logs.String()
	for name, v := range map[string]string{
		"o e-mail": m.emailA, "o e-mail recusado": m.emailFora, "o cookie": sess, "o pre": pre, "o token": tok,
		"o code": code, "o state": state, "o code inventado": "code-secreto-123", "o state inventado": "state-secreto-456",
		"a volta": "volta-secreta", "o IP": "192.0.2.10", "o método inventado": "SEGREDO-NO-METODO", "a query": "after_seq",
	} {
		if strings.Contains(logs, v) {
			t.Errorf("%s está no log:\n%s", name, logs)
		}
	}
	for _, want := range []string{`rota="POST /auth/start"`, `rota="GET /auth/google/callback"`, "status=303",
		"login_recusado motivo=fora_da_lista", "login_recusado motivo=state_invalido", "metodo=outro", "ms="} {
		if !strings.Contains(logs, want) {
			t.Errorf("o log não tem %s:\n%s", want, logs)
		}
	}
}

// manipulador acha um atributo on...= (onclick, onload), que a CSP bloquearia.
var manipulador = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)

// testPagina: o app (F4: o build da nuvem do painel, no lugar da página
// provisória da F3) e as duas páginas fixas do login que não deu.
func testPagina(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaAndroid)
	for path, ctype := range map[string]string{
		"/": "text/html", "/assets/index-teste1.js": "text/javascript", "/pagina.css": "text/css",
	} {
		res := n.pedir(http.MethodGet, path, nil)
		if res.Code != http.StatusOK || !strings.HasPrefix(res.Header().Get("Content-Type"), ctype) {
			t.Errorf("%s: %d %q", path, res.Code, res.Header().Get("Content-Type"))
		}
		body := res.Body.String()
		// A CSP é default-src 'self': nada embutido.
		for _, banned := range []string{"<script>", " style=", " onclick=", "javascript:", "http://", "https://"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s tem %q (a CSP o bloquearia, ou é endereço de fora)", path, banned)
			}
		}
	}
	// As páginas de recusa não ecoam o pedido.
	res := m.navegador(uaAndroid).voltar(url.Values{"state": {"<script>alert(1)</script>"}, "code": {"<b>x</b>"}})
	if strings.Contains(res.Body.String(), "alert(1)") || strings.Contains(res.Body.String(), "<b>x</b>") {
		t.Error("a página de falha ecoa o pedido")
	}
	if res.Code != http.StatusBadRequest {
		t.Errorf("callback com state lixo: %d", res.Code)
	}
	// As duas páginas fixas, no visual da tela Entrar do app (F4): o título,
	// a linha e o link de volta ao app; sem script nem estilo embutido, e o
	// estilo delas é o /pagina.css.
	recusa := m.navegador(uaAndroid).entrar(m.g.ClaimsBoas(sub(), m.emailFora))
	for name, c := range map[string]struct {
		res    resposta
		textos []string
	}{
		"recusado": {recusa, []string{"Esta conta não tem acesso a esta nuvem.", "Nada foi criado com ela.", `<a class="botao" href="/">Entrar com outra conta</a>`}},
		"falhou":   {res, []string{"Não deu para entrar.", `<a class="botao" href="/">Tentar de novo</a>`}},
	} {
		body := c.res.Body.String()
		for _, want := range append(c.textos, `href="/pagina.css"`, `<html lang="pt-BR">`) {
			if !strings.Contains(body, want) {
				t.Errorf("a página %s não tem %q", name, want)
			}
		}
		for _, banned := range []string{"<script", "<style", " style=", "javascript:", "http://", "https://"} {
			if strings.Contains(body, banned) {
				t.Errorf("a página %s tem %q", name, banned)
			}
		}
		if manipulador.MatchString(body) {
			t.Errorf("a página %s tem um manipulador embutido (on...=)", name)
		}
	}
}
