package cloudstore

import (
	"context"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
)

// As imagens das notas no MemoryStore (ADR-022, N10b; imagens.go): as mesmas
// regras do Postgres (pgstore/imagens.go), sob o mutex do Store. As linhas
// ficam em memUser.images (vivas e com lápide, pelo id) e os bytes em
// memUser.imageBytes (só as vivas). A nota guardada em memUser.notes nunca leva
// as imagens: elas entram na hora de devolver a nota (comImagens), como a
// subconsulta do Postgres.

// comImagens devolve n com as imagens vivas dela, na ordem da nota. Nunca nil.
func (u *memUser) comImagens(n Note) Note {
	n.Imagens = u.imagensDaNota(n.ID)
	return n
}

// imagensDaNota são as imagens vivas da nota, na ordem dela. Nunca nil.
func (u *memUser) imagensDaNota(noteID string) []NoteImage {
	out := []NoteImage{}
	for _, im := range u.images {
		if im.NoteID == noteID && im.Viva() {
			out = append(out, im)
		}
	}
	OrdenarImagens(out)
	return out
}

// lugarDaImagem lê o que DecidirAnexo e DecidirTirar precisam, sob o mutex.
func (u *memUser) lugarDaImagem(noteID, id string) LugarDaImagem {
	var l LugarDaImagem
	if n, ok := u.notes[noteID]; ok {
		l.Nota = &n
	}
	_, l.NotaComLapide = u.noteTombs[noteTombKey(EntityNote, noteID)]
	if im, ok := u.images[id]; ok {
		l.Atual = &im
	}
	for _, im := range u.images {
		if !im.Viva() {
			continue
		}
		l.Total += im.Bytes
		if im.NoteID == noteID {
			l.NaNota++
		}
	}
	return l
}

// tirarImagensDaNota apaga as linhas e os bytes das imagens da nota (as vivas e
// as lápides): a cascata da chave estrangeira do Postgres, quando a nota sai de
// vez.
func (u *memUser) tirarImagensDaNota(noteID string) {
	for id, im := range u.images {
		if im.NoteID == noteID {
			delete(u.images, id)
			delete(u.imageBytes, id)
		}
	}
}

// guardarImagem grava a imagem viva com o seq e os bytes (uma cópia: quem
// chamou pode mexer no slice depois).
func (u *memUser) guardarImagem(im NoteImage, b []byte, seq int64) {
	im.LastSeq = seq
	u.images[im.ID] = im
	u.imageBytes[im.ID] = append([]byte(nil), b...)
}

// pullImages são as mudanças das imagens depois de afterSeq: a viva é upsert
// (os metadados), a da lápide é tombstone. Sem aparelho de origem: a imagem
// não tem evento próprio (a linha carrega o last_seq).
func (u *memUser) pullImages(afterSeq int64) []Change {
	var changes []Change
	for _, im := range u.images {
		if im.LastSeq <= afterSeq {
			continue
		}
		c := Change{Seq: im.LastSeq, EntityType: EntityNoteImage, EntityID: im.ID, Operation: OpTombstone}
		if im.Viva() {
			img := im
			c.Operation, c.EntityVersion, c.Image = OpUpsert, 1, &img
		}
		changes = append(changes, c)
	}
	return changes
}

func (s *MemoryStore) AttachImage(_ context.Context, a Actor, n ImagemNova, now time.Time) (NoteImage, bool, error) {
	noteID, ok := CanonicalUUID(n.NoteID)
	if !ok {
		return NoteImage{}, false, ErrNoteNotFound
	}
	id, ok := CanonicalUUID(n.ID)
	if !ok {
		return NoteImage{}, false, ErrImageNotFound
	}
	n.NoteID, n.ID = noteID, id
	var out NoteImage
	var created bool
	// A linha não guarda o aparelho (a 0026): o do Actor é o do token que o
	// autenticou, e não é conferido de novo aqui (como no Postgres).
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		im, grava, err := DecidirAnexo(u.lugarDaImagem(noteID, id), a.UserID, n, now)
		if err != nil {
			return false, err
		}
		if !grava {
			out = im
			return false, nil
		}
		u.guardarImagem(im, n.Limpa.Bytes, seq)
		out, created = u.images[id], true
		return true, nil
	})
	return out, created, err
}

func (s *MemoryStore) CreateNoteWithImage(_ context.Context, a Actor, img imagemdanota.Limpa, now time.Time) (Note, error) {
	if a.DeviceID != "" {
		return Note{}, ErrDeviceNotAllowed
	}
	id, idDaImagem := NewUUIDv7(), NewUUIDv7()
	var out Note
	err := s.writeVarios(a.UserID, func(u *memUser, proximo func() int64) error {
		m, err := PlanejarNotaComImagem(id, u.topoDoGrupo(false, false, ""), now)
		if err != nil {
			return err
		}
		o := DecideNote(m, nil, false, nil, a, now)
		if _, err := ResultadoDaNota(o, nil); err != nil {
			return err
		}
		l := u.lugarDaImagem(id, idDaImagem)
		nova := o.NextNote
		l.Nota, l.NaNota = &nova, 0
		im, _, err := DecidirAnexo(l, a.UserID, ImagemNova{ID: idDaImagem, NoteID: id, Limpa: img}, now)
		if err != nil {
			return err
		}
		u.gravarNota(o, id, proximo(), a, now)
		u.guardarImagem(im, img.Bytes, proximo())
		out = u.comImagens(u.notes[id])
		return nil
	})
	return out, err
}

func (s *MemoryStore) ImageInfo(_ context.Context, userID, id string) (NoteImage, bool, error) {
	canon, ok := CanonicalUUID(id)
	if !ok {
		return NoteImage{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return NoteImage{}, false, nil
	}
	im, ok := u.images[canon]
	return im, ok, nil
}

func (s *MemoryStore) GetImage(_ context.Context, userID, noteID, id string) (NoteImage, []byte, error) {
	canonNota, ok1 := CanonicalUUID(noteID)
	canon, ok2 := CanonicalUUID(id)
	if !ok1 || !ok2 {
		return NoteImage{}, nil, ErrImageNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return NoteImage{}, nil, ErrImageNotFound
	}
	im, ok := u.images[canon]
	if !ok || !im.Viva() || im.NoteID != canonNota {
		return NoteImage{}, nil, ErrImageNotFound
	}
	return im, append([]byte(nil), u.imageBytes[canon]...), nil
}

func (s *MemoryStore) RemoveImage(_ context.Context, a Actor, noteID, id string, now time.Time) error {
	canonNota, ok := CanonicalUUID(noteID)
	if !ok {
		return ErrNoteNotFound
	}
	canon, ok := CanonicalUUID(id)
	if !ok {
		return ErrImageNotFound
	}
	return s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		l := u.lugarDaImagem(canonNota, canon)
		acao, err := DecidirTirar(l, canonNota)
		if err != nil || acao == TirarNada {
			return false, err
		}
		quando := Normalize(now)
		im := NoteImage{ID: canon, UserID: a.UserID, NoteID: canonNota, CreatedAt: quando}
		if acao == TirarLapide {
			im = *l.Atual
		}
		im.RemovedAt, im.LastSeq = &quando, seq
		u.images[canon] = im
		delete(u.imageBytes, canon)
		return true, nil
	})
}
