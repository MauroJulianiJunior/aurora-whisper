package guard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewToken_Randomness(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken() erro: %v", err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken() erro: %v", err)
	}
	if a == b {
		t.Fatal("duas chamadas a NewToken produziram o mesmo valor")
	}
	if len(a) != tokenBytes*2 { // hex: 2 chars por byte
		t.Fatalf("token com tamanho inesperado: %d chars", len(a))
	}
}

func TestRequireToken(t *testing.T) {
	const token = "segredo-de-teste-com-mesmo-tamanho-do-esperado"
	handler := RequireToken(token)(okHandler())

	tests := []struct {
		name       string
		authHeader string // "" significa: nao envia o header
		wantStatus int
	}{
		{
			name:       "sem token",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token errado",
			authHeader: "Bearer token-invalido-mas-mesmo-tamanho-do-outro",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "sem o prefixo Bearer",
			authHeader: token,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token correto",
			authHeader: "Bearer " + token,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("Authorization=%q: status = %d, esperava %d", tt.authHeader, rec.Code, tt.wantStatus)
			}
		})
	}
}
