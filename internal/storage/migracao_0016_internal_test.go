package storage

import (
	"context"
	"reflect"
	"testing"
)

// A 0016 (a cópia do dicionário para o celular, ADR-023, D3) só põe uma coluna
// em local_settings: dictionary_copy_enabled, desligada (0) em toda linha de
// antes. Nada do dicionário sai do PC sem o clique.

// semAsColunasDa0016 confere e tira, das linhas lidas depois do Migrate (que
// roda até a última), a coluna da 0016: os testes das migrations de antes
// comparam as tabelas com o que eram.
func semAsColunasDa0016(t *testing.T, tabelas map[string][]map[string]any) {
	t.Helper()
	for i, linha := range tabelas["local_settings"] {
		if v, ok := linha["dictionary_copy_enabled"]; !ok || !reflect.DeepEqual(v, int64(0)) {
			t.Errorf("local_settings linha %d: a coluna da 0016 dictionary_copy_enabled = %v (presente %v); quero 0", i, v, ok)
		}
		delete(linha, "dictionary_copy_enabled")
	}
}

func TestMigracao0016_SoGanhaAColunaDesligada(t *testing.T) {
	ctx := context.Background()
	db := bancoNa0007(t)
	for _, v := range []string{"0008_sync_aprendido_perdido", "0009_notas", "0010_ditado_virou_nota",
		"0011_local_settings_gatilho_da_nota", "0012_notas_posicao", "0013_notas_sync", "0014_notas_imagens",
		"0015_notas_imagens_sync"} {
		aplicarSo(t, db, v)
	}
	antes := todasAsTabelas(t, db)
	if len(antes["local_settings"]) != 1 {
		t.Fatalf("semeadura: %d linhas em local_settings", len(antes["local_settings"]))
	}
	if err := NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	depois := todasAsTabelas(t, db)
	if len(depois) != len(antes) {
		t.Fatalf("a 0016 criou ou tirou tabela: %d antes, %d depois", len(antes), len(depois))
	}
	semAsColunasDa0016(t, depois)
	for tb, a := range antes {
		d := depois[tb]
		if tb == "schema_migrations" {
			if len(d) != len(a)+1 || d[len(a)]["version"] != "0016_local_settings_copia_do_dicionario" {
				t.Fatalf("as migrations aplicadas: %v", d)
			}
			d = d[:len(a)]
		}
		if !reflect.DeepEqual(a, d) {
			t.Errorf("%s mudou:\nantes  %v\ndepois %v", tb, a, d)
		}
	}

	// (A linha semeada tem updated_at "x", que o Get não lê.)
	if _, err := db.Exec(`UPDATE local_settings SET updated_at = '2026-11-05T00:00:00.000000000Z'`); err != nil {
		t.Fatal(err)
	}
	// A linha de antes é lida como antes, com a cópia desligada; ligar e ler de
	// volta.
	repo := NewSQLiteSettingsRepository(db)
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DictionaryCopyEnabled {
		t.Fatal("a linha antiga saiu com a cópia do dicionário ligada, quero desligada")
	}
	got.DictionaryCopyEnabled = true
	salvo, err := repo.Put(ctx, got, got.Version)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !salvo.DictionaryCopyEnabled {
		t.Fatal("ligada, voltou desligada")
	}
	salvo.DictionaryCopyEnabled = false
	if salvo, err = repo.Put(ctx, salvo, salvo.Version); err != nil || salvo.DictionaryCopyEnabled {
		t.Fatalf("desligada, voltou %v (err %v)", salvo.DictionaryCopyEnabled, err)
	}
}
