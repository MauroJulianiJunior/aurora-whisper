package server

import (
	"context"
	"net/http"
	"time"
)

// O log de acesso da nuvem (ADR-019, "Servidor"; 09, "Logging proibido"): uma
// linha por pedido, com o método, o PADRÃO da rota (r.Pattern, nunca a URL), o
// status, os milissegundos e os bytes; o push e o pull acrescentam contagens;
// e um motivo de lista fechada quando o pedido foi barrado antes da rota
// (host, origem_cruzada, limite, panico) ou o login foi recusado. Nunca a URL,
// a query, o corpo, o cookie, o token, o code, o state, o e-mail nem o IP.

type acessoKey struct{}

// acessoInfo é o que as camadas de dentro contam para a linha de log.
type acessoInfo struct {
	rota      string
	motivo    string
	contagens []any
	// semBytes tira os bytes da resposta da linha (ocultarBytes): na leitura
	// de uma imagem de nota, eles seriam o tamanho dela (N10b).
	semBytes bool
}

func acessoDe(ctx context.Context) *acessoInfo {
	info, _ := ctx.Value(acessoKey{}).(*acessoInfo)
	return info
}

func anotarMotivo(ctx context.Context, motivo string) {
	if info := acessoDe(ctx); info != nil {
		info.motivo = motivo
	}
}

// ocultarBytes tira os bytes da resposta da linha de log deste pedido: a
// resposta é uma imagem de nota, e o tamanho dela não vai para o log (N10b).
func ocultarBytes(ctx context.Context) {
	if info := acessoDe(ctx); info != nil {
		info.semBytes = true
	}
}

// anotarContagens acrescenta pares nome/número à linha de log.
func anotarContagens(ctx context.Context, kv ...any) {
	if info := acessoDe(ctx); info != nil {
		info.contagens = append(info.contagens, kv...)
	}
}

type gravador struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (g *gravador) WriteHeader(code int) {
	if !g.wrote {
		g.status, g.wrote = code, true
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gravador) Write(b []byte) (int, error) {
	if !g.wrote {
		g.status, g.wrote = http.StatusOK, true
	}
	n, err := g.ResponseWriter.Write(b)
	g.bytes += n
	return n, err
}

func (g *gravador) Unwrap() http.ResponseWriter { return g.ResponseWriter }

// metodos são os métodos que o log escreve como vieram; qualquer outro vira
// "outro" (o método é texto livre do cliente).
var metodos = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
}

func (c *Cloud) registrarAcesso(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &acessoInfo{}
		g := &gravador{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(g, r.WithContext(context.WithValue(r.Context(), acessoKey{}, info)))

		method := r.Method
		if !metodos[method] {
			method = "outro"
		}
		rota := info.rota
		if rota == "" {
			rota = "sem rota"
		}
		attrs := []any{
			"metodo", method, "rota", rota, "status", g.status,
			"ms", time.Since(start).Milliseconds(),
		}
		if !info.semBytes {
			attrs = append(attrs, "bytes", g.bytes)
		}
		if info.motivo != "" {
			attrs = append(attrs, "motivo", info.motivo)
		}
		attrs = append(attrs, info.contagens...)
		c.log.Info("pedido", attrs...)
	})
}
