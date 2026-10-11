package localapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O selo no painel do PC (ADR-019, "Decisões da F7c"): o item leva a versão
// que perdeu (versao_em_conflito, no formato da nuvem), e o PATCH com
// resolver_conflito escolhe, com o mesmo contrato da nuvem.

func comSelo(tr *storage.Transcription) {
	pc := "0199a000-0000-7000-8000-0000000000c2"
	tr.Version = 3
	tr.Conflito = &storage.Conflito{FinalText: "a outra versão", DeviceID: &pc,
		Em: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)}
}

func patch(t *testing.T, srv http.Handler, id string, corpo map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(corpo)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/history/"+id, bytes.NewReader(body)))
	return w
}

func TestSelo_OItemLevaAVersaoQuePerdeu(t *testing.T) {
	srv, repo := newTestServer(t)
	seedTranscription(repo, "com", time.Now().UTC(), comSelo)
	seedTranscription(repo, "sem", time.Now().UTC().Add(-time.Minute), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var p struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || len(p.Items) != 2 {
		t.Fatalf("lista: %s", w.Body.String())
	}
	selo, ok := p.Items[0]["versao_em_conflito"].(map[string]any)
	if !ok || selo["final_text"] != "a outra versão" || selo["conflict_at"] != "2026-10-06T10:00:00Z" ||
		selo["device_id"] != "0199a000-0000-7000-8000-0000000000c2" {
		t.Errorf("o selo na lista: %v", p.Items[0])
	}
	if _, tem := p.Items[1]["versao_em_conflito"]; tem {
		t.Errorf("o ditado sem selo trouxe o campo: %v", p.Items[1])
	}
}

func TestSelo_Escolher(t *testing.T) {
	srv, repo := newTestServer(t)
	seedTranscription(repo, "s1", time.Now().UTC(), comSelo)
	seedTranscription(repo, "s2", time.Now().UTC(), comSelo)
	seedTranscription(repo, "sem", time.Now().UTC(), nil)
	h := srv.Handler()

	for nome, c := range map[string]struct {
		id     string
		corpo  map[string]any
		status int
	}{
		"o texto e a escolha juntos": {"s1", map[string]any{"base_version": 3, "final_text": "x", "resolver_conflito": "trocar"}, http.StatusBadRequest},
		"escolha desconhecida":       {"s1", map[string]any{"base_version": 3, "resolver_conflito": "as duas"}, http.StatusBadRequest},
		"escolha vazia":              {"s1", map[string]any{"base_version": 3, "resolver_conflito": ""}, http.StatusBadRequest},
		"escolha que não é texto":    {"s1", map[string]any{"base_version": 3, "resolver_conflito": true}, http.StatusBadRequest},
		"base velha":                 {"s1", map[string]any{"base_version": 2, "resolver_conflito": "trocar"}, http.StatusConflict},
		"sem selo":                   {"sem", map[string]any{"base_version": 1, "resolver_conflito": "manter"}, http.StatusConflict},
		"que não existe":             {"nada", map[string]any{"base_version": 1, "resolver_conflito": "manter"}, http.StatusNotFound},
	} {
		if w := patch(t, h, c.id, c.corpo); w.Code != c.status {
			t.Errorf("%s: %d (%s); quero %d", nome, w.Code, w.Body.String(), c.status)
		}
	}
	if g, _ := repo.Get(t.Context(), testUserID, "s1"); g.Version != 3 || g.Conflito == nil {
		t.Fatalf("as recusas mudaram o ditado: %+v", g)
	}

	w := patch(t, h, "s1", map[string]any{"base_version": 3, "resolver_conflito": "trocar"})
	if w.Code != http.StatusOK {
		t.Fatalf("trocar: %d %s", w.Code, w.Body.String())
	}
	var it map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &it)
	if it["final_text"] != "a outra versão" || it["version"] != float64(4) || it["versao_em_conflito"] != nil || it["sugestoes"] != nil {
		t.Errorf("trocar devolveu %v", it)
	}
	w = patch(t, h, "s2", map[string]any{"base_version": 3, "resolver_conflito": "manter"})
	_ = json.Unmarshal(w.Body.Bytes(), &it)
	if w.Code != http.StatusOK || it["final_text"] != "texto final" || it["versao_em_conflito"] != nil {
		t.Errorf("manter: %d %v", w.Code, it)
	}
	// A edição de sempre continua igual (sem o campo da escolha).
	if w := patch(t, h, "sem", map[string]any{"base_version": 1, "final_text": "editado"}); w.Code != http.StatusOK {
		t.Errorf("a edição: %d %s", w.Code, w.Body.String())
	}
}
