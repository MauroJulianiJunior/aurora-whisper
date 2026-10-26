package localapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// inicioFalso faz o papel do registro do Windows.
type inicioFalso struct {
	mu           sync.Mutex
	ligado       bool
	indisponivel string
	falha        error
	pedidos      []bool
}

func (f *inicioFalso) Estado() (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ligado, f.indisponivel
}

func (f *inicioFalso) Definir(ligar bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pedidos = append(f.pedidos, ligar)
	if f.falha != nil {
		return f.falha
	}
	f.ligado = ligar
	return nil
}

// modelosFalsos faz o papel do provedor de IA.
type modelosFalsos struct {
	mu         sync.Mutex
	modelos    []string
	err        error
	chaveVista string
	chamadas   int
}

func (m *modelosFalsos) ListarModelos(_ context.Context, _, chave string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chamadas++
	m.chaveVista = chave
	return m.modelos, m.err
}

func padraoDeTeste() storage.Settings {
	return storage.Settings{
		Hotkey:               "ctrl+win",
		HoldThresholdMs:      700,
		BeepEnabled:          true,
		TextMode:             "smart",
		ASRPolicy:            "local_only",
		ProtectedFieldPolicy: "block",
		NoteTriggerEnabled:   true,
	}
}

func prefsDeTeste() localapi.Preferencias {
	return localapi.Preferencias{
		Padrao:  padraoDeTeste(),
		Chaves:  chaves.NovaMemoria(),
		Inicio:  &inicioFalso{},
		Modelos: &modelosFalsos{},
	}
}

// fazer manda um pedido direto ao handler (sem a cadeia do guard, que
// tem teste próprio) e devolve a resposta gravada.
func fazer(t *testing.T, h http.Handler, metodo, url string, corpo any) *httptest.ResponseRecorder {
	t.Helper()
	var leitor *bytes.Reader
	switch c := corpo.(type) {
	case nil:
		leitor = bytes.NewReader(nil)
	case string:
		leitor = bytes.NewReader([]byte(c))
	default:
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		leitor = bytes.NewReader(b)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(metodo, url, leitor))
	return w
}
