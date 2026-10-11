package ia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Nenhum teste deste arquivo sai da máquina: todo provedor é um
// httptest.Server em loopback, e a "chave" (ia_test.go) não é uma chave.

// ditado é o texto que o teste manda; nenhum pedaço dele pode aparecer fora
// do corpo do pedido (nem em erro, nem em log).
const ditado = "o cliente Zephyrino pediu a DCTFWeb de agosto ate sexta"

// pedidoReal monta o PolishRequest como resolver.Polir monta.
func pedidoReal(texto string) resolver.PolishRequest {
	return resolver.PolishRequest{
		Text:       texto,
		Instrucoes: resolver.PolishPrompt,
		Mensagem:   resolver.PolishMensagemUsuario(texto),
		Schema:     resolver.PolishSchemaSaida(),
		PromptRef:  resolver.PolishPromptRef,
	}
}

// visto é um pedido que chegou ao servidor falso.
type visto struct {
	metodo, caminho, url string
	cab                  http.Header
	corpo                []byte
}

// falso é um provedor de mentira: guarda cada pedido e responde como o teste
// mandar.
type falso struct {
	*Cliente
	url    string
	mu     sync.Mutex
	vistos []visto
}

func novoFalso(t *testing.T, resposta http.HandlerFunc) *falso {
	t.Helper()
	f := &falso{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.vistos = append(f.vistos, visto{r.Method, r.URL.Path, r.URL.String(), r.Header.Clone(), corpo})
		f.mu.Unlock()
		resposta(w, r)
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	f.Cliente = &Cliente{BaseOpenAI: srv.URL, BaseAnthropic: srv.URL}
	return f
}

// fixo responde sempre a mesma coisa.
func fixo(status int, corpo string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, corpo)
	}
}

func (f *falso) pedidos() []visto {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]visto(nil), f.vistos...)
}

func (f *falso) unico(t *testing.T) visto {
	t.Helper()
	p := f.pedidos()
	if len(p) != 1 {
		t.Fatalf("o provedor recebeu %d pedidos, quero exatamente 1 (sem retry)", len(p))
	}
	return p[0]
}

func decodificar(t *testing.T, corpo []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(corpo, &m); err != nil {
		t.Fatalf("o corpo enviado não é um objeto JSON: %v\n%s", err, corpo)
	}
	return m
}

func json2(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// schemaEsperado é escrito à mão, não vem de resolver.PolishSchemaSaida: se
// o schema do resolver mudar sem querer, este teste vê.
func schemaEsperado() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"final_text": map[string]any{"type": "string"},
		},
		"required":             []any{"final_text"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------
// Respostas dos provedores
// ---------------------------------------------------------------------

// saida é o JSON que o modelo escreve.
func saida(final string) string {
	b, _ := json.Marshal(map[string]string{"final_text": final})
	return string(b)
}

// respostaOpenAI é uma resposta completa da Responses API: um item de
// raciocínio ANTES da mensagem, como a API devolve, para o texto não poder
// ser lido de output[0].content[0].
func respostaOpenAI(texto string) string {
	b, _ := json.Marshal(map[string]any{
		"id": "resp_1", "object": "response", "status": "completed",
		"error": nil, "incomplete_details": nil,
		"output": []any{
			map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{}},
			map[string]any{
				"type": "message", "id": "msg_1", "status": "completed", "role": "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": texto, "annotations": []any{}}},
			},
		},
		"usage": map[string]any{
			"input_tokens": 1234, "output_tokens": 56,
			"output_tokens_details": map[string]any{"reasoning_tokens": 7},
		},
	})
	return string(b)
}

func respostaAnthropic(texto, parada string) string {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
		"content":     []any{map[string]any{"type": "text", "text": texto}},
		"stop_reason": parada, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 321, "output_tokens": 45},
	})
	return string(b)
}

// eco é a mensagem de erro com a chave e o ditado dentro, como a OpenAI faz
// no 401. Nada disto pode sair de Polir.
func eco() string { return "Incorrect API key provided: " + chave + " ditado: " + ditado }

func erroOpenAI(tipo, codigo string) string {
	var cod any
	if codigo != "" {
		cod = codigo
	}
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": eco(), "type": tipo, "param": nil, "code": cod}})
	return string(b)
}

// erroOpenAIParam é o erro de um parâmetro que o modelo não aceita: a OpenAI
// diz qual campo em error.param.
func erroOpenAIParam(tipo, codigo, param string) string {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": eco(), "type": tipo, "param": param, "code": codigo}})
	return string(b)
}

func erroAnthropic(tipo, mensagem string, detalhes map[string]any) string {
	e := map[string]any{"type": tipo, "message": mensagem + " " + eco()}
	if detalhes != nil {
		e["details"] = detalhes
	}
	b, _ := json.Marshal(map[string]any{"type": "error", "error": e, "request_id": "req_1"})
	return string(b)
}

// chamada de cada provedor nos testes.
var (
	chamadaOpenAI    = Chamada{Provedor: OpenAI, Modelo: "modelo-openai"}
	chamadaAnthropic = Chamada{Provedor: Anthropic, Modelo: "modelo-anthropic"}
)

func chamadaDe(p string) Chamada {
	if p == Anthropic {
		return chamadaAnthropic
	}
	return chamadaOpenAI
}

// motivoDe extrai o motivo do erro de Polir e confere que o erro é só isso.
func motivoDe(t *testing.T, err error) resolver.MotivoFallback {
	t.Helper()
	var e *resolver.ErroPolish
	if !errors.As(err, &e) {
		t.Fatalf("err = %v (%T), quero *resolver.ErroPolish", err, err)
	}
	return e.Motivo
}

// semVazamento confere que o texto não traz a chave, o ditado nem o corpo de
// erro do provedor.
func semVazamento(t *testing.T, onde, texto string) {
	t.Helper()
	for _, proibido := range []string{chave, "Zephyrino", "DCTFWeb", "Incorrect API key", "invalid_request_error", "request_id"} {
		if strings.Contains(texto, proibido) {
			t.Errorf("%s vaza %q: %q", onde, proibido, texto)
		}
	}
}

// ---------------------------------------------------------------------
// O corpo que sai
// ---------------------------------------------------------------------

func TestPolir_OpenAI_CorpoEnviado(t *testing.T) {
	f := novoFalso(t, fixo(200, respostaOpenAI(saida("Bom dia."))))
	req := pedidoReal(ditado)
	if _, err := f.Polir(context.Background(), chamadaOpenAI, chave, req); err != nil {
		t.Fatalf("Polir: %v", err)
	}
	p := f.unico(t)

	esperado := map[string]any{
		"model":        "modelo-openai",
		"instructions": resolver.PolishPrompt,
		"input":        []any{map[string]any{"role": "user", "content": req.Mensagem}},
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "polish_saida", "strict": true, "schema": schemaEsperado(),
		}},
		"reasoning":         map[string]any{"effort": "none"},
		"store":             false,
		"max_output_tokens": float64(2048),
	}
	if got := decodificar(t, p.corpo); !reflect.DeepEqual(got, esperado) {
		t.Fatalf("corpo enviado à OpenAI diferente do esperado.\nrecebido: %s\nesperado: %s", json2(got), json2(esperado))
	}
}

// Campos críticos, um a um, com a razão de cada um. O teste do corpo inteiro
// acima já os cobre; este diz qual quebrou.
func TestPolir_OpenAI_CamposCriticos(t *testing.T) {
	f := novoFalso(t, fixo(200, respostaOpenAI(saida("Bom dia."))))
	req := pedidoReal(ditado)
	if _, err := f.Polir(context.Background(), chamadaOpenAI, chave, req); err != nil {
		t.Fatal(err)
	}
	corpo := decodificar(t, f.unico(t).corpo)

	if v, ok := corpo["store"]; !ok || v != false {
		t.Errorf("store = %v (presente=%v), quero false explícito: sem isso a resposta fica 30 dias na conta", v, ok)
	}
	raciocinio, _ := corpo["reasoning"].(map[string]any)
	if raciocinio["effort"] != "none" {
		t.Errorf("reasoning = %v, quero effort none: o padrão da API (medium) custa latência e dinheiro", corpo["reasoning"])
	}
	if corpo["max_output_tokens"] != float64(2048) {
		t.Errorf("max_output_tokens = %v", corpo["max_output_tokens"])
	}
	formato := corpo["text"].(map[string]any)["format"].(map[string]any)
	if formato["strict"] != true || formato["type"] != "json_schema" || formato["name"] != resolver.PolishSchemaNome {
		t.Errorf("text.format = %v", formato)
	}
	entrada := corpo["input"].([]any)
	if len(entrada) != 1 {
		t.Fatalf("input tem %d mensagens, quero 1", len(entrada))
	}
	msg := entrada[0].(map[string]any)
	if msg["role"] != "user" || msg["content"] != req.Mensagem {
		t.Errorf("a mensagem user tem de ser EXATAMENTE PolishRequest.Mensagem; veio %v", msg)
	}
	if corpo["instructions"] != resolver.PolishPrompt {
		t.Error("instructions tem de ser o prompt do PolishRequest")
	}
}

func TestPolir_OpenAI_EsforcoDeRaciocinio(t *testing.T) {
	for _, c := range []struct {
		nome, esforco string
		quero         any // nil = campo ausente
	}{
		{"padrão é none", "", "none"},
		{"minimal passa como veio", "minimal", "minimal"},
		{"low passa como veio", "low", "low"},
		{"omitir tira o campo", EsforcoOmitido, nil},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso(t, fixo(200, respostaOpenAI(saida("Bom dia."))))
			ch := chamadaOpenAI
			ch.Esforco = c.esforco
			if _, err := f.Polir(context.Background(), ch, chave, pedidoReal(ditado)); err != nil {
				t.Fatal(err)
			}
			corpo := decodificar(t, f.unico(t).corpo)
			r, existe := corpo["reasoning"]
			if c.quero == nil {
				if existe {
					t.Fatalf("reasoning = %v, quero o campo ausente", r)
				}
				return
			}
			if got := r.(map[string]any)["effort"]; got != c.quero {
				t.Fatalf("effort = %v, quero %v", got, c.quero)
			}
		})
	}
}

func TestPolir_Anthropic_CorpoEnviado(t *testing.T) {
	f := novoFalso(t, fixo(200, respostaAnthropic(saida("Bom dia."), "end_turn")))
	req := pedidoReal(ditado)
	ch := chamadaAnthropic
	ch.Esforco = "low" // só a OpenAI tem esforço: aqui não pode aparecer nada
	if _, err := f.Polir(context.Background(), ch, chave, req); err != nil {
		t.Fatalf("Polir: %v", err)
	}
	p := f.unico(t)

	esperado := map[string]any{
		"model":      "modelo-anthropic",
		"max_tokens": float64(2048),
		"system":     resolver.PolishPrompt,
		"messages":   []any{map[string]any{"role": "user", "content": req.Mensagem}},
		"output_config": map[string]any{"format": map[string]any{
			"type": "json_schema", "schema": schemaEsperado(),
		}},
	}
	got := decodificar(t, p.corpo)
	if !reflect.DeepEqual(got, esperado) {
		t.Fatalf("corpo enviado à Anthropic diferente do esperado.\nrecebido: %s\nesperado: %s", json2(got), json2(esperado))
	}
	if _, ok := got["thinking"]; ok {
		t.Error("a Anthropic recebeu thinking: sem pensamento, só custa latência")
	}
}

// O ditado só pode estar no corpo UMA vez (dentro da mensagem user), e a
// mensagem é a do PolishRequest: nada de campo de contexto, de janela, de
// app ou de dicionário. O teste do corpo inteiro enumera os campos; este
// confere o que os campos carregam.
func TestPolir_CorpoSoLevaOTextoDoDitado(t *testing.T) {
	req := pedidoReal(ditado)
	req.Language = "pt" // idioma do ASR: nunca vai ao provedor
	for _, p := range []string{OpenAI, Anthropic} {
		f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
			if p == OpenAI {
				io.WriteString(w, respostaOpenAI(saida("Bom dia.")))
			} else {
				io.WriteString(w, respostaAnthropic(saida("Bom dia."), "end_turn"))
			}
		})
		if _, err := f.Polir(context.Background(), chamadaDe(p), chave, req); err != nil {
			t.Fatal(err)
		}
		corpo := f.unico(t).corpo
		if n := bytes.Count(corpo, []byte("Zephyrino")); n != 1 {
			t.Errorf("%s: o ditado aparece %d vezes no corpo, quero 1 (só na mensagem user)", p, n)
		}
		if bytes.Contains(corpo, []byte(`"pt"`)) {
			t.Errorf("%s: o idioma do ASR foi enviado ao provedor", p)
		}
		if bytes.Contains(corpo, []byte(chave)) {
			t.Errorf("%s: a chave está no corpo", p)
		}
		// A mensagem user decodifica para o JSON {"ditado": ...} com o texto.
		var m struct {
			Ditado string `json:"ditado"`
		}
		msg := decodificar(t, corpo)
		var conteudo string
		if p == OpenAI {
			conteudo = msg["input"].([]any)[0].(map[string]any)["content"].(string)
		} else {
			conteudo = msg["messages"].([]any)[0].(map[string]any)["content"].(string)
		}
		if err := json.Unmarshal([]byte(conteudo), &m); err != nil || m.Ditado != ditado {
			t.Errorf("%s: a mensagem user não é {\"ditado\": texto}: %q (%v)", p, conteudo, err)
		}
	}
}

// ---------------------------------------------------------------------
// Cabeçalhos, endereço e onde a chave NÃO pode estar
// ---------------------------------------------------------------------

func TestPolir_CabecalhosEEndereco(t *testing.T) {
	for _, c := range []struct {
		provedor, caminho, cabecalho, valor string
		ausentes                            []string
		resposta                            string
	}{
		{OpenAI, "/v1/responses", "Authorization", "Bearer " + chave, []string{"x-api-key", "anthropic-version"}, respostaOpenAI(saida("ok"))},
		{Anthropic, "/v1/messages", "x-api-key", chave, []string{"Authorization"}, respostaAnthropic(saida("ok"), "end_turn")},
	} {
		t.Run(c.provedor, func(t *testing.T) {
			f := novoFalso(t, fixo(200, c.resposta))
			if _, err := f.Polir(context.Background(), chamadaDe(c.provedor), chave, pedidoReal(ditado)); err != nil {
				t.Fatal(err)
			}
			p := f.unico(t)
			if p.metodo != http.MethodPost || p.caminho != c.caminho {
				t.Errorf("pedido = %s %s, quero POST %s", p.metodo, p.caminho, c.caminho)
			}
			if got := p.cab.Get(c.cabecalho); got != c.valor {
				t.Errorf("%s = %q (%d caracteres)", c.cabecalho, "***", len(got))
			}
			for _, a := range c.ausentes {
				if p.cab.Get(a) != "" {
					t.Errorf("o cabeçalho %s não devia ir a este provedor", a)
				}
			}
			if got := p.cab.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := p.cab.Get("Accept"); got != "application/json" {
				t.Errorf("Accept = %q", got)
			}
			if c.provedor == Anthropic && p.cab.Get("anthropic-version") != "2023-06-01" {
				t.Errorf("anthropic-version = %q", p.cab.Get("anthropic-version"))
			}

			// A chave só pode estar no cabeçalho de autenticação.
			if strings.Contains(p.url, chave) || strings.Contains(p.url, "?") {
				t.Errorf("a URL leva query ou a chave: %s", p.url)
			}
			if bytes.Contains(p.corpo, []byte(chave)) {
				t.Error("a chave está no corpo")
			}
			for nome, valores := range p.cab {
				if nome == http.CanonicalHeaderKey(c.cabecalho) {
					continue
				}
				if strings.Contains(strings.Join(valores, " "), chave) {
					t.Errorf("a chave está no cabeçalho %s", nome)
				}
			}
		})
	}
}

// TestRequisicaoPolish_ValorZeroFalaComAsAPIsOficiaisPorHTTPS trava o destino
// da chave no POST, como o teste de ListarModelos: todo outro teste aponta o
// Cliente para um httptest.Server, e sem este uma troca de
// BaseOpenAI/BaseAnthropic para http:// ou para outro host passaria a suíte
// inteira. Só MONTA a requisição: nada é enviado, nem por engano (a trava de
// rede do TestMain é a segunda camada).
func TestRequisicaoPolish_ValorZeroFalaComAsAPIsOficiaisPorHTTPS(t *testing.T) {
	for _, c := range []struct{ provedor, url, cabecalho, valor string }{
		{OpenAI, "https://api.openai.com/v1/responses", "Authorization", "Bearer " + chave},
		{Anthropic, "https://api.anthropic.com/v1/messages", "x-api-key", chave},
	} {
		req, err := (&Cliente{}).requisicaoPolish(context.Background(), c.provedor, chave, []byte("{}"))
		if err != nil {
			t.Fatalf("%s: %v", c.provedor, err)
		}
		if got := req.URL.String(); got != c.url {
			t.Errorf("%s: URL = %q, quero %q", c.provedor, got, c.url)
		}
		if req.Method != http.MethodPost {
			t.Errorf("%s: método = %s, quero POST", c.provedor, req.Method)
		}
		if got := req.Header.Get(c.cabecalho); got != c.valor {
			t.Errorf("%s: %s = %q (%d caracteres), quero a chave ali", c.provedor, c.cabecalho, "***", len(got))
		}
	}
	// As bases injetadas valem, cada uma no seu provedor.
	c := &Cliente{BaseOpenAI: "http://127.0.0.1:1", BaseAnthropic: "http://127.0.0.1:2"}
	for provedor, quero := range map[string]string{
		OpenAI:    "http://127.0.0.1:1/v1/responses",
		Anthropic: "http://127.0.0.1:2/v1/messages",
	} {
		req, err := c.requisicaoPolish(context.Background(), provedor, chave, []byte("{}"))
		if err != nil || req.URL.String() != quero {
			t.Errorf("%s: URL = %v, %v; quero %s", provedor, req, err, quero)
		}
	}
}

// O HTTP injetado é quem fala com o provedor. A base aponta para um servidor
// que o teste NÃO deixa ser alcançado: se Polir ignorasse o cliente injetado,
// o pedido iria para ele (loopback, sem sair da máquina) e o teste falharia.
func TestPolir_UsaOClienteHTTPInjetado(t *testing.T) {
	var alcancado atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alcancado.Store(true)
	}))
	defer srv.Close()

	for _, c := range []struct{ provedor, caminho, resposta string }{
		{OpenAI, "/v1/responses", respostaOpenAI(saida("ok"))},
		{Anthropic, "/v1/messages", respostaAnthropic(saida("ok"), "end_turn")},
	} {
		var vista string
		cli := &Cliente{
			BaseOpenAI: srv.URL, BaseAnthropic: srv.URL,
			HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				vista = r.URL.String()
				return &http.Response{
					StatusCode: 200, Header: http.Header{},
					Body: io.NopCloser(strings.NewReader(c.resposta)),
				}, nil
			})},
		}
		resp, err := cli.Polir(context.Background(), chamadaDe(c.provedor), chave, pedidoReal(ditado))
		if err != nil || string(resp.Texto) != saida("ok") {
			t.Fatalf("%s: %q, %v", c.provedor, resp.Texto, err)
		}
		if vista != srv.URL+c.caminho {
			t.Errorf("%s: o cliente injetado viu %q, quero %q", c.provedor, vista, srv.URL+c.caminho)
		}
	}
	if alcancado.Load() {
		t.Fatal("o cliente HTTP injetado foi ignorado: o pedido foi para outro transporte")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// ---------------------------------------------------------------------
// Respostas boas e as que não valem
// ---------------------------------------------------------------------

func TestPolir_OpenAI_TextoExtraido(t *testing.T) {
	final := saida("Bom dia, tudo bem?")
	msg := func(conteudo ...any) map[string]any {
		return map[string]any{"type": "message", "role": "assistant", "content": conteudo}
	}
	parte := func(tipo, campo, valor string) map[string]any { return map[string]any{"type": tipo, campo: valor} }
	corpoDe := func(status string, output ...any) string {
		b, _ := json.Marshal(map[string]any{"status": status, "output": output})
		return string(b)
	}

	for _, c := range []struct {
		nome   string
		corpo  string
		texto  string                  // quando vale
		motivo resolver.MotivoFallback // quando não vale
	}{
		{"mensagem simples", corpoDe("completed", msg(parte("output_text", "text", final))), final, ""},
		{"raciocínio antes da mensagem", corpoDe("completed", map[string]any{"type": "reasoning"}, msg(parte("output_text", "text", final))), final, ""},
		{"mensagem depois de outros itens", corpoDe("completed", map[string]any{"type": "function_call", "content": []any{parte("output_text", "text", "NÃO")}}, msg(parte("output_text", "text", final))), final, ""},
		{"partes de texto são juntas na ordem", corpoDe("completed", msg(parte("output_text", "text", `{"final_text":`), parte("output_text", "text", `"Oi."}`))), `{"final_text":"Oi."}`, ""},
		{"parte que não é texto é ignorada", corpoDe("completed", msg(parte("annotation", "text", "NÃO"), parte("output_text", "text", final))), final, ""},
		{"status ausente vale", corpoDe("", msg(parte("output_text", "text", final))), final, ""},

		{"incomplete por max_output_tokens", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"final_"}]}]}`, "", resolver.MotivoTruncada},
		{"incomplete sem detalhe", corpoDe("incomplete", msg(parte("output_text", "text", final))), "", resolver.MotivoTruncada},
		{"incomplete por filtro de conteúdo é recusa", `{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`, "", resolver.MotivoRecusa},
		{"recusa do modelo", corpoDe("completed", msg(parte("refusal", "refusal", "Não posso ajudar com isso."))), "", resolver.MotivoRecusa},
		{"recusa junto com texto", corpoDe("completed", msg(parte("output_text", "text", final), parte("refusal", "refusal", "não"))), "", resolver.MotivoRecusa},
		{"status failed", corpoDe("failed", msg(parte("output_text", "text", final))), "", resolver.MotivoRespostaInvalida},
		{"status cancelled", corpoDe("cancelled"), "", resolver.MotivoRespostaInvalida},
		{"erro no envelope", `{"status":"completed","error":{"code":"server_error","message":"x"},"output":[{"type":"message","content":[{"type":"output_text","text":"{}"}]}]}`, "", resolver.MotivoRespostaInvalida},
		{"erro null no envelope não conta", `{"status":"completed","error":null,"output":[{"type":"message","content":[{"type":"output_text","text":"{}"}]}]}`, `{}`, ""},
		{"sem output", `{"status":"completed"}`, "", resolver.MotivoRespostaInvalida},
		{"output vazio", corpoDe("completed"), "", resolver.MotivoRespostaInvalida},
		{"só raciocínio", corpoDe("completed", map[string]any{"type": "reasoning"}), "", resolver.MotivoRespostaInvalida},
		{"texto vazio", corpoDe("completed", msg(parte("output_text", "text", ""))), "", resolver.MotivoRespostaInvalida},
		{"texto só de espaço", corpoDe("completed", msg(parte("output_text", "text", " \n\t "))), "", resolver.MotivoRespostaInvalida},
		{"texto fora de item message", corpoDe("completed", map[string]any{"type": "reasoning", "content": []any{parte("output_text", "text", final)}}), "", resolver.MotivoRespostaInvalida},
		{"não é JSON", `isto não é json`, "", resolver.MotivoRespostaInvalida},
		{"corpo vazio", ``, "", resolver.MotivoRespostaInvalida},
		{"é uma lista", `[]`, "", resolver.MotivoRespostaInvalida},
		// Um envelope que não decodifica é inválido por inteiro, mesmo com um
		// texto bom ao lado do campo de tipo errado.
		{"status de outro tipo", `{"status":5,"output":[{"type":"message","content":[{"type":"output_text","text":"{}"}]}]}`, "", resolver.MotivoRespostaInvalida},
		{"output do tipo errado", `{"status":"completed","output":"texto"}`, "", resolver.MotivoRespostaInvalida},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso(t, fixo(200, c.corpo))
			resp, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
			if c.motivo == "" {
				if err != nil {
					t.Fatalf("erro inesperado: %v", err)
				}
				if string(resp.Texto) != c.texto {
					t.Fatalf("Texto = %q, quero %q", resp.Texto, c.texto)
				}
				return
			}
			if got := motivoDe(t, err); got != c.motivo {
				t.Fatalf("motivo = %q, quero %q", got, c.motivo)
			}
			if resp.Texto != nil {
				t.Fatalf("Texto = %q, quero nenhum quando há erro", resp.Texto)
			}
		})
	}
}

func TestPolir_Anthropic_TextoExtraido(t *testing.T) {
	final := saida("Bom dia, tudo bem?")
	corpoDe := func(parada string, blocos ...any) string {
		b, _ := json.Marshal(map[string]any{"type": "message", "stop_reason": parada, "content": blocos})
		return string(b)
	}
	texto := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

	for _, c := range []struct {
		nome   string
		corpo  string
		texto  string
		motivo resolver.MotivoFallback
	}{
		{"bloco de texto", corpoDe("end_turn", texto(final)), final, ""},
		{"blocos que não são texto são ignorados", corpoDe("end_turn", map[string]any{"type": "thinking", "text": "NÃO"}, texto(final)), final, ""},
		{"blocos de texto são juntos na ordem", corpoDe("end_turn", texto(`{"final_text":`), texto(`"Oi."}`)), `{"final_text":"Oi."}`, ""},

		{"max_tokens é truncada", corpoDe("max_tokens", texto(`{"final_`)), "", resolver.MotivoTruncada},
		{"refusal é recusa", corpoDe("refusal", texto("Não posso.")), "", resolver.MotivoRecusa},
		{"tool_use", corpoDe("tool_use", texto(final)), "", resolver.MotivoRespostaInvalida},
		{"pause_turn", corpoDe("pause_turn", texto(final)), "", resolver.MotivoRespostaInvalida},
		{"stop_sequence", corpoDe("stop_sequence", texto(final)), "", resolver.MotivoRespostaInvalida},
		{"sem stop_reason", corpoDe("", texto(final)), "", resolver.MotivoRespostaInvalida},
		{"sem conteúdo", corpoDe("end_turn"), "", resolver.MotivoRespostaInvalida},
		{"texto vazio", corpoDe("end_turn", texto("")), "", resolver.MotivoRespostaInvalida},
		{"texto só de espaço", corpoDe("end_turn", texto("  \n")), "", resolver.MotivoRespostaInvalida},
		{"só pensamento", corpoDe("end_turn", map[string]any{"type": "thinking", "text": final}), "", resolver.MotivoRespostaInvalida},
		{"não é JSON", `<html>`, "", resolver.MotivoRespostaInvalida},
		{"corpo vazio", ``, "", resolver.MotivoRespostaInvalida},
		{"usage de outro tipo", `{"stop_reason":"end_turn","content":[{"type":"text","text":"{}"}],"usage":"oops"}`, "", resolver.MotivoRespostaInvalida},
		{"content do tipo errado", `{"stop_reason":"end_turn","content":"texto"}`, "", resolver.MotivoRespostaInvalida},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso(t, fixo(200, c.corpo))
			resp, err := f.Polir(context.Background(), chamadaAnthropic, chave, pedidoReal(ditado))
			if c.motivo == "" {
				if err != nil {
					t.Fatalf("erro inesperado: %v", err)
				}
				if string(resp.Texto) != c.texto {
					t.Fatalf("Texto = %q, quero %q", resp.Texto, c.texto)
				}
				return
			}
			if got := motivoDe(t, err); got != c.motivo {
				t.Fatalf("motivo = %q, quero %q", got, c.motivo)
			}
			if resp.Texto != nil {
				t.Fatalf("Texto = %q, quero nenhum quando há erro", resp.Texto)
			}
		})
	}
}

func TestPolir_TokensDaResposta(t *testing.T) {
	f := novoFalso(t, fixo(200, respostaOpenAI(saida("ok"))))
	resp, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
	if err != nil {
		t.Fatal(err)
	}
	if quero := (Tokens{Entrada: 1234, Saida: 56, Raciocinio: 7}); resp.Tokens != quero {
		t.Fatalf("Tokens = %+v, quero %+v", resp.Tokens, quero)
	}

	f = novoFalso(t, fixo(200, respostaAnthropic(saida("ok"), "end_turn")))
	resp, err = f.Polir(context.Background(), chamadaAnthropic, chave, pedidoReal(ditado))
	if err != nil {
		t.Fatal(err)
	}
	if quero := (Tokens{Entrada: 321, Saida: 45}); resp.Tokens != quero {
		t.Fatalf("Tokens = %+v, quero %+v", resp.Tokens, quero)
	}
}

// Uma resposta recusada ou truncada também foi cobrada: os tokens vêm mesmo
// com erro, para o custo do benchmark não subestimar.
func TestPolir_TokensVemMesmoComErro(t *testing.T) {
	truncada := `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":900,"output_tokens":2048,"output_tokens_details":{"reasoning_tokens":2000}}}`
	f := novoFalso(t, fixo(200, truncada))
	resp, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
	if motivoDe(t, err) != resolver.MotivoTruncada {
		t.Fatalf("err = %v", err)
	}
	if quero := (Tokens{Entrada: 900, Saida: 2048, Raciocinio: 2000}); resp.Tokens != quero {
		t.Fatalf("Tokens = %+v, quero %+v", resp.Tokens, quero)
	}

	cortada := `{"stop_reason":"max_tokens","content":[{"type":"text","text":"{"}],"usage":{"input_tokens":700,"output_tokens":2048}}`
	f = novoFalso(t, fixo(200, cortada))
	resp, err = f.Polir(context.Background(), chamadaAnthropic, chave, pedidoReal(ditado))
	if motivoDe(t, err) != resolver.MotivoTruncada {
		t.Fatalf("err = %v", err)
	}
	if quero := (Tokens{Entrada: 700, Saida: 2048}); resp.Tokens != quero {
		t.Fatalf("Tokens = %+v, quero %+v", resp.Tokens, quero)
	}

	recusada := `{"stop_reason":"refusal","content":[],"usage":{"input_tokens":800,"output_tokens":12}}`
	f = novoFalso(t, fixo(200, recusada))
	resp, err = f.Polir(context.Background(), chamadaAnthropic, chave, pedidoReal(ditado))
	if motivoDe(t, err) != resolver.MotivoRecusa {
		t.Fatalf("err = %v", err)
	}
	if quero := (Tokens{Entrada: 800, Saida: 12}); resp.Tokens != quero {
		t.Fatalf("Tokens = %+v, quero %+v", resp.Tokens, quero)
	}
}

// ---------------------------------------------------------------------
// Erros HTTP traduzidos
// ---------------------------------------------------------------------

func TestPolir_ErrosHTTPTraduzidos(t *testing.T) {
	const credito = "Your credit balance is too low to access the Anthropic API."
	for _, c := range []struct {
		nome     string
		provedor string
		status   int
		corpo    string
		quero    resolver.MotivoFallback
	}{
		// OpenAI: chave.
		{"openai 401 chave inválida", OpenAI, 401, erroOpenAI("invalid_request_error", "invalid_api_key"), resolver.MotivoChaveRecusada},
		{"openai 401 sem corpo", OpenAI, 401, ``, resolver.MotivoChaveRecusada},
		{"openai 403 região", OpenAI, 403, erroOpenAI("", "unsupported_country_region_territory"), resolver.MotivoChaveRecusada},
		// OpenAI: modelo.
		{"openai 404", OpenAI, 404, erroOpenAI("invalid_request_error", "model_not_found"), resolver.MotivoModeloInexistente},
		{"openai 404 sem corpo", OpenAI, 404, ``, resolver.MotivoModeloInexistente},
		{"openai 400 model_not_found", OpenAI, 400, erroOpenAI("invalid_request_error", "model_not_found"), resolver.MotivoModeloInexistente},
		{"openai 403 model_not_found (conta sem acesso)", OpenAI, 403, erroOpenAI("invalid_request_error", "model_not_found"), resolver.MotivoModeloInexistente},
		{"openai model_not_found em maiúsculas", OpenAI, 400, erroOpenAI("invalid_request_error", "MODEL_NOT_FOUND"), resolver.MotivoModeloInexistente},
		// OpenAI: crédito e limite.
		{"openai 429 insufficient_quota (código)", OpenAI, 429, erroOpenAI("insufficient_quota", "insufficient_quota"), resolver.MotivoSemCredito},
		{"openai 429 insufficient_quota (só no tipo)", OpenAI, 429, erroOpenAI("insufficient_quota", ""), resolver.MotivoSemCredito},
		{"openai 429 credit_balance_exhausted", OpenAI, 429, erroOpenAI("", "credit_balance_exhausted"), resolver.MotivoSemCredito},
		{"openai 429 organization_spend_limit_exceeded", OpenAI, 429, erroOpenAI("", "organization_spend_limit_exceeded"), resolver.MotivoSemCredito},
		{"openai 429 project_spend_limit_exceeded", OpenAI, 429, erroOpenAI("", "project_spend_limit_exceeded"), resolver.MotivoSemCredito},
		{"openai 429 organization_usage_limit_exceeded", OpenAI, 429, erroOpenAI("", "organization_usage_limit_exceeded"), resolver.MotivoSemCredito},
		{"openai 400 billing_hard_limit_reached", OpenAI, 400, erroOpenAI("", "billing_hard_limit_reached"), resolver.MotivoSemCredito},
		{"openai 402", OpenAI, 402, ``, resolver.MotivoSemCredito},
		{"openai crédito com tipo em maiúsculas", OpenAI, 429, erroOpenAI("INSUFFICIENT_QUOTA", ""), resolver.MotivoSemCredito},
		{"openai crédito com código em maiúsculas", OpenAI, 429, erroOpenAI("", "CREDIT_BALANCE_EXHAUSTED"), resolver.MotivoSemCredito},
		{"anthropic teto de gasto com detalhe em maiúsculas", Anthropic, 429, erroAnthropic("rate_limit_error", "x", map[string]any{"error_code": "ENFORCED_SPEND_LIMIT_REACHED"}), resolver.MotivoSemCredito},
		{"openai 429 rate_limit_exceeded", OpenAI, 429, erroOpenAI("rate_limit_error", "rate_limit_exceeded"), resolver.MotivoLimite},
		{"openai 429 slow_down", OpenAI, 429, erroOpenAI("rate_limit_error", "slow_down"), resolver.MotivoLimite},
		{"openai 429 sem corpo", OpenAI, 429, ``, resolver.MotivoLimite},
		{"openai 429 corpo que não é JSON", OpenAI, 429, `<html>too many</html>`, resolver.MotivoLimite},
		{"openai 429 código que não é texto", OpenAI, 429, `{"error":{"code":429,"type":7}}`, resolver.MotivoLimite},
		// OpenAI: servidor.
		{"openai 500", OpenAI, 500, erroOpenAI("server_error", ""), resolver.MotivoSobrecarga},
		{"openai 503", OpenAI, 503, erroOpenAI("service_unavailable_error", "server_is_overloaded"), resolver.MotivoSobrecarga},
		{"openai 502", OpenAI, 502, `<html>bad gateway</html>`, resolver.MotivoSobrecarga},
		{"openai 504", OpenAI, 504, ``, resolver.MotivoSobrecarga},
		// OpenAI: pedido.
		{"openai 400 parâmetro não suportado", OpenAI, 400, erroOpenAI("invalid_request_error", "unsupported_parameter"), resolver.MotivoPedidoRecusado},
		{"openai 400 sem corpo", OpenAI, 400, ``, resolver.MotivoPedidoRecusado},
		// OpenAI: o modelo existe, mas não aceita o esforço de raciocínio do pedido.
		{"openai 400 esforço não suportado (unsupported_value)", OpenAI, 400, erroOpenAIParam("invalid_request_error", "unsupported_value", "reasoning.effort"), resolver.MotivoModeloIncompativel},
		{"openai 400 raciocínio não suportado (unsupported_parameter)", OpenAI, 400, erroOpenAIParam("invalid_request_error", "unsupported_parameter", "reasoning.effort"), resolver.MotivoModeloIncompativel},
		{"openai 400 param reasoning sem ponto", OpenAI, 400, erroOpenAIParam("invalid_request_error", "", "reasoning"), resolver.MotivoModeloIncompativel},
		{"openai 400 param reasoning em maiúsculas", OpenAI, 400, erroOpenAIParam("invalid_request_error", "", "REASONING.EFFORT"), resolver.MotivoModeloIncompativel},
		{"openai 400 outro param segue pedido recusado", OpenAI, 400, erroOpenAIParam("invalid_request_error", "unsupported_parameter", "temperature"), resolver.MotivoPedidoRecusado},
		{"openai 400 reasoning só no meio do param segue pedido recusado", OpenAI, 400, erroOpenAIParam("invalid_request_error", "", "text.reasoning"), resolver.MotivoPedidoRecusado},
		{"openai 400 param que não é texto", OpenAI, 400, `{"error":{"param":7,"code":"unsupported_value"}}`, resolver.MotivoPedidoRecusado},
		{"openai 422 com param reasoning só vale no 400", OpenAI, 422, erroOpenAIParam("invalid_request_error", "", "reasoning.effort"), resolver.MotivoPedidoRecusado},
		{"openai 403 com param reasoning segue chave recusada", OpenAI, 403, erroOpenAIParam("invalid_request_error", "", "reasoning.effort"), resolver.MotivoChaveRecusada},
		{"openai 500 com param reasoning segue sobrecarga", OpenAI, 500, erroOpenAIParam("server_error", "", "reasoning.effort"), resolver.MotivoSobrecarga},
		{"openai 400 modelo inexistente vale mais que o param", OpenAI, 400, erroOpenAIParam("invalid_request_error", "model_not_found", "reasoning.effort"), resolver.MotivoModeloInexistente},
		{"openai 400 sem crédito vale mais que o param", OpenAI, 400, erroOpenAIParam("insufficient_quota", "", "reasoning.effort"), resolver.MotivoSemCredito},
		{"openai 413", OpenAI, 413, ``, resolver.MotivoPedidoRecusado},
		{"openai 422", OpenAI, 422, erroOpenAI("invalid_request_error", ""), resolver.MotivoPedidoRecusado},
		{"openai 409", OpenAI, 409, ``, resolver.MotivoPedidoRecusado},

		// Anthropic: chave.
		{"anthropic 401", Anthropic, 401, erroAnthropic("authentication_error", "invalid x-api-key", nil), resolver.MotivoChaveRecusada},
		{"anthropic 403", Anthropic, 403, erroAnthropic("permission_error", "sem permissão", nil), resolver.MotivoChaveRecusada},
		// Anthropic: modelo.
		{"anthropic 404", Anthropic, 404, erroAnthropic("not_found_error", "model: modelo-anthropic", nil), resolver.MotivoModeloInexistente},
		// Anthropic: crédito e limite.
		{"anthropic 402", Anthropic, 402, erroAnthropic("billing_error", "pagamento", nil), resolver.MotivoSemCredito},
		{"anthropic 400 sem crédito (frase)", Anthropic, 400, erroAnthropic("invalid_request_error", credito, nil), resolver.MotivoSemCredito},
		{"anthropic 400 sem crédito (maiúsculas)", Anthropic, 400, erroAnthropic("invalid_request_error", "YOUR CREDIT BALANCE IS TOO LOW", nil), resolver.MotivoSemCredito},
		{"anthropic 400 teto de uso (frase)", Anthropic, 400, erroAnthropic("invalid_request_error", "You have reached your specified workspace API usage limits.", nil), resolver.MotivoSemCredito},
		{"anthropic 400 teto de gasto (frase)", Anthropic, 400, erroAnthropic("invalid_request_error", "monthly spend limit reached", nil), resolver.MotivoSemCredito},
		{"anthropic 429 teto de gasto mensal", Anthropic, 429, erroAnthropic("rate_limit_error", "limite", map[string]any{"error_code": "enforced_spend_limit_reached"}), resolver.MotivoSemCredito},
		{"anthropic 429 rate limit", Anthropic, 429, erroAnthropic("rate_limit_error", "rate limit", nil), resolver.MotivoLimite},
		{"anthropic 429 frase de crédito só vale no 400", Anthropic, 429, erroAnthropic("rate_limit_error", credito, nil), resolver.MotivoLimite},
		// Anthropic: servidor.
		{"anthropic 500", Anthropic, 500, erroAnthropic("api_error", "erro", nil), resolver.MotivoSobrecarga},
		{"anthropic 504", Anthropic, 504, erroAnthropic("timeout_error", "erro", nil), resolver.MotivoSobrecarga},
		{"anthropic 529", Anthropic, 529, erroAnthropic("overloaded_error", "sobrecarga", nil), resolver.MotivoSobrecarga},
		// Anthropic: pedido.
		{"anthropic 400 schema", Anthropic, 400, erroAnthropic("invalid_request_error", "output_config.format: schema não suportado", nil), resolver.MotivoPedidoRecusado},
		{"anthropic 413", Anthropic, 413, erroAnthropic("request_too_large", "grande", nil), resolver.MotivoPedidoRecusado},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso(t, fixo(c.status, c.corpo))
			_, err := f.Polir(context.Background(), chamadaDe(c.provedor), chave, pedidoReal(ditado))
			if got := motivoDe(t, err); got != c.quero {
				t.Fatalf("HTTP %d: motivo = %q, quero %q", c.status, got, c.quero)
			}
			f.unico(t) // sem retry, nem em 429 nem em 5xx
			semVazamento(t, "err.Error()", err.Error())
			if err.Error() != "polish: "+string(c.quero) {
				t.Fatalf("o erro devia ser só o motivo fixo, veio %q", err.Error())
			}
		})
	}
}

// Corpo de erro enorme não estoura a memória e continua classificado pelo
// status.
func TestPolir_ErroComCorpoGigante(t *testing.T) {
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write(bytes.Repeat([]byte("x"), 3<<20))
	})
	_, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
	if got := motivoDe(t, err); got != resolver.MotivoSobrecarga {
		t.Fatalf("motivo = %q", got)
	}
}

// Só o 200 vale: um 3xx ou outro 2xx nunca é aceito, mesmo com um corpo que
// pareceria uma resposta boa.
func TestPolir_SoOStatus200Vale(t *testing.T) {
	for _, p := range []string{OpenAI, Anthropic} {
		bom := respostaOpenAI(saida("ok"))
		if p == Anthropic {
			bom = respostaAnthropic(saida("ok"), "end_turn")
		}
		for _, status := range []int{201, 202, 204, 301, 302, 304, 307} {
			f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
				if status >= 300 {
					w.Header().Set("Location", "/outro")
				}
				w.WriteHeader(status)
				if status != 204 && status != 304 {
					io.WriteString(w, bom)
				}
			})
			resp, err := f.Polir(context.Background(), chamadaDe(p), chave, pedidoReal(ditado))
			if got := motivoDe(t, err); got != resolver.MotivoRespostaInvalida {
				t.Errorf("%s HTTP %d: motivo = %q, quero resposta_invalida", p, status, got)
			}
			if resp.Texto != nil {
				t.Errorf("%s HTTP %d: aceitou o texto de uma resposta que não é 200", p, status)
			}
		}
	}
}

func TestPolir_RedirecionamentoNaoESeguido(t *testing.T) {
	var visitado atomic.Bool
	destino := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visitado.Store(true)
		w.Write([]byte(respostaOpenAI(saida("ok"))))
	}))
	defer destino.Close()

	for _, p := range []string{OpenAI, Anthropic} {
		for _, status := range []int{301, 302, 307, 308} {
			f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destino.URL+"/v1/responses", status)
			})
			// Mesmo com um cliente injetado que seguiria redirecionamento.
			f.HTTP = &http.Client{}
			_, err := f.Polir(context.Background(), chamadaDe(p), chave, pedidoReal(ditado))
			if got := motivoDe(t, err); got != resolver.MotivoRespostaInvalida {
				t.Errorf("%s %d: motivo = %q, quero resposta_invalida", p, status, got)
			}
			f.unico(t)
		}
	}
	if visitado.Load() {
		t.Fatal("o redirecionamento foi seguido: a chave e o ditado foram entregues a outro servidor")
	}
}

// Toda resposta tem o corpo fechado, seja qual for o caminho: sem isto cada
// ditado vazaria a conexão e as goroutines de leitura e escrita do transporte.
func TestPolir_FechaOCorpoDaResposta(t *testing.T) {
	for _, c := range []struct {
		nome, provedor string
		status         int
		corpo          string
	}{
		{"200 bom da OpenAI", OpenAI, 200, respostaOpenAI(saida("ok"))},
		{"200 bom da Anthropic", Anthropic, 200, respostaAnthropic(saida("ok"), "end_turn")},
		{"erro HTTP", OpenAI, 401, erroOpenAI("invalid_request_error", "invalid_api_key")},
		{"redirecionamento", OpenAI, 302, ""},
		{"200 acima do limite", OpenAI, 200, respostaComTamanho(t, limiteRespostaPolish+1)},
		{"200 que não decodifica", Anthropic, 200, "lixo"},
		{"200 com stop_reason que não vale", Anthropic, 200, respostaAnthropic(saida("ok"), "tool_use")},
	} {
		t.Run(c.nome, func(t *testing.T) {
			var fechado atomic.Int32
			cli := &Cliente{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: c.status, Header: http.Header{"Location": {"/outro"}},
					Body: &corpoMarcado{Reader: strings.NewReader(c.corpo), fechou: func() { fechado.Add(1) }},
				}, nil
			})}}
			cli.Polir(context.Background(), chamadaDe(c.provedor), chave, pedidoReal(ditado))
			if fechado.Load() == 0 {
				t.Fatal("o corpo da resposta não foi fechado")
			}
		})
	}
}

// ---------------------------------------------------------------------
// Tamanho da resposta
// ---------------------------------------------------------------------

// respostaComTamanho devolve uma resposta válida da OpenAI com exatamente n
// bytes: o JSON seguido de espaços, que o JSON aceita.
func respostaComTamanho(t *testing.T, n int) string {
	t.Helper()
	base := respostaOpenAI(saida("ok"))
	if len(base) > n {
		t.Fatalf("a resposta base já tem %d bytes", len(base))
	}
	return base + strings.Repeat(" ", n-len(base))
}

func TestPolir_LimiteDeTamanhoDaResposta(t *testing.T) {
	// No limite: vale.
	f := novoFalso(t, fixo(200, respostaComTamanho(t, limiteRespostaPolish)))
	if _, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado)); err != nil {
		t.Fatalf("resposta com exatamente o limite (%d bytes) foi recusada: %v", limiteRespostaPolish, err)
	}
	// Um byte acima: cortada, e o erro é de resposta inválida.
	f = novoFalso(t, fixo(200, respostaComTamanho(t, limiteRespostaPolish+1)))
	_, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
	if got := motivoDe(t, err); got != resolver.MotivoRespostaInvalida {
		t.Fatalf("um byte acima do limite: motivo = %q, quero resposta_invalida", got)
	}
}

// O limite é de 1 MiB (o contrato), e um ditado enorme cabe com folga: 300 KB
// de texto voltam inteiros.
func TestPolir_LimiteEhUmMebibyteEUmDitadoGrandeCabe(t *testing.T) {
	if limiteRespostaPolish != 1<<20 {
		t.Fatalf("limiteRespostaPolish = %d, o contrato diz 1 MiB", limiteRespostaPolish)
	}
	grande := saida(strings.Repeat("palavra ", 40000)) // 320 KB
	f := novoFalso(t, fixo(200, respostaOpenAI(grande)))
	resp, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
	if err != nil || string(resp.Texto) != grande {
		t.Fatalf("um texto de %d KB não voltou inteiro: %v", len(grande)>>10, err)
	}
}

// Um endereço base inválido não vira detalhe no erro.
func TestPolir_EnderecoBaseInvalido(t *testing.T) {
	c := &Cliente{BaseOpenAI: "http://[::1", BaseAnthropic: "http://[::1"}
	for _, p := range []string{OpenAI, Anthropic} {
		_, err := c.Polir(context.Background(), chamadaDe(p), chave, pedidoReal(ditado))
		if err == nil {
			t.Fatalf("%s: esperava erro", p)
		}
		var e *resolver.ErroPolish
		if errors.As(err, &e) {
			t.Errorf("%s: um endereço inválido não é erro de provedor: %v", p, err)
		}
		semVazamento(t, p+": err.Error()", err.Error())
		if strings.Contains(err.Error(), "[::1") {
			t.Errorf("%s: o erro traz o endereço: %q", p, err)
		}
	}
}

// Uma resposta gigante não é lida inteira: o cliente para no limite e fecha a
// conexão, então o servidor vê o corte.
func TestPolir_RespostaGiganteEhCortada(t *testing.T) {
	var escritos int64
	parou := make(chan struct{})
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(parou)
		w.WriteHeader(200)
		bloco := bytes.Repeat([]byte(" "), 64<<10)
		for i := 0; i < 2000; i++ { // 125 MiB se fosse até o fim
			n, err := w.Write(bloco)
			escritos += int64(n)
			if err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})
	_, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
	if got := motivoDe(t, err); got != resolver.MotivoRespostaInvalida {
		t.Fatalf("motivo = %q, quero resposta_invalida", got)
	}
	<-parou
	if escritos > 64<<20 {
		t.Fatalf("o servidor conseguiu escrever %d MiB: o cliente não cortou a leitura", escritos>>20)
	}
}

// ---------------------------------------------------------------------
// Prazo, rede e cancelamento
// ---------------------------------------------------------------------

func TestPolir_PrazoDoContextoDoChamador(t *testing.T) {
	for _, p := range []string{OpenAI, Anthropic} {
		solta := make(chan struct{})
		f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-solta:
			case <-r.Context().Done():
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		inicio := time.Now()
		_, err := f.Polir(ctx, chamadaDe(p), chave, pedidoReal(ditado))
		cancel()
		close(solta)
		if got := motivoDe(t, err); got != resolver.MotivoPrazo {
			t.Errorf("%s: motivo = %q, quero prazo", p, got)
		}
		// O prazo do chamador (80 ms) vence o do cliente (10 s).
		if d := time.Since(inicio); d > 3*time.Second {
			t.Errorf("%s: o prazo do contexto não cortou a chamada: %v", p, d)
		}
		f.unico(t)
	}
}

// Sem prazo no contexto, vale o do cliente: uma chamada nunca fica pendurada.
func TestPolir_SemPrazoNoContextoValeOTimeoutDoCliente(t *testing.T) {
	solta := make(chan struct{})
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-solta:
		case <-r.Context().Done():
		}
	})
	defer close(solta)
	f.Timeout = 60 * time.Millisecond
	inicio := time.Now()
	_, err := f.Polir(context.Background(), chamadaOpenAI, chave, pedidoReal(ditado))
	if got := motivoDe(t, err); got != resolver.MotivoPrazo {
		t.Fatalf("motivo = %q, quero prazo", got)
	}
	if d := time.Since(inicio); d > 3*time.Second {
		t.Fatalf("o Timeout do cliente não cortou a chamada: %v", d)
	}
}

// Servidor que responde os cabeçalhos e trava no corpo: o prazo também vale
// para a leitura.
func TestPolir_PrazoNaLeituraDoCorpo(t *testing.T) {
	solta := make(chan struct{})
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"status":`))
		w.(http.Flusher).Flush()
		select {
		case <-solta:
		case <-r.Context().Done():
		}
	})
	defer close(solta)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := f.Polir(ctx, chamadaOpenAI, chave, pedidoReal(ditado))
	if got := motivoDe(t, err); got != resolver.MotivoPrazo {
		t.Fatalf("motivo = %q, quero prazo", got)
	}
}

// Cancelar (o Agent encerrando) não é prazo.
func TestPolir_CancelamentoNaoEPrazo(t *testing.T) {
	solta := make(chan struct{})
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-solta:
		case <-r.Context().Done():
		}
	})
	defer close(solta)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := f.Polir(ctx, chamadaOpenAI, chave, pedidoReal(ditado))
	if got := motivoDe(t, err); got != resolver.MotivoTransporte {
		t.Fatalf("motivo = %q, quero transporte (cancelamento não é prazo)", got)
	}
}

func TestPolir_SemRede(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	endereco := srv.URL
	srv.Close() // ninguém escuta mais ali
	c := &Cliente{BaseOpenAI: endereco, BaseAnthropic: endereco}
	for _, p := range []string{OpenAI, Anthropic} {
		_, err := c.Polir(context.Background(), chamadaDe(p), chave, pedidoReal(ditado))
		if got := motivoDe(t, err); got != resolver.MotivoTransporte {
			t.Errorf("%s: motivo = %q, quero transporte", p, got)
		}
		semVazamento(t, p+": err.Error()", err.Error())
		if strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("%s: o erro traz o endereço da requisição: %q", p, err)
		}
	}
}

// ---------------------------------------------------------------------
// Pedidos que nem chegam à rede
// ---------------------------------------------------------------------

func TestPolir_PedidoInvalidoNemAbreARede(t *testing.T) {
	semInstrucoes := pedidoReal(ditado)
	semInstrucoes.Instrucoes = ""
	semMensagem := pedidoReal(ditado)
	semMensagem.Mensagem = ""
	semSchema := pedidoReal(ditado)
	semSchema.Schema = nil
	naoSerializa := pedidoReal(ditado)
	naoSerializa.Schema = map[string]any{"x": make(chan int)}

	for _, c := range []struct {
		nome   string
		ch     Chamada
		chave  string
		req    resolver.PolishRequest
		motivo resolver.MotivoFallback // vazio: erro comum
		semKey bool                    // ErrPolishSemChave
	}{
		{"provedor desconhecido", Chamada{Provedor: "outro", Modelo: "m"}, chave, pedidoReal(ditado), "", false},
		{"provedor vazio", Chamada{Modelo: "m"}, chave, pedidoReal(ditado), "", false},
		{"sem instruções", chamadaOpenAI, chave, semInstrucoes, "", false},
		{"sem mensagem", chamadaOpenAI, chave, semMensagem, "", false},
		{"sem schema", chamadaOpenAI, chave, semSchema, "", false},
		{"schema que não vira JSON", chamadaOpenAI, chave, naoSerializa, "", false},
		{"sem modelo", Chamada{Provedor: OpenAI}, chave, pedidoReal(ditado), resolver.MotivoModeloInexistente, false},
		{"sem chave", chamadaOpenAI, "", pedidoReal(ditado), "", true},
		{"chave com espaço", chamadaOpenAI, "sk-abc def", pedidoReal(ditado), resolver.MotivoChaveRecusada, false},
		{"chave com quebra de linha", chamadaAnthropic, "sk-abc\nX-Outro: 1", pedidoReal(ditado), resolver.MotivoChaveRecusada, false},
		{"chave com tabulação", chamadaOpenAI, "sk-abc\tdef", pedidoReal(ditado), resolver.MotivoChaveRecusada, false},
		{"chave com acento", chamadaOpenAI, "sk-chavé", pedidoReal(ditado), resolver.MotivoChaveRecusada, false},
		{"chave com DEL", chamadaOpenAI, "sk-abc\x7f", pedidoReal(ditado), resolver.MotivoChaveRecusada, false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso(t, fixo(200, respostaOpenAI(saida("ok"))))
			_, err := f.Polir(context.Background(), c.ch, c.chave, c.req)
			if err == nil {
				t.Fatal("esperava erro")
			}
			if n := len(f.pedidos()); n != 0 {
				t.Fatalf("o pedido chegou à rede (%d)", n)
			}
			switch {
			case c.semKey:
				if !errors.Is(err, resolver.ErrPolishSemChave) {
					t.Fatalf("err = %v, quero ErrPolishSemChave", err)
				}
			case c.motivo != "":
				if got := motivoDe(t, err); got != c.motivo {
					t.Fatalf("motivo = %q, quero %q", got, c.motivo)
				}
			default:
				var e *resolver.ErroPolish
				if errors.As(err, &e) || errors.Is(err, resolver.ErrPolishSemChave) {
					t.Fatalf("err = %v: quero um erro comum, sem motivo de provedor", err)
				}
			}
			semVazamento(t, "err.Error()", err.Error())
			if c.chave != "" && strings.Contains(err.Error(), c.chave) {
				t.Fatalf("o erro traz a chave: %q", err)
			}
		})
	}
}

// A chave usável é toda de ASCII visível: os extremos da faixa passam.
func TestChaveUsavel(t *testing.T) {
	for chave, quero := range map[string]bool{
		"!":                         true, // 0x21, o primeiro visível
		"~":                         true, // 0x7e, o último
		"sk-proj-AbC_123.xyz":       true,
		" ":                         false, // 0x20
		"a b":                       false,
		"a\x7f":                     false, // DEL
		"a\x80":                     false,
		"\x1f":                      false,
		"a\r":                       false,
		"olá":                       false,
		"sk-ant-api03-Zz9_-Zz9.Zz9": true,
	} {
		if got := chaveUsavel(chave); got != quero {
			t.Errorf("chaveUsavel(%q) = %v, quero %v", chave, got, quero)
		}
	}
}

// ---------------------------------------------------------------------
// Polidor: o resolver.PolishClient
// ---------------------------------------------------------------------

func TestPolidor_ChamaAChaveACadaDitado(t *testing.T) {
	f := novoFalso(t, fixo(200, respostaOpenAI(saida("ok"))))
	chamadas := 0
	chaves := []string{"sk-primeira", "sk-segunda"}
	p := &Polidor{
		Cliente: f.Cliente, Chamada: chamadaOpenAI,
		Chave: func(ctx context.Context) (string, error) {
			chamadas++
			return chaves[chamadas-1], nil
		},
	}
	for range chaves {
		if _, err := p.Polish(context.Background(), pedidoReal(ditado)); err != nil {
			t.Fatal(err)
		}
	}
	if chamadas != 2 {
		t.Fatalf("Chave foi chamada %d vezes, quero 1 por ditado", chamadas)
	}
	var vistas []string
	for _, p := range f.pedidos() {
		vistas = append(vistas, strings.TrimPrefix(p.cab.Get("Authorization"), "Bearer "))
	}
	if !reflect.DeepEqual(vistas, chaves) {
		t.Fatalf("chaves enviadas = %v, quero %v: a segunda chamada tem de usar a chave nova", vistas, chaves)
	}
}

func TestPolidor_DevolveOTextoBrutoDoModelo(t *testing.T) {
	final := saida("Bom dia, tudo bem?")
	for _, prov := range []string{OpenAI, Anthropic} {
		f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
			if prov == OpenAI {
				io.WriteString(w, respostaOpenAI(final))
			} else {
				io.WriteString(w, respostaAnthropic(final, "end_turn"))
			}
		})
		p := &Polidor{
			Cliente: f.Cliente, Chamada: chamadaDe(prov),
			Chave: func(context.Context) (string, error) { return chave, nil },
		}
		got, err := p.Polish(context.Background(), pedidoReal(ditado))
		if err != nil || string(got) != final {
			t.Fatalf("%s: Polish = %q, %v; quero %q", prov, got, err, final)
		}
	}
}

func TestPolidor_ChaveAusenteOuQueFalha(t *testing.T) {
	var chamado atomic.Bool
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) { chamado.Store(true) })

	for _, c := range []struct {
		nome  string
		chave func(context.Context) (string, error)
	}{
		{"função nula", nil},
		{"sem chave salva", func(context.Context) (string, error) { return "", resolver.ErrPolishSemChave }},
		{"sem chave, embrulhado", func(context.Context) (string, error) {
			return "", fmt.Errorf("ler: %w", resolver.ErrPolishSemChave)
		}},
		{"falha do cofre com segredo no texto", func(context.Context) (string, error) {
			return "", errors.New("cofre quebrou: " + chave + " " + ditado)
		}},
		{"chave vazia sem erro", func(context.Context) (string, error) { return "", nil }},
		// Com erro, o resto do que a função devolveu não vale (convenção do Go).
		{"chave e erro juntos", func(context.Context) (string, error) { return chave, errors.New("cofre trancado") }},
	} {
		t.Run(c.nome, func(t *testing.T) {
			p := &Polidor{Cliente: f.Cliente, Chamada: chamadaOpenAI, Chave: c.chave}
			_, err := p.Polish(context.Background(), pedidoReal(ditado))
			if !errors.Is(err, resolver.ErrPolishSemChave) {
				t.Fatalf("err = %v, quero ErrPolishSemChave", err)
			}
			semVazamento(t, "err.Error()", err.Error())
		})
	}
	if chamado.Load() {
		t.Fatal("sem chave a rede não pode ser aberta")
	}
}

// Se a leitura da chave morre porque o ditado estourou o prazo (ou o Agent está
// encerrando), o motivo é prazo (ou transporte), não "sem chave": a tela não
// pode pedir à pessoa que cole a chave de novo à toa. Já um "sem chave"
// explícito continua sendo "sem chave" com qualquer contexto, e volta como o
// próprio erro sentinela, sem o texto que o embrulhava.
func TestPolidor_ChaveQueFalhaComOContextoVencido(t *testing.T) {
	var chamado atomic.Bool
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) { chamado.Store(true) })
	segredo := chave + " " + ditado

	expirado, cancelaExpirado := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelaExpirado()
	cancelado, cancela := context.WithCancel(context.Background())
	cancela()

	for _, c := range []struct {
		nome      string
		ctx       context.Context
		erro      error
		motivo    resolver.MotivoFallback
		ehSemChav bool
	}{
		{"prazo estourado, erro opaco", expirado, errors.New("cofre: " + segredo), resolver.MotivoPrazo, false},
		{"prazo estourado, erro do ctx embrulhado", expirado, fmt.Errorf("ler %s: %w", segredo, context.DeadlineExceeded), resolver.MotivoPrazo, false},
		{"cancelado, erro opaco", cancelado, errors.New("cofre: " + segredo), resolver.MotivoTransporte, false},
		{"prazo estourado, sem chave explícito", expirado, fmt.Errorf("cofre: %s: %w", segredo, resolver.ErrPolishSemChave), resolver.MotivoSemChave, true},
		{"cancelado, sem chave explícito", cancelado, fmt.Errorf("cofre: %s: %w", segredo, resolver.ErrPolishSemChave), resolver.MotivoSemChave, true},
		{"contexto vivo, erro opaco", context.Background(), errors.New("cofre: " + segredo), resolver.MotivoSemChave, true},
	} {
		t.Run(c.nome, func(t *testing.T) {
			p := &Polidor{
				Cliente: f.Cliente, Chamada: chamadaOpenAI,
				Chave: func(context.Context) (string, error) { return "", c.erro },
			}
			_, err := p.Polish(c.ctx, pedidoReal(ditado))
			if err == nil {
				t.Fatal("sem erro")
			}
			semVazamento(t, "err.Error()", err.Error())
			procurar(t, "err.Error()", err.Error())
			if c.ehSemChav != (err == resolver.ErrPolishSemChave) {
				t.Fatalf("err = %v; ErrPolishSemChave (o próprio valor) devia ser %v", err, c.ehSemChav)
			}
			if !c.ehSemChav && !errors.Is(err, c.ctx.Err()) {
				t.Fatalf("err = %v, quero o erro do contexto (%v)", err, c.ctx.Err())
			}

			// E o que o resolver faz com isso.
			res := resolver.ResolveResult{Mode: resolver.ModeSmart, FinalText: "bom dia tudo bem com você"}
			got := resolver.Polir(c.ctx, p, res, resolver.PolirOpcoes{})
			if got.Reescrito || got.MotivoFallback != c.motivo {
				t.Fatalf("Reescrito=%v Motivo=%q, quero %q", got.Reescrito, got.MotivoFallback, c.motivo)
			}
		})
	}
	if chamado.Load() {
		t.Fatal("sem chave a rede não pode ser aberta")
	}
}

func TestPolidor_ContextoChegaNaChave(t *testing.T) {
	f := novoFalso(t, fixo(200, respostaOpenAI(saida("ok"))))
	type marca struct{}
	ctx := context.WithValue(context.Background(), marca{}, "eu")
	var visto any
	p := &Polidor{
		Cliente: f.Cliente, Chamada: chamadaOpenAI,
		Chave: func(ctx context.Context) (string, error) { visto = ctx.Value(marca{}); return chave, nil },
	}
	if _, err := p.Polish(ctx, pedidoReal(ditado)); err != nil {
		t.Fatal(err)
	}
	if visto != "eu" {
		t.Fatal("o ctx do ditado não chegou à função da chave")
	}
}

// Sem Cliente, o Polidor usa o valor zero, que fala com as APIs oficiais.
// Confere sem abrir a rede: o transporte do pacote é trocado por um que só
// anota o destino.
func TestPolidor_ClienteNuloUsaAsAPIsOficiais(t *testing.T) {
	if got := (&Polidor{}).cliente(); !reflect.DeepEqual(got, &Cliente{}) {
		t.Fatalf("cliente() sem Cliente = %+v, quero o valor zero", got)
	}
	c := &Cliente{Timeout: time.Second}
	if got := (&Polidor{Cliente: c}).cliente(); got != c {
		t.Fatal("cliente() trocou o Cliente que o Polidor tem")
	}

	var vista string
	trocarTransporte(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		vista = r.URL.String()
		return nil, errors.New("sem rede de propósito")
	}))
	p := &Polidor{
		Chamada: chamadaOpenAI,
		Chave:   func(context.Context) (string, error) { return chave, nil },
	}
	_, err := p.Polish(context.Background(), pedidoReal(ditado))
	if vista != "https://api.openai.com/v1/responses" {
		t.Fatalf("URL = %q", vista)
	}
	if motivoDe(t, err) != resolver.MotivoTransporte {
		t.Fatalf("err = %v", err)
	}
}

func TestPolidor_UsoRecebeSoNumeros(t *testing.T) {
	f := novoFalso(t, fixo(200, respostaOpenAI(saida("ok"))))
	var vistos []Tokens
	p := &Polidor{
		Cliente: f.Cliente, Chamada: chamadaOpenAI,
		Chave: func(context.Context) (string, error) { return chave, nil },
		Uso:   func(u Tokens) { vistos = append(vistos, u) },
	}
	if _, err := p.Polish(context.Background(), pedidoReal(ditado)); err != nil {
		t.Fatal(err)
	}
	if quero := []Tokens{{Entrada: 1234, Saida: 56, Raciocinio: 7}}; !reflect.DeepEqual(vistos, quero) {
		t.Fatalf("Uso recebeu %+v, quero %+v", vistos, quero)
	}

	// Resposta recusada mas cobrada: Uso vê os tokens e o erro segue.
	f = novoFalso(t, fixo(200, `{"stop_reason":"refusal","usage":{"input_tokens":5,"output_tokens":1}}`))
	vistos = nil
	p = &Polidor{
		Cliente: f.Cliente, Chamada: chamadaAnthropic,
		Chave: func(context.Context) (string, error) { return chave, nil },
		Uso:   func(u Tokens) { vistos = append(vistos, u) },
	}
	_, err := p.Polish(context.Background(), pedidoReal(ditado))
	if motivoDe(t, err) != resolver.MotivoRecusa || len(vistos) != 1 || vistos[0] != (Tokens{Entrada: 5, Saida: 1}) {
		t.Fatalf("err = %v, Uso = %+v", err, vistos)
	}

	// Sem tokens informados (erro HTTP), Uso nem é chamado.
	f = novoFalso(t, fixo(503, ``))
	vistos = nil
	p.Cliente = f.Cliente
	if _, err := p.Polish(context.Background(), pedidoReal(ditado)); err == nil || len(vistos) != 0 {
		t.Fatalf("err = %v, Uso = %+v", err, vistos)
	}

	// Uso é opcional.
	f = novoFalso(t, fixo(200, respostaOpenAI(saida("ok"))))
	p = &Polidor{Cliente: f.Cliente, Chamada: chamadaOpenAI, Chave: func(context.Context) (string, error) { return chave, nil }}
	if _, err := p.Polish(context.Background(), pedidoReal(ditado)); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------
// De ponta a ponta com resolver.Polir
// ---------------------------------------------------------------------

func TestPolidor_ComResolverPolir(t *testing.T) {
	const smart = "bom dia tudo bem com você"
	res := resolver.ResolveResult{Mode: resolver.ModeSmart, RawText: smart, FinalText: smart}

	for _, c := range []struct {
		nome      string
		provedor  string
		status    int
		corpo     string
		reescrito bool
		motivo    resolver.MotivoFallback
		final     string
	}{
		{"openai organiza", OpenAI, 200, respostaOpenAI(saida("Bom dia, tudo bem com você?")), true, "", "Bom dia, tudo bem com você?"},
		{"anthropic organiza", Anthropic, 200, respostaAnthropic(saida("Bom dia, tudo bem com você?"), "end_turn"), true, "", "Bom dia, tudo bem com você?"},
		{"openai sem crédito", OpenAI, 429, erroOpenAI("insufficient_quota", "insufficient_quota"), false, resolver.MotivoSemCredito, smart},
		{"openai chave recusada", OpenAI, 401, erroOpenAI("", "invalid_api_key"), false, resolver.MotivoChaveRecusada, smart},
		{"anthropic modelo inexistente", Anthropic, 404, erroAnthropic("not_found_error", "model", nil), false, resolver.MotivoModeloInexistente, smart},
		{"openai sobrecarga", OpenAI, 503, ``, false, resolver.MotivoSobrecarga, smart},
		{"anthropic recusa", Anthropic, 200, respostaAnthropic("Não posso.", "refusal"), false, resolver.MotivoRecusa, smart},
		{"openai texto que responde a pergunta é barrado pelas guardas", OpenAI, 200, respostaOpenAI(saida("Bom dia! Como posso ajudar você hoje?")), false, resolver.MotivoPalavras, smart},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso(t, fixo(c.status, c.corpo))
			p := &Polidor{Cliente: f.Cliente, Chamada: chamadaDe(c.provedor), Chave: func(context.Context) (string, error) { return chave, nil }}
			got := resolver.Polir(context.Background(), p, res, resolver.PolirOpcoes{})
			if got.Reescrito != c.reescrito || got.MotivoFallback != c.motivo {
				t.Fatalf("Reescrito=%v Motivo=%q, quero %v/%q (Warnings %q)", got.Reescrito, got.MotivoFallback, c.reescrito, c.motivo, got.Warnings)
			}
			if got.FinalText != c.final {
				t.Fatalf("FinalText = %q, quero %q", got.FinalText, c.final)
			}
			for _, w := range got.Warnings {
				semVazamento(t, "Warning", w)
			}
		})
	}
}

// Sem chave: o Polir vê "sem_chave" e a rede nem é aberta.
func TestPolidor_ComResolverPolir_SemChave(t *testing.T) {
	var chamado atomic.Bool
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) { chamado.Store(true) })
	p := &Polidor{Cliente: f.Cliente, Chamada: chamadaOpenAI}
	res := resolver.ResolveResult{Mode: resolver.ModeSmart, FinalText: "bom dia tudo bem com você"}
	got := resolver.Polir(context.Background(), p, res, resolver.PolirOpcoes{})
	if got.Reescrito || got.MotivoFallback != resolver.MotivoSemChave || chamado.Load() {
		t.Fatalf("Reescrito=%v Motivo=%q rede=%v", got.Reescrito, got.MotivoFallback, chamado.Load())
	}
}

// O prazo do Agent chega ao Polir como "prazo".
func TestPolidor_ComResolverPolir_Prazo(t *testing.T) {
	solta := make(chan struct{})
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-solta:
		case <-r.Context().Done():
		}
	})
	defer close(solta)
	p := &Polidor{Cliente: f.Cliente, Chamada: chamadaOpenAI, Chave: func(context.Context) (string, error) { return chave, nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	res := resolver.ResolveResult{Mode: resolver.ModeSmart, FinalText: "bom dia tudo bem com você"}
	got := resolver.Polir(ctx, p, res, resolver.PolirOpcoes{})
	if got.Reescrito || got.MotivoFallback != resolver.MotivoPrazo {
		t.Fatalf("Reescrito=%v Motivo=%q, quero prazo", got.Reescrito, got.MotivoFallback)
	}
}

// ---------------------------------------------------------------------
// Canário: a chave e o ditado nunca saem
// ---------------------------------------------------------------------

// pedacos são todas as janelas de 8 caracteres do texto: achar qualquer uma
// é vazamento (mesmo desenho do TestChave_NuncaSaiDoCofre, no localapi).
func pedacos(texto string) []string {
	var p []string
	for i := 0; i+8 <= len(texto); i++ {
		p = append(p, texto[i:i+8])
	}
	return p
}

func procurar(t *testing.T, onde, texto string) {
	t.Helper()
	for _, alvo := range []struct{ nome, texto string }{{"a chave", chave}, {"o ditado", ditado}} {
		for _, p := range pedacos(alvo.texto) {
			if strings.Contains(texto, p) {
				t.Errorf("pedaço de %s (%q) em %s", alvo.nome, p, onde)
				return
			}
		}
	}
}

// TestChave_NuncaSaiDoPolish passa a chave e o ditado por TODOS os caminhos
// do POST — sucesso, cada categoria de erro dos dois provedores, recusa,
// truncamento, redirecionamento, prazo, sem rede — com um provedor que devolve
// os dois no corpo e num cabeçalho de resposta, e procura os dois em tudo o
// que o pacote produz: cada erro (Error, %v, %+v, %#v, %s, %q), o Polidor e o
// Cliente formatados, o log padrão do processo e o slog.
func TestChave_NuncaSaiDoPolish(t *testing.T) {
	var logPadrao bytes.Buffer
	antesSlog, antesLog := slog.Default(), log.Writer()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logPadrao, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&logPadrao)
	t.Cleanup(func() { slog.SetDefault(antesSlog); log.SetOutput(antesLog) })

	var logExplicito bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logExplicito, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var recebidas atomic.Int32
	// modo é lido pela goroutine do servidor e trocado pela do teste: atômico,
	// para o -race não ter o que apontar.
	var modo atomic.Pointer[func(w http.ResponseWriter, r *http.Request)]
	f := novoFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key")+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == chave {
			recebidas.Add(1)
		}
		w.Header().Set("X-Eco", chave+" "+ditado)
		(*modo.Load())(w, r)
	})

	destino := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirecionamento seguido até o destino")
	}))
	defer destino.Close()

	respostas := map[string]func(w http.ResponseWriter, r *http.Request){
		"sucesso": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, respostaOpenAI(saida("Bom dia.")))
		},
		"sucesso anthropic": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, respostaAnthropic(saida("Bom dia."), "end_turn"))
		},
		"401":           fixo(401, erroOpenAI("invalid_request_error", "invalid_api_key")),
		"402":           fixo(402, erroAnthropic("billing_error", "x", nil)),
		"404":           fixo(404, erroOpenAI("invalid_request_error", "model_not_found")),
		"429 sem saldo": fixo(429, erroOpenAI("insufficient_quota", "insufficient_quota")),
		"429 limite":    fixo(429, erroOpenAI("rate_limit_error", "rate_limit_exceeded")),
		"400 pedido":    fixo(400, erroOpenAI("invalid_request_error", "unsupported_parameter")),
		"400 crédito":   fixo(400, erroAnthropic("invalid_request_error", "Your credit balance is too low", nil)),
		"503":           fixo(503, erroOpenAI("service_unavailable_error", "server_is_overloaded")),
		"529":           fixo(529, erroAnthropic("overloaded_error", "x", nil)),
		"200 recusa":    fixo(200, `{"stop_reason":"refusal","content":[{"type":"text","text":"`+eco()+`"}]}`),
		"200 truncada":  fixo(200, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"`+eco()+`"}]}]}`),
		"200 refusal":   fixo(200, `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"`+eco()+`"}]}]}`),
		"200 lixo":      fixo(200, "lixo "+eco()),
		"200 envelope":  fixo(200, `{"status":"failed","error":{"message":"`+eco()+`"}}`),
		"redirecionar":  func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destino.URL+"?k="+chave, 307) },
		"não é objeto":  fixo(200, `"`+eco()+`"`),
		"stop_reason ?": fixo(200, `{"stop_reason":"`+eco()+`","content":[]}`),
		"status ?":      fixo(200, `{"status":"`+eco()+`"}`),
		"corpo grande": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Write(bytes.Repeat([]byte(eco()), 60000))
		},
		"erro com eco":   fixo(500, "<html>"+eco()+"</html>"),
		"json de erro 2": fixo(418, erroAnthropic("invalid_request_error", eco(), map[string]any{"error_code": eco()})),
	}

	polidores := map[string]*Polidor{}
	for _, p := range []string{OpenAI, Anthropic} {
		polidores[p] = &Polidor{
			Cliente: f.Cliente, Chamada: chamadaDe(p),
			Chave: func(context.Context) (string, error) { return chave, nil },
		}
	}

	var todosOsErros []error
	total := 0
	for nome, h := range respostas {
		modo.Store(&h)
		for _, prov := range []string{OpenAI, Anthropic} {
			total++
			p := polidores[prov]
			resp, err := f.Polir(context.Background(), chamadaDe(prov), chave, pedidoReal(ditado))
			_ = resp
			_, err2 := p.Polish(context.Background(), pedidoReal(ditado))
			for _, e := range []error{err, err2} {
				if e == nil {
					continue
				}
				todosOsErros = append(todosOsErros, e)
				for _, verbo := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
					procurar(t, fmt.Sprintf("%s/%s %s do erro", nome, prov, verbo), fmt.Sprintf(verbo, e))
				}
				logger.Error("polish falhou", "erro", e, "motivo", fmt.Sprint(e))
				slog.Error("polish falhou", "erro", e) // o log padrão do processo
				log.Printf("polish: %v", e)
			}
		}
	}
	if total < 40 || len(todosOsErros) < 30 {
		t.Fatalf("o teste não exercitou os caminhos: %d chamadas, %d erros", total, len(todosOsErros))
	}

	// Com dentes: a chave de fato circulou, e é pedido de fato o que sai.
	if int(recebidas.Load()) < total {
		t.Fatalf("o provedor viu a chave %d vezes em %d chamadas: o teste não exercitou o caminho", recebidas.Load(), total)
	}

	for nome, p := range polidores {
		for _, verbo := range []string{"%v", "%+v", "%#v"} {
			procurar(t, fmt.Sprintf("%s do Polidor %s", verbo, nome), fmt.Sprintf(verbo, p))
			procurar(t, fmt.Sprintf("%s do Polidor %s (valor)", verbo, nome), fmt.Sprintf(verbo, *p))
		}
		logger.Info("polidor", "polidor", p, "chamada", p.Chamada)
	}
	for _, verbo := range []string{"%v", "%+v", "%#v"} {
		procurar(t, verbo+" do Cliente", fmt.Sprintf(verbo, f.Cliente))
		procurar(t, verbo+" do Cliente (valor)", fmt.Sprintf(verbo, *f.Cliente))
		procurar(t, verbo+" da Chamada", fmt.Sprintf(verbo, chamadaOpenAI))
	}

	if logExplicito.Len() == 0 || logPadrao.Len() == 0 {
		t.Fatal("os logs ficaram vazios: o teste não está vendo o que se registra")
	}
	procurar(t, "slog explícito", logExplicito.String())
	procurar(t, "log padrão do processo", logPadrao.String())

	// O pacote não escreve nada no log por conta própria: o log padrão só
	// tem as linhas que este teste mandou.
	if got := strings.Count(logPadrao.String(), "polish falhou") + strings.Count(logPadrao.String(), "polish: "); got == 0 {
		t.Fatal("o log padrão não recebeu as linhas do teste")
	}
}
