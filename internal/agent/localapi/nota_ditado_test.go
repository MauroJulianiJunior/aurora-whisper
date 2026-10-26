package localapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As rotas do "Virar nota" (ADR-022, N3), sobre um SQLite de verdade: o
// histórico e as notas moram no mesmo banco, e a ação mexe nas duas tabelas.

func servidorDeVirarNota(t *testing.T) (http.Handler, *sql.DB, *storage.SQLiteTranscriptionRepository) {
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
	hist := storage.NewSQLiteTranscriptionRepository(db)
	pref := prefsDeTeste()
	pref.Notas = storage.NewSQLiteNotaRepository(db)
	srv := localapi.New("dono-1", hist, storage.NewInMemorySettingsRepository(), pref)
	return srv.Handler(), db, hist
}

func ditadoDeVirarNota(t *testing.T, hist *storage.SQLiteTranscriptionRepository, id, final string) {
	t.Helper()
	agora := time.Now().UTC()
	err := hist.Put(storage.Transcription{
		ID: id, UserID: "dono-1", SourcePlatform: "windows", RawText: "bruto " + id, FinalText: final,
		Mode: "smart", ASRProvider: "local", Status: "done", CreatedAt: agora, UpdatedAt: agora, Version: 1,
		SyncState: "pending",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func idsDoHistorico(t *testing.T, h http.Handler, query string) []string {
	t.Helper()
	w := fazer(t, h, "GET", "/api/history"+query, nil)
	esperar(t, w, 200)
	var p struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestVirarNotaAPI_CriaEsconde(t *testing.T) {
	h, _, hist := servidorDeVirarNota(t)
	ditadoDeVirarNota(t, hist, "d1", "- leite\n- pão")
	ditadoDeVirarNota(t, hist, "d2", "outro")

	w := fazer(t, h, "POST", "/api/history/d1/virar-nota", nil)
	esperar(t, w, 201)
	n := lerNotaJSON(t, w)
	if n.Corpo != "- leite\n- pão" || n.Titulo != "" || n.Cor != nil || n.DitadoDeOrigem == nil || *n.DitadoDeOrigem != "d1" {
		t.Errorf("nota = %+v", n)
	}
	// A nota está nas notas, com a origem.
	l := lerLista(t, fazer(t, h, "GET", "/api/notas", nil))
	if len(l) != 1 || l[0].ID != n.ID {
		t.Errorf("as notas = %+v", l)
	}
	// O ditado saiu do histórico, e da busca, e não está na lixeira.
	if ids := idsDoHistorico(t, h, ""); len(ids) != 1 || ids[0] != "d2" {
		t.Errorf("o histórico = %v; quero só d2", ids)
	}
	if ids := idsDoHistorico(t, h, "?q=bruto"); len(ids) != 1 || ids[0] != "d2" {
		t.Errorf("a busca = %v; quero só d2", ids)
	}
	if ids := idsDoHistorico(t, h, "?trash=true"); len(ids) != 0 {
		t.Errorf("a lixeira = %v; quero vazia", ids)
	}
}

func TestVirarNotaAPI_Recusas(t *testing.T) {
	h, _, hist := servidorDeVirarNota(t)
	ditadoDeVirarNota(t, hist, "d-lixeira", "f")
	ditadoDeVirarNota(t, hist, "d-ok", "f")
	ditadoDeVirarNota(t, hist, "d-vazio", "   ")
	esperar(t, fazer(t, h, "DELETE", "/api/history/d-lixeira", nil), 204)
	esperar(t, fazer(t, h, "POST", "/api/history/d-ok/virar-nota", nil), 201)

	esperarErro(t, fazer(t, h, "POST", "/api/history/nao-existe/virar-nota", nil), 404, "NOT_FOUND")
	esperarErro(t, fazer(t, h, "POST", "/api/history/d-lixeira/virar-nota", nil), 409, "IN_TRASH")
	esperarErro(t, fazer(t, h, "POST", "/api/history/d-ok/virar-nota", nil), 409, "ALREADY_NOTE")
	// d-vazio tem o bruto ("bruto d-vazio"), então vira nota com ele.
	w := fazer(t, h, "POST", "/api/history/d-vazio/virar-nota", nil)
	esperar(t, w, 201)
	if n := lerNotaJSON(t, w); n.Corpo != "bruto d-vazio" {
		t.Errorf("corpo = %q; quero o texto bruto", n.Corpo)
	}
	if l := lerLista(t, fazer(t, h, "GET", "/api/notas", nil)); len(l) != 2 {
		t.Errorf("%d notas; quero 2 (as recusas não criam)", len(l))
	}
}

func TestDesfazerVirarNotaAPI(t *testing.T) {
	h, _, hist := servidorDeVirarNota(t)
	ditadoDeVirarNota(t, hist, "d1", "um")
	ditadoDeVirarNota(t, hist, "d2", "dois")
	n1 := lerNotaJSON(t, fazer(t, h, "POST", "/api/history/d1/virar-nota", nil))
	n2 := lerNotaJSON(t, fazer(t, h, "POST", "/api/history/d2/virar-nota", nil))

	// Intacta: a nota sai e o ditado volta.
	w := fazer(t, h, "POST", "/api/notas/"+n1.ID+"/desfazer-virar", nil)
	esperar(t, w, 200)
	var r struct {
		DitadoID    string `json:"ditado_id"`
		NotaApagada bool   `json:"nota_apagada"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || r.DitadoID != "d1" || !r.NotaApagada {
		t.Errorf("resposta = %s (%v)", w.Body.String(), err)
	}
	esperarErro(t, fazer(t, h, "GET", "/api/notas/"+n1.ID, nil), 404, "NOT_FOUND")
	if ids := idsDoHistorico(t, h, ""); len(ids) != 1 || ids[0] != "d1" {
		t.Errorf("o histórico = %v; quero d1 de volta", ids)
	}

	// Editada: a nota fica, o ditado volta.
	esperar(t, fazer(t, h, "PATCH", "/api/notas/"+n2.ID, map[string]any{"base_version": n2.Version, "corpo": "dois, mexido"}), 200)
	w = fazer(t, h, "POST", "/api/notas/"+n2.ID+"/desfazer-virar", nil)
	esperar(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || r.DitadoID != "d2" || r.NotaApagada {
		t.Errorf("resposta = %s (%v)", w.Body.String(), err)
	}
	esperar(t, fazer(t, h, "GET", "/api/notas/"+n2.ID, nil), 200)
	if ids := idsDoHistorico(t, h, ""); len(ids) != 2 {
		t.Errorf("o histórico = %v; quero d1 e d2", ids)
	}

	// Repetido, ou de nota que não veio de ditado: 409. Nota inexistente: 404.
	esperarErro(t, fazer(t, h, "POST", "/api/notas/"+n2.ID+"/desfazer-virar", nil), 409, "CONFLICT")
	solta := criarNotaAPI(t, h, map[string]any{"corpo": "solta"})
	esperarErro(t, fazer(t, h, "POST", "/api/notas/"+solta.ID+"/desfazer-virar", nil), 409, "CONFLICT")
	esperarErro(t, fazer(t, h, "POST", "/api/notas/nao-existe/desfazer-virar", nil), 404, "NOT_FOUND")
}

func TestVirarNotaAPI_SoDoDono(t *testing.T) {
	h, db, _ := servidorDeVirarNota(t)
	if _, err := db.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
		asr_provider, status, created_at, updated_at) VALUES ('alheio', 'outro', 'windows', 'b', 'segredo', 'smart', 'local', 'done',
		'2026-10-25T12:00:00.000000000Z', '2026-10-25T12:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	esperarErro(t, fazer(t, h, "POST", "/api/history/alheio/virar-nota", nil), 404, "NOT_FOUND")
}
