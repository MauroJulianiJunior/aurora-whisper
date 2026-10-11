package cloudstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// O payload do push tem uma lista FECHADA de campos por tipo (ADR-019,
// "Contrato de sync"): campo fora dela é rejected (campo_desconhecido). É
// assim que target_app, áudio e contexto nunca entram, nem por engano de um
// cliente.
//
// Dois formatos para o ditado:
//
//   - criar (base_version 0): o ditado inteiro, com os campos de
//     createFields. device_id não vem no payload: é o aparelho do token.
//   - mudar (base_version >= 1): só o que o PC mudou desde a base, entre
//     final_text e deleted_at (patchFields). É o PC que diz o que mudou,
//     porque o servidor não guarda o estado de cada versão antiga, e sem isso
//     não saberia separar "o PC mudou o texto" de "o PC tem o texto velho".
//     E, desde a F7c, a escolha do selo feita no PC: resolver_conflito
//     ("manter" ou "trocar") com o conflict_at do selo escolhido, os dois
//     juntos, com ou sem final_text e deleted_at (ver DecidePush).

// createFields é a lista fechada do ditado novo (true = obrigatório).
var createFields = map[string]bool{
	"source_platform": true,
	"raw_text":        true,
	"final_text":      true,
	"mode":            true,
	"language":        false,
	"asr_provider":    true,
	"asr_model":       false,
	"duration_ms":     false,
	"processing_ms":   false,
	"confidence":      false,
	"status":          true,
	"created_at":      true,
	"deleted_at":      false,
}

// patchFields é a lista fechada da mudança de um ditado que já existe.
var patchFields = map[string]bool{
	"final_text":        false,
	"deleted_at":        false,
	"resolver_conflito": false,
	"conflict_at":       false,
}

// Tamanhos máximos dos campos curtos (em bytes). [ASSUMPTION] Nenhum
// documento os fixa; servem para um cliente com defeito não gravar lixo
// grande em campos de metadado. O texto ditado tem o seu, MaxTextBytes.
const (
	maxSourcePlatformLen = 32
	maxLanguageLen       = 35 // a maior etiqueta BCP 47 comum
	maxASRProviderLen    = 100
	maxASRModelLen       = 200
)

var validModes = map[string]bool{"literal": true, "smart": true, "polish": true}
var validStatus = map[string]bool{"done": true, "error": true}

// patch é o que o PC mudou num ditado, sobre a base.
type patch struct {
	hasText  bool
	text     string
	hasTrash bool
	// trash é o novo deleted_at; nulo é "restaurar".
	trash *time.Time
	// hasChoice: o PC escolheu uma versão do selo (F7c). choice é
	// EscolhaManter ou EscolhaTrocar, e choiceAt, o conflict_at do selo que
	// ele viu e escolheu.
	hasChoice bool
	choice    string
	choiceAt  time.Time
}

// decodeObject lê o payload como objeto JSON e confere a lista fechada.
// Devolve o código de recusa, ou "".
func decodeObject(raw json.RawMessage, allowed map[string]bool) (map[string]json.RawMessage, string) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, CodeInvalidField
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, CodeInvalidField
	}
	for k := range obj {
		if _, ok := allowed[k]; !ok {
			return nil, CodeUnknownField
		}
	}
	for k, required := range allowed {
		if _, ok := obj[k]; required && !ok {
			return nil, CodeInvalidField
		}
	}
	return obj, ""
}

func isNull(v json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

// cleanString confere um texto: string JSON, sem o caractere nulo (o
// Postgres não o guarda) e até max bytes (0 = sem teto).
func cleanString(v json.RawMessage, max int) (string, bool) {
	if isNull(v) {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	if strings.ContainsRune(s, 0) || !utf8.ValidString(s) {
		return "", false
	}
	if max > 0 && len(s) > max {
		return "", false
	}
	return s, true
}

func requiredText(obj map[string]json.RawMessage, key string, max int, nonEmpty bool) (string, bool) {
	s, ok := cleanString(obj[key], max)
	if !ok || nonEmpty && strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

// optionalText: ausente ou null vira nil.
func optionalText(obj map[string]json.RawMessage, key string, max int) (*string, bool) {
	v, present := obj[key]
	if !present || isNull(v) {
		return nil, true
	}
	s, ok := cleanString(v, max)
	if !ok {
		return nil, false
	}
	return &s, true
}

// optionalMillis: inteiro de 0 a 2^31-1 (a coluna é INTEGER).
func optionalMillis(obj map[string]json.RawMessage, key string) (*int64, bool) {
	v, present := obj[key]
	if !present || isNull(v) {
		return nil, true
	}
	var n int64
	if err := json.Unmarshal(v, &n); err != nil {
		return nil, false
	}
	if n < 0 || n > math.MaxInt32 {
		return nil, false
	}
	return &n, true
}

func optionalConfidence(obj map[string]json.RawMessage) (*float32, bool) {
	v, present := obj["confidence"]
	if !present || isNull(v) {
		return nil, true
	}
	var f float64
	if err := json.Unmarshal(v, &f); err != nil {
		return nil, false
	}
	if math.IsNaN(f) || f < 0 || f > 1 {
		return nil, false
	}
	c := float32(f)
	return &c, true
}

// parseTime lê uma data RFC3339, em UTC e com a precisão do Postgres
// (microssegundos), para o servidor em memória e o de Postgres guardarem o
// mesmo instante.
func parseTime(v json.RawMessage) (time.Time, bool) {
	if isNull(v) {
		return time.Time{}, false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return Normalize(t), true
}

// optionalTime: ausente ou null vira nil.
func optionalTime(obj map[string]json.RawMessage, key string) (*time.Time, bool) {
	v, present := obj[key]
	if !present || isNull(v) {
		return nil, true
	}
	t, ok := parseTime(v)
	if !ok {
		return nil, false
	}
	return &t, true
}

// Normalize põe um instante em UTC e na precisão do Postgres.
func Normalize(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

// parseCreate lê o payload de um ditado novo. Devolve o ditado (sem id,
// usuário, aparelho nem versões) ou o código de recusa.
func parseCreate(raw json.RawMessage) (Transcription, string) {
	obj, code := decodeObject(raw, createFields)
	if code != "" {
		return Transcription{}, code
	}
	var t Transcription
	var ok bool
	if t.SourcePlatform, ok = requiredText(obj, "source_platform", maxSourcePlatformLen, true); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.RawText, ok = requiredText(obj, "raw_text", MaxTextBytes, false); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.FinalText, ok = requiredText(obj, "final_text", MaxTextBytes, false); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.Mode, ok = requiredText(obj, "mode", 0, false); !ok || !validModes[t.Mode] {
		return Transcription{}, CodeInvalidField
	}
	if t.Language, ok = optionalText(obj, "language", maxLanguageLen); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.ASRProvider, ok = requiredText(obj, "asr_provider", maxASRProviderLen, true); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.ASRModel, ok = optionalText(obj, "asr_model", maxASRModelLen); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.DurationMs, ok = optionalMillis(obj, "duration_ms"); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.ProcessingMs, ok = optionalMillis(obj, "processing_ms"); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.Confidence, ok = optionalConfidence(obj); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.Status, ok = requiredText(obj, "status", 0, false); !ok || !validStatus[t.Status] {
		return Transcription{}, CodeInvalidField
	}
	if t.CreatedAt, ok = parseTime(obj["created_at"]); !ok {
		return Transcription{}, CodeInvalidField
	}
	if t.DeletedAt, ok = optionalTime(obj, "deleted_at"); !ok {
		return Transcription{}, CodeInvalidField
	}
	return t, ""
}

// parsePatch lê a mudança de um ditado que existe: final_text e/ou
// deleted_at (null é restaurar) e/ou a escolha do selo (resolver_conflito com
// conflict_at, os dois juntos). Payload vazio é recusado.
func parsePatch(raw json.RawMessage) (patch, string) {
	obj, code := decodeObject(raw, patchFields)
	if code != "" {
		return patch{}, code
	}
	if len(obj) == 0 {
		return patch{}, CodeInvalidField
	}
	var p patch
	if v, present := obj["final_text"]; present {
		s, ok := cleanString(v, MaxTextBytes)
		if !ok {
			return patch{}, CodeInvalidField
		}
		p.hasText, p.text = true, s
	}
	if _, present := obj["deleted_at"]; present {
		t, ok := optionalTime(obj, "deleted_at")
		if !ok {
			return patch{}, CodeInvalidField
		}
		p.hasTrash, p.trash = true, t
	}
	escolha, temEscolha := obj["resolver_conflito"]
	quando, temQuando := obj["conflict_at"]
	if temEscolha != temQuando {
		return patch{}, CodeInvalidField
	}
	if temEscolha {
		s, ok := cleanString(escolha, 0)
		if !ok || !EscolhaValida(s) {
			return patch{}, CodeInvalidField
		}
		at, ok := parseTime(quando)
		if !ok {
			return patch{}, CodeInvalidField
		}
		p.hasChoice, p.choice, p.choiceAt = true, s, at
	}
	return p, ""
}

// CheckListFilter confere os textos livres da listagem (q e mode): com o
// caractere nulo ou UTF-8 inválido, o Postgres recusa a consulta (SQLSTATE
// 22021) e o servidor em memória responderia uma lista vazia. Os dois Stores
// chamam isto antes de tudo, e o pedido é ErrInvalidFilter (400) nos dois.
func CheckListFilter(f ListFilter) error {
	texts := []string{f.Query}
	if f.Mode != nil {
		texts = append(texts, *f.Mode)
	}
	for _, s := range texts {
		if strings.ContainsRune(s, 0) || !utf8.ValidString(s) {
			return ErrInvalidFilter
		}
	}
	return nil
}

// CanonicalUUID confere que s é um UUID no formato 8-4-4-4-12 e o devolve
// em minúsculas, que é como o Postgres o devolve. ok=false para qualquer
// outra coisa.
func CanonicalUUID(s string) (string, bool) {
	if len(s) != 36 {
		return "", false
	}
	b := []byte(strings.ToLower(s))
	for i, c := range b {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return "", false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return "", false
			}
		}
	}
	return string(b), true
}

// EncodeCursor e DecodeCursor são o cursor opaco da listagem: o instante da
// ordenação (created_at no histórico, deleted_at na lixeira) e o id, para o
// desempate.
func EncodeCursor(at time.Time, id string) string {
	raw := Normalize(at).Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor devolve ErrInvalidCursor para qualquer coisa que
// EncodeCursor não produziria.
func DecodeCursor(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", ErrInvalidCursor
	}
	at, id, found := strings.Cut(string(raw), "|")
	if !found {
		return time.Time{}, "", ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", ErrInvalidCursor
	}
	canon, ok := CanonicalUUID(id)
	if !ok {
		return time.Time{}, "", ErrInvalidCursor
	}
	return Normalize(t), canon, nil
}
