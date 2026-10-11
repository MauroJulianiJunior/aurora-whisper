package pgstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Depois de restaurar um backup (ADR-019, "Decisões da F11", F11d;
// cloudstore.Restaurador): o epoch novo de cada conta e a revogação de todas
// as sessões e de todos os aparelhos, numa transação só. Quem chama é o
// comando pos-restauracao (cmd/server), com a API parada.

var _ cloudstore.Restaurador = (*Store)(nil)

const (
	// O primeiro comando trava os contadores de todas as contas, como o
	// ponto único de gravação: nada grava no meio (e a API está parada).
	sqlContasDaRestauracao = `SELECT user_id FROM user_sync_counters ORDER BY user_id FOR UPDATE`
	// Todas as sessões saem (a sessão derrubada depois do backup voltaria).
	sqlDerrubarTodasAsSessoes = `DELETE FROM web_sessions`
	// Todos os aparelhos ainda não revogados: revoked_at e a versão mais um,
	// como o RevogarAparelho. A linha fica (os ditados apontam para ela, e
	// parear de novo a reaproveita).
	sqlRevogarTodosOsAparelhos = `UPDATE devices SET revoked_at = $1, version = version + 1 WHERE revoked_at IS NULL`
	// E os tokens, todos (o de um aparelho revogado depois do backup voltou
	// com o backup).
	sqlApagarTodosOsTokens = `DELETE FROM device_tokens`
)

func (s *Store) DepoisDeRestaurar(ctx context.Context, now time.Time) (cloudstore.Restauracao, error) {
	var r cloudstore.Restauracao
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return r, sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	rows, err := tx.Query(ctx, sqlContasDaRestauracao)
	if err != nil {
		return r, sanitize("travar os contadores", err)
	}
	contas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return r, sanitize("travar os contadores", err)
	}
	for _, conta := range contas {
		// O mesmo comando do "apagar meus dados" (contas.go): o epoch novo e a
		// marca d'água das lápides no last_seq.
		tag, err := tx.Exec(ctx, sqlNovoEpoch, conta, cloudstore.NewUUIDv7())
		if err != nil {
			return r, sanitize("trocar o epoch", err)
		}
		r.Contas += tag.RowsAffected()
	}
	tag, err := tx.Exec(ctx, sqlDerrubarTodasAsSessoes)
	if err != nil {
		return r, sanitize("derrubar as sessões", err)
	}
	r.Sessoes = tag.RowsAffected()
	if tag, err = tx.Exec(ctx, sqlRevogarTodosOsAparelhos, now); err != nil {
		return r, sanitize("revogar os aparelhos", err)
	}
	r.Aparelhos = tag.RowsAffected()
	if _, err := tx.Exec(ctx, sqlApagarTodosOsTokens); err != nil {
		return r, sanitize("apagar os tokens", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.Restauracao{}, sanitize("confirmar o depois de restaurar", err)
	}
	return r, nil
}
