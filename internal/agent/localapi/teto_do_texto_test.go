package localapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O PC não aceita a edição que a nuvem recusaria (ADR-019, "Decisões da F2";
// F7a): o texto acima de 64 KiB e o caractere nulo dão 400 e não mudam o
// ditado. Sem isso, a edição valeria aqui e ficaria
// "recusada" no sync, sem a pessoa saber.
func TestPatchHistory_TetoDoTexto(t *testing.T) {
	for _, c := range []struct {
		nome   string
		texto  string
		status int
	}{
		{"no teto", strings.Repeat("a", storage.MaxTextoBytes), http.StatusOK},
		{"acima do teto", strings.Repeat("a", storage.MaxTextoBytes+1), http.StatusBadRequest},
		{"acima do teto em bytes, com acento", strings.Repeat("ã", storage.MaxTextoBytes/2+1), http.StatusBadRequest},
		{"caractere nulo", "antes\x00depois", http.StatusBadRequest},
	} {
		t.Run(c.nome, func(t *testing.T) {
			srv, repo := newTestServer(t)
			seedTranscription(repo, "t1", time.Now().UTC(), nil)
			body, _ := json.Marshal(map[string]any{"base_version": 1, "final_text": c.texto})
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/history/t1", bytes.NewReader(body)))
			if w.Code != c.status {
				t.Fatalf("status = %d; quero %d (%s)", w.Code, c.status, w.Body.String())
			}
			if c.status != http.StatusOK {
				if got, _ := repo.Get(t.Context(), testUserID, "t1"); got.FinalText != "texto final" || got.Version != 1 {
					t.Errorf("a edição recusada mudou o ditado: %q v%d", got.FinalText, got.Version)
				}
			}
		})
	}
}

// O corpo do PATCH tem teto: um corpo enorme é 400, sem ser lido inteiro.
func TestPatchHistory_CorpoGrandeDemais(t *testing.T) {
	srv, repo := newTestServer(t)
	seedTranscription(repo, "t1", time.Now().UTC(), nil)
	body := `{"base_version":1,"final_text":"` + strings.Repeat(`\u0000`, 200<<10) + `"}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/history/t1", strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}
