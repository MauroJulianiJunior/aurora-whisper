//go:build logindeteste

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O servidor de teste (go test -tags logindeteste ./cmd/server): sobe na
// memória, sem o Google, com a conta de teste e os ditados de exemplo, serve
// o app e desliga limpo. Nenhum segredo sai na tela nem no log.
func TestTeste_SobeComOsExemplosEDesliga(t *testing.T) {
	env := map[string]string{envPainelDir: painelDir(t), envHTTPAddr: "127.0.0.1:0"}
	addrCh := make(chan net.Addr, 1)
	onListen = func(a net.Addr) { addrCh <- a }
	t.Cleanup(func() { onListen = nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr safeBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"teste"}, func(k string) string { return env[k] }, &stdout, &stderr) }()
	var addr net.Addr
	select {
	case addr = <-addrCh:
	case code := <-done:
		t.Fatalf("o teste saiu com %d antes de escutar: %s", code, stderr.String())
	case <-time.After(20 * time.Second):
		t.Fatal("o teste não escutou em 20 s")
	}
	host := "localhost:" + strconv.Itoa(addr.(*net.TCPAddr).Port)

	pedir := func(method, path, body, cookie string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+addr.String()+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(raw)
	}

	if resp, body := pedir(http.MethodGet, "/", "", ""); resp.StatusCode != http.StatusOK || !strings.Contains(body, `<div id="root">`) {
		t.Fatalf("o app: %d %s", resp.StatusCode, body)
	}
	if resp, body := pedir(http.MethodGet, "/auth/teste", "", ""); resp.StatusCode != http.StatusOK ||
		strings.TrimSpace(body) != `{"conta":"voce@teste.local","google":false}` {
		t.Fatalf("GET /auth/teste: %d %s", resp.StatusCode, body)
	}
	if resp, _ := pedir(http.MethodPost, "/auth/start", "{}", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /auth/start no servidor de teste: %d, quero 404 (sem Google)", resp.StatusCode)
	}
	resp, body := pedir(http.MethodPost, "/auth/teste", `{"email":"voce@teste.local"}`, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /auth/teste: %d %s", resp.StatusCode, body)
	}
	var sessao string
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-sessao" {
			sessao = c.Name + "=" + c.Value
		}
	}
	if sessao == "" {
		t.Fatal("o login de teste não abriu a sessão")
	}
	// O login de teste cai na conta de id fixo (a D3 da F6: o PC ligado a ela
	// reconecta depois de a nuvem de teste reiniciar).
	if resp, body := pedir(http.MethodGet, "/api/v1/me", "", sessao); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/me: %d %s", resp.StatusCode, body)
	} else {
		var eu struct{ ID, Email string }
		if err := json.Unmarshal([]byte(body), &eu); err != nil || eu.ID != IDDaContaDeTeste || eu.Email != ContaDeTeste {
			t.Errorf("GET /api/v1/me = %s (%v); quero o id %s", body, err, IDDaContaDeTeste)
		}
	}

	type item struct {
		FinalText string           `json:"final_text"`
		DeviceID  *string          `json:"device_id"`
		Selo      *json.RawMessage `json:"versao_em_conflito"`
	}
	listar := func(q string) []item {
		resp, body := pedir(http.MethodGet, "/api/v1/transcriptions"+q, "", sessao)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("lista%s: %d %s", q, resp.StatusCode, body)
		}
		var p struct{ Items []item }
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		return p.Items
	}
	historico, lixeira := listar(""), listar("?trash=true")
	if len(historico) != len(exemplos)-1 || len(lixeira) != 1 || lixeira[0].FinalText != "Rascunho antigo que não serve mais." {
		t.Fatalf("histórico com %d e lixeira com %v; quero %d e o rascunho", len(historico), lixeira, len(exemplos)-1)
	}
	selos, pcs := 0, map[string]bool{}
	for _, it := range historico {
		if it.Selo != nil && string(*it.Selo) != "null" {
			selos++
			if it.FinalText != "Confirmo a entrega para quinta à tarde." || !strings.Contains(string(*it.Selo), "Confirmo a entrega para sexta de manhã.") {
				t.Errorf("o selo = %s sobre %q", *it.Selo, it.FinalText)
			}
		}
		if it.DeviceID != nil {
			pcs[*it.DeviceID] = true
		}
	}
	if selos != 1 || len(pcs) != 2 {
		t.Errorf("%d selos e %d PCs; quero 1 selo e os ditados de 2 PCs", selos, len(pcs))
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("o desligamento saiu com %d: %s", code, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("o teste não desligou em 20 s")
	}
	out := stdout.String()
	if !strings.Contains(out, "Nuvem de teste em http://"+host) || !strings.Contains(out, "voce@teste.local") {
		t.Errorf("stdout = %q", out)
	}
	// As chaves do servidor (256 bits em base64url, 43 caracteres) e o cookie
	// não aparecem em lugar nenhum.
	segredo := regexp.MustCompile(`[A-Za-z0-9_-]{43}`)
	if s := segredo.FindString(out + stderr.String()); s != "" {
		t.Errorf("a saída tem algo com cara de segredo: %q", s)
	}
	if strings.Contains(out+stderr.String(), strings.TrimPrefix(sessao, "__Host-sessao=")) {
		t.Error("a saída tem o cookie da sessão")
	}
	if !strings.Contains(stderr.String(), `"msg":"servindo"`) || !strings.Contains(stderr.String(), `"msg":"desligado"`) {
		t.Errorf("o log = %s", stderr.String())
	}
	// A retenção roda na subida (F9d): a linha sai antes de "desligado", só
	// com contagens (os exemplos são de hoje, então nada vence), sem o id da
	// conta nem texto ditado.
	logs := stderr.String()
	ret, desl := strings.Index(logs, `"msg":"retenção"`), strings.Index(logs, `"msg":"desligado"`)
	if ret < 0 || ret > desl {
		t.Fatalf("a linha \"retenção\" não saiu antes de \"desligado\":\n%s", logs)
	}
	var linha map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(logs[strings.LastIndex(logs[:ret], "\n")+1:], "\n", 2)[0]), &linha); err != nil {
		t.Fatalf("a linha da retenção não é JSON: %v", err)
	}
	for _, k := range []string{"ativos", "lixeira", "lapides_criadas", "lapides_vencidas", "eventos", "mutacoes", "falhas"} {
		if linha[k] != float64(0) {
			t.Errorf("retenção na subida: %s = %v, quero 0", k, linha[k])
		}
	}
	if u, _ := linha["usuarios"].(float64); u > 1 {
		t.Errorf("retenção na subida: %v usuários, quero só a conta de teste", linha["usuarios"])
	}
	for _, proibido := range []string{IDDaContaDeTeste, "Bom dia, pessoal", "Rascunho antigo"} {
		if strings.Contains(logs, proibido) {
			t.Errorf("o log traz %q", proibido)
		}
	}
}

// Cada subida da nuvem de teste cria a conta de teste com o MESMO id: o PC
// conectado numa subida (que guarda a conta, a D3 da F6) conecta de novo na
// seguinte.
func TestTeste_ContaDeTesteComOMesmoIDACadaSubida(t *testing.T) {
	ctx := context.Background()
	for subida := 1; subida <= 2; subida++ {
		mem := cloudstore.NewMemoryStore()
		if err := semearExemplos(ctx, mem, time.Now(), subida == 1); err != nil {
			t.Fatalf("subida %d: %v", subida, err)
		}
		u, err := mem.EntrarComGoogle(ctx, cloudstore.Identidade{Sub: "teste:" + ContaDeTeste, Email: ContaDeTeste, Nome: "Conta de teste"}, time.Now())
		if err != nil || u.ID != IDDaContaDeTeste {
			t.Errorf("subida %d: a conta de teste = %q (%v); quero %s", subida, u.ID, err, IDDaContaDeTeste)
		}
		if c := mem.Contar(); c.Usuarios != 1 {
			t.Errorf("subida %d: %d contas; quero só a de teste", subida, c.Usuarios)
		}
	}
}

// Sem exemplos (TESTE_SEM_EXEMPLOS=1, o roteiro da F7): só a conta de teste,
// nenhum ditado nem aparelho, para nada inventado descer ao histórico do bin\.
func TestTeste_SemExemplosSoAConta(t *testing.T) {
	ctx := context.Background()
	com := cloudstore.NewMemoryStore()
	if err := semearExemplos(ctx, com, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if c := com.Contar(); c.Ditados == 0 || c.Aparelhos == 0 {
		t.Fatalf("com exemplos: %+v; quero ditados e aparelhos", c)
	}
	sem := cloudstore.NewMemoryStore()
	if err := semearExemplos(ctx, sem, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if c := sem.Contar(); c.Usuarios != 1 || c.Contadores != 1 || c.Ditados != 0 || c.Aparelhos != 0 || c.Eventos != 0 {
		t.Errorf("sem exemplos: %+v; quero só a conta de teste", c)
	}
}

// Só na própria máquina, e só com o build do painel.
func TestTeste_ConfiguracaoErrada(t *testing.T) {
	dir := painelDir(t)
	for name, env := range map[string]map[string]string{
		"sem PAINEL_DIR":        {envHTTPAddr: "127.0.0.1:0"},
		"PAINEL_DIR sem index":  {envPainelDir: t.TempDir(), envHTTPAddr: "127.0.0.1:0"},
		"HTTP_ADDR na rede":     {envPainelDir: dir, envHTTPAddr: "0.0.0.0:0"},
		"HTTP_ADDR sem máquina": {envPainelDir: dir, envHTTPAddr: ":0"},
		"HTTP_ADDR sem porta":   {envPainelDir: dir, envHTTPAddr: "127.0.0.1"},
	} {
		if _, errOut, code := runCmd(t, env, "teste"); code != 2 || !strings.HasPrefix(errOut, "teste: ") {
			t.Errorf("%s: %d %q, quero 2", name, code, errOut)
		}
	}
	if _, errOut, code := runCmd(t, map[string]string{envPainelDir: dir}, "teste", "agora"); code != 2 || !strings.Contains(errOut, "argumento inesperado") {
		t.Errorf("com argumento: %d %q", code, errOut)
	}
}
