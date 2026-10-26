package localapi_test

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/notafoco"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O aviso "o cursor está dentro de uma nota" (ADR-022, "Ditado feito dentro da
// nota"): PUT e DELETE /api/notas/foco, o token no POST /api/notas e o descartar.
// O guard envolve o mux inteiro em produção, então aqui, como nas outras rotas,
// só a lógica da rota.

type focoDeTeste struct {
	h    http.Handler
	db   *sql.DB
	hist *storage.SQLiteTranscriptionRepository
	foco *notafoco.Marcador
}

func novoFocoDeTeste(t *testing.T) focoDeTeste {
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
	foco := notafoco.Novo(nil)
	pref := prefsDeTeste()
	pref.Notas = storage.NewSQLiteNotaRepository(db)
	pref.Foco = foco
	srv := localapi.New("dono-1", hist, storage.NewInMemorySettingsRepository(), pref)
	return focoDeTeste{h: srv.Handler(), db: db, hist: hist, foco: foco}
}

func (f focoDeTeste) paraOHistorico() string {
	if p := f.foco.ParaOHistorico(); p != nil {
		return *p
	}
	return ""
}

func (f focoDeTeste) criarNota(t *testing.T, corpo string) notaJSON {
	t.Helper()
	w := fazer(t, f.h, "POST", "/api/notas", map[string]any{"corpo": corpo})
	esperar(t, w, 201)
	return lerNotaJSON(t, w)
}

// ditadoEscondido grava, como o Runtime, um ditado já marcado.
func (f focoDeTeste) ditadoEscondido(t *testing.T, id, valor string) {
	t.Helper()
	agora := time.Now().UTC()
	err := f.hist.Put(storage.Transcription{
		ID: id, UserID: "dono-1", SourcePlatform: "windows", RawText: "bruto " + id, FinalText: "final " + id,
		Mode: "smart", ASRProvider: "local", Status: "done", CreatedAt: agora, UpdatedAt: agora, Version: 1,
		SyncState: "pending", VirouNota: &valor,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFoco_PutDaNotaEDoToken(t *testing.T) {
	f := novoFocoDeTeste(t)
	n := f.criarNota(t, "uma nota")

	esperar(t, fazer(t, f.h, "PUT", "/api/notas/foco", map[string]string{"nota": n.ID}), 204)
	if got := f.paraOHistorico(); got != n.ID {
		t.Errorf("aviso = %q; quero %s", got, n.ID)
	}
	esperar(t, fazer(t, f.h, "PUT", "/api/notas/foco", map[string]string{"token": "0b6f1c2e-1111"}), 204)
	if got := f.paraOHistorico(); got != "nova:0b6f1c2e-1111" {
		t.Errorf("aviso = %q", got)
	}
}

func TestFoco_PutRecusas(t *testing.T) {
	f := novoFocoDeTeste(t)
	n := f.criarNota(t, "uma nota")
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/"+n.ID, nil), 204) // lixeira

	casos := []struct {
		nome   string
		corpo  any
		status int
		code   string
	}{
		{"vazio", map[string]string{}, 400, "BAD_REQUEST"},
		{"nota e token", map[string]string{"nota": "a", "token": "b"}, 400, "BAD_REQUEST"},
		{"token ruim", map[string]string{"token": "a:b"}, 400, "BAD_REQUEST"},
		{"campo desconhecido", map[string]string{"notas": "a"}, 400, "BAD_REQUEST"},
		{"nota que não existe", map[string]string{"nota": "nao-existe"}, 404, "NOT_FOUND"},
		{"nota na lixeira", map[string]string{"nota": n.ID}, 409, "CONFLICT"},
		{"descartar no PUT", map[string]any{"token": "tk", "descartar": true}, 400, "BAD_REQUEST"},
	}
	for _, c := range casos {
		esperarErro(t, fazer(t, f.h, "PUT", "/api/notas/foco", c.corpo), c.status, c.code)
		if got := f.paraOHistorico(); got != "" {
			t.Errorf("%s: deixou aviso %q", c.nome, got)
		}
	}
}

func TestFoco_Indisponivel503(t *testing.T) {
	// Sem o marcador.
	f := novoFocoDeTeste(t)
	pref := prefsDeTeste()
	pref.Notas = storage.NewSQLiteNotaRepository(f.db)
	srv := localapi.New("dono-1", f.hist, storage.NewInMemorySettingsRepository(), pref)
	esperarErro(t, fazer(t, srv.Handler(), "PUT", "/api/notas/foco", map[string]string{"token": "tk"}), 503, "UNAVAILABLE")
	esperarErro(t, fazer(t, srv.Handler(), "DELETE", "/api/notas/foco", nil), 503, "UNAVAILABLE")
	// Sem as notas.
	pref = prefsDeTeste()
	pref.Foco = notafoco.Novo(nil)
	srv = localapi.New("dono-1", f.hist, storage.NewInMemorySettingsRepository(), pref)
	esperarErro(t, fazer(t, srv.Handler(), "PUT", "/api/notas/foco", map[string]string{"token": "tk"}), 503, "UNAVAILABLE")
}

func TestFoco_DeleteSemCorpoLimpaQualquerAviso(t *testing.T) {
	f := novoFocoDeTeste(t)
	f.foco.DefinirNova("tk")
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/foco", nil), 204)
	if got := f.paraOHistorico(); got != "" {
		t.Errorf("aviso = %q", got)
	}
	// Sem aviso nenhum também é 204.
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/foco", nil), 204)
}

func TestFoco_DeleteComCorpoSoLimpaOMesmo(t *testing.T) {
	f := novoFocoDeTeste(t)
	a, b := f.criarNota(t, "a"), f.criarNota(t, "b")
	esperar(t, fazer(t, f.h, "PUT", "/api/notas/foco", map[string]string{"nota": b.ID}), 204)

	// O DELETE atrasado da nota a não apaga o aviso da nota b.
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]string{"nota": a.ID}), 204)
	if got := f.paraOHistorico(); got != b.ID {
		t.Errorf("o DELETE de a apagou o aviso de b: %q", got)
	}
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]string{"nota": b.ID}), 204)
	if got := f.paraOHistorico(); got != "" {
		t.Errorf("aviso = %q", got)
	}

	esperar(t, fazer(t, f.h, "PUT", "/api/notas/foco", map[string]string{"token": "tk-1"}), 204)
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]string{"token": "tk-2"}), 204)
	if got := f.paraOHistorico(); got != "nova:tk-1" {
		t.Errorf("aviso = %q", got)
	}
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]string{"token": "tk-1"}), 204)
	if got := f.paraOHistorico(); got != "" {
		t.Errorf("aviso = %q", got)
	}
}

func TestFoco_DeleteRecusas(t *testing.T) {
	f := novoFocoDeTeste(t)
	esperarErro(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]string{"nota": "a", "token": "b"}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]any{"nota": "a", "descartar": true}), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, f.h, "DELETE", "/api/notas/foco", "{não é json"), 400, "BAD_REQUEST")
	esperarErro(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]any{"token": "a:b", "descartar": true}), 400, "BAD_REQUEST")
}

// A rota nova não rouba a de DELETE /api/notas/{id}: "foco" não é id de nota.
func TestFoco_NaoConfundeComAsRotasDeNotaPorId(t *testing.T) {
	f := novoFocoDeTeste(t)
	n := f.criarNota(t, "uma nota")
	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/"+n.ID, nil), 204)
	esperar(t, fazer(t, f.h, "GET", "/api/notas/"+n.ID, nil), 200)
}

func TestFoco_CriarComTokenLigaOsDitadosDaCaixa(t *testing.T) {
	f := novoFocoDeTeste(t)
	f.ditadoEscondido(t, "d1", "nova:tk-1")
	f.ditadoEscondido(t, "d2", "nova:tk-1")
	f.ditadoEscondido(t, "d3", "nova:tk-2")

	w := fazer(t, f.h, "POST", "/api/notas", map[string]any{"corpo": "- leite", "token": "tk-1"})
	esperar(t, w, 201)
	n := lerNotaJSON(t, w)
	if n.DitadoDeOrigem != nil {
		t.Errorf("a nota ganhou ditado_de_origem %q", *n.DitadoDeOrigem)
	}
	var d1, d3 string
	if err := f.db.QueryRow(`SELECT virou_nota FROM local_transcriptions WHERE id='d1'`).Scan(&d1); err != nil || d1 != n.ID {
		t.Errorf("d1: %q, %v; quero %s", d1, err, n.ID)
	}
	if err := f.db.QueryRow(`SELECT virou_nota FROM local_transcriptions WHERE id='d3'`).Scan(&d3); err != nil || d3 != "nova:tk-2" {
		t.Errorf("d3: %q, %v", d3, err)
	}
	// Nenhum dos dois aparece no histórico.
	if ids := idsDoHistorico(t, f.h, ""); len(ids) != 0 {
		t.Errorf("o histórico = %v", ids)
	}
	// Token ruim no POST: 400, sem nota.
	esperarErro(t, fazer(t, f.h, "POST", "/api/notas", map[string]any{"corpo": "x", "token": "a:b"}), 400, "BAD_REQUEST")
}

func TestFoco_DescartarDevolveOsDitadosAoHistorico(t *testing.T) {
	f := novoFocoDeTeste(t)
	f.ditadoEscondido(t, "d1", "nova:tk-1")
	f.ditadoEscondido(t, "d2", "nova:tk-2")
	esperar(t, fazer(t, f.h, "PUT", "/api/notas/foco", map[string]string{"token": "tk-1"}), 204)

	esperar(t, fazer(t, f.h, "DELETE", "/api/notas/foco", map[string]any{"token": "tk-1", "descartar": true}), 204)
	if got := f.paraOHistorico(); got != "" {
		t.Errorf("aviso = %q", got)
	}
	ids := idsDoHistorico(t, f.h, "")
	if len(ids) != 1 || ids[0] != "d1" {
		t.Errorf("o histórico = %v; quero só d1 de volta", ids)
	}
}
