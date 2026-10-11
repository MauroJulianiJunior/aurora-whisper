package server

import "net/http"

// handleHealthLive implementa GET /health/live: o processo está de pé.
// Não depende de nenhuma dependência externa — é a checagem de
// liveness, não de readiness.
func handleHealthLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReadyChecker reporta se uma dependência do backend está pronta para
// servir (ex.: pool de conexão com o Postgres real, no Sprint 6). Nesta
// fase, não há dependência externa real — o check default sempre passa.
type ReadyChecker func() error

// handleHealthReady implementa GET /health/ready: roda cada checker e
// devolve 200 só se todos passarem.
func handleHealthReady(checkers ...ReadyChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for _, check := range checkers {
			if err := check(); err != nil {
				writeError(w, http.StatusServiceUnavailable, codeInternal, "dependência indisponível")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
