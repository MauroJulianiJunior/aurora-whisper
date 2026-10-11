// Package pgstore é o cloudstore.Store sobre o PostgreSQL (ADR-019, fatia
// F2): o ponto único de gravação da nuvem e as leituras do histórico e do
// pull. As regras (o que gravar e o que responder) são as de
// internal/server/cloudstore, as mesmas do Store em memória; este pacote só
// decide COMO: a transação, a ordem das travas e o SQL.
//
// # A transação de gravação
//
// Toda escrita (CRUD, push, "excluir definitivamente", e o ditado criado na
// nuvem, Create, em criar.go) é uma transação curta em READ COMMITTED, cujo
// PRIMEIRO comando é
//
//	UPDATE user_sync_counters SET last_seq = last_seq + 1
//	WHERE user_id = $1 RETURNING last_seq, sync_epoch
//
// A trava da linha do contador fica até o commit: duas gravações do mesmo
// usuário se enfileiram, a ordem do seq é a ordem dos commits, e o pull nunca
// vê um seq maior antes de um menor. Depois vêm, nesta ordem (a ordem das
// travas é fixa, então não há deadlock): a entidade (FOR UPDATE), o
// sync_event com o mesmo seq e, no push, a applied_mutation.
//
// Quando a decisão não grava entidade (um reenvio, uma recusa, um gone, uma
// lixeira que já estava lá), o número volta: um UPDATE ... last_seq - 1 na
// mesma transação, ainda com a trava. Ninguém viu o número (a trava não
// soltou), então é o mesmo que o rollback; e a applied_mutation de uma recusa
// ou de um gone ainda é gravada, para o reenvio receber duplicate. Assim os
// seq de cada usuário são 1, 2, 3... sem buraco.
//
// O READ COMMITTED é pressuposto (ADR-019, "Para as fatias seguintes"): em
// REPEATABLE READ ou SERIALIZABLE, o segundo escritor do mesmo usuário
// receberia 40001 em vez de esperar a trava.
//
// O aprendido (F8c; ADR-019, "Decisões da F8") passa pelo mesmo ponto: a
// linha da learned_pairs é lida FOR UPDATE por (user_id, id), a decisão é a
// cloudstore.DecideLearned (a mesma do Store em memória), e o evento é
// learned upsert, o desfazer inclusive (a linha desfeita continua, com
// undone_at). As datas da linha (created_at, updated_at, undone_at) vêm do
// relógio de quem chama, nunca do DEFAULT now() do banco.
//
// # O pull
//
// Lê o epoch, os ditados, as lápides e o aprendido numa transação somente
// leitura em REPEATABLE READ: um snapshot só. Consultas soltas em READ
// COMMITTED poderiam ver, de uma para a outra, uma gravação confirmada no
// meio, e o cursor pularia a mudança.
//
// # Erros
//
// Nenhum erro deste pacote carrega texto ditado: o erro do Postgres traz, no
// Detail ("Failing row contains (...)") e às vezes na Message, os valores da
// linha. DBError guarda só a operação, o SQLSTATE, a constraint e a coluna.
// Este pacote não loga nada.
package pgstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Store é o cloudstore.Store sobre um pool do Postgres. Seguro para uso
// concorrente (o pool é).
type Store struct {
	pool *pgxpool.Pool
}

var _ cloudstore.Store = (*Store)(nil)

// New monta o Store sobre um pool já aberto. Quem abre e fecha o pool é quem
// sobe o servidor (F3); o banco precisa ter as migrations aplicadas.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// DBError é um erro do Postgres sem os valores da linha: a operação, o
// SQLSTATE, a constraint e a coluna. Nunca o Message nem o Detail, que podem
// trazer texto ditado.
type DBError struct {
	Op         string
	Code       string
	Constraint string
	Column     string
}

// Operacao e SQLState deixam quem loga sem importar este pacote (o job de
// retenção, internal/server) dizer o erro só pela operação e pelo código.
func (e *DBError) Operacao() string { return e.Op }

func (e *DBError) SQLState() string { return e.Code }

func (e *DBError) Error() string {
	s := "pgstore: " + e.Op + ": SQLSTATE " + e.Code
	if e.Constraint != "" {
		s += " (" + e.Constraint + ")"
	}
	if e.Column != "" {
		s += " coluna " + e.Column
	}
	return s
}

// sanitize troca um erro do Postgres por um DBError. Os outros erros (rede,
// contexto, o pool fechado) não carregam valores de linha e passam com a
// operação na frente.
func sanitize(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return &DBError{Op: op, Code: pgErr.Code, Constraint: pgErr.ConstraintName, Column: pgErr.ColumnName}
	}
	return fmt.Errorf("pgstore: %s: %w", op, err)
}

// As colunas de transcriptions, na ordem de scanTranscription e de
// insertArgs.
var transcriptionCols = []string{
	"id", "user_id", "device_id", "source_platform", "raw_text", "final_text", "mode", "language",
	"asr_provider", "asr_model", "duration_ms", "processing_ms", "confidence", "status",
	"created_at", "updated_at", "deleted_at", "purge_at",
	"version", "text_version", "trash_version", "text_device_id", "trash_device_id", "last_seq",
	"conflict_final_text", "conflict_device_id", "conflict_at",
}

func colsWith(prefix string) string {
	return joinCols(transcriptionCols, prefix)
}

// joinCols junta as colunas, cada uma com o prefixo da tabela ("t.id"), ou
// sem ele.
func joinCols(cols []string, prefix string) string {
	if prefix == "" {
		return strings.Join(cols, ", ")
	}
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = prefix + "." + c
	}
	return strings.Join(out, ", ")
}

// As colunas de learned_pairs (a 0016, que a F8 não muda), na ordem de
// scanLearned e de sqlInsertLearned.
var learnedCols = []string{
	"id", "user_id", "variant", "normalized_variant", "term", "restricted", "domain",
	"created_at", "updated_at", "undone_at", "version", "last_seq",
}

var (
	sqlSelectOne = "SELECT " + colsWith("") + " FROM transcriptions WHERE user_id = $1 AND id = $2"
	sqlInsert    = "INSERT INTO transcriptions (" + colsWith("") + ") VALUES (" + placeholders(len(transcriptionCols)) + ")"

	sqlSelectLearned = "SELECT " + joinCols(learnedCols, "") + " FROM learned_pairs WHERE user_id = $1 AND id = $2"
	sqlInsertLearned = "INSERT INTO learned_pairs (" + joinCols(learnedCols, "") + ") VALUES (" + placeholders(len(learnedCols)) + ")"
)

const (
	sqlNextSeq = `UPDATE user_sync_counters SET last_seq = last_seq + 1
		WHERE user_id = $1 RETURNING last_seq, sync_epoch`
	sqlGiveBackSeq = `UPDATE user_sync_counters SET last_seq = last_seq - 1 WHERE user_id = $1`

	sqlUpdate = `UPDATE transcriptions SET
		final_text = $3, deleted_at = $4, purge_at = $5, updated_at = $6,
		version = $7, text_version = $8, trash_version = $9, last_seq = $10,
		conflict_final_text = $11, conflict_device_id = $12, conflict_at = $13,
		text_device_id = $15, trash_device_id = $16
		WHERE user_id = $1 AND id = $2 AND version = $14`

	sqlEvent = `INSERT INTO sync_events
		(user_id, seq, entity_type, entity_id, operation, entity_version, device_id, changed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	sqlTombstoneExists = `SELECT EXISTS (SELECT 1 FROM tombstones
		WHERE user_id = $1 AND entity_type = 'transcription' AND entity_id = $2)`

	sqlApplied = `SELECT entity_type, entity_id, device_id, operation, status, result_version, merged, result_code
		FROM applied_mutations WHERE user_id = $1 AND mutation_id = $2`

	sqlRecordApplied = `INSERT INTO applied_mutations
		(user_id, mutation_id, device_id, entity_type, entity_id, operation, applied_at, status, result_version, merged, result_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	// O UPDATE do aprendido não toca normalized_variant nem created_at: a
	// identidade da linha é a do id (a chave da variante), e a grafia da
	// variante pode mudar ("SF" e "sf" são o mesmo id). Assim o UPDATE nunca
	// alcança o UNIQUE (user_id, normalized_variant).
	sqlUpdateLearned = `UPDATE learned_pairs SET
		variant = $3, term = $4, restricted = $5, domain = $6,
		updated_at = $7, undone_at = $8, version = $9, last_seq = $10
		WHERE user_id = $1 AND id = $2 AND version = $11`

	// A chave conferida antes do INSERT: outra linha do usuário com a mesma
	// normalized_variant (e, como a leitura pelo id não achou nada, com outro
	// id). Toda linha gravada por este código tem id = v5(nome + a chave que
	// ela guarda) (dictionary.IDDoPar, conferido pelas pré-checagens): a mesma
	// chave dá sempre o mesmo id, e a leitura pelo id já a teria achado. Então
	// isto só acontece quando o id da linha não vem da chave pelo nome de hoje:
	// o nome ou o namespace do v5 mudou (o "v2" que dictionary/par.go prevê
	// para uma regra nova de chave), ou a linha foi posta à mão. Uma regra de
	// chave mudada SEM trocar o nome não é pega aqui: dá outra chave e outro
	// id, e o INSERT grava uma segunda linha para a mesma variante, em
	// silêncio (o risco "a estabilidade do id" da ADR-019, que os vetores
	// congelados de par_test.go avisam).
	sqlLearnedKeyTaken = `SELECT EXISTS (SELECT 1 FROM learned_pairs
		WHERE user_id = $1 AND normalized_variant = $2)`

	sqlDelete    = `DELETE FROM transcriptions WHERE user_id = $1 AND id = $2`
	sqlTombstone = `INSERT INTO tombstones (user_id, entity_type, entity_id, reason, dictated_at, seq)
		VALUES ($1, 'transcription', $2, 'apagado', $3, $4)`

	sqlEpoch = `SELECT sync_epoch FROM user_sync_counters WHERE user_id = $1`
)

func placeholders(n int) string {
	p := make([]string, n)
	for i := range p {
		p[i] = "$" + strconv.Itoa(i+1)
	}
	return strings.Join(p, ", ")
}

// scanTranscription lê uma linha com as colunas de transcriptionCols, mais
// os destinos extras no fim.
func scanTranscription(row pgx.Row, extra ...any) (cloudstore.Transcription, error) {
	var t cloudstore.Transcription
	var cText, cDevice *string
	var cAt *time.Time
	dest := []any{
		&t.ID, &t.UserID, &t.DeviceID, &t.SourcePlatform, &t.RawText, &t.FinalText, &t.Mode, &t.Language,
		&t.ASRProvider, &t.ASRModel, &t.DurationMs, &t.ProcessingMs, &t.Confidence, &t.Status,
		&t.CreatedAt, &t.UpdatedAt, &t.DeletedAt, &t.PurgeAt,
		&t.Version, &t.TextVersion, &t.TrashVersion, &t.TextDeviceID, &t.TrashDeviceID, &t.LastSeq,
		&cText, &cDevice, &cAt,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return cloudstore.Transcription{}, err
	}
	// O pgx devolve timestamptz no fuso local; a nuvem fala em UTC.
	t.CreatedAt, t.UpdatedAt = t.CreatedAt.UTC(), t.UpdatedAt.UTC()
	t.DeletedAt, t.PurgeAt = utc(t.DeletedAt), utc(t.PurgeAt)
	if cText != nil && cAt != nil {
		t.Conflict = &cloudstore.Conflict{FinalText: *cText, DeviceID: cDevice, At: cAt.UTC()}
	}
	return t, nil
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func conflictArgs(c *cloudstore.Conflict) (text *string, device *string, at *time.Time) {
	if c == nil {
		return nil, nil, nil
	}
	ft, a := c.FinalText, c.At
	return &ft, c.DeviceID, &a
}

func insertArgs(t cloudstore.Transcription) []any {
	cText, cDevice, cAt := conflictArgs(t.Conflict)
	return []any{
		t.ID, t.UserID, t.DeviceID, t.SourcePlatform, t.RawText, t.FinalText, t.Mode, t.Language,
		t.ASRProvider, t.ASRModel, t.DurationMs, t.ProcessingMs, t.Confidence, t.Status,
		t.CreatedAt, t.UpdatedAt, t.DeletedAt, t.PurgeAt,
		t.Version, t.TextVersion, t.TrashVersion, t.TextDeviceID, t.TrashDeviceID, t.LastSeq,
		cText, cDevice, cAt,
	}
}

// gravar é o ponto único de gravação: abre a transação, sobe o contador
// (primeiro comando), chama fn com o seq e confirma. fn devolve wrote=false
// quando não gravou entidade nem evento; aí o número volta antes do commit.
// Um erro de fn desfaz tudo (o rollback devolve o número também).
func (s *Store) gravar(ctx context.Context, userID string, fn func(tx pgx.Tx, seq int64, epoch string) (wrote bool, err error)) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var seq int64
	var epoch string
	if err := tx.QueryRow(ctx, sqlNextSeq, userID).Scan(&seq, &epoch); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cloudstore.ErrUnknownUser
		}
		return sanitize("subir o contador", err)
	}
	wrote, err := fn(tx, seq, epoch)
	if err != nil {
		return err
	}
	if !wrote {
		if _, err := tx.Exec(ctx, sqlGiveBackSeq, userID); err != nil {
			return sanitize("devolver o número", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return sanitize("confirmar", err)
	}
	return nil
}

// readForUpdate lê o ditado e trava a linha. nil quando não existe.
func readForUpdate(ctx context.Context, tx pgx.Tx, userID, id string) (*cloudstore.Transcription, error) {
	t, err := scanTranscription(tx.QueryRow(ctx, sqlSelectOne+" FOR UPDATE", userID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, sanitize("ler o ditado", err)
	}
	return &t, nil
}

// writeEvent grava o sync_event da mudança, com o tipo da entidade
// (transcription ou learned) e o seq da transação.
func writeEvent(ctx context.Context, tx pgx.Tx, userID string, seq int64, entityType, id, op string, version int64, device *string, now time.Time) error {
	_, err := tx.Exec(ctx, sqlEvent, userID, seq, entityType, id, op, version, device, now)
	return sanitize("gravar o evento", err)
}

// scanLearned lê uma linha com as colunas de learnedCols, mais os destinos
// extras no fim.
func scanLearned(row pgx.Row, extra ...any) (cloudstore.Learned, error) {
	var l cloudstore.Learned
	dest := []any{
		&l.ID, &l.UserID, &l.Variant, &l.NormalizedVariant, &l.Term, &l.Restricted, &l.Domain,
		&l.CreatedAt, &l.UpdatedAt, &l.UndoneAt, &l.Version, &l.LastSeq,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return cloudstore.Learned{}, err
	}
	l.CreatedAt, l.UpdatedAt = l.CreatedAt.UTC(), l.UpdatedAt.UTC()
	l.UndoneAt = utc(l.UndoneAt)
	return l, nil
}

// readLearnedForUpdate lê o aprendido por (user_id, id) e trava a linha. nil
// quando não existe.
func readLearnedForUpdate(ctx context.Context, tx pgx.Tx, userID, id string) (*cloudstore.Learned, error) {
	l, err := scanLearned(tx.QueryRow(ctx, sqlSelectLearned+" FOR UPDATE", userID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, sanitize("ler o aprendido", err)
	}
	return &l, nil
}

// putLearned grava o aprendido novo (insert) ou a versão nova sobre
// prevVersion (update), e o evento, com o seq. O evento é sempre upsert, o
// desfazer inclusive: o CHECK da 0008 só aceita upsert e tombstone, e o
// aprendido não tem lápide.
func putLearned(ctx context.Context, tx pgx.Tx, l cloudstore.Learned, insert bool, prevVersion int64, seq int64, a cloudstore.Actor, now time.Time) error {
	l.LastSeq = seq
	if insert {
		if _, err := tx.Exec(ctx, sqlInsertLearned, l.ID, l.UserID, l.Variant, l.NormalizedVariant, l.Term,
			l.Restricted, l.Domain, l.CreatedAt, l.UpdatedAt, l.UndoneAt, l.Version, l.LastSeq); err != nil {
			return sanitize("gravar o aprendido", err)
		}
	} else {
		tag, err := tx.Exec(ctx, sqlUpdateLearned, l.UserID, l.ID, l.Variant, l.Term, l.Restricted, l.Domain,
			l.UpdatedAt, l.UndoneAt, l.Version, l.LastSeq, prevVersion)
		if err != nil {
			return sanitize("gravar o aprendido", err)
		}
		if tag.RowsAffected() != 1 {
			// Como no ditado: não acontece com a trava do contador e o FOR
			// UPDATE, e é melhor falhar do que gravar o evento de uma
			// mudança que não houve.
			return errors.New("pgstore: gravar o aprendido: a versão mudou durante a transação")
		}
	}
	return writeEvent(ctx, tx, l.UserID, seq, cloudstore.EntityLearned, l.ID, cloudstore.OpUpsert, l.Version, a.Device(), now)
}

// put grava a linha nova (insert) ou a versão nova sobre prevVersion
// (update), e o evento, com o seq.
func put(ctx context.Context, tx pgx.Tx, t cloudstore.Transcription, insert bool, prevVersion int64, seq int64, a cloudstore.Actor, now time.Time) error {
	t.LastSeq = seq
	if insert {
		if _, err := tx.Exec(ctx, sqlInsert, insertArgs(t)...); err != nil {
			return sanitize("gravar o ditado", err)
		}
	} else {
		cText, cDevice, cAt := conflictArgs(t.Conflict)
		tag, err := tx.Exec(ctx, sqlUpdate, t.UserID, t.ID,
			t.FinalText, t.DeletedAt, t.PurgeAt, t.UpdatedAt,
			t.Version, t.TextVersion, t.TrashVersion, t.LastSeq,
			cText, cDevice, cAt, prevVersion, t.TextDeviceID, t.TrashDeviceID)
		if err != nil {
			return sanitize("gravar o ditado", err)
		}
		if tag.RowsAffected() != 1 {
			// Não acontece com a trava do contador e o FOR UPDATE; se
			// acontecer, é melhor falhar do que gravar o evento de uma
			// mudança que não houve.
			return errors.New("pgstore: gravar o ditado: a versão mudou durante a transação")
		}
	}
	return writeEvent(ctx, tx, t.UserID, seq, cloudstore.EntityTranscription, t.ID, cloudstore.OpUpsert, t.Version, a.Device(), now)
}

func (s *Store) Get(ctx context.Context, userID, id string) (cloudstore.Transcription, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.Transcription{}, cloudstore.ErrNotFound
	}
	t, err := scanTranscription(s.pool.QueryRow(ctx, sqlSelectOne, userID, canon))
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.Transcription{}, cloudstore.ErrNotFound
	}
	if err != nil {
		return cloudstore.Transcription{}, sanitize("ler o ditado", err)
	}
	return t, nil
}

func (s *Store) Edit(ctx context.Context, a cloudstore.Actor, id string, baseVersion int64, finalText string, now time.Time) (cloudstore.Transcription, error) {
	now = cloudstore.Normalize(now)
	return s.change(ctx, a, id, now, func(cur cloudstore.Transcription) (cloudstore.Transcription, error) {
		return cloudstore.Edit(cur, a.Device(), baseVersion, finalText, now)
	})
}

func (s *Store) ResolveConflict(ctx context.Context, a cloudstore.Actor, id string, baseVersion int64, escolha string, now time.Time) (cloudstore.Transcription, error) {
	now = cloudstore.Normalize(now)
	return s.change(ctx, a, id, now, func(cur cloudstore.Transcription) (cloudstore.Transcription, error) {
		return cloudstore.ResolveConflict(cur, a.Device(), baseVersion, escolha, now)
	})
}

// change grava, pelo ponto único de gravação, a mudança que rule decide sobre
// a linha travada (a edição e a escolha do selo), ou nada, quando rule recusa.
func (s *Store) change(ctx context.Context, a cloudstore.Actor, id string, now time.Time,
	rule func(cloudstore.Transcription) (cloudstore.Transcription, error)) (cloudstore.Transcription, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.Transcription{}, cloudstore.ErrNotFound
	}
	var out cloudstore.Transcription
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		cur, err := readForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if cur == nil {
			return false, cloudstore.ErrNotFound
		}
		next, err := rule(*cur)
		if err != nil {
			return false, err
		}
		if err := put(ctx, tx, next, false, cur.Version, seq, a, now); err != nil {
			return false, err
		}
		next.LastSeq = seq
		out = next
		return true, nil
	})
	return out, err
}

func (s *Store) Trash(ctx context.Context, a cloudstore.Actor, id string, now time.Time) (cloudstore.Transcription, error) {
	return s.toggle(ctx, a, id, now, cloudstore.Trash)
}

func (s *Store) Restore(ctx context.Context, a cloudstore.Actor, id string, now time.Time) (cloudstore.Transcription, error) {
	return s.toggle(ctx, a, id, now, cloudstore.Restore)
}

func (s *Store) toggle(ctx context.Context, a cloudstore.Actor, id string, now time.Time,
	rule func(cloudstore.Transcription, *string, time.Time) (cloudstore.Transcription, bool)) (cloudstore.Transcription, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.Transcription{}, cloudstore.ErrNotFound
	}
	now = cloudstore.Normalize(now)
	var out cloudstore.Transcription
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		cur, err := readForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if cur == nil {
			return false, cloudstore.ErrNotFound
		}
		next, changed := rule(*cur, a.Device(), now)
		if !changed {
			out = *cur
			return false, nil
		}
		if err := put(ctx, tx, next, false, cur.Version, seq, a, now); err != nil {
			return false, err
		}
		next.LastSeq = seq
		out = next
		return true, nil
	})
	return out, err
}

func (s *Store) Purge(ctx context.Context, a cloudstore.Actor, id string, now time.Time) error {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.ErrNotFound
	}
	now = cloudstore.Normalize(now)
	return s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		cur, err := readForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if cur == nil {
			return false, cloudstore.ErrNotFound
		}
		if err := cloudstore.CheckPurge(*cur); err != nil {
			return false, err
		}
		// Fora da janela (há mais de FolgaDaLapide), a lápide nasceria
		// vencida: só apaga, e o número volta (F9).
		lapide := cloudstore.DeixaLapide(cur.CreatedAt, now)
		if err := apagarDeVez(ctx, tx, *cur, lapide, seq, a.Device(), now); err != nil {
			return false, err
		}
		return lapide, nil
	})
}

// apagarDeVez apaga o ditado travado de vez: o "excluir definitivamente" do
// CRUD e a retenção (retencao.go). Com lapide, grava a lápide (dictated_at = o
// created_at do ditado) e o evento tombstone com o seq, a versão seguinte e o
// aparelho de quem apagou (nulo na retenção e no celular); sem ela, só a linha
// sai, e quem chama devolve o número (wrote=false no gravar).
func apagarDeVez(ctx context.Context, tx pgx.Tx, cur cloudstore.Transcription, lapide bool, seq int64, device *string, now time.Time) error {
	if _, err := tx.Exec(ctx, sqlDelete, cur.UserID, cur.ID); err != nil {
		return sanitize("apagar o ditado", err)
	}
	if !lapide {
		return nil
	}
	if _, err := tx.Exec(ctx, sqlTombstone, cur.UserID, cur.ID, cur.CreatedAt, seq); err != nil {
		return sanitize("gravar a lápide", err)
	}
	return writeEvent(ctx, tx, cur.UserID, seq, cloudstore.EntityTranscription, cur.ID, cloudstore.OpTombstone, cur.Version+1, device, now)
}

func (s *Store) Push(ctx context.Context, a cloudstore.Actor, muts []cloudstore.Mutation, now time.Time) (cloudstore.PushResponse, error) {
	if a.DeviceID == "" {
		return cloudstore.PushResponse{}, cloudstore.ErrNoDevice
	}
	now = cloudstore.Normalize(now)
	resp := cloudstore.PushResponse{Results: make([]cloudstore.Result, 0, len(muts))}
	for _, m := range muts {
		r, epoch, err := s.pushOne(ctx, a, m, now)
		if err != nil {
			// As anteriores já estão confirmadas: o PC reenvia o lote com os
			// mesmos mutation_id e recebe duplicate nelas.
			return cloudstore.PushResponse{}, err
		}
		if epoch != "" {
			resp.Epoch = epoch
		}
		resp.Results = append(resp.Results, r)
	}
	if resp.Epoch == "" {
		if err := s.pool.QueryRow(ctx, sqlEpoch, a.UserID).Scan(&resp.Epoch); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return cloudstore.PushResponse{}, cloudstore.ErrUnknownUser
			}
			return cloudstore.PushResponse{}, sanitize("ler o epoch", err)
		}
	}
	return resp, nil
}

// pushOne aplica uma mutação na sua transação. Devolve também o epoch lido
// pelo contador ("" quando a recusa nem abriu transação).
func (s *Store) pushOne(ctx context.Context, a cloudstore.Actor, m cloudstore.Mutation, now time.Time) (cloudstore.Result, string, error) {
	code, recordable := cloudstore.CheckMutation(m)
	if code != "" && !recordable {
		return cloudstore.Rejected(m, code).Result, "", nil
	}
	mutationID, _ := cloudstore.CanonicalUUID(m.MutationID)
	entityID, _ := cloudstore.CanonicalUUID(m.EntityID)
	var res cloudstore.Result
	var epoch string
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, ep string) (bool, error) {
		epoch = ep
		prev, found, err := readApplied(ctx, tx, a.UserID, mutationID)
		if err != nil {
			return false, err
		}
		if found {
			// A operação da primeira vez vem da coluna da 0009 (F8c): o
			// reenvio com outra é mutacao_reusada.
			res = cloudstore.Duplicate(prev, m, a)
			return false, nil
		}
		var out cloudstore.Outcome
		var wrote bool
		switch {
		case code != "":
			out = cloudstore.Rejected(m, code)
		case m.EntityType == cloudstore.EntityLearned:
			out, wrote, err = pushLearned(ctx, tx, a, m, entityID, seq, now)
		default:
			out, wrote, err = pushTranscription(ctx, tx, a, m, entityID, seq, now)
		}
		if err != nil {
			return false, err
		}
		if err := recordApplied(ctx, tx, a, mutationID, out.Record(m, a), now); err != nil {
			return false, err
		}
		res = out.Result
		return wrote, nil
	})
	return res, epoch, err
}

// pushTranscription decide e grava, na transação do push, uma mutação de
// ditado: a linha FOR UPDATE, a lápide, cloudstore.DecidePush. wrote diz se
// gravou a entidade e o evento (com seq).
func pushTranscription(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, m cloudstore.Mutation, entityID string,
	seq int64, now time.Time) (out cloudstore.Outcome, wrote bool, err error) {
	cur, err := readForUpdate(ctx, tx, a.UserID, entityID)
	if err != nil {
		return cloudstore.Outcome{}, false, err
	}
	var tomb bool
	if err := tx.QueryRow(ctx, sqlTombstoneExists, a.UserID, entityID).Scan(&tomb); err != nil {
		return cloudstore.Outcome{}, false, sanitize("ler a lápide", err)
	}
	out = cloudstore.DecidePush(m, cur, tomb, a, now)
	if out.Write == cloudstore.WriteNone {
		return out, false, nil
	}
	var prevVersion int64
	if cur != nil {
		prevVersion = cur.Version
	}
	if err := put(ctx, tx, out.Next, out.Write == cloudstore.WriteInsert, prevVersion, seq, a, now); err != nil {
		return cloudstore.Outcome{}, false, err
	}
	if out.Result.State != nil {
		st := out.Next
		st.LastSeq = seq
		out.Result.State = &st
	}
	return out, true, nil
}

// pushLearned decide e grava, na transação do push, uma mutação do aprendido
// (F8c; ADR-019, "Decisões da F8"): a linha da learned_pairs FOR UPDATE por
// (user_id, id) e cloudstore.DecideLearned, a mesma decisão do Store em
// memória (as pré-checagens, a tabela da nuvem, o state do conflict, que é a
// linha lida). wrote diz se gravou a linha e o evento (com seq).
//
// Antes do INSERT, a chave (normalized_variant) é conferida: se outra linha do
// usuário já a tem, com outro id, a mutação é recusada com campo_invalido, sem
// gravar entidade nem gastar seq (a recusa fica em applied_mutations, como
// toda recusa). Como o id é o v5 do nome mais a chave, isso só acontece
// quando a linha que está lá tem um id que não vem da chave pelo nome de hoje:
// um nome ou namespace do v5 trocado (o "v2" de dictionary/par.go), ou uma
// linha posta à mão (sqlLearnedKeyTaken). Sem a conferência, o INSERT bateria
// no UNIQUE (23505), cujo DETAIL poria a variante no log do Postgres, e a
// resposta seria um 500 que o PC reenviaria sem fim. A consequência para o
// "v2": todo par já sincronizado cuja chave não muda receberia campo_invalido
// para sempre; o "v2" precisa vir com uma migração que regrave o id das
// linhas antes de um PC mandar ids v2 (decisão do Mauro, pendente). Uma regra
// de chave mudada sem trocar o nome NÃO é pega: outra chave, outro id, e uma
// segunda linha em silêncio. O Store em memória não tem esse caso: as linhas
// dele são todas do mesmo nome.
func pushLearned(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, m cloudstore.Mutation, entityID string,
	seq int64, now time.Time) (out cloudstore.Outcome, wrote bool, err error) {
	cur, err := readLearnedForUpdate(ctx, tx, a.UserID, entityID)
	if err != nil {
		return cloudstore.Outcome{}, false, err
	}
	out = cloudstore.DecideLearned(m, cur, a, now)
	switch out.Write {
	case cloudstore.WriteNone:
		return out, false, nil
	case cloudstore.WriteInsert:
		var taken bool
		if err := tx.QueryRow(ctx, sqlLearnedKeyTaken, a.UserID, out.NextLearned.NormalizedVariant).Scan(&taken); err != nil {
			return cloudstore.Outcome{}, false, sanitize("conferir a chave do aprendido", err)
		}
		if taken {
			return cloudstore.Rejected(m, cloudstore.CodeInvalidField), false, nil
		}
	}
	var prevVersion int64
	if cur != nil {
		prevVersion = cur.Version
	}
	if err := putLearned(ctx, tx, out.NextLearned, out.Write == cloudstore.WriteInsert, prevVersion, seq, a, now); err != nil {
		return cloudstore.Outcome{}, false, err
	}
	return out, true, nil
}

func readApplied(ctx context.Context, tx pgx.Tx, userID, mutationID string) (cloudstore.Applied, bool, error) {
	var p cloudstore.Applied
	var status string
	var version *int64
	var op, code *string
	err := tx.QueryRow(ctx, sqlApplied, userID, mutationID).Scan(&p.EntityType, &p.EntityID, &p.DeviceID, &op, &status, &version, &p.Merged, &code)
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.Applied{}, false, nil
	}
	if err != nil {
		return cloudstore.Applied{}, false, sanitize("ler a mutação aplicada", err)
	}
	p.Status = cloudstore.Status(status)
	if op != nil {
		p.Operation = *op
	}
	if version != nil {
		p.Version = *version
	}
	if code != nil {
		p.Code = *code
	}
	return p, true, nil
}

func recordApplied(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, mutationID string, p cloudstore.Applied, now time.Time) error {
	var version *int64
	if p.Version > 0 {
		v := p.Version
		version = &v
	}
	// A operação como cloudstore.RecordedOperation a deixa (upsert, delete
	// ou vazia), e vazia é nula: a coluna nunca guarda o que o cliente
	// mandou fora da lista.
	var op *string
	if p.Operation != "" {
		o := p.Operation
		op = &o
	}
	var code *string
	if p.Code != "" {
		c := p.Code
		code = &c
	}
	_, err := tx.Exec(ctx, sqlRecordApplied, a.UserID, mutationID, a.DeviceID, p.EntityType, p.EntityID,
		op, now, string(p.Status), version, p.Merged, code)
	return sanitize("gravar a mutação aplicada", err)
}
