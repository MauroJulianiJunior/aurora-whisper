package retention

import (
	"context"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// TestRegression_ActiveHistory_MonthEndNeverPurgesEarly fixa um defeito
// encontrado por ataque adversarial no F1-E.
//
// Existiam duas implementações da mesma regra de 6 meses, e elas
// discordavam. ActiveExpiryAt soma 6 meses A PARTIR de CreatedAt, enquanto
// o Job subtraía 6 meses de now e comparava ao contrário. AddDate normaliza
// o estouro de dia para a frente, e isso não é comutativo: para um item
// criado em 31/ago a expiração oficial cai em 3/mar (fevereiro não tem dia
// 31), mas "1/mar menos 6 meses" dá 1/set, e 31/ago é anterior a isso.
//
// Consequência: o histórico do usuário era apagado, de forma definitiva e
// irreversível, até 2 dias antes da hora.
func TestRegression_ActiveHistory_MonthEndNeverPurgesEarly(t *testing.T) {
	created := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	expiry := ActiveExpiryAt(created)

	// Sanidade: este cenário só tem valor se a data realmente estourar o
	// fim do mês. Se um dia ActiveExpiryAt passar a fazer clamping, este
	// guard avisa em vez de deixar o teste virar decoração.
	if expiry.Day() == created.Day() && expiry.Month() == time.February {
		t.Fatalf("cenário inválido: %s não exercita o estouro de fim de mês (expiry=%s)", created, expiry)
	}

	for _, delta := range []time.Duration{-72 * time.Hour, -48 * time.Hour, -24 * time.Hour, -time.Second} {
		when := expiry.Add(delta)
		t.Run(when.Format("2006-01-02T15:04:05"), func(t *testing.T) {
			fakeClock := clock.NewFake(created)
			repo := NewFakeTranscriptionRepository()
			repo.Create("t1", "u1", created)
			job := &Job{Clock: fakeClock, Transcriptions: repo}

			fakeClock.Set(when)
			result, err := job.Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.ActivePurged != 0 || !repo.Exists("t1") {
				t.Fatalf("purgou em %s, mas a retenção só termina em %s — dado do usuário apagado antes da hora", when, expiry)
			}
		})
	}

	t.Run("purga exatamente no fim da retenção", func(t *testing.T) {
		fakeClock := clock.NewFake(created)
		repo := NewFakeTranscriptionRepository()
		repo.Create("t1", "u1", created)
		job := &Job{Clock: fakeClock, Transcriptions: repo}

		fakeClock.Set(expiry)
		result, err := job.Run(context.Background())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.ActivePurged != 1 || repo.Exists("t1") {
			t.Fatalf("não purgou em %s, que é exatamente o fim da retenção", expiry)
		}
	})
}

// TestRegression_ActiveHistory_LeapYearBoundary cobre 29 de fevereiro, o
// outro caso em que aritmética de mês costuma escorregar.
func TestRegression_ActiveHistory_LeapYearBoundary(t *testing.T) {
	created := time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC) // 2028 é bissexto
	expiry := ActiveExpiryAt(created)

	fakeClock := clock.NewFake(created)
	repo := NewFakeTranscriptionRepository()
	repo.Create("t1", "u1", created)
	job := &Job{Clock: fakeClock, Transcriptions: repo}

	fakeClock.Set(expiry.Add(-time.Second))
	if result, err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	} else if result.ActivePurged != 0 || !repo.Exists("t1") {
		t.Fatalf("purgou um segundo antes do fim da retenção (expiry=%s)", expiry)
	}

	fakeClock.Set(expiry)
	if result, err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	} else if result.ActivePurged != 1 {
		t.Fatalf("ActivePurged = %d, want 1 no fim da retenção (expiry=%s)", result.ActivePurged, expiry)
	}
}

// TestRegression_ClockGoingBackwardsNeverPurges trava a propriedade de que
// um relógio andando para trás — ajuste de horário, correção de NTP — nunca
// apaga nada. Hoje isso é verdade por construção, porque Run recalcula tudo
// do zero a cada chamada e não guarda estado entre execuções. O teste existe
// para que uma refatoração futura que introduza cache ou purge_at
// persistido não quebre a garantia em silêncio.
func TestRegression_ClockGoingBackwardsNeverPurges(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	repo.Create("t1", "u1", start)
	job := &Job{Clock: fakeClock, Transcriptions: repo}
	ctx := context.Background()

	if _, err := job.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	fakeClock.Set(start.AddDate(0, 0, -10))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run com relógio para trás: %v", err)
	}
	if result.TranscriptionsPurged() != 0 || !repo.Exists("t1") {
		t.Fatalf("relógio andando para trás purgou %d item(ns) — não pode apagar nada", result.TranscriptionsPurged())
	}
}
