// Package authtest é a suíte do login, da sessão e do escopo das credenciais
// da nuvem (ADR-019, F3), pela API HTTP, como a contracttest: os mesmos
// testes rodam contra o MemoryStore e contra o Postgres (pgstore). O Google é
// um dublê (GoogleFalso, sobre httptest.NewTLSServer): nenhum teste chama o
// Google de verdade.
//
// Este pacote é só para testes: nenhum binário o importa.
package authtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Backend é o que a suíte precisa do armazenamento sob teste.
type Backend struct {
	Store    cloudstore.Store
	Accounts cloudstore.Accounts
	// NewDevice cria um aparelho do usuário com o HMAC do token dele (o
	// pareamento é da F6).
	NewDevice func(t *testing.T, userID string, tokenHMAC []byte) string
	// RevokeDevice revoga um aparelho (devices.revoked_at).
	RevokeDevice func(t *testing.T, deviceID string)
	// DisableUser desativa uma conta (users.disabled_at).
	DisableUser func(t *testing.T, userID string)
	// Contar devolve quantas linhas há de usuários, contadores e sessões.
	Contar func(t *testing.T) Contagem
	// Despejo devolve, como texto, tudo o que está guardado (os bytes em
	// hex): o teste confere que um segredo em claro não está em lugar nenhum.
	Despejo func(t *testing.T) string
}

// Contagem são as linhas que um login recusado (ou um pareamento recusado)
// não pode criar.
type Contagem struct {
	Usuarios, Contadores, Sessoes int
	// Pedidos, Aparelhos e Tokens: pairing_requests, devices e device_tokens
	// (F6).
	Pedidos, Aparelhos, Tokens int
}

// A origem e as chaves do servidor da suíte (de teste).
var (
	segredoDeSessao = []byte("segredo-de-sessao-da-suite-de-login-0123456789ab")
	pepperDeToken   = []byte("pepper-de-token-da-suite-de-login-0123456789abcd")
)

var agora0 = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// relogio é um relógio de teste seguro para uso concorrente.
type relogio struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relogio) Now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogio) andar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

// opcoes mudam o servidor de um caso.
type opcoes struct {
	base    string
	limites server.Limites
	proxy   string
	// contas é CONTAS_PERMITIDAS; vazio usa os e-mails do mundo.
	contas string
}

// mundo é um servidor da nuvem montado sobre o Backend, com o dublê do
// Google, um relógio e o log capturado. Cada caso cria o seu, com e-mails
// únicos (o Backend é o mesmo em todos os casos de uma rodada).
type mundo struct {
	t      *testing.T
	b      Backend
	clk    *relogio
	g      *GoogleFalso
	h      http.Handler
	cloud  *server.Cloud
	logs   *contracttest.LockedBuffer
	base   *url.URL
	opcoes opcoes

	// emailA e emailB estão na lista; emailFora, não.
	emailA, emailB, emailFora string
}

func novoMundo(t *testing.T, b Backend, o opcoes) *mundo {
	t.Helper()
	if o.base == "" {
		o.base = contracttest.BaseURL
	}
	if o.limites == (server.Limites{}) {
		o.limites = server.Limites{LoginPorIP: 1000, InicioTotal: 1000, PushPedidos: 1000, PushMutacoes: 100000,
			PareamentoPorIP: 1000, PareamentoTotal: 1000}
	}
	tag := contracttest.NewID()[:8]
	m := &mundo{
		t: t, b: b, clk: &relogio{t: agora0}, logs: &contracttest.LockedBuffer{}, opcoes: o,
		emailA: "pessoa-a-" + tag + "@exemplo.test", emailB: "pessoa-b-" + tag + "@exemplo.test",
		emailFora: "intrusa-" + tag + "@exemplo.test",
	}
	base, err := server.ParseBaseURL(o.base)
	if err != nil {
		t.Fatal(err)
	}
	m.base = base
	m.g = NovoGoogleFalso(t, server.RedirectURL(base), m.clk.Now)
	m.montar(o.contas)
	return m
}

// montar (re)monta o servidor sobre o mesmo Backend, com a lista dada (vazia:
// os e-mails A e B). Reiniciar com outra lista é o caso "tirar o e-mail da
// lista".
func (m *mundo) montar(contas string) {
	m.t.Helper()
	if contas == "" {
		contas = m.emailA + "," + strings.ToUpper(m.emailB)
	}
	lista, err := cloudauth.ParseContas(contas)
	if err != nil {
		m.t.Fatal(err)
	}
	proxy, err := cloudauth.ParseProxyConfiavel(m.opcoes.proxy)
	if err != nil {
		m.t.Fatal(err)
	}
	// As rotas das chaves da IA (ADR-021, C3a) montadas, para o escopo, o
	// CSRF e o Host as percorrerem com as outras. Sem o aceite (nil), nenhum
	// pedido chega ao provedor.
	chaves, _ := m.b.Store.(cloudstore.ChavesDeIA)
	cofreDaSuite, err := cofre.Novo(strings.Repeat("5a", 32))
	if err != nil || chaves == nil {
		m.t.Fatalf("o cofre ou as chaves da suíte: %v", err)
	}
	m.cloud, err = server.NewCloud(server.CloudConfig{
		Store: m.b.Store, Accounts: m.b.Accounts, BaseURL: m.opcoes.base,
		SessionSecret: segredoDeSessao, DeviceTokenPepper: pepperDeToken, Contas: lista,
		Google: m.g.Client(), Proxy: proxy, Now: m.clk.Now, Painel: contracttest.PainelDeTeste(),
		Logger:  slog.New(slog.NewTextHandler(m.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Limites: m.opcoes.limites,
		Cofre:   cofreDaSuite, ChavesDeIA: chaves, Modelos: provedorQueNaoPodeSerChamado{m.t},
	})
	if err != nil {
		m.t.Fatalf("NewCloud: %v", err)
	}
	m.h = m.cloud.Handler()
}

// provedorQueNaoPodeSerChamado é o provedor da IA da suíte: nenhum caso
// daqui chega a ele (sem o aceite, as rotas das chaves recusam antes).
type provedorQueNaoPodeSerChamado struct{ t *testing.T }

func (p provedorQueNaoPodeSerChamado) ListarModelos(context.Context, string, string) ([]string, error) {
	p.t.Error("um pedido da suíte de login chegou ao provedor da IA")
	return nil, context.Canceled
}

// navegador guarda os cookies de uma origem, como um navegador, e manda os
// pedidos do app (fetch, Sec-Fetch-Site: same-origin).
type navegador struct {
	m       *mundo
	cookies map[string]string
	ua      string
	ip      string
}

func (m *mundo) navegador(ua string) *navegador {
	return &navegador{m: m, cookies: map[string]string{}, ua: ua, ip: "192.0.2.10:5555"}
}

type resposta struct {
	*httptest.ResponseRecorder
	cookies map[string]*http.Cookie
}

// req monta um pedido para o servidor do mundo, com o Host da base.
func (m *mundo) req(method, path string, body any) *http.Request {
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			m.t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = m.base.Host
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func (m *mundo) serve(req *http.Request) resposta {
	rec := httptest.NewRecorder()
	m.h.ServeHTTP(rec, req)
	out := resposta{ResponseRecorder: rec, cookies: map[string]*http.Cookie{}}
	for _, c := range rec.Result().Cookies() {
		out.cookies[c.Name] = c
	}
	return out
}

// pedir manda um pedido do app, com os cookies guardados, e guarda os que a
// resposta mandar.
func (n *navegador) pedir(method, path string, body any, mudar ...func(*http.Request)) resposta {
	n.m.t.Helper()
	req := n.m.req(method, path, body)
	req.RemoteAddr = n.ip
	req.Header.Set("User-Agent", n.ua)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for name, v := range n.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: v})
	}
	for _, f := range mudar {
		f(req)
	}
	res := n.m.serve(req)
	for name, c := range res.cookies {
		if c.MaxAge < 0 || c.Value == "" {
			delete(n.cookies, name)
		} else {
			n.cookies[name] = c.Value
		}
	}
	return res
}

// comecar é o POST /auth/start; devolve a URL do Google.
func (n *navegador) comecar(volta string) string {
	n.m.t.Helper()
	var body any = map[string]any{}
	if volta != "" {
		body = map[string]any{"volta": volta}
	}
	res := n.pedir(http.MethodPost, "/auth/start", body)
	if res.Code != http.StatusOK {
		n.m.t.Fatalf("POST /auth/start: %d %s", res.Code, res.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil || out.URL == "" {
		n.m.t.Fatalf("POST /auth/start devolveu %s", res.Body.String())
	}
	return out.URL
}

// voltar é o navegador chegando do Google ao callback (navegação de outro
// site: Sec-Fetch-Site cross-site).
func (n *navegador) voltar(query url.Values) resposta {
	n.m.t.Helper()
	return n.pedir(http.MethodGet, "/auth/google/callback?"+query.Encode(), nil, func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	})
}

// entrar faz o login inteiro com as claims dadas (nulas: o sub e o e-mail
// padrão do Google falso).
func (n *navegador) entrar(claims map[string]any) resposta {
	n.m.t.Helper()
	code, state := n.m.g.Autorizar(n.m.t, n.comecar(""), claims)
	return n.voltar(url.Values{"code": {code}, "state": {state}})
}

// entrarComo faz o login que dá certo com o sub e o e-mail.
func (n *navegador) entrarComo(sub, email string) {
	n.m.t.Helper()
	res := n.entrar(n.m.g.ClaimsBoas(sub, email))
	if res.Code != http.StatusSeeOther || n.cookies["__Host-sessao"] == "" {
		n.m.t.Fatalf("login de %s: %d %s", sub, res.Code, res.Body.String())
	}
}

// me é o GET /me; devolve o status e o e-mail.
func (n *navegador) me() (int, string) {
	res := n.pedir(http.MethodGet, "/api/v1/me", nil)
	var out struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out.Email
}

// userID é o id do usuário do navegador (pelo GET /me).
func (n *navegador) userID() string {
	n.m.t.Helper()
	res := n.pedir(http.MethodGet, "/api/v1/me", nil)
	var out struct {
		ID string `json:"id"`
	}
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &out) != nil {
		n.m.t.Fatalf("GET /me: %d %s", res.Code, res.Body.String())
	}
	return out.ID
}

// aparelho cria um aparelho do usuário e devolve o token dele.
func (m *mundo) aparelho(userID string) (deviceID, token string) {
	m.t.Helper()
	token = cloudauth.NovoSegredo()
	return m.b.NewDevice(m.t, userID, cloudauth.HMAC(pepperDeToken, token)), token
}

// comToken manda um pedido com o Bearer de um aparelho (o PC: sem cookie, sem
// Sec-Fetch-Site).
func (m *mundo) comToken(method, path, token string, body any) resposta {
	m.t.Helper()
	req := m.req(method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return m.serve(req)
}

// epoch lê o sync_epoch do usuário pelo Store (o pull com cursor 0) e o seq
// da última mudança.
func (m *mundo) epoch(userID string) (string, int64) {
	m.t.Helper()
	p, err := m.b.Store.Pull(context.Background(), userID, 0, cloudstore.MaxPullLimit)
	if err != nil {
		m.t.Fatalf("Pull: %v", err)
	}
	return p.Epoch, p.LastSeq
}

func ditado(texto string, criado time.Time) map[string]any {
	return map[string]any{
		"source_platform": "windows", "raw_text": "bruto: " + texto, "final_text": texto,
		"mode": "smart", "asr_provider": "whisper-local", "status": "done",
		"created_at": criado.Add(-time.Hour).Format(time.RFC3339),
	}
}

func criar(id, texto string, criado time.Time) map[string]any {
	return map[string]any{"mutation_id": contracttest.NewID(), "entity_type": "transcription", "entity_id": id,
		"operation": "upsert", "base_version": 0, "payload": ditado(texto, criado)}
}
