package ia

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// chave tem cara de chave de API sem ser uma. Nenhum teste deste arquivo
// sai da máquina: todo provedor aqui é um httptest.Server em loopback.
const chave = "sk-teste-ChaveQueNuncaPodeVazar-0123456789"

// provedorFalso sobe um httptest.Server e aponta o Cliente para ele.
func provedorFalso(t *testing.T, h http.HandlerFunc) *Cliente {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Cliente{BaseOpenAI: srv.URL, BaseAnthropic: srv.URL}
}

func TestListarModelos_OpenAI(t *testing.T) {
	c := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("pedido = %s %s, quero GET /v1/models", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+chave {
			t.Errorf("Authorization errado (%d caracteres)", len(got))
		}
		if r.Header.Get("x-api-key") != "" {
			t.Error("a OpenAI recebeu o cabeçalho da Anthropic")
		}
		// Fora de ordem de propósito: a OpenAI não promete ordem.
		w.Write([]byte(`{"object":"list","data":[
			{"id":"modelo-b","created":100},
			{"id":"modelo-novo","created":300},
			{"id":"modelo-a","created":100},
			{"id":"","created":999}]}`))
	})
	got, err := c.ListarModelos(context.Background(), OpenAI, chave)
	if err != nil {
		t.Fatalf("ListarModelos: %v", err)
	}
	if quero := []string{"modelo-novo", "modelo-a", "modelo-b"}; !reflect.DeepEqual(got, quero) {
		t.Fatalf("modelos = %v, quero %v (mais novo primeiro, nome no empate, sem id vazio)", got, quero)
	}
}

func TestListarModelos_Anthropic(t *testing.T) {
	c := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("pedido = %s %s, quero GET /v1/models", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "1000" {
			t.Errorf("limit = %q, quero 1000 (a lista inteira numa página)", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("x-api-key") != chave {
			t.Error("x-api-key errado")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("a Anthropic recebeu o cabeçalho da OpenAI")
		}
		w.Write([]byte(`{"data":[{"type":"model","id":"modelo-2"},{"type":"model","id":"modelo-1"}],"has_more":false}`))
	})
	got, err := c.ListarModelos(context.Background(), Anthropic, chave)
	if err != nil {
		t.Fatalf("ListarModelos: %v", err)
	}
	if quero := []string{"modelo-2", "modelo-1"}; !reflect.DeepEqual(got, quero) {
		t.Fatalf("modelos = %v, quero %v (na ordem do provedor)", got, quero)
	}
}

func TestListarModelos_ListaVaziaNaoEhErro(t *testing.T) {
	c := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[]}`)) })
	got, err := c.ListarModelos(context.Background(), OpenAI, chave)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

// TestListarModelos_ErrosTraduzidosSemAChave: cada falha HTTP vira uma
// mensagem fixa em português — e o corpo do provedor, que no 401 da
// OpenAI traz um pedaço da chave, não passa adiante.
func TestListarModelos_ErrosTraduzidosSemAChave(t *testing.T) {
	for _, c := range []struct {
		status int
		motivo Motivo
		trecho string
	}{
		{401, MotivoChaveRecusada, "recusou a chave"},
		{403, MotivoSemPermissao, "negou acesso"},
		{429, MotivoLimite, "limite de uso"},
		{500, MotivoIndisponivel, "HTTP 500"},
		{529, MotivoIndisponivel, "HTTP 529"},
		{302, MotivoRespostaInesperada, "HTTP 302"},
		{404, MotivoRespostaInesperada, "HTTP 404"},
	} {
		for _, p := range []string{OpenAI, Anthropic} {
			cli := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
				if c.status == 302 {
					w.Header().Set("Location", "/outro")
				}
				w.WriteHeader(c.status)
				w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + chave + `"}}`))
			})
			_, err := cli.ListarModelos(context.Background(), p, chave)
			var e *Erro
			if !errors.As(err, &e) {
				t.Fatalf("%s %d: err = %v, quero *Erro", p, c.status, err)
			}
			if e.Motivo != c.motivo {
				t.Errorf("%s %d: motivo = %s, quero %s", p, c.status, e.Motivo, c.motivo)
			}
			if !strings.Contains(e.Mensagem, c.trecho) || !strings.Contains(e.Mensagem, Nome(p)) {
				t.Errorf("%s %d: mensagem = %q", p, c.status, e.Mensagem)
			}
			if strings.Contains(err.Error(), chave) || strings.Contains(err.Error(), "Incorrect") {
				t.Errorf("%s %d: a mensagem trouxe o corpo do provedor: %q", p, c.status, err)
			}
		}
	}
}

// TestListarModelos_NaoSegueRedirecionamento: um 3xx para outro lugar
// não é seguido, e o outro lugar nunca vê a chave — o x-api-key da
// Anthropic não é removido pelo net/http ao trocar de domínio.
func TestListarModelos_NaoSegueRedirecionamento(t *testing.T) {
	var visitado atomic.Bool
	destino := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visitado.Store(true)
		w.Write([]byte(`{"data":[{"id":"x"}]}`))
	}))
	defer destino.Close()

	for _, p := range []string{OpenAI, Anthropic} {
		c := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destino.URL+"/v1/models", http.StatusTemporaryRedirect)
		})
		// Mesmo com um cliente injetado que seguiria redirecionamento.
		c.HTTP = &http.Client{}
		if _, err := c.ListarModelos(context.Background(), p, chave); err == nil {
			t.Errorf("%s: o redirecionamento foi tratado como sucesso", p)
		}
	}
	if visitado.Load() {
		t.Fatal("o redirecionamento foi seguido: a chave foi entregue a outro servidor")
	}
}

func TestListarModelos_TempoEsgotado(t *testing.T) {
	solta := make(chan struct{})
	c := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-solta:
		case <-r.Context().Done():
		}
	})
	defer close(solta)
	c.Timeout = 50 * time.Millisecond

	inicio := time.Now()
	_, err := c.ListarModelos(context.Background(), OpenAI, chave)
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoTempoEsgotado {
		t.Fatalf("err = %v, quero tempo esgotado", err)
	}
	if d := time.Since(inicio); d > 5*time.Second {
		t.Fatalf("o timeout não cortou a chamada: %v", d)
	}
}

func TestListarModelos_TimeoutPadraoEhDezSegundos(t *testing.T) {
	if TimeoutPadrao != 10*time.Second {
		t.Fatalf("TimeoutPadrao = %v, o contrato diz 10 s", TimeoutPadrao)
	}
	err := falhaDeRede(func() context.Context {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		t.Cleanup(cancel)
		return ctx
	}(), OpenAI, TimeoutPadrao)
	if !strings.Contains(err.Error(), "10 segundos") {
		t.Fatalf("mensagem = %q", err)
	}
}

func TestListarModelos_SemRede(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	endereco := srv.URL
	srv.Close() // ninguém escuta mais ali
	c := &Cliente{BaseOpenAI: endereco, BaseAnthropic: endereco}
	for _, p := range []string{OpenAI, Anthropic} {
		_, err := c.ListarModelos(context.Background(), p, chave)
		var e *Erro
		if !errors.As(err, &e) || e.Motivo != MotivoSemRede {
			t.Fatalf("%s: err = %v, quero sem rede", p, err)
		}
		if strings.Contains(err.Error(), chave) || strings.Contains(err.Error(), "127.0.0.1") {
			t.Fatalf("%s: a mensagem trouxe detalhe da requisição: %q", p, err)
		}
	}
}

func TestListarModelos_RespostaQueNaoEhLista(t *testing.T) {
	for _, corpo := range []string{`não é json`, `{}`, `{"data":null}`, `[]`} {
		for _, p := range []string{OpenAI, Anthropic} {
			c := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(corpo)) })
			_, err := c.ListarModelos(context.Background(), p, chave)
			var e *Erro
			if !errors.As(err, &e) || e.Motivo != MotivoRespostaInesperada {
				t.Errorf("%s %q: err = %v, quero resposta inesperada", p, corpo, err)
			}
		}
	}
}

func TestListarModelos_ProvedorDesconhecidoNemChama(t *testing.T) {
	var chamado atomic.Bool
	c := provedorFalso(t, func(w http.ResponseWriter, r *http.Request) { chamado.Store(true) })
	if _, err := c.ListarModelos(context.Background(), "outro", chave); err == nil {
		t.Fatal("provedor desconhecido aceito")
	}
	if chamado.Load() {
		t.Fatal("chamou a rede com provedor desconhecido")
	}
}

// TestRequisicao_ValorZeroFalaComAsAPIsOficiaisPorHTTPS trava o destino
// da chave. Todo outro teste aponta o Cliente para um httptest.Server, e
// sem este uma troca de BaseOpenAI/BaseAnthropic para http:// ou para
// outro host passaria a suíte inteira — e mandaria a chave de quem usa
// para fora da máquina, em claro ou para quem não devia. Só monta a
// requisição: não chama a rede.
func TestRequisicao_ValorZeroFalaComAsAPIsOficiaisPorHTTPS(t *testing.T) {
	for _, c := range []struct {
		provedor, url, cabecalho, valor string
	}{
		{OpenAI, "https://api.openai.com/v1/models", "Authorization", "Bearer " + chave},
		{Anthropic, "https://api.anthropic.com/v1/models?limit=1000", "x-api-key", chave},
	} {
		req, err := (&Cliente{}).requisicao(context.Background(), c.provedor, chave)
		if err != nil {
			t.Fatalf("%s: %v", c.provedor, err)
		}
		if got := req.URL.String(); got != c.url {
			t.Errorf("%s: URL = %q, quero %q", c.provedor, got, c.url)
		}
		if req.Method != http.MethodGet {
			t.Errorf("%s: método = %s, quero GET", c.provedor, req.Method)
		}
		if strings.Contains(req.URL.String(), chave) {
			t.Errorf("%s: a chave está na URL", c.provedor)
		}
		if got := req.Header.Get(c.cabecalho); got != c.valor {
			t.Errorf("%s: %s = %q, quero a chave ali", c.provedor, c.cabecalho, got)
		}
	}
}

func TestValidoENome(t *testing.T) {
	if !Valido(OpenAI) || !Valido(Anthropic) || Valido("") || Valido("OpenAI") || Valido("google") {
		t.Fatal("Valido aceita/recusa errado")
	}
	if Nome(OpenAI) != "OpenAI" || Nome(Anthropic) != "Anthropic" {
		t.Fatal("Nome errado")
	}
}

// Toda resposta tem o corpo fechado, seja qual for o caminho: sem isto cada
// teste da chave vazaria a conexão e as goroutines do transporte.
func TestListarModelos_FechaOCorpoDaResposta(t *testing.T) {
	for _, c := range []struct {
		nome   string
		status int
		corpo  string
	}{
		{"200 com a lista", 200, `{"data":[{"id":"modelo-1","created":1}]}`},
		{"200 que não é lista", 200, "lixo"},
		{"erro HTTP", 401, `{"error":{}}`},
		{"redirecionamento", 302, ""},
	} {
		t.Run(c.nome, func(t *testing.T) {
			var fechado atomic.Int32
			cli := &Cliente{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: c.status, Header: http.Header{"Location": {"/outro"}},
					Body: &corpoMarcado{Reader: strings.NewReader(c.corpo), fechou: func() { fechado.Add(1) }},
				}, nil
			})}}
			cli.ListarModelos(context.Background(), OpenAI, chave)
			if fechado.Load() == 0 {
				t.Fatal("o corpo da resposta não foi fechado")
			}
		})
	}
}
