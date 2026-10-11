package cloudauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var agora0 = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

const clientTeste = "cliente-teste.apps.googleusercontent.com"

// jwt monta um ID token com as claims dadas e uma assinatura qualquer (a
// validação não a confere: ver ValidarIDToken).
func jwt(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return enc(map[string]string{"alg": "RS256", "kid": "x", "typ": "JWT"}) + "." + enc(claims) + ".YXNzaW5hdHVyYQ"
}

func claimsBoas() map[string]any {
	return map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            clientTeste,
		"azp":            clientTeste,
		"sub":            "110169484474386276334",
		"email":          "Mauro@Exemplo.test",
		"email_verified": true,
		"nonce":          "nonce-do-servidor",
		"iat":            agora0.Add(-time.Minute).Unix(),
		"exp":            agora0.Add(time.Hour).Unix(),
		"name":           "Mauro\x07 Juliani",
	}
}

func com(mudar func(c map[string]any)) map[string]any {
	c := claimsBoas()
	mudar(c)
	return c
}

func TestValidarIDToken_CaminhoFeliz(t *testing.T) {
	id, err := ValidarIDToken(jwt(t, claimsBoas()), clientTeste, "nonce-do-servidor", agora0)
	if err != nil {
		t.Fatalf("ValidarIDToken: %v", err)
	}
	if id.Sub != "110169484474386276334" || id.Email != "mauro@exemplo.test" || id.Nome != "Mauro Juliani" {
		t.Fatalf("identidade = %+v (o e-mail em minúsculas, o nome sem controle)", id)
	}
	// O outro iss que o Google usa, aud em lista com azp, e sem azp com uma
	// audiência só.
	for name, c := range map[string]map[string]any{
		"iss sem https":       com(func(c map[string]any) { c["iss"] = "accounts.google.com" }),
		"aud em lista":        com(func(c map[string]any) { c["aud"] = []string{"outro", clientTeste} }),
		"sem azp":             com(func(c map[string]any) { delete(c, "azp") }),
		"exp dentro da folga": com(func(c map[string]any) { c["exp"] = agora0.Add(-4 * time.Minute).Unix() }),
		"iat dentro da folga": com(func(c map[string]any) { c["iat"] = agora0.Add(4 * time.Minute).Unix() }),
	} {
		if _, err := ValidarIDToken(jwt(t, c), clientTeste, "nonce-do-servidor", agora0); err != nil {
			t.Errorf("%s: %v, quero aceito", name, err)
		}
	}
}

// Cada claim errada é recusada pelo motivo certo.
func TestValidarIDToken_CadaClaimErrada(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  error
	}{
		{"iss de outro emissor", "", ErrEmissor},
		{"iss vazio", "", ErrEmissor},
		{"aud de outro client", "", ErrAudiencia},
		{"aud em lista sem este client", "", ErrAudiencia},
		{"aud ausente", "", ErrAudiencia},
		{"aud número", "", ErrAudiencia},
		{"azp de outro client", "", ErrAzp},
		{"duas audiências sem azp", "", ErrAzp},
		{"vencido além da folga", "", ErrVencido},
		{"sem exp", "", ErrVencido},
		{"emitido no futuro além da folga", "", ErrEmitidoNoFuturo},
		{"sem iat", "", ErrEmitidoNoFuturo},
		{"nonce diferente", "", ErrNonce},
		{"sem nonce", "", ErrNonce},
		{"sub vazio", "", ErrSub},
		{"sub de 256", "", ErrSub},
		{"sub com espaço", "", ErrSub},
		{"sem e-mail", "", ErrSemEmail},
		{"email_verified false", "", ErrEmailNaoVerificado},
		{"email_verified texto", "", ErrEmailNaoVerificado},
		{"sem email_verified", "", ErrEmailNaoVerificado},
	}
	mut := map[string]func(c map[string]any){
		"iss de outro emissor":            func(c map[string]any) { c["iss"] = "https://evil.example" },
		"iss vazio":                       func(c map[string]any) { delete(c, "iss") },
		"aud de outro client":             func(c map[string]any) { c["aud"] = "outro" },
		"aud em lista sem este client":    func(c map[string]any) { c["aud"] = []string{"a", "b"} },
		"aud ausente":                     func(c map[string]any) { delete(c, "aud") },
		"aud número":                      func(c map[string]any) { c["aud"] = 7 },
		"azp de outro client":             func(c map[string]any) { c["azp"] = "outro" },
		"duas audiências sem azp":         func(c map[string]any) { c["aud"] = []string{clientTeste, "outro"}; delete(c, "azp") },
		"vencido além da folga":           func(c map[string]any) { c["exp"] = agora0.Add(-6 * time.Minute).Unix() },
		"sem exp":                         func(c map[string]any) { delete(c, "exp") },
		"emitido no futuro além da folga": func(c map[string]any) { c["iat"] = agora0.Add(6 * time.Minute).Unix() },
		"sem iat":                         func(c map[string]any) { delete(c, "iat") },
		"nonce diferente":                 func(c map[string]any) { c["nonce"] = "outro" },
		"sem nonce":                       func(c map[string]any) { delete(c, "nonce") },
		"sub vazio":                       func(c map[string]any) { c["sub"] = "" },
		"sub de 256":                      func(c map[string]any) { c["sub"] = strings.Repeat("9", 256) },
		"sub com espaço":                  func(c map[string]any) { c["sub"] = "1 2" },
		"sem e-mail":                      func(c map[string]any) { delete(c, "email") },
		"email_verified false":            func(c map[string]any) { c["email_verified"] = false },
		"email_verified texto":            func(c map[string]any) { c["email_verified"] = "true" },
		"sem email_verified":              func(c map[string]any) { delete(c, "email_verified") },
	}
	for _, c := range cases {
		_, err := ValidarIDToken(jwt(t, com(mut[c.name])), clientTeste, "nonce-do-servidor", agora0)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, quero %v", c.name, err, c.want)
		}
	}
	// O nonce esperado vazio nunca passa, nem com o token sem nonce.
	if _, err := ValidarIDToken(jwt(t, com(func(c map[string]any) { delete(c, "nonce") })), clientTeste, "", agora0); !errors.Is(err, ErrNonce) {
		t.Errorf("nonce esperado vazio: %v, quero ErrNonce", err)
	}
	// O clientID vazio nunca passa.
	if _, err := ValidarIDToken(jwt(t, com(func(c map[string]any) { c["aud"] = "" })), "", "nonce-do-servidor", agora0); !errors.Is(err, ErrAudiencia) {
		t.Errorf("clientID vazio: %v, quero ErrAudiencia", err)
	}
}

func TestValidarIDToken_Malformado(t *testing.T) {
	for _, tok := range []string{"", "a.b", "a.b.c.d", ".x.y", "a..c", "a.!!!.c",
		"a." + base64.RawURLEncoding.EncodeToString([]byte("não é json")) + ".c"} {
		if _, err := ValidarIDToken(tok, clientTeste, "n", agora0); !errors.Is(err, ErrTokenMalformado) {
			t.Errorf("%q: %v, quero ErrTokenMalformado", tok, err)
		}
	}
}
