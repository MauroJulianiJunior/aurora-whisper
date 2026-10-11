package server

import "net/http"

// meResponse é o corpo de GET /me — o perfil do usuário autenticado,
// espelhando o Principal resolvido pela sessão. Nunca inclui token,
// senha ou qualquer segredo (docs/ai-spec/09-security-privacy.md,
// "Logging proibido" — o mesmo raciocínio vale para o que expomos de
// volta ao próprio dono, mas por clareza de contrato, não por sigilo).
type meResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
}

// handleMe implementa GET /me: devolve o perfil da sessão corrente. Não
// há parâmetro de rota nem corpo — o único dado de entrada é o
// Principal já resolvido pelo middleware requireSession.
func handleMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "autenticação obrigatória")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID:          principal.UserID,
		Email:       principal.Email,
		DisplayName: principal.DisplayName,
	})
}
