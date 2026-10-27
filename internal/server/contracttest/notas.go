package contracttest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Os casos das notas e dos marcadores de nota (ADR-022, N7): o push e o pull
// das duas entidades, a capacidade "entidades", o virou_nota do ditado, as
// listagens do celular, o "apagar meus dados" e a arrumação dos eventos, pela
// API, contra a memória e contra o Postgres.

// --- As mutações, como o PC as manda. ---

// notaInteira é o payload de criar de uma nota, com as trocas pedidas (nil
// apaga a chave).
func notaInteira(changes map[string]any) map[string]any {
	p := map[string]any{
		"titulo":     "Mercado",
		"corpo":      "- [ ] leite\n- [ ] pão",
		"cor":        nil,
		"fixada":     false,
		"arquivada":  false,
		"posicao":    "V",
		"marcadores": []string{},
		"created_at": now0.Add(-time.Hour).Format(time.RFC3339Nano),
	}
	for k, v := range changes {
		if v == nil {
			delete(p, k)
			continue
		}
		p[k] = v
	}
	return p
}

func criarNota(id string, changes map[string]any) mut {
	return mut{MutationID: newID(), EntityType: "note", EntityID: id, Operation: "upsert", Payload: notaInteira(changes)}
}

func mudarNota(id string, base int64, fields map[string]any) mut {
	return mut{MutationID: newID(), EntityType: "note", EntityID: id, Operation: "upsert", BaseVersion: base, Payload: fields}
}

func apagarNota(id string) mut {
	return mut{MutationID: newID(), EntityType: "note", EntityID: id, Operation: "delete"}
}

func criarMarcador(id, nome string) mut {
	return mut{MutationID: newID(), EntityType: "label", EntityID: id, Operation: "upsert",
		Payload: map[string]any{"nome": nome, "created_at": now0.Add(-time.Hour).Format(time.RFC3339Nano)}}
}

func renomearMarcador(id string, base int64, nome string) mut {
	return mut{MutationID: newID(), EntityType: "label", EntityID: id, Operation: "upsert", BaseVersion: base,
		Payload: map[string]any{"nome": nome}}
}

func apagarMarcador(id string) mut {
	return mut{MutationID: newID(), EntityType: "label", EntityID: id, Operation: "delete"}
}

// --- O que volta. ---

type notaResultJSON struct {
	MutationID    string          `json:"mutation_id"`
	Status        string          `json:"status"`
	Original      string          `json:"original"`
	EntityVersion int64           `json:"entity_version"`
	Mesclado      bool            `json:"mesclado"`
	Code          string          `json:"code"`
	Motivo        string          `json:"motivo"`
	State         json.RawMessage `json:"state"`
}

type notaSeloJSON struct {
	Titulo     string    `json:"titulo"`
	Corpo      string    `json:"corpo"`
	DeviceID   *string   `json:"device_id"`
	ConflictAt time.Time `json:"conflict_at"`
}

type notaJSON struct {
	ID               string        `json:"id"`
	Titulo           string        `json:"titulo"`
	Corpo            string        `json:"corpo"`
	Cor              *string       `json:"cor"`
	Fixada           bool          `json:"fixada"`
	Arquivada        bool          `json:"arquivada"`
	Posicao          string        `json:"posicao"`
	Marcadores       []string      `json:"marcadores"`
	DitadoDeOrigem   *string       `json:"ditado_de_origem"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	DeletedAt        *time.Time    `json:"deleted_at"`
	Version          int64         `json:"version"`
	VersaoEmConflito *notaSeloJSON `json:"versao_em_conflito"`
}

type marcadorJSON struct {
	ID        string    `json:"id"`
	Nome      string    `json:"nome"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version"`
}

type notasPullJSON struct {
	Changes   []typedChangeJSON `json:"changes"`
	LastSeq   int64             `json:"last_seq"`
	HasMore   bool              `json:"has_more"`
	SyncEpoch string            `json:"sync_epoch"`
	Entidades []string          `json:"entidades"`
}

// As chaves exatas do fio (o contrato da N7): nenhuma a mais, nenhuma a menos.
var (
	chavesDaNota = []string{"arquivada", "cor", "corpo", "created_at", "deleted_at", "ditado_de_origem", "fixada", "id",
		"marcadores", "posicao", "titulo", "updated_at", "version", "versao_em_conflito"}
	chavesDoMarcador = []string{"created_at", "id", "nome", "updated_at", "version"}
	chavesDoSelo     = []string{"conflict_at", "corpo", "device_id", "titulo"}
)

// conferirChaves decodifica raw num objeto e confere as chaves.
func conferirChaves(t *testing.T, raw json.RawMessage, quero []string, o string) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("%s: não é um objeto JSON: %v\n%s", o, err, raw)
	}
	var got []string
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	quero = append([]string(nil), quero...)
	sort.Strings(quero)
	if strings.Join(got, ",") != strings.Join(quero, ",") {
		t.Fatalf("%s: as chaves %v, quero %v", o, got, quero)
	}
	if sel, ok := obj["versao_em_conflito"]; ok && string(sel) != "null" {
		conferirChaves(t, sel, chavesDoSelo, o+": versao_em_conflito")
	}
}

func lerNota(t *testing.T, raw json.RawMessage) notaJSON {
	t.Helper()
	conferirChaves(t, raw, chavesDaNota, "a nota")
	var n notaJSON
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("a nota: %v", err)
	}
	if n.Marcadores == nil {
		t.Fatalf("a nota sem marcadores (quero sempre o array): %s", raw)
	}
	return n
}

func lerMarcador(t *testing.T, raw json.RawMessage) marcadorJSON {
	t.Helper()
	conferirChaves(t, raw, chavesDoMarcador, "o marcador")
	var l marcadorJSON
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatalf("o marcador: %v", err)
	}
	return l
}

// pushNota manda uma mutação de nota ou de marcador e devolve o resultado.
func (w *world) pushNota(token string, m mut) notaResultJSON {
	w.t.Helper()
	rec := w.pushRaw(token, m)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("push: status %d, quero 200: %s", rec.Code, rec.Body.String())
	}
	p := decode[struct {
		Results []notaResultJSON `json:"results"`
	}](w.t, rec)
	if len(p.Results) != 1 || p.Results[0].MutationID != m.MutationID {
		w.t.Fatalf("push: resultados %+v para a mutação %s", p.Results, m.MutationID)
	}
	return p.Results[0]
}

// mustNota exige o applied com a versão (e, se mesclado, o mesclado).
func (w *world) mustNota(token string, m mut, version int64, mesclado bool) {
	w.t.Helper()
	if r := w.pushNota(token, m); r.Status != "applied" || r.EntityVersion != version || r.Mesclado != mesclado {
		w.t.Fatalf("%s %s %s (base %d) = %+v, quero applied v%d mesclado=%v", m.EntityType, m.Operation, m.EntityID,
			m.BaseVersion, r, version, mesclado)
	}
}

func (w *world) pullNotas(token string, after int64, limit int) notasPullJSON {
	w.t.Helper()
	path := fmt.Sprintf("/api/v1/sync/pull?after_seq=%d", after)
	if limit > 0 {
		path += fmt.Sprintf("&limit=%d", limit)
	}
	rec := w.do(http.MethodGet, path, token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("pull: status %d, quero 200: %s", rec.Code, rec.Body.String())
	}
	return decode[notasPullJSON](w.t, rec)
}

// mudancaDe acha a mudança de uma entidade no pull.
func mudancaDe(p notasPullJSON, id string) (typedChangeJSON, bool) {
	for _, c := range p.Changes {
		if c.EntityID == id {
			return c, true
		}
	}
	return typedChangeJSON{}, false
}

// notaNoPull lê o estado da nota no pull do começo.
func (w *world) notaNoPull(token, id string) notaJSON {
	w.t.Helper()
	c, ok := mudancaDe(w.pullNotas(token, 0, 0), id)
	if !ok || c.EntityType != "note" || c.Operation != "upsert" {
		w.t.Fatalf("a nota %s no pull = %+v %v, quero note upsert", id, c, ok)
	}
	n := lerNota(w.t, c.Payload)
	if n.Version != c.EntityVersion {
		w.t.Fatalf("a nota %s: a versão do payload (%d) não é a da mudança (%d)", id, n.Version, c.EntityVersion)
	}
	return n
}

type listaDeNotasJSON struct {
	Items   []json.RawMessage `json:"items"`
	HasMore bool              `json:"has_more"`
}

// listarNotas é o GET /notes do celular, com a vista (vazia: sem o parâmetro).
func (w *world) listarNotas(token, vista string) ([]notaJSON, bool) {
	w.t.Helper()
	path := "/api/v1/notes"
	if vista != "" {
		path += "?vista=" + vista
	}
	rec := w.do(http.MethodGet, path, token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	p := decode[listaDeNotasJSON](w.t, rec)
	out := make([]notaJSON, 0, len(p.Items))
	for _, raw := range p.Items {
		out = append(out, lerNota(w.t, raw))
	}
	return out, p.HasMore
}

func (w *world) listarMarcadores(token string) ([]marcadorJSON, bool) {
	w.t.Helper()
	rec := w.do(http.MethodGet, "/api/v1/labels", token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET /labels: %d %s", rec.Code, rec.Body.String())
	}
	p := decode[listaDeNotasJSON](w.t, rec)
	out := make([]marcadorJSON, 0, len(p.Items))
	for _, raw := range p.Items {
		out = append(out, lerMarcador(w.t, raw))
	}
	return out, p.HasMore
}

func idsDe(ns []notaJSON) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

// --- Os casos. ---

// O PC cria um marcador, uma nota com ele e um ditado que virou a nota; o outro
// PC recebe tudo no pull, em ordem de seq, no formato do contrato, e a
// capacidade "entidades" vem sempre.
func testNotasCriaEPull(t *testing.T, w *world) {
	if p := w.pullNotas(pcA2, 0, 0); strings.Join(p.Entidades, ",") != "transcription,learned,note,label" {
		t.Fatalf("entidades = %v, quero transcription,learned,note,label (mesmo sem nada)", p.Entidades)
	}
	label, note, unknown, dict := newID(), newID(), newID(), newID()
	w.mustNota(pcA1, criarMarcador(label, "  Mercado   do mês "), 1, false) // seq 1
	w.mustNota(pcA1, criarNota(note, map[string]any{                        // seq 2
		"cor": "verde", "fixada": true, "marcadores": []string{unknown, strings.ToUpper(label)}, "ditado_de_origem": dict,
	}), 1, false)
	d := dictation("vira nota")
	d["virou_nota"] = note
	if r := w.pushOne(pcA1, createWith(dict, d)); r.Status != "applied" { // seq 3
		t.Fatalf("o ditado com virou_nota: %+v", r)
	}

	p := w.pullNotas(pcA2, 0, 0)
	if len(p.Changes) != 3 || p.LastSeq != 3 || p.HasMore {
		t.Fatalf("o pull = %+v, quero as 3 mudanças", p)
	}
	if strings.Join(p.Entidades, ",") != "transcription,learned,note,label" {
		t.Errorf("entidades = %v", p.Entidades)
	}
	for i, quero := range []string{"label", "note", "transcription"} {
		c := p.Changes[i]
		if c.Seq != int64(i+1) || c.EntityType != quero || c.Operation != "upsert" || c.EntityVersion != 1 ||
			c.DeviceID == nil || *c.DeviceID != w.devA1 {
			t.Errorf("a mudança %d = %+v, quero %s upsert v1 do PC 1", i+1, c, quero)
		}
	}
	l := lerMarcador(t, p.Changes[0].Payload)
	if l.ID != label || l.Nome != "Mercado do mês" || l.Version != 1 || !l.UpdatedAt.Equal(now0) {
		t.Errorf("o marcador = %+v (o nome limpo, a versão 1)", l)
	}
	n := lerNota(t, p.Changes[1].Payload)
	marc := []string{label, unknown}
	sort.Strings(marc)
	switch {
	case n.ID != note || n.Titulo != "Mercado" || n.Corpo != "- [ ] leite\n- [ ] pão" || n.Cor == nil || *n.Cor != "verde":
		t.Errorf("a nota = %+v", n)
	case !n.Fixada || n.Arquivada || n.Posicao != "V" || n.Version != 1 || n.DeletedAt != nil || n.VersaoEmConflito != nil:
		t.Errorf("a nota = %+v", n)
	case strings.Join(n.Marcadores, ",") != strings.Join(marc, ","):
		t.Errorf("os marcadores = %v, quero %v (em ordem de id; o desconhecido fica)", n.Marcadores, marc)
	case n.DitadoDeOrigem == nil || *n.DitadoDeOrigem != dict:
		t.Errorf("o ditado de origem = %v", n.DitadoDeOrigem)
	case !n.UpdatedAt.Equal(now0) || !n.CreatedAt.Equal(now0.Add(-time.Hour)):
		t.Errorf("as datas: %v %v", n.CreatedAt, n.UpdatedAt)
	}
	var item map[string]json.RawMessage
	if err := json.Unmarshal(p.Changes[2].Payload, &item); err != nil {
		t.Fatal(err)
	}
	if string(item["virou_nota"]) != `"`+note+`"` {
		t.Errorf("o virou_nota do ditado no pull = %s, quero %q", item["virou_nota"], note)
	}

	// O reenvio é duplicate com a mesma resposta; o mesmo id para outro tipo, ou
	// outra operação, é mutacao_reusada.
	m := criarNota(newID(), nil)
	w.mustNota(pcA1, m, 1, false)
	if r := w.pushNota(pcA1, m); r.Status != "duplicate" || r.Original != "applied" || r.EntityVersion != 1 {
		t.Errorf("o reenvio da nota = %+v", r)
	}
	outro := criarMarcador(m.EntityID, "x")
	outro.MutationID = m.MutationID
	if r := w.pushNota(pcA1, outro); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Errorf("o mesmo id como marcador = %+v", r)
	}
	del := apagarNota(m.EntityID)
	del.MutationID = m.MutationID
	if r := w.pushNota(pcA1, del); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Errorf("o mesmo id com o delete = %+v", r)
	}
	if r := w.pushNota(pcA2, m); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Errorf("o mesmo id por outro PC = %+v", r)
	}
	// Recusas não gastam seq.
	for _, c := range []struct {
		m    mut
		code string
	}{
		{criarNota(newID(), map[string]any{"contexto": "x"}), "campo_desconhecido"},
		{criarNota(newID(), map[string]any{"posicao": "V0"}), "campo_invalido"},
		{criarNota(newID(), map[string]any{"created_at": now0.Add(48 * time.Hour).Format(time.RFC3339)}), "data_no_futuro"},
		{mudarNota(newID(), 2, map[string]any{"cor": "azul"}), "base_invalida"},
		{criarMarcador(newID(), "a\nb"), "campo_invalido"},
	} {
		if r := w.pushNota(pcA1, c.m); r.Status != "rejected" || r.Code != c.code {
			t.Errorf("%v = %+v, quero %s", c.m.Payload, r, c.code)
		}
	}
	if p := w.pullNotas(pcA2, 0, 0); p.LastSeq != 4 {
		t.Errorf("o last_seq depois das recusas = %d, quero 4", p.LastSeq)
	}
}

// O conflito de conteúdo vai para o selo (o state do conflict, com o noteDTO),
// e a escolha pelo PC o resolve; a mescla da lista de marcar junta as marcas
// dos dois lados, sem selo.
func testNotasConflitoEMescla(t *testing.T, w *world) {
	n := newID()
	base := "- [ ] leite\n- [ ] pão"
	w.mustNota(pcA1, criarNota(n, nil), 1, false)
	// O PC 2 marca o leite (a base é a atual).
	w.mustNota(pcA2, mudarNota(n, 1, map[string]any{"titulo": "Mercado", "corpo": "- [ ] pão\n- [x] leite", "corpo_base": base}), 2, false)
	// O PC 1, sem ter visto, marca o pão: mesclado, sem selo.
	w.mustNota(pcA1, mudarNota(n, 1, map[string]any{"titulo": "Mercado", "corpo": "- [ ] leite\n- [x] pão", "corpo_base": base}), 3, true)
	if got := w.notaNoPull(pcA2, n); got.Corpo != "- [x] pão\n- [x] leite" || got.VersaoEmConflito != nil || got.Version != 3 {
		t.Fatalf("a mescla = %+v, quero os dois marcados e sem selo", got)
	}

	// O PC 2 edita o texto sobre a base 2 (a mescla da v3 foi do PC 1): conflito,
	// o texto dele vai para o selo, e o state é a nota com o selo. (O PC 1,
	// sobre a base 1, editaria sem conflito: o conteúdo de agora é dele.)
	edicao := mudarNota(n, 2, map[string]any{"titulo": "Mercado", "corpo": "- [ ] leite integral\n- [ ] pão", "cor": "azul"})
	r := w.pushNota(pcA2, edicao)
	if r.Status != "conflict" || r.EntityVersion != 4 || r.State == nil {
		t.Fatalf("o conflito = %+v", r)
	}
	st := lerNota(t, r.State)
	if st.Corpo != "- [x] pão\n- [x] leite" || st.VersaoEmConflito == nil || st.VersaoEmConflito.Corpo != "- [ ] leite integral\n- [ ] pão" ||
		st.VersaoEmConflito.Titulo != "Mercado" || st.VersaoEmConflito.DeviceID == nil || *st.VersaoEmConflito.DeviceID != w.devA2 ||
		st.Version != 4 || st.Cor == nil || *st.Cor != "azul" {
		t.Fatalf("o state do conflito = %+v %+v (a cor vale: o último que escreve vence)", st, st.VersaoEmConflito)
	}
	// O reenvio do conflito é duplicate, sem state.
	if r := w.pushNota(pcA2, edicao); r.Status != "duplicate" || r.Original != "conflict" || r.EntityVersion != 4 || r.State != nil {
		t.Fatalf("o reenvio do conflito = %+v", r)
	}
	// A escolha pelo PC: trocar.
	w.mustNota(pcA2, mudarNota(n, 4, map[string]any{
		"resolver_conflito": "trocar", "conflict_at": st.VersaoEmConflito.ConflictAt.Format(time.RFC3339Nano),
	}), 5, false)
	if got := w.notaNoPull(pcA2, n); got.Corpo != "- [ ] leite integral\n- [ ] pão" || got.VersaoEmConflito != nil {
		t.Fatalf("depois de trocar = %+v", got)
	}

	// Lixeira contra edição antiga: o PC 2 põe na lixeira, o PC 1 edita sobre
	// a base de antes: a lixeira fica, e a edição vai para o selo.
	w.mustNota(pcA2, mudarNota(n, 5, map[string]any{"deleted_at": now0.Add(-time.Minute).Format(time.RFC3339Nano)}), 6, false)
	if r := w.pushNota(pcA1, mudarNota(n, 5, map[string]any{"titulo": "Mercado", "corpo": "edição antiga"})); r.Status != "conflict" {
		t.Fatalf("a edição antiga contra a lixeira = %+v, quero conflict", r)
	}
	got := w.notaNoPull(pcA2, n)
	if got.DeletedAt == nil || got.VersaoEmConflito == nil || got.VersaoEmConflito.Corpo != "edição antiga" {
		t.Fatalf("a lixeira com o selo = %+v", got)
	}
	// Restaurar contra a lixeira mais nova: fica na lixeira, mesclado.
	w.mustNota(pcA1, mudarNota(n, 5, map[string]any{"deleted_at": nil}), 8, true)
	if got := w.notaNoPull(pcA2, n); got.DeletedAt == nil {
		t.Fatalf("o restaurar contra a lixeira mais nova tirou da lixeira: %+v", got)
	}
}

// Apagar de vez deixa a lápide (que não vence): o pull a leva, o create
// atrasado recebe gone, e apagar de novo também.
func testNotasApagarDeVez(t *testing.T, w *world) {
	n, u := newID(), newID()
	w.mustNota(pcA1, criarNota(n, nil), 1, false) // seq 1
	del := apagarNota(n)
	w.mustNota(pcA1, del, 2, false) // seq 2
	if r := w.pushNota(pcA1, del); r.Status != "duplicate" || r.Original != "applied" || r.EntityVersion != 2 {
		t.Errorf("o reenvio do apagar = %+v", r)
	}
	p := w.pullNotas(pcA2, 0, 0)
	c, ok := mudancaDe(p, n)
	if !ok || c.Seq != 2 || c.EntityType != "note" || c.Operation != "tombstone" || len(c.Payload) != 0 {
		t.Fatalf("a lápide no pull = %+v %v", c, ok)
	}
	for _, m := range []mut{criarNota(n, nil), mudarNota(n, 1, map[string]any{"cor": "azul"}), apagarNota(n)} {
		if r := w.pushNota(pcA2, m); r.Status != "gone" || r.Motivo != "apagado" {
			t.Errorf("%s depois de apagar = %+v, quero gone apagado", m.Operation, r)
		}
	}
	// Apagar o que a nuvem nunca teve: a lápide nasce, e o create atrasado
	// recebe gone.
	w.mustNota(pcA1, apagarNota(u), 1, false) // seq 3
	if r := w.pushNota(pcA2, criarNota(u, nil)); r.Status != "gone" || r.Motivo != "apagado" {
		t.Errorf("o create atrasado = %+v, quero gone apagado", r)
	}
	if c, ok := mudancaDe(w.pullNotas(pcA2, 2, 0), u); !ok || c.Operation != "tombstone" || c.Seq != 3 {
		t.Errorf("a lápide do que a nuvem não tinha = %+v %v", c, ok)
	}
	// O apagar com payload é recusado.
	bad := apagarNota(newID())
	bad.Payload = map[string]any{"titulo": "x"}
	if r := w.pushNota(pcA1, bad); r.Status != "rejected" || r.Code != "campo_desconhecido" {
		t.Errorf("o apagar com payload = %+v", r)
	}
	// Nada na lista do celular.
	for _, vista := range []string{"ativas", "arquivo", "lixeira"} {
		if ns, _ := w.listarNotas(celA, vista); len(ns) != 0 {
			t.Errorf("a vista %s depois de apagar = %v", vista, idsDe(ns))
		}
	}
	// A lápide não vence: um ano depois, ainda gone.
	w.andar(now0.AddDate(1, 0, 0))
	w.reter(w.userA)
	if r := w.pushNota(pcA1, criarNota(n, map[string]any{"created_at": w.clk.Now().Add(-time.Hour).Format(time.RFC3339Nano)})); r.Status != "gone" {
		t.Errorf("um ano depois = %+v, quero gone (a lápide da nota não vence)", r)
	}
}

// Apagar de vez um marcador o tira de todas as notas, sem subir a versão
// delas; a nota que chega depois com ele não o grava. Renomear vence sem selo.
func testNotasMarcadores(t *testing.T, w *world) {
	l, outro, n := newID(), newID(), newID()
	w.mustNota(pcA1, criarMarcador(l, "Mercado"), 1, false)
	w.mustNota(pcA1, criarMarcador(outro, "Feira"), 1, false)
	w.mustNota(pcA1, criarNota(n, map[string]any{"marcadores": []string{l, outro}}), 1, false)
	// Renomear: o nome do PC vence; com a base velha, mesclado.
	w.mustNota(pcA2, renomearMarcador(outro, 1, "Feira livre"), 2, false)
	w.mustNota(pcA1, renomearMarcador(outro, 1, "Sacolão"), 3, true)
	ms, _ := w.listarMarcadores(celA)
	if len(ms) != 2 || ms[0].Nome != "Mercado" || ms[1].Nome != "Sacolão" || ms[1].Version != 3 {
		t.Errorf("os marcadores = %+v, quero Mercado e Sacolão (por nome)", ms)
	}

	w.mustNota(pcA2, apagarMarcador(l), 2, false)
	ns, _ := w.listarNotas(celA, "")
	if len(ns) != 1 || strings.Join(ns[0].Marcadores, ",") != outro || ns[0].Version != 1 {
		t.Fatalf("a nota depois de apagar o marcador = %+v, quero só o outro e a versão 1", ns)
	}
	if c, ok := mudancaDe(w.pullNotas(pcA1, 0, 0), l); !ok || c.EntityType != "label" || c.Operation != "tombstone" {
		t.Errorf("a lápide do marcador no pull = %+v %v", c, ok)
	}
	// O PC 1 manda a nota com o marcador apagado: ele não volta.
	// (Com o apagado tirado, o conjunto é o mesmo de agora: nada a gravar.)
	w.mustNota(pcA1, mudarNota(n, 1, map[string]any{"marcadores": []string{l, outro}}), 1, false)
	if got := w.notaNoPull(pcA1, n); strings.Join(got.Marcadores, ",") != outro {
		t.Errorf("o marcador apagado voltou: %v", got.Marcadores)
	}
	if r := w.pushNota(pcA1, renomearMarcador(l, 1, "de volta")); r.Status != "gone" || r.Motivo != "apagado" {
		t.Errorf("renomear o apagado = %+v", r)
	}
	if ms, _ := w.listarMarcadores(celA); len(ms) != 1 || ms[0].ID != outro {
		t.Errorf("os marcadores depois de apagar = %+v", ms)
	}
}

// O virou_nota do ditado: no criar e no mudar, sem conflito, no Get e no pull;
// o ditado ligado some da listagem e da busca do histórico.
func testNotasVirouNota(t *testing.T, w *world) {
	ligado, solto, nota := newID(), newID(), newID()
	d := dictation("PALAVRA-DA-BUSCA ligado")
	d["virou_nota"] = nota
	if r := w.pushOne(pcA1, createWith(ligado, d)); r.Status != "applied" {
		t.Fatalf("criar ligado: %+v", r)
	}
	w.mustCreate(pcA1, solto, "PALAVRA-DA-BUSCA solto")
	get := func(id string) map[string]json.RawMessage {
		rec := w.do(http.MethodGet, "/api/v1/transcriptions/"+id, celA, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", id, rec.Code)
		}
		return decode[map[string]json.RawMessage](t, rec)
	}
	if v := get(ligado)["virou_nota"]; string(v) != `"`+nota+`"` {
		t.Errorf("o GET do ligado: virou_nota = %s", v)
	}
	if v, ok := get(solto)["virou_nota"]; !ok || string(v) != "null" {
		t.Errorf("o GET do solto: virou_nota = %s %v, quero null (sempre presente)", v, ok)
	}
	listar := func() []string {
		var ids []string
		for _, it := range decode[struct{ Items []itemJSON }](t, w.do(http.MethodGet, "/api/v1/transcriptions", celA, nil)).Items {
			ids = append(ids, it.ID)
		}
		for _, it := range decode[struct{ Items []itemJSON }](t, w.do(http.MethodPost, "/api/v1/transcriptions/search", celA,
			map[string]any{"q": "PALAVRA-DA-BUSCA"})).Items {
			ids = append(ids, "busca:"+it.ID)
		}
		return ids
	}
	if got := strings.Join(listar(), ","); got != solto+",busca:"+solto {
		t.Errorf("a listagem e a busca = %s, quero só o solto", got)
	}
	// Tirar a ligação pelo patch (sem conflito, com a base velha também).
	if r := w.pushOne(pcA1, change(ligado, 1, map[string]any{"virou_nota": nil})); r.Status != "applied" || r.EntityVersion != 2 {
		t.Fatalf("tirar a ligação: %+v", r)
	}
	if got := listar(); len(got) != 4 {
		t.Errorf("depois de tirar a ligação, a listagem e a busca = %v, quero os dois", got)
	}
	if r := w.pushOne(pcA2, change(solto, 1, map[string]any{"virou_nota": nota})); r.Status != "applied" {
		t.Fatalf("ligar o solto: %+v", r)
	}
	if r := w.pushOne(pcA2, change(solto, 1, map[string]any{"virou_nota": "nova:123"})); r.Status != "rejected" || r.Code != "campo_invalido" {
		t.Errorf("virou_nota que não é UUID = %+v", r)
	}
	c, ok := changeOf(w.pull(pcA1, 0, 0), solto)
	if !ok || c.EntityVersion != 2 {
		t.Fatalf("o solto no pull = %+v", c)
	}
	if got := w.get(celA, solto); got.Version != 2 || got.FinalText != "PALAVRA-DA-BUSCA solto" {
		t.Errorf("a ligação mexeu no texto: %+v", got)
	}
	if got := strings.Join(listar(), ","); got != ligado+",busca:"+ligado {
		t.Errorf("a listagem e a busca = %s, quero só o que perdeu a ligação", got)
	}
}

// As listagens do celular: só com a sessão, só da conta, as vistas e a ordem,
// a vista inválida, e o teto com has_more.
func testNotasListagens(t *testing.T, w *world) {
	for _, path := range []string{"/api/v1/notes", "/api/v1/notes?vista=lixeira", "/api/v1/labels"} {
		if rec := w.do(http.MethodGet, path, pcA1, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s com o token do PC: %d, quero 403", path, rec.Code)
		}
		if rec := w.do(http.MethodGet, path, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s sem credencial: %d, quero 401", path, rec.Code)
		}
	}
	for _, v := range []string{"todas", "", "ATIVAS"} {
		if rec := w.do(http.MethodGet, "/api/v1/notes?vista="+v, celA, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("a vista %q: %d, quero 400", v, rec.Code)
		}
	}

	a, b, c, fix, arq, lix1, lix2 := newID(), newID(), newID(), newID(), newID(), newID(), newID()
	w.mustNota(pcA1, criarNota(a, map[string]any{"posicao": "a"}), 1, false)
	w.mustNota(pcA1, criarNota(b, map[string]any{"posicao": "B"}), 1, false)
	w.mustNota(pcA1, criarNota(c, map[string]any{"posicao": "B"}), 1, false)
	w.mustNota(pcA1, criarNota(fix, map[string]any{"posicao": "z", "fixada": true}), 1, false)
	w.mustNota(pcA1, criarNota(arq, map[string]any{"arquivada": true}), 1, false)
	w.mustNota(pcA1, criarNota(lix1, map[string]any{"deleted_at": now0.Add(-2 * time.Hour).Format(time.RFC3339Nano)}), 1, false)
	w.mustNota(pcA1, criarNota(lix2, map[string]any{"deleted_at": now0.Add(-time.Hour).Format(time.RFC3339Nano)}), 1, false)
	w.mustNota(pcB, criarNota(newID(), nil), 1, false)
	w.mustNota(pcA1, criarMarcador(newID(), "b"), 1, false)
	w.mustNota(pcA1, criarMarcador(newID(), "B"), 1, false)
	w.mustNota(pcA1, criarMarcador(newID(), "Á"), 1, false)

	bc := []string{b, c}
	sort.Strings(bc)
	quero := strings.Join(append(append([]string{fix}, bc...), a), ",")
	for _, v := range []string{"", "ativas"} {
		ns, more := w.listarNotas(celA, v)
		if strings.Join(idsDe(ns), ",") != quero || more {
			t.Errorf("as ativas (vista %q) = %v %v, quero %s (fixada, posição em bytes, id)", v, idsDe(ns), more, quero)
		}
	}
	if ns, _ := w.listarNotas(celA, "arquivo"); strings.Join(idsDe(ns), ",") != arq {
		t.Errorf("o arquivo = %v", idsDe(ns))
	}
	if ns, _ := w.listarNotas(celA, "lixeira"); strings.Join(idsDe(ns), ",") != lix2+","+lix1 {
		t.Errorf("a lixeira = %v, quero a mais recente primeiro", idsDe(ns))
	}
	ms, _ := w.listarMarcadores(celA)
	var nomes []string
	for _, m := range ms {
		nomes = append(nomes, m.Nome)
	}
	if strings.Join(nomes, ",") != "B,b,Á" {
		t.Errorf("os marcadores = %v, quero B,b,Á (a ordem dos bytes)", nomes)
	}
	// A conta B só vê a nota dela, e nenhum marcador.
	if ns, _ := w.listarNotas(celB, ""); len(ns) != 1 {
		t.Errorf("a conta B vê %d notas, quero 1", len(ns))
	}
	if ms, _ := w.listarMarcadores(celB); len(ms) != 0 {
		t.Errorf("a conta B vê os marcadores de A: %v", ms)
	}
	// E o PC de B não vê nem mexe na nota de A.
	if _, ok := mudancaDe(w.pullNotas(pcB, 0, 0), a); ok {
		t.Error("o pull de B trouxe a nota de A")
	}
	if r := w.pushNota(pcB, mudarNota(a, 1, map[string]any{"cor": "azul"})); r.Status != "rejected" || r.Code != "base_invalida" {
		t.Errorf("B mudando a nota de A = %+v, quero base_invalida (para B ela não existe)", r)
	}
	if got := w.notaNoPull(pcA1, a); got.Cor != nil {
		t.Errorf("a nota de A mudou pelo push de B: %+v", got)
	}

	// O teto: 501 notas ativas, 500 na lista e has_more.
	var lote []mut
	for i := 0; i < cloudstore.MaxNotesList+1; i++ {
		lote = append(lote, criarNota(newID(), map[string]any{"posicao": fmt.Sprintf("V%03dz", i+1)}))
	}
	w.push(pcB, lote[:cloudstore.MaxPushMutations]...)
	w.push(pcB, lote[cloudstore.MaxPushMutations:]...)
	if ns, more := w.listarNotas(celB, "ativas"); len(ns) != cloudstore.MaxNotesList || !more {
		t.Errorf("com 502 notas: %d e has_more %v, quero %d e true", len(ns), more, cloudstore.MaxNotesList)
	}
}

// O orçamento de texto: notas no teto, misturadas com ditados no teto, cortam
// a página do pull (e a lista do celular) antes do limite, e o pull lê tudo,
// página a página, sem perder nem repetir.
func testNotasOrcamentoDoPull(t *testing.T, w *world) {
	grande := strings.Repeat("x", cloudstore.MaxTextBytes)
	// 48 mudanças: 36 notas e 12 ditados, cada um perto dos 64 KiB. As 36 notas
	// passam dos 2 MiB do orçamento.
	const n = 48
	for i := 0; i < n; i++ {
		if i%4 == 3 {
			d := dictation(grande)
			d["raw_text"] = "curto"
			if r := w.pushOne(pcA1, createWith(newID(), d)); r.Status != "applied" {
				t.Fatalf("o ditado %d: %+v", i, r)
			}
			continue
		}
		w.mustNota(pcA1, criarNota(newID(), map[string]any{"corpo": grande}), 1, false)
	}
	var after int64
	var seqs []int64
	paginas := 0
	for {
		p := w.pullNotas(pcA2, after, 0)
		paginas++
		for _, c := range p.Changes {
			seqs = append(seqs, c.Seq)
		}
		after = p.LastSeq
		if !p.HasMore {
			break
		}
		if paginas > n {
			t.Fatal("o cursor não anda")
		}
	}
	if paginas < 2 || len(seqs) != n {
		t.Fatalf("%d páginas e %d mudanças, quero mais de uma página e as %d", paginas, len(seqs), n)
	}
	for i, s := range seqs {
		if s != int64(i+1) {
			t.Fatalf("o pull leu %v, quero 1..%d", seqs, n)
		}
	}
	if ns, more := w.listarNotas(celA, ""); !more || len(ns) >= 36 || len(ns) == 0 {
		t.Errorf("a lista com 36 notas no teto: %d e has_more %v, quero o corte do orçamento", len(ns), more)
	}
}

// O "apagar meus dados" leva as notas, os marcadores, as lápides, os eventos e
// as ligações: depois dele, o mesmo id cria de novo, e a base de antes é
// base_invalida.
func testNotasApagarMeusDados(t *testing.T, w *world) {
	n, l, morta := newID(), newID(), newID()
	w.mustNota(pcA1, criarMarcador(l, "Mercado"), 1, false)
	w.mustNota(pcA1, criarNota(n, map[string]any{"marcadores": []string{l}}), 1, false)
	w.mustNota(pcA1, apagarNota(morta), 1, false)
	d := dictation("ligado")
	d["virou_nota"] = n
	dict := newID()
	if r := w.pushOne(pcA1, createWith(dict, d)); r.Status != "applied" {
		t.Fatal(r)
	}
	w.mustNota(pcB, criarNota(n, nil), 1, false)
	if rec := w.do(http.MethodDelete, "/api/v1/me/data", celA, map[string]any{"confirmar": "apagar"}); rec.Code != http.StatusNoContent {
		t.Fatalf("apagar meus dados: %d %s", rec.Code, rec.Body.String())
	}
	const pcNovo = "pc-a-novo"
	w.newDevice(w.userA, pcNovo)
	if p := w.pullNotas(pcNovo, 0, 0); len(p.Changes) != 0 {
		t.Fatalf("o pull depois de apagar = %+v", p)
	}
	if ns, _ := w.listarNotas(celA, ""); len(ns) != 0 {
		t.Errorf("as notas depois de apagar = %v", idsDe(ns))
	}
	if ms, _ := w.listarMarcadores(celA); len(ms) != 0 {
		t.Errorf("os marcadores depois de apagar = %v", ms)
	}
	if r := w.pushNota(pcNovo, mudarNota(n, 1, map[string]any{"cor": "azul"})); r.Status != "rejected" || r.Code != "base_invalida" {
		t.Errorf("mudar com a base de antes = %+v", r)
	}
	// A lápide também saiu: o id que tinha lápide cria de novo.
	w.mustNota(pcNovo, criarNota(morta, nil), 1, false)
	// A nota de B ficou.
	if got := w.notaNoPull(pcB, n); got.Version != 1 {
		t.Errorf("a nota de B = %+v", got)
	}
}

// A arrumação do job apaga o evento órfão vencido das notas e dos marcadores,
// e fica o que é o last_seq de uma nota ou de um marcador vivo; a lápide fica
// para sempre. Só com o NoteEvents do Backend.
func testNotasArrumacao(t *testing.T, w *world) {
	if w.b.NoteEvents == nil {
		t.Skip("o Backend não lê os eventos das notas")
	}
	n, l := newID(), newID()
	w.mustNota(pcA1, criarNota(n, nil), 1, false)                               // seq 1: órfão depois da mudança
	w.mustNota(pcA2, mudarNota(n, 1, map[string]any{"fixada": true}), 2, false) // seq 2: o last_seq de n
	w.mustNota(pcA1, criarMarcador(l, "Mercado"), 1, false)                     // seq 3: órfão depois do apagar
	w.mustNota(pcA1, apagarMarcador(l), 2, false)                               // seq 4: o evento da lápide
	seqs := func() string {
		var out []string
		for _, e := range w.b.NoteEvents(t, w.userA) {
			out = append(out, fmt.Sprint(e.Seq))
		}
		return strings.Join(out, ",")
	}
	if got := seqs(); got != "1,2,3,4" {
		t.Fatalf("os eventos das notas = %s", got)
	}
	w.clk.Set(now0.Add(cloudstore.PrazoDosEventos - time.Microsecond))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(now0.Add(cloudstore.PrazoDosEventos))
	w.wantRetido(w.userA, cloudstore.Retido{Eventos: 3})
	if got := seqs(); got != "2" {
		t.Fatalf("os eventos das notas depois da arrumação = %s, quero 2", got)
	}
	p := w.pullNotas(pcA1, 0, 0)
	if c, ok := mudancaDe(p, n); !ok || c.DeviceID == nil || *c.DeviceID != w.devA2 {
		t.Errorf("a origem da nota = %+v, quero o PC 2", c)
	}
	if c, ok := mudancaDe(p, l); !ok || c.Operation != "tombstone" || c.Seq != 4 {
		t.Errorf("a lápide do marcador = %+v %v (a lápide nunca vence)", c, ok)
	}
}

// Nenhum caminho das notas escreve o título, o corpo ou o nome em log.
func testNotasNadaDeTextoEmLog(t *testing.T, w *world) {
	var buf bytes.Buffer
	var mu sync.Mutex
	sink := lockedWriter{w: &buf, mu: &mu}
	prevOut, prevFlags := log.Writer(), log.Flags()
	prevSlog := slog.Default()
	log.SetOutput(sink)
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		slog.SetDefault(prevSlog)
	})
	const (
		titulo = "TITULO-SECRETO-5531"
		corpo  = "CORPO-SECRETO-7720"
		nome   = "NOME-SECRETO-1904"
	)
	n, l := newID(), newID()
	w.mustNota(pcA1, criarMarcador(l, nome), 1, false)
	w.mustNota(pcA1, criarNota(n, map[string]any{"titulo": titulo, "corpo": "- [ ] " + corpo, "marcadores": []string{l}}), 1, false)
	w.pushNota(pcA2, mudarNota(n, 1, map[string]any{"titulo": titulo, "corpo": "- [x] " + corpo, "corpo_base": "- [ ] " + corpo}))
	w.pushNota(pcA1, mudarNota(n, 1, map[string]any{"titulo": titulo, "corpo": corpo + " outro"})) // conflict
	w.pushNota(pcA1, mudarNota(n, 9, map[string]any{"titulo": titulo, "corpo": corpo}))            // base_invalida
	w.pushNota(pcA1, mudarNota(n, 2, map[string]any{"titulo": titulo, corpo: nome}))               // campo_desconhecido
	w.pushNota(pcA1, renomearMarcador(l, 1, nome+"\n"))                                            // campo_invalido
	w.do(http.MethodPost, "/api/v1/sync/push", pcA1, `{"mutations":[{"entity_type":"note","payload":{"titulo":"`+titulo)
	w.pullNotas(pcA1, 0, 0)
	w.listarNotas(celA, "")
	w.listarMarcadores(celA)
	_, err := w.b.Store.Push(context.Background(), cloudstore.Actor{UserID: w.userA, DeviceID: w.devB},
		[]cloudstore.Mutation{{MutationID: newID(), EntityType: "note", EntityID: n, Operation: "upsert",
			Payload: []byte(`{"titulo":"` + titulo + `"}`)}}, now0)
	if err == nil {
		t.Error("push com o aparelho de outro usuário não falhou")
	}
	secrets := []string{titulo, corpo, nome, "SECRETO"}
	mu.Lock()
	defer mu.Unlock()
	server := w.logs.String()
	for _, s := range secrets {
		if err != nil && strings.Contains(err.Error(), s) {
			t.Errorf("o erro do Store carrega o texto: %v", err)
		}
		if strings.Contains(buf.String(), s) || strings.Contains(server, s) {
			t.Fatalf("o texto das notas (%s) no log:\n%s\n%s", s, buf.String(), server)
		}
	}
	for _, want := range []string{`rota="GET /api/v1/notes"`, `rota="GET /api/v1/labels"`, "itens="} {
		if !strings.Contains(server, want) {
			t.Errorf("o log do servidor não tem %s", want)
		}
	}
}
