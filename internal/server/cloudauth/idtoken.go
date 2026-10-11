package cloudauth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Identidade é quem o ID token diz que entrou. A chave é o Sub, nunca o
// e-mail (ADR-019, "Login"). Email vem em minúsculas.
type Identidade struct {
	Sub   string
	Email string
	Nome  string
}

// FolgaDoRelogio é a tolerância para exp e iat: o relógio do servidor e o do
// Google não precisam bater no segundo.
const FolgaDoRelogio = 5 * time.Minute

// Emissores aceitos no iss (a documentação do Google cita os dois).
var emissoresGoogle = []string{"https://accounts.google.com", "accounts.google.com"}

// Por que cada claim falhou. ErrEmailNaoVerificado é a única que vira um
// motivo próprio no log (email_nao_verificado); as outras são token_invalido.
var (
	ErrTokenMalformado    = errors.New("id token malformado")
	ErrEmissor            = errors.New("id token: iss não é o Google")
	ErrAudiencia          = errors.New("id token: aud não é este client")
	ErrAzp                = errors.New("id token: azp não é este client")
	ErrVencido            = errors.New("id token: vencido (exp)")
	ErrEmitidoNoFuturo    = errors.New("id token: emitido no futuro (iat)")
	ErrNonce              = errors.New("id token: nonce diferente")
	ErrSub                = errors.New("id token: sub vazio ou grande demais")
	ErrSemEmail           = errors.New("id token: sem e-mail")
	ErrEmailNaoVerificado = errors.New("id token: e-mail não verificado")
)

// maxSub: o OpenID Connect Core (2) limita o sub a 255 caracteres ASCII, e o
// banco também (a 0001).
const maxSub = 255

// maxNome: o nome que a lista de sessões e o /me mostram. O resto é cortado.
const maxNome = 100

// ValidarIDToken confere o ID token que o endpoint de token do Google
// devolveu, SEM conferir a assinatura.
//
// Por que é seguro sem a assinatura: o token chegou direto do endpoint de
// token do Google (GoogleTokenURL, fixo no código), por TLS, numa chamada
// autenticada com o client secret. É o caso que o OpenID Connect Core 1.0
// (3.1.3.7, item 6) e a documentação do Google dispensam da checagem da
// assinatura: o TLS já garante quem emitiu. Se um dia o ID token chegar pelo
// navegador (o One Tap, por exemplo), aí a assinatura tem de ser conferida
// pelas chaves públicas do Google (JWKS).
//
// O que confere, nesta ordem: iss é o Google; aud é o clientID (e o azp,
// quando vem); exp e iat, com FolgaDoRelogio; o nonce é o que o servidor
// guardou no início do login; sub não vazio; e-mail presente e
// email_verified igual a true (o booleano, não o texto "true").
func ValidarIDToken(token, clientID, nonce string, agora time.Time) (Identidade, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return Identidade{}, ErrTokenMalformado
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return Identidade{}, ErrTokenMalformado
	}
	var c struct {
		Iss           string          `json:"iss"`
		Aud           json.RawMessage `json:"aud"`
		Azp           string          `json:"azp"`
		Exp           *int64          `json:"exp"`
		Iat           *int64          `json:"iat"`
		Nonce         string          `json:"nonce"`
		Sub           string          `json:"sub"`
		Email         string          `json:"email"`
		EmailVerified json.RawMessage `json:"email_verified"`
		Name          string          `json:"name"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return Identidade{}, ErrTokenMalformado
	}

	if !contem(emissoresGoogle, c.Iss) {
		return Identidade{}, ErrEmissor
	}
	aud, ok := audiencias(c.Aud)
	if !ok || clientID == "" || !contem(aud, clientID) {
		return Identidade{}, ErrAudiencia
	}
	// Com mais de uma audiência, o OpenID Connect exige o azp; quando vem, é
	// este client.
	if len(aud) > 1 && c.Azp == "" || c.Azp != "" && c.Azp != clientID {
		return Identidade{}, ErrAzp
	}
	if c.Exp == nil || !agora.Before(time.Unix(*c.Exp, 0).Add(FolgaDoRelogio)) {
		return Identidade{}, ErrVencido
	}
	if c.Iat == nil || time.Unix(*c.Iat, 0).After(agora.Add(FolgaDoRelogio)) {
		return Identidade{}, ErrEmitidoNoFuturo
	}
	if nonce == "" || c.Nonce != nonce {
		return Identidade{}, ErrNonce
	}
	if c.Sub == "" || len(c.Sub) > maxSub || !ascii(c.Sub) {
		return Identidade{}, ErrSub
	}
	email := NormalizarEmail(c.Email)
	if email == "" {
		return Identidade{}, ErrSemEmail
	}
	if !bytes.Equal(bytes.TrimSpace(c.EmailVerified), []byte("true")) {
		return Identidade{}, ErrEmailNaoVerificado
	}
	return Identidade{Sub: c.Sub, Email: email, Nome: limparNome(c.Name)}, nil
}

// audiencias lê o aud, que pode ser um texto ou uma lista.
func audiencias(raw json.RawMessage) ([]string, bool) {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, true
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, false
	}
	return many, true
}

func contem(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// limparNome tira caracteres de controle e corta em maxNome runas.
func limparNome(s string) string {
	if !utf8.ValidString(s) {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsControl(r) {
			continue
		}
		if n == maxNome {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
