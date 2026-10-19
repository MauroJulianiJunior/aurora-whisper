package localapi_test

import (
	"encoding/json"
	"errors"
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

// As rotas /api/microfone*: o aviso de microfone bloqueado pelo Windows e o
// botão que abre a tela certa. Dublê no lugar do registro e do ShellExecute:
// nenhum teste abre janela nem lê a máquina de quem roda.

type microfoneFalso struct {
	bloqueado bool
	motivo    string
	erroAbrir error
	aberturas int
}

func (m *microfoneFalso) Estado() (bool, string) { return m.bloqueado, m.motivo }
func (m *microfoneFalso) AbrirConfiguracoes() error {
	m.aberturas++
	return m.erroAbrir
}

func apiComMicrofone(t *testing.T, m localapi.Microfone) http.Handler {
	t.Helper()
	pref := prefsDeTeste()
	pref.Microfone = m
	return localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
}

func TestMicrofone_GetDevolveBloqueioEMotivo(t *testing.T) {
	for _, c := range []struct {
		nome      string
		bloqueado bool
		motivo    string
	}{
		{"liberado", false, ""},
		{"conta", true, "conta"},
		{"empresa", true, "empresa"},
		{"não deu para ler", false, "desconhecido"},
	} {
		w := fazer(t, apiComMicrofone(t, &microfoneFalso{bloqueado: c.bloqueado, motivo: c.motivo}), "GET", "/api/microfone", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: GET = %d %s", c.nome, w.Code, w.Body)
		}
		var campos map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &campos); err != nil {
			t.Fatal(err)
		}
		if len(campos) != 2 || campos["bloqueado"] != c.bloqueado || campos["motivo"] != c.motivo {
			t.Errorf("%s: corpo = %v; quero só bloqueado=%v motivo=%q", c.nome, campos, c.bloqueado, c.motivo)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: sem no-store", c.nome)
		}
	}
}

func TestMicrofone_PostAbreAsConfiguracoes(t *testing.T) {
	f := &microfoneFalso{bloqueado: true, motivo: "conta"}
	w := fazer(t, apiComMicrofone(t, f), "POST", "/api/microfone/configuracoes", nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s; quero 204", w.Code, w.Body)
	}
	if f.aberturas != 1 {
		t.Errorf("abriu %d vez(es), quero 1", f.aberturas)
	}
}

func TestMicrofone_PostQueFalhaDevolveErroDoContrato(t *testing.T) {
	f := &microfoneFalso{erroAbrir: errors.New("painel: ShellExecute falhou (código 31)")}
	w := fazer(t, apiComMicrofone(t, f), "POST", "/api/microfone/configuracoes", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("POST = %d; quero 500", w.Code)
	}
	var env struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "INTERNAL" || env.Error.Message != localapi.MsgNaoAbriuConfiguracoes || env.Error.RequestID == "" {
		t.Errorf("erro = %+v", env.Error)
	}
	if strings.Contains(w.Body.String(), "ShellExecute") {
		t.Error("o erro interno foi para a tela")
	}
}

// Abrir uma janela não pode sair de um GET (uma <img> ou um link bastaria):
// o GET na rota de abrir não casa e nada abre.
func TestMicrofone_AbrirNaoRodaSobGet(t *testing.T) {
	f := &microfoneFalso{}
	h := apiComMicrofone(t, f)
	for _, m := range []string{"GET", "HEAD", "PUT", "DELETE"} {
		if w := fazer(t, h, m, "/api/microfone/configuracoes", nil); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d; quero 405", m, w.Code)
		}
	}
	if w := fazer(t, h, "POST", "/api/microfone", nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/microfone = %d; quero 405", w.Code)
	}
	if f.aberturas != 0 {
		t.Errorf("abriu %d vez(es) por um método que não é POST", f.aberturas)
	}
}

func TestMicrofone_SemInterfaceResponde503(t *testing.T) {
	h := apiComMicrofone(t, nil)
	for _, r := range []struct{ metodo, caminho string }{
		{"GET", "/api/microfone"}, {"POST", "/api/microfone/configuracoes"},
	} {
		w := fazer(t, h, r.metodo, r.caminho, nil)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d; quero 503", r.metodo, r.caminho, w.Code)
		}
	}
}

// Atrás do guard, como no Agent: sem token (401) ou com Host forjado (403)
// o Abrir nem roda; com tudo certo, roda uma vez.
func TestMicrofone_AbrirPassaPeloGuard(t *testing.T) {
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
	f := &microfoneFalso{bloqueado: true, motivo: "conta"}
	server := &http.Server{Handler: guard.Protect(port, token)(apiComMicrofone(t, f))}
	go func() { _ = server.Serve(lis) }()
	defer server.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	post := func(host, tok string) int {
		req, err := http.NewRequest("POST", "http://"+addr+"/api/microfone/configuracoes", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(addr, ""); got != http.StatusUnauthorized {
		t.Errorf("sem token = %d; quero 401", got)
	}
	if got := post("evil.example:1234", token); got != http.StatusForbidden {
		t.Errorf("host forjado = %d; quero 403", got)
	}
	if f.aberturas != 0 {
		t.Fatalf("o pedido barrado pelo guard abriu a janela %d vez(es)", f.aberturas)
	}
	if got := post(addr, token); got != http.StatusNoContent {
		t.Errorf("pedido legítimo = %d; quero 204", got)
	}
	if f.aberturas != 1 {
		t.Errorf("abriu %d vez(es), quero 1", f.aberturas)
	}
}
