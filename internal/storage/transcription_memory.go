package storage

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// InMemoryTranscriptionRepository é a implementação em memória de
// TranscriptionRepository usada na Fase 1 para validar o contrato (ver
// docs/adr/ADR-008, "Escopo nesta fase": a implementação real em SQLite
// fica para o Sprint 2). É segura para uso concorrente.
type InMemoryTranscriptionRepository struct {
	mu   sync.Mutex
	byID map[string]Transcription
}

// NewInMemoryTranscriptionRepository constrói um repositório vazio.
func NewInMemoryTranscriptionRepository() *InMemoryTranscriptionRepository {
	return &InMemoryTranscriptionRepository{
		byID: make(map[string]Transcription),
	}
}

var _ TranscriptionRepository = (*InMemoryTranscriptionRepository)(nil)

// Put insere ou substitui uma transcrição diretamente, sem passar pelas
// regras de List/UpdateFinalText. Existe só para preparar fixtures nos
// testes deste pacote e dos pacotes que consomem TranscriptionRepository.
// Put devolve error para casar com a assinatura da implementação SQLite
// — gravar em disco falha de formas que gravar num mapa não falha, e a
// suíte de conformidade exercita as duas pelo mesmo contrato. Aqui nunca
// falha.
func (r *InMemoryTranscriptionRepository) Put(t Transcription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[t.ID] = t
	return nil
}

// cursorKey é a posição de desempate estável usada pelo cursor de List:
// instante desc, ID desc.
//
// O instante é o da ordem da vista: CreatedAt no Histórico, DeletedAt na
// Lixeira (ver chaveDeOrdem).
type cursorKey struct {
	instante time.Time
	id       string
}

// chaveDeOrdem devolve por onde t é ordenado em List.
//
// A Lixeira ordena pela exclusão, não pela criação. Até a revisão de
// 29-set-2026 as duas vistas ordenavam por CreatedAt: um ditado de dois
// meses atrás, apagado agora, ia para o FIM da Lixeira, atrás de tudo o
// que foi criado depois dele — e, com mais de 50 itens, para fora da
// primeira página. Quem acabou de apagar procura o que apagou no topo. A
// migration já dizia isso ("Lixeira: itens excluídos, mais recentes
// primeiro", índice (user_id, deleted_at DESC)), mas nenhuma consulta a
// seguia.
//
// Na Lixeira DeletedAt nunca é nil: é o próprio filtro da vista.
func chaveDeOrdem(t Transcription, lixeira bool) cursorKey {
	if lixeira {
		return cursorKey{instante: *t.DeletedAt, id: t.ID}
	}
	return cursorKey{instante: t.CreatedAt, id: t.ID}
}

// encodeCursor pode usar RFC3339Nano: o texto do cursor só volta a ser
// instante (decodeCursor) e nunca é comparado como texto. Quem compara
// como texto é o SQL, e lá o instante sai por emTexto (formatoTempo).
func encodeCursor(k cursorKey) string {
	raw := k.instante.UTC().Format(time.RFC3339Nano) + "|" + k.id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (cursorKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursorKey{}, fmt.Errorf("storage: cursor inválido: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return cursorKey{}, fmt.Errorf("storage: cursor malformado")
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return cursorKey{}, fmt.Errorf("storage: cursor com timestamp inválido: %w", err)
	}
	return cursorKey{instante: ts, id: parts[1]}, nil
}

// after compara a ordenação (instante desc, ID desc): true se a
// devolvida por k vem estritamente depois do ponto de corte cur (ou
// seja, deve ser incluída na página seguinte a cur).
func (k cursorKey) after(cur cursorKey) bool {
	if k.instante.Equal(cur.instante) {
		return k.id < cur.id
	}
	return k.instante.Before(cur.instante)
}

func (r *InMemoryTranscriptionRepository) List(_ context.Context, filter TranscriptionListFilter) (TranscriptionPage, error) {
	if filter.UserID == "" {
		return TranscriptionPage{}, fmt.Errorf("storage: List requer UserID")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultTranscriptionListLimit
	}
	if limit > MaxTranscriptionListLimit {
		limit = MaxTranscriptionListLimit
	}

	var cursor *cursorKey
	if filter.Cursor != "" {
		ck, err := decodeCursor(filter.Cursor)
		if err != nil {
			return TranscriptionPage{}, err
		}
		cursor = &ck
	}

	query := strings.ToLower(strings.TrimSpace(filter.Query))

	r.mu.Lock()
	matched := make([]Transcription, 0, len(r.byID))
	for _, t := range r.byID {
		if t.UserID != filter.UserID {
			continue
		}
		if filter.Trash && t.DeletedAt == nil {
			continue
		}
		if !filter.Trash && t.DeletedAt != nil {
			continue
		}
		if filter.DeviceID != nil && (t.DeviceID == nil || *t.DeviceID != *filter.DeviceID) {
			continue
		}
		if filter.Mode != nil && t.Mode != *filter.Mode {
			continue
		}
		if filter.ASRProvider != nil && t.ASRProvider != *filter.ASRProvider {
			continue
		}
		if filter.From != nil && t.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && t.CreatedAt.After(*filter.To) {
			continue
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(t.RawText), query) &&
			!strings.Contains(strings.ToLower(t.FinalText), query) {
			continue
		}
		matched = append(matched, t)
	}
	r.mu.Unlock()

	// i vem antes de j quando j vem depois de i: a mesma comparação do
	// cursor, para a ordem e o ponto de corte não poderem divergir.
	sort.Slice(matched, func(i, j int) bool {
		return chaveDeOrdem(matched[j], filter.Trash).after(chaveDeOrdem(matched[i], filter.Trash))
	})

	if cursor != nil {
		start := 0
		for start < len(matched) {
			if chaveDeOrdem(matched[start], filter.Trash).after(*cursor) {
				break
			}
			start++
		}
		matched = matched[start:]
	}

	page := TranscriptionPage{}
	if len(matched) > limit {
		page.Items = matched[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeCursor(chaveDeOrdem(last, filter.Trash))
	} else {
		page.Items = matched
	}
	if page.Items == nil {
		page.Items = []Transcription{}
	}
	return page, nil
}

func (r *InMemoryTranscriptionRepository) Get(_ context.Context, userID, id string) (Transcription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return Transcription{}, ErrNotFound
	}
	return t, nil
}

func (r *InMemoryTranscriptionRepository) UpdateFinalText(_ context.Context, userID, id string, baseVersion int64, finalText string) (Transcription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return Transcription{}, ErrNotFound
	}
	if t.Version != baseVersion {
		return Transcription{}, ErrVersionConflict
	}

	t.FinalText = finalText
	t.Version++
	t.UpdatedAt = time.Now().UTC()
	r.byID[id] = t
	return t, nil
}

// ResolverConflito é a escolha do selo, com as regras do SQLite (sem a fila
// do sync, que só existe lá).
func (r *InMemoryTranscriptionRepository) ResolverConflito(_ context.Context, userID, id string, baseVersion int64, escolha string) (Transcription, error) {
	if escolha != EscolhaManter && escolha != EscolhaTrocar {
		return Transcription{}, ErrEscolhaInvalida
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return Transcription{}, ErrNotFound
	}
	if t.Version != baseVersion || t.Conflito == nil {
		return Transcription{}, ErrVersionConflict
	}
	if escolha == EscolhaTrocar {
		t.FinalText = t.Conflito.FinalText
	}
	t.Conflito = nil
	t.Version++
	t.UpdatedAt = time.Now().UTC()
	r.byID[id] = t
	return t, nil
}

func (r *InMemoryTranscriptionRepository) SoftDelete(_ context.Context, userID, id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return ErrNotFound
	}
	if t.DeletedAt != nil {
		// Já está na lixeira: nada muda, nem a versão. Reapagar
		// reescrevia DeletedAt e empurrava PurgeAt para frente (revisão
		// de 29-set-2026; ver o SoftDelete da versão SQLite).
		return nil
	}

	nowUTC := now.UTC()
	purgeAt := nowUTC.AddDate(0, 0, 30) // lixeira: 30 dias (ver "Retenção").
	t.DeletedAt = &nowUTC
	t.PurgeAt = &purgeAt
	t.UpdatedAt = nowUTC
	t.Version++
	r.byID[id] = t
	return nil
}

func (r *InMemoryTranscriptionRepository) Restore(_ context.Context, userID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.byID[id]
	if !ok || t.UserID != userID {
		return ErrNotFound
	}
	if t.DeletedAt == nil {
		return nil // não está na lixeira: nada a restaurar, nem versão a subir.
	}

	t.DeletedAt = nil
	t.PurgeAt = nil
	t.UpdatedAt = time.Now().UTC()
	t.Version++
	r.byID[id] = t
	return nil
}

// PurgeExpired apaga o que venceu na lixeira. Sem a guarda da D3: a versão em
// memória não tem fila nem nuvem (EsperandoANuvem é sempre zero).
func (r *InMemoryTranscriptionRepository) PurgeExpired(_ context.Context, now time.Time) (ResultadoDoExpurgo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	purged := 0
	for id, t := range r.byID {
		if t.PurgeAt != nil && !t.PurgeAt.After(now) {
			delete(r.byID, id)
			purged++
		}
	}
	return ResultadoDoExpurgo{Apagados: purged}, nil
}
