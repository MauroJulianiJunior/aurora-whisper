package ia

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Este arquivo é a TRAVA DE REDE da suíte do pacote e os testes do transporte.
//
// Quase todo teste do pacote aponta o Cliente para um httptest.Server em
// loopback, com o HTTP nil, ou seja, com o transporte real. Se um dia o
// código ignorar a base injetada (uma refatoração, um mutante), o pedido
// SAI da máquina rumo a api.openai.com ou api.anthropic.com, levando a chave
// e o ditado de teste, e fica nos logs do provedor. Por isso, antes de
// qualquer teste, TestMain:
//
//  1. aponta HTTP_PROXY e HTTPS_PROXY para uma porta morta em loopback
//     (loopback nunca passa por proxy, então o httptest continua
//     funcionando; qualquer outro destino não vai a lugar nenhum);
//  2. troca o transporte do pacote e o http.DefaultTransport por um que
//     RECUSA todo destino que não seja loopback, conta a tentativa e faz a
//     suíte inteira falhar no fim.
//
// Os dois valem para qualquer teste novo, sem ninguém lembrar de nada.

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

// trocarTransporte põe rt como o transporte do pacote durante o teste.
func trocarTransporte(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	antes := transporteHTTP1
	transporteHTTP1 = rt
	t.Cleanup(func() { transporteHTTP1 = antes })
}

// ---------------------------------------------------------------------
// A trava funciona
// ---------------------------------------------------------------------

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
	var corposFechados int
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		passou = append(passou, r.URL.Host)
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	reg := &registroDeRede{}
	g := guardaDeRede{base, reg}

	for _, alvo := range []string{
		"https://api.openai.com/v1/responses",
		"https://api.anthropic.com/v1/messages",
		"http://8.8.8.8/",
		"http://localhost.exemplo.com/",
		"http://127.0.0.1.exemplo.com/",
		"http://10.0.0.1:8080/",
		"http://[2001:db8::1]/",
	} {
		req, _ := http.NewRequest(http.MethodPost, alvo, nil)
		req.Body = &corpoMarcado{Reader: strings.NewReader("corpo"), fechou: func() { corposFechados++ }}
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
	if got := len(reg.lista()); got != 7 {
		t.Errorf("a guarda registrou %d destinos, quero 7", got)
	}
	if corposFechados != 7 {
		t.Errorf("a guarda fechou %d corpos de pedido recusado, quero 7 (o RoundTripper tem que fechá-lo)", corposFechados)
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
	if got := len(reg.lista()); got != 7 {
		t.Errorf("loopback foi registrado como recusado: %d", got)
	}
}

// O proxy morto: um transporte NOVO, que só sabe o que o ambiente diz, não
// alcança host nenhum. O host é de um TLD reservado (RFC 2606): nem que o
// proxy não valesse, o nome não resolve.
func TestProxyMorto_NadaPassaPorFora(t *testing.T) {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	defer tr.CloseIdleConnections()
	for _, alvo := range []string{"https://api.openai.invalid/v1/responses", "http://api.anthropic.invalid/v1/messages"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, alvo, nil)
		resp, err := tr.RoundTrip(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			t.Fatalf("%s: o pedido passou", alvo)
		}
		if !strings.Contains(err.Error(), "proxyconnect") {
			t.Errorf("%s: o pedido não foi pelo proxy morto: %v", alvo, err)
		}
	}
}

// esteBinario é o caminho do binário de teste em execução, que os testes de
// processo filho rodam de novo (o primeiro argumento do processo pode ser
// relativo ao diretório de quem o chamou).
func esteBinario(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("não achei o binário de teste: %v", err)
	}
	return exe
}

const varFilhoTrava = "AURORA_IA_TESTE_TRAVA_FILHO"

// TestTrava_Filho é o corpo do processo filho de
// TestTrava_ReprovaOPacoteSeAlgoTentouSair: dois testes CEGOS, que mandam um
// pedido para um host reservado (RFC 2606, nunca resolve) e ignoram o erro,
// um pelo http.DefaultTransport e outro pelo transporte do pacote. Sozinho
// (sem a variável de ambiente) não faz nada.
func TestTrava_Filho(t *testing.T) {
	if os.Getenv(varFilhoTrava) == "" {
		t.Skip("só roda como filho de TestTrava_ReprovaOPacoteSeAlgoTentouSair")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.openai.invalid/", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
	c := &Cliente{BaseAnthropic: "https://api.anthropic.invalid"}
	c.Polir(context.Background(), chamadaAnthropic, chave, pedidoReal(ditado))
}

// Um teste que engole o erro da guarda passaria sozinho, e a tentativa de sair
// da máquina ficaria só no registro. O TestMain tem que reprovar o pacote.
func TestTrava_ReprovaOPacoteSeAlgoTentouSair(t *testing.T) {
	if os.Getenv(varFilhoTrava) != "" {
		t.Skip("é o filho")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, esteBinario(t), "-test.run=^TestTrava_Filho$", "-test.count=1", "-test.v", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), varFilhoTrava+"=1")
	saida, err := cmd.CombinedOutput()
	out := string(saida)
	if err == nil {
		t.Fatalf("o pacote passou mesmo com dois pedidos para fora da máquina:\n%s", out)
	}
	// Os testes do filho passam (são cegos de propósito): quem reprova é o
	// TestMain, com os destinos, e só os destinos.
	for _, quero := range []string{
		"--- PASS: TestTrava_Filho",
		"FALHA: 2 pedido(s) da suíte tentaram sair da máquina",
		"api.openai.invalid", "api.anthropic.invalid",
	} {
		if !strings.Contains(out, quero) {
			t.Errorf("a saída do filho não traz %q:\n%s", quero, out)
		}
	}
	if strings.Contains(out, chave) {
		t.Error("a saída do filho traz a chave")
	}
}

// corpoMarcado é um io.ReadCloser que avisa quando foi fechado.
type corpoMarcado struct {
	io.Reader
	fechou func()
}

func (c *corpoMarcado) Close() error {
	c.fechou()
	return nil
}

// ---------------------------------------------------------------------
// O transporte do pacote: só HTTP/1.1
// ---------------------------------------------------------------------

func TestNovoTransporteHTTP1_SoHTTP1EComProxyDoAmbiente(t *testing.T) {
	tr := novoTransporteHTTP1()
	if tr.Protocols == nil || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() {
		t.Fatalf("Protocols = %v, quero só HTTP/1.1", tr.Protocols)
	}
	if tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 {
		t.Error("o transporte tem caminho para o HTTP/2")
	}
	// O proxy do ambiente continua valendo (é ele que o TestMain usa como
	// trava), e os prazos do transporte padrão também.
	if tr.Proxy == nil {
		t.Fatal("o transporte não usa o proxy do ambiente")
	}
	alvo, _ := url.Parse("https://api.openai.invalid/v1/responses")
	if p, err := tr.Proxy(&http.Request{URL: alvo}); err != nil || p == nil || p.String() != proxyMorto {
		t.Errorf("proxy para um host externo = %v, %v; quero %s", p, err, proxyMorto)
	}
	if tr.TLSHandshakeTimeout <= 0 || tr.IdleConnTimeout <= 0 || tr.DialContext == nil {
		t.Error("o transporte não tem os prazos do transporte padrão")
	}
}

// protocolos guarda o protocolo (1 ou 2) de cada pedido que chegou ao
// servidor de teste.
type protocolos struct {
	mu sync.Mutex
	v  []int
}

func (p *protocolos) add(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.v = append(p.v, n)
}

func (p *protocolos) tira() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.v
	p.v = nil
	return v
}

// servidorTLSComHTTP2 é um provedor de mentira que fala HTTP/2 se o cliente
// pedir. Cada pedido registra o protocolo que chegou.
func servidorTLSComHTTP2(t *testing.T, protos *protocolos) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		protos.add(r.ProtoMajor)
		switch r.URL.Path {
		case "/v1/messages":
			io.WriteString(w, respostaAnthropic(saida("ok"), "end_turn"))
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"modelo-1","created":1}]}`)
		default:
			io.WriteString(w, respostaOpenAI(saida("ok")))
		}
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// transporteQueConfia é o transporte do pacote (só HTTP/1.1), mas confiando
// no certificado do servidor de teste.
func transporteQueConfia(srv *httptest.Server) *http.Transport {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr := novoTransporteHTTP1()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	return tr
}

// O provedor de teste ACEITA HTTP/2; o Cliente não pode pedir. Com HTTP/2, a
// chave e o começo do corpo iriam ao log padrão sob GODEBUG=http2debug (o
// teste abaixo prova), e o cabeçalho de autenticação ficaria na tabela HPACK
// da conexão.
func TestTransportePadrao_NaoFalaHTTP2(t *testing.T) {
	var protos protocolos
	srv := servidorTLSComHTTP2(t, &protos)

	// Controle: um cliente comum negocia HTTP/2 com este servidor. Sem isto,
	// "chegou HTTP/1.1" poderia ser só o servidor que não oferece HTTP/2.
	resp, err := srv.Client().Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := protos.tira(); len(v) != 1 || v[0] != 2 {
		t.Fatalf("controle: o cliente comum falou HTTP/%v, quero HTTP/2 (o servidor de teste devia oferecer)", v)
	}

	trocarTransporte(t, transporteQueConfia(srv))
	c := &Cliente{BaseOpenAI: srv.URL, BaseAnthropic: srv.URL}
	for _, p := range []string{OpenAI, Anthropic} {
		if _, err := c.Polir(context.Background(), chamadaDe(p), chave, pedidoReal(ditado)); err != nil {
			t.Fatalf("Polir %s: %v", p, err)
		}
		if _, err := c.ListarModelos(context.Background(), p, chave); err != nil {
			t.Fatalf("ListarModelos %s: %v", p, err)
		}
	}
	// Um HTTP injetado que não traz Transport também fica em HTTP/1.1.
	c.HTTP = &http.Client{}
	if _, err := c.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado)); err != nil {
		t.Fatalf("Polir com HTTP sem Transport: %v", err)
	}

	v := protos.tira()
	if len(v) != 5 {
		t.Fatalf("o provedor recebeu %d pedidos, quero 5", len(v))
	}
	for i, p := range v {
		if p != 1 {
			t.Errorf("pedido %d chegou em HTTP/%d, quero HTTP/1", i, p)
		}
	}
}

// O transporte exportado é o mesmo do pacote: só HTTP/1.1, mesmo diante de um
// servidor que oferece HTTP/2, e um novo a cada chamada (quem o embrulha pode
// mexer nele sem afetar o dos outros).
func TestNovoTransporteHTTP1_SoFalaHTTP1(t *testing.T) {
	var protos protocolos
	srv := servidorTLSComHTTP2(t, &protos)

	tr := NovoTransporteHTTP1()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	defer tr.CloseIdleConnections()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/models", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := protos.tira(); len(v) != 1 || v[0] != 1 {
		t.Errorf("o transporte exportado falou HTTP/%v, quero HTTP/1", v)
	}
	if tr.Protocols == nil || tr.Protocols.HTTP2() || !tr.Protocols.HTTP1() {
		t.Errorf("Protocols = %v, quero só HTTP/1", tr.Protocols)
	}
	if NovoTransporteHTTP1() == tr {
		t.Error("NovoTransporteHTTP1 devolveu o mesmo transporte duas vezes; quero um novo a cada chamada")
	}
}

const varFilho = "AURORA_IA_TESTE_HTTP2DEBUG"

// TestHTTP2Debug_Filho é o corpo do processo filho de
// TestHTTP2Debug_ChaveNaoVaiParaOLog: sozinho (sem a variável de ambiente)
// não faz nada.
func TestHTTP2Debug_Filho(t *testing.T) {
	modo := os.Getenv(varFilho)
	if modo == "" {
		t.Skip("só roda como filho de TestHTTP2Debug_ChaveNaoVaiParaOLog")
	}
	var protos protocolos
	srv := servidorTLSComHTTP2(t, &protos)

	switch modo {
	case "controle":
		// Um cliente HTTP/2 comum, com a chave e o ditado como o Polish os
		// mandaria.
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/responses", strings.NewReader(`{"input":"`+ditado+`"}`))
		req.Header.Set("Authorization", "Bearer "+chave)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	case "polish":
		trocarTransporte(t, transporteQueConfia(srv))
		c := &Cliente{BaseOpenAI: srv.URL, BaseAnthropic: srv.URL}
		for _, p := range []string{OpenAI, Anthropic} {
			if _, err := c.Polir(context.Background(), chamadaDe(p), chave, pedidoReal(ditado)); err != nil {
				t.Fatalf("Polir %s: %v", p, err)
			}
		}
		if _, err := c.ListarModelos(context.Background(), OpenAI, chave); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("modo %q desconhecido", modo)
	}
}

// TestHTTP2Debug_ChaveNaoVaiParaOLog reproduz o vazamento: o net/http escreve
// no log padrão (o mesmo que o Agent manda para o agent.log) cada cabeçalho e
// o começo de cada corpo do HTTP/2 quando GODEBUG traz http2debug. Roda o
// mesmo binário de teste como processo filho, com essa variável, porque o
// net/http a lê uma vez, ao iniciar.
func TestHTTP2Debug_ChaveNaoVaiParaOLog(t *testing.T) {
	if os.Getenv(varFilho) != "" {
		t.Skip("é o filho")
	}
	rodar := func(modo string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, esteBinario(t), "-test.run=^TestHTTP2Debug_Filho$", "-test.count=1", "-test.timeout=60s")
		cmd.Env = append(os.Environ(), "GODEBUG=http2debug=2", varFilho+"="+modo)
		saida, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("o processo filho (%s) falhou: %v\n%s", modo, err, saida)
		}
		return string(saida)
	}

	// Com dentes: o cliente HTTP/2 comum vaza a chave para o log, então este
	// teste enxerga o que procura.
	if controle := rodar("controle"); !strings.Contains(controle, chave) {
		t.Fatalf("o controle (HTTP/2 comum) não vazou a chave para o log: o teste não enxerga o problema.\n%s", controle)
	}

	log := rodar("polish")
	procurar(t, "o log do net/http (GODEBUG=http2debug=2) durante o Polish", log)
	if strings.Contains(log, "Bearer") || strings.Contains(log, "http2:") {
		t.Errorf("o Polish deixou rastro de HTTP/2 no log:\n%s", log)
	}
}
