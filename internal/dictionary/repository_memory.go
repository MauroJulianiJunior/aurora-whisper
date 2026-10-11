package dictionary

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Constantes de score de Search. Não são confidence do ASR (ver doc de
// DictionaryCandidate) — são só a régua interna deste pacote para
// desempate e para o filtro de contenção fonética.
const (
	scoreExact    = 1.0
	scoreAlias    = 0.85
	phoneticBase  = 0.6
	phoneticFloor = 0.3 // score mínimo de um match fonético já filtrado por similaridade, sem ficar perto de scoreAlias.

	// phoneticSimilarityThreshold é o filtro de contenção pedido por
	// docs/ai-spec/16-ai-behavior-spec.md ("Dictionary": "não pode
	// substituir distantemente só porque o termo existe"): duas palavras
	// podem colidir na chave fonética (que é deliberadamente permissiva,
	// ver PTBRSoundex) sem soarem parecido o bastante para o dicionário
	// sugerir a troca. Só promovemos o match fonético a candidato quando
	// a similaridade textual (Levenshtein normalizada) do termo buscado
	// contra a forma candidata é >= este limiar.
	phoneticSimilarityThreshold = 0.5
)

// keySep separa os componentes de uma chave de unicidade interna. Não é
// exposto: só usado nos mapas de índice deste arquivo.
const keySep = "\x1f"

func entryKey(userID, normalizedForm string) string {
	return userID + keySep + normalizedForm
}

func aliasKey(entryID, normalizedAlias string) string {
	return entryID + keySep + normalizedAlias
}

// InMemoryDictionaryRepository é a implementação em memória de
// DictionaryRepository usada na Fase 1 para validar o contrato (mesma
// estratégia de internal/storage.InMemoryTranscriptionRepository: mock
// determinístico e síncrono, sem I/O real). É segura para uso concorrente.
type InMemoryDictionaryRepository struct {
	mu sync.Mutex

	encoder PhoneticEncoder

	entries map[string]DictionaryEntry // por ID
	aliases map[string]DictionaryAlias // por ID

	byUserNormalized       map[string]string // entryKey -> entry ID
	byEntryNormalizedAlias map[string]string // aliasKey -> alias ID

	// O índice de busca (indice.go) é derivado de entries e aliases, mantido
	// junto deles por cada gravação. Search só o lê.
	indice indiceDeBusca
}

// NewInMemoryDictionaryRepository constrói um repositório vazio. encoder
// nil usa PTBRSoundex{} (default do pacote); passar outro PhoneticEncoder
// é a forma de exercitar o requisito de "chave fonética plugável" sem
// mudar Search.
func NewInMemoryDictionaryRepository(encoder PhoneticEncoder) *InMemoryDictionaryRepository {
	if encoder == nil {
		encoder = PTBRSoundex{}
	}
	return &InMemoryDictionaryRepository{
		encoder:                encoder,
		entries:                make(map[string]DictionaryEntry),
		aliases:                make(map[string]DictionaryAlias),
		byUserNormalized:       make(map[string]string),
		byEntryNormalizedAlias: make(map[string]string),
		indice:                 novoIndiceDeBusca(),
	}
}

var _ DictionaryRepository = (*InMemoryDictionaryRepository)(nil)

// Put insere ou substitui uma entrada diretamente, sem passar pelas regras
// de Upsert (recálculo de NormalizedForm, versão, unicidade). Existe só
// para preparar fixtures nos testes deste pacote e dos pacotes que
// consomem DictionaryRepository (ex.: internal/resolver).
func (r *InMemoryDictionaryRepository) Put(entry DictionaryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gravarEntrada(entry)
	r.byUserNormalized[entryKey(entry.UserID, entry.NormalizedForm)] = entry.ID
}

// PutAlias é o equivalente de Put para aliases.
func (r *InMemoryDictionaryRepository) PutAlias(alias DictionaryAlias) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gravarAlias(alias)
	r.byEntryNormalizedAlias[aliasKey(alias.EntryID, alias.NormalizedAlias)] = alias.ID
}

// gravarEntrada põe a entrada em entries, por ID, e no índice de busca. Todo
// caminho que muda entries passa por aqui. Chamador segura r.mu.
func (r *InMemoryDictionaryRepository) gravarEntrada(entry DictionaryEntry) {
	r.entries[entry.ID] = entry
	r.indice.poeEntrada(entry, r.encoder)
}

// gravarAlias põe o alias em aliases, por ID, e no índice de busca (tirando
// dele o alias antigo de mesmo ID, que pode ser de outra entrada). Todo caminho
// que muda aliases passa por aqui. Chamador segura r.mu.
func (r *InMemoryDictionaryRepository) gravarAlias(alias DictionaryAlias) {
	antigo, tinha := r.aliases[alias.ID]
	r.aliases[alias.ID] = alias
	r.indice.poeAlias(antigo, tinha, alias, r.encoder)
}

func (r *InMemoryDictionaryRepository) Upsert(_ context.Context, entry DictionaryEntry) error {
	if entry.ID == "" || entry.UserID == "" || strings.TrimSpace(entry.CanonicalForm) == "" {
		return ErrInvalidEntry
	}
	entry.NormalizedForm = Normalize(entry.CanonicalForm)
	if entry.NormalizedForm == "" {
		return ErrInvalidEntry
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	key := entryKey(entry.UserID, entry.NormalizedForm)

	// Já existe uma entrada com essa chave de negócio (UserID,
	// NormalizedForm), possivelmente com outro ID: é ela que recebe a
	// atualização — entry.ID pedido é ignorado nesse caso (ver contrato
	// em DictionaryRepository.Upsert).
	if existingID, ok := r.byUserNormalized[key]; ok {
		existing := r.entries[existingID]
		entry.ID = existing.ID
		entry.CreatedAt = existing.CreatedAt
		entry.Version = existing.Version + 1
		entry.UpdatedAt = now
		r.gravarEntrada(entry)
		return nil
	}

	// ID já usado, mas com uma forma canônica diferente (edição de
	// CanonicalForm de uma entrada existente): limpa a chave antiga do
	// índice de unicidade antes de reindexar.
	if old, ok := r.entries[entry.ID]; ok {
		delete(r.byUserNormalized, entryKey(old.UserID, old.NormalizedForm))
		entry.CreatedAt = old.CreatedAt
		entry.Version = old.Version + 1
	} else {
		entry.CreatedAt = now
		entry.Version = 1
	}
	entry.UpdatedAt = now
	r.gravarEntrada(entry)
	r.byUserNormalized[key] = entry.ID
	return nil
}

func (r *InMemoryDictionaryRepository) UpsertAlias(_ context.Context, alias DictionaryAlias) error {
	if alias.ID == "" || alias.EntryID == "" || strings.TrimSpace(alias.Alias) == "" {
		return ErrInvalidAlias
	}
	alias.NormalizedAlias = Normalize(alias.Alias)
	if alias.NormalizedAlias == "" {
		return ErrInvalidAlias
	}
	if alias.Source == "" {
		alias.Source = SourceManual
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.entries[alias.EntryID]; !ok {
		return ErrInvalidAlias
	}

	encoded := r.encoder.Encode(alias.NormalizedAlias)
	alias.PhoneticKey = &encoded

	now := time.Now().UTC()
	key := aliasKey(alias.EntryID, alias.NormalizedAlias)

	if existingID, ok := r.byEntryNormalizedAlias[key]; ok {
		existing := r.aliases[existingID]
		alias.ID = existing.ID
		alias.CreatedAt = existing.CreatedAt
		if alias.UsageCount == 0 {
			alias.UsageCount = existing.UsageCount
		}
		alias.UpdatedAt = now
		r.gravarAlias(alias)
		return nil
	}

	if old, ok := r.aliases[alias.ID]; ok {
		delete(r.byEntryNormalizedAlias, aliasKey(old.EntryID, old.NormalizedAlias))
		alias.CreatedAt = old.CreatedAt
	} else {
		alias.CreatedAt = now
	}
	alias.UpdatedAt = now
	r.gravarAlias(alias)
	r.byEntryNormalizedAlias[key] = alias.ID
	return nil
}

func (r *InMemoryDictionaryRepository) Search(_ context.Context, query DictionaryQuery) ([]DictionaryCandidate, error) {
	if query.UserID == "" {
		return nil, fmt.Errorf("dictionary: Search requer UserID")
	}

	normQuery := Normalize(query.Term)
	if normQuery == "" {
		return []DictionaryCandidate{}, nil
	}
	collapsedQuery := CollapseSpaces(normQuery)

	r.mu.Lock()
	defer r.mu.Unlock()

	queryPhonetic := r.encoder.Encode(normQuery)

	candidates := make([]DictionaryCandidate, 0)
	for _, ix := range r.indice.entradas {
		entry := &ix.e
		if !entry.Active || entry.UserID != query.UserID {
			continue
		}
		if query.Language != nil && entry.Language != nil && *entry.Language != *query.Language {
			continue
		}
		if query.Domain != nil && entry.Domain != nil && *entry.Domain != *query.Domain {
			continue
		}
		// Entrada restrita só aparece quando o domínio pedido bate
		// EXATAMENTE com o dela — domínio ausente dos dois lados não
		// serve. Ver DictionaryEntry.Restricted.
		if entry.Restricted &&
			(query.Domain == nil || entry.Domain == nil || *entry.Domain != *query.Domain) {
			continue
		}

		dominioBate := query.Domain != nil && entry.Domain != nil && *entry.Domain == *query.Domain
		if c, ok := r.bestMatch(ix, normQuery, collapsedQuery, queryPhonetic, dominioBate); ok {
			candidates = append(candidates, c)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.CanonicalForm != b.CanonicalForm {
			return a.CanonicalForm < b.CanonicalForm
		}
		return a.EntryID < b.EntryID
	})

	if query.Limit > 0 && len(candidates) > query.Limit {
		candidates = candidates[:query.Limit]
	}
	return candidates, nil
}

// bestMatch devolve o melhor candidato de entry para a busca já
// normalizada, testando, em ordem de confiança: casamento exato (com
// tolerância a espaçamento via CollapseSpaces), casamento por alias, e só
// então casamento fonético — sempre com o filtro de contenção de
// similaridade textual (ver phoneticSimilarityThreshold). Chamador já
// segura r.mu.
// bestMatch acha o melhor casamento de entry contra a consulta.
//
// dominioBate diz se o domínio pedido é EXATAMENTE o da entrada, e é o
// que libera alias restrito. Falso quando qualquer um dos dois é
// desconhecido — ver DictionaryAlias.Restricted.
func (r *InMemoryDictionaryRepository) bestMatch(ix *entradaIndexada, normQuery, collapsedQuery, queryPhonetic string, dominioBate bool) (DictionaryCandidate, bool) {
	entry := &ix.e
	// Os aliases desta entrada e as formas que dependem só deles já estão no
	// índice (indice.go): nada é recalculado por consulta.
	aliases := r.indice.aliases[entry.ID]
	base := DictionaryCandidate{
		EntryID:       entry.ID,
		CanonicalForm: entry.CanonicalForm,
		Priority:      entry.Priority,
		AliasOnly:     entry.AliasOnly,
	}

	if normQuery == entry.NormalizedForm ||
		(collapsedQuery != "" && collapsedQuery == ix.colapsada) {
		base.Reason = MatchReasonExact
		base.Score = scoreExact
		return base, true
	}

	for i := range aliases {
		alias := &aliases[i]
		if alias.a.Restricted && !dominioBate {
			continue
		}
		if alias.a.NormalizedAlias == normQuery ||
			(collapsedQuery != "" && collapsedQuery == alias.colapsado) {
			base.Reason = MatchReasonAlias
			base.Score = scoreAlias
			return base, true
		}
	}

	// Entrada do painel age só pela variante exata: a fonética é evidência
	// fraca sobre um termo que a pessoa nunca revisou, e ela só confirmou a
	// variante. Ver DictionaryEntry.AliasOnly.
	if entry.AliasOnly {
		return DictionaryCandidate{}, false
	}

	if queryPhonetic == "" {
		return DictionaryCandidate{}, false
	}

	bestScore := 0.0
	matched := false

	if ix.fonetica == queryPhonetic {
		if sim := textSimilarity(normQuery, entry.NormalizedForm); sim >= phoneticSimilarityThreshold {
			if score := phoneticScore(sim); score > bestScore {
				bestScore = score
				matched = true
			}
		}
	}

	for i := range aliases {
		alias := &aliases[i]
		// A restrição vale aqui também. Match fonético não é
		// auto-aplicado (é evidência fraca), mas um alias restrito nem
		// como CANDIDATO deve aparecer fora do domínio dele: o candidato
		// vira span incerto, e span incerto é o que o resolver cloud
		// recebe permissão de mexer.
		if alias.a.Restricted && !dominioBate {
			continue
		}
		if alias.fonetico != queryPhonetic {
			continue
		}
		if sim := textSimilarity(normQuery, alias.a.NormalizedAlias); sim >= phoneticSimilarityThreshold {
			if score := phoneticScore(sim); score > bestScore {
				bestScore = score
				matched = true
			}
		}
	}

	if !matched {
		return DictionaryCandidate{}, false
	}
	base.Reason = MatchReasonPhonetic
	base.Score = bestScore
	return base, true
}

// phoneticScore mapeia a similaridade textual (já acima do limiar de
// contenção) para o intervalo [phoneticFloor, phoneticBase], para que um
// match fonético nunca supere um match exato/alias, mas ainda distinga
// "quase idêntico" de "só passou raspando no limiar".
func phoneticScore(similarity float64) float64 {
	return phoneticFloor + (phoneticBase-phoneticFloor)*similarity
}
