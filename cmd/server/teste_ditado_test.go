//go:build logindeteste

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O ditado no celular no servidor de teste do PC (ADR-021, C3-correções): o
// cofre por processo e o provedor de mentira. Só roda com a build tag
// logindeteste (go test -tags logindeteste ./cmd/server); sem ela, o que prova
// que nada disto existe no build normal é teste_off_test.go.

// nuvemDeTeste sobe o servidor de teste (a mesma subida de TestTeste_*), sem os
// ditados de exemplo, e devolve o que um teste de HTTP precisa. O desligamento
// fica por conta do t.Cleanup, e a saída (stdout e stderr) é lida por saida.
type nuvemDeTeste struct {
	t     *testing.T
	addr  net.Addr
	host  string
	saida func() string
}

func subirNuvemDeTeste(t *testing.T) *nuvemDeTeste {
	t.Helper()
	env := map[string]string{envPainelDir: painelDir(t), envHTTPAddr: "127.0.0.1:0", envSemExemplos: "1"}
	addrCh := make(chan net.Addr, 1)
	onListen = func(a net.Addr) { addrCh <- a }
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr safeBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"teste"}, func(k string) string { return env[k] }, &stdout, &stderr) }()
	t.Cleanup(func() {
		cancel()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("o desligamento saiu com %d: %s", code, stderr.String())
			}
		case <-time.After(20 * time.Second):
			t.Error("o servidor de teste não desligou em 20 s")
		}
		onListen = nil
	})
	select {
	case addr := <-addrCh:
		return &nuvemDeTeste{t: t, addr: addr, host: "localhost:" + strconv.Itoa(addr.(*net.TCPAddr).Port),
			saida: func() string { return stdout.String() + stderr.String() }}
	case code := <-done:
		t.Fatalf("o teste saiu com %d antes de escutar: %s", code, stderr.String())
	case <-time.After(20 * time.Second):
		t.Fatal("o teste não escutou em 20 s")
	}
	return nil
}

// pedir faz um pedido HTTP de verdade à nuvem de teste, com o cookie da sessão
// (se houver) e os cabeçalhos extras, em pares nome, valor.
func (n *nuvemDeTeste) pedir(method, path, body, cookie string, cabecalhos ...string) (*http.Response, string) {
	n.t.Helper()
	req, err := http.NewRequest(method, "http://"+n.addr.String()+path, strings.NewReader(body))
	if err != nil {
		n.t.Fatal(err)
	}
	req.Host = n.host
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for i := 0; i+1 < len(cabecalhos); i += 2 {
		req.Header.Set(cabecalhos[i], cabecalhos[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(raw)
}

// entrar faz o login de teste e devolve o cookie da sessão.
func (n *nuvemDeTeste) entrar() string {
	n.t.Helper()
	resp, body := n.pedir(http.MethodPost, "/auth/teste", `{"email":"voce@teste.local"}`, "")
	if resp.StatusCode != http.StatusNoContent {
		n.t.Fatalf("POST /auth/teste: %d %s", resp.StatusCode, body)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-sessao" {
			return c.Name + "=" + c.Value
		}
	}
	n.t.Fatal("o login de teste não abriu a sessão")
	return ""
}

// O ditado no celular funciona no servidor de teste: as rotas /dictation e
// /ai-keys existem (não são 404), o cofre é do processo, e o provedor é de
// mentira. O caminho inteiro de um primeiro uso, pela rede de verdade: o
// estado, o PUT da chave sem o aceite, o aceite, a chave, a lista de modelos, as
// falhas do provedor de mentira e desligar o ditado. Nenhuma chave sai em lugar
// nenhum da saída.
func TestTeste_DitadoNoCelularFuncionaComOCofreEOProvedorDeMentira(t *testing.T) {
	n := subirNuvemDeTeste(t)
	sessao := n.entrar()
	const chave = "sk-teste-de-mentira-0123456789abcdef"

	resp, body := n.pedir(http.MethodGet, "/api/v1/dictation", "", sessao)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/dictation: %d %s (as rotas do ditado no celular não subiram?)", resp.StatusCode, body)
	}
	var estado struct {
		Aviso  struct{ Versao int }
		Aceite *json.RawMessage
		Chaves []struct {
			Provedor    string
			Configurada bool
			Final       string
		}
	}
	if err := json.Unmarshal([]byte(body), &estado); err != nil || estado.Aviso.Versao != cloudstore.VersaoDoAviso ||
		(estado.Aceite != nil && string(*estado.Aceite) != "null") || len(estado.Chaves) != 3 {
		t.Fatalf("o estado inicial = %s (%v)", body, err)
	}

	// Sem o aceite, a chave não entra (o aceite de verdade, o da conta).
	resp, body = n.pedir(http.MethodPut, "/api/v1/ai-keys/openai", `{"chave":"`+chave+`"}`, sessao)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, `"SEM_ACEITE"`) {
		t.Fatalf("PUT sem aceite: %d %s", resp.StatusCode, body)
	}
	resp, body = n.pedir(http.MethodPost, "/api/v1/dictation/consent",
		`{"versao":`+strconv.Itoa(cloudstore.VersaoDoAviso)+`,"transcricao":"openai"}`, sessao)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /dictation/consent: %d %s", resp.StatusCode, body)
	}
	resp, body = n.pedir(http.MethodPut, "/api/v1/ai-keys/openai", `{"chave":"`+chave+`"}`, sessao)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"final":"cdef"`) || strings.Contains(body, chave) {
		t.Fatalf("PUT com aceite: %d %s", resp.StatusCode, body)
	}
	// A lista de modelos é a do provedor de mentira, para o mesmo provedor.
	resp, body = n.pedir(http.MethodGet, "/api/v1/ai-keys/openai/models", "", sessao, "Sec-Fetch-Site", "same-origin")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"gpt-4o-mini-transcribe"`) || !strings.Contains(body, `"whisper-1"`) {
		t.Fatalf("GET models: %d %s", resp.StatusCode, body)
	}
	// E só da própria origem: o de outro site é 403.
	resp, body = n.pedir(http.MethodGet, "/api/v1/ai-keys/openai/models", "", sessao, "Sec-Fetch-Site", "cross-site")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, `"FORBIDDEN"`) {
		t.Fatalf("GET models de outro site: %d %s", resp.StatusCode, body)
	}

	// As falhas do provedor de mentira, pelo começo da chave.
	for prefixo, c := range map[string]struct {
		status int
		code   string
	}{
		chaveQueRecusa:  {http.StatusUnprocessableEntity, "PROVEDOR_RECUSOU_CHAVE"},
		chaveSemCredito: {http.StatusBadGateway, "PROVEDOR_LIMITE"},
		chaveQueCai:     {http.StatusBadGateway, "PROVEDOR_FORA"},
	} {
		ruim := prefixo + "-0123456789abcdef"
		resp, body = n.pedir(http.MethodPut, "/api/v1/ai-keys/openai", `{"chave":"`+ruim+`"}`, sessao)
		if resp.StatusCode != c.status || !strings.Contains(body, `"`+c.code+`"`) || strings.Contains(body, ruim) {
			t.Errorf("PUT com a chave %q: %d %s, quero %d %s", prefixo, resp.StatusCode, body, c.status, c.code)
		}
	}

	// Desligar o ditado leva a chave, e o estado diz isso.
	if resp, body = n.pedir(http.MethodDelete, "/api/v1/dictation/consent", "", sessao); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /dictation/consent: %d %s", resp.StatusCode, body)
	}
	resp, body = n.pedir(http.MethodGet, "/api/v1/dictation", "", sessao)
	if err := json.Unmarshal([]byte(body), &estado); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /dictation depois de desligar: %d %s (%v)", resp.StatusCode, body, err)
	}
	for _, c := range estado.Chaves {
		if c.Configurada || c.Final != "" {
			t.Errorf("depois de desligar, a chave %s continua guardada: %+v", c.Provedor, c)
		}
	}

	// Sem sessão, 401, como o resto.
	if resp, _ = n.pedir(http.MethodGet, "/api/v1/dictation", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /dictation sem sessão: %d, quero 401", resp.StatusCode)
	}

	// Nem a chave colada nem a chave de cifra do processo (64 caracteres
	// hexadecimais) aparecem na saída.
	saida := n.saida()
	if strings.Contains(saida, chave) || regexp.MustCompile(`[0-9a-fA-F]{64}`).MatchString(saida) {
		t.Errorf("a saída tem a chave colada ou a de cifra: %s", saida)
	}
	if !strings.Contains(saida, "provedor de mentira") {
		t.Errorf("a saída não avisa que o provedor é de mentira: %s", saida)
	}
}

// Cada subida tem a sua chave de cifra: o cofre de um processo não abre o de
// outro (o id da chave de cifra muda a cada subida), como o SESSION_SECRET.
func TestTeste_OCofreDeTesteENovoACadaSubida(t *testing.T) {
	a, err := novoCofreDeTeste()
	if err != nil {
		t.Fatal(err)
	}
	b, err := novoCofreDeTeste()
	if err != nil {
		t.Fatal(err)
	}
	if a.MesmoID(b.ID()) {
		t.Fatal("duas subidas com a mesma chave de cifra")
	}
	selada, err := a.Selar(IDDaContaDeTeste, cloudstore.ProvedorOpenAI, "sk-teste-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Abrir(IDDaContaDeTeste, cloudstore.ProvedorOpenAI, selada); err == nil {
		t.Fatal("o cofre de outra subida abriu a chave")
	}
}

// O provedor de mentira não chama rede, devolve a lista de cada provedor e
// recusa o que não conhece.
func TestTeste_OProvedorDeMentira(t *testing.T) {
	ctx := context.Background()
	for _, p := range cloudstore.ProvedoresDeIA {
		lista, err := modelosDeTeste{}.ListarModelos(ctx, p, "sk-qualquer-0123456789")
		if err != nil || len(lista) == 0 {
			t.Errorf("%s: %v, %v", p, lista, err)
			continue
		}
		// A lista devolvida é de quem a recebe.
		lista[0] = "mudei"
		if outra, _ := (modelosDeTeste{}).ListarModelos(ctx, p, "sk-qualquer-0123456789"); outra[0] == "mudei" {
			t.Errorf("%s: mudar a lista devolvida mudou a do provedor", p)
		}
	}
	if _, err := (modelosDeTeste{}).ListarModelos(ctx, "xai", "sk-qualquer"); err == nil {
		t.Error("provedor desconhecido")
	}
	for prefixo, motivo := range map[string]ia.Motivo{
		chaveQueRecusa: ia.MotivoChaveRecusada, chaveSemCredito: ia.MotivoLimite, chaveQueCai: ia.MotivoIndisponivel,
	} {
		_, err := modelosDeTeste{}.ListarModelos(ctx, "openai", prefixo+"-x")
		var e *ia.Erro
		if !errors.As(err, &e) || e.Motivo != motivo || strings.Contains(err.Error(), prefixo+"-x") {
			t.Errorf("%s: %v", prefixo, err)
		}
	}
}

// A rota de ditar (ADR-021, C5b) funciona no servidor de teste, com a
// transcrição e o Polish de mentira: o ditado sai 201 com o texto de mentira,
// passa pelo Polish de mentira (mode polish), e o reenvio do mesmo id sai 200.
// Nada sai da máquina, e nem a chave, nem o áudio, nem o texto aparecem na
// saída.
func TestTeste_DitarComATranscricaoEOPolishDeMentira(t *testing.T) {
	n := subirNuvemDeTeste(t)
	sessao := n.entrar()
	const chave = "sk-teste-de-mentira-0123456789abcdef"
	versao := strconv.Itoa(cloudstore.VersaoDoAviso)
	for _, p := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/dictation/consent", `{"versao":` + versao + `,"transcricao":"openai","polish":"openai"}`},
		{http.MethodPut, "/api/v1/ai-keys/openai", `{"chave":"` + chave + `"}`},
		{http.MethodPut, "/api/v1/dictation/settings", `{"transcricao":{"provedor":"openai","modelo":""},"dica":false,"polish":{"ligado":true,"provedor":"openai","modelo":"gpt-4o-mini"}}`},
	} {
		if resp, body := n.pedir(p.method, p.path, p.body, sessao, "Sec-Fetch-Site", "same-origin"); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s: %d %s", p.method, p.path, resp.StatusCode, body)
		}
	}
	audio := "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\xf2\x81\x04\x42\xf3\x81\x08\x42\x82\x84webm\x42\x87\x81\x04\x42\x85\x81\x02" +
		strings.Repeat("AUDIO-DE-MENTIRA-", 100)
	id := cloudstore.NewUUIDv7()
	ditar := func() (*http.Response, string) {
		return n.pedir(http.MethodPost, "/api/v1/dictations", audio, sessao, "Sec-Fetch-Site", "same-origin",
			"Content-Type", "audio/webm", "Ditado-Id", id, "Ditado-Duracao-Ms", "3000", "Ditado-Fala-Ms", "2000")
	}
	resp, body := ditar()
	var d struct {
		ID, SourcePlatform, RawText, FinalText, Mode string
		DeviceID                                     *string
	}
	if err := json.Unmarshal([]byte(body), &d); err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /dictations: %d %s (%v)", resp.StatusCode, body, err)
	}
	if !strings.Contains(body, `"source_platform":"web"`) || !strings.Contains(body, `"device_id":null`) ||
		!strings.Contains(body, `"raw_text":"`+TextoDeMentira+`"`) || !strings.Contains(body, `"mode":"polish"`) ||
		!strings.Contains(body, `"final_text":"Este é um ditado de mentira do servidor de teste pelo celular."`) {
		t.Fatalf("o ditado de mentira: %s", body)
	}
	if resp, body = ditar(); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"id":"`+id+`"`) {
		t.Fatalf("o reenvio: %d %s", resp.StatusCode, body)
	}
	saida := n.saida()
	if strings.Contains(saida, chave) || strings.Contains(saida, "AUDIO-DE-MENTIRA") || strings.Contains(saida, TextoDeMentira) {
		t.Errorf("a saída tem a chave, o áudio ou o texto: %s", saida)
	}
	if !strings.Contains(saida, `"msg":"ditado"`) {
		t.Errorf("a saída não tem a linha \"ditado\": %s", saida)
	}
}

// A transcrição e o Polish de mentira não chamam rede: respondem pelo
// transporte, e a chave que começa com "recusada", "sem-credito" ou
// "fora-do-ar" vira a falha correspondente.
func TestTeste_ATranscricaoEOPolishDeMentira(t *testing.T) {
	for prefixo, status := range map[string]int{
		chaveQueRecusa: http.StatusUnauthorized, chaveSemCredito: http.StatusTooManyRequests,
		chaveQueCai: http.StatusServiceUnavailable, "sk-boa": http.StatusOK,
	} {
		req, _ := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/audio/transcriptions", strings.NewReader("x"))
		req.Header.Set("Authorization", "Bearer "+prefixo+"-0123456789")
		resp, err := vozDeMentira{}.RoundTrip(req)
		if err != nil || resp.StatusCode != status {
			t.Errorf("voz %s: %v %v", prefixo, resp, err)
		}
		req, _ = http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages",
			strings.NewReader(`{"messages":[{"role":"user","content":"{\"ditado\":\"oi tudo bem com você hoje\"}"}]}`))
		req.Header.Set("x-api-key", prefixo+"-0123456789")
		resp, err = polishDeMentira{}.RoundTrip(req)
		if err != nil || resp.StatusCode != status {
			t.Errorf("Polish %s: %v %v", prefixo, resp, err)
			continue
		}
		if status == http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(raw), `Oi tudo bem com você hoje.`) || !strings.Contains(string(raw), `"end_turn"`) {
				t.Errorf("o Polish de mentira da Anthropic: %s", raw)
			}
		}
	}
}
