package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
)

// TRAVA DE REDE da suíte do comando, no mesmo espírito da de internal/ia.
//
// O polishbench existe para chamar as APIs pagas da OpenAI e da Anthropic. Um
// teste que, por refatoração ou mutante, ignorasse o endereço injetado, mandaria
// a chave de teste e o ditado de teste a api.openai.com e api.anthropic.com. Por
// isso, antes de qualquer teste, TestMain:
//
//  1. aponta HTTP_PROXY e HTTPS_PROXY para uma porta morta em loopback
//     (loopback nunca passa por proxy, então o httptest continua funcionando;
//     o transporte de produção, que respeita o ambiente, não sai de lugar
//     nenhum);
//  2. troca o http.DefaultTransport por um que RECUSA todo destino fora de
//     loopback, anota a tentativa e faz o pacote inteiro falhar no fim.
//
// Os testes que rodam o comando inteiro injetam um transporte de teste que
// passa pela mesma guarda (transporteDeTeste), então um pedido para fora
// também é anotado.

const proxyMorto = "http://127.0.0.1:1"

var variaveisDeProxy = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"}

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
	os.Setenv("NO_PROXY", "")
	os.Setenv("no_proxy", "")
	http.DefaultTransport = guardaDeRede{http.DefaultTransport, recusados}

	code := m.Run()
	if h := recusados.lista(); len(h) > 0 {
		fmt.Fprintf(os.Stderr, "FALHA: %d pedido(s) da suíte tentaram sair da máquina (destinos: %v)\n", len(h), h)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// transporteDeTeste é o transporte que os testes do comando injetam: fala com
// o próprio computador (os servidores falsos), sem proxy, e passa pela guarda.
func transporteDeTeste() http.RoundTripper {
	return guardaDeRede{&http.Transport{Proxy: nil}, recusados}
}

func TestTravaDeRede_EstaInstalada(t *testing.T) {
	if _, ok := http.DefaultTransport.(guardaDeRede); !ok {
		t.Errorf("http.DefaultTransport é %T, quero a guarda de rede do TestMain", http.DefaultTransport)
	}
	for _, k := range variaveisDeProxy {
		if got := os.Getenv(k); got != proxyMorto {
			t.Errorf("%s = %q, quero o proxy morto %q", k, got, proxyMorto)
		}
	}
}

func TestGuardaDeRede_SoDeixaPassarLoopback(t *testing.T) {
	reg := &registroDeRede{}
	passou := 0
	g := guardaDeRede{roundTripFunc(func(*http.Request) (*http.Response, error) {
		passou++
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}), reg}
	for _, alvo := range []string{
		"https://api.openai.com/v1/responses", "https://api.anthropic.com/v1/messages",
		"http://8.8.8.8/", "http://localhost.exemplo.com/", "http://10.0.0.1:8080/",
	} {
		req, _ := http.NewRequest(http.MethodPost, alvo, nil)
		if resp, err := g.RoundTrip(req); err == nil {
			resp.Body.Close()
			t.Errorf("%s: a guarda deixou passar", alvo)
		}
	}
	if passou != 0 || len(reg.lista()) != 5 {
		t.Errorf("passaram %d, registrados %d; quero 0 e 5", passou, len(reg.lista()))
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
	if passou != 3 || len(reg.lista()) != 5 {
		t.Errorf("passaram %d, registrados %d; quero 3 e 5", passou, len(reg.lista()))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
