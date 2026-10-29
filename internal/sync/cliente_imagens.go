package syncengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
)

// As imagens das notas no fio (ADR-022, "Decisões da N10b"; 06, "Imagens das
// notas (nuvem)"): os bytes nunca vão no push nem no pull. O PC sobe a imagem
// que anexou pelo PUT, baixa a que veio de outro lugar pelo GET e tira pelo
// DELETE, cada um com o token do aparelho, fora de qualquer transação:
//
//	PUT    /api/v1/sync/notes/{nota}/images/{imagem}   o JPEG cru, com Imagem-SHA256 e Imagem-Criada-Em
//	GET    /api/v1/sync/notes/{nota}/images/{imagem}   os bytes
//	DELETE /api/v1/sync/notes/{nota}/images/{imagem}   tira (idempotente)
//
// O status e o código do erro voltam para a decisão do motor
// (DecidirEnvioDaImagem, DecidirBaixaDaImagem), que é pura; erro aqui é só o que
// para a fase: o 401, o 429, o redirecionamento, e a rede ou o prazo. Nenhum
// erro leva a URL, o token, os bytes nem o tamanho.

// PrazoDaImagem é o prazo de cada pedido de imagem: 4 MB a 1 Mbit/s levam uns
// 32 s, mais que o PrazoDoPedido do resto do sync.
const PrazoDaImagem = 2 * time.Minute

// RespostaDaImagem é o que a nuvem respondeu a um pedido de imagem: o status e o
// código do erro (o "code" do JSON de erro; vazio no sucesso).
type RespostaDaImagem struct {
	Status int
	Codigo string
}

// ClienteDeImagens é o lado das imagens do SyncClient. O Cliente de produção o
// cumpre; o motor confere com uma asserção de tipo (o SyncClient sem ele deixa as
// imagens de fora, como a nuvem sem a capacidade).
type ClienteDeImagens interface {
	// SubirImagem manda o PUT da imagem id da nota, com o SHA-256 da cópia
	// guardada e a hora do anexo.
	SubirImagem(ctx context.Context, nota, id string, jpeg []byte, sha string, criadaEm time.Time) (RespostaDaImagem, error)
	// BaixarImagem lê os bytes (até imagemdanota.MaxBytes; mais que isso é a
	// resposta 200 sem os bytes, que a decisão trata como estragada).
	BaixarImagem(ctx context.Context, nota, id string) ([]byte, RespostaDaImagem, error)
	// TirarImagem manda o DELETE.
	TirarImagem(ctx context.Context, nota, id string) (RespostaDaImagem, error)
}

var _ ClienteDeImagens = (*Cliente)(nil)

// caminhoDaImagem é o caminho de uma imagem no sync. Os ids vêm da fila (UUIDs
// conferidos), e o PathEscape é a segunda guarda.
func caminhoDaImagem(nota, id string) string {
	return "/api/v1/sync/notes/" + url.PathEscape(nota) + "/images/" + url.PathEscape(id)
}

func (c *Cliente) SubirImagem(ctx context.Context, nota, id string, jpeg []byte, sha string, criadaEm time.Time) (RespostaDaImagem, error) {
	cab := http.Header{}
	cab.Set("Content-Type", "image/jpeg")
	cab.Set("Imagem-SHA256", sha)
	if !criadaEm.IsZero() {
		cab.Set("Imagem-Criada-Em", criadaEm.UTC().Format(time.RFC3339Nano))
	}
	r, _, err := c.pedirImagem(ctx, http.MethodPut, caminhoDaImagem(nota, id), jpeg, cab, false)
	return r, err
}

func (c *Cliente) BaixarImagem(ctx context.Context, nota, id string) ([]byte, RespostaDaImagem, error) {
	r, b, err := c.pedirImagem(ctx, http.MethodGet, caminhoDaImagem(nota, id), nil, nil, true)
	return b, r, err
}

func (c *Cliente) TirarImagem(ctx context.Context, nota, id string) (RespostaDaImagem, error) {
	r, _, err := c.pedirImagem(ctx, http.MethodDelete, caminhoDaImagem(nota, id), nil, nil, false)
	return r, err
}

// maxCorpoDoErro é o teto do que o PC lê de uma resposta de erro de imagem (o
// JSON do contrato, uma frase).
const maxCorpoDoErro = 4 << 10

// pedirImagem faz um pedido de imagem. comBytes: o 200 traz os bytes (o GET).
func (c *Cliente) pedirImagem(ctx context.Context, metodo, caminho string, corpo []byte, cab http.Header, comBytes bool) (RespostaDaImagem, []byte, error) {
	var body io.Reader
	if corpo != nil {
		body = bytes.NewReader(corpo)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.base+caminho, body)
	if err != nil {
		return RespostaDaImagem{}, nil, contrato(CodigoRede)
	}
	for k, v := range cab {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpDasImagens().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return RespostaDaImagem{}, nil, ctx.Err()
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return RespostaDaImagem{}, nil, transitorio(CodigoPrazo)
		}
		return RespostaDaImagem{}, nil, transitorio(CodigoRede)
	}
	defer resp.Body.Close()
	r := RespostaDaImagem{Status: resp.StatusCode}
	switch s := resp.StatusCode; {
	case s == http.StatusUnauthorized:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxCorpoDoErro))
		return r, nil, ErrNaoAutorizado
	case s == http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxCorpoDoErro))
		return r, nil, &ErrEspere{Espera: esperaPedida(resp.Header.Get("Retry-After"), time.Now())}
	case s >= 300 && s < 400:
		return r, nil, transitorio(CodigoRedirecionamento)
	case s >= 200 && s < 300:
		if !comBytes {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxCorpoDoErro))
			return r, nil, nil
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, imagemdanota.MaxBytes+1))
		if err != nil {
			if ctx.Err() != nil {
				return RespostaDaImagem{}, nil, ctx.Err()
			}
			return RespostaDaImagem{}, nil, transitorio(CodigoRede)
		}
		if len(b) > imagemdanota.MaxBytes {
			// Mais que a maior imagem: não é a imagem guardada.
			return r, nil, nil
		}
		return r, b, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxCorpoDoErro))
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil {
		r.Codigo = e.Error.Code
	}
	return r, nil, nil
}

// httpDasImagens é o cliente dos pedidos de imagem: o mesmo transporte e a
// mesma recusa de redirecionamento do resto do sync, com o PrazoDaImagem.
func (c *Cliente) httpDasImagens() *http.Client {
	return &http.Client{Transport: c.http.Transport, Timeout: PrazoDaImagem, CheckRedirect: c.http.CheckRedirect}
}
