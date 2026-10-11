package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// paraCadaSettings roda o mesmo corpo contra TODAS as implementações de
// SettingsRepository — a mesma regra de paraCadaRepo, do histórico: o
// contrato é UM, e uma implementação nova herda todas as asserções.
func paraCadaSettings(t *testing.T, corpo func(*testing.T, storage.SettingsRepository)) {
	t.Helper()
	for _, impl := range []struct {
		nome string
		novo func(*testing.T) storage.SettingsRepository
	}{
		{"memoria", func(*testing.T) storage.SettingsRepository { return storage.NewInMemorySettingsRepository() }},
		{"sqlite", func(t *testing.T) storage.SettingsRepository {
			db := openTestDB(t)
			if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			return storage.NewSQLiteSettingsRepository(db)
		}},
	} {
		t.Run(impl.nome, func(t *testing.T) { corpo(t, impl.novo(t)) })
	}
}

// preferenciasCheias preenche TODOS os campos com valores diferentes do
// zero de cada tipo: um campo esquecido no INSERT, no UPDATE ou no Scan
// volta zerado e o round-trip acusa.
func preferenciasCheias() storage.Settings {
	microfone, modelo := "mic-1", "small-q4"
	return storage.Settings{
		Hotkey:               "ctrl+win",
		HoldThresholdMs:      1234,
		BeepEnabled:          true,
		OverlayPosition:      "bottom-center",
		MicrophoneDeviceID:   &microfone,
		Autostart:            true,
		ASRPolicy:            "local_only",
		LocalModelID:         &modelo,
		LocalContextEnabled:  true,
		ProtectedFieldPolicy: "block",
		LocalPort:            48173,
		TextMode:             "polish",
		AIProvider:           "anthropic",
		AIModel:              "modelo-escolhido",

		MicrophoneAlwaysReady: true,
	}
}

func TestSettingsRepository_Get_NotFoundBeforeFirstPut(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		if _, err := repo.Get(context.Background()); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("esperava ErrNotFound antes do primeiro Put, achou %v", err)
		}
	})
}

func TestSettingsRepository_PutThenGet_RoundTripsEveryField(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		ctx := context.Background()
		quero := preferenciasCheias()

		saved, err := repo.Put(ctx, quero, 0)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if saved.Version != 1 {
			t.Fatalf("primeiro Put deveria ter Version 1, achou %d", saved.Version)
		}
		if saved.UpdatedAt.IsZero() {
			t.Fatal("Put deveria preencher UpdatedAt")
		}

		got, err := repo.Get(ctx)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !got.UpdatedAt.Equal(saved.UpdatedAt) {
			t.Errorf("UpdatedAt: Get = %v, Put devolveu %v", got.UpdatedAt, saved.UpdatedAt)
		}
		quero.Version, quero.UpdatedAt = got.Version, got.UpdatedAt
		if *got.MicrophoneDeviceID != *quero.MicrophoneDeviceID || *got.LocalModelID != *quero.LocalModelID {
			t.Errorf("campos opcionais não voltaram: %+v", got)
		}
		got.MicrophoneDeviceID, got.LocalModelID = quero.MicrophoneDeviceID, quero.LocalModelID
		if got != quero {
			t.Fatalf("Get devolveu diferente do salvo:\n got  %+v\n quero %+v", got, quero)
		}
	})
}

func TestSettingsRepository_OpcionaisAusentesVoltamAusentes(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		s := preferenciasCheias()
		s.MicrophoneDeviceID, s.LocalModelID = nil, nil
		if _, err := repo.Put(context.Background(), s, 0); err != nil {
			t.Fatalf("Put: %v", err)
		}
		got, err := repo.Get(context.Background())
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.MicrophoneDeviceID != nil || got.LocalModelID != nil {
			t.Fatalf("ausente voltou preenchido: mic=%v modelo=%v", got.MicrophoneDeviceID, got.LocalModelID)
		}
	})
}

func TestSettingsRepository_Put_IncrementsVersionAndReplaces(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		ctx := context.Background()
		primeiro, err := repo.Put(ctx, preferenciasCheias(), 0)
		if err != nil {
			t.Fatalf("primeiro Put: %v", err)
		}
		mudado := preferenciasCheias()
		mudado.HoldThresholdMs, mudado.BeepEnabled, mudado.TextMode = 500, false, "literal"
		mudado.AIProvider, mudado.AIModel = "", ""
		mudado.MicrophoneAlwaysReady = false
		segundo, err := repo.Put(ctx, mudado, primeiro.Version)
		if err != nil {
			t.Fatalf("segundo Put: %v", err)
		}
		if segundo.Version != primeiro.Version+1 {
			t.Fatalf("esperava Version %d, achou %d", primeiro.Version+1, segundo.Version)
		}
		got, _ := repo.Get(ctx)
		if got.HoldThresholdMs != 500 || got.BeepEnabled || got.TextMode != "literal" || got.AIProvider != "" || got.AIModel != "" || got.MicrophoneAlwaysReady {
			t.Fatalf("o segundo Put não substituiu: %+v", got)
		}
	})
}

// TestSettingsRepository_MicrofoneSempreProntoLigaEDesliga: a preferência
// do ADR-015 vai e volta nos dois sentidos, e um Put que não a liga grava
// false (o padrão), inclusive por cima de um true anterior.
func TestSettingsRepository_MicrofoneSempreProntoLigaEDesliga(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		ctx := context.Background()
		s := preferenciasCheias()
		s.MicrophoneAlwaysReady = false
		v1, err := repo.Put(ctx, s, 0)
		if err != nil {
			t.Fatalf("Put desligado: %v", err)
		}
		if got, _ := repo.Get(ctx); got.MicrophoneAlwaysReady {
			t.Fatal("gravado desligado, voltou ligado")
		}

		s.MicrophoneAlwaysReady = true
		v2, err := repo.Put(ctx, s, v1.Version)
		if err != nil {
			t.Fatalf("Put ligado: %v", err)
		}
		if got, _ := repo.Get(ctx); !got.MicrophoneAlwaysReady {
			t.Fatal("gravado ligado, voltou desligado")
		}

		s.MicrophoneAlwaysReady = false
		if _, err := repo.Put(ctx, s, v2.Version); err != nil {
			t.Fatalf("Put desligando de novo: %v", err)
		}
		if got, _ := repo.Get(ctx); got.MicrophoneAlwaysReady {
			t.Fatal("desligado por cima de um ligado, continuou ligado")
		}
	})
}

// TestSettingsRepository_Put_VersaoVelhaEhConflito é a concorrência
// otimista: duas abas leram a versão 1; a primeira salva, a segunda
// recebe conflito — e o que a primeira salvou fica.
func TestSettingsRepository_Put_VersaoVelhaEhConflito(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		ctx := context.Background()
		v1, err := repo.Put(ctx, preferenciasCheias(), 0)
		if err != nil {
			t.Fatalf("Put inicial: %v", err)
		}

		aba1 := preferenciasCheias()
		aba1.HoldThresholdMs = 900
		if _, err := repo.Put(ctx, aba1, v1.Version); err != nil {
			t.Fatalf("Put da primeira aba: %v", err)
		}

		aba2 := preferenciasCheias()
		aba2.HoldThresholdMs = 300
		if _, err := repo.Put(ctx, aba2, v1.Version); !errors.Is(err, storage.ErrVersionConflict) {
			t.Fatalf("Put com versão velha: err = %v, quero ErrVersionConflict", err)
		}
		// Versão do futuro também não passa.
		if _, err := repo.Put(ctx, aba2, v1.Version+5); !errors.Is(err, storage.ErrVersionConflict) {
			t.Fatalf("Put com versão inexistente: err = %v, quero ErrVersionConflict", err)
		}

		got, _ := repo.Get(ctx)
		if got.HoldThresholdMs != 900 || got.Version != v1.Version+1 {
			t.Fatalf("o conflito mexeu no que estava salvo: %+v", got)
		}
	})
}

// TestSettingsRepository_Put_PrimeiraGravacaoSoComVersaoZero: antes de
// qualquer Put, a única versão aceita é 0; depois dele, 0 é conflito —
// senão duas abas abertas antes da primeira gravação se sobrescreveriam.
func TestSettingsRepository_Put_PrimeiraGravacaoSoComVersaoZero(t *testing.T) {
	paraCadaSettings(t, func(t *testing.T, repo storage.SettingsRepository) {
		ctx := context.Background()
		if _, err := repo.Put(ctx, preferenciasCheias(), 1); !errors.Is(err, storage.ErrVersionConflict) {
			t.Fatalf("Put com versão 1 no repositório vazio: err = %v, quero ErrVersionConflict", err)
		}
		if _, err := repo.Get(ctx); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("o Put recusado gravou alguma coisa: %v", err)
		}
		if _, err := repo.Put(ctx, preferenciasCheias(), 0); err != nil {
			t.Fatalf("primeiro Put: %v", err)
		}
		if _, err := repo.Put(ctx, preferenciasCheias(), 0); !errors.Is(err, storage.ErrVersionConflict) {
			t.Fatalf("segundo Put com versão 0: err = %v, quero ErrVersionConflict", err)
		}
	})
}
