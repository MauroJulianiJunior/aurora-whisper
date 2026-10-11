package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A 0006 passa pelo historico.db de verdade do Mauro (o do bin\, e depois o
// do Agent instalado): ela não pode perder nem mudar nada. A prova é a da
// 0005: um banco num arquivo, migrado até a 0005 (como o de quem tem a F7a/b
// hoje), com o que um banco conectado tem (ditados ativos e na lixeira, um com
// selo, um de outro PC com a versão da nuvem, marcadores na fila, um deles em
// voo, o cursor, a instalação com a nuvem e as preferências), lido tabela por
// tabela antes e depois.

func TestMigracao0006_NaoPerdeNemMudaNada(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	migrarAte(t, db, "0005_sync_fila")

	const dono = "0199a000-0000-7000-8000-000000000001"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	base := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		criado := base.Add(time.Duration(i) * time.Hour).Format(formatoTempo)
		var apagado, purga, servidor, aparelho, selo, seloEm any
		if i%3 == 2 {
			apagado = base.Add(time.Duration(i)*time.Hour + time.Minute).Format(formatoTempo)
			purga = base.AddDate(0, 0, 30).Format(formatoTempo)
		}
		if i >= 5 {
			servidor = int64(i)
		}
		if i == 6 {
			aparelho = "0199a000-0000-7000-8000-0000000000ff"
		}
		if i == 7 {
			selo, seloEm = "a outra versão, com acento: ação", base.Format(formatoTempo)
		}
		exec(`INSERT INTO local_transcriptions (id, user_id, device_id, source_platform, raw_text, final_text,
			mode, language, asr_provider, asr_model, duration_ms, processing_ms, confidence, target_app, status,
			created_at, updated_at, deleted_at, purge_at, version, sync_state, server_version, last_sync_error,
			conflito_texto, conflito_device_id, conflito_em)
			VALUES (?, ?, ?, 'windows', ?, ?, 'smart', 'pt', 'whisper-local', 'small-q4_0', 3200, 410, 0.5, NULL,
			'done', ?, ?, ?, ?, ?, 'pending', ?, NULL, ?, NULL, ?)`,
			fmt.Sprintf("0199a000-0000-7000-8000-00000000010%d", i), dono, aparelho,
			fmt.Sprintf("bruto %d com acento: ação", i), fmt.Sprintf("final %d com acento: ação", i),
			criado, criado, apagado, purga, i+1, servidor, selo, seloEm)
	}
	exec(`INSERT INTO local_settings (id, hotkey, hold_threshold_ms, overlay_position, asr_policy,
		protected_field_policy, local_port, updated_at, text_mode, ai_provider, ai_model, microphone_always_ready)
		VALUES (1, 'ctrl+win', 300, 'bottom', 'local_only', 'block', 0, 'x', 'polish', 'anthropic', 'claude', 1)`)
	exec(`INSERT INTO instalacao (nome, valor) VALUES ('dono_local', ?), ('dono_local_migrado_em', 'x'),
		('install_id', 'i1'), ('nuvem_url', 'http://localhost:8080'), ('nuvem_aparelho', 'a1'),
		('nuvem_conectado_em', '2026-10-06T12:00:00Z')`, dono)
	exec(`INSERT INTO sync_state (user_id, device_id, last_server_seq, last_success_at, sync_epoch, pareado_em, ultimo_erro)
		VALUES (?, 'a1', 7, 'x', 'e1', '2026-10-06T12:00:00Z', NULL)`, dono)
	exec(`INSERT INTO sync_outbox (entity_type, entity_id, campos, mutation_id, versao_enviada, criado_em)
		VALUES ('transcription', '0199a000-0000-7000-8000-000000000105', 1, 'm1', 6, ?),
		       ('transcription', '0199a000-0000-7000-8000-000000000107', 3, NULL, NULL, ?)`,
		base.Format(formatoTempo), base.Format(formatoTempo))

	tabelas := []string{"local_transcriptions", "local_settings", "instalacao", "sync_state", "sync_outbox",
		"sync_outbox_0001", "local_dictionary_entries", "local_corrections"}
	antes := map[string][]map[string]any{}
	for _, tb := range tabelas {
		antes[tb] = linhasDe(t, db, tb)
	}
	if len(antes["local_transcriptions"]) != 8 || len(antes["sync_outbox"]) != 2 {
		t.Fatalf("semeadura: %d ditados, %d marcadores", len(antes["local_transcriptions"]), len(antes["sync_outbox"]))
	}

	// O Agent novo sobe: o Migrator aplica só a 0006.
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	novas := map[string]map[string]any{"sync_outbox": {"inicial": int64(0), "selo_escolha": nil, "selo_em": nil}}
	for _, tb := range tabelas {
		depois := linhasDe(t, db, tb)
		if len(depois) != len(antes[tb]) {
			t.Fatalf("%s: %d linhas antes, %d depois", tb, len(antes[tb]), len(depois))
		}
		for i := range depois {
			for c, quero := range novas[tb] {
				if v, ok := depois[i][c]; !ok || !reflect.DeepEqual(v, quero) {
					t.Errorf("%s linha %d: a coluna nova %s = %v (presente %v); quero %v", tb, i, c, v, ok, quero)
				}
				delete(depois[i], c)
			}
			if !reflect.DeepEqual(depois[i], antes[tb][i]) {
				t.Errorf("%s linha %d mudou:\nantes  %v\ndepois %v", tb, i, antes[tb][i], depois[i])
			}
		}
	}

	// O repositório e a fila leem o banco migrado: os ditados, o selo e os
	// marcadores (o em voo continua em voo, nenhum é do envio inicial).
	repo := NewSQLiteTranscriptionRepository(db)
	selado, err := repo.Get(ctx, dono, "0199a000-0000-7000-8000-000000000107")
	if err != nil || selado.Conflito == nil || selado.Conflito.FinalText != "a outra versão, com acento: ação" {
		t.Fatalf("o selo depois da 0006: %+v, %v", selado.Conflito, err)
	}
	ms, err := repo.Fila(dono).Marcadores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || !ms[0].EmVoo() || ms[0].VersaoEnviada != 6 || ms[1].Campos != 3 {
		t.Fatalf("os marcadores depois da 0006: %+v", ms)
	}
	for _, m := range ms {
		if m.Inicial || m.SeloEscolha != "" || !m.SeloEm.IsZero() {
			t.Errorf("marcador com o envio inicial ou a escolha: %+v", m)
		}
	}

	// A tabela nova do esquecer nasce vazia.
	var anotados int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_mudou_aqui`).Scan(&anotados); err != nil || anotados != 0 {
		t.Errorf("a sync_mudou_aqui depois da 0006: %d linhas, %v", anotados, err)
	}

	// A coluna da escolha só aceita as duas escolhas, e a inicial só 0 e 1.
	if _, err := db.Exec(`UPDATE sync_outbox SET selo_escolha = 'outra'`); err == nil {
		t.Error("a escolha aceitou um valor fora da lista")
	}
	if _, err := db.Exec(`UPDATE sync_outbox SET inicial = 2`); err == nil {
		t.Error("inicial aceitou 2")
	}

	// Migrar de novo não faz nada (e não falha).
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate de novo: %v", err)
	}
}

// A 0006 é uma transação: se ela falha no meio, nada dela fica. A falha é
// real: uma coluna selo_em que já existe faz o terceiro ALTER falhar, depois
// de os dois primeiros terem rodado.
func TestMigracao0006_FalhaNoMeioNaoDeixaNada(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	migrarAte(t, db, "0005_sync_fila")
	if _, err := db.Exec(`ALTER TABLE sync_outbox ADD COLUMN selo_em TEXT`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('transcription', 't1', 2, 'x')`); err != nil {
		t.Fatal(err)
	}
	if err := NewMigrator().Migrate(ctx, db); err == nil {
		t.Fatal("a 0006 passou com a coluna ocupada")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = '0006_sync_envio_inicial'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a 0006 ficou registrada: %d, %v", n, err)
	}
	if _, err := db.Exec(`SELECT inicial FROM sync_outbox`); err == nil {
		t.Error("a coluna inicial da 0006 ficou")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'sync_mudou_aqui'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a tabela sync_mudou_aqui da 0006 ficou: %d, %v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_outbox WHERE campos = 2`).Scan(&n); err != nil || n != 1 {
		t.Errorf("o marcador de antes não está intacto: %d, %v", n, err)
	}
}
