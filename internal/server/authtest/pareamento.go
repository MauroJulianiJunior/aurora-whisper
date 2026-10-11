package authtest

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O pareamento de um PC e os aparelhos (ADR-019, F6), pela API: o PC falso
// fala com a nuvem como o Agent (sem cookie, sem Sec-Fetch-Site), e o
// navegador é o da pessoa logada.

// pcFalso é o Agent: o install_id e o verificador do PKCE dele.
type pcFalso struct {
	m           *mundo
	installID   string
	verificador string
	ip          string
}

func (m *mundo) novoPC() *pcFalso {
	return &pcFalso{m: m, installID: contracttest.NewID(), verificador: cloudauth.NovoSegredo(), ip: "198.51.100.7:40000"}
}

// pedirComo manda um pedido do PC (sem cookie, sem Sec-Fetch-Site).
func (pc *pcFalso) pedir(method, path string, body any) resposta {
	pc.m.t.Helper()
	req := pc.m.req(method, path, body)
	req.RemoteAddr = pc.ip
	return pc.m.serve(req)
}

func (pc *pcFalso) corpoDoInicio() map[string]any {
	return map[string]any{
		"install_id": pc.installID, "device_name": "PC do escritório", "platform": "windows",
		"app_version": "0.2.0", "code_challenge": cloudauth.DesafioS256(pc.verificador), "redirect_port": 51234,
	}
}

type inicio struct {
	Pedido      string `json:"pedido"`
	AuthURL     string `json:"auth_url"`
	Conferencia string `json:"conferencia"`
	ExpiresIn   int    `json:"expires_in"`
}

// comecar é o POST /device-auth/start que dá certo.
func (pc *pcFalso) comecar() inicio {
	pc.m.t.Helper()
	res := pc.pedir(http.MethodPost, "/api/v1/device-auth/start", pc.corpoDoInicio())
	if res.Code != http.StatusOK {
		pc.m.t.Fatalf("start: %d %s", res.Code, res.Body.String())
	}
	var out inicio
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		pc.m.t.Fatalf("start: %v %s", err, res.Body.String())
	}
	return out
}

type troca struct {
	DeviceID string `json:"device_id"`
	Token    string `json:"token"`
	Conta    struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"conta"`
}

func (pc *pcFalso) trocar(code, verificador string) (resposta, troca) {
	pc.m.t.Helper()
	res := pc.pedir(http.MethodPost, "/api/v1/device-auth/exchange", map[string]any{"code": code, "code_verifier": verificador})
	var out troca
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res, out
}

// aprovar é o POST de aprovação; devolve o code e o state do retorno.
func (n *navegador) aprovar(pedido string) (resposta, string, string) {
	n.m.t.Helper()
	res := n.pedir(http.MethodPost, "/api/v1/device-auth/requests/"+pedido+"/approve", nil)
	if res.Code != http.StatusOK {
		return res, "", ""
	}
	var out struct {
		Retorno string `json:"retorno"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		n.m.t.Fatalf("aprovar: %v", err)
	}
	u, err := url.Parse(out.Retorno)
	if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:51234" || u.Path != "/nuvem/retorno" || u.Fragment != "" {
		n.m.t.Fatalf("o retorno não é o painel local do PC: %q", out.Retorno)
	}
	q := u.Query()
	if len(q) != 2 {
		n.m.t.Fatalf("o retorno leva %v; quero só o code e o state", q)
	}
	return res, q.Get("code"), q.Get("state")
}

// parear faz o pareamento inteiro do PC com a conta do navegador.
func (pc *pcFalso) parear(n *navegador) troca {
	pc.m.t.Helper()
	ini := pc.comecar()
	res, code, _ := n.aprovar(ini.Pedido)
	if res.Code != http.StatusOK {
		pc.m.t.Fatalf("aprovar: %d %s", res.Code, res.Body.String())
	}
	tr, out := pc.trocar(code, pc.verificador)
	if tr.Code != http.StatusOK {
		pc.m.t.Fatalf("troca: %d %s", tr.Code, tr.Body.String())
	}
	return out
}

type aparelhoJSON struct {
	ID          string    `json:"id"`
	Nome        string    `json:"nome"`
	Plataforma  string    `json:"plataforma"`
	Versao      string    `json:"versao"`
	ConectadoEm time.Time `json:"conectado_em"`
	UltimoUso   time.Time `json:"ultimo_uso_em"`
}

func aparelhos(t *testing.T, n *navegador) []aparelhoJSON {
	t.Helper()
	res := n.pedir(http.MethodGet, "/api/v1/devices", nil)
	var out struct {
		Items []aparelhoJSON `json:"items"`
	}
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &out) != nil || out.Items == nil {
		t.Fatalf("GET /devices: %d %s", res.Code, res.Body.String())
	}
	return out.Items
}

func codigoInvalido(t *testing.T, res resposta, oQue string) {
	t.Helper()
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"CODIGO_INVALIDO"`) {
		t.Errorf("%s: %d %s; quero 400 CODIGO_INVALIDO", oQue, res.Code, res.Body.String())
	}
}

func testParearCaminhoFeliz(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	userID := n.userID()
	antes := b.Contar(t)
	pc := m.novoPC()

	ini := pc.comecar()
	if id, ok := cloudstore.CanonicalUUID(ini.Pedido); !ok || id != ini.Pedido {
		t.Fatalf("pedido = %q, quero um UUID em minúsculas", ini.Pedido)
	}
	if ini.AuthURL != contracttest.BaseURL+"/device/authorize?pedido="+ini.Pedido {
		t.Errorf("auth_url = %q", ini.AuthURL)
	}
	if !cloudauth.PareceConferencia(ini.Conferencia) || ini.ExpiresIn != 600 {
		t.Errorf("conferência %q, expires_in %d; quero 4 caracteres do alfabeto e 600", ini.Conferencia, ini.ExpiresIn)
	}
	if c := b.Contar(t); c.Pedidos != antes.Pedidos+1 || c.Aparelhos != antes.Aparelhos {
		t.Fatalf("depois do start: %+v (antes %+v); quero um pedido a mais e nenhum aparelho", c, antes)
	}

	// A página lê o pedido: o nome do PC, a plataforma, a versão e o código.
	res := n.pedir(http.MethodGet, "/api/v1/device-auth/requests/"+ini.Pedido, nil)
	var ped struct {
		Nome, Plataforma, Versao, Conferencia string
		ExpiraEm                              time.Time `json:"expira_em"`
	}
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &ped) != nil {
		t.Fatalf("ler o pedido: %d %s", res.Code, res.Body.String())
	}
	if ped.Nome != "PC do escritório" || ped.Plataforma != "windows" || ped.Versao != "0.2.0" || ped.Conferencia != ini.Conferencia ||
		!ped.ExpiraEm.Equal(agora0.Add(10*time.Minute)) {
		t.Fatalf("pedido = %+v", ped)
	}
	if strings.Contains(res.Body.String(), pc.installID) || strings.Contains(res.Body.String(), "challenge") {
		t.Error("a página recebe o install_id ou o desafio")
	}

	res, code, state := n.aprovar(ini.Pedido)
	if res.Code != http.StatusOK || state != ini.Pedido || !cloudauth.PareceSegredo(code) {
		t.Fatalf("aprovar: %d, state %q (pedido %q), code com formato %v", res.Code, state, ini.Pedido, cloudauth.PareceSegredo(code))
	}

	tr, out := pc.trocar(code, pc.verificador)
	if tr.Code != http.StatusOK {
		t.Fatalf("troca: %d %s", tr.Code, tr.Body.String())
	}
	if _, ok := cloudstore.CanonicalUUID(out.DeviceID); !ok || !cloudauth.PareceSegredo(out.Token) ||
		out.Conta.ID != userID || out.Conta.Email != m.emailA {
		t.Fatalf("troca = %+v", out)
	}
	if c := b.Contar(t); c.Aparelhos != antes.Aparelhos+1 || c.Tokens != antes.Tokens+1 {
		t.Fatalf("depois da troca: %+v (antes %+v); quero um aparelho e um token a mais", c, antes)
	}

	// O token vale no /me, com o aparelho.
	var me struct {
		ID       string `json:"id"`
		Aparelho struct {
			ID, Nome, Plataforma string
		} `json:"aparelho"`
	}
	res = m.comToken(http.MethodGet, "/api/v1/me", out.Token, nil)
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &me) != nil || me.ID != userID ||
		me.Aparelho.ID != out.DeviceID || me.Aparelho.Nome != "PC do escritório" || me.Aparelho.Plataforma != "windows" {
		t.Fatalf("GET /me com o token novo: %d %s", res.Code, res.Body.String())
	}
	// E o sync.
	if res := m.comToken(http.MethodGet, "/api/v1/sync/pull?after_seq=0", out.Token, nil); res.Code != http.StatusOK {
		t.Fatalf("pull com o token novo: %d", res.Code)
	}

	// A lista de aparelhos da conta.
	list := aparelhos(t, n)
	if len(list) != 1 || list[0].ID != out.DeviceID || list[0].Nome != "PC do escritório" || list[0].Plataforma != "windows" ||
		list[0].Versao != "0.2.0" || !list[0].ConectadoEm.Equal(agora0) || !list[0].UltimoUso.Equal(agora0) {
		t.Fatalf("aparelhos = %+v", list)
	}

	// O token, os bytes dele, o SHA-256 puro dele, o code e o verificador não
	// estão em lugar nenhum do banco. O HMAC com o pepper está.
	dump := b.Despejo(t)
	raw, _ := base64.RawURLEncoding.DecodeString(out.Token)
	for name, v := range map[string]string{
		"o token": out.Token, "os bytes do token": hex.EncodeToString(raw),
		"o SHA-256 do token": hex.EncodeToString(cloudauth.Hash(out.Token)),
		"o code":             code, "o verificador": pc.verificador,
	} {
		if strings.Contains(dump, v) {
			t.Errorf("%s está no banco", name)
		}
	}
	if !strings.Contains(dump, hex.EncodeToString(cloudauth.HMAC(pepperDeToken, out.Token))) {
		t.Error("o HMAC do token não está no banco (o despejo não vê a tabela?)")
	}
	if strings.Contains(dump, hex.EncodeToString(cloudauth.HMAC(segredoDeSessao, out.Token))) {
		t.Error("o token foi gravado com a chave da sessão, e não com o pepper")
	}
}

func testParearLinkCopiado(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	pc := m.novoPC()
	ini := pc.comecar()
	_, code, _ := n.aprovar(ini.Pedido)
	antes := b.Contar(t)

	// Quem tem o link (o code do retorno) e não tem o verificador: 400, e o
	// code continua valendo para o PC que começou.
	for _, v := range []string{cloudauth.NovoSegredo(), ini.Pedido[:36] + "1234567", cloudauth.DesafioS256(pc.verificador)} {
		res, _ := pc.trocar(code, v)
		codigoInvalido(t, res, "verificador errado")
	}
	if c := b.Contar(t); c != antes {
		t.Fatalf("a troca com o verificador errado mudou o banco: %+v -> %+v", antes, c)
	}
	res, out := pc.trocar(code, pc.verificador)
	if res.Code != http.StatusOK {
		t.Fatalf("o PC que começou, depois das tentativas erradas: %d %s; o code não podia ter queimado", res.Code, res.Body.String())
	}
	// Uso único: o mesmo code de novo, com o verificador certo, não vale, e o
	// primeiro token continua.
	res, _ = pc.trocar(code, pc.verificador)
	codigoInvalido(t, res, "o code de novo")
	if res := m.comToken(http.MethodGet, "/api/v1/me", out.Token, nil); res.Code != http.StatusOK {
		t.Fatalf("o token da troca: %d", res.Code)
	}
}

func testParearCodigoQueNaoVale(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	antes := b.Contar(t)

	// Formato errado: 400 sem ir ao banco.
	pc := m.novoPC()
	for _, corpo := range []map[string]any{
		{"code": "curto", "code_verifier": pc.verificador},
		{"code": cloudauth.NovoSegredo(), "code_verifier": "curto"},
		{"code": "", "code_verifier": ""},
	} {
		codigoInvalido(t, pc.pedir(http.MethodPost, "/api/v1/device-auth/exchange", corpo), "formato errado")
	}
	for _, corpo := range []any{map[string]any{"code": cloudauth.NovoSegredo(), "code_verifier": pc.verificador, "extra": 1}, `{"code":`, nil, `[]`} {
		if res := pc.pedir(http.MethodPost, "/api/v1/device-auth/exchange", corpo); res.Code != http.StatusBadRequest {
			t.Errorf("troca com corpo %v: %d, quero 400", corpo, res.Code)
		}
	}
	// Um code que ninguém aprovou: o pedido existe, mas sem aprovação não há
	// code; um inventado não vale.
	pc.comecar()
	res, _ := pc.trocar(cloudauth.NovoSegredo(), pc.verificador)
	codigoInvalido(t, res, "code inventado")

	// Aprovado e vencido.
	ini := pc.comecar()
	_, code, _ := n.aprovar(ini.Pedido)
	m.clk.andar(10 * time.Minute)
	res, _ = pc.trocar(code, pc.verificador)
	codigoInvalido(t, res, "code vencido")
	if c := b.Contar(t); c.Aparelhos != antes.Aparelhos || c.Tokens != antes.Tokens {
		t.Fatalf("um code que não vale criou aparelho ou token: %+v -> %+v", antes, c)
	}

	// Aprovado, com o verificador certo e o code errado (quem tem o
	// verificador do PC e não viu o retorno do navegador; ou o verificador no
	// lugar do code): 400 e nada gravado. A troca confere o hash do code E o
	// desafio; e o code de verdade ainda troca, para o PC que começou.
	ini = pc.comecar()
	_, code, _ = n.aprovar(ini.Pedido)
	antes = b.Contar(t)
	for _, errado := range []string{cloudauth.NovoSegredo(), pc.verificador} {
		res, _ = pc.trocar(errado, pc.verificador)
		codigoInvalido(t, res, "code errado com o verificador certo")
	}
	if c := b.Contar(t); c != antes {
		t.Fatalf("a troca com o code errado mudou o banco: %+v -> %+v", antes, c)
	}
	if res, _ = pc.trocar(code, pc.verificador); res.Code != http.StatusOK {
		t.Fatalf("o code de verdade, depois das tentativas com o code errado: %d %s; o pedido não podia ter queimado",
			res.Code, res.Body.String())
	}
}

func testParearAprovar(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	pc := m.novoPC()

	ini := pc.comecar()
	if res, _, _ := n.aprovar(ini.Pedido); res.Code != http.StatusOK {
		t.Fatalf("aprovar: %d", res.Code)
	}
	// Aprovado: a página não o mostra mais, e aprovar de novo não dá outro
	// code.
	for _, rq := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/device-auth/requests/" + ini.Pedido},
		{http.MethodPost, "/api/v1/device-auth/requests/" + ini.Pedido + "/approve"},
	} {
		res := n.pedir(rq.method, rq.path, nil)
		if res.Code != http.StatusNotFound {
			t.Errorf("%s %s depois de aprovado: %d, quero 404", rq.method, rq.path, res.Code)
		}
	}
	// Vencido: nem a página, nem a aprovação.
	ini = pc.comecar()
	m.clk.andar(10 * time.Minute)
	for _, rq := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/device-auth/requests/" + ini.Pedido},
		{http.MethodPost, "/api/v1/device-auth/requests/" + ini.Pedido + "/approve"},
	} {
		if res := n.pedir(rq.method, rq.path, nil); res.Code != http.StatusNotFound {
			t.Errorf("%s depois do prazo: %d, quero 404", rq.path, res.Code)
		}
	}
	// Um segundo antes do prazo ainda vale.
	ini = pc.comecar()
	m.clk.andar(10*time.Minute - time.Second)
	if res := n.pedir(http.MethodGet, "/api/v1/device-auth/requests/"+ini.Pedido, nil); res.Code != http.StatusOK {
		t.Errorf("um segundo antes do prazo: %d, quero 200", res.Code)
	}
	// Desconhecido, ou que não é UUID: o mesmo 404.
	for _, id := range []string{contracttest.NewID(), "nao-e-uuid", strings.ToUpper(contracttest.NewID())} {
		for _, path := range []string{"/api/v1/device-auth/requests/" + id, "/api/v1/device-auth/requests/" + id + "/approve"} {
			method := http.MethodGet
			if strings.HasSuffix(path, "/approve") {
				method = http.MethodPost
			}
			if res := n.pedir(method, path, nil); res.Code != http.StatusNotFound {
				t.Errorf("%s %s: %d, quero 404", method, path, res.Code)
			}
		}
	}
	// Sem sessão: 401.
	ini = pc.comecar()
	if res := m.serve(m.req(http.MethodPost, "/api/v1/device-auth/requests/"+ini.Pedido+"/approve", nil)); res.Code != http.StatusUnauthorized {
		t.Errorf("aprovar sem sessão: %d, quero 401", res.Code)
	}
}

func testParearInicioValidado(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	pc := m.novoPC()
	antes := b.Contar(t)
	com := func(campo string, valor any) map[string]any {
		c := pc.corpoDoInicio()
		if valor == nil {
			delete(c, campo)
		} else {
			c[campo] = valor
		}
		return c
	}
	desafio := cloudauth.DesafioS256(pc.verificador)
	ruins := map[string]any{
		"sem install_id":            com("install_id", nil),
		"install_id não UUID":       com("install_id", "meu-pc"),
		"sem nome":                  com("device_name", nil),
		"nome vazio":                com("device_name", "   "),
		"nome de 81":                com("device_name", strings.Repeat("é", 81)),
		"nome com controle":         com("device_name", "PC\x00x"),
		"nome com quebra":           com("device_name", "PC\nx"),
		"plataforma linux":          com("platform", "linux"),
		"sem plataforma":            com("platform", nil),
		"versão com espaço":         com("app_version", "0.2 beta"),
		"versão de 65":              com("app_version", strings.Repeat("1", 65)),
		"desafio de 42":             com("code_challenge", desafio[:42]),
		"desafio de 44":             com("code_challenge", desafio+"A"),
		"desafio com =":             com("code_challenge", desafio[:42]+"="),
		"desafio com +":             com("code_challenge", desafio[:42]+"+"),
		"sem desafio":               com("code_challenge", nil),
		"porta 80":                  com("redirect_port", 80),
		"porta 1023":                com("redirect_port", 1023),
		"porta 1024":                com("redirect_port", 1024),
		"porta 8080":                com("redirect_port", 8080),
		"porta 49151":               com("redirect_port", 49151),
		"porta 65536":               com("redirect_port", 65536),
		"porta texto":               com("redirect_port", "51234"),
		"campo a mais":              com("user_id", contracttest.NewID()),
		"o state escolhido pelo PC": com("pedido", contracttest.NewID()),
		"corpo grande":              com("device_name", strings.Repeat("x", 2000)),
		"corpo vazio":               nil,
		"JSON quebrado":             `{"install_id":`,
		"lista":                     `[]`,
		"dois objetos":              `{}{}`,
	}
	for nome, corpo := range ruins {
		if res := pc.pedir(http.MethodPost, "/api/v1/device-auth/start", corpo); res.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, quero 400", nome, res.Code, res.Body.String())
		}
	}
	if c := b.Contar(t); c != antes {
		t.Fatalf("um start recusado gravou: %+v -> %+v", antes, c)
	}
	// O limite de cada campo vale: 80 caracteres (não bytes) e a porta 49152
	// (o começo da faixa dinâmica do Windows) e 65535; a versão vazia também.
	for nome, corpo := range map[string]map[string]any{
		"nome de 80 com acento": com("device_name", strings.Repeat("é", 80)),
		"porta 49152":           com("redirect_port", 49152),
		"porta 65535":           com("redirect_port", 65535),
		"versão vazia":          com("app_version", ""),
		"sem versão":            com("app_version", nil),
	} {
		if res := pc.pedir(http.MethodPost, "/api/v1/device-auth/start", corpo); res.Code != http.StatusOK {
			t.Errorf("%s: %d %s, quero 200", nome, res.Code, res.Body.String())
		}
	}
}

func testParearLimites(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{limites: limitesDoPareamento(3, 5)})
	pc := m.novoPC()
	iniciar := func(ip string) resposta {
		pc.ip = ip
		return pc.pedir(http.MethodPost, "/api/v1/device-auth/start", pc.corpoDoInicio())
	}
	for i := range 3 {
		if res := iniciar("198.51.100.1:1"); res.Code != http.StatusOK {
			t.Fatalf("start %d do IP 1: %d", i+1, res.Code)
		}
	}
	if res := iniciar("198.51.100.1:2"); res.Code != http.StatusTooManyRequests || res.Header().Get("Retry-After") == "" {
		t.Fatalf("4º start do IP 1: %d, quero 429 com Retry-After", res.Code)
	}
	// A troca do mesmo IP gasta do mesmo balde.
	pc.ip = "198.51.100.1:3"
	if res, _ := pc.trocar(cloudauth.NovoSegredo(), pc.verificador); res.Code != http.StatusTooManyRequests {
		t.Fatalf("troca do IP 1 sem fichas: %d, quero 429", res.Code)
	}
	// O total: 5 starts de todos.
	for i := range 2 {
		if res := iniciar("198.51.100.2:1"); res.Code != http.StatusOK {
			t.Fatalf("start %d do IP 2: %d", i+1, res.Code)
		}
	}
	if res := iniciar("198.51.100.3:1"); res.Code != http.StatusTooManyRequests {
		t.Fatalf("6º start no total: %d, quero 429", res.Code)
	}
	// A troca não passa pelo total.
	pc.ip = "198.51.100.3:2"
	if res, _ := pc.trocar(cloudauth.NovoSegredo(), pc.verificador); res.Code != http.StatusBadRequest {
		t.Fatalf("troca de outro IP com o total esgotado: %d, quero 400 (não 429)", res.Code)
	}
	m.clk.andar(time.Minute)
	if res := iniciar("198.51.100.1:4"); res.Code != http.StatusOK {
		t.Fatalf("start depois de um minuto: %d", res.Code)
	}
}

func testParearForaDaLista(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	pc := m.novoPC()
	ini := pc.comecar()
	_, code, _ := n.aprovar(ini.Pedido)
	antes := b.Contar(t)
	m.montar(m.emailB) // reinicia sem o e-mail A, entre a aprovação e a troca
	res, _ := pc.trocar(code, pc.verificador)
	codigoInvalido(t, res, "a conta que saiu da lista")
	if c := b.Contar(t); c.Aparelhos != antes.Aparelhos || c.Tokens != antes.Tokens {
		t.Fatalf("a troca de uma conta fora da lista criou aparelho: %+v -> %+v", antes, c)
	}
	m.montar("") // volta
}

func testAparelhosEscopo(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	a, bb := m.navegador(uaWindows), m.navegador(uaAndroid)
	a.entrarComo(sub(), m.emailA)
	bb.entrarComo(sub(), m.emailB)
	pc1, pc2, pcB := m.novoPC(), m.novoPC(), m.novoPC()
	t1, t2, tB := pc1.parear(a), pc2.parear(a), pcB.parear(bb)

	// O token não lista, não lê nem aprova pedido, e não revoga outro
	// aparelho, nem da mesma conta.
	ini := m.novoPC().comecar()
	for _, rq := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodGet, "/api/v1/device-auth/requests/" + ini.Pedido},
		{http.MethodPost, "/api/v1/device-auth/requests/" + ini.Pedido + "/approve"},
		{http.MethodDelete, "/api/v1/devices/" + t2.DeviceID},
		{http.MethodDelete, "/api/v1/devices/" + tB.DeviceID},
		{http.MethodDelete, "/api/v1/devices/nao-e-uuid"},
	} {
		if res := m.comToken(rq.method, rq.path, t1.Token, nil); res.Code != http.StatusForbidden {
			t.Errorf("%s %s com o token: %d, quero 403", rq.method, rq.path, res.Code)
		}
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", t2.Token, nil); res.Code != http.StatusOK {
		t.Fatalf("o outro PC da conta caiu: %d", res.Code)
	}
	// A sessão de B não revoga o aparelho de A: 404, o mesmo de um que não
	// existe.
	for _, id := range []string{t1.DeviceID, contracttest.NewID(), "nao-e-uuid"} {
		if res := bb.pedir(http.MethodDelete, "/api/v1/devices/"+id, nil); res.Code != http.StatusNotFound {
			t.Errorf("B revogando %s: %d, quero 404", id, res.Code)
		}
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", t1.Token, nil); res.Code != http.StatusOK {
		t.Fatalf("o PC de A caiu pela sessão de B: %d", res.Code)
	}
	// As listas são de cada conta.
	if list := aparelhos(t, bb); len(list) != 1 || list[0].ID != tB.DeviceID {
		t.Errorf("aparelhos de B = %+v", list)
	}
	if list := aparelhos(t, a); len(list) != 2 {
		t.Errorf("aparelhos de A = %+v", list)
	}
	// O próprio PC se desconecta com o token (o Desconectar): 204, e o token
	// passa a ser 401.
	if res := m.comToken(http.MethodDelete, "/api/v1/devices/"+strings.ToUpper(t1.DeviceID), t1.Token, nil); res.Code != http.StatusNoContent {
		t.Fatalf("o PC se desconectando: %d %s", res.Code, res.Body.String())
	}
	for _, path := range []string{"/api/v1/me", "/api/v1/sync/pull?after_seq=0"} {
		if res := m.comToken(http.MethodGet, path, t1.Token, nil); res.Code != http.StatusUnauthorized {
			t.Errorf("%s com o token revogado: %d, quero 401", path, res.Code)
		}
	}
	if list := aparelhos(t, a); len(list) != 1 || list[0].ID != t2.DeviceID {
		t.Errorf("aparelhos de A depois de desconectar o PC 1 = %+v; quero só o PC 2 (o revogado some da lista)", list)
	}
}

func testAparelhosRevogar(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	pc := m.novoPC()
	t1 := pc.parear(n)
	antes := b.Contar(t)

	if res := n.pedir(http.MethodDelete, "/api/v1/devices/"+t1.DeviceID, nil); res.Code != http.StatusNoContent {
		t.Fatalf("revogar: %d %s", res.Code, res.Body.String())
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", t1.Token, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("o token revogado: %d, quero 401", res.Code)
	}
	if list := aparelhos(t, n); len(list) != 0 {
		t.Fatalf("aparelhos depois de revogar = %+v; quero nenhum", list)
	}
	// A linha fica (os ditados apontam para ela); o token sai.
	if c := b.Contar(t); c.Aparelhos != antes.Aparelhos || c.Tokens != antes.Tokens-1 {
		t.Fatalf("revogar: %+v -> %+v; quero o aparelho no banco e o token fora", antes, c)
	}
	if res := n.pedir(http.MethodDelete, "/api/v1/devices/"+t1.DeviceID, nil); res.Code != http.StatusNotFound {
		t.Errorf("revogar de novo: %d, quero 404", res.Code)
	}

	// Parear de novo o mesmo PC (o mesmo install_id) reaproveita a linha, e o
	// token velho continua 401.
	pc.verificador = cloudauth.NovoSegredo()
	t2 := pc.parear(n)
	if t2.DeviceID != t1.DeviceID {
		t.Fatalf("parear de novo criou outro aparelho: %s, quero %s", t2.DeviceID, t1.DeviceID)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", t1.Token, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("o token velho depois de parear de novo: %d, quero 401", res.Code)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", t2.Token, nil); res.Code != http.StatusOK {
		t.Errorf("o token novo: %d", res.Code)
	}
	if c := b.Contar(t); c.Aparelhos != antes.Aparelhos || c.Tokens != antes.Tokens {
		t.Fatalf("parear de novo: %+v -> %+v; quero a mesma linha e um token só", antes, c)
	}
	// Parear de novo sem revogar (o mesmo PC, outra vez) também troca o token.
	pc.verificador = cloudauth.NovoSegredo()
	t3 := pc.parear(n)
	if t3.DeviceID != t1.DeviceID {
		t.Fatal("parear de novo, sem revogar, criou outro aparelho")
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", t2.Token, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("o token anterior depois de parear sem revogar: %d, quero 401", res.Code)
	}
	if list := aparelhos(t, n); len(list) != 1 || list[0].ID != t1.DeviceID {
		t.Errorf("aparelhos = %+v", list)
	}
	// Outro PC (outro install_id) é outro aparelho.
	if t4 := m.novoPC().parear(n); t4.DeviceID == t1.DeviceID {
		t.Error("outro install_id virou o mesmo aparelho")
	}
}

func testAparelhos180Dias(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	s := sub()
	n.entrarComo(s, m.emailA)
	usado, parado := m.novoPC().parear(n), m.novoPC().parear(n)

	// O usado anda o prazo a cada uso.
	for range 3 {
		m.clk.andar(179 * 24 * time.Hour)
		if res := m.comToken(http.MethodGet, "/api/v1/me", usado.Token, nil); res.Code != http.StatusOK {
			t.Fatalf("token usado a cada 179 dias: %d, quero 200", res.Code)
		}
	}
	// O parado venceu (537 dias sem uso), e não aparece na lista.
	n = m.navegador(uaWindows) // a sessão de antes passou do teto de 90 dias
	n.entrarComo(s, m.emailA)
	list := aparelhos(t, n)
	if len(list) != 1 || list[0].ID != usado.DeviceID {
		t.Errorf("aparelhos = %+v; quero só o usado", list)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", parado.Token, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("token sem uso: %d, quero 401", res.Code)
	}
	// A conta exata: 180 dias depois do último uso já não vale; um segundo
	// antes, vale.
	m.clk.andar(180*24*time.Hour - time.Second)
	if res := m.comToken(http.MethodGet, "/api/v1/me", usado.Token, nil); res.Code != http.StatusOK {
		t.Fatalf("um segundo antes dos 180 dias: %d, quero 200", res.Code)
	}
	m.clk.andar(180 * 24 * time.Hour)
	if res := m.comToken(http.MethodGet, "/api/v1/me", usado.Token, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("180 dias depois do último uso: %d, quero 401", res.Code)
	}
	// Um token que nunca foi usado vence 180 dias depois de nascer.
	n = m.navegador(uaWindows)
	n.entrarComo(s, m.emailA)
	novo := m.novoPC().parear(n)
	m.clk.andar(180 * 24 * time.Hour)
	n = m.navegador(uaWindows)
	n.entrarComo(s, m.emailA)
	if list := aparelhos(t, n); len(list) != 0 {
		t.Errorf("o token nunca usado, 180 dias depois: na lista %+v", list)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", novo.Token, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("o token nunca usado, 180 dias depois: %d, quero 401", res.Code)
	}
}

func testParearArrumar(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	ctx := context.Background()
	pc := m.novoPC()

	// Um pedido esperando e um aprovado sem troca. O Backend é de todos os
	// casos (a arrumação apaga os vencidos de todos): o que conta é se os
	// dois DESTE caso estão no banco.
	noBanco := func(ids ...string) (n int) {
		dump := b.Despejo(t)
		for _, id := range ids {
			if strings.Contains(dump, id) {
				n++
			}
		}
		return n
	}
	esperando := pc.comecar()
	ini := pc.comecar()
	n.aprovar(ini.Pedido)
	m.clk.andar(10*time.Minute - time.Second)
	m.cloud.Arrumar(ctx)
	if got := noBanco(esperando.Pedido, ini.Pedido); got != 2 {
		t.Fatalf("a arrumação apagou pedido que não venceu: %d de 2 no banco", got)
	}
	m.clk.andar(time.Second)
	m.cloud.Arrumar(ctx)
	if got := noBanco(esperando.Pedido, ini.Pedido); got != 0 {
		t.Fatalf("a arrumação deixou %d dos 2 pedidos vencidos", got)
	}
	if !strings.Contains(m.logs.String(), "pedidos=") {
		t.Errorf("a arrumação não conta os pedidos no log:\n%s", m.logs.String())
	}

	// O "apagar meus dados" com um PC pareado e um pedido aprovado sem troca:
	// o PC cai, o aparelho sai da lista, e o pedido aprovado sai.
	tok := pc.parear(n)
	ini = pc.comecar()
	n.aprovar(ini.Pedido)
	esperando = pc.comecar()
	antes := b.Contar(t)
	if res := n.pedir(http.MethodDelete, "/api/v1/me/data", map[string]any{"confirmar": "apagar"}); res.Code != http.StatusNoContent {
		t.Fatalf("apagar: %d", res.Code)
	}
	if res := m.comToken(http.MethodGet, "/api/v1/me", tok.Token, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("o PC depois de apagar os dados: %d, quero 401", res.Code)
	}
	if list := aparelhos(t, n); len(list) != 0 {
		t.Errorf("aparelhos depois de apagar = %+v", list)
	}
	// Saem: o aparelho, o token, o pedido trocado e o aprovado. Fica o que
	// espera (não é de ninguém até alguém aprovar).
	if c := b.Contar(t); c.Aparelhos != antes.Aparelhos-1 || c.Tokens != antes.Tokens-1 || c.Pedidos != antes.Pedidos-2 {
		t.Errorf("apagar: %+v -> %+v; quero um aparelho, um token e dois pedidos a menos", antes, c)
	}
	if res := n.pedir(http.MethodGet, "/api/v1/device-auth/requests/"+esperando.Pedido, nil); res.Code != http.StatusOK {
		t.Errorf("o pedido que esperava: %d, quero 200", res.Code)
	}
}

func testParearLog(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	n := m.navegador(uaWindows)
	n.entrarComo(sub(), m.emailA)
	pc := m.novoPC()
	ini := pc.comecar()
	n.pedir(http.MethodGet, "/api/v1/device-auth/requests/"+ini.Pedido, nil)
	_, code, state := n.aprovar(ini.Pedido)
	pc.trocar(code, cloudauth.NovoSegredo())
	_, out := pc.trocar(code, pc.verificador)
	n.pedir(http.MethodGet, "/api/v1/devices", nil)
	m.comToken(http.MethodDelete, "/api/v1/devices/"+out.DeviceID, out.Token, nil)

	logs := m.logs.String()
	for name, v := range map[string]string{
		"o code": code, "o state": state, "o pedido": ini.Pedido, "o verificador": pc.verificador,
		"o desafio": cloudauth.DesafioS256(pc.verificador), "a conferência": "conferencia=" + ini.Conferencia,
		"o token": out.Token, "o install_id": pc.installID, "o nome do PC": "escritório", "o aparelho": out.DeviceID,
		"o e-mail": m.emailA, "a porta do PC": "51234",
	} {
		if strings.Contains(logs, v) {
			t.Errorf("%s está no log:\n%s", name, logs)
		}
	}
	for _, want := range []string{`rota="POST /api/v1/device-auth/start"`, `rota="GET /api/v1/device-auth/requests/{id}"`,
		`rota="POST /api/v1/device-auth/requests/{id}/approve"`, `rota="POST /api/v1/device-auth/exchange"`,
		`rota="GET /api/v1/devices"`, `rota="DELETE /api/v1/devices/{id}"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("o log não tem %s:\n%s", want, logs)
		}
	}
}

func testParearPagina(t *testing.T, b Backend) {
	m := novoMundo(t, b, opcoes{})
	sem := m.navegador(uaWindows)
	// A página é o app, sem sessão (o app mostra Entrar) e com busca.
	for _, path := range []string{"/device/authorize", "/device/authorize?pedido=" + contracttest.NewID()} {
		res := sem.pedir(http.MethodGet, path, nil)
		if res.Code != http.StatusOK || !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") ||
			res.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(res.Body.String(), `<div id="root">`) {
			t.Errorf("%s: %d %q %q", path, res.Code, res.Header().Get("Content-Type"), res.Header().Get("Cache-Control"))
		}
		if len(res.cookies) != 0 {
			t.Errorf("%s pôs cookie: %v", path, res.cookies)
		}
	}
	// A volta do login para a página passa no ValidarVolta.
	n := m.navegador(uaWindows)
	volta := "/device/authorize?pedido=" + contracttest.NewID()
	code, state := m.g.Autorizar(t, n.comecar(volta), m.g.ClaimsBoas(sub(), m.emailA))
	if res := n.voltar(url.Values{"code": {code}, "state": {state}}); res.Code != http.StatusSeeOther || res.Header().Get("Location") != volta {
		t.Errorf("a volta do login para a aprovação: %d %q", res.Code, res.Header().Get("Location"))
	}
}

// limitesDoPareamento: os limites altos do mundo, com os do pareamento dados.
func limitesDoPareamento(porIP, total int) server.Limites {
	return server.Limites{LoginPorIP: 1000, InicioTotal: 1000, PushPedidos: 1000, PushMutacoes: 100000,
		PareamentoPorIP: porIP, PareamentoTotal: total}
}
