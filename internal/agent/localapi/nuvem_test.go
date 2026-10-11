package localapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// nuvemFalsa é a nuvem.Servico vista pela API local: devolve o que o teste
// mandar.
type nuvemFalsa struct {
	estado      nuvem.Estado
	err         error
	urlRecebida string
	chamadas    []string
}

func (n *nuvemFalsa) Estado(context.Context) (nuvem.Estado, error) {
	n.chamadas = append(n.chamadas, "estado")
	return n.estado, n.err
}

func (n *nuvemFalsa) Conectar(_ context.Context, u string) (nuvem.Estado, error) {
	n.chamadas = append(n.chamadas, "conectar")
	n.urlRecebida = u
	return n.estado, n.err
}

func (n *nuvemFalsa) Conferir(context.Context) (nuvem.Estado, error) {
	n.chamadas = append(n.chamadas, "conferir")
	return n.estado, n.err
}

func (n *nuvemFalsa) Desconectar(context.Context) (nuvem.Estado, error) {
	n.chamadas = append(n.chamadas, "desconectar")
	return n.estado, n.err
}

func (n *nuvemFalsa) EnviarHistorico(context.Context) (nuvem.Estado, error) {
	n.chamadas = append(n.chamadas, "enviar")
	return n.estado, n.err
}

func (n *nuvemFalsa) Esquecer(context.Context) (nuvem.Estado, error) {
	n.chamadas = append(n.chamadas, "esquecer")
	return n.estado, n.err
}

func apiComNuvem(t *testing.T, n localapi.Nuvem) http.Handler {
	t.Helper()
	pref := prefsDeTeste()
	pref.Nuvem = n
	return localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
}

type erroDaAPI struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// As quatro rotas traduzem o Servico: o estado com 200, a recusa com o
// status e a frase dela, o navegador que não abriu com 200 e o aviso.
func TestNuvem_Rotas(t *testing.T) {
	conectado := nuvem.Estado{Estado: nuvem.Conectado, URL: "https://nuvem.exemplo.test", Email: "voce@teste.local", Nome: "DELL-G15"}
	for _, rota := range []struct{ metodo, caminho, chamada string }{
		{"GET", "/api/nuvem", "estado"},
		{"POST", "/api/nuvem/conferir", "conferir"},
		{"POST", "/api/nuvem/desconectar", "desconectar"},
		{"POST", "/api/nuvem/enviar", "enviar"},
		{"POST", "/api/nuvem/esquecer", "esquecer"},
	} {
		f := &nuvemFalsa{estado: conectado}
		w := fazer(t, apiComNuvem(t, f), rota.metodo, rota.caminho, nil)
		var got nuvem.Estado
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != conectado {
			t.Errorf("%s %s = %d %s", rota.metodo, rota.caminho, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: sem no-store", rota.caminho)
		}
		if len(f.chamadas) != 1 || f.chamadas[0] != rota.chamada {
			t.Errorf("%s chamou %v", rota.caminho, f.chamadas)
		}
	}

	// O Conectar leva a URL do corpo.
	f := &nuvemFalsa{estado: nuvem.Estado{Estado: nuvem.Conectando, Conferencia: "K7QM"}}
	w := fazer(t, apiComNuvem(t, f), "POST", "/api/nuvem/conectar", map[string]string{"url": "http://localhost:8080"})
	if w.Code != 200 || f.urlRecebida != "http://localhost:8080" || !strings.Contains(w.Body.String(), `"conferencia":"K7QM"`) {
		t.Errorf("conectar = %d %s (url %q)", w.Code, w.Body, f.urlRecebida)
	}

	// O navegador que não abriu: 200, com o estado e o aviso.
	f = &nuvemFalsa{estado: nuvem.Estado{Estado: nuvem.Conectando, Aviso: nuvem.AvisoNaoAbriu}, err: fmt.Errorf("%w: x", nuvem.ErrNaoAbriu)}
	w = fazer(t, apiComNuvem(t, f), "POST", "/api/nuvem/conectar", map[string]string{"url": "http://localhost:8080"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"aviso":"nao_abriu"`) {
		t.Errorf("não abriu = %d %s", w.Code, w.Body)
	}

	// A recusa: o status e a frase do Servico.
	for _, caso := range []struct {
		status int
		frase  string
		code   string
	}{
		{400, nuvem.MsgUseHTTPS, "BAD_REQUEST"},
		{409, nuvem.MsgOutraConta, "CONFLICT"},
		{502, nuvem.MsgSemNuvem, "UNAVAILABLE"},
		{503, nuvem.MsgPainelNaoSubiu, "UNAVAILABLE"},
		{500, nuvem.MsgCofre, "INTERNAL"},
	} {
		f := &nuvemFalsa{err: &nuvem.Erro{Status: caso.status, Mensagem: caso.frase}}
		w := fazer(t, apiComNuvem(t, f), "POST", "/api/nuvem/conectar", map[string]string{"url": "x"})
		var e erroDaAPI
		if w.Code != caso.status || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Message != caso.frase || e.Error.Code != caso.code {
			t.Errorf("recusa %d = %d %s", caso.status, w.Code, w.Body)
		}
	}

	// Um erro que não é do Servico vira uma frase sem detalhe.
	f = &nuvemFalsa{err: errors.New("detalhe de dentro https://segredo.exemplo")}
	w = fazer(t, apiComNuvem(t, f), "GET", "/api/nuvem", nil)
	if w.Code != 500 || strings.Contains(w.Body.String(), "segredo") {
		t.Errorf("erro inesperado = %d %s", w.Code, w.Body)
	}

	// Corpo que não é JSON: 400, e o Servico nem é chamado.
	f = &nuvemFalsa{}
	w = fazer(t, apiComNuvem(t, f), "POST", "/api/nuvem/conectar", "{url")
	if w.Code != 400 || len(f.chamadas) != 0 {
		t.Errorf("corpo ruim = %d (chamadas %v)", w.Code, f.chamadas)
	}
	w = fazer(t, apiComNuvem(t, f), "POST", "/api/nuvem/conectar", `{"url":"`+strings.Repeat("a", 2000)+`"}`)
	if w.Code != 400 || len(f.chamadas) != 0 {
		t.Errorf("corpo grande = %d (chamadas %v)", w.Code, f.chamadas)
	}
}

// O envio e o esquecer (F7c): a recusa vem com o status e a frase do
// Servico (o 409 de quem está conectado, a cópia que falha com o motivo, o
// 507 do disco cheio), e o GET leva o resumo do histórico.
func TestNuvem_EnviarEEsquecer(t *testing.T) {
	for _, caso := range []struct {
		caminho string
		status  int
		frase   string
		code    string
	}{
		{"/api/nuvem/enviar", 409, nuvem.MsgConecteAntes, "CONFLICT"},
		{"/api/nuvem/enviar", 409, nuvem.MsgCopiaFalhou + " " + nuvem.MsgCopiasDemais, "CONFLICT"},
		{"/api/nuvem/enviar", 507, nuvem.MsgCopiaFalhou + " " + nuvem.MsgDiscoCheio, "INTERNAL"},
		{"/api/nuvem/enviar", 500, nuvem.MsgCopiaFalhou, "INTERNAL"},
		{"/api/nuvem/esquecer", 409, nuvem.MsgDesconecteAntes, "CONFLICT"},
		{"/api/nuvem/esquecer", 409, nuvem.MsgCancelePedido, "CONFLICT"},
	} {
		f := &nuvemFalsa{err: &nuvem.Erro{Status: caso.status, Mensagem: caso.frase}}
		w := fazer(t, apiComNuvem(t, f), "POST", caso.caminho, nil)
		var e erroDaAPI
		if w.Code != caso.status || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Message != caso.frase || e.Error.Code != caso.code {
			t.Errorf("%s %d = %d %s", caso.caminho, caso.status, w.Code, w.Body)
		}
	}
	comResumo := nuvem.Estado{Estado: nuvem.Conectado, URL: "http://localhost:8080", Historico: &nuvem.ResumoDoHistorico{
		Previa: 0, Aceito: true, Envio: &nuvem.Progresso{Feitos: 120, Total: 800}, Pendentes: 680, Mudancas: 3, ConferidoEm: "2026-10-06T12:30:00Z"}}
	w := fazer(t, apiComNuvem(t, &nuvemFalsa{estado: comResumo}), "GET", "/api/nuvem", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"envio":{"feitos":120,"total":800}`) ||
		!strings.Contains(w.Body.String(), `"pendentes":680,"mudancas":3`) ||
		!strings.Contains(w.Body.String(), `"conferido_em":"2026-10-06T12:30:00Z"`) || !strings.Contains(w.Body.String(), `"previa":0`) {
		t.Errorf("o resumo no GET: %d %s", w.Code, w.Body)
	}
}

// Um Agent sem a nuvem montada: 503 em todas.
func TestNuvem_SemServico(t *testing.T) {
	h := apiComNuvem(t, nil)
	for _, r := range [][2]string{{"GET", "/api/nuvem"}, {"POST", "/api/nuvem/conectar"}, {"POST", "/api/nuvem/conferir"},
		{"POST", "/api/nuvem/desconectar"}, {"POST", "/api/nuvem/enviar"}, {"POST", "/api/nuvem/esquecer"}} {
		if w := fazer(t, h, r[0], r[1], map[string]string{"url": "x"}); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d", r[0], r[1], w.Code)
		}
	}
}

// A chave do provedor de IA não alcança o token da nuvem: "nuvem" e
// "nuvem-portatil" não são provedores, e as rotas /api/ia/chave os recusam
// antes do Cofre (que tem o mesmo prefixo de alvo, AuroraWhisper/).
func TestNuvem_ChaveDeIANaoAlcancaOToken(t *testing.T) {
	cofre := chaves.NovaMemoria()
	if err := cofre.Gravar("nuvem", "token-da-nuvem-que-nao-sai"); err != nil {
		t.Fatal(err)
	}
	pref := prefsDeTeste()
	pref.Chaves = cofre
	h := localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
	for _, p := range []string{nuvem.NomeNoCofreInstalado, nuvem.NomeNoCofrePortatil} {
		for _, w := range []*httptest.ResponseRecorder{
			fazer(t, h, "GET", "/api/ia/chave?provedor="+p, nil),
			fazer(t, h, "PUT", "/api/ia/chave", map[string]string{"provedor": p, "chave": "outro"}),
			fazer(t, h, "DELETE", "/api/ia/chave?provedor="+p, nil),
			fazer(t, h, "POST", "/api/ia/testar", map[string]string{"provedor": p}),
		} {
			if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "token-da") {
				t.Errorf("provedor %s: %d %s", p, w.Code, w.Body)
			}
		}
	}
	if got, err := cofre.Ler("nuvem"); err != nil || got != "token-da-nuvem-que-nao-sai" {
		t.Errorf("o token mudou: %q, %v", got, err)
	}
}

// ─── O token nunca sai pela API local ───

func segredoDeTeste() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// instalacaoDeTeste é a tabela instalacao num mapa.
type instalacaoDeTeste struct {
	mu sync.Mutex
	m  map[string]string
}

func (i *instalacaoDeTeste) Ler(_ context.Context, nome string) (string, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	v, ok := i.m[nome]
	return v, ok, nil
}

func (i *instalacaoDeTeste) Gravar(_ context.Context, v map[string]string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for k, x := range v {
		i.m[k] = x
	}
	return nil
}

func (i *instalacaoDeTeste) Apagar(_ context.Context, nomes ...string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, k := range nomes {
		delete(i.m, k)
	}
	return nil
}

// TestNuvem_TokenNuncaSaiPelaAPI passa o Servico de verdade pelas rotas da
// API local (conectar, o retorno, o estado, conferir e desconectar), com
// uma nuvem falsa que pareia de verdade, e procura o token em toda resposta
// e no log da API.
func TestNuvem_TokenNuncaSaiPelaAPI(t *testing.T) {
	var (
		mu      sync.Mutex
		desafio string
		code    string
		token   string
	)
	falsa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/device-auth/start":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			desafio, _ = b["code_challenge"].(string)
			io.WriteString(w, `{"pedido":"0192f0a1-7c3e-7d4a-9b2f-1a2b3c4d5e6f","conferencia":"K7QM","expires_in":600}`)
		case "/api/v1/device-auth/exchange":
			var b struct{ Code, Code_verifier string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			h := sha256.Sum256([]byte(b.Code_verifier))
			if b.Code != code || base64.RawURLEncoding.EncodeToString(h[:]) != desafio {
				w.WriteHeader(400)
				return
			}
			token = segredoDeTeste()
			fmt.Fprintf(w, `{"device_id":"0192f0a1-0000-7000-8000-0000000a9a1e","token":%q,"conta":{"id":"0192f0a1-0000-7000-8000-00000000c0a1","email":"voce@teste.local"}}`, token)
		case "/api/v1/me":
			// O token volta "ecoado" num cabeçalho e no corpo, como um
			// servidor descuidado: nada disso pode chegar à API local.
			w.Header().Set("X-Eco", r.Header.Get("Authorization"))
			fmt.Fprintf(w, `{"eco":%q}`, r.Header.Get("Authorization"))
		default:
			w.Header().Set("X-Eco", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer falsa.Close()

	var logAPI bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logAPI, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cliente := nuvem.NovoCliente()
	cliente.Transport = &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, falsa.Listener.Addr().String())
	}}
	cofre := chaves.NovaMemoria()
	s := nuvem.Novo(nuvem.Config{
		Cofre: cofre, NomeNoCofre: nuvem.NomeNoCofrePortatil, Instalacao: &instalacaoDeTeste{m: map[string]string{}},
		Abrir: func(string) error { return nil }, HTTP: cliente, Politica: nuvem.Politica{Dev: true, Portatil: true},
		NomeDoPC: "PC", NovoID: storage.NewID, Log: logger,
	})
	s.UsarPorta(51234)
	pref := prefsDeTeste()
	pref.Nuvem = s
	pref.Chaves = cofre
	pref.Log = logger
	h := localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()

	var saidas []*httptest.ResponseRecorder
	saidas = append(saidas, fazer(t, h, "POST", "/api/nuvem/conectar", map[string]string{"url": "http://localhost:8080"}))
	mu.Lock()
	code = segredoDeTeste()
	mu.Unlock()
	ret := httptest.NewRecorder()
	s.Retorno().ServeHTTP(ret, httptest.NewRequest("GET", "/nuvem/retorno?code="+code+"&state=0192f0a1-7c3e-7d4a-9b2f-1a2b3c4d5e6f", nil))
	if ret.Code != 200 {
		t.Fatalf("retorno = %d", ret.Code)
	}
	saidas = append(saidas, ret,
		fazer(t, h, "GET", "/api/nuvem", nil),
		fazer(t, h, "POST", "/api/nuvem/conferir", nil),
		fazer(t, h, "GET", "/api/ia/chave?provedor=nuvem-portatil", nil),
		fazer(t, h, "GET", "/api/settings", nil),
		fazer(t, h, "POST", "/api/nuvem/desconectar", nil),
	)
	mu.Lock()
	tok := token
	mu.Unlock()
	if tok == "" {
		t.Fatal("não pareou: o teste não mede nada")
	}
	if !strings.Contains(saidas[2].Body.String(), `"estado":"conectado"`) {
		t.Fatalf("estado depois do retorno = %s", saidas[2].Body)
	}
	for i, w := range saidas {
		if strings.Contains(w.Body.String(), tok) || strings.Contains(fmt.Sprint(w.Header()), tok) {
			t.Errorf("resposta %d tem o token", i)
		}
	}
	if strings.Contains(logAPI.String(), tok) || strings.Contains(logAPI.String(), code) {
		t.Errorf("o log tem o token ou o code:\n%s", logAPI.String())
	}
}
