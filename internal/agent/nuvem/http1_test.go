package nuvem_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
)

// O cliente HTTP da nuvem fala só HTTP/1.1 (auditoria de rede do PC,
// 13-out-2026). Com HTTP/2, o net/http escreve cada cabeçalho (o Bearer do
// token do aparelho) e o começo de cada corpo (a resposta da troca, que traz o
// token) no log padrão do processo quando a variável de ambiente GODEBUG traz
// http2debug, e o Agent manda o log padrão para o agent.log. É a mesma trava da
// chave de IA (internal/ia, TestHTTP2Debug_ChaveNaoVaiParaOLog).

// tokenDoFilho é o token que a nuvem de teste entrega na troca: 43 caracteres
// de base64url, como os de verdade, e fácil de achar num log.
const tokenDoFilho = "TOKENdoPCnaNuvem-0123456789abcdefghijklmnop"

// protocolosDaNuvem anota o protocolo (1 ou 2) de cada pedido que chegou.
type protocolosDaNuvem struct {
	mu sync.Mutex
	v  []int
}

func (p *protocolosDaNuvem) add(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.v = append(p.v, n)
}

func (p *protocolosDaNuvem) tira() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.v
	p.v = nil
	return v
}

// novaNuvemHTTP2 é a nuvemFalsa (o pareamento, o /me e o DELETE) em TLS, que
// fala HTTP/2 se o cliente pedir, com o token da troca fixo.
func novaNuvemHTTP2(t *testing.T, protos *protocolosDaNuvem) *nuvemFalsa {
	t.Helper()
	n := &nuvemFalsa{t: t, expiraEm: 600, authURL: "https://outro-lugar.exemplo/device/authorize?pedido=" + pedidoFalso,
		contaDaVez: contaFalsa, trocaToken: tokenDoFilho}
	n.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protos.add(r.ProtoMajor)
		n.servir(w, r)
	}))
	n.srv.EnableHTTP2 = true
	n.srv.StartTLS()
	t.Cleanup(n.srv.Close)
	return n
}

// clienteDeProducaoPara é o nuvem.NovoCliente com o MESMO transporte, só
// confiando no certificado da nuvem de teste e discando para ela qualquer que
// seja o host (o PC "pensa" que fala com example.com, que o certificado do
// httptest cobre). Sem um transporte próprio, o cliente usaria o
// http.DefaultTransport, e o teste usa uma cópia dele (que fala HTTP/2, como ele).
func clienteDeProducaoPara(t *testing.T, n *nuvemFalsa) *http.Client {
	t.Helper()
	c := nuvem.NovoCliente()
	var tr *http.Transport
	switch v := c.Transport.(type) {
	case nil:
		tr = http.DefaultTransport.(*http.Transport).Clone()
		c.Transport = tr
	case *http.Transport:
		tr = v
	default:
		t.Fatalf("o transporte da nuvem é %T; quero um *http.Transport", c.Transport)
	}
	pool := x509.NewCertPool()
	pool.AddCert(n.srv.Certificate())
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	addr := n.srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	return c
}

// usarOTokenDoAparelho faz o caminho do token por inteiro, em https: conectar,
// aprovar e trocar (a resposta traz o token), conferir (GET /me com o Bearer)
// e desconectar (DELETE com o Bearer).
func usarOTokenDoAparelho(t *testing.T, n *nuvemFalsa) {
	t.Helper()
	c := novoCenario(t, comPolitica(nuvem.Politica{}), func(cfg *nuvem.Config) { cfg.HTTP = clienteDeProducaoPara(t, n) })
	c.nuvem = n
	_, _, mesAntes := n.contar()
	c.conectar("https://example.com")
	if rr := c.retorno(n.aprovar(), pedidoFalso); rr.Code != http.StatusOK {
		t.Fatalf("retorno = %d %q", rr.Code, rr.Body.String())
	}
	e, err := c.s.Conferir(context.Background())
	if err != nil || e.Estado != nuvem.Conectado || e.Aviso != "" {
		t.Fatalf("Conferir = %+v, %v; quero conectado, sem aviso", e, err)
	}
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatalf("Desconectar: %v", err)
	}
	if _, _, mes := n.contar(); mes-mesAntes != 1 {
		t.Errorf("o /me foi chamado %d vezes, quero 1", mes-mesAntes)
	}
}

func TestNovoCliente_TransporteSoHTTP1(t *testing.T) {
	c := nuvem.NovoCliente()
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("NovoCliente usa o transporte %T; nil é o http.DefaultTransport, que fala HTTP/2", c.Transport)
	}
	if tr.Protocols == nil || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() {
		t.Errorf("Protocols = %v, quero só HTTP/1.1", tr.Protocols)
	}
	if tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 {
		t.Error("o transporte tem caminho para o HTTP/2")
	}
	if tr.Proxy == nil || tr.TLSHandshakeTimeout <= 0 || tr.DialContext == nil {
		t.Error("o transporte não tem o proxy do ambiente e os prazos do transporte padrão")
	}
	// O que já valia continua: o prazo e o redirecionamento não seguido.
	if c.Timeout != nuvem.PrazoDaRede || c.CheckRedirect == nil {
		t.Errorf("NovoCliente perdeu o prazo (%v) ou a recusa de redirecionamento", c.Timeout)
	}
}

// TestNovoCliente_NaoFalaHTTP2 vai à rede de verdade (loopback) com uma nuvem
// que ACEITA HTTP/2: o pareamento, a conferência e o desconectar do Servico
// chegam todos em HTTP/1.1.
func TestNovoCliente_NaoFalaHTTP2(t *testing.T) {
	var protos protocolosDaNuvem
	n := novaNuvemHTTP2(t, &protos)

	// Controle: um cliente comum negocia HTTP/2 com esta nuvem. Sem isto,
	// "chegou HTTP/1.1" poderia ser só a nuvem que não oferece HTTP/2.
	resp, err := n.srv.Client().Get(n.srv.URL + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := protos.tira(); len(v) != 1 || v[0] != 2 {
		t.Fatalf("controle: o cliente comum falou HTTP/%v, quero HTTP/2", v)
	}

	usarOTokenDoAparelho(t, n)
	v := protos.tira()
	if len(v) < 4 {
		t.Fatalf("a nuvem recebeu %d pedidos, quero pelo menos 4 (start, troca, /me, DELETE)", len(v))
	}
	for i, p := range v {
		if p != 1 {
			t.Errorf("pedido %d chegou em HTTP/%d, quero HTTP/1", i, p)
		}
	}
}

const varFilhoDaNuvem = "AURORA_NUVEM_TESTE_HTTP2DEBUG"

// TestHTTP2Debug_FilhoDaNuvem é o corpo do processo filho de
// TestHTTP2Debug_TokenDoAparelhoNaoVaiParaOLog: sozinho (sem a variável) não
// faz nada.
func TestHTTP2Debug_FilhoDaNuvem(t *testing.T) {
	modo := os.Getenv(varFilhoDaNuvem)
	if modo == "" {
		t.Skip("só roda como filho de TestHTTP2Debug_TokenDoAparelhoNaoVaiParaOLog")
	}
	var protos protocolosDaNuvem
	n := novaNuvemHTTP2(t, &protos)

	switch modo {
	case "controle":
		// Um cliente HTTP/2 comum, com o token como o Conferir o manda.
		req, _ := http.NewRequest(http.MethodGet, n.srv.URL+"/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenDoFilho)
		resp, err := n.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	case "nuvem":
		usarOTokenDoAparelho(t, n)
	default:
		t.Fatalf("modo %q desconhecido", modo)
	}
}

// TestHTTP2Debug_TokenDoAparelhoNaoVaiParaOLog reproduz o vazamento e prova
// que o Servico não o tem: roda o próprio binário de teste como processo filho
// com GODEBUG=http2debug=2 (o net/http lê a variável uma vez, ao iniciar) e
// procura o token na saída dele, que é para onde vai o log padrão.
func TestHTTP2Debug_TokenDoAparelhoNaoVaiParaOLog(t *testing.T) {
	if os.Getenv(varFilhoDaNuvem) != "" {
		t.Skip("é o filho")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	rodar := func(modo string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestHTTP2Debug_FilhoDaNuvem$", "-test.count=1", "-test.timeout=60s")
		cmd.Env = append(os.Environ(), "GODEBUG=http2debug=2", varFilhoDaNuvem+"="+modo)
		saida, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("o processo filho (%s) falhou: %v\n%s", modo, err, saida)
		}
		return string(saida)
	}

	// Com dentes: o cliente HTTP/2 comum vaza o token para o log.
	if controle := rodar("controle"); !strings.Contains(controle, tokenDoFilho) {
		t.Fatalf("o controle (HTTP/2 comum) não vazou o token para o log: o teste não enxerga o problema.\n%s", controle)
	}

	log := rodar("nuvem")
	if strings.Contains(log, tokenDoFilho) {
		t.Errorf("o token do aparelho foi para o log do net/http (GODEBUG=http2debug=2):\n%s", log)
	}
	if strings.Contains(log, "Bearer") || strings.Contains(log, "http2:") {
		t.Errorf("o Servico deixou rastro de HTTP/2 no log:\n%s", log)
	}
}
