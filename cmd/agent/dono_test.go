package main

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Os testes do dono fixo rodam em bancos TEMPORÁRIOS. Nunca o
// bin\historico.db: é o histórico real do Mauro.

// agoraDaSubida é a hora fixa das subidas destes testes; ela dá nome à
// cópia.
var agoraDaSubida = time.Date(2026, 9, 29, 10, 15, 0, 0, time.Local)

// historicoDeTeste cria um historico.db migrado numa pasta temporária,
// com um ditado por dono dado, todos com "abacaxi" no texto — a palavra
// que o log nunca pode ter.
func historicoDeTeste(t *testing.T, donos ...string) (string, *sql.DB) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // como em abrirHistorico
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSQLiteTranscriptionRepository(db)
	quando := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for i, d := range donos {
		q := quando.Add(time.Duration(i) * time.Minute)
		if err := repo.Put(storage.Transcription{
			ID: storage.NewID(), UserID: d, SourcePlatform: "windows",
			RawText: "o abacaxi ficou na mesa", FinalText: "O abacaxi ficou na mesa.",
			Mode: "smart", ASRProvider: "whisper-local", Status: "done",
			CreatedAt: q, UpdatedAt: q, Version: 1, SyncState: "pending",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return caminho, db
}

// noPainel conta os ditados que o painel mostraria para o dono.
func noPainel(t *testing.T, db *sql.DB, dono string) int {
	t.Helper()
	p, err := storage.NewSQLiteTranscriptionRepository(db).List(context.Background(),
		storage.TranscriptionListFilter{UserID: dono, Limit: storage.MaxTranscriptionListLimit})
	if err != nil {
		t.Fatal(err)
	}
	return len(p.Items)
}

// linhaDoLog devolve a linha do log que contém msg, ou "".
func linhaDoLog(log, msg string) string {
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, msg) {
			return l
		}
	}
	return ""
}

const (
	msgMigrou = "historico: ditados migrados para o dono fixo"
	msgDono   = "historico: dono dos ditados e do painel"
	msgFalhou = "historico: não migrei para o dono fixo"
	msgOutro  = "historico: há linhas de outro dono"
)

// escolher chama escolherDono e falha o teste se ele devolver erro.
func escolher(t *testing.T, log *slog.Logger, db *sql.DB, caminho, doDicionario string, agora time.Time) string {
	t.Helper()
	dono, err := escolherDono(context.Background(), log, db, caminho, doDicionario, agora)
	if err != nil {
		t.Fatalf("escolherDono: %v", err)
	}
	return dono
}

// TestEscolherDono_PrimeiraSubida é a subida do Agent novo sobre o banco
// do Mauro: os ditados de "mauro" e de "local" vão para o dono fixo, o
// painel os mostra, e o log diz o que migrou e onde ficou a cópia — sem
// texto ditado.
func TestEscolherDono_PrimeiraSubida(t *testing.T) {
	caminho, db := historicoDeTeste(t, "mauro", "mauro", "local")
	var buf bytes.Buffer

	dono := escolher(t, slog.New(slog.NewTextHandler(&buf, nil)), db, caminho, "mauro", agoraDaSubida)
	log := buf.String()

	if dono == "mauro" || dono == "local" || len(dono) != 36 {
		t.Fatalf("dono = %q, quer o UUID do dono fixo", dono)
	}
	if n := noPainel(t, db, dono); n != 3 {
		t.Errorf("o painel do dono fixo mostra %d ditado(s), quer os 3 antigos", n)
	}

	migrou := linhaDoLog(log, msgMigrou)
	copia := storage.CaminhoDaCopia(caminho, agoraDaSubida)
	for _, trecho := range []string{"level=INFO", "linhas.local_transcriptions=3", filepath.Base(copia), "dono=" + dono} {
		if !strings.Contains(migrou, trecho) {
			t.Errorf("a linha da migração não tem %q:\n%s", trecho, log)
		}
	}
	if _, err := os.Stat(copia); err != nil {
		t.Errorf("a cópia não está onde o log diz: %v", err)
	}
	if l := linhaDoLog(log, msgDono); !strings.Contains(l, "usuario="+dono) {
		t.Errorf("o log da subida não diz o dono fixo:\n%s", log)
	}
	if strings.Contains(strings.ToLower(log), "abacaxi") {
		t.Errorf("texto ditado no log:\n%s", log)
	}
	if l := linhaDoLog(log, msgOutro); l != "" {
		t.Errorf("avisou linhas de outro dono logo depois de migrar tudo:\n%s", l)
	}
}

// TestEscolherDono_SubidaSeguinte: com o dono gravado, nada migra, nada é
// copiado, e o usuario do dicionário não importa mais — trocar o
// dicionário não esconde o histórico.
func TestEscolherDono_SubidaSeguinte(t *testing.T) {
	caminho, db := historicoDeTeste(t, "mauro")
	primeiro := escolher(t, slog.New(slog.DiscardHandler), db, caminho, "mauro", agoraDaSubida)

	var buf bytes.Buffer
	depois := agoraDaSubida.Add(24 * time.Hour)
	dono := escolher(t, slog.New(slog.NewTextHandler(&buf, nil)), db, caminho, "ana", depois)

	if dono != primeiro {
		t.Errorf("o dono mudou com o dicionário: %q, depois %q", primeiro, dono)
	}
	if linhaDoLog(buf.String(), msgMigrou) != "" {
		t.Errorf("migrou de novo:\n%s", buf.String())
	}
	if _, err := os.Stat(storage.CaminhoDaCopia(caminho, depois)); err == nil {
		t.Error("copiou o banco de novo")
	}
	if n := noPainel(t, db, dono); n != 1 {
		t.Errorf("o painel mostra %d ditado(s), quer 1", n)
	}
	if l := linhaDoLog(buf.String(), msgOutro); l != "" {
		t.Errorf("avisou linhas de outro dono num banco sem nenhuma:\n%s", l)
	}
}

// TestEscolherDono_AvisaLinhasDeOutroDono: depois da migração, um Agent
// anterior ao dono fixo rodou sobre o mesmo banco e gravou um ditado como
// "mauro". A subida seguinte não o migra (o dono já está gravado), mas
// avisa em WARN quantas linhas ficaram fora do painel — sem texto.
func TestEscolherDono_AvisaLinhasDeOutroDono(t *testing.T) {
	caminho, db := historicoDeTeste(t, "mauro", "mauro")
	primeiro := escolher(t, slog.New(slog.DiscardHandler), db, caminho, "mauro", agoraDaSubida)

	// O Agent antigo grava com o usuario do dicionário.
	quando := agoraDaSubida.Add(time.Hour)
	if err := storage.NewSQLiteTranscriptionRepository(db).Put(storage.Transcription{
		ID: storage.NewID(), UserID: "mauro", SourcePlatform: "windows",
		RawText: "o abacaxi da versão antiga", FinalText: "O abacaxi da versão antiga.",
		Mode: "smart", ASRProvider: "whisper-local", Status: "done",
		CreatedAt: quando, UpdatedAt: quando, Version: 1, SyncState: "pending",
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	dono := escolher(t, slog.New(slog.NewTextHandler(&buf, nil)), db, caminho, "mauro", agoraDaSubida.Add(2*time.Hour))
	log := buf.String()

	if dono != primeiro {
		t.Errorf("o dono mudou: %q, depois %q", primeiro, dono)
	}
	aviso := linhaDoLog(log, msgOutro)
	for _, trecho := range []string{"level=WARN", "linhas.local_transcriptions=1"} {
		if !strings.Contains(aviso, trecho) {
			t.Errorf("o aviso das linhas de outro dono não tem %q:\n%s", trecho, log)
		}
	}
	if strings.Contains(aviso, "linhas.sync_state") || strings.Contains(aviso, "linhas.local_corrections") {
		t.Errorf("o aviso lista tabela sem linha de outro dono:\n%s", aviso)
	}
	if strings.Contains(strings.ToLower(log), "abacaxi") {
		t.Errorf("texto ditado no log:\n%s", log)
	}
	if linhaDoLog(log, msgMigrou) != "" {
		t.Errorf("migrou de novo um banco que já tem dono:\n%s", log)
	}
	if n := noPainel(t, db, dono); n != 2 {
		t.Errorf("o painel do dono fixo mostra %d ditado(s), quer os 2 migrados", n)
	}
}

// TestAbrirHistoricoEm_DonoGravadoIlegivelSobeSemHistorico: o banco já
// tem dono, mas ele não se lê (gravado vazio — edição à mão). A regra
// antiga gravaria os ditados desta sessão como "mauro", e eles nunca mais
// apareceriam no painel: a migração não roda sobre um banco com dono. O
// Agent sobe sem histórico, que é a degradação que já existe.
func TestAbrirHistoricoEm_DonoGravadoIlegivelSobeSemHistorico(t *testing.T) {
	caminho, db := historicoDeTeste(t, "mauro")
	if _, err := db.Exec(`INSERT INTO instalacao (nome, valor) VALUES ('dono_local', '')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var buf bytes.Buffer

	hist, _, dono, fechar, err := abrirHistoricoEm(slog.New(slog.NewTextHandler(&buf, nil)), caminho, "mauro", clock.NewFake(agoraDaSubida))
	if err == nil {
		fechar()
		t.Fatalf("subiu com histórico e dono %q sobre um dono gravado ilegível", dono)
	}
	if hist != nil || dono != "" {
		t.Errorf("com erro, devolveu histórico %v e dono %q", hist, dono)
	}
	log := buf.String()
	if l := linhaDoLog(log, msgFalhou); l != "" {
		t.Errorf("disse que a próxima subida tenta de novo, e ela não tenta:\n%s", l)
	}
	if l := linhaDoLog(log, msgDono); l != "" {
		t.Errorf("escolheu um dono assim mesmo:\n%s", l)
	}
	if _, err := os.Stat(storage.CaminhoDaCopia(caminho, agoraDaSubida)); err == nil {
		t.Error("copiou um banco que já tem dono")
	}
}

// TestAbrirHistoricoEm_UsaODonoFixo: o que abrirHistorico entrega ao
// Agent — o repositório e o dono — é o do dono fixo, com os ditados
// antigos migrados.
func TestAbrirHistoricoEm_UsaODonoFixo(t *testing.T) {
	caminho, db := historicoDeTeste(t, "mauro", "local")
	db.Close()

	hist, _, dono, fechar, err := abrirHistoricoEm(slog.New(slog.DiscardHandler), caminho, "mauro", clock.NewFake(agoraDaSubida))
	if err != nil {
		t.Fatalf("abrirHistoricoEm: %v", err)
	}
	defer fechar()
	if dono == "mauro" || dono == "local" || len(dono) != 36 {
		t.Errorf("dono = %q, quer o UUID do dono fixo", dono)
	}
	p, err := hist.List(context.Background(), storage.TranscriptionListFilter{UserID: dono, Limit: storage.MaxTranscriptionListLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 {
		t.Errorf("o histórico do dono devolvido tem %d ditado(s), quer os 2 antigos", len(p.Items))
	}
}

// TestEscolherDono_CopiaQueFalhaCaiNaRegraAntiga: sem cópia, nenhuma linha
// muda, e nesta execução o dono é o da regra antiga — senão o painel
// apareceria vazio. A subida seguinte migra.
func TestEscolherDono_CopiaQueFalhaCaiNaRegraAntiga(t *testing.T) {
	casos := []struct {
		doDicionario, quer string
	}{
		{"mauro", "mauro"},
		{"", "local"},
	}
	for _, c := range casos {
		t.Run("dicionario="+c.doDicionario, func(t *testing.T) {
			caminho, db := historicoDeTeste(t, "mauro", "local")
			// O destino da cópia já existe: o VACUUM INTO recusa, e ela
			// nunca é sobrescrita.
			if err := os.WriteFile(storage.CaminhoDaCopia(caminho, agoraDaSubida), []byte("outra"), 0o644); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer

			dono := escolher(t, slog.New(slog.NewTextHandler(&buf, nil)), db, caminho, c.doDicionario, agoraDaSubida)

			if dono != c.quer {
				t.Errorf("dono = %q, quer %q (regra antiga)", dono, c.quer)
			}
			if l := linhaDoLog(buf.String(), msgFalhou); !strings.Contains(l, "level=WARN") {
				t.Errorf("a falha não saiu em WARN:\n%s", buf.String())
			}
			if n := noPainel(t, db, c.quer); n != 1 {
				t.Errorf("o painel de %q mostra %d ditado(s), quer 1 — as linhas mudaram sem cópia", c.quer, n)
			}

			// A próxima subida tenta de novo, e migra os dois.
			novo := escolher(t, slog.New(slog.DiscardHandler), db, caminho, c.doDicionario, agoraDaSubida.Add(time.Minute))
			if novo == c.quer || noPainel(t, db, novo) != 2 {
				t.Errorf("a subida seguinte não migrou: dono %q, %d ditado(s)", novo, noPainel(t, db, novo))
			}
		})
	}
}

// TestCamposDaMigracao_SemCopia: banco sem linha a migrar não tem cópia,
// e o log diz isso em vez de um caminho vazio.
func TestCamposDaMigracao_SemCopia(t *testing.T) {
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info(msgMigrou,
		camposDaMigracao(storage.DonoFixo{Dono: "d", Migrou: true, Linhas: map[string]int64{"b": 0, "a": 0}})...)
	l := buf.String()
	if !strings.Contains(l, "nenhuma") {
		t.Errorf("sem cópia, o log não diz que não houve cópia: %s", l)
	}
	a, b := strings.Index(l, "linhas.a=0"), strings.Index(l, "linhas.b=0")
	if a < 0 || b < 0 || a > b {
		t.Errorf("as tabelas não saem em ordem de nome: %s", l)
	}
}

// TestAbrirHistoricoEm_CopiaAntesDoExpurgo: num banco de antes do dono
// fixo, a cópia de segurança da migração é tirada ANTES do primeiro
// expurgo da lixeira. Um item cujo prazo venceu sai do banco na subida,
// mas continua na cópia — ligar o expurgo antes do dono apagaria o item
// sem cópia nenhuma.
func TestAbrirHistoricoEm_CopiaAntesDoExpurgo(t *testing.T) {
	ctx := context.Background()
	caminho, db := historicoDeTeste(t, "local")
	repo := storage.NewSQLiteTranscriptionRepository(db)
	quando := agoraDaSubida.AddDate(0, 0, -40)
	if err := repo.Put(storage.Transcription{
		ID: "vencido", UserID: "local", SourcePlatform: "windows",
		RawText: "bruto", FinalText: "final", Mode: "smart", ASRProvider: "whisper-local", Status: "done",
		CreatedAt: quando, UpdatedAt: quando, Version: 1, SyncState: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	// Na lixeira há 30 dias: o prazo é a própria subida.
	if err := repo.SoftDelete(ctx, "local", "vencido", agoraDaSubida.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	hist, _, dono, fechar, err := abrirHistoricoEm(slog.New(slog.DiscardHandler), caminho, "mauro", clock.NewFake(agoraDaSubida))
	if err != nil {
		t.Fatalf("abrirHistoricoEm: %v", err)
	}
	defer fechar()
	if _, err := hist.Get(ctx, dono, "vencido"); err == nil {
		t.Error("o item vencido da lixeira continua no banco depois da subida")
	}

	copia, err := storage.AbrirSomenteLeitura(storage.CaminhoDaCopia(caminho, agoraDaSubida))
	if err != nil {
		t.Fatalf("abrindo a cópia da migração: %v", err)
	}
	defer copia.Close()
	var n int
	if err := copia.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_transcriptions WHERE id = 'vencido'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("a cópia tem %d linha(s) do item vencido, quer 1: o expurgo rodou antes da cópia", n)
	}
}
