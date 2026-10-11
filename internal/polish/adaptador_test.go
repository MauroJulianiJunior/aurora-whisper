package polish

// O adaptador do Polish (ADR-016, S4; ADR-021, C5a) sozinho, sem o Agent: a
// chave vinda de uma função a cada ditado, a política de esforço e o
// resolver montado. Tudo contra um provedor falso em loopback (httptest),
// com um transporte que RECUSA qualquer destino fora da máquina: nenhum
// teste daqui fala com a OpenAI ou a Anthropic de verdade. É o que o
// servidor da C5b vai montar igual ao Agent, que tem os testes dele em
// cmd/agent (polish_test.go), sem mudança.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

const (
	ditadoDeTeste = "o cliente mandou o balancete de agosto"
	polidoDeTeste = "O cliente mandou o balancete de agosto."
	// Chave óbvia de mentira.
	chaveDeTeste = "sk-teste-chave-de-mentira-0123456789"
)

// soLoopback deixa passar só pedido para a própria máquina. Se um dia o
// adaptador ignorar a base injetada, o pedido morre aqui em vez de levar a
// chave e o ditado de teste a um provedor real.
type soLoopback struct {
	base http.RoundTripper
	fora *atomic.Int32
}

func (s soLoopback) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		if req.Body != nil {
			req.Body.Close()
		}
		s.fora.Add(1)
		return nil, errors.New("rede: o teste tentou sair da máquina")
	}
	return s.base.RoundTrip(req)
}

type pedidoVisto struct {
	caminho string
	chave   string
	corpo   map[string]any
}

// provedorFalso é a OpenAI e a Anthropic de mentira, no mesmo endereço (os
// caminhos são diferentes). responder decide a resposta de cada pedido.
type provedorFalso struct {
	srv       *httptest.Server
	mu        sync.Mutex
	pedidos   []pedidoVisto
	responder func(p pedidoVisto, w http.ResponseWriter)
	fora      atomic.Int32
}

func novoProvedorFalso(t *testing.T, responder func(p pedidoVisto, w http.ResponseWriter)) *provedorFalso {
	t.Helper()
	f := &provedorFalso{responder: responder}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		p := pedidoVisto{
			caminho: r.URL.Path,
			chave:   r.Header.Get("x-api-key") + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		}
		_ = json.Unmarshal(corpo, &p.corpo)
		f.mu.Lock()
		f.pedidos = append(f.pedidos, p)
		f.mu.Unlock()
		f.responder(p, w)
	}))
	t.Cleanup(func() {
		f.srv.Close()
		if n := f.fora.Load(); n > 0 {
			t.Errorf("%d pedido(s) tentaram sair da máquina", n)
		}
	})
	return f
}

func (f *provedorFalso) cliente() *ia.Cliente {
	return &ia.Cliente{
		BaseOpenAI:    f.srv.URL,
		BaseAnthropic: f.srv.URL,
		HTTP:          &http.Client{Transport: soLoopback{base: ia.NovoTransporteHTTP1(), fora: &f.fora}},
	}
}

func (f *provedorFalso) vistos() []pedidoVisto {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pedidoVisto(nil), f.pedidos...)
}

func respostaOpenAI(w http.ResponseWriter, finalText string) {
	saida, _ := json.Marshal(map[string]string{"final_text": finalText})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "completed",
		"output": []any{map[string]any{
			"type":    "message",
			"content": []any{map[string]any{"type": "output_text", "text": string(saida)}},
		}},
		"usage": map[string]any{"input_tokens": 852, "output_tokens": 34},
	})
}

func respostaAnthropic(w http.ResponseWriter, finalText string) {
	saida, _ := json.Marshal(map[string]string{"final_text": finalText})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"stop_reason": "end_turn",
		"content":     []any{map[string]any{"type": "text", "text": string(saida)}},
		"usage":       map[string]any{"input_tokens": 1157, "output_tokens": 30},
	})
}

// respostaIncompativel é o 400 da OpenAI para um modelo que recusa o
// esforço de raciocínio pedido.
func respostaIncompativel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprint(w, `{"error":{"type":"invalid_request_error","param":"reasoning.effort","code":"unsupported_value","message":"Unsupported value: 'reasoning.effort'."}}`)
}

// esforcoDoPedido lê o reasoning.effort do corpo, ou "sem campo".
func esforcoDoPedido(p pedidoVisto) string {
	r, ok := p.corpo["reasoning"].(map[string]any)
	if !ok {
		return "sem campo"
	}
	e, _ := r["effort"].(string)
	return e
}

// chaveFixa é a Chave de quem tem uma chave só.
func chaveFixa(chave string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return chave, nil }
}

// ditar roda a metade de rede de UM ditado como o runtime a roda: abre o
// Polidor e chama Reescrever com o resultado do Smart.
func ditar(t *testing.T, m Montagem, texto string) (resolver.ResolveResult, string, [2]int) {
	t.Helper()
	if m.Smart == nil {
		m.Smart = resolver.NewSmartResolver(nil)
	}
	var tokens [2]int
	m.Contar = func(e, s int) { tokens = [2]int{e, s} }
	pr, rotulo := m.Abrir()
	req := resolver.ResolveRequest{ASRResult: asr.ASRResult{Text: texto, Language: "pt-BR"}}
	smart, err := m.Smart.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Smart: %v", err)
	}
	return pr.Reescrever(context.Background(), req, smart), rotulo, tokens
}

func montagemDeTeste(f *provedorFalso, provedor, modelo string, politica *PoliticaEsforco) Montagem {
	return Montagem{
		Cliente:  f.cliente(),
		Esforco:  politica,
		Provedor: provedor,
		Modelo:   modelo,
		Chave:    chaveFixa(chaveDeTeste),
	}
}

// TestAbrir_AceitoComAChaveDeQuemChama: o caminho feliz. O Polish vai ao
// provedor com a chave que a função devolveu, com o modelo do ditado e o
// esforço padrão, e volta aceito, com os tokens contados.
func TestAbrir_AceitoComAChaveDeQuemChama(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	res, rotulo, tokens := ditar(t, montagemDeTeste(f, "openai", "modelo-x", NovaPoliticaEsforco(nil)), ditadoDeTeste)

	if !res.Reescrito || res.FinalText != polidoDeTeste || res.MotivoFallback != resolver.MotivoNenhum {
		t.Fatalf("resultado = %+v, quero o Polish aceito", res)
	}
	vistos := f.vistos()
	if len(vistos) != 1 || vistos[0].chave != chaveDeTeste || vistos[0].caminho != "/v1/responses" {
		t.Fatalf("pedidos = %+v, quero um POST /v1/responses com a chave de quem chama", vistos)
	}
	if vistos[0].corpo["model"] != "modelo-x" {
		t.Errorf("modelo enviado = %v, quero o do ditado", vistos[0].corpo["model"])
	}
	if esforcoDoPedido(vistos[0]) != "none" || rotulo != "none" || tokens != [2]int{852, 34} {
		t.Errorf("esforço enviado %q, rótulo %q, tokens %v; quero none, none e [852 34]", esforcoDoPedido(vistos[0]), rotulo, tokens)
	}
}

// TestAbrir_AnthropicNaoRecebeEsforco: o pedido vai ao endereço da
// Anthropic, com a chave dela, sem reasoning.
func TestAbrir_AnthropicNaoRecebeEsforco(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaAnthropic(w, polidoDeTeste) })
	res, rotulo, tokens := ditar(t, montagemDeTeste(f, "anthropic", "modelo-a", NovaPoliticaEsforco(nil)), ditadoDeTeste)
	vistos := f.vistos()
	if !res.Reescrito || len(vistos) != 1 || vistos[0].caminho != "/v1/messages" || vistos[0].chave != chaveDeTeste {
		t.Fatalf("resultado %+v, pedidos %+v; quero a Anthropic, com a chave", res, vistos)
	}
	if _, tem := vistos[0].corpo["reasoning"]; tem || rotulo != "nao_se_aplica" || tokens != [2]int{1157, 30} {
		t.Errorf("reasoning %v, rótulo %q, tokens %v; quero nenhum reasoning, nao_se_aplica e [1157 30]", vistos[0].corpo["reasoning"], rotulo, tokens)
	}
}

// TestAbrir_ChaveLidaACadaDitado: a chave não é guardada. A função é
// chamada a cada ditado, e o que ela devolver vale no ditado seguinte
// (trocar, remover). O mesmo vale no servidor, onde ela decifra a chave da
// conta.
func TestAbrir_ChaveLidaACadaDitado(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	var chaveAtual atomic.Value
	chaveAtual.Store("sk-primeira-chave-1111")
	var leituras atomic.Int32
	m := montagemDeTeste(f, "openai", "m", NovaPoliticaEsforco(nil))
	m.Chave = func(context.Context) (string, error) {
		leituras.Add(1)
		c := chaveAtual.Load().(string)
		if c == "" {
			return "", resolver.ErrPolishSemChave
		}
		return c, nil
	}

	ditar(t, m, ditadoDeTeste)
	chaveAtual.Store("sk-segunda-chave-2222")
	ditar(t, m, ditadoDeTeste)
	chaveAtual.Store("")
	res, _, _ := ditar(t, m, ditadoDeTeste)

	vistos := f.vistos()
	if len(vistos) != 2 || vistos[0].chave != "sk-primeira-chave-1111" || vistos[1].chave != "sk-segunda-chave-2222" {
		t.Fatalf("chaves recebidas = %+v; quero a primeira, depois a segunda, e nada depois de remover", vistos)
	}
	if res.Reescrito || res.MotivoFallback != resolver.MotivoSemChave || res.FinalText != ditadoDeTeste {
		t.Fatalf("depois de remover a chave: %+v, quero o Smart com o motivo sem_chave", res)
	}
	if leituras.Load() != 3 {
		t.Errorf("a chave foi pedida %d vez(es); quero 3, uma por ditado", leituras.Load())
	}
}

// TestAbrir_ErroDaChaveViraSemChave: a função que falha (o cofre ocupado,
// a decifragem que não bate) pode trazer qualquer coisa no erro: ele vira
// sem_chave, nada sai, e o texto dele não chega ao resultado.
func TestAbrir_ErroDaChaveViraSemChave(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	m := montagemDeTeste(f, "openai", "m", NovaPoliticaEsforco(nil))
	m.Chave = func(context.Context) (string, error) { return "", errors.New("cofre ocupado: " + chaveDeTeste) }

	res, _, _ := ditar(t, m, ditadoDeTeste)
	if res.Reescrito || res.MotivoFallback != resolver.MotivoSemChave || len(f.vistos()) != 0 {
		t.Fatalf("resultado = %+v, pedidos %d; quero sem_chave e nenhum pedido", res, len(f.vistos()))
	}
	if strings.Contains(fmt.Sprint(res.Warnings), "ocupado") || strings.Contains(fmt.Sprint(res), chaveDeTeste) {
		t.Errorf("o texto do erro da chave chegou ao resultado: %v", res.Warnings)
	}

	// Sem função nenhuma é o mesmo que sem chave.
	m.Chave = nil
	if res, _, _ := ditar(t, m, ditadoDeTeste); res.MotivoFallback != resolver.MotivoSemChave || len(f.vistos()) != 0 {
		t.Fatalf("Chave nula: %+v, pedidos %d; quero sem_chave e nenhum pedido", res, len(f.vistos()))
	}
}

// TestAbrir_ProvedorInvalidoNaoTemClienteNemLeAChave: sem provedor válido, o
// Polish não tem cliente — e nem chama a função da chave. A política pode
// faltar, porque nem é consultada.
func TestAbrir_ProvedorInvalidoNaoTemClienteNemLeAChave(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	for _, provedor := range []string{"", "google", "OpenAI"} {
		var leituras atomic.Int32
		m := montagemDeTeste(f, provedor, "m", nil)
		m.Chave = func(context.Context) (string, error) { leituras.Add(1); return chaveDeTeste, nil }

		res, rotulo, _ := ditar(t, m, ditadoDeTeste)
		if res.Reescrito || res.MotivoFallback != resolver.MotivoSemCliente || rotulo != "" || res.FinalText != ditadoDeTeste {
			t.Fatalf("provedor %q: %+v, rótulo %q; quero o Smart com sem_cliente e rótulo vazio", provedor, res, rotulo)
		}
		if leituras.Load() != 0 {
			t.Fatalf("provedor %q: leu a chave %d vez(es)", provedor, leituras.Load())
		}
	}
	if n := len(f.vistos()); n != 0 {
		t.Fatalf("o provedor recebeu %d pedido(s) sem provedor válido", n)
	}
}

// TestAbrir_PoliticaDeEsforcoDesce: nenhum nome de modelo no código. Um
// modelo que recusa o "none" recebe "minimal" no ditado seguinte, e depois
// nenhum campo; o ditado que recebeu a recusa cai no Smart (um pedido por
// ditado, sem retry); outro modelo começa do padrão. A política é UMA, a
// mesma entre as montagens, como a do processo.
func TestAbrir_PoliticaDeEsforcoDesce(t *testing.T) {
	f := novoProvedorFalso(t, func(p pedidoVisto, w http.ResponseWriter) {
		if p.corpo["model"] == "modelo-sem-raciocinio" && esforcoDoPedido(p) != "sem campo" {
			respostaIncompativel(w)
			return
		}
		respostaOpenAI(w, polidoDeTeste)
	})
	var logBuf bytes.Buffer
	politica := NovaPoliticaEsforco(slog.New(slog.NewTextHandler(&logBuf, nil)))

	type passo struct {
		modelo, querEsforco, querRotulo string
		querMotivo                      resolver.MotivoFallback
	}
	for i, p := range []passo{
		{"modelo-sem-raciocinio", "none", "none", resolver.MotivoModeloIncompativel},
		{"modelo-novo", "none", "none", resolver.MotivoNenhum}, // outro modelo não herda
		{"modelo-sem-raciocinio", "minimal", "minimal", resolver.MotivoModeloIncompativel},
		{"modelo-sem-raciocinio", "sem campo", "omitir", resolver.MotivoNenhum},
		{"modelo-sem-raciocinio", "sem campo", "omitir", resolver.MotivoNenhum}, // fica no último degrau
		{"modelo-novo", "none", "none", resolver.MotivoNenhum},
	} {
		res, rotulo, _ := ditar(t, montagemDeTeste(f, "openai", p.modelo, politica), ditadoDeTeste)
		vistos := f.vistos()
		if len(vistos) != i+1 {
			t.Fatalf("passo %d: %d pedido(s), quero %d: sem retry, um pedido por ditado", i, len(vistos), i+1)
		}
		if got := esforcoDoPedido(vistos[i]); got != p.querEsforco || rotulo != p.querRotulo {
			t.Fatalf("passo %d (%s): esforço enviado %q, rótulo %q; quero %q e %q", i, p.modelo, got, rotulo, p.querEsforco, p.querRotulo)
		}
		if res.MotivoFallback != p.querMotivo || res.Reescrito != (p.querMotivo == resolver.MotivoNenhum) {
			t.Fatalf("passo %d: %+v, quero o motivo %q", i, res, p.querMotivo)
		}
		if p.querMotivo != resolver.MotivoNenhum && res.FinalText != ditadoDeTeste {
			t.Fatalf("passo %d: o ditado recusado não caiu no Smart: %q", i, res.FinalText)
		}
	}
	if n := strings.Count(logBuf.String(), "recusou o esforço"); n != 2 {
		t.Fatalf("log da política (%d linha[s]), quero 2:\n%s", n, logBuf.String())
	}

	// Outra política (outro processo): o padrão volta.
	if _, rotulo, _ := ditar(t, montagemDeTeste(f, "openai", "modelo-sem-raciocinio", NovaPoliticaEsforco(nil)), ditadoDeTeste); rotulo != "none" {
		t.Fatalf("outra política começou com %q; a memória é só do processo", rotulo)
	}
}

// TestAbrir_NadaDeSegredoNemDeTextoSaiPeloAdaptador estende a conferência
// do Agent (TestChave_NuncaSaiDoPolishDoAgent) ao pacote comum: a chave sai
// da função, vai ao cabeçalho e volta ecoada pelo provedor no corpo, num
// cabeçalho dos erros e dentro do próprio final_text. Nada do que o
// adaptador devolve — resultado, avisos, motivo, rótulo — nem o log dele
// pode ter um pedaço dela. O ditado também não vai ao log.
func TestAbrir_NadaDeSegredoNemDeTextoSaiPeloAdaptador(t *testing.T) {
	const chave = "sk-proj-QwErTyVaZaMeNtOzZ-XyXyKpKpWvWv-kLmNoPqR-fImJ"
	const ditado = "o cliente Zephyrion pagou a guia de agosto"
	var n atomic.Int32
	f := novoProvedorFalso(t, func(p pedidoVisto, w http.ResponseWriter) {
		w.Header().Set("X-Eco", p.chave)
		switch n.Add(1) {
		case 1: // a chave dentro do texto "polido": a guarda recusa
			respostaOpenAI(w, ditado+" "+p.chave)
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":{"message":"Incorrect API key provided: %s","code":"invalid_api_key"}}`, p.chave)
		case 3:
			respostaIncompativel(w)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":{"message":"falhou com %s"}}`, p.chave)
		}
	})
	var logAdaptador bytes.Buffer
	politica := NovaPoliticaEsforco(slog.New(slog.NewTextHandler(&logAdaptador, &slog.HandlerOptions{Level: slog.LevelDebug})))
	m := montagemDeTeste(f, "openai", "modelo-x", politica)
	m.Chave = chaveFixa(chave)

	var tudo strings.Builder
	for i := 0; i < 4; i++ {
		res, rotulo, tokens := ditar(t, m, ditado)
		if res.Reescrito {
			t.Fatalf("ditado %d aceito com a chave no texto: %q", i, res.FinalText)
		}
		fmt.Fprintf(&tudo, "%+v|%v|%s|%v\n", res, res.Warnings, rotulo, tokens)
	}
	if got := len(f.vistos()); got != 4 || f.vistos()[0].chave != chave {
		t.Fatalf("o provedor recebeu %d pedido(s); quero 4, com a chave: o teste não exercitou o caminho", got)
	}
	if !strings.Contains(logAdaptador.String(), "recusou o esforço") {
		t.Fatalf("o log do adaptador ficou sem a linha da política: o teste não está vendo o que se registra:\n%s", logAdaptador.String())
	}
	for onde, texto := range map[string]string{
		"resultado":        tudo.String(),
		"log do adaptador": logAdaptador.String(),
		"a política":       fmt.Sprintf("%+v", politica),
	} {
		for i := 0; i+8 <= len(chave); i++ {
			if strings.Contains(texto, chave[i:i+8]) {
				t.Errorf("pedaço da chave (%q) em %s", chave[i:i+8], onde)
				break
			}
		}
	}
	if strings.Contains(logAdaptador.String(), "Zephyrion") {
		t.Errorf("texto ditado no log do adaptador:\n%s", logAdaptador.String())
	}
}
