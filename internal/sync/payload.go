package syncengine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O payload do push (ADR-019, "Decisões da F2"; a F7a confirma):
//
//   - criar (server_version nulo, base_version 0): o ditado inteiro, só com
//     os campos da lista fechada do servidor. Nunca target_app (é contexto, e
//     o Agent nem o grava), áudio, contexto, id ou device_id;
//   - mudar (base_version = server_version): só o que o marcador diz que
//     mudou, entre final_text e deleted_at (null é restaurar), e a escolha do
//     selo feita no PC (resolver_conflito e conflict_at, F7c). O texto é
//     lido da linha agora, nunca da fila.
//
// Os UUIDs vão em minúsculas, como a nuvem os devolve.

// payloadCriar é o ditado inteiro, na lista fechada do servidor.
type payloadCriar struct {
	SourcePlatform string   `json:"source_platform"`
	RawText        string   `json:"raw_text"`
	FinalText      string   `json:"final_text"`
	Mode           string   `json:"mode"`
	Language       *string  `json:"language,omitempty"`
	ASRProvider    string   `json:"asr_provider"`
	ASRModel       *string  `json:"asr_model,omitempty"`
	DurationMs     *int64   `json:"duration_ms,omitempty"`
	ProcessingMs   *int64   `json:"processing_ms,omitempty"`
	Confidence     *float64 `json:"confidence,omitempty"`
	Status         string   `json:"status"`
	CreatedAt      string   `json:"created_at"`
	DeletedAt      *string  `json:"deleted_at,omitempty"`
}

// formatoDaNuvem é como as datas vão no payload (RFC 3339, em UTC). A nuvem
// guarda com microssegundos.
const formatoDaNuvem = time.RFC3339Nano

func dataDaNuvem(t time.Time) string { return t.UTC().Format(formatoDaNuvem) }

// MutacaoDe monta a mutação do marcador, com o mutationID dado (o do envio em
// curso, num reenvio; um novo, senão).
func MutacaoDe(it storage.ItemDaFila, mutationID string) (Mutation, error) {
	l := it.Linha
	m := Mutation{
		MutationID: strings.ToLower(mutationID),
		EntityType: storage.EntidadeDitado,
		EntityID:   strings.ToLower(l.ID),
		Operation:  OpUpsert,
	}
	var corpo any
	if l.ServerVersion == nil {
		c := payloadCriar{
			SourcePlatform: l.SourcePlatform, RawText: l.RawText, FinalText: l.FinalText,
			Mode: l.Mode, Language: l.Language, ASRProvider: l.ASRProvider, ASRModel: l.ASRModel,
			DurationMs: l.DurationMs, ProcessingMs: l.ProcessingMs, Confidence: l.Confidence,
			Status: l.Status, CreatedAt: dataDaNuvem(l.CreatedAt),
		}
		if l.DeletedAt != nil {
			d := dataDaNuvem(*l.DeletedAt)
			c.DeletedAt = &d
		}
		corpo = c
	} else {
		m.BaseVersion = *l.ServerVersion
		campos := it.Marcador.Campos
		escolha := it.Marcador.SeloEscolha
		if campos&(storage.CampoTexto|storage.CampoLixeira) == 0 && escolha == "" {
			// Um marcador sem campo nem escolha sobre um ditado que a nuvem
			// já tem (não deveria acontecer: só o ditado novo nasce sem
			// campo). Os dois campos com a base certa não mudam nada que a
			// nuvem não tenha.
			campos = storage.CampoTexto | storage.CampoLixeira
		}
		p := map[string]any{}
		if escolha != "" {
			// A escolha do selo feita no PC (F7c), com o selo escolhido: a
			// nuvem só a aplica sobre o mesmo selo. O texto não vai junto
			// por causa dela.
			p["resolver_conflito"] = escolha
			p["conflict_at"] = dataDaNuvem(it.Marcador.SeloEm)
		}
		if campos&storage.CampoTexto != 0 {
			p["final_text"] = l.FinalText
		}
		if campos&storage.CampoLixeira != 0 {
			if l.DeletedAt != nil {
				p["deleted_at"] = dataDaNuvem(*l.DeletedAt)
			} else {
				p["deleted_at"] = nil
			}
		}
		corpo = p
	}
	raw, err := json.Marshal(corpo)
	if err != nil {
		return Mutation{}, fmt.Errorf("sync: montando o payload: %w", err)
	}
	m.Payload = raw
	return m, nil
}

// Os tetos do lote do push (ADR-019, "Decisões da F2": até 500 mutações e 1
// MiB de corpo no servidor). O PC fica abaixo do corpo do servidor com
// folga: o JSON da mutação, fora o payload, é pequeno.
const (
	MaxMutacoesPorLote = 500
	MaxBytesPorLote    = 512 << 10
)

// CortarLote diz quantas das mutações (na ordem) cabem num lote: até
// MaxMutacoesPorLote e até MaxBytesPorLote de payload somado, sempre pelo
// menos uma (um ditado sozinho tem no máximo 128 KiB de texto).
func CortarLote(muts []Mutation) int {
	usados := 0
	for i, m := range muts {
		if i == MaxMutacoesPorLote {
			return i
		}
		usados += len(m.Payload)
		if usados > MaxBytesPorLote && i > 0 {
			return i
		}
	}
	return len(muts)
}
