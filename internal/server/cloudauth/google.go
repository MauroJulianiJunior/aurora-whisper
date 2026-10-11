package cloudauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Os endereços do Google (OpenID Connect; accounts.google.com/.well-known/
// openid-configuration). Ficam no código, e NUNCA vêm do ambiente nem do JSON
// do client: é por o ID token chegar direto deste endpoint de token, por TLS,
// numa chamada autenticada com o client secret, que a assinatura dele não
// precisa ser conferida (idtoken.go). Um endpoint de token configurável
// permitiria a quem muda a configuração forjar qualquer identidade.
const (
	GoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
)

// EscoposGoogle são os escopos do login (ADR-019, decisão 6): o básico, que
// não exige verificação do app.
const EscoposGoogle = "openid email profile"

// PrazoDaTroca é o teto da chamada ao endpoint de token.
const PrazoDaTroca = 10 * time.Second

// maxRespostaToken limita o corpo da resposta do endpoint de token.
const maxRespostaToken = 64 << 10

// ErrGoogle: a troca do código falhou (rede, prazo, resposta que não é 200
// ou sem id_token). O texto é fixo: nunca o corpo da resposta nem o código.
var ErrGoogle = errors.New("a troca do código com o Google falhou")

// Google é o client OAuth do Google (um client Web, confidencial). AuthURL,
// TokenURL e HTTPClient já vêm com os valores do Google em NovoGoogle; só os
// testes os trocam, por um dublê (internal/server/authtest).
type Google struct {
	ClientID     string
	ClientSecret string
	// RedirectURL é PUBLIC_BASE_URL + /auth/google/callback. Tem de estar,
	// igual, nos redirect_uris do client no Google Cloud.
	RedirectURL string

	AuthURL    string
	TokenURL   string
	HTTPClient *http.Client
}

// NovoGoogle monta o client com os endereços do Google.
func NovoGoogle(clientID, clientSecret, redirectURL string) *Google {
	return &Google{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      GoogleAuthURL,
		TokenURL:     GoogleTokenURL,
		HTTPClient:   novoHTTPClient(),
	}
}

func novoHTTPClient() *http.Client {
	return &http.Client{
		Timeout: PrazoDaTroca,
		// O endpoint de token não redireciona. Seguir um redirecionamento
		// levaria o client secret (no corpo) para outro lugar.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// URLDeAutorizacao é para onde o navegador vai no início do login: o fluxo
// de código, com state, nonce e PKCE (S256). prompt=select_account deixa a
// pessoa escolher a conta, em vez de entrar calada com a que o navegador já
// usa.
func (g *Google) URLDeAutorizacao(state, nonce, desafio string) string {
	q := url.Values{}
	q.Set("client_id", g.ClientID)
	q.Set("redirect_uri", g.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", EscoposGoogle)
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", desafio)
	q.Set("code_challenge_method", "S256")
	q.Set("prompt", "select_account")
	return g.AuthURL + "?" + q.Encode()
}

// TrocarCodigo troca o código do callback pelo ID token, no endpoint de
// token, com o client secret e o verificador do PKCE. Devolve o ID token
// cru; quem o valida é ValidarIDToken. Qualquer falha é ErrGoogle.
func (g *Google) TrocarCodigo(ctx context.Context, code, verificador string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, PrazoDaTroca)
	defer cancel()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", g.RedirectURL)
	form.Set("client_id", g.ClientID)
	form.Set("client_secret", g.ClientSecret)
	form.Set("code_verifier", verificador)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", ErrGoogle
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := g.HTTPClient
	if client == nil {
		client = novoHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ErrGoogle
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespostaToken))
	if err != nil {
		return "", ErrGoogle
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Join(ErrGoogle, errors.New("status "+strconv.Itoa(resp.StatusCode)))
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.IDToken == "" {
		return "", ErrGoogle
	}
	return tok.IDToken, nil
}
