package guard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireOrigin(t *testing.T) {
	const port = 54321
	handler := RequireOrigin(port)(okHandler())

	tests := []struct {
		name       string
		origin     string // "" significa: nao envia o header
		wantStatus int
	}{
		{
			name:       "pagina maliciosa cross-origin",
			origin:     "https://evil.example",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "origin ausente e permitido (cliente nao-browser)",
			origin:     "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "origin http://127.0.0.1:porta correto",
			origin:     "http://127.0.0.1:54321",
			wantStatus: http.StatusOK,
		},
		{
			name:       "origin http://localhost:porta correto",
			origin:     "http://localhost:54321",
			wantStatus: http.StatusOK,
		},
		{
			name:       "origin com porta errada",
			origin:     "http://127.0.0.1:9999",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "origin null (iframe sandboxed / file://)",
			origin:     "null",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("Origin=%q: status = %d, esperava %d", tt.origin, rec.Code, tt.wantStatus)
			}
		})
	}
}
