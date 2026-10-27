package localapi_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
)

// A escolha do selo da nota pela API (ADR-022, N7):
// POST /api/notas/{id}/resolver-conflito.

type notaComSeloJSON struct {
	notaJSON
	Conflito *struct {
		Titulo     string  `json:"titulo"`
		Corpo      string  `json:"corpo"`
		DeviceID   *string `json:"device_id"`
		ConflictAt string  `json:"conflict_at"`
	} `json:"conflito"`
}

func lerNotaComSelo(t *testing.T, h http.Handler, id string) notaComSeloJSON {
	t.Helper()
	w := fazer(t, h, "GET", "/api/notas/"+id, nil)
	esperar(t, w, 200)
	var n notaComSeloJSON
	if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

// seloNaNota põe o selo direto no banco (quem o grava, no uso real, é o sync).
func seloNaNota(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE local_notes SET conflito_titulo = 'Da outra', conflito_corpo = 'corpo da outra',
		conflito_device_id = 'outro-aparelho', conflito_em = ? WHERE id = ?`, "2026-10-20T10:00:00.000000000Z", id); err != nil {
		t.Fatal(err)
	}
}

func TestNotasAPI_SeloNoDTO(t *testing.T) {
	h, db := servidorDeNotas(t)
	n := criarNotaAPI(t, h, map[string]any{"titulo": "Minha", "corpo": "meu corpo"})

	// Sem selo, o campo não existe no JSON.
	w := fazer(t, h, "GET", "/api/notas/"+n.ID, nil)
	esperar(t, w, 200)
	var cru map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &cru); err != nil {
		t.Fatal(err)
	}
	if _, tem := cru["conflito"]; tem {
		t.Errorf("nota sem selo trouxe conflito: %s", w.Body.String())
	}

	seloNaNota(t, db, n.ID)
	g := lerNotaComSelo(t, h, n.ID)
	if g.Conflito == nil || g.Conflito.Titulo != "Da outra" || g.Conflito.Corpo != "corpo da outra" ||
		g.Conflito.DeviceID == nil || *g.Conflito.DeviceID != "outro-aparelho" || g.Conflito.ConflictAt != "2026-10-20T10:00:00Z" {
		t.Errorf("selo no DTO: %+v", g.Conflito)
	}
	// A lista leva o selo também (o cartão mostra o aviso).
	w = fazer(t, h, "GET", "/api/notas", nil)
	var lista struct {
		Items []notaComSeloJSON `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &lista); err != nil || len(lista.Items) != 1 || lista.Items[0].Conflito == nil {
		t.Errorf("a lista não levou o selo (%v): %s", err, w.Body.String())
	}
}

func TestNotasAPI_ResolverConflito(t *testing.T) {
	h, db := servidorDeNotas(t)

	// Manter: o texto atual fica, o selo sai, a versão sobe.
	a := criarNotaAPI(t, h, map[string]any{"titulo": "Minha", "corpo": "meu corpo"})
	seloNaNota(t, db, a.ID)
	w := fazer(t, h, "POST", "/api/notas/"+a.ID+"/resolver-conflito", map[string]any{"base_version": a.Version, "escolha": "manter"})
	esperar(t, w, 200)
	var m notaComSeloJSON
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Titulo != "Minha" || m.Corpo != "meu corpo" || m.Conflito != nil || m.Version != a.Version+1 {
		t.Errorf("manter: %+v", m)
	}

	// Trocar: o título e o corpo do selo passam a valer.
	b := criarNotaAPI(t, h, map[string]any{"titulo": "Minha", "corpo": "meu corpo"})
	seloNaNota(t, db, b.ID)
	w = fazer(t, h, "POST", "/api/notas/"+b.ID+"/resolver-conflito", map[string]any{"base_version": b.Version, "escolha": "trocar"})
	esperar(t, w, 200)
	var tr notaComSeloJSON
	if err := json.Unmarshal(w.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Titulo != "Da outra" || tr.Corpo != "corpo da outra" || tr.Conflito != nil || tr.Version != b.Version+1 {
		t.Errorf("trocar: %+v", tr)
	}
	if g := lerNotaComSelo(t, h, b.ID); g.Titulo != "Da outra" || g.Conflito != nil {
		t.Errorf("depois de trocar, lendo de novo: %+v", g)
	}
}

func TestNotasAPI_ResolverConflitoRecusas(t *testing.T) {
	h, db := servidorDeNotas(t)
	n := criarNotaAPI(t, h, map[string]any{"titulo": "Minha", "corpo": "meu corpo"})
	url := "/api/notas/" + n.ID + "/resolver-conflito"

	// Sem selo, a escolha é 409 (o painel relê).
	esperarErro(t, fazer(t, h, "POST", url, map[string]any{"base_version": n.Version, "escolha": "manter"}), 409, "CONFLICT")

	seloNaNota(t, db, n.ID)
	// Sem base_version, ou escolha fora das duas, ou campo desconhecido: 400.
	esperarErro(t, fazer(t, h, "POST", url, map[string]any{"escolha": "manter"}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "POST", url, map[string]any{"base_version": n.Version, "escolha": "talvez"}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "POST", url, map[string]any{"base_version": n.Version}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "POST", url, map[string]any{"base_version": n.Version, "escolha": "manter", "texto": "x"}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, h, "POST", url, "{"), 400, "BAD_REQUEST")
	// Versão velha: 409.
	esperarErro(t, fazer(t, h, "POST", url, map[string]any{"base_version": n.Version + 1, "escolha": "manter"}), 409, "CONFLICT")
	// Nota que não existe: 404.
	esperarErro(t, fazer(t, h, "POST", "/api/notas/nao-existe/resolver-conflito", map[string]any{"base_version": 1, "escolha": "manter"}), 404, "NOT_FOUND")

	// As recusas não mexeram na nota nem no selo.
	if g := lerNotaComSelo(t, h, n.ID); g.Conflito == nil || g.Titulo != "Minha" || g.Version != n.Version {
		t.Errorf("as recusas mudaram a nota: %+v", g)
	}
}
