package cloudstore

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Testes puros das regras do push e do CRUD: nada de banco nem de HTTP. A
// suíte de contrato (internal/server/contracttest) prova as mesmas regras
// pela API, contra o servidor em memória e contra o Postgres.

var testNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

const (
	testEntity   = "0192a000-0000-7000-8000-000000000001"
	testMutation = "0192a000-0000-7000-8000-0000000000a1"
	testDevice   = "0192a000-0000-7000-8000-0000000000d1"
	testUser     = "0192a000-0000-7000-8000-0000000000e1"
)

var pc = Actor{UserID: testUser, DeviceID: testDevice}

// createPayload é um ditado válido, com as trocas pedidas (nil apaga a
// chave).
func createPayload(t *testing.T, changes map[string]any) json.RawMessage {
	t.Helper()
	p := map[string]any{
		"source_platform": "windows",
		"raw_text":        "texto bruto do ditado",
		"final_text":      "Texto final do ditado.",
		"mode":            "smart",
		"language":        "pt-BR",
		"asr_provider":    "whisper-local",
		"asr_model":       "small-q4_0",
		"duration_ms":     3200,
		"processing_ms":   410,
		"confidence":      0.91,
		"status":          "done",
		"created_at":      testNow.Add(-time.Hour).Format(time.RFC3339Nano),
	}
	for k, v := range changes {
		if v == nil {
			delete(p, k)
			continue
		}
		p[k] = v
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mutation(base int64, payload json.RawMessage) Mutation {
	return Mutation{
		MutationID: testMutation, EntityType: EntityTranscription, EntityID: testEntity,
		Operation: OpUpsert, BaseVersion: base, Payload: payload,
	}
}

// existing monta um ditado que já está no servidor, na versão v, com o
// texto mudado por último na versão textV e a lixeira na trashV.
func existing(v, textV, trashV int64, text string, trashed bool) *Transcription {
	created := testNow.Add(-24 * time.Hour)
	t := &Transcription{
		ID: testEntity, UserID: testUser, SourcePlatform: "windows",
		RawText: "bruto", FinalText: text, Mode: "smart", ASRProvider: "whisper-local",
		Status: "done", CreatedAt: created, UpdatedAt: created,
		Version: v, TextVersion: textV, TrashVersion: trashV, LastSeq: 9,
	}
	if trashed {
		d := testNow.Add(-time.Hour)
		t.DeletedAt = &d
		t.PurgeAt = purgeAt(created, &d, d)
	}
	return t
}

// otherDevice é outro PC do mesmo usuário.
const otherDevice = "0192a000-0000-7000-8000-0000000000d2"

// by marca o aparelho que mudou por último o texto e a lixeira ("" = o
// celular, nulo).
func by(t *Transcription, textDev, trashDev string) *Transcription {
	ptr := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	t.TextDeviceID, t.TrashDeviceID = ptr(textDev), ptr(trashDev)
	return t
}

func trashAt(ts time.Time) string { return ts.Format(time.RFC3339Nano) }

func TestCheckMutation(t *testing.T) {
	ok := mutation(0, nil)
	cases := []struct {
		name       string
		change     func(m *Mutation)
		code       string
		recordable bool
	}{
		{"válida", func(*Mutation) {}, "", true},
		{"mutation_id que não é UUID", func(m *Mutation) { m.MutationID = "mut-1" }, CodeInvalidMutation, false},
		{"mutation_id vazio", func(m *Mutation) { m.MutationID = "" }, CodeInvalidMutation, false},
		{"entity_id que não é UUID", func(m *Mutation) { m.EntityID = "t1" }, CodeInvalidMutation, false},
		{"entity_type vazio", func(m *Mutation) { m.EntityType = "" }, CodeInvalidMutation, false},
		{"entity_type enorme", func(m *Mutation) { m.EntityType = strings.Repeat("x", 33) }, CodeInvalidMutation, false},
		{"entity_type com nulo", func(m *Mutation) { m.EntityType = "trans\x00" }, CodeInvalidMutation, false},
		// Antes da F8 era tipo_ainda_nao_sincroniza: desde a F8b, o learned
		// segue (o payload é de DecideLearned).
		{"learned sincroniza desde a F8", func(m *Mutation) { m.EntityType = EntityLearned }, "", true},
		{"tipo desconhecido", func(m *Mutation) { m.EntityType = "dictionary_entry" }, CodeUnknownType, true},
		{"operação desconhecida", func(m *Mutation) { m.Operation = "patch" }, CodeInvalidOperation, true},
		{"operação desconhecida do learned", func(m *Mutation) { m.EntityType, m.Operation = EntityLearned, "patch" }, CodeInvalidOperation, true},
		{"operação vazia do learned", func(m *Mutation) { m.EntityType, m.Operation = EntityLearned, "" }, CodeInvalidOperation, true},
		{"delete de ditado", func(m *Mutation) { m.Operation = OpDelete }, CodeInvalidOperation, true},
		{"delete do learned", func(m *Mutation) { m.EntityType, m.Operation = EntityLearned, OpDelete }, "", true},
		{"base negativa", func(m *Mutation) { m.BaseVersion = -1 }, CodeInvalidBase, true},
		{"base negativa do upsert do learned", func(m *Mutation) { m.EntityType, m.BaseVersion = EntityLearned, -1 }, CodeInvalidBase, true},
		// O desfazer não olha a base: nunca é base_invalida.
		{"delete do learned com base negativa", func(m *Mutation) {
			m.EntityType, m.Operation, m.BaseVersion = EntityLearned, OpDelete, -1
		}, "", true},
		{"delete de ditado com base negativa", func(m *Mutation) { m.Operation, m.BaseVersion = OpDelete, -1 }, CodeInvalidOperation, true},
		{"UUID em maiúsculas vale", func(m *Mutation) { m.EntityID = strings.ToUpper(testEntity) }, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := ok
			c.change(&m)
			code, rec := CheckMutation(m)
			if code != c.code || rec != c.recordable {
				t.Errorf("CheckMutation = (%q, %v), quero (%q, %v)", code, rec, c.code, c.recordable)
			}
		})
	}
}

func TestDecidePush_CriaODitado(t *testing.T) {
	m := mutation(0, createPayload(t, nil))
	m.EntityID = strings.ToUpper(testEntity)
	out := DecidePush(m, nil, false, pc, testNow)

	if out.Result.Status != StatusApplied || out.Result.EntityVersion != 1 || out.Result.Merged {
		t.Fatalf("resultado = %+v, quero applied v1 sem mescla", out.Result)
	}
	if out.Write != WriteInsert {
		t.Fatalf("Write = %v, quero WriteInsert", out.Write)
	}
	n := out.Next
	if n.ID != testEntity {
		t.Errorf("ID = %q, quero o UUID em minúsculas %q", n.ID, testEntity)
	}
	if n.UserID != testUser || n.DeviceID == nil || *n.DeviceID != testDevice {
		t.Errorf("dono/aparelho = %q/%v, quero o do Actor", n.UserID, n.DeviceID)
	}
	if n.Version != 1 || n.TextVersion != 1 || n.TrashVersion != 1 {
		t.Errorf("versões = %d/%d/%d, quero 1/1/1", n.Version, n.TextVersion, n.TrashVersion)
	}
	if n.FinalText != "Texto final do ditado." || n.RawText != "texto bruto do ditado" || n.Mode != "smart" || n.Status != "done" {
		t.Errorf("campos do ditado = %+v", n)
	}
	if !n.CreatedAt.Equal(testNow.Add(-time.Hour)) || !n.UpdatedAt.Equal(testNow) {
		t.Errorf("datas = %v/%v, quero created_at do PC e updated_at do servidor", n.CreatedAt, n.UpdatedAt)
	}
	if n.Confidence == nil || *n.Confidence != float32(0.91) || n.DurationMs == nil || *n.DurationMs != 3200 {
		t.Errorf("metadados do ASR = %v/%v", n.Confidence, n.DurationMs)
	}
	if n.DeletedAt != nil || n.PurgeAt != nil || n.Conflict != nil {
		t.Errorf("ditado novo com lixeira ou selo: %+v", n)
	}
}

func TestDecidePush_CriaNaLixeiraComPrazoDeExpurgo(t *testing.T) {
	created := testNow.AddDate(0, -6, 10) // faltam 10 dias para os 6 meses
	trashed := testNow.Add(-time.Hour)
	m := mutation(0, createPayload(t, map[string]any{
		"created_at": trashAt(created), "deleted_at": trashAt(trashed),
	}))
	out := DecidePush(m, nil, false, pc, testNow)
	if out.Result.Status != StatusApplied || out.Next.DeletedAt == nil {
		t.Fatalf("resultado = %+v, quero applied na lixeira", out.Result)
	}
	// Os 6 meses vencem antes dos 30 dias: vale o que vier primeiro.
	if want := created.AddDate(0, 6, 0); out.Next.PurgeAt == nil || !out.Next.PurgeAt.Equal(want) {
		t.Errorf("purge_at = %v, quero %v (os 6 meses)", out.Next.PurgeAt, want)
	}
}

// AC-042 (a API aceita um futuro Android) e REQ-042: a origem do ditado no
// push é um texto livre de 1 a 32 caracteres, e não uma lista fechada em
// "windows". Esta prova substitui a do servidor em memória da F1-D, que saiu na
// C5c da ADR-021, e roda sobre as regras que o servidor da nuvem usa de
// verdade (a suíte de contrato as exerce pela API, contra a memória e o
// Postgres).
func TestDecidePush_AceitaQualquerOrigemDeTextoLivre(t *testing.T) {
	for _, plataforma := range []string{"windows", "android", "web", "ios", "linux-arm64", "a", strings.Repeat("p", 32)} {
		t.Run(plataforma, func(t *testing.T) {
			out := DecidePush(mutation(0, createPayload(t, map[string]any{"source_platform": plataforma})), nil, false, pc, testNow)
			if out.Result.Status != StatusApplied || out.Write == WriteNone || out.Next.SourcePlatform != plataforma {
				t.Errorf("resultado = %+v (write %v, origem %q), quero applied com a origem %q", out.Result, out.Write, out.Next.SourcePlatform, plataforma)
			}
		})
	}
}

func TestDecidePush_RecusaOPayloadForaDaLista(t *testing.T) {
	cases := []struct {
		name    string
		payload json.RawMessage
		code    string
	}{
		{"target_app", createPayload(t, map[string]any{"target_app": "chrome"}), CodeUnknownField},
		{"áudio", createPayload(t, map[string]any{"audio": "UklGRg=="}), CodeUnknownField},
		{"contexto", createPayload(t, map[string]any{"context": map[string]any{"app": "chrome"}}), CodeUnknownField},
		{"device_id no payload", createPayload(t, map[string]any{"device_id": testDevice}), CodeUnknownField},
		{"id no payload", createPayload(t, map[string]any{"id": testEntity}), CodeUnknownField},
		{"sem raw_text", createPayload(t, map[string]any{"raw_text": nil}), CodeInvalidField},
		{"sem final_text", createPayload(t, map[string]any{"final_text": nil}), CodeInvalidField},
		{"sem created_at", createPayload(t, map[string]any{"created_at": nil}), CodeInvalidField},
		{"sem status", createPayload(t, map[string]any{"status": nil}), CodeInvalidField},
		{"sem mode", createPayload(t, map[string]any{"mode": nil}), CodeInvalidField},
		{"sem asr_provider", createPayload(t, map[string]any{"asr_provider": nil}), CodeInvalidField},
		{"sem source_platform", createPayload(t, map[string]any{"source_platform": nil}), CodeInvalidField},
		{"source_platform vazio", createPayload(t, map[string]any{"source_platform": " "}), CodeInvalidField},
		{"source_platform grande", createPayload(t, map[string]any{"source_platform": strings.Repeat("w", 33)}), CodeInvalidField},
		{"asr_provider vazio", createPayload(t, map[string]any{"asr_provider": ""}), CodeInvalidField},
		{"mode desconhecido", createPayload(t, map[string]any{"mode": "magic"}), CodeInvalidField},
		{"status completed", createPayload(t, map[string]any{"status": "completed"}), CodeInvalidField},
		{"raw_text null", createPayload(t, map[string]any{"raw_text": json.RawMessage("null")}), CodeInvalidField},
		{"final_text número", createPayload(t, map[string]any{"final_text": 7}), CodeInvalidField},
		{"texto com nulo", createPayload(t, map[string]any{"final_text": "a\x00b"}), CodeInvalidField},
		{"confidence acima de 1", createPayload(t, map[string]any{"confidence": 1.01}), CodeInvalidField},
		{"confidence negativa", createPayload(t, map[string]any{"confidence": -0.1}), CodeInvalidField},
		{"duração negativa", createPayload(t, map[string]any{"duration_ms": -1}), CodeInvalidField},
		{"duração fora do INTEGER", createPayload(t, map[string]any{"duration_ms": int64(1) << 31}), CodeInvalidField},
		{"duração fracionária", createPayload(t, map[string]any{"processing_ms": 1.5}), CodeInvalidField},
		{"language grande", createPayload(t, map[string]any{"language": strings.Repeat("p", 36)}), CodeInvalidField},
		{"asr_model grande", createPayload(t, map[string]any{"asr_model": strings.Repeat("m", 201)}), CodeInvalidField},
		{"created_at sem fuso", createPayload(t, map[string]any{"created_at": "2026-10-05T11:00:00"}), CodeInvalidField},
		{"deleted_at inválido", createPayload(t, map[string]any{"deleted_at": "ontem"}), CodeInvalidField},
		{"payload lista", json.RawMessage(`[1,2]`), CodeInvalidField},
		{"payload null", json.RawMessage(`null`), CodeInvalidField},
		{"payload ausente", nil, CodeInvalidField},
		{"JSON quebrado", json.RawMessage(`{"raw_text":`), CodeInvalidField},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := DecidePush(mutation(0, c.payload), nil, false, pc, testNow)
			if out.Result.Status != StatusRejected || out.Result.Code != c.code || out.Write != WriteNone {
				t.Errorf("resultado = %+v (write %v), quero rejected %q sem gravar", out.Result, out.Write, c.code)
			}
		})
	}
	// Os opcionais podem faltar ou vir nulos.
	opt := createPayload(t, map[string]any{
		"language": nil, "asr_model": json.RawMessage("null"), "duration_ms": nil,
		"processing_ms": nil, "confidence": nil, "deleted_at": json.RawMessage("null"),
	})
	if out := DecidePush(mutation(0, opt), nil, false, pc, testNow); out.Result.Status != StatusApplied {
		t.Errorf("sem os opcionais: %+v, quero applied", out.Result)
	}
	// Texto vazio é ditado válido (um ditado com erro pode não ter texto).
	empty := createPayload(t, map[string]any{"raw_text": "", "final_text": "", "status": "error"})
	if out := DecidePush(mutation(0, empty), nil, false, pc, testNow); out.Result.Status != StatusApplied {
		t.Errorf("texto vazio: %+v, quero applied", out.Result)
	}
}

func TestDecidePush_JanelaEFolga(t *testing.T) {
	cases := []struct {
		name    string
		created time.Time
		deleted *time.Time
		status  Status
		detail  string
	}{
		{"dentro da janela por um segundo", testNow.AddDate(0, -6, 0).Add(time.Second), nil, StatusApplied, ""},
		{"exatamente 6 meses", testNow.AddDate(0, -6, 0), nil, StatusGone, ReasonOutOfWindow},
		{"um ano atrás", testNow.AddDate(-1, 0, 0), nil, StatusGone, ReasonOutOfWindow},
		{"no limite da folga", testNow.Add(FutureTolerance), nil, StatusApplied, ""},
		{"além da folga", testNow.Add(FutureTolerance + time.Microsecond), nil, StatusRejected, CodeFutureDate},
	}
	future := testNow.Add(FutureTolerance + time.Second)
	cases = append(cases, struct {
		name    string
		created time.Time
		deleted *time.Time
		status  Status
		detail  string
	}{"lixeira além da folga", testNow.Add(-time.Hour), &future, StatusRejected, CodeFutureDate})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := map[string]any{"created_at": trashAt(c.created)}
			if c.deleted != nil {
				ch["deleted_at"] = trashAt(*c.deleted)
			}
			r := DecidePush(mutation(0, createPayload(t, ch)), nil, false, pc, testNow).Result
			if r.Status != c.status || r.Code+r.Reason != c.detail {
				t.Errorf("resultado = %+v, quero %s %q", r, c.status, c.detail)
			}
		})
	}
}

func TestDecidePush_LapideEhGoneApagado(t *testing.T) {
	for _, base := range []int64{0, 3} {
		out := DecidePush(mutation(base, createPayload(t, nil)), nil, true, pc, testNow)
		if out.Result.Status != StatusGone || out.Result.Reason != ReasonDeleted || out.Write != WriteNone {
			t.Errorf("base %d com lápide: %+v, quero gone apagado sem gravar", base, out.Result)
		}
	}
}

func TestDecidePush_MudancaDeDitadoQueNaoExisteEhGoneForaDaJanela(t *testing.T) {
	out := DecidePush(mutation(2, rawJSON(t, map[string]any{"final_text": "x"})), nil, false, pc, testNow)
	if out.Result.Status != StatusGone || out.Result.Reason != ReasonOutOfWindow || out.Write != WriteNone {
		t.Errorf("resultado = %+v, quero gone fora_da_janela", out.Result)
	}
}

func TestDecidePush_DitadoQueSaiuDaJanelaEhGone(t *testing.T) {
	cur := existing(1, 1, 1, "a", false)
	cur.CreatedAt = testNow.AddDate(0, -6, 0)
	out := DecidePush(mutation(1, rawJSON(t, map[string]any{"final_text": "b"})), cur, false, pc, testNow)
	if out.Result.Status != StatusGone || out.Result.Reason != ReasonOutOfWindow {
		t.Errorf("resultado = %+v, quero gone fora_da_janela", out.Result)
	}
}

func TestDecidePush_BaseDoFuturoEhRecusada(t *testing.T) {
	out := DecidePush(mutation(4, rawJSON(t, map[string]any{"final_text": "b"})), existing(3, 1, 1, "a", false), false, pc, testNow)
	if out.Result.Status != StatusRejected || out.Result.Code != CodeInvalidBase {
		t.Errorf("resultado = %+v, quero rejected base_invalida", out.Result)
	}
}

func TestDecidePush_PatchForaDaLista(t *testing.T) {
	cur := existing(1, 1, 1, "a", false)
	cases := []struct {
		name    string
		payload json.RawMessage
		code    string
	}{
		{"campo imutável", rawJSON(t, map[string]any{"raw_text": "outro"}), CodeUnknownField},
		{"target_app", rawJSON(t, map[string]any{"final_text": "b", "target_app": "x"}), CodeUnknownField},
		{"vazio", json.RawMessage(`{}`), CodeInvalidField},
		{"texto nulo", json.RawMessage(`{"final_text":null}`), CodeInvalidField},
		{"texto com nulo", rawJSON(t, map[string]any{"final_text": "a\x00"}), CodeInvalidField},
		{"lixeira inválida", json.RawMessage(`{"deleted_at":"amanhã"}`), CodeInvalidField},
		{"lixeira no futuro", rawJSON(t, map[string]any{"deleted_at": trashAt(testNow.Add(48 * time.Hour))}), CodeFutureDate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := DecidePush(mutation(1, c.payload), cur, false, pc, testNow)
			if out.Result.Status != StatusRejected || out.Result.Code != c.code || out.Write != WriteNone {
				t.Errorf("resultado = %+v, quero rejected %q", out.Result, c.code)
			}
		})
	}
}

// TestDecidePush_RegrasDeMescla é a tabela das regras de conflito da ADR-019.
func TestDecidePush_RegrasDeMescla(t *testing.T) {
	trashPC := trashAt(testNow.Add(-time.Minute))
	type want struct {
		status      Status
		merged      bool
		write       bool
		text        string
		trashed     bool
		selo        string // "" = sem selo
		textVersion int64  // 0 = não confere
		trashVer    int64
	}
	cases := []struct {
		name  string
		cur   *Transcription
		base  int64
		patch map[string]any
		want  want
	}{
		{
			name: "base atual, texto novo: applied sem mescla",
			cur:  existing(2, 2, 1, "a", false), base: 2, patch: map[string]any{"final_text": "b"},
			want: want{status: StatusApplied, write: true, text: "b", textVersion: 3, trashVer: 1},
		},
		{
			name: "base atual, mesmo texto: nada a gravar",
			cur:  existing(2, 2, 1, "a", false), base: 2, patch: map[string]any{"final_text": "a"},
			want: want{status: StatusApplied, text: "a"},
		},
		{
			name: "lixeira no PC, texto no servidor: mescla",
			cur:  existing(2, 2, 1, "texto do celular", false), base: 1, patch: map[string]any{"deleted_at": trashPC},
			want: want{status: StatusApplied, merged: true, write: true, text: "texto do celular", trashed: true, textVersion: 2, trashVer: 3},
		},
		{
			name: "texto no PC, restaurar no servidor: mescla",
			cur:  existing(3, 1, 3, "a", false), base: 2, patch: map[string]any{"final_text": "b"},
			want: want{status: StatusApplied, merged: true, write: true, text: "b", textVersion: 4, trashVer: 3},
		},
		{
			name: "texto contra texto: vale o primeiro, o do PC vai para o selo",
			cur:  existing(2, 2, 1, "do celular", false), base: 1, patch: map[string]any{"final_text": "do PC"},
			want: want{status: StatusConflict, write: true, text: "do celular", selo: "do PC", textVersion: 2, trashVer: 1},
		},
		{
			name: "o mesmo texto dos dois lados não é conflito",
			cur:  existing(2, 2, 1, "igual", false), base: 1, patch: map[string]any{"final_text": "igual"},
			want: want{status: StatusApplied, merged: true, write: true, text: "igual", textVersion: 2},
		},
		{
			name: "edição antiga contra a lixeira: apagar vence, a edição vai para o selo",
			cur:  existing(2, 1, 2, "a", true), base: 1, patch: map[string]any{"final_text": "editado no PC"},
			want: want{status: StatusConflict, write: true, text: "a", trashed: true, selo: "editado no PC", textVersion: 1, trashVer: 2},
		},
		{
			name: "edição e lixeira no PC, lixeira no servidor: mescla",
			cur:  existing(2, 1, 2, "a", true), base: 1, patch: map[string]any{"final_text": "b", "deleted_at": trashPC},
			want: want{status: StatusApplied, merged: true, write: true, text: "b", trashed: true, textVersion: 3, trashVer: 2},
		},
		{
			name: "restaurar contra uma lixeira mais nova: fica na lixeira, mescla",
			cur:  existing(4, 1, 4, "a", true), base: 2, patch: map[string]any{"deleted_at": nil},
			want: want{status: StatusApplied, merged: true, write: true, text: "a", trashed: true, trashVer: 4},
		},
		{
			name: "restaurar o que já estava na lixeira na base",
			cur:  existing(2, 1, 2, "a", true), base: 2, patch: map[string]any{"deleted_at": nil},
			want: want{status: StatusApplied, write: true, text: "a", trashVer: 3},
		},
		{
			name: "editar um item que já estava na lixeira na base",
			cur:  existing(2, 1, 2, "a", true), base: 2, patch: map[string]any{"final_text": "b"},
			want: want{status: StatusApplied, write: true, text: "b", trashed: true, textVersion: 3, trashVer: 2},
		},
		{
			name: "os dois puseram na lixeira: a mesma mudança",
			cur:  existing(2, 1, 2, "a", true), base: 1, patch: map[string]any{"deleted_at": trashPC},
			want: want{status: StatusApplied, merged: true, write: true, text: "a", trashed: true, trashVer: 2},
		},
		// "Depois da base" é de OUTRO aparelho: o campo que o próprio PC mudou por
		// último (num applied mesclado, cuja versão ele não guarda) ele conhece.
		{
			name: "restaurar a própria lixeira mesclada: vale (S1)",
			cur:  by(existing(3, 2, 3, "do celular", true), "", testDevice), base: 1, patch: map[string]any{"deleted_at": nil},
			want: want{status: StatusApplied, merged: true, write: true, text: "do celular", textVersion: 2, trashVer: 4},
		},
		{
			name: "restaurar contra a lixeira de outro PC, mais nova que a base: fica na lixeira",
			cur:  by(existing(3, 2, 3, "do celular", true), "", otherDevice), base: 1, patch: map[string]any{"deleted_at": nil},
			want: want{status: StatusApplied, merged: true, write: true, text: "do celular", trashed: true, trashVer: 3},
		},
		{
			name: "segunda edição sobre a própria edição mesclada: vale (S2)",
			cur:  by(existing(4, 4, 3, "A", false), testDevice, ""), base: 1, patch: map[string]any{"final_text": "B"},
			want: want{status: StatusApplied, merged: true, write: true, text: "B", textVersion: 5, trashVer: 3},
		},
		{
			name: "edição sobre a edição de outro PC, mais nova que a base: conflito",
			cur:  by(existing(4, 4, 3, "A", false), otherDevice, ""), base: 1, patch: map[string]any{"final_text": "B"},
			want: want{status: StatusConflict, write: true, text: "A", selo: "B", textVersion: 4, trashVer: 3},
		},
		{
			name: "edição sobre a própria lixeira mesclada: não é edição antiga",
			cur:  by(existing(3, 1, 3, "a", true), "", testDevice), base: 1, patch: map[string]any{"final_text": "b"},
			want: want{status: StatusApplied, merged: true, write: true, text: "b", trashed: true, textVersion: 4, trashVer: 3},
		},
		{
			name: "o celular (sem aparelho) nunca é o mesmo aparelho",
			cur:  existing(3, 3, 1, "do celular", false), base: 1, patch: map[string]any{"final_text": "do PC"},
			want: want{status: StatusConflict, write: true, text: "do celular", selo: "do PC", textVersion: 3},
		},
		{
			name: "base velha sem mudança nenhuma ainda grava versão nova",
			cur:  existing(3, 1, 1, "a", false), base: 1, patch: map[string]any{"final_text": "a"},
			want: want{status: StatusApplied, merged: true, write: true, text: "a"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := DecidePush(mutation(c.base, rawJSON(t, c.patch)), c.cur, false, pc, testNow)
			r, n, w := out.Result, out.Next, c.want
			if r.Status != w.status || r.Merged != w.merged {
				t.Fatalf("resultado = %+v, quero %s mesclado=%v", r, w.status, w.merged)
			}
			if (out.Write != WriteNone) != w.write {
				t.Fatalf("Write = %v, quero gravar=%v", out.Write, w.write)
			}
			if !w.write {
				if r.EntityVersion != c.cur.Version {
					t.Errorf("sem gravar, entity_version = %d, quero a atual %d", r.EntityVersion, c.cur.Version)
				}
				return
			}
			if out.Write != WriteUpdate || n.Version != c.cur.Version+1 || r.EntityVersion != n.Version {
				t.Errorf("versão gravada = %d (resultado %d, write %v), quero %d", n.Version, r.EntityVersion, out.Write, c.cur.Version+1)
			}
			if !n.UpdatedAt.Equal(testNow) {
				t.Errorf("updated_at = %v, quero %v", n.UpdatedAt, testNow)
			}
			if n.FinalText != w.text {
				t.Errorf("texto = %q, quero %q", n.FinalText, w.text)
			}
			if (n.DeletedAt != nil) != w.trashed || (n.PurgeAt != nil) != w.trashed {
				t.Errorf("lixeira = %v / purge %v, quero na lixeira=%v", n.DeletedAt, n.PurgeAt, w.trashed)
			}
			if w.textVersion != 0 && n.TextVersion != w.textVersion {
				t.Errorf("TextVersion = %d, quero %d", n.TextVersion, w.textVersion)
			}
			if w.trashVer != 0 && n.TrashVersion != w.trashVer {
				t.Errorf("TrashVersion = %d, quero %d", n.TrashVersion, w.trashVer)
			}
			switch {
			case w.selo == "" && n.Conflict != nil:
				t.Errorf("selo inesperado: %+v", n.Conflict)
			case w.selo != "":
				if n.Conflict == nil || n.Conflict.FinalText != w.selo || !n.Conflict.At.Equal(testNow) ||
					n.Conflict.DeviceID == nil || *n.Conflict.DeviceID != testDevice {
					t.Errorf("selo = %+v, quero %q do aparelho %s", n.Conflict, w.selo, testDevice)
				}
				if r.State == nil || r.State.Conflict == nil || r.State.Version != n.Version || r.State.FinalText != n.FinalText {
					t.Errorf("conflict sem o estado atual: %+v", r.State)
				}
			}
			if w.status != StatusConflict && r.State != nil {
				t.Errorf("%s não traz o estado, e trouxe %+v", w.status, r.State)
			}
		})
	}
}

// TestDecidePush_DitadoInteiroSobreUmQueExiste: o recomeço manda o ditado
// inteiro com a base vazia; o servidor compara com tudo o que mudou.
func TestDecidePush_DitadoInteiroSobreUmQueExiste(t *testing.T) {
	t.Run("igual: mescla", func(t *testing.T) {
		cur := existing(3, 1, 1, "Texto final do ditado.", false)
		out := DecidePush(mutation(0, createPayload(t, nil)), cur, false, pc, testNow)
		if out.Result.Status != StatusApplied || !out.Result.Merged || out.Next.Version != 4 {
			t.Errorf("resultado = %+v, quero applied mesclado v4", out.Result)
		}
	})
	t.Run("texto diferente: conflito", func(t *testing.T) {
		cur := existing(3, 2, 1, "editado no celular", false)
		out := DecidePush(mutation(0, createPayload(t, nil)), cur, false, pc, testNow)
		if out.Result.Status != StatusConflict || out.Next.FinalText != "editado no celular" ||
			out.Next.Conflict == nil || out.Next.Conflict.FinalText != "Texto final do ditado." {
			t.Errorf("resultado = %+v / %+v, quero conflict com o texto do PC no selo", out.Result, out.Next.Conflict)
		}
	})
	t.Run("fora da lixeira no PC, na lixeira no servidor: fica na lixeira", func(t *testing.T) {
		cur := existing(2, 1, 2, "Texto final do ditado.", true)
		out := DecidePush(mutation(0, createPayload(t, nil)), cur, false, pc, testNow)
		if out.Result.Status != StatusApplied || !out.Result.Merged || out.Next.DeletedAt == nil {
			t.Errorf("resultado = %+v, quero applied mesclado ainda na lixeira", out.Result)
		}
	})
	t.Run("payload inválido é recusado", func(t *testing.T) {
		cur := existing(1, 1, 1, "a", false)
		out := DecidePush(mutation(0, createPayload(t, map[string]any{"target_app": "x"})), cur, false, pc, testNow)
		if out.Result.Status != StatusRejected || out.Result.Code != CodeUnknownField {
			t.Errorf("resultado = %+v, quero rejected campo_desconhecido", out.Result)
		}
	})
}

func TestDuplicate(t *testing.T) {
	m := mutation(1, nil)
	prev := Applied{EntityType: EntityTranscription, EntityID: testEntity, DeviceID: testDevice, Operation: OpUpsert,
		Status: StatusApplied, Version: 5, Merged: true}

	r := Duplicate(prev, m, pc)
	if r.Status != StatusDuplicate || r.Original != StatusApplied || r.EntityVersion != 5 || !r.Merged || r.MutationID != testMutation {
		t.Errorf("duplicate = %+v, quero o mesmo resultado (v5, mesclado)", r)
	}
	upper := m
	upper.EntityID = strings.ToUpper(testEntity)
	if r := Duplicate(prev, upper, pc); r.Status != StatusDuplicate {
		t.Errorf("o mesmo id em maiúsculas: %+v, quero duplicate", r)
	}
	if r := Duplicate(prev, m, Actor{UserID: testUser, DeviceID: strings.ToUpper(testDevice)}); r.Status != StatusDuplicate {
		t.Errorf("o mesmo aparelho em maiúsculas: %+v, quero duplicate", r)
	}
	otherPC := Actor{UserID: testUser, DeviceID: otherDevice}
	if r := Duplicate(prev, m, otherPC); r.Status != StatusRejected || r.Code != CodeReusedMutation {
		t.Errorf("outro aparelho: %+v, quero rejected mutacao_reusada", r)
	}

	other := m
	other.EntityID = "0192a000-0000-7000-8000-000000000002"
	if r := Duplicate(prev, other, pc); r.Status != StatusRejected || r.Code != CodeReusedMutation {
		t.Errorf("outra entidade: %+v, quero rejected mutacao_reusada", r)
	}
	otherType := m
	otherType.EntityType = EntityLearned
	if r := Duplicate(prev, otherType, pc); r.Status != StatusRejected || r.Code != CodeReusedMutation {
		t.Errorf("outro tipo: %+v, quero rejected mutacao_reusada", r)
	}

	rej := Applied{EntityType: EntityTranscription, EntityID: testEntity, DeviceID: testDevice, Operation: OpUpsert,
		Status: StatusRejected, Code: CodeUnknownField}
	if r := Duplicate(rej, m, pc); r.Original != StatusRejected || r.Code != CodeUnknownField || r.Reason != "" {
		t.Errorf("duplicate de rejected = %+v", r)
	}
	g := Applied{EntityType: EntityTranscription, EntityID: testEntity, DeviceID: testDevice, Operation: OpUpsert,
		Status: StatusGone, Code: ReasonDeleted}
	if r := Duplicate(g, m, pc); r.Original != StatusGone || r.Reason != ReasonDeleted || r.Code != "" {
		t.Errorf("duplicate de gone = %+v", r)
	}

	// A operação (F8): com outra, mutacao_reusada; a de fora da lista conta
	// como vazia, e o reenvio dela compara igual a ela mesma.
	otherOp := m
	otherOp.Operation = OpDelete
	if r := Duplicate(prev, otherOp, pc); r.Status != StatusRejected || r.Code != CodeReusedMutation {
		t.Errorf("outra operação: %+v, quero rejected mutacao_reusada", r)
	}
	undo := Applied{EntityType: EntityLearned, EntityID: testEntity, DeviceID: testDevice, Operation: OpDelete,
		Status: StatusApplied, Version: 2}
	lm := Mutation{MutationID: testMutation, EntityType: EntityLearned, EntityID: testEntity, Operation: OpDelete}
	if r := Duplicate(undo, lm, pc); r.Status != StatusDuplicate || r.Original != StatusApplied || r.EntityVersion != 2 {
		t.Errorf("o desfazer reenviado como desfazer: %+v, quero duplicate do applied v2", r)
	}
	lm.Operation = OpUpsert
	if r := Duplicate(undo, lm, pc); r.Status != StatusRejected || r.Code != CodeReusedMutation {
		t.Errorf("o desfazer reenviado como reaprender: %+v, quero rejected mutacao_reusada", r)
	}
	weird := Applied{EntityType: EntityTranscription, EntityID: testEntity, DeviceID: testDevice, Operation: "",
		Status: StatusRejected, Code: CodeInvalidOperation}
	wm := m
	wm.Operation = "patch"
	if r := Duplicate(weird, wm, pc); r.Status != StatusDuplicate || r.Code != CodeInvalidOperation {
		t.Errorf("o reenvio da operação fora da lista: %+v, quero duplicate do rejected", r)
	}
	wm.Operation = "outra"
	if r := Duplicate(weird, wm, pc); r.Status != StatusDuplicate {
		t.Errorf("outra operação fora da lista (as duas contam como vazias): %+v, quero duplicate", r)
	}
	if r := Duplicate(weird, m, pc); r.Status != StatusRejected || r.Code != CodeReusedMutation {
		t.Errorf("a operação fora da lista reenviada como upsert: %+v, quero rejected mutacao_reusada", r)
	}
}

func TestRecordedOperation(t *testing.T) {
	for op, want := range map[string]string{
		OpUpsert: OpUpsert, OpDelete: OpDelete, "": "", "patch": "", "UPSERT": "", OpTombstone: "", "delete\x00": "",
	} {
		if got := RecordedOperation(op); got != want {
			t.Errorf("RecordedOperation(%q) = %q, quero %q", op, got, want)
		}
	}
}

func TestOutcomeRecord(t *testing.T) {
	m := mutation(0, nil)
	m.EntityID = strings.ToUpper(testEntity)
	g := Outcome{Result: Result{Status: StatusGone, Reason: ReasonOutOfWindow}}.Record(m, pc)
	if g.Code != ReasonOutOfWindow || g.EntityID != testEntity || g.EntityType != EntityTranscription || g.DeviceID != testDevice {
		t.Errorf("gravado do gone = %+v", g)
	}
	a := Outcome{Result: Result{Status: StatusApplied, EntityVersion: 3, Merged: true}}.Record(m, pc)
	if a.Status != StatusApplied || a.Version != 3 || !a.Merged || a.Code != "" {
		t.Errorf("gravado do applied = %+v", a)
	}
	rj := Rejected(m, CodeFutureDate).Record(m, pc)
	if rj.Status != StatusRejected || rj.Code != CodeFutureDate {
		t.Errorf("gravado do rejected = %+v", rj)
	}
	// A operação (F8): upsert e delete como vieram; fora da lista, vazia.
	if a.Operation != OpUpsert {
		t.Errorf("operação gravada do upsert = %q", a.Operation)
	}
	del := m
	del.EntityType, del.Operation = EntityLearned, OpDelete
	if r := Rejected(del, CodeNothingToUndo).Record(del, pc); r.Operation != OpDelete || r.EntityType != EntityLearned || r.Code != CodeNothingToUndo {
		t.Errorf("gravado do desfazer = %+v", r)
	}
	bad := m
	bad.Operation = "patch"
	if r := Rejected(bad, CodeInvalidOperation).Record(bad, pc); r.Operation != "" {
		t.Errorf("operação fora da lista gravada como %q, quero vazia", r.Operation)
	}
}

func TestEdit(t *testing.T) {
	cur := *existing(3, 1, 2, "a", false)
	next, err := Edit(cur, nil, 3, "b", testNow)
	if err != nil || next.FinalText != "b" || next.Version != 4 || next.TextVersion != 4 || next.TrashVersion != 2 || !next.UpdatedAt.Equal(testNow) {
		t.Errorf("Edit = %+v, %v", next, err)
	}
	if _, err := Edit(cur, nil, 2, "b", testNow); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("base velha: %v, quero ErrVersionConflict", err)
	}
	if _, err := Edit(cur, nil, 4, "b", testNow); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("base do futuro: %v, quero ErrVersionConflict", err)
	}
	if _, err := Edit(cur, nil, 3, "a\x00", testNow); !errors.Is(err, ErrInvalidText) {
		t.Errorf("texto com nulo: %v, quero ErrInvalidText", err)
	}
	// Editar mantém o selo (escolher uma versão é o ResolveConflict).
	cur.Conflict = &Conflict{FinalText: "perdeu", At: testNow}
	if next, _ := Edit(cur, nil, 3, "b", testNow); next.Conflict == nil {
		t.Error("Edit limpou o selo")
	}
}

// A escolha do selo (ADR-019, "Decisões da F4"): só sobre a versão atual e só
// com selo; as duas escolhas limpam o selo e gravam uma versão nova; só
// "trocar" muda o texto, e só ela conta como mudança do texto.
func TestResolveConflict(t *testing.T) {
	pcText := testDevice
	withSeal := func(final, lost string) Transcription {
		cur := *by(existing(3, 2, 1, final, false), pcText, "")
		cur.Conflict = &Conflict{FinalText: lost, DeviceID: ptr(otherDevice), At: testNow.Add(-time.Hour)}
		return cur
	}
	later := testNow.Add(time.Minute)
	cases := []struct {
		name      string
		cur       Transcription
		base      int64
		escolha   string
		err       error
		text      string
		textV     int64
		textDevSt string // "" é nulo (o celular), senão o aparelho
	}{
		{"manter fica com o texto atual", withSeal("ficou", "perdeu"), 3, EscolhaManter, nil, "ficou", 2, pcText},
		{"trocar põe o texto do selo", withSeal("ficou", "perdeu"), 3, EscolhaTrocar, nil, "perdeu", 4, ""},
		{"trocar com o mesmo texto não muda o texto", withSeal("igual", "igual"), 3, EscolhaTrocar, nil, "igual", 2, pcText},
		{"base velha", withSeal("ficou", "perdeu"), 2, EscolhaTrocar, ErrVersionConflict, "", 0, ""},
		{"base do futuro", withSeal("ficou", "perdeu"), 4, EscolhaManter, ErrVersionConflict, "", 0, ""},
		{"sem selo", *existing(3, 2, 1, "a", false), 3, EscolhaManter, ErrNoConflict, "", 0, ""},
		{"escolha inválida", withSeal("ficou", "perdeu"), 3, "outra", ErrInvalidChoice, "", 0, ""},
		{"escolha vazia", withSeal("ficou", "perdeu"), 3, "", ErrInvalidChoice, "", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := c.cur
			next, err := ResolveConflict(c.cur, nil, c.base, c.escolha, later)
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("erro = %v, quero %v", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if next.Conflict != nil {
				t.Errorf("o selo ficou: %+v", next.Conflict)
			}
			if next.FinalText != c.text || next.Version != 4 || !next.UpdatedAt.Equal(later) {
				t.Errorf("ResolveConflict = texto %q v%d em %v; quero %q v4 em %v", next.FinalText, next.Version, next.UpdatedAt, c.text, later)
			}
			if next.TextVersion != c.textV {
				t.Errorf("TextVersion = %d, quero %d", next.TextVersion, c.textV)
			}
			if got := deref(next.TextDeviceID); got != c.textDevSt {
				t.Errorf("TextDeviceID = %q, quero %q", got, c.textDevSt)
			}
			if next.TrashVersion != before.TrashVersion || next.DeletedAt != before.DeletedAt || next.RawText != before.RawText {
				t.Errorf("a escolha mexeu na lixeira ou no bruto: %+v", next)
			}
			if before.Conflict == nil {
				t.Error("a regra mudou o estado de entrada")
			}
		})
	}
	if !EscolhaValida(EscolhaManter) || !EscolhaValida(EscolhaTrocar) || EscolhaValida("Manter") {
		t.Error("EscolhaValida")
	}
}

func ptr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestTrashRestore(t *testing.T) {
	cur := *existing(2, 1, 1, "a", false)
	trashed, changed := Trash(cur, nil, testNow)
	if !changed || trashed.DeletedAt == nil || !trashed.DeletedAt.Equal(testNow) || trashed.Version != 3 || trashed.TrashVersion != 3 || trashed.TextVersion != 1 {
		t.Fatalf("Trash = %+v, %v", trashed, changed)
	}
	if want := testNow.AddDate(0, 0, 30); trashed.PurgeAt == nil || !trashed.PurgeAt.Equal(want) {
		t.Errorf("purge_at = %v, quero %v", trashed.PurgeAt, want)
	}
	if again, changed := Trash(trashed, nil, testNow.Add(time.Hour)); changed || again.Version != 3 || !again.DeletedAt.Equal(testNow) {
		t.Errorf("Trash de novo = %+v, %v; quero nada a gravar", again, changed)
	}
	restored, changed := Restore(trashed, nil, testNow)
	if !changed || restored.DeletedAt != nil || restored.PurgeAt != nil || restored.Version != 4 || restored.TrashVersion != 4 {
		t.Errorf("Restore = %+v, %v", restored, changed)
	}
	if _, changed := Restore(cur, nil, testNow); changed {
		t.Error("Restore de quem não está na lixeira gravou")
	}
	if err := CheckPurge(cur); !errors.Is(err, ErrNotInTrash) {
		t.Errorf("CheckPurge fora da lixeira: %v", err)
	}
	if err := CheckPurge(trashed); err != nil {
		t.Errorf("CheckPurge na lixeira: %v", err)
	}
}

func TestCanonicalUUID(t *testing.T) {
	for in, want := range map[string]string{
		testEntity:                             testEntity,
		"0192A000-0000-7000-8000-00000000000F": "0192a000-0000-7000-8000-00000000000f",
	} {
		if got, ok := CanonicalUUID(in); !ok || got != want {
			t.Errorf("CanonicalUUID(%q) = %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "t1", "0192a000000070008000000000000001", "0192a000-0000-7000-8000-00000000000g",
		"0192a000_0000-7000-8000-000000000001", "{0192a000-0000-7000-8000-000000000001}", testEntity + " "} {
		if _, ok := CanonicalUUID(bad); ok {
			t.Errorf("CanonicalUUID(%q) aceitou", bad)
		}
	}
}

func TestCursor(t *testing.T) {
	at := time.Date(2026, 10, 5, 12, 0, 0, 123456789, time.FixedZone("x", 3600))
	c := EncodeCursor(at, testEntity)
	gotAt, gotID, err := DecodeCursor(c)
	if err != nil || !gotAt.Equal(Normalize(at)) || gotID != testEntity {
		t.Errorf("DecodeCursor = %v, %q, %v", gotAt, gotID, err)
	}
	for _, bad := range []string{"%%%", "c2VtLWJhcnJh", EncodeCursor(at, "x")} {
		if _, _, err := DecodeCursor(bad); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("DecodeCursor(%q) = %v, quero ErrInvalidCursor", bad, err)
		}
	}
}

func TestNormalizeEPrecisao(t *testing.T) {
	in := time.Date(2026, 10, 5, 12, 0, 0, 123456789, time.FixedZone("x", -3*3600))
	got := Normalize(in)
	if got.Location() != time.UTC || got.Nanosecond() != 123456000 || !got.Equal(in.Truncate(time.Microsecond)) {
		t.Errorf("Normalize = %v", got)
	}
	m := mutation(0, createPayload(t, map[string]any{"created_at": in.Format(time.RFC3339Nano)}))
	if out := DecidePush(m, nil, false, pc, testNow); !out.Next.CreatedAt.Equal(got) || out.Next.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at gravado = %v, quero %v em UTC", out.Next.CreatedAt, got)
	}
}

func TestActorDevice(t *testing.T) {
	if (Actor{UserID: "u"}).Device() != nil {
		t.Error("Actor sem aparelho devolveu aparelho")
	}
	if d := pc.Device(); d == nil || *d != testDevice {
		t.Errorf("Device = %v", d)
	}
}

func TestCheckListFilter(t *testing.T) {
	mode := func(s string) *string { return &s }
	for _, f := range []ListFilter{{}, {Query: "bom dia, ação"}, {Mode: mode("smart")}, {Query: "%_\\"}} {
		if err := CheckListFilter(f); err != nil {
			t.Errorf("CheckListFilter(%+v) = %v, quero nil", f, err)
		}
	}
	for _, f := range []ListFilter{
		{Query: "a\x00b"}, {Query: "\xff"}, {Query: "a\xc3"},
		{Mode: mode("\x00")}, {Mode: mode("smart\xff")},
	} {
		if err := CheckListFilter(f); !errors.Is(err, ErrInvalidFilter) {
			t.Errorf("CheckListFilter(%q/%v) = %v, quero ErrInvalidFilter", f.Query, f.Mode, err)
		}
	}
}

func TestTetoDoTexto(t *testing.T) {
	atTeto, acima := strings.Repeat("a", MaxTextBytes), strings.Repeat("a", MaxTextBytes+1)
	for _, field := range []string{"raw_text", "final_text"} {
		if out := DecidePush(mutation(0, createPayload(t, map[string]any{field: atTeto})), nil, false, pc, testNow); out.Result.Status != StatusApplied {
			t.Errorf("%s no teto: %+v, quero applied", field, out.Result)
		}
		if out := DecidePush(mutation(0, createPayload(t, map[string]any{field: acima})), nil, false, pc, testNow); out.Result.Status != StatusRejected || out.Result.Code != CodeInvalidField {
			t.Errorf("%s acima do teto: %+v, quero rejected campo_invalido", field, out.Result)
		}
	}
	cur := existing(1, 1, 1, "a", false)
	if out := DecidePush(mutation(1, rawJSON(t, map[string]any{"final_text": atTeto})), cur, false, pc, testNow); out.Result.Status != StatusApplied {
		t.Errorf("mudança no teto: %+v, quero applied", out.Result)
	}
	if out := DecidePush(mutation(1, rawJSON(t, map[string]any{"final_text": acima})), cur, false, pc, testNow); out.Result.Status != StatusRejected || out.Result.Code != CodeInvalidField {
		t.Errorf("mudança acima do teto: %+v, quero rejected campo_invalido", out.Result)
	}
	if _, err := Edit(*cur, nil, 1, atTeto, testNow); err != nil {
		t.Errorf("Edit no teto: %v", err)
	}
	if _, err := Edit(*cur, nil, 1, acima, testNow); !errors.Is(err, ErrTextTooLong) {
		t.Errorf("Edit acima do teto: %v, quero ErrTextTooLong", err)
	}
	// Os bytes contam, não as letras: "ç" são dois.
	if out := DecidePush(mutation(0, createPayload(t, map[string]any{"final_text": strings.Repeat("ç", MaxTextBytes/2+1)})), nil, false, pc, testNow); out.Result.Status != StatusRejected {
		t.Errorf("acima do teto em bytes: %+v, quero rejected", out.Result)
	}
}

func TestTextBytes(t *testing.T) {
	it := Transcription{RawText: "abc", FinalText: "de"}
	if n := TextBytes(it); n != 5 {
		t.Errorf("TextBytes = %d, quero 5", n)
	}
	it.Conflict = &Conflict{FinalText: "fgh"}
	if n := TextBytes(it); n != 8 {
		t.Errorf("TextBytes com selo = %d, quero 8", n)
	}
	if n := (Change{Item: &it}).TextBytes(); n != 8 {
		t.Errorf("Change.TextBytes = %d, quero 8", n)
	}
	if n := (Change{Operation: OpTombstone}).TextBytes(); n != 0 {
		t.Errorf("lápide = %d, quero 0", n)
	}
	// O aprendido (F8): a variante, o termo e o nome do domínio, em bytes.
	dom := "fiscal"
	l := Learned{Variant: "é", Term: "ECF"}
	if n := (Change{Learned: &l}).TextBytes(); n != 5 {
		t.Errorf("aprendido sem domínio = %d, quero 5", n)
	}
	l.Domain = &dom
	if n := LearnedTextBytes(l); n != 11 {
		t.Errorf("aprendido com domínio = %d, quero 11", n)
	}
}

func TestCutPage(t *testing.T) {
	id := func(n int) int { return n }
	big := PageTextBudget / 4
	cases := []struct {
		name  string
		sizes []int
		limit int
		n     int
		more  bool
	}{
		{"vazia", nil, 5, 0, false},
		{"cabe inteira", []int{1, 2, 3}, 5, 3, false},
		{"exatamente o limite", []int{1, 2, 3}, 3, 3, false},
		{"passa do limite", []int{1, 2, 3, 4}, 3, 3, true},
		{"chega ao orçamento no quarto, sobra um", []int{big, big, big, big, 1}, 10, 4, true},
		{"chega ao orçamento no último", []int{big, big, big, big}, 10, 4, false},
		{"um abaixo do orçamento não corta", []int{big, big, big, big - 1, 1}, 10, 5, false},
		{"um item só, maior que o orçamento", []int{PageTextBudget + 1, 1}, 10, 1, true},
		{"lápides (zero) depois do corte contam como sobra", []int{PageTextBudget, 0}, 10, 1, true},
		{"o limite vem antes do orçamento", []int{big, big, big, big}, 2, 2, true},
	}
	for _, c := range cases {
		n, more := CutPage(c.sizes, c.limit, id)
		if n != c.n || more != c.more {
			t.Errorf("%s: CutPage = (%d, %v), quero (%d, %v)", c.name, n, more, c.n, c.more)
		}
	}
}

// Cada gravação guarda o aparelho da última mudança do texto e da lixeira: o
// do push, o do token no CRUD (nulo no celular), e nada muda no campo que não
// mudou (o texto que perdeu um conflito, a lixeira que já estava lá).
func TestAparelhoDeCadaCampo(t *testing.T) {
	dev := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	check := func(name string, got Transcription, text, trash string) {
		t.Helper()
		if dev(got.TextDeviceID) != text || dev(got.TrashDeviceID) != trash {
			t.Errorf("%s: aparelho do texto %q, da lixeira %q; quero %q e %q", name, dev(got.TextDeviceID), dev(got.TrashDeviceID), text, trash)
		}
	}
	check("criar", DecidePush(mutation(0, createPayload(t, nil)), nil, false, pc, testNow).Next, testDevice, testDevice)

	cel := by(existing(2, 1, 1, "a", false), otherDevice, "")
	check("texto pelo push", DecidePush(mutation(2, rawJSON(t, map[string]any{"final_text": "b"})), cel, false, pc, testNow).Next, testDevice, "")
	check("lixeira pelo push", DecidePush(mutation(2, rawJSON(t, map[string]any{"deleted_at": trashAt(testNow)})), cel, false, pc, testNow).Next, otherDevice, testDevice)
	conflict := DecidePush(mutation(1, rawJSON(t, map[string]any{"final_text": "b"})), by(existing(2, 2, 1, "a", false), otherDevice, ""), false, pc, testNow)
	if conflict.Result.Status != StatusConflict {
		t.Fatalf("quero conflict: %+v", conflict.Result)
	}
	check("o texto que perdeu não muda o aparelho do texto", conflict.Next, otherDevice, "")
	same := DecidePush(mutation(1, rawJSON(t, map[string]any{"deleted_at": trashAt(testNow)})), by(existing(2, 1, 2, "a", true), "", otherDevice), false, pc, testNow)
	check("a mesma lixeira não muda o aparelho da lixeira", same.Next, "", otherDevice)
	restored := DecidePush(mutation(2, rawJSON(t, map[string]any{"deleted_at": nil})), by(existing(3, 1, 2, "a", true), "", otherDevice), false, pc, testNow)
	if restored.Next.DeletedAt != nil {
		t.Fatalf("restaurar com a base da lixeira: %+v, quero fora da lixeira", restored.Result)
	}
	check("restaurar pelo push", restored.Next, "", testDevice)

	pcDev := pc.Device()
	base := *by(existing(2, 1, 1, "a", false), otherDevice, otherDevice)
	edited, err := Edit(base, nil, 2, "b", testNow)
	if err != nil {
		t.Fatal(err)
	}
	check("Edit pelo celular", edited, "", otherDevice)
	edited, _ = Edit(base, pcDev, 2, "b", testNow)
	check("Edit pelo token do PC", edited, testDevice, otherDevice)
	trashed, _ := Trash(base, nil, testNow)
	check("Trash pelo celular", trashed, otherDevice, "")
	back, _ := Restore(trashed, pcDev, testNow)
	check("Restore pelo token do PC", back, otherDevice, testDevice)
}
