package syncengine

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

func chavesDe(t *testing.T, raw json.RawMessage) (map[string]any, []string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("payload não é objeto: %s", raw)
	}
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return m, ks
}

func linhaDoPayload() storage.Transcription {
	app := "chrome"
	modelo := "small-q4_0"
	dur, proc := int64(3200), int64(410)
	return storage.Transcription{
		ID: strings.ToUpper(idDoTeste), UserID: "dono", SourcePlatform: "windows", RawText: "bruto", FinalText: "final",
		Mode: "smart", ASRProvider: "whisper-local", ASRModel: &modelo, DurationMs: &dur, ProcessingMs: &proc,
		TargetApp: &app, Status: "done",
		CreatedAt: time.Date(2026, 10, 6, 9, 0, 0, 123456789, time.FixedZone("BRT", -3*3600)),
		UpdatedAt: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC), Version: 3, SyncState: storage.SituacaoNormal,
	}
}

// Criar (sem server_version): o ditado inteiro, só a lista fechada do
// servidor, nunca target_app, id, device_id, versões ou o selo; base 0; o id em
// minúsculas; a data em UTC.
func TestMutacaoDe_CriarSobeInteiro(t *testing.T) {
	l := linhaDoPayload()
	m, err := MutacaoDe(storage.ItemDaFila{Marcador: storage.Marcador{Campos: storage.CampoTexto}, Linha: l}, "M-1")
	if err != nil {
		t.Fatal(err)
	}
	if m.BaseVersion != 0 || m.EntityID != idDoTeste || m.MutationID != "m-1" || m.Operation != OpUpsert || m.EntityType != storage.EntidadeDitado {
		t.Errorf("mutação: %+v", m)
	}
	p, ks := chavesDe(t, m.Payload)
	if got := strings.Join(ks, ","); got != "asr_model,asr_provider,created_at,duration_ms,final_text,mode,processing_ms,raw_text,source_platform,status" {
		t.Errorf("campos: %s", got)
	}
	if p["created_at"] != "2026-10-06T12:00:00.123456789Z" || p["final_text"] != "final" {
		t.Errorf("payload: %v", p)
	}

	// Na lixeira, o deleted_at vai junto.
	apagado := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	l.DeletedAt = &apagado
	m, _ = MutacaoDe(storage.ItemDaFila{Linha: l}, "m-2")
	if p, _ := chavesDe(t, m.Payload); p["deleted_at"] != "2026-10-06T10:00:00Z" {
		t.Errorf("deleted_at: %v", p["deleted_at"])
	}
}

// Mudar (com server_version): base = server_version, e só os campos do
// marcador; restaurar é deleted_at null.
func TestMutacaoDe_MudarSoOsCampos(t *testing.T) {
	apagado := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		nome     string
		campos   int
		lixeira  *time.Time
		querChav string
		quero    map[string]any
	}{
		{"texto", storage.CampoTexto, nil, "final_text", map[string]any{"final_text": "final"}},
		{"lixeira", storage.CampoLixeira, &apagado, "deleted_at", map[string]any{"deleted_at": "2026-10-06T10:00:00Z"}},
		{"restaurar", storage.CampoLixeira, nil, "deleted_at", map[string]any{"deleted_at": nil}},
		{"os dois", storage.CampoTexto | storage.CampoLixeira, &apagado, "deleted_at,final_text",
			map[string]any{"final_text": "final", "deleted_at": "2026-10-06T10:00:00Z"}},
		{"sem campo: os dois, com a base certa", 0, nil, "deleted_at,final_text",
			map[string]any{"final_text": "final", "deleted_at": nil}},
	} {
		t.Run(c.nome, func(t *testing.T) {
			l := linhaDoPayload()
			l.ServerVersion = i64(7)
			l.DeletedAt = c.lixeira
			m, err := MutacaoDe(storage.ItemDaFila{Marcador: storage.Marcador{Campos: c.campos}, Linha: l}, "m-1")
			if err != nil {
				t.Fatal(err)
			}
			if m.BaseVersion != 7 {
				t.Errorf("base = %d", m.BaseVersion)
			}
			p, ks := chavesDe(t, m.Payload)
			if strings.Join(ks, ",") != c.querChav || !reflect.DeepEqual(p, c.quero) {
				t.Errorf("payload = %v; quero %v", p, c.quero)
			}
		})
	}
}

// O corte do lote: até 500 mutações e 512 KiB de payload, sempre pelo menos
// uma.
func TestCortarLote(t *testing.T) {
	de := func(n, tam int) []Mutation {
		ms := make([]Mutation, n)
		for i := range ms {
			ms[i].Payload = make(json.RawMessage, tam)
		}
		return ms
	}
	for _, c := range []struct {
		nome  string
		muts  []Mutation
		quero int
	}{
		{"vazio", nil, 0},
		{"poucas", de(3, 100), 3},
		{"o teto de número", de(MaxMutacoesPorLote+20, 10), MaxMutacoesPorLote},
		{"o teto de bytes", de(10, 100<<10), 5},
		{"uma grande sozinha passa", de(3, MaxBytesPorLote+1), 1},
		{"exatamente no teto", de(4, MaxBytesPorLote/4), 4},
	} {
		if got := CortarLote(c.muts); got != c.quero {
			t.Errorf("%s: %d; quero %d", c.nome, got, c.quero)
		}
	}
}
