package guard

import (
	"fmt"
	"net/http"
	"strings"
)

// RequireOrigin retorna um middleware que rejeita, com 403, qualquer
// requisição cujo header Origin esteja presente e não seja uma origem
// permitida: "http://127.0.0.1:<port>" ou "http://localhost:<port>"
// (comparação case-insensitive).
//
// Origin ausente é permitido — clientes que não são navegador (o
// próprio Agent, curl, testes) normalmente não enviam Origin, e
// exigi-lo quebraria esses usos legítimos sem ganho de segurança real:
// a defesa contra DNS rebinding é o Host (RequireHost), não o Origin.
// Este middleware cobre o caso complementar: uma página cross-origin
// (ex.: https://evil.example) chamando a API local via fetch/XHR. Nesse
// caso o navegador preenche Origin automaticamente com a origem real da
// página, e JavaScript não consegue falsificar esse valor — por isso
// ele é confiável como sinal de origem cross-origin.
func RequireOrigin(port int) func(http.Handler) http.Handler {
	allowed := map[string]struct{}{
		fmt.Sprintf("http://127.0.0.1:%d", port): {},
		fmt.Sprintf("http://localhost:%d", port): {},
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := allowed[strings.ToLower(origin)]; !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
