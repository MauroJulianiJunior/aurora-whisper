package localapi

import (
	"encoding/json"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Códigos de erro usados pelas respostas deste pacote, no formato de
// docs/ai-spec/06-api-contract.md.
const (
	codeBadRequest = "BAD_REQUEST"
	codeNotFound   = "NOT_FOUND"
	codeConflict   = "CONFLICT"
	codeInternal   = "INTERNAL"
	// codeUnavailable é "não agora, e não por erro do pedido": o Agent não
	// tem o dicionário em que o aprendizado grava (ausente, inválido, de
	// outro dono) ou não montou o aprendizado.
	codeUnavailable = "UNAVAILABLE"
)

// apiError é o corpo de "error" na resposta JSON de erro.
type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

// writeError escreve uma resposta de erro no formato do contrato,
// gerando um request_id novo (via storage.NewID, o mesmo gerador de
// UUID v7 usado no resto do sistema) para facilitar correlação em
// relatos do usuário, sem que ele precise aparecer em nenhum log real
// (este pacote não loga nada por padrão — ver docs/ai-spec/09-security-privacy.md,
// "Logging proibido").
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: apiError{
		Code:      code,
		Message:   message,
		RequestID: storage.NewID(),
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
