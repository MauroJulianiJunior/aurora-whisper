package ia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Esta metade do pacote é o cliente do Polish: UMA chamada de geração por
// provedor (POST /v1/responses na OpenAI, POST /v1/messages na Anthropic),
// pedindo que o modelo devolva {"final_text": "..."} pelo formato
// estruturado do provedor. O que vai no pedido vem pronto de
// resolver.PolishRequest (prompt, mensagem do usuário, schema); aqui só se
// monta o envelope de cada provedor.
//
// # O que o cliente garante
//
//   - Nada além do PolishRequest sai da máquina, mais o nome do modelo e a
//     chave (no cabeçalho). Não há campo de contexto, de janela nem de
//     dicionário no corpo.
//   - A chave entra por PARÂMETRO a cada chamada (Cliente.Polir) ou por uma
//     função chamada a cada ditado (Polidor.Chave): nenhum campo guarda a
//     chave. Ela só vai no cabeçalho — nunca na URL, no corpo, num erro ou
//     num log.
//   - Nenhum corpo de resposta de erro do provedor passa adiante: a
//     resposta é lida só para classificar (tipo e código do erro) e vira um
//     *resolver.ErroPolish com um motivo fixo.
//   - Sem retry e sem seguir redirecionamento. O prazo é o do ctx de quem
//     chama; sem prazo no ctx vale Cliente.Timeout (ou TimeoutPadrao), para
//     uma chamada nunca ficar pendurada.
//   - A resposta lida é limitada a limiteRespostaPolish.
//
// O que o modelo respondeu é DADO NÃO CONFIÁVEL: este pacote só extrai o
// texto. Quem valida é resolver.Polir.

const (
	// maxTokensSaida é o teto de tokens de saída dos dois provedores. Um
	// ditado organizado tem algumas centenas; 2048 sobra e ainda limita o
	// gasto de uma resposta que fuja do controle.
	maxTokensSaida = 2048

	// limiteRespostaPolish corta o corpo lido. A resposta esperada é um JSON
	// com o texto do ditado: alguns KB.
	limiteRespostaPolish = 1 << 20

	// EsforcoPadrao é o esforço de raciocínio que a OpenAI recebe quando
	// Chamada.Esforco é vazio: sem raciocínio, que só custa tokens e latência
	// numa tarefa de organizar texto. O padrão da API seria "medium".
	EsforcoPadrao = "none"
	// EsforcoOmitido faz o pedido à OpenAI seguir SEM o campo reasoning, para
	// modelos que não têm raciocínio e recusam o parâmetro.
	EsforcoOmitido = "omitir"
)

// Chamada diz a quem o Polish fala. Nenhum nome de modelo mora no código: a
// escolha é de quem usa (ou do benchmark).
type Chamada struct {
	Provedor string
	Modelo   string
	// Esforco só vale na OpenAI: o reasoning.effort do pedido. Vazio usa
	// EsforcoPadrao; EsforcoOmitido tira o campo. A Anthropic nunca recebe
	// pensamento (thinking).
	Esforco string
}

// Tokens é o consumo que o provedor informou. Só números.
type Tokens struct {
	Entrada, Saida int
	// Raciocinio já está contado em Saida (o provedor cobra como saída); é
	// informado à parte para conferir que o esforço "none" valeu.
	Raciocinio int
}

// Resposta é o que Polir devolve. Tokens vem preenchido mesmo quando há erro,
// se o provedor chegou a informar (uma resposta truncada ou recusada também
// foi cobrada).
type Resposta struct {
	// Texto é o JSON BRUTO que o modelo escreveu — {"final_text": "..."} —,
	// sem o envelope do provedor. Dado não confiável: resolver.Polir valida.
	Texto  []byte
	Tokens Tokens
}

// Polir faz a chamada. A chave é a de quem usa, lida por quem chama, e não é
// guardada. O erro, se houver, é um *resolver.ErroPolish (motivo classificado),
// resolver.ErrPolishSemChave (chave vazia) ou um erro sem detalhe nenhum do
// pedido (pedido incompleto, provedor desconhecido).
func (c *Cliente) Polir(ctx context.Context, ch Chamada, chave string, req resolver.PolishRequest) (Resposta, error) {
	if !Valido(ch.Provedor) {
		return Resposta{}, fmt.Errorf("ia: provedor desconhecido %q", ch.Provedor)
	}
	// Um pedido sem prompt mandaria o ditado ao provedor sem instrução
	// nenhuma, gastando dinheiro: recusa antes de abrir a rede.
	if req.Instrucoes == "" || req.Mensagem == "" || req.Schema == nil {
		return Resposta{}, errors.New("ia: PolishRequest sem instruções, mensagem ou schema")
	}
	if ch.Modelo == "" {
		return Resposta{}, erroPolish(resolver.MotivoModeloInexistente)
	}
	if chave == "" {
		return Resposta{}, resolver.ErrPolishSemChave
	}
	if !chaveUsavel(chave) {
		return Resposta{}, erroPolish(resolver.MotivoChaveRecusada)
	}

	corpo, err := json.Marshal(pedidoPolish(ch, req))
	if err != nil {
		return Resposta{}, errors.New("ia: montando o pedido do Polish")
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = TimeoutPadrao
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status, lido, err := c.postar(ctx, ch.Provedor, chave, corpo)
	if err != nil {
		return Resposta{}, err
	}
	if status != http.StatusOK {
		return Resposta{}, erroPolish(motivoHTTP(status, lido))
	}
	if ch.Provedor == OpenAI {
		return extrairOpenAI(lido)
	}
	return extrairAnthropic(lido)
}

// erroPolish é o erro fixo de um motivo: sem corpo, sem chave, sem ditado.
func erroPolish(m resolver.MotivoFallback) error { return &resolver.ErroPolish{Motivo: m} }

// chaveUsavel diz se a chave só tem caracteres ASCII visíveis, que é o que
// qualquer chave de API tem. Uma chave com espaço ou quebra de linha não vai
// ao cabeçalho: seria rejeitada de qualquer jeito, e nem deve sair daqui.
func chaveUsavel(chave string) bool {
	for i := 0; i < len(chave); i++ {
		if chave[i] <= ' ' || chave[i] > '~' {
			return false
		}
	}
	return true
}

// pedidoPolish monta o corpo do provedor. Estes são TODOS os campos que
// saem: acrescentar um exige mexer aqui e no teste que os enumera.
func pedidoPolish(ch Chamada, req resolver.PolishRequest) map[string]any {
	mensagens := []map[string]string{{"role": "user", "content": req.Mensagem}}
	if ch.Provedor == Anthropic {
		return map[string]any{
			"model":      ch.Modelo,
			"max_tokens": maxTokensSaida,
			"system":     req.Instrucoes,
			"messages":   mensagens,
			"output_config": map[string]any{
				"format": map[string]any{"type": "json_schema", "schema": req.Schema},
			},
		}
	}
	corpo := map[string]any{
		"model":        ch.Modelo,
		"instructions": req.Instrucoes,
		"input":        mensagens,
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   resolver.PolishSchemaNome,
				"strict": true,
				"schema": req.Schema,
			},
		},
		// Sem isto a resposta ficaria guardada 30 dias na conta de quem usa.
		"store":             false,
		"max_output_tokens": maxTokensSaida,
	}
	esforco := ch.Esforco
	if esforco == "" {
		esforco = EsforcoPadrao
	}
	if esforco != EsforcoOmitido {
		corpo["reasoning"] = map[string]string{"effort": esforco}
	}
	return corpo
}

// requisicaoPolish monta o POST ao provedor: endereço, corpo e cabeçalhos,
// com a chave só no cabeçalho de autenticação. Não abre a rede, então o teste
// confere o destino do valor zero (as APIs oficiais, por HTTPS) sem enviar
// nada.
func (c *Cliente) requisicaoPolish(ctx context.Context, provedor, chave string, corpo []byte) (*http.Request, error) {
	base, caminho := c.BaseOpenAI, "/v1/responses"
	if base == "" {
		base = BaseOpenAI
	}
	if provedor == Anthropic {
		base, caminho = c.BaseAnthropic, "/v1/messages"
		if base == "" {
			base = BaseAnthropic
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+caminho, bytes.NewReader(corpo))
	if err != nil {
		return nil, errors.New("ia: montando a requisição do Polish")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if provedor == OpenAI {
		req.Header.Set("Authorization", "Bearer "+chave)
	} else {
		req.Header.Set("x-api-key", chave)
		req.Header.Set("anthropic-version", versaoAnthropic)
	}
	return req, nil
}

// postar envia o corpo e lê a resposta, limitada. Um erro devolvido é de rede
// ou de prazo (*resolver.ErroPolish), ou resposta grande demais.
func (c *Cliente) postar(ctx context.Context, provedor, chave string, corpo []byte) (int, []byte, error) {
	req, err := c.requisicaoPolish(ctx, provedor, chave, corpo)
	if err != nil {
		return 0, nil, err
	}
	resp, err := c.semRedirecionamento().Do(req)
	if err != nil {
		return 0, nil, falhaDeRedePolish(ctx)
	}
	defer resp.Body.Close()
	lido, err := io.ReadAll(io.LimitReader(resp.Body, limiteRespostaPolish+1))
	if err != nil {
		return 0, nil, falhaDeRedePolish(ctx)
	}
	// Uma resposta boa maior que o limite não é lida pela metade: é inválida.
	// Um erro com corpo enorme é classificado pelo que coube (o corte deixa o
	// JSON inválido, e vale o status).
	if len(lido) > limiteRespostaPolish && resp.StatusCode == http.StatusOK {
		return 0, nil, erroPolish(resolver.MotivoRespostaInvalida)
	}
	return resp.StatusCode, lido, nil
}

// falhaDeRedePolish separa o prazo (o ctx expirou) do resto. Cancelamento não
// é prazo: é o Agent encerrando.
func falhaDeRedePolish(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return erroPolish(resolver.MotivoPrazo)
	}
	return erroPolish(resolver.MotivoTransporte)
}

// Códigos de erro (error.type, error.code ou error.details.error_code, em
// minúsculas) que dizem "conta sem crédito ou sem teto de gasto: repetir não
// adianta". Os provedores os usam junto com 429 (a mesma família do limite por
// minuto, que passa sozinho) ou 400, então o status sozinho não separa.
var sinaisSemCredito = []string{
	"insufficient_quota", "billing_hard_limit_reached", "credit_balance_exhausted",
	"organization_spend_limit_exceeded", "project_spend_limit_exceeded",
	"organization_usage_limit_exceeded", "enforced_spend_limit_reached",
}

// A Anthropic devolve 400 invalid_request_error, sem código, com uma frase,
// quando o crédito acaba ou o teto de gasto é atingido.
var frasesSemCredito = []string{"credit balance", "usage limit", "spend limit"}

// motivoHTTP classifica uma resposta que não é 200. Lê o corpo só para achar
// o tipo e o código do erro; o texto da mensagem só é comparado, nunca
// devolvido.
func motivoHTTP(status int, corpo []byte) resolver.MotivoFallback {
	if status < 400 {
		// 3xx (redirecionamento nunca é seguido) ou 2xx que não é o 200.
		return resolver.MotivoRespostaInvalida
	}
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Code    any    `json:"code"`
			Param   any    `json:"param"`
			Message string `json:"message"`
			Details struct {
				ErrorCode string `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	// Corpo que não é JSON deixa tudo vazio: vale só o status.
	_ = json.Unmarshal(corpo, &e)
	codigo, _ := e.Error.Code.(string)
	param, _ := e.Error.Param.(string)
	sinais := []string{
		strings.ToLower(e.Error.Type), strings.ToLower(codigo), strings.ToLower(e.Error.Details.ErrorCode),
	}
	mensagem := strings.ToLower(e.Error.Message)

	switch {
	case status == http.StatusPaymentRequired,
		slices.ContainsFunc(sinais, func(s string) bool { return slices.Contains(sinaisSemCredito, s) }),
		status == http.StatusBadRequest && semCreditoNaFrase(mensagem):
		return resolver.MotivoSemCredito
	// model_not_found também vem com 400 e com 403 (conta sem acesso ao
	// modelo), que sem isto seriam "pedido recusado" e "chave recusada".
	case status == http.StatusNotFound, slices.Contains(sinais, "model_not_found"):
		return resolver.MotivoModeloInexistente
	// O modelo existe, mas recusa o esforço de raciocínio que o pedido manda
	// (o padrão "none" só vale nos modelos mais novos da OpenAI): o erro traz
	// o campo recusado em error.param. Escolher outro modelo resolve.
	case status == http.StatusBadRequest && strings.HasPrefix(strings.ToLower(param), "reasoning"):
		return resolver.MotivoModeloIncompativel
	case status == http.StatusTooManyRequests:
		return resolver.MotivoLimite
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return resolver.MotivoChaveRecusada
	case status >= 500:
		return resolver.MotivoSobrecarga
	}
	return resolver.MotivoPedidoRecusado
}

func semCreditoNaFrase(mensagem string) bool {
	return slices.ContainsFunc(frasesSemCredito, func(f string) bool { return strings.Contains(mensagem, f) })
}

// extrairOpenAI lê a resposta da Responses API. O texto não está em
// output[0].content[0]: o array output tem itens de vários tipos (raciocínio,
// mensagem, chamada de ferramenta), então percorre os itens "message" e junta
// as partes "output_text". Recusa, incomplete e ausência de texto são erro.
func extrairOpenAI(corpo []byte) (Resposta, error) {
	var r struct {
		Status            string          `json:"status"`
		Error             json.RawMessage `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			OutputTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(corpo, &r); err != nil {
		return Resposta{}, erroPolish(resolver.MotivoRespostaInvalida)
	}
	resp := Resposta{Tokens: Tokens{
		Entrada:    r.Usage.InputTokens,
		Saida:      r.Usage.OutputTokens,
		Raciocinio: r.Usage.OutputTokensDetails.ReasoningTokens,
	}}
	if len(r.Error) > 0 && string(r.Error) != "null" {
		return resp, erroPolish(resolver.MotivoRespostaInvalida)
	}
	switch r.Status {
	case "", "completed":
	case "incomplete":
		if r.IncompleteDetails != nil && r.IncompleteDetails.Reason == "content_filter" {
			return resp, erroPolish(resolver.MotivoRecusa)
		}
		return resp, erroPolish(resolver.MotivoTruncada)
	default:
		return resp, erroPolish(resolver.MotivoRespostaInvalida)
	}

	var texto strings.Builder
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, parte := range item.Content {
			switch parte.Type {
			case "refusal":
				return resp, erroPolish(resolver.MotivoRecusa)
			case "output_text":
				texto.WriteString(parte.Text)
			}
		}
	}
	if strings.TrimSpace(texto.String()) == "" {
		return resp, erroPolish(resolver.MotivoRespostaInvalida)
	}
	resp.Texto = []byte(texto.String())
	return resp, nil
}

// extrairAnthropic lê a resposta da Messages API. Só stop_reason "end_turn"
// vale: "refusal" é recusa, "max_tokens" é resposta cortada, e qualquer outro
// (tool_use, pause_turn, stop_sequence, vazio) é resposta que o protocolo não
// promete.
func extrairAnthropic(corpo []byte) (Resposta, error) {
	var r struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(corpo, &r); err != nil {
		return Resposta{}, erroPolish(resolver.MotivoRespostaInvalida)
	}
	resp := Resposta{Tokens: Tokens{Entrada: r.Usage.InputTokens, Saida: r.Usage.OutputTokens}}
	switch r.StopReason {
	case "end_turn":
	case "refusal":
		return resp, erroPolish(resolver.MotivoRecusa)
	case "max_tokens":
		return resp, erroPolish(resolver.MotivoTruncada)
	default:
		return resp, erroPolish(resolver.MotivoRespostaInvalida)
	}

	var texto strings.Builder
	for _, bloco := range r.Content {
		if bloco.Type == "text" {
			texto.WriteString(bloco.Text)
		}
	}
	if strings.TrimSpace(texto.String()) == "" {
		return resp, erroPolish(resolver.MotivoRespostaInvalida)
	}
	resp.Texto = []byte(texto.String())
	return resp, nil
}

// Polidor é o resolver.PolishClient de um provedor e um modelo. A chave NÃO
// mora nele: Chave é chamada a cada ditado, então "remover a chave" vale no
// ditado seguinte e nenhum campo do Polidor (nem do Cliente) guarda a chave.
type Polidor struct {
	// Cliente é o transporte. Nil usa o valor zero: as APIs oficiais.
	Cliente *Cliente
	Chamada Chamada
	// Chave devolve a chave de quem usa (o Agent a lê do Gerenciador de
	// Credenciais), ou resolver.ErrPolishSemChave se não há. Nil é o mesmo que
	// "sem chave". Não pode pôr a chave em erro nenhum.
	Chave func(ctx context.Context) (string, error)
	// Uso, se não for nil, recebe o consumo de cada resposta que o provedor
	// informou (também as recusadas e as truncadas, que foram cobradas). Só
	// números: o log pode guardá-los.
	Uso func(Tokens)
}

var _ resolver.PolishClient = (*Polidor)(nil)

// Polish implementa resolver.PolishClient.
func (p *Polidor) Polish(ctx context.Context, req resolver.PolishRequest) ([]byte, error) {
	chave, err := p.chaveDoDitado(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.cliente().Polir(ctx, p.Chamada, chave, req)
	if p.Uso != nil && resp.Tokens != (Tokens{}) {
		p.Uso(resp.Tokens)
	}
	// Cliente.Polir só devolve Texto quando não há erro.
	return resp.Texto, err
}

// cliente é o transporte: o Cliente do Polidor ou, se for nil, o valor zero,
// que fala com as APIs oficiais.
func (p *Polidor) cliente() *Cliente {
	if p.Cliente == nil {
		return &Cliente{}
	}
	return p.Cliente
}

// chaveDoDitado pede a chave. Um erro de quem a lê nunca é repassado (poderia
// trazer o que não devia). Duas leituras dele valem:
//   - resolver.ErrPolishSemChave (achado por errors.Is): "sem chave", o
//     próprio valor sentinela, sem o texto que o embrulhava;
//   - o ctx do ditado já vencido ou cancelado: devolve o erro do ctx, que não
//     carrega segredo nenhum, e o resolver classifica como prazo ou
//     transporte. Sem isto, a leitura da chave que morre com o prazo do
//     ditado estourado apareceria na tela como "sem chave".
//
// Qualquer outro erro é "sem chave".
func (p *Polidor) chaveDoDitado(ctx context.Context) (string, error) {
	if p.Chave == nil {
		return "", resolver.ErrPolishSemChave
	}
	chave, err := p.Chave(ctx)
	if err != nil {
		if !errors.Is(err, resolver.ErrPolishSemChave) && ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", resolver.ErrPolishSemChave
	}
	return chave, nil
}
