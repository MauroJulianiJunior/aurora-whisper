package guard

import (
	"fmt"
	"net/http"
	"strings"
)

// RequireHost retorna um middleware que aceita somente requisições cujo
// header Host seja, exatamente (após lower-case), "127.0.0.1:<port>"
// ou "localhost:<port>". Qualquer outro valor resulta em 403.
//
// Este é o middleware que efetivamente barra DNS rebinding (THR-002):
// ver a explicação da ameaça em doc.go. Checar o IP de origem da
// conexão não ajuda aqui — a conexão chega de loopback tanto numa
// chamada legítima quanto num ataque de rebinding. O que muda é o
// header Host, que no ataque carrega o domínio do atacante.
//
// Decisões documentadas (as "armadilhas" do enunciado):
//
//   - Host sem porta (ex.: "127.0.0.1", "localhost") é rejeitado. Um
//     navegador só omite a porta no header Host quando ela é a porta
//     padrão do esquema (80 para http, 443 para https); a Local API
//     nunca escuta nessas portas, então uma chamada legítima do
//     dashboard sempre inclui a porta explicitamente. Host sem porta
//     não bate com nenhuma entrada do allowlist e cai em 403.
//   - Host IPv6 de loopback ("[::1]:<port>") é rejeitado. guard.Listen
//     bina somente em 127.0.0.1 (IPv4); o allowlist deste middleware
//     também cobre só as duas formas citadas no enunciado
//     ("127.0.0.1:<port>" e "localhost:<port>"). Ampliar para IPv6
//     seria superfície extra sem necessidade real hoje.
//   - Porta errada (ex.: "127.0.0.1:<outra porta>") é rejeitada pelo
//     mesmo motivo: não bate com a entrada exata do allowlist.
//   - A comparação é por igualdade exata de string (após lower-case),
//     não por parsing de host/porta. Host com espaços ou caracteres
//     estranhos simplesmente não bate com nenhuma entrada válida —
//     parsing tolerante aqui só criaria uma nova superfície de bug.
func RequireHost(port int) func(http.Handler) http.Handler {
	allowed := map[string]struct{}{
		fmt.Sprintf("127.0.0.1:%d", port): {},
		fmt.Sprintf("localhost:%d", port): {},
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := strings.ToLower(r.Host)
			if _, ok := allowed[host]; !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
