package local

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Estes testes cobrem o caminho secreto do whisper-server (process.go, "O
// caminho secreto de cada subida") e a trava dos argumentos que exporiam o
// ditado. São puros: nenhum processo sobe, e o "servidor" é um httptest.

// caminhoDeTeste tem a forma de um caminho secreto (barra e 32 dígitos
// hexadecimais) sem ser um de verdade.
const caminhoDeTeste = "/c0ffee00c0ffee00c0ffee00c0ffee00"

// pedacosDoCaminho são todos os trechos de 8 dígitos do caminho: achar
// qualquer um deles num texto é achar parte do segredo, mesmo que cortada.
func pedacosDoCaminho(caminho string) []string {
	hexa := strings.TrimPrefix(caminho, "/")
	var p []string
	for i := 0; i+8 <= len(hexa); i++ {
		p = append(p, hexa[i:i+8])
	}
	return p
}

func semPedacoDoCaminho(t *testing.T, onde, texto, caminho string) {
	t.Helper()
	for _, p := range pedacosDoCaminho(caminho) {
		if strings.Contains(texto, p) {
			t.Errorf("%s traz um pedaço do caminho secreto (%q):\n%s", onde, p, texto)
			return
		}
	}
}

// valorDoArgumento devolve o que vem depois de flag no argv, e se veio.
func valorDoArgumento(args []string, flag string) (string, bool) {
	i := indice(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

// ─── O caminho: 128 bits, novo a cada subida ───

func TestNovoCaminhoSecreto_128BitsENovoACadaChamada(t *testing.T) {
	vistos := map[string]bool{}
	for i := 0; i < 64; i++ {
		c := novoCaminhoSecreto()
		if len(c) != 33 || c[0] != '/' {
			t.Fatalf("caminho = %q, quero uma barra e 32 dígitos hexadecimais", c)
		}
		b, err := hex.DecodeString(c[1:])
		if err != nil || len(b) != 16 {
			t.Fatalf("caminho = %q não é hexadecimal de 16 bytes (128 bits): %v", c, err)
		}
		if strings.ToLower(c) != c {
			t.Errorf("caminho = %q: quero hexadecimal minúsculo, como o servidor o recebe", c)
		}
		if vistos[c] {
			t.Fatalf("o caminho %q saiu duas vezes em 64 chamadas: não é aleatório", c)
		}
		vistos[c] = true
	}
}

// TestNovaSubida_OComandoEOEnderecoLevamOMesmoCaminhoNovo: o servidor sobe com
// um caminho e o Agent fala com ele pelo MESMO; a subida seguinte, com outro.
func TestNovaSubida_OComandoEOEnderecoLevamOMesmoCaminhoNovo(t *testing.T) {
	cfg := Config{ServerPath: "whisper-server.exe", ModelPath: "m.bin", Language: "pt"}

	cmd1, end1 := novaSubida(cfg, "127.0.0.1", 53124)
	got, ok := valorDoArgumento(cmd1.Args, "--request-path")
	if !ok {
		t.Fatalf("argv = %q: falta o --request-path", cmd1.Args)
	}
	if got != end1.caminho {
		t.Errorf("--request-path = %q, mas o endereço fala com %q", got, end1.caminho)
	}
	if len(end1.caminho) != 33 {
		t.Errorf("caminho = %q, quero 33 caracteres (barra e 128 bits em hexadecimal)", end1.caminho)
	}
	if end1.base != "http://127.0.0.1:53124" {
		t.Errorf("base = %q, quero http://127.0.0.1:53124", end1.base)
	}
	if u := end1.url("/inference"); u != "http://127.0.0.1:53124"+end1.caminho+"/inference" {
		t.Errorf("url(/inference) = %q: o pedido não leva o caminho", u)
	}

	cmd2, end2 := novaSubida(cfg, "127.0.0.1", 53124)
	if end2.caminho == end1.caminho {
		t.Errorf("duas subidas com o mesmo caminho %q: tem de ser novo a cada uma", end1.caminho)
	}
	if got2, _ := valorDoArgumento(cmd2.Args, "--request-path"); got2 != end2.caminho {
		t.Errorf("segunda subida: --request-path = %q, endereço = %q", got2, end2.caminho)
	}
}

// TestServerArgs_CaminhoSecretoPorUltimo: o caminho vai no argv, e no fim. O
// whisper-server imprime o texto de uso (com o que já leu do argv) quando
// encontra um argumento desconhecido; por último, o caminho ainda está vazio
// nesse texto.
func TestServerArgs_CaminhoSecretoPorUltimo(t *testing.T) {
	for _, cfg := range []Config{
		{ModelPath: "m.bin"},
		{ModelPath: "m.bin", Language: "pt", Threads: 12, AudioCtx: 750, SemFlashAttention: true},
	} {
		args := serverArgs(cfg, "127.0.0.1", 1, caminhoDeTeste)
		n := len(args)
		if n < 2 || args[n-2] != "--request-path" || args[n-1] != caminhoDeTeste {
			t.Errorf("argv = %q: quero --request-path %s no fim", args, caminhoDeTeste)
		}
		if c := strings.Count(strings.Join(args, " "), "--request-path"); c != 1 {
			t.Errorf("argv = %q: --request-path %d vezes", args, c)
		}
	}
}

// ─── A trava: nada que escreva o ditado no log ───

// TestServerArgs_NuncaLevaFlagQueExpoeODitado: com -pr, o whisper-server
// escreve o texto de cada ditado no whisper-server.log (provado na auditoria:
// sem ele, nenhuma linha de texto no log). -debug despeja dados do áudio, e
// --convert grava o áudio num arquivo temporário e chama o ffmpeg. Nenhuma
// Config, nem a do Agent nem a do benchmark nem uma com tudo ligado, pode
// produzir um deles.
func TestServerArgs_NuncaLevaFlagQueExpoeODitado(t *testing.T) {
	proibidas := []string{"-pr", "--print-realtime", "-debug", "--debug-mode", "--convert"}
	prog := pastaDoPrograma(t)
	configs := map[string]Config{
		"vazia": {},
		"a do Agent": {
			ServerPath: prog + `\whisper-server.exe`, Dir: prog, ModelPath: prog + `\ggml-small-q4_0.bin`,
			Language: "pt", Threads: 12, Janela: JanelaRecomendada, SemFlashAttention: true,
		},
		"a do benchmark": {ServerPath: "s", ModelPath: "m", Language: "pt", Threads: 12, AudioCtx: 750},
		"tudo ligado": {
			ServerPath: "s", ModelPath: "m", Dir: "d", Language: "pt", Threads: 4, AudioCtx: 750,
			Janela: JanelaRecomendada, Decodificacao: Decodificacao{EntropyThold: 2.8, TemperatureInc: 0.4},
			SemFlashAttention: true, ReadyTimeout: 1, SaidaDoServidor: &bytes.Buffer{},
		},
	}
	for nome, cfg := range configs {
		args := serverArgs(cfg, "127.0.0.1", 53124, caminhoDeTeste)
		for _, a := range args {
			for _, p := range proibidas {
				if a == p || strings.HasPrefix(a, p+"=") {
					t.Errorf("Config %s: o argv leva %q, que expõe o ditado: %q", nome, p, args)
				}
			}
		}
	}
}

// ─── O endereço formatado não mostra o caminho ───

func TestEndereco_FormatadoNaoMostraOCaminho(t *testing.T) {
	end := endereco{base: "http://127.0.0.1:53124", caminho: caminhoDeTeste}
	for _, verbo := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		texto := fmt.Sprintf(verbo, end)
		semPedacoDoCaminho(t, "o endereço com "+verbo, texto, caminhoDeTeste)
		if !strings.Contains(texto, "127.0.0.1:53124") {
			t.Errorf("%s = %q: a base tem de aparecer (é o que ajuda a ler o log)", verbo, texto)
		}
	}
}

func TestEndereco_SemCaminho(t *testing.T) {
	end := endereco{base: "http://127.0.0.1:1", caminho: caminhoDeTeste}
	got := end.semCaminho("File Not Found (" + caminhoDeTeste + "/inference)")
	if got != "File Not Found (/<caminho>/inference)" {
		t.Errorf("semCaminho = %q", got)
	}
	// Sem caminho (um dublê), o texto passa igual: trocar "" inseriria o
	// rótulo entre cada letra.
	if got := (endereco{base: "x"}).semCaminho("abc"); got != "abc" {
		t.Errorf("semCaminho sem caminho = %q, quero o texto igual", got)
	}
}

// ─── Os pedidos usam o caminho ───

// servidorComCaminho responde a transcrição só em caminho+"/inference"; em
// qualquer outra rota faz o que o whisper-server faz, 404 com o caminho pedido
// no corpo. Anota cada caminho que chegou.
func servidorComCaminho(t *testing.T, caminho, texto string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var chegaram []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		mu.Lock()
		chegaram = append(chegaram, r.URL.Path)
		mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != caminho+"/inference" {
			http.Error(w, "File Not Found ("+r.URL.Path+")", http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"text":%q}`, texto)
	}))
	t.Cleanup(s.Close)
	return s, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), chegaram...)
	}
}

func TestTranscribe_PedidoUsaOCaminhoSecreto(t *testing.T) {
	srv, chegaram := servidorComCaminho(t, caminhoDeTeste, "pelo caminho certo")
	p := providerCom(t, &procFalso{baseURL: srv.URL, caminho: caminhoDeTeste})

	res, err := p.Transcribe(context.Background(), audioDeTeste(16000), asr.ASROptions{Vocabulary: "DCTFWeb"})
	if err != nil {
		t.Fatalf("Transcribe: %v (chegaram %q)", err, chegaram())
	}
	if res.Text != "pelo caminho certo" {
		t.Errorf("Text = %q", res.Text)
	}
	if c := chegaram(); len(c) != 1 || c[0] != caminhoDeTeste+"/inference" {
		t.Errorf("o servidor recebeu %q, quero só %s/inference", c, caminhoDeTeste)
	}
}

// TestTranscribe_ReenvioUsaOCaminhoDaNovaSubida: o servidor morre no meio do
// pedido e sobe de novo, com OUTRO caminho; o reenvio vai pelo novo.
func TestTranscribe_ReenvioUsaOCaminhoDaNovaSubida(t *testing.T) {
	const segundo = "/0ddba11500ddba11500ddba11500ddba"
	proc := &procFalsoComCaminhos{caminhos: []string{caminhoDeTeste, segundo}}
	var mu sync.Mutex
	var chegaram []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		mu.Lock()
		chegaram = append(chegaram, r.URL.Path)
		primeira := len(chegaram) == 1
		mu.Unlock()
		if primeira {
			proc.vivo.Store(false)
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		fmt.Fprint(w, `{"text":"voltou"}`)
	}))
	t.Cleanup(srv.Close)
	proc.baseURL = srv.URL
	p := providerCom(t, proc)

	if _, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(chegaram) != 2 || chegaram[0] != caminhoDeTeste+"/inference" || chegaram[1] != segundo+"/inference" {
		t.Errorf("pedidos em %q; quero o primeiro pelo caminho da 1a subida e o reenvio pelo da 2a", chegaram)
	}
}

// procFalsoComCaminhos é o procFalso que devolve um caminho diferente a cada
// subida, como o servidor de verdade.
type procFalsoComCaminhos struct {
	procFalso
	mu       sync.Mutex
	caminhos []string
}

func (f *procFalsoComCaminhos) Start(ctx context.Context) (endereco, error) {
	f.mu.Lock()
	f.caminho, f.caminhos = f.caminhos[0], f.caminhos[1:]
	f.mu.Unlock()
	return f.procFalso.Start(ctx)
}

// ─── O caminho nunca vai a erro nem a log ───

// TestTranscribe_CaminhoNuncaVaiParaOErroNemParaOLog passa o pedido por cada
// jeito de falhar (o servidor que ecoa o caminho no corpo, como o 404 do
// whisper-server; o eco longo, cortado no meio do caminho; a conexão recusada;
// a conexão cortada; o contexto cancelado; a URL que não se monta) e procura o
// caminho, ou qualquer pedaço de 8 dígitos dele, no erro (Error, %v, %+v, %#v),
// no log padrão do processo e no slog. O Agent registra esses erros no
// agent.log.
func TestTranscribe_CaminhoNuncaVaiParaOErroNemParaOLog(t *testing.T) {
	var logPadrao, logSlog bytes.Buffer
	antesLog, antesFlags, antesSlog := log.Writer(), log.Flags(), slog.Default()
	log.SetOutput(&logPadrao)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logSlog, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(antesLog)
		log.SetFlags(antesFlags)
		slog.SetDefault(antesSlog)
	})

	eco := func(status int, antes string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseMultipartForm(32 << 20)
			w.WriteHeader(status)
			fmt.Fprint(w, antes+"File Not Found ("+r.URL.Path+")")
		}
	}
	fechado := httptest.NewServer(http.NotFoundHandler())
	baseFechada := fechado.URL
	fechado.Close()

	casos := []struct {
		nome    string
		handler http.HandlerFunc // nil: usa base
		base    string
		ctx     func() context.Context
		querErr error
	}{
		{nome: "404 com o caminho no corpo", handler: eco(http.StatusNotFound, "")},
		// 174 bytes e o "File Not Found (" antes: o caminho começa no byte 190,
		// e o corte de 200 bytes (primeirosBytes) cai no meio dele.
		{nome: "eco longo cortado no meio do caminho", handler: eco(http.StatusRequestEntityTooLarge, strings.Repeat("x", 174))},
		{nome: "conexão recusada", base: baseFechada},
		{nome: "conexão cortada", handler: func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseMultipartForm(32 << 20)
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		}},
		{nome: "contexto cancelado", handler: eco(http.StatusOK, ""), ctx: func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, querErr: context.Canceled},
		{nome: "URL que não se monta", base: "http://127.0.0.1:%zz"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			base := c.base
			if c.handler != nil {
				srv := httptest.NewServer(c.handler)
				t.Cleanup(srv.Close)
				base = srv.URL
			}
			ctx := context.Background()
			if c.ctx != nil {
				ctx = c.ctx()
			}
			p := providerCom(t, &procFalso{baseURL: base, caminho: caminhoDeTeste})
			_, err := p.Transcribe(ctx, audioDeTeste(8000), asr.ASROptions{})
			if err == nil {
				t.Fatal("Transcribe = nil, quero erro")
			}
			if c.querErr != nil && !errors.Is(err, c.querErr) {
				t.Errorf("err = %v, quero que embrulhe %v (errors.Is tem de continuar achando a causa)", err, c.querErr)
			}
			for _, verbo := range []string{"%v", "%+v", "%#v", "%s"} {
				semPedacoDoCaminho(t, "o erro com "+verbo, fmt.Sprintf(verbo, err), caminhoDeTeste)
			}
		})
	}
	semPedacoDoCaminho(t, "o log padrão", logPadrao.String(), caminhoDeTeste)
	semPedacoDoCaminho(t, "o slog", logSlog.String(), caminhoDeTeste)
}

// TestTranscribe_ErroDoServidorAindaDizOQueAconteceu: tirar o caminho não
// pode tirar a pista. O status e o resto do corpo continuam no erro.
func TestTranscribe_ErroDoServidorAindaDizOQueAconteceu(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		http.Error(w, "File Not Found ("+r.URL.Path+")", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	p := providerCom(t, &procFalso{baseURL: srv.URL, caminho: caminhoDeTeste})
	_, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
	if err == nil {
		t.Fatal("Transcribe = nil, quero erro")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "File Not Found (/<caminho>/inference)") {
		t.Errorf("err = %v: quero o status e o corpo, com o caminho trocado pelo rótulo", err)
	}
}
