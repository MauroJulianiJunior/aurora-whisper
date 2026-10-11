package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O pareamento de um PC e os aparelhos da conta no Postgres (ADR-019, F6),
// sobre as tabelas da F1 (0002 devices, 0013 pairing_requests, 0014
// device_tokens), que servem como estão. O relógio é sempre o do servidor
// (now, que os testes controlam), nunca o now() do Postgres.

const (
	sqlCriarPedido = `INSERT INTO pairing_requests
		(id, code_challenge, install_id, device_name, platform, app_version, redirect_port, confirmation_code, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10)`

	// O pedido que ainda espera: não aprovado, não usado, não vencido.
	sqlLerPedido = `SELECT id, code_challenge, install_id, device_name, platform, COALESCE(app_version, ''),
			redirect_port, confirmation_code, created_at, expires_at
		FROM pairing_requests
		WHERE id = $1 AND approved_by IS NULL AND used_at IS NULL AND expires_at > $2`

	// Aprovar confere e grava no MESMO comando: dois POST de aprovação ao
	// mesmo tempo, e o segundo espera a trava da linha e, com approved_by já
	// preenchido, não casa o WHERE.
	sqlAprovarPedido = `UPDATE pairing_requests SET approved_by = $2, approved_at = $3, code_hash = $4
		WHERE id = $1 AND approved_by IS NULL AND used_at IS NULL AND expires_at > $3
		RETURNING id, code_challenge, install_id, device_name, platform, COALESCE(app_version, ''),
			redirect_port, confirmation_code, created_at, expires_at`

	// A troca do code: uso único, sem corrida, e o desafio do PKCE no WHERE
	// (com o hash do code): um verificador errado não casa a linha e não a
	// consome. Oito trocas simultâneas do mesmo code: a primeira marca
	// used_at, as outras esperam a trava e não casam mais.
	sqlConsumirCodigo = `UPDATE pairing_requests SET used_at = $3
		WHERE code_hash = $1 AND code_challenge = $2 AND approved_by IS NOT NULL
			AND used_at IS NULL AND expires_at > $3
		RETURNING approved_by, install_id, device_name, platform, COALESCE(app_version, '')`

	sqlContaDaTroca = `SELECT email FROM users WHERE id = $1 AND disabled_at IS NULL`

	// O aparelho: o mesmo de antes pelo usuário e o install_id (o UNIQUE
	// (user_id, install_id) da 0002), sem revogação e com a versão mais um;
	// senão, um novo. O id novo só vale no INSERT.
	sqlGravarAparelho = `INSERT INTO devices (id, user_id, install_id, name, platform, app_version, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $7)
		ON CONFLICT (user_id, install_id) DO UPDATE
			SET name = EXCLUDED.name, platform = EXCLUDED.platform, app_version = EXCLUDED.app_version,
				revoked_at = NULL, version = devices.version + 1, last_seen_at = EXCLUDED.last_seen_at
		RETURNING id`

	// Um token por aparelho (device_id UNIQUE): o de antes sai.
	sqlApagarTokenDoAparelho = `DELETE FROM device_tokens WHERE device_id = $1`
	sqlGravarToken           = `INSERT INTO device_tokens (token_hmac, device_id, created_at) VALUES ($1, $2, $3)`

	// A lista: os não revogados com o token valendo, o usado por último
	// primeiro. O token sem uso há TokenSemUso ($2 é o limite) não entra.
	sqlListarAparelhos = `SELECT d.id, d.name, d.platform, COALESCE(d.app_version, ''), t.created_at,
			COALESCE(t.last_used_at, t.created_at)
		FROM devices d JOIN device_tokens t ON t.device_id = d.id
		WHERE d.user_id = $1 AND d.revoked_at IS NULL AND COALESCE(t.last_used_at, t.created_at) > $2
		ORDER BY COALESCE(t.last_used_at, t.created_at) DESC, d.id`

	// Revogar: revoked_at e a versão mais um, só do aparelho do usuário, e
	// só se ainda não foi revogado; o token sai na mesma transação.
	sqlRevogarAparelho = `UPDATE devices SET revoked_at = $3, version = version + 1
		WHERE user_id = $1 AND id = $2 AND revoked_at IS NULL`

	sqlArrumarPedidos = `DELETE FROM pairing_requests WHERE expires_at <= $1`
)

func scanPedido(row pgx.Row) (cloudstore.PedidoDePareamento, error) {
	var p cloudstore.PedidoDePareamento
	err := row.Scan(&p.ID, &p.CodeChallenge, &p.InstallID, &p.Nome, &p.Plataforma, &p.Versao,
		&p.Porta, &p.Conferencia, &p.CriadoEm, &p.ExpiraEm)
	p.CriadoEm, p.ExpiraEm = p.CriadoEm.UTC(), p.ExpiraEm.UTC()
	return p, err
}

func (s *Store) CriarPedidoDePareamento(ctx context.Context, p cloudstore.PedidoDePareamento) error {
	_, err := s.pool.Exec(ctx, sqlCriarPedido, p.ID, p.CodeChallenge, p.InstallID, p.Nome, p.Plataforma, p.Versao,
		p.Porta, p.Conferencia, p.CriadoEm, p.ExpiraEm)
	if code(err) == "23505" {
		return cloudstore.ErrPedido
	}
	return sanitize("gravar o pedido de pareamento", err)
}

func (s *Store) LerPedidoDePareamento(ctx context.Context, id string, now time.Time) (cloudstore.PedidoDePareamento, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.PedidoDePareamento{}, cloudstore.ErrPedido
	}
	p, err := scanPedido(s.pool.QueryRow(ctx, sqlLerPedido, canon, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.PedidoDePareamento{}, cloudstore.ErrPedido
	}
	if err != nil {
		return cloudstore.PedidoDePareamento{}, sanitize("ler o pedido de pareamento", err)
	}
	return p, nil
}

func (s *Store) AprovarPedidoDePareamento(ctx context.Context, id, userID string, codeHash []byte, now time.Time) (cloudstore.PedidoDePareamento, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.PedidoDePareamento{}, cloudstore.ErrPedido
	}
	p, err := scanPedido(s.pool.QueryRow(ctx, sqlAprovarPedido, canon, userID, now, codeHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.PedidoDePareamento{}, cloudstore.ErrPedido
	}
	if err != nil {
		return cloudstore.PedidoDePareamento{}, sanitize("aprovar o pedido de pareamento", err)
	}
	return p, nil
}

func (s *Store) TrocarCodigoDePareamento(ctx context.Context, t cloudstore.TrocaDeCodigo) (cloudstore.AparelhoPareado, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cloudstore.AparelhoPareado{}, sanitize("abrir a transação", err)
	}
	// Qualquer saída sem o commit desfaz tudo, inclusive o used_at: a conta
	// recusada no meio não queima o code (ele não serve a mais ninguém).
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var userID, installID, nome, plataforma, versao string
	err = tx.QueryRow(ctx, sqlConsumirCodigo, t.CodeHash, t.Desafio, t.Agora).
		Scan(&userID, &installID, &nome, &plataforma, &versao)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.AparelhoPareado{}, cloudstore.ErrCodigo
	}
	if err != nil {
		return cloudstore.AparelhoPareado{}, sanitize("consumir o código de pareamento", err)
	}
	var email string
	err = tx.QueryRow(ctx, sqlContaDaTroca, userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.AparelhoPareado{}, cloudstore.ErrCodigo
	}
	if err != nil {
		return cloudstore.AparelhoPareado{}, sanitize("ler a conta do pareamento", err)
	}
	if t.Permite == nil || !t.Permite(email) {
		return cloudstore.AparelhoPareado{}, cloudstore.ErrCodigo
	}
	var deviceID string
	if err := tx.QueryRow(ctx, sqlGravarAparelho, cloudstore.NewUUIDv7(), userID, installID, nome, plataforma, versao, t.Agora).
		Scan(&deviceID); err != nil {
		return cloudstore.AparelhoPareado{}, sanitize("gravar o aparelho", err)
	}
	if _, err := tx.Exec(ctx, sqlApagarTokenDoAparelho, deviceID); err != nil {
		return cloudstore.AparelhoPareado{}, sanitize("apagar o token anterior", err)
	}
	if _, err := tx.Exec(ctx, sqlGravarToken, t.TokenHMAC, deviceID, t.Agora); err != nil {
		if code(err) == "23505" {
			return cloudstore.AparelhoPareado{}, cloudstore.ErrCodigo
		}
		return cloudstore.AparelhoPareado{}, sanitize("gravar o token", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.AparelhoPareado{}, sanitize("confirmar o pareamento", err)
	}
	return cloudstore.AparelhoPareado{DeviceID: deviceID, UserID: userID, Email: email}, nil
}

func (s *Store) ListarAparelhos(ctx context.Context, userID string, now time.Time) ([]cloudstore.Aparelho, error) {
	rows, err := s.pool.Query(ctx, sqlListarAparelhos, userID, now.Add(-cloudstore.TokenSemUso))
	if err != nil {
		return nil, sanitize("listar os aparelhos", err)
	}
	defer rows.Close()
	out := []cloudstore.Aparelho{}
	for rows.Next() {
		var a cloudstore.Aparelho
		if err := rows.Scan(&a.ID, &a.Nome, &a.Plataforma, &a.Versao, &a.ConectadoEm, &a.UltimoUso); err != nil {
			return nil, sanitize("listar os aparelhos", err)
		}
		a.ConectadoEm, a.UltimoUso = a.ConectadoEm.UTC(), a.UltimoUso.UTC()
		out = append(out, a)
	}
	return out, sanitize("listar os aparelhos", rows.Err())
}

func (s *Store) RevogarAparelho(ctx context.Context, userID, deviceID string, now time.Time) error {
	canon, ok := cloudstore.CanonicalUUID(deviceID)
	if !ok {
		return cloudstore.ErrAparelhoNaoEncontrado
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	tag, err := tx.Exec(ctx, sqlRevogarAparelho, userID, canon, now)
	if err != nil {
		return sanitize("revogar o aparelho", err)
	}
	if tag.RowsAffected() == 0 {
		return cloudstore.ErrAparelhoNaoEncontrado
	}
	if _, err := tx.Exec(ctx, sqlApagarTokenDoAparelho, canon); err != nil {
		return sanitize("apagar o token do aparelho", err)
	}
	return sanitize("confirmar a revogação", tx.Commit(ctx))
}
