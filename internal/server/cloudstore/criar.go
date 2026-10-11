package cloudstore

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"
)

// O ditado que nasce na nuvem (ADR-021, C1): o do celular. Nenhum PC o
// criou, então ele não passa pelo push: o Store.Create o grava pelo ponto
// único de gravação, com a regra de criar do push (decideCreate) e a validação
// do payload dele (parseCreate). Este arquivo tem a parte pura: o que o
// servidor sabe de um ditado novo, e a linha que a regra manda gravar. Nada
// aqui toca banco, rede ou log.

// As origens (source_platform) de um ditado que nasce na nuvem. São uma lista
// fechada porque o PC se apoia nela: o que chega com uma destas não nasceu
// nele (internal/storage, "nasceu aqui"). O PC grava "windows".
const (
	// SourcePlatformWeb é o app do celular (o PWA).
	SourcePlatformWeb = "web"
	// SourcePlatformAndroid é o app nativo do passo 2.
	SourcePlatformAndroid = "android"
)

// NewDictation é o que o servidor sabe de um ditado que ele mesmo transcreveu:
// os campos do payload de criar do push, menos os que o Store decide (o id, o
// usuário, o aparelho, as versões e as datas). A data do ditado é a da
// gravação, do relógio do servidor; o status é sempre done; e não há lixeira.
// Não tem áudio, contexto nem target_app: não existe campo para isso.
type NewDictation struct {
	// SourcePlatform é SourcePlatformWeb ou SourcePlatformAndroid.
	SourcePlatform string
	RawText        string
	FinalText      string
	// Mode diz o que valeu de fato: polish se o Polish foi aplicado, senão smart.
	Mode        string
	Language    *string
	ASRProvider string
	ASRModel    *string
	// DurationMs e ProcessingMs: a duração do áudio e o tempo do servidor.
	DurationMs   *int64
	ProcessingMs *int64
	Confidence   *float32
}

// IsCloudPlatform diz se p é uma origem de ditado que nasce na nuvem.
func IsCloudPlatform(p string) bool {
	return p == SourcePlatformWeb || p == SourcePlatformAndroid
}

// payload é o NewDictation como o payload de criar do push o carrega, com a
// data do ditado em now. É por ele que a validação é a mesma do push, e não
// uma cópia dela: se um campo entrar na lista fechada do ditado
// (createFields), a primeira chamada falha em vez de gravar sem ele.
func (n NewDictation) payload(now time.Time) (json.RawMessage, error) {
	// O json.Marshal troca o UTF-8 inválido por U+FFFD em silêncio, e o texto
	// gravado seria outro que o recebido. O push recusa (cleanString).
	for _, s := range []string{n.SourcePlatform, n.RawText, n.FinalText, n.Mode, n.ASRProvider} {
		if !utf8.ValidString(s) {
			return nil, fmt.Errorf("%w: %s", ErrInvalidDictation, CodeInvalidField)
		}
	}
	for _, s := range []*string{n.Language, n.ASRModel} {
		if s != nil && !utf8.ValidString(*s) {
			return nil, fmt.Errorf("%w: %s", ErrInvalidDictation, CodeInvalidField)
		}
	}
	raw, err := json.Marshal(map[string]any{
		"source_platform": n.SourcePlatform,
		"raw_text":        n.RawText,
		"final_text":      n.FinalText,
		"mode":            n.Mode,
		"language":        n.Language,
		"asr_provider":    n.ASRProvider,
		"asr_model":       n.ASRModel,
		"duration_ms":     n.DurationMs,
		"processing_ms":   n.ProcessingMs,
		"confidence":      n.Confidence,
		"status":          "done",
		"created_at":      now.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		// Só um número que não vira JSON (NaN, infinito) chega aqui.
		return nil, fmt.Errorf("%w: %s", ErrInvalidDictation, CodeInvalidField)
	}
	return raw, nil
}

// NewTranscription é a regra de criar do Store.Create, pura: valida o ditado
// novo como o push valida o payload de criar e devolve a linha a gravar (a
// versão 1, sem aparelho, com a data now), ou o erro. Os dois Stores a chamam
// antes de abrir a transação, então um ditado inválido nem trava o contador
// nem gasta seq. O LastSeq é do Store.
//
// Erros: ErrDeviceNotAllowed (o Actor tem aparelho), ErrInvalidID,
// ErrInvalidDictation (a origem não é da nuvem, ou o payload não vale).
func NewTranscription(a Actor, id string, n NewDictation, now time.Time) (Transcription, error) {
	if a.DeviceID != "" {
		return Transcription{}, ErrDeviceNotAllowed
	}
	canon, ok := CanonicalUUID(id)
	if !ok {
		return Transcription{}, ErrInvalidID
	}
	if !IsCloudPlatform(n.SourcePlatform) {
		return Transcription{}, fmt.Errorf("%w: %s", ErrInvalidDictation, CodeInvalidField)
	}
	now = Normalize(now)
	raw, err := n.payload(now)
	if err != nil {
		return Transcription{}, err
	}
	out := decideCreate(Mutation{EntityID: canon, Payload: raw}, a, now)
	if out.Write != WriteInsert {
		// Rejected (o payload não vale) ou gone (a data fora da janela): com a
		// data now não acontece, e nos dois casos o motivo é de uma lista
		// fechada.
		why := out.Result.Code
		if why == "" {
			why = out.Result.Reason
		}
		return Transcription{}, fmt.Errorf("%w: %s", ErrInvalidDictation, why)
	}
	return out.Next, nil
}
