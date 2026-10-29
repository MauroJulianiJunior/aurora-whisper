package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O Store como cloudstore.Accounts (ADR-019, fatia F3): o login pendente, o
// usuário, as sessões, o token do aparelho e o "apagar meus dados", sobre as
// tabelas da F1 (0001, 0002, 0010 a 0014). O relógio é sempre o do servidor
// (now, que os testes controlam), nunca o now() do Postgres.

var _ cloudstore.Accounts = (*Store)(nil)

const (
	sqlCriarLogin = `INSERT INTO pending_logins
		(state_hash, pre_hash, code_verifier, nonce, return_to, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	// Uso único: acha e marca no mesmo comando. Dois callbacks com o mesmo
	// state não passam os dois (o segundo espera a trava da linha e, já com
	// used_at, não casa o WHERE). O pre_hash está no WHERE: um callback com o
	// state certo e o __Host-pre errado não casa e não consome a linha.
	sqlConsumirLogin = `UPDATE pending_logins SET used_at = $2
		WHERE state_hash = $1 AND pre_hash = $3 AND used_at IS NULL AND expires_at > $2
		RETURNING pre_hash, code_verifier, nonce, return_to, created_at, expires_at`

	sqlUsuarioPorSub = `SELECT id, email, COALESCE(display_name, ''), disabled_at IS NOT NULL
		FROM users WHERE google_sub = $1 FOR UPDATE`
	sqlEmailDeOutro  = `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1 AND id <> $2)`
	sqlAtualizarUser = `UPDATE users SET email = $2, display_name = NULLIF($3, ''), updated_at = $4 WHERE id = $1`
	// sqlEmailEDeOutroSub diz se o e-mail já é de OUTRO sub (D7). Se o dono é o
	// mesmo sub, é um primeiro login concorrente que commitou depois da busca
	// por sub (duas abas no primeiro login): recomeçar, e não recusar.
	sqlEmailEDeOutroSub = `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1 AND google_sub <> $2)`
	sqlEmailEDoMesmoSub = `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1 AND google_sub = $2)`
	// ON CONFLICT DO NOTHING sem alvo cobre os dois UNIQUE (sub e e-mail): um
	// primeiro login concorrente com o mesmo sub (ou o mesmo e-mail) não
	// insere, e a transação recomeça e cai no caminho de quem já existe.
	sqlCriarUsuario = `INSERT INTO users (id, google_sub, email, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $5) ON CONFLICT DO NOTHING`
	sqlCriarContador = `INSERT INTO user_sync_counters (user_id, sync_epoch) VALUES ($1, $2)`

	sqlApagarSessaoPorHMAC = `DELETE FROM web_sessions WHERE token_hmac = $1`
	sqlCriarSessao         = `INSERT INTO web_sessions (id, user_id, token_hmac, label, created_at, last_seen_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $5, $6)`

	// Resolve e anota o uso no mesmo comando: o vencimento anda para
	// now + SemUso ($3), sem passar de created_at + Teto ($4, em segundos).
	sqlResolverSessao = `UPDATE web_sessions s
		SET last_seen_at = $2, expires_at = LEAST($3, s.created_at + make_interval(secs => $4))
		FROM users u
		WHERE s.token_hmac = $1 AND s.revoked_at IS NULL AND s.expires_at > $2
			AND s.created_at + make_interval(secs => $4) > $2
			AND u.id = s.user_id AND u.disabled_at IS NULL
		RETURNING s.id, s.user_id, s.label, s.created_at, s.last_seen_at, s.expires_at,
			u.email, COALESCE(u.display_name, '')`

	sqlListarSessoes = `SELECT id, user_id, label, created_at, last_seen_at, expires_at FROM web_sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2
		ORDER BY last_seen_at DESC, id`
	sqlDerrubarSessao = `DELETE FROM web_sessions WHERE user_id = $1 AND id = $2`

	// Resolve e anota o uso no mesmo comando. O token sem uso há
	// TokenSemUso ($3 é o limite: now - 180 dias), contado do último uso ou
	// da criação, não casa (ADR-019, D7; F6).
	sqlResolverToken = `UPDATE device_tokens t SET last_used_at = $2
		FROM devices d JOIN users u ON u.id = d.user_id
		WHERE t.token_hmac = $1 AND d.id = t.device_id AND d.revoked_at IS NULL AND u.disabled_at IS NULL
			AND COALESCE(t.last_used_at, t.created_at) > $3
		RETURNING d.id, d.user_id, d.name, d.platform, u.email, COALESCE(u.display_name, '')`

	// O primeiro comando do "apagar meus dados" trava o contador, como o
	// ponto único de gravação: um push ou uma edição do mesmo usuário espera,
	// e depois não acha mais o aparelho (a chave estrangeira recusa) nem o
	// ditado.
	sqlNovoEpoch = `UPDATE user_sync_counters
		SET sync_epoch = $2, tombstone_watermark = GREATEST(tombstone_watermark, last_seq)
		WHERE user_id = $1`

	sqlArrumarLogins  = `DELETE FROM pending_logins WHERE expires_at <= $1`
	sqlArrumarSessoes = `DELETE FROM web_sessions WHERE expires_at <= $1 OR revoked_at IS NOT NULL`
)

// apagarDaConta é a ordem do "apagar meus dados", depois do contador: quem
// aponta para o ditado e para o aparelho sai antes deles. Cada comando é
// filtrado pelo usuário ($1).
var apagarDaConta = []struct{ op, sql string }{
	{"apagar as correções", `DELETE FROM corrections WHERE user_id = $1`},
	// As notas (ADR-022, N7; as 0020 a 0025): a ligação do ditado com a nota
	// antes do ditado (ela aponta para ele), as ligações dos marcadores antes
	// das notas, e os eventos (que apontam para os aparelhos) antes dos
	// aparelhos, como todo o resto.
	{"apagar as ligações dos ditados com as notas", `DELETE FROM transcription_note_links WHERE user_id = $1`},
	{"apagar os marcadores das notas", `DELETE FROM note_labels WHERE user_id = $1`},
	// As imagens das notas (N10b; a 0026) antes das notas, que elas apontam (a
	// cascata as levaria; o DELETE explícito diz o que sai).
	{"apagar as imagens das notas", `DELETE FROM note_images WHERE user_id = $1`},
	{"apagar as notas", `DELETE FROM notes WHERE user_id = $1`},
	{"apagar os marcadores", `DELETE FROM labels WHERE user_id = $1`},
	{"apagar as lápides das notas", `DELETE FROM note_tombstones WHERE user_id = $1`},
	{"apagar os eventos das notas", `DELETE FROM note_sync_events WHERE user_id = $1`},
	{"apagar os ditados", `DELETE FROM transcriptions WHERE user_id = $1`},
	{"apagar as lápides", `DELETE FROM tombstones WHERE user_id = $1`},
	{"apagar os eventos", `DELETE FROM sync_events WHERE user_id = $1`},
	{"apagar as mutações", `DELETE FROM applied_mutations WHERE user_id = $1`},
	{"apagar o aprendido", `DELETE FROM learned_pairs WHERE user_id = $1`},
	{"apagar o dicionário", `DELETE FROM dictionary_entries WHERE user_id = $1`},
	{"apagar as preferências", `DELETE FROM user_settings WHERE user_id = $1`},
	// As três tabelas do ditado no celular (ADR-021; a 0017, a 0018 e a 0019):
	// as preferências, os aceites e as chaves da IA, só cifradas. Os aceites
	// saem inteiros, os que terminaram também: o apagar leva tudo o que é da
	// conta.
	//
	// A ORDEM importa: o aceite ANTES das chaves. O revogar (RevogarAceite)
	// trava a linha do aceite e depois apaga as chaves; se o apagar apagasse as
	// chaves primeiro e esperasse o aceite depois, os dois se esperariam (um
	// deadlock, SQLSTATE 40P01, que o Postgres desfaz abortando o apagar). Com
	// os dois na mesma ordem, quem chega depois espera o outro terminar.
	{"apagar as preferências do ditado", `DELETE FROM dictation_settings WHERE user_id = $1`},
	{"apagar os aceites do ditado", `DELETE FROM dictation_consents WHERE user_id = $1`},
	{"apagar as chaves da IA", `DELETE FROM ai_provider_keys WHERE user_id = $1`},
	{"apagar os pedidos de pareamento", `DELETE FROM pairing_requests WHERE approved_by = $1`},
	// Os tokens vão junto (device_tokens: ON DELETE CASCADE).
	{"apagar os aparelhos", `DELETE FROM devices WHERE user_id = $1`},
}

func (s *Store) CriarLoginPendente(ctx context.Context, p cloudstore.LoginPendente) error {
	returnTo := p.ReturnTo
	if returnTo == "" {
		returnTo = "/"
	}
	_, err := s.pool.Exec(ctx, sqlCriarLogin, p.StateHash, p.PreHash, p.CodeVerifier, p.Nonce, returnTo,
		p.CriadoEm, p.ExpiraEm)
	return sanitize("gravar o login pendente", err)
}

func (s *Store) ConsumirLoginPendente(ctx context.Context, stateHash, preHash []byte, now time.Time) (cloudstore.LoginPendente, error) {
	p := cloudstore.LoginPendente{StateHash: stateHash}
	err := s.pool.QueryRow(ctx, sqlConsumirLogin, stateHash, now, preHash).
		Scan(&p.PreHash, &p.CodeVerifier, &p.Nonce, &p.ReturnTo, &p.CriadoEm, &p.ExpiraEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.LoginPendente{}, cloudstore.ErrLoginPendente
	}
	if err != nil {
		return cloudstore.LoginPendente{}, sanitize("consumir o login pendente", err)
	}
	p.CriadoEm, p.ExpiraEm = p.CriadoEm.UTC(), p.ExpiraEm.UTC()
	return p, nil
}

// errRecomecar: um primeiro login concorrente inseriu antes; a transação
// recomeça.
var errRecomecar = errors.New("pgstore: recomeçar o login")

func (s *Store) EntrarComGoogle(ctx context.Context, id cloudstore.Identidade, now time.Time) (cloudstore.Usuario, error) {
	for range 3 {
		u, err := s.entrar(ctx, id, now)
		if errors.Is(err, errRecomecar) {
			continue
		}
		return u, err
	}
	return cloudstore.Usuario{}, errors.New("pgstore: o login concorrente não assentou")
}

func (s *Store) entrar(ctx context.Context, id cloudstore.Identidade, now time.Time) (cloudstore.Usuario, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cloudstore.Usuario{}, sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var u cloudstore.Usuario
	var disabled bool
	err = tx.QueryRow(ctx, sqlUsuarioPorSub, id.Sub).Scan(&u.ID, &u.Email, &u.Nome, &disabled)
	switch {
	case err == nil:
		if disabled {
			return cloudstore.Usuario{}, cloudstore.ErrContaDesativada
		}
		if u.Email != id.Email {
			var taken bool
			if err := tx.QueryRow(ctx, sqlEmailDeOutro, id.Email, u.ID).Scan(&taken); err != nil {
				return cloudstore.Usuario{}, sanitize("procurar o e-mail", err)
			}
			if taken {
				return cloudstore.Usuario{}, cloudstore.ErrEmailEmUso
			}
		}
		if _, err := tx.Exec(ctx, sqlAtualizarUser, u.ID, id.Email, id.Nome, now); err != nil {
			if code(err) == "23505" {
				return cloudstore.Usuario{}, cloudstore.ErrEmailEmUso
			}
			return cloudstore.Usuario{}, sanitize("atualizar o usuário", err)
		}
		u.Email, u.Nome = id.Email, id.Nome
	case errors.Is(err, pgx.ErrNoRows):
		var deOutro, doMesmo bool
		if err := tx.QueryRow(ctx, sqlEmailEDeOutroSub, id.Email, id.Sub).Scan(&deOutro); err != nil {
			return cloudstore.Usuario{}, sanitize("procurar o e-mail", err)
		}
		if deOutro {
			return cloudstore.Usuario{}, cloudstore.ErrSubDiferente
		}
		// Um primeiro login concorrente do mesmo sub commitou entre a busca
		// por sub (que não o via) e esta: recomeça e cai no caminho de quem
		// já existe. Sem isto, a segunda aba recebia "sub diferente".
		if err := tx.QueryRow(ctx, sqlEmailEDoMesmoSub, id.Email, id.Sub).Scan(&doMesmo); err != nil {
			return cloudstore.Usuario{}, sanitize("procurar o e-mail", err)
		}
		if doMesmo {
			return cloudstore.Usuario{}, errRecomecar
		}
		u = cloudstore.Usuario{ID: cloudstore.NewUUIDv7(), Email: id.Email, Nome: id.Nome}
		tag, err := tx.Exec(ctx, sqlCriarUsuario, u.ID, id.Sub, id.Email, id.Nome, now)
		if err != nil {
			return cloudstore.Usuario{}, sanitize("criar o usuário", err)
		}
		if tag.RowsAffected() != 1 {
			return cloudstore.Usuario{}, errRecomecar
		}
		if _, err := tx.Exec(ctx, sqlCriarContador, u.ID, cloudstore.NewUUIDv7()); err != nil {
			return cloudstore.Usuario{}, sanitize("criar o contador", err)
		}
	default:
		return cloudstore.Usuario{}, sanitize("procurar o usuário", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.Usuario{}, sanitize("confirmar o login", err)
	}
	return u, nil
}

func code(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func (s *Store) CriarSessao(ctx context.Context, n cloudstore.NovaSessao) (cloudstore.Sessao, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cloudstore.Sessao{}, sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if n.Substitui != nil {
		if _, err := tx.Exec(ctx, sqlApagarSessaoPorHMAC, n.Substitui); err != nil {
			return cloudstore.Sessao{}, sanitize("apagar a sessão anterior", err)
		}
	}
	sess := cloudstore.Sessao{
		ID: cloudstore.NewUUIDv7(), UserID: n.UserID, Rotulo: n.Rotulo,
		CriadaEm: n.Agora.UTC(), UltimoUso: n.Agora.UTC(), ExpiraEm: n.ExpiraEm.UTC(),
	}
	if _, err := tx.Exec(ctx, sqlCriarSessao, sess.ID, n.UserID, n.TokenHMAC, n.Rotulo, n.Agora, n.ExpiraEm); err != nil {
		return cloudstore.Sessao{}, sanitize("gravar a sessão", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.Sessao{}, sanitize("confirmar a sessão", err)
	}
	return sess, nil
}

func scanSessao(row pgx.Row, extra ...any) (cloudstore.Sessao, error) {
	var s cloudstore.Sessao
	dest := append([]any{&s.ID, &s.UserID, &s.Rotulo, &s.CriadaEm, &s.UltimoUso, &s.ExpiraEm}, extra...)
	if err := row.Scan(dest...); err != nil {
		return cloudstore.Sessao{}, err
	}
	s.CriadaEm, s.UltimoUso, s.ExpiraEm = s.CriadaEm.UTC(), s.UltimoUso.UTC(), s.ExpiraEm.UTC()
	return s, nil
}

func (s *Store) ResolverSessao(ctx context.Context, tokenHMAC []byte, now time.Time, p cloudstore.PoliticaDeSessao) (cloudstore.SessaoResolvida, error) {
	var r cloudstore.SessaoResolvida
	sess, err := scanSessao(s.pool.QueryRow(ctx, sqlResolverSessao, tokenHMAC, now, now.Add(p.SemUso), p.Teto.Seconds()),
		&r.Email, &r.Nome)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.SessaoResolvida{}, cloudstore.ErrCredencial
	}
	if err != nil {
		return cloudstore.SessaoResolvida{}, sanitize("resolver a sessão", err)
	}
	r.Sessao = sess
	return r, nil
}

func (s *Store) ListarSessoes(ctx context.Context, userID string, now time.Time) ([]cloudstore.Sessao, error) {
	rows, err := s.pool.Query(ctx, sqlListarSessoes, userID, now)
	if err != nil {
		return nil, sanitize("listar as sessões", err)
	}
	defer rows.Close()
	out := []cloudstore.Sessao{}
	for rows.Next() {
		sess, err := scanSessao(rows)
		if err != nil {
			return nil, sanitize("listar as sessões", err)
		}
		out = append(out, sess)
	}
	return out, sanitize("listar as sessões", rows.Err())
}

func (s *Store) DerrubarSessao(ctx context.Context, userID, sessionID string) error {
	canon, ok := cloudstore.CanonicalUUID(sessionID)
	if !ok {
		return cloudstore.ErrSessaoNaoEncontrada
	}
	tag, err := s.pool.Exec(ctx, sqlDerrubarSessao, userID, canon)
	if err != nil {
		return sanitize("derrubar a sessão", err)
	}
	if tag.RowsAffected() == 0 {
		return cloudstore.ErrSessaoNaoEncontrada
	}
	return nil
}

func (s *Store) DerrubarSessaoPorHMAC(ctx context.Context, tokenHMAC []byte) error {
	_, err := s.pool.Exec(ctx, sqlApagarSessaoPorHMAC, tokenHMAC)
	return sanitize("apagar a sessão", err)
}

func (s *Store) ResolverTokenDeAparelho(ctx context.Context, tokenHMAC []byte, now time.Time) (cloudstore.AparelhoResolvido, error) {
	var a cloudstore.AparelhoResolvido
	err := s.pool.QueryRow(ctx, sqlResolverToken, tokenHMAC, now, now.Add(-cloudstore.TokenSemUso)).
		Scan(&a.DeviceID, &a.UserID, &a.DeviceNome, &a.Plataforma, &a.Email, &a.Nome)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.AparelhoResolvido{}, cloudstore.ErrCredencial
	}
	if err != nil {
		return cloudstore.AparelhoResolvido{}, sanitize("resolver o token do aparelho", err)
	}
	return a, nil
}

func (s *Store) ApagarDadosDaConta(ctx context.Context, userID, keepSessionID string, _ time.Time) (string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return "", sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	epoch := cloudstore.NewUUIDv7()
	tag, err := tx.Exec(ctx, sqlNovoEpoch, userID, epoch)
	if err != nil {
		return "", sanitize("trocar o epoch", err)
	}
	if tag.RowsAffected() != 1 {
		return "", cloudstore.ErrUnknownUser
	}
	for _, step := range apagarDaConta {
		if _, err := tx.Exec(ctx, step.sql, userID); err != nil {
			return "", sanitize(step.op, err)
		}
	}
	keep, ok := cloudstore.CanonicalUUID(keepSessionID)
	if !ok {
		return "", errors.New("pgstore: a sessão que fica não é um UUID")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM web_sessions WHERE user_id = $1 AND id <> $2`, userID, keep); err != nil {
		return "", sanitize("apagar as outras sessões", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", sanitize("confirmar o apagar", err)
	}
	return epoch, nil
}

func (s *Store) Arrumar(ctx context.Context, now time.Time) (cloudstore.Arrumacao, error) {
	var a cloudstore.Arrumacao
	tag, err := s.pool.Exec(ctx, sqlArrumarLogins, now)
	if err != nil {
		return a, sanitize("arrumar os logins pendentes", err)
	}
	a.LoginsPendentes = tag.RowsAffected()
	tag, err = s.pool.Exec(ctx, sqlArrumarSessoes, now)
	if err != nil {
		return a, sanitize("arrumar as sessões", err)
	}
	a.Sessoes = tag.RowsAffected()
	tag, err = s.pool.Exec(ctx, sqlArrumarPedidos, now)
	if err != nil {
		return a, sanitize("arrumar os pedidos de pareamento", err)
	}
	a.Pedidos = tag.RowsAffected()
	return a, nil
}

// Ping confere que o banco responde (o /health/ready).
func (s *Store) Ping(ctx context.Context) error {
	return sanitize("ping", s.pool.Ping(ctx))
}
