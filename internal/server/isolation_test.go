package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
)

// TestAC035_TranscriptionsIsolatedByUser — AC-035 ("Dados cloud isolados
// por user"), NFR-012 ("user_id sempre vem da sessão"), THR-004 (IDOR):
// usuário B nunca vê nem altera dado do usuário A, mesmo:
//   - adivinhando o ID do registro de A (GET/PATCH/DELETE direto);
//   - tentando forjar user_id no corpo do POST de criação.
func TestAC035_TranscriptionsIsolatedByUser(t *testing.T) {
	h := newHarness(t)

	// A cria uma transcrição.
	createBody := map[string]any{
		"id": "t-belongs-to-a", "source_platform": "windows",
		"raw_text": "texto de A", "final_text": "texto de A", "mode": "literal",
		"asr_provider": "mock",
	}
	rec := h.do(http.MethodPost, "/api/v1/transcriptions", h.tokenA, createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("criação por A: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	// B tenta forjar user_id no corpo — o payload não tem sequer o
	// campo para isso (verificado estruturalmente em outro teste), mas
	// aqui provamos o efeito: mesmo enviando "user_id": h.userA no
	// JSON cru, o registro criado por B pertence a B, não a A.
	rawWithForgedUserID := fmt.Sprintf(`{"id":"t-b-forged","user_id":%q,"source_platform":"windows","raw_text":"x","final_text":"x","mode":"literal","asr_provider":"mock"}`, h.userA)
	req := newRawJSONRequest(t, http.MethodPost, "/api/v1/transcriptions", h.tokenB, rawWithForgedUserID)
	recB := httptest.NewRecorder()
	h.handler.ServeHTTP(recB, req)
	if recB.Code != http.StatusCreated {
		t.Fatalf("criação por B com user_id forjado: status = %d, corpo = %s", recB.Code, recB.Body.String())
	}
	// Confirma que o registro criado por B com user_id forjado no
	// corpo pertence a B, não a A: A não consegue editá-lo nem apagá-lo
	// (contrato não expõe GET /transcriptions/{id}, só a listagem e as
	// mutações — usamos PATCH como sonda de posse).
	if got := h.do(http.MethodPatch, "/api/v1/transcriptions/t-b-forged", h.tokenA, map[string]any{"base_version": 1, "final_text": "invadido"}); got.Code != http.StatusNotFound {
		t.Fatalf("A conseguiu editar registro criado por B (user_id forjado vazou isolamento): status = %d", got.Code)
	}

	// B tenta editar o registro de A adivinhando o ID.
	patchBody := map[string]any{"base_version": 1, "final_text": "hackeado"}
	if rec := h.do(http.MethodPatch, "/api/v1/transcriptions/t-belongs-to-a", h.tokenB, patchBody); rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH de B no registro de A: status = %d, esperava 404", rec.Code)
	}

	// B tenta apagar o registro de A.
	if rec := h.do(http.MethodDelete, "/api/v1/transcriptions/t-belongs-to-a", h.tokenB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE de B no registro de A: status = %d, esperava 404", rec.Code)
	}

	// Confirma que nada disso afetou o registro de A: A ainda o vê
	// intacto na sua listagem, e é o único item que ele vê (nunca a de
	// B).
	rec = h.do(http.MethodGet, "/api/v1/transcriptions", h.tokenA, nil)
	list := decodeBody[struct {
		Items []struct {
			ID        string `json:"id"`
			FinalText string `json:"final_text"`
		} `json:"items"`
	}](t, rec)
	if len(list.Items) != 1 || list.Items[0].ID != "t-belongs-to-a" || list.Items[0].FinalText != "texto de A" {
		t.Fatalf("listagem de A após tentativas de B = %+v, esperava só t-belongs-to-a intacto", list.Items)
	}
}

// TestAC035_DevicesIsolatedByUser — mesma garantia para /devices,
// semeado diretamente no repositório (como device-auth ainda não existe
// nesta fase).
func TestAC035_DevicesIsolatedByUser(t *testing.T) {
	h := newHarness(t)
	devices := server.NewInMemoryDeviceRepository()
	devices.Put(server.Device{ID: "dev-a", UserID: h.userA, Name: "PC da A", Platform: "windows"})
	devices.Put(server.Device{ID: "dev-b", UserID: h.userB, Name: "Celular da B", Platform: "android"})

	srv := server.New(server.Deps{Sessions: h.sess, Devices: devices, Clock: h.fake})
	h.handler = srv.Handler()

	// B não vê o device de A na listagem.
	rec := h.do(http.MethodGet, "/api/v1/devices", h.tokenB, nil)
	list := decodeBody[struct {
		Devices []struct {
			ID string `json:"id"`
		} `json:"devices"`
	}](t, rec)
	for _, d := range list.Devices {
		if d.ID == "dev-a" {
			t.Fatalf("listagem de devices de B inclui device de A")
		}
	}

	// B não consegue renomear nem revogar o device de A.
	if rec := h.do(http.MethodPatch, "/api/v1/devices/dev-a", h.tokenB, map[string]any{"name": "invadido"}); rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH de B no device de A: status = %d, esperava 404", rec.Code)
	}
	if rec := h.do(http.MethodDelete, "/api/v1/devices/dev-a", h.tokenB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE de B no device de A: status = %d, esperava 404", rec.Code)
	}

	// A ainda enxerga seu device intacto.
	rec = h.do(http.MethodGet, "/api/v1/devices", h.tokenA, nil)
	listA := decodeBody[struct {
		Devices []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"devices"`
	}](t, rec)
	if len(listA.Devices) != 1 || listA.Devices[0].Name != "PC da A" {
		t.Fatalf("device de A alterado por B: %+v", listA.Devices)
	}
}

// TestAC035_DictionaryIsolatedByUser — mesma garantia para /dictionary.
func TestAC035_DictionaryIsolatedByUser(t *testing.T) {
	h := newHarness(t)

	createBody := map[string]any{"id": "dict-a", "canonical_form": "DCTFWeb"}
	if rec := h.do(http.MethodPost, "/api/v1/dictionary", h.tokenA, createBody); rec.Code != http.StatusCreated {
		t.Fatalf("criação por A: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	if rec := h.do(http.MethodPatch, "/api/v1/dictionary/dict-a", h.tokenB, map[string]any{"canonical_form": "hackeado"}); rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH de B no dictionary de A: status = %d, esperava 404", rec.Code)
	}
	if rec := h.do(http.MethodDelete, "/api/v1/dictionary/dict-a", h.tokenB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE de B no dictionary de A: status = %d, esperava 404", rec.Code)
	}

	rec := h.do(http.MethodGet, "/api/v1/dictionary", h.tokenB, nil)
	list := decodeBody[struct {
		Items []struct {
			CanonicalForm string `json:"canonical_form"`
		} `json:"items"`
	}](t, rec)
	if len(list.Items) != 0 {
		t.Fatalf("listagem de dictionary de B vê entry de A: %+v", list.Items)
	}
}

// TestAC035_SyncIsolatedByUser — mesmo push/pull, escopado por
// sessão: B nunca vê os eventos de A, mesmo consultando com
// after_seq=0 (início do stream) e mesmo se seus contadores de seq
// coincidirem numericamente (cada usuário tem sua própria linha do
// tempo de seq nesta implementação — ver sync.go).
func TestAC035_SyncIsolatedByUser(t *testing.T) {
	h := newHarness(t)

	pushA := map[string]any{
		"device_id": "dev-a",
		"mutations": []map[string]any{
			{"mutation_id": "mut-a-1", "entity_type": "transcription", "entity_id": "ent-a",
				"operation": "upsert", "base_version": 0, "payload": map[string]any{"final_text": "de A"}},
		},
	}
	if rec := h.do(http.MethodPost, "/api/v1/sync/push", h.tokenA, pushA); rec.Code != http.StatusOK {
		t.Fatalf("push de A: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", h.tokenB, nil)
	pull := decodeBody[struct {
		Changes []map[string]any `json:"changes"`
	}](t, rec)
	if len(pull.Changes) != 0 {
		t.Fatalf("pull de B vê mudanças de A: %+v", pull.Changes)
	}
}

// --- helpers deste arquivo ---

func newRawJSONRequest(t *testing.T, method, path, token, rawBody string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}
