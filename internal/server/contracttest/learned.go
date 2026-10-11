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

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// Os casos do aprendido (ADR-019, "Decisões da F8", F8b): a tabela da nuvem
// (cloudstore.DecideLearned), uma linha por caso, as pré-checagens, o
// reenvio que confere a operação, o pull fundido por seq com os ditados, o
// PC da F7 que decodifica e não aplica, o "apagar meus dados" e o log sem
// texto. Pela API, contra o Store em memória e (desde a F8c) o Postgres.

// learnedJSON é o aprendido no fio: o payload do pull e o state do conflict.
type learnedJSON struct {
	ID         string     `json:"id"`
	Variant    string     `json:"variant"`
	Term       string     `json:"term"`
	Restricted bool       `json:"restricted"`
	Domain     *string    `json:"domain"`
	UndoneAt   *time.Time `json:"undone_at"`
}

// learnedKeys são as chaves do learnedJSON, e só elas: nada da linha (o
// normalized_variant, o user_id, as versões, as datas) vai para o fio.
var learnedKeys = []string{"domain", "id", "restricted", "term", "undone_at", "variant"}

type learnedResultJSON struct {
	MutationID    string       `json:"mutation_id"`
	Status        string       `json:"status"`
	Original      string       `json:"original"`
	EntityVersion int64        `json:"entity_version"`
	Mesclado      bool         `json:"mesclado"`
	Code          string       `json:"code"`
	Motivo        string       `json:"motivo"`
	State         *learnedJSON `json:"state"`
}

// typedChangeJSON é uma linha do pull com o payload cru, para ler por tipo.
type typedChangeJSON struct {
	Seq           int64           `json:"seq"`
	EntityType    string          `json:"entity_type"`
	EntityID      string          `json:"entity_id"`
	Operation     string          `json:"operation"`
	EntityVersion int64           `json:"entity_version"`
	DeviceID      *string         `json:"device_id"`
	Payload       json.RawMessage `json:"payload"`
}

type typedPullJSON struct {
	Changes   []typedChangeJSON `json:"changes"`
	LastSeq   int64             `json:"last_seq"`
	HasMore   bool              `json:"has_more"`
	SyncEpoch string            `json:"sync_epoch"`
}

// par é o payload do upsert do aprendido: sem restrição e sem domínio.
func par(variant, term string) map[string]any {
	return map[string]any{"variant": variant, "term": term, "restricted": false, "domain": nil}
}

// parNoDominio é o payload com o domínio e a restrição.
func parNoDominio(variant, term, domain string, restricted bool) map[string]any {
	return map[string]any{"variant": variant, "term": term, "restricted": restricted, "domain": domain}
}

// aprender é o upsert do aprendido, com o id da variante (o que o PC manda).
func aprender(variant, term string, base int64) mut {
	return aprenderCom(variant, base, par(variant, term))
}

func aprenderCom(variant string, base int64, payload map[string]any) mut {
	return mut{MutationID: newID(), EntityType: "learned", EntityID: dictionary.IDDoPar(variant),
		Operation: "upsert", BaseVersion: base, Payload: payload}
}

// desfazer é o delete do aprendido, sem payload.
func desfazer(variant string, base int64) mut {
	return mut{MutationID: newID(), EntityType: "learned", EntityID: dictionary.IDDoPar(variant),
		Operation: "delete", BaseVersion: base}
}

// pushLearned manda uma mutação do aprendido e devolve o resultado e o corpo
// cru.
func (w *world) pushLearned(token string, m mut) (learnedResultJSON, string) {
	w.t.Helper()
	rec := w.pushRaw(token, m)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("push: status %d, quero 200: %s", rec.Code, rec.Body.String())
	}
	p := decode[struct {
		Results []learnedResultJSON `json:"results"`
	}](w.t, rec)
	if len(p.Results) != 1 || p.Results[0].MutationID != m.MutationID {
		w.t.Fatalf("push: resultados %+v para a mutação %s", p.Results, m.MutationID)
	}
	return p.Results[0], rec.Body.String()
}

// mustLearn aprende pelo PC e confere o applied com a versão.
func (w *world) mustLearn(token string, m mut, version int64) {
	w.t.Helper()
	if r, _ := w.pushLearned(token, m); r.Status != "applied" || r.EntityVersion != version {
		w.t.Fatalf("%s %s (base %d) = %+v, quero applied v%d", m.Operation, m.EntityID, m.BaseVersion, r, version)
	}
}

func (w *world) pullTyped(token string, after int64, limit int) typedPullJSON {
	w.t.Helper()
	path := fmt.Sprintf("/api/v1/sync/pull?after_seq=%d", after)
	if limit > 0 {
		path += fmt.Sprintf("&limit=%d", limit)
	}
	rec := w.do(http.MethodGet, path, token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("pull: status %d, quero 200: %s", rec.Code, rec.Body.String())
	}
	return decode[typedPullJSON](w.t, rec)
}

// learnedOf acha o aprendido de uma variante no pull e lê o payload,
// conferindo as chaves.
func learnedOf(t *testing.T, p typedPullJSON, variant string) (typedChangeJSON, learnedJSON, bool) {
	t.Helper()
	id := dictionary.IDDoPar(variant)
	for _, c := range p.Changes {
		if c.EntityID != id {
			continue
		}
		if c.EntityType != "learned" || c.Operation != "upsert" {
			t.Fatalf("a mudança do aprendido %s = %+v, quero learned upsert (o aprendido não tem lápide)", variant, c)
		}
		return c, decodeLearned(t, c.Payload), true
	}
	return typedChangeJSON{}, learnedJSON{}, false
}

// decodeLearned lê o aprendido do fio e confere que ele traz exatamente as
// chaves do contrato, domain e undone_at inclusive (null quando não há).
func decodeLearned(t *testing.T, raw json.RawMessage) learnedJSON {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("o aprendido não é um objeto: %v (%s)", err, raw)
	}
	var got []string
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	if fmt.Sprint(got) != fmt.Sprint(learnedKeys) {
		t.Fatalf("as chaves do aprendido = %v, quero %v: %s", got, learnedKeys, raw)
	}
	var l learnedJSON
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatalf("o aprendido não se lê: %v (%s)", err, raw)
	}
	return l
}

// eventsSince são os eventos do usuário depois dos n primeiros.
func (w *world) eventsSince(userID string, n int) []cloudstore.Event {
	w.t.Helper()
	all := w.b.Events(w.t, userID)
	if len(all) < n {
		w.t.Fatalf("os eventos encolheram: %d, eram %d", len(all), n)
	}
	return all[n:]
}

// O caso "learned antes da F8", invertido: o learned sincroniza, na memória
// e, desde a F8c, no Postgres.
func testLearnedSyncs(t *testing.T, w *world) {
	r, body := w.pushLearned(pcA1, aprender("sf", "ECF", 0))
	if r.Status != "applied" || r.EntityVersion != 1 || r.Code != "" {
		t.Fatalf("o primeiro aprendido = %+v, quero applied v1 (antes da F8: tipo_ainda_nao_sincroniza)", r)
	}
	if strings.Contains(body, "tipo_ainda_nao_sincroniza") {
		t.Fatalf("a resposta ainda fala em tipo_ainda_nao_sincroniza: %s", body)
	}
	ev := w.b.Events(t, w.userA)
	if len(ev) != 1 || ev[0].EntityType != "learned" || ev[0].Operation != "upsert" || ev[0].EntityVersion != 1 ||
		ev[0].EntityID != dictionary.IDDoPar("sf") || ev[0].DeviceID == nil || *ev[0].DeviceID != w.devA1 {
		t.Fatalf("eventos = %+v, quero um learned upsert v1 do PC 1", ev)
	}
	c, l, ok := learnedOf(t, w.pullTyped(pcA2, 0, 0), "sf")
	if !ok || c.Seq != 1 || c.EntityVersion != 1 || c.DeviceID == nil || *c.DeviceID != w.devA1 {
		t.Fatalf("o pull do PC 2 = %+v, quero o aprendido v1 no seq 1, do PC 1", c)
	}
	if l.ID != dictionary.IDDoPar("sf") || l.Variant != "sf" || l.Term != "ECF" || l.Restricted || l.Domain != nil || l.UndoneAt != nil {
		t.Fatalf("o aprendido no pull = %+v", l)
	}
}

// learnedRow é uma linha da tabela da nuvem (ADR-019, "Decisões da F8", as
// regras da nuvem), com um par só dela.
type learnedRow struct {
	name string
	// prep leva a linha da nuvem ao estado da coluna "A linha na nuvem".
	prep func(w *world, v string)
	// send é a mutação, e o PC que a manda.
	send func(v string) (string, mut)
	// A resposta.
	status, code string
	version      int64
	// grava: a versão nova, um seq e um evento (upsert, o desfazer
	// inclusive). Sem gravar, nenhum evento e nenhum seq.
	grava bool
	// check confere o mais (o state do conflict, o estado depois no pull).
	check func(t *testing.T, w *world, v string, r learnedResultJSON)
}

// testLearnedTable é a tabela da nuvem, uma linha por caso, na ordem da ADR
// (a primeira que casa decide). No fim, os seq do usuário são 1..n sem
// buraco: nenhuma linha que não grava gastou seq.
func testLearnedTable(t *testing.T, w *world) {
	ativa := func(w *world, v string) { w.mustLearn(pcA1, aprender(v, "Termo", 0), 1) }
	// outroTermo: o PC 1 aprendeu e depois trocou o termo (v2).
	outroTermo := func(w *world, v string) {
		ativa(w, v)
		w.mustLearn(pcA1, aprender(v, "Termo novo", 1), 2)
	}
	// desfeita: o PC 1 aprendeu e desfez (v2).
	desfeita := func(w *world, v string) {
		ativa(w, v)
		w.mustLearn(pcA1, desfazer(v, 1), 2)
	}
	nada := func(*world, string) {}
	stateIs := func(term string, undone bool) func(*testing.T, *world, string, learnedResultJSON) {
		return func(t *testing.T, _ *world, v string, r learnedResultJSON) {
			if r.State == nil || r.State.ID != dictionary.IDDoPar(v) || r.State.Variant != v || r.State.Term != term ||
				(r.State.UndoneAt != nil) != undone {
				t.Errorf("state = %+v, quero o par da nuvem (termo %q, desfeito %v)", r.State, term, undone)
			}
		}
	}
	pulledAs := func(term string, version int64, undone bool) func(*testing.T, *world, string, learnedResultJSON) {
		return func(t *testing.T, w *world, v string, _ learnedResultJSON) {
			c, l, ok := learnedOf(t, w.pullTyped(pcA2, 0, 0), v)
			if !ok || c.EntityVersion != version || l.Term != term || (l.UndoneAt != nil) != undone {
				t.Errorf("no pull: %+v / %+v, quero termo %q v%d, desfeito %v", c, l, term, version, undone)
			}
		}
	}
	notPulled := func(t *testing.T, w *world, v string, _ learnedResultJSON) {
		if _, _, ok := learnedOf(t, w.pullTyped(pcA2, 0, 0), v); ok {
			t.Error("a recusa criou a linha")
		}
	}

	rows := []learnedRow{
		{name: "não existe, upsert base 0: applied v1, insere", prep: nada,
			send:   func(v string) (string, mut) { return pcA1, aprender(v, "Termo", 0) },
			status: "applied", version: 1, grava: true, check: pulledAs("Termo", 1, false)},
		{name: "não existe, upsert base > 0: base_invalida, nada", prep: nada,
			send:   func(v string) (string, mut) { return pcA1, aprender(v, "Termo", 2) },
			status: "rejected", code: "base_invalida", check: notPulled},
		{name: "existe, upsert base > versão: base_invalida, nada", prep: ativa,
			send:   func(v string) (string, mut) { return pcA1, aprender(v, "Termo novo", 2) },
			status: "rejected", code: "base_invalida", check: pulledAs("Termo", 1, false)},
		{name: "ativa com o mesmo termo, restrição e domínio, upsert base 0 de outro PC: applied com a versão atual, nem seq", prep: outroTermo,
			send:   func(v string) (string, mut) { return pcA2, aprender(v, "Termo novo", 0) },
			status: "applied", version: 2, check: pulledAs("Termo novo", 2, false)},
		{name: "ativa com o mesmo conteúdo, upsert base = versão: applied com a versão atual, nem seq", prep: ativa,
			send:   func(v string) (string, mut) { return pcA1, aprender(v, "Termo", 1) },
			status: "applied", version: 1, check: pulledAs("Termo", 1, false)},
		{name: "com outro conteúdo, upsert base = versão: applied v+1 (troca o termo)", prep: ativa,
			send:   func(v string) (string, mut) { return pcA2, aprender(v, "Termo trocado", 1) },
			status: "applied", version: 2, grava: true, check: pulledAs("Termo trocado", 2, false)},
		{name: "com outra restrição, upsert base = versão: applied v+1", prep: func(w *world, v string) {
			w.mustLearn(pcA1, aprenderCom(v, 0, parNoDominio(v, "Termo", "fiscal", false)), 1)
		},
			send:   func(v string) (string, mut) { return pcA1, aprenderCom(v, 1, parNoDominio(v, "Termo", "fiscal", true)) },
			status: "applied", version: 2, grava: true,
			check: func(t *testing.T, w *world, v string, _ learnedResultJSON) {
				_, l, _ := learnedOf(t, w.pullTyped(pcA2, 0, 0), v)
				if !l.Restricted || l.Domain == nil || *l.Domain != "fiscal" {
					t.Errorf("no pull: %+v, quero restrito ao fiscal", l)
				}
			}},
		{name: "desfeita, upsert base = versão: applied v+1, reaprende e limpa undone_at", prep: desfeita,
			send:   func(v string) (string, mut) { return pcA1, aprender(v, "Termo", 2) },
			status: "applied", version: 3, grava: true, check: pulledAs("Termo", 3, false)},
		{name: "com outro conteúdo, upsert base < versão: conflict, vale o primeiro", prep: outroTermo,
			send:   func(v string) (string, mut) { return pcA2, aprender(v, "Do PC 2", 1) },
			status: "conflict", version: 2, check: func(t *testing.T, w *world, v string, r learnedResultJSON) {
				stateIs("Termo novo", false)(t, w, v, r)
				pulledAs("Termo novo", 2, false)(t, w, v, r)
			}},
		{name: "com outro conteúdo, upsert base 0: conflict", prep: ativa,
			send:   func(v string) (string, mut) { return pcA2, aprender(v, "Do PC 2", 0) },
			status: "conflict", version: 1, check: stateIs("Termo", false)},
		{name: "desfeita, upsert base < versão: conflict, o desfazer vence", prep: desfeita,
			send:   func(v string) (string, mut) { return pcA2, aprender(v, "Termo", 1) },
			status: "conflict", version: 2, check: func(t *testing.T, w *world, v string, r learnedResultJSON) {
				stateIs("Termo", true)(t, w, v, r)
				pulledAs("Termo", 2, true)(t, w, v, r)
			}},
		{name: "ativa, delete com a base atual: applied v+1 com undone_at", prep: ativa,
			send:   func(v string) (string, mut) { return pcA1, desfazer(v, 1) },
			status: "applied", version: 2, grava: true, check: pulledAs("Termo", 2, true)},
		{name: "ativa, delete com a base velha de outro PC: applied v+1 (o desfazer não olha a base)", prep: outroTermo,
			send:   func(v string) (string, mut) { return pcA2, desfazer(v, 1) },
			status: "applied", version: 3, grava: true, check: pulledAs("Termo novo", 3, true)},
		{name: "ativa, delete com a base do futuro: applied v+1, nunca base_invalida", prep: ativa,
			send:   func(v string) (string, mut) { return pcA1, desfazer(v, 7) },
			status: "applied", version: 2, grava: true, check: pulledAs("Termo", 2, true)},
		{name: "desfeita, delete: applied com a versão atual, nada", prep: desfeita,
			send:   func(v string) (string, mut) { return pcA2, desfazer(v, 0) },
			status: "applied", version: 2, check: pulledAs("Termo", 2, true)},
		{name: "não existe, delete: nada_a_desfazer, nada", prep: nada,
			send:   func(v string) (string, mut) { return pcA1, desfazer(v, 0) },
			status: "rejected", code: "nada_a_desfazer", check: notPulled},
		{name: "não existe, delete com base: nada_a_desfazer, nunca base_invalida", prep: nada,
			send:   func(v string) (string, mut) { return pcA1, desfazer(v, 4) },
			status: "rejected", code: "nada_a_desfazer", check: notPulled},
	}
	devOf := map[string]string{pcA1: w.devA1, pcA2: w.devA2}
	for i, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			// Uma variante por linha: "linha a", "linha b", ... (o id não
			// depende do espaço, e as letras não colidem).
			v := "linha " + string(rune('a'+i))
			row.prep(w, v)
			n := len(w.b.Events(t, w.userA))
			tok, m := row.send(v)
			r, body := w.pushLearned(tok, m)
			if r.Status != row.status || r.Code != row.code || r.EntityVersion != row.version {
				t.Fatalf("resposta = %+v, quero %s %q v%d", r, row.status, row.code, row.version)
			}
			if strings.Contains(body, "mesclado") {
				t.Errorf("o aprendido nunca é mesclado: %s", body)
			}
			if (row.status == "conflict") != strings.Contains(body, `"state"`) {
				t.Errorf("o state vem só no conflict: %s", body)
			}
			if row.status == "conflict" {
				var raw struct {
					Results []struct {
						State json.RawMessage `json:"state"`
					} `json:"results"`
				}
				_ = json.Unmarshal([]byte(body), &raw)
				decodeLearned(t, raw.Results[0].State) // as chaves do contrato
			}
			got := w.eventsSince(w.userA, n)
			switch {
			case row.grava && (len(got) != 1 || got[0].EntityType != "learned" || got[0].Operation != "upsert" ||
				got[0].EntityID != dictionary.IDDoPar(v) || got[0].EntityVersion != row.version ||
				got[0].DeviceID == nil || *got[0].DeviceID != devOf[tok]):
				t.Errorf("eventos novos = %+v, quero um learned upsert v%d do aparelho que mandou", got, row.version)
			case !row.grava && len(got) != 0:
				t.Errorf("eventos novos = %+v, quero nenhum", got)
			}
			if row.check != nil {
				row.check(t, w, v, r)
			}
		})
	}
	// Nenhuma linha que não grava gastou seq: os eventos são 1..n.
	requireContiguous(t, w.seqsOf(w.userA), len(w.b.Events(t, w.userA)))
}

// As pré-checagens: recusadas antes do banco, sem gravar entidade nem evento
// e sem gastar seq. A recusa fica em applied_mutations, como toda recusa do
// push: o reenvio é duplicate com o mesmo código.
func testLearnedPrechecks(t *testing.T, w *world) {
	long := strings.Repeat("é", dictionary.MaxRunasPar+1)
	edge := strings.Repeat("ã", dictionary.MaxRunasPar)
	with := func(variant string, changes map[string]any) mut {
		p := par(variant, "ECF")
		for k, v := range changes {
			p[k] = v
		}
		return aprenderCom(variant, 0, p)
	}
	without := func(key string) mut {
		p := par("sf", "ECF")
		delete(p, key)
		return aprenderCom("sf", 0, p)
	}
	// cru manda o payload byte a byte: o json.Marshal de um map trocaria o
	// byte que não é UTF-8 por U+FFFD antes de sair daqui.
	cru := func(payload string) mut {
		m := aprender("sf", "ECF", 0)
		m.Payload = json.RawMessage(payload)
		return m
	}
	cases := []struct {
		name string
		m    mut
		code string
	}{
		{"campo fora da lista", with("sf", map[string]any{"priority": 50}), "campo_desconhecido"},
		{"target_app", with("sf", map[string]any{"target_app": "chrome.exe"}), "campo_desconhecido"},
		{"o mapa de domínios", with("sf", map[string]any{"dominios": map[string]any{"fiscal": []string{"x.exe"}}}), "campo_desconhecido"},
		{"campo no delete", func() mut { m := desfazer("sf", 0); m.Payload = map[string]any{"variant": "sf"}; return m }(), "campo_desconhecido"},
		{"o id que não confere com a variante", func() mut { m := aprender("sf", "ECF", 0); m.EntityID = dictionary.IDDoPar("sfe"); return m }(), "campo_invalido"},
		{"o id de outro UUID", func() mut { m := aprender("sf", "ECF", 0); m.EntityID = newID(); return m }(), "campo_invalido"},
		{"variante vazia", aprender("", "ECF", 0), "campo_invalido"},
		{"variante sem letra nem dígito", aprender("!!!", "ECF", 0), "campo_invalido"},
		{"variante com 81 runas", aprender(long, "ECF", 0), "campo_invalido"},
		{"termo vazio", aprender("sf", "", 0), "campo_invalido"},
		{"termo com 81 runas", aprender("sf", long, 0), "campo_invalido"},
		{"domínio vazio", with("sf", map[string]any{"domain": ""}), "campo_invalido"},
		{"domínio com 81 runas", with("sf", map[string]any{"domain": long}), "campo_invalido"},
		{"restrito sem domínio", with("sf", map[string]any{"restricted": true}), "campo_invalido"},
		{"variante com o caractere nulo", aprender("s\x00f", "ECF", 0), "campo_invalido"},
		{"termo com o caractere nulo", aprender("sf", "E\x00CF", 0), "campo_invalido"},
		{"domínio com o caractere nulo", with("sf", map[string]any{"domain": "fis\x00cal"}), "campo_invalido"},
		// O texto que não é UTF-8 válido, cru no corpo ou num surrogate solto:
		// o encoding/json o trocaria por U+FFFD sem erro, e o par seria gravado
		// com o texto trocado ("s\xfff" tem a chave de "sf").
		{"variante com byte que não é UTF-8", cru("{\"variant\":\"s\xfff\",\"term\":\"ECF\",\"restricted\":false,\"domain\":null}"), "campo_invalido"},
		{"termo com byte que não é UTF-8", cru("{\"variant\":\"sf\",\"term\":\"E\xffCF\",\"restricted\":false,\"domain\":null}"), "campo_invalido"},
		{"domínio com byte que não é UTF-8", cru("{\"variant\":\"sf\",\"term\":\"ECF\",\"restricted\":true,\"domain\":\"fis\xffcal\"}"), "campo_invalido"},
		{"termo com surrogate solto", cru(`{"variant":"sf","term":"E\ud800CF","restricted":false,"domain":null}`), "campo_invalido"},
		{"sem variant", without("variant"), "campo_invalido"},
		{"sem domain", without("domain"), "campo_invalido"},
		{"restricted que não é booleano", with("sf", map[string]any{"restricted": "false"}), "campo_invalido"},
		{"payload null", aprenderCom("sf", 0, nil), "campo_invalido"},
		{"payload que não é objeto", func() mut { m := aprender("sf", "ECF", 0); m.Payload = []string{"sf", "ECF"}; return m }(), "campo_invalido"},
	}
	for _, c := range cases {
		r, _ := w.pushLearned(pcA1, c.m)
		if r.Status != "rejected" || r.Code != c.code {
			t.Errorf("%s: %+v, quero rejected %s", c.name, r, c.code)
		}
	}
	if got := w.b.Events(t, w.userA); len(got) != 0 {
		t.Fatalf("as recusas gravaram eventos: %+v", got)
	}
	if p := w.pullTyped(pcA1, 0, 0); len(p.Changes) != 0 || p.LastSeq != 0 {
		t.Fatalf("as recusas chegaram ao pull: %+v", p)
	}
	// O reenvio de uma recusa é duplicate, com o mesmo código.
	again := cases[0].m
	if r, _ := w.pushLearned(pcA1, again); r.Status != "duplicate" || r.Original != "rejected" || r.Code != "campo_desconhecido" {
		t.Errorf("reenvio da recusa = %+v, quero duplicate do rejected", r)
	}
	// Nenhuma recusa gastou seq: o primeiro que vale é o seq 1. As 80 runas
	// valem (é a medida em runas, e não em bytes: 160 bytes aqui).
	w.mustLearn(pcA1, aprenderCom(edge, 0, parNoDominio(edge, edge, edge, true)), 1)
	c, l, ok := learnedOf(t, w.pullTyped(pcA1, 0, 0), edge)
	if !ok || c.Seq != 1 || l.Variant != edge || l.Term != edge || l.Domain == nil || *l.Domain != edge || !l.Restricted {
		t.Fatalf("o par de 80 runas = %+v / %+v, quero no seq 1, como foi mandado", c, l)
	}
	requireContiguous(t, w.seqsOf(w.userA), 1)
}

// O reenvio confere a operação: o mesmo mutation_id com a mesma é duplicate;
// com outra, mutacao_reusada, e nada muda.
func testLearnedDuplicateOperation(t *testing.T, w *world) {
	learn := aprender("sf", "ECF", 0)
	w.mustLearn(pcA1, learn, 1)
	if r, _ := w.pushLearned(pcA1, learn); r.Status != "duplicate" || r.Original != "applied" || r.EntityVersion != 1 {
		t.Fatalf("reenvio do aprender = %+v, quero duplicate do applied v1", r)
	}
	asUndo := desfazer("sf", 1)
	asUndo.MutationID = learn.MutationID
	if r, _ := w.pushLearned(pcA1, asUndo); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Fatalf("o id do aprender como desfazer = %+v, quero rejected mutacao_reusada", r)
	}
	if _, l, _ := learnedOf(t, w.pullTyped(pcA1, 0, 0), "sf"); l.UndoneAt != nil {
		t.Fatal("o desfazer com o id reusado desfez o par")
	}

	undo := desfazer("sf", 1)
	w.mustLearn(pcA1, undo, 2)
	if r, _ := w.pushLearned(pcA1, undo); r.Status != "duplicate" || r.Original != "applied" || r.EntityVersion != 2 {
		t.Fatalf("reenvio do desfazer = %+v, quero duplicate do applied v2", r)
	}
	asLearn := aprender("sf", "ECF", 2)
	asLearn.MutationID = undo.MutationID
	if r, _ := w.pushLearned(pcA1, asLearn); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Fatalf("o id do desfazer como reaprender = %+v, quero rejected mutacao_reusada", r)
	}
	if _, l, _ := learnedOf(t, w.pullTyped(pcA1, 0, 0), "sf"); l.UndoneAt == nil {
		t.Fatal("o reaprender com o id reusado reaprendeu o par")
	}
	// O mesmo id, a mesma operação, por outro aparelho: mutacao_reusada,
	// como no ditado.
	if r, _ := w.pushLearned(pcA2, undo); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Fatalf("o desfazer do PC 1 reenviado pelo PC 2 = %+v, quero rejected mutacao_reusada", r)
	}
	// O ditado: o mesmo id com o delete (a operação recusada) também é
	// outra operação.
	id := newID()
	w.mustCreate(pcA1, id, "um ditado")
	del := mut{MutationID: newID(), EntityType: "transcription", EntityID: id, Operation: "delete"}
	if r := w.pushOne(pcA1, del); r.Status != "rejected" || r.Code != "operacao_invalida" {
		t.Fatalf("delete de ditado = %+v", r)
	}
	if r := w.pushOne(pcA1, del); r.Status != "duplicate" || r.Code != "operacao_invalida" {
		t.Fatalf("reenvio do delete de ditado = %+v, quero duplicate do rejected", r)
	}
	asUpsert := change(id, 1, map[string]any{"final_text": "outro"})
	asUpsert.MutationID = del.MutationID
	if r := w.pushOne(pcA1, asUpsert); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Fatalf("o id do delete como upsert = %+v, quero rejected mutacao_reusada", r)
	}
	// aprender, desfazer, o ditado: três eventos, sem buraco.
	requireContiguous(t, w.seqsOf(w.userA), 3)
}

// O desfazer cuja resposta se perdeu, seguido de um reaprender no mesmo PC:
// o PC repete o envio em voo com a mesma operação (duplicate, com a versão
// do desfazer) e manda o reaprender com outro id e essa base. Termina
// reaprendido na nuvem, sem conflito, sem nada que vire etiqueta no PC.
func testLearnedUndoLostThenRelearn(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)
	undo := desfazer("sf", 1)
	w.mustLearn(pcA1, undo, 2) // a resposta "se perde": o PC não a viu
	r, _ := w.pushLearned(pcA1, undo)
	if r.Status != "duplicate" || r.Original != "applied" || r.EntityVersion != 2 {
		t.Fatalf("o desfazer em voo reenviado = %+v, quero duplicate do applied v2", r)
	}
	r, _ = w.pushLearned(pcA1, aprender("sf", "ECF", r.EntityVersion))
	if r.Status != "applied" || r.EntityVersion != 3 || r.State != nil {
		t.Fatalf("o reaprender com a base que voltou = %+v, quero applied v3, sem conflito", r)
	}
	c, l, ok := learnedOf(t, w.pullTyped(pcA2, 0, 0), "sf")
	if !ok || c.EntityVersion != 3 || l.UndoneAt != nil || l.Term != "ECF" {
		t.Fatalf("na nuvem: %+v / %+v, quero reaprendido, v3", c, l)
	}
	// O caminho errado, para contraste: reenviar o desfazer em voo como
	// reaprender, com o mesmo id, seria mutacao_reusada (e a base velha
	// perderia para o próprio desfazer).
	wrong := aprender("sf", "ECF", 1)
	wrong.MutationID = undo.MutationID
	if r, _ := w.pushLearned(pcA1, wrong); r.Status != "rejected" || r.Code != "mutacao_reusada" {
		t.Fatalf("o desfazer em voo reenviado como reaprender = %+v, quero rejected mutacao_reusada", r)
	}
	requireContiguous(t, w.seqsOf(w.userA), 3)
}

// O mesmo id (a mesma variante) em duas contas é outra linha: o que uma faz
// não aparece nem muda na outra, e o seq de cada uma é dela.
func testLearnedTwoAccounts(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)
	w.mustLearn(pcB, aprender("SF", "SPED Fiscal", 0), 1)
	w.mustLearn(pcA1, desfazer("sf", 1), 2)
	_, lb, ok := learnedOf(t, w.pullTyped(pcB, 0, 0), "sf")
	if !ok || lb.Term != "SPED Fiscal" || lb.Variant != "SF" || lb.UndoneAt != nil {
		t.Fatalf("a linha de B = %+v, quero a dela, ativa (o desfazer foi de A)", lb)
	}
	_, la, _ := learnedOf(t, w.pullTyped(pcA1, 0, 0), "sf")
	if la.Term != "ECF" || la.UndoneAt == nil {
		t.Fatalf("a linha de A = %+v, quero a dela, desfeita", la)
	}
	// B não alcança o que só A tem.
	w.mustLearn(pcA1, aprender("nfe", "NF-e", 0), 1)
	if r, _ := w.pushLearned(pcB, desfazer("nfe", 1)); r.Status != "rejected" || r.Code != "nada_a_desfazer" {
		t.Fatalf("B desfaz o par que só A tem = %+v, quero nada_a_desfazer", r)
	}
	if r, _ := w.pushLearned(pcB, aprender("nfe", "Outro", 1)); r.Status != "rejected" || r.Code != "base_invalida" {
		t.Fatalf("B muda, com a base de A, o par que só A tem = %+v, quero base_invalida", r)
	}
	if _, l, _ := learnedOf(t, w.pullTyped(pcA1, 0, 0), "nfe"); l.Term != "NF-e" || l.UndoneAt != nil {
		t.Fatalf("o par de A mudou: %+v", l)
	}
	if _, _, ok := learnedOf(t, w.pullTyped(pcB, 0, 0), "nfe"); ok {
		t.Fatal("o pull de B traz o par de A")
	}
	// O mesmo mutation_id nas duas contas: cada uma aplica o seu.
	ma, mb := aprender("ecd", "ECD", 0), aprender("ecd", "ECD", 0)
	mb.MutationID = ma.MutationID
	w.mustLearn(pcA1, ma, 1)
	w.mustLearn(pcB, mb, 1)
	requireContiguous(t, w.seqsOf(w.userA), 4)
	requireContiguous(t, w.seqsOf(w.userB), 2)
}

// O pull funde por seq os ditados, as lápides e o aprendido, no estado atual
// de cada um, com o payload por tipo; e pagina sem perder nem repetir.
func testLearnedPullMerged(t *testing.T, w *world) {
	d1, d2 := newID(), newID()
	w.mustCreate(pcA1, d1, "primeiro ditado")                                                // 1
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)                                           // 2
	w.mustCreate(pcA1, d2, "segundo ditado")                                                 // 3
	w.mustLearn(pcA2, aprenderCom("nfe", 0, parNoDominio("nfe", "NF-e", "fiscal", true)), 1) // 4
	if rec := w.edit(celA, d1, 1, "primeiro, editado"); rec.Code != http.StatusOK {          // 5
		t.Fatalf("edição: %d", rec.Code)
	}
	w.mustLearn(pcA1, desfazer("sf", 1), 2)                                                                        // 6
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+d2, celA, nil); rec.Code != http.StatusNoContent { // 7
		t.Fatalf("lixeira: %d", rec.Code)
	}
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, d2, now0); err != nil { // 8, lápide
		t.Fatalf("Purge: %v", err)
	}
	w.mustLearn(pcA1, aprender("ecd", "ECD", 0), 1) // 9
	requireContiguous(t, w.seqsOf(w.userA), 9)

	ids := map[string]string{
		d1: "d1", d2: "d2", dictionary.IDDoPar("sf"): "sf", dictionary.IDDoPar("nfe"): "nfe", dictionary.IDDoPar("ecd"): "ecd",
	}
	type page struct {
		after   int64
		limit   int
		names   []string
		seqs    []int64
		last    int64
		hasMore bool
	}
	for _, pg := range []page{
		{0, 2, []string{"nfe", "d1"}, []int64{4, 5}, 5, true},
		{5, 2, []string{"sf", "d2"}, []int64{6, 8}, 8, true},
		{8, 2, []string{"ecd"}, []int64{9}, 9, false},
		{0, 0, []string{"nfe", "d1", "sf", "d2", "ecd"}, []int64{4, 5, 6, 8, 9}, 9, false},
		// O cursor exatamente num aprendido: ele não volta (after_seq é
		// exclusivo).
		{4, 0, []string{"d1", "sf", "d2", "ecd"}, []int64{5, 6, 8, 9}, 9, false},
		{6, 0, []string{"d2", "ecd"}, []int64{8, 9}, 9, false},
		{9, 0, nil, nil, 9, false},
	} {
		p := w.pullTyped(pcA1, pg.after, pg.limit)
		var names []string
		var seqs []int64
		for _, c := range p.Changes {
			names = append(names, ids[c.EntityID])
			seqs = append(seqs, c.Seq)
		}
		if fmt.Sprint(names) != fmt.Sprint(pg.names) || fmt.Sprint(seqs) != fmt.Sprint(pg.seqs) ||
			p.LastSeq != pg.last || p.HasMore != pg.hasMore {
			t.Errorf("pull depois de %d (limit %d) = %v %v last %d more %v; quero %v %v %d %v",
				pg.after, pg.limit, names, seqs, p.LastSeq, p.HasMore, pg.names, pg.seqs, pg.last, pg.hasMore)
		}
	}

	p := w.pullTyped(pcA1, 0, 0)
	for _, c := range p.Changes {
		switch ids[c.EntityID] {
		case "d1":
			var it itemJSON
			if err := json.Unmarshal(c.Payload, &it); err != nil || c.EntityType != "transcription" || it.FinalText != "primeiro, editado" ||
				c.DeviceID != nil {
				t.Errorf("o ditado = %+v / %+v, quero o editado no celular", c, it)
			}
		case "d2":
			if c.EntityType != "transcription" || c.Operation != "tombstone" || len(c.Payload) != 0 {
				t.Errorf("a lápide = %+v", c)
			}
		}
	}
	c, sf, _ := learnedOf(t, p, "sf")
	if sf.UndoneAt == nil || sf.Term != "ECF" || c.EntityVersion != 2 || c.DeviceID == nil || *c.DeviceID != w.devA1 {
		t.Errorf("sf = %+v / %+v, quero desfeito (v2, pelo PC 1), com o texto", c, sf)
	}
	if !sf.UndoneAt.Equal(now0) {
		t.Errorf("undone_at = %v, quero o relógio do servidor %v", sf.UndoneAt, now0)
	}
	c, nfe, _ := learnedOf(t, p, "nfe")
	if !nfe.Restricted || nfe.Domain == nil || *nfe.Domain != "fiscal" || nfe.UndoneAt != nil || c.DeviceID == nil || *c.DeviceID != w.devA2 {
		t.Errorf("nfe = %+v / %+v, quero restrito ao fiscal, do PC 2", c, nfe)
	}
	// As chaves do payload já são conferidas por decodeLearned; aqui, o corpo
	// inteiro de uma página só com o aprendido.
	raw := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=8", pcA1, nil).Body.String()
	for _, banned := range []string{"normalized_variant", "user_id", "created_at", "updated_at", "priority", "dominios"} {
		if strings.Contains(raw, banned) {
			t.Errorf("o pull do aprendido expõe %q: %s", banned, raw)
		}
	}
	for _, needed := range []string{`"domain":null`, `"undone_at":null`, `"restricted":false`} {
		if !strings.Contains(raw, needed) {
			t.Errorf("o pull do aprendido não traz %s explícito: %s", needed, raw)
		}
	}
}

// Um PC da F7 (internal/sync, antes da F8f) recebe o aprendido: decodifica a
// mudança no Item dele sem erro (nenhum nome coincide, com outro tipo), a
// conta como "não aplicado" (e o cursor anda), e lê o ditado da mesma página
// como sempre. O state do conflict do aprendido também decodifica.
func testLearnedDecodesOnF7PC(t *testing.T, w *world) {
	d := newID()
	w.mustCreate(pcA1, d, "um ditado")
	w.mustLearn(pcA1, aprenderCom("nfe", 0, parNoDominio("nfe", "NF-e", "fiscal", true)), 1)
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)
	w.mustLearn(pcA1, desfazer("sf", 1), 2)

	// O pull, como o syncengine.Cliente o lê (json.Unmarshal no ChangePage).
	rec := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", pcA2, nil)
	var page syncengine.ChangePage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("o PC da F7 não lê o pull com o aprendido: %v\n%s", err, rec.Body.String())
	}
	if len(page.Changes) != 3 || page.LastSeq != 4 || page.HasMore {
		t.Fatalf("a página = %+v, quero o ditado e os dois aprendidos, até o seq 4", page)
	}
	for _, c := range page.Changes {
		dec := syncengine.DecidirMudanca(storage.EstadoDoItem{ID: c.EntityID}, c)
		switch c.EntityType {
		case "learned":
			if c.Payload == nil || c.Payload.ID != c.EntityID || c.Payload.FinalText != "" {
				t.Errorf("o aprendido no Item da F7 = %+v", c.Payload)
			}
			if dec.Tipo != syncengine.TipoNaoAplicado {
				t.Errorf("o PC da F7 decidiu %q para o aprendido, quero %q", dec.Tipo, syncengine.TipoNaoAplicado)
			}
		case "transcription":
			if c.Payload == nil || c.Payload.FinalText != "um ditado" || dec.Tipo != syncengine.TipoRecebido {
				t.Errorf("o ditado na mesma página = %+v (%q), quero recebido", c.Payload, dec.Tipo)
			}
		default:
			t.Errorf("mudança inesperada: %+v", c)
		}
	}

	// O conflict do aprendido, como o Cliente lê o push (PushResult).
	rec = w.pushRaw(pcA2, aprender("nfe", "Outro termo", 0))
	var res syncengine.PushResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("o PC da F7 não lê o conflict do aprendido: %v\n%s", err, rec.Body.String())
	}
	if len(res.Results) != 1 || res.Results[0].Status != syncengine.StatusConflict || res.Results[0].State == nil ||
		res.Results[0].State.ID != dictionary.IDDoPar("nfe") {
		t.Fatalf("o conflict no PushResult da F7 = %+v", res.Results)
	}
}

// "Apagar meus dados" leva o aprendido: a conta recomeça sem par nenhum, um
// PC novo não recebe nada e manda tudo do zero; a outra conta fica.
func testLearnedEraseAccount(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)
	w.mustLearn(pcA1, aprenderCom("nfe", 0, parNoDominio("nfe", "NF-e", "fiscal", true)), 1)
	w.mustLearn(pcB, aprender("sf", "SPED", 0), 1)
	epoch := w.pullTyped(pcA1, 0, 0).SyncEpoch
	if rec := w.do(http.MethodDelete, "/api/v1/me/data", celA, map[string]any{"confirmar": "apagar"}); rec.Code != http.StatusNoContent {
		t.Fatalf("apagar meus dados: %d %s", rec.Code, rec.Body.String())
	}
	const pcNovo = "pc-a-novo"
	w.newDevice(w.userA, pcNovo)
	p := w.pullTyped(pcNovo, 0, 0)
	if len(p.Changes) != 0 || p.SyncEpoch == epoch {
		t.Fatalf("o pull depois de apagar = %+v, quero vazio e com outro epoch", p)
	}
	if r, _ := w.pushLearned(pcNovo, desfazer("sf", 1)); r.Status != "rejected" || r.Code != "nada_a_desfazer" {
		t.Fatalf("desfazer depois de apagar = %+v, quero nada_a_desfazer", r)
	}
	if r, _ := w.pushLearned(pcNovo, aprender("nfe", "NF-e", 1)); r.Status != "rejected" || r.Code != "base_invalida" {
		t.Fatalf("mudar com a base de antes = %+v, quero base_invalida (o PC zera a base)", r)
	}
	w.mustLearn(pcNovo, aprender("nfe", "NF-e", 0), 1)
	if _, l, ok := learnedOf(t, w.pullTyped(pcB, 0, 0), "sf"); !ok || l.Term != "SPED" || l.UndoneAt != nil {
		t.Fatalf("o par de B depois de A apagar = %+v, quero o dele, intacto", l)
	}
}

// Nenhum caminho do aprendido escreve a variante, o termo ou o domínio em
// log: nem os felizes, nem as recusas, nem o conflito, nem o erro do Store.
func testLearnedNoTextInLogs(t *testing.T, w *world) {
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
	w.mustLearn(pcA1, aprenderCom(variant, 0, parNoDominio(variant, term, domain, true)), 1)
	w.pushLearned(pcA2, aprenderCom(variant, 0, parNoDominio(variant, term, domain, true)))        // o mesmo conteúdo
	w.pushLearned(pcA2, aprender(variant, "outro "+term, 0))                                       // conflict
	w.pushLearned(pcA1, aprender(variant, term, 9))                                                // base_invalida
	w.pushLearned(pcA1, aprender(variant, term+"\x00", 1))                                         // campo_invalido (o nulo)
	w.pushLearned(pcA1, aprenderCom(variant, 1, map[string]any{"variant": variant, domain: term})) // campo_desconhecido
	bad := aprender(variant, term, 1)
	bad.EntityID = newID()
	w.pushLearned(pcA1, bad) // o id que não confere
	undo := desfazer(variant, 1)
	w.mustLearn(pcA1, undo, 2)
	w.pushLearned(pcA1, undo)                          // duplicate
	w.pushLearned(pcA1, desfazer(variant, 2))          // já desfeito
	w.pushLearned(pcA1, desfazer("outra "+variant, 0)) // nada_a_desfazer
	reused := aprender(variant, term, 2)
	reused.MutationID = undo.MutationID
	w.pushLearned(pcA1, reused) // mutacao_reusada
	w.do(http.MethodPost, "/api/v1/sync/push", pcA1, `{"mutations":[{"entity_type":"learned","payload":{"variant":"`+variant)
	w.pullTyped(pcA1, 0, 0)
	// Um erro de verdade do Store (o aparelho não é do usuário).
	_, err := w.b.Store.Push(context.Background(), cloudstore.Actor{UserID: w.userA, DeviceID: w.devB},
		[]cloudstore.Mutation{{MutationID: newID(), EntityType: "learned", EntityID: dictionary.IDDoPar(variant),
			Operation: "upsert", Payload: []byte(`{"variant":"` + variant + `","term":"` + term +
				`","restricted":true,"domain":"` + domain + `"}`)}}, now0)
	if err == nil {
		t.Error("push com o aparelho de outro usuário não falhou")
	} else {
		for _, s := range secrets {
			if strings.Contains(err.Error(), s) {
				t.Errorf("o erro do Store carrega o texto do aprendido: %v", err)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	server := w.logs.String()
	for _, s := range secrets {
		if strings.Contains(buf.String(), s) {
			t.Fatalf("o texto do aprendido (%s) no log:\n%s", s, buf.String())
		}
		if strings.Contains(server, s) {
			t.Fatalf("o texto do aprendido (%s) no log do servidor:\n%s", s, server)
		}
	}
	// E o log do servidor existe, com as contagens.
	for _, want := range []string{`rota="POST /api/v1/sync/push"`, "mutacoes=1", "conflict=1", "rejected=1", "mudancas="} {
		if !strings.Contains(server, want) {
			t.Errorf("o log do servidor não tem %s:\n%s", want, server)
		}
	}
}
