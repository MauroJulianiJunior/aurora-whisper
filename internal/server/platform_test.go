package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
)

// TestAC042_AndroidPlatformAccepted — AC-042 ("API aceita futuro
// Android"), REQ-042: nenhuma rota deste pacote depende de Win32, e
// platform="android" é aceito em device, transcription e sync como
// qualquer outra plataforma — sem validação, sem branch especial, sem
// erro.
func TestAC042_AndroidPlatformAccepted(t *testing.T) {
	h := newHarness(t)

	// Device Android já pareado (device-auth em si é fora do escopo
	// desta slice — ver TAREFA do bloco; aqui semeamos como
	// device-auth.exchange faria).
	devices := server.NewInMemoryDeviceRepository()
	devices.Put(server.Device{ID: "android-1", UserID: h.userA, Name: "Pixel da A", Platform: "android"})
	srv := server.New(server.Deps{Sessions: h.sess, Devices: devices, Clock: h.fake})
	h.handler = srv.Handler()

	// GET /devices devolve o device android normalmente.
	rec := h.do(http.MethodGet, "/api/v1/devices", h.tokenA, nil)
	list := decodeBody[struct {
		Devices []struct {
			ID       string `json:"id"`
			Platform string `json:"platform"`
		} `json:"devices"`
	}](t, rec)
	found := false
	for _, d := range list.Devices {
		if d.ID == "android-1" {
			found = true
			if d.Platform != "android" {
				t.Fatalf("platform devolvida = %q, esperava android", d.Platform)
			}
		}
	}
	if !found {
		t.Fatalf("device android não apareceu na listagem: %+v", list.Devices)
	}

	// PATCH no device android funciona igual a qualquer outro.
	if rec := h.do(http.MethodPatch, "/api/v1/devices/android-1", h.tokenA, map[string]any{"name": "Pixel renomeado"}); rec.Code != http.StatusOK {
		t.Fatalf("PATCH em device android: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	// POST /transcriptions com source_platform="android".
	deviceID := "android-1"
	createBody := map[string]any{
		"id": "t-android-1", "device_id": deviceID, "source_platform": "android",
		"raw_text": "ditado do celular", "final_text": "ditado do celular",
		"mode": "literal", "asr_provider": "mock",
	}
	rec = h.do(http.MethodPost, "/api/v1/transcriptions", h.tokenA, createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /transcriptions com source_platform=android: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	// A listagem filtrada por device_id=android-1 devolve a transcrição.
	rec = h.do(http.MethodGet, "/api/v1/transcriptions?device_id=android-1", h.tokenA, nil)
	list2 := decodeBody[struct {
		Items []struct {
			ID             string `json:"id"`
			SourcePlatform string `json:"source_platform"`
		} `json:"items"`
	}](t, rec)
	if len(list2.Items) != 1 || list2.Items[0].SourcePlatform != "android" {
		t.Fatalf("listagem filtrada por device android = %+v", list2.Items)
	}

	// POST /sync/push e GET /sync/pull funcionam normalmente com um
	// device_id de origem android — nenhuma rota de sync depende de
	// plataforma.
	push := map[string]any{
		"device_id": deviceID,
		"mutations": []map[string]any{
			{"mutation_id": "android-mut-1", "entity_type": "transcription", "entity_id": "t-android-1",
				"operation": "upsert", "base_version": 0, "payload": map[string]any{"final_text": "editado no android"}},
		},
	}
	rec = h.do(http.MethodPost, "/api/v1/sync/push", h.tokenA, push)
	pushResult := decodeBody[struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}](t, rec)
	if rec.Code != http.StatusOK || len(pushResult.Results) != 1 || pushResult.Results[0].Status != "applied" {
		t.Fatalf("push via device android: status http = %d, resultado = %+v", rec.Code, pushResult.Results)
	}

	rec = h.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", h.tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull após push android: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	var pullBody map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &pullBody); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
}
