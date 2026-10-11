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

func scanChaveDeIA(row pgx.Row) (cloudstore.ChaveDeIA, error) {
	var k cloudstore.ChaveDeIA
	err := row.Scan(&k.Provedor, &k.IDDaCifra, &k.Nonce, &k.Cifrado, &k.Final, &k.CriadaEm, &k.AtualizadaEm)
	k.CriadaEm, k.AtualizadaEm = k.CriadaEm.UTC(), k.AtualizadaEm.UTC()
	return k, err
}

func (s *Store) GravarChaveDeIA(ctx context.Context, userID string, k cloudstore.ChaveDeIA, now time.Time) (cloudstore.ChaveDeIA, error) {
	if err := cloudstore.ConferirChaveDeIA(k); err != nil {
		return cloudstore.ChaveDeIA{}, err
	}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.ChaveDeIA{}, cloudstore.ErrUnknownUser
	}
	out := cloudstore.ChaveDeIA{
		Provedor: k.Provedor, IDDaCifra: slices.Clone(k.IDDaCifra), Nonce: slices.Clone(k.Nonce),
		Cifrado: slices.Clone(k.Cifrado), Final: k.Final,
	}
	err := s.pool.QueryRow(ctx, sqlGravarChaveDeIA, uid, k.Provedor, k.IDDaCifra, k.Nonce, k.Cifrado, k.Final, now.UTC()).
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
