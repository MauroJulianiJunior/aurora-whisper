package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As escritas do celular nas notas e nos marcadores no Postgres (ADR-022, N8;
// cloudstore/notas_celular.go): o plano é o mesmo do Store em memória, e a
// gravação passa pelo mesmo ponto único (gravar): o contador primeiro, depois a
// linha FOR UPDATE, o resto e o evento na note_sync_events. O "Ditar para nota"
// e o Desfazer gravam duas entidades (a nota e o ditado), cada uma com o seu
// seq, na mesma transação. Nenhum erro carrega texto.

const (
	// O topo do grupo: a menor posição (COLLATE "C", a da coluna) fora da
	// lixeira, sem a própria nota ("" é nenhuma). O índice é o idx_notes_list.
	sqlTopoDoGrupo = `SELECT COALESCE(MIN(posicao), '') FROM notes
		WHERE user_id = $1 AND deleted_at IS NULL AND arquivada = $2 AND fixada = $3 AND id::text <> $4`
	sqlMarcadorVivo = `SELECT EXISTS (SELECT 1 FROM labels WHERE user_id = $1 AND id = $2)`
)

var (
	// As notas que o "Ditar para nota" pode escolher: fora da lixeira e do
	// Arquivo.
	sqlNotasAtivas = "SELECT " + joinCols(noteCols, "n") + ", " + noteLabelsOf("n") +
		" FROM notes n WHERE n.user_id = $1 AND n.deleted_at IS NULL AND NOT n.arquivada"
	sqlMarcadoresDaPessoa = "SELECT " + joinCols(labelCols, "") + " FROM labels WHERE user_id = $1"
)

// proximoSeq tira mais um número do contador, já travado pela transação: o
// segundo seq de uma gravação que escreve duas entidades.
func proximoSeq(ctx context.Context, tx pgx.Tx, userID string) (int64, error) {
	var seq int64
	var epoch string
	if err := tx.QueryRow(ctx, sqlNextSeq, userID).Scan(&seq, &epoch); err != nil {
		return 0, sanitize("subir o contador", err)
	}
	return seq, nil
}

// contextoDaNota é o que o plano lê, na transação.
func contextoDaNota(ctx context.Context, tx pgx.Tx, userID string) cloudstore.ContextoDaNota {
	return cloudstore.ContextoDaNota{
		Nota: func(id string) (*cloudstore.Note, error) {
			n, err := scanNote(tx.QueryRow(ctx, sqlSelectNote, userID, id))
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			if err != nil {
				return nil, sanitize("ler a vizinha", err)
			}
			return &n, nil
		},
		Topo: func(arquivada, fixada bool, exceto string) (string, error) {
			return topoDoGrupo(ctx, tx, userID, arquivada, fixada, exceto)
		},
		MarcadorVivo: func(id string) (bool, error) {
			return marcadorVivo(ctx, tx, userID, id)
		},
	}
}

func topoDoGrupo(ctx context.Context, tx pgx.Tx, userID string, arquivada, fixada bool, exceto string) (string, error) {
	var topo string
	if err := tx.QueryRow(ctx, sqlTopoDoGrupo, userID, arquivada, fixada, exceto).Scan(&topo); err != nil {
		return "", sanitize("ler o topo do grupo", err)
	}
	return topo, nil
}

func marcadorVivo(ctx context.Context, tx pgx.Tx, userID, id string) (bool, error) {
	var vivo bool
	if err := tx.QueryRow(ctx, sqlMarcadorVivo, userID, id).Scan(&vivo); err != nil {
		return false, sanitize("ler o marcador", err)
	}
	return vivo, nil
}

func marcadoresDaPessoa(ctx context.Context, tx pgx.Tx, userID string) ([]cloudstore.Label, error) {
	rows, err := tx.Query(ctx, sqlMarcadoresDaPessoa, userID)
	if err != nil {
		return nil, sanitize("ler os marcadores", err)
	}
	defer rows.Close()
	var out []cloudstore.Label
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, sanitize("ler os marcadores", err)
		}
		out = append(out, l)
	}
	return out, sanitize("ler os marcadores", rows.Err())
}

func notasAtivas(ctx context.Context, tx pgx.Tx, userID string) ([]cloudstore.Note, error) {
	rows, err := tx.Query(ctx, sqlNotasAtivas, userID)
	if err != nil {
		return nil, sanitize("ler as notas ativas", err)
	}
	defer rows.Close()
	var out []cloudstore.Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, sanitize("ler as notas ativas", err)
		}
		out = append(out, n)
	}
	return out, sanitize("ler as notas ativas", rows.Err())
}

func (s *Store) GetNote(ctx context.Context, userID, id string) (cloudstore.Note, error) {
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.Note{}, cloudstore.ErrNoteNotFound
	}
	n, err := scanNote(s.pool.QueryRow(ctx, sqlSelectNote, userID, canon))
	if errors.Is(err, pgx.ErrNoRows) {
		return cloudstore.Note{}, cloudstore.ErrNoteNotFound
	}
	if err != nil {
		return cloudstore.Note{}, sanitize("ler a nota", err)
	}
	return n, nil
}

func (s *Store) CreateNote(ctx context.Context, a cloudstore.Actor, n cloudstore.NovaNotaDoCelular, now time.Time) (cloudstore.Note, error) {
	if a.DeviceID != "" {
		return cloudstore.Note{}, cloudstore.ErrDeviceNotAllowed
	}
	id := cloudstore.NewUUIDv7()
	var out cloudstore.Note
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		pedido := n
		pedido.Marcadores = make([]string, 0, len(n.Marcadores))
		for _, m := range n.Marcadores {
			canon, ok := cloudstore.CanonicalUUID(m)
			if !ok {
				return false, cloudstore.ErrLabelNotFound
			}
			vivo, err := marcadorVivo(ctx, tx, a.UserID, canon)
			if err != nil {
				return false, err
			}
			if !vivo {
				return false, cloudstore.ErrLabelNotFound
			}
			pedido.Marcadores = append(pedido.Marcadores, canon)
		}
		topo, err := topoDoGrupo(ctx, tx, a.UserID, n.Arquivada, n.Fixada, "")
		if err != nil {
			return false, err
		}
		m, err := cloudstore.PlanejarNovaNota(id, pedido, topo, now)
		if err != nil {
			return false, err
		}
		o := cloudstore.DecideNote(m, nil, false, nil, a, now)
		if _, err := cloudstore.ResultadoDaNota(o, nil); err != nil {
			return false, err
		}
		if err := putNote(ctx, tx, o.NextNote, nil, seq, a, now); err != nil {
			return false, err
		}
		out = o.NextNote
		out.LastSeq = seq
		return true, nil
	})
	return out, err
}

func (s *Store) ChangeNote(ctx context.Context, a cloudstore.Actor, id string, p cloudstore.PedidoDaNota, now time.Time) (cloudstore.NotaGravada, error) {
	if a.DeviceID != "" {
		return cloudstore.NotaGravada{}, cloudstore.ErrDeviceNotAllowed
	}
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.NotaGravada{}, cloudstore.ErrNoteNotFound
	}
	var g cloudstore.NotaGravada
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		cur, err := readNoteForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if cur == nil {
			return false, cloudstore.ErrNoteNotFound
		}
		plano, err := cloudstore.PlanejarNota(p, *cur, contextoDaNota(ctx, tx, a.UserID), now)
		if err != nil {
			return false, err
		}
		if plano.Nada {
			g = cloudstore.NotaGravada{Nota: *cur}
			return false, nil
		}
		dead, err := deadLabels(ctx, tx, a.UserID, cloudstore.NoteLabelIDs(plano.Mutation))
		if err != nil {
			return false, err
		}
		o := cloudstore.DecideNote(plano.Mutation, cur, false, dead, a, now)
		if g, err = cloudstore.ResultadoDaNota(o, plano.Enviado); err != nil {
			return false, err
		}
		if o.Write == cloudstore.WriteNone {
			g.Nota = *cur
			return false, nil
		}
		if err := gravarNotaDecidida(ctx, tx, a, o, canon, cur, seq, now); err != nil {
			return false, err
		}
		g.Nota = o.NextNote
		if o.Write == cloudstore.WriteTombstone {
			g.Nota = cloudstore.Note{}
		} else {
			g.Nota.LastSeq = seq
		}
		return true, nil
	})
	return g, err
}

func (s *Store) CreateLabel(ctx context.Context, a cloudstore.Actor, nome string, now time.Time) (cloudstore.Label, error) {
	if a.DeviceID != "" {
		return cloudstore.Label{}, cloudstore.ErrDeviceNotAllowed
	}
	id := cloudstore.NewUUIDv7()
	var out cloudstore.Label
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		todos, err := marcadoresDaPessoa(ctx, tx, a.UserID)
		if err != nil {
			return false, err
		}
		m, err := cloudstore.PlanejarMarcadorNovo(id, nome, todos, now)
		if err != nil {
			return false, err
		}
		o := cloudstore.DecideLabel(m, nil, false, a, now)
		if o.Write == cloudstore.WriteNone {
			return false, fmt.Errorf("%w: %s", cloudstore.ErrInvalidNote, o.Result.Code)
		}
		if err := gravarMarcadorDecidido(ctx, tx, a, o, id, nil, seq, now); err != nil {
			return false, err
		}
		out = o.NextLabel
		out.LastSeq = seq
		return true, nil
	})
	return out, err
}

func (s *Store) RenameLabel(ctx context.Context, a cloudstore.Actor, id string, base int64, nome string, now time.Time) (cloudstore.Label, error) {
	if a.DeviceID != "" {
		return cloudstore.Label{}, cloudstore.ErrDeviceNotAllowed
	}
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.Label{}, cloudstore.ErrLabelNotFound
	}
	var out cloudstore.Label
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		cur, err := readLabelForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if cur == nil {
			return false, cloudstore.ErrLabelNotFound
		}
		todos, err := marcadoresDaPessoa(ctx, tx, a.UserID)
		if err != nil {
			return false, err
		}
		m, nada, err := cloudstore.PlanejarRenomear(*cur, base, nome, todos)
		if err != nil {
			return false, err
		}
		if nada {
			out = *cur
			return false, nil
		}
		o := cloudstore.DecideLabel(m, cur, false, a, now)
		if o.Write == cloudstore.WriteNone {
			out = *cur
			return false, nil
		}
		if err := gravarMarcadorDecidido(ctx, tx, a, o, canon, cur, seq, now); err != nil {
			return false, err
		}
		out = o.NextLabel
		out.LastSeq = seq
		return true, nil
	})
	return out, err
}

func (s *Store) DeleteLabel(ctx context.Context, a cloudstore.Actor, id string, now time.Time) error {
	if a.DeviceID != "" {
		return cloudstore.ErrDeviceNotAllowed
	}
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.ErrLabelNotFound
	}
	return s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		cur, err := readLabelForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if cur == nil {
			return false, cloudstore.ErrLabelNotFound
		}
		o := cloudstore.DecideLabel(cloudstore.Mutation{
			MutationID: cloudstore.NewUUIDv7(), EntityType: cloudstore.EntityLabel, EntityID: canon, Operation: cloudstore.OpDelete,
		}, cur, false, a, now)
		if o.Write != cloudstore.WriteTombstone {
			return false, fmt.Errorf("%w: %s", cloudstore.ErrInvalidNote, o.Result.Code)
		}
		if err := gravarMarcadorDecidido(ctx, tx, a, o, canon, cur, seq, now); err != nil {
			return false, err
		}
		return true, nil
	})
}

// notaDoDitado lê a nota ligada ao ditado, travada (nil sem ligação ou sem a
// nota).
func notaDoDitado(ctx context.Context, tx pgx.Tx, userID string, t cloudstore.Transcription) (*cloudstore.Note, error) {
	if t.VirouNota == nil {
		return nil, nil
	}
	return readNoteForUpdate(ctx, tx, userID, *t.VirouNota)
}

func (s *Store) CreateParaNota(ctx context.Context, a cloudstore.Actor, epoch, id string, n cloudstore.NewDictation, now time.Time) (cloudstore.Transcription, bool, cloudstore.DestinoDoDitado, error) {
	now = cloudstore.Normalize(now)
	n.VirouNota = nil
	t, err := cloudstore.NewTranscription(a, id, n, now)
	if err != nil {
		return cloudstore.Transcription{}, false, cloudstore.DestinoDoDitado{}, err
	}
	var (
		out     cloudstore.Transcription
		created bool
		destino cloudstore.DestinoDoDitado
	)
	err = s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, current string) (bool, error) {
		if epoch == "" || epoch != current {
			return false, cloudstore.ErrEpochChanged
		}
		cur, err := readForUpdate(ctx, tx, a.UserID, t.ID)
		if err != nil {
			return false, err
		}
		if cur != nil {
			nota, err := notaDoDitado(ctx, tx, a.UserID, *cur)
			if err != nil {
				return false, err
			}
			out, destino = *cur, cloudstore.DestinoGravado(*cur, nota)
			return false, nil
		}
		var tomb bool
		if err := tx.QueryRow(ctx, sqlTombstoneExists, a.UserID, t.ID).Scan(&tomb); err != nil {
			return false, sanitize("ler a lápide", err)
		}
		if tomb {
			return false, cloudstore.ErrTombstoned
		}
		ativas, err := notasAtivas(ctx, tx, a.UserID)
		if err != nil {
			return false, err
		}
		topo, err := topoDoGrupo(ctx, tx, a.UserID, false, false, "")
		if err != nil {
			return false, err
		}
		plano, o, err := cloudstore.DecidirParaNota(ativas, t, topo, a, now)
		if err != nil {
			return false, err
		}
		var alvo *cloudstore.Note
		if plano.Alvo != nil {
			// A nota de destino, agora travada: o contador já serializa as
			// gravações do usuário, e o FOR UPDATE é a segunda guarda do UPDATE.
			if alvo, err = readNoteForUpdate(ctx, tx, a.UserID, plano.Alvo.ID); err != nil {
				return false, err
			}
			if alvo == nil || alvo.Version != plano.Alvo.Version {
				// Não acontece com o contador travado; se acontecer, nada grava.
				return false, errors.New("pgstore: a nota de destino mudou durante a transação")
			}
		}
		if err := gravarNotaDecidida(ctx, tx, a, o, plano.Destino.NotaID, alvo, seq, now); err != nil {
			return false, err
		}
		seqDoDitado, err := proximoSeq(ctx, tx, a.UserID)
		if err != nil {
			return false, err
		}
		nota := plano.Destino.NotaID
		t.VirouNota = &nota
		if err := put(ctx, tx, t, true, 0, seqDoDitado, a, now); err != nil {
			return false, err
		}
		if err := gravarVirouNota(ctx, tx, a.UserID, t.ID, nil, t.VirouNota); err != nil {
			return false, err
		}
		t.LastSeq = seqDoDitado
		out, created, destino = t, true, plano.Destino
		return true, nil
	})
	if err != nil {
		return cloudstore.Transcription{}, false, cloudstore.DestinoDoDitado{}, err
	}
	return out, created, destino, nil
}

func (s *Store) UndoParaNota(ctx context.Context, a cloudstore.Actor, id string, p cloudstore.PedidoDeDesfazer, now time.Time) (cloudstore.Desfeito, error) {
	if a.DeviceID != "" {
		return cloudstore.Desfeito{}, cloudstore.ErrDeviceNotAllowed
	}
	now = cloudstore.Normalize(now)
	canon, ok := cloudstore.CanonicalUUID(id)
	if !ok {
		return cloudstore.Desfeito{}, cloudstore.ErrNotFound
	}
	var out cloudstore.Desfeito
	err := s.gravar(ctx, a.UserID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		t, err := readForUpdate(ctx, tx, a.UserID, canon)
		if err != nil {
			return false, err
		}
		if t == nil {
			return false, cloudstore.ErrNotFound
		}
		n, err := notaDoDitado(ctx, tx, a.UserID, *t)
		if err != nil {
			return false, err
		}
		nota, ditado, notaApagada, err := cloudstore.DecidirDesfazer(*t, n, p, a, now)
		if err != nil {
			return false, err
		}
		seqDoDitado := seq
		if nota != nil {
			if err := gravarNotaDecidida(ctx, tx, a, *nota, n.ID, n, seq, now); err != nil {
				return false, err
			}
			if seqDoDitado, err = proximoSeq(ctx, tx, a.UserID); err != nil {
				return false, err
			}
		}
		if err := put(ctx, tx, ditado.Next, false, t.Version, seqDoDitado, a, now); err != nil {
			return false, err
		}
		if err := gravarVirouNota(ctx, tx, a.UserID, t.ID, t.VirouNota, ditado.Next.VirouNota); err != nil {
			return false, err
		}
		out = cloudstore.Desfeito{Ditado: ditado.Next, NotaVoltou: nota != nil, NotaApagada: notaApagada}
		out.Ditado.LastSeq = seqDoDitado
		return true, nil
	})
	return out, err
}
