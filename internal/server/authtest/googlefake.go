package authtest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
)

// GoogleFalso é o dublê do Google: um servidor TLS local (httptest) com o
// endpoint de token. Nenhum teste chama o Google de verdade. O fluxo é o do
// navegador: o teste lê a URL de autorização que o servidor devolveu,
// "escolhe a conta" (Autorizar, que guarda o desafio do PKCE e o nonce com o
// code) e manda o code ao callback; o servidor troca o code aqui, e o dublê
// confere o que o Google confere (o grant, o code de uso único, o
// redirect_uri, o client_id, o client_secret e o code_verifier contra o
// desafio) antes de devolver o ID token com as claims que o teste escolheu.
type GoogleFalso struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string

	srv   *httptest.Server
	agora func() time.Time

	mu    sync.Mutex
	codes map[string]*pedidoGoogle
	// Falhar faz o endpoint de token responder 500.
	falhar bool
	// recusas conta as trocas que o dublê recusou (verificador errado,
	// client errado...): o caminho feliz exige zero.
	recusas int
	trocas  int
}

type pedidoGoogle struct {
	desafio string
	claims  map[string]any
	usado   bool
}

// NovoGoogleFalso sobe o dublê. agora é o relógio do servidor sob teste (as
// claims exp e iat saem dele).
func NovoGoogleFalso(t *testing.T, redirectURL string, agora func() time.Time) *GoogleFalso {
	t.Helper()
	g := &GoogleFalso{
		ClientID: "cliente-de-teste.apps.googleusercontent.com", ClientSecret: "segredo-do-client-de-teste",
		RedirectURL: redirectURL, agora: agora, codes: map[string]*pedidoGoogle{},
	}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(g.token))
	t.Cleanup(g.srv.Close)
	return g
}

// Client é o client do Google que o servidor sob teste usa, apontado para o
// dublê.
func (g *GoogleFalso) Client() *cloudauth.Google {
	c := cloudauth.NovoGoogle(g.ClientID, g.ClientSecret, g.RedirectURL)
	c.AuthURL = g.srv.URL + "/o/oauth2/v2/auth"
	c.TokenURL = g.srv.URL + "/token"
	c.HTTPClient = g.srv.Client()
	return c
}

// Falhar liga ou desliga o 500 no endpoint de token.
func (g *GoogleFalso) Falhar(v bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.falhar = v
}

// Contagens devolve quantas trocas houve e quantas foram recusadas.
func (g *GoogleFalso) Contagens() (trocas, recusas int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.trocas, g.recusas
}

// ClaimsBoas são as claims de um login que dá certo, para o sub e o e-mail
// dados (o nonce vem do pedido, em Autorizar).
func (g *GoogleFalso) ClaimsBoas(sub, email string) map[string]any {
	now := g.agora()
	return map[string]any{
		"iss": "https://accounts.google.com", "aud": g.ClientID, "azp": g.ClientID,
		"sub": sub, "email": email, "email_verified": true, "name": "Pessoa de Teste",
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

// Autorizar faz o papel da tela do Google: confere a URL de autorização que o
// servidor montou e devolve o code e o state do redirecionamento de volta.
// claims nulas vira ClaimsBoas; o nonce do pedido entra, a não ser que as
// claims já tragam um (para o teste do nonce errado).
func (g *GoogleFalso) Autorizar(t *testing.T, authURL string, claims map[string]any) (code, state string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("URL de autorização inválida: %v", err)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": g.ClientID, "redirect_uri": g.RedirectURL, "response_type": "code",
		"scope": "openid email profile", "code_challenge_method": "S256", "prompt": "select_account",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Fatalf("a URL de autorização tem %s=%q, quero %q", k, q.Get(k), v)
		}
	}
	for _, k := range []string{"state", "nonce", "code_challenge"} {
		if len(q.Get(k)) < 43 {
			t.Fatalf("a URL de autorização tem %s curto demais: %d caracteres", k, len(q.Get(k)))
		}
	}
	if claims == nil {
		claims = g.ClaimsBoas("sub-padrao", "padrao@exemplo.test")
	}
	if _, ok := claims["nonce"]; !ok {
		claims["nonce"] = q.Get("nonce")
	}
	code = cloudauth.NovoSegredo()
	g.mu.Lock()
	g.codes[code] = &pedidoGoogle{desafio: q.Get("code_challenge"), claims: claims}
	g.mu.Unlock()
	return code, q.Get("state")
}

func (g *GoogleFalso) token(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.trocas++
	recusar := func() {
		g.recusas++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}
	if r.URL.Path != "/token" || r.Method != http.MethodPost || r.ParseForm() != nil {
		recusar()
		return
	}
	f := r.PostForm
	p, ok := g.codes[f.Get("code")]
	if !ok || p.usado || f.Get("grant_type") != "authorization_code" || f.Get("redirect_uri") != g.RedirectURL ||
		f.Get("client_id") != g.ClientID || f.Get("client_secret") != g.ClientSecret ||
		cloudauth.DesafioS256(f.Get("code_verifier")) != p.desafio {
		recusar()
		return
	}
	p.usado = true
	if g.falhar {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id_token": jwt(p.claims), "access_token": "token-de-acesso-falso", "token_type": "Bearer", "expires_in": 3599,
	})
}

// jwt monta um ID token com uma assinatura qualquer: o servidor não a confere
// (o token vem do endpoint de token, por TLS; ver cloudauth.ValidarIDToken).
func jwt(claims map[string]any) string {
	enc := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return enc(map[string]string{"alg": "RS256", "kid": "falso", "typ": "JWT"}) + "." + enc(claims) + ".YXNzaW5hdHVyYS1mYWxzYQ"
}
