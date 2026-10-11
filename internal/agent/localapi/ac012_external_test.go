package localapi_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/guard"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// AC-012 — "Dashboard local não aceita conexão externa" — ponta a
// ponta: sobe a API REAL (localapi.Server montado sobre guard.Protect,
// exatamente como no start do Agent), sem repetir os testes unitários
// do pacote guard. O que este teste prova é que a montagem — rota
// esquecida de middleware é o tipo de bug que só ele pega — está
// correta.
func TestAC012_LocalAPIRejectsExternalAccess(t *testing.T) {
	lis, err := guard.Listen(0)
	if err != nil {
		t.Fatalf("guard.Listen: %v", err)
	}
	defer lis.Close()

	token, err := guard.NewToken()
	if err != nil {
		t.Fatalf("guard.NewToken: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port

	repo := storage.NewInMemoryTranscriptionRepository()
	settings := storage.NewInMemorySettingsRepository()
	seedNow := time.Now().UTC()
	repo.Put(storage.Transcription{
		ID: "t1", UserID: testUserID, SourcePlatform: "windows",
		RawText: "x", FinalText: "x", Mode: "smart", ASRProvider: "local",
		Status: "completed", CreatedAt: seedNow, UpdatedAt: seedNow, Version: 1,
	})

	srv := localapi.New(testUserID, repo, settings, prefsDeTeste())
	handler := guard.Protect(port, token)(srv.Handler())

	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(lis) }()
	defer server.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}

	newRequest := func(t *testing.T) *http.Request {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/history", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		return req
	}

	t.Run("sem token -> 401", func(t *testing.T) {
		req := newRequest(t)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, esperava 401", resp.StatusCode)
		}
	})

	t.Run("host forjado (dns rebinding) -> 403", func(t *testing.T) {
		req := newRequest(t)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Host = "evil.example:1234" // simula domínio atacante apontando p/ 127.0.0.1
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, esperava 403", resp.StatusCode)
		}
	})

	t.Run("origin cross-origin -> 403", func(t *testing.T) {
		req := newRequest(t)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", "https://evil.example")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, esperava 403", resp.StatusCode)
		}
	})

	t.Run("token errado -> 401", func(t *testing.T) {
		req := newRequest(t)
		req.Header.Set("Authorization", "Bearer token-errado-completamente-invalido-000000000000000000000")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, esperava 401", resp.StatusCode)
		}
	})

	t.Run("chamada legítima (host/origin/token corretos) -> 200", func(t *testing.T) {
		req := newRequest(t)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", "http://127.0.0.1:"+fmt.Sprint(port))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, esperava 200", resp.StatusCode)
		}
	})

	// As rotas de configurações e da chave de API passam pela MESMA
	// cadeia do histórico: sem token, 401; com Host forjado, 403 — antes
	// de o handler tocar no cofre. Uma rota nova montada fora do guard é
	// exatamente o bug que só um teste de ponta a ponta pega.
	for _, rota := range []struct{ metodo, caminho, corpo string }{
		{http.MethodGet, "/api/settings", ""},
		{http.MethodPut, "/api/settings", `{"base_version":0}`},
		{http.MethodGet, "/api/ia/chave?provedor=openai", ""},
		{http.MethodPut, "/api/ia/chave", `{"provedor":"openai","chave":"sk-teste-que-nao-pode-entrar-000"}`},
		{http.MethodDelete, "/api/ia/chave?provedor=openai", ""},
		{http.MethodPost, "/api/ia/testar", `{"provedor":"openai"}`},
		{http.MethodGet, "/api/aprendizado", ""},
		{http.MethodPost, "/api/aprendizado", `{"id":"t1","de":"sf","para":"ECF"}`},
		{http.MethodPost, "/api/aprendizado/desfazer", `{"variante":"sf"}`},
		// Os 6 meses no PC (F10d e F10e): a rota que apaga ditado e a que apaga
		// cópia passam pela mesma cadeia, e sem token nada chega ao handler.
		{http.MethodGet, "/api/retencao", ""},
		{http.MethodPost, "/api/retencao/ligar", `{"previa":1}`},
		{http.MethodPost, "/api/retencao/desligar", ""},
		{http.MethodPost, "/api/retencao/copias/apagar", ""},
		// O contador do sync (ADR-021, S): a leitura é o aviso de que o painel
		// está aberto, e só quem tem o token a faz.
		{http.MethodGet, "/api/nuvem/mudancas", ""},
	} {
		t.Run(rota.metodo+" "+rota.caminho+" sem token -> 401, host forjado -> 403", func(t *testing.T) {
			for _, c := range []struct {
				host   string
				token  string
				status int
			}{
				{addr, "", http.StatusUnauthorized},
				{"evil.example:1234", token, http.StatusForbidden},
			} {
				req, err := http.NewRequest(rota.metodo, "http://"+addr+rota.caminho, strings.NewReader(rota.corpo))
				if err != nil {
					t.Fatalf("NewRequest: %v", err)
				}
				req.Host = c.host
				if c.token != "" {
					req.Header.Set("Authorization", "Bearer "+c.token)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("Do: %v", err)
				}
				resp.Body.Close()
				if resp.StatusCode != c.status {
					t.Fatalf("host %s: status = %d, esperava %d", c.host, resp.StatusCode, c.status)
				}
			}
		})
	}
	if _, err := settings.Get(context.Background()); err == nil {
		t.Fatal("um PUT /api/settings barrado pelo guard gravou as configurações")
	}
}
