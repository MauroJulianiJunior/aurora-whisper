package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As preferências e o aceite do ditado no celular (ADR-021, C3b) sobre a
// dictation_settings (a 0018) e a dictation_consents (a 0019). As regras (os
// provedores, o modelo, o aceite que substitui o anterior) são as de
// cloudstore, as mesmas do Store em memória; aqui só o SQL. Nada passa pelo
// contador: não sincroniza, não tem seq nem evento.
//
// Gravar o aceite e revogá-lo travam a linha do usuário (FOR NO KEY UPDATE, que
// não atrapalha as chaves estrangeiras que apontam para ela): duas gravações
// da mesma conta ao mesmo tempo se enfileiram, e a segunda vê o que a primeira
// deixou, em vez de bater no índice único do aceite em aberto.

var _ cloudstore.DitadoNoCelular = (*Store)(nil)

// sqlGravarPreferencias grava as preferências inteiras por cima. created_at
// fica o da primeira gravação; updated_at nunca fica antes dele (o CHECK da
// 0018), mesmo com o relógio de quem grava voltando.
const sqlGravarPreferencias = `
INSERT INTO dictation_settings (user_id, asr_provider, asr_model, use_hint, polish_enabled, polish_provider, polish_model, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (user_id) DO UPDATE SET
    asr_provider    = EXCLUDED.asr_provider,
    asr_model       = EXCLUDED.asr_model,
    use_hint        = EXCLUDED.use_hint,
    polish_enabled  = EXCLUDED.polish_enabled,
    polish_provider = EXCLUDED.polish_provider,
    polish_model    = EXCLUDED.polish_model,
    updated_at      = GREATEST(EXCLUDED.updated_at, dictation_settings.created_at)
RETURNING created_at, updated_at`

const colunasDasPreferencias = `asr_provider, asr_model, use_hint, polish_enabled, polish_provider, polish_model, created_at, updated_at`

// sqlTravarUsuario trava a linha do usuário até o fim da transação, e diz se
// ele existe.
const sqlTravarUsuario = `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`

const colunasDoAceite = `id, notice_version, asr_provider, COALESCE(polish_provider, ''), accepted_at, ended_at, COALESCE(end_reason, '')`

const (
	sqlAceiteAberto = `SELECT ` + colunasDoAceite + ` FROM dictation_consents WHERE user_id = $1 AND ended_at IS NULL`

	sqlSubstituirAceite = `UPDATE dictation_consents SET ended_at = GREATEST($2::timestamptz, accepted_at), end_reason = 'substituido'
		WHERE user_id = $1 AND ended_at IS NULL`

	sqlGravarAceite = `INSERT INTO dictation_consents (id, user_id, notice_version, asr_provider, polish_provider, accepted_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	sqlRevogarAceite = `UPDATE dictation_consents SET ended_at = GREATEST($2::timestamptz, accepted_at), end_reason = 'revogado'
		WHERE user_id = $1 AND ended_at IS NULL`

	sqlApagarChavesDaConta = `DELETE FROM ai_provider_keys WHERE user_id = $1`

	sqlListarAceites = `SELECT ` + colunasDoAceite + ` FROM dictation_consents WHERE user_id = $1
		ORDER BY accepted_at, (ended_at IS NULL), id`
)

func scanAceite(row pgx.Row) (cloudstore.Aceite, error) {
	var (
		a      cloudstore.Aceite
		fim    *time.Time
		motivo string
	)
	if err := row.Scan(&a.ID, &a.Versao, &a.Transcricao, &a.Polish, &a.AceitoEm, &fim, &motivo); err != nil {
		return cloudstore.Aceite{}, err
	}
	a.AceitoEm = a.AceitoEm.UTC()
	if fim != nil {
		a.EncerradoEm = fim.UTC()
	}
	a.Motivo = motivo
	return a, nil
}

func (s *Store) LerPreferenciasDoDitado(ctx context.Context, userID string) (cloudstore.PreferenciasDoDitado, error) {
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.PreferenciasPadrao(), nil
	}
	var p cloudstore.PreferenciasDoDitado
	err := s.pool.QueryRow(ctx, `SELECT `+colunasDasPreferencias+` FROM dictation_settings WHERE user_id = $1`, uid).
		Scan(&p.Transcricao.Provedor, &p.Transcricao.Modelo, &p.Dica, &p.PolishLigado, &p.Polish.Provedor, &p.Polish.Modelo,
			&p.CriadaEm, &p.AtualizadaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.PreferenciasPadrao(), nil
	}
	if err != nil {
		return cloudstore.PreferenciasDoDitado{}, sanitize("ler as preferências do ditado", err)
	}
	p.CriadaEm, p.AtualizadaEm = p.CriadaEm.UTC(), p.AtualizadaEm.UTC()
	return p, nil
}

func (s *Store) GravarPreferenciasDoDitado(ctx context.Context, userID string, p cloudstore.PreferenciasDoDitado, now time.Time) (cloudstore.PreferenciasDoDitado, error) {
	if err := cloudstore.ConferirPreferencias(p); err != nil {
		return cloudstore.PreferenciasDoDitado{}, err
	}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.PreferenciasDoDitado{}, cloudstore.ErrUnknownUser
	}
	err := s.pool.QueryRow(ctx, sqlGravarPreferencias, uid, p.Transcricao.Provedor, p.Transcricao.Modelo, p.Dica, p.PolishLigado,
		p.Polish.Provedor, p.Polish.Modelo, now.UTC()).Scan(&p.CriadaEm, &p.AtualizadaEm)
	if err != nil {
		err = sanitize("gravar as preferências do ditado", err)
		var db *DBError
		if errors.As(err, &db) && db.Code == "23503" {
			return cloudstore.PreferenciasDoDitado{}, cloudstore.ErrUnknownUser
		}
		return cloudstore.PreferenciasDoDitado{}, err
	}
	p.CriadaEm, p.AtualizadaEm = p.CriadaEm.UTC(), p.AtualizadaEm.UTC()
	return p, nil
}

func (s *Store) LerAceiteAberto(ctx context.Context, userID string) (cloudstore.Aceite, error) {
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.Aceite{}, cloudstore.ErrSemAceite
	}
	a, err := scanAceite(s.pool.QueryRow(ctx, sqlAceiteAberto, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.Aceite{}, cloudstore.ErrSemAceite
	}
	if err != nil {
		return cloudstore.Aceite{}, sanitize("ler o aceite do ditado", err)
	}
	return a, nil
}

func (s *Store) GravarAceite(ctx context.Context, userID string, n cloudstore.NovoAceite, now time.Time) (cloudstore.Aceite, error) {
	if err := cloudstore.ConferirAceite(n); err != nil {
		return cloudstore.Aceite{}, err
	}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return cloudstore.Aceite{}, cloudstore.ErrUnknownUser
	}
	now = now.UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cloudstore.Aceite{}, sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := travarUsuario(ctx, tx, uid); err != nil {
		return cloudstore.Aceite{}, err
	}
	aberto, err := scanAceite(tx.QueryRow(ctx, sqlAceiteAberto, uid))
	switch {
	case err == nil:
		if aberto.Versao == n.Versao && aberto.Transcricao == n.Transcricao && aberto.Polish == n.Polish {
			// Aceitar de novo o mesmo não grava uma linha: a data é a da
			// primeira vez.
			if err := tx.Commit(ctx); err != nil {
				return cloudstore.Aceite{}, sanitize("confirmar o aceite do ditado", err)
			}
			return aberto, nil
		}
		if _, err := tx.Exec(ctx, sqlSubstituirAceite, uid, now); err != nil {
			return cloudstore.Aceite{}, sanitize("encerrar o aceite anterior", err)
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return cloudstore.Aceite{}, sanitize("ler o aceite do ditado", err)
	}

	a := cloudstore.Aceite{ID: cloudstore.NewUUIDv7(), Versao: n.Versao, Transcricao: n.Transcricao, Polish: n.Polish, AceitoEm: now}
	var polish *string
	if n.Polish != "" {
		polish = &n.Polish
	}
	if _, err := tx.Exec(ctx, sqlGravarAceite, a.ID, uid, a.Versao, a.Transcricao, polish, now); err != nil {
		return cloudstore.Aceite{}, sanitize("gravar o aceite do ditado", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.Aceite{}, sanitize("confirmar o aceite do ditado", err)
	}
	return a, nil
}

func (s *Store) RevogarAceite(ctx context.Context, userID string, now time.Time) (cloudstore.Revogacao, error) {
	var r cloudstore.Revogacao
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return r, nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return r, sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := travarUsuario(ctx, tx, uid); err != nil {
		if errors.Is(err, cloudstore.ErrUnknownUser) {
			return r, nil
		}
		return r, err
	}
	tag, err := tx.Exec(ctx, sqlRevogarAceite, uid, now.UTC())
	if err != nil {
		return cloudstore.Revogacao{}, sanitize("revogar o aceite do ditado", err)
	}
	r.Revogou = tag.RowsAffected() > 0
	tag, err = tx.Exec(ctx, sqlApagarChavesDaConta, uid)
	if err != nil {
		return cloudstore.Revogacao{}, sanitize("apagar as chaves da IA", err)
	}
	r.ChavesApagadas = int(tag.RowsAffected())
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.Revogacao{}, sanitize("confirmar a revogação do aceite", err)
	}
	return r, nil
}

func (s *Store) ListarAceites(ctx context.Context, userID string) ([]cloudstore.Aceite, error) {
	out := []cloudstore.Aceite{}
	uid, ok := cloudstore.CanonicalUUID(userID)
	if !ok {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, sqlListarAceites, uid)
	if err != nil {
		return nil, sanitize("listar os aceites do ditado", err)
	}
	aceites, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (cloudstore.Aceite, error) { return scanAceite(r) })
	if err != nil {
		return nil, sanitize("listar os aceites do ditado", err)
	}
	return append(out, aceites...), nil
}

// travarUsuario trava a linha do usuário até o fim da transação. O usuário que
// não existe é ErrUnknownUser.
func travarUsuario(ctx context.Context, tx pgx.Tx, uid string) error {
	var um int
	err := tx.QueryRow(ctx, sqlTravarUsuario, uid).Scan(&um)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.ErrUnknownUser
	}
	return sanitize("travar o usuário", err)
}
