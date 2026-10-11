package pgstore

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As chaves da IA de cada pessoa (ADR-021, C3a) sobre a ai_provider_keys (a
// 0017): uma linha por pessoa e por provedor, só cifrada. As regras (o
// provedor da lista, os tamanhos) são as de cloudstore.ConferirChaveDeIA, as
// mesmas do Store em memória; aqui só o SQL. Nada passa pelo contador: a chave
// não sincroniza, não tem seq nem evento.

var _ cloudstore.ChavesDeIA = (*Store)(nil)

// sqlGravarChaveDeIA grava por cima da linha do provedor. created_at fica o da
// primeira chave; updated_at nunca fica antes dele (o CHECK da 0017), mesmo
// com o relógio de quem grava voltando.
const sqlGravarChaveDeIA = `
INSERT INTO ai_provider_keys (user_id, provider, cipher_key_id, nonce, ciphertext, key_suffix, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
ON CONFLICT (user_id, provider) DO UPDATE SET
    cipher_key_id = EXCLUDED.cipher_key_id,
    nonce         = EXCLUDED.nonce,
    ciphertext    = EXCLUDED.ciphertext,
    key_suffix    = EXCLUDED.key_suffix,
    updated_at    = GREATEST(EXCLUDED.updated_at, ai_provider_keys.created_at)
RETURNING created_at, updated_at`

const colunasDaChaveDeIA = `provider, cipher_key_id, nonce, ciphertext, key_suffix, created_at, updated_at`

// sqlTravarContadorDaConta trava o contador de sync do usuário até o fim da
// transação e devolve o sync_epoch dele. É a MESMA trava que o primeiro comando
// do "apagar meus dados" pede (sqlNovoEpoch, um UPDATE do contador): ou ele
// termina antes, e aqui o epoch já é o novo, ou espera esta transação, e apaga
// a chave que ela gravou junto com o resto.
const sqlTravarContadorDaConta = `SELECT sync_epoch FROM user_sync_counters WHERE user_id = $1 FOR NO KEY UPDATE`

func scanChaveDeIA(row pgx.Row) (cloudstore.ChaveDeIA, error) {
	var k cloudstore.ChaveDeIA
	err := row.Scan(&k.Provedor, &k.IDDaCifra, &k.Nonce, &k.Cifrado, &k.Final, &k.CriadaEm, &k.AtualizadaEm)
	k.CriadaEm, k.AtualizadaEm = k.CriadaEm.UTC(), k.AtualizadaEm.UTC()
	return k, err
}

func (s *Store) EpochDaConta(ctx context.Context, userID string) (string, error) {
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return "", cloudstore.ErrUnknownUser
	}
	var epoch string
	err := s.pool.QueryRow(ctx, sqlEpoch, uid).Scan(&epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", cloudstore.ErrUnknownUser
	}
	if err != nil {
		return "", sanitize("ler o epoch", err)
	}
	return epoch, nil
}

func (s *Store) GravarChaveDeIA(ctx context.Context, userID string, k cloudstore.ChaveDeIA, now time.Time) (cloudstore.ChaveDeIA, error) {
	if err := cloudstore.ConferirChaveDeIA(k); err != nil {
		return cloudstore.ChaveDeIA{}, err
	}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.ChaveDeIA{}, cloudstore.ErrUnknownUser
	}
	return inserirChaveDeIA(ctx, s.pool, uid, k, now)
}

// GravarChaveDeIAConferindo grava a chave numa transação que trava a conta, e
// só depois de conferir a condição sob essa trava. Duas travas, sempre nesta
// ordem (a linha do usuário, depois o contador):
//
//   - a linha do usuário (FOR NO KEY UPDATE) é a do GravarAceite e do
//     RevogarAceite: um revogar ou um aceite novo em andamento termina antes de
//     esta transação ler o aceite, ou espera ela terminar (e então apaga a chave
//     que ela gravou);
//   - o contador (FOR NO KEY UPDATE) é o do "apagar meus dados" (o UPDATE do
//     novo epoch é o primeiro comando dele) e do ponto único de gravação do
//     histórico.
//
// O READ COMMITTED faz a leitura do epoch, depois de esperar uma trava, ver o
// que a transação que a segurava confirmou, e o SELECT do aceite, que vem depois
// das travas, tem um snapshot novo. Ninguém trava o contador e depois pede a
// linha do usuário, então as duas ordens não se cruzam.
func (s *Store) GravarChaveDeIAConferindo(ctx context.Context, userID string, cond cloudstore.CondicaoDaChave, k cloudstore.ChaveDeIA, now time.Time) (cloudstore.ChaveDeIA, error) {
	if err := cloudstore.ConferirChaveDeIA(k); err != nil {
		return cloudstore.ChaveDeIA{}, err
	}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.ChaveDeIA{}, cloudstore.ErrUnknownUser
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cloudstore.ChaveDeIA{}, sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := travarUsuario(ctx, tx, uid); err != nil {
		return cloudstore.ChaveDeIA{}, err
	}
	var epoch string
	if err := tx.QueryRow(ctx, sqlTravarContadorDaConta, uid).Scan(&epoch); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cloudstore.ChaveDeIA{}, cloudstore.ErrUnknownUser
		}
		return cloudstore.ChaveDeIA{}, sanitize("travar o contador", err)
	}
	if cond.Epoch == "" || cond.Epoch != epoch {
		return cloudstore.ChaveDeIA{}, cloudstore.ErrEpochChanged
	}
	if cond.ExigeAceite {
		a, err := scanAceite(tx.QueryRow(ctx, sqlAceiteAberto, uid))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return cloudstore.ChaveDeIA{}, cloudstore.ErrSemAceite
		case err != nil:
			return cloudstore.ChaveDeIA{}, sanitize("ler o aceite do ditado", err)
		case !a.Cobre(k.Provedor):
			return cloudstore.ChaveDeIA{}, cloudstore.ErrSemAceite
		}
	}
	out, err := inserirChaveDeIA(ctx, tx, uid, k, now)
	if err != nil {
		return cloudstore.ChaveDeIA{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.ChaveDeIA{}, sanitize("confirmar a chave da IA", err)
	}
	return out, nil
}

// consultaUnica é o que o pool e a transação têm em comum para gravar a linha.
type consultaUnica interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// inserirChaveDeIA grava a linha por cima da do provedor (sqlGravarChaveDeIA) e
// devolve as datas. O usuário que a chave estrangeira não acha é ErrUnknownUser.
func inserirChaveDeIA(ctx context.Context, q consultaUnica, uid string, k cloudstore.ChaveDeIA, now time.Time) (cloudstore.ChaveDeIA, error) {
	out := cloudstore.ChaveDeIA{
		Provedor: k.Provedor, IDDaCifra: slices.Clone(k.IDDaCifra), Nonce: slices.Clone(k.Nonce),
		Cifrado: slices.Clone(k.Cifrado), Final: k.Final,
	}
	err := q.QueryRow(ctx, sqlGravarChaveDeIA, uid, k.Provedor, k.IDDaCifra, k.Nonce, k.Cifrado, k.Final, now.UTC()).
		Scan(&out.CriadaEm, &out.AtualizadaEm)
	if err != nil {
		err = sanitize("gravar a chave da IA", err)
		var db *DBError
		if errors.As(err, &db) && db.Code == "23503" {
			return cloudstore.ChaveDeIA{}, cloudstore.ErrUnknownUser
		}
		return cloudstore.ChaveDeIA{}, err
	}
	out.CriadaEm, out.AtualizadaEm = out.CriadaEm.UTC(), out.AtualizadaEm.UTC()
	return out, nil
}

func (s *Store) LerChaveDeIA(ctx context.Context, userID, provedor string) (cloudstore.ChaveDeIA, error) {
	if !cloudstore.ProvedorDeIAValido(provedor) {
		return cloudstore.ChaveDeIA{}, cloudstore.ErrProvedorDeIA
	}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.ChaveDeIA{}, cloudstore.ErrChaveDeIAAusente
	}
	k, err := scanChaveDeIA(s.pool.QueryRow(ctx,
		`SELECT `+colunasDaChaveDeIA+` FROM ai_provider_keys WHERE user_id = $1 AND provider = $2`, uid, provedor))
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.ChaveDeIA{}, cloudstore.ErrChaveDeIAAusente
	}
	if err != nil {
		return cloudstore.ChaveDeIA{}, sanitize("ler a chave da IA", err)
	}
	return k, nil
}

func (s *Store) ListarChavesDeIA(ctx context.Context, userID string) ([]cloudstore.ChaveDeIA, error) {
	out := []cloudstore.ChaveDeIA{}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+colunasDaChaveDeIA+` FROM ai_provider_keys WHERE user_id = $1`, uid)
	if err != nil {
		return nil, sanitize("listar as chaves da IA", err)
	}
	porProvedor, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (cloudstore.ChaveDeIA, error) { return scanChaveDeIA(r) })
	if err != nil {
		return nil, sanitize("listar as chaves da IA", err)
	}
	// A ordem é a da lista fechada, não a do banco.
	for _, p := range cloudstore.ProvedoresDeIA {
		for _, k := range porProvedor {
			if k.Provedor == p {
				out = append(out, k)
			}
		}
	}
	return out, nil
}

func (s *Store) ApagarChaveDeIA(ctx context.Context, userID, provedor string) (bool, error) {
	if !cloudstore.ProvedorDeIAValido(provedor) {
		return false, cloudstore.ErrProvedorDeIA
	}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return false, nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM ai_provider_keys WHERE user_id = $1 AND provider = $2`, uid, provedor)
	if err != nil {
		return false, sanitize("apagar a chave da IA", err)
	}
	return tag.RowsAffected() > 0, nil
}
