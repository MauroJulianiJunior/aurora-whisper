package pgstore

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// betweenPullQueries, quando não é nil, roda depois de cada leitura do pull
// que tem outra depois dela: depois dos ditados (done = pullReadTranscriptions)
// e depois das lápides (done = pullReadTombstones), antes do aprendido. Só os
// testes o ligam, para gravar no meio e provar que as três leituras são do
// mesmo snapshot.
var betweenPullQueries func(done string)

// As leituras do pull, na ordem, pelo nome que betweenPullQueries recebe.
const (
	pullReadTranscriptions = "transcriptions"
	pullReadTombstones     = "tombstones"
)

// rowsRead, quando não é nil, recebe quantas linhas de ditado o pull ("pull")
// e a listagem ("list") leram do banco. Só os testes o ligam, para provar que
// a leitura para no orçamento de texto, e não só a resposta.
var rowsRead func(where string, n int)

var sqlPullEntities = "SELECT " + readColsWith("t") + `, e.device_id
	FROM transcriptions t
	LEFT JOIN sync_events e ON e.user_id = t.user_id AND e.seq = t.last_seq
	WHERE t.user_id = $1 AND t.last_seq > $2
	ORDER BY t.last_seq
	LIMIT $3`

const sqlPullTombstones = `SELECT entity_type, entity_id, seq FROM tombstones
	WHERE user_id = $1 AND seq > $2
	ORDER BY seq
	LIMIT $3`

// O aprendido do pull (F8c): o estado atual de cada par que mudou depois do
// cursor, o desfeito inclusive (com undone_at: o aprendido não tem lápide), e
// o aparelho de origem pelo sync_event do last_seq, como no ditado. O índice
// é o UNIQUE (user_id, last_seq) da 0016.
var sqlPullLearned = "SELECT " + joinCols(learnedCols, "l") + `, e.device_id
	FROM learned_pairs l
	LEFT JOIN sync_events e ON e.user_id = l.user_id AND e.seq = l.last_seq
	WHERE l.user_id = $1 AND l.last_seq > $2
	ORDER BY l.last_seq
	LIMIT $3`

func (s *Store) Pull(ctx context.Context, userID string, afterSeq int64, limit int) (cloudstore.PullResponse, error) {
	if limit <= 0 {
		limit = cloudstore.DefaultPullLimit
	}
	if limit > cloudstore.MaxPullLimit {
		limit = cloudstore.MaxPullLimit
	}
	// Um snapshot só para o epoch, os ditados, as lápides e o aprendido.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return cloudstore.PullResponse{}, sanitize("abrir a leitura", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	resp := cloudstore.PullResponse{LastSeq: afterSeq}
	if err := tx.QueryRow(ctx, sqlEpoch, userID).Scan(&resp.Epoch); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cloudstore.PullResponse{}, cloudstore.ErrUnknownUser
		}
		return cloudstore.PullResponse{}, sanitize("ler o epoch", err)
	}

	// limit+1 de cada lado: os limit+1 primeiros do conjunto estão entre eles.
	// As entidades param de ser lidas uma linha depois da que chegou ao
	// orçamento de texto (cloudstore.CutPage corta ali, e a linha a mais diz
	// que há mais): a resposta, e a memória para montá-la, ficam no
	// orçamento mais dois itens, e não em limit itens do tamanho do teto.
	rows, err := tx.Query(ctx, sqlPullEntities, userID, afterSeq, limit+1)
	if err != nil {
		return cloudstore.PullResponse{}, sanitize("ler as entidades", err)
	}
	var entities []cloudstore.Change
	used, over := 0, false
	for rows.Next() {
		var origin *string
		t, err := scanTranscription(rows, &origin)
		if err != nil {
			rows.Close()
			return cloudstore.PullResponse{}, sanitize("ler as entidades", err)
		}
		item := t
		entities = append(entities, cloudstore.Change{
			Seq: t.LastSeq, EntityType: cloudstore.EntityTranscription, EntityID: t.ID,
			Operation: cloudstore.OpUpsert, EntityVersion: t.Version, DeviceID: origin, Item: &item,
		})
		if over {
			break
		}
		used += cloudstore.TextBytes(t)
		over = used >= cloudstore.PageTextBudget
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cloudstore.PullResponse{}, sanitize("ler as entidades", err)
	}
	if rowsRead != nil {
		rowsRead("pull", len(entities))
	}

	if betweenPullQueries != nil {
		betweenPullQueries(pullReadTranscriptions)
	}

	rows, err = tx.Query(ctx, sqlPullTombstones, userID, afterSeq, limit+1)
	if err != nil {
		return cloudstore.PullResponse{}, sanitize("ler as lápides", err)
	}
	var tombs []cloudstore.Change
	for rows.Next() {
		c := cloudstore.Change{Operation: cloudstore.OpTombstone}
		if err := rows.Scan(&c.EntityType, &c.EntityID, &c.Seq); err != nil {
			rows.Close()
			return cloudstore.PullResponse{}, sanitize("ler as lápides", err)
		}
		tombs = append(tombs, c)
	}
	if err := rows.Err(); err != nil {
		return cloudstore.PullResponse{}, sanitize("ler as lápides", err)
	}

	if betweenPullQueries != nil {
		betweenPullQueries(pullReadTombstones)
	}

	// O aprendido (F8c), no mesmo snapshot. Sem parar no orçamento de texto:
	// um par tem no máximo três textos de 80 runas (uns 960 bytes), e os
	// limit+1 pares cabem com folga nele.
	rows, err = tx.Query(ctx, sqlPullLearned, userID, afterSeq, limit+1)
	if err != nil {
		return cloudstore.PullResponse{}, sanitize("ler o aprendido", err)
	}
	var learned []cloudstore.Change
	for rows.Next() {
		var origin *string
		l, err := scanLearned(rows, &origin)
		if err != nil {
			rows.Close()
			return cloudstore.PullResponse{}, sanitize("ler o aprendido", err)
		}
		pair := l
		learned = append(learned, cloudstore.Change{
			Seq: l.LastSeq, EntityType: cloudstore.EntityLearned, EntityID: l.ID,
			Operation: cloudstore.OpUpsert, EntityVersion: l.Version, DeviceID: origin, Learned: &pair,
		})
	}
	if err := rows.Err(); err != nil {
		return cloudstore.PullResponse{}, sanitize("ler o aprendido", err)
	}

	// As notas, os marcadores e as lápides deles (N7), no mesmo snapshot.
	notes, labels, noteTombs, err := pullNotes(ctx, tx, userID, afterSeq, limit+1)
	if err != nil {
		return cloudstore.PullResponse{}, err
	}

	changes, more := mergePage(limit, entities, tombs, learned, notes, labels, noteTombs)
	resp.Changes, resp.HasMore = changes, more
	resp.Entidades = cloudstore.EntidadesDoSync()
	if len(changes) > 0 {
		resp.LastSeq = changes[len(changes)-1].Seq
	}
	if err := tx.Commit(ctx); err != nil {
		return cloudstore.PullResponse{}, sanitize("fechar a leitura", err)
	}
	return resp, nil
}

// mergePage junta as listas do pull (os ditados, as lápides e o aprendido,
// cada uma em ordem de seq e com até limit+1 linhas) por seq e corta a página
// com cloudstore.CutPage: no máximo limit mudanças, e o corte do orçamento de
// texto. more diz que há mais depois da página. Nunca devolve nil.
//
// Cada seq é de uma mudança só (o seq vem do contador, e cada gravação grava
// uma entidade), então não há empate. Os limit+1 primeiros do conjunto estão
// entre os limit+1 primeiros de cada lista. Se os ditados pararam de ser lidos
// no orçamento (uma linha depois da que chegou a ele), o que vem depois do
// último lido pode estar incompleto, mas o corte é antes dele: na lista
// junta, o texto acumulado até o ditado que chegou ao orçamento é pelo menos
// o dos ditados até ele (a lápide não tem texto, e o aprendido só soma), então
// CutPage para nele ou antes, e a linha lida a mais diz que há mais.
//
// Desde a N7, as notas também param no orçamento (pullNotes), e o argumento é
// o mesmo para cada lista parada: o corte da junta fica antes da primeira
// linha que fez QUALQUER uma delas chegar ao orçamento, e o que falta de cada
// lista está depois dessa linha.
func mergePage(limit int, lists ...[]cloudstore.Change) ([]cloudstore.Change, bool) {
	total := 0
	for _, l := range lists {
		total += len(l)
	}
	changes := make([]cloudstore.Change, 0, total)
	for _, l := range lists {
		changes = append(changes, l...)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Seq < changes[j].Seq })
	if len(changes) > limit+1 {
		changes = changes[:limit+1]
	}
	n, more := cloudstore.CutPage(changes, limit, cloudstore.Change.TextBytes)
	return changes[:n], more
}

func (s *Store) List(ctx context.Context, f cloudstore.ListFilter) (cloudstore.Page, error) {
	if err := cloudstore.CheckListFilter(f); err != nil {
		return cloudstore.Page{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = cloudstore.DefaultListLimit
	}
	if limit > cloudstore.MaxListLimit {
		limit = cloudstore.MaxListLimit
	}
	sortCol := "created_at"
	where := []string{"user_id = $1", "deleted_at IS NULL",
		// O ditado que virou nota sai da listagem do histórico (N7).
		"NOT EXISTS (SELECT 1 FROM transcription_note_links vl WHERE vl.user_id = transcriptions.user_id AND vl.transcription_id = transcriptions.id)"}
	if f.Trash {
		sortCol = "deleted_at"
		where[1] = "deleted_at IS NOT NULL"
	}
	args := []any{f.UserID}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.DeviceID != nil {
		canon, ok := cloudstore.CanonicalUUID(*f.DeviceID)
		if !ok {
			return cloudstore.Page{}, cloudstore.ErrInvalidFilter
		}
		where = append(where, "device_id = "+arg(canon))
	}
	if f.Mode != nil {
		where = append(where, "mode = "+arg(*f.Mode))
	}
	if f.From != nil {
		where = append(where, "created_at >= "+arg(*f.From))
	}
	if f.To != nil {
		where = append(where, "created_at <= "+arg(*f.To))
	}
	if f.Query != "" {
		p := arg("%" + escapeLike(f.Query) + "%")
		where = append(where, "(raw_text ILIKE "+p+" ESCAPE '\\' OR final_text ILIKE "+p+" ESCAPE '\\')")
	}
	if f.Cursor != "" {
		at, id, err := cloudstore.DecodeCursor(f.Cursor)
		if err != nil {
			return cloudstore.Page{}, err
		}
		where = append(where, "("+sortCol+", id) < ("+arg(at)+", "+arg(id)+")")
	}
	sql := "SELECT " + readColsWith("") + " FROM transcriptions WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + sortCol + " DESC, id DESC LIMIT " + arg(limit+1)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return cloudstore.Page{}, sanitize("listar", err)
	}
	defer rows.Close()
	// Como no pull: para de ler uma linha depois da que chegou ao orçamento
	// de texto, e cloudstore.CutPage corta ali.
	items := []cloudstore.Transcription{}
	used, over := 0, false
	for rows.Next() {
		t, err := scanTranscription(rows)
		if err != nil {
			return cloudstore.Page{}, sanitize("listar", err)
		}
		items = append(items, t)
		if over {
			break
		}
		used += cloudstore.TextBytes(t)
		over = used >= cloudstore.PageTextBudget
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cloudstore.Page{}, sanitize("listar", err)
	}
	if rowsRead != nil {
		rowsRead("list", len(items))
	}
	n, more := cloudstore.CutPage(items, limit, cloudstore.TextBytes)
	page := cloudstore.Page{Items: items[:n]}
	if more {
		last := page.Items[n-1]
		at := last.CreatedAt
		if f.Trash {
			at = *last.DeletedAt
		}
		page.NextCursor = cloudstore.EncodeCursor(at, last.ID)
	}
	return page, nil
}

// escapeLike escapa os curingas do LIKE: a busca é por trecho literal, como
// no servidor em memória.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
