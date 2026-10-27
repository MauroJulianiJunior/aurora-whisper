package cloudstore

import (
	"context"
	"sort"
	"time"
)

// A nota e o marcador de nota no MemoryStore (ADR-022, N7): as mesmas regras
// do Postgres (pgstore/notas.go), sob o mutex do Store. Os eventos das notas
// ficam numa lista à parte (noteEvents), como no Postgres, onde moram na
// note_sync_events: o CHECK do entity_type da sync_events não aceita tipo novo.

// noteTombKey é a chave da lápide de nota ou de marcador (a PK da
// note_tombstones é (user_id, entity_type, entity_id)).
func noteTombKey(entityType, id string) string { return entityType + "|" + id }

// putNoteEvent grava o evento de nota ou de marcador com o seq.
func (u *memUser) putNoteEvent(entityType, id, op string, version, seq int64, a Actor, now time.Time) {
	u.noteEvents = append(u.noteEvents, Event{
		Seq: seq, EntityType: entityType, EntityID: id, Operation: op,
		EntityVersion: version, DeviceID: a.Device(), ChangedAt: Normalize(now),
	})
}

// pushNote decide e grava, sob o mutex, uma mutação de nota. wrote diz se o
// seq foi gasto.
func (u *memUser) pushNote(a Actor, m Mutation, entityID string, seq int64, now time.Time) (Outcome, bool) {
	var curPtr *Note
	if cur, ok := u.notes[entityID]; ok {
		curPtr = &cur
	}
	_, tomb := u.noteTombs[noteTombKey(EntityNote, entityID)]
	out := DecideNote(m, curPtr, tomb, u.marcadoresMortos(NoteLabelIDs(m)), a, now)
	if out.Write == WriteNone {
		return out, false
	}
	u.gravarNota(out, entityID, seq, a, now)
	if out.Result.NoteState != nil {
		st := u.notes[entityID]
		out.Result.NoteState = &st
	}
	return out, true
}

// gravarNota grava, com o seq, o que DecideNote decidiu (Write não é
// WriteNone): a lápide e o evento tombstone, ou a linha e o evento upsert.
func (u *memUser) gravarNota(out Outcome, entityID string, seq int64, a Actor, now time.Time) {
	if out.Write == WriteTombstone {
		delete(u.notes, entityID)
		u.noteTombs[noteTombKey(EntityNote, entityID)] = seq
		u.putNoteEvent(EntityNote, entityID, OpTombstone, out.Result.EntityVersion, seq, a, now)
		return
	}
	n := out.NextNote
	n.LastSeq = seq
	u.notes[n.ID] = n
	u.putNoteEvent(EntityNote, n.ID, OpUpsert, n.Version, seq, a, now)
}

// marcadoresMortos são os ids, entre os dados, com lápide de marcador.
func (u *memUser) marcadoresMortos(ids []string) map[string]bool {
	dead := map[string]bool{}
	for _, id := range ids {
		if _, ok := u.noteTombs[noteTombKey(EntityLabel, id)]; ok {
			dead[id] = true
		}
	}
	return dead
}

// pushLabel decide e grava, sob o mutex, uma mutação de marcador de nota. O
// apagar de vez tira o marcador de todas as notas do usuário, sem subir a
// versão delas (o PC que apagou manda as notas mudadas; os outros tiram as
// ligações ao receber a lápide).
func (u *memUser) pushLabel(a Actor, m Mutation, entityID string, seq int64, now time.Time) (Outcome, bool) {
	var curPtr *Label
	if cur, ok := u.labels[entityID]; ok {
		curPtr = &cur
	}
	_, tomb := u.noteTombs[noteTombKey(EntityLabel, entityID)]
	out := DecideLabel(m, curPtr, tomb, a, now)
	if out.Write == WriteNone {
		return out, false
	}
	u.gravarMarcador(out, entityID, seq, a, now)
	return out, true
}

// gravarMarcador grava, com o seq, o que DecideLabel decidiu (Write não é
// WriteNone). A lápide tira o marcador de todas as notas, sem subir a versão
// delas.
func (u *memUser) gravarMarcador(out Outcome, entityID string, seq int64, a Actor, now time.Time) {
	if out.Write == WriteTombstone {
		delete(u.labels, entityID)
		for id, n := range u.notes {
			if set := liveLabels(n.Marcadores, map[string]bool{entityID: true}); len(set) != len(n.Marcadores) {
				n.Marcadores = set
				u.notes[id] = n
			}
		}
		u.noteTombs[noteTombKey(EntityLabel, entityID)] = seq
		u.putNoteEvent(EntityLabel, entityID, OpTombstone, out.Result.EntityVersion, seq, a, now)
		return
	}
	l := out.NextLabel
	l.LastSeq = seq
	u.labels[l.ID] = l
	u.putNoteEvent(EntityLabel, l.ID, OpUpsert, l.Version, seq, a, now)
}

// pullNotes são as mudanças de notas, marcadores e lápides deles depois de
// afterSeq, com o aparelho de origem pelo evento do last_seq.
func (u *memUser) pullNotes(afterSeq int64) []Change {
	origin := make(map[int64]*string, len(u.noteEvents))
	for _, e := range u.noteEvents {
		origin[e.Seq] = e.DeviceID
	}
	var changes []Change
	for _, n := range u.notes {
		if n.LastSeq > afterSeq {
			note := n
			changes = append(changes, Change{
				Seq: n.LastSeq, EntityType: EntityNote, EntityID: n.ID,
				Operation: OpUpsert, EntityVersion: n.Version, DeviceID: origin[n.LastSeq], Note: &note,
			})
		}
	}
	for _, l := range u.labels {
		if l.LastSeq > afterSeq {
			label := l
			changes = append(changes, Change{
				Seq: l.LastSeq, EntityType: EntityLabel, EntityID: l.ID,
				Operation: OpUpsert, EntityVersion: l.Version, DeviceID: origin[l.LastSeq], Label: &label,
			})
		}
	}
	for key, seq := range u.noteTombs {
		if seq > afterSeq {
			entityType, id := splitTombKey(key)
			changes = append(changes, Change{Seq: seq, EntityType: entityType, EntityID: id, Operation: OpTombstone})
		}
	}
	return changes
}

func splitTombKey(key string) (entityType, id string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}

// ListNotes lista as notas da vista, na ordem de NoteBefore, até o limite e o
// orçamento de texto.
func (s *MemoryStore) ListNotes(_ context.Context, userID, vista string, limit int) (NotePage, error) {
	if !VistaValida(vista) {
		return NotePage{}, ErrInvalidFilter
	}
	limit = NotesListLimit(limit)
	var notes []Note
	s.mu.Lock()
	if u, ok := s.users[userID]; ok {
		for _, n := range u.notes {
			if NaVista(vista, n) {
				notes = append(notes, n)
			}
		}
	}
	s.mu.Unlock()
	sort.Slice(notes, func(i, j int) bool { return NoteBefore(vista, notes[i], notes[j]) })
	n, more := CutPage(notes, limit, NoteTextBytes)
	page := NotePage{Items: notes[:n], HasMore: more}
	if page.Items == nil {
		page.Items = []Note{}
	}
	return page, nil
}

// ListLabels lista os marcadores pelo nome (LabelBefore), até o limite.
func (s *MemoryStore) ListLabels(_ context.Context, userID string, limit int) (LabelPage, error) {
	limit = NotesListLimit(limit)
	var labels []Label
	s.mu.Lock()
	if u, ok := s.users[userID]; ok {
		for _, l := range u.labels {
			labels = append(labels, l)
		}
	}
	s.mu.Unlock()
	sort.Slice(labels, func(i, j int) bool { return LabelBefore(labels[i], labels[j]) })
	page := LabelPage{Items: labels}
	if len(labels) > limit {
		page.Items, page.HasMore = labels[:limit], true
	}
	if page.Items == nil {
		page.Items = []Label{}
	}
	return page, nil
}

// NoteEvents devolve os eventos das notas e dos marcadores do usuário (os da
// note_sync_events), em ordem de seq. Só testes.
func (s *MemoryStore) NoteEvents(userID string) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return nil
	}
	return append([]Event(nil), u.noteEvents...)
}
