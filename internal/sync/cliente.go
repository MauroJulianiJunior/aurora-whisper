package syncengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// O cliente HTTPS do sync (ADR-019, F7b): o push e o pull com o token do
// aparelho (o Bearer, lido do Gerenciador de Credenciais pelo nuvem.Servico,
// atrás do portão sysinput), com prazo, sem seguir redirecionamento, só em
// HTTP/1.1 (NovoHTTP) e com o corpo limitado nos dois sentidos.
//
// Nenhum erro daqui leva a URL, o token, o corpo ou o erro cru do net/http
// (que traz a URL): só um código de uma lista fechada.

// Os limites do cliente.
const (
	// PrazoDoPedido é o prazo de cada pedido à nuvem.
	PrazoDoPedido = 30 * time.Second
	// MaxCorpoDoPedido é o teto do corpo do push (o do servidor é 1 MiB; o
	// lote fica bem abaixo, MaxBytesPorLote).
	MaxCorpoDoPedido = 1 << 20
	// MaxCorpoDaResposta é o teto do que o PC lê de uma resposta. Uma página
	// do pull tem no máximo o orçamento de texto da nuvem (2 MiB) mais o
	// último item (até 3 textos de 64 KiB), contados em bytes crus; no JSON,
	// o pior caso é 6 vezes isso (o encoder escapa <, > e & e os caracteres
	// de controle como \u00XX), uns 13 MiB, mais o envelope de 500 itens.
	// Com 8 MiB, uma página assim travava o cursor para sempre (a mesma
	// página, de novo e de novo); TestSyncE2E_PaginaComTextoEscapado e
	// TestSyncE2E_TetoDaRespostaCobreOPiorCaso conferem. Passar disso é
	// tratado como falha de rede: o reenvio dá duplicate, e o pull traz o
	// estado.
	MaxCorpoDaResposta = 16 << 20
	// MaxEsperaPedida é o teto do Retry-After de um 429.
	MaxEsperaPedida = 10 * time.Minute
	// EsperaPadraoDo429 vale quando o 429 vem sem um Retry-After legível.
	EsperaPadraoDo429 = time.Minute
	// LimiteDoPull é o limit de cada página (o teto do servidor).
	LimiteDoPull = 500
)

// Os erros do cliente.
var (
	// ErrNaoAutorizado: 401. O token foi revogado, venceu ou não existe
	// mais: o PC fica desconectado (nuvem.Servico.TokenRecusado), e o ditado
	// segue local.
	ErrNaoAutorizado = errors.New("sync: a nuvem recusou o token")
	// ErrRecomeco: 410 (RESYNC_REQUIRED): o PC perdeu lápides. No mesmo
	// epoch, ele lê de novo do cursor 0, sem reenviar nada (Motor.Rodada).
	ErrRecomeco = errors.New("sync: a nuvem pediu o recomeço")
	// ErrTransitorio: a rede, o prazo, um 5xx, um redirecionamento ou uma
	// resposta grande demais. Espera com jitter e tenta de novo.
	ErrTransitorio = errors.New("sync: a nuvem não respondeu agora")
	// ErrContrato: a resposta não é a do contrato (400, 403, 404, um JSON
	// que não se lê, resultados que não batem com o lote). Espera longa.
	ErrContrato = errors.New("sync: a resposta da nuvem não é a do contrato")
)

// ErrEspere é o 429: a nuvem pediu para esperar.
type ErrEspere struct{ Espera time.Duration }

func (e *ErrEspere) Error() string {
	return "sync: a nuvem pediu para esperar " + e.Espera.String()
}

// Falha é um ErrTransitorio ou ErrContrato com o código (lista fechada) do
// que aconteceu, para o log.
type Falha struct {
	Tipo   error
	Codigo string
}

func (f *Falha) Error() string { return f.Tipo.Error() + " (" + f.Codigo + ")" }
func (f *Falha) Unwrap() error { return f.Tipo }

// Os códigos das falhas (lista fechada).
const (
	CodigoRede             = "rede"
	CodigoPrazo            = "prazo"
	CodigoServidor         = "servidor"
	CodigoRedirecionamento = "redirecionamento"
	CodigoCorpoGrande      = "corpo_grande"
	CodigoJSON             = "json"
	CodigoStatus           = "status"
	CodigoLote             = "lote"
	CodigoCursor           = "cursor"
	CodigoPedidoGrande     = "pedido_grande"
)

func transitorio(codigo string) error { return &Falha{Tipo: ErrTransitorio, Codigo: codigo} }
func contrato(codigo string) error    { return &Falha{Tipo: ErrContrato, Codigo: codigo} }

// CodigoDoErro é o código de lista fechada de um erro do sync, para o log e
// para sync_state.ultimo_erro.
func CodigoDoErro(err error) string {
	var f *Falha
	var e *ErrEspere
	switch {
	case err == nil:
		return ""
	case errors.As(err, &f):
		return f.Codigo
	case errors.As(err, &e):
		return "espere"
	case errors.Is(err, ErrNaoAutorizado):
		return "nao_autorizado"
	case errors.Is(err, ErrRecomeco):
		return "recomeco"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "cancelado"
	default:
		return "local"
	}
}

// NovoHTTP é o http.Client do sync: prazo de PrazoDoPedido, nenhum
// redirecionamento seguido (uma 3xx é falha: o token nunca vai a outro
// endereço) e só HTTP/1.1.
//
// Só HTTP/1.1, com o mesmo transporte que protege a chave de IA
// (ia.NovoTransporteHTTP1: o proxy e os prazos do transporte padrão): o HTTP/2
// do net/http escreve cada cabeçalho (o Bearer do token) e o começo de cada
// corpo (o texto dos ditados no push) no log padrão quando a variável de
// ambiente GODEBUG traz http2debug, e o Agent manda o log padrão para o
// agent.log (TestHTTP2Debug_TokenNaoVaiParaOLog). Um transporte novo por
// cliente; o Agent monta um só (NovoLaco).
func NovoHTTP() *http.Client {
	return &http.Client{
		Transport:     ia.NovoTransporteHTTP1(),
		Timeout:       PrazoDoPedido,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Cliente é o SyncClient de produção.
type Cliente struct {
	base  string
	token string
	http  *http.Client
}

var _ SyncClient = (*Cliente)(nil)

// NovoCliente fala com a nuvem em base (a origem, sem barra no fim: a
// nuvem.NormalizarURL já a deixou assim) com o token do aparelho. h nil é
// NovoHTTP().
func NovoCliente(base, token string, h *http.Client) *Cliente {
	if h == nil {
		h = NovoHTTP()
	}
	return &Cliente{base: strings.TrimRight(base, "/"), token: token, http: h}
}

// Push manda o lote e confere a resposta: um resultado por mutação, na
// ordem, e o epoch. O state de cada resultado é lido no tipo da mutação do
// mesmo lugar (fio.go, "A leitura pelo tipo"), antes das conferências, como a
// F7 o lia junto com o resto.
func (c *Cliente) Push(ctx context.Context, muts []Mutation) (PushResult, error) {
	if muts == nil {
		muts = []Mutation{}
	}
	corpo, err := json.Marshal(map[string]any{"mutations": muts})
	if err != nil {
		return PushResult{}, fmt.Errorf("sync: montando o push: %w", err)
	}
	if len(corpo) > MaxCorpoDoPedido {
		return PushResult{}, contrato(CodigoPedidoGrande)
	}
	var w respostaDoPush
	if err := c.pedir(ctx, http.MethodPost, "/api/v1/sync/push", corpo, &w); err != nil {
		return PushResult{}, err
	}
	out, err := w.resultado(muts)
	if err != nil {
		return PushResult{}, contrato(CodigoJSON)
	}
	if out.Epoch == "" || len(out.Results) != len(muts) {
		return PushResult{}, contrato(CodigoLote)
	}
	for i := range out.Results {
		if !strings.EqualFold(out.Results[i].MutationID, muts[i].MutationID) {
			return PushResult{}, contrato(CodigoLote)
		}
	}
	return out, nil
}

// Pull lê uma página depois de afterSeq e confere que o cursor não volta.
// Cada payload é lido no tipo da mudança (fio.go, "A leitura pelo tipo").
func (c *Cliente) Pull(ctx context.Context, afterSeq int64, limit int) (ChangePage, error) {
	q := url.Values{"after_seq": {strconv.FormatInt(afterSeq, 10)}}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var w paginaDoFio
	if err := c.pedir(ctx, http.MethodGet, "/api/v1/sync/pull?"+q.Encode(), nil, &w); err != nil {
		return ChangePage{}, err
	}
	out, err := w.pagina()
	if err != nil {
		return ChangePage{}, contrato(CodigoJSON)
	}
	if out.Epoch == "" || out.LastSeq < afterSeq {
		return ChangePage{}, contrato(CodigoCursor)
	}
	return out, nil
}

// pedir faz um pedido à nuvem e lê a resposta 200 em saida.
func (c *Cliente) pedir(ctx context.Context, metodo, caminho string, corpo []byte, saida any) error {
	var body io.Reader
	if corpo != nil {
		body = bytes.NewReader(corpo)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.base+caminho, body)
	if err != nil {
		// Só uma URL malformada chega aqui, e ela não vai para o erro.
		return contrato(CodigoRede)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return transitorio(CodigoPrazo)
		}
		return transitorio(CodigoRede)
	}
	defer resp.Body.Close()
	lido := io.LimitReader(resp.Body, MaxCorpoDaResposta+1)
	defer func() { _, _ = io.Copy(io.Discard, lido) }()

	switch s := resp.StatusCode; {
	case s == http.StatusOK:
		raw, err := io.ReadAll(lido)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return transitorio(CodigoRede)
		}
		if len(raw) > MaxCorpoDaResposta {
			return transitorio(CodigoCorpoGrande)
		}
		if err := json.Unmarshal(raw, saida); err != nil {
			return contrato(CodigoJSON)
		}
		return nil
	case s == http.StatusUnauthorized:
		return ErrNaoAutorizado
	case s == http.StatusGone:
		return ErrRecomeco
	case s == http.StatusTooManyRequests:
		return &ErrEspere{Espera: esperaPedida(resp.Header.Get("Retry-After"), time.Now())}
	case s >= 300 && s < 400:
		return transitorio(CodigoRedirecionamento)
	case s >= 500:
		return transitorio(CodigoServidor)
	default:
		return contrato(CodigoStatus)
	}
}

// esperaPedida lê o Retry-After (segundos, ou uma data HTTP), com o teto de
// MaxEsperaPedida; ilegível ou ausente é EsperaPadraoDo429.
func esperaPedida(v string, agora time.Time) time.Duration {
	v = strings.TrimSpace(v)
	var d time.Duration
	if n, err := strconv.Atoi(v); err == nil {
		switch {
		case n < 0:
			return EsperaPadraoDo429
		case n > int(MaxEsperaPedida/time.Second):
			return MaxEsperaPedida
		}
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(agora)
	} else {
		return EsperaPadraoDo429
	}
	if d <= 0 {
		d = time.Second
	}
	if d > MaxEsperaPedida {
		d = MaxEsperaPedida
	}
	return d
}
