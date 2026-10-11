package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// O filtro do envio inicial vai de novo no INSERT de cada bloco (marcar, em
// envio_sqlite.go): a leitura dos elegíveis e os blocos são transações
// separadas (o ditado não espera o histórico inteiro), e o ditado que mudou
// nesse meio-tempo não pode ganhar o marcador. Aqui, entre a leitura e o
// bloco: um vai para a lixeira, um sobe pela fila (ganha a versão da nuvem),
// um é recusado e um vira de outro PC. Só o que continua elegível entra.
func TestMarcar_FiltroDeNovoNoInsert(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	const dono = "0199a000-0000-7000-8000-00000000d0d0"
	agora := time.Now().UTC()
	repo := NewSQLiteTranscriptionRepository(db)
	ids := make([]string, 5)
	for i := range ids {
		ids[i] = fmt.Sprintf("0199b000-0000-7000-8000-%012d", i)
		criado := agora.Add(-time.Duration(i+1) * time.Hour)
		// Desconectado: o Put do histórico não põe marcador.
		if err := repo.Put(Transcription{ID: ids[i], UserID: dono, SourcePlatform: "windows", RawText: "r", FinalText: "f",
			Mode: "smart", ASRProvider: "local", Status: "done", CreatedAt: criado, UpdatedAt: criado, Version: 1,
			SyncState: SituacaoNormal}); err != nil {
			t.Fatal(err)
		}
	}
	f := repo.Fila(dono)
	lidos, err := f.elegiveis(ctx, db, agora)
	if err != nil || len(lidos) != 5 {
		t.Fatalf("os elegíveis: %v, %v", lidos, err)
	}

	// Entre a leitura e o bloco.
	if err := repo.SoftDelete(ctx, dono, ids[1], agora); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		coluna string
		valor  any
		id     string
	}{
		{"server_version", 1, ids[2]},
		{"sync_state", SituacaoRecusado, ids[3]},
		{"device_id", "0199a000-0000-7000-8000-0000000000c2", ids[4]},
	} {
		if _, err := db.Exec(`UPDATE local_transcriptions SET `+m.coluna+` = ? WHERE id = ?`, m.valor, m.id); err != nil {
			t.Fatal(err)
		}
	}

	n, err := f.marcar(ctx, lidos, true, "", agora)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := f.Marcadores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(ms) != 1 || ms[0].EntityID != ids[0] || !ms[0].Inicial {
		t.Errorf("marcou %d: %+v; quero só o primeiro, do envio inicial", n, ms)
	}
	if _, total, err := f.Progresso(ctx); err != nil || total != 1 {
		t.Errorf("o total do progresso: %d (%v); quero 1, o que entrou", total, err)
	}
}
