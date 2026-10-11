// Package chavestest é a suíte das chaves da IA do ditado no celular (ADR-021,
// C3a): o cofre, a ai_provider_keys e as três rotas /api/v1/ai-keys, pela API
// HTTP e direto no cloudstore.ChavesDeIA. Os mesmos casos rodam contra o
// MemoryStore (internal/server) e contra o Postgres (internal/server/pgstore),
// como a contracttest e a authtest.
//
// Nenhum caso chama um provedor de verdade: o provedor é um dublê que conta as
// chamadas e anota a chave que recebeu. O aceite do ditado (a C3b) também é
// um dublê. As chaves de cifra e as chaves das pessoas são de mentira.
//
// Este pacote é só para testes: nenhum binário o importa.
package chavestest

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Backend é o armazenamento sob teste.
type Backend struct {
	Store    cloudstore.Store
	Accounts cloudstore.Accounts
	Chaves   cloudstore.ChavesDeIA
	// NewDevice cria um aparelho do usuário com o HMAC do token dele (o 403
	// das rotas com o token de um PC).
	NewDevice func(t *testing.T, userID string, tokenHMAC []byte) string
	// Despejo devolve, como texto, tudo o que está guardado: no Postgres, as
	// linhas de todas as tabelas (o bytea sai em hex, \x...); na memória, os
	// bytes em hex e crus. O canário procura a chave nele, em claro e em hex.
	Despejo func(t *testing.T) string
}

// As chaves de cifra da suíte, de mentira (64 caracteres hexadecimais).
var (
	ChaveDeCifraA = strings.Repeat("a1b2c3d4", 8)
	ChaveDeCifraB = strings.Repeat("9f8e7d6c", 8)
)

// As chaves do servidor da suíte (de mentira).
var (
	segredoDeSessao = []byte("segredo-de-sessao-da-suite-das-chaves-0123456789")
	pepperDeToken   = []byte("pepper-de-token-da-suite-das-chaves-0123456789ab")
)

var agora0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// aceiteFalso é o AceiteDoDitado da suíte (o de verdade é da C3b): o aceite
// vigente por conta e provedor, que o caso dá e tira.
type aceiteFalso struct {
	mu       sync.Mutex
	vigente  map[string]bool
	err      error
	chamadas int
}

func (a *aceiteFalso) AceiteVigente(_ context.Context, userID, provedor string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chamadas++
	return a.vigente[userID+"|"+provedor], a.err
}

func (a *aceiteFalso) dar(userID string, provedores ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range provedores {
		a.vigente[userID+"|"+p] = true
	}
}

func (a *aceiteFalso) tirar(userID string, provedores ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range provedores {
		delete(a.vigente, userID+"|"+p)
	}
}

func (a *aceiteFalso) falhar(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.err = err
}

// chamada é uma chamada ao provedor falso.
type chamada struct {
	provedor, chave string
}

// provedorFalso é o ModelosDoProvedor da suíte: conta as chamadas, anota a
// chave que recebeu e devolve a lista, ou a falha que o caso pôs.
type provedorFalso struct {
	mu       sync.Mutex
	chamadas []chamada
	modelos  []string
	// falha, se não for nil, é o erro de cada chamada, montado com a chave
	// que chegou (o caso do provedor que devolve a chave no erro).
	falha func(chave string) error
}

func (p *provedorFalso) ListarModelos(_ context.Context, provedor, chave string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chamadas = append(p.chamadas, chamada{provedor, chave})
	if p.falha != nil {
		return nil, p.falha(chave)
	}
	return append([]string(nil), p.modelos...), nil
}

func (p *provedorFalso) quantas() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.chamadas)
}

func (p *provedorFalso) ultima() chamada {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.chamadas) == 0 {
		return chamada{}
	}
	return p.chamadas[len(p.chamadas)-1]
}

func (p *provedorFalso) falharCom(f func(chave string) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.falha = f
}

// mundo é a nuvem montada sobre o Backend, com duas contas (A, com um PC, e
// B), uma sessão de cada, o aceite e o provedor falsos, o relógio e o log.
type mundo struct {
	t      *testing.T
	b      Backend
	clk    *clock.Fake
	logs   *contracttest.LockedBuffer
	aceite *aceiteFalso
	prov   *provedorFalso
	contas cloudauth.Contas

	cloud *server.Cloud
	h     http.Handler
	cofre *cofre.Cofre
	// semAceiteNaNuvem monta a nuvem sem o AceiteDoDitado (nil na
	// configuração: como o serve até a C3b ligar o dela).
	semAceiteNaNuvem bool

	userA, userB string
	creds        map[string]credencial
}

type credencial struct {
	valor  string
	bearer bool
}

// As credenciais pelo nome.
const (
	celA = "cel-a"
	celB = "cel-b"
	pcA  = "pc-a"
)

func novoMundo(t *testing.T, b Backend) *mundo {
	t.Helper()
	ctx := context.Background()
	m := &mundo{
		t: t, b: b, clk: clock.NewFake(agora0), logs: &contracttest.LockedBuffer{},
		aceite: &aceiteFalso{vigente: map[string]bool{}},
		prov:   &provedorFalso{modelos: []string{"modelo-novo", "modelo-velho"}},
		creds:  map[string]credencial{},
	}
	var emails []string
	novoUsuario := func(nome string) string {
		email := nome + "-" + contracttest.NewID()[:8] + "@exemplo.test"
		emails = append(emails, email)
		u, err := b.Accounts.EntrarComGoogle(ctx, cloudstore.Identidade{Sub: "sub-" + contracttest.NewID(), Email: email}, agora0)
		if err != nil {
			t.Fatalf("criar o usuário %s: %v", nome, err)
		}
		return u.ID
	}
	novaSessao := func(userID, nome string) {
		tok := cloudauth.NovoSegredo()
		m.creds[nome] = credencial{valor: tok}
		if _, err := b.Accounts.CriarSessao(ctx, cloudstore.NovaSessao{
			UserID: userID, TokenHMAC: cloudauth.HMAC(segredoDeSessao, tok), Rotulo: "Android · Chrome",
			Agora: agora0, ExpiraEm: agora0.Add(server.SessaoSemUso),
		}); err != nil {
			t.Fatalf("criar a sessão %s: %v", nome, err)
		}
	}
	m.userA, m.userB = novoUsuario("a"), novoUsuario("b")
	novaSessao(m.userA, celA)
	novaSessao(m.userB, celB)
	tok := cloudauth.NovoSegredo()
	m.creds[pcA] = credencial{valor: tok, bearer: true}
	b.NewDevice(t, m.userA, cloudauth.HMAC(pepperDeToken, tok))
	contas, err := cloudauth.ParseContas(strings.Join(emails, ","))
	if err != nil {
		t.Fatal(err)
	}
	m.contas = contas
	m.montar(ChaveDeCifraA)
	return m
}

// montar (re)monta a nuvem sobre o mesmo Backend com a chave de cifra dada
// (vazia: sem o arquivo), o mesmo aceite, o mesmo provedor e o mesmo log. É o
// servidor reiniciado com outro AI_KEYS_SECRET_FILE.
func (m *mundo) montar(chaveDeCifra string) {
	m.t.Helper()
	m.cofre = nil
	if chaveDeCifra != "" {
		c, err := cofre.Novo(chaveDeCifra)
		if err != nil {
			m.t.Fatal(err)
		}
		m.cofre = c
	}
	var aceite server.AceiteDoDitado = m.aceite
	if m.semAceiteNaNuvem {
		aceite = nil
	}
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: m.b.Store, Accounts: m.b.Accounts, BaseURL: contracttest.BaseURL,
		SessionSecret: segredoDeSessao, DeviceTokenPepper: pepperDeToken, Contas: m.contas,
		Google: cloudauth.NovoGoogle("cliente-da-suite", "segredo-da-suite", contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Now: m.clk.Now,
		Logger: slog.New(slog.NewTextHandler(m.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Cofre:  m.cofre, ChavesDeIA: m.b.Chaves, Modelos: m.prov, Aceite: aceite,
	})
	if err != nil {
		m.t.Fatalf("NewCloud: %v", err)
	}
	m.cloud, m.h = cloud, cloud.Handler()
}

// pedir faz um pedido à nuvem (sem rede) com a credencial pelo nome, e o corpo
// cru (para o JSON quebrado ir como está).
func (m *mundo) pedir(quem, method, path, corpo string) *httptest.ResponseRecorder {
	m.t.Helper()
	var r *http.Request
	if corpo == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(corpo))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Host = contracttest.HostNuvem
	if c, ok := m.creds[quem]; ok {
		if c.bearer {
			r.Header.Set("Authorization", "Bearer "+c.valor)
		} else {
			r.AddCookie(&http.Cookie{Name: "__Host-sessao", Value: c.valor})
			r.Header.Set("Sec-Fetch-Site", "same-origin")
		}
	}
	rec := httptest.NewRecorder()
	m.h.ServeHTTP(rec, r)
	return rec
}

// put grava a chave do provedor pela rota.
func (m *mundo) put(quem, provedor, chave string) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.pedir(quem, http.MethodPut, "/api/v1/ai-keys/"+provedor, corpoDaChave(m.t, chave))
}

func (m *mundo) modelos(quem, provedor string) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.pedir(quem, http.MethodGet, "/api/v1/ai-keys/"+provedor+"/models", "")
}

func (m *mundo) apagar(quem, provedor string) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.pedir(quem, http.MethodDelete, "/api/v1/ai-keys/"+provedor, "")
}

func corpoDaChave(t *testing.T, chave string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"chave": chave})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// erroDe lê o bloco de erro da resposta.
func erroDe(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Error.Code == "" {
		t.Fatalf("a resposta %d não é um erro do contrato: %s", rec.Code, rec.Body.String())
	}
	return env.Error.Code, env.Error.Message
}

// exigirErro confere o status e o código da resposta.
func exigirErro(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, quero %d: %s", rec.Code, status, rec.Body.String())
	}
	got, msg := erroDe(t, rec)
	if got != code {
		t.Fatalf("código %q, quero %q (%s)", got, code, msg)
	}
	return msg
}

type chaveDTO struct {
	Provedor    string `json:"provedor"`
	Configurada bool   `json:"configurada"`
	Final       string `json:"final"`
}

type modelosDTO struct {
	Provedor string   `json:"provedor"`
	Modelos  []string `json:"modelos"`
}

func decodificar[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("a resposta %d não é o JSON esperado: %v\n%s", rec.Code, err, rec.Body.String())
	}
	return v
}

// linha lê a linha guardada da conta, direto no ChavesDeIA.
func (m *mundo) linha(userID, provedor string) (cloudstore.ChaveDeIA, error) {
	return m.b.Chaves.LerChaveDeIA(context.Background(), userID, provedor)
}
