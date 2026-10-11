package local

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// procFalso é o dublê do servidor: permite testar subida, morte e reinício
// sem binário, sem modelo e sem Windows.
type procFalso struct {
	baseURL   string
	falhasAte int32 // quantas chamadas a Start ainda falham
	starts    int32
	stops     int32
	vivo      atomic.Bool
}

func (f *procFalso) Start(ctx context.Context) (string, error) {
	n := atomic.AddInt32(&f.starts, 1)
	if n <= atomic.LoadInt32(&f.falhasAte) {
		return "", fmt.Errorf("falha simulada de subida %d", n)
	}
	f.vivo.Store(true)
	return f.baseURL, nil
}

func (f *procFalso) Alive() bool { return f.vivo.Load() }

func (f *procFalso) Stop() error {
	atomic.AddInt32(&f.stops, 1)
	f.vivo.Store(false)
	return nil
}

// servidorFalso responde /inference com o texto dado.
func servidorFalso(t *testing.T, texto string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ler o corpo INTEIRO antes de responder: o contrário não é
		// full-duplex em HTTP/1.1 e trunca o corpo (achado registrado em
		// docs/benchmarks/transporte-2026-09-17.md).
		_ = r.ParseMultipartForm(32 << 20)
		fmt.Fprintf(w, `{"text":%q}`, texto)
	}))
	t.Cleanup(s.Close)
	return s
}

func audioDeTeste(n int) asr.AudioBuffer {
	pcm := make([]int16, n)
	for i := range pcm {
		pcm[i] = int16(i % 1000)
	}
	return asr.AudioBuffer{PCM: pcm, SampleRateHz: 16000}
}

func providerCom(t *testing.T, proc serverProcess) *Provider {
	t.Helper()
	p := New(Config{ServerPath: "x", ModelPath: "y", Language: "pt"}, "small-q4_0")
	p.novoProcesso = func(Config) serverProcess { return proc }
	return p
}

func TestTranscribe_CaminhoFeliz(t *testing.T) {
	srv := servidorFalso(t, " Já mandei o arquivo. ")
	p := providerCom(t, &procFalso{baseURL: srv.URL})

	res, err := p.Transcribe(context.Background(), audioDeTeste(16000), asr.ASROptions{Language: "pt-BR"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "Já mandei o arquivo." {
		t.Errorf("Text = %q, want %q (espaços das bordas têm de sair)", res.Text, "Já mandei o arquivo.")
	}
	if res.Model != "small-q4_0" {
		t.Errorf("Model = %q", res.Model)
	}
	if res.ProcessingTime <= 0 {
		t.Error("ProcessingTime não foi preenchido")
	}
}

// TestTranscribe_NuncaFabricaConfidence trava a regra do
// 16-ai-behavior-spec.md: com Capabilities().Confidence == false, o
// resultado NÃO pode trazer confidence.
//
// Importa porque o Engine usa confidence para decidir fallback para a
// nuvem. Um valor fabricado aqui faria o Engine decidir com um número que
// ninguém mediu.
func TestTranscribe_NuncaFabricaConfidence(t *testing.T) {
	srv := servidorFalso(t, "qualquer coisa")
	p := providerCom(t, &procFalso{baseURL: srv.URL})

	if p.Capabilities().Confidence {
		t.Fatal("Capabilities().Confidence = true, mas o whisper-server não devolve confidence")
	}
	res, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Confidence.Present {
		t.Errorf("Confidence.Present = true (valor %v) — provider sem confidence não pode inventar um",
			res.Confidence.Value)
	}
}

// TestTranscribe_TranscricaoVaziaEhErro: texto vazio não pode passar por
// transcrição. Seguiria como "a pessoa não disse nada", o histórico
// registraria o ditado como malsucedido, e a causa real — servidor com
// problema, áudio truncado — ficaria invisível.
func TestTranscribe_TranscricaoVaziaEhErro(t *testing.T) {
	for _, texto := range []string{"", "   ", "\n\t "} {
		srv := servidorFalso(t, texto)
		p := providerCom(t, &procFalso{baseURL: srv.URL})

		res, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
		if err == nil {
			t.Fatalf("Transcribe(texto=%q) = (%+v, nil), want erro", texto, res)
		}
	}
}

// TestTranscribe_ServidorMorto_ReiniciaERefaz é o comportamento que
// justifica o processo residente ter ciclo de vida: o servidor morre no
// meio da sessão, e o ditado se recupera sozinho em vez de ser perdido.
func TestTranscribe_ServidorMorto_ReiniciaERefaz(t *testing.T) {
	srv := servidorFalso(t, "recuperado")
	proc := &procFalso{baseURL: srv.URL}
	p := providerCom(t, proc)

	if err := p.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	proc.vivo.Store(false) // o servidor morreu entre um ditado e outro

	res, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe depois da morte: %v", err)
	}
	if res.Text != "recuperado" {
		t.Errorf("Text = %q", res.Text)
	}
	if got := atomic.LoadInt32(&proc.starts); got < 2 {
		t.Errorf("starts = %d, want >= 2 — o servidor tinha de ter sido recolocado de pé", got)
	}
}

// TestTranscribe_ServidorNaoSobe_DesisteComErroClaro: insistir para sempre
// transformaria cada ditado numa espera longa terminando em erro. Falhar
// rápido e dizer o motivo é melhor para quem usa.
func TestTranscribe_ServidorNaoSobe_DesisteComErroClaro(t *testing.T) {
	proc := &procFalso{falhasAte: 99}
	p := providerCom(t, proc)

	_, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
	if err == nil {
		t.Fatal("Transcribe = nil, want erro")
	}
	if !errors.Is(err, ErrServerDead) {
		t.Errorf("err = %v, want que embrulhe ErrServerDead", err)
	}
	if got := atomic.LoadInt32(&proc.starts); got != maxReinicios {
		t.Errorf("starts = %d, want %d — nem menos (desistiu cedo) nem mais (insistiu além do limite)", got, maxReinicios)
	}
}

// TestTranscribe_NuncaLiberaOBuffer: liberar aqui quebraria a policy
// local_then_cloud, que reusa o MESMO buffer na chamada seguinte
// (asr.AudioBuffer, TEST-010).
func TestTranscribe_NuncaLiberaOBuffer(t *testing.T) {
	srv := servidorFalso(t, "texto")
	p := providerCom(t, &procFalso{baseURL: srv.URL})

	liberado := false
	buf := audioDeTeste(8000)
	buf.Release = func() { liberado = true }

	if _, err := p.Transcribe(context.Background(), buf, asr.ASROptions{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if liberado {
		t.Error("o provider chamou Release — só o dono do buffer pode, e depois de todos os providers")
	}
}

// TestTranscribe_CorpoTruncadoEhErro: um corpo cortado no meio não pode
// virar transcrição pela metade. Foi o defeito que a medição de transporte
// do Sprint 3 encontrou do lado servidor.
func TestTranscribe_CorpoTruncadoEhErro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		w.Header().Set("Content-Length", "5000")
		fmt.Fprint(w, `{"text":"pela met`) // JSON cortado
	}))
	defer srv.Close()

	p := providerCom(t, &procFalso{baseURL: srv.URL})
	res, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
	if err == nil {
		t.Fatalf("Transcribe = (%+v, nil), want erro — corpo truncado virou transcrição", res)
	}
	if strings.Contains(res.Text, "pela met") {
		t.Errorf("devolveu texto parcial: %q", res.Text)
	}
}

func TestTranscribe_ErroHTTPEhErro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "modelo não carregado", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := providerCom(t, &procFalso{baseURL: srv.URL})
	if res, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{}); err == nil {
		t.Fatalf("Transcribe = (%+v, nil), want erro para HTTP 500", res)
	}
}

func TestClose_EncerraOServidor(t *testing.T) {
	srv := servidorFalso(t, "texto")
	proc := &procFalso{baseURL: srv.URL}
	p := providerCom(t, proc)

	if err := p.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if atomic.LoadInt32(&proc.stops) == 0 {
		t.Error("Close não encerrou o servidor — ele ficaria de pé segurando a memória do modelo")
	}
	if proc.Alive() {
		t.Error("o servidor continua vivo depois de Close")
	}
}

func TestEncodeWAV_CabecalhoEAmostras(t *testing.T) {
	pcm := []int16{0, 1, -1, 32767, -32768, 100}
	b, err := encodeWAV(pcm, 16000)
	if err != nil {
		t.Fatalf("encodeWAV: %v", err)
	}
	if len(b) != 44+len(pcm)*2 {
		t.Fatalf("len = %d, want %d", len(b), 44+len(pcm)*2)
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[36:40]) != "data" {
		t.Fatalf("cabeçalho inválido: %q %q %q", b[0:4], b[8:12], b[36:40])
	}
	if got := binary.LittleEndian.Uint32(b[24:]); got != 16000 {
		t.Errorf("sample rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint16(b[22:]); got != 1 {
		t.Errorf("canais = %d, want 1 (mono)", got)
	}
	if got := binary.LittleEndian.Uint16(b[34:]); got != 16 {
		t.Errorf("bits = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(b[40:]); got != uint32(len(pcm)*2) {
		t.Errorf("tamanho do chunk data = %d, want %d", got, len(pcm)*2)
	}
	// As amostras precisam voltar EXATAS, inclusive os extremos.
	for i, want := range pcm {
		if got := int16(binary.LittleEndian.Uint16(b[44+i*2:])); got != want {
			t.Errorf("amostra %d = %d, want %d", i, got, want)
		}
	}
}

func TestEncodeWAV_SampleRateInvalido(t *testing.T) {
	for _, rate := range []int{0, -1} {
		if _, err := encodeWAV([]int16{1, 2}, rate); err == nil {
			t.Errorf("encodeWAV(rate=%d) = nil, want erro", rate)
		}
	}
}

func TestServerArgs_RepassaDesempenhoEFicaEmLoopback(t *testing.T) {
	cfg := Config{ModelPath: "models/quant/small-q4_0.bin", Language: "pt", Threads: 12, AudioCtx: 750}
	linha := strings.Join(serverArgs(cfg, "127.0.0.1", 53124), " ")

	for _, esperado := range []string{"--host 127.0.0.1", "--port 53124", "-m models/quant/small-q4_0.bin", "-l pt", "-t 12", "-ac 750"} {
		if !strings.Contains(linha, esperado) {
			t.Errorf("argv = %q, falta %q", linha, esperado)
		}
	}
	if strings.Contains(linha, "0.0.0.0") {
		t.Errorf("argv = %q — bind fora de loopback exporia a transcrição para a rede", linha)
	}
}

// TestServerArgs_OmiteDesempenhoQuandoZero: passar "-t 0" faria o servidor
// rodar com zero threads, não com o default dele.
func TestServerArgs_OmiteDesempenhoQuandoZero(t *testing.T) {
	linha := strings.Join(serverArgs(Config{ModelPath: "m.bin"}, "127.0.0.1", 1), " ")
	for _, proibido := range []string{"-t ", "-ac ", "-l "} {
		if strings.Contains(linha, proibido) {
			t.Errorf("argv = %q, não devia trazer %q com valor zero/vazio", linha, proibido)
		}
	}
}

func TestBackoff_CresceESatura(t *testing.T) {
	if got := backoffPara(0); got != backoffBase {
		t.Errorf("backoffPara(0) = %v, want %v", got, backoffBase)
	}
	anterior := backoffPara(0)
	for n := 1; n < 10; n++ {
		d := backoffPara(n)
		if d < anterior {
			t.Errorf("backoffPara(%d) = %v, menor que o anterior %v — não pode diminuir", n, d, anterior)
		}
		if d > backoffMax {
			t.Errorf("backoffPara(%d) = %v, passou de backoffMax %v", n, d, backoffMax)
		}
		anterior = d
	}
	if backoffPara(50) != backoffMax {
		t.Errorf("backoffPara(50) = %v, want saturar em %v", backoffPara(50), backoffMax)
	}
}

func TestReadyTimeout_ZeroUsaDefault(t *testing.T) {
	if got := (Config{}).readyTimeout(); got != DefaultReadyTimeout {
		t.Errorf("readyTimeout() = %v, want %v", got, DefaultReadyTimeout)
	}
	if got := (Config{ReadyTimeout: 3 * time.Second}).readyTimeout(); got != 3*time.Second {
		t.Errorf("readyTimeout() = %v, want 3s", got)
	}
}

// TestTranscribe_ServidorMorreNoMeioDaRequisicao cobre a corrida que o
// outro teste de morte NÃO alcança.
//
// Lá o servidor já está morto quando Transcribe começa, então quem
// recupera é a verificação do início. Aqui ele está vivo na verificação e
// morre DURANTE a requisição — a janela real, porque entre olhar e pedir
// existe tempo.
//
// Sem a segunda tentativa, esse ditado seria perdido com erro de rede, e
// a pessoa perderia a fala por um processo que já voltou.
func TestTranscribe_ServidorMorreNoMeioDaRequisicao(t *testing.T) {
	proc := &procFalso{}
	var chamadas int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		if atomic.AddInt32(&chamadas, 1) == 1 {
			// Morre no meio: derruba a conexão e marca o processo como
			// morto, como aconteceria de verdade.
			proc.vivo.Store(false)
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("httptest não suporta Hijacker")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("Hijack: %v", err)
				return
			}
			conn.Close()
			return
		}
		fmt.Fprint(w, `{"text":"voltou depois de morrer"}`)
	}))
	defer srv.Close()

	proc.baseURL = srv.URL
	p := providerCom(t, proc)

	if err := p.Warmup(context.Background()); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if !proc.Alive() {
		t.Fatal("o processo tinha de estar vivo antes da chamada — o teste não testaria a janela certa")
	}

	res, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v — o provider tinha de ter reiniciado e refeito", err)
	}
	if res.Text != "voltou depois de morrer" {
		t.Errorf("Text = %q", res.Text)
	}
	if got := atomic.LoadInt32(&chamadas); got != 2 {
		t.Errorf("chamadas ao servidor = %d, want 2 (a que morreu e a refeita)", got)
	}
	if got := atomic.LoadInt32(&proc.starts); got < 2 {
		t.Errorf("starts = %d, want >= 2 — o servidor tinha de ter sido recolocado de pé", got)
	}
}

// TestThreadsPara_ReproduzOOtimoMedidoEDegradaSemEstourar.
//
// O caso de 20 núcleos não é hipotético: é a máquina onde a varredura
// rodou, e 12 foi o melhor ponto medido (876 ms, contra 890 ms com 16 e
// 1120 ms com 4). Se alguém mexer na regra, este teste diz que o valor
// medido deixou de ser reproduzido.
func TestThreadsPara_ReproduzOOtimoMedidoEDegradaSemEstourar(t *testing.T) {
	casos := []struct{ nucleos, want int }{
		{20, 12}, // a máquina da medição
		{24, 12}, // acima do teto, satura
		{12, 12},
		{8, 8},
		{4, 4},
		{2, 2},
		{1, 1},
		{0, 1},  // contagem inválida não pode virar zero threads
		{-3, 1}, // idem
	}
	for _, c := range casos {
		if got := threadsPara(c.nucleos); got != c.want {
			t.Errorf("threadsPara(%d) = %d, want %d", c.nucleos, got, c.want)
		}
	}
}

// TestThreadsPara_NuncaPedeMaisQueOsNucleos: pedir mais threads que
// núcleos não acelera, cria disputa por núcleo que não existe.
func TestThreadsPara_NuncaPedeMaisQueOsNucleos(t *testing.T) {
	for n := 1; n <= 40; n++ {
		got := threadsPara(n)
		if got > n {
			t.Errorf("threadsPara(%d) = %d, pediu mais threads que núcleos", n, got)
		}
		if got < 1 {
			t.Errorf("threadsPara(%d) = %d, want pelo menos 1", n, got)
		}
	}
}

func TestThreadsRecomendadas_UsaAMaquinaDeVerdade(t *testing.T) {
	got := ThreadsRecomendadas()
	if got < 1 || got > tetoDeThreads {
		t.Errorf("ThreadsRecomendadas() = %d, fora da faixa [1, %d]", got, tetoDeThreads)
	}
	t.Logf("nesta máquina: %d núcleos lógicos -> %d threads", runtime.NumCPU(), got)
}

// servidorQueCapturaCampos devolve um servidor falso que guarda os
// campos de formulário recebidos, para os testes de vocabulário poderem
// asseverar o que de fato foi PARA A REDE — e não só que o código
// compila.
func servidorQueCapturaCampos(t *testing.T, texto string, capturado *map[string][]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		mu.Lock()
		if r.MultipartForm != nil {
			*capturado = r.MultipartForm.Value
		}
		mu.Unlock()
		fmt.Fprintf(w, `{"text":%q}`, texto)
	}))
	t.Cleanup(s.Close)
	return s
}

// TestTranscribe_VocabularioVaiNoCampoPrompt prova que o vocabulário
// chega ao servidor.
//
// Sem este teste, "o campo foi escrito" seria só uma afirmação do código
// sobre si mesmo: o multipart poderia ser montado e descartado, ou o
// nome do campo poderia estar errado, e tudo continuaria compilando e
// transcrevendo — só que sem viés nenhum, e o ganho de Entity Accuracy
// medido em 26-set-2026 simplesmente não aconteceria em produção.
func TestTranscribe_VocabularioVaiNoCampoPrompt(t *testing.T) {
	var campos map[string][]string
	srv := servidorQueCapturaCampos(t, "ok", &campos)
	p := providerCom(t, &procFalso{baseURL: srv.URL})

	const vocab = "DCTFWeb, EFD-Reinf, Supabase"
	_, err := p.Transcribe(context.Background(), audioDeTeste(16000),
		asr.ASROptions{Language: "pt-BR", Vocabulary: vocab})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	got := campos["prompt"]
	if len(got) != 1 {
		t.Fatalf("campo \"prompt\" chegou %d vezes, want 1 — campos recebidos: %v", len(got), campos)
	}
	if got[0] != vocab {
		t.Errorf("prompt = %q, want %q", got[0], vocab)
	}
}

// TestTranscribe_SemVocabularioNaoMandaOCampo: o caminho sem vocabulário
// precisa continuar byte a byte igual ao que já estava em produção e foi
// medido. Um campo vazio enviado "por via das dúvidas" mudaria a
// requisição e invalidaria a comparação com o p50 já publicado.
func TestTranscribe_SemVocabularioNaoMandaOCampo(t *testing.T) {
	var campos map[string][]string
	srv := servidorQueCapturaCampos(t, "ok", &campos)
	p := providerCom(t, &procFalso{baseURL: srv.URL})

	if _, err := p.Transcribe(context.Background(), audioDeTeste(16000),
		asr.ASROptions{Language: "pt-BR"}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if vs, ok := campos["prompt"]; ok {
		t.Errorf("campo \"prompt\" foi enviado mesmo sem vocabulário: %q", vs)
	}
}

// TestCapabilities_DeclaraVocabulario: quem monta o vocabulário paga para
// montá-lo (consulta ao dicionário, orçamento de latência). Se a
// capability mentisse, esse trabalho seria feito e jogado fora em
// silêncio.
func TestCapabilities_DeclaraVocabulario(t *testing.T) {
	p := New(Config{ServerPath: "x", ModelPath: "y"}, "m")
	if !p.Capabilities().Vocabulary {
		t.Error("Capabilities().Vocabulary = false, mas pedir() envia o campo prompt")
	}
}
