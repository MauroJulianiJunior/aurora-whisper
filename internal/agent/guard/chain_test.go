package guard

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProtect_FullChain exercita Protect (Host + Origin + Token juntos)
// como a Local API real vai usá-lo: token certo + Host certo deve
// passar; qualquer defesa isolada falhando deve barrar a requisição.
func TestProtect_FullChain(t *testing.T) {
	const port = 54321
	const token = "token-valido-para-o-teste-de-integracao"

	handler := Protect(port, token)(okHandler())

	tests := []struct {
		name       string
		host       string
		origin     string // "" = nao envia
		authHeader string // "" = nao envia
		wantStatus int
	}{
		{
			name:       "token certo + Host certo -> 200",
			host:       "127.0.0.1:54321",
			authHeader: "Bearer " + token,
			wantStatus: http.StatusOK,
		},
		{
			name:       "Host errado bloqueia mesmo com token certo",
			host:       "attacker.example",
			authHeader: "Bearer " + token,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "Origin cross-origin bloqueia mesmo com Host e token certos",
			host:       "127.0.0.1:54321",
			origin:     "https://evil.example",
			authHeader: "Bearer " + token,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "Host e Origin certos mas sem token -> 401",
			host:       "localhost:54321",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, esperava %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestLogRequests_NeverLeaksToken prova, capturando o log num buffer,
// que o token nunca aparece em nenhuma saida de log — mesmo quando a
// cadeia completa (incluindo LogRequests) processa requisicoes com o
// token certo e com o token errado.
func TestLogRequests_NeverLeaksToken(t *testing.T) {
	const port = 54321
	const token = "token-super-secreto-que-jamais-pode-vazar-em-log"

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	chain := Chain(
		LogRequests(logger),
		Protect(port, token),
	)
	handler := chain(okHandler())

	requests := []struct {
		authHeader string
	}{
		{authHeader: "Bearer " + token},                       // sucesso
		{authHeader: "Bearer token-errado-mas-mesmo-tamanho"}, // 401
		{authHeader: ""}, // 401, sem header
	}

	for _, r := range requests {
		req := httptest.NewRequest(http.MethodGet, "/historico", nil)
		req.Host = "127.0.0.1:54321"
		if r.authHeader != "" {
			req.Header.Set("Authorization", r.authHeader)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	output := buf.String()
	if output == "" {
		t.Fatal("esperava alguma saida de log (LogRequests deveria ter registrado as 3 requisicoes)")
	}
	if strings.Contains(output, token) {
		t.Fatalf("o token vazou no log:\n%s", output)
	}
	if strings.Contains(strings.ToLower(output), "bearer") {
		t.Fatalf("o header Authorization vazou no log:\n%s", output)
	}
}
