package syncengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// A cópia do dicionário no fio (ADR-023, "C. Três rotas da nuvem"): o PC manda
// uma cópia do dicionario.json (a Semente recortada por dictionary.Semente.
// BytesDaCopia) para o celular usar no ditado. Três rotas, cada uma com o token
// do aparelho, fora de qualquer transação:
//
//	GET    /api/v1/sync/dictionary   o estado da fonte
//	PUT    /api/v1/sync/dictionary   sobe a cópia, com Dicionario-SHA256 e Dicionario-Fonte
//	DELETE /api/v1/sync/dictionary   apaga a cópia, se este PC for a fonte
//
// O status, o código do erro, o motivo (do 422) e o estado voltam para a decisão
// do motor (DecidirEnvioDoDicionario), que é pura; erro aqui é só o que para a
// fase: o 401, o 429, o redirecionamento, e a rede ou o prazo. Nenhum erro leva
// a URL, o token, os bytes nem o tamanho, e nenhuma resposta guarda o corpo
// cru: só os campos fechados do estado (o nome do aparelho é o único texto, e só
// vai ao painel).

// PrazoDoDicionario é o prazo de cada pedido do dicionário: a cópia tem até
// 512 KiB (dictionary.MaxBytesDaCopia).
const PrazoDoDicionario = 30 * time.Second

// ModoDoEnvio é o cabeçalho Dicionario-Fonte do PUT.
type ModoDoEnvio string

const (
	// EnvioTomar é o clique em "Levar o dicionário para o celular": grava
	// sempre, e este PC passa a ser a fonte.
	EnvioTomar ModoDoEnvio = "tomar"
	// EnvioManter é o envio automático: só grava se este PC já é a fonte.
	EnvioManter ModoDoEnvio = "manter"
)

// Quem é a fonte da cópia, do ponto de vista de quem pergunta.
const (
	FonteEste    = "este"
	FonteOutro   = "outro"
	FonteNenhuma = "nenhuma"
)

// Os códigos de erro das rotas do dicionário (ADR-023, C).
const (
	CodigoNaoEAFonte        = "nao_e_a_fonte"
	CodigoDicionarioInvalid = "dicionario_invalido"
)

// EstadoDoDicionario é o estado da fonte, o corpo do GET e do 200 do PUT e do
// 409. Aparelho e Termos só vêm com a cópia; SHA256 e AtualizadoEm também.
type EstadoDoDicionario struct {
	// Fonte: FonteEste, FonteOutro ou FonteNenhuma.
	Fonte string
	// Aparelho é o nome do PC que é a fonte (só com FonteOutro e FonteEste).
	Aparelho     string
	Termos       int
	SHA256       string
	AtualizadoEm time.Time
}

// RespostaDoDicionario é o que a nuvem respondeu a um pedido do dicionário.
type RespostaDoDicionario struct {
	Status int
	// Codigo é o code do JSON de erro (vazio no sucesso).
	Codigo string
	// Motivo é o motivo fechado do 422 (dictionary.MotivoDaCopia), ou vazio.
	Motivo string
	// Estado vem no 200 e no 409 (e no 204, nunca); nil se a nuvem não o mandou
	// ou ele não se lê.
	Estado *EstadoDoDicionario
}

// ClienteDoDicionario é o lado do dicionário do SyncClient. O Cliente de
// produção o cumpre; o motor confere com uma asserção de tipo (o SyncClient sem
// ele deixa o dicionário de fora, como a nuvem sem a rota).
type ClienteDoDicionario interface {
	// EstadoDoDicionario é o GET.
	EstadoDoDicionario(ctx context.Context) (RespostaDoDicionario, error)
	// SubirDicionario é o PUT da cópia (os bytes de Semente.BytesDaCopia) com o
	// SHA-256 deles e o modo.
	SubirDicionario(ctx context.Context, copia []byte, sha string, modo ModoDoEnvio) (RespostaDoDicionario, error)
	// ApagarDicionario é o DELETE.
	ApagarDicionario(ctx context.Context) (RespostaDoDicionario, error)
}

var _ ClienteDoDicionario = (*Cliente)(nil)

const caminhoDoDicionario = "/api/v1/sync/dictionary"

// maxCorpoDoDicionario é o teto do que o PC lê de uma resposta do dicionário (o
// estado ou o JSON de erro: umas centenas de bytes).
const maxCorpoDoDicionario = 16 << 10

func (c *Cliente) EstadoDoDicionario(ctx context.Context) (RespostaDoDicionario, error) {
	return c.pedirDicionario(ctx, http.MethodGet, nil, nil)
}

func (c *Cliente) SubirDicionario(ctx context.Context, copia []byte, sha string, modo ModoDoEnvio) (RespostaDoDicionario, error) {
	cab := http.Header{}
	cab.Set("Content-Type", "application/json")
	cab.Set("Dicionario-SHA256", sha)
	cab.Set("Dicionario-Fonte", string(modo))
	if copia == nil {
		copia = []byte{}
	}
	return c.pedirDicionario(ctx, http.MethodPut, copia, cab)
}

func (c *Cliente) ApagarDicionario(ctx context.Context) (RespostaDoDicionario, error) {
	return c.pedirDicionario(ctx, http.MethodDelete, nil, nil)
}

// corpoDoDicionario é a resposta do dicionário, lida com folga: o estado pode
// vir no topo ou em "estado", e o code e o motivo, no topo ou em "error".
type corpoDoDicionario struct {
	Fonte        string `json:"fonte"`
	Aparelho     string `json:"aparelho"`
	Termos       int    `json:"termos"`
	SHA256       string `json:"sha256"`
	AtualizadoEm string `json:"atualizado_em"`
	Code         string `json:"code"`
	Motivo       string `json:"motivo"`

	Estado *struct {
		Fonte        string `json:"fonte"`
		Aparelho     string `json:"aparelho"`
		Termos       int    `json:"termos"`
		SHA256       string `json:"sha256"`
		AtualizadoEm string `json:"atualizado_em"`
	} `json:"estado"`
	Error *struct {
		Code   string `json:"code"`
		Motivo string `json:"motivo"`
	} `json:"error"`
}

// lerRespostaDoDicionario separa o código, o motivo e o estado do corpo. Um
// corpo que não se lê é uma resposta só com o status.
func lerRespostaDoDicionario(status int, raw []byte) RespostaDoDicionario {
	r := RespostaDoDicionario{Status: status}
	var c corpoDoDicionario
	if len(raw) == 0 || json.Unmarshal(raw, &c) != nil {
		return r
	}
	r.Codigo, r.Motivo = c.Code, c.Motivo
	if c.Error != nil {
		if c.Error.Code != "" {
			r.Codigo = c.Error.Code
		}
		if c.Error.Motivo != "" {
			r.Motivo = c.Error.Motivo
		}
	}
	fonte, aparelho, termos, sha, em := c.Fonte, c.Aparelho, c.Termos, c.SHA256, c.AtualizadoEm
	if c.Estado != nil {
		fonte, aparelho, termos, sha, em = c.Estado.Fonte, c.Estado.Aparelho, c.Estado.Termos, c.Estado.SHA256, c.Estado.AtualizadoEm
	}
	switch fonte {
	case FonteEste, FonteOutro, FonteNenhuma:
		e := &EstadoDoDicionario{Fonte: fonte, Aparelho: aparelho, Termos: termos, SHA256: sha}
		if t, err := time.Parse(time.RFC3339Nano, em); err == nil {
			e.AtualizadoEm = t.UTC()
		}
		r.Estado = e
	}
	return r
}

// pedirDicionario faz um pedido do dicionário. O 401, o 429, o redirecionamento
// e a rede voltam como erro (param a fase); o resto volta como status.
func (c *Cliente) pedirDicionario(ctx context.Context, metodo string, corpo []byte, cab http.Header) (RespostaDoDicionario, error) {
	var body io.Reader
	if corpo != nil {
		body = bytes.NewReader(corpo)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.base+caminhoDoDicionario, body)
	if err != nil {
		return RespostaDoDicionario{}, contrato(CodigoRede)
	}
	for k, v := range cab {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpDoDicionario().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return RespostaDoDicionario{}, ctx.Err()
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return RespostaDoDicionario{}, transitorio(CodigoPrazo)
		}
		return RespostaDoDicionario{}, transitorio(CodigoRede)
	}
	defer resp.Body.Close()
	s := resp.StatusCode
	switch {
	case s == http.StatusUnauthorized:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxCorpoDoDicionario))
		return RespostaDoDicionario{Status: s}, ErrNaoAutorizado
	case s == http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxCorpoDoDicionario))
		return RespostaDoDicionario{Status: s}, &ErrEspere{Espera: esperaPedida(resp.Header.Get("Retry-After"), time.Now())}
	case s >= 300 && s < 400:
		return RespostaDoDicionario{Status: s}, transitorio(CodigoRedirecionamento)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxCorpoDoDicionario))
	if err != nil {
		if ctx.Err() != nil {
			return RespostaDoDicionario{}, ctx.Err()
		}
		return RespostaDoDicionario{}, transitorio(CodigoRede)
	}
	return lerRespostaDoDicionario(s, raw), nil
}

// httpDoDicionario é o cliente dos pedidos do dicionário: o mesmo transporte e
// a mesma recusa de redirecionamento do resto do sync, com o PrazoDoDicionario.
func (c *Cliente) httpDoDicionario() *http.Client {
	return &http.Client{Transport: c.http.Transport, Timeout: PrazoDoDicionario, CheckRedirect: c.http.CheckRedirect}
}
