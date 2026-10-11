package guard

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// tokenBytes é o tamanho, em bytes, do token gerado por NewToken. 32
// bytes (256 bits) codificados em hex resultam numa string de 64
// caracteres — suficiente para tornar força bruta inviável.
const tokenBytes = 32

// NewToken gera um token aleatório criptograficamente seguro (via
// crypto/rand) para autenticar chamadas à Local API.
//
// O token só existe em memória: cada start do Agent chama NewToken uma
// única vez e mantém o valor pelo tempo de vida do processo. Nunca é
// persistido em disco/banco nem escrito em log (ver
// docs/ai-spec/09-security-privacy.md, "Logging proibido"). O
// dashboard local também mantém o token só em memória, conforme a
// mesma spec.
func NewToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("guard: falha ao gerar token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

const bearerPrefix = "Bearer "

// RequireToken retorna um middleware que exige o header
// "Authorization: Bearer <token>" com exatamente o token esperado
// (tipicamente o valor retornado por NewToken no start do Agent).
//
// A comparação do token é feita em tempo constante via
// crypto/subtle.ConstantTimeCompare — comparar com == vazaria o token
// por timing, byte a byte, para um atacante capaz de medir a latência
// de muitas tentativas.
//
// Requisição sem o header, ou com o token errado, resulta em 401. Este
// middleware nunca escreve o header Authorization nem o token, certo
// ou errado, em nenhuma saída (log, erro de resposta, etc.).
func RequireToken(expected string) func(http.Handler) http.Handler {
	want := []byte(expected)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			got, ok := strings.CutPrefix(header, bearerPrefix)
			if !ok || len(got) != len(want) || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
