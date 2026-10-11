package nuvem

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Provedores aceitos.
const (
	OpenAI = "openai"
	Groq   = "groq"
)

// Valido diz se p é um provedor de transcrição aceito.
func Valido(p string) bool { return p == OpenAI || p == Groq }

// Nome é como o provedor aparece para quem usa.
func Nome(p string) string {
	switch p {
	case OpenAI:
		return "OpenAI"
	case Groq:
		return "Groq"
	}
	return p
}

// Os modelos padrão (ADR-021, escolhas menores 1 e 2). São o ponto de partida,
// nunca uma imposição: a pessoa troca, e a lista de onde ela escolhe vem do
// provedor (Cliente.ListarModelos). Nenhum outro lugar do código escreve um
// nome de modelo.
//
// O padrão da OpenAI vale se a chamada de teste em pt-BR da C7 for boa; senão
// o padrão passa a ser ModeloAlternativoOpenAI, o mais barato.
const (
	ModeloPadraoOpenAI      = "gpt-transcribe"
	ModeloAlternativoOpenAI = "gpt-4o-mini-transcribe"
	ModeloPadraoGroq        = "whisper-large-v3-turbo"
)

// ModeloPadrao é o modelo de transcrição que vale quando a pessoa não escolheu
// outro. Vazio para um provedor desconhecido.
func ModeloPadrao(provedor string) string {
	switch provedor {
	case OpenAI:
		return ModeloPadraoOpenAI
	case Groq:
		return ModeloPadraoGroq
	}
	return ""
}

// IdiomaPadrao é o idioma pedido quando nem a chamada nem as ASROptions
// dizem outro: a ADR-021 fixa o português.
const IdiomaPadrao = "pt"

// Endereços oficiais das APIs, até o /v1 (ou o /openai/v1 da Groq).
const (
	BaseOpenAI = "https://api.openai.com/v1"
	BaseGroq   = "https://api.groq.com/openai/v1"
)

const (
	// TimeoutPadrao é o prazo de uma transcrição (ADR-021, "Prazos"). Com
	// prazo mais curto no ctx de quem chama, o mais curto vale.
	TimeoutPadrao = 20 * time.Second
	// TimeoutListaPadrao é o prazo da lista de modelos, o mesmo do teste da
	// chave no internal/ia.
	TimeoutListaPadrao = 10 * time.Second

	// TetoArquivo é o maior áudio que as duas APIs aceitam (25 MB). Acima
	// disso nem se tenta: seria um 413 depois de subir tudo.
	TetoArquivo = 25 << 20
)

// formatosPorProvedor são os tipos de áudio comprimido que cada provedor
// aceita (ADR-021, "O motor") e a extensão do nome do arquivo que a API usa
// para reconhecer o contêiner. O app manda webm; os outros existem porque o
// contrato (asr.ASRCapabilities.Formatos) pede a lista do que o provedor
// aceita, e uma extensão errada faria o provedor recusar um áudio bom.
var formatosPorProvedor = map[string]map[string]string{
	OpenAI: {
		"audio/webm": "webm",
		"audio/mpeg": "mp3",
		"audio/mp4":  "m4a",
		"audio/wav":  "wav",
	},
	Groq: {
		"audio/webm": "webm",
		"audio/mpeg": "mp3",
		"audio/mp4":  "m4a",
		"audio/wav":  "wav",
		"audio/ogg":  "ogg",
		"audio/flac": "flac",
	},
}

// Formatos lista, em ordem estável, os tipos de áudio comprimido do provedor.
// Vazio para um provedor desconhecido.
func Formatos(provedor string) []string {
	return slices.Sorted(maps.Keys(formatosPorProvedor[provedor]))
}

// perfil é o que muda de um modelo para outro no pedido.
type perfil struct {
	// campoIdioma é o nome do campo do idioma: "language" na maioria dos
	// modelos, "languages" no gpt-transcribe.
	campoIdioma string
	// formato é o response_format: "verbose_json" quando o modelo devolve
	// segmentos com no_speech_prob (o whisper), senão "json".
	formato string
}

// perfilDoModelo escolhe o perfil pelo nome do modelo. É uma tabela mínima e
// conservadora: o que o modelo realmente aceita se confere numa chamada de
// teste (ADR-021, C7), e um modelo desconhecido cai no perfil mais comum.
func perfilDoModelo(modelo string) perfil {
	p := perfil{campoIdioma: "language", formato: "json"}
	m := strings.ToLower(modelo)
	if strings.HasPrefix(m, "gpt-transcribe") {
		p.campoIdioma = "languages"
	}
	if strings.Contains(m, "whisper") {
		p.formato = "verbose_json"
	}
	return p
}

// Chamada diz o que transcrever e como.
type Chamada struct {
	// Provedor é OpenAI ou Groq.
	Provedor string
	// Modelo é o modelo de transcrição. Vazio usa ModeloPadrao.
	Modelo string
	// Idioma é o código ISO 639-1 ("pt"). Vazio usa IdiomaPadrao.
	Idioma string
	// Dica vai no campo prompt do provedor (o vocabulário que enviesa o
	// modelo). Vazia, o campo nem é enviado.
	Dica string
}

// Cliente chama as APIs de transcrição. O valor zero funciona e fala com as
// APIs oficiais; os campos existem para os testes apontarem para um
// httptest.Server.
type Cliente struct {
	// HTTP é o cliente usado. Nil usa um novo; um cliente sem Transport usa o
	// do pacote (só HTTP/1.1). Seja qual for, redirecionamento nunca é
	// seguido (ver o doc do pacote).
	HTTP *http.Client
	// BaseOpenAI e BaseGroq trocam o endereço da API. Vazio usa o oficial.
	BaseOpenAI, BaseGroq string
	// Timeout limita a transcrição inteira. Zero usa TimeoutPadrao.
	Timeout time.Duration
	// TimeoutLista limita a lista de modelos. Zero usa TimeoutListaPadrao.
	TimeoutLista time.Duration
}

func (c *Cliente) base(provedor string) string {
	switch provedor {
	case OpenAI:
		if c.BaseOpenAI != "" {
			return c.BaseOpenAI
		}
		return BaseOpenAI
	case Groq:
		if c.BaseGroq != "" {
			return c.BaseGroq
		}
		return BaseGroq
	}
	return ""
}

// semRedirecionamento devolve o cliente HTTP que toda chamada usa: o
// injetado (ou um novo) com o redirecionamento desligado, para a chave nunca
// seguir um 3xx para outro lugar, e com transporteHTTP1 quando ninguém pôs um
// transporte.
func (c *Cliente) semRedirecionamento() *http.Client {
	cli := http.Client{}
	if c.HTTP != nil {
		cli = *c.HTTP
	}
	if cli.Transport == nil {
		cli.Transport = transporteHTTP1
	}
	cli.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cli
}

// transporteHTTP1 é o transporte de quem não injeta o seu: os mesmos limites
// de tempo e o mesmo proxy do http.DefaultTransport, mas SÓ HTTP/1.1 (ver o
// doc do pacote). É variável para o teste do pacote trocá-lo por uma trava de
// rede (rede_test.go).
var transporteHTTP1 http.RoundTripper = novoTransporteHTTP1()

func novoTransporteHTTP1() *http.Transport {
	var so1 http.Protocols
	so1.SetHTTP1(true)
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		Protocols:           &so1,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        4,
		IdleConnTimeout:     90 * time.Second,
	}
}

// chaveUsavel confere o que dá para conferir sem falar com o provedor: a
// chave existe e só tem caracteres que cabem num cabeçalho. Uma chave com
// espaço ou quebra de linha não é uma chave, e iria quebrar o cabeçalho.
func chaveUsavel(provedor, chave string) error {
	if chave == "" {
		return novoErro(provedor, MotivoSemChave, fmt.Sprintf("Falta a chave da %s.", Nome(provedor)))
	}
	for i := 0; i < len(chave); i++ {
		if b := chave[i]; b <= ' ' || b >= 0x7f {
			return novoErro(provedor, MotivoChaveRecusada, fmt.Sprintf(
				"A chave da %s tem caracteres que uma chave não tem. Cole de novo, só a chave.", Nome(provedor)))
		}
	}
	return nil
}

// Transcrever manda o áudio comprimido ao provedor e devolve a transcrição.
//
// O áudio é lido, nunca alterado nem guardado, e não vai a disco. A chave só
// vai no cabeçalho. Os erros são *Erro (a falha do provedor, traduzida),
// ErrSemFala (o provedor atendeu e não devolveu texto), os erros de formato de
// asr (asr.ErrComprimidoNaoSuportado, asr.ErrAudioInvalido) e, se quem chamou
// cancelou, context.Canceled.
func (c *Cliente) Transcrever(ctx context.Context, ch Chamada, chave string, audio asr.Comprimido) (asr.ASRResult, error) {
	inicio := time.Now()
	if !Valido(ch.Provedor) {
		return asr.ASRResult{}, fmt.Errorf("asr/nuvem: provedor desconhecido %q", ch.Provedor)
	}
	modelo := ch.Modelo
	if modelo == "" {
		modelo = ModeloPadrao(ch.Provedor)
	}
	idioma := ch.Idioma
	if idioma == "" {
		idioma = IdiomaPadrao
	}

	tipo := asr.NormalizarTipo(audio.Tipo)
	extensao, aceito := formatosPorProvedor[ch.Provedor][tipo]
	if !aceito {
		return asr.ASRResult{}, asr.ErrComprimidoNaoSuportado
	}
	if len(audio.Bytes) == 0 {
		return asr.ASRResult{}, fmt.Errorf("%w: áudio comprimido sem bytes", asr.ErrAudioInvalido)
	}
	if len(audio.Bytes) > TetoArquivo {
		return asr.ASRResult{}, novoErro(ch.Provedor, MotivoArquivoGrande, fmt.Sprintf(
			"O áudio passa do tamanho que a %s aceita.", Nome(ch.Provedor)))
	}
	if err := chaveUsavel(ch.Provedor, chave); err != nil {
		return asr.ASRResult{}, err
	}

	p := perfilDoModelo(modelo)
	campos := []campo{{"model", modelo}, {p.campoIdioma, idioma}}
	if ch.Dica != "" {
		campos = append(campos, campo{"prompt", ch.Dica})
	}
	campos = append(campos, campo{"temperature", "0"}, campo{"response_format", p.formato})
	corpo, err := montarCorpo(campos, "ditado."+extensao, tipo, audio.Bytes)
	if err != nil {
		return asr.ASRResult{}, respostaIlegivel(ch.Provedor)
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = TimeoutPadrao
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base(ch.Provedor)+"/audio/transcriptions", io.NopCloser(corpo.leitor))
	if err != nil {
		return asr.ASRResult{}, novoErro(ch.Provedor, MotivoSemRede, fmt.Sprintf(
			"Não consegui falar com a %s.", Nome(ch.Provedor)))
	}
	// O tamanho EXPLÍCITO é o que impede o chunked: com um Body que o
	// net/http não reconhece (o MultiReader), ContentLength zero significa
	// "tamanho desconhecido". GetBody fica nil de propósito: o pedido nunca é
	// reenviado sozinho (nem por redirecionamento, que não se segue, nem por
	// conexão velha: um POST não repete), então um ditado nunca é cobrado duas
	// vezes por obra do transporte.
	req.ContentLength = corpo.tamanho
	req.Header.Set("Authorization", "Bearer "+chave)
	req.Header.Set("Content-Type", corpo.contentType)
	req.Header.Set("Accept", "application/json")

	resp, err := c.semRedirecionamento().Do(req)
	if err != nil {
		return asr.ASRResult{}, falhaDeRede(ctx, ch.Provedor)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return asr.ASRResult{}, falhaHTTP(ch.Provedor, resp.StatusCode)
	}
	dados, err := io.ReadAll(io.LimitReader(resp.Body, limiteResposta+1))
	if err != nil {
		return asr.ASRResult{}, falhaDeRede(ctx, ch.Provedor)
	}
	if len(dados) > limiteResposta {
		return asr.ASRResult{}, respostaIlegivel(ch.Provedor)
	}

	lido, ok := lerTranscricao(dados)
	if !ok {
		return asr.ASRResult{}, respostaIlegivel(ch.Provedor)
	}
	if lido.texto == "" {
		return asr.ASRResult{}, ErrSemFala
	}
	return asr.ASRResult{
		Text:           lido.texto,
		Language:       idioma,
		Spans:          lido.spans,
		Model:          modelo,
		ProcessingTime: time.Since(inicio),
		// Confidence fica ausente de propósito: o no_speech_prob e o
		// avg_logprob servem ao filtro de silêncio, e não são a confiança do
		// texto que o contrato promete (docs/ai-spec/16-ai-behavior-spec.md).
	}, nil
}
