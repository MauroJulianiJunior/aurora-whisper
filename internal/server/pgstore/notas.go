package pgstore

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// A nota e o marcador de nota no Postgres (ADR-022, N7; as 0020 a 0024). As
// regras são as de cloudstore (DecideNote e DecideLabel), as mesmas do Store
// em memória; este arquivo é a transação e o SQL.
//
// A gravação passa pelo mesmo ponto único do ditado (gravar, store.go): o
// contador primeiro, depois a linha FOR UPDATE (a da nota ou a do marcador), a
// lápide, as ligações da nota com os marcadores (note_labels), o evento com o
// mesmo seq e, por último, a applied_mutation (pushOne). A ordem das travas é
// sempre essa, e o contador travado serializa tudo o que o mesmo usuário grava:
// o apagar de um marcador, que tira as ligações de várias notas, não disputa
// linha com o push de uma nota.
//
// O evento vai para a note_sync_events, e a lápide para a note_tombstones: o
// CHECK do entity_type da sync_events (0008) e o da tombstones (0015) não
// aceitam tipo novo, e uma migration só aditiva não os alarga (ADR-021, "As
// migrations").
//
// Nenhum erro deste arquivo carrega texto (o título, o corpo, o nome): o
// sanitize guarda só a operação, o SQLSTATE, a constraint e a coluna.

// As colunas de notes (a 0021), na ordem de scanNote e de noteArgs.
var noteCols = []string{
	"id", "user_id", "titulo", "corpo", "cor", "fixada", "arquivada", "posicao", "ditado_de_origem",
	"created_at", "updated_at", "deleted_at", "purge_at",
	"version", "content_version", "trash_version", "content_device_id", "trash_device_id", "last_seq",
	"conflict_titulo", "conflict_corpo", "conflict_device_id", "conflict_at",
}

// As colunas de labels (a 0020), na ordem de scanLabel e de sqlInsertLabel.
var labelCols = []string{"id", "user_id", "nome", "created_at", "updated_at", "version", "last_seq"}

// noteLabelsOf é a subconsulta que lê os marcadores de uma nota (a alias da
// tabela notes), em ordem de id, como texto. O ARRAY de uma subconsulta vazia
// é o array vazio, nunca NULL.
func noteLabelsOf(alias string) string {
	return "ARRAY(SELECT nl.label_id::text FROM note_labels nl WHERE nl.user_id = " + alias +
		".user_id AND nl.note_id = " + alias + ".id ORDER BY nl.label_id)"
}

// noteExtrasOf são as duas subconsultas que toda leitura de nota leva depois
// das colunas: os marcadores (noteLabelsOf) e as imagens vivas
// (noteImagesOf, N10b; imagens.go), na ordem de scanNote.
func noteExtrasOf(alias string) string {
	return noteLabelsOf(alias) + ", " + noteImagesOf(alias)
}

var (
	sqlSelectNote = "SELECT " + joinCols(noteCols, "n") + ", " + noteExtrasOf("n") +
		" FROM notes n WHERE n.user_id = $1 AND n.id = $2"
	sqlInsertNote = "INSERT INTO notes (" + joinCols(noteCols, "") + ") VALUES (" + placeholders(len(noteCols)) + ")"

	sqlSelectLabel = "SELECT " + joinCols(labelCols, "") + " FROM labels WHERE user_id = $1 AND id = $2"
	sqlInsertLabel = "INSERT INTO labels (" + joinCols(labelCols, "") + ") VALUES (" + placeholders(len(labelCols)) + ")"

	// O pull: o estado atual de cada nota e de cada marcador que mudou depois
	// do cursor, com o aparelho de origem pelo evento do last_seq (o índice é
	// o UNIQUE (user_id, last_seq) de cada tabela).
	sqlPullNotes = "SELECT " + joinCols(noteCols, "n") + ", " + noteExtrasOf("n") + `, e.device_id
	FROM notes n
	LEFT JOIN note_sync_events e ON e.user_id = n.user_id AND e.seq = n.last_seq
	WHERE n.user_id = $1 AND n.last_seq > $2
	ORDER BY n.last_seq
	LIMIT $3`
	sqlPullLabels = "SELECT " + joinCols(labelCols, "b") + `, e.device_id
	FROM labels b
	LEFT JOIN note_sync_events e ON e.user_id = b.user_id AND e.seq = b.last_seq
	WHERE b.user_id = $1 AND b.last_seq > $2
	ORDER BY b.last_seq
	LIMIT $3`

	// As listagens do celular. A posição é COLLATE "C" na própria coluna (a
	// 0021): a ordem é a dos bytes, a mesma do Go e do PC.
	sqlListNotesAtivas = "SELECT " + joinCols(noteCols, "n") + ", " + noteExtrasOf("n") + ` FROM notes n
	WHERE n.user_id = $1 AND n.deleted_at IS NULL AND NOT n.arquivada
	ORDER BY n.fixada DESC, n.posicao, n.id
	LIMIT $2`
	sqlListNotesArquivo = "SELECT " + joinCols(noteCols, "n") + ", " + noteExtrasOf("n") + ` FROM notes n
	WHERE n.user_id = $1 AND n.deleted_at IS NULL AND n.arquivada
	ORDER BY n.fixada DESC, n.posicao, n.id
	LIMIT $2`
	sqlListNotesLixeira = "SELECT " + joinCols(noteCols, "n") + ", " + noteExtrasOf("n") + ` FROM notes n
	WHERE n.user_id = $1 AND n.deleted_at IS NOT NULL
	ORDER BY n.deleted_at DESC, n.id
	LIMIT $2`
	sqlListLabels = "SELECT " + joinCols(labelCols, "") + ` FROM labels
	WHERE user_id = $1
	ORDER BY nome COLLATE "C", id
	LIMIT $2`
)

const (
	// O UPDATE da nota não toca o id, o dono, a criação nem o ditado de
	// origem (só se gravam no INSERT).
	sqlUpdateNote = `UPDATE notes SET
		titulo = $3, corpo = $4, cor = $5, fixada = $6, arquivada = $7, posicao = $8,
		updated_at = $9, deleted_at = $10, purge_at = $11,
		version = $12, content_version = $13, trash_version = $14,
		content_device_id = $15, trash_device_id = $16, last_seq = $17,
		conflict_titulo = $18, conflict_corpo = $19, conflict_device_id = $20, conflict_at = $21
		WHERE user_id = $1 AND id = $2 AND version = $22`

	sqlDeleteNote = `DELETE FROM notes WHERE user_id = $1 AND id = $2`

	// As ligações da nota com os marcadores: trocadas inteiras quando o
	// conjunto muda. O array vem como text[] (o pgx manda []string assim).
	sqlDeleteNoteLabels = `DELETE FROM note_labels WHERE user_id = $1 AND note_id = $2`
	sqlInsertNoteLabels = `INSERT INTO note_labels (user_id, note_id, label_id)
		SELECT $1, $2, unnest($3::text[]::uuid[])`
	// O apagar de vez de um marcador o tira de todas as notas do usuário, sem
	// subir a versão delas (o índice é o idx_note_labels_label da 0022).
	sqlDeleteLabelLinks = `DELETE FROM note_labels WHERE user_id = $1 AND label_id = $2`

	sqlUpdateLabel = `UPDATE labels SET nome = $3, updated_at = $4, version = $5, last_seq = $6
		WHERE user_id = $1 AND id = $2 AND version = $7`
	sqlDeleteLabel = `DELETE FROM labels WHERE user_id = $1 AND id = $2`

	sqlNoteTombstoneExists = `SELECT EXISTS (SELECT 1 FROM note_tombstones
		WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3)`
	sqlNoteTombstone = `INSERT INTO note_tombstones (user_id, entity_type, entity_id, seq)
		VALUES ($1, $2, $3, $4)`
	// Os marcadores com lápide entre os que a mutação da nota cita.
	sqlDeadLabels = `SELECT entity_id::text FROM note_tombstones
		WHERE user_id = $1 AND entity_type = 'label' AND entity_id = ANY ($2::text[]::uuid[])`

	sqlNoteEvent = `INSERT INTO note_sync_events
		(user_id, seq, entity_type, entity_id, operation, entity_version, device_id, changed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	sqlPullNoteTombstones = `SELECT entity_type, entity_id, seq FROM note_tombstones
		WHERE user_id = $1 AND seq > $2
		ORDER BY seq
		LIMIT $3`
)

// scanNote lê uma linha com as colunas de noteCols, os marcadores e as
// imagens (noteExtrasOf), mais os destinos extras no fim.
func scanNote(row pgx.Row, extra ...any) (cloudstore.Note, error) {
	var n cloudstore.Note
	var cTitulo, cCorpo, cDevice *string
	var cAt *time.Time
	var labels []string
	var imagens []byte
	dest := []any{
		&n.ID, &n.UserID, &n.Titulo, &n.Corpo, &n.Cor, &n.Fixada, &n.Arquivada, &n.Posicao, &n.DitadoDeOrigem,
		&n.CreatedAt, &n.UpdatedAt, &n.DeletedAt, &n.PurgeAt,
		&n.Version, &n.ContentVersion, &n.TrashVersion, &n.ContentDeviceID, &n.TrashDeviceID, &n.LastSeq,
		&cTitulo, &cCorpo, &cDevice, &cAt,
		&labels, &imagens,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return cloudstore.Note{}, err
	}
	var err error
	if n.Imagens, err = lerImagensDaNota(imagens, n.ID, n.UserID); err != nil {
		return cloudstore.Note{}, err
	}
	n.CreatedAt, n.UpdatedAt = n.CreatedAt.UTC(), n.UpdatedAt.UTC()
	n.DeletedAt, n.PurgeAt = utc(n.DeletedAt), utc(n.PurgeAt)
	if cTitulo != nil && cCorpo != nil && cAt != nil {
		n.Conflict = &cloudstore.NoteConflict{Titulo: *cTitulo, Corpo: *cCorpo, DeviceID: cDevice, At: cAt.UTC()}
	}
	if labels == nil {
		labels = []string{}
	}
	// O ORDER BY da subconsulta é o do uuid, que é a ordem do texto em
	// minúsculas; o sort garante a ordem do Go mesmo assim.
	sort.Strings(labels)
	n.Marcadores = labels
	return n, nil
}

// noteArgs são os argumentos do INSERT da nota, na ordem de noteCols.
func noteArgs(n cloudstore.Note) []any {
	cTitulo, cCorpo, cDevice, cAt := noteConflictArgs(n.Conflict)
	return []any{
		n.ID, n.UserID, n.Titulo, n.Corpo, n.Cor, n.Fixada, n.Arquivada, n.Posicao, n.DitadoDeOrigem,
		n.CreatedAt, n.UpdatedAt, n.DeletedAt, n.PurgeAt,
		n.Version, n.ContentVersion, n.TrashVersion, n.ContentDeviceID, n.TrashDeviceID, n.LastSeq,
		cTitulo, cCorpo, cDevice, cAt,
	}
}

func noteConflictArgs(c *cloudstore.NoteConflict) (titulo, corpo, device *string, at *time.Time) {
	if c == nil {
		return nil, nil, nil, nil
	}
	t, b, a := c.Titulo, c.Corpo, c.At
	return &t, &b, c.DeviceID, &a
}

func scanLabel(row pgx.Row, extra ...any) (cloudstore.Label, error) {
	var l cloudstore.Label
	dest := []any{&l.ID, &l.UserID, &l.Nome, &l.CreatedAt, &l.UpdatedAt, &l.Version, &l.LastSeq}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return cloudstore.Label{}, err
	}
	l.CreatedAt, l.UpdatedAt = l.CreatedAt.UTC(), l.UpdatedAt.UTC()
	return l, nil
}

// readNoteForUpdate lê a nota e trava a linha. nil quando não existe. O FOR
// UPDATE trava só a linha de notes (o ARRAY é uma subconsulta).
func readNoteForUpdate(ctx context.Context, tx pgx.Tx, userID, id string) (*cloudstore.Note, error) {
	n, err := scanNote(tx.QueryRow(ctx, sqlSelectNote+" FOR UPDATE OF n", userID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, sanitize("ler a nota", err)
	}
	return &n, nil
}

func readLabelForUpdate(ctx context.Context, tx pgx.Tx, userID, id string) (*cloudstore.Label, error) {
	l, err := scanLabel(tx.QueryRow(ctx, sqlSelectLabel+" FOR UPDATE", userID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, sanitize("ler o marcador", err)
	}
	return &l, nil
}

func noteTombstoned(ctx context.Context, tx pgx.Tx, userID, entityType, id string) (bool, error) {
	var tomb bool
	if err := tx.QueryRow(ctx, sqlNoteTombstoneExists, userID, entityType, id).Scan(&tomb); err != nil {
		return false, sanitize("ler a lápide da nota", err)
	}
	return tomb, nil
}

// deadLabels lê quais dos ids têm lápide de marcador.
func deadLabels(ctx context.Context, tx pgx.Tx, userID string, ids []string) (map[string]bool, error) {
	dead := map[string]bool{}
	if len(ids) == 0 {
		return dead, nil
	}
	rows, err := tx.Query(ctx, sqlDeadLabels, userID, ids)
	if err != nil {
		return nil, sanitize("ler as lápides dos marcadores", err)
	}
	ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, sanitize("ler as lápides dos marcadores", err)
	}
	for _, id := range ids {
		dead[id] = true
	}
	return dead, nil
}

func writeNoteEvent(ctx context.Context, tx pgx.Tx, userID string, seq int64, entityType, id, op string, version int64, device *string, now time.Time) error {
	_, err := tx.Exec(ctx, sqlNoteEvent, userID, seq, entityType, id, op, version, device, now)
	return sanitize("gravar o evento da nota", err)
}

// writeNoteTombstone apaga a linha (quando existe; as ligações da nota saem em
// cascata), grava a lápide e o evento tombstone com a versão de version.
func writeNoteTombstone(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, entityType, id string, exists bool,
	version, seq int64, now time.Time) error {
	if exists {
		del := sqlDeleteNote
		if entityType == cloudstore.EntityLabel {
			del = sqlDeleteLabel
		}
		if _, err := tx.Exec(ctx, del, a.UserID, id); err != nil {
			return sanitize("apagar a nota", err)
		}
	}
	if entityType == cloudstore.EntityLabel {
		// As ligações não têm chave estrangeira para labels (o marcador pode
		// chegar depois da nota): saem aqui, tenha a linha existido ou não.
		if _, err := tx.Exec(ctx, sqlDeleteLabelLinks, a.UserID, id); err != nil {
			return sanitize("tirar o marcador das notas", err)
		}
	}
	if _, err := tx.Exec(ctx, sqlNoteTombstone, a.UserID, entityType, id, seq); err != nil {
		return sanitize("gravar a lápide da nota", err)
	}
	return writeNoteEvent(ctx, tx, a.UserID, seq, entityType, id, cloudstore.OpTombstone, version, a.Device(), now)
}

// putNote grava a nota nova (insert) ou a versão nova sobre prev (update), as
// ligações com os marcadores (só quando o conjunto muda) e o evento.
func putNote(ctx context.Context, tx pgx.Tx, n cloudstore.Note, prev *cloudstore.Note, seq int64, a cloudstore.Actor, now time.Time) error {
	n.LastSeq = seq
	if prev == nil {
		if _, err := tx.Exec(ctx, sqlInsertNote, noteArgs(n)...); err != nil {
			return sanitize("gravar a nota", err)
		}
	} else {
		cTitulo, cCorpo, cDevice, cAt := noteConflictArgs(n.Conflict)
		tag, err := tx.Exec(ctx, sqlUpdateNote, n.UserID, n.ID,
			n.Titulo, n.Corpo, n.Cor, n.Fixada, n.Arquivada, n.Posicao,
			n.UpdatedAt, n.DeletedAt, n.PurgeAt,
			n.Version, n.ContentVersion, n.TrashVersion,
			n.ContentDeviceID, n.TrashDeviceID, n.LastSeq,
			cTitulo, cCorpo, cDevice, cAt, prev.Version)
		if err != nil {
			return sanitize("gravar a nota", err)
		}
		if tag.RowsAffected() != 1 {
			// Como no ditado: não acontece com a trava do contador e o FOR
			// UPDATE.
			return errors.New("pgstore: gravar a nota: a versão mudou durante a transação")
		}
	}
	if prev == nil || !sameStrings(prev.Marcadores, n.Marcadores) {
		if prev != nil {
			if _, err := tx.Exec(ctx, sqlDeleteNoteLabels, n.UserID, n.ID); err != nil {
				return sanitize("trocar os marcadores da nota", err)
			}
		}
		if len(n.Marcadores) > 0 {
			if _, err := tx.Exec(ctx, sqlInsertNoteLabels, n.UserID, n.ID, n.Marcadores); err != nil {
				return sanitize("trocar os marcadores da nota", err)
			}
		}
	}
	return writeNoteEvent(ctx, tx, n.UserID, seq, cloudstore.EntityNote, n.ID, cloudstore.OpUpsert, n.Version, a.Device(), now)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// pushNote decide e grava, na transação do push, uma mutação de nota: a linha
// FOR UPDATE, a lápide, as lápides dos marcadores citados e
// cloudstore.DecideNote. wrote diz se gravou (com seq).
func pushNote(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, m cloudstore.Mutation, entityID string,
	seq int64, now time.Time) (out cloudstore.Outcome, wrote bool, err error) {
	cur, err := readNoteForUpdate(ctx, tx, a.UserID, entityID)
	if err != nil {
		return cloudstore.Outcome{}, false, err
	}
	tomb, err := noteTombstoned(ctx, tx, a.UserID, cloudstore.EntityNote, entityID)
	if err != nil {
		return cloudstore.Outcome{}, false, err
	}
	dead, err := deadLabels(ctx, tx, a.UserID, cloudstore.NoteLabelIDs(m))
	if err != nil {
		return cloudstore.Outcome{}, false, err
	}
	out = cloudstore.DecideNote(m, cur, tomb, dead, a, now)
	if out.Write == cloudstore.WriteNone {
		return out, false, nil
	}
	if err := gravarNotaDecidida(ctx, tx, a, out, entityID, cur, seq, now); err != nil {
		return cloudstore.Outcome{}, false, err
	}
	if out.Result.NoteState != nil {
		st := out.NextNote
		st.LastSeq = seq
		out.Result.NoteState = &st
	}
	return out, true, nil
}

// pushLabel decide e grava, na transação do push, uma mutação de marcador de
// nota (cloudstore.DecideLabel).
func pushLabel(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, m cloudstore.Mutation, entityID string,
	seq int64, now time.Time) (out cloudstore.Outcome, wrote bool, err error) {
	cur, err := readLabelForUpdate(ctx, tx, a.UserID, entityID)
	if err != nil {
		return cloudstore.Outcome{}, false, err
	}
	tomb, err := noteTombstoned(ctx, tx, a.UserID, cloudstore.EntityLabel, entityID)
	if err != nil {
		return cloudstore.Outcome{}, false, err
	}
	out = cloudstore.DecideLabel(m, cur, tomb, a, now)
	if out.Write == cloudstore.WriteNone {
		return out, false, nil
	}
	if err := gravarMarcadorDecidido(ctx, tx, a, out, entityID, cur, seq, now); err != nil {
		return cloudstore.Outcome{}, false, err
	}
	return out, true, nil
}

// gravarNotaDecidida grava, com o seq, o que DecideNote decidiu (Write não é
// WriteNone) sobre cur: a lápide, ou a linha nova (cur nil) ou a versão nova.
func gravarNotaDecidida(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, out cloudstore.Outcome, entityID string,
	cur *cloudstore.Note, seq int64, now time.Time) error {
	if out.Write == cloudstore.WriteTombstone {
		return writeNoteTombstone(ctx, tx, a, cloudstore.EntityNote, entityID, cur != nil, out.Result.EntityVersion, seq, now)
	}
	return putNote(ctx, tx, out.NextNote, cur, seq, a, now)
}

// gravarMarcadorDecidido grava, com o seq, o que DecideLabel decidiu (Write
// não é WriteNone) sobre cur.
func gravarMarcadorDecidido(ctx context.Context, tx pgx.Tx, a cloudstore.Actor, out cloudstore.Outcome, entityID string,
	cur *cloudstore.Label, seq int64, now time.Time) error {
	if out.Write == cloudstore.WriteTombstone {
		return writeNoteTombstone(ctx, tx, a, cloudstore.EntityLabel, entityID, cur != nil, out.Result.EntityVersion, seq, now)
	}
	l := out.NextLabel
	l.LastSeq = seq
	if cur == nil {
		if _, err := tx.Exec(ctx, sqlInsertLabel, l.ID, l.UserID, l.Nome, l.CreatedAt, l.UpdatedAt, l.Version, l.LastSeq); err != nil {
			return sanitize("gravar o marcador", err)
		}
	} else {
		tag, err := tx.Exec(ctx, sqlUpdateLabel, l.UserID, l.ID, l.Nome, l.UpdatedAt, l.Version, l.LastSeq, cur.Version)
		if err != nil {
			return sanitize("gravar o marcador", err)
		}
		if tag.RowsAffected() != 1 {
			return errors.New("pgstore: gravar o marcador: a versão mudou durante a transação")
		}
	}
	return writeNoteEvent(ctx, tx, l.UserID, seq, cloudstore.EntityLabel, l.ID, cloudstore.OpUpsert, l.Version, a.Device(), now)
}

// pullNotes lê, na transação do pull (o mesmo snapshot), as notas, os
// marcadores e as lápides deles depois de afterSeq, cada lista com até limit
// linhas. As notas param de ser lidas uma linha depois da que chega ao
// orçamento de texto, como os ditados (mergePage explica por que a página
// junta fica certa com duas listas paradas no orçamento).
func pullNotes(ctx context.Context, tx pgx.Tx, userID string, afterSeq int64, limit int) (notes, labels, tombs []cloudstore.Change, err error) {
	rows, err := tx.Query(ctx, sqlPullNotes, userID, afterSeq, limit)
	if err != nil {
		return nil, nil, nil, sanitize("ler as notas", err)
	}
	used, over := 0, false
	for rows.Next() {
		var origin *string
		n, err := scanNote(rows, &origin)
		if err != nil {
			rows.Close()
			return nil, nil, nil, sanitize("ler as notas", err)
		}
		note := n
		notes = append(notes, cloudstore.Change{
			Seq: n.LastSeq, EntityType: cloudstore.EntityNote, EntityID: n.ID,
			Operation: cloudstore.OpUpsert, EntityVersion: n.Version, DeviceID: origin, Note: &note,
		})
		if over {
			break
		}
		used += cloudstore.NoteTextBytes(n)
		over = used >= cloudstore.PageTextBudget
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, sanitize("ler as notas", err)
	}

	// Os marcadores: um nome tem no máximo 50 caracteres, e as limit linhas
	// cabem com folga no orçamento.
	rows, err = tx.Query(ctx, sqlPullLabels, userID, afterSeq, limit)
	if err != nil {
		return nil, nil, nil, sanitize("ler os marcadores", err)
	}
	for rows.Next() {
		var origin *string
		l, err := scanLabel(rows, &origin)
		if err != nil {
			rows.Close()
			return nil, nil, nil, sanitize("ler os marcadores", err)
		}
		label := l
		labels = append(labels, cloudstore.Change{
			Seq: l.LastSeq, EntityType: cloudstore.EntityLabel, EntityID: l.ID,
			Operation: cloudstore.OpUpsert, EntityVersion: l.Version, DeviceID: origin, Label: &label,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, sanitize("ler os marcadores", err)
	}

	rows, err = tx.Query(ctx, sqlPullNoteTombstones, userID, afterSeq, limit)
	if err != nil {
		return nil, nil, nil, sanitize("ler as lápides das notas", err)
	}
	for rows.Next() {
		c := cloudstore.Change{Operation: cloudstore.OpTombstone}
		if err := rows.Scan(&c.EntityType, &c.EntityID, &c.Seq); err != nil {
			rows.Close()
			return nil, nil, nil, sanitize("ler as lápides das notas", err)
		}
		tombs = append(tombs, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, sanitize("ler as lápides das notas", err)
	}
	return notes, labels, tombs, nil
}

// ListNotes lista as notas da vista, até o limite e o orçamento de texto
// (uma linha a mais que o limite diz que passou dele). Um comando só (READ
// COMMITTED): a página é de um snapshot.
func (s *Store) ListNotes(ctx context.Context, userID, vista string, limit int) (cloudstore.NotePage, error) {
	var sql string
	switch vista {
	case cloudstore.VistaAtivas:
		sql = sqlListNotesAtivas
	case cloudstore.VistaArquivo:
		sql = sqlListNotesArquivo
	case cloudstore.VistaLixeira:
		sql = sqlListNotesLixeira
	default:
		return cloudstore.NotePage{}, cloudstore.ErrInvalidFilter
	}
	limit = cloudstore.NotesListLimit(limit)
	rows, err := s.pool.Query(ctx, sql, userID, limit+1)
	if err != nil {
		return cloudstore.NotePage{}, sanitize("listar as notas", err)
	}
	defer rows.Close()
	items := []cloudstore.Note{}
	used, over := 0, false
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return cloudstore.NotePage{}, sanitize("listar as notas", err)
		}
		items = append(items, n)
		if over {
			break
		}
		used += cloudstore.NoteTextBytes(n)
		over = used >= cloudstore.PageTextBudget
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cloudstore.NotePage{}, sanitize("listar as notas", err)
	}
	n, more := cloudstore.CutPage(items, limit, cloudstore.NoteTextBytes)
	return cloudstore.NotePage{Items: items[:n], HasMore: more}, nil
}

// ListLabels lista os marcadores pelo nome (COLLATE "C"), até o limite.
func (s *Store) ListLabels(ctx context.Context, userID string, limit int) (cloudstore.LabelPage, error) {
	limit = cloudstore.NotesListLimit(limit)
	rows, err := s.pool.Query(ctx, sqlListLabels, userID, limit+1)
	if err != nil {
		return cloudstore.LabelPage{}, sanitize("listar os marcadores", err)
	}
	defer rows.Close()
	page := cloudstore.LabelPage{Items: []cloudstore.Label{}}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return cloudstore.LabelPage{}, sanitize("listar os marcadores", err)
		}
		page.Items = append(page.Items, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cloudstore.LabelPage{}, sanitize("listar os marcadores", err)
	}
	if len(page.Items) > limit {
		page.Items, page.HasMore = page.Items[:limit], true
	}
	return page, nil
}
