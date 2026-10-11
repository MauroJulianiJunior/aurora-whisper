package contracttest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Os casos do aprendido no celular (ADR-019, "Decisões da F8", F8d): GET
// /learned e POST /learned/undo, só com a sessão, pelo ponto único de gravação
// do Store. Pela API, contra o Store em memória e o Postgres.

// phoneItemJSON é o item do celular (06, "Learned").
type phoneItemJSON struct {
	ID       string `json:"id"`
	Termo    string `json:"termo"`
	Variante string `json:"variante"`
	Restrito bool   `json:"restrito"`
	Dominio  string `json:"dominio"`
}

type phoneListJSON struct {
	Items   []phoneItemJSON `json:"items"`
	HasMore bool            `json:"has_more"`
}

// phoneItemKeys são as chaves do item, e só elas: nada da linha (o
// normalized_variant, o user_id, as versões, as datas, o undone_at) vai ao
// celular.
var phoneItemKeys = []string{"dominio", "id", "restrito", "termo", "variante"}

func sortedKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("não é um objeto: %v (%s)", err, raw)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// phoneList lista o aprendido pelo celular e confere o formato do envelope
// (items e has_more, e mais nada; items nunca nulo) e do item (as cinco
// chaves). Devolve a lista e o corpo cru.
func (w *world) phoneList(token string) (phoneListJSON, string) {
	w.t.Helper()
	rec := w.do(http.MethodGet, "/api/v1/learned", token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET /learned: status %d, quero 200: %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if got := sortedKeys(w.t, rec.Body.Bytes()); fmt.Sprint(got) != "[has_more items]" {
		w.t.Fatalf("as chaves do envelope = %v, quero [has_more items]: %s", got, raw)
	}
	var env struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Items == nil {
		w.t.Fatalf("items não é uma lista (nula?): %v (%s)", err, raw)
	}
	for _, it := range env.Items {
		if got := sortedKeys(w.t, it); fmt.Sprint(got) != fmt.Sprint(phoneItemKeys) {
			w.t.Fatalf("as chaves do item = %v, quero %v: %s", got, phoneItemKeys, it)
		}
	}
	return decode[phoneListJSON](w.t, rec), raw
}

func (w *world) phoneUndo(token, id string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.do(http.MethodPost, "/api/v1/learned/undo", token, map[string]any{"id": id})
}

// mustPhoneUndo desfaz pelo celular, confere o 200 e as chaves do item, e o
// devolve.
func (w *world) mustPhoneUndo(token, id string) phoneItemJSON {
	w.t.Helper()
	rec := w.phoneUndo(token, id)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("POST /learned/undo %s: status %d, quero 200: %s", id, rec.Code, rec.Body.String())
	}
	if got := sortedKeys(w.t, rec.Body.Bytes()); fmt.Sprint(got) != fmt.Sprint(phoneItemKeys) {
		w.t.Fatalf("as chaves do item desfeito = %v, quero %v: %s", got, phoneItemKeys, rec.Body.String())
	}
	return decode[phoneItemJSON](w.t, rec)
}

// errorOf lê o envelope de erro.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	e := decode[struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}](t, rec)
	return e.Error.Code, e.Error.Message
}

// mustNotFound confere o 404 do aprendido: o código NOT_FOUND e a frase
// própria (e não a do ditado).
func mustNotFound(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	code, msg := errorOf(t, rec)
	if rec.Code != http.StatusNotFound || code != "NOT_FOUND" || msg != "troca aprendida não encontrada" {
		t.Errorf("%s: %d %s %q; quero 404 NOT_FOUND \"troca aprendida não encontrada\"", name, rec.Code, code, msg)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "transcri") || strings.Contains(strings.ToLower(rec.Body.String()), "ditado") {
		t.Errorf("%s: a resposta fala do ditado: %s", name, rec.Body.String())
	}
}

// item é o que o celular deve ver de um par.
func item(variant, term string, restricted bool, domain string) phoneItemJSON {
	return phoneItemJSON{ID: dictionary.IDDoPar(variant), Termo: term, Variante: variant, Restrito: restricted, Dominio: domain}
}

// A lista do celular e o desfazer: só os não desfeitos, do mais recente para
// o mais antigo, só os da conta; o item do 06 (e nada da linha); o desfazer
// devolve o item e o tira da lista; reaprender traz de volta, no topo.
func testPhoneListAndUndo(t *testing.T, w *world) {
	// Vazia: items é a lista vazia, e não nula.
	if l, raw := w.phoneList(celA); len(l.Items) != 0 || l.HasMore || !strings.Contains(raw, `"items":[]`) {
		t.Fatalf("a lista vazia = %+v (%s), quero items [] e has_more false", l, raw)
	}

	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)                                                         // seq 1
	w.mustLearn(pcA1, aprenderCom("nfe", 0, parNoDominio("nfe", "NF-e", "fiscal", true)), 1)               // seq 2
	w.mustLearn(pcA2, aprenderCom("DCTF Web", 0, parNoDominio("DCTF Web", "DCTFWeb", "fiscal", false)), 1) // seq 3
	w.mustLearn(pcB, aprender("sf", "SPED", 0), 1)

	l, raw := w.phoneList(celA)
	want := []phoneItemJSON{
		item("DCTF Web", "DCTFWeb", false, "fiscal"),
		item("nfe", "NF-e", true, "fiscal"),
		item("sf", "ECF", false, ""),
	}
	if !reflect.DeepEqual(l.Items, want) || l.HasMore {
		t.Fatalf("a lista = %+v, has_more %v; quero %+v (o mais recente primeiro, só os da conta)", l.Items, l.HasMore, want)
	}
	for _, banned := range []string{`"normalized_variant"`, `"user_id"`, `"undone_at"`, `"last_seq"`, `"version"`, `"term"`,
		`"variant"`, `"domain"`, `"restricted"`, `"created_at"`, `"updated_at"`, `"priority"`, `"dominios"`} {
		if strings.Contains(raw, banned) {
			t.Errorf("a lista expõe %s: %s", banned, raw)
		}
	}
	for _, needed := range []string{`"dominio":""`, `"restrito":false`, `"restrito":true`, `"has_more":false`} {
		if !strings.Contains(raw, needed) {
			t.Errorf("a lista não traz %s explícito: %s", needed, raw)
		}
	}
	if b, _ := w.phoneList(celB); len(b.Items) != 1 || b.Items[0] != item("sf", "SPED", false, "") {
		t.Fatalf("a lista de B = %+v, quero só o par dele", b.Items)
	}

	// Desfazer devolve o item (o que a lista mostrava) e o tira da lista.
	if got := w.mustPhoneUndo(celA, dictionary.IDDoPar("nfe")); got != want[1] {
		t.Fatalf("o desfeito = %+v, quero o item %+v", got, want[1])
	}
	if l, _ := w.phoneList(celA); !reflect.DeepEqual(l.Items, []phoneItemJSON{want[0], want[2]}) {
		t.Fatalf("a lista depois de desfazer = %+v, quero sem o nfe", l.Items)
	}

	// Reaprender (no PC, sabendo do desfazer: base 2) traz o par de volta, no
	// topo da lista (o seq novo).
	w.mustLearn(pcA1, aprenderCom("nfe", 2, parNoDominio("nfe", "NF-e", "fiscal", true)), 3)
	if l, _ := w.phoneList(celA); !reflect.DeepEqual(l.Items, []phoneItemJSON{want[1], want[0], want[2]}) {
		t.Fatalf("a lista depois de reaprender = %+v, quero o nfe no topo", l.Items)
	}
}

// As duas rotas são só da sessão: o token de um PC recebe 403 (e o desfazer
// dele não faz nada: o PC desfaz pelo sync), e sem credencial é 401.
func testPhoneOnlySession(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)
	id := dictionary.IDDoPar("sf")
	before := len(w.b.Events(t, w.userA))
	for _, tok := range []string{pcA1, pcA2, pcB} {
		for name, rec := range map[string]*httptest.ResponseRecorder{
			"GET /learned":       w.do(http.MethodGet, "/api/v1/learned", tok, nil),
			"POST /learned/undo": w.phoneUndo(tok, id),
		} {
			code, _ := errorOf(t, rec)
			if rec.Code != http.StatusForbidden || code != "FORBIDDEN" {
				t.Errorf("%s com o token %s: %d %s, quero 403 FORBIDDEN", name, tok, rec.Code, code)
			}
			if strings.Contains(rec.Body.String(), "ECF") || strings.Contains(rec.Body.String(), id) {
				t.Errorf("%s com o token %s: a resposta de erro traz o aprendido: %s", name, tok, rec.Body.String())
			}
		}
	}
	for _, tok := range []string{"", "token-que-nao-existe"} {
		if rec := w.do(http.MethodGet, "/api/v1/learned", tok, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET /learned sem credencial (%q): %d, quero 401", tok, rec.Code)
		}
		if rec := w.phoneUndo(tok, id); rec.Code != http.StatusUnauthorized {
			t.Errorf("POST /learned/undo sem credencial (%q): %d, quero 401", tok, rec.Code)
		}
	}
	// Nada foi desfeito: o par segue na lista, na v1, sem evento novo.
	if l, _ := w.phoneList(celA); len(l.Items) != 1 || l.Items[0] != item("sf", "ECF", false, "") {
		t.Fatalf("a lista depois dos pedidos do PC = %+v, quero o par intacto", l.Items)
	}
	if got := len(w.b.Events(t, w.userA)); got != before {
		t.Fatalf("%d eventos, eram %d: o PC desfez pela rota do celular", got, before)
	}
}

// O desfazer de outra conta, o que não existe e o id que não é UUID dão o
// mesmo 404, com a frase do aprendido, sem gravar nem gastar seq. O mesmo id
// em duas contas (o id é o hash da variante) desfaz só o da conta de quem
// pede. Sem o id, ou com o corpo errado, é 400.
func testPhoneOtherAccount404(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)   // A, seq 1
	w.mustLearn(pcA1, aprender("nfe", "NF-e", 0), 1) // A, seq 2
	w.mustLearn(pcB, aprender("sf", "SPED", 0), 1)   // B, seq 1: o MESMO id de A
	nfe, sf := dictionary.IDDoPar("nfe"), dictionary.IDDoPar("sf")
	if nfe == sf {
		t.Fatal("os ids deveriam ser o hash da variante")
	}
	eventsA, eventsB := len(w.b.Events(t, w.userA)), len(w.b.Events(t, w.userB))

	// B não tem o nfe: o par de A é o mesmo que não existir.
	mustNotFound(t, "o par de outra conta", w.phoneUndo(celB, nfe))
	for name, id := range map[string]string{
		"o que não existe":               newID(),
		"o texto da variante":            "sf",
		"o termo":                        "ECF",
		"vazio":                          "",
		"não é UUID":                     "nao-e-um-uuid",
		"UUID com lixo no fim":           sf + "x",
		"UUID sem os hífens":             strings.ReplaceAll(sf, "-", ""),
		"UUID com o caractere nulo":      sf[:35] + "\x00",
		"UUID de outra variante":         dictionary.IDDoPar("ecd"),
		"UUID com espaços em volta":      " " + sf + " ",
		"UUID entre chaves":              "{" + sf + "}",
		"UUID com 'urn:uuid:' na frente": "urn:uuid:" + sf,
	} {
		mustNotFound(t, name, w.phoneUndo(celA, id))
	}
	if a, b := len(w.b.Events(t, w.userA)), len(w.b.Events(t, w.userB)); a != eventsA || b != eventsB {
		t.Fatalf("eventos A %d e B %d, eram %d e %d: os 404 gravaram", a, b, eventsA, eventsB)
	}
	if l, _ := w.phoneList(celA); len(l.Items) != 2 {
		t.Fatalf("a lista de A depois dos 404 = %+v, quero os dois pares", l.Items)
	}

	// O mesmo id nas duas contas: B desfaz o dele, e o de A fica.
	if got := w.mustPhoneUndo(celB, sf); got != item("sf", "SPED", false, "") {
		t.Fatalf("o desfeito de B = %+v, quero o par dele (SPED), e não o de A", got)
	}
	if l, _ := w.phoneList(celB); len(l.Items) != 0 {
		t.Fatalf("a lista de B = %+v, quero vazia", l.Items)
	}
	if l, _ := w.phoneList(celA); len(l.Items) != 2 || l.Items[1] != item("sf", "ECF", false, "") {
		t.Fatalf("a lista de A depois de B desfazer o mesmo id = %+v, quero os dois pares intactos", l.Items)
	}
	if _, l, ok := learnedOf(t, w.pullTyped(pcA2, 0, 0), "sf"); !ok || l.Term != "ECF" || l.UndoneAt != nil {
		t.Fatalf("o par de A no pull = %+v, quero ECF sem undone_at (B desfez só o dele)", l)
	}
	if got := len(w.b.Events(t, w.userA)); got != eventsA {
		t.Fatalf("A tem %d eventos, eram %d: o desfazer de B gravou na conta de A", got, eventsA)
	}
	// E o id em maiúsculas é o mesmo id.
	if got := w.mustPhoneUndo(celA, strings.ToUpper(nfe)); got != item("nfe", "NF-e", false, "") {
		t.Fatalf("o desfeito com o id em maiúsculas = %+v", got)
	}

	// O corpo errado é 400, e não 404.
	big := `{"id":"` + strings.Repeat("a", 2048) + `"}`
	for name, body := range map[string]any{
		"sem corpo":           nil,
		"objeto vazio":        map[string]any{},
		"id nulo":             map[string]any{"id": nil},
		"id que é número":     map[string]any{"id": 5},
		"id que é objeto":     map[string]any{"id": map[string]any{"a": 1}},
		"JSON quebrado":       `{"id":"` + sf,
		"não é um objeto":     `[]`,
		"corpo grande demais": big,
	} {
		rec := w.do(http.MethodPost, "/api/v1/learned/undo", celA, body)
		if code, _ := errorOf(t, rec); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, quero 400", name, rec.Code, code)
		}
	}
	if a := len(w.b.Events(t, w.userA)); a != eventsA+1 {
		t.Fatalf("eventos de A = %d, quero %d (só o desfazer do nfe)", a, eventsA+1)
	}
}

// O desfazer repetido não grava nem gasta seq, venha de onde vier: o
// celular duas vezes, o celular depois do PC, o PC depois do celular. Os seq
// seguem 1..n sem buraco, e o próximo a gravar usa o número seguinte.
func testPhoneUndoRepeatedDoesNotSpendSeq(t *testing.T, w *world) {
	sf, nfe := dictionary.IDDoPar("sf"), dictionary.IDDoPar("nfe")
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1) // seq 1
	first := w.mustPhoneUndo(celA, sf)             // seq 2
	requireContiguous(t, w.seqsOf(w.userA), 2)
	for range 3 {
		if again := w.mustPhoneUndo(celA, sf); again != first {
			t.Fatalf("desfazer de novo = %+v, quero o mesmo item %+v", again, first)
		}
	}
	requireContiguous(t, w.seqsOf(w.userA), 2)

	// O PC desfaz o que o celular já desfez: applied na versão atual (2), sem
	// gravar.
	if r, _ := w.pushLearned(pcA2, desfazer("sf", 1)); r.Status != "applied" || r.EntityVersion != 2 {
		t.Fatalf("o delete do PC sobre o desfeito do celular = %+v, quero applied v2", r)
	}
	requireContiguous(t, w.seqsOf(w.userA), 2)

	// O contrário: o PC desfaz primeiro (seq 4), e o celular encontra o par
	// já desfeito.
	w.mustLearn(pcA1, aprender("nfe", "NF-e", 0), 1) // seq 3
	w.mustLearn(pcA1, desfazer("nfe", 1), 2)         // seq 4
	if got := w.mustPhoneUndo(celA, nfe); got != item("nfe", "NF-e", false, "") {
		t.Fatalf("o desfazer do celular sobre o desfeito do PC = %+v, quero o item", got)
	}
	requireContiguous(t, w.seqsOf(w.userA), 4)

	// Nenhuma versão subiu: as duas seguem na v2.
	p := w.pullTyped(pcA2, 0, 0)
	for _, v := range []string{"sf", "nfe"} {
		if c, l, ok := learnedOf(t, p, v); !ok || c.EntityVersion != 2 || l.UndoneAt == nil {
			t.Errorf("%s no pull = %+v %+v, quero desfeito na v2", v, c, l)
		}
	}
	// O próximo a gravar usa o seq 5: nenhum número foi perdido.
	w.mustLearn(pcA1, aprender("ecd", "ECD", 0), 1)
	requireContiguous(t, w.seqsOf(w.userA), 5)
	if c, _, ok := learnedOf(t, w.pullTyped(pcA2, 4, 0), "ecd"); !ok || c.Seq != 5 {
		t.Fatalf("o aprendido seguinte = %+v, quero o seq 5", c)
	}
}

// O desfazer do celular chega ao PC no pull como upsert do learned, com
// undone_at e a versão nova, sem aparelho de origem (o celular). Vale o
// desfazer (D4): o PC que ainda não sabe e manda outro termo recebe o
// conflict com o par desfeito; o PC que reaprende sabendo (base 2) traz o par
// de volta.
func testPhoneUndoReachesPull(t *testing.T, w *world) {
	id := dictionary.IDDoPar("sf")
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1) // seq 1
	if _, l, ok := learnedOf(t, w.pullTyped(pcA2, 0, 0), "sf"); !ok || l.UndoneAt != nil {
		t.Fatalf("o par no pull antes do desfazer = %+v", l)
	}

	undoAt := now0.Add(time.Hour)
	w.clk.Set(undoAt)
	w.mustPhoneUndo(celA, id) // seq 2

	for _, tok := range []string{pcA1, pcA2} {
		p := w.pullTyped(tok, 1, 0)
		if len(p.Changes) != 1 || p.LastSeq != 2 || p.HasMore {
			t.Fatalf("o pull de %s depois do desfazer = %+v, quero uma mudança, até o seq 2", tok, p)
		}
		c, l, _ := learnedOf(t, p, "sf")
		if c.Seq != 2 || c.EntityVersion != 2 || c.Operation != "upsert" || c.EntityType != "learned" || c.DeviceID != nil {
			t.Errorf("a mudança de %s = %+v, quero learned upsert v2 no seq 2, sem aparelho (o celular)", tok, c)
		}
		if l.UndoneAt == nil || !l.UndoneAt.Equal(undoAt) || l.Term != "ECF" || l.Variant != "sf" || l.ID != id {
			t.Errorf("o par desfeito no pull de %s = %+v, quero undone_at %v e o texto mantido", tok, l, undoAt)
		}
	}
	if p := w.pullTyped(pcB, 0, 0); len(p.Changes) != 0 {
		t.Fatalf("o pull de B = %+v, quero vazio", p)
	}
	ev := w.b.Events(t, w.userA)
	if len(ev) != 2 || ev[1].Seq != 2 || ev[1].EntityType != "learned" || ev[1].Operation != "upsert" ||
		ev[1].EntityVersion != 2 || ev[1].EntityID != id || ev[1].DeviceID != nil {
		t.Fatalf("eventos = %+v, quero o segundo learned upsert v2 do celular (sem aparelho)", ev)
	}

	// Vale o desfazer: o PC que não sabia dele e manda outro termo com a base
	// velha recebe o conflict com o par desfeito.
	r, _ := w.pushLearned(pcA2, aprender("sf", "OUTRO", 1))
	if r.Status != "conflict" || r.EntityVersion != 2 || r.State == nil || r.State.UndoneAt == nil || r.State.Term != "ECF" {
		t.Fatalf("o aprender com a base velha sobre o desfeito = %+v, quero conflict v2 com o par desfeito", r)
	}
	if l, _ := w.phoneList(celA); len(l.Items) != 0 {
		t.Fatalf("a lista depois do conflict = %+v, quero vazia (o desfazer venceu)", l.Items)
	}
	// Quem sabe do desfazer (base 2) reaprende, e o par volta, na v3.
	w.mustLearn(pcA2, aprender("sf", "OUTRO", 2), 3)
	if l, _ := w.phoneList(celA); len(l.Items) != 1 || l.Items[0] != item("sf", "OUTRO", false, "") {
		t.Fatalf("a lista depois de reaprender = %+v", l.Items)
	}
	if _, l, _ := learnedOf(t, w.pullTyped(pcA1, 2, 0), "sf"); l.UndoneAt != nil || l.Term != "OUTRO" {
		t.Fatalf("o par reaprendido no pull = %+v, quero sem undone_at", l)
	}
}

// ends diz a lista pelas pontas ("var0499..var0000", ou "vazia"), para a
// mensagem de um teste que não pode quebrar com a lista vazia.
func ends(l phoneListJSON) string {
	if len(l.Items) == 0 {
		return "vazia"
	}
	return l.Items[0].Variante + ".." + l.Items[len(l.Items)-1].Variante
}

// O teto fixo da lista (500): vêm os mais recentes e has_more quando passa
// dele; com exatamente 500, não há has_more; os desfeitos não contam.
func testPhoneCeilingAndHasMore(t *testing.T, w *world) {
	const n = cloudstore.MaxLearnedList
	if n != 500 {
		t.Fatalf("o teto é %d, a proposta da ADR-019 (escolha menor 3) é 500", n)
	}
	var muts []mut
	for i := range n {
		muts = append(muts, aprender(fmt.Sprintf("var%04d", i), "T", 0))
	}
	for from := 0; from < n; from += n / 2 {
		for i, r := range w.push(pcA1, muts[from:from+n/2]...).Results {
			if r.Status != "applied" {
				t.Fatalf("aprender var%04d: %+v", from+i, r)
			}
		}
	}

	l, raw := w.phoneList(celA)
	if len(l.Items) != n || l.HasMore || !strings.Contains(raw, `"has_more":false`) || ends(l) != "var0499..var0000" {
		t.Fatalf("com %d pares: %d itens, has_more %v, %s; quero todos, sem has_more, var0499..var0000", n, len(l.Items), l.HasMore, ends(l))
	}

	// O 501º: vêm os 500 mais recentes, e o mais antigo fica de fora.
	w.mustLearn(pcA1, aprender("var-extra", "T", 0), 1)
	l, raw = w.phoneList(celA)
	if len(l.Items) != n || !l.HasMore || !strings.Contains(raw, `"has_more":true`) || ends(l) != "var-extra..var0001" {
		t.Fatalf("com %d pares: %d itens, has_more %v, %s; quero %d, has_more, var-extra..var0001", n+1, len(l.Items), l.HasMore, ends(l), n)
	}

	// Desfazer um tira o desfeito da conta: sobram 500, sem has_more, e o
	// mais antigo volta à lista.
	w.mustPhoneUndo(celA, dictionary.IDDoPar("var-extra"))
	l, _ = w.phoneList(celA)
	if len(l.Items) != n || l.HasMore || ends(l) != "var0499..var0000" {
		t.Fatalf("depois de desfazer um: %d itens, has_more %v, %s; quero os 500 vivos, sem has_more, var0499..var0000", len(l.Items), l.HasMore, ends(l))
	}

	// Outra conta não conta para o teto.
	if b, _ := w.phoneList(celB); len(b.Items) != 0 || b.HasMore {
		t.Fatalf("a lista de B = %+v", b)
	}
}

// O CSRF e o Host valem nas duas rotas novas: o desfazer de outra origem é
// 403 e não faz nada; o Host estranho é 400 antes da rota; e não há CORS.
func testPhoneCSRFAndHost(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)
	id := dictionary.IDDoPar("sf")
	before := len(w.b.Events(t, w.userA))
	untouched := func(when string) {
		t.Helper()
		if l, _ := w.phoneList(celA); len(l.Items) != 1 || l.Items[0] != item("sf", "ECF", false, "") {
			t.Fatalf("%s: a lista = %+v, quero o par intacto", when, l.Items)
		}
		if got := len(w.b.Events(t, w.userA)); got != before {
			t.Fatalf("%s: %d eventos, eram %d", when, got, before)
		}
	}

	for name, mudar := range map[string]func(*http.Request){
		"Sec-Fetch-Site cross-site": func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"Sec-Fetch-Site same-site":  func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") },
		"Origin de outro site": func(r *http.Request) {
			r.Header.Del("Sec-Fetch-Site")
			r.Header.Set("Origin", "https://evil.example")
		},
	} {
		rec := w.doWith(http.MethodPost, "/api/v1/learned/undo", celA, map[string]any{"id": id}, mudar)
		if code, _ := errorOf(t, rec); rec.Code != http.StatusForbidden || code != "FORBIDDEN" {
			t.Errorf("o desfazer com %s: %d %s, quero 403 FORBIDDEN", name, rec.Code, code)
		}
		for k := range rec.Header() {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Errorf("o desfazer com %s: a resposta traz %s", name, k)
			}
		}
	}
	untouched("depois dos pedidos de outra origem")

	for _, host := range []string{"evil.example", "127.0.0.1:8080", "nuvem.exemplo.test:9999", "localhost:1", ""} {
		for name, rec := range map[string]*httptest.ResponseRecorder{
			"GET /learned": w.doWith(http.MethodGet, "/api/v1/learned", celA, nil, func(r *http.Request) { r.Host = host }),
			"POST /learned/undo": w.doWith(http.MethodPost, "/api/v1/learned/undo", celA, map[string]any{"id": id},
				func(r *http.Request) { r.Host = host }),
		} {
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"host inválido"`) {
				t.Errorf("%s com o Host %q: %d %s, quero o 400 \"host inválido\"", name, host, rec.Code, rec.Body.String())
			}
		}
	}
	untouched("depois dos pedidos com o Host estranho")

	// O preflight cai no 404, sem CORS.
	pre := w.doWith(http.MethodOptions, "/api/v1/learned/undo", "", nil, func(r *http.Request) {
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Access-Control-Request-Method", "POST")
	})
	if pre.Code != http.StatusNotFound {
		t.Errorf("preflight: %d, quero 404", pre.Code)
	}
	for k := range pre.Header() {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("preflight com %s", k)
		}
	}

	// O mesmo pedido, de dentro (same-origin, o Host certo), vale.
	if got := w.mustPhoneUndo(celA, id); got != item("sf", "ECF", false, "") {
		t.Fatalf("o desfazer de verdade = %+v", got)
	}
}

// O celular não aprende: não há rota de criar nem de mudar o aprendido, nem
// outro método nas duas rotas (404, nunca 405), e nada é gravado.
func testPhoneCannotWrite(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)
	id := dictionary.IDDoPar("sf")
	before := len(w.b.Events(t, w.userA))
	body := map[string]any{"variante": "sf", "termo": "SPED", "id": id}
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/learned"}, {http.MethodPut, "/api/v1/learned"}, {http.MethodPatch, "/api/v1/learned"},
		{http.MethodDelete, "/api/v1/learned"},
		{http.MethodGet, "/api/v1/learned/undo"}, {http.MethodPut, "/api/v1/learned/undo"}, {http.MethodDelete, "/api/v1/learned/undo"},
		{http.MethodPatch, "/api/v1/learned/" + id}, {http.MethodPut, "/api/v1/learned/" + id},
		{http.MethodDelete, "/api/v1/learned/" + id}, {http.MethodPost, "/api/v1/learned/" + id + "/undo"},
		{http.MethodPost, "/api/v1/learned/undo/" + id},
	} {
		for _, tok := range []string{celA, pcA1} {
			if rec := w.do(c.method, c.path, tok, body); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s (%s): %d, quero 404", c.method, c.path, tok, rec.Code)
			}
		}
	}
	if got := len(w.b.Events(t, w.userA)); got != before {
		t.Fatalf("%d eventos, eram %d: uma rota que não deveria existir gravou", got, before)
	}
	if l, _ := w.phoneList(celA); len(l.Items) != 1 || l.Items[0] != item("sf", "ECF", false, "") {
		t.Fatalf("a lista = %+v, quero o par intacto", l.Items)
	}
}

// Nenhum caminho das duas rotas escreve a variante, o termo ou o domínio em
// log (nem os felizes, nem os 404, os 400, os 403, o Host estranho), e o
// texto que vai na URL por engano não pára no log: o log de acesso só tem o
// padrão da rota e contagens. O texto também não volta nas respostas de erro.
func testPhoneNoTextInLogs(t *testing.T, w *world) {
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
		variant = "VARIANTE-SECRETA-4417"
		term    = "TERMO-SECRETO-8812"
		domain  = "DOMINIO-SECRETO-3391"
	)
	secrets := []string{variant, term, domain, "VARIANTE-SECRETA", "TERMO-SECRETO", "DOMINIO-SECRETO"}
	id := dictionary.IDDoPar(variant)
	w.mustLearn(pcA1, aprenderCom(variant, 0, parNoDominio(variant, term, domain, true)), 1)

	// Um erro de verdade do Store, com o par ainda ativo (o aparelho não é do
	// usuário: a memória o recusa, e o Postgres, pela chave estrangeira).
	_, err := w.b.Store.UndoLearned(context.Background(), cloudstore.Actor{UserID: w.userA, DeviceID: w.devB}, id, now0)
	if err == nil {
		t.Error("o desfazer com o aparelho de outro usuário não falhou")
	} else {
		for _, s := range append(secrets, id) {
			if strings.Contains(err.Error(), s) {
				t.Errorf("o erro do Store carrega o texto do aprendido: %v", err)
			}
		}
	}

	w.do(http.MethodGet, "/api/v1/learned", celA, nil)
	// O texto na URL por engano (a rota não lê a query, e o log não a tem).
	w.do(http.MethodGet, "/api/v1/learned?q="+variant, celA, nil)
	w.do(http.MethodGet, "/api/v1/learned?variante="+variant+"&termo="+term+"&dominio="+domain, celA, nil)
	w.do(http.MethodGet, "/api/v1/learned/"+variant, celA, nil)
	w.do(http.MethodPost, "/api/v1/learned/undo?id="+variant, celA, map[string]any{"id": id})
	w.phoneUndo(celA, id) // de novo: 200 sem gravar
	// As respostas de erro não repetem o que o cliente mandou.
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"o texto no lugar do id": w.phoneUndo(celA, variant),
		"o termo no lugar do id": w.phoneUndo(celA, term),
		"outra conta":            w.phoneUndo(celB, id),
		"JSON quebrado":          w.do(http.MethodPost, "/api/v1/learned/undo", celA, `{"id":"`+variant),
		"sem id":                 w.do(http.MethodPost, "/api/v1/learned/undo", celA, map[string]any{"variante": variant}),
		"o token do PC":          w.phoneUndo(pcA1, id),
		"outra origem": w.doWith(http.MethodPost, "/api/v1/learned/undo", celA, map[string]any{"id": id},
			func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }),
		"o Host estranho": w.doWith(http.MethodGet, "/api/v1/learned?q="+variant, celA, nil,
			func(r *http.Request) { r.Host = "evil.example" }),
	} {
		if rec.Code < 400 {
			t.Errorf("%s: %d, quero um erro", name, rec.Code)
		}
		for _, s := range append(secrets, id) {
			if strings.Contains(rec.Body.String(), s) {
				t.Errorf("%s: a resposta %d repete o que o cliente mandou (%s): %s", name, rec.Code, s, rec.Body.String())
			}
		}
	}
	// Depois do desfazer a lista é vazia: itens=0.
	if l, _ := w.phoneList(celA); len(l.Items) != 0 {
		t.Fatalf("a lista depois de desfazer = %+v", l.Items)
	}

	mu.Lock()
	defer mu.Unlock()
	server := w.logs.String()
	for _, s := range append(secrets, id) {
		if strings.Contains(buf.String(), s) {
			t.Fatalf("o texto do aprendido (%s) no log:\n%s", s, buf.String())
		}
		if strings.Contains(server, s) {
			t.Fatalf("o texto do aprendido (%s) no log do servidor:\n%s", s, server)
		}
	}
	// E o log do servidor existe, com o padrão da rota (e não a URL) e a
	// contagem.
	for _, want := range []string{`rota="GET /api/v1/learned"`, `rota="POST /api/v1/learned/undo"`, "itens=1", "itens=0"} {
		if !strings.Contains(server, want) {
			t.Errorf("o log do servidor não tem %s:\n%s", want, server)
		}
	}
	if strings.Contains(server, "/api/v1/learned?") || strings.Contains(server, "learned/undo?") {
		t.Errorf("o log do servidor tem a query de uma URL:\n%s", server)
	}
}
