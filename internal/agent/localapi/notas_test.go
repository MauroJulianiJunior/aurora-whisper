package localapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // driver puro Go, só para teste (ADR-008).

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// servidorDeNotas monta a Local API sobre um SQLite de verdade em memória
// (as notas só têm essa implementação), sem a cadeia do guard.
func servidorDeNotas(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	pref := prefsDeTeste()
	pref.Notas = storage.NewSQLiteNotaRepository(db)
	srv := localapi.New("dono-1", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref)
	return srv.Handler(), db
}

type erroJSON struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func esperar(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, quer %d; corpo: %s", w.Code, status, w.Body.String())
	}
}

func esperarErro(t *testing.T, w *httptest.ResponseRecorder, status int, code string) erroJSON {
	t.Helper()
	esperar(t, w, status)
	var e erroJSON
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e.Error.Code != code || e.Error.Message == "" {
		t.Fatalf("erro = %s (%v), quer code %s com mensagem", w.Body.String(), err, code)
	}
	return e
}

type notaJSON struct {
	ID             string   `json:"id"`
	Titulo         string   `json:"titulo"`
	Corpo          string   `json:"corpo"`
	Cor            *string  `json:"cor"`
	Fixada         bool     `json:"fixada"`
	Arquivada      bool     `json:"arquivada"`
	Marcadores     []string `json:"marcadores"`
	Posicao        string   `json:"posicao"`
	DitadoDeOrigem *string  `json:"ditado_de_origem"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	DeletedAt      *string  `json:"deleted_at"`
	Version        int64    `json:"version"`
}

func lerNotaJSON(t *testing.T, w *httptest.ResponseRecorder) notaJSON {
	t.Helper()
	var n notaJSON
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil {
		t.Fatalf("nota inválida: %v\n%s", err, w.Body.String())
	}
	return n
}

func lerLista(t *testing.T, w *httptest.ResponseRecorder) []notaJSON {
	t.Helper()
	esperar(t, w, 200)
	var l struct {
		Items []notaJSON `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil || l.Items == nil {
		t.Fatalf("lista inválida (%v): %s", err, w.Body.String())
	}
	return l.Items
}

func criarNotaAPI(t *testing.T, h http.Handler, corpo any) notaJSON {
	t.Helper()
	w := fazer(t, h, "POST", "/api/notas", corpo)
	esperar(t, w, 201)
	return lerNotaJSON(t, w)
}

type marcadorJSON struct {
	ID      string `json:"id"`
	Nome    string `json:"nome"`
	Version int64  `json:"version"`
}

func criarMarcadorAPI(t *testing.T, h http.Handler, nome string) marcadorJSON {
	t.Helper()
	w := fazer(t, h, "POST", "/api/marcadores", map[string]any{"nome": nome})
	esperar(t, w, 201)
	var m marcadorJSON
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNotasAPI_SemRepositorioE503(t *testing.T) {
	srv := localapi.New("dono-1", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), prefsDeTeste())
	h := srv.Handler()
	for _, rota := range [][2]string{
		{"GET", "/api/notas"}, {"POST", "/api/notas"}, {"GET", "/api/notas/x"}, {"PATCH", "/api/notas/x"},
		{"DELETE", "/api/notas/x"}, {"POST", "/api/notas/x/restaurar"}, {"POST", "/api/notas/x/mover"}, {"DELETE", "/api/notas/x/definitivo"},
		{"PUT", "/api/notas/x/marcadores/y"}, {"DELETE", "/api/notas/x/marcadores/y"},
		{"POST", "/api/history/x/virar-nota"}, {"POST", "/api/notas/x/desfazer-virar"},
		{"GET", "/api/marcadores"}, {"POST", "/api/marcadores"}, {"PATCH", "/api/marcadores/x"}, {"DELETE", "/api/marcadores/x"},
	} {
		e := esperarErro(t, fazer(t, h, rota[0], rota[1], "{}"), 503, "UNAVAILABLE")
		if e.Error.Message != localapi.MsgNotasNaoProntas {
			t.Errorf("%s %s: mensagem %q", rota[0], rota[1], e.Error.Message)
		}
	}
}

func TestNotasAPI_CriarELer(t *testing.T) {
	h, _ := servidorDeNotas(t)
	m := criarMarcadorAPI(t, h, "Compras")

	w := fazer(t, h, "POST", "/api/notas", map[string]any{
		"titulo": "Mercado", "corpo": "- [ ] leite\n- [ ] pão", "cor": "verde", "fixada": true,
		"ditado_de_origem": "ditado-1", "marcadores": []string{m.ID},
	})
	esperar(t, w, 201)
	n := lerNotaJSON(t, w)
	if n.ID == "" || n.Titulo != "Mercado" || n.Corpo != "- [ ] leite\n- [ ] pão" || n.Cor == nil || *n.Cor != "verde" ||
		!n.Fixada || n.Arquivada || n.Version != 1 || len(n.Marcadores) != 1 || n.Marcadores[0] != m.ID ||
		n.DitadoDeOrigem == nil || *n.DitadoDeOrigem != "ditado-1" || n.DeletedAt != nil {
		t.Errorf("nota criada: %+v", n)
	}

	// O JSON usa snake_case e traz cor nula e marcadores [] (nunca ausentes).
	simples := fazer(t, h, "POST", "/api/notas", map[string]any{"corpo": "só texto"})
	esperar(t, simples, 201)
	var cru map[string]json.RawMessage
	if err := json.Unmarshal(simples.Body.Bytes(), &cru); err != nil {
		t.Fatal(err)
	}
	if string(cru["cor"]) != "null" || string(cru["marcadores"]) != "[]" || string(cru["titulo"]) != `""` {
		t.Errorf("campos da nota simples: cor=%s marcadores=%s titulo=%s", cru["cor"], cru["marcadores"], cru["titulo"])
	}
	for _, k := range []string{"id", "created_at", "updated_at", "version", "fixada", "arquivada"} {
		if _, ok := cru[k]; !ok {
			t.Errorf("faltou o campo %q", k)
		}
	}
	if _, ok := cru["deleted_at"]; ok {
		t.Errorf("deleted_at deveria faltar numa nota ativa")
	}

	w = fazer(t, h, "GET", "/api/notas/"+n.ID, nil)
	esperar(t, w, 200)
	if lida := lerNotaJSON(t, w); lida.ID != n.ID || lida.Corpo != n.Corpo {
		t.Errorf("GET: %+v", lida)
	}
	esperarErro(t, fazer(t, h, "GET", "/api/notas/nao-existe", nil), 404, "NOT_FOUND")
}

func TestNotasAPI_CriarRecusa(t *testing.T) {
	h, _ := servidorDeNotas(t)
	casos := []struct {
		nome   string
		corpo  any
		trecho string
	}{
		{"json inválido", "{", "JSON"},
		{"campo desconhecido", `{"corpo":"x","corpos":"y"}`, "JSON"},
		{"vazia", map[string]any{"titulo": " ", "corpo": ""}, "vazia"},
		{"cor inválida", map[string]any{"corpo": "x", "cor": "turquesa"}, "cor inválida"},
		{"corpo grande", map[string]any{"corpo": strings.Repeat("a", 64<<10+1)}, "64 KiB"},
		{"título grande", map[string]any{"corpo": "x", "titulo": strings.Repeat("a", 1<<10+1)}, "1 KiB"},
		{"nulo", map[string]any{"corpo": "a\u0000b"}, "caractere inválido"},
		{"marcador inexistente", map[string]any{"corpo": "x", "marcadores": []string{"nao-existe"}}, "marcador inexistente"},
	}
	for _, c := range casos {
		e := esperarErro(t, fazer(t, h, "POST", "/api/notas", c.corpo), 400, "BAD_REQUEST")
		if !strings.Contains(e.Error.Message, c.trecho) {
			t.Errorf("%s: mensagem %q não menciona %q", c.nome, e.Error.Message, c.trecho)
		}
	}
	if l := lerLista(t, fazer(t, h, "GET", "/api/notas", nil)); len(l) != 0 {
		t.Errorf("as recusas deixaram nota: %v", l)
	}
}

func TestNotasAPI_PatchEVersao(t *testing.T) {
	h, _ := servidorDeNotas(t)
	n := criarNotaAPI(t, h, map[string]any{"titulo": "a", "corpo": "b", "cor": "rosa"})
	url := "/api/notas/" + n.ID

	// Edita texto; o resto fica.
	w := fazer(t, h, "PATCH", url, map[string]any{"base_version": 1, "corpo": "- um\n- dois"})
	esperar(t, w, 200)
	n2 := lerNotaJSON(t, w)
	if n2.Version != 2 || n2.Corpo != "- um\n- dois" || n2.Titulo != "a" || n2.Cor == nil || *n2.Cor != "rosa" {
		t.Errorf("depois do PATCH: %+v", n2)
	}

	// Versão velha: 409 CONFLICT, e nada muda.
	esperarErro(t, fazer(t, h, "PATCH", url, map[string]any{"base_version": 1, "corpo": "tarde"}), 409, "CONFLICT")
	if lida := lerNotaJSON(t, fazer(t, h, "GET", url, nil)); lida.Corpo != "- um\n- dois" {
		t.Errorf("o 409 mexeu na nota: %+v", lida)
	}

	// Fixar, arquivar e tirar a cor (null) no mesmo pedido.
	w = fazer(t, h, "PATCH", url, `{"base_version":2,"fixada":true,"arquivada":true,"cor":null}`)
	esperar(t, w, 200)
	n3 := lerNotaJSON(t, w)
	if !n3.Fixada || !n3.Arquivada || n3.Cor != nil || n3.Version != 3 {
		t.Errorf("fixar+arquivar+sem cor: %+v", n3)
	}
	// Trocar a cor e desfazer o resto.
	w = fazer(t, h, "PATCH", url, `{"base_version":3,"fixada":false,"arquivada":false,"cor":"azul"}`)
	esperar(t, w, 200)
	if n4 := lerNotaJSON(t, w); n4.Fixada || n4.Arquivada || n4.Cor == nil || *n4.Cor != "azul" {
		t.Errorf("desfazendo: %+v", n4)
	}
	// "cor": "" também tira a cor.
	w = fazer(t, h, "PATCH", url, `{"base_version":4,"cor":""}`)
	esperar(t, w, 200)
	if n5 := lerNotaJSON(t, w); n5.Cor != nil {
		t.Errorf("cor vazia deveria tirar a cor: %+v", n5)
	}
	// Sem mexer na cor (ausente), ela fica.
	fazer(t, h, "PATCH", url, `{"base_version":5,"cor":"roxo"}`)
	w = fazer(t, h, "PATCH", url, `{"base_version":6,"titulo":"novo"}`)
	esperar(t, w, 200)
	if n7 := lerNotaJSON(t, w); n7.Cor == nil || *n7.Cor != "roxo" || n7.Titulo != "novo" {
		t.Errorf("cor ausente não pode mudar: %+v", n7)
	}

	recusas := []struct {
		nome   string
		corpo  any
		status int
		code   string
	}{
		{"sem base_version", map[string]any{"corpo": "x"}, 400, "BAD_REQUEST"},
		{"nada a mudar", map[string]any{"base_version": 7}, 400, "BAD_REQUEST"},
		{"campo desconhecido", `{"base_version":7,"marcadores":["x"]}`, 400, "BAD_REQUEST"},
		{"cor inválida", map[string]any{"base_version": 7, "cor": "marrom"}, 400, "BAD_REQUEST"},
		{"cor de tipo errado", `{"base_version":7,"cor":5}`, 400, "BAD_REQUEST"},
		{"corpo grande", map[string]any{"base_version": 7, "corpo": strings.Repeat("a", 64<<10+1)}, 400, "BAD_REQUEST"},
		{"json inválido", "[", 400, "BAD_REQUEST"},
	}
	for _, c := range recusas {
		esperarErro(t, fazer(t, h, "PATCH", url, c.corpo), c.status, c.code)
	}
	esperarErro(t, fazer(t, h, "PATCH", "/api/notas/nao-existe", map[string]any{"base_version": 1, "corpo": "x"}), 404, "NOT_FOUND")
	if lida := lerNotaJSON(t, fazer(t, h, "GET", url, nil)); lida.Version != 7 {
		t.Errorf("as recusas subiram a versão: %d", lida.Version)
	}
}

func TestNotasAPI_ListaVistasFiltroEBusca(t *testing.T) {
	h, _ := servidorDeNotas(t)
	m := criarMarcadorAPI(t, h, "Trabalho")
	a := criarNotaAPI(t, h, map[string]any{"titulo": "Reunião", "corpo": "pauta"})
	b := criarNotaAPI(t, h, map[string]any{"titulo": "Mercado", "corpo": "leite", "marcadores": []string{m.ID}})
	c := criarNotaAPI(t, h, map[string]any{"titulo": "Velha", "corpo": "x", "arquivada": true})
	d := criarNotaAPI(t, h, map[string]any{"titulo": "Lixo", "corpo": "x"})
	esperar(t, fazer(t, h, "DELETE", "/api/notas/"+d.ID, nil), 204)

	titulos := func(url string) string {
		var ts []string
		for _, n := range lerLista(t, fazer(t, h, "GET", url, nil)) {
			ts = append(ts, n.Titulo)
		}
		return strings.Join(ts, ",")
	}
	if got := titulos("/api/notas"); got != "Mercado,Reunião" {
		t.Errorf("ativas: %q", got)
	}
	if got := titulos("/api/notas?vista=ativas"); got != "Mercado,Reunião" {
		t.Errorf("vista=ativas: %q", got)
	}
	if got := titulos("/api/notas?vista=arquivo"); got != "Velha" {
		t.Errorf("arquivo: %q", got)
	}
	if got := titulos("/api/notas?vista=lixeira"); got != "Lixo" {
		t.Errorf("lixeira: %q", got)
	}
	if got := titulos("/api/notas?marcador=" + m.ID); got != "Mercado" {
		t.Errorf("por marcador: %q", got)
	}
	if got := titulos("/api/notas?q=REUNIAO"); got != "Reunião" {
		t.Errorf("busca sem caixa nem acento: %q", got)
	}
	if got := titulos("/api/notas?q=leite&marcador=" + m.ID); got != "Mercado" {
		t.Errorf("busca com marcador: %q", got)
	}
	if got := titulos("/api/notas?q=nada-disso"); got != "" {
		t.Errorf("busca sem resultado: %q", got)
	}
	// Vazia é [] e nunca null.
	if w := fazer(t, h, "GET", "/api/notas?q=nada-disso", nil); !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Errorf("lista vazia: %s", w.Body.String())
	}
	esperarErro(t, fazer(t, h, "GET", "/api/notas?vista=todas", nil), 400, "BAD_REQUEST")
	_, _, _ = a, b, c
}

func TestNotasAPI_LixeiraRestaurarEApagarDeVez(t *testing.T) {
	h, _ := servidorDeNotas(t)
	n := criarNotaAPI(t, h, map[string]any{"corpo": "x"})
	url := "/api/notas/" + n.ID

	// Fora da lixeira, não se apaga de vez.
	esperarErro(t, fazer(t, h, "DELETE", url+"/definitivo", nil), 409, "CONFLICT")

	esperar(t, fazer(t, h, "DELETE", url, nil), 204)
	esperar(t, fazer(t, h, "DELETE", url, nil), 204) // repetido: 204 de novo
	na := lerNotaJSON(t, fazer(t, h, "GET", url, nil))
	if na.DeletedAt == nil || na.Version != 2 {
		t.Errorf("na lixeira: %+v", na)
	}
	// Na lixeira não se edita.
	e := esperarErro(t, fazer(t, h, "PATCH", url, map[string]any{"base_version": 2, "corpo": "y"}), 409, "CONFLICT")
	if !strings.Contains(e.Error.Message, "lixeira") {
		t.Errorf("mensagem: %q", e.Error.Message)
	}

	w := fazer(t, h, "POST", url+"/restaurar", nil)
	esperar(t, w, 200)
	if r := lerNotaJSON(t, w); r.DeletedAt != nil || r.Version != 3 {
		t.Errorf("restaurada: %+v", r)
	}
	esperarErro(t, fazer(t, h, "POST", "/api/notas/nao-existe/restaurar", nil), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "DELETE", "/api/notas/nao-existe", nil), 404, "NOT_FOUND")

	esperar(t, fazer(t, h, "DELETE", url, nil), 204)
	esperar(t, fazer(t, h, "DELETE", url+"/definitivo", nil), 204)
	esperarErro(t, fazer(t, h, "GET", url, nil), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "DELETE", url+"/definitivo", nil), 404, "NOT_FOUND")
}

func TestMarcadoresAPI(t *testing.T) {
	h, _ := servidorDeNotas(t)
	a := criarMarcadorAPI(t, h, "  Compras ")
	if a.Nome != "Compras" || a.Version != 1 {
		t.Errorf("marcador criado: %+v", a)
	}
	criarMarcadorAPI(t, h, "Ação")

	// Nome repetido (sem caixa nem acento): 409 com código próprio.
	for _, repetido := range []string{"compras", "ACAO"} {
		esperarErro(t, fazer(t, h, "POST", "/api/marcadores", map[string]any{"nome": repetido}), 409, "NAME_IN_USE")
	}
	esperarErro(t, fazer(t, h, "POST", "/api/marcadores", map[string]any{"nome": "  "}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "POST", "/api/marcadores", map[string]any{"nome": strings.Repeat("x", 51)}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "POST", "/api/marcadores", map[string]any{"nome": "a", "cor": "x"}), 400, "BAD_REQUEST")

	// A lista vem por nome.
	w := fazer(t, h, "GET", "/api/marcadores", nil)
	esperar(t, w, 200)
	var l struct {
		Items []marcadorJSON `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil || len(l.Items) != 2 || l.Items[0].Nome != "Ação" || l.Items[1].Nome != "Compras" {
		t.Errorf("lista: %v %s", err, w.Body.String())
	}

	// Renomear.
	w = fazer(t, h, "PATCH", "/api/marcadores/"+a.ID, map[string]any{"base_version": 1, "nome": "Mercado"})
	esperar(t, w, 200)
	var r marcadorJSON
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	if r.Nome != "Mercado" || r.Version != 2 {
		t.Errorf("renomeado: %+v", r)
	}
	esperarErro(t, fazer(t, h, "PATCH", "/api/marcadores/"+a.ID, map[string]any{"base_version": 1, "nome": "Outro"}), 409, "CONFLICT")
	esperarErro(t, fazer(t, h, "PATCH", "/api/marcadores/"+a.ID, map[string]any{"base_version": 2, "nome": "acao"}), 409, "NAME_IN_USE")
	esperarErro(t, fazer(t, h, "PATCH", "/api/marcadores/"+a.ID, map[string]any{"nome": "Outro"}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "PATCH", "/api/marcadores/"+a.ID, map[string]any{"base_version": 2, "nome": ""}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "PATCH", "/api/marcadores/nao-existe", map[string]any{"base_version": 1, "nome": "x"}), 404, "NOT_FOUND")

	// Apagar.
	esperar(t, fazer(t, h, "DELETE", "/api/marcadores/"+a.ID, nil), 204)
	esperar(t, fazer(t, h, "DELETE", "/api/marcadores/"+a.ID, nil), 204)
	esperarErro(t, fazer(t, h, "DELETE", "/api/marcadores/nao-existe", nil), 404, "NOT_FOUND")
}

func TestNotasAPI_MarcarEDesmarcar(t *testing.T) {
	h, _ := servidorDeNotas(t)
	m := criarMarcadorAPI(t, h, "Ideias")
	n := criarNotaAPI(t, h, map[string]any{"corpo": "x"})
	url := "/api/notas/" + n.ID + "/marcadores/" + m.ID

	w := fazer(t, h, "PUT", url, nil)
	esperar(t, w, 200)
	if got := lerNotaJSON(t, w); len(got.Marcadores) != 1 || got.Marcadores[0] != m.ID || got.Version != 2 {
		t.Errorf("marcada: %+v", got)
	}
	// Idempotente: a versão não sobe.
	w = fazer(t, h, "PUT", url, nil)
	esperar(t, w, 200)
	if got := lerNotaJSON(t, w); got.Version != 2 {
		t.Errorf("marcar de novo subiu a versão: %d", got.Version)
	}
	esperarErro(t, fazer(t, h, "PUT", "/api/notas/"+n.ID+"/marcadores/nao-existe", nil), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "PUT", "/api/notas/nao-existe/marcadores/"+m.ID, nil), 404, "NOT_FOUND")

	w = fazer(t, h, "DELETE", url, nil)
	esperar(t, w, 200)
	if got := lerNotaJSON(t, w); len(got.Marcadores) != 0 || got.Version != 3 {
		t.Errorf("desmarcada: %+v", got)
	}

	// Apagar o marcador tira das notas e não apaga a nota.
	fazer(t, h, "PUT", url, nil)
	esperar(t, fazer(t, h, "DELETE", "/api/marcadores/"+m.ID, nil), 204)
	got := lerNotaJSON(t, fazer(t, h, "GET", "/api/notas/"+n.ID, nil))
	if got.ID != n.ID || len(got.Marcadores) != 0 {
		t.Errorf("a nota depois de apagar o marcador: %+v", got)
	}
}

// As rotas de notas só enxergam as notas do dono do Agent.
func TestNotasAPI_SoDoDono(t *testing.T) {
	h, db := servidorDeNotas(t)
	if _, err := db.Exec(`INSERT INTO local_notes (id, user_id, corpo, created_at, updated_at)
		VALUES ('alheia', 'outro', 'segredo', '2026-10-25T12:00:00.000000000Z', '2026-10-25T12:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_labels (id, user_id, nome, nome_dobrado, created_at, updated_at)
		VALUES ('m-alheio', 'outro', 'Alheio', 'alheio', '2026-10-25T12:00:00.000000000Z', '2026-10-25T12:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	esperarErro(t, fazer(t, h, "GET", "/api/notas/alheia", nil), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "PATCH", "/api/notas/alheia", map[string]any{"base_version": 1, "corpo": "x"}), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "DELETE", "/api/notas/alheia", nil), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "DELETE", "/api/marcadores/m-alheio", nil), 404, "NOT_FOUND")
	if l := lerLista(t, fazer(t, h, "GET", "/api/notas?q=segredo", nil)); len(l) != 0 {
		t.Errorf("a busca achou nota alheia: %v", l)
	}
	w := fazer(t, h, "GET", "/api/marcadores", nil)
	if strings.Contains(w.Body.String(), "Alheio") {
		t.Errorf("a lista de marcadores mostrou marcador alheio: %s", w.Body.String())
	}
}
