package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A cópia do envio inicial segura a conexão única do Agent (SetMaxOpenConns(1))
// entre a contagem e o VACUUM INTO, de propósito: nenhuma escrita passa no
// meio, e o Put do ditado que chegue nesse tempo ESPERA, em vez de falhar com
// SQLITE_BUSY numa segunda conexão. Este teste mede essa espera sobre 7 mil
// ditados de tamanho comum (o mesmo histórico do espera_do_ditado_test), e
// prova que o Put esperou: a cópia tem os 7 mil, sem o ditado novo, e confere.
// O teto é largo (guarda de regressão, não benchmark); os números medidos
// estão na ADR-019 ("Decisões da F7c").
func TestEspera_PutDuranteACopiaDoEnvio(t *testing.T) {
	if testing.Short() {
		t.Skip("mede tempo")
	}
	ctx := context.Background()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	db, err := sql.Open("sqlite", caminho)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	const dono = "0199a000-0000-7000-8000-00000000d0d0"
	const n = 7000
	agora := time.Now().UTC()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	texto := strings.Repeat("palavra ", 160) // 1,25 KiB, um ditado de uns 40 s
	for i := 0; i < n; i++ {
		criado := agora.Add(-time.Duration(i) * time.Minute).Format(formatoTempo)
		if _, err := tx.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
			asr_provider, status, created_at, updated_at, version, sync_state)
			VALUES (?, ?, 'windows', ?, ?, 'smart', 'local', 'done', ?, ?, 1, 'pending')`,
			fmt.Sprintf("0199b000-0000-7000-8000-%012d", i), dono, texto, texto, criado, criado); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	repo := NewSQLiteTranscriptionRepository(db)
	fila := repo.Fila(dono)

	segurou := make(chan struct{})
	ganchoDaCopia = func() { close(segurou) }
	t.Cleanup(func() { ganchoDaCopia = nil })
	espera := make(chan time.Duration, 1)
	falhou := make(chan error, 1)
	const novo = "0199b000-0000-7000-8000-ffffffffffff"
	go func() {
		<-segurou
		t0 := time.Now()
		err := repo.Put(Transcription{ID: novo, UserID: dono, SourcePlatform: "windows", RawText: "r", FinalText: "f",
			Mode: "smart", ASRProvider: "local", Status: "done", CreatedAt: agora, UpdatedAt: agora, Version: 1,
			SyncState: SituacaoNormal})
		espera <- time.Since(t0)
		falhou <- err
	}()
	inicio := time.Now()
	r, err := fila.EnviarHistorico(ctx, caminho, agora)
	envio := time.Since(inicio)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-segurou:
	default:
		// Sem isto, um envio que pulasse a cópia deixaria o teste parado até
		// o prazo do go test, em vez de dizer o que faltou.
		t.Fatal("o envio marcou sem passar pela cópia (o gancho não rodou)")
	}
	put := <-espera
	if err := <-falhou; err != nil {
		t.Fatalf("o Put durante a cópia falhou (em vez de esperar): %v", err)
	}
	t.Logf("o envio de %d ditados (a cópia e %d blocos) levou %v; o Put esperou %v",
		n, (n+BlocoDoEnvio-1)/BlocoDoEnvio, envio, put)
	if r.Marcados != n || r.Copia == "" {
		t.Fatalf("resultado: %+v", r)
	}
	if put > 5*time.Second {
		t.Errorf("o Put esperou %v durante a cópia", put)
	}
	if _, err := repo.Get(ctx, dono, novo); err != nil {
		t.Fatalf("o ditado novo não ficou: %v", err)
	}
	c, err := AbrirSomenteLeitura(r.Copia)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var naCopia int
	if err := c.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&naCopia); err != nil || naCopia != n {
		t.Errorf("a cópia tem %d ditados (%v); quero %d (o Put esperou a cópia)", naCopia, err, n)
	}
}
