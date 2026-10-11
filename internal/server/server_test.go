package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/mock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
)

// testHarness sobe um server.Server completo (http.Handler puro, sem
// listener de rede — como localapi_test faz, mas este pacote não
// precisa da cadeia guard porque a autenticação AQUI é a sessão cloud,
// não loopback/Origin) com dois usuários já autenticados, para os testes
// de isolamento e dos demais recursos.
type testHarness struct {
	t       *testing.T
	handler http.Handler
	sess    *server.InMemorySessionStore
	fake    *clock.Fake

	tokenA, tokenB string
	userA, userB   string
}

const (
	tokenUserA = "token-user-a"
	tokenUserB = "token-user-b"
	userAID    = "user-a"
	userBID    = "user-b"
)

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	return newHarnessWithASR(t, mock.NewConfident("ola mundo", 0.9))
}

// newHarnessWithASR é igual a newHarness, mas com o ASRProvider do
// gateway configurável — usado pelos testes que precisam inspecionar o
// texto/confidence devolvido por um provider específico.
func newHarnessWithASR(t *testing.T, asrProvider asr.ASRProvider) *testHarness {
	t.Helper()

	sess := server.NewInMemorySessionStore()
	sess.Put(tokenUserA, server.Principal{UserID: userAID, Email: "a@example.com", DisplayName: "User A"})
	sess.Put(tokenUserB, server.Principal{UserID: userBID, Email: "b@example.com", DisplayName: "User B"})

	fake := &clock.Fake{}
	fake.Set(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))

	srv := server.New(server.Deps{
		Sessions:    sess,
		Clock:       fake,
		ASRProvider: asrProvider,
	})

	return &testHarness{
		t: t, handler: srv.Handler(), sess: sess, fake: fake,
		tokenA: tokenUserA, tokenB: tokenUserB, userA: userAID, userB: userBID,
	}
}

// do executa uma requisição contra o handler em memória (httptest, sem
// rede real) e devolve a resposta gravada.
func (h *testHarness) do(method, path, token string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// doMultipartAudio monta e executa POST /api/v1/asr/transcribe com um
// campo de arquivo "audio" contendo pcm (bytes crus), simulando o envio
// de áudio pelo Windows Agent.
func (h *testHarness) doMultipartAudio(token string, pcm []byte, extraFields map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("audio", "audio.pcm")
	if err != nil {
		h.t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(pcm); err != nil {
		h.t.Fatalf("write pcm: %v", err)
	}
	for k, v := range extraFields {
		if err := mw.WriteField(k, v); err != nil {
			h.t.Fatalf("WriteField: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		h.t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/asr/transcribe", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode body: %v — body: %s", err, rec.Body.String())
	}
	return v
}

// TestHealthAndUpdatesDoNotRequireSession — /health/* e /updates/* são
// as únicas rotas públicas (ver server.go, comentário de Handler).
func TestHealthAndUpdatesDoNotRequireSession(t *testing.T) {
	h := newHarness(t)

	if rec := h.do(http.MethodGet, "/api/v1/health/live", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("health/live: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodGet, "/api/v1/health/ready", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("health/ready: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodGet, "/api/v1/updates/windows/latest?current=0.1.0&arch=x64", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("updates/windows/latest: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
}

// TestProtectedRoutesRequireSession cobre AC-035/NFR-012 pelo lado
// negativo mais básico: sem token nenhum, tudo que não é health/updates
// devolve 401, nunca vaza dado.
func TestProtectedRoutesRequireSession(t *testing.T) {
	h := newHarness(t)

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/me"},
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodGet, "/api/v1/transcriptions"},
		{http.MethodGet, "/api/v1/dictionary"},
		{http.MethodGet, "/api/v1/settings"},
		{http.MethodGet, "/api/v1/sync/pull"},
	}
	for _, rt := range routes {
		rec := h.do(rt.method, rt.path, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sem token: status = %d, esperava 401", rt.method, rt.path, rec.Code)
		}
	}

	rec := h.do(http.MethodGet, "/api/v1/me", "token-completamente-invalido", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /me com token inválido: status = %d, esperava 401", rec.Code)
	}
}

// TestMeReflectsSessionPrincipal — GET /me nunca lê nada do corpo (não
// tem corpo), só do Principal da sessão.
func TestMeReflectsSessionPrincipal(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodGet, "/api/v1/me", h.tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}](t, rec)
	if body.ID != h.userA || body.Email != "a@example.com" {
		t.Fatalf("corpo = %+v, esperava user %s", body, h.userA)
	}
}
