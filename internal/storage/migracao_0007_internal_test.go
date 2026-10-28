package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A 0007 passa pelo historico.db de verdade do Mauro (o do bin\, conectado e
// com o histórico enviado): ela não pode perder nem mudar nada. A prova é a
// da 0006: um banco num arquivo, migrado até a 0006 (como o de quem tem a
// F7c hoje), com o que um banco conectado tem (ditados ativos e na lixeira,
// um com selo, um de outro PC, um recusado, as versões da nuvem, marcadores
// na fila, um em voo, um do envio inicial e um com a escolha do selo, o
// anotado do esquecer, o cursor, a instalação com a nuvem e o aceite, e as
// preferências), lido tabela por tabela antes e depois. As tabelas são todas
// as do banco, lidas do sqlite_master, e não uma lista: uma tabela esquecida
// na lista passaria calada.

// bancoNa0006 abre um banco num arquivo, migrado até a 0006, com dados.
func bancoNa0006(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "historico.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	migrarAte(t, db, "0006_sync_envio_inicial")

	const dono = "0199a000-0000-7000-8000-000000000001"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 9; i++ {
		criado := base.Add(time.Duration(i) * time.Hour).Format(formatoTempo)
		var apagado, purga, servidor, aparelho, selo, seloEm, erro any
		situacao := SituacaoNormal
		if i%3 == 2 {
			apagado = base.Add(time.Duration(i)*time.Hour + time.Minute).Format(formatoTempo)
			purga = base.AddDate(0, 0, 30).Format(formatoTempo)
		}
		if i >= 4 {
			servidor = int64(10 + i)
		}
		if i == 6 {
			aparelho = "0199a000-0000-7000-8000-0000000000ff"
		}
		if i == 7 {
			selo, seloEm = "a outra versão, com acento: ação", base.Format(formatoTempo)
		}
		if i == 3 {
			situacao, erro = SituacaoRecusado, "texto_grande"
		}
		exec(`INSERT INTO local_transcriptions (id, user_id, device_id, source_platform, raw_text, final_text,
			mode, language, asr_provider, asr_model, duration_ms, processing_ms, confidence, target_app, status,
			created_at, updated_at, deleted_at, purge_at, version, sync_state, server_version, last_sync_error,
			conflito_texto, conflito_device_id, conflito_em)
			VALUES (?, ?, ?, 'windows', ?, ?, 'smart', 'pt', 'whisper-local', 'small-q4_0', 3200, 410, 0.5, NULL,
			'done', ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
			fmt.Sprintf("0199a000-0000-7000-8000-00000000010%d", i), dono, aparelho,
			fmt.Sprintf("bruto %d com acento: ação", i), fmt.Sprintf("final %d com acento: ação", i),
			criado, criado, apagado, purga, i+1, situacao, servidor, erro, selo, seloEm)
	}
	exec(`INSERT INTO local_settings (id, hotkey, hold_threshold_ms, overlay_position, asr_policy,
		protected_field_policy, local_port, updated_at, text_mode, ai_provider, ai_model, microphone_always_ready)
		VALUES (1, 'ctrl+win', 300, 'bottom', 'local_only', 'block', 0, 'x', 'polish', 'anthropic', 'claude', 1)`)
	exec(`INSERT INTO instalacao (nome, valor) VALUES ('dono_local', ?), ('dono_local_migrado_em', 'x'),
		('install_id', 'i1'), ('nuvem_url', 'http://localhost:8080'), ('nuvem_conta', 'c1'), ('nuvem_aparelho', 'a1'),
		('nuvem_conectado_em', '2026-10-06T12:00:00Z'), ('nuvem_envio_aceito_em', '2026-10-06T12:05:00Z'),
		('nuvem_envio_total', '3'), ('nuvem_copia', 'historico.db.antes-da-nuvem-20261006-120500.db'),
		('nuvem_esquecida_em', '2026-10-05T12:00:00.000000000Z')`, dono)
	exec(`INSERT INTO sync_state (user_id, device_id, last_server_seq, last_success_at, sync_epoch, pareado_em, ultimo_erro)
		VALUES (?, 'a1', 1234, 'x', 'e1', '2026-10-06T12:00:00Z', NULL)`, dono)
	exec(`INSERT INTO sync_outbox (entity_type, entity_id, campos, mutation_id, versao_enviada, criado_em, inicial, selo_escolha, selo_em)
		VALUES ('transcription', '0199a000-0000-7000-8000-000000000105', 1, 'm1', 6, ?, 0, NULL, NULL),
		       ('transcription', '0199a000-0000-7000-8000-000000000100', 0, NULL, NULL, ?, 1, NULL, NULL),
		       ('transcription', '0199a000-0000-7000-8000-000000000107', 1, NULL, NULL, ?, 0, 'trocar', ?)`,
		base.Format(formatoTempo), base.Format(formatoTempo), base.Format(formatoTempo), base.Format(formatoTempo))
	exec(`INSERT INTO sync_mudou_aqui (entity_type, entity_id) VALUES ('transcription', '0199a000-0000-7000-8000-000000000101')`)
	exec(`INSERT INTO local_dictionary_entries (id, user_id, canonical_form, normalized_form, created_at, updated_at)
		VALUES ('e1', ?, 'Supabase', 'supabase', 'x', 'x')`, dono)
	return db
}

// todasAsTabelas lê todas as tabelas de dados do banco, cada uma inteira.
func todasAsTabelas(t *testing.T, db *sql.DB) map[string][]map[string]any {
	t.Helper()
	nomes, err := tabelasDoBanco(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]map[string]any{}
	for _, n := range nomes {
		out[n] = linhasDe(t, db, n)
	}
	return out
}

func TestMigracao0007_NaoPerdeNemMudaNada(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0006(t)
	antes := todasAsTabelas(t, db)
	if len(antes["local_transcriptions"]) != 9 || len(antes["sync_outbox"]) != 3 || len(antes["sync_state"]) != 1 ||
		len(antes["sync_mudou_aqui"]) != 1 {
		t.Fatalf("semeadura: %d ditados, %d marcadores, %d cursores, %d anotados", len(antes["local_transcriptions"]),
			len(antes["sync_outbox"]), len(antes["sync_state"]), len(antes["sync_mudou_aqui"]))
	}
	if _, ok := antes["sync_aprendido"]; ok {
		t.Fatal("a semeadura já tem a tabela da 0007")
	}

	// O Agent novo sobe: o Migrator aplica a 0007, e as que vêm depois dela
	// (a 0008 só põe colunas na sync_aprendido, que nasce aqui).
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var aplicadas []string
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		aplicadas = append(aplicadas, v)
	}
	rows.Close()
	if len(aplicadas) < 7 || aplicadas[6] != "0007_sync_aprendido" {
		t.Fatalf("as migrations aplicadas: %v", aplicadas)
	}

	depois := todasAsTabelas(t, db)
	// Tabela por tabela, linha por linha: as que existiam estão iguais (a
	// 0007 não põe coluna em nenhuma, então nem a coluna nova é tirada aqui;
	// a schema_migrations só ganha as linhas da 0007 e das seguintes).
	var nomes []string
	for n := range antes {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	for _, tb := range nomes {
		a, d := antes[tb], depois[tb]
		if tb == "schema_migrations" && len(d) >= len(a) {
			d = d[:len(a)]
		}
		if !reflect.DeepEqual(a, d) {
			t.Errorf("%s mudou:\nantes  %v\ndepois %v", tb, a, d)
		}
	}
	// Só nascem as duas tabelas da 0007, vazias (e as três das notas, da 0009, e a
	// das imagens das notas, da 0014, que este teste também aplica: o Migrate roda
	// até a última).
	var novas []string
	for n, linhas := range depois {
		if _, ok := antes[n]; !ok {
			novas = append(novas, n)
			if len(linhas) != 0 {
				t.Errorf("a tabela nova %s nasceu com %d linhas", n, len(linhas))
			}
		}
	}
	sort.Strings(novas)
	if !reflect.DeepEqual(novas, []string{"local_labels", "local_note_images", "local_note_labels", "local_notes", "sync_aprendido", "sync_veio_da_nuvem"}) {
		t.Errorf("as tabelas novas: %v", novas)
	}

	// O cursor e as versões da nuvem dos ditados, conferidos à parte: a 0007
	// NÃO volta o cursor (escolha menor 4).
	var cursor int64
	var epoch string
	if err := db.QueryRow(`SELECT last_server_seq, sync_epoch FROM sync_state`).Scan(&cursor, &epoch); err != nil ||
		cursor != 1234 || epoch != "e1" {
		t.Errorf("o cursor depois da 0007: %d, %q, %v", cursor, epoch, err)
	}
	versoes := map[string]any{}
	for _, l := range depois["local_transcriptions"] {
		versoes[l["id"].(string)] = l["server_version"]
	}
	for _, l := range antes["local_transcriptions"] {
		if !reflect.DeepEqual(versoes[l["id"].(string)], l["server_version"]) {
			t.Errorf("a versão da nuvem do ditado %v mudou: %v -> %v", l["id"], l["server_version"], versoes[l["id"].(string)])
		}
	}

	// O repositório e a fila leem o banco migrado: o selo, os marcadores (o
	// em voo continua em voo, o do envio inicial continua inicial), o
	// progresso e o espelho, vazio.
	const dono = "0199a000-0000-7000-8000-000000000001"
	repo := NewSQLiteTranscriptionRepository(db)
	selado, err := repo.Get(ctx, dono, "0199a000-0000-7000-8000-000000000107")
	if err != nil || selado.Conflito == nil || selado.Conflito.FinalText != "a outra versão, com acento: ação" {
		t.Fatalf("o selo depois da 0007: %+v, %v", selado.Conflito, err)
	}
	fila := repo.Fila(dono)
	ms, err := fila.Marcadores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 || !ms[0].EmVoo() || ms[0].VersaoEnviada != 6 || ms[1].SeloEscolha != EscolhaTrocar || !ms[2].Inicial {
		t.Fatalf("os marcadores depois da 0007: %+v", ms)
	}
	if feitos, total, err := fila.Progresso(ctx); err != nil || feitos != 2 || total != 3 {
		t.Errorf("o progresso depois da 0007: %d de %d, %v", feitos, total, err)
	}
	espelho, err := fila.Espelho(ctx)
	if err != nil || len(espelho) != 0 {
		t.Errorf("o espelho depois da 0007: %v, %v", espelho, err)
	}

	// Migrar de novo não faz nada (e não falha).
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate de novo: %v", err)
	}
	if !reflect.DeepEqual(todasAsTabelas(t, db), depois) {
		t.Error("migrar de novo mudou o banco")
	}
}

// A 0007 é uma transação: se ela falha no meio, nada dela fica. A falha é
// real: uma tabela sync_veio_da_nuvem que já existe faz o segundo CREATE
// falhar, depois de o primeiro ter rodado.
func TestMigracao0007_FalhaNoMeioNaoDeixaNada(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0006(t)
	if _, err := db.Exec(`CREATE TABLE sync_veio_da_nuvem (x TEXT)`); err != nil {
		t.Fatal(err)
	}
	antes := todasAsTabelas(t, db)
	if err := NewMigrator().Migrate(ctx, db); err == nil {
		t.Fatal("a 0007 passou com a tabela ocupada")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = '0007_sync_aprendido'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a 0007 ficou registrada: %d, %v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'sync_aprendido'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a tabela sync_aprendido da 0007 ficou: %d, %v", n, err)
	}
	if !reflect.DeepEqual(todasAsTabelas(t, db), antes) {
		t.Error("a 0007 que falhou mudou o banco")
	}
}
