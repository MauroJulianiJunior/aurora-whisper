// Package ia fala com os provedores de IA que quem usa escolhe na tela de
// Configurações (OpenAI, Anthropic), com a chave que ELA colou — nunca
// uma chave do projeto.
//
// Há duas chamadas. Listar os modelos (GET /v1/models, ListarModelos)
// serve para duas coisas ao mesmo tempo — provar que a chave funciona e
// trazer a lista de onde a pessoa escolhe o modelo. Nenhum nome de modelo
// mora no código: a lista vem do provedor, e a escolha é de quem usa. A
// segunda é a do modo Polish (polish.go): uma geração por provedor, com o
// texto ditado, que devolve {"final_text": "..."}.
//
// # O que nunca sai daqui
//
// A chave só vai no cabeçalho da requisição ao provedor. Não entra em
// mensagem de erro, nem em log, nem na URL. O corpo das respostas de erro
// do provedor também não passa adiante: a OpenAI, por exemplo, devolve
// no 401 um pedaço da chave recusada. Toda falha vira uma mensagem fixa,
// em português, escrita aqui (Erro).
//
// Redirecionamento não é seguido: o cabeçalho x-api-key da Anthropic não
// é daqueles que o net/http remove ao trocar de domínio, e seguir um 3xx
// entregaria a chave a quem quer que o provedor — ou quem estivesse no
// meio — apontasse.
//
// Só HTTP/1.1, de propósito: o HTTP/2 do net/http escreve cada cabeçalho
// (a chave) e o começo de cada corpo (o ditado) no log padrão do processo
// quando a variável de ambiente GODEBUG traz http2debug, e guarda o
// cabeçalho de autenticação na tabela HPACK da conexão, que fica em memória
// depois do ditado. O HTTP/1.1 não tem nem uma coisa nem outra, e para uma
// chamada por ditado não perde nada (ver transporteHTTP1).
//
// net/http puro, sem SDK: é uma requisição por provedor.
package ia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"time"
)

// Provedores aceitos.
const (
	OpenAI    = "openai"
	Anthropic = "anthropic"
)

// Valido diz se p é um provedor aceito.
func Valido(p string) bool { return p == OpenAI || p == Anthropic }

// Nome é como o provedor aparece para quem usa.
func Nome(p string) string {
	switch p {
	case OpenAI:
		return "OpenAI"
	case Anthropic:
		return "Anthropic"
	}
	return p
}

// Endereços oficiais das APIs.
const (
	BaseOpenAI    = "https://api.openai.com"
	BaseAnthropic = "https://api.anthropic.com"

	// versaoAnthropic é o cabeçalho anthropic-version que a API exige.
	versaoAnthropic = "2023-06-01"

	// TimeoutPadrao é quanto o teste da chave espera o provedor, e o teto de
	// uma chamada do Polish quando o ctx de quem chama não traz prazo (com
	// prazo, o mais curto dos dois vale).
	TimeoutPadrao = 10 * time.Second

	// limiteResposta corta o corpo lido do provedor. A lista de modelos
	// da OpenAI tem dezenas de KB.
	limiteResposta = 4 << 20
)

// Motivo classifica uma falha, para quem precisa decidir por ela.
type Motivo string

const (
	MotivoChaveRecusada      Motivo = "chave_recusada"
	MotivoSemPermissao       Motivo = "sem_permissao"
	MotivoLimite             Motivo = "limite"
	MotivoIndisponivel       Motivo = "indisponivel"
	MotivoTempoEsgotado      Motivo = "tempo_esgotado"
	MotivoSemRede            Motivo = "sem_rede"
	MotivoRespostaInesperada Motivo = "resposta_inesperada"
)

// Erro é uma falha do provedor já traduzida para quem usa. Não embrulha o
// erro de origem de propósito: ele não é necessário para a tela, e um
// erro de rede carrega a URL e o que mais a biblioteca achar útil.
type Erro struct {
	Motivo   Motivo
	Mensagem string
}

func (e *Erro) Error() string { return e.Mensagem }

// Cliente chama os provedores. O valor zero funciona e fala com as APIs
// oficiais; os campos existem para os testes apontarem para um
// httptest.Server.
type Cliente struct {
	// HTTP é o cliente usado. Nil usa um novo; um cliente sem Transport usa o
	// do pacote (só HTTP/1.1). Seja qual for, redirecionamento nunca é
	// seguido (ver o doc do pacote).
	HTTP *http.Client
	// BaseOpenAI e BaseAnthropic trocam o endereço da API. Vazio usa o
	// oficial.
	BaseOpenAI, BaseAnthropic string
	// Timeout limita a chamada inteira. Zero usa TimeoutPadrao.
	Timeout time.Duration
}

// ListarModelos pergunta ao provedor, com chave, quais modelos ela
// alcança. A ordem é a do mais novo para o mais antigo, como o provedor a
// informa.
func (c *Cliente) ListarModelos(ctx context.Context, provedor, chave string) ([]string, error) {
	if !Valido(provedor) {
		return nil, fmt.Errorf("ia: provedor desconhecido %q", provedor)
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = TimeoutPadrao
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := c.requisicao(ctx, provedor, chave)
	if err != nil {
		return nil, err
	}

	resp, err := c.semRedirecionamento().Do(req)
	if err != nil {
		return nil, falhaDeRede(ctx, provedor, timeout)
	}
	defer resp.Body.Close()
	corpo, err := io.ReadAll(io.LimitReader(resp.Body, limiteResposta))
	if err != nil {
		return nil, falhaDeRede(ctx, provedor, timeout)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, falhaHTTP(provedor, resp.StatusCode)
	}

	if provedor == OpenAI {
		return modelosOpenAI(corpo)
	}
	return modelosAnthropic(corpo)
}

// semRedirecionamento devolve o cliente HTTP que toda chamada usa: o
// injetado (ou um novo) com o redirecionamento desligado, para a chave nunca
// seguir um 3xx para outro lugar, e com transporteHTTP1 quando ninguém pôs
// um transporte.
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
// doc do pacote). Um só para o processo, para o Agent reaproveitar a conexão
// e o TLS de um ditado para o outro. É variável para o teste do pacote
// trocá-lo por uma trava de rede (rede_test.go).
var transporteHTTP1 http.RoundTripper = novoTransporteHTTP1()

// NovoTransporteHTTP1 devolve um transporte NOVO, igual ao do pacote (só
// HTTP/1.1, o proxy e os limites de tempo do padrão), para quem precisa
// embrulhá-lo sem perder a garantia do doc do pacote: o cmd/polishbench conta
// o status de cada resposta por um RoundTripper próprio, e esse RoundTripper
// tem de falar por cima de um transporte só HTTP/1.1, como o do Agent.
func NovoTransporteHTTP1() *http.Transport { return novoTransporteHTTP1() }

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

func (c *Cliente) requisicao(ctx context.Context, provedor, chave string) (*http.Request, error) {
	base, url := c.BaseOpenAI, "/v1/models"
	if base == "" {
		base = BaseOpenAI
	}
	if provedor == Anthropic {
		// limit=1000 é o máximo da API: a lista inteira numa página só.
		base, url = c.BaseAnthropic, "/v1/models?limit=1000"
		if base == "" {
			base = BaseAnthropic
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+url, nil)
	if err != nil {
		return nil, fmt.Errorf("ia: montando a requisição: %w", err)
	}
	if provedor == OpenAI {
		req.Header.Set("Authorization", "Bearer "+chave)
	} else {
		req.Header.Set("x-api-key", chave)
		req.Header.Set("anthropic-version", versaoAnthropic)
	}
	return req, nil
}

func falhaDeRede(ctx context.Context, provedor string, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Erro{MotivoTempoEsgotado, fmt.Sprintf(
			"A %s não respondeu em %d segundos. Confira a conexão com a internet e tente de novo.",
			Nome(provedor), int(timeout.Round(time.Second)/time.Second))}
	}
	return &Erro{MotivoSemRede, fmt.Sprintf(
		"Não consegui falar com a %s. Confira a conexão com a internet.", Nome(provedor))}
}

func falhaHTTP(provedor string, status int) error {
	nome := Nome(provedor)
	switch {
	case status == http.StatusUnauthorized:
		return &Erro{MotivoChaveRecusada, fmt.Sprintf(
			"A %s recusou a chave. Confira se ela foi copiada inteira e se ainda está ativa.", nome)}
	case status == http.StatusForbidden:
		return &Erro{MotivoSemPermissao, fmt.Sprintf(
			"A %s aceitou a chave, mas negou acesso à lista de modelos. Confira as permissões da chave.", nome)}
	case status == http.StatusTooManyRequests:
		return &Erro{MotivoLimite, fmt.Sprintf(
			"A %s recusou por limite de uso ou falta de crédito. Confira o saldo da conta e tente de novo em alguns minutos.", nome)}
	case status >= 500:
		return &Erro{MotivoIndisponivel, fmt.Sprintf(
			"A %s está com problema agora (HTTP %d). Tente de novo mais tarde.", nome, status)}
	}
	return &Erro{MotivoRespostaInesperada, fmt.Sprintf(
		"A %s respondeu de um jeito inesperado (HTTP %d).", nome, status)}
}

func inesperada(provedor string) error {
	return &Erro{MotivoRespostaInesperada, fmt.Sprintf(
		"A %s respondeu de um jeito inesperado: a lista de modelos não veio.", Nome(provedor))}
}

// modelosOpenAI lê {"data":[{"id","created"}]}. A OpenAI não promete
// ordem nenhuma; ordena pelo mais novo (created), e pelo nome no empate.
func modelosOpenAI(corpo []byte) ([]string, error) {
	var r struct {
		Data *[]struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(corpo, &r); err != nil || r.Data == nil {
		return nil, inesperada(OpenAI)
	}
	dados := *r.Data
	sort.SliceStable(dados, func(i, j int) bool {
		if dados[i].Created != dados[j].Created {
			return dados[i].Created > dados[j].Created
		}
		return dados[i].ID < dados[j].ID
	})
	ids := make([]string, 0, len(dados))
	for _, m := range dados {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// modelosAnthropic lê {"data":[{"id"}]}, que a Anthropic já devolve do
// mais novo para o mais antigo.
func modelosAnthropic(corpo []byte) ([]string, error) {
	var r struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(corpo, &r); err != nil || r.Data == nil {
		return nil, inesperada(Anthropic)
	}
	ids := make([]string, 0, len(*r.Data))
	for _, m := range *r.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}
