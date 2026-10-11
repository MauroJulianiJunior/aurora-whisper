package guard

import (
	"log"
	"net/http"
)

// Middleware é um decorador de http.Handler, no formato usado por todo
// este pacote e composável via Chain.
type Middleware func(http.Handler) http.Handler

// Chain compõe middlewares numa única cadeia, executados na ordem em
// que aparecem: Chain(a, b, c)(h) equivale a a(b(c(h))) — ou seja, uma
// requisição passa primeiro por a, depois b, depois c, antes de chegar
// em h.
func Chain(mws ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		h := final
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}

// Protect monta a cadeia completa de defesa da Local API, na ordem
// correta para o modelo de ameaça de
// docs/ai-spec/09-security-privacy.md (THR-001/THR-002):
//
//  1. RequireHost — barra DNS rebinding. É a checagem mais barata (só
//     olha um header) e a mais importante: sem ela, AC-012 não vale
//     nada, mesmo com listener em loopback.
//  2. RequireOrigin — barra chamada cross-origin feita por página web
//     via fetch/XHR.
//  3. RequireToken — exige posse do token da sessão atual, comparado
//     em tempo constante.
//
// port deve ser a porta efetiva do listener (guard.Listen — tipicamente
// lis.Addr().(*net.TCPAddr).Port), a mesma usada para montar os
// allowlists de Host/Origin. token é o valor gerado por NewToken no
// start do Agent.
func Protect(port int, token string) Middleware {
	return Chain(
		RequireHost(port),
		RequireOrigin(port),
		RequireToken(token),
	)
}

// statusRecorder captura o status HTTP escrito por um handler, para
// LogRequests poder registrá-lo sem precisar inspecionar o corpo da
// resposta.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// LogRequests retorna um middleware de auditoria que registra, em out,
// uma linha por requisição com método, path e status resultante.
//
// Deliberadamente NÃO registra headers, query string, body ou remote
// addr: nenhum desses campos deveria carregar segredo neste sistema,
// mas o objetivo deste middleware é auditoria mínima, não um log geral
// — e essa lista mínima é a forma mais simples de garantir que Authorization,
// o token e cookies (proibidos em log por
// docs/ai-spec/09-security-privacy.md, "Logging proibido") nunca
// passem por aqui, hoje ou depois de alguém editar este arquivo.
func LogRequests(out *log.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			out.Printf("guard: %s %s -> %d", r.Method, r.URL.Path, rec.status)
		})
	}
}
