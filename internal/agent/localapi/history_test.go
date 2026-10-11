package localapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

const testUserID = "user-1"

func ptr[T any](v T) *T { return &v }

func newTestServer(t *testing.T) (*localapi.Server, *storage.InMemoryTranscriptionRepository) {
	t.Helper()
	repo := storage.NewInMemoryTranscriptionRepository()
	settings := storage.NewInMemorySettingsRepository()
	return localapi.New(testUserID, repo, settings, prefsDeTeste()), repo
}

func seedTranscription(repo *storage.InMemoryTranscriptionRepository, id string, createdAt time.Time, mutate func(*storage.Transcription)) storage.Transcription {
	tr := storage.Transcription{
		ID:             id,
		UserID:         testUserID,
		SourcePlatform: "windows",
		RawText:        "texto bruto",
		FinalText:      "texto final",
		Mode:           "smart",
		ASRProvider:    "local",
		ASRModel:       ptr("small-pt"),
		DurationMs:     ptr(int64(3200)),
		ProcessingMs:   ptr(int64(410)),
		Confidence:     ptr(0.91),
		TargetApp:      ptr("chrome"),
		Status:         "completed",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
		Version:        1,
		SyncState:      "pending",
	}
	if mutate != nil {
		mutate(&tr)
	}
	repo.Put(tr)
	return tr
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decodificar resposta JSON: %v (body=%s)", err, w.Body.String())
	}
	return v
}

// historyItemView e historyPageView espelham só os campos que os
// testes deste arquivo precisam inspecionar na resposta JSON.
type historyItemView struct {
	ID           string  `json:"id"`
	FinalText    string  `json:"final_text"`
	RawText      string  `json:"raw_text"`
	ASRProvider  string  `json:"asr_provider"`
	ASRModel     *string `json:"asr_model"`
	DurationMs   *int64  `json:"duration_ms"`
	ProcessingMs *int64  `json:"processing_ms"`
	Version      int64   `json:"version"`
}

type historyPageView struct {
	Items      []historyItemView `json:"items"`
	NextCursor string            `json:"next_cursor"`
}

// AC-011: o dashboard local consegue listar o histórico.
func TestAC011_ListHistory(t *testing.T) {
	srv, repo := newTestServer(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	seedTranscription(repo, "t1", base, nil)
	seedTranscription(repo, "t2", base.Add(time.Minute), nil)
	// Transcrição de outro usuário não deve aparecer.
	seedTranscription(repo, "t3", base.Add(2*time.Minute), func(tr *storage.Transcription) {
		tr.UserID = "outro-usuario"
	})

	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	page := decodeJSON[historyPageView](t, w)
	if len(page.Items) != 2 {
		t.Fatalf("esperava 2 itens, veio %d: %+v", len(page.Items), page.Items)
	}
	// Mais recente primeiro.
	if page.Items[0].ID != "t2" || page.Items[1].ID != "t1" {
		t.Fatalf("ordem inesperada: %+v", page.Items)
	}
}

// AC-043: a resposta traz provider, modelo, duração e latência
// (processing_ms).
func TestAC043_ResponseHasProviderModelDurationLatency(t *testing.T) {
	srv, repo := newTestServer(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	seedTranscription(repo, "t1", base, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/history/t1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	item := decodeJSON[historyItemView](t, w)
	if item.ASRProvider != "local" {
		t.Errorf("asr_provider = %q, esperava %q", item.ASRProvider, "local")
	}
	if item.ASRModel == nil || *item.ASRModel != "small-pt" {
		t.Errorf("asr_model = %v, esperava small-pt", item.ASRModel)
	}
	if item.DurationMs == nil || *item.DurationMs != 3200 {
		t.Errorf("duration_ms = %v, esperava 3200", item.DurationMs)
	}
	if item.ProcessingMs == nil || *item.ProcessingMs != 410 {
		t.Errorf("processing_ms = %v, esperava 410", item.ProcessingMs)
	}
}

// AC-013: buscar, editar e excluir funcionam; excluir manda para a
// lixeira e restore traz de volta.
func TestAC013_SearchEditDeleteRestore(t *testing.T) {
	srv, repo := newTestServer(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	seedTranscription(repo, "t1", base, func(tr *storage.Transcription) {
		tr.FinalText = "abrir o modulo fiscal"
	})
	seedTranscription(repo, "t2", base.Add(time.Minute), func(tr *storage.Transcription) {
		tr.FinalText = "outro assunto qualquer"
	})

	t.Run("busca", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/history?q=fiscal", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		page := decodeJSON[historyPageView](t, w)
		if len(page.Items) != 1 || page.Items[0].ID != "t1" {
			t.Fatalf("busca não filtrou como esperado: %+v", page.Items)
		}
	})

	t.Run("editar", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"base_version": 1, "final_text": "texto editado"})
		req := httptest.NewRequest(http.MethodPatch, "/api/history/t1", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		item := decodeJSON[historyItemView](t, w)
		if item.FinalText != "texto editado" {
			t.Errorf("final_text = %q, esperava %q", item.FinalText, "texto editado")
		}
		if item.Version != 2 {
			t.Errorf("version = %d, esperava 2", item.Version)
		}
	})

	t.Run("excluir e restore", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/history/t2", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("DELETE status = %d, body = %s", w.Code, w.Body.String())
		}

		// Some do histórico ativo, aparece na lixeira.
		wActive := httptest.NewRecorder()
		srv.Handler().ServeHTTP(wActive, httptest.NewRequest(http.MethodGet, "/api/history", nil))
		active := decodeJSON[historyPageView](t, wActive)
		for _, it := range active.Items {
			if it.ID == "t2" {
				t.Fatalf("t2 ainda aparece no histórico ativo após delete")
			}
		}

		wTrash := httptest.NewRecorder()
		srv.Handler().ServeHTTP(wTrash, httptest.NewRequest(http.MethodGet, "/api/history?trash=true", nil))
		trash := decodeJSON[historyPageView](t, wTrash)
		found := false
		for _, it := range trash.Items {
			if it.ID == "t2" {
				found = true
			}
		}
		if !found {
			t.Fatalf("t2 não aparece na lixeira após delete: %+v", trash.Items)
		}

		// Restore.
		wRestore := httptest.NewRecorder()
		srv.Handler().ServeHTTP(wRestore, httptest.NewRequest(http.MethodPost, "/api/history/t2/restore", nil))
		if wRestore.Code != http.StatusNoContent {
			t.Fatalf("restore status = %d, body = %s", wRestore.Code, wRestore.Body.String())
		}

		wActive2 := httptest.NewRecorder()
		srv.Handler().ServeHTTP(wActive2, httptest.NewRequest(http.MethodGet, "/api/history", nil))
		active2 := decodeJSON[historyPageView](t, wActive2)
		found = false
		for _, it := range active2.Items {
			if it.ID == "t2" {
				found = true
			}
		}
		if !found {
			t.Fatalf("t2 não voltou ao histórico ativo após restore")
		}
	})
}

// DELETE repetido (uma aba com a lista velha) responde 204 de novo e não
// muda nada: nem "na lixeira desde", nem a versão (revisão de
// 29-set-2026; a regra mora no repositório, e a suíte de conformidade de
// storage a cobre nas duas implementações).
func TestDeleteHistory_RepetidoNaoMexe(t *testing.T) {
	srv, repo := newTestServer(t)
	seedTranscription(repo, "t1", time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), nil)
	apagar := func() {
		t.Helper()
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/history/t1", nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("DELETE = %d, body = %s", w.Code, w.Body.String())
		}
	}

	apagar()
	antes, _ := repo.Get(t.Context(), testUserID, "t1")
	apagar()
	depois, _ := repo.Get(t.Context(), testUserID, "t1")

	if antes.DeletedAt == nil || depois.DeletedAt == nil || !depois.DeletedAt.Equal(*antes.DeletedAt) || depois.Version != antes.Version {
		t.Errorf("o segundo DELETE mudou o item: deleted_at %v -> %v, version %d -> %d",
			antes.DeletedAt, depois.DeletedAt, antes.Version, depois.Version)
	}
}

// Edição com base_version desatualizado -> 409 Conflict.
func TestPatchHistory_StaleBaseVersion_Returns409(t *testing.T) {
	srv, repo := newTestServer(t)
	seedTranscription(repo, "t1", time.Now().UTC(), nil)

	body, _ := json.Marshal(map[string]any{"base_version": 99, "final_text": "x"})
	req := httptest.NewRequest(http.MethodPatch, "/api/history/t1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, esperava 409, body = %s", w.Code, w.Body.String())
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != "CONFLICT" {
		t.Errorf("error.code = %q, esperava CONFLICT", env.Error.Code)
	}
}

// ID inexistente -> 404 em todas as rotas por ID.
func TestHistoryRoutes_UnknownID_Returns404(t *testing.T) {
	srv, _ := newTestServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"get", http.MethodGet, "/api/history/does-not-exist", nil},
		{"patch", http.MethodPatch, "/api/history/does-not-exist", []byte(`{"base_version":1,"final_text":"x"}`)},
		{"delete", http.MethodDelete, "/api/history/does-not-exist", nil},
		{"restore", http.MethodPost, "/api/history/does-not-exist/restore", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var req *http.Request
			if c.body != nil {
				req = httptest.NewRequest(c.method, c.path, bytes.NewReader(c.body))
			} else {
				req = httptest.NewRequest(c.method, c.path, nil)
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("%s %s: status = %d, esperava 404, body = %s", c.method, c.path, w.Code, w.Body.String())
			}
		})
	}
}

// Cursor de paginação percorre o conjunto inteiro sem repetir nem
// pular registro, mesmo com desempate por ID em timestamps iguais.
func TestListHistory_CursorPagination_CoversAllWithoutSkipOrRepeat(t *testing.T) {
	srv, repo := newTestServer(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	const total = 25
	want := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		id := ptrID(i)
		// Todos com o MESMO timestamp para forçar o desempate por ID.
		seedTranscription(repo, id, base, nil)
		want[id] = true
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		path := "/api/history?limit=7"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		page := decodeJSON[historyPageView](t, w)
		for _, it := range page.Items {
			if seen[it.ID] {
				t.Fatalf("item %s repetido entre páginas", it.ID)
			}
			seen[it.ID] = true
		}
		pages++
		if pages > total {
			t.Fatalf("mais páginas que itens possíveis — provável loop infinito")
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("total visto = %d, esperava %d", len(seen), total)
	}
	for id := range want {
		if !seen[id] {
			t.Fatalf("item %s nunca apareceu em nenhuma página", id)
		}
	}
}

func ptrID(i int) string {
	return fmt.Sprintf("id-%03d", i)
}
