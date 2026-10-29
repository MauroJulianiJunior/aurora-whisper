package cloudstore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore é o Store em memória: as mesmas regras do Postgres, com um
// mutex no lugar da trava do contador. Serve aos testes de contrato (a mesma
// suíte roda contra ele e contra o Postgres) e a quem precisa de um servidor
// sem banco. Seguro para uso concorrente.
//
// Ele imita o que o esquema do Postgres garante e as regras não veem: o
// contador por usuário (um usuário sem contador não grava), o aparelho do
// mesmo usuário (a chave estrangeira composta) e a ordem do seq.
//
// Ele também implementa Accounts (memory_contas.go), sob o MESMO mutex: o
// "apagar meus dados" troca o epoch, apaga os ditados, o aprendido e os
// aparelhos e derruba as sessões de uma vez, como a transação do Postgres.
type MemoryStore struct {
	mu    sync.Mutex
	users map[string]*memUser

	// Accounts (memory_contas.go).
	bySub    map[string]string // google_sub -> user_id
	byEmail  map[string]string // email -> user_id
	pending  map[string]LoginPendente
	sessions map[string]*memSession // HMAC em hex -> sessão
	devices  map[string]*memDevice  // device_id -> aparelho
	tokens   map[string]*memToken   // HMAC em hex -> token
	pedidos  map[string]*memPedido  // id -> pedido de pareamento (F6)

	// ganchoDaRetencao roda antes de cada candidato do Reter ser decidido
	// de novo sob o mutex (só testes: AntesDaTravaDaRetencao).
	ganchoDaRetencao func(id string)
}

type memUser struct {
	lastSeq   int64
	watermark int64
	epoch     string
	devices   map[string]bool
	items     map[string]Transcription
	tombs     map[string]memTomb
	learned   map[string]Learned
	events    []Event
	applied   map[string]memApplied
	// As notas e os marcadores de nota (N7; memory_notas.go): as linhas, as
	// lápides (pela chave noteTombKey, com o seq) e os eventos, à parte dos
	// do ditado como a note_sync_events do Postgres. A ligação do ditado com a
	// nota é o próprio Transcription.VirouNota.
	notes      map[string]Note
	labels     map[string]Label
	noteTombs  map[string]int64
	noteEvents []Event
	// As imagens das notas (N10b; memory_imagens.go): as linhas pelo id (as
	// vivas e as lápides, que ficam na linha) e os bytes das vivas. Saem com a
	// nota (a cascata do Postgres).
	images     map[string]NoteImage
	imageBytes map[string][]byte
	// chavesDeIA são as linhas de ai_provider_keys, pelo provedor (ADR-021,
	// C3a; chaves_ia.go). Nil até a primeira gravação.
	chavesDeIA map[string]ChaveDeIA
	// preferenciasDoDitado é a linha de dictation_settings (nil: a conta nunca
	// gravou, e vale PreferenciasPadrao) e aceitesDoDitado, as de
	// dictation_consents, do mais velho ao mais novo; só o último pode estar
	// em aberto (ADR-021, C3b; ditado_celular.go).
	preferenciasDoDitado *PreferenciasDoDitado
	aceitesDoDitado      []Aceite

	// A conta (vazios num usuário de AddUser).
	sub      string
	email    string
	nome     string
	disabled bool
}

type memTomb struct {
	seq        int64
	dictatedAt time.Time
}

// memApplied é a linha de applied_mutations: a resposta e quando foi aplicada
// (a arrumação conta por ela, PrazoDasMutacoes).
type memApplied struct {
	Applied
	at time.Time
}

// Event é uma linha de sync_events, para os testes conferirem a sequência.
type Event struct {
	Seq           int64
	EntityType    string
	EntityID      string
	Operation     string
	EntityVersion int64
	DeviceID      *string
	// ChangedAt é o relógio de quem gravou (changed_at): a arrumação conta
	// por ele (PrazoDosEventos).
	ChangedAt time.Time
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore cria um Store em memória vazio.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:    make(map[string]*memUser),
		bySub:    make(map[string]string),
		byEmail:  make(map[string]string),
		pending:  make(map[string]LoginPendente),
		sessions: make(map[string]*memSession),
		devices:  make(map[string]*memDevice),
		tokens:   make(map[string]*memToken),
		pedidos:  make(map[string]*memPedido),
	}
}

// AddUser cria o usuário e o contador dele (o que a F3 faz no primeiro
// login), com um epoch novo.
func (s *MemoryStore) AddUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; ok {
		return
	}
	s.users[userID] = newMemUser()
}

func newMemUser() *memUser {
	return &memUser{
		epoch:      NewUUIDv7(),
		devices:    make(map[string]bool),
		items:      make(map[string]Transcription),
		tombs:      make(map[string]memTomb),
		learned:    make(map[string]Learned),
		applied:    make(map[string]memApplied),
		notes:      make(map[string]Note),
		labels:     make(map[string]Label),
		noteTombs:  make(map[string]int64),
		images:     make(map[string]NoteImage),
		imageBytes: make(map[string][]byte),
	}
}

// AddDevice registra um aparelho do usuário, sem token: os testes que não
// passam pelo pareamento (F6) e os PCs fictícios do servidor de teste.
func (s *MemoryStore) AddDevice(userID, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok {
		u.devices[deviceID] = true
		s.devices[deviceID] = &memDevice{
			userID: userID, installID: "install-" + deviceID, name: "PC de teste", platform: "windows",
			version: 1, createdAt: time.Now(),
		}
	}
}

// TrocarEpoch troca o sync_epoch do usuário sem mexer nos dados: o que a
// restauração de um backup faz (ADR-019, F11), para os testes do recomeço do
// PC (F7a/b). Só testes e o servidor de teste usam o MemoryStore.
func (s *MemoryStore) TrocarEpoch(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok {
		u.epoch = NewUUIDv7()
	}
}

// Events devolve os eventos do usuário, em ordem de seq.
func (s *MemoryStore) Events(userID string) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return nil
	}
	return append([]Event(nil), u.events...)
}

// LastSeq devolve o contador do usuário.
func (s *MemoryStore) LastSeq(userID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[userID]; ok {
		return u.lastSeq
	}
	return 0
}

// write é o ponto único de gravação em memória: o seq é o próximo do
// contador, e só é gasto se fn gravar alguma coisa (wrote). Com o mutex
// preso do começo ao fim, a ordem do seq é a ordem das gravações.
func (s *MemoryStore) write(userID string, fn func(u *memUser, seq int64) (wrote bool, err error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return ErrUnknownUser
	}
	seq := u.lastSeq + 1
	wrote, err := fn(u, seq)
	if err != nil {
		return err
	}
	if wrote {
		u.lastSeq = seq
	}
	return nil
}

func (u *memUser) checkDevice(a Actor) error {
	if a.DeviceID != "" && !u.devices[a.DeviceID] {
		return ErrUnknownDevice
	}
	return nil
}

// put grava a linha e o evento com o seq e o relógio de quem gravou.
func (u *memUser) put(t Transcription, seq int64, a Actor, now time.Time) {
	t.LastSeq = seq
	u.items[t.ID] = t
	u.events = append(u.events, Event{
		Seq: seq, EntityType: EntityTranscription, EntityID: t.ID,
		Operation: OpUpsert, EntityVersion: t.Version, DeviceID: a.Device(), ChangedAt: Normalize(now),
	})
}

// putLearned grava a linha do aprendido e o evento com o seq. O evento de
// toda mudança do aprendido, o desfazer inclusive, é upsert (o CHECK da 0008:
// a linha desfeita continua, com undone_at).
func (u *memUser) putLearned(l Learned, seq int64, a Actor, now time.Time) {
	l.LastSeq = seq
	u.learned[l.ID] = l
	u.events = append(u.events, Event{
		Seq: seq, EntityType: EntityLearned, EntityID: l.ID,
		Operation: OpUpsert, EntityVersion: l.Version, DeviceID: a.Device(), ChangedAt: Normalize(now),
	})
}

func (u *memUser) get(id string) (Transcription, bool) {
	canon, ok := CanonicalUUID(id)
	if !ok {
		return Transcription{}, false
	}
	t, ok := u.items[canon]
	return t, ok
}

func (s *MemoryStore) List(_ context.Context, f ListFilter) (Page, error) {
	if err := CheckListFilter(f); err != nil {
		return Page{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	var cursorAt time.Time
	var cursorID string
	if f.Cursor != "" {
		var err error
		if cursorAt, cursorID, err = DecodeCursor(f.Cursor); err != nil {
			return Page{}, err
		}
	}
	var deviceID string
	if f.DeviceID != nil {
		canon, ok := CanonicalUUID(*f.DeviceID)
		if !ok {
			return Page{}, ErrInvalidFilter
		}
		deviceID = canon
	}
	query := strings.ToLower(f.Query)

	s.mu.Lock()
	var matched []Transcription
	if u, ok := s.users[f.UserID]; ok {
		for _, t := range u.items {
			if f.Trash != (t.DeletedAt != nil) {
				continue
			}
			// O ditado que virou nota sai da listagem do histórico (N7): ele
			// está na nota. O Get e o pull continuam o vendo.
			if t.VirouNota != nil {
				continue
			}
			if f.DeviceID != nil && (t.DeviceID == nil || *t.DeviceID != deviceID) {
				continue
			}
			if f.Mode != nil && t.Mode != *f.Mode {
				continue
			}
			if f.From != nil && t.CreatedAt.Before(*f.From) {
				continue
			}
			if f.To != nil && t.CreatedAt.After(*f.To) {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(t.RawText), query) &&
				!strings.Contains(strings.ToLower(t.FinalText), query) {
				continue
			}
			matched = append(matched, t)
		}
	}
	s.mu.Unlock()

	key := func(t Transcription) time.Time {
		if f.Trash {
			return *t.DeletedAt
		}
		return t.CreatedAt
	}
	// Mais novo primeiro; no empate, o id maior primeiro.
	sort.Slice(matched, func(i, j int) bool {
		ki, kj := key(matched[i]), key(matched[j])
		if ki.Equal(kj) {
			return matched[i].ID > matched[j].ID
		}
		return ki.After(kj)
	})
	if f.Cursor != "" {
		start := 0
		for start < len(matched) {
			k := key(matched[start])
			if k.Before(cursorAt) || k.Equal(cursorAt) && matched[start].ID < cursorID {
				break
			}
			start++
		}
		matched = matched[start:]
	}
	n, more := CutPage(matched, limit, TextBytes)
	page := Page{Items: matched[:n]}
	if more {
		last := page.Items[n-1]
		page.NextCursor = EncodeCursor(key(last), last.ID)
	}
	if page.Items == nil {
		page.Items = []Transcription{}
	}
	return page, nil
}

func (s *MemoryStore) Get(_ context.Context, userID, id string) (Transcription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return Transcription{}, ErrNotFound
	}
	t, ok := u.get(id)
	if !ok {
		return Transcription{}, ErrNotFound
	}
	return t, nil
}

func (s *MemoryStore) Situacao(_ context.Context, userID, id string) (SituacaoDoId, error) {
	canon, ok := CanonicalUUID(id)
	if !ok {
		return SituacaoDoId{}, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return SituacaoDoId{}, ErrUnknownUser
	}
	sit := SituacaoDoId{Estado: IdNada, Epoch: u.epoch}
	if t, ok := u.items[canon]; ok {
		sit.Estado, sit.Ditado = IdDitado, &t
	} else if _, ok := u.tombs[canon]; ok {
		sit.Estado = IdLapide
	}
	return sit, nil
}

// Create é o ponto único de gravação do ditado que nasce na nuvem (ADR-021,
// C1): a mesma regra do Postgres, com o mutex no lugar da trava do contador, e
// o epoch conferido sob ele.
func (s *MemoryStore) Create(_ context.Context, a Actor, epoch, id string, n NewDictation, now time.Time) (Transcription, bool, error) {
	now = Normalize(now)
	t, err := NewTranscription(a, id, n, now)
	if err != nil {
		return Transcription{}, false, err
	}
	var out Transcription
	created := false
	err = s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		if epoch == "" || epoch != u.epoch {
			return false, ErrEpochChanged
		}
		if cur, ok := u.items[t.ID]; ok {
			// O segundo de uma corrida (ou o reenvio) recebe o que o primeiro
			// gravou, sem gravar nem gastar seq.
			out = cur
			return false, nil
		}
		if _, ok := u.tombs[t.ID]; ok {
			return false, ErrTombstoned
		}
		u.put(t, seq, a, now)
		out, created = u.items[t.ID], true
		return true, nil
	})
	if err != nil {
		return Transcription{}, false, err
	}
	return out, created, nil
}

func (s *MemoryStore) Edit(_ context.Context, a Actor, id string, baseVersion int64, finalText string, now time.Time) (Transcription, error) {
	return s.change(a, id, now, func(cur Transcription) (Transcription, error) {
		return Edit(cur, a.Device(), baseVersion, finalText, now)
	})
}

func (s *MemoryStore) ResolveConflict(_ context.Context, a Actor, id string, baseVersion int64, escolha string, now time.Time) (Transcription, error) {
	return s.change(a, id, now, func(cur Transcription) (Transcription, error) {
		return ResolveConflict(cur, a.Device(), baseVersion, escolha, now)
	})
}

// change grava a mudança que rule decide sobre o estado atual (a edição e a
// escolha do selo), ou nada, quando rule recusa.
func (s *MemoryStore) change(a Actor, id string, now time.Time, rule func(Transcription) (Transcription, error)) (Transcription, error) {
	var out Transcription
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		if err := u.checkDevice(a); err != nil {
			return false, err
		}
		cur, ok := u.get(id)
		if !ok {
			return false, ErrNotFound
		}
		next, err := rule(cur)
		if err != nil {
			return false, err
		}
		u.put(next, seq, a, now)
		out = u.items[next.ID]
		return true, nil
	})
	return out, err
}

func (s *MemoryStore) Trash(_ context.Context, a Actor, id string, now time.Time) (Transcription, error) {
	return s.toggle(a, id, now, func(cur Transcription) (Transcription, bool) { return Trash(cur, a.Device(), now) })
}

func (s *MemoryStore) Restore(_ context.Context, a Actor, id string, now time.Time) (Transcription, error) {
	return s.toggle(a, id, now, func(cur Transcription) (Transcription, bool) { return Restore(cur, a.Device(), now) })
}

func (s *MemoryStore) toggle(a Actor, id string, now time.Time, rule func(Transcription) (Transcription, bool)) (Transcription, error) {
	var out Transcription
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		if err := u.checkDevice(a); err != nil {
			return false, err
		}
		cur, ok := u.get(id)
		if !ok {
			return false, ErrNotFound
		}
		next, changed := rule(cur)
		if !changed {
			out = cur
			return false, nil
		}
		u.put(next, seq, a, now)
		out = u.items[next.ID]
		return true, nil
	})
	return out, err
}

func (s *MemoryStore) Purge(_ context.Context, a Actor, id string, now time.Time) error {
	now = Normalize(now)
	return s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		if err := u.checkDevice(a); err != nil {
			return false, err
		}
		cur, ok := u.get(id)
		if !ok {
			return false, ErrNotFound
		}
		if err := CheckPurge(cur); err != nil {
			return false, err
		}
		// Fora da janela (há mais de FolgaDaLapide), a lápide nasceria
		// vencida: só apaga, e o seq volta (F9).
		lapide := DeixaLapide(cur.CreatedAt, now)
		u.apagarDeVez(cur, lapide, seq, a.Device(), now)
		return lapide, nil
	})
}

// apagarDeVez apaga o ditado de vez: o "excluir definitivamente" do CRUD e a
// retenção (memory_retencao.go). Com lapide, deixa a lápide e o evento
// tombstone com o seq, a versão seguinte e o aparelho de quem apagou (nulo na
// retenção e no celular); sem ela, só a linha sai, e quem chama devolve o seq.
func (u *memUser) apagarDeVez(cur Transcription, lapide bool, seq int64, device *string, now time.Time) {
	delete(u.items, cur.ID)
	if !lapide {
		return
	}
	u.tombs[cur.ID] = memTomb{seq: seq, dictatedAt: cur.CreatedAt}
	u.events = append(u.events, Event{
		Seq: seq, EntityType: EntityTranscription, EntityID: cur.ID,
		Operation: OpTombstone, EntityVersion: cur.Version + 1, DeviceID: device, ChangedAt: Normalize(now),
	})
}

// ListLearned é o aprendido que o celular lista: só os não desfeitos, do mais
// recente (last_seq) para o mais antigo, até o limite (o teto, por padrão).
func (s *MemoryStore) ListLearned(_ context.Context, userID string, limit int) (LearnedPage, error) {
	limit = LearnedListLimit(limit)
	var live []Learned
	s.mu.Lock()
	if u, ok := s.users[userID]; ok {
		for _, l := range u.learned {
			if l.UndoneAt == nil {
				live = append(live, l)
			}
		}
	}
	s.mu.Unlock()
	// O last_seq é único por usuário: sem empate.
	sort.Slice(live, func(i, j int) bool { return live[i].LastSeq > live[j].LastSeq })
	page := LearnedPage{Items: live}
	if len(live) > limit {
		page.Items, page.HasMore = live[:limit], true
	}
	if page.Items == nil {
		page.Items = []Learned{}
	}
	return page, nil
}

// UndoLearned desfaz o aprendido pelo ponto único de gravação: a linha ganha
// undone_at e a versão mais um, e o evento é upsert com o seq novo e sem
// aparelho (o celular). Já desfeito, devolve a linha como está, sem gravar nem
// gastar seq.
func (s *MemoryStore) UndoLearned(_ context.Context, a Actor, id string, now time.Time) (Learned, error) {
	canon, ok := CanonicalUUID(id)
	if !ok {
		return Learned{}, ErrLearnedNotFound
	}
	var out Learned
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		if err := u.checkDevice(a); err != nil {
			return false, err
		}
		cur, ok := u.learned[canon]
		if !ok {
			return false, ErrLearnedNotFound
		}
		next, changed := UndoPair(cur, now)
		if !changed {
			out = cur
			return false, nil
		}
		u.putLearned(next, seq, a, now)
		out = u.learned[next.ID]
		return true, nil
	})
	return out, err
}

func (s *MemoryStore) Push(_ context.Context, a Actor, muts []Mutation, now time.Time) (PushResponse, error) {
	if a.DeviceID == "" {
		return PushResponse{}, ErrNoDevice
	}
	resp := PushResponse{Results: make([]Result, 0, len(muts))}
	for _, m := range muts {
		r, err := s.pushOne(a, m, now)
		if err != nil {
			return PushResponse{}, err
		}
		resp.Results = append(resp.Results, r)
	}
	s.mu.Lock()
	if u, ok := s.users[a.UserID]; ok {
		resp.Epoch = u.epoch
	}
	s.mu.Unlock()
	if resp.Epoch == "" {
		return PushResponse{}, ErrUnknownUser
	}
	return resp, nil
}

func (s *MemoryStore) pushOne(a Actor, m Mutation, now time.Time) (Result, error) {
	code, recordable := CheckMutation(m)
	if code != "" && !recordable {
		return Rejected(m, code).Result, nil
	}
	mutationID, _ := CanonicalUUID(m.MutationID)
	var res Result
	err := s.write(a.UserID, func(u *memUser, seq int64) (bool, error) {
		if err := u.checkDevice(a); err != nil {
			return false, err
		}
		if prev, ok := u.applied[mutationID]; ok {
			res = Duplicate(prev.Applied, m, a)
			return false, nil
		}
		var out Outcome
		entityID, _ := CanonicalUUID(m.EntityID)
		learned := m.EntityType == EntityLearned
		switch {
		case code != "":
			out = Rejected(m, code)
		case m.EntityType == EntityNote || m.EntityType == EntityLabel:
			// A nota e o marcador (N7) gravam a linha, a lápide e o evento
			// deles mesmos (memory_notas.go).
			var wrote bool
			if m.EntityType == EntityNote {
				out, wrote = u.pushNote(a, m, entityID, seq, now)
			} else {
				out, wrote = u.pushLabel(a, m, entityID, seq, now)
			}
			u.applied[mutationID] = memApplied{Applied: out.Record(m, a), at: Normalize(now)}
			res = out.Result
			return wrote, nil
		case learned:
			var curPtr *Learned
			if cur, exists := u.learned[entityID]; exists {
				curPtr = &cur
			}
			out = DecideLearned(m, curPtr, a, now)
		default:
			cur, exists := u.items[entityID]
			_, tomb := u.tombs[entityID]
			var curPtr *Transcription
			if exists {
				curPtr = &cur
			}
			out = DecidePush(m, curPtr, tomb, a, now)
		}
		wrote := out.Write != WriteNone
		switch {
		case wrote && learned:
			u.putLearned(out.NextLearned, seq, a, now)
		case wrote:
			u.put(out.Next, seq, a, now)
			if out.Result.State != nil {
				st := u.items[out.Next.ID]
				out.Result.State = &st
			}
		}
		u.applied[mutationID] = memApplied{Applied: out.Record(m, a), at: Normalize(now)}
		res = out.Result
		return wrote, nil
	})
	return res, err
}

func (s *MemoryStore) Pull(_ context.Context, userID string, afterSeq int64, limit int) (PullResponse, error) {
	if limit <= 0 {
		limit = DefaultPullLimit
	}
	if limit > MaxPullLimit {
		limit = MaxPullLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return PullResponse{}, ErrUnknownUser
	}
	origin := make(map[int64]*string, len(u.events))
	for _, e := range u.events {
		origin[e.Seq] = e.DeviceID
	}
	var changes []Change
	for _, t := range u.items {
		if t.LastSeq > afterSeq {
			item := t
			changes = append(changes, Change{
				Seq: t.LastSeq, EntityType: EntityTranscription, EntityID: t.ID,
				Operation: OpUpsert, EntityVersion: t.Version, DeviceID: origin[t.LastSeq], Item: &item,
			})
		}
	}
	for id, tb := range u.tombs {
		if tb.seq > afterSeq {
			changes = append(changes, Change{Seq: tb.seq, EntityType: EntityTranscription, EntityID: id, Operation: OpTombstone})
		}
	}
	// O aprendido (F8): sempre upsert, o desfeito inclusive (com undone_at),
	// fundido aos ditados e às lápides por seq.
	for _, l := range u.learned {
		if l.LastSeq > afterSeq {
			pair := l
			changes = append(changes, Change{
				Seq: l.LastSeq, EntityType: EntityLearned, EntityID: l.ID,
				Operation: OpUpsert, EntityVersion: l.Version, DeviceID: origin[l.LastSeq], Learned: &pair,
			})
		}
	}
	// As notas, os marcadores e as lápides deles (N7), e as imagens das notas
	// (N10b: só os metadados).
	changes = append(changes, u.pullNotes(afterSeq)...)
	changes = append(changes, u.pullImages(afterSeq)...)
	sort.Slice(changes, func(i, j int) bool { return changes[i].Seq < changes[j].Seq })
	resp := PullResponse{LastSeq: afterSeq, Epoch: u.epoch, Entidades: EntidadesDoSync()}
	n, more := CutPage(changes, limit, Change.TextBytes)
	changes, resp.HasMore = changes[:n], more
	if len(changes) > 0 {
		resp.LastSeq = changes[len(changes)-1].Seq
	}
	if changes == nil {
		changes = []Change{}
	}
	resp.Changes = changes
	return resp, nil
}
