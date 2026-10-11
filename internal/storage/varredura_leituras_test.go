package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As duas leituras que a varredura do aprendido (F8f3) faz antes de decidir:
// o aceite do "Enviar o histórico" e a conferência do pareamento antes de
// tocar o dicionario.json. Nenhuma das duas escreve.

func TestEnvioAceito(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	if aceito, err := f.fila.EnvioAceito(ctx); err != nil || aceito {
		t.Fatalf("sem o clique: %v, %v", aceito, err)
	}
	aceitar(t, f)
	if aceito, err := f.fila.EnvioAceito(ctx); err != nil || !aceito {
		t.Fatalf("depois do clique: %v, %v", aceito, err)
	}
}

// Sem o estado do pareamento, ErrPareamentoMudou; com o epoch nulo, outro, ou
// vazio, ErrEpochDiferente, e o epoch nulo continua nulo (nunca adota); com o
// mesmo, nil.
func TestConferirPareamento(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	if err := f.fila.ConferirPareamento(ctx, "a1", "e1"); !errors.Is(err, storage.ErrPareamentoMudou) {
		t.Fatalf("sem o estado: %v", err)
	}
	preparado(t, f)
	if err := f.fila.ConferirPareamento(ctx, "a1", "e1"); !errors.Is(err, storage.ErrEpochDiferente) {
		t.Fatalf("o epoch nulo: %v", err)
	}
	var epoch any
	if err := f.db.QueryRow(`SELECT sync_epoch FROM sync_state`).Scan(&epoch); err != nil || epoch != nil {
		t.Fatalf("a conferência adotou o epoch: %v, %v", epoch, err)
	}
	executar(t, f.db, `UPDATE sync_state SET sync_epoch = 'e1'`)
	for _, c := range []struct {
		aparelho, epoch string
		quero           error
	}{
		{"a1", "e1", nil},
		{"a1", "e2", storage.ErrEpochDiferente},
		{"a1", "", storage.ErrEpochDiferente},
		{"a2", "e1", storage.ErrPareamentoMudou},
	} {
		if err := f.fila.ConferirPareamento(ctx, c.aparelho, c.epoch); !errors.Is(err, c.quero) || (c.quero == nil && err != nil) {
			t.Errorf("%s/%s: %v, want %v", c.aparelho, c.epoch, err, c.quero)
		}
	}
}
