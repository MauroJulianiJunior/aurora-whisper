package nuvem

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// Este arquivo é a TRAVA DE REDE da suíte do pacote (a mesma ideia do
// internal/ia): nenhuma chamada real à internet, nunca.
//
// Quase todo teste do pacote aponta o Cliente para um httptest.Server em
// loopback, com o HTTP nil, ou seja, com o transporte real. Se um dia o código
// ignorar a base injetada (uma refatoração, um mutante), o pedido SAI da
// máquina rumo a api.openai.com ou api.groq.com, levando a chave e o áudio de
// teste, e fica nos logs do provedor. Por isso, antes de qualquer teste,
// TestMain:
//
//  1. aponta HTTP_PROXY e HTTPS_PROXY para uma porta morta em loopback
//     (loopback nunca passa por proxy, então o httptest continua
//     funcionando; qualquer outro destino não vai a lugar nenhum);
//  2. troca o transporte do pacote e o http.DefaultTransport por um que
//     RECUSA todo destino que não seja loopback, conta a tentativa e faz a
//     suíte inteira falhar no fim.

const proxyMorto = "http://127.0.0.1:1"

var variaveisDeProxy = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"}

// registroDeRede guarda os destinos que a trava recusou.
type registroDeRede struct {
	mu    sync.Mutex
	hosts []string
}

func (r *registroDeRede) add(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hosts = append(r.hosts, host)
}

func (r *registroDeRede) lista() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hosts...)
}

// recusados são os pedidos que a suíte inteira tentou mandar para fora.
var recusados = &registroDeRede{}

// guardaDeRede deixa passar só o que vai para a própria máquina.
type guardaDeRede struct {
	base http.RoundTripper
	reg  *registroDeRede
}

func (g guardaDeRede) RoundTrip(req *http.Request) (*http.Response, error) {
	if !destinoLocal(req.URL.Hostname()) {
		if req.Body != nil {
			req.Body.Close()
		}
		g.reg.add(req.URL.Host)
		return nil, errors.New("rede: o teste tentou sair da máquina")
	}
	return g.base.RoundTrip(req)
}

// destinoLocal diz se o host é a própria máquina: "localhost" ou um IP de
// loopback. Nada de prefixo ("localhost.exemplo.com" não é local).
func destinoLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func TestMain(m *testing.M) {
	os.Exit(rodarComTravaDeRede(m))
}

func rodarComTravaDeRede(m *testing.M) int {
	for _, k := range variaveisDeProxy {
		os.Setenv(k, proxyMorto)
	}
	// Um NO_PROXY do ambiente poderia liberar um provedor real.
	os.Setenv("NO_PROXY", "")
	os.Setenv("no_proxy", "")

	http.DefaultTransport = guardaDeRede{http.DefaultTransport, recusados}
	transporteHTTP1 = guardaDeRede{transporteHTTP1, recusados}

	code := m.Run()
	if h := recusados.lista(); len(h) > 0 {
		fmt.Fprintf(os.Stderr, "FALHA: %d pedido(s) da suíte tentaram sair da máquina (destinos: %v)\n", len(h), h)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// roundTripFunc é um http.RoundTripper de função, para os testes que olham o
// pedido sem servidor nenhum (nem loopback).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTravaDeRede_EstaInstalada(t *testing.T) {
	if _, ok := http.DefaultTransport.(guardaDeRede); !ok {
		t.Errorf("http.DefaultTransport é %T, quero a guarda de rede do TestMain", http.DefaultTransport)
	}
	if _, ok := transporteHTTP1.(guardaDeRede); !ok {
		t.Errorf("transporteHTTP1 é %T, quero a guarda de rede do TestMain", transporteHTTP1)
	}
	for _, k := range variaveisDeProxy {
		if got := os.Getenv(k); got != proxyMorto {
			t.Errorf("%s = %q, quero o proxy morto %q", k, got, proxyMorto)
		}
	}
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		if got := os.Getenv(k); got != "" {
			t.Errorf("%s = %q, quero vazio: uma exceção de proxy do ambiente liberaria um provedor real", k, got)
		}
	}
}

func TestGuardaDeRede_SoDeixaPassarLoopback(t *testing.T) {
	var passou []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		passou = append(passou, r.URL.Host)
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	reg := &registroDeRede{}
	g := guardaDeRede{base, reg}

	for _, alvo := range []string{
		"https://api.openai.com/v1/audio/transcriptions",
		"https://api.groq.com/openai/v1/audio/transcriptions",
		"http://8.8.8.8/",
		"http://localhost.exemplo.com/",
		"http://127.0.0.1.exemplo.com/",
		"http://10.0.0.1:8080/",
	} {
		req, _ := http.NewRequest(http.MethodPost, alvo, strings.NewReader("corpo"))
		if resp, err := g.RoundTrip(req); err == nil {
			resp.Body.Close()
			t.Errorf("%s: a guarda deixou passar", alvo)
		} else if strings.Contains(err.Error(), "api.") {
			t.Errorf("%s: o erro traz o destino inteiro: %v", alvo, err)
		}
	}
	if len(passou) != 0 {
		t.Errorf("a guarda entregou %v ao transporte de verdade", passou)
	}
	if got := len(reg.lista()); got != 6 {
		t.Errorf("a guarda registrou %d destinos, quero 6", got)
	}

	for _, alvo := range []string{"http://127.0.0.1:8080/", "http://[::1]:80/", "http://localhost:9/"} {
		req, _ := http.NewRequest(http.MethodGet, alvo, nil)
		resp, err := g.RoundTrip(req)
		if err != nil {
			t.Errorf("%s: a guarda barrou a própria máquina: %v", alvo, err)
			continue
		}
		resp.Body.Close()
	}
	if len(passou) != 3 {
		t.Errorf("passaram %d pedidos para a própria máquina, quero 3", len(passou))
	}
}
