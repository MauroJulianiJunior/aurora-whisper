package retention

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

var start = time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)

// TestJob_ActiveHistory_PurgesAt6Months_NotADayBefore cobre AC-020: o
// histórico ativo purga aos 6 meses, e não purga um dia antes.
func TestJob_ActiveHistory_PurgesAt6Months_NotADayBefore(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	repo.Create("t1", "u1", start)

	job := &Job{Clock: fakeClock, Transcriptions: repo}
	ctx := context.Background()

	// Um dia antes dos 6 meses: não purga.
	fakeClock.Set(ActiveExpiryAt(start).Add(-24 * time.Hour))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ActivePurged != 0 {
		t.Fatalf("ActivePurged = %d, want 0 (um dia antes dos 6 meses)", result.ActivePurged)
	}
	if !repo.Exists("t1") {
		t.Fatal("transcrição não deveria ter sido purgada um dia antes dos 6 meses")
	}

	// Exatamente aos 6 meses: purga.
	fakeClock.Set(ActiveExpiryAt(start))
	result, err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ActivePurged != 1 {
		t.Fatalf("ActivePurged = %d, want 1 (exatamente aos 6 meses)", result.ActivePurged)
	}
	if repo.Exists("t1") {
		t.Fatal("transcrição deveria ter sido purgada aos 6 meses")
	}
}

// TestJob_Trash_PurgesAtD30_NotD29 cobre AC-019: a lixeira purga em D+30
// a partir do delete, e não purga em D+29. Item recém-criado, então a
// regra de sobreposição não interfere (30 dias < 6 meses de criado).
func TestJob_Trash_PurgesAtD30_NotD29(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	repo.Create("t1", "u1", start)
	repo.SoftDelete("t1", start)

	job := &Job{Clock: fakeClock, Transcriptions: repo}
	ctx := context.Background()

	fakeClock.Set(start.AddDate(0, 0, 29))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.TrashPurged != 0 {
		t.Fatalf("TrashPurged = %d, want 0 em D+29", result.TrashPurged)
	}
	if !repo.Exists("t1") {
		t.Fatal("item não deveria ter sido purgado em D+29")
	}

	fakeClock.Set(start.AddDate(0, 0, 30))
	result, err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.TrashPurged != 1 {
		t.Fatalf("TrashPurged = %d, want 1 em D+30", result.TrashPurged)
	}
	if repo.Exists("t1") {
		t.Fatal("item deveria ter sido purgado em D+30")
	}
}

// TestJob_Trash_RestoreWorks_D1_D15_D29 cobre AC-018: restore funciona em
// D+1, D+15 e D+29 após o delete (ou seja, o job ainda não purgou, e
// Restore tira o item da lixeira com sucesso).
func TestJob_Trash_RestoreWorks_D1_D15_D29(t *testing.T) {
	for _, offsetDays := range []int{1, 15, 29} {
		offsetDays := offsetDays
		t.Run(fmt.Sprintf("D+%d", offsetDays), func(t *testing.T) {
			fakeClock := clock.NewFake(start)
			repo := NewFakeTranscriptionRepository()
			repo.Create("t1", "u1", start)
			repo.SoftDelete("t1", start)

			job := &Job{Clock: fakeClock, Transcriptions: repo}
			ctx := context.Background()

			fakeClock.Set(start.AddDate(0, 0, offsetDays))
			result, err := job.Run(ctx)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.TrashPurged != 0 {
				t.Fatalf("TrashPurged = %d, want 0 em D+%d", result.TrashPurged, offsetDays)
			}
			if !repo.Exists("t1") {
				t.Fatalf("item não deveria ter sido purgado em D+%d", offsetDays)
			}

			repo.Restore("t1")

			trashItems, err := repo.TrashItems(ctx)
			if err != nil {
				t.Fatalf("TrashItems: %v", err)
			}
			for _, item := range trashItems {
				if item.ID == "t1" {
					t.Fatalf("item ainda está na lixeira depois de Restore (D+%d)", offsetDays)
				}
			}
		})
	}
}

// TestJob_Overlap_OldItemDiesAt6MonthsFromCreation_NotAt30DaysFromDelete
// cobre a REGRA DE SOBREPOSIÇÃO: um item criado há quase 6 meses e
// deletado hoje não sobrevive 30 dias — ele morre quando completa 6
// meses de CRIADO, que aqui é 1 dia depois do delete.
func TestJob_Overlap_OldItemDiesAt6MonthsFromCreation_NotAt30DaysFromDelete(t *testing.T) {
	createdAt := start
	// deletado a 5 meses e 29 dias de criado: falta exatamente 1 dia
	// para os 6 meses de criado.
	deletedAt := ActiveExpiryAt(createdAt).AddDate(0, 0, -1)

	fakeClock := clock.NewFake(deletedAt)
	repo := NewFakeTranscriptionRepository()
	repo.Create("t1", "u1", createdAt)
	repo.SoftDelete("t1", deletedAt)

	job := &Job{Clock: fakeClock, Transcriptions: repo}
	ctx := context.Background()

	// Poucas horas depois do delete: nem os 6 meses (1 dia) nem, é
	// claro, os 30 dias de lixeira venceram.
	fakeClock.Set(deletedAt.Add(23 * time.Hour))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.TrashPurged != 0 {
		t.Fatalf("TrashPurged = %d, want 0 (23h após o delete)", result.TrashPurged)
	}
	if !repo.Exists("t1") {
		t.Fatal("item não deveria ter sido purgado 23h após o delete")
	}

	// Exatamente 1 dia depois do delete = exatamente 6 meses de
	// criado: já vence, muito antes dos 30 dias de lixeira que a regra
	// simples de trash sozinha permitiria.
	fakeClock.Set(deletedAt.AddDate(0, 0, 1))
	result, err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.TrashPurged != 1 {
		t.Fatalf("TrashPurged = %d, want 1 (item deveria morrer aos 6 meses de criado, não aos 30 dias de deletado)", result.TrashPurged)
	}
	if repo.Exists("t1") {
		t.Fatal("item deveria ter sido purgado pela regra de sobreposição")
	}
}

// TestJob_Overlap_NewItemUsesNormal30DayWindow cobre o caso inverso da
// sobreposição: item novo deletado hoje -> os 30 dias normais de lixeira
// valem (a regra de 6 meses não interfere porque está longe de vencer).
func TestJob_Overlap_NewItemUsesNormal30DayWindow(t *testing.T) {
	createdAt := start
	deletedAt := start

	fakeClock := clock.NewFake(deletedAt)
	repo := NewFakeTranscriptionRepository()
	repo.Create("t1", "u1", createdAt)
	repo.SoftDelete("t1", deletedAt)

	job := &Job{Clock: fakeClock, Transcriptions: repo}
	ctx := context.Background()

	fakeClock.Set(deletedAt.AddDate(0, 0, 29))
	if result, err := job.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	} else if result.TrashPurged != 0 {
		t.Fatalf("TrashPurged = %d, want 0 em D+29", result.TrashPurged)
	}

	fakeClock.Set(deletedAt.AddDate(0, 0, 30))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.TrashPurged != 1 {
		t.Fatalf("TrashPurged = %d, want 1 em D+30 (janela normal de 30 dias)", result.TrashPurged)
	}
}

// TestJob_Idempotent roda o job 3x seguidas sobre o mesmo estado e
// exige o mesmo resultado: a primeira rodada purga o vencido, as
// seguintes não purgam nada de novo nem falham (NFR-013).
func TestJob_Idempotent(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	// Um vencido no histórico ativo e um vencido na lixeira.
	repo.Create("active-expired", "u1", start)
	repo.Create("trash-expired", "u1", start)
	repo.SoftDelete("trash-expired", start)

	corrections := NewFakeCorrectionRepository()
	corrections.Create("c1", "active-expired")

	job := &Job{Clock: fakeClock, Transcriptions: repo, Corrections: corrections}
	ctx := context.Background()

	// Avança além dos 6 meses (que também é > 30 dias, então ambos os
	// itens já venceram).
	fakeClock.Set(ActiveExpiryAt(start))

	first, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	if first.ActivePurged != 1 || first.TrashPurged != 1 || first.CorrectionsUnlinked != 1 {
		t.Fatalf("resultado da 1a rodada = %+v, want ActivePurged=1 TrashPurged=1 CorrectionsUnlinked=1", first)
	}

	for i := 2; i <= 3; i++ {
		result, err := job.Run(ctx)
		if err != nil {
			t.Fatalf("Run #%d: %v", i, err)
		}
		if result.ActivePurged != 0 || result.TrashPurged != 0 || result.CorrectionsUnlinked != 0 {
			t.Fatalf("resultado da rodada #%d = %+v, want tudo zero (idempotência)", i, result)
		}
	}

	if repo.Count() != 0 {
		t.Fatalf("repo.Count() = %d, want 0 depois das purges", repo.Count())
	}
}

// TestJob_CorrectionSurvivesTranscriptionPurge cobre ON DELETE SET NULL:
// a correction aprendida sobrevive ao purge da transcrição que a
// originou, com TranscriptionID nulo.
func TestJob_CorrectionSurvivesTranscriptionPurge(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	repo.Create("t1", "u1", start)

	corrections := NewFakeCorrectionRepository()
	corrections.Create("c1", "t1")

	job := &Job{Clock: fakeClock, Transcriptions: repo, Corrections: corrections}
	ctx := context.Background()

	fakeClock.Set(ActiveExpiryAt(start))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ActivePurged != 1 {
		t.Fatalf("ActivePurged = %d, want 1", result.ActivePurged)
	}
	if repo.Exists("t1") {
		t.Fatal("transcrição deveria ter sido purgada")
	}

	tid := corrections.TranscriptionIDOf("c1")
	if tid != nil {
		t.Fatalf("correction c1 ainda referencia transcription_id = %q, want nil", *tid)
	}
}

// TestJob_NothingExpires_NothingPurged cobre o caso "repositório jovem":
// nada venceu, então o job não apaga nada.
func TestJob_NothingExpires_NothingPurged(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	repo.Create("fresh-active", "u1", start)
	repo.Create("fresh-trash-source", "u1", start)
	repo.SoftDelete("fresh-trash-source", start)

	corrections := NewFakeCorrectionRepository()
	syncRepo := NewFakeSyncRepository()
	syncRepo.PutEvent("e1", start)
	syncRepo.PutMutation("m1", start)

	job := &Job{Clock: fakeClock, Transcriptions: repo, Corrections: corrections, Sync: syncRepo}
	ctx := context.Background()

	// Mesmo instante da criação: nada venceu.
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != (Result{}) {
		t.Fatalf("result = %+v, want Result{} (nada venceu)", result)
	}
	if repo.Count() != 2 {
		t.Fatalf("repo.Count() = %d, want 2 (nada purgado)", repo.Count())
	}
}

// TestJob_Sync_PurgesOlderThanRetention cobre a limpeza [ASSUMPTION] de
// SyncEvent/AppliedMutation, incluindo o número configurável.
func TestJob_Sync_PurgesOlderThanRetention(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	syncRepo := NewFakeSyncRepository()

	syncRepo.PutEvent("old", start)
	syncRepo.PutEvent("new", start.AddDate(0, 0, 1))
	syncRepo.PutMutation("old-mut", start)
	syncRepo.PutMutation("new-mut", start.AddDate(0, 0, 1))

	job := &Job{Clock: fakeClock, Transcriptions: repo, Sync: syncRepo}
	ctx := context.Background()

	// 90 dias (padrão) depois do evento antigo: ele já venceu, o mais
	// novo (criado 1 dia depois) ainda não.
	fakeClock.Set(start.AddDate(0, 0, DefaultSyncRetentionDays))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SyncEventsPurged != 1 {
		t.Fatalf("SyncEventsPurged = %d, want 1", result.SyncEventsPurged)
	}
	if result.AppliedMutationsPurged != 1 {
		t.Fatalf("AppliedMutationsPurged = %d, want 1", result.AppliedMutationsPurged)
	}

	// O restante ("new"/"new-mut") só vence 1 dia depois.
	fakeClock.Set(start.AddDate(0, 0, DefaultSyncRetentionDays+1))
	result, err = job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SyncEventsPurged != 1 || result.AppliedMutationsPurged != 1 {
		t.Fatalf("resultado = %+v, want SyncEventsPurged=1 AppliedMutationsPurged=1", result)
	}
}

// TestJob_Sync_RetentionDaysConfigurable garante que
// Job.SyncRetentionDays sobrescreve o padrão de 90 dias — a spec marca
// o número como [ASSUMPTION], não decisão fechada.
func TestJob_Sync_RetentionDaysConfigurable(t *testing.T) {
	fakeClock := clock.NewFake(start)
	repo := NewFakeTranscriptionRepository()
	syncRepo := NewFakeSyncRepository()
	syncRepo.PutEvent("e1", start)

	job := &Job{Clock: fakeClock, Transcriptions: repo, Sync: syncRepo, SyncRetentionDays: 7}
	ctx := context.Background()

	fakeClock.Set(start.AddDate(0, 0, 7))
	result, err := job.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SyncEventsPurged != 1 {
		t.Fatalf("SyncEventsPurged = %d, want 1 com SyncRetentionDays=7", result.SyncEventsPurged)
	}
}

// TestJob_RequiresClockAndTranscriptions garante que Job.Run falha de
// forma clara (não com panic) se a configuração mínima faltar, em vez
// de silenciosamente não purgar nada.
func TestJob_RequiresClockAndTranscriptions(t *testing.T) {
	ctx := context.Background()

	if _, err := (&Job{Transcriptions: NewFakeTranscriptionRepository()}).Run(ctx); err == nil {
		t.Fatal("Run sem Clock deveria falhar")
	}

	if _, err := (&Job{Clock: clock.NewFake(start)}).Run(ctx); err == nil {
		t.Fatal("Run sem Transcriptions deveria falhar")
	}
}
