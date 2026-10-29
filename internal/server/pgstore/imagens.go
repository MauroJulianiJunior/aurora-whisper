package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As imagens das notas no Postgres (ADR-022, N10b; a 0026). As regras são as de
// cloudstore (DecidirAnexo e DecidirTirar), as mesmas do Store em memória; este
// arquivo é a transação e o SQL.
//
// Toda gravação passa pelo ponto único (gravar, store.go): o contador travado
// primeiro (a contagem por nota e a soma do teto são lidas DEPOIS dele, então
// dois anexos ao mesmo tempo da mesma conta não passam os dois), depois a nota
// FOR UPDATE (o tirar da nota e a lixeira dela não se cruzam com o anexo) e a
// linha da imagem. A lápide fica na própria linha (removed_at, sem os bytes).
//
// Os bytes chegam já conferidos (imagemdanota.Conferir, no handler, fora da
// transação) e só são lidos pela rota de leitura (sqlImageBytes): nenhuma outra
// consulta toca a coluna conteudo, que mora fora da linha (STORAGE EXTERNAL).
// Nenhum erro deste arquivo carrega bytes, tamanho ou texto.

// As colunas de note_images que os metadados leem, na ordem de scanImage.
const imageCols = "id, user_id, note_id, largura, altura, bytes, sha256, created_at, removed_at, last_seq"

const (
	sqlImageForUpdate = `SELECT ` + imageCols + ` FROM note_images WHERE user_id = $1 AND id = $2 FOR UPDATE`
	sqlImageInfo      = `SELECT ` + imageCols + ` FROM note_images WHERE user_id = $1 AND id = $2`
	// A contagem da nota e a soma da conta, só as vivas (o índice parcial
	// idx_note_images_note): as da lixeira contam no teto.
	sqlImageCounts = `SELECT COUNT(*) FILTER (WHERE note_id = $2), COALESCE(SUM(bytes), 0)
		FROM note_images WHERE user_id = $1 AND removed_at IS NULL`
	sqlInsertImage = `INSERT INTO note_images (` + imageCols + `, conteudo)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9, $10)`
	// A lápide sobre a linha viva: os bytes saem, os metadados ficam.
	sqlRemoveImage = `UPDATE note_images SET conteudo = NULL, removed_at = $3, last_seq = $4
		WHERE user_id = $1 AND id = $2 AND removed_at IS NULL`
	// A lápide de um id que a nuvem nunca teve, sem os metadados.
	sqlImageTombstone = `INSERT INTO note_images (id, user_id, note_id, created_at, removed_at, last_seq)
		VALUES ($1, $2, $3, $4, $4, $5)`
	// Os bytes, só da imagem viva da nota pedida.
	sqlImageBytes = `SELECT ` + imageCols + `, conteudo FROM note_images
		WHERE user_id = $1 AND note_id = $2 AND id = $3 AND removed_at IS NULL`
	// O pull: as imagens que mudaram depois do cursor (o índice é o UNIQUE
	// (user_id, last_seq)). A viva é upsert; a da lápide, tombstone.
	sqlPullImages = `SELECT ` + imageCols + ` FROM note_images
		WHERE user_id = $1 AND last_seq > $2
		ORDER BY last_seq
		LIMIT $3`
)

// noteImagesOf é a subconsulta das imagens vivas de uma nota (a alias da tabela
// notes), na ordem da nota, como JSON: os metadados, nunca os bytes. O vazio é
// o array vazio, nunca NULL.
func noteImagesOf(alias string) string {
	return `COALESCE((SELECT json_agg(json_build_object('id', i.id, 'largura', i.largura, 'altura', i.altura,
		'bytes', i.bytes, 'sha256', i.sha256, 'created_at', i.created_at, 'last_seq', i.last_seq)
		ORDER BY i.created_at, i.id) FROM note_images i
		WHERE i.user_id = ` + alias + `.user_id AND i.note_id = ` + alias + `.id AND i.removed_at IS NULL), '[]'::json)`
}

// imagemDoJSON é um item da subconsulta noteImagesOf.
type imagemDoJSON struct {
	ID        string    `json:"id"`
	Largura   int       `json:"largura"`
	Altura    int       `json:"altura"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
	LastSeq   int64     `json:"last_seq"`
}

// lerImagensDaNota lê o JSON da subconsulta noteImagesOf. Nunca nil.
func lerImagensDaNota(raw []byte, noteID, userID string) ([]cloudstore.NoteImage, error) {
	out := []cloudstore.NoteImage{}
	if len(raw) == 0 {
		return out, nil
	}
	var itens []imagemDoJSON
	if err := json.Unmarshal(raw, &itens); err != nil {
		return nil, errors.New("pgstore: ler as imagens da nota: o JSON não se lê")
	}
	for _, it := range itens {
		out = append(out, cloudstore.NoteImage{
			ID: it.ID, UserID: userID, NoteID: noteID, Largura: it.Largura, Altura: it.Altura, Bytes: it.Bytes,
			SHA256: it.SHA256, CreatedAt: it.CreatedAt.UTC(), LastSeq: it.LastSeq,
		})
	}
	// O ORDER BY do json_agg é o do Postgres (o uuid no empate); o sort garante
	// a ordem do Go mesmo assim.
	cloudstore.OrdenarImagens(out)
	return out, nil
}

// scanImage lê uma linha com as colunas de imageCols, mais os destinos extras.
// As colunas nulas (a lápide de um id que a nuvem nunca teve) viram zero.
func scanImage(row pgx.Row, extra ...any) (cloudstore.NoteImage, error) {
	var im cloudstore.NoteImage
	var largura, altura *int
	var bytes *int64
	var sha *string
	dest := []any{&im.ID, &im.UserID, &im.NoteID, &largura, &altura, &bytes, &sha, &im.CreatedAt, &im.RemovedAt, &im.LastSeq}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return cloudstore.NoteImage{}, err
	}
	if largura != nil {
		im.Largura = *largura
	}
	if altura != nil {
		im.Altura = *altura
	}
	if bytes != nil {
		im.Bytes = *bytes
	}
	if sha != nil {
		im.SHA256 = *sha
	}
	im.CreatedAt, im.RemovedAt = im.CreatedAt.UTC(), utc(im.RemovedAt)
	return im, nil
}

// lugarDaImagem lê, na transação que já travou o contador, o que DecidirAnexo e
// DecidirTirar precisam: a nota FOR UPDATE, a lápide dela, a linha da imagem
// FOR UPDATE e as contagens.
func lugarDaImagem(ctx context.Context, tx pgx.Tx, userID, noteID, id string) (cloudstore.LugarDaImagem, error) {
	var l cloudstore.LugarDaImagem
	var err error
	if l.Nota, err = readNoteForUpdate(ctx, tx, userID, noteID); err != nil {
		return l, err
	}
	if l.Nota == nil {
		if l.NotaComLapide, err = noteTombstoned(ctx, tx, userID, cloudstore.EntityNote, noteID); err != nil {
			return l, err
		}
	}
	im, err := scanImage(tx.QueryRow(ctx, sqlImageForUpdate, userID, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return l, sanitize("ler a imagem", err)
	default:
		l.Atual = &im
	}
	if err := tx.QueryRow(ctx, sqlImageCounts, userID, noteID).Scan(&l.NaNota, &l.Total); err != nil {
		return l, sanitize("contar as imagens", err)
	}
	return l, nil
}

// inserirImagem grava a imagem viva com os bytes e o seq.
func inserirImagem(ctx context.Context, tx pgx.Tx, im cloudstore.NoteImage, b []byte, seq int64) error {
	_, err := tx.Exec(ctx, sqlInsertImage, im.ID, im.UserID, im.NoteID, im.Largura, im.Altura, im.Bytes, im.SHA256,
		im.CreatedAt, seq, b)
	return sanitize("gravar a imagem", err)
}

func (s *Store) AttachImage(ctx context.Context, a cloudstore.Actor, n cloudstore.ImagemNova, now time.Time) (cloudstore.NoteImage, bool, error) {
	noteID, ok := cloudstore.CanonicalUUID(n.NoteID)
	if !ok {
		return cloudstore.NoteImage{}, false, cloudstore.ErrNoteNotFound
	}
	id, ok := cloudstore.CanonicalUUID(n.ID)
	if !ok {
		return cloudstore.NoteImage{}, false, cloudstore.ErrImageNotFound
	}
	n.NoteID, n.ID = noteID, id
	var out cloudstore.NoteImage
	var created bool
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		l, err := lugarDaImagem(ctx, tx, a.UserID, noteID, id)
		if err != nil {
			return false, err
		}
		im, grava, err := cloudstore.DecidirAnexo(l, a.UserID, n, now)
		if err != nil {
			return false, err
		}
		if !grava {
			out = im
			return false, nil
		}
		im.LastSeq = seq
		if err := inserirImagem(ctx, tx, im, n.Limpa.Bytes, seq); err != nil {
			return false, err
		}
		out, created = im, true
		return true, nil
	})
	return out, created, err
}

func (s *Store) CreateNoteWithImage(ctx context.Context, a cloudstore.Actor, img imagemdanota.Limpa, now time.Time) (cloudstore.Note, error) {
	if a.DeviceID != "" {
		return cloudstore.Note{}, cloudstore.ErrDeviceNotAllowed
	}
	id, idDaImagem := cloudstore.NewUUIDv7(), cloudstore.NewUUIDv7()
	var out cloudstore.Note
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		topo, err := topoDoGrupo(ctx, tx, a.UserID, false, false, "")
		if err != nil {
			return false, err
		}
		m, err := cloudstore.PlanejarNotaComImagem(id, topo, now)
		if err != nil {
			return false, err
		}
		o := cloudstore.DecideNote(m, nil, false, nil, a, now)
		if _, err := cloudstore.ResultadoDaNota(o, nil); err != nil {
			return false, err
		}
		l, err := lugarDaImagem(ctx, tx, a.UserID, id, idDaImagem)
		if err != nil {
			return false, err
		}
		nova := o.NextNote
		l.Nota, l.NaNota = &nova, 0
		im, _, err := cloudstore.DecidirAnexo(l, a.UserID, cloudstore.ImagemNova{ID: idDaImagem, NoteID: id, Limpa: img}, now)
		if err != nil {
			return false, err
		}
		if err := putNote(ctx, tx, o.NextNote, nil, seq, a, now); err != nil {
			return false, err
		}
		seqDaImagem, err := proximoSeq(ctx, tx, a.UserID)
		if err != nil {
			return false, err
		}
		im.LastSeq = seqDaImagem
		if err := inserirImagem(ctx, tx, im, img.Bytes, seqDaImagem); err != nil {
			return false, err
		}
		out = o.NextNote
		out.LastSeq = seq
		out.Imagens = []cloudstore.NoteImage{im}
		return true, nil
	})
	return out, err
}

func (s *Store) ImageInfo(ctx context.Context, userID, id string) (cloudstore.NoteImage, bool, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.NoteImage{}, false, nil
	}
	im, err := scanImage(s.pool.QueryRow(ctx, sqlImageInfo, userID, canon))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cloudstore.NoteImage{}, false, nil
	case err != nil:
		return cloudstore.NoteImage{}, false, sanitize("ler a imagem", err)
	}
	return im, true, nil
}

func (s *Store) GetImage(ctx context.Context, userID, noteID, id string) (cloudstore.NoteImage, []byte, error) {
	canonNota, ok1 := cloudstore.CanonicalUUID(noteID)
	canon, ok2 := cloudstore.CanonicalUUID(id)
	if !ok1 || !ok2 {
		return cloudstore.NoteImage{}, nil, cloudstore.ErrImageNotFound
	}
	var b []byte
	im, err := scanImage(s.pool.QueryRow(ctx, sqlImageBytes, userID, canonNota, canon), &b)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cloudstore.NoteImage{}, nil, cloudstore.ErrImageNotFound
	case err != nil:
		return cloudstore.NoteImage{}, nil, sanitize("ler os bytes da imagem", err)
	}
	if int64(len(b)) != im.Bytes {
		// O CHECK da 0026 não deixa; se acontecer, não é a imagem guardada.
		return cloudstore.NoteImage{}, nil, fmt.Errorf("pgstore: ler os bytes da imagem: o tamanho não confere")
	}
	return im, b, nil
}

func (s *Store) RemoveImage(ctx context.Context, a cloudstore.Actor, noteID, id string, now time.Time) error {
	canonNota, ok := cloudstore.CanonicalUUID(noteID)
	if !ok {
		return cloudstore.ErrNoteNotFound
	}
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.ErrImageNotFound
	}
	return s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		l, err := lugarDaImagem(ctx, tx, a.UserID, canonNota, canon)
		if err != nil {
			return false, err
		}
		acao, err := cloudstore.DecidirTirar(l, canonNota)
		if err != nil || acao == cloudstore.TirarNada {
			return false, err
		}
		quando := cloudstore.Normalize(now)
		if acao == cloudstore.TirarLapide {
			tag, err := tx.Exec(ctx, sqlRemoveImage, a.UserID, canon, quando, seq)
			if err != nil {
				return false, sanitize("tirar a imagem", err)
			}
			if tag.RowsAffected() != 1 {
				return false, errors.New("pgstore: tirar a imagem: a linha mudou durante a transação")
			}
			return true, nil
		}
		if _, err := tx.Exec(ctx, sqlImageTombstone, canon, a.UserID, canonNota, quando, seq); err != nil {
			return false, sanitize("gravar a lápide da imagem", err)
		}
		return true, nil
	})
}

// pullImages lê, na transação do pull (o mesmo snapshot), as imagens que
// mudaram depois de afterSeq, até limit linhas. Sem texto: não contam no
// orçamento da página.
func pullImages(ctx context.Context, tx pgx.Tx, userID string, afterSeq int64, limit int) ([]cloudstore.Change, error) {
	rows, err := tx.Query(ctx, sqlPullImages, userID, afterSeq, limit)
	if err != nil {
		return nil, sanitize("ler as imagens", err)
	}
	defer rows.Close()
	var changes []cloudstore.Change
	for rows.Next() {
		im, err := scanImage(rows)
		if err != nil {
			return nil, sanitize("ler as imagens", err)
		}
		c := cloudstore.Change{Seq: im.LastSeq, EntityType: cloudstore.EntityNoteImage, EntityID: im.ID, Operation: cloudstore.OpTombstone}
		if im.Viva() {
			img := im
			c.Operation, c.EntityVersion, c.Image = cloudstore.OpUpsert, 1, &img
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, sanitize("ler as imagens", err)
	}
	return changes, nil
}
