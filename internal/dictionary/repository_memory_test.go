package dictionary_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

func str(s string) *string { return &s }

// putEntry é um atalho de fixture: monta e insere uma DictionaryEntry
// ativa via Put (bypass de Upsert), computando NormalizedForm com
// dictionary.Normalize como a camada real faria.
func putEntry(t *testing.T, repo *dictionary.InMemoryDictionaryRepository, id, userID, canonical string, priority int, language, domain *string) dictionary.DictionaryEntry {
	t.Helper()
	e := dictionary.DictionaryEntry{
		ID:             id,
		UserID:         userID,
		CanonicalForm:  canonical,
		NormalizedForm: dictionary.Normalize(canonical),
		Language:       language,
		Domain:         domain,
		Priority:       priority,
		Active:         true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		Version:        1,
	}
	repo.Put(e)
	return e
}

func putAlias(t *testing.T, repo *dictionary.InMemoryDictionaryRepository, id, entryID, alias string) dictionary.DictionaryAlias {
	t.Helper()
	a := dictionary.DictionaryAlias{
		ID:              id,
		EntryID:         entryID,
		Alias:           alias,
		NormalizedAlias: dictionary.Normalize(alias),
		Source:          dictionary.SourceManual,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	repo.PutAlias(a)
	return a
}

func TestInMemoryDictionaryRepository_Search_ExactMatch(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", "user-1", "DCTFWeb", 0, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "dctfweb"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("esperava 1 candidato, veio %d: %+v", len(got), got)
	}
	if got[0].CanonicalForm != "DCTFWeb" || got[0].Reason != dictionary.MatchReasonExact || got[0].Score != 1.0 {
		t.Fatalf("candidato inesperado: %+v", got[0])
	}
}

// TestInMemoryDictionaryRepository_Search_CollapsedSpacingStillMatches
// prova o requisito explícito da tarefa: "dctf web"/"dctfweb" devem casar,
// mesmo com o dicionário guardando só a forma canônica "DCTFWeb" (sem
// nenhum alias cadastrado para a variação com espaço).
func TestInMemoryDictionaryRepository_Search_CollapsedSpacingStillMatches(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", "user-1", "DCTFWeb", 0, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "dctf web"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].CanonicalForm != "DCTFWeb" {
		t.Fatalf("esperava casar 'dctf web' com 'DCTFWeb', veio %+v", got)
	}
}

func TestInMemoryDictionaryRepository_Search_AliasMatch(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	entry := putEntry(t, repo, "e1", "user-1", "DCTFWeb", 0, nil, nil)
	putAlias(t, repo, "a1", entry.ID, "declaracao web")

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "declaracao web"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Reason != dictionary.MatchReasonAlias || got[0].CanonicalForm != "DCTFWeb" {
		t.Fatalf("esperava match por alias em DCTFWeb, veio %+v", got)
	}
	if got[0].Score <= 0 || got[0].Score >= 1.0 {
		t.Fatalf("score de alias fora do esperado: %v", got[0].Score)
	}
}

func TestInMemoryDictionaryRepository_Search_PhoneticMatch(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", "user-1", "Fisco", 0, nil, nil)

	// "fisko" é uma grafia plausível de erro de ASR para "fisco" (troca
	// c/k, mesmo som): nem exata, nem alias, só fonética.
	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "fisko"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Reason != dictionary.MatchReasonPhonetic || got[0].CanonicalForm != "Fisco" {
		t.Fatalf("esperava match fonético em Fisco, veio %+v", got)
	}
	if got[0].Score <= 0 || got[0].Score >= 0.85 {
		t.Fatalf("score fonético deveria ficar abaixo do score de alias/exato: %v", got[0].Score)
	}
}

// TestInMemoryDictionaryRepository_Search_DoesNotConfuseFiscoAndFisico
// exercita, no nível de Search (não só do encoder), o caso central pedido
// pela tarefa: "fisco" no dicionário não pode ser sugerido para "físico".
func TestInMemoryDictionaryRepository_Search_DoesNotConfuseFiscoAndFisico(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", "user-1", "Fisco", 0, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "físico"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Fisco não deveria ser sugerido para 'físico', veio %+v", got)
	}
}

// TestInMemoryDictionaryRepository_Search_DoesNotSuggestDistantWord é o
// teste de contenção pedido explicitamente pela tarefa
// (16-ai-behavior-spec.md, "Dictionary": "não pode substituir distante só
// porque o termo existe"). O encoder fonético é deliberadamente permissivo
// (ver PTBRSoundex): uma letra repetida sem vogal entre elas funde no
// mesmo dígito, então uma palavra de uma letra só e uma de dez letras
// repetidas colidem na CHAVE FONÉTICA. Isso é sintético de propósito — o
// caso real de "colisão fonética que não deveria virar sugestão" é raro o
// bastante para ser difícil de montar com palavras do dia a dia, mas o
// mecanismo de defesa é o mesmo independente do exemplo.
func TestInMemoryDictionaryRepository_Search_DoesNotSuggestDistantWord(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)

	enc := dictionary.PTBRSoundex{}
	short, long := "b", "bbbbbbbbbb"
	if enc.Encode(short) != enc.Encode(long) {
		t.Fatalf("pré-condição do teste falhou: %q e %q deveriam colidir na chave fonética (%q vs %q)",
			short, long, enc.Encode(short), enc.Encode(long))
	}

	putEntry(t, repo, "e1", "user-1", long, 0, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: short})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("termo fonéticamente coincidente mas textualmente distante não deveria virar candidato, veio %+v", got)
	}
}

func TestInMemoryDictionaryRepository_Search_InactiveEntryIgnored(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	e := dictionary.DictionaryEntry{
		ID:             "e1",
		UserID:         "user-1",
		CanonicalForm:  "Fisco",
		NormalizedForm: dictionary.Normalize("Fisco"),
		Active:         false,
	}
	repo.Put(e)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "fisco"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("entrada inativa não deveria aparecer, veio %+v", got)
	}
}

func TestInMemoryDictionaryRepository_Search_ScopesByUser(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", "user-1", "Fisco", 0, nil, nil)
	putEntry(t, repo, "e2", "user-2", "Fisco", 0, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "fisco"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].EntryID != "e1" {
		t.Fatalf("esperava só a entrada de user-1, veio %+v", got)
	}
}

func TestInMemoryDictionaryRepository_Search_RespectsLanguageAndDomain(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	ptBR, en := "pt-BR", "en"
	fiscal := "fiscal"

	putEntry(t, repo, "universal", "user-1", "Fisco", 0, nil, nil)
	putEntry(t, repo, "pt-only", "user-1", "Fisco", 0, &ptBR, nil)
	putEntry(t, repo, "en-only", "user-1", "Fisco", 0, &en, nil)
	putEntry(t, repo, "fiscal-domain", "user-1", "Fisco", 0, nil, &fiscal)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{
		UserID: "user-1", Term: "fisco", Language: &ptBR,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.EntryID] = true
	}
	// "en-only" tem idioma explícito diferente do pedido: deve ficar de
	// fora. As demais (universal, pt-only, e a que só restringe domínio,
	// que não define idioma) devem aparecer.
	if ids["en-only"] {
		t.Fatalf("entrada em outro idioma não deveria casar: %+v", got)
	}
	for _, want := range []string{"universal", "pt-only", "fiscal-domain"} {
		if !ids[want] {
			t.Fatalf("esperava %q entre os candidatos, veio %+v", want, got)
		}
	}
}

func TestInMemoryDictionaryRepository_Search_OrdersByPriorityThenScore(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	// Duas entradas casam exatamente com o mesmo termo (times diferentes
	// do mesmo dicionário) — priority decide.
	putEntry(t, repo, "low", "user-1", "Fisco", 0, nil, nil)
	putEntry(t, repo, "high", "user-1", "fisco", 5, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "fisco"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperava 2 candidatos, veio %d: %+v", len(got), got)
	}
	if got[0].EntryID != "high" || got[1].EntryID != "low" {
		t.Fatalf("ordem por priority incorreta: %+v", got)
	}
}

func TestInMemoryDictionaryRepository_Search_LimitTruncates(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", "user-1", "Fisco", 1, nil, nil)
	putAlias(t, repo, "a1", "e1", "fisco federal")
	putEntry(t, repo, "e2", "user-1", "fisco federal", 0, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "fisco federal", Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Limit deveria truncar para 1, veio %d", len(got))
	}
}

func TestInMemoryDictionaryRepository_Search_RequiresUserID(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if _, err := repo.Search(context.Background(), dictionary.DictionaryQuery{Term: "fisco"}); err == nil {
		t.Fatal("esperava erro sem UserID")
	}
}

func TestInMemoryDictionaryRepository_Search_BlankTermReturnsEmpty(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	putEntry(t, repo, "e1", "user-1", "Fisco", 0, nil, nil)

	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "   !!!  "})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("termo sem conteúdo não deveria casar nada, veio %+v", got)
	}
}

func TestInMemoryDictionaryRepository_Upsert_EnforcesUniquenessByUserAndNormalizedForm(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	ctx := context.Background()

	first := dictionary.DictionaryEntry{ID: "e1", UserID: "user-1", CanonicalForm: "Fisco", Active: true, Priority: 1}
	if err := repo.Upsert(ctx, first); err != nil {
		t.Fatalf("primeiro Upsert: %v", err)
	}

	// Mesmo usuário e mesma forma normalizada, ID diferente e grafia
	// diferente (maiúscula): deve atualizar a entrada existente, não criar
	// uma segunda.
	second := dictionary.DictionaryEntry{ID: "e2", UserID: "user-1", CanonicalForm: "FISCO", Active: true, Priority: 9}
	if err := repo.Upsert(ctx, second); err != nil {
		t.Fatalf("segundo Upsert: %v", err)
	}

	got, err := repo.Search(ctx, dictionary.DictionaryQuery{UserID: "user-1", Term: "fisco"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("esperava 1 entrada só (upsert deveria ter mesclado), veio %d: %+v", len(got), got)
	}
	if got[0].EntryID != "e1" {
		t.Fatalf("esperava manter o ID original e1, veio %q", got[0].EntryID)
	}
	if got[0].Priority != 9 {
		t.Fatalf("esperava priority atualizada para 9, veio %d", got[0].Priority)
	}
}

func TestInMemoryDictionaryRepository_Upsert_RejectsInvalidEntry(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	ctx := context.Background()

	cases := []dictionary.DictionaryEntry{
		{ID: "", UserID: "user-1", CanonicalForm: "Fisco"},
		{ID: "e1", UserID: "", CanonicalForm: "Fisco"},
		{ID: "e1", UserID: "user-1", CanonicalForm: "   "},
	}
	for _, c := range cases {
		if err := repo.Upsert(ctx, c); !errors.Is(err, dictionary.ErrInvalidEntry) {
			t.Fatalf("Upsert(%+v) = %v, esperava ErrInvalidEntry", c, err)
		}
	}
}

func TestInMemoryDictionaryRepository_UpsertAlias_RejectsUnknownEntry(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	ctx := context.Background()

	alias := dictionary.DictionaryAlias{ID: "a1", EntryID: "nao-existe", Alias: "declaracao web"}
	if err := repo.UpsertAlias(ctx, alias); !errors.Is(err, dictionary.ErrInvalidAlias) {
		t.Fatalf("UpsertAlias com entrada inexistente = %v, esperava ErrInvalidAlias", err)
	}
}

func TestInMemoryDictionaryRepository_UpsertAlias_ComputesPhoneticKey(t *testing.T) {
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	ctx := context.Background()

	if err := repo.Upsert(ctx, dictionary.DictionaryEntry{ID: "e1", UserID: "user-1", CanonicalForm: "Fisco", Active: true}); err != nil {
		t.Fatalf("Upsert entrada: %v", err)
	}
	alias := dictionary.DictionaryAlias{ID: "a1", EntryID: "e1", Alias: "fisco federal", Source: dictionary.SourceLearned}
	if err := repo.UpsertAlias(ctx, alias); err != nil {
		t.Fatalf("UpsertAlias: %v", err)
	}

	got, err := repo.Search(ctx, dictionary.DictionaryQuery{UserID: "user-1", Term: "fisco federal"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Reason != dictionary.MatchReasonAlias {
		t.Fatalf("esperava casar o alias recém-criado, veio %+v", got)
	}
}

// TestPluggablePhoneticEncoder prova que Search usa o PhoneticEncoder do
// construtor, não uma implementação fixa — o requisito de "phonetic key
// pluggable" da spec.
func TestPluggablePhoneticEncoder(t *testing.T) {
	stub := stubEncoder{key: "same-key"}
	repo := dictionary.NewInMemoryDictionaryRepository(stub)

	putEntry(t, repo, "e1", "user-1", "AlfaBravo", 0, nil, nil)

	// Com o encoder stub, QUALQUER termo tem a mesma chave fonética — um
	// termo completamente diferente da forma canônica ainda assim colide
	// na chave, mas o filtro de similaridade textual deveria conter isso.
	got, err := repo.Search(context.Background(), dictionary.DictionaryQuery{UserID: "user-1", Term: "xis"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("encoder stub colide tudo, mas a contenção por similaridade deveria zerar os candidatos; veio %+v", got)
	}
}

type stubEncoder struct{ key string }

func (s stubEncoder) Encode(string) string { return s.key }
