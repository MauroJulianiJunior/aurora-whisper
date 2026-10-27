package cloudstore

import (
	"context"
	"fmt"
	"time"
)

// As escritas do celular nas notas e nos marcadores no MemoryStore (ADR-022,
// N8; notas_celular.go): as mesmas do Postgres (pgstore/notas_celular.go), sob
// o mutex do Store. O plano (PlanejarNota e os outros) é decidido antes de
// qualquer gravação: um erro no meio nunca deixa metade gravada.

// writeVarios é o write das gravações que gastam mais de um seq (o "Ditar para
// nota" grava a nota e o ditado; o Desfazer, a nota e o ditado): cada seq sai
// de proximo, na ordem. fn só grava depois de decidir tudo; com erro, nenhum
// seq é gasto.
func (s *MemoryStore) writeVarios(userID string, fn func(u *memUser, proximo func() int64) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return ErrUnknownUser
	}
	ultimo := u.lastSeq
	proximo := func() int64 {
		ultimo++
		return ultimo
	}
	if err := fn(u, proximo); err != nil {
		return err
	}
	u.lastSeq = ultimo
	return nil
}

// contextoDaNota é o que o plano lê do usuário (ContextoDaNota), sob o mutex.
func (u *memUser) contextoDaNota() ContextoDaNota {
	return ContextoDaNota{
		Nota: func(id string) (*Note, error) {
			if n, ok := u.notes[id]; ok {
				return &n, nil
			}
			return nil, nil
		},
		Topo: func(arquivada, fixada bool, exceto string) (string, error) {
			return u.topoDoGrupo(arquivada, fixada, exceto), nil
		},
		MarcadorVivo: func(id string) (bool, error) {
			_, ok := u.labels[id]
			return ok, nil
		},
	}
}

// topoDoGrupo é a menor posição do grupo fora da lixeira, sem a nota exceto.
func (u *memUser) topoDoGrupo(arquivada, fixada bool, exceto string) string {
	topo := ""
	for _, n := range u.notes {
		if n.DeletedAt != nil || n.Arquivada != arquivada || n.Fixada != fixada || n.ID == exceto || n.Posicao == "" {
			continue
		}
		if topo == "" || n.Posicao < topo {
			topo = n.Posicao
		}
	}
	return topo
}

func (u *memUser) todosOsMarcadores() []Label {
	out := make([]Label, 0, len(u.labels))
	for _, l := range u.labels {
		out = append(out, l)
	}
	return out
}

func (s *MemoryStore) GetNote(_ context.Context, userID, id string) (Note, error) {
	canon, ok := CanonicalUUID(id)
	if !ok {
		return Note{}, ErrNoteNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok {
		if n, ok := u.notes[canon]; ok {
			return n, nil
		}
	}
	return Note{}, ErrNoteNotFound
}

func (s *MemoryStore) CreateNote(_ context.Context, a Actor, n NovaNotaDoCelular, now time.Time) (Note, error) {
	if a.DeviceID != "" {
		return Note{}, ErrDeviceNotAllowed
	}
	id := NewUUIDv7()
	var out Note
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		pedido := n
		pedido.Marcadores = make([]string, 0, len(n.Marcadores))
		for _, m := range n.Marcadores {
			canon, ok := CanonicalUUID(m)
			if _, vivo := u.labels[canon]; !ok || !vivo {
				return false, ErrLabelNotFound
			}
			pedido.Marcadores = append(pedido.Marcadores, canon)
		}
		m, err := PlanejarNovaNota(id, pedido, u.topoDoGrupo(n.Arquivada, n.Fixada, ""), now)
		if err != nil {
			return false, err
		}
		o := DecideNote(m, nil, false, nil, a, now)
		if _, err := ResultadoDaNota(o, nil); err != nil {
			return false, err
		}
		u.gravarNota(o, id, seq, a, now)
		out = u.notes[id]
		return true, nil
	})
	return out, err
}

func (s *MemoryStore) ChangeNote(_ context.Context, a Actor, id string, p PedidoDaNota, now time.Time) (NotaGravada, error) {
	if a.DeviceID != "" {
		return NotaGravada{}, ErrDeviceNotAllowed
	}
	canon, ok := CanonicalUUID(id)
	if !ok {
		return NotaGravada{}, ErrNoteNotFound
	}
	var g NotaGravada
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		cur, ok := u.notes[canon]
		if !ok {
			return false, ErrNoteNotFound
		}
		plano, err := PlanejarNota(p, cur, u.contextoDaNota(), now)
		if err != nil {
			return false, err
		}
		if plano.Nada {
			g = NotaGravada{Nota: cur}
			return false, nil
		}
		o := DecideNote(plano.Mutation, &cur, false, u.marcadoresMortos(NoteLabelIDs(plano.Mutation)), a, now)
		if g, err = ResultadoDaNota(o, plano.Enviado); err != nil {
			return false, err
		}
		if o.Write == WriteNone {
			g.Nota = cur
			return false, nil
		}
		u.gravarNota(o, canon, seq, a, now)
		g.Nota = u.notes[canon]
		return true, nil
	})
	return g, err
}

func (s *MemoryStore) CreateLabel(_ context.Context, a Actor, nome string, now time.Time) (Label, error) {
	if a.DeviceID != "" {
		return Label{}, ErrDeviceNotAllowed
	}
	id := NewUUIDv7()
	var out Label
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		m, err := PlanejarMarcadorNovo(id, nome, u.todosOsMarcadores(), now)
		if err != nil {
			return false, err
		}
		o := DecideLabel(m, nil, false, a, now)
		if o.Write == WriteNone {
			return false, fmt.Errorf("%w: %s", ErrInvalidNote, o.Result.Code)
		}
		u.gravarMarcador(o, id, seq, a, now)
		out = u.labels[id]
		return true, nil
	})
	return out, err
}

func (s *MemoryStore) RenameLabel(_ context.Context, a Actor, id string, base int64, nome string, now time.Time) (Label, error) {
	if a.DeviceID != "" {
		return Label{}, ErrDeviceNotAllowed
	}
	canon, ok := CanonicalUUID(id)
	if !ok {
		return Label{}, ErrLabelNotFound
	}
	var out Label
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		cur, ok := u.labels[canon]
		if !ok {
			return false, ErrLabelNotFound
		}
		m, nada, err := PlanejarRenomear(cur, base, nome, u.todosOsMarcadores())
		if err != nil {
			return false, err
		}
		if nada {
			out = cur
			return false, nil
		}
		o := DecideLabel(m, &cur, false, a, now)
		if o.Write == WriteNone {
			out = cur
			return false, nil
		}
		u.gravarMarcador(o, canon, seq, a, now)
		out = u.labels[canon]
		return true, nil
	})
	return out, err
}

func (s *MemoryStore) DeleteLabel(_ context.Context, a Actor, id string, now time.Time) error {
	if a.DeviceID != "" {
		return ErrDeviceNotAllowed
	}
	canon, ok := CanonicalUUID(id)
	if !ok {
		return ErrLabelNotFound
	}
	return s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		cur, ok := u.labels[canon]
		if !ok {
			return false, ErrLabelNotFound
		}
		o := DecideLabel(Mutation{MutationID: NewUUIDv7(), EntityType: EntityLabel, EntityID: canon, Operation: OpDelete}, &cur, false, a, now)
		if o.Write != WriteTombstone {
			return false, fmt.Errorf("%w: %s", ErrInvalidNote, o.Result.Code)
		}
		u.gravarMarcador(o, canon, seq, a, now)
		return true, nil
	})
}

// notaDoDitado é a nota ligada ao ditado (nil: sem ligação, ou a nota não existe).
func (u *memUser) notaDoDitado(t Transcription) *Note {
	if t.VirouNota == nil {
		return nil
	}
	if n, ok := u.notes[*t.VirouNota]; ok {
		return &n
	}
	return nil
}

// ativas são as notas fora da lixeira e do Arquivo: as que o "Ditar para nota"
// pode escolher.
func (u *memUser) ativas() []Note {
	var out []Note
	for _, n := range u.notes {
		if n.DeletedAt == nil && !n.Arquivada {
			out = append(out, n)
		}
	}
	return out
}

func (s *MemoryStore) CreateParaNota(_ context.Context, a Actor, epoch, id string, n NewDictation, now time.Time) (Transcription, bool, DestinoDoDitado, error) {
	now = Normalize(now)
	n.VirouNota = nil
	t, err := NewTranscription(a, id, n, now)
	if err != nil {
		return Transcription{}, false, DestinoDoDitado{}, err
	}
	var (
		out     Transcription
		created bool
		destino DestinoDoDitado
	)
	err = s.writeVarios(a.UserID, func(u *memUser, proximo func() int64) error {
		if epoch == "" || epoch != u.epoch {
			return ErrEpochChanged
		}
		if cur, ok := u.items[t.ID]; ok {
			out, destino = cur, DestinoGravado(cur, u.notaDoDitado(cur))
			return nil
		}
		if _, ok := u.tombs[t.ID]; ok {
			return ErrTombstoned
		}
		plano, o, err := DecidirParaNota(u.ativas(), t, u.topoDoGrupo(false, false, ""), a, now)
		if err != nil {
			return err
		}
		u.gravarNota(o, plano.Destino.NotaID, proximo(), a, now)
		nota := plano.Destino.NotaID
		t.VirouNota = &nota
		u.put(t, proximo(), a, now)
		out, created, destino = u.items[t.ID], true, plano.Destino
		return nil
	})
	if err != nil {
		return Transcription{}, false, DestinoDoDitado{}, err
	}
	return out, created, destino, nil
}

// DecidirParaNota planeja e decide a nota do "Ditar para nota". O acrescentar
// que as regras recusam (o corpo passaria do teto) vira a nota nova com o
// ditado: nada se perde. Os dois Stores a usam.
func DecidirParaNota(ativas []Note, t Transcription, topo string, a Actor, now time.Time) (PlanoDoParaNota, Outcome, error) {
	novaID := NewUUIDv7()
	plano, err := PlanejarParaNota(ativas, t.FinalText, t.ID, novaID, topo, now)
	if err != nil {
		return PlanoDoParaNota{}, Outcome{}, err
	}
	o := DecideNote(plano.Mutation, plano.Alvo, false, nil, a, now)
	if plano.Alvo != nil && (o.Result.Status != StatusApplied || o.Write == WriteNone) {
		if plano, err = PlanejarParaNota(nil, t.FinalText, t.ID, novaID, topo, now); err != nil {
			return PlanoDoParaNota{}, Outcome{}, err
		}
		o = DecideNote(plano.Mutation, nil, false, nil, a, now)
	}
	if o.Result.Status != StatusApplied || o.Write == WriteNone {
		return PlanoDoParaNota{}, Outcome{}, fmt.Errorf("%w: %s", ErrInvalidNote, o.Result.Code)
	}
	return plano, o, nil
}

func (s *MemoryStore) UndoParaNota(_ context.Context, a Actor, id string, p PedidoDeDesfazer, now time.Time) (Desfeito, error) {
	if a.DeviceID != "" {
		return Desfeito{}, ErrDeviceNotAllowed
	}
	now = Normalize(now)
	canon, ok := CanonicalUUID(id)
	if !ok {
		return Desfeito{}, ErrNotFound
	}
	var out Desfeito
	err := s.writeVarios(a.UserID, func(u *memUser, proximo func() int64) error {
		t, ok := u.items[canon]
		if !ok {
			return ErrNotFound
		}
		n := u.notaDoDitado(t)
		nota, ditado, notaApagada, err := DecidirDesfazer(t, n, p, a, now)
		if err != nil {
			return err
		}
		if nota != nil {
			u.gravarNota(*nota, n.ID, proximo(), a, now)
		}
		u.put(ditado.Next, proximo(), a, now)
		out = Desfeito{Ditado: u.items[canon], NotaVoltou: nota != nil, NotaApagada: notaApagada}
		return nil
	})
	return out, err
}

// DecidirDesfazer decide o Desfazer inteiro antes de gravar: a nota (nil: fica
// como está) e o ditado que volta ao histórico. Os dois Stores a usam.
func DecidirDesfazer(t Transcription, n *Note, p PedidoDeDesfazer, a Actor, now time.Time) (*Outcome, Outcome, bool, error) {
	plano, err := PlanejarDesfazer(t, n, p)
	if err != nil {
		return nil, Outcome{}, false, err
	}
	var nota *Outcome
	if plano.Nota != nil {
		o := DecideNote(*plano.Nota, n, false, nil, a, now)
		if o.Result.Status == StatusApplied && o.Write != WriteNone {
			nota = &o
		}
	}
	ditado := DecidePush(plano.Ditado, &t, false, a, now)
	if ditado.Result.Status != StatusApplied || ditado.Write == WriteNone {
		return nil, Outcome{}, false, ErrNotLinked
	}
	return nota, ditado, nota != nil && plano.NotaApagada, nil
}
