package nuvem

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// ---------------------------------------------------------------------
// O que vai no pedido
// ---------------------------------------------------------------------

// Os campos enviados (ADR-021, "O que o provider faz em cada chamada"): o
// modelo, o idioma pt, o prompt com a dica, temperature 0, o response_format e
// o arquivo com o nome e o tipo certos.
func TestTranscribe_CamposEnviados(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, OpenAI, "gpt-4o-mini-transcribe")

	res, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{Language: "pt-BR", Vocabulary: canarioDica})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "Já mandei o arquivo." {
		t.Errorf("Text = %q, quero o texto do provedor sem os espaços das bordas", res.Text)
	}

	pedidos := srv.pedidos()
	if len(pedidos) != 1 {
		t.Fatalf("o provedor recebeu %d pedidos, quero 1", len(pedidos))
	}
	v := pedidos[0]
	if v.Defeito != "" {
		t.Fatalf("o servidor não leu o multipart: %s", v.Defeito)
	}
	if v.Metodo != http.MethodPost || v.Caminho != "/v1/audio/transcriptions" {
		t.Errorf("pedido %s %s, quero POST /v1/audio/transcriptions", v.Metodo, v.Caminho)
	}
	if got := v.Cab.Get("Authorization"); got != "Bearer "+chaveDeTeste {
		t.Errorf("Authorization = %q", got)
	}
	if !strings.HasPrefix(v.Cab.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Errorf("Content-Type = %q", v.Cab.Get("Content-Type"))
	}

	want := []parCampo{
		{"model", "gpt-4o-mini-transcribe"},
		{"language", "pt"},
		{"prompt", canarioDica},
		{"temperature", "0"},
		{"response_format", "json"},
	}
	if !reflect.DeepEqual(v.Campos, want) {
		t.Errorf("campos = %v\nquero    %v", v.Campos, want)
	}
	if !v.TemArquivo || v.NomeArquivo != "ditado.webm" || v.TipoArquivo != "audio/webm" {
		t.Errorf("arquivo = (tem=%v, nome=%q, tipo=%q), quero ditado.webm / audio/webm", v.TemArquivo, v.NomeArquivo, v.TipoArquivo)
	}
	if string(v.BytesArquivo) != string(audioWebm().Bytes) {
		t.Error("os bytes do arquivo que chegaram não são os que foram enviados")
	}
}

// O que muda de um modelo para outro: o campo do idioma e o response_format.
func TestTranscribe_PerfilPorModelo(t *testing.T) {
	casos := []struct {
		provedor, modelo string
		campoIdioma      string
		formato          string
		caminho          string
	}{
		{OpenAI, "gpt-transcribe", "languages", "json", "/v1/audio/transcriptions"},
		{OpenAI, "gpt-4o-mini-transcribe", "language", "json", "/v1/audio/transcriptions"},
		{OpenAI, "gpt-4o-transcribe", "language", "json", "/v1/audio/transcriptions"},
		{OpenAI, "whisper-1", "language", "verbose_json", "/v1/audio/transcriptions"},
		{Groq, "whisper-large-v3-turbo", "language", "verbose_json", "/openai/v1/audio/transcriptions"},
		{Groq, "whisper-large-v3", "language", "verbose_json", "/openai/v1/audio/transcriptions"},
	}
	for _, c := range casos {
		t.Run(c.provedor+"/"+c.modelo, func(t *testing.T) {
			srv := novoProvedorFalso(t, responder(200, respostaOK))
			p := providerDe(srv, c.provedor, c.modelo)
			if _, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{}); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			v := srv.pedidos()[0]
			if v.Caminho != c.caminho {
				t.Errorf("caminho = %q, quero %q", v.Caminho, c.caminho)
			}
			if got, ok := v.campo(c.campoIdioma); !ok || got != "pt" {
				t.Errorf("campo %q = (%q, %v), quero pt", c.campoIdioma, got, ok)
			}
			outro := "languages"
			if c.campoIdioma == "languages" {
				outro = "language"
			}
			if _, ok := v.campo(outro); ok {
				t.Errorf("o pedido leva também o campo %q", outro)
			}
			if got, _ := v.campo("response_format"); got != c.formato {
				t.Errorf("response_format = %q, quero %q", got, c.formato)
			}
			if got, _ := v.campo("model"); got != c.modelo {
				t.Errorf("model = %q, quero %q", got, c.modelo)
			}
		})
	}
}

// Sem dica, o campo prompt nem é enviado.
func TestTranscribe_SemDicaNaoMandaPrompt(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, Groq, "")
	if _, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{}); err != nil {
		t.Fatal(err)
	}
	v := srv.pedidos()[0]
	if _, ok := v.campo("prompt"); ok {
		t.Errorf("campos = %v: o prompt foi enviado sem dica", v.nomes())
	}
	want := []string{"model", "language", "temperature", "response_format"}
	if !reflect.DeepEqual(v.nomes(), want) {
		t.Errorf("campos = %v, quero %v", v.nomes(), want)
	}
}

// Os padrões das escolhas menores 1 e 2, e o que vale quando a pessoa não
// escolheu modelo.
func TestModeloPadrao(t *testing.T) {
	if ModeloPadraoOpenAI != "gpt-transcribe" || ModeloAlternativoOpenAI != "gpt-4o-mini-transcribe" || ModeloPadraoGroq != "whisper-large-v3-turbo" {
		t.Fatalf("as constantes mudaram: %q %q %q", ModeloPadraoOpenAI, ModeloAlternativoOpenAI, ModeloPadraoGroq)
	}
	if ModeloPadrao(OpenAI) != ModeloPadraoOpenAI || ModeloPadrao(Groq) != ModeloPadraoGroq || ModeloPadrao("outro") != "" {
		t.Error("ModeloPadrao não devolve o padrão de cada provedor")
	}

	for _, c := range []struct{ provedor, modelo string }{
		{OpenAI, ModeloPadraoOpenAI},
		{Groq, ModeloPadraoGroq},
	} {
		srv := novoProvedorFalso(t, responder(200, respostaOK))
		p := providerDe(srv, c.provedor, "") // sem modelo escolhido
		res, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
		if err != nil {
			t.Fatalf("%s: %v", c.provedor, err)
		}
		if got, _ := srv.pedidos()[0].campo("model"); got != c.modelo {
			t.Errorf("%s: model = %q, quero o padrão %q", c.provedor, got, c.modelo)
		}
		if res.Model != c.modelo {
			t.Errorf("%s: Result.Model = %q, quero %q", c.provedor, res.Model, c.modelo)
		}
	}

	// Trocável: o modelo da pessoa vence o padrão.
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, OpenAI, ModeloAlternativoOpenAI)
	if _, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := srv.pedidos()[0].campo("model"); got != ModeloAlternativoOpenAI {
		t.Errorf("model = %q", got)
	}
}

// O idioma do pedido: o das ASROptions (pt-BR vira pt), senão o do Provider,
// senão o português.
func TestTranscribe_Idioma(t *testing.T) {
	casos := []struct {
		opts, provider, want string
	}{
		{"pt-BR", "", "pt"},
		{"PT_br", "", "pt"},
		{"en-US", "", "en"},
		{"", "", "pt"},
		{"", "es-AR", "es"},
		{"lixo!", "", "pt"},
		{"português", "", "pt"},
		{"", "xyzw", "pt"},
	}
	for _, c := range casos {
		srv := novoProvedorFalso(t, responder(200, respostaOK))
		p := providerDe(srv, Groq, "")
		p.Idioma = c.provider
		res, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{Language: c.opts})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if got, _ := srv.pedidos()[0].campo("language"); got != c.want {
			t.Errorf("opts=%q provider=%q: language = %q, quero %q", c.opts, c.provider, got, c.want)
		}
		if c.opts != "" && res.Language != c.opts {
			t.Errorf("Result.Language = %q, quero o das opções (%q)", res.Language, c.opts)
		}
	}
}

// O tipo do áudio chega sem parâmetros, e o nome do arquivo segue o contêiner.
func TestTranscribe_TipoDoArquivo(t *testing.T) {
	casos := []struct {
		provedor, tipo, nome, tipoEnviado string
	}{
		{OpenAI, "audio/webm", "ditado.webm", "audio/webm"},
		{OpenAI, "Audio/WebM; codecs=opus", "ditado.webm", "audio/webm"},
		{OpenAI, "audio/mpeg", "ditado.mp3", "audio/mpeg"},
		{OpenAI, "audio/mp4", "ditado.m4a", "audio/mp4"},
		{OpenAI, "audio/wav", "ditado.wav", "audio/wav"},
		{Groq, "audio/ogg", "ditado.ogg", "audio/ogg"},
		{Groq, "audio/flac", "ditado.flac", "audio/flac"},
	}
	for _, c := range casos {
		srv := novoProvedorFalso(t, responder(200, respostaOK))
		p := providerDe(srv, c.provedor, "")
		buf := asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: audioWebm().Bytes, Tipo: c.tipo}}
		if _, err := p.Transcribe(context.Background(), buf, asr.ASROptions{}); err != nil {
			t.Fatalf("%s %q: %v", c.provedor, c.tipo, err)
		}
		v := srv.pedidos()[0]
		if v.NomeArquivo != c.nome || v.TipoArquivo != c.tipoEnviado {
			t.Errorf("%s %q: arquivo %q (%s), quero %q (%s)", c.provedor, c.tipo, v.NomeArquivo, v.TipoArquivo, c.nome, c.tipoEnviado)
		}
		if !p.Capabilities().AceitaFormato(c.tipo) {
			t.Errorf("%s: Capabilities().AceitaFormato(%q) = false, mas a transcrição aceitou", c.provedor, c.tipo)
		}
	}
}

// Um formato que o provedor não aceita é recusado ANTES de qualquer pedido.
func TestTranscribe_FormatoNaoAceito(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	casos := []struct{ provedor, tipo string }{
		{OpenAI, "audio/ogg"}, // só a Groq
		{OpenAI, "audio/flac"},
		{Groq, "audio/x-nada"},
		{Groq, "video/webm"},
		{Groq, "application/octet-stream"},
	}
	for _, c := range casos {
		p := providerDe(srv, c.provedor, "")
		buf := asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: []byte{1, 2, 3}, Tipo: c.tipo}}
		_, err := p.Transcribe(context.Background(), buf, asr.ASROptions{})
		if !errors.Is(err, asr.ErrComprimidoNaoSuportado) {
			t.Errorf("%s %q: err = %v, quero asr.ErrComprimidoNaoSuportado", c.provedor, c.tipo, err)
		}
		if p.Capabilities().AceitaFormato(c.tipo) {
			t.Errorf("%s: Capabilities().AceitaFormato(%q) = true", c.provedor, c.tipo)
		}
	}
	if n := len(srv.pedidos()); n != 0 {
		t.Errorf("o provedor recebeu %d pedidos de formato recusado", n)
	}
}

// O pacote não codifica áudio: PCM é recusado, e um buffer ambíguo também.
func TestTranscribe_RecusaPCMEBufferAmbiguo(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, OpenAI, "")

	_, err := p.Transcribe(context.Background(), asr.AudioBuffer{PCM: []int16{1, 2, 3}, SampleRateHz: 16000}, asr.ASROptions{})
	if !errors.Is(err, asr.ErrPCMNaoSuportado) {
		t.Errorf("PCM: err = %v, quero asr.ErrPCMNaoSuportado", err)
	}
	_, err = p.Transcribe(context.Background(), asr.AudioBuffer{}, asr.ASROptions{})
	if !errors.Is(err, asr.ErrPCMNaoSuportado) {
		t.Errorf("buffer vazio: err = %v, quero asr.ErrPCMNaoSuportado", err)
	}

	ambiguo := asr.AudioBuffer{PCM: []int16{1}, SampleRateHz: 16000, Comprimido: audioWebm()}
	if _, err := p.Transcribe(context.Background(), ambiguo, asr.ASROptions{}); !errors.Is(err, asr.ErrAudioInvalido) {
		t.Errorf("PCM e comprimido juntos: err = %v, quero asr.ErrAudioInvalido", err)
	}
	semBytes := asr.AudioBuffer{Comprimido: asr.Comprimido{Tipo: "audio/webm"}}
	if _, err := p.Transcribe(context.Background(), semBytes, asr.ASROptions{}); !errors.Is(err, asr.ErrAudioInvalido) {
		t.Errorf("comprimido sem bytes: err = %v, quero asr.ErrAudioInvalido", err)
	}
	if n := len(srv.pedidos()); n != 0 {
		t.Errorf("o provedor recebeu %d pedidos", n)
	}
}

// O teto do provedor (25 MB) é conferido antes de subir o áudio.
func TestTranscribe_AcimaDoTetoNaoEnvia(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, OpenAI, "")
	grande := asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: make([]byte, TetoArquivo+1), Tipo: "audio/webm"}}

	_, err := p.Transcribe(context.Background(), grande, asr.ASROptions{})
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoArquivoGrande {
		t.Fatalf("err = %v, quero *Erro com MotivoArquivoGrande", err)
	}
	if n := len(srv.pedidos()); n != 0 {
		t.Errorf("o provedor recebeu %d pedidos de um áudio acima do teto", n)
	}

	// Exatamente no teto, passa.
	noTeto := asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: make([]byte, TetoArquivo), Tipo: "audio/webm"}}
	if _, err := p.Transcribe(context.Background(), noTeto, asr.ASROptions{}); err != nil {
		t.Errorf("no teto: %v", err)
	}
}

// ---------------------------------------------------------------------
// A chave
// ---------------------------------------------------------------------

// A chave é lida a cada chamada: trocá-la vale na seguinte, e nada a guarda.
func TestTranscribe_ChaveLidaACadaChamada(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	chaves := []string{"primeira-chave-falsa", "segunda-chave-falsa"}
	lidas := 0
	p := providerDe(srv, OpenAI, "")
	p.Chave = func(context.Context) (string, error) {
		k := chaves[lidas]
		lidas++
		return k, nil
	}
	for range chaves {
		if _, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if lidas != 2 {
		t.Errorf("a chave foi lida %d vezes, quero 2 (uma por chamada)", lidas)
	}
	vistos := srv.pedidos()
	for i, k := range chaves {
		if got := vistos[i].Cab.Get("Authorization"); got != "Bearer "+k {
			t.Errorf("chamada %d: Authorization = %q, quero a chave lida naquela chamada", i, got)
		}
	}
}

// Sem chave, ou com a leitura falhando, nada é enviado e o erro de origem não
// passa adiante.
func TestTranscribe_SemChave(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	const segredoNoErro = "SEGREDO-DENTRO-DO-ERRO-DA-LEITURA"
	casos := map[string]func(context.Context) (string, error){
		"função nula":    nil,
		"chave vazia":    func(context.Context) (string, error) { return "", nil },
		"leitura falhou": func(context.Context) (string, error) { return "", errors.New(segredoNoErro) },
	}
	for nome, fn := range casos {
		p := providerDe(srv, OpenAI, "")
		p.Chave = fn
		_, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
		var e *Erro
		if !errors.As(err, &e) || e.Motivo != MotivoSemChave {
			t.Errorf("%s: err = %v, quero *Erro com MotivoSemChave", nome, err)
			continue
		}
		semRastro(t, nome, err.Error(), segredoNoErro)
	}
	if n := len(srv.pedidos()); n != 0 {
		t.Errorf("o provedor recebeu %d pedidos sem chave", n)
	}
}

// Uma "chave" com espaço, quebra de linha ou byte de controle não é uma chave:
// nada é enviado (e o cabeçalho não é quebrado).
func TestTranscribe_ChaveMalFormada(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	for _, k := range []string{"tem espaço", "tem\nquebra", "tem\r\ncabecalho: injetado", "tab\taqui", "acentuação-ção", "del\x7f"} {
		p := providerDe(srv, OpenAI, "")
		p.Chave = func(context.Context) (string, error) { return k, nil }
		_, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
		var e *Erro
		if !errors.As(err, &e) || e.Motivo != MotivoChaveRecusada {
			t.Errorf("%q: err = %v, quero *Erro com MotivoChaveRecusada", k, err)
			continue
		}
		semRastro(t, "o erro", err.Error(), k)
	}
	if n := len(srv.pedidos()); n != 0 {
		t.Errorf("o provedor recebeu %d pedidos com chave mal formada", n)
	}
}

// Se o ctx já venceu quando a leitura da chave falha, o erro é o do ctx.
func TestTranscribe_ChaveComCtxCancelado(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, OpenAI, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.Chave = func(c context.Context) (string, error) { return "", c.Err() }
	_, err := p.Transcribe(ctx, bufferWebm(), asr.ASROptions{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, quero context.Canceled", err)
	}
}

// ---------------------------------------------------------------------
// O que o provider diz de si
// ---------------------------------------------------------------------

func TestProvider_IDECapabilities(t *testing.T) {
	op := &Provider{Provedor: OpenAI}
	gr := &Provider{Provedor: Groq, Modelo: "whisper-large-v3"}
	if op.ID() != "openai" || gr.ID() != "groq" {
		t.Errorf("ID = %q e %q", op.ID(), gr.ID())
	}

	for _, p := range []*Provider{op, gr} {
		c := p.Capabilities()
		if c.Confidence {
			t.Errorf("%s: Confidence = true, mas o provedor não devolve confiança do texto", p.ID())
		}
		if !c.Vocabulary {
			t.Errorf("%s: Vocabulary = false, mas a dica vai no prompt", p.ID())
		}
		if c.Streaming || c.NBest {
			t.Errorf("%s: declara o que não entrega: %+v", p.ID(), c)
		}
		if !c.AceitaFormato("audio/webm") {
			t.Errorf("%s: não declara audio/webm", p.ID())
		}
	}
	// O padrão da OpenAI (gpt-transcribe) não devolve segmentos; o whisper da
	// Groq devolve.
	if op.Capabilities().Timestamps {
		t.Error("OpenAI com o modelo padrão declara Timestamps")
	}
	if !gr.Capabilities().Timestamps {
		t.Error("Groq com o whisper não declara Timestamps")
	}
	if op.Capabilities().AceitaFormato("audio/ogg") || !gr.Capabilities().AceitaFormato("audio/ogg") {
		t.Error("ogg: a OpenAI não aceita e a Groq aceita (ADR-021, tabela do motor)")
	}
	if got := Formatos("outro"); len(got) != 0 {
		t.Errorf("Formatos de um provedor desconhecido = %v", got)
	}
	// A lista é nova a cada chamada: quem a muda não afeta os outros.
	f := op.Capabilities().Formatos
	f[0] = "audio/x-mexido"
	if op.Capabilities().Formatos[0] == "audio/x-mexido" {
		t.Error("Capabilities().Formatos compartilha o array entre chamadas")
	}
}

func TestProvider_ResultadoNaoFabricaConfidence(t *testing.T) {
	const demora = 30 * time.Millisecond
	resposta := responder(200, `{"text":"oi","segments":[{"start":0,"end":1,"text":"oi","avg_logprob":-0.1,"no_speech_prob":0.01}]}`)
	srv := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(demora)
		resposta(w, r)
	})
	p := providerDe(srv, Groq, "")
	res, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Confidence.Present {
		t.Errorf("Confidence = %+v: o provider inventou uma confiança", res.Confidence)
	}
	// O relógio do Windows é grosso: um tempo medido de verdade, e não "maior
	// que zero", é o que prova que o ProcessingTime foi preenchido.
	if res.ProcessingTime < demora*2/3 || res.Model != ModeloPadraoGroq {
		t.Errorf("ProcessingTime=%v (o provedor demorou %v) Model=%q", res.ProcessingTime, demora, res.Model)
	}
}

func TestProvider_WarmupNaoChamaARede(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, OpenAI, "")
	if err := p.Warmup(context.Background()); err != nil {
		t.Errorf("Warmup: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Warmup(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Warmup com ctx cancelado = %v", err)
	}
	if n := len(srv.pedidos()); n != 0 {
		t.Errorf("Warmup fez %d pedidos (seriam cobrados)", n)
	}
}

func TestProvider_ProvedorDesconhecido(t *testing.T) {
	chamouChave := false
	p := &Provider{Provedor: "outro", Chave: func(context.Context) (string, error) { chamouChave = true; return chaveDeTeste, nil }}
	if _, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{}); err == nil {
		t.Fatal("Transcribe com provedor desconhecido = nil")
	}
	if chamouChave {
		t.Error("a chave foi lida para um provedor desconhecido")
	}
}

// O provider nunca libera o buffer: quem libera é o dono (asr.AudioBuffer).
func TestTranscribe_NuncaChamaRelease(t *testing.T) {
	liberado := 0
	buf := bufferWebm()
	buf.Release = func() { liberado++ }
	// No sucesso e nas falhas.
	for _, status := range []int{200, 401, 500} {
		srv := novoProvedorFalso(t, responder(status, respostaOK))
		providerDe(srv, OpenAI, "").Transcribe(context.Background(), buf, asr.ASROptions{})
	}
	if liberado != 0 {
		t.Errorf("Release foi chamado %d vez(es) pelo provider", liberado)
	}
}

// O áudio recebido não é alterado.
func TestTranscribe_NaoAlteraOAudio(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, OpenAI, "")
	buf := bufferWebm()
	antes := append([]byte(nil), buf.Comprimido.Bytes...)
	if _, err := p.Transcribe(context.Background(), buf, asr.ASROptions{}); err != nil {
		t.Fatal(err)
	}
	if string(buf.Comprimido.Bytes) != string(antes) {
		t.Error("o provider alterou os bytes do áudio")
	}
}

// ---------------------------------------------------------------------
// Prazo e cancelamento
// ---------------------------------------------------------------------

func TestTranscribe_TempoEsgotado(t *testing.T) {
	srv := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // o corpo já foi lido por lerPedido
	})
	p := providerDe(srv, OpenAI, "")
	p.Cliente.Timeout = 80 * time.Millisecond

	inicio := time.Now()
	_, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoTempoEsgotado {
		t.Fatalf("err = %v, quero *Erro com MotivoTempoEsgotado", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("o erro do prazo não embrulha context.DeadlineExceeded")
	}
	if d := time.Since(inicio); d > 5*time.Second {
		t.Errorf("levou %v para desistir de um prazo de 80ms", d)
	}
}

// O prazo do ctx de quem chama, se menor, vale (o mais curto dos dois).
func TestTranscribe_PrazoDoCtxMenorVale(t *testing.T) {
	srv := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	p := providerDe(srv, OpenAI, "") // Timeout padrão: 20s
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	inicio := time.Now()
	_, err := p.Transcribe(ctx, bufferWebm(), asr.ASROptions{})
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoTempoEsgotado {
		t.Fatalf("err = %v, quero MotivoTempoEsgotado", err)
	}
	if d := time.Since(inicio); d > 5*time.Second {
		t.Errorf("levou %v: o prazo do ctx não valeu", d)
	}
}

// Cancelar não é falha do provedor: volta context.Canceled puro.
func TestTranscribe_Cancelado(t *testing.T) {
	srv := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	p := providerDe(srv, OpenAI, "")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()
	_, err := p.Transcribe(ctx, bufferWebm(), asr.ASROptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, quero context.Canceled", err)
	}
	var e *Erro
	if errors.As(err, &e) {
		t.Errorf("o cancelamento virou falha do provedor: %v", e)
	}
}

// Sem servidor do outro lado: sem_rede, e a mensagem não traz o endereço.
func TestTranscribe_SemRede(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, respostaOK))
	p := providerDe(srv, Groq, "")
	endereco := srv.URL
	srv.Close() // a porta fica morta

	_, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoSemRede {
		t.Fatalf("err = %v, quero *Erro com MotivoSemRede", err)
	}
	semRastro(t, "o erro", err.Error(), endereco, "127.0.0.1", chaveDeTeste)
	if errors.Unwrap(err) != nil {
		t.Errorf("o erro embrulha %v: o erro de rede carrega a URL", errors.Unwrap(err))
	}
}
