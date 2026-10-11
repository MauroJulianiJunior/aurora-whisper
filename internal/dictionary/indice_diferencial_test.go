package dictionary

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// O índice de busca (indice.go) não pode mudar o que Search devolve. Este
// teste compara o repositório de hoje com a REFERÊNCIA: o repositório como era
// antes do índice (cada consulta varria todas as entradas e todos os aliases e
// refazia a chave fonética), copiado aqui sem mudança de lógica. As duas
// recebem a mesma sequência de gravações (Put, PutAlias, Upsert, UpsertAlias,
// com repetições, trocas de entrada e aliases sem entrada) e as mesmas
// consultas, e têm de devolver exatamente o mesmo, inclusive o erro.

// referenciaDoRepositorio é o InMemoryDictionaryRepository de antes do índice.
type referenciaDoRepositorio struct {
	encoder PhoneticEncoder

	entries map[string]DictionaryEntry
	aliases map[string]DictionaryAlias

	byUserNormalized       map[string]string
	byEntryNormalizedAlias map[string]string
}

func novaReferencia(enc PhoneticEncoder) *referenciaDoRepositorio {
	return &referenciaDoRepositorio{
		encoder:                enc,
		entries:                map[string]DictionaryEntry{},
		aliases:                map[string]DictionaryAlias{},
		byUserNormalized:       map[string]string{},
		byEntryNormalizedAlias: map[string]string{},
	}
}

func (r *referenciaDoRepositorio) Put(entry DictionaryEntry) {
	r.entries[entry.ID] = entry
	r.byUserNormalized[entryKey(entry.UserID, entry.NormalizedForm)] = entry.ID
}

func (r *referenciaDoRepositorio) PutAlias(alias DictionaryAlias) {
	r.aliases[alias.ID] = alias
	r.byEntryNormalizedAlias[aliasKey(alias.EntryID, alias.NormalizedAlias)] = alias.ID
}

func (r *referenciaDoRepositorio) Upsert(entry DictionaryEntry) error {
	if entry.ID == "" || entry.UserID == "" || strings.TrimSpace(entry.CanonicalForm) == "" {
		return ErrInvalidEntry
	}
	entry.NormalizedForm = Normalize(entry.CanonicalForm)
	if entry.NormalizedForm == "" {
		return ErrInvalidEntry
	}
	key := entryKey(entry.UserID, entry.NormalizedForm)
	if existingID, ok := r.byUserNormalized[key]; ok {
		existing := r.entries[existingID]
		entry.ID = existing.ID
		r.entries[existing.ID] = entry
		return nil
	}
	if old, ok := r.entries[entry.ID]; ok {
		delete(r.byUserNormalized, entryKey(old.UserID, old.NormalizedForm))
	}
	r.entries[entry.ID] = entry
	r.byUserNormalized[key] = entry.ID
	return nil
}

func (r *referenciaDoRepositorio) UpsertAlias(alias DictionaryAlias) error {
	if alias.ID == "" || alias.EntryID == "" || strings.TrimSpace(alias.Alias) == "" {
		return ErrInvalidAlias
	}
	alias.NormalizedAlias = Normalize(alias.Alias)
	if alias.NormalizedAlias == "" {
		return ErrInvalidAlias
	}
	if _, ok := r.entries[alias.EntryID]; !ok {
		return ErrInvalidAlias
	}
	encoded := r.encoder.Encode(alias.NormalizedAlias)
	alias.PhoneticKey = &encoded
	key := aliasKey(alias.EntryID, alias.NormalizedAlias)
	if existingID, ok := r.byEntryNormalizedAlias[key]; ok {
		existing := r.aliases[existingID]
		alias.ID = existing.ID
		r.aliases[existing.ID] = alias
		return nil
	}
	if old, ok := r.aliases[alias.ID]; ok {
		delete(r.byEntryNormalizedAlias, aliasKey(old.EntryID, old.NormalizedAlias))
	}
	r.aliases[alias.ID] = alias
	r.byEntryNormalizedAlias[key] = alias.ID
	return nil
}

func (r *referenciaDoRepositorio) Search(query DictionaryQuery) ([]DictionaryCandidate, error) {
	if query.UserID == "" {
		return nil, fmt.Errorf("dictionary: Search requer UserID")
	}
	normQuery := Normalize(query.Term)
	if normQuery == "" {
		return []DictionaryCandidate{}, nil
	}
	collapsedQuery := CollapseSpaces(normQuery)
	queryPhonetic := r.encoder.Encode(normQuery)

	candidates := make([]DictionaryCandidate, 0)
	for _, entry := range r.entries {
		if !entry.Active || entry.UserID != query.UserID {
			continue
		}
		if query.Language != nil && entry.Language != nil && *entry.Language != *query.Language {
			continue
		}
		if query.Domain != nil && entry.Domain != nil && *entry.Domain != *query.Domain {
			continue
		}
		if entry.Restricted &&
			(query.Domain == nil || entry.Domain == nil || *entry.Domain != *query.Domain) {
			continue
		}
		dominioBate := query.Domain != nil && entry.Domain != nil && *entry.Domain == *query.Domain
		if c, ok := r.bestMatch(entry, normQuery, collapsedQuery, queryPhonetic, dominioBate); ok {
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

func (r *referenciaDoRepositorio) bestMatch(entry DictionaryEntry, normQuery, collapsedQuery, queryPhonetic string, dominioBate bool) (DictionaryCandidate, bool) {
	base := DictionaryCandidate{
		EntryID:       entry.ID,
		CanonicalForm: entry.CanonicalForm,
		Priority:      entry.Priority,
		AliasOnly:     entry.AliasOnly,
	}
	if normQuery == entry.NormalizedForm ||
		(collapsedQuery != "" && collapsedQuery == CollapseSpaces(entry.NormalizedForm)) {
		base.Reason = MatchReasonExact
		base.Score = scoreExact
		return base, true
	}
	for _, alias := range r.aliases {
		if alias.EntryID != entry.ID {
			continue
		}
		if alias.Restricted && !dominioBate {
			continue
		}
		if alias.NormalizedAlias == normQuery ||
			(collapsedQuery != "" && collapsedQuery == CollapseSpaces(alias.NormalizedAlias)) {
			base.Reason = MatchReasonAlias
			base.Score = scoreAlias
			return base, true
		}
	}
	if entry.AliasOnly {
		return DictionaryCandidate{}, false
	}
	if queryPhonetic == "" {
		return DictionaryCandidate{}, false
	}
	bestScore := 0.0
	matched := false
	if r.encoder.Encode(entry.NormalizedForm) == queryPhonetic {
		if sim := textSimilarity(normQuery, entry.NormalizedForm); sim >= phoneticSimilarityThreshold {
			if score := phoneticScore(sim); score > bestScore {
				bestScore = score
				matched = true
			}
		}
	}
	for _, alias := range r.aliases {
		if alias.EntryID != entry.ID {
			continue
		}
		if alias.Restricted && !dominioBate {
			continue
		}
		aliasPhonetic := alias.PhoneticKey
		if aliasPhonetic == nil {
			encoded := r.encoder.Encode(alias.NormalizedAlias)
			aliasPhonetic = &encoded
		}
		if *aliasPhonetic != queryPhonetic {
			continue
		}
		if sim := textSimilarity(normQuery, alias.NormalizedAlias); sim >= phoneticSimilarityThreshold {
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

// vocabularioDoDiferencial junta formas que colidem de propósito: a mesma
// palavra com e sem espaço, com e sem acento e caixa, e vizinhas fonéticas.
var vocabularioDoDiferencial = []string{
	"RAG", "rague", "ragui", "Ragui", "DCTF Web", "dctfweb", "DCTFWeb", "dctf-web",
	"n8n", "n oito n", "Fisco", "Físico", "fisco federal", "ECD", "ecedê", "ECF", "ecefe",
	"Vitti", "Viti", "Vite", "Pedro Dias", "pedro das", "DAS", "daz", "Nota Fiscal", "notafiscal",
	"Metalúrgica Toffanetti", "toffaneti", "SPED", "sped fiscau", "Loggi", "logui", "log",
	"café", "cafe", "çedilha", "cedilha", "ação", "acao",
}

func ptrDe(s string) *string { return &s }

// operacaoAleatoria aplica uma gravação sorteada nas duas e confere o erro.
func operacaoAleatoria(t *testing.T, r *rand.Rand, real *InMemoryDictionaryRepository, ref *referenciaDoRepositorio, passo int) {
	t.Helper()
	ctx := context.Background()
	usuarios := []string{"u1", "u2", "u3"}
	dominios := []*string{nil, ptrDe("dev"), ptrDe("contabil")}
	idiomas := []*string{nil, ptrDe("pt-BR"), ptrDe("en")}
	idEntrada := fmt.Sprintf("e%d", r.IntN(14))
	idAlias := fmt.Sprintf("a%d", r.IntN(30))
	palavra := func() string { return vocabularioDoDiferencial[r.IntN(len(vocabularioDoDiferencial))] }

	switch r.IntN(10) {
	case 0: // Put: a entrada entra como está, sem recalcular nada.
		e := DictionaryEntry{
			ID: idEntrada, UserID: usuarios[r.IntN(len(usuarios))], CanonicalForm: palavra(),
			Priority: r.IntN(4) * 10, Active: r.IntN(5) != 0, Restricted: r.IntN(5) == 0,
			AliasOnly: r.IntN(4) == 0, Domain: dominios[r.IntN(len(dominios))], Language: idiomas[r.IntN(len(idiomas))],
		}
		e.NormalizedForm = Normalize(palavra()) // de propósito sem relação com a forma canônica
		real.Put(e)
		ref.Put(e)
	case 1: // PutAlias: pode apontar para entrada que não existe, com ou sem chave fonética.
		entrada := idEntrada
		if r.IntN(8) == 0 {
			entrada = "e-fantasma"
		}
		a := DictionaryAlias{ID: idAlias, EntryID: entrada, Alias: palavra(), Restricted: r.IntN(4) == 0}
		a.NormalizedAlias = Normalize(palavra())
		if r.IntN(2) == 0 {
			chave := []string{"R200", "D230", "F220", "V300", "N500", ""}[r.IntN(6)]
			a.PhoneticKey = &chave
		}
		real.PutAlias(a)
		ref.PutAlias(a)
	case 2, 3, 4: // Upsert
		e := DictionaryEntry{
			ID: idEntrada, UserID: usuarios[r.IntN(len(usuarios))], CanonicalForm: palavra(),
			Priority: r.IntN(4) * 10, Active: r.IntN(6) != 0, Restricted: r.IntN(6) == 0,
			AliasOnly: r.IntN(4) == 0, Domain: dominios[r.IntN(len(dominios))], Language: idiomas[r.IntN(len(idiomas))],
		}
		if r.IntN(12) == 0 {
			e.CanonicalForm = "  " // inválida
		}
		erroReal, erroRef := real.Upsert(ctx, e), ref.Upsert(e)
		if erroReal != erroRef {
			t.Fatalf("passo %d: Upsert: erro real = %v, referência = %v", passo, erroReal, erroRef)
		}
	default: // UpsertAlias
		entrada := idEntrada
		if r.IntN(10) == 0 {
			entrada = "e-fantasma"
		}
		a := DictionaryAlias{ID: idAlias, EntryID: entrada, Alias: palavra(), Restricted: r.IntN(4) == 0}
		erroReal, erroRef := real.UpsertAlias(ctx, a), ref.UpsertAlias(a)
		if erroReal != erroRef {
			t.Fatalf("passo %d: UpsertAlias: erro real = %v, referência = %v", passo, erroReal, erroRef)
		}
	}
}

func TestIndiceDeBusca_IgualAReferenciaEmSequenciasAleatorias(t *testing.T) {
	encoders := map[string]PhoneticEncoder{"PTBRSoundex": PTBRSoundex{}, "stub": stubEncoderDoDiferencial{}}
	for nome, enc := range encoders {
		for semente := uint64(1); semente <= 40; semente++ {
			t.Run(fmt.Sprintf("%s/semente-%d", nome, semente), func(t *testing.T) {
				r := rand.New(rand.NewPCG(semente, 7))
				real := NewInMemoryDictionaryRepository(enc)
				ref := novaReferencia(enc)
				ctx := context.Background()
				for passo := 0; passo < 120; passo++ {
					operacaoAleatoria(t, r, real, ref, passo)
					if passo%6 != 5 {
						continue
					}
					// Consultas depois de várias gravações, escolhidas entre o
					// vocabulário, variações de espaço e caixa, e termos soltos.
					for q := 0; q < 8; q++ {
						termo := vocabularioDoDiferencial[r.IntN(len(vocabularioDoDiferencial))]
						switch r.IntN(4) {
						case 0:
							termo = strings.ToUpper(termo)
						case 1:
							termo = strings.ReplaceAll(termo, " ", "")
						case 2:
							termo = termo + "x"
						}
						consulta := DictionaryQuery{
							UserID:   []string{"u1", "u2", "u3", "u4"}[r.IntN(4)],
							Term:     termo,
							Language: []*string{nil, ptrDe("pt-BR"), ptrDe("en")}[r.IntN(3)],
							Domain:   []*string{nil, ptrDe("dev"), ptrDe("contabil")}[r.IntN(3)],
							Limit:    []int{0, 0, 1, 2}[r.IntN(4)],
						}
						got, errReal := real.Search(ctx, consulta)
						quer, errRef := ref.Search(consulta)
						if (errReal == nil) != (errRef == nil) {
							t.Fatalf("passo %d: Search(%+v): erro real = %v, referência = %v", passo, consulta, errReal, errRef)
						}
						if !reflect.DeepEqual(got, quer) {
							t.Fatalf("passo %d: Search(%q, user %s, dom %v, lang %v, limite %d) diverge da referência:\nreal:       %+v\nreferência: %+v",
								passo, consulta.Term, consulta.UserID, consulta.Domain, consulta.Language, consulta.Limit, got, quer)
						}
					}
				}
			})
		}
	}
}

// stubEncoderDoDiferencial faz toda palavra de até 4 letras colidir, para o
// ramo fonético ser exercitado com muitas colisões.
type stubEncoderDoDiferencial struct{}

func (stubEncoderDoDiferencial) Encode(s string) string {
	n := Normalize(s)
	if len(n) <= 4 {
		return "curta"
	}
	return PTBRSoundex{}.Encode(s)
}
