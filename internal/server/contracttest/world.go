// Package contracttest é a suíte de contrato da etapa 5 (ADR-019, F2): os
// mesmos testes, pela API HTTP (internal/server montado sobre um
// cloudstore.Store), rodam contra o Store em memória e contra o Postgres
// (internal/server/pgstore). Assim o servidor em memória e o de Postgres têm
// o mesmo contrato por construção, e não por disciplina.
//
// Este pacote é só para testes: nenhum binário o importa.
package contracttest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Backend é o Store sob teste e o que a suíte precisa para preparar dados que
// a API ainda não cria: o aparelho e o token dele são do pareamento (F6). O
// usuário e a sessão vêm do Accounts, como no login de verdade (F3).
type Backend struct {
	Store    cloudstore.Store
	Accounts cloudstore.Accounts
	// NewDevice cria um aparelho do usuário e grava o HMAC do token dele.
	NewDevice func(t *testing.T, userID string, tokenHMAC []byte) string
	// Events devolve os eventos de sync do usuário, em ordem de seq.
	Events func(t *testing.T, userID string) []cloudstore.Event
	// NoteEvents devolve os eventos das notas e dos marcadores de nota (a
	// note_sync_events, N7), em ordem de seq. Nil: o caso da arrumação deles
	// pula.
	NoteEvents func(t *testing.T, userID string) []cloudstore.Event
	// AntesDaTrava liga f para rodar antes de cada candidato do Reter ser
	// decidido de novo sob a trava (o gancho de teste do Store), até o fim do
	// teste; f pode chamar o Store. Nil: os casos que precisam dele pulam
	// essa parte.
	AntesDaTrava func(t *testing.T, f func(id string))
	// Fotografar copia o banco inteiro agora (o backup da noite) e devolve a
	// restauração dele, no lugar (o pg_restore): o caso da restauração (F11d;
	// restauracao.go). Nil: o caso pula.
	Fotografar func(t *testing.T) (restaurar func(t *testing.T))
}

// Credenciais da suíte, pelo nome. "pc" é o token de um aparelho (Bearer);
// "cel" é a sessão do navegador (o celular), no cookie __Host-sessao.
const (
	pcA1 = "pc-a1"
	pcA2 = "pc-a2"
	celA = "cel-a"
	pcB  = "pc-b"
	celB = "cel-b"
)

// A origem e as chaves do servidor da suíte (de teste, nunca de verdade). Com
// a build tag logindeteste, o servidor só sobe em localhost, e a suíte também.
var (
	BaseURL   = baseDaSuite()
	HostNuvem = strings.TrimPrefix(strings.TrimPrefix(BaseURL, "https://"), "http://")
)

func baseDaSuite() string {
	if server.LoginDeTesteCompilado {
		return "http://localhost:8080"
	}
	return "https://nuvem.exemplo.test"
}

var (
	SegredoDeSessao = []byte("segredo-de-sessao-da-suite-de-contrato-0123456789")
	PepperDeToken   = []byte("pepper-de-token-da-suite-de-contrato-0123456789ab")
)

// PainelDeTeste é um build do painel de mentira (o de verdade é o
// web/dist-nuvem): o index.html, os arquivos da raiz (os do app instalável da
// F5 inclusive: o sw.js, o manifest e um ícone) e três de assets/, o bastante
// para o servidor subir e servir o app (ADR-019, F4 e F5).
func PainelDeTeste() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<!doctype html><html lang="pt-BR"><head><meta charset="UTF-8"><title>Aurora Whisper</title>` +
			`<script type="module" crossorigin src="/assets/index-teste1.js"></script>` +
			`<link rel="stylesheet" crossorigin href="/assets/index-teste2.css">` +
			`<link rel="manifest" href="/manifest.webmanifest"></head><body><div id="root"></div></body></html>`)},
		"favicon.svg":                     {Data: []byte(`<svg viewBox="0 0 32 32"><path d="M16 4V28"/></svg>`)},
		"licencas-fontes.txt":             {Data: []byte("SIL Open Font License 1.1")},
		"sw.js":                           {Data: []byte(`'use strict'; self.addEventListener('fetch', () => {})`)},
		"manifest.webmanifest":            {Data: []byte(`{"id":"/","name":"Aurora Whisper","icons":[{"src":"/icone-192.png","sizes":"192x192"}]}`)},
		"icone-192.png":                   {Data: []byte("\x89PNG\r\n\x1a\nicone de teste")},
		"assets/index-teste1.js":          {Data: []byte(`console.log("app")`)},
		"assets/index-teste2.css":         {Data: []byte(`body{margin:0}`)},
		"assets/inter-latin-teste3.woff2": {Data: []byte("wOF2")},
	}
}

type credencial struct {
	valor  string
	bearer bool
}

// now0 é o relógio da suíte.
var now0 = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// world são dois usuários (A, com dois PCs e o celular; B, com um PC e o
// celular) sobre um servidor montado com o Store.
type world struct {
	t     *testing.T
	b     Backend
	h     http.Handler
	clk   *clock.Fake
	userA string
	userB string
	devA1 string
	devA2 string
	devB  string
	creds map[string]credencial
	// logs é tudo o que o servidor registrou (o log de acesso e o login).
	logs *LockedBuffer
	// newDevice cria mais um aparelho do usuário, com a credencial pelo nome
	// (depois do "apagar meus dados", os de antes não valem mais).
	newDevice func(userID, name string) string
}

func newWorld(t *testing.T, b Backend) *world {
	t.Helper()
	ctx := context.Background()
	w := &world{t: t, b: b, clk: clock.NewFake(now0), creds: map[string]credencial{}, logs: &LockedBuffer{}}
	var emails []string
	newUser := func(nome string) string {
		email := nome + "-" + newID()[:8] + "@exemplo.test"
		emails = append(emails, email)
		u, err := b.Accounts.EntrarComGoogle(ctx, cloudstore.Identidade{Sub: "sub-" + newID(), Email: email}, now0)
		if err != nil {
			t.Fatalf("criar o usuário %s: %v", nome, err)
		}
		return u.ID
	}
	newDevice := func(userID, name string) string {
		tok := cloudauth.NovoSegredo()
		w.creds[name] = credencial{valor: tok, bearer: true}
		return b.NewDevice(t, userID, cloudauth.HMAC(PepperDeToken, tok))
	}
	newSession := func(userID, name string) {
		tok := cloudauth.NovoSegredo()
		w.creds[name] = credencial{valor: tok}
		if _, err := b.Accounts.CriarSessao(ctx, cloudstore.NovaSessao{
			UserID: userID, TokenHMAC: cloudauth.HMAC(SegredoDeSessao, tok), Rotulo: "Android · Chrome",
			Agora: now0, ExpiraEm: now0.Add(server.SessaoSemUso),
		}); err != nil {
			t.Fatalf("criar a sessão %s: %v", name, err)
		}
	}
	w.userA, w.userB = newUser("a"), newUser("b")
	w.newDevice = newDevice
	w.devA1, w.devA2 = newDevice(w.userA, pcA1), newDevice(w.userA, pcA2)
	w.devB = newDevice(w.userB, pcB)
	newSession(w.userA, celA)
	newSession(w.userB, celB)

	contas, err := cloudauth.ParseContas(strings.Join(emails, ","))
	if err != nil {
		t.Fatal(err)
	}
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: b.Store, Accounts: b.Accounts, BaseURL: BaseURL,
		SessionSecret: SegredoDeSessao, DeviceTokenPepper: PepperDeToken, Contas: contas,
		Google: cloudauth.NovoGoogle("cliente-da-suite", "segredo-da-suite", BaseURL+"/auth/google/callback"),
		Painel: PainelDeTeste(),
		Now:    w.clk.Now, Logger: slog.New(slog.NewTextHandler(w.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		// A suíte de contrato não testa os limites (a authtest testa) e
		// manda centenas de pushes com o relógio parado.
		Limites: server.Limites{PushPedidos: 1 << 20, PushMutacoes: 1 << 30},
	})
	if err != nil {
		t.Fatalf("NewCloud: %v", err)
	}
	w.h = cloud.Handler()
	return w
}

// LockedBuffer é um bytes.Buffer seguro para uso concorrente (o destino dos
// logs nos testes).
type LockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *LockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *LockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// NewID devolve um UUID novo, em minúsculas (para os Backends criarem
// usuários e aparelhos).
func NewID() string { return newID() }

// newID devolve um UUID novo, em minúsculas.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// do faz uma requisição ao servidor (sem rede) e devolve a resposta.
func (w *world) do(method, path, token string, body any) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.doWith(method, path, token, body, nil)
}

// doWith é do com um ajuste no pedido antes de servi-lo (um cabeçalho de outra
// origem, um Host estranho).
func (w *world) doWith(method, path, token string, body any, mudar func(*http.Request)) *httptest.ResponseRecorder {
	w.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			w.t.Fatalf("marshal: %v", err)
		}
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = HostNuvem
	if c, ok := w.creds[token]; ok {
		if c.bearer {
			req.Header.Set("Authorization", "Bearer "+c.valor)
		} else {
			req.AddCookie(&http.Cookie{Name: "__Host-sessao", Value: c.valor})
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if mudar != nil {
		mudar(req)
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("resposta não é o JSON esperado (%d): %v\n%s", rec.Code, err, rec.Body.String())
	}
	return v
}

// mut é uma mutação do push, como o PC a manda.
type mut struct {
	MutationID  string `json:"mutation_id"`
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	Operation   string `json:"operation"`
	BaseVersion int64  `json:"base_version"`
	Payload     any    `json:"payload,omitempty"`
}

// dictation é o payload de um ditado novo, ditado uma hora antes do relógio.
func dictation(text string) map[string]any {
	return map[string]any{
		"source_platform": "windows",
		"raw_text":        "bruto: " + text,
		"final_text":      text,
		"mode":            "smart",
		"language":        "pt-BR",
		"asr_provider":    "whisper-local",
		"asr_model":       "small-q4_0",
		"duration_ms":     2500,
		"processing_ms":   300,
		"confidence":      0.9,
		"status":          "done",
		"created_at":      now0.Add(-time.Hour).Format(time.RFC3339Nano),
	}
}

func create(id, text string) mut {
	return mut{MutationID: newID(), EntityType: "transcription", EntityID: id, Operation: "upsert", Payload: dictation(text)}
}

func createWith(id string, payload map[string]any) mut {
	return mut{MutationID: newID(), EntityType: "transcription", EntityID: id, Operation: "upsert", Payload: payload}
}

func change(id string, base int64, fields map[string]any) mut {
	return mut{MutationID: newID(), EntityType: "transcription", EntityID: id, Operation: "upsert", BaseVersion: base, Payload: fields}
}

type conflictJSON struct {
	FinalText  string    `json:"final_text"`
	DeviceID   *string   `json:"device_id"`
	ConflictAt time.Time `json:"conflict_at"`
}

type itemJSON struct {
	ID               string        `json:"id"`
	DeviceID         *string       `json:"device_id"`
	SourcePlatform   string        `json:"source_platform"`
	RawText          string        `json:"raw_text"`
	FinalText        string        `json:"final_text"`
	Mode             string        `json:"mode"`
	Status           string        `json:"status"`
	Confidence       *float64      `json:"confidence"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	DeletedAt        *time.Time    `json:"deleted_at"`
	Version          int64         `json:"version"`
	VersaoEmConflito *conflictJSON `json:"versao_em_conflito"`
}

type resultJSON struct {
	MutationID    string    `json:"mutation_id"`
	Status        string    `json:"status"`
	Original      string    `json:"original"`
	EntityVersion int64     `json:"entity_version"`
	Mesclado      bool      `json:"mesclado"`
	Code          string    `json:"code"`
	Motivo        string    `json:"motivo"`
	State         *itemJSON `json:"state"`
}

type pushJSON struct {
	Results   []resultJSON `json:"results"`
	SyncEpoch string       `json:"sync_epoch"`
}

type changeJSON struct {
	Seq           int64     `json:"seq"`
	EntityType    string    `json:"entity_type"`
	EntityID      string    `json:"entity_id"`
	Operation     string    `json:"operation"`
	EntityVersion int64     `json:"entity_version"`
	DeviceID      *string   `json:"device_id"`
	Payload       *itemJSON `json:"payload"`
}

type pullJSON struct {
	Changes   []changeJSON `json:"changes"`
	LastSeq   int64        `json:"last_seq"`
	HasMore   bool         `json:"has_more"`
	SyncEpoch string       `json:"sync_epoch"`
}

// pushRaw manda as mutações e devolve a resposta crua (status e corpo).
func (w *world) pushRaw(token string, muts ...mut) *httptest.ResponseRecorder {
	w.t.Helper()
	if muts == nil {
		muts = []mut{}
	}
	return w.do(http.MethodPost, "/api/v1/sync/push", token, map[string]any{"mutations": muts})
}

func (w *world) push(token string, muts ...mut) pushJSON {
	w.t.Helper()
	rec := w.pushRaw(token, muts...)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("push: status %d, quero 200: %s", rec.Code, rec.Body.String())
	}
	p := decode[pushJSON](w.t, rec)
	if len(p.Results) != len(muts) {
		w.t.Fatalf("push: %d resultados para %d mutações", len(p.Results), len(muts))
	}
	for i, r := range p.Results {
		if r.MutationID != muts[i].MutationID {
			w.t.Fatalf("push: resultado %d é de %q, quero %q (a ordem é a das mutações)", i, r.MutationID, muts[i].MutationID)
		}
	}
	return p
}

func (w *world) pushOne(token string, m mut) resultJSON {
	w.t.Helper()
	return w.push(token, m).Results[0]
}

func (w *world) pull(token string, after int64, limit int) pullJSON {
	w.t.Helper()
	path := "/api/v1/sync/pull?after_seq=" + strconv.FormatInt(after, 10)
	if limit > 0 {
		path += "&limit=" + strconv.Itoa(limit)
	}
	rec := w.do(http.MethodGet, path, token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("pull: status %d, quero 200: %s", rec.Code, rec.Body.String())
	}
	return decode[pullJSON](w.t, rec)
}

func (w *world) get(token, id string) itemJSON {
	w.t.Helper()
	rec := w.do(http.MethodGet, "/api/v1/transcriptions/"+id, token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET %s: status %d: %s", id, rec.Code, rec.Body.String())
	}
	return decode[itemJSON](w.t, rec)
}

func (w *world) edit(token, id string, base int64, text string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.do(http.MethodPatch, "/api/v1/transcriptions/"+id, token, map[string]any{"base_version": base, "final_text": text})
}

// mustApplied cria o ditado pelo PC e confere o applied v1.
func (w *world) mustCreate(token, id, text string) {
	w.t.Helper()
	if r := w.pushOne(token, create(id, text)); r.Status != "applied" || r.EntityVersion != 1 {
		w.t.Fatalf("criar %s: %+v, quero applied v1", id, r)
	}
}

// changeOf acha a mudança de uma entidade no pull.
func changeOf(p pullJSON, id string) (changeJSON, bool) {
	for _, c := range p.Changes {
		if c.EntityID == id {
			return c, true
		}
	}
	return changeJSON{}, false
}

// Versões para goroutines: sem t.Fatal (do e json.Marshal só falhariam com
// um valor que não vira JSON, e os daqui viram).
func (w *world) pushRawNoFatal(token string, muts ...mut) *httptest.ResponseRecorder {
	return w.pushRaw(token, muts...)
}

func (w *world) editNoFatal(token, id string, base int64, text string) *httptest.ResponseRecorder {
	return w.edit(token, id, base, text)
}

func decodeNoFatal(rec *httptest.ResponseRecorder) pullJSON {
	var p pullJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p
}

func decodeNoFatalPush(rec *httptest.ResponseRecorder) pushJSON {
	var p pushJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p
}

// seqsOf são os seq dos eventos do usuário.
func (w *world) seqsOf(userID string) []int64 {
	w.t.Helper()
	var seqs []int64
	for _, e := range w.b.Events(w.t, userID) {
		seqs = append(seqs, e.Seq)
	}
	return seqs
}

// requireContiguous confere que os seq são 1..n, sem buraco nem repetição.
func requireContiguous(t *testing.T, seqs []int64, n int) {
	t.Helper()
	if len(seqs) != n {
		t.Fatalf("%d eventos, quero %d: %v", len(seqs), n, seqs)
	}
	for i, s := range seqs {
		if s != int64(i+1) {
			t.Fatalf("seq %d na posição %d, quero %d (buraco ou repetição): %v", s, i, i+1, seqs)
		}
	}
}
