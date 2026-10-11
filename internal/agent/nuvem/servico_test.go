package nuvem_test

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
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
)

// Os dublês: a instalação (a tabela instalacao, num mapa), o Cofre
// (chaves.Memoria, com as mesmas regras do Gerenciador), o navegador (uma
// função que anota o endereço) e a nuvem (um httptest que faz o que a nuvem
// de verdade faz no pareamento, com o PKCE conferido). A nuvem de verdade
// contra o Agent de verdade está em internal/server/integration.

type instalacaoMem struct {
	mu sync.Mutex
	m  map[string]string
}

func novaInstalacao() *instalacaoMem { return &instalacaoMem{m: map[string]string{}} }

func (i *instalacaoMem) Ler(_ context.Context, nome string) (string, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	v, ok := i.m[nome]
	return v, ok, nil
}

func (i *instalacaoMem) Gravar(_ context.Context, valores map[string]string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for k, v := range valores {
		i.m[k] = v
	}
	return nil
}

func (i *instalacaoMem) Apagar(_ context.Context, nomes ...string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, k := range nomes {
		delete(i.m, k)
	}
	return nil
}

func (i *instalacaoMem) tudo() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	raw, _ := json.Marshal(i.m)
	return string(raw)
}

func (i *instalacaoMem) valor(nome string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.m[nome]
}

// Os valores fixos da nuvem falsa.
const (
	pedidoFalso      = "0192f0a1-7c3e-7d4a-9b2f-1a2b3c4d5e6f"
	contaFalsa       = "0192f0a1-0000-7000-8000-00000000c0a1"
	aparelhoFalso    = "0192f0a1-0000-7000-8000-0000000a9a1e"
	emailFalso       = "voce@teste.local"
	conferenciaFalsa = "K7QM"
)

// segredo é como a nuvem gera o code e o token: 256 bits em base64url.
func segredo() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func s256(v string) string {
	h := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// nuvemFalsa faz o pareamento como a nuvem: o start guarda o desafio; o
// "navegador" aprova (aprovar) e recebe um code; a troca confere o code, o
// uso único e o verificador contra o desafio; o /me e o DELETE do próprio
// aparelho conferem o token.
type nuvemFalsa struct {
	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	pedidos    int // todos os pedidos que chegaram
	start      map[string]any
	desafio    string
	code       string
	codeUsado  bool
	trocas     int
	token      string
	revogado   bool
	mes        int
	deletes    []string // o caminho e o Bearer de cada DELETE
	contaDaVez string

	// Para os casos de falha.
	startStatus    int
	startCorpo     string
	trocaStatus    int
	trocaToken     string // "" é um token novo
	meStatus       int    // 0: confere o token
	deleteStatus   int    // 0: confere o token
	expiraEm       int
	authURL        string
	redirecionarEm string
}

func novaNuvemFalsa(t *testing.T) *nuvemFalsa { return novaNuvem(t, false) }

// novaNuvem: comTLS é a nuvem em https (o certificado do httptest vale
// para example.com).
func novaNuvem(t *testing.T, comTLS bool) *nuvemFalsa {
	n := &nuvemFalsa{t: t, expiraEm: 600, authURL: "https://outro-lugar.exemplo/device/authorize?pedido=" + pedidoFalso, contaDaVez: contaFalsa}
	if comTLS {
		n.srv = httptest.NewTLSServer(http.HandlerFunc(n.servir))
	} else {
		n.srv = httptest.NewServer(http.HandlerFunc(n.servir))
	}
	t.Cleanup(n.srv.Close)
	return n
}

func (n *nuvemFalsa) servir(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pedidos++
	if n.redirecionarEm != "" {
		http.Redirect(w, r, n.redirecionarEm, http.StatusTemporaryRedirect)
		return
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/device-auth/start":
		if n.startStatus != 0 {
			w.WriteHeader(n.startStatus)
			io.WriteString(w, n.startCorpo)
			return
		}
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n.start = b
		n.desafio, _ = b["code_challenge"].(string)
		n.code, n.codeUsado = "", false
		corpo := n.startCorpo
		if corpo == "" {
			raw, _ := json.Marshal(map[string]any{"pedido": pedidoFalso, "auth_url": n.authURL, "conferencia": conferenciaFalsa, "expires_in": n.expiraEm})
			corpo = string(raw)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, corpo)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/device-auth/exchange":
		n.trocas++
		var b struct {
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if n.trocaStatus != 0 {
			w.WriteHeader(n.trocaStatus)
			return
		}
		if n.code == "" || b.Code != n.code || n.codeUsado || s256(b.CodeVerifier) != n.desafio {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"code":"CODIGO_INVALIDO"}}`)
			return
		}
		n.codeUsado = true
		n.token = n.trocaToken
		if n.token == "" {
			n.token = segredo()
		}
		n.revogado = false
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_id": aparelhoFalso, "token": n.token, "conta": map[string]string{"id": n.contaDaVez, "email": emailFalso},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me":
		n.mes++
		if n.meStatus != 0 {
			w.WriteHeader(n.meStatus)
			return
		}
		if bearer == "" || bearer != n.token || n.revogado {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"email":"`+emailFalso+`"}`)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/devices/"):
		n.deletes = append(n.deletes, r.URL.Path+" "+bearer)
		if n.deleteStatus != 0 {
			w.WriteHeader(n.deleteStatus)
			return
		}
		if bearer == "" || bearer != n.token || n.revogado {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/v1/devices/"+aparelhoFalso {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		n.revogado = true
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// aprovar é o clique em Aprovar na página da nuvem: um code novo para o
// pedido em curso, que o navegador leva ao retorno.
func (n *nuvemFalsa) aprovar() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.code = segredo()
	return n.code
}

func (n *nuvemFalsa) contar() (pedidos, trocas, mes int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.pedidos, n.trocas, n.mes
}

func (n *nuvemFalsa) mexer(f func(n *nuvemFalsa)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	f(n)
}

// cliente é o NovoCliente com todo pedido indo para a nuvem falsa, qualquer
// que seja o host da URL (o PC "pensa" que fala com localhost:8080).
func (n *nuvemFalsa) cliente() *http.Client {
	c := nuvem.NovoCliente()
	addr := n.srv.Listener.Addr().String()
	tr := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}
	if n.srv.TLS != nil {
		tr.TLSClientConfig = n.srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	}
	c.Transport = tr
	return c
}

// relogio de teste.
type relogio struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relogio) agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogio) andar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

// cenario é um Servico montado sobre os dublês.
type cenario struct {
	t     *testing.T
	s     *nuvem.Servico
	nuvem *nuvemFalsa
	inst  *instalacaoMem
	cofre *chaves.Memoria
	clk   *relogio
	logs  *bytes.Buffer
	logMu *sync.Mutex

	abriuMu  sync.Mutex
	abertos  []string
	erroAbre error
	ids      int
}

const portaDoPainel = 51234

// URLDaNuvem é o que a pessoa cola: a nuvem de teste no PC.
const urlDaNuvem = "http://localhost:8080"

type opcao func(*nuvem.Config)

func comPolitica(p nuvem.Politica) opcao { return func(c *nuvem.Config) { c.Politica = p } }
func comAlvo(nome string) opcao          { return func(c *nuvem.Config) { c.NomeNoCofre = nome } }

// logSeguro é um io.Writer seguro para uso concorrente.
type logSeguro struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (l logSeguro) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func novoCenario(t *testing.T, ops ...opcao) *cenario {
	t.Helper()
	c := &cenario{
		t: t, nuvem: novaNuvemFalsa(t), inst: novaInstalacao(), cofre: chaves.NovaMemoria(),
		clk: &relogio{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{}, logMu: &sync.Mutex{},
	}
	cfg := nuvem.Config{
		Cofre: c.cofre, NomeNoCofre: nuvem.NomeNoCofre(true), Instalacao: c.inst,
		Abrir: func(u string) error {
			c.abriuMu.Lock()
			defer c.abriuMu.Unlock()
			c.abertos = append(c.abertos, u)
			return c.erroAbre
		},
		HTTP: c.nuvem.cliente(), Agora: c.clk.agora,
		Politica: nuvem.Politica{Dev: true, Portatil: true},
		Versao:   "0.1.0-sprint2", NomeDoPC: "DELL-G15\r\n",
		NovoID: func() string {
			c.ids++
			return fmt.Sprintf("0192f0a1-0000-7000-8000-%012d", c.ids)
		},
		Log: slog.New(slog.NewTextHandler(logSeguro{c.logMu, c.logs}, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	for _, o := range ops {
		o(&cfg)
	}
	c.s = nuvem.Novo(cfg)
	c.s.UsarPorta(portaDoPainel)
	return c
}

func (c *cenario) textoDoLog() string {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	return c.logs.String()
}

func (c *cenario) abertosAgora() []string {
	c.abriuMu.Lock()
	defer c.abriuMu.Unlock()
	return append([]string(nil), c.abertos...)
}

func (c *cenario) conectar(bruta string) nuvem.Estado {
	c.t.Helper()
	e, err := c.s.Conectar(context.Background(), bruta)
	if err != nil {
		c.t.Fatalf("Conectar: %v", err)
	}
	return e
}

// retorno é o navegador chegando ao 127.0.0.1 do PC.
func (c *cenario) retorno(code, state string) *httptest.ResponseRecorder {
	c.t.Helper()
	q := url.Values{}
	if code != "" {
		q.Set("code", code)
	}
	if state != "" {
		q.Set("state", state)
	}
	rr := httptest.NewRecorder()
	c.s.Retorno().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:51234/nuvem/retorno?"+q.Encode(), nil))
	return rr
}

func (c *cenario) estado() nuvem.Estado {
	c.t.Helper()
	e, err := c.s.Estado(context.Background())
	if err != nil {
		c.t.Fatalf("Estado: %v", err)
	}
	return e
}

// conectarDeVez faz o caminho inteiro: Conectar, aprovar e o retorno.
func (c *cenario) conectarDeVez() string {
	c.t.Helper()
	c.conectar(urlDaNuvem)
	rr := c.retorno(c.nuvem.aprovar(), pedidoFalso)
	if rr.Code != http.StatusOK {
		c.t.Fatalf("retorno = %d, quero 200", rr.Code)
	}
	tok, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil)
	if err != nil {
		c.t.Fatalf("o token não foi guardado: %v", err)
	}
	return tok
}

func querErro(t *testing.T, err error, status int, msg string) {
	t.Helper()
	var e *nuvem.Erro
	if !errors.As(err, &e) || e.Status != status || e.Mensagem != msg {
		t.Fatalf("erro = %v; quero %d %q", err, status, msg)
	}
}

// ─── A política, antes de qualquer rede ───

// O build de desenvolvimento fora do modo portátil não fala com a nuvem: nem
// um pedido sai, e nada é gravado (nem o install_id). O de release com http
// também não.
func TestConectar_PoliticaAntesDaRede(t *testing.T) {
	for _, caso := range []struct {
		nome  string
		pol   nuvem.Politica
		url   string
		frase string
	}{
		{"dev instalado", nuvem.Politica{Dev: true}, urlDaNuvem, nuvem.MsgSoPortatil},
		{"dev instalado, https", nuvem.Politica{Dev: true}, "https://nuvem.exemplo.test", nuvem.MsgSoPortatil},
		{"release com http", nuvem.Politica{Portatil: true}, urlDaNuvem, nuvem.MsgUseHTTPS},
		{"dev com http fora do localhost", nuvem.Politica{Dev: true, Portatil: true}, "http://127.0.0.1:8080", nuvem.MsgHTTPSOuLocalhost},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			c := novoCenario(t, comPolitica(caso.pol))
			_, err := c.s.Conectar(context.Background(), caso.url)
			querErro(t, err, http.StatusBadRequest, caso.frase)
			if p, _, _ := c.nuvem.contar(); p != 0 {
				t.Errorf("%d pedidos à nuvem; quero 0", p)
			}
			if len(c.abertosAgora()) != 0 {
				t.Error("o navegador abriu")
			}
			if tudo := c.inst.tudo(); tudo != "{}" {
				t.Errorf("a instalação foi gravada: %s", tudo)
			}
		})
	}
}

// O release conecta com https, no modo instalado e no portátil: a política
// deixa passar, o pedido chega à nuvem, e o endereço aberto é o https.
func TestConectar_ReleaseComHTTPS(t *testing.T) {
	for _, portatil := range []bool{false, true} {
		n := novaNuvem(t, true)
		c := novoCenario(t, comPolitica(nuvem.Politica{Portatil: portatil}), func(cfg *nuvem.Config) { cfg.HTTP = n.cliente() })
		c.nuvem = n
		e := c.conectar("https://example.com")
		if p, _, _ := n.contar(); p != 1 {
			t.Errorf("portátil=%v: %d pedidos", portatil, p)
		}
		quero := "https://example.com/device/authorize?pedido=" + pedidoFalso
		if ab := c.abertosAgora(); len(ab) != 1 || ab[0] != quero || e.AbrirURL != quero {
			t.Errorf("portátil=%v: abriu %v", portatil, ab)
		}
		if rr := c.retorno(n.aprovar(), pedidoFalso); rr.Code != http.StatusOK {
			t.Errorf("portátil=%v: retorno = %d", portatil, rr.Code)
		}
	}
}

// ─── O caminho feliz: PKCE, o endereço aberto e o token guardado ───

func TestConectar_CaminhoInteiro(t *testing.T) {
	c := novoCenario(t)
	e := c.conectar("  HTTP://LocalHost:8080/ ")

	// O start: o desafio S256 de 43 caracteres, a porta do painel, o nome
	// limpo, a plataforma, a versão e um install_id UUID.
	c.nuvem.mexer(func(n *nuvemFalsa) {
		if len(n.desafio) != 43 {
			t.Errorf("code_challenge com %d caracteres", len(n.desafio))
		}
		if n.start["redirect_port"] != float64(portaDoPainel) || n.start["platform"] != "windows" ||
			n.start["device_name"] != "DELL-G15" || n.start["app_version"] != "0.1.0-sprint2" {
			t.Errorf("start = %v", n.start)
		}
		if id, _ := n.start["install_id"].(string); id != "0192f0a1-0000-7000-8000-000000000001" {
			t.Errorf("install_id = %q", id)
		}
		if _, tem := n.start["code_verifier"]; tem {
			t.Error("o verificador foi no start")
		}
	})

	// O endereço aberto é montado aqui, com a URL que a pessoa digitou: o
	// auth_url da nuvem (outro-lugar.exemplo) é ignorado.
	querAbrir := "http://localhost:8080/device/authorize?pedido=" + pedidoFalso
	if ab := c.abertosAgora(); len(ab) != 1 || ab[0] != querAbrir {
		t.Fatalf("abriu %v; quero só %s", ab, querAbrir)
	}
	if e.Estado != nuvem.Conectando || e.Conferencia != conferenciaFalsa || e.AbrirURL != querAbrir || e.URL != urlDaNuvem || e.Aviso != "" {
		t.Errorf("estado ao conectar = %+v", e)
	}
	if got := c.estado(); got != e {
		t.Errorf("Estado depois do Conectar = %+v; quero %+v", got, e)
	}

	rr := c.retorno(c.nuvem.aprovar(), pedidoFalso)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "PC conectado") {
		t.Fatalf("retorno = %d %q", rr.Code, rr.Body.String())
	}
	tok, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil)
	if err != nil || len(tok) != 43 {
		t.Fatalf("token no cofre = %q, %v", tok, err)
	}
	e = c.estado()
	if e.Estado != nuvem.Conectado || e.Email != emailFalso || e.Nome != "DELL-G15" || e.URL != urlDaNuvem || e.Aviso != "" {
		t.Errorf("estado conectado = %+v", e)
	}
	for nome, quero := range map[string]string{
		"nuvem_url": urlDaNuvem, "nuvem_conta": contaFalsa, "nuvem_email": emailFalso,
		"nuvem_aparelho": aparelhoFalso, "nuvem_nome": "DELL-G15", "nuvem_conectado_em": "2026-10-05T12:00:00Z",
	} {
		if got := c.inst.valor(nome); got != quero {
			t.Errorf("instalacao[%s] = %q; quero %q", nome, got, quero)
		}
	}

	// Desconectar e conectar de novo: o mesmo install_id (a nuvem reaproveita
	// o aparelho por ele), e o endereço aberto de novo.
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.conectar(urlDaNuvem)
	c.nuvem.mexer(func(n *nuvemFalsa) {
		if id, _ := n.start["install_id"].(string); id != "0192f0a1-0000-7000-8000-000000000001" {
			t.Errorf("install_id na segunda vez = %q", id)
		}
	})
	if len(c.abertosAgora()) != 2 {
		t.Errorf("abriu %d vezes; quero 2", len(c.abertosAgora()))
	}
}

// O verificador fica no Agent: a troca só passa com o que gera o desafio do
// start (a nuvem falsa o confere, como a de verdade). Um Conectar novo
// troca o verificador, e o code do pedido anterior não serve mais.
func TestConectar_PKCEPorPedido(t *testing.T) {
	c := novoCenario(t)
	c.conectar(urlDaNuvem)
	var desafio1 string
	c.nuvem.mexer(func(n *nuvemFalsa) { desafio1 = n.desafio })
	c.conectar(urlDaNuvem)
	var desafio2 string
	c.nuvem.mexer(func(n *nuvemFalsa) { desafio2 = n.desafio })
	if desafio1 == desafio2 {
		t.Fatal("o mesmo desafio em dois pedidos")
	}
	if rr := c.retorno(c.nuvem.aprovar(), pedidoFalso); rr.Code != http.StatusOK {
		t.Fatalf("retorno = %d", rr.Code)
	}
}

// O navegador que não abre deixa o pedido valendo: o painel mostra o código
// e o "Abrir de novo", e o retorno ainda conecta.
func TestConectar_NavegadorNaoAbriu(t *testing.T) {
	c := novoCenario(t)
	c.erroAbre = errors.New("painel: ShellExecute falhou (código 2)")
	e, err := c.s.Conectar(context.Background(), urlDaNuvem)
	if !errors.Is(err, nuvem.ErrNaoAbriu) {
		t.Fatalf("err = %v; quero ErrNaoAbriu", err)
	}
	if e.Estado != nuvem.Conectando || e.Aviso != nuvem.AvisoNaoAbriu || e.AbrirURL == "" || e.Conferencia != conferenciaFalsa {
		t.Errorf("estado = %+v", e)
	}
	if got := c.estado(); got.Aviso != nuvem.AvisoNaoAbriu {
		t.Errorf("Estado = %+v", got)
	}
	if rr := c.retorno(c.nuvem.aprovar(), pedidoFalso); rr.Code != http.StatusOK {
		t.Fatalf("retorno = %d", rr.Code)
	}
	if got := c.estado(); got.Estado != nuvem.Conectado || got.Aviso != "" {
		t.Errorf("depois do retorno = %+v", got)
	}
}

// Uma nuvem que não responde direito não cria pedido nem abre nada.
func TestConectar_NuvemQueNaoServe(t *testing.T) {
	for nome, mudar := range map[string]func(n *nuvemFalsa){
		"500":                 func(n *nuvemFalsa) { n.startStatus = http.StatusInternalServerError },
		"pedido que não é id": func(n *nuvemFalsa) { n.startCorpo = `{"pedido":"x","conferencia":"K7QM","expires_in":600}` },
		"sem conferência": func(n *nuvemFalsa) {
			n.startCorpo = `{"pedido":"` + pedidoFalso + `","conferencia":"","expires_in":600}`
		},
		"conferência estranha": func(n *nuvemFalsa) {
			n.startCorpo = `{"pedido":"` + pedidoFalso + `","conferencia":"<b>","expires_in":600}`
		},
		"sem prazo": func(n *nuvemFalsa) {
			n.startCorpo = `{"pedido":"` + pedidoFalso + `","conferencia":"K7QM","expires_in":0}`
		},
		"não é JSON":       func(n *nuvemFalsa) { n.startCorpo = `<html>` },
		"redirecionamento": func(n *nuvemFalsa) { n.redirecionarEm = "http://localhost:9/api/v1/device-auth/start" },
	} {
		t.Run(nome, func(t *testing.T) {
			c := novoCenario(t)
			c.nuvem.mexer(mudar)
			e, err := c.s.Conectar(context.Background(), urlDaNuvem)
			querErro(t, err, http.StatusBadGateway, nuvem.MsgSemNuvem)
			if e != (nuvem.Estado{}) || len(c.abertosAgora()) != 0 {
				t.Errorf("estado %+v, abertos %v", e, c.abertosAgora())
			}
			if p, _, _ := c.nuvem.contar(); p != 1 {
				t.Errorf("%d pedidos; quero 1 (sem seguir o redirecionamento)", p)
			}
			if got := c.estado(); got.Estado != nuvem.Desconectado {
				t.Errorf("Estado = %+v", got)
			}
		})
	}
}

// Sem a porta do painel (ele não subiu), não há para onde voltar.
func TestConectar_SemPainel(t *testing.T) {
	c := novoCenario(t)
	c.s.UsarPorta(0)
	_, err := c.s.Conectar(context.Background(), urlDaNuvem)
	querErro(t, err, http.StatusServiceUnavailable, nuvem.MsgPainelNaoSubiu)
	if p, _, _ := c.nuvem.contar(); p != 0 {
		t.Errorf("%d pedidos", p)
	}
}

func TestConectar_JaConectado(t *testing.T) {
	c := novoCenario(t)
	c.conectarDeVez()
	p0, _, _ := c.nuvem.contar()
	_, err := c.s.Conectar(context.Background(), urlDaNuvem)
	querErro(t, err, http.StatusConflict, nuvem.MsgJaConectado)
	if p, _, _ := c.nuvem.contar(); p != p0 {
		t.Error("pediu à nuvem já conectado")
	}
}

// ─── O retorno ao 127.0.0.1 ───

// Um state errado não consome o pedido (quem manda lixo ao 127.0.0.1 não
// derruba o pareamento de quem o começou) e não troca nada; o certo, depois,
// conecta.
func TestRetorno_StateErradoNaoConsome(t *testing.T) {
	c := novoCenario(t)
	c.conectar(urlDaNuvem)
	code := c.nuvem.aprovar()
	for _, state := range []string{"", "outro", strings.ToUpper(pedidoFalso), pedidoFalso + "x", "0192f0a1-7c3e-7d4a-9b2f-1a2b3c4d5e60"} {
		if rr := c.retorno(code, state); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Não deu para conectar") {
			t.Errorf("state %q: %d", state, rr.Code)
		}
	}
	// Um code que não tem a forma de um segredo também não consome.
	for _, ruim := range []string{"", "curto", code + "a"} {
		if rr := c.retorno(ruim, pedidoFalso); rr.Code != http.StatusBadRequest {
			t.Errorf("code %q: %d", ruim, rr.Code)
		}
	}
	if _, trocas, _ := c.nuvem.contar(); trocas != 0 {
		t.Fatalf("%d trocas com o state errado", trocas)
	}
	if e := c.estado(); e.Estado != nuvem.Conectando {
		t.Fatalf("o pedido saiu: %+v", e)
	}
	if rr := c.retorno(code, pedidoFalso); rr.Code != http.StatusOK {
		t.Fatalf("o certo depois dos errados = %d", rr.Code)
	}
}

// O certo consome o pedido uma vez, antes da rede: o segundo retorno igual
// (o Voltar do navegador, ou dois juntos) não troca de novo.
func TestRetorno_UsoUnico(t *testing.T) {
	c := novoCenario(t)
	c.conectar(urlDaNuvem)
	code := c.nuvem.aprovar()
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.retorno(code, pedidoFalso).Code == http.StatusOK {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	// Uma troca só. Os que chegaram durante a troca são recusados; os que
	// chegaram depois dela veem a mesma página "PC conectado" (abaixo).
	if _, trocas, _ := c.nuvem.contar(); trocas != 1 || ok.Load() < 1 {
		t.Fatalf("%d trocas e %d retornos certos; quero 1 troca", trocas, ok.Load())
	}
	// O mesmo retorno de novo (a página recarregada): "PC conectado" outra
	// vez, sem trocar nada.
	rr := c.retorno(code, pedidoFalso)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "PC conectado") {
		t.Errorf("o mesmo retorno de novo = %d", rr.Code)
	}
	// Outro state, com o PC conectado: recusado.
	if rr := c.retorno(code, "0192f0a1-7c3e-7d4a-9b2f-1a2b3c4d5e60"); rr.Code != http.StatusBadRequest {
		t.Errorf("outro state depois de conectar = %d", rr.Code)
	}
	if _, trocas, _ := c.nuvem.contar(); trocas != 1 {
		t.Fatalf("%d trocas depois dos retornos repetidos", trocas)
	}
	// Desconectado, o retorno velho já não diz "PC conectado".
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rr := c.retorno(code, pedidoFalso); rr.Code != http.StatusBadRequest {
		t.Errorf("o retorno velho depois de desconectar = %d", rr.Code)
	}
}

// O prazo do pedido: o da nuvem, e nunca mais que PrazoMaximoDoPedido.
func TestRetorno_Prazo(t *testing.T) {
	for _, caso := range []struct {
		expira int
		valido time.Duration
	}{{600, 10 * time.Minute}, {120, 2 * time.Minute}, {86400, nuvem.PrazoMaximoDoPedido}} {
		c := novoCenario(t)
		c.nuvem.mexer(func(n *nuvemFalsa) { n.expiraEm = caso.expira })
		c.conectar(urlDaNuvem)
		code := c.nuvem.aprovar()
		c.clk.andar(caso.valido - time.Second)
		if e := c.estado(); e.Estado != nuvem.Conectando {
			t.Errorf("expires_in %d: venceu antes: %+v", caso.expira, e)
		}
		c.clk.andar(time.Second)
		if rr := c.retorno(code, pedidoFalso); rr.Code != http.StatusBadRequest {
			t.Errorf("expires_in %d: retorno vencido = %d", caso.expira, rr.Code)
		}
		if _, trocas, _ := c.nuvem.contar(); trocas != 0 {
			t.Errorf("expires_in %d: trocou vencido", caso.expira)
		}
		if e := c.estado(); e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoVencido {
			t.Errorf("expires_in %d: estado = %+v", caso.expira, e)
		}
	}
}

// O Estado também tira o pedido vencido, sem retorno nenhum.
func TestEstado_PedidoVencido(t *testing.T) {
	c := novoCenario(t)
	c.conectar(urlDaNuvem)
	c.clk.andar(10 * time.Minute)
	if e := c.estado(); e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoVencido {
		t.Fatalf("estado = %+v", e)
	}
	if rr := c.retorno(c.nuvem.aprovar(), pedidoFalso); rr.Code != http.StatusBadRequest {
		t.Errorf("retorno depois do vencimento = %d", rr.Code)
	}
}

// A troca que não dá certo não guarda nada e diz na página e no painel.
func TestRetorno_TrocaFalha(t *testing.T) {
	for nome, mudar := range map[string]func(n *nuvemFalsa){
		"400":            func(n *nuvemFalsa) { n.trocaStatus = http.StatusBadRequest },
		"500":            func(n *nuvemFalsa) { n.trocaStatus = http.StatusInternalServerError },
		"token estranho": func(n *nuvemFalsa) { n.trocaToken = "curto" },
		"conta sem id":   func(n *nuvemFalsa) { n.contaDaVez = "x" },
	} {
		t.Run(nome, func(t *testing.T) {
			c := novoCenario(t)
			c.nuvem.mexer(mudar)
			c.conectar(urlDaNuvem)
			rr := c.retorno(c.nuvem.aprovar(), pedidoFalso)
			if rr.Code == http.StatusOK || !strings.Contains(rr.Body.String(), "Não deu para conectar") {
				t.Fatalf("retorno = %d %q", rr.Code, rr.Body.String())
			}
			if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
				t.Errorf("algo foi guardado no cofre: %v", err)
			}
			if c.inst.valor("nuvem_aparelho") != "" || c.inst.valor("nuvem_conta") != "" {
				t.Errorf("a instalação foi gravada: %s", c.inst.tudo())
			}
			if e := c.estado(); e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoFalhou {
				t.Errorf("estado = %+v", e)
			}
		})
	}
}

// O retorno é uma página fixa: só GET, sem cache, com a CSP própria, e sem
// ecoar nada do pedido.
func TestRetorno_PaginaFixa(t *testing.T) {
	c := novoCenario(t)
	c.conectar(urlDaNuvem)
	code := c.nuvem.aprovar()

	rr := httptest.NewRecorder()
	c.s.Retorno().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/nuvem/retorno?code="+code+"&state="+pedidoFalso, nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", rr.Code)
	}
	for _, rr := range []*httptest.ResponseRecorder{c.retorno(code, "errado<script>"), c.retorno(code, pedidoFalso)} {
		h := rr.Header()
		if h.Get("Cache-Control") != "no-store" || h.Get("Content-Security-Policy") != nuvem.CSPDoRetorno ||
			!strings.HasPrefix(h.Get("Content-Type"), "text/html") {
			t.Errorf("cabeçalhos = %v", h)
		}
		corpo := rr.Body.String()
		for _, nao := range []string{code, pedidoFalso, "<script>", conferenciaFalsa} {
			if strings.Contains(corpo, nao) {
				t.Errorf("a página ecoa %q", nao)
			}
		}
	}
	if !strings.Contains(nuvem.CSPDoRetorno, "default-src 'none'") || strings.Contains(nuvem.CSPDoRetorno, "script-src") {
		t.Errorf("CSP do retorno = %q", nuvem.CSPDoRetorno)
	}
}

// ─── O Cofre e o modo ───

// O token vai para o alvo do modo: o portátil (o bin\, conta de teste) e o
// instalado nunca dividem a credencial.
func TestCofre_AlvoDoModo(t *testing.T) {
	for _, portatil := range []bool{true, false} {
		alvo, outro := nuvem.NomeNoCofre(portatil), nuvem.NomeNoCofre(!portatil)
		c := novoCenario(t, comAlvo(alvo))
		c.conectar(urlDaNuvem)
		if rr := c.retorno(c.nuvem.aprovar(), pedidoFalso); rr.Code != http.StatusOK {
			t.Fatalf("retorno = %d", rr.Code)
		}
		if _, err := c.cofre.Ler(alvo); err != nil {
			t.Errorf("portátil=%v: o token não está em %s: %v", portatil, alvo, err)
		}
		if _, err := c.cofre.Ler(outro); !errors.Is(err, chaves.ErrAusente) {
			t.Errorf("portátil=%v: o token foi para %s", portatil, outro)
		}
	}
}

// Os dois alvos do token aparecem no Gerenciador como o token da nuvem, e não
// como a chave de API de um provedor (o comentário do chaves.ComentarioDe).
func TestCofre_ComentarioDosAlvos(t *testing.T) {
	for _, alvo := range []string{nuvem.NomeNoCofre(true), nuvem.NomeNoCofre(false)} {
		if got := chaves.ComentarioDe(alvo); got != "Aurora Whisper: token deste PC na nuvem" {
			t.Errorf("o comentário de %s = %q", alvo, got)
		}
	}
}

// O aparelho na instalação sem o token no cofre (apagado à mão, ou pelo
// desinstalador) é desconectado, e a instalação, limpa.
func TestEstado_SemTokenNoCofre(t *testing.T) {
	c := novoCenario(t)
	c.conectarDeVez()
	if err := c.cofre.Apagar(nuvem.NomeNoCofrePortatil); err != nil {
		t.Fatal(err)
	}
	if e := c.estado(); e.Estado != nuvem.Desconectado || e.URL != urlDaNuvem {
		t.Fatalf("estado = %+v", e)
	}
	if c.inst.valor("nuvem_aparelho") != "" {
		t.Error("o aparelho ficou na instalação")
	}
	if c.inst.valor("nuvem_conta") != contaFalsa {
		t.Error("a conta saiu da instalação (a D3 precisa dela)")
	}
}

// ─── A D3: a instalação ligada a uma conta não troca de conta ───

// Outra conta na mesma nuvem: a troca acontece (só ela diz a conta), o
// aparelho que acabou de nascer é revogado com o token novo, e nada é
// guardado.
func TestD3_OutraConta(t *testing.T) {
	c := novoCenario(t)
	outra := "0192f0a1-0000-7000-8000-0000000000bb"
	if err := c.inst.Gravar(context.Background(), map[string]string{"nuvem_url": urlDaNuvem, "nuvem_conta": outra, "nuvem_email": "outra@teste.local"}); err != nil {
		t.Fatal(err)
	}
	c.conectar(urlDaNuvem)
	rr := c.retorno(c.nuvem.aprovar(), pedidoFalso)
	if rr.Code != http.StatusConflict {
		t.Fatalf("retorno = %d", rr.Code)
	}
	var token string
	c.nuvem.mexer(func(n *nuvemFalsa) {
		token = n.token
		if len(n.deletes) != 1 || n.deletes[0] != "/api/v1/devices/"+aparelhoFalso+" "+n.token || !n.revogado {
			t.Errorf("revogação = %v (revogado %v)", n.deletes, n.revogado)
		}
	})
	if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
		t.Error("o token da outra conta foi guardado")
	}
	if c.inst.valor("nuvem_conta") != outra || c.inst.valor("nuvem_email") != "outra@teste.local" || c.inst.valor("nuvem_aparelho") != "" {
		t.Errorf("a instalação mudou: %s", c.inst.tudo())
	}
	if strings.Contains(c.inst.tudo(), token) {
		t.Error("o token foi para a instalação")
	}
	if e := c.estado(); e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoOutraConta {
		t.Errorf("estado = %+v", e)
	}
}

// A mesma conta de antes (desconectou e conecta de novo) passa.
func TestD3_MesmaConta(t *testing.T) {
	c := novoCenario(t)
	if err := c.inst.Gravar(context.Background(), map[string]string{"nuvem_url": urlDaNuvem, "nuvem_conta": contaFalsa}); err != nil {
		t.Fatal(err)
	}
	c.conectarDeVez()
	if e := c.estado(); e.Estado != nuvem.Conectado {
		t.Fatalf("estado = %+v", e)
	}
}

// Outra nuvem: recusado antes de qualquer rede.
func TestD3_OutraNuvem(t *testing.T) {
	c := novoCenario(t)
	if err := c.inst.Gravar(context.Background(), map[string]string{"nuvem_url": "https://outra.exemplo.test", "nuvem_conta": contaFalsa}); err != nil {
		t.Fatal(err)
	}
	_, err := c.s.Conectar(context.Background(), urlDaNuvem)
	querErro(t, err, http.StatusConflict, nuvem.MsgOutraConta)
	if p, _, _ := c.nuvem.contar(); p != 0 {
		t.Errorf("%d pedidos", p)
	}
}

// Outra nuvem gravada DEPOIS do Conectar e antes do retorno (dois Conectar
// para nuvens diferentes, e a troca do primeiro gravou a URL dele): a segunda
// guarda da D3, dentro da troca, recusa mesmo com a conta ausente ou igual. O
// aparelho que acabou de nascer é revogado com o token novo, e nada muda.
func TestD3_OutraNuvemGravadaDuranteOPedido(t *testing.T) {
	for nome, conta := range map[string]string{"sem conta gravada": "", "com a mesma conta": contaFalsa} {
		t.Run(nome, func(t *testing.T) {
			c := novoCenario(t)
			c.conectar(urlDaNuvem)
			antes := map[string]string{"nuvem_url": "https://outra.exemplo.test"}
			if conta != "" {
				antes["nuvem_conta"] = conta
			}
			if err := c.inst.Gravar(context.Background(), antes); err != nil {
				t.Fatal(err)
			}
			rr := c.retorno(c.nuvem.aprovar(), pedidoFalso)
			if rr.Code != http.StatusConflict {
				t.Fatalf("retorno = %d, quero 409", rr.Code)
			}
			var token string
			c.nuvem.mexer(func(n *nuvemFalsa) {
				token = n.token
				if n.trocas != 1 || len(n.deletes) != 1 || n.deletes[0] != "/api/v1/devices/"+aparelhoFalso+" "+n.token || !n.revogado {
					t.Errorf("trocas %d, revogação = %v (revogado %v)", n.trocas, n.deletes, n.revogado)
				}
			})
			if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
				t.Error("o token foi guardado")
			}
			if c.inst.valor("nuvem_url") != "https://outra.exemplo.test" || c.inst.valor("nuvem_conta") != conta || c.inst.valor("nuvem_aparelho") != "" {
				t.Errorf("a instalação mudou: %s", c.inst.tudo())
			}
			if strings.Contains(c.inst.tudo(), token) {
				t.Error("o token foi para a instalação")
			}
			if e := c.estado(); e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoOutraConta {
				t.Errorf("estado = %+v", e)
			}
		})
	}
}

// ─── Conferir: o 401 desconecta; a rede fora, não ───

func TestConferir_401Desconecta(t *testing.T) {
	c := novoCenario(t)
	c.conectarDeVez()
	// Revogado na nuvem (pelo app, em Aparelhos).
	c.nuvem.mexer(func(n *nuvemFalsa) { n.revogado = true })
	e, err := c.s.Conferir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoRevogado || e.URL != urlDaNuvem {
		t.Fatalf("estado = %+v", e)
	}
	if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
		t.Error("o token recusado ficou no cofre")
	}
	if c.inst.valor("nuvem_aparelho") != "" || c.inst.valor("nuvem_conta") != contaFalsa || c.inst.valor("nuvem_email") != emailFalso {
		t.Errorf("instalação = %s", c.inst.tudo())
	}
	// Desconectado, o Conferir não fala mais com a nuvem.
	_, _, mes0 := c.nuvem.contar()
	c.clk.andar(time.Hour)
	if _, err := c.s.Conferir(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, mes := c.nuvem.contar(); mes != mes0 {
		t.Error("conferiu desconectado")
	}
}

func TestConferir_SemRedeNaoApaga(t *testing.T) {
	for nome, mudar := range map[string]func(c *cenario){
		"503":          func(c *cenario) { c.nuvem.mexer(func(n *nuvemFalsa) { n.meStatus = http.StatusServiceUnavailable }) },
		"fora do ar":   func(c *cenario) { c.nuvem.srv.Close() },
		"403 do proxy": func(c *cenario) { c.nuvem.mexer(func(n *nuvemFalsa) { n.meStatus = http.StatusForbidden }) },
	} {
		t.Run(nome, func(t *testing.T) {
			c := novoCenario(t)
			tok := c.conectarDeVez()
			mudar(c)
			e, err := c.s.Conferir(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if e.Estado != nuvem.Conectado || e.Aviso != nuvem.AvisoSemRede {
				t.Fatalf("estado = %+v", e)
			}
			if got, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); err != nil || got != tok {
				t.Error("o token saiu sem o 401")
			}
		})
	}
}

// Uma vez a cada 30 s, no máximo: o painel chama ao abrir as Configurações e
// ao voltar à aba, e não vira laço.
func TestConferir_NoMaximoACada30s(t *testing.T) {
	c := novoCenario(t)
	c.conectarDeVez()
	for range 3 {
		if _, err := c.s.Conferir(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, mes := c.nuvem.contar(); mes != 1 {
		t.Fatalf("%d GET /me; quero 1", mes)
	}
	c.clk.andar(nuvem.IntervaloDaConferencia - time.Second)
	_, _ = c.s.Conferir(context.Background())
	if _, _, mes := c.nuvem.contar(); mes != 1 {
		t.Fatalf("%d GET /me antes dos 30 s", mes)
	}
	c.clk.andar(time.Second)
	_, _ = c.s.Conferir(context.Background())
	if _, _, mes := c.nuvem.contar(); mes != 2 {
		t.Fatalf("%d GET /me depois dos 30 s; quero 2", mes)
	}
	// O aviso de sem rede some quando a nuvem volta a responder.
	c.nuvem.mexer(func(n *nuvemFalsa) { n.meStatus = http.StatusBadGateway })
	c.clk.andar(nuvem.IntervaloDaConferencia)
	if e, _ := c.s.Conferir(context.Background()); e.Aviso != nuvem.AvisoSemRede {
		t.Fatalf("estado = %+v", e)
	}
	c.nuvem.mexer(func(n *nuvemFalsa) { n.meStatus = 0 })
	c.clk.andar(nuvem.IntervaloDaConferencia)
	if e, _ := c.s.Conferir(context.Background()); e.Estado != nuvem.Conectado || e.Aviso != "" {
		t.Fatalf("estado = %+v", e)
	}
}

// ─── Desconectar ───

// Com a nuvem no ar: revoga o próprio aparelho, com o token dele, e apaga o
// token daqui. A URL, a conta e o e-mail ficam (a D3 e o campo).
func TestDesconectar_ComNuvem(t *testing.T) {
	c := novoCenario(t)
	tok := c.conectarDeVez()
	e, err := c.s.Desconectar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e.Estado != nuvem.Desconectado || e.Aviso != "" || e.URL != urlDaNuvem {
		t.Fatalf("estado = %+v", e)
	}
	c.nuvem.mexer(func(n *nuvemFalsa) {
		if len(n.deletes) != 1 || n.deletes[0] != "/api/v1/devices/"+aparelhoFalso+" "+tok || !n.revogado {
			t.Errorf("DELETE = %v", n.deletes)
		}
	})
	if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
		t.Error("o token ficou")
	}
	if c.inst.valor("nuvem_aparelho") != "" || c.inst.valor("nuvem_url") != urlDaNuvem || c.inst.valor("nuvem_conta") != contaFalsa {
		t.Errorf("instalação = %s", c.inst.tudo())
	}
}

// Sem a nuvem: desconecta aqui do mesmo jeito, e o aviso manda revogar em
// Aparelhos. Um 401 (já revogado) ou 404 (já apagado) contam como revogado.
func TestDesconectar_SemNuvem(t *testing.T) {
	for nome, caso := range map[string]struct {
		mudar func(c *cenario)
		aviso string
	}{
		"fora do ar":  {func(c *cenario) { c.nuvem.srv.Close() }, nuvem.AvisoSemResposta},
		"500":         {func(c *cenario) { c.nuvem.mexer(func(n *nuvemFalsa) { n.deleteStatus = 500 }) }, nuvem.AvisoSemResposta},
		"já revogado": {func(c *cenario) { c.nuvem.mexer(func(n *nuvemFalsa) { n.revogado = true }) }, ""},
		"já apagado":  {func(c *cenario) { c.nuvem.mexer(func(n *nuvemFalsa) { n.deleteStatus = 404 }) }, ""},
	} {
		t.Run(nome, func(t *testing.T) {
			c := novoCenario(t)
			c.conectarDeVez()
			caso.mudar(c)
			e, err := c.s.Desconectar(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if e.Estado != nuvem.Desconectado || e.Aviso != caso.aviso {
				t.Fatalf("estado = %+v; quero o aviso %q", e, caso.aviso)
			}
			if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
				t.Error("o token ficou")
			}
		})
	}
}

// Com um pedido em curso, Desconectar é o Cancelar: o pedido sai, e o
// retorno dele não conecta mais.
func TestDesconectar_CancelaOPedido(t *testing.T) {
	c := novoCenario(t)
	c.conectar(urlDaNuvem)
	code := c.nuvem.aprovar()
	e, err := c.s.Desconectar(context.Background())
	if err != nil || e.Estado != nuvem.Desconectado || e.Aviso != "" {
		t.Fatalf("Desconectar = %+v, %v", e, err)
	}
	if rr := c.retorno(code, pedidoFalso); rr.Code != http.StatusBadRequest {
		t.Errorf("retorno do pedido cancelado = %d", rr.Code)
	}
	if _, trocas, _ := c.nuvem.contar(); trocas != 0 {
		t.Error("trocou o pedido cancelado")
	}
}

// ─── O que nunca sai daqui ───

// O token, o code, o verificador e o state não aparecem em resposta nenhuma
// do Estado, na página do retorno, no log, nem (o token) na instalação. A
// URL, o e-mail e a conferência também não vão para o log.
func TestSegredosForaDasRespostasEDoLog(t *testing.T) {
	c := novoCenario(t)
	var estados []nuvem.Estado
	estados = append(estados, c.conectar(urlDaNuvem), c.estado())
	code := c.nuvem.aprovar()
	paginas := []string{c.retorno(code, "errado").Body.String(), c.retorno(code, pedidoFalso).Body.String()}
	estados = append(estados, c.estado())
	c.clk.andar(time.Minute)
	e, _ := c.s.Conferir(context.Background())
	estados = append(estados, e)
	e, _ = c.s.Desconectar(context.Background())
	estados = append(estados, e)

	var token, verificador string
	c.nuvem.mexer(func(n *nuvemFalsa) {
		token = n.token
		// O verificador não fica com a nuvem: só o desafio. Mas o teste
		// sabe qual foi: o que a troca mandou passou pelo S256.
		verificador = n.desafio
	})
	if token == "" {
		t.Fatal("não conectou")
	}
	raw, _ := json.Marshal(estados)
	tudo := map[string]string{"estados": string(raw), "páginas": strings.Join(paginas, "\n"), "instalação": c.inst.tudo()}
	for onde, texto := range tudo {
		for nome, segredo := range map[string]string{"token": token, "code": code, "desafio": verificador} {
			if strings.Contains(texto, segredo) {
				t.Errorf("o %s está em %s", nome, onde)
			}
		}
	}
	log := c.textoDoLog()
	if log == "" {
		t.Fatal("nada no log: o teste não mede nada")
	}
	for nome, segredo := range map[string]string{
		"token": token, "code": code, "desafio": verificador, "pedido": pedidoFalso, "conferência": conferenciaFalsa,
		"e-mail": emailFalso, "url": "localhost:8080", "aparelho": aparelhoFalso, "conta": contaFalsa,
	} {
		if strings.Contains(log, segredo) {
			t.Errorf("o %s está no log:\n%s", nome, log)
		}
	}
}
