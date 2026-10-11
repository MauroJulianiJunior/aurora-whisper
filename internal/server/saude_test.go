package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// A saúde do job de retenção e o aviso do X-Forwarded-For (F11c; ADR-019,
// "Decisões da F11"). Sem banco: o Store em memória, às vezes embrulhado para
// falhar (storeQueFalha, retencao_test.go).

// O resumo da rodada: as falhas de banco e de outra categoria são graves; a de
// contexto (o desligamento no meio) não. O resumo volta e fica guardado para a
// rota, com a idade no relógio da nuvem.
func TestReter_OResumoDaRodadaEASaude(t *testing.T) {
	ctx := context.Background()
	mem := cloudstore.NewMemoryStore()
	a, b, c := contracttest.NewID(), contracttest.NewID(), contracttest.NewID()
	for _, u := range []string{a, b, c} {
		mem.AddUser(u)
	}
	agora := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	relogio := agora
	s := &storeQueFalha{MemoryStore: mem, falhar: map[string]error{}}
	var logs contracttest.LockedBuffer
	cloud := novaNuvemDoJob(t, s, mem, &logs, func() time.Time { return relogio })

	if got := cloud.SaudeDaRetencao(); got != (server.SaudeDaRetencao{}) {
		t.Fatalf("antes da primeira rodada = %+v, quero tudo zero", got)
	}
	if r := cloud.Reter(ctx); r != (server.RodadaDaRetencao{Usuarios: 3}) {
		t.Fatalf("a rodada sem falha = %+v", r)
	}
	relogio = agora.Add(90 * time.Minute)
	if got := cloud.SaudeDaRetencao(); got != (server.SaudeDaRetencao{Rodadas: 1, UltimaHaS: 5400}) {
		t.Fatalf("a saúde 90 min depois = %+v", got)
	}

	s.falhar[a] = erroDoDriver{cru: "ERROR: deadlock"}
	s.falhar[b] = errors.New("pgstore: outra coisa")
	if r := cloud.Reter(ctx); r != (server.RodadaDaRetencao{Usuarios: 3, Falhas: 2, FalhasGraves: 2}) {
		t.Fatalf("a rodada com banco e outro = %+v", r)
	}
	if got := cloud.SaudeDaRetencao(); got != (server.SaudeDaRetencao{Rodadas: 2, FalhasGraves: 2}) {
		t.Fatalf("a saúde depois da rodada com falha = %+v", got)
	}

	// O contexto (o desligamento no meio da rodada, ou o prazo): a falha conta,
	// mas não é grave, e a rodada, sem outra falha, limpa as graves.
	s.falhar = map[string]error{a: context.Canceled}
	if r := cloud.Reter(ctx); r != (server.RodadaDaRetencao{Usuarios: 3, Falhas: 1}) {
		t.Fatalf("a rodada com a falha de contexto = %+v", r)
	}
	if got := cloud.SaudeDaRetencao(); got.Rodadas != 3 || got.FalhasGraves != 0 {
		t.Fatalf("a saúde depois do contexto = %+v", got)
	}
	s.falhar = map[string]error{}

	// A listagem que falha também é grave.
	s.listar = errors.New("pgstore: listar")
	if r := cloud.Reter(ctx); r.FalhasGraves != 1 || r.Usuarios != 0 {
		t.Fatalf("a listagem que falha = %+v", r)
	}
}

// A rota da saúde do job: só para quem pede pela própria máquina, e sem
// cabeçalho de proxy. Do proxy (outro contêiner ou outro servidor), ou com o
// X-Forwarded-For de um proxy na mesma máquina, é o 404 de rota não montada,
// com o mesmo corpo. A resposta tem só números.
func TestRotaDaSaudeDoJob_SoDaPropriaMaquina(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	var logs contracttest.LockedBuffer
	agora := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	relogio := agora
	cloud := novaNuvemDoJob(t, mem, mem, &logs, func() time.Time { return relogio })
	cloud.Reter(context.Background())
	relogio = agora.Add(30 * time.Second)
	h := cloud.Handler()

	pedir := func(remote string, mexer func(r *http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/health/retencao", nil)
		r.RemoteAddr = remote
		// Um Host qualquer: o health não confere o Host (a checagem bate no IP).
		r.Host = "127.0.0.1:8080"
		if mexer != nil {
			mexer(r)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	// O corpo do erro, sem o request_id (que muda a cada pedido).
	semID := func(corpo []byte) string {
		var e struct {
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal(corpo, &e); err != nil {
			return "não é JSON: " + string(corpo)
		}
		delete(e.Error, "request_id")
		out, _ := json.Marshal(e.Error)
		return string(out)
	}
	naoMontada := func() string {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/nada-montado", nil)
		r.Host = contracttest.HostNuvem
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return semID(rec.Body.Bytes())
	}()

	for _, remote := range []string{"127.0.0.1:5000", "[::1]:5000", "127.0.0.9:1", "[::ffff:127.0.0.1]:5000"} {
		rec := pedir(remote, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("de %s: %d, quero 200", remote, rec.Code)
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if strings.Join(chaves(m), ",") != "falhas_graves,rodadas,ultima_ha_s" {
			t.Fatalf("as chaves = %v", chaves(m))
		}
		if m["rodadas"] != float64(1) || m["ultima_ha_s"] != float64(30) || m["falhas_graves"] != float64(0) {
			t.Errorf("de %s: %v", remote, m)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("sem o no-store: %v", rec.Header())
		}
	}
	for nome, c := range map[string]struct {
		remote string
		mexer  func(r *http.Request)
	}{
		"de outro contêiner":                {"172.18.0.7:40000", nil},
		"da rede de fora":                   {"203.0.113.9:40000", nil},
		"um endereço que não é IP":          {"@socket", nil},
		"do loopback com o X-Forwarded-For": {"127.0.0.1:5000", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.9") }},
		"do loopback com o Forwarded":       {"127.0.0.1:5000", func(r *http.Request) { r.Header.Set("Forwarded", "for=203.0.113.9") }},
		"do loopback com o X-Real-Ip":       {"127.0.0.1:5000", func(r *http.Request) { r.Header.Set("X-Real-Ip", "203.0.113.9") }},
		"do loopback com o XFF vazio":       {"127.0.0.1:5000", func(r *http.Request) { r.Header["X-Forwarded-For"] = []string{""} }},
		"de um IPv6 de fora":                {"[2001:db8::1]:40000", nil},
		"de 0.0.0.0, que não é loopback":    {"0.0.0.0:40000", nil},
	} {
		rec := pedir(c.remote, c.mexer)
		if rec.Code != http.StatusNotFound || semID(rec.Body.Bytes()) != naoMontada {
			t.Errorf("%s: %d %s; quero o 404 de rota não montada (%s)", nome, rec.Code, rec.Body.String(), naoMontada)
		}
	}
}

// O WARN do X-Forwarded-For: uma vez só por processo, quando o cabeçalho chega
// de quem não é o PROXY_CONFIAVEL; sem IP nem valor de cabeçalho; diz só se
// há proxy configurado. Do proxy confiável, ou sem o cabeçalho, nada.
func TestAvisoDoXFF_UmaVezSoESemIP(t *testing.T) {
	novo := func(t *testing.T, proxy string) (http.Handler, *contracttest.LockedBuffer) {
		t.Helper()
		p, err := cloudauth.ParseProxyConfiavel(proxy)
		if err != nil {
			t.Fatal(err)
		}
		contas, err := cloudauth.ParseContas("mauro@exemplo.test")
		if err != nil {
			t.Fatal(err)
		}
		var logs contracttest.LockedBuffer
		mem := cloudstore.NewMemoryStore()
		cloud, err := server.NewCloud(server.CloudConfig{
			Store: mem, Accounts: mem, BaseURL: contracttest.BaseURL,
			SessionSecret: contracttest.SegredoDeSessao, DeviceTokenPepper: contracttest.PepperDeToken, Contas: contas,
			Google: cloudauth.NovoGoogle("cliente", "secret-do-client", contracttest.BaseURL+"/auth/google/callback"),
			Painel: contracttest.PainelDeTeste(), Proxy: p, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		return cloud.Handler(), &logs
	}
	pedir := func(h http.Handler, remote, xff string) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	const aviso = "X-Forwarded-For de quem não é o proxy confiável"
	avisos := func(logs *contracttest.LockedBuffer) []map[string]any {
		var out []map[string]any
		for _, l := range linhasCruas(t, logs.String()) {
			if strings.HasPrefix(l["msg"].(string), aviso) {
				out = append(out, l)
			}
		}
		return out
	}

	t.Run("sem PROXY_CONFIAVEL, de muitos endereços", func(t *testing.T) {
		h, logs := novo(t, "")
		pedir(h, "198.51.100.7:1000", "") // sem o cabeçalho: nada
		if n := len(avisos(logs)); n != 0 {
			t.Fatalf("sem o X-Forwarded-For saiu %d aviso(s)", n)
		}
		for i := range 5 {
			pedir(h, "198.51.100.7:1000", "203.0.113."+strconv.Itoa(i+1))
		}
		pedir(h, "198.51.100.8:1000", "203.0.113.50")
		got := avisos(logs)
		if len(got) != 1 {
			t.Fatalf("%d avisos, quero 1 só:\n%s", len(got), logs.String())
		}
		if got[0]["level"] != "WARN" || got[0]["proxy_confiavel"] != false {
			t.Errorf("o aviso = %v", got[0])
		}
		if strings.Join(chaves(got[0]), ",") != "level,msg,proxy_confiavel,time" {
			t.Errorf("as chaves do aviso = %v", chaves(got[0]))
		}
		for _, ip := range []string{"198.51.100", "203.0.113"} {
			if strings.Contains(logs.String(), ip) {
				t.Errorf("o log traz o IP %s:\n%s", ip, logs.String())
			}
		}
	})
	t.Run("com o PROXY_CONFIAVEL certo", func(t *testing.T) {
		h, logs := novo(t, "172.18.0.0/16")
		for range 3 {
			pedir(h, "172.18.0.2:1000", "203.0.113.9")
		}
		if n := len(avisos(logs)); n != 0 {
			t.Fatalf("do proxy confiável saiu %d aviso(s):\n%s", n, logs.String())
		}
		// Um pedido direto, de fora do proxy, com o cabeçalho forjado: o aviso.
		pedir(h, "198.51.100.7:1000", "10.0.0.1")
		pedir(h, "198.51.100.7:1000", "10.0.0.2")
		got := avisos(logs)
		if len(got) != 1 || got[0]["proxy_confiavel"] != true {
			t.Fatalf("os avisos = %v", got)
		}
	})
}

// linhasCruas lê o log JSON inteiro.
func linhasCruas(t *testing.T, logs string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(logs), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("o log não é JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}
