package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// A cópia do dicionário de um PC no Postgres (ADR-023; a 0027). As regras (o
// tomar, o manter, o reenvio do mesmo SHA-256) são as de cloudstore.DecidirCopia,
// as mesmas do Store em memória; este arquivo é a transação e o SQL. Nada passa
// pelo contador: a cópia não sincroniza, não tem seq nem evento.
//
// A ordem das travas é sempre o aparelho e depois a cópia (o PUT, o
// RevogarAparelho e o "apagar meus dados" a seguem), para os três não se
// esperarem em círculo: o PUT trava a linha do PC (FOR SHARE, que também
// espera uma revogação em curso e a vê terminada) e depois a da cópia
// (FOR UPDATE); a revogação atualiza o aparelho e depois apaga a cópia; o
// apagar trava os aparelhos antes de apagar qualquer coisa (ApagarDadosDaConta).
//
// Nenhum erro e nenhum SQL daqui carrega o conteúdo, um termo ou o nome do
// aparelho; o conteúdo só sai por GetDictionaryCopy.

var _ cloudstore.DicionarioDaConta = (*Store)(nil)

const (
	// O estado da cópia, sem os bytes, só se o PC que é a fonte não foi
	// revogado (a revogação apaga a cópia; isto é a garantia a mais).
	sqlCopiaInfo = `SELECT c.device_id, d.name, c.terms, c.sha256, c.updated_at
		FROM dictionary_copies c
		JOIN devices d ON d.user_id = c.user_id AND d.id = c.device_id AND d.revoked_at IS NULL
		WHERE c.user_id = $1`
	// O estado e os bytes (o ditado no celular monta o Smart com eles).
	sqlCopiaComBytes = `SELECT c.device_id, d.name, c.terms, c.sha256, c.updated_at, c.content
		FROM dictionary_copies c
		JOIN devices d ON d.user_id = c.user_id AND d.id = c.device_id AND d.revoked_at IS NULL
		WHERE c.user_id = $1`
	// O PC que grava: existe, é da conta e não foi revogado. FOR SHARE: uma
	// revogação em curso termina antes, e ela vê o PUT que veio depois como de um
	// aparelho revogado (ErrUnknownDevice), em vez de deixar a cópia para trás.
	sqlCopiaAparelho = `SELECT name FROM devices WHERE user_id = $1 AND id = $2 AND revoked_at IS NULL FOR SHARE`
	// A cópia atual, travada, com o nome e a situação do PC que é a fonte. FOR
	// UPDATE OF c: trava só a linha da cópia, nunca a do aparelho.
	sqlCopiaTravada = `SELECT c.device_id, d.name, d.revoked_at IS NOT NULL, c.terms, c.sha256, c.updated_at
		FROM dictionary_copies c
		JOIN devices d ON d.user_id = c.user_id AND d.id = c.device_id
		WHERE c.user_id = $1
		FOR UPDATE OF c`
	// Gravar é trocar a linha da conta: o PC, os bytes, o SHA-256, os termos e a
	// hora. Uma cópia por conta (a chave primária é o user_id).
	sqlCopiaGravar = `INSERT INTO dictionary_copies (user_id, device_id, content, sha256, terms, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id) DO UPDATE SET
			device_id = EXCLUDED.device_id, content = EXCLUDED.content, sha256 = EXCLUDED.sha256,
			terms = EXCLUDED.terms, updated_at = EXCLUDED.updated_at`
	// Apagar só se este PC é a fonte.
	sqlCopiaApagar = `DELETE FROM dictionary_copies WHERE user_id = $1 AND device_id = $2`
	// A revogação do PC leva a cópia dele (RevogarAparelho, na mesma transação).
	sqlCopiaApagarDoAparelho = `DELETE FROM dictionary_copies WHERE user_id = $1 AND device_id = $2`
)

// Os bytes da cópia são TEXT no banco: o conteúdo tem de ser UTF-8 sem NUL. O
// handler confere antes (utf8.Valid); o banco recusaria o resto com um erro de
// codificação, e o SHA-256 só bate com o que ficou guardado se nada for trocado.

func (s *Store) DictionaryCopyInfo(ctx context.Context, userID string) (*cloudstore.CopiaDoDicionario, error) {
	var c cloudstore.CopiaDoDicionario
	err := s.pool.QueryRow(ctx, sqlCopiaInfo, userID).Scan(&c.DeviceID, &c.DeviceNome, &c.Termos, &c.SHA256, &c.AtualizadaEm)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, sanitize("ler a cópia do dicionário", err)
	}
	c.AtualizadaEm = c.AtualizadaEm.UTC()
	return &c, nil
}

func (s *Store) GetDictionaryCopy(ctx context.Context, userID string) (cloudstore.CopiaDoDicionario, []byte, bool, error) {
	var c cloudstore.CopiaDoDicionario
	var conteudo string
	err := s.pool.QueryRow(ctx, sqlCopiaComBytes, userID).
		Scan(&c.DeviceID, &c.DeviceNome, &c.Termos, &c.SHA256, &c.AtualizadaEm, &conteudo)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cloudstore.CopiaDoDicionario{}, nil, false, nil
	case err != nil:
		return cloudstore.CopiaDoDicionario{}, nil, false, sanitize("ler a cópia do dicionário", err)
	}
	c.AtualizadaEm = c.AtualizadaEm.UTC()
	return c, []byte(conteudo), true, nil
}

func (s *Store) PutDictionaryCopy(ctx context.Context, a cloudstore.Actor, n cloudstore.NovaCopia, modo cloudstore.ModoDaCopia, now time.Time) (cloudstore.ResultadoDaCopia, error) {
	if a.DeviceID == "" {
		return cloudstore.ResultadoDaCopia{}, cloudstore.ErrDeviceNotAllowed
	}
	deviceID, ok := cloudstore.CanonicalUUID(a.DeviceID)
	if !ok {
		return cloudstore.ResultadoDaCopia{}, cloudstore.ErrUnknownDevice
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cloudstore.ResultadoDaCopia{}, sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var nomeDeste string
	if err := tx.QueryRow(ctx, sqlCopiaAparelho, a.UserID, deviceID).Scan(&nomeDeste); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cloudstore.ResultadoDaCopia{}, cloudstore.ErrUnknownDevice
		}
		return cloudstore.ResultadoDaCopia{}, sanitize("ler o aparelho", err)
	}

	// A cópia atual; a de um PC revogado conta como nenhuma (e o tomar a troca).
	var atual *cloudstore.CopiaDoDicionario
	var c cloudstore.CopiaDoDicionario
	var revogado bool
	err = tx.QueryRow(ctx, sqlCopiaTravada, a.UserID).Scan(&c.DeviceID, &c.DeviceNome, &revogado, &c.Termos, &c.SHA256, &c.AtualizadaEm)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return cloudstore.ResultadoDaCopia{}, sanitize("ler a cópia do dicionário", err)
	case !revogado:
		c.AtualizadaEm = c.AtualizadaEm.UTC()
		atual = &c
	}

	grava, naoEra := cloudstore.DecidirCopia(atual, deviceID, n, modo)
	res := cloudstore.ResultadoDaCopia{Gravou: grava, NaoEraAFonte: naoEra, Atual: atual}
	if grava {
		agora := now.UTC()
		if _, err := tx.Exec(ctx, sqlCopiaGravar, a.UserID, deviceID, string(n.Conteudo), n.SHA256, n.Termos, agora); err != nil {
			return cloudstore.ResultadoDaCopia{}, sanitize("gravar a cópia do dicionário", err)
		}
		res.Atual = &cloudstore.CopiaDoDicionario{DeviceID: deviceID, DeviceNome: nomeDeste, Termos: n.Termos,
			SHA256: n.SHA256, AtualizadaEm: agora}
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.ResultadoDaCopia{}, sanitize("confirmar a cópia do dicionário", err)
	}
	return res, nil
}

func (s *Store) DeleteDictionaryCopy(ctx context.Context, a cloudstore.Actor) (bool, error) {
	if a.DeviceID == "" {
		return false, cloudstore.ErrDeviceNotAllowed
	}
	deviceID, ok := cloudstore.CanonicalUUID(a.DeviceID)
	if !ok {
		return false, nil
	}
	tag, err := s.pool.Exec(ctx, sqlCopiaApagar, a.UserID, deviceID)
	if err != nil {
		return false, sanitize("apagar a cópia do dicionário", err)
	}
	return tag.RowsAffected() == 1, nil
}
