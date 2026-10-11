package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As chaves da nuvem na tabela instalacao (F6): gravar, ler, substituir e
// apagar; e nada fora de install_id e nuvem_* (o dono fixo não se mexe por
// aqui).
func TestInstalacao_ChavesDaNuvem(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	inst := storage.NewSQLiteSettingsRepository(db).Instalacao()

	if v, ok, err := inst.Ler(ctx, "nuvem_url"); err != nil || ok || v != "" {
		t.Fatalf("Ler de chave ausente = %q, %v, %v", v, ok, err)
	}
	if err := inst.Gravar(ctx, map[string]string{"install_id": "i-1", "nuvem_url": "https://a.test", "nuvem_conta": "c"}); err != nil {
		t.Fatal(err)
	}
	if err := inst.Gravar(ctx, map[string]string{"nuvem_url": "https://b.test"}); err != nil {
		t.Fatal(err)
	}
	for nome, quero := range map[string]string{"install_id": "i-1", "nuvem_url": "https://b.test", "nuvem_conta": "c"} {
		if v, ok, err := inst.Ler(ctx, nome); err != nil || !ok || v != quero {
			t.Errorf("Ler(%s) = %q, %v, %v; quero %q", nome, v, ok, err, quero)
		}
	}
	if err := inst.Apagar(ctx, "nuvem_conta", "nuvem_nao_existe"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := inst.Ler(ctx, "nuvem_conta"); ok {
		t.Error("nuvem_conta continua depois de apagar")
	}
	for _, nome := range []string{"dono_local", "dono_local_copia", "nuvem_", "outra", ""} {
		if err := inst.Gravar(ctx, map[string]string{nome: "x"}); !errors.Is(err, storage.ErrChaveDaInstalacao) {
			t.Errorf("Gravar(%q) = %v, quero ErrChaveDaInstalacao", nome, err)
		}
		if err := inst.Apagar(ctx, nome); !errors.Is(err, storage.ErrChaveDaInstalacao) {
			t.Errorf("Apagar(%q) = %v, quero ErrChaveDaInstalacao", nome, err)
		}
		if _, _, err := inst.Ler(ctx, nome); !errors.Is(err, storage.ErrChaveDaInstalacao) {
			t.Errorf("Ler(%q) = %v, quero ErrChaveDaInstalacao", nome, err)
		}
	}
	// Uma chave fora da lista no meio do lote: nada é gravado.
	if err := inst.Gravar(ctx, map[string]string{"nuvem_email": "x@y", "dono_local": "z"}); !errors.Is(err, storage.ErrChaveDaInstalacao) {
		t.Fatal(err)
	}
	if _, ok, _ := inst.Ler(ctx, "nuvem_email"); ok {
		t.Error("o lote recusado gravou parte")
	}
}
