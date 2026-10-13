package syncengine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// O cliente do sync fala só HTTP/1.1 (auditoria de rede do PC, 13-out-2026).
// Com HTTP/2, o net/http escreve cada cabeçalho (o Bearer do token do
// aparelho) e o começo de cada corpo (o texto dos ditados no push) no log
// padrão do processo quando a variável de ambiente GODEBUG traz http2debug, e o
// Agent manda o log padrão para o agent.log. É a mesma trava da chave de IA
// (internal/ia, TestHTTP2Debug_ChaveNaoVaiParaOLog).

// textoDoDitadoNoPush é o texto de um ditado que vai no corpo do push.
const textoDoDitadoNoPush = "ditado-que-nao-pode-ir-ao-log-do-sync"

// protocolosDoSync anota o protocolo (1 ou 2) de cada pedido que chegou.
type protocolosDoSync struct {
	mu sync.Mutex
	v  []int
}

func (p *protocolosDoSync) add(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.v = append(p.v, n)
}

func (p *protocolosDoSync) tira() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.v
	p.v = nil
	return v
}

// nuvemComHTTP2 é uma nuvem de mentira em TLS que fala HTTP/2 se o cliente
// pedir, e responde o push e o pull como a nuvem.
func nuvemComHTTP2(t *testing.T, protos *protocolosDoSync) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		protos.add(r.ProtoMajor)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/sync/push" {
			_, _ = io.WriteString(w, `{"results":[{"mutation_id":"0199a000-0000-7000-8000-000000000001","status":"applied","entity_version":2}],"sync_epoch":"e1"}`)
			return
		}
		_, _ = io.WriteString(w, `{"changes":[],"last_seq":0,"sync_epoch":"e1"}`)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// confiarNaNuvem põe o certificado da nuvem de teste no transporte de h, SEM
// trocar o transporte: é o de produção que tem de ir para a rede. Sem um
// transporte próprio, o cliente usaria o http.DefaultTransport, e o teste usa
// uma cópia dele (que fala HTTP/2, como ele).
func confiarNaNuvem(t *testing.T, h *http.Client, srv *httptest.Server) {
	t.Helper()
	var tr *http.Transport
	switch v := h.Transport.(type) {
	case nil:
		tr = http.DefaultTransport.(*http.Transport).Clone()
		h.Transport = tr
	case *http.Transport:
		tr = v
	default:
		t.Fatalf("o transporte do sync é %T; quero um *http.Transport", h.Transport)
	}
	manterOHTTP2DeProducao(tr)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
}

// manterOHTTP2DeProducao liga o ForceAttemptHTTP2 num transporte em que o
// net/http tentaria o HTTP/2 sozinho, e nada faz nos outros. Tem de ser chamada
// ANTES de o teste mexer no transporte.
//
// Sem isto, o teste podia ficar cego: num transporte sem Protocols, sem
// TLSNextProto e sem ForceAttemptHTTP2, o net/http tenta o HTTP/2 só se não
// houver um TLSClientConfig nem um discador próprios (Transport.protocols, "be
// conservative"). O RootCAs do teste é um TLSClientConfig próprio: ele
// desligaria o HTTP/2 que a produção, sem ele, falaria com a nuvem de verdade, e
// a nuvem de teste veria HTTP/1.1. Com Protocols explícito, como o de produção,
// nada muda (TestHTTP2_OTesteVeOHTTP2DeUmTransporteSemProtocols).
func manterOHTTP2DeProducao(tr *http.Transport) {
	if tr.Protocols == nil && tr.TLSNextProto == nil && !tr.ForceAttemptHTTP2 &&
		tr.TLSClientConfig == nil && tr.DialContext == nil && tr.DialTLSContext == nil {
		tr.ForceAttemptHTTP2 = true
	}
}

// TestHTTP2_OTesteVeOHTTP2DeUmTransporteSemProtocols prova que confiarNaNuvem
// não esconde o HTTP/2: um cliente cujo transporte não tem Protocols (e que,
// em produção, sem TLS nem discador próprios, falaria HTTP/2) chega em HTTP/2 à
// nuvem de teste depois de confiarNaNuvem. Sem isto, o
// TestCliente_NaoFalaHTTP2 e o TestHTTP2Debug_TokenNaoVaiParaOLog passavam
// para esse transporte, e só o TestNovoHTTP_TransporteSoHTTP1 o pegava.
func TestHTTP2_OTesteVeOHTTP2DeUmTransporteSemProtocols(t *testing.T) {
	var protos protocolosDoSync
	srv := nuvemComHTTP2(t, &protos)

	h := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second}}
	confiarNaNuvem(t, h, srv)
	if _, err := NovoCliente(srv.URL, tokenDeMentira, h).Pull(context.Background(), 0, 10); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if v := protos.tira(); len(v) != 1 || v[0] != 2 {
		t.Errorf("o transporte sem Protocols chegou em HTTP/%v; quero HTTP/2, como em produção: o teste está cego", v)
	}

	// E o de produção, explícito, continua só HTTP/1.1.
	h = NovoHTTP()
	confiarNaNuvem(t, h, srv)
	if _, err := NovoCliente(srv.URL, tokenDeMentira, h).Pull(context.Background(), 0, 10); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if v := protos.tira(); len(v) != 1 || v[0] != 1 {
		t.Errorf("o transporte de produção chegou em HTTP/%v; quero HTTP/1", v)
	}
}

func pushComTexto() []Mutation {
	return []Mutation{{MutationID: "0199a000-0000-7000-8000-000000000001", EntityType: "transcription",
		EntityID: idDoTeste, Operation: OpUpsert,
		Payload: json.RawMessage(`{"final_text":"` + textoDoDitadoNoPush + `"}`)}}
}

func TestNovoHTTP_TransporteSoHTTP1(t *testing.T) {
	tr, ok := NovoHTTP().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("NovoHTTP usa o transporte %T; nil é o http.DefaultTransport, que fala HTTP/2", NovoHTTP().Transport)
	}
	if tr.Protocols == nil || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() {
		t.Errorf("Protocols = %v, quero só HTTP/1.1", tr.Protocols)
	}
	if tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 {
		t.Error("o transporte tem caminho para o HTTP/2")
	}
	// O proxy do ambiente e os prazos do transporte padrão continuam valendo.
	if tr.Proxy == nil || tr.TLSHandshakeTimeout <= 0 || tr.DialContext == nil {
		t.Error("o transporte não tem o proxy do ambiente e os prazos do transporte padrão")
	}
	// O que já valia continua: o prazo e o redirecionamento não seguido.
	h := NovoHTTP()
	if h.Timeout != PrazoDoPedido || h.CheckRedirect == nil {
		t.Errorf("NovoHTTP perdeu o prazo (%v) ou a recusa de redirecionamento", h.Timeout)
	}
}

// TestCliente_NaoFalaHTTP2 vai à rede de verdade (loopback) com uma nuvem que
// ACEITA HTTP/2: o push e o pull do cliente de produção chegam em HTTP/1.1, e
// o NovoCliente com h nil também.
func TestCliente_NaoFalaHTTP2(t *testing.T) {
	var protos protocolosDoSync
	srv := nuvemComHTTP2(t, &protos)

	// Controle: um cliente comum negocia HTTP/2 com esta nuvem. Sem isto,
	// "chegou HTTP/1.1" poderia ser só a nuvem que não oferece HTTP/2.
	resp, err := srv.Client().Get(srv.URL + "/api/v1/sync/pull")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := protos.tira(); len(v) != 1 || v[0] != 2 {
		t.Fatalf("controle: o cliente comum falou HTTP/%v, quero HTTP/2", v)
	}

	h := NovoHTTP()
	confiarNaNuvem(t, h, srv)
	c := NovoCliente(srv.URL, tokenDeMentira, h)
	if _, err := c.Push(context.Background(), pushComTexto()); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := c.Pull(context.Background(), 0, 10); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	// h nil: o NovoCliente monta o seu, que tem de ser o mesmo.
	semH := NovoCliente(srv.URL, tokenDeMentira, nil)
	confiarNaNuvem(t, semH.http, srv)
	if _, err := semH.Pull(context.Background(), 0, 10); err != nil {
		t.Fatalf("Pull com h nil: %v", err)
	}
	v := protos.tira()
	if len(v) != 3 {
		t.Fatalf("a nuvem recebeu %d pedidos, quero 3", len(v))
	}
	for i, p := range v {
		if p != 1 {
			t.Errorf("pedido %d chegou em HTTP/%d, quero HTTP/1", i, p)
		}
	}
}

const varFilhoDoSync = "AURORA_SYNC_TESTE_HTTP2DEBUG"

// TestHTTP2Debug_FilhoDoSync é o corpo do processo filho de
// TestHTTP2Debug_TokenNaoVaiParaOLog: sozinho (sem a variável) não faz nada.
func TestHTTP2Debug_FilhoDoSync(t *testing.T) {
	modo := os.Getenv(varFilhoDoSync)
	if modo == "" {
		t.Skip("só roda como filho de TestHTTP2Debug_TokenNaoVaiParaOLog")
	}
	var protos protocolosDoSync
	srv := nuvemComHTTP2(t, &protos)

	switch modo {
	case "controle":
		// Um cliente HTTP/2 comum, com o token e o ditado como o push os manda.
		corpo, _ := json.Marshal(map[string]any{"mutations": pushComTexto()})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/sync/push", strings.NewReader(string(corpo)))
		req.Header.Set("Authorization", "Bearer "+tokenDeMentira)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	case "sync":
		h := NovoHTTP()
		confiarNaNuvem(t, h, srv)
		c := NovoCliente(srv.URL, tokenDeMentira, h)
		if _, err := c.Push(context.Background(), pushComTexto()); err != nil {
			t.Fatalf("Push: %v", err)
		}
		if _, err := c.Pull(context.Background(), 0, 10); err != nil {
			t.Fatalf("Pull: %v", err)
		}
	default:
		t.Fatalf("modo %q desconhecido", modo)
	}
}

// TestHTTP2Debug_TokenNaoVaiParaOLog reproduz o vazamento e prova que o sync
// não o tem: roda o próprio binário de teste como processo filho com
// GODEBUG=http2debug=2 (o net/http lê a variável uma vez, ao iniciar) e procura
// o token e o texto do ditado na saída dele, que é para onde vai o log padrão.
func TestHTTP2Debug_TokenNaoVaiParaOLog(t *testing.T) {
	if os.Getenv(varFilhoDoSync) != "" {
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
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestHTTP2Debug_FilhoDoSync$", "-test.count=1", "-test.timeout=60s")
		cmd.Env = append(os.Environ(), "GODEBUG=http2debug=2", varFilhoDoSync+"="+modo)
		saida, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("o processo filho (%s) falhou: %v\n%s", modo, err, saida)
		}
		return string(saida)
	}

	// Com dentes: o cliente HTTP/2 comum vaza o token para o log.
	if controle := rodar("controle"); !strings.Contains(controle, tokenDeMentira) {
		t.Fatalf("o controle (HTTP/2 comum) não vazou o token para o log: o teste não enxerga o problema.\n%s", controle)
	}

	log := rodar("sync")
	for nome, segredo := range map[string]string{"o token": tokenDeMentira, "o texto do ditado": textoDoDitadoNoPush} {
		if strings.Contains(log, segredo) {
			t.Errorf("%s foi para o log do net/http (GODEBUG=http2debug=2):\n%s", nome, log)
		}
	}
	if strings.Contains(log, "Bearer") || strings.Contains(log, "http2:") {
		t.Errorf("o sync deixou rastro de HTTP/2 no log:\n%s", log)
	}
}
