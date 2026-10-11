package localapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// GET /api/nuvem/mudancas (ADR-021, subfatia S): o contador de mudanças do
// sync, que o painel do PC lê a cada poucos segundos enquanto está à vista. A
// leitura é o aviso de que o painel está aberto.

// contadorFalso é o laço de sync visto pela API local.
type contadorFalso struct {
	mu       sync.Mutex
	valor    uint64
	leituras int
}

func (c *contadorFalso) LerMudancas() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.leituras++
	return c.valor
}

func (c *contadorFalso) sobe() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valor++
}

func (c *contadorFalso) lidas() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.leituras
}

func apiComContador(t *testing.T, c localapi.MudancasDoSync, repo storage.TranscriptionRepository, n localapi.Nuvem) http.Handler {
	t.Helper()
	pref := prefsDeTeste()
	pref.Mudancas = c
	pref.Nuvem = n
	if repo == nil {
		repo = storage.NewInMemoryTranscriptionRepository()
	}
	return localapi.New("u-local", repo, storage.NewInMemorySettingsRepository(), pref).Handler()
}

// A resposta é o número, e cada leitura é contada pelo laço (é o aviso de que o
// painel está aberto); sem cache.
func TestMudancas_DevolveOContadorEAvisaOLaco(t *testing.T) {
	c := &contadorFalso{valor: 7}
	h := apiComContador(t, c, nil, nil)

	w := fazer(t, h, http.MethodGet, "/api/nuvem/mudancas", nil)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"contador":7}` {
		t.Fatalf("GET = %d %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; quero no-store", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q", got)
	}
	if c.lidas() != 1 {
		t.Errorf("o laço viu %d leituras; quero 1", c.lidas())
	}

	c.sobe()
	w = fazer(t, h, http.MethodGet, "/api/nuvem/mudancas", nil)
	if strings.TrimSpace(w.Body.String()) != `{"contador":8}` || c.lidas() != 2 {
		t.Errorf("depois de subir: %s, %d leituras", w.Body, c.lidas())
	}
}

// A resposta não leva texto nenhum: um número e mais nada. Com um histórico
// cheio de texto ditado e a nuvem conectada, o corpo continua sendo só o
// contador: nada do histórico, do e-mail, do nome do PC, da URL, e nenhum campo
// além de "contador".
func TestMudancas_RespostaSemTexto(t *testing.T) {
	repo := storage.NewInMemoryTranscriptionRepository()
	agora := time.Now().UTC()
	const segredo = "extrato-do-cliente-confidencial-4417"
	if err := repo.Put(storage.Transcription{
		ID: "t1", UserID: "u-local", SourcePlatform: "windows", Mode: "smart", ASRProvider: "whisper-local",
		RawText: segredo + " bruto", FinalText: segredo, Status: "done", CreatedAt: agora, UpdatedAt: agora, Version: 1,
	}); err != nil {
		t.Fatal(err)
	}
	f := &nuvemFalsa{estado: nuvem.Estado{Estado: nuvem.Conectado, URL: "https://nuvem.exemplo.test", Email: "voce@teste.local", Nome: "DELL-G15"}}
	h := apiComContador(t, &contadorFalso{valor: 3}, repo, f)

	w := fazer(t, h, http.MethodGet, "/api/nuvem/mudancas", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", w.Code, w.Body)
	}
	for _, proibido := range []string{segredo, "t1", "voce@teste.local", "DELL-G15", "nuvem.exemplo.test", "u-local", "Bearer", "token"} {
		if strings.Contains(w.Body.String(), proibido) {
			t.Errorf("a resposta tem %q: %s", proibido, w.Body)
		}
	}
	var campos map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &campos); err != nil {
		t.Fatal(err)
	}
	n, ok := campos["contador"].(float64)
	if len(campos) != 1 || !ok || n != 3 {
		t.Errorf("os campos são %v; quero só {contador: 3}", campos)
	}
	// A rota não toca no histórico nem na nuvem: leitura de memória.
	if len(f.chamadas) != 0 {
		t.Errorf("a rota chamou a nuvem: %v", f.chamadas)
	}
}

// Só GET: a leitura não recebe nada do painel (o contador não é gravável por
// ele) e o texto de quem dita nunca vai na URL nem no corpo.
func TestMudancas_SoGET(t *testing.T) {
	c := &contadorFalso{}
	h := apiComContador(t, c, nil, nil)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if w := fazer(t, h, m, "/api/nuvem/mudancas", `{"contador":99}`); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d; quero 405", m, w.Code)
		}
	}
	if c.lidas() != 0 || c.valor != 0 {
		t.Errorf("um método que não é GET mexeu no contador: %d leituras, valor %d", c.lidas(), c.valor)
	}
}

// Sem o laço (o Agent sem histórico), a rota diz que não está disponível, com o
// envelope de sempre.
func TestMudancas_SemOLaco(t *testing.T) {
	pref := prefsDeTeste()
	h := localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
	w := fazer(t, h, http.MethodGet, "/api/nuvem/mudancas", nil)
	var e erroDaAPI
	if w.Code != http.StatusServiceUnavailable || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != "UNAVAILABLE" || e.Error.Message == "" {
		t.Errorf("GET = %d %s", w.Code, w.Body)
	}
}
