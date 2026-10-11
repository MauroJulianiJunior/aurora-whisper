package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O subcomando healthcheck (F11c): o GET no loopback, na porta de HTTP_ADDR,
// com prazo, sem seguir redirecionamento, saindo só com 0 ou 1.

// portaDe devolve a porta de um httptest.Server.
func portaDe(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, porta, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return porta
}

// healthcheck roda o subcomando com HTTP_ADDR em 0.0.0.0 (o do contêiner) na
// porta do srv.
func healthcheck(t *testing.T, srv *httptest.Server, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	env := map[string]string{envHTTPAddr: "0.0.0.0:" + portaDe(t, srv)}
	return runCmd(t, env, append([]string{"healthcheck"}, args...)...)
}

func TestHealthcheck_Vida(t *testing.T) {
	var caminhos atomic.Value
	caminhos.Store("")
	var status atomic.Int32
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caminhos.Store(r.URL.Path)
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()

	if out, errOut, code := healthcheck(t, srv); code != 0 || !strings.Contains(out, "OK") || errOut != "" {
		t.Fatalf("200: %d %q %q", code, out, errOut)
	}
	if got := caminhos.Load(); got != "/api/v1/health/live" {
		t.Errorf("o pedido foi a %q", got)
	}
	for _, s := range []int{http.StatusServiceUnavailable, http.StatusNotFound, http.StatusNoContent, http.StatusInternalServerError} {
		status.Store(int32(s))
		if _, errOut, code := healthcheck(t, srv); code != 1 || !strings.Contains(errOut, fmt.Sprint(s)) {
			t.Errorf("%d: %d %q; quero 1", s, code, errOut)
		}
	}
}

// O redirecionamento não é seguido: a resposta da porta é a 302, e o destino
// nunca recebe o pedido.
func TestHealthcheck_NaoSegueRedirecionamento(t *testing.T) {
	var destinoVisto atomic.Bool
	destino := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinoVisto.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer destino.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destino.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()
	if _, errOut, code := healthcheck(t, srv); code != 1 || !strings.Contains(errOut, "302") {
		t.Fatalf("com redirecionamento: %d %q; quero 1", code, errOut)
	}
	if destinoVisto.Load() {
		t.Error("o healthcheck seguiu o redirecionamento")
	}
}

// O prazo estourado é 1; ninguém escutando também.
func TestHealthcheck_PrazoESemNinguem(t *testing.T) {
	antes := prazoDoHealthcheck
	prazoDoHealthcheck = 100 * time.Millisecond
	t.Cleanup(func() { prazoDoHealthcheck = antes })
	soltar := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-soltar:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(soltar)
	inicio := time.Now()
	if _, errOut, code := healthcheck(t, srv); code != 1 || !strings.Contains(errOut, "sem resposta") {
		t.Fatalf("prazo estourado: %d %q; quero 1", code, errOut)
	}
	if d := time.Since(inicio); d > 5*time.Second {
		t.Errorf("o prazo não valeu: %s", d)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	livre := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if _, errOut, code := runCmd(t, map[string]string{envHTTPAddr: fmt.Sprintf("0.0.0.0:%d", livre)}, "healthcheck"); code != 1 || errOut == "" {
		t.Errorf("ninguém escutando: %d %q; quero 1", code, errOut)
	}
}

// O uso errado também é 1 (o 2 é reservado pelo Docker), e o HTTP_ADDR errado
// não aparece na mensagem.
func TestHealthcheck_UsoErradoEhUm(t *testing.T) {
	for nome, c := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"opção desconhecida":     {nil, []string{"-tudo"}},
		"argumento solto":        {nil, []string{"agora"}},
		"HTTP_ADDR sem porta":    {map[string]string{envHTTPAddr: "ENDERECO-SECRETO"}, nil},
		"porta que não é número": {map[string]string{envHTTPAddr: "0.0.0.0:PORTA-SECRETA"}, nil},
		"porta fora da faixa":    {map[string]string{envHTTPAddr: "0.0.0.0:70000"}, nil},
	} {
		out, errOut, code := runCmd(t, c.env, append([]string{"healthcheck"}, c.args...)...)
		if code != 1 {
			t.Errorf("%s: %d; quero 1", nome, code)
		}
		if strings.Contains(out+errOut, "SECRET") {
			t.Errorf("%s: a saída traz o valor: %q", nome, out+errOut)
		}
	}
}

// O alvo: a porta de HTTP_ADDR, no loopback dela ou em 127.0.0.1.
func TestEnderecoDaSaude(t *testing.T) {
	for in, want := range map[string]string{
		"":                "127.0.0.1:8080",
		"0.0.0.0:8080":    "127.0.0.1:8080",
		":9000":           "127.0.0.1:9000",
		"[::]:8080":       "127.0.0.1:8080",
		"localhost:8080":  "127.0.0.1:8080",
		"127.0.0.2:8081":  "127.0.0.2:8081",
		"[::1]:8080":      "[::1]:8080",
		"10.0.0.5:8080":   "127.0.0.1:8080",
		"172.18.0.3:8080": "127.0.0.1:8080",
	} {
		got, err := enderecoDaSaude(in)
		if err != nil || got != want {
			t.Errorf("enderecoDaSaude(%q) = %q, %v; quero %q", in, got, err, want)
		}
	}
}

// O -retencao contra respostas feitas à mão: o limite de 2 h (o 7200 vale, o
// 7201 não), a falha grave, a falta de rodada, o JSON que não é o esperado e o
// status que não é 200.
func TestHealthcheck_RetencaoRegras(t *testing.T) {
	var corpo atomic.Value
	var status atomic.Int32
	var caminho atomic.Value
	caminho.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caminho.Store(r.URL.Path)
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(corpo.Load().(string)))
	}))
	defer srv.Close()
	for _, c := range []struct {
		nome   string
		status int
		corpo  string
		want   int
	}{
		{"recente", 200, `{"rodadas":3,"ultima_ha_s":60,"falhas_graves":0}`, 0},
		{"no limite de 2 h", 200, `{"rodadas":3,"ultima_ha_s":7200,"falhas_graves":0}`, 0},
		{"passou de 2 h", 200, `{"rodadas":3,"ultima_ha_s":7201,"falhas_graves":0}`, 1},
		{"muito velha", 200, `{"rodadas":3,"ultima_ha_s":9223372036,"falhas_graves":0}`, 1},
		{"falha grave", 200, `{"rodadas":3,"ultima_ha_s":60,"falhas_graves":1}`, 1},
		{"sem rodada", 200, `{"rodadas":0,"ultima_ha_s":0,"falhas_graves":0}`, 1},
		{"não é JSON", 200, `ok`, 1},
		{"404 (de fora do loopback)", 404, `{"error":{}}`, 1},
		{"503", 503, `{"rodadas":3,"ultima_ha_s":60,"falhas_graves":0}`, 1},
	} {
		status.Store(int32(c.status))
		corpo.Store(c.corpo)
		out, errOut, code := healthcheck(t, srv, "-retencao")
		if code != c.want {
			t.Errorf("%s: %d %q %q; quero %d", c.nome, code, out, errOut, c.want)
		}
	}
	if got := caminho.Load(); got != "/api/v1/health/retencao" {
		t.Errorf("o -retencao pediu %q", got)
	}
}

// relogioDoTeste é um relógio que o teste anda.
type relogioDoTeste struct{ ns atomic.Int64 }

func (r *relogioDoTeste) agora() time.Time      { return time.Unix(0, r.ns.Load()).UTC() }
func (r *relogioDoTeste) andar(d time.Duration) { r.ns.Add(int64(d)) }

// retencaoQueFalha faz o Reter falhar com o erro escolhido (nil: não falha).
type retencaoQueFalha struct {
	*cloudstore.MemoryStore
	err atomic.Value // error, embrulhado
}

type erroEmbrulhado struct{ err error }

func (r *retencaoQueFalha) Reter(ctx context.Context, userID string, now time.Time) (cloudstore.Retido, error) {
	if e, _ := r.err.Load().(erroEmbrulhado); e.err != nil {
		return cloudstore.Retido{}, e.err
	}
	return r.MemoryStore.Reter(ctx, userID, now)
}

// O -retencao de ponta a ponta: a nuvem de verdade (a rota e o Reter, no
// Store em memória) num servidor do loopback, com o relógio da nuvem andando.
// Saudável depois de uma rodada; 1 com mais de 2 h sem rodada; 1 com a falha
// de outra categoria; 0 de novo com a falha de contexto (o desligamento) e
// depois de uma rodada boa.
func TestHealthcheck_RetencaoContraANuvem(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	mem.AddUser(contracttest.NewID())
	store := &retencaoQueFalha{MemoryStore: mem}
	store.err.Store(erroEmbrulhado{})
	relogio := &relogioDoTeste{}
	relogio.ns.Store(time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).UnixNano())
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: store, Accounts: mem, BaseURL: contracttest.BaseURL,
		SessionSecret: []byte(testSessionSecret), DeviceTokenPepper: []byte(testPepper), Contas: contas,
		Google: cloudauth.NovoGoogle("cliente", testClientSecret, contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Now: relogio.agora, Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(cloud.Handler())
	defer srv.Close()
	quer := func(oque string, want int) {
		t.Helper()
		out, errOut, code := healthcheck(t, srv, "-retencao")
		if code != want {
			t.Errorf("%s: %d %q %q; quero %d", oque, code, out, errOut, want)
		}
	}

	quer("antes de qualquer rodada", 1)
	cloud.Reter(context.Background())
	quer("logo depois de uma rodada", 0)
	relogio.andar(2 * time.Hour)
	quer("2 h depois", 0)
	relogio.andar(time.Second)
	quer("2 h e 1 s depois", 1)
	cloud.Reter(context.Background())
	quer("depois de outra rodada", 0)
	store.err.Store(erroEmbrulhado{errors.New("pgstore: dial tcp 10.0.0.9:5432")})
	cloud.Reter(context.Background())
	quer("com a falha de outra categoria", 1)
	store.err.Store(erroEmbrulhado{context.Canceled})
	cloud.Reter(context.Background())
	quer("com a falha de contexto", 0)
	// O de vida, na mesma nuvem.
	if _, errOut, code := healthcheck(t, srv); code != 0 {
		t.Errorf("o de vida: %d %q", code, errOut)
	}
}
