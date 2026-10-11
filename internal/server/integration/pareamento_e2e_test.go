package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/painel"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/authtest"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O pareamento de ponta a ponta (ADR-019, F6): o lado do PC
// (internal/agent/nuvem, com o servidor do painel de verdade em loopback)
// contra o servidor da nuvem de verdade (internal/server, na memória), por
// HTTP. O navegador é o teste: abre a página de aprovação com o cookie da
// sessão, lê o pedido, aprova e segue o retorno até o 127.0.0.1 do PC. Prova
// que os dois lados falam o mesmo contrato (os nomes do JSON, o state, o
// PKCE, o Bearer), o que os dublês de cada lado não provam.
//
// Nada aqui toca o sistema: o Cofre é o chaves.Memoria e o "navegador" é
// um http.Client.

type instalacaoE2E struct {
	mu sync.Mutex
	m  map[string]string
}

func (i *instalacaoE2E) Ler(_ context.Context, n string) (string, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	v, ok := i.m[n]
	return v, ok, nil
}

func (i *instalacaoE2E) Gravar(_ context.Context, v map[string]string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for k, x := range v {
		i.m[k] = x
	}
	return nil
}

func (i *instalacaoE2E) Apagar(_ context.Context, ns ...string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, k := range ns {
		delete(i.m, k)
	}
	return nil
}

func portaDe(srv *httptest.Server) int {
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

// naFaixaDinamica troca o listener do servidor ainda parado por um em
// 127.0.0.1 numa porta de 49152 a 65535: a nuvem só aceita o retorno do PC
// nessa faixa (a dinâmica do Windows, onde o painel do Agent abre). A porta
// automática do Linux (32768 a 60999) cai fora dela com frequência.
func naFaixaDinamica(t *testing.T, srv *httptest.Server) {
	t.Helper()
	for porta := 49152; porta <= 65535; porta++ {
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(porta))
		if err == nil {
			srv.Listener.Close()
			srv.Listener = l
			return
		}
	}
	t.Fatal("nenhuma porta livre de 49152 a 65535 em 127.0.0.1")
}

func TestPareamento_AgentContraANuvemDeVerdade(t *testing.T) {
	ctx := context.Background()
	agora := time.Now()

	// ─── A nuvem ───
	mem := cloudstore.NewMemoryStore()
	u, err := mem.EntrarComGoogle(ctx, cloudstore.Identidade{Sub: "sub-e2e", Email: "voce@teste.local", Nome: "Você"}, agora)
	if err != nil {
		t.Fatal(err)
	}
	segredo := []byte(cloudauth.NovoSegredo())
	sessao := cloudauth.NovoSegredo()
	if _, err := mem.CriarSessao(ctx, cloudstore.NovaSessao{
		UserID: u.ID, TokenHMAC: cloudauth.HMAC(segredo, sessao), Rotulo: "Chrome no Windows", Agora: agora, ExpiraEm: agora.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	contas, err := cloudauth.ParseContas("voce@teste.local")
	if err != nil {
		t.Fatal(err)
	}
	nuvemSrv := httptest.NewUnstartedServer(nil)
	base := "http://localhost:" + strconv.Itoa(portaDe(nuvemSrv))
	var logNuvem bytes.Buffer
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: mem, Accounts: mem, BaseURL: base, SessionSecret: segredo, DeviceTokenPepper: []byte(cloudauth.NovoSegredo()),
		Contas: contas, Google: authtest.NovoGoogleFalso(t, base+"/auth/google/callback", time.Now).Client(),
		Painel: contracttest.PainelDeTeste(), Logger: slog.New(slog.NewTextHandler(&logNuvem, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	nuvemSrv.Config.Handler = cloud.Handler()
	nuvemSrv.Start()
	defer nuvemSrv.Close()

	// ─── O PC: o Servico e o servidor do painel em loopback ───
	cofre := chaves.NovaMemoria()
	inst := &instalacaoE2E{m: map[string]string{}}
	var abertos []string
	var logPC bytes.Buffer
	s := nuvem.Novo(nuvem.Config{
		Cofre: cofre, NomeNoCofre: nuvem.NomeNoCofrePortatil, Instalacao: inst,
		Abrir:    func(u string) error { abertos = append(abertos, u); return nil },
		Politica: nuvem.Politica{Dev: true, Portatil: true}, Versao: "0.1.0-sprint2", NomeDoPC: "DELL-G15",
		NovoID: cloudstore.NewUUIDv7, Log: slog.New(slog.NewTextHandler(&logPC, nil)),
	})
	painelSrv := httptest.NewUnstartedServer(nil)
	naFaixaDinamica(t, painelSrv)
	portaDoPainel := portaDe(painelSrv)
	painelSrv.Config.Handler = painel.Handler(painel.Config{
		Porta: portaDoPainel, Token: "tok", Bilhetes: painel.NovosBilhetes(), DirPainel: t.TempDir(), Retorno: s.Retorno(),
		API: http.NotFoundHandler(),
	})
	painelSrv.Start()
	defer painelSrv.Close()
	s.UsarPorta(portaDoPainel)

	// ─── O navegador, com o cookie da sessão ───
	navegador := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	pedir := func(metodo, endereco string, comSessao bool) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(metodo, endereco, nil)
		if comSessao {
			req.AddCookie(&http.Cookie{Name: "__Host-sessao", Value: sessao})
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		resp, err := navegador.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", metodo, endereco, err)
		}
		defer resp.Body.Close()
		corpo, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, corpo
	}
	conectar := func() {
		t.Helper()
		e, err := s.Conectar(ctx, base)
		if err != nil {
			t.Fatalf("Conectar: %v\nlog do PC:\n%s\nlog da nuvem:\n%s", err, logPC.String(), logNuvem.String())
		}
		// 1. A página de aprovação serve o app.
		aberto := abertos[len(abertos)-1]
		if !strings.HasPrefix(aberto, base+"/device/authorize?pedido=") || e.AbrirURL != aberto {
			t.Fatalf("abriu %q", aberto)
		}
		if st, corpo := pedir("GET", aberto, false); st != 200 || !strings.Contains(string(corpo), `<div id="root">`) {
			t.Fatalf("a página de aprovação = %d", st)
		}
		pedido := strings.TrimPrefix(aberto, base+"/device/authorize?pedido=")
		// 2. O app lê o pedido: o nome do PC e o mesmo código do painel.
		st, corpo := pedir("GET", base+"/api/v1/device-auth/requests/"+pedido, true)
		var p struct{ Nome, Plataforma, Conferencia string }
		if st != 200 || json.Unmarshal(corpo, &p) != nil || p.Nome != "DELL-G15" || p.Plataforma != "windows" || p.Conferencia != e.Conferencia {
			t.Fatalf("o pedido = %d %s (o painel mostra %q)", st, corpo, e.Conferencia)
		}
		// 3. Aprovar devolve o retorno ao 127.0.0.1 do PC.
		st, corpo = pedir("POST", base+"/api/v1/device-auth/requests/"+pedido+"/approve", true)
		var a struct{ Retorno string }
		if st != 200 || json.Unmarshal(corpo, &a) != nil ||
			!strings.HasPrefix(a.Retorno, "http://127.0.0.1:"+strconv.Itoa(portaDoPainel)+"/nuvem/retorno?code=") {
			t.Fatalf("aprovar = %d %s", st, corpo)
		}
		// 4. O navegador segue o retorno até o PC.
		if st, corpo := pedir("GET", a.Retorno, false); st != 200 || !strings.Contains(string(corpo), "PC conectado") {
			t.Fatalf("o retorno no PC = %d %s", st, corpo)
		}
		// O mesmo retorno de novo (a página recarregada): a mesma página, sem
		// trocar o code de novo (a nuvem o recusaria, e o token não muda).
		antes, _ := cofre.Ler(nuvem.NomeNoCofrePortatil)
		if st, corpo := pedir("GET", a.Retorno, false); st != 200 || !strings.Contains(string(corpo), "PC conectado") {
			t.Errorf("o retorno repetido = %d", st)
		}
		if depois, _ := cofre.Ler(nuvem.NomeNoCofrePortatil); depois != antes {
			t.Error("o retorno repetido trocou o token")
		}
	}

	conectar()
	e, err := s.Estado(ctx)
	if err != nil || e.Estado != nuvem.Conectado || e.Email != "voce@teste.local" || e.Nome != "DELL-G15" || e.URL != base {
		t.Fatalf("estado = %+v, %v", e, err)
	}
	token, err := cofre.Ler(nuvem.NomeNoCofrePortatil)
	if err != nil {
		t.Fatal(err)
	}
	// O token vale na nuvem: o /me com o Bearer traz o aparelho.
	reqMe, _ := http.NewRequest("GET", base+"/api/v1/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(reqMe)
	if err != nil {
		t.Fatal(err)
	}
	corpoMe, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	aparelho := inst.m["nuvem_aparelho"]
	if resp.StatusCode != 200 || !strings.Contains(string(corpoMe), aparelho) {
		t.Fatalf("/me com o token = %d %s", resp.StatusCode, corpoMe)
	}

	// A lista de Aparelhos, na sessão, mostra o PC.
	listar := func() []map[string]any {
		t.Helper()
		st, corpo := pedir("GET", base+"/api/v1/devices", true)
		var l struct{ Items []map[string]any }
		if st != 200 || json.Unmarshal(corpo, &l) != nil {
			t.Fatalf("lista = %d %s", st, corpo)
		}
		return l.Items
	}
	if l := listar(); len(l) != 1 || l[0]["id"] != aparelho || l[0]["nome"] != "DELL-G15" || l[0]["plataforma"] != "windows" {
		t.Fatalf("lista = %v", l)
	}

	// Revogar no app: o Conferir do PC recebe o 401 e desconecta.
	if st, _ := pedir("DELETE", base+"/api/v1/devices/"+aparelho, true); st != 204 {
		t.Fatalf("revogar = %d", st)
	}
	e, err = s.Conferir(ctx)
	if err != nil || e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoRevogado {
		t.Fatalf("depois de revogar = %+v, %v", e, err)
	}
	if _, err := cofre.Ler(nuvem.NomeNoCofrePortatil); err == nil {
		t.Error("o token revogado ficou no cofre")
	}
	if l := listar(); len(l) != 0 {
		t.Errorf("o revogado continua na lista: %v", l)
	}

	// Conectar de novo: o mesmo aparelho (o install_id), com outro token; o
	// velho continua recusado.
	conectar()
	if inst.m["nuvem_aparelho"] != aparelho {
		t.Errorf("parear de novo criou outro aparelho: %s, era %s", inst.m["nuvem_aparelho"], aparelho)
	}
	novo, _ := cofre.Ler(nuvem.NomeNoCofrePortatil)
	if novo == "" || novo == token {
		t.Error("o token não mudou")
	}
	reqMe.Header.Set("Authorization", "Bearer "+token)
	if resp, err := http.DefaultClient.Do(reqMe); err != nil || resp.StatusCode != 401 {
		t.Errorf("o token velho = %v, %v", resp, err)
	} else {
		resp.Body.Close()
	}

	// Desconectar no PC revoga na nuvem, com o próprio token.
	e, err = s.Desconectar(ctx)
	if err != nil || e.Estado != nuvem.Desconectado || e.Aviso != "" {
		t.Fatalf("desconectar = %+v, %v", e, err)
	}
	if l := listar(); len(l) != 0 {
		t.Errorf("desconectado continua na lista: %v", l)
	}

	// Os logs dos dois lados, sem o token, o code nem a sessão.
	for nome, log := range map[string]string{"nuvem": logNuvem.String(), "PC": logPC.String()} {
		for _, segredo := range []string{token, novo, sessao, "voce@teste.local"} {
			if strings.Contains(log, segredo) {
				t.Errorf("o log da %s tem um segredo", nome)
			}
		}
	}
}
