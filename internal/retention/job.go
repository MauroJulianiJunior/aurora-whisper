package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// Job é o motor de retenção: uma execução (Run) apaga definitivamente
// tudo o que já venceu, segundo o relógio injetado.
//
// Job não agenda a própria cadência — "purge daily" (AC-018/019/020,
// docs/ai-spec/09-security-privacy.md) é responsabilidade de quem chama
// Run periodicamente (ex.: um scheduler do backend). Isso mantém Run
// determinístico e fácil de testar com clock.Fake: cada chamada é uma
// fotografia do que venceu naquele instante.
type Job struct {
	// Clock fornece Now(). Nunca é opcional: um Job sem Clock não tem
	// como decidir o que venceu.
	Clock clock.Clock

	// Transcriptions é obrigatório: sem ele o job não tem o que purgar.
	Transcriptions TranscriptionRepository

	// Corrections é opcional. Quando nil, o job ainda purga transcrições
	// vencidas, mas não desvincula corrections (ON DELETE SET NULL fica
	// a cargo de quem chamar isso separadamente).
	Corrections CorrectionRepository

	// Sync é opcional. Quando nil, o job não limpa
	// SyncEvent/AppliedMutation.
	Sync SyncRepository

	// SyncRetentionDays sobrescreve DefaultSyncRetentionDays (90 dias,
	// [ASSUMPTION]). Zero ou negativo usa o padrão.
	SyncRetentionDays int
}

// Result é a contagem de itens purgados nesta execução, por categoria —
// pensado para alimentar uma métrica retention_purged_total{category=...}
// (contador cumulativo por categoria; o chamador soma Result.* nos labels
// correspondentes a cada rodada).
type Result struct {
	// ActivePurged é quantas transcrições do histórico ativo venceram os
	// 6 meses (AC-020) e foram apagadas nesta rodada.
	ActivePurged int

	// TrashPurged é quantas transcrições da lixeira venceram (AC-019, ou
	// a regra de sobreposição) e foram apagadas nesta rodada.
	TrashPurged int

	// CorrectionsUnlinked é quantas corrections tiveram TranscriptionID
	// zerado porque a transcrição de origem foi purgada nesta rodada.
	CorrectionsUnlinked int

	// SyncEventsPurged é quantos SyncEvent [ASSUMPTION] foram apagados.
	SyncEventsPurged int

	// AppliedMutationsPurged é quantos AppliedMutation [ASSUMPTION]
	// foram apagados.
	AppliedMutationsPurged int
}

// TranscriptionsPurged é o total de transcrições apagadas nesta rodada
// (histórico ativo + lixeira), independente da categoria — conveniência
// para quem só quer saber "quantas transcrições sumiram".
func (r Result) TranscriptionsPurged() int {
	return r.ActivePurged + r.TrashPurged
}

// Run executa uma rodada de retenção: identifica, a partir do estado
// ATUAL dos repositórios, tudo que já venceu segundo j.Clock.Now(), apaga
// definitivamente e desvincula corrections órfãs.
//
// Idempotente (NFR-013): como cada rodada recalcula o vencido a partir do
// estado corrente (não de um lote decidido antes), uma segunda chamada
// imediatamente após a primeira não encontra nada novo — os repositórios
// concretos devem devolver contagem 0 para Purge/ClearTranscriptionRef em
// IDs já removidos, nunca erro (ver TranscriptionRepository.Purge).
func (j *Job) Run(ctx context.Context) (Result, error) {
	if j.Clock == nil {
		return Result{}, fmt.Errorf("retention: Job.Clock é obrigatório")
	}
	if j.Transcriptions == nil {
		return Result{}, fmt.Errorf("retention: Job.Transcriptions é obrigatório")
	}

	now := j.Clock.Now()
	var result Result

	purgeIDs, err := j.collectExpiredTranscriptionIDs(ctx, now, &result)
	if err != nil {
		return Result{}, err
	}

	if len(purgeIDs) > 0 {
		purged, err := j.Transcriptions.Purge(ctx, purgeIDs)
		if err != nil {
			return Result{}, fmt.Errorf("retention: purgar transcrições: %w", err)
		}
		// purged pode ser menor que len(purgeIDs) se, entre a leitura e
		// a purge, algo já tiver sumido (ex.: corrida com outra rodada)
		// — isso não é falha, é o comportamento idempotente esperado.
		// O reporte por categoria (ActivePurged/TrashPurged) já reflete
		// o que foi IDENTIFICADO como vencido nesta rodada; ver doc de
		// Result.
		_ = purged

		if j.Corrections != nil {
			unlinked, err := j.Corrections.ClearTranscriptionRef(ctx, purgeIDs)
			if err != nil {
				return Result{}, fmt.Errorf("retention: desvincular corrections: %w", err)
			}
			result.CorrectionsUnlinked = unlinked
		}
	}

	if j.Sync != nil {
		days := j.SyncRetentionDays
		if days <= 0 {
			days = DefaultSyncRetentionDays
		}
		syncCutoff := now.AddDate(0, 0, -days)

		eventsPurged, err := j.Sync.PurgeSyncEventsOlderThan(ctx, syncCutoff)
		if err != nil {
			return Result{}, fmt.Errorf("retention: purgar sync events: %w", err)
		}
		result.SyncEventsPurged = eventsPurged

		mutationsPurged, err := j.Sync.PurgeAppliedMutationsOlderThan(ctx, syncCutoff)
		if err != nil {
			return Result{}, fmt.Errorf("retention: purgar applied mutations: %w", err)
		}
		result.AppliedMutationsPurged = mutationsPurged
	}

	return result, nil
}

// collectExpiredTranscriptionIDs reúne os IDs de tudo que já venceu — do
// histórico ativo (6 meses de CreatedAt) e da lixeira (EffectiveTrashPurgeAt,
// que já aplica a regra de sobreposição) — e preenche as contagens por
// categoria em result.
func (j *Job) collectExpiredTranscriptionIDs(ctx context.Context, now time.Time, result *Result) ([]string, error) {
	// O cutoff abaixo é um pré-filtro GROSSEIRO, existe só para o banco
	// poder usar índice. Ele NÃO é a regra — a decisão de verdade é por
	// item, com ActiveExpiryAt, no laço mais abaixo.
	//
	// Por que os dois não podem ser a mesma conta: AddDate normaliza o
	// estouro de dia para a frente, e isso não é comutativo. Para um item
	// criado em 31/ago, ActiveExpiryAt dá 3/mar (fevereiro não tem dia 31),
	// mas "now menos 6 meses" em 1/mar dá 1/set — e 31/ago é anterior a
	// isso. Decidir pelo cutoff purgaria o item 2 dias ANTES do fim da
	// retenção. Apagar dado do usuário cedo é inaceitável; retê-lo um pouco
	// a mais é seguro, e é para esse lado que o desenho erra de propósito.
	//
	// O pré-filtro é sempre inclusivo o bastante: como AddDate só empurra
	// para a frente, "now menos 6 meses" nunca cai antes do CreatedAt de um
	// item que já venceu. Ele pode trazer candidatos a mais, nunca de menos.
	coarseCutoff := now.AddDate(0, -ActiveRetentionMonths, 0)
	candidates, err := j.Transcriptions.ActiveOlderThan(ctx, coarseCutoff)
	if err != nil {
		return nil, fmt.Errorf("retention: listar histórico ativo vencido: %w", err)
	}

	trashItems, err := j.Transcriptions.TrashItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("retention: listar lixeira: %w", err)
	}

	ids := make([]string, 0, len(candidates)+len(trashItems))
	for _, t := range candidates {
		if ActiveExpiryAt(t.CreatedAt).After(now) {
			continue // ainda não completou os 6 meses (AC-020).
		}
		ids = append(ids, t.ID)
		result.ActivePurged++
	}

	for _, t := range trashItems {
		purgeAt := EffectiveTrashPurgeAt(t.CreatedAt, t.DeletedAt)
		if purgeAt.After(now) {
			continue // ainda não venceu — restore continua possível (AC-018).
		}
		ids = append(ids, t.ID)
		result.TrashPurged++
	}

	return ids, nil
}
