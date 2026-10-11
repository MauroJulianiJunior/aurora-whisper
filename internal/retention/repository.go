package retention

import (
	"context"
	"time"
)

// TranscriptionRef identifica uma transcrição do histórico ativo elegível
// para purge, com o mínimo necessário para relatório (Job.Run nunca
// precisa do raw_text/final_text — só do ID e do dono, para não passar
// texto do usuário por código que não precisa dele).
type TranscriptionRef struct {
	ID     string
	UserID string
	// CreatedAt é obrigatório: sem ela, o Job não tem como aplicar
	// ActiveExpiryAt por item e ficaria refém do cutoff grosseiro da
	// consulta — que, por causa da normalização de AddDate em fim de mês,
	// apaga dado antes da hora. Ver o comentário em
	// collectExpiredTranscriptionIDs.
	CreatedAt time.Time
}

// TrashedTranscription é uma transcrição atualmente na lixeira, com as
// duas datas que a regra de sobreposição precisa (EffectiveTrashPurgeAt):
// CreatedAt (início da retenção de 6 meses) e DeletedAt (início dos 30
// dias de lixeira).
type TrashedTranscription struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	DeletedAt time.Time
}

// TranscriptionRepository é o contrato mínimo que o motor de retenção
// precisa do armazenamento de transcrições — tanto para o histórico ativo
// (AC-020) quanto para a lixeira (AC-018, AC-019, regra de sobreposição).
//
// Este pacote não decide "onde" as transcrições vivem (SQLite local ou
// Postgres cloud); ele só decide QUANDO cada uma vence, a partir do que a
// implementação concreta devolver aqui.
type TranscriptionRepository interface {
	// ActiveOlderThan devolve as transcrições ativas (nunca deletadas,
	// ou seja fora da lixeira) cujo CreatedAt seja <= cutoff. O chamador
	// (Job.Run) passa cutoff = now menos 6 meses, então tudo que voltar
	// aqui já venceu a retenção de histórico ativo.
	ActiveOlderThan(ctx context.Context, cutoff time.Time) ([]TranscriptionRef, error)

	// TrashItems devolve todo item atualmente na lixeira (deletado e
	// ainda não purgado), com CreatedAt/DeletedAt para o motor aplicar
	// EffectiveTrashPurgeAt e decidir, ele mesmo, quais já venceram — a
	// decisão de "quando" não é responsabilidade do repositório.
	TrashItems(ctx context.Context) ([]TrashedTranscription, error)

	// Purge apaga definitivamente as transcrições com os IDs informados.
	// É idempotente: um ID que já foi purgado (ou nunca existiu) é
	// ignorado silenciosamente, sem erro — condição necessária para
	// NFR-013 (rodar o job duas vezes seguidas não pode falhar). Devolve
	// quantas linhas foram de fato apagadas nesta chamada.
	Purge(ctx context.Context, ids []string) (int, error)
}

// CorrectionRepository é o contrato mínimo para desvincular corrections de
// uma transcrição purgada — ON DELETE SET NULL em
// Correction.transcription_id (docs/ai-spec/05-data-model.md), preservando
// o aprendizado mesmo depois que o ditado que o originou já morreu.
type CorrectionRepository interface {
	// ClearTranscriptionRef zera (NULL) o TranscriptionID de toda
	// correction que apontava para algum dos IDs em transcriptionIDs.
	// Idempotente: corrections já desvinculadas não contam de novo.
	// Devolve quantas corrections foram afetadas nesta chamada.
	ClearTranscriptionRef(ctx context.Context, transcriptionIDs []string) (int, error)
}

// SyncRepository é o contrato mínimo para a limpeza [ASSUMPTION] de
// SyncEvent/AppliedMutation (docs/ai-spec/05-data-model.md: "[ASSUMPTION]
// 90 dias ou compactação"). Um Job sem necessidade de limpar sync (ex.:
// ambiente local sem outbox) pode deixar Job.Sync nil.
type SyncRepository interface {
	// PurgeSyncEventsOlderThan apaga SyncEvent com changed_at <= cutoff e
	// devolve quantos foram apagados.
	PurgeSyncEventsOlderThan(ctx context.Context, cutoff time.Time) (int, error)

	// PurgeAppliedMutationsOlderThan apaga AppliedMutation com
	// applied_at <= cutoff e devolve quantos foram apagados.
	PurgeAppliedMutationsOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}
