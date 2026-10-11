package guard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireHost(t *testing.T) {
	const port = 54321
	handler := RequireHost(port)(okHandler())

	tests := []struct {
		name       string
		host       string
		wantStatus int
	}{
		{
			name:       "ataque de DNS rebinding: dominio do atacante",
			host:       "attacker.example",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "127.0.0.1 com porta errada",
			host:       "127.0.0.1:9999",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "127.0.0.1 sem porta",
			host:       "127.0.0.1",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "localhost sem porta",
			host:       "localhost",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "localhost em caixa alta: deve funcionar (case-insensitive)",
			host:       "LOCALHOST:54321",
			wantStatus: http.StatusOK,
		},
		{
			name:       "127.0.0.1 correto",
			host:       "127.0.0.1:54321",
			wantStatus: http.StatusOK,
		},
		{
			name:       "localhost correto",
			host:       "localhost:54321",
			wantStatus: http.StatusOK,
		},
		{
			name:       "loopback IPv6 nao esta no allowlist",
			host:       "[::1]:54321",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "host com espaco",
			host:       "127.0.0.1:54321 ",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "host vazio",
			host:       "",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("Host=%q: status = %d, esperava %d", tt.host, rec.Code, tt.wantStatus)
			}
		})
	}
}
