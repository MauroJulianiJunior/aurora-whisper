package storage_test

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A opção "Levar o dicionário para o celular" (ADR-023, F) sobre os dois
// repositórios de preferências.

func TestOpcaoDaCopiaDoDicionario(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		ctx := context.Background()
		padrao := preferenciasCheias()
		padrao.DictionaryCopyEnabled = false
		o := storage.NovaOpcaoDaCopiaDoDicionario(repo, padrao)

		// Sem preferências salvas: desligada, e desligar não cria linha (a versão 0
		// do painel continua valendo).
		if l, err := o.Ligada(ctx); err != nil || l {
			t.Fatalf("Ligada sem linha: %v, %v", l, err)
		}
		if err := o.Gravar(ctx, false); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Get(ctx); err != storage.ErrNotFound {
			t.Fatalf("desligar sem linha criou preferências: %v", err)
		}

		// Ligar sem linha cria a linha com o padrão, ligada.
		if err := o.Gravar(ctx, true); err != nil {
			t.Fatal(err)
		}
		s, err := repo.Get(ctx)
		if err != nil || !s.DictionaryCopyEnabled || s.Version != 1 || s.Hotkey != padrao.Hotkey || s.TextMode != padrao.TextMode {
			t.Fatalf("ligar sem linha: %+v, %v", s, err)
		}
		if l, _ := o.Ligada(ctx); !l {
			t.Fatal("Ligada não viu a opção")
		}

		// Já no valor pedido: não sobe a versão.
		if err := o.Gravar(ctx, true); err != nil {
			t.Fatal(err)
		}
		if s, _ = repo.Get(ctx); s.Version != 1 {
			t.Fatalf("gravar o mesmo valor subiu a versão: %d", s.Version)
		}

		// Desligar muda só a opção; as outras preferências ficam.
		s.BeepEnabled = !s.BeepEnabled
		if s, err = repo.Put(ctx, s, s.Version); err != nil {
			t.Fatal(err)
		}
		if err := o.Gravar(ctx, false); err != nil {
			t.Fatal(err)
		}
		got, _ := repo.Get(ctx)
		if got.DictionaryCopyEnabled || got.BeepEnabled != s.BeepEnabled || got.Version != s.Version+1 {
			t.Fatalf("desligar: %+v (antes %+v)", got, s)
		}

		// O PUT das outras preferências preserva a opção.
		if err := o.Gravar(ctx, true); err != nil {
			t.Fatal(err)
		}
		got, _ = repo.Get(ctx)
		got.HoldThresholdMs++
		if got, err = repo.Put(ctx, got, got.Version); err != nil || !got.DictionaryCopyEnabled {
			t.Fatalf("o Put das outras preferências perdeu a opção: %+v, %v", got, err)
		}
	})
}
