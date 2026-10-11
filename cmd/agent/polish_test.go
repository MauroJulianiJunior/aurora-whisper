package main

// O adaptador do Polish (ADR-016, S4): a chave lida do cofre a cada
// ditado, a política de esforço e o resolver montado sem dicionário. Tudo
// contra um provedor falso em loopback (httptest), com um transporte que
// RECUSA qualquer destino fora da máquina: nenhum teste daqui fala com a
// OpenAI ou a Anthropic de verdade, nem lê o Gerenciador de Credenciais
// (o cofre é chaves.Memoria).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// soLoopback deixa passar só pedido para a própria máquina. Se um dia o
// adaptador ignorar a base injetada, o pedido morre aqui em vez de levar a
// chave e o ditado de teste a um provedor real.
type soLoopback struct {
	base  http.RoundTripper
	fora  *atomic.Int32
	hosts *sync.Map
}

func (s soLoopback) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		if req.Body != nil {
			req.Body.Close()
		}
		s.fora.Add(1)
		s.hosts.Store(req.URL.Host, true)
		return nil, errors.New("rede: o teste tentou sair da máquina")
	}
	return s.base.RoundTrip(req)
}

// pedidoVisto é o que o provedor falso recebeu: o cabeçalho de
// autenticação e o corpo decodificado.
type pedidoVisto struct {
	caminho string
	chave   string
	corpo   map[string]any
}

// provedorFalso é a OpenAI e a Anthropic de mentira, no mesmo endereço
// (os caminhos são diferentes). responder decide a resposta de cada pedido.
type provedorFalso struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	pedidos   []pedidoVisto
	responder func(p pedidoVisto, w http.ResponseWriter)
	fora      atomic.Int32
	hosts     sync.Map
}

func novoProvedorFalso(t *testing.T, responder func(p pedidoVisto, w http.ResponseWriter)) *provedorFalso {
	t.Helper()
	f := &provedorFalso{t: t, responder: responder}
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
			var hosts []string
			f.hosts.Range(func(k, _ any) bool { hosts = append(hosts, k.(string)); return true })
			t.Errorf("%d pedido(s) tentaram sair da máquina: %v", n, hosts)
		}
	})
	return f
}

// cliente é o ia.Cliente apontado para o provedor falso, só loopback.
func (f *provedorFalso) cliente() *ia.Cliente {
	return &ia.Cliente{
		BaseOpenAI:    f.srv.URL,
		BaseAnthropic: f.srv.URL,
		HTTP:          &http.Client{Transport: soLoopback{base: ia.NovoTransporteHTTP1(), fora: &f.fora, hosts: &f.hosts}},
	}
}

func (f *provedorFalso) vistos() []pedidoVisto {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pedidoVisto(nil), f.pedidos...)
}

// respostaOpenAI escreve um 200 da Responses API com o final_text dado.
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

// respostaAnthropic escreve um 200 da Messages API com o final_text dado.
func respostaAnthropic(w http.ResponseWriter, finalText string) {
	saida, _ := json.Marshal(map[string]string{"final_text": finalText})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"stop_reason": "end_turn",
		"content":     []any{map[string]any{"type": "text", "text": string(saida)}},
		"usage":       map[string]any{"input_tokens": 1157, "output_tokens": 30},
	})
}

const (
	ditadoDeTeste = "o cliente mandou o balancete de agosto"
	polidoDeTeste = "O cliente mandou o balancete de agosto."
	chaveDeTeste  = "sk-teste-chave-de-mentira-0123456789"
)

// ditar roda a metade de rede de UM ditado como o runtime a roda: abre o
// Polidor pelo adaptador e chama Reescrever com o resultado do Smart.
func ditar(t *testing.T, a *polishDoAgent, provedor, modelo, texto string) (resolver.ResolveResult, string, [2]int) {
	t.Helper()
	var tokens [2]int
	pol, esforco := a.abrir(provedor, modelo, func(e, s int) { tokens = [2]int{e, s} })
	req := resolver.ResolveRequest{ASRResult: asr.ASRResult{Text: texto, Language: "pt-BR"}}
	smart, err := a.smart.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Smart: %v", err)
	}
	return pol.Reescrever(context.Background(), req, smart), esforco, tokens
}

func adaptadorDeTeste(t *testing.T, f *provedorFalso, cofre chaves.Cofre, log *slog.Logger) *polishDoAgent {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	// O Smart sem dicionário: o que cmd/agent monta sem dicionario.json.
	return novoPolishDoAgent(resolver.NewSmartResolver(nil), cofre, f.cliente(), log)
}

// TestPolishDoAgent_AceitoSemDicionario: o caminho feliz, e o que a S4
// existe para tornar alcançável — sem dicionario.json, o resolver é
// montado com o dicionário nulo, e o Polish vai ao provedor, com a chave
// do cofre, e volta aceito, com os tokens contados.
func TestPolishDoAgent_AceitoSemDicionario(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	cofre := chaves.NovaMemoria()
	if err := cofre.Gravar("openai", chaveDeTeste); err != nil {
		t.Fatal(err)
	}
	res, esforco, tokens := ditar(t, adaptadorDeTeste(t, f, cofre, nil), "openai", "modelo-x", ditadoDeTeste)

	if !res.Reescrito || res.FinalText != polidoDeTeste || res.MotivoFallback != resolver.MotivoNenhum {
		t.Fatalf("resultado = %+v, quero o Polish aceito", res)
	}
	vistos := f.vistos()
	if len(vistos) != 1 || vistos[0].chave != chaveDeTeste || vistos[0].caminho != "/v1/responses" {
		t.Fatalf("pedidos = %+v, quero um POST /v1/responses com a chave do cofre", vistos)
	}
	if vistos[0].corpo["model"] != "modelo-x" {
		t.Errorf("modelo enviado = %v, quero o do keydown", vistos[0].corpo["model"])
	}
	if esforco != "none" || tokens != [2]int{852, 34} {
		t.Errorf("esforço = %q, tokens = %v; quero none e [852 34]", esforco, tokens)
	}
}

// TestPolishDoAgent_SemChaveNaoAbreRede: sem chave no cofre, o motivo é
// sem_chave e o provedor não recebe NADA — nem uma conexão.
func TestPolishDoAgent_SemChaveNaoAbreRede(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	for _, provedor := range []string{"openai", "anthropic"} {
		res, _, _ := ditar(t, adaptadorDeTeste(t, f, chaves.NovaMemoria(), nil), provedor, "modelo-x", ditadoDeTeste)
		if res.Reescrito || res.MotivoFallback != resolver.MotivoSemChave || res.FinalText != ditadoDeTeste {
			t.Fatalf("%s sem chave: %+v; quero o Smart com o motivo sem_chave", provedor, res)
		}
	}
	if n := len(f.vistos()); n != 0 {
		t.Fatalf("o provedor recebeu %d pedido(s) sem chave nenhuma", n)
	}
}

// cofreQueFalha é o Gerenciador que não lê: o erro dele (que poderia
// trazer qualquer coisa) vira sem_chave, e nada sai.
type cofreQueFalha struct{ chaves.Cofre }

func (cofreQueFalha) Ler(string) (string, error) {
	return "", errors.New("Gerenciador ocupado: " + chaveDeTeste)
}

func TestPolishDoAgent_ErroDoCofreViraSemChave(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	res, _, _ := ditar(t, adaptadorDeTeste(t, f, cofreQueFalha{}, nil), "openai", "m", ditadoDeTeste)
	if res.MotivoFallback != resolver.MotivoSemChave || len(f.vistos()) != 0 {
		t.Fatalf("resultado = %+v, pedidos %d; quero sem_chave e nenhum pedido", res, len(f.vistos()))
	}
	if strings.Contains(fmt.Sprint(res.Warnings), "ocupado") {
		t.Errorf("o texto do erro do cofre chegou ao resultado: %v", res.Warnings)
	}
}

// TestPolishDoAgent_ChaveLidaACadaDitado: a chave não é guardada. Trocar
// no cofre vale no ditado seguinte, e remover também (ADR-016, decisão 2).
func TestPolishDoAgent_ChaveLidaACadaDitado(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	cofre := chaves.NovaMemoria()
	a := adaptadorDeTeste(t, f, cofre, nil)

	_ = cofre.Gravar("openai", "sk-primeira-chave-1111")
	ditar(t, a, "openai", "m", ditadoDeTeste)
	_ = cofre.Gravar("openai", "sk-segunda-chave-2222")
	ditar(t, a, "openai", "m", ditadoDeTeste)
	_ = cofre.Apagar("openai")
	res, _, _ := ditar(t, a, "openai", "m", ditadoDeTeste)

	vistos := f.vistos()
	if len(vistos) != 2 || vistos[0].chave != "sk-primeira-chave-1111" || vistos[1].chave != "sk-segunda-chave-2222" {
		t.Fatalf("chaves recebidas = %+v; quero a primeira, depois a segunda, e nada depois de apagar", vistos)
	}
	if res.MotivoFallback != resolver.MotivoSemChave {
		t.Fatalf("depois de apagar a chave: %+v, quero sem_chave", res)
	}
}

// TestPolishDoAgent_ChaveDoProvedorDoKeydown: a chave lida é a do provedor
// que valia no keydown, e o pedido vai ao endereço dele.
func TestPolishDoAgent_ChaveDoProvedorDoKeydown(t *testing.T) {
	f := novoProvedorFalso(t, func(p pedidoVisto, w http.ResponseWriter) {
		if p.caminho == "/v1/messages" {
			respostaAnthropic(w, polidoDeTeste)
			return
		}
		respostaOpenAI(w, polidoDeTeste)
	})
	cofre := chaves.NovaMemoria()
	_ = cofre.Gravar("openai", "sk-da-openai-aaaa")
	_ = cofre.Gravar("anthropic", "sk-ant-da-anthropic-bbbb")
	a := adaptadorDeTeste(t, f, cofre, nil)

	res, esforco, _ := ditar(t, a, "anthropic", "modelo-a", ditadoDeTeste)
	vistos := f.vistos()
	if !res.Reescrito || len(vistos) != 1 || vistos[0].caminho != "/v1/messages" || vistos[0].chave != "sk-ant-da-anthropic-bbbb" {
		t.Fatalf("resultado %+v, pedidos %+v; quero a Anthropic, com a chave dela", res, vistos)
	}
	if _, tem := vistos[0].corpo["reasoning"]; tem || esforco != "nao_se_aplica" {
		t.Errorf("a Anthropic recebeu reasoning (%v) ou o rótulo do esforço é %q", vistos[0].corpo["reasoning"], esforco)
	}
}

// TestPolishDoAgent_ProvedorInvalidoNaoLeAChave: sem provedor escolhido, o
// Polish não tem cliente — e nem lê o cofre.
func TestPolishDoAgent_ProvedorInvalidoNaoLeAChave(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) { respostaOpenAI(w, polidoDeTeste) })
	cofre := &cofreQueConta{Cofre: chaves.NovaMemoria()}
	for _, provedor := range []string{"", "google"} {
		res, esforco, _ := ditar(t, adaptadorDeTeste(t, f, cofre, nil), provedor, "m", ditadoDeTeste)
		if res.MotivoFallback != resolver.MotivoSemCliente || esforco != "" {
			t.Fatalf("provedor %q: %+v, esforço %q; quero sem_cliente", provedor, res, esforco)
		}
	}
	if cofre.leituras.Load() != 0 || len(f.vistos()) != 0 {
		t.Fatalf("leu o cofre %d vez(es) e mandou %d pedido(s) sem provedor", cofre.leituras.Load(), len(f.vistos()))
	}
}

type cofreQueConta struct {
	chaves.Cofre
	leituras atomic.Int32
}

func (c *cofreQueConta) Ler(p string) (string, error) {
	c.leituras.Add(1)
	return c.Cofre.Ler(p)
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

// respostaIncompativel é o 400 da OpenAI para um modelo que recusa o
// esforço de raciocínio pedido.
func respostaIncompativel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprint(w, `{"error":{"type":"invalid_request_error","param":"reasoning.effort","code":"unsupported_value","message":"Unsupported value: 'reasoning.effort'."}}`)
}

// TestPolishDoAgent_PoliticaDeEsforco: nenhum nome de modelo no código.
// Um modelo que recusa o "none" recebe "minimal" no ditado seguinte, e
// depois nenhum campo; o ditado que recebeu a recusa cai no Smart; outro
// modelo começa do padrão; e a memória é por provedor e modelo.
func TestPolishDoAgent_PoliticaDeEsforco(t *testing.T) {
	// "modelo-sem-raciocinio" só aceita o pedido sem o campo; "modelo-novo"
	// aceita tudo.
	f := novoProvedorFalso(t, func(p pedidoVisto, w http.ResponseWriter) {
		if p.corpo["model"] == "modelo-sem-raciocinio" && esforcoDoPedido(p) != "sem campo" {
			respostaIncompativel(w)
			return
		}
		respostaOpenAI(w, polidoDeTeste)
	})
	cofre := chaves.NovaMemoria()
	_ = cofre.Gravar("openai", chaveDeTeste)
	var logBuf bytes.Buffer
	a := adaptadorDeTeste(t, f, cofre, slog.New(slog.NewTextHandler(&logBuf, nil)))

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
		res, rotulo, _ := ditar(t, a, "openai", p.modelo, ditadoDeTeste)
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
	// Uma linha por degrau descido, com o recusado e o próximo.
	if n := strings.Count(logBuf.String(), "recusou o esforço"); n != 2 ||
		!strings.Contains(logBuf.String(), "recusado=none proximo=minimal") ||
		!strings.Contains(logBuf.String(), "recusado=minimal proximo=omitir") {
		t.Fatalf("log da política (%d linha[s]):\n%s", n, logBuf.String())
	}

	// Nova execução, nova memória: o padrão volta.
	b := adaptadorDeTeste(t, f, cofre, nil)
	if _, rotulo, _ := ditar(t, b, "openai", "modelo-sem-raciocinio", ditadoDeTeste); rotulo != "none" {
		t.Fatalf("outra execução começou com %q; a memória é só desta execução", rotulo)
	}
}

// TestPoliticaEsforco_RecusaRepetidaNaoPulaDegrau: duas recusas do mesmo
// degrau (dois ditados em voo) descem um degrau só; a Anthropic não entra
// na política.
func TestPoliticaEsforco_RecusaRepetidaNaoPulaDegrau(t *testing.T) {
	var buf bytes.Buffer
	p := novaPoliticaEsforco(slog.New(slog.NewTextHandler(&buf, nil)))
	p.recusou("openai", "m", "")
	p.recusou("openai", "m", "")
	if got := p.atual("openai", "m"); got != "minimal" {
		t.Fatalf("depois de duas recusas do none: %q, quero minimal", got)
	}
	p.recusou("openai", "m", "minimal")
	p.recusou("openai", "m", ia.EsforcoOmitido)
	if got := p.atual("openai", "m"); got != ia.EsforcoOmitido {
		t.Fatalf("no último degrau: %q, quero %q", got, ia.EsforcoOmitido)
	}
	p.recusou("anthropic", "m", "")
	if got := p.atual("anthropic", "m"); got != "" {
		t.Fatalf("Anthropic: %q, quero vazio (não recebe esforço)", got)
	}
	// Dois degraus descidos na OpenAI, nenhum na Anthropic.
	if n := strings.Count(buf.String(), "recusou o esforço"); n != 2 || strings.Contains(buf.String(), "provedor=anthropic") {
		t.Fatalf("log da política (%d linha[s]):\n%s", n, buf.String())
	}
}

// TestChave_NuncaSaiDoPolishDoAgent estende o TestChave_NuncaSaiDoCofre
// (localapi) ao POST do Polish: a chave sai do cofre, vai ao cabeçalho e
// volta ecoada pelo provedor no corpo e num cabeçalho dos erros, e dentro
// do próprio final_text. Nada do que o adaptador devolve — resultado,
// avisos, motivo, rótulo — nem o log dele e o log padrão do processo pode
// ter um pedaço dela.
func TestChave_NuncaSaiDoPolishDoAgent(t *testing.T) {
	const chave = "sk-proj-QwErTyVaZaMeNtOzZ-XyXyKpKpWvWv-kLmNoPqR-fImJ"
	var logPadrao bytes.Buffer
	antesSlog, antesLog := slog.Default(), log.Writer()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logPadrao, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&logPadrao)
	t.Cleanup(func() { slog.SetDefault(antesSlog); log.SetOutput(antesLog) })

	var n atomic.Int32
	f := novoProvedorFalso(t, func(p pedidoVisto, w http.ResponseWriter) {
		w.Header().Set("X-Eco", p.chave)
		switch n.Add(1) {
		case 1: // a chave dentro do texto "polido": a guarda recusa
			respostaOpenAI(w, "O cliente mandou o balancete de agosto "+p.chave)
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
	cofre := chaves.NovaMemoria()
	_ = cofre.Gravar("openai", chave)
	var logAdaptador bytes.Buffer
	a := adaptadorDeTeste(t, f, cofre, slog.New(slog.NewTextHandler(&logAdaptador, &slog.HandlerOptions{Level: slog.LevelDebug})))

	var tudo strings.Builder
	for i := 0; i < 4; i++ {
		res, rotulo, tokens := ditar(t, a, "openai", "modelo-x", ditadoDeTeste)
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
		"resultado":           tudo.String(),
		"log do adaptador":    logAdaptador.String(),
		"log padrão":          logPadrao.String(),
		"o próprio adaptador": fmt.Sprintf("%+v %+v", a, a.esforco),
	} {
		for i := 0; i+8 <= len(chave); i++ {
			if strings.Contains(texto, chave[i:i+8]) {
				t.Errorf("pedaço da chave (%q) em %s", chave[i:i+8], onde)
				break
			}
		}
	}
}

// TestAbrirDicionario_SemArquivoMontaOSmart: sem dicionario.json, o Agent
// sobe com o Smart de dicionário NULO (ADR-016, decisão 13) — não um
// repositório vazio, cuja busca exige usuário e faria o Polish cair sempre
// com o motivo dicionario —, e o log diz o que vale.
func TestAbrirDicionario_SemArquivoMontaOSmart(t *testing.T) {
	var buf bytes.Buffer
	smart, vocab, usuario, dominio, err := abrirDicionarioEm(slog.New(slog.NewTextHandler(&buf, nil)),
		filepath.Join(t.TempDir(), nomeDicionario))
	if err != nil || smart == nil {
		t.Fatalf("sem arquivo: smart = %v, err = %v; quero o Smart montado e nenhum erro", smart, err)
	}
	if smart.Dictionary != nil || vocab != "" || usuario != "" || dominio != nil {
		t.Fatalf("sem arquivo: dicionário %v, vocabulário %q, usuário %q, domínio %v", smart.Dictionary, vocab, usuario, dominio != nil)
	}
	if !strings.Contains(buf.String(), "nenhum encontrado") || !strings.Contains(buf.String(), "autocorreção") {
		t.Fatalf("o log da subida não diz o que vale:\n%s", buf.String())
	}
	// O Smart roda (autocorreção incluída): não é mais o texto intocado.
	out, err := smart.Resolve(context.Background(), resolver.ResolveRequest{ASRResult: asr.ASRResult{Text: "manda  para o João"}})
	if err != nil || out.FinalText != "manda para o João" {
		t.Fatalf("Smart sem dicionário = %q, %v", out.FinalText, err)
	}
}

// TestAbrirDicionario_ComArquivoMontaOSmartComORepositorio: o caminho de
// sempre continua: o arquivo presente vira o repositório do Smart.
func TestAbrirDicionario_ComArquivoMontaOSmartComORepositorio(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), nomeDicionario)
	if err := os.WriteFile(caminho, []byte(`{"usuario":"mauro","entradas":[{"canonica":"DCTFWeb","prioridade":100}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	smart, vocab, usuario, _, err := abrirDicionarioEm(slog.New(slog.DiscardHandler), caminho)
	if err != nil || smart == nil || smart.Dictionary == nil || usuario != "mauro" || !strings.Contains(vocab, "DCTFWeb") {
		t.Fatalf("com arquivo: smart %v, usuário %q, vocabulário %q, err %v", smart, usuario, vocab, err)
	}
	// Arquivo presente mas inválido continua sendo erro.
	if err := os.WriteFile(caminho, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := abrirDicionarioEm(slog.New(slog.DiscardHandler), caminho); err == nil {
		t.Fatal("dicionario.json inválido subiu sem erro")
	}
}

// TestUltimoPolishParaATela: o que o runtime guarda do último Polish chega
// à API local com o motivo, o provedor do keydown e a hora; sem nenhum
// Polish ainda, a tela não recebe nada.
func TestUltimoPolishParaATela(t *testing.T) {
	var estado winruntime.EstadoPolish
	ler := ultimoPolishParaATela(func() winruntime.EstadoPolish { return estado })

	if d, ok := ler(); ok || d != (localapi.DesfechoDoPolish{}) {
		t.Fatalf("sem Polish ainda: %+v, %v; quero vazio e false", d, ok)
	}

	quando := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	estado = winruntime.EstadoPolish{Motivo: resolver.MotivoSemCredito, Provedor: "openai", Quando: quando}
	d, ok := ler()
	if !ok || d != (localapi.DesfechoDoPolish{Motivo: "sem_credito", Provedor: "openai", Quando: quando}) {
		t.Fatalf("com motivo: %+v, %v", d, ok)
	}

	// Polish aceito: motivo vazio, mas houve Polish — a tela limpa a linha.
	estado = winruntime.EstadoPolish{Provedor: "openai", Quando: quando}
	if d, ok := ler(); !ok || d.Motivo != "" || d.Provedor != "openai" {
		t.Fatalf("aceito: %+v, %v", d, ok)
	}
}
