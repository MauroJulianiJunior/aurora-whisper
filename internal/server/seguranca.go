package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
)

// Os cabeçalhos, o Host, o CSRF, a guarda de pânico e o limite de tentativas
// da nuvem (ADR-019, "Servidor" e "Proteção do navegador").

// csp vale para toda resposta: só a própria origem, nada de plugin, de
// <base>, de moldura nem de formulário para outro site. A página (pagina/)
// não tem script nem estilo embutido para caber nela.
const csp = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'"

// hsts: um ano, sem includeSubDomains (o domínio pode ter outros serviços).
// Só com PUBLIC_BASE_URL em https: em http://localhost ele não vale nada, e
// o navegador o ignora.
const hsts = "max-age=31536000"

func (c *Cloud) cabecalhos(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if c.https {
			h.Set("Strict-Transport-Security", hsts)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// conferirHost recusa (400) um Host diferente do de PUBLIC_BASE_URL. Barra o
// DNS rebinding (um site que aponta o próprio nome para 127.0.0.1) e garante
// que o cookie e o redirect do Google são os desta origem: a página tem de
// abrir em localhost, e não em 127.0.0.1, quando a base é localhost. O health
// fica de fora, para a checagem do contêiner bater no IP.
func (c *Cloud) conferirHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/health/") && normalHost(r.Host, c.scheme()) != c.host {
			anotarMotivo(r.Context(), "host")
			writeError(w, http.StatusBadRequest, codeBadRequest, "host inválido")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *Cloud) scheme() string {
	if c.https {
		return "https"
	}
	return "http"
}

// negarOrigemCruzada é a resposta do http.CrossOriginProtection: um pedido
// que não é GET nem HEAD vindo de outro site (Sec-Fetch-Site cross-site ou
// same-site, ou um Origin de outro host) recebe 403.
func (c *Cloud) negarOrigemCruzada(w http.ResponseWriter, r *http.Request) {
	anotarMotivo(r.Context(), "origem_cruzada")
	writeError(w, http.StatusForbidden, codeForbidden, "pedido de outra origem")
}

// recuperar responde 500 a um pânico num handler e registra só a rota: o
// pânico padrão do http.Server escreveria o endereço de quem pediu e o valor
// do pânico no log.
func (c *Cloud) recuperar(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			anotarMotivo(r.Context(), "panico")
			writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		}()
		next.ServeHTTP(w, r)
	})
}

// chaveDoIP é a chave do limite de tentativas por IP: o RemoteAddr, ou o
// último salto do X-Forwarded-For quando o pedido vem do proxy confiável; o
// IPv6 agrupado por /64 (cloudauth.ChaveDoLimite). Nunca vai a log.
func (c *Cloud) chaveDoIP(r *http.Request) string {
	return cloudauth.ChaveDoLimite(c.cfg.Proxy.IPDoCliente(r.RemoteAddr, r.Header.Values("X-Forwarded-For")))
}

// permitir gasta n fichas da chave no limite e, sem fichas, responde 429 com
// Retry-After.
func (c *Cloud) permitir(w http.ResponseWriter, r *http.Request, l *cloudauth.Limite, chave string, n int) bool {
	ok, wait := l.Permitir(chave, n)
	if ok {
		return true
	}
	negarPorLimite(w, r, wait)
	writeError(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "tentativas demais; espere e tente de novo")
	return false
}

func negarPorLimite(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	anotarMotivo(r.Context(), "limite")
	w.Header().Set("Retry-After", strconv.Itoa(cloudauth.RetryAfter(wait)))
}
