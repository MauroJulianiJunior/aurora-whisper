package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O ditado que nasce na nuvem (ADR-021, C1): o Create e o Situacao sobre o
// Postgres. As regras (a validação, a linha a gravar) são as de
// cloudstore.NewTranscription, as mesmas do Store em memória; aqui ficam a
// transação, a ordem das travas e o SQL.

// Situacao lê o que a conta sabe do id: a linha do ditado, a lápide, e o epoch
// do contador, num snapshot só (REPEATABLE READ, somente leitura, como o
// pull). Não trava nada e não gasta seq.
func (s *Store) Situacao(ctx context.Context, userID, id string) (cloudstore.SituacaoDoId, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.SituacaoDoId{}, cloudstore.ErrInvalidID
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return cloudstore.SituacaoDoId{}, sanitize("abrir a leitura", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var sit cloudstore.SituacaoDoId
	if err := tx.QueryRow(ctx, sqlEpoch, userID).Scan(&sit.Epoch); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cloudstore.SituacaoDoId{}, cloudstore.ErrUnknownUser
		}
		return cloudstore.SituacaoDoId{}, sanitize("ler o epoch", err)
	}
	t, err := scanTranscription(tx.QueryRow(ctx, sqlSelectOne, userID, canon))
	switch {
	case err == nil:
		sit.Estado, sit.Ditado = cloudstore.IdDitado, &t
		return sit, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return cloudstore.SituacaoDoId{}, sanitize("ler o ditado", err)
	}
	var tomb bool
	if err := tx.QueryRow(ctx, sqlTombstoneExists, userID, canon).Scan(&tomb); err != nil {
		return cloudstore.SituacaoDoId{}, sanitize("ler a lápide", err)
	}
	if tomb {
		sit.Estado = cloudstore.IdLapide
	}
	return sit, nil
}

// Create grava o ditado novo pelo ponto único de gravação: o primeiro comando
// da transação sobe o contador e devolve o epoch DELE, e é com esse que o
// epoch esperado é conferido, sob a trava. "Apagar meus dados" troca o epoch
// na transação que trava o mesmo contador, então ou ela termina antes (o
// Create vê o epoch novo e recusa) ou o Create termina antes (e os dados vão
// embora com o resto): não há janela entre conferir e gravar.
func (s *Store) Create(ctx context.Context, a cloudstore.Actor, epoch, id string, n cloudstore.NewDictation, now time.Time) (cloudstore.Transcription, bool, error) {
	now = cloudstore.Normalize(now)
	t, err := cloudstore.NewTranscription(a, id, n, now)
	if err != nil {
		return cloudstore.Transcription{}, false, err
	}
	var out cloudstore.Transcription
	created := false
	err = s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, current string) (bool, error) {
		if epoch == "" || epoch != current {
			return false, cloudstore.ErrEpochChanged
		}
		cur, err := readForUpdate(ctx, tx, a.UserID, t.ID)
		if err != nil {
			return false, err
		}
		if cur != nil {
			// O segundo de uma corrida (ou o reenvio) recebe o que o primeiro
			// gravou, sem gravar nem gastar seq.
			out = *cur
			return false, nil
		}
		var tomb bool
		if err := tx.QueryRow(ctx, sqlTombstoneExists, a.UserID, t.ID).Scan(&tomb); err != nil {
			return false, sanitize("ler a lápide", err)
		}
		if tomb {
			return false, cloudstore.ErrTombstoned
		}
		if err := put(ctx, tx, t, true, 0, seq, a, now); err != nil {
			return false, err
		}
		t.LastSeq = seq
		out, created = t, true
		return true, nil
	})
	if err != nil {
		return cloudstore.Transcription{}, false, err
	}
	return out, created, nil
}
