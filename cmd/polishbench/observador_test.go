package main

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func respostaCom(status int, corpo string) roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(corpo))}, nil
	}
}

func chamar(t *testing.T, o *observador) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/v1/responses", nil)
	resp, err := o.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestObservador_ContaStatusELeCorpoDeVolta(t *testing.T) {
	corpo := `{"error":{"message":"m","type":"invalid_request_error","code":"unsupported_value","param":"reasoning.effort"}}`
	o := novoObservador(respostaCom(400, corpo))
	// O cliente lê o MESMO corpo de volta.
	if got := chamar(t, o); got != corpo {
		t.Errorf("o corpo mudou: %q", got)
	}
	chamar(t, o)
	got := o.Contagens()
	if got["400 tipo=invalid_request_error codigo=unsupported_value param=reasoning.effort"] != 2 || len(got) != 1 {
		t.Errorf("contagens = %v", got)
	}
	// Um 200 conta só o status e devolve o corpo sem tocar.
	o2 := novoObservador(respostaCom(200, `{"output":"segredo do ditado"}`))
	if got := chamar(t, o2); got != `{"output":"segredo do ditado"}` {
		t.Errorf("o corpo do 200 mudou: %q", got)
	}
	if c := o2.Contagens(); c["200"] != 1 || len(c) != 1 {
		t.Errorf("contagens do 200 = %v", c)
	}
	if len(o2.Mensagens()) != 0 {
		t.Error("um 200 não tem mensagem")
	}
}

func TestObservador_ErroDeRede(t *testing.T) {
	o := novoObservador(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("ECONNRESET com detalhes") }))
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/", nil)
	if _, err := o.RoundTrip(req); err == nil {
		t.Fatal("o erro sumiu")
	}
	if got := o.Contagens(); got["erro de rede ou prazo"] != 1 || len(got) != 1 {
		t.Errorf("contagens = %v", got)
	}
}

// Os identificadores só entram se forem identificadores. O que parece chave (ou
// qualquer coisa que o provedor invente) vira "(outro)".
func TestIdentificadoresDoErro(t *testing.T) {
	casos := []struct{ corpo, quero string }{
		{`{"error":{"type":"invalid_request_error","code":"invalid_api_key","param":null}}`, " tipo=invalid_request_error codigo=invalid_api_key"},
		{`{"error":{"type":"invalid_request_error","code":null,"param":"reasoning.effort"}}`, " tipo=invalid_request_error param=reasoning.effort"},
		{`{"type":"error","error":{"type":"rate_limit_error","details":{"error_code":"enforced_spend_limit_reached"}}}`, " tipo=rate_limit_error detalhe=enforced_spend_limit_reached"},
		{`{"error":{"code":"sk-proj-AbC123xyz","type":"Invalid","param":"a b"}}`, " tipo=(outro) codigo=(outro) param=(outro)"},
		{`{"error":{"code":"insufficient_quota_2"}}`, " codigo=(outro)"}, // dígito: não é identificador
		{`{"error":{"code":"` + strings.Repeat("a", 49) + `"}}`, " codigo=(outro)"},
		{`{"error":{"code":12,"type":["x"]}}`, ""}, // não é texto
		{`{"error":{"message":"só mensagem"}}`, ""},
		{`não é json`, ""},
		{``, ""},
		{`{"error":"texto"}`, ""},
	}
	for _, c := range casos {
		if got := identificadoresDoErro([]byte(c.corpo)); got != c.quero {
			t.Errorf("%s -> %q, quero %q", c.corpo, got, c.quero)
		}
	}
}

func TestMensagemSegura(t *testing.T) {
	longo := strings.Repeat("palavra ", 60)
	casos := []struct {
		nome, entrada, quero string
	}{
		{"texto simples", "Unsupported value: 'reasoning.effort' does not support 'none'.", "Unsupported value: 'reasoning.effort' does not support 'none'."},
		{"pedaço de chave da OpenAI", "Incorrect API key provided: sk-proj-********************************************7Q8A. Find it", "Incorrect API key provided: …. Find it"},
		{"chave inteira", "invalid x-api-key sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789", "invalid x-api-key …"},
		{"id de organização", "Rate limit reached in organization org-AbCdEfGhIjKlMnOpQrStUv on tokens", "Rate limit reached in organization … on tokens"},
		{"19 caracteres ficam, 20 somem", "abcdefghijklmnopqrs abcdefghijklmnopqrst", "abcdefghijklmnopqrs …"},
		{"controle e quebra de linha viram espaço", "linha um\nlinha\tdois\x00três\x1b[0m", "linha um linha dois três [0m"},
		{"espaço a mais junta", "  a    b   ", "a b"},
		{"vazio", "", ""},
	}
	for _, c := range casos {
		if got := mensagemSegura(c.entrada); got != c.quero {
			t.Errorf("%s: %q, quero %q", c.nome, got, c.quero)
		}
	}
	got := mensagemSegura(longo)
	if n := len([]rune(got)); n != maxRunasDaMensagem+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("mensagem longa: %d runas, quero %d com reticências", n, maxRunasDaMensagem+1)
	}
}

// A mensagem só é guardada nos erros de PEDIDO (400, 404, 422). Nos de
// credencial (401, 403) é onde a OpenAI cita a chave; nos de limite e de
// servidor não há o que guardar.
func TestObservador_MensagemSoNosErrosDePedido(t *testing.T) {
	corpo := `{"error":{"message":"Incorrect API key provided: sk-proj-AAAAAAAAAAAAAAAAAAAAAAAA. Motivo comum","type":"invalid_request_error","code":"x"}}`
	for status, quer := range map[int]bool{400: true, 404: true, 422: true, 401: false, 403: false, 402: false, 429: false, 500: false, 503: false} {
		o := novoObservador(respostaCom(status, corpo))
		chamar(t, o)
		got := o.Mensagens()
		if quer && len(got) != 1 {
			t.Errorf("status %d: mensagens = %v, quero uma", status, got)
		}
		if !quer && len(got) != 0 {
			t.Errorf("status %d: guardou mensagem %v", status, got)
		}
		for m := range got {
			if strings.Contains(m, "sk-proj") {
				t.Errorf("status %d: a mensagem trouxe a chave: %q", status, m)
			}
		}
	}
	o := novoObservador(respostaCom(400, corpo))
	chamar(t, o)
	chamar(t, o)
	if got := o.Mensagens()["400: Incorrect API key provided: …. Motivo comum"]; got != 2 {
		t.Errorf("mensagens = %v, quero a mesma mensagem 2 vezes", o.Mensagens())
	}
}

func TestObservador_TetoDeMensagensDistintas(t *testing.T) {
	n := 0
	o := novoObservador(roundTripFunc(func(*http.Request) (*http.Response, error) {
		n++
		corpo := `{"error":{"message":"erro numero ` + strconv.Itoa(n) + `"}}`
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(corpo))}, nil
	}))
	for i := 0; i < 200; i++ {
		chamar(t, o)
	}
	if got := len(o.Mensagens()); got > maxMensagensDistintas {
		t.Errorf("guardou %d mensagens distintas, o teto é %d", got, maxMensagensDistintas)
	}
	// Uma mensagem que já está guardada continua contando depois do teto.
	total := 0
	for _, v := range o.Mensagens() {
		total += v
	}
	if total < maxMensagensDistintas {
		t.Errorf("total = %d", total)
	}
}

func TestErroDePedido(t *testing.T) {
	for _, s := range []int{400, 404, 422} {
		if !erroDePedido(s) {
			t.Errorf("%d devia ser erro de pedido", s)
		}
	}
	for _, s := range []int{200, 401, 402, 403, 413, 429, 500, 529} {
		if erroDePedido(s) {
			t.Errorf("%d não devia ser erro de pedido", s)
		}
	}
}

func TestContagensOrdenadasECopia(t *testing.T) {
	m := map[string]int{"b": 1, "a": 2, "c": 3}
	got := contagensOrdenadas(m)
	if strings.Join(got, "") != "abc" {
		t.Errorf("ordem = %v", got)
	}
	o := novoObservador(respostaCom(200, "{}"))
	chamar(t, o)
	c := o.Contagens()
	c["200"] = 99
	if o.Contagens()["200"] != 1 {
		t.Error("Contagens devolveu o mapa interno")
	}
}
