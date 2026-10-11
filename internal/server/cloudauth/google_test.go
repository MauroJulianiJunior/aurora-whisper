package cloudauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGoogle_URLDeAutorizacao(t *testing.T) {
	g := NovoGoogle("cliente", "segredo-que-nao-vai-na-url", "http://localhost:8080/auth/google/callback")
	if g.AuthURL != GoogleAuthURL || g.TokenURL != GoogleTokenURL || g.HTTPClient == nil {
		t.Fatalf("NovoGoogle sem os endereços do Google: %+v", g)
	}
	raw := g.URLDeAutorizacao("o-state", "o-nonce", DesafioS256("o-verificador"))
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host+u.Path != GoogleAuthURL {
		t.Errorf("endereço = %s", raw)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": "cliente", "redirect_uri": "http://localhost:8080/auth/google/callback",
		"response_type": "code", "scope": "openid email profile", "state": "o-state", "nonce": "o-nonce",
		"code_challenge": DesafioS256("o-verificador"), "code_challenge_method": "S256", "prompt": "select_account",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, quero %q", k, q.Get(k), v)
		}
	}
	if len(q) != len(want) {
		t.Errorf("parâmetros a mais: %v", q)
	}
	if strings.Contains(raw, "segredo-que-nao-vai-na-url") || strings.Contains(raw, "o-verificador") {
		t.Error("a URL leva o client secret ou o verificador")
	}
}

func TestGoogle_TrocarCodigo(t *testing.T) {
	var got url.Values
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(body))
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch got.Get("code") {
		case "erro":
			http.Error(w, `{"error":"invalid_grant","segredo":"nao-pode-vazar"}`, http.StatusBadRequest)
		case "sem-token":
			_, _ = io.WriteString(w, `{"access_token":"x"}`)
		case "redireciona":
			http.Redirect(w, r, "https://outro.example/token", http.StatusTemporaryRedirect)
		case "lento":
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		default:
			_, _ = io.WriteString(w, `{"id_token":"o.id.token","access_token":"x"}`)
		}
	}))
	defer srv.Close()
	g := NovoGoogle("cliente", "o-segredo", "http://localhost:8080/auth/google/callback")
	g.TokenURL, g.HTTPClient = srv.URL+"/token", srv.Client()

	tok, err := g.TrocarCodigo(context.Background(), "codigo-bom", "o-verificador")
	if err != nil || tok != "o.id.token" {
		t.Fatalf("TrocarCodigo = %q, %v", tok, err)
	}
	want := map[string]string{
		"grant_type": "authorization_code", "code": "codigo-bom", "client_id": "cliente",
		"client_secret": "o-segredo", "code_verifier": "o-verificador",
		"redirect_uri": "http://localhost:8080/auth/google/callback",
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("%s = %q, quero %q", k, got.Get(k), v)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	for _, code := range []string{"erro", "sem-token", "redireciona", "lento"} {
		c := context.Background()
		if code == "lento" {
			c = ctx
		}
		_, err := g.TrocarCodigo(c, code, "v")
		if !errors.Is(err, ErrGoogle) {
			t.Errorf("%s: %v, quero ErrGoogle", code, err)
		}
		if err != nil && strings.Contains(err.Error(), "nao-pode-vazar") {
			t.Errorf("%s: o erro traz o corpo da resposta: %v", code, err)
		}
	}
	// O client sem redirecionamento: o padrão de NovoGoogle também não segue.
	if err := NovoGoogle("c", "s", "r").HTTPClient.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("CheckRedirect = %v, quero ErrUseLastResponse", err)
	}
}
