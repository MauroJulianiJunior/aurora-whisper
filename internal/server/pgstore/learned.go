package pgstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As rotas do celular para o aprendido no Postgres (F8d; ADR-019, "Decisões
// da F8", as rotas do celular): a listagem e o desfazer. O push e o pull do
// aprendido estão em store.go e read.go (F8c).

// A listagem do celular: só os pares não desfeitos do usuário, do mais recente
// (last_seq) para o mais antigo, com uma linha a mais que o limite para dizer
// que passou dele. O índice é o UNIQUE (user_id, last_seq) da 0016, lido de
// trás para a frente; a leitura é um comando só (READ COMMITTED), então a
// página é de um snapshot.
var sqlListLearned = "SELECT " + joinCols(learnedCols, "") + `
	FROM learned_pairs
	WHERE user_id = $1 AND undone_at IS NULL
	ORDER BY last_seq DESC
	LIMIT $2`

// ListLearned lista o aprendido que ainda vale, do mais recente para o mais
// antigo, até o limite (cloudstore.LearnedListLimit: o teto por padrão). Só
// lê.
func (s *Store) ListLearned(ctx context.Context, userID string, limit int) (cloudstore.LearnedPage, error) {
	limit = cloudstore.LearnedListLimit(limit)
	rows, err := s.pool.Query(ctx, sqlListLearned, userID, limit+1)
	if err != nil {
		return cloudstore.LearnedPage{}, sanitize("listar o aprendido", err)
	}
	defer rows.Close()
	page := cloudstore.LearnedPage{Items: []cloudstore.Learned{}}
	for rows.Next() {
		l, err := scanLearned(rows)
		if err != nil {
			return cloudstore.LearnedPage{}, sanitize("listar o aprendido", err)
		}
		page.Items = append(page.Items, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cloudstore.LearnedPage{}, sanitize("listar o aprendido", err)
	}
	if len(page.Items) > limit {
		page.Items, page.HasMore = page.Items[:limit], true
	}
	return page, nil
}

// UndoLearned desfaz o aprendido pelo ponto único de gravação: o contador
// primeiro, a linha da learned_pairs FOR UPDATE por (user_id, id), a regra
// cloudstore.UndoPair (a mesma do delete do PC), o UPDATE e o evento learned
// upsert com o mesmo seq e sem aparelho (o celular). Já desfeito, devolve a
// linha como está e o número volta, sem gravar nada. O id que não é UUID, o
// que não existe e o de outro usuário são o mesmo erro, sem abrir transação
// quando o id nem é um UUID.
func (s *Store) UndoLearned(ctx context.Context, a cloudstore.Actor, id string, now time.Time) (cloudstore.Learned, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.Learned{}, cloudstore.ErrLearnedNotFound
	}
	now = cloudstore.Normalize(now)
	var out cloudstore.Learned
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		cur, err := readLearnedForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if cur == nil {
			return false, cloudstore.ErrLearnedNotFound
		}
		next, changed := cloudstore.UndoPair(*cur, now)
		if !changed {
			out = *cur
			return false, nil
		}
		if err := putLearned(ctx, tx, next, false, cur.Version, seq, a, now); err != nil {
			return false, err
		}
		next.LastSeq = seq
		out = next
		return true, nil
	})
	return out, err
}
