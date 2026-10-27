package cloudstore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Testes puros das regras da nota e do marcador de nota (notas_regras.go). A
// suíte de contrato (contracttest/notas.go) prova as mesmas regras pela API,
// contra a memória e contra o Postgres.

const (
	testLabel1 = "0192a000-0000-7000-8000-0000000000b1"
	testLabel2 = "0192a000-0000-7000-8000-0000000000b2"
	testLabelX = "0192a000-0000-7000-8000-0000000000bf"
)

// notePayload é uma nota inteira válida (o criar), com as trocas pedidas (nil
// apaga a chave).
func notePayload(t *testing.T, changes map[string]any) json.RawMessage {
	t.Helper()
	p := map[string]any{
		"titulo":     "Mercado",
		"corpo":      "- [ ] leite\n- [ ] pão",
		"cor":        "verde",
		"fixada":     false,
		"arquivada":  false,
		"posicao":    "V",
		"marcadores": []string{testLabel2, testLabel1},
		"created_at": testNow.Add(-time.Hour).Format(time.RFC3339Nano),
	}
	for k, v := range changes {
		if v == nil {
			delete(p, k)
			continue
		}
		p[k] = v
	}
	return rawJSON(t, p)
}

func noteMut(base int64, payload json.RawMessage) Mutation {
	return Mutation{
		MutationID: testMutation, EntityType: EntityNote, EntityID: testEntity,
		Operation: OpUpsert, BaseVersion: base, Payload: payload,
	}
}

func noteDelete() Mutation {
	return Mutation{MutationID: testMutation, EntityType: EntityNote, EntityID: testEntity, Operation: OpDelete}
}

// existingNote é uma nota que está na nuvem, na versão v, com o conteúdo
// mudado por último na contentV (pelo outro aparelho) e a lixeira na trashV.
func existingNote(v, contentV, trashV int64, corpo string, trashed bool) *Note {
	created := testNow.Add(-24 * time.Hour)
	other := otherDevice
	n := &Note{
		ID: testEntity, UserID: testUser, Titulo: "Mercado", Corpo: corpo, Cor: strPtr("verde"),
		Posicao: "V", Marcadores: []string{testLabel1},
		CreatedAt: created, UpdatedAt: created,
		Version: v, ContentVersion: contentV, TrashVersion: trashV,
		ContentDeviceID: &other, TrashDeviceID: &other, LastSeq: 9,
	}
	if trashed {
		d := testNow.Add(-time.Hour)
		n.DeletedAt, n.PurgeAt = &d, noteTrashPurgeAt(&d)
	}
	return n
}

func TestDecideNote_Cria(t *testing.T) {
	out := DecideNote(noteMut(0, notePayload(t, map[string]any{"ditado_de_origem": testEntity})), nil, false, nil, pc, testNow)
	if out.Result.Status != StatusApplied || out.Result.EntityVersion != 1 || out.Write != WriteInsert || out.Result.Merged {
		t.Fatalf("criar: %+v", out.Result)
	}
	n := out.NextNote
	switch {
	case n.ID != testEntity || n.UserID != testUser || n.Titulo != "Mercado" || n.Corpo != "- [ ] leite\n- [ ] pão":
		t.Errorf("a nota: %+v", n)
	case n.Cor == nil || *n.Cor != "verde" || n.Fixada || n.Arquivada || n.Posicao != "V":
		t.Errorf("os enfeites: %+v", n)
	case len(n.Marcadores) != 2 || n.Marcadores[0] != testLabel1 || n.Marcadores[1] != testLabel2:
		t.Errorf("os marcadores saem em ordem de id: %v", n.Marcadores)
	case n.DitadoDeOrigem == nil || *n.DitadoDeOrigem != testEntity:
		t.Errorf("o ditado de origem: %v", n.DitadoDeOrigem)
	case n.Version != 1 || n.ContentVersion != 1 || n.TrashVersion != 1 || !sameDevice(n.ContentDeviceID, pc.Device()) || !sameDevice(n.TrashDeviceID, pc.Device()):
		t.Errorf("as versões e os aparelhos: %+v", n)
	case !n.UpdatedAt.Equal(testNow) || n.DeletedAt != nil || n.PurgeAt != nil || n.Conflict != nil:
		t.Errorf("as datas: %+v", n)
	}

	// Na lixeira: o purge_at é deleted_at + 30 dias. Um marcador com lápide
	// sai do conjunto; o que a nuvem não conhece fica.
	del := testNow.Add(-2 * time.Hour)
	out = DecideNote(noteMut(0, notePayload(t, map[string]any{
		"deleted_at": del.Format(time.RFC3339Nano), "cor": jsonNull{}, "marcadores": []string{testLabel1, testLabelX},
	})), nil, false, map[string]bool{testLabel1: true}, pc, testNow)
	n = out.NextNote
	if out.Write != WriteInsert || n.DeletedAt == nil || n.PurgeAt == nil || !n.PurgeAt.Equal(del.AddDate(0, 0, 30)) || n.Cor != nil {
		t.Errorf("na lixeira: %+v", n)
	}
	if len(n.Marcadores) != 1 || n.Marcadores[0] != testLabelX {
		t.Errorf("o marcador com lápide não sai: %v", n.Marcadores)
	}
}

func TestDecideNote_Recusas(t *testing.T) {
	big := strings.Repeat("x", MaxTextBytes+1)
	cases := []struct {
		name string
		m    Mutation
		code string
	}{
		{"campo fora da lista", noteMut(0, notePayload(t, map[string]any{"target_app": "chrome"})), CodeUnknownField},
		{"sem titulo", noteMut(0, notePayload(t, map[string]any{"titulo": nil})), CodeInvalidField},
		{"sem marcadores", noteMut(0, notePayload(t, map[string]any{"marcadores": nil})), CodeInvalidField},
		{"sem cor (a chave é obrigatória, o valor pode ser null)", noteMut(0, notePayload(t, map[string]any{"cor": nil})), CodeInvalidField},
		{"titulo nulo", noteMut(0, notePayload(t, map[string]any{"titulo": jsonNull{}})), CodeInvalidField},
		{"título de 1025 bytes", noteMut(0, notePayload(t, map[string]any{"titulo": strings.Repeat("é", 512) + "x"})), CodeInvalidField},
		{"corpo grande demais", noteMut(0, notePayload(t, map[string]any{"corpo": big})), CodeInvalidField},
		{"corpo com o nulo", noteMut(0, notePayload(t, map[string]any{"corpo": "a\x00b"})), CodeInvalidField},
		{"cor fora da lista", noteMut(0, notePayload(t, map[string]any{"cor": "preto"})), CodeInvalidField},
		{"fixada nula", noteMut(0, notePayload(t, map[string]any{"fixada": jsonNull{}})), CodeInvalidField},
		{"arquivada texto", noteMut(0, notePayload(t, map[string]any{"arquivada": "sim"})), CodeInvalidField},
		{"posição terminada em zero", noteMut(0, notePayload(t, map[string]any{"posicao": "V0"})), CodeInvalidField},
		{"posição vazia", noteMut(0, notePayload(t, map[string]any{"posicao": ""})), CodeInvalidField},
		{"marcador repetido", noteMut(0, notePayload(t, map[string]any{"marcadores": []string{testLabel1, strings.ToUpper(testLabel1)}})), CodeInvalidField},
		{"marcador que não é UUID", noteMut(0, notePayload(t, map[string]any{"marcadores": []string{"mercado"}})), CodeInvalidField},
		{"marcadores nulos", noteMut(0, notePayload(t, map[string]any{"marcadores": jsonNull{}})), CodeInvalidField},
		{"ditado de origem que não é UUID", noteMut(0, notePayload(t, map[string]any{"ditado_de_origem": "x"})), CodeInvalidField},
		{"created_at inválido", noteMut(0, notePayload(t, map[string]any{"created_at": "ontem"})), CodeInvalidField},
		{"created_at no futuro", noteMut(0, notePayload(t, map[string]any{"created_at": testNow.Add(25 * time.Hour).Format(time.RFC3339)})), CodeFutureDate},
		{"deleted_at no futuro", noteMut(0, notePayload(t, map[string]any{"deleted_at": testNow.Add(25 * time.Hour).Format(time.RFC3339)})), CodeFutureDate},
		{"payload que não é objeto", noteMut(0, json.RawMessage(`[]`)), CodeInvalidField},
		{"base > 0 numa nota que a nuvem não tem", noteMut(3, rawJSON(t, map[string]any{"cor": "azul"})), CodeInvalidBase},
	}
	for _, c := range cases {
		out := DecideNote(c.m, nil, false, nil, pc, testNow)
		if out.Result.Status != StatusRejected || out.Result.Code != c.code || out.Write != WriteNone {
			t.Errorf("%s: %+v, quero rejected %s", c.name, out.Result, c.code)
		}
	}
	// 101 marcadores.
	ids := make([]string, MaxNoteLabels+1)
	for i := range ids {
		ids[i] = NewUUIDv7()
	}
	if out := DecideNote(noteMut(0, notePayload(t, map[string]any{"marcadores": ids})), nil, false, nil, pc, testNow); out.Result.Code != CodeInvalidField {
		t.Errorf("101 marcadores: %+v", out.Result)
	}

	// O mudar: as chaves do que mudou.
	cur := existingNote(4, 2, 1, "x", false)
	for name, c := range map[string]struct {
		payload any
		code    string
	}{
		"vazio":                           {map[string]any{}, CodeInvalidField},
		"titulo sem corpo":                {map[string]any{"titulo": "a"}, CodeInvalidField},
		"corpo sem titulo":                {map[string]any{"corpo": "a"}, CodeInvalidField},
		"corpo_base sem o conteúdo":       {map[string]any{"corpo_base": "a"}, CodeInvalidField},
		"escolha sem conflict_at":         {map[string]any{"resolver_conflito": "manter"}, CodeInvalidField},
		"escolha fora da lista":           {map[string]any{"resolver_conflito": "outra", "conflict_at": trashAt(testNow)}, CodeInvalidField},
		"created_at no mudar":             {map[string]any{"created_at": trashAt(testNow)}, CodeUnknownField},
		"ditado_de_origem no mudar":       {map[string]any{"ditado_de_origem": nil}, CodeUnknownField},
		"campo estranho":                  {map[string]any{"contexto": "x"}, CodeUnknownField},
		"lixeira no futuro":               {map[string]any{"deleted_at": trashAt(testNow.Add(48 * time.Hour))}, CodeFutureDate},
		"posição fora do alfabeto":        {map[string]any{"posicao": "a-b"}, CodeInvalidField},
		"base maior que a versão da nota": {nil, CodeInvalidBase},
	} {
		m := noteMut(4, rawJSON(t, c.payload))
		if c.payload == nil {
			m = noteMut(5, rawJSON(t, map[string]any{"cor": "azul"}))
		}
		out := DecideNote(m, cur, false, nil, pc, testNow)
		if out.Result.Status != StatusRejected || out.Result.Code != c.code {
			t.Errorf("mudar, %s: %+v, quero rejected %s", name, out.Result, c.code)
		}
	}
}

func TestDecideNote_LapideEApagar(t *testing.T) {
	if out := DecideNote(noteMut(0, notePayload(t, nil)), nil, true, nil, pc, testNow); out.Result.Status != StatusGone || out.Result.Reason != ReasonDeleted {
		t.Errorf("criar sobre a lápide: %+v, quero gone apagado", out.Result)
	}
	if out := DecideNote(noteMut(3, rawJSON(t, map[string]any{"cor": "azul"})), nil, true, nil, pc, testNow); out.Result.Status != StatusGone {
		t.Errorf("mudar sobre a lápide: %+v, quero gone apagado", out.Result)
	}
	// Apagar a que existe: a versão seguinte.
	out := DecideNote(noteDelete(), existingNote(4, 2, 1, "x", true), false, nil, pc, testNow)
	if out.Result.Status != StatusApplied || out.Result.EntityVersion != 5 || out.Write != WriteTombstone {
		t.Errorf("apagar a que existe: %+v %v", out.Result, out.Write)
	}
	// A que não existe nem tem lápide: a lápide nasce, versão 1.
	out = DecideNote(noteDelete(), nil, false, nil, pc, testNow)
	if out.Result.Status != StatusApplied || out.Result.EntityVersion != 1 || out.Write != WriteTombstone {
		t.Errorf("apagar a desconhecida: %+v %v", out.Result, out.Write)
	}
	// A que já tem lápide: gone.
	out = DecideNote(noteDelete(), nil, true, nil, pc, testNow)
	if out.Result.Status != StatusGone || out.Result.Reason != ReasonDeleted || out.Write != WriteNone {
		t.Errorf("apagar de novo: %+v %v", out.Result, out.Write)
	}
	// O payload do apagar é vazio.
	m := noteDelete()
	m.Payload = rawJSON(t, map[string]any{"titulo": "x"})
	if out := DecideNote(m, nil, false, nil, pc, testNow); out.Result.Code != CodeUnknownField {
		t.Errorf("apagar com payload: %+v", out.Result)
	}
	for _, p := range []string{"", "null", "{}"} {
		m.Payload = json.RawMessage(p)
		if out := DecideNote(m, nil, false, nil, pc, testNow); out.Result.Status != StatusApplied {
			t.Errorf("apagar com payload %q: %+v", p, out.Result)
		}
	}
}

func TestDecideNote_RegrasDeMescla(t *testing.T) {
	type want struct {
		status   Status
		merged   bool
		version  int64
		corpo    string
		trashed  bool
		selo     string // o corpo do selo; "" sem selo
		contentV int64
	}
	base := "- [ ] leite\n- [ ] pão"
	cases := []struct {
		name string
		cur  *Note
		m    Mutation
		want want
	}{
		{
			name: "base atual, o PC muda o conteúdo: applied",
			cur:  existingNote(3, 3, 1, base, false),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "outro"})),
			want: want{status: StatusApplied, version: 4, corpo: "outro", contentV: 4},
		},
		{
			name: "o mesmo conteúdo: nada a gravar",
			cur:  existingNote(3, 3, 1, base, false),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": base})),
			want: want{status: StatusApplied, version: 3, corpo: base, contentV: 3},
		},
		{
			name: "os dois mudaram o conteúdo: conflito, o do PC vai para o selo",
			cur:  existingNote(5, 5, 1, "da nuvem", false),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "do PC"})),
			want: want{status: StatusConflict, version: 6, corpo: "da nuvem", selo: "do PC", contentV: 5},
		},
		{
			name: "os dois só marcaram itens diferentes: mesclado, sem selo",
			cur:  existingNote(5, 5, 1, "- [ ] pão\n- [x] leite", false),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "- [ ] leite\n- [x] pão", "corpo_base": base})),
			want: want{status: StatusApplied, merged: true, version: 6, corpo: "- [x] pão\n- [x] leite", contentV: 6},
		},
		{
			name: "marcou itens, mas sem o corpo da base: conflito normal",
			cur:  existingNote(5, 5, 1, "- [ ] pão\n- [x] leite", false),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "- [ ] leite\n- [x] pão"})),
			want: want{status: StatusConflict, version: 6, corpo: "- [ ] pão\n- [x] leite", selo: "- [ ] leite\n- [x] pão", contentV: 5},
		},
		{
			name: "marcou itens, mas o título mudou: conflito normal",
			cur:  existingNote(5, 5, 1, "- [ ] pão\n- [x] leite", false),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Feira", "corpo": "- [ ] leite\n- [x] pão", "corpo_base": base})),
			want: want{status: StatusConflict, version: 6, corpo: "- [ ] pão\n- [x] leite", selo: "- [ ] leite\n- [x] pão", contentV: 5},
		},
		{
			name: "marcou itens, mas a nota está na lixeira: conflito normal",
			cur:  func() *Note { n := existingNote(5, 5, 5, "- [ ] pão\n- [x] leite", true); return n }(),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "- [ ] leite\n- [x] pão", "corpo_base": base})),
			want: want{status: StatusConflict, version: 6, corpo: "- [ ] pão\n- [x] leite", trashed: true, selo: "- [ ] leite\n- [x] pão", contentV: 5},
		},
		{
			name: "a lixeira de um lado e o conteúdo do outro: mesclado",
			cur:  existingNote(4, 1, 4, base, true),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "novo", "deleted_at": trashAt(testNow.Add(-time.Hour))})),
			want: want{status: StatusApplied, merged: true, version: 5, corpo: "novo", trashed: true, contentV: 5},
		},
		{
			name: "edição antiga contra a lixeira: o selo",
			cur:  existingNote(4, 1, 4, base, true),
			m:    noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "edição antiga"})),
			want: want{status: StatusConflict, version: 5, corpo: base, trashed: true, selo: "edição antiga", contentV: 1},
		},
		{
			name: "restaurar contra uma lixeira mais nova: a lixeira fica, mesclado",
			cur:  existingNote(4, 1, 4, base, true),
			m:    noteMut(3, rawJSON(t, map[string]any{"deleted_at": nil})),
			want: want{status: StatusApplied, merged: true, version: 5, corpo: base, trashed: true, contentV: 1},
		},
		{
			name: "restaurar sobre a base: sai da lixeira",
			cur:  existingNote(4, 1, 4, base, true),
			m:    noteMut(4, rawJSON(t, map[string]any{"deleted_at": nil})),
			want: want{status: StatusApplied, version: 5, corpo: base, contentV: 1},
		},
		{
			name: "a base 0 numa nota que existe: o conteúdo diferente vai para o selo",
			cur:  existingNote(4, 2, 1, "da nuvem", false),
			m:    noteMut(0, notePayload(t, map[string]any{"corpo": "do PC"})),
			want: want{status: StatusConflict, version: 5, corpo: "da nuvem", selo: "do PC", contentV: 2},
		},
		{
			name: "a base 0 com o mesmo conteúdo: mesclado, sem selo",
			cur:  existingNote(4, 2, 1, base, false),
			m:    noteMut(0, notePayload(t, nil)),
			want: want{status: StatusApplied, merged: true, version: 5, corpo: base, contentV: 2},
		},
	}
	for _, c := range cases {
		out := DecideNote(c.m, c.cur, false, nil, pc, testNow)
		r := out.Result
		n := out.NextNote
		if out.Write == WriteNone {
			n = *c.cur
		}
		switch {
		case r.Status != c.want.status || r.Merged != c.want.merged || r.EntityVersion != c.want.version:
			t.Errorf("%s: %+v, quero %s mesclado=%v v%d", c.name, r, c.want.status, c.want.merged, c.want.version)
		case n.Corpo != c.want.corpo || (n.DeletedAt != nil) != c.want.trashed || n.ContentVersion != c.want.contentV:
			t.Errorf("%s: corpo %q lixeira %v contentV %d; quero %q %v %d", c.name, n.Corpo, n.DeletedAt != nil, n.ContentVersion,
				c.want.corpo, c.want.trashed, c.want.contentV)
		case c.want.selo == "" && n.Conflict != nil:
			t.Errorf("%s: selo inesperado %+v", c.name, n.Conflict)
		case c.want.selo != "" && (n.Conflict == nil || n.Conflict.Corpo != c.want.selo || !n.Conflict.At.Equal(testNow) ||
			!sameDevice(n.Conflict.DeviceID, pc.Device())):
			t.Errorf("%s: selo %+v, quero o corpo %q do PC", c.name, n.Conflict, c.want.selo)
		case c.want.status == StatusConflict && (r.NoteState == nil || r.NoteState.Version != c.want.version):
			t.Errorf("%s: o conflict sem o state: %+v", c.name, r)
		case out.Write != WriteNone && n.Version != c.want.version:
			t.Errorf("%s: a linha gravada na versão %d, quero %d", c.name, n.Version, c.want.version)
		}
	}
}

// O último que escreve vence, sem selo: a cor, fixada, arquivada, os
// marcadores e a posição, mesmo com a base velha e o conteúdo da nuvem mudado.
func TestDecideNote_OsEnfeitesSemSelo(t *testing.T) {
	cur := existingNote(6, 6, 1, "da nuvem", false)
	out := DecideNote(noteMut(2, rawJSON(t, map[string]any{
		"cor": nil, "fixada": true, "arquivada": true, "posicao": "1", "marcadores": []string{testLabel2, testLabelX},
	})), cur, false, map[string]bool{testLabelX: true}, pc, testNow)
	n := out.NextNote
	switch {
	case out.Result.Status != StatusApplied || !out.Result.Merged || out.Result.EntityVersion != 7:
		t.Errorf("os enfeites: %+v", out.Result)
	case n.Cor != nil || !n.Fixada || !n.Arquivada || n.Posicao != "1" || n.Conflict != nil:
		t.Errorf("a linha: %+v", n)
	case len(n.Marcadores) != 1 || n.Marcadores[0] != testLabel2:
		t.Errorf("os marcadores (o com lápide sai): %v", n.Marcadores)
	case n.Corpo != "da nuvem" || n.ContentVersion != 6 || !n.UpdatedAt.Equal(cur.UpdatedAt):
		t.Errorf("o conteúdo e o updated_at não mudam com enfeite: %+v", n)
	}
	// Base atual e o mesmo valor: nada a gravar.
	cur = existingNote(6, 6, 1, "x", false)
	out = DecideNote(noteMut(6, rawJSON(t, map[string]any{"cor": "verde", "posicao": "V", "marcadores": []string{testLabel1}})), cur, false, nil, pc, testNow)
	if out.Write != WriteNone || out.Result.EntityVersion != 6 {
		t.Errorf("os mesmos enfeites: %+v %v", out.Result, out.Write)
	}
}

// O campo que o próprio PC mudou por último é conhecido por ele, mesmo com a
// base velha (o applied mesclado), como no ditado.
func TestDecideNote_OCampoDoProprioPC(t *testing.T) {
	cur := existingNote(5, 5, 1, "minha edição", false)
	mine := testDevice
	cur.ContentDeviceID = &mine
	out := DecideNote(noteMut(3, rawJSON(t, map[string]any{"titulo": "Mercado", "corpo": "minha segunda edição"})), cur, false, nil, pc, testNow)
	if out.Result.Status != StatusApplied || out.NextNote.Corpo != "minha segunda edição" || out.NextNote.Conflict != nil {
		t.Errorf("a segunda edição do mesmo PC: %+v %+v", out.Result, out.NextNote)
	}
}

func TestDecideNote_EscolhaDoSelo(t *testing.T) {
	at := testNow.Add(-time.Hour)
	withSeal := func() *Note {
		n := existingNote(5, 4, 1, "da nuvem", false)
		n.Conflict = &NoteConflict{Titulo: "Mercado (PC)", Corpo: "do PC", DeviceID: strPtr(testDevice), At: at}
		return n
	}
	// manter: o selo sai, o conteúdo fica.
	out := DecideNote(noteMut(5, rawJSON(t, map[string]any{"resolver_conflito": "manter", "conflict_at": trashAt(at)})), withSeal(), false, nil, pc, testNow)
	if out.Result.Status != StatusApplied || out.Result.Merged || out.NextNote.Conflict != nil || out.NextNote.Corpo != "da nuvem" || out.NextNote.ContentVersion != 4 {
		t.Errorf("manter: %+v %+v", out.Result, out.NextNote)
	}
	// trocar: o selo vira o conteúdo (título e corpo), como uma mudança do PC.
	out = DecideNote(noteMut(5, rawJSON(t, map[string]any{"resolver_conflito": "trocar", "conflict_at": trashAt(at)})), withSeal(), false, nil, pc, testNow)
	n := out.NextNote
	if out.Result.Status != StatusApplied || n.Conflict != nil || n.Titulo != "Mercado (PC)" || n.Corpo != "do PC" || n.ContentVersion != 6 ||
		!sameDevice(n.ContentDeviceID, pc.Device()) || !n.UpdatedAt.Equal(testNow) {
		t.Errorf("trocar: %+v %+v", out.Result, n)
	}
	// Outro selo (outro conflict_at): a escolha cai, mesclado, e o selo fica.
	out = DecideNote(noteMut(5, rawJSON(t, map[string]any{"resolver_conflito": "trocar", "conflict_at": trashAt(at.Add(time.Second))})), withSeal(), false, nil, pc, testNow)
	if out.Result.Status != StatusApplied || !out.Result.Merged || out.NextNote.Conflict == nil || out.NextNote.Corpo != "da nuvem" {
		t.Errorf("a escolha que cai: %+v %+v", out.Result, out.NextNote)
	}
	// O conteúdo mudou por outro aparelho depois da base: a escolha cai.
	n2 := withSeal()
	n2.ContentVersion = 5
	out = DecideNote(noteMut(4, rawJSON(t, map[string]any{"resolver_conflito": "manter", "conflict_at": trashAt(at)})), n2, false, nil, pc, testNow)
	if !out.Result.Merged || out.NextNote.Conflict == nil {
		t.Errorf("a escolha sobre um conteúdo mudado depois: %+v %+v", out.Result, out.NextNote)
	}
}

func labelMut(base int64, payload any) Mutation {
	var raw json.RawMessage
	if payload != nil {
		b, _ := json.Marshal(payload)
		raw = b
	}
	return Mutation{MutationID: testMutation, EntityType: EntityLabel, EntityID: testEntity, Operation: OpUpsert, BaseVersion: base, Payload: raw}
}

func TestDecideLabel_ATabela(t *testing.T) {
	created := testNow.Add(-time.Hour).Format(time.RFC3339Nano)
	criar := map[string]any{"nome": "  Mercado   do  mês ", "created_at": created}
	cur := func(v int64) *Label {
		return &Label{ID: testEntity, UserID: testUser, Nome: "Mercado", CreatedAt: testNow.Add(-48 * time.Hour), UpdatedAt: testNow.Add(-48 * time.Hour), Version: v}
	}
	out := DecideLabel(labelMut(0, criar), nil, false, pc, testNow)
	if out.Result.Status != StatusApplied || out.Result.EntityVersion != 1 || out.Write != WriteInsert || out.NextLabel.Nome != "Mercado do mês" ||
		!out.NextLabel.UpdatedAt.Equal(testNow) {
		t.Errorf("criar: %+v %+v", out.Result, out.NextLabel)
	}
	for _, c := range []struct {
		name    string
		m       Mutation
		cur     *Label
		tomb    bool
		status  Status
		code    string
		version int64
		merged  bool
		write   Write
		nome    string
	}{
		{"criar sobre a lápide", labelMut(0, criar), nil, true, StatusGone, "", 0, false, WriteNone, ""},
		{"base > 0 sem a linha", labelMut(2, map[string]any{"nome": "x"}), nil, false, StatusRejected, CodeInvalidBase, 0, false, WriteNone, ""},
		{"base > versão", labelMut(4, map[string]any{"nome": "x"}), cur(3), false, StatusRejected, CodeInvalidBase, 0, false, WriteNone, ""},
		{"renomear sobre a base", labelMut(3, map[string]any{"nome": "Feira"}), cur(3), false, StatusApplied, "", 4, false, WriteUpdate, "Feira"},
		{"o mesmo nome sobre a base", labelMut(3, map[string]any{"nome": " Mercado"}), cur(3), false, StatusApplied, "", 3, false, WriteNone, ""},
		{"renomear com a base velha: vence, mesclado", labelMut(1, map[string]any{"nome": "Feira"}), cur(3), false, StatusApplied, "", 4, true, WriteUpdate, "Feira"},
		{"o mesmo nome com a base velha: versão nova, mesclado", labelMut(1, map[string]any{"nome": "Mercado"}), cur(3), false, StatusApplied, "", 4, true, WriteUpdate, "Mercado"},
		{"a base 0 sobre um que existe (o recomeço)", labelMut(0, criar), cur(3), false, StatusApplied, "", 4, true, WriteUpdate, "Mercado do mês"},
		{"nome vazio", labelMut(3, map[string]any{"nome": "   "}), cur(3), false, StatusRejected, CodeInvalidField, 0, false, WriteNone, ""},
		{"nome de 51 caracteres", labelMut(3, map[string]any{"nome": strings.Repeat("é", 51)}), cur(3), false, StatusRejected, CodeInvalidField, 0, false, WriteNone, ""},
		{"nome com quebra de linha", labelMut(3, map[string]any{"nome": "a\nb"}), cur(3), false, StatusRejected, CodeInvalidField, 0, false, WriteNone, ""},
		{"created_at no mudar", labelMut(3, map[string]any{"nome": "a", "created_at": created}), cur(3), false, StatusRejected, CodeUnknownField, 0, false, WriteNone, ""},
		{"sem o nome no mudar", labelMut(3, map[string]any{}), cur(3), false, StatusRejected, CodeInvalidField, 0, false, WriteNone, ""},
		{"criar sem created_at", labelMut(0, map[string]any{"nome": "a"}), nil, false, StatusRejected, CodeInvalidField, 0, false, WriteNone, ""},
	} {
		out := DecideLabel(c.m, c.cur, c.tomb, pc, testNow)
		r := out.Result
		if r.Status != c.status || r.Code != c.code || r.EntityVersion != c.version || r.Merged != c.merged || out.Write != c.write ||
			c.nome != "" && out.NextLabel.Nome != c.nome {
			t.Errorf("%s: %+v %v %q", c.name, r, out.Write, out.NextLabel.Nome)
		}
	}
	// O apagar: a que existe, a desconhecida e a com lápide.
	del := Mutation{MutationID: testMutation, EntityType: EntityLabel, EntityID: testEntity, Operation: OpDelete}
	if out := DecideLabel(del, cur(3), false, pc, testNow); out.Result.EntityVersion != 4 || out.Write != WriteTombstone {
		t.Errorf("apagar o que existe: %+v", out.Result)
	}
	if out := DecideLabel(del, nil, false, pc, testNow); out.Result.EntityVersion != 1 || out.Write != WriteTombstone {
		t.Errorf("apagar o desconhecido: %+v", out.Result)
	}
	if out := DecideLabel(del, nil, true, pc, testNow); out.Result.Status != StatusGone || out.Write != WriteNone {
		t.Errorf("apagar de novo: %+v", out.Result)
	}
}

// A nota e o marcador passam pelo CheckMutation com upsert e delete, e o
// reenvio confere o tipo e a operação.
func TestCheckMutation_NotaEMarcador(t *testing.T) {
	for _, et := range []string{EntityNote, EntityLabel} {
		for _, op := range []string{OpUpsert, OpDelete} {
			m := Mutation{MutationID: testMutation, EntityType: et, EntityID: testEntity, Operation: op}
			if code, ok := CheckMutation(m); code != "" || !ok {
				t.Errorf("%s %s: %q %v", et, op, code, ok)
			}
		}
		m := Mutation{MutationID: testMutation, EntityType: et, EntityID: testEntity, Operation: "tombstone"}
		if code, _ := CheckMutation(m); code != CodeInvalidOperation {
			t.Errorf("%s tombstone: %q", et, code)
		}
		m = Mutation{MutationID: testMutation, EntityType: et, EntityID: testEntity, Operation: OpDelete, BaseVersion: -1}
		if code, _ := CheckMutation(m); code != "" {
			t.Errorf("%s delete com base negativa (o apagar não olha a base): %q", et, code)
		}
	}
	// O ditado continua sem delete.
	if code, _ := CheckMutation(Mutation{MutationID: testMutation, EntityType: EntityTranscription, EntityID: testEntity, Operation: OpDelete}); code != CodeInvalidOperation {
		t.Errorf("delete de ditado: %q", code)
	}

	// O reenvio: o mesmo id com outro tipo, ou outra operação, é mutacao_reusada.
	noteApplied := Applied{EntityType: EntityNote, EntityID: testEntity, DeviceID: testDevice, Operation: OpUpsert, Status: StatusApplied, Version: 2}
	if r := Duplicate(noteApplied, noteMut(1, nil), pc); r.Status != StatusDuplicate || r.EntityVersion != 2 {
		t.Errorf("o reenvio da nota: %+v", r)
	}
	if r := Duplicate(noteApplied, noteDelete(), pc); r.Code != CodeReusedMutation {
		t.Errorf("o mesmo id com o delete: %+v", r)
	}
	if r := Duplicate(noteApplied, labelMut(1, nil), pc); r.Code != CodeReusedMutation {
		t.Errorf("o mesmo id como marcador: %+v", r)
	}
	gone := Applied{EntityType: EntityNote, EntityID: testEntity, DeviceID: testDevice, Operation: OpDelete, Status: StatusGone, Code: ReasonDeleted}
	if r := Duplicate(gone, noteDelete(), pc); r.Status != StatusDuplicate || r.Original != StatusGone || r.Reason != ReasonDeleted {
		t.Errorf("o reenvio do apagar que foi gone: %+v", r)
	}
}

// O virou_nota do ditado (N7): no criar e no mudar, sem conflito; o último que
// escreve vence e só a versão da linha sobe.
func TestDecidePush_VirouNota(t *testing.T) {
	nota := "0192a000-0000-7000-8000-0000000000f1"
	out := DecidePush(mutation(0, createPayload(t, map[string]any{"virou_nota": strings.ToUpper(nota)})), nil, false, pc, testNow)
	if out.Write != WriteInsert || out.Next.VirouNota == nil || *out.Next.VirouNota != nota {
		t.Fatalf("criar com virou_nota: %+v %v", out.Result, out.Next.VirouNota)
	}
	if out := DecidePush(mutation(0, createPayload(t, map[string]any{"virou_nota": "nova:abc"})), nil, false, pc, testNow); out.Result.Code != CodeInvalidField {
		t.Errorf("virou_nota que não é UUID: %+v", out.Result)
	}

	cur := existing(3, 3, 1, "texto", false)
	// Mudar só a ligação: applied, versão nova, sem mexer no texto nem na lixeira.
	out = DecidePush(mutation(3, rawJSON(t, map[string]any{"virou_nota": nota})), cur, false, pc, testNow)
	if out.Result.Status != StatusApplied || out.Result.Merged || out.Next.Version != 4 || out.Next.TextVersion != 3 || out.Next.TrashVersion != 1 ||
		out.Next.VirouNota == nil || *out.Next.VirouNota != nota {
		t.Errorf("mudar a ligação: %+v %+v", out.Result, out.Next)
	}
	// Com a base velha e o texto mudado por outro: a ligação vale, sem selo.
	cur = existing(5, 5, 1, "da nuvem", false)
	cur.VirouNota = strPtr("0192a000-0000-7000-8000-0000000000f2")
	out = DecidePush(mutation(2, rawJSON(t, map[string]any{"virou_nota": nil})), cur, false, pc, testNow)
	if out.Result.Status != StatusApplied || !out.Result.Merged || out.Next.VirouNota != nil || out.Next.Conflict != nil {
		t.Errorf("tirar a ligação com a base velha: %+v %+v", out.Result, out.Next)
	}
	// A mesma ligação sobre a base: nada a gravar.
	cur = existing(3, 3, 1, "texto", false)
	cur.VirouNota = strPtr(nota)
	if out := DecidePush(mutation(3, rawJSON(t, map[string]any{"virou_nota": nota})), cur, false, pc, testNow); out.Write != WriteNone {
		t.Errorf("a mesma ligação: %+v", out.Result)
	}
	// O recomeço (base 0) sem a chave: a ligação da nuvem fica.
	out = DecidePush(mutation(0, createPayload(t, map[string]any{"final_text": "texto"})), cur, false, pc, testNow)
	if out.Next.VirouNota == nil || *out.Next.VirouNota != nota {
		t.Errorf("o recomeço sem a chave tirou a ligação: %+v", out.Next.VirouNota)
	}
}
