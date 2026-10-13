package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Códigos de erro usados pelas respostas deste pacote, no formato de
// docs/ai-spec/06-api-contract.md ("Convenções" → bloco de erro). Espelha
// a mesma convenção de internal/agent/localapi/httpx.go — os dois
// pacotes não compartilham código porque um é a API local (sem sessão) e
// o outro é a API cloud (com sessão), mas o formato de erro do contrato
// é o mesmo para ambos.
const (
	codeBadRequest   = "BAD_REQUEST"
	codeUnauthorized = "UNAUTHORIZED"
	codeForbidden    = "FORBIDDEN"
	codeNotFound     = "NOT_FOUND"
	codeConflict     = "CONFLICT"
	codeValidation   = "VALIDATION"
	codeTooLarge     = "PAYLOAD_TOO_LARGE"
	codeInternal     = "INTERNAL"
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

// writeError escreve uma resposta de erro no formato do contrato. Nunca
// inclui o corpo da requisição, cabeçalhos de autenticação nem qualquer
// dado sensível na mensagem — só texto estático ou nomes de campo (ver
// docs/ai-spec/09-security-privacy.md, "Logging proibido"; este pacote,
// além disso, nunca loga nada por padrão).
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

// maxJSONBodyBytes limita o corpo das requisições JSON deste pacote.
// [ASSUMPTION] Nenhum documento fixa um número; 1 MiB é generoso para
// qualquer payload de sync/CRUD deste contrato (o áudio do ditado do celular
// tem limite próprio, em ditar.go) e evita que um corpo gigante prenda um
// handler.
const maxJSONBodyBytes = 1 << 20

// FraseDoCorpoInvalido é a resposta 400 a QUALQUER erro de decodificação do
// corpo em decodeJSON/decodeJSONLimit (JSON quebrado, tipo errado, corpo
// grande demais). É sempre a mesma: a mensagem do decodificador traz o nome
// de um tipo interno do Go (authStartRequest.volta) ou um caractere do próprio
// pedido, e o corpo é de quem pede (o POST /auth/start é público). O mesmo
// critério do decodeEstrito do pareamento e das chaves de IA. Exportada para
// o teste conferir que é ela, e só ela.
const FraseDoCorpoInvalido = "corpo da requisição inválido"

// decodeJSON decodifica o corpo da requisição em T. Em caso de erro,
// já escreve a resposta 400 no formato do contrato e devolve ok=false —
// o chamador só precisa checar ok e retornar.
//
// T nunca deve declarar um campo UserID/user_id: é assim que "user_id
// sempre vem da sessão autenticada, nunca do payload" (NFR-012, AC-035,
// THR-004) fica estrutural em vez de disciplina — não há campo para o
// encoding/json preencher, então um user_id no corpo da requisição é
// silenciosamente ignorado, não lido por engano por um handler futuro.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	return decodeJSONLimit[T](w, r, maxJSONBodyBytes)
}

// decodeJSONLimit é decodeJSON com outro teto para o corpo (o login e o
// "apagar meus dados" aceitam só 1 KiB).
func decodeJSONLimit[T any](w http.ResponseWriter, r *http.Request, limit int64) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&v); err != nil {
		if err == io.EOF {
			// Corpo vazio: trata como zero value (cada rota confere o que
			// precisa do valor depois).
			return v, true
		}
		writeError(w, http.StatusBadRequest, codeBadRequest, FraseDoCorpoInvalido)
		var zero T
		return zero, false
	}
	return v, true
}
