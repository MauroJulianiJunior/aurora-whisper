package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// repoDeTeste é o contrato que esta suíte exercita: o repositório
// público mais o Put de semeadura.
//
// Put não está em storage.TranscriptionRepository de propósito — a
// interface descreve o que o DASHBOARD consome, e escrever é do caminho
// do ditado. Mas toda implementação precisa das duas coisas, e a suíte
// precisa semear.
type repoDeTeste interface {
	storage.TranscriptionRepository
	Put(storage.Transcription) error
}

// paraCadaRepo roda o mesmo corpo contra TODAS as implementações.
//
// Até 28-set-2026 estes testes só existiam para a versão em memória,
// embora se chamassem TestTranscriptionRepository_*. Quando a
// implementação SQLite entrou, ela teria de ganhar uma suíte paralela,
// escrita à mão — e duas suítes para o mesmo contrato divergem: a
// segunda cobre o que quem a escreveu lembrou de cobrir.
//
// Aqui o contrato é UM. Uma implementação nova entra na lista abaixo e
// herda todas as asserções que já existem.
func paraCadaRepo(t *testing.T, corpo func(*testing.T, repoDeTeste)) {
	t.Helper()
	for _, impl := range []struct {
		nome string
		novo func(*testing.T) repoDeTeste
	}{
		{"memoria", func(*testing.T) repoDeTeste { return storage.NewInMemoryTranscriptionRepository() }},
		{"sqlite", func(t *testing.T) repoDeTeste {
			db := openTestDB(t)
			if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			return storage.NewSQLiteTranscriptionRepository(db)
		}},
	} {
		t.Run(impl.nome, func(t *testing.T) { corpo(t, impl.novo(t)) })
	}
}

func newTranscription(id, userID string, createdAt time.Time) storage.Transcription {
	return storage.Transcription{
		ID:             id,
		UserID:         userID,
		SourcePlatform: "windows",
		RawText:        "raw " + id,
		FinalText:      "final " + id,
		Mode:           "smart",
		ASRProvider:    "local",
		Status:         "done",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
		Version:        1,
		SyncState:      "pending",
	}
}

func TestTranscriptionRepository_List_OrdersByCreatedAtDesc(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
		repo.Put(newTranscription("tr-1", "user-1", base))
		repo.Put(newTranscription("tr-2", "user-1", base.Add(time.Minute)))
		repo.Put(newTranscription("tr-3", "user-1", base.Add(2*time.Minute)))

		page, err := repo.List(context.Background(), storage.TranscriptionListFilter{UserID: "user-1"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 3 {
			t.Fatalf("esperava 3 itens, achou %d", len(page.Items))
		}
		gotOrder := []string{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID}
		want := []string{"tr-3", "tr-2", "tr-1"}
		for i := range want {
			if gotOrder[i] != want[i] {
				t.Fatalf("ordem = %v, esperava %v", gotOrder, want)
			}
		}
	})
}

func TestTranscriptionRepository_List_ScopesByUser(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		now := time.Now().UTC()
		repo.Put(newTranscription("tr-1", "user-1", now))
		repo.Put(newTranscription("tr-2", "user-2", now))

		page, err := repo.List(context.Background(), storage.TranscriptionListFilter{UserID: "user-1"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != "tr-1" {
			t.Fatalf("List vazou item de outro usuário: %+v", page.Items)
		}
	})
}

func TestTranscriptionRepository_List_FiltersTrash(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		now := time.Now().UTC()
		active := newTranscription("tr-active", "user-1", now)
		repo.Put(active)

		trashed := newTranscription("tr-trashed", "user-1", now)
		deletedAt := now
		trashed.DeletedAt = &deletedAt
		repo.Put(trashed)

		activePage, err := repo.List(context.Background(), storage.TranscriptionListFilter{UserID: "user-1", Trash: false})
		if err != nil {
			t.Fatalf("List ativo: %v", err)
		}
		if len(activePage.Items) != 1 || activePage.Items[0].ID != "tr-active" {
			t.Fatalf("histórico ativo devolveu itens inesperados: %+v", activePage.Items)
		}

		trashPage, err := repo.List(context.Background(), storage.TranscriptionListFilter{UserID: "user-1", Trash: true})
		if err != nil {
			t.Fatalf("List lixeira: %v", err)
		}
		if len(trashPage.Items) != 1 || trashPage.Items[0].ID != "tr-trashed" {
			t.Fatalf("lixeira devolveu itens inesperados: %+v", trashPage.Items)
		}
	})
}

func TestTranscriptionRepository_List_FiltersByQueryDeviceModeProvider(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		now := time.Now().UTC()

		device1 := "device-1"
		t1 := newTranscription("tr-1", "user-1", now)
		t1.DeviceID = &device1
		t1.Mode = "smart"
		t1.ASRProvider = "local"
		t1.RawText = "reunião de contabilidade"
		repo.Put(t1)

		device2 := "device-2"
		t2 := newTranscription("tr-2", "user-1", now.Add(time.Minute))
		t2.DeviceID = &device2
		t2.Mode = "literal"
		t2.ASRProvider = "cloud"
		t2.RawText = "outro assunto"
		repo.Put(t2)

		cases := []struct {
			name   string
			filter storage.TranscriptionListFilter
			wantID string
		}{
			{"query", storage.TranscriptionListFilter{UserID: "user-1", Query: "contabilidade"}, "tr-1"},
			{"device", storage.TranscriptionListFilter{UserID: "user-1", DeviceID: &device2}, "tr-2"},
			{"mode", storage.TranscriptionListFilter{UserID: "user-1", Mode: strPtr("literal")}, "tr-2"},
			{"provider", storage.TranscriptionListFilter{UserID: "user-1", ASRProvider: strPtr("local")}, "tr-1"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				page, err := repo.List(context.Background(), tc.filter)
				if err != nil {
					t.Fatalf("List: %v", err)
				}
				if len(page.Items) != 1 || page.Items[0].ID != tc.wantID {
					t.Fatalf("filtro %s: esperava só %q, achou %+v", tc.name, tc.wantID, page.Items)
				}
			})
		}
	})
}

func strPtr(s string) *string { return &s }

func TestTranscriptionRepository_List_CursorPaginatesWithoutGapsOrOverlap(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
		for i := 0; i < 5; i++ {
			id := string(rune('a' + i))
			repo.Put(newTranscription("tr-"+id, "user-1", base.Add(time.Duration(i)*time.Minute)))
		}

		var allIDs []string
		cursor := ""
		for pageN := 0; pageN < 10; pageN++ {
			page, err := repo.List(context.Background(), storage.TranscriptionListFilter{
				UserID: "user-1",
				Limit:  2,
				Cursor: cursor,
			})
			if err != nil {
				t.Fatalf("List página %d: %v", pageN, err)
			}
			for _, item := range page.Items {
				allIDs = append(allIDs, item.ID)
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}

		want := []string{"tr-e", "tr-d", "tr-c", "tr-b", "tr-a"}
		if len(allIDs) != len(want) {
			t.Fatalf("paginação devolveu %v, esperava %v", allIDs, want)
		}
		for i := range want {
			if allIDs[i] != want[i] {
				t.Fatalf("paginação devolveu %v, esperava %v", allIDs, want)
			}
		}
	})
}

// TestTranscriptionRepository_Lixeira_OrdenaPelaExclusao trava a ordem da
// Lixeira: o que foi apagado por último vem primeiro, por mais antigo que
// seja o ditado.
//
// Até a revisão de 29-set-2026 a Lixeira ordenava por CreatedAt, como o
// Histórico: o ditado de dois meses atrás que a pessoa acabou de apagar
// ia para o fim da lista, atrás de tudo o que foi criado depois dele, e
// com mais de 50 itens caía fora da primeira página.
//
// Aqui a ordem de exclusão é o INVERSO da de criação, para que ordenar
// pela coluna errada não passe por acaso. Os instantes de exclusão têm
// fração de segundo de largura variável em RFC3339Nano (".5" contra
// ".50001"), como em TestTranscriptionRepository_OrdenaPorFracaoDeSegundo:
// um cursor comparado como texto fora de formatoTempo repetiria item. E
// dois itens empatam no instante de exclusão, na fronteira de uma página,
// para exercitar o desempate por ID no cursor.
func TestTranscriptionRepository_Lixeira_OrdenaPelaExclusao(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		ctx := context.Background()
		agora := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
		antes := agora.Add(-2 * time.Hour)
		// Em microssegundos inteiros, para a fração sair exata:
		// 500030 µs vira ".50003" em RFC3339Nano, 500000 µs vira ".5".
		us := func(n int64) time.Duration { return time.Duration(n) * time.Microsecond }

		itens := []struct {
			id      string
			criado  time.Time
			apagado time.Time
		}{
			{"tr-antigo", agora.AddDate(0, 0, -60), agora}, // o mais antigo, apagado por último
			{"tr-1", agora.AddDate(0, 0, -5), antes.Add(us(500030))},
			{"tr-2", agora.AddDate(0, 0, -4), antes.Add(us(500020))},
			{"tr-3", agora.AddDate(0, 0, -3), antes.Add(us(500010))},
			{"tr-4", agora.AddDate(0, 0, -2), antes.Add(us(500000))},
			{"tr-5", agora.AddDate(0, 0, -1), antes.Add(us(499990))},
			{"tr-6", agora.Add(-time.Hour), antes.Add(us(499990))}, // empata com tr-5
		}
		for _, it := range itens {
			if err := repo.Put(newTranscription(it.id, "user-1", it.criado)); err != nil {
				t.Fatal(err)
			}
		}
		// Apagados pelo caminho de verdade, em ordem qualquer: o que
		// conta é o instante passado a SoftDelete.
		for _, it := range itens {
			if err := repo.SoftDelete(ctx, "user-1", it.id, it.apagado); err != nil {
				t.Fatalf("SoftDelete %s: %v", it.id, err)
			}
		}
		// Um ditado ativo, criado depois de todos, não entra na Lixeira.
		if err := repo.Put(newTranscription("tr-ativo", "user-1", agora)); err != nil {
			t.Fatal(err)
		}

		want := []string{"tr-antigo", "tr-1", "tr-2", "tr-3", "tr-4", "tr-6", "tr-5"}

		primeira, err := repo.List(ctx, storage.TranscriptionListFilter{UserID: "user-1", Trash: true})
		if err != nil {
			t.Fatalf("List lixeira: %v", err)
		}
		if len(primeira.Items) == 0 || primeira.Items[0].ID != "tr-antigo" {
			t.Fatalf("a Lixeira não começa pelo último apagado: %v", ids(primeira.Items))
		}

		var vistos []string
		cursor := ""
		for pagina := 0; pagina < 10; pagina++ {
			p, err := repo.List(ctx, storage.TranscriptionListFilter{UserID: "user-1", Trash: true, Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatalf("List lixeira, página %d: %v", pagina, err)
			}
			vistos = append(vistos, ids(p.Items)...)
			if p.NextCursor == "" {
				break
			}
			cursor = p.NextCursor
		}
		if strings.Join(vistos, ",") != strings.Join(want, ",") {
			t.Fatalf("paginação da Lixeira = %v, want %v (pulou, repetiu ou ordenou pela criação)", vistos, want)
		}

		// E o Histórico continua pela criação.
		ativo, err := repo.List(ctx, storage.TranscriptionListFilter{UserID: "user-1"})
		if err != nil {
			t.Fatalf("List histórico: %v", err)
		}
		if got := ids(ativo.Items); len(got) != 1 || got[0] != "tr-ativo" {
			t.Fatalf("Histórico = %v, want [tr-ativo]", got)
		}
	})
}

func ids(itens []storage.Transcription) []string {
	out := make([]string, len(itens))
	for i, it := range itens {
		out[i] = it.ID
	}
	return out
}

func TestTranscriptionRepository_UpdateFinalText_OptimisticConcurrency(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		now := time.Now().UTC()
		repo.Put(newTranscription("tr-1", "user-1", now))

		updated, err := repo.UpdateFinalText(context.Background(), "user-1", "tr-1", 1, "texto editado")
		if err != nil {
			t.Fatalf("UpdateFinalText com base_version correta: %v", err)
		}
		if updated.FinalText != "texto editado" || updated.Version != 2 {
			t.Fatalf("update inesperado: %+v", updated)
		}

		_, err = repo.UpdateFinalText(context.Background(), "user-1", "tr-1", 1, "outro texto")
		if err != storage.ErrVersionConflict {
			t.Fatalf("esperava ErrVersionConflict com base_version obsoleta, achou %v", err)
		}
	})
}

func TestTranscriptionRepository_SoftDeleteAndRestore(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		now := time.Now().UTC()
		repo.Put(newTranscription("tr-1", "user-1", now))

		if err := repo.SoftDelete(context.Background(), "user-1", "tr-1", now); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		got, err := repo.Get(context.Background(), "user-1", "tr-1")
		if err != nil {
			t.Fatalf("Get após SoftDelete: %v", err)
		}
		if got.DeletedAt == nil || got.PurgeAt == nil {
			t.Fatalf("SoftDelete deveria preencher DeletedAt e PurgeAt: %+v", got)
		}
		if !got.PurgeAt.Equal(got.DeletedAt.AddDate(0, 0, 30)) {
			t.Fatalf("PurgeAt deveria ser DeletedAt + 30 dias, achou DeletedAt=%v PurgeAt=%v", got.DeletedAt, got.PurgeAt)
		}

		if err := repo.Restore(context.Background(), "user-1", "tr-1"); err != nil {
			t.Fatalf("Restore: %v", err)
		}
		got, err = repo.Get(context.Background(), "user-1", "tr-1")
		if err != nil {
			t.Fatalf("Get após Restore: %v", err)
		}
		if got.DeletedAt != nil || got.PurgeAt != nil {
			t.Fatalf("Restore deveria limpar DeletedAt e PurgeAt: %+v", got)
		}
	})
}

// TestTranscriptionRepository_SoftDeleteNaLixeiraNaoMexe: apagar o que
// já está na lixeira não faz nada, e sem erro.
//
// O cenário da revisão de 29-set-2026: no dia 28 às 18:00 a aba A põe o
// ditado na lixeira; no dia 29 às 10:00 a aba B, com a lista velha, manda
// apagar o mesmo ditado. Antes, o segundo SoftDelete reescrevia
// DeletedAt ("na lixeira desde" virava 29/09), empurrava PurgeAt mais 30
// dias e subia a versão.
func TestTranscriptionRepository_SoftDeleteNaLixeiraNaoMexe(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		ctx := context.Background()
		if err := repo.Put(newTranscription("tr-1", "user-1", time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))); err != nil {
			t.Fatal(err)
		}
		abaA := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
		abaB := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

		if err := repo.SoftDelete(ctx, "user-1", "tr-1", abaA); err != nil {
			t.Fatalf("primeiro SoftDelete: %v", err)
		}
		antes, err := repo.Get(ctx, "user-1", "tr-1")
		if err != nil {
			t.Fatal(err)
		}

		if err := repo.SoftDelete(ctx, "user-1", "tr-1", abaB); err != nil {
			t.Fatalf("SoftDelete de item já na lixeira devolveu erro: %v", err)
		}
		depois, err := repo.Get(ctx, "user-1", "tr-1")
		if err != nil {
			t.Fatal(err)
		}

		if depois.DeletedAt == nil || !depois.DeletedAt.Equal(abaA) {
			t.Errorf("DeletedAt = %v, want %v — reapagar reescreveu \"na lixeira desde\"", depois.DeletedAt, abaA)
		}
		if depois.PurgeAt == nil || antes.PurgeAt == nil || !depois.PurgeAt.Equal(*antes.PurgeAt) {
			t.Errorf("PurgeAt = %v, want %v — reapagar adiou a purga", depois.PurgeAt, antes.PurgeAt)
		}
		if !depois.UpdatedAt.Equal(antes.UpdatedAt) || depois.Version != antes.Version {
			t.Errorf("UpdatedAt/Version = %v/%d, want %v/%d — reapagar não é mudança",
				depois.UpdatedAt, depois.Version, antes.UpdatedAt, antes.Version)
		}

		// O "não achei" continua sendo erro: sem efeito só vale para o que
		// existe.
		if err := repo.SoftDelete(ctx, "user-1", "não-existe", abaB); err != storage.ErrNotFound {
			t.Errorf("SoftDelete de id inexistente = %v, want ErrNotFound", err)
		}
		if err := repo.SoftDelete(ctx, "user-2", "tr-1", abaB); err != storage.ErrNotFound {
			t.Errorf("SoftDelete de item de outro usuário = %v, want ErrNotFound", err)
		}
	})
}

// TestTranscriptionRepository_RestoreForaDaLixeiraNaoMexe: restaurar o que
// não está na lixeira não faz nada, e sem erro — nem sobe a versão, que
// é o que a edição usa para detectar conflito (revisão de 29-set-2026).
func TestTranscriptionRepository_RestoreForaDaLixeiraNaoMexe(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		ctx := context.Background()
		if err := repo.Put(newTranscription("tr-1", "user-1", time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))); err != nil {
			t.Fatal(err)
		}
		antes, err := repo.Get(ctx, "user-1", "tr-1")
		if err != nil {
			t.Fatal(err)
		}

		if err := repo.Restore(ctx, "user-1", "tr-1"); err != nil {
			t.Fatalf("Restore de item fora da lixeira devolveu erro: %v", err)
		}
		depois, err := repo.Get(ctx, "user-1", "tr-1")
		if err != nil {
			t.Fatal(err)
		}
		if depois.DeletedAt != nil || depois.PurgeAt != nil {
			t.Errorf("Restore sem efeito mexeu na lixeira: DeletedAt=%v PurgeAt=%v", depois.DeletedAt, depois.PurgeAt)
		}
		if !depois.UpdatedAt.Equal(antes.UpdatedAt) || depois.Version != antes.Version {
			t.Errorf("UpdatedAt/Version = %v/%d, want %v/%d — restaurar o que não estava na lixeira não é mudança",
				depois.UpdatedAt, depois.Version, antes.UpdatedAt, antes.Version)
		}

		if err := repo.Restore(ctx, "user-1", "não-existe"); err != storage.ErrNotFound {
			t.Errorf("Restore de id inexistente = %v, want ErrNotFound", err)
		}
		if err := repo.Restore(ctx, "user-2", "tr-1"); err != storage.ErrNotFound {
			t.Errorf("Restore de item de outro usuário = %v, want ErrNotFound", err)
		}
	})
}

func TestTranscriptionRepository_PurgeExpired(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)

		expired := newTranscription("tr-expired", "user-1", now.AddDate(0, 0, -40))
		purgeAtExpired := now.AddDate(0, 0, -1)
		expired.DeletedAt = &purgeAtExpired
		expired.PurgeAt = &purgeAtExpired
		repo.Put(expired)

		notYet := newTranscription("tr-not-yet", "user-1", now)
		purgeAtFuture := now.AddDate(0, 0, 10)
		notYet.DeletedAt = &now
		notYet.PurgeAt = &purgeAtFuture
		repo.Put(notYet)

		active := newTranscription("tr-active", "user-1", now)
		repo.Put(active)

		purged, err := repo.PurgeExpired(context.Background(), now)
		if err != nil {
			t.Fatalf("PurgeExpired: %v", err)
		}
		if purged.Apagados != 1 || purged.EsperandoANuvem != 0 {
			t.Fatalf("esperava 1 registro purgado e nenhum esperando a nuvem, veio %+v", purged)
		}

		if _, err := repo.Get(context.Background(), "user-1", "tr-expired"); err != storage.ErrNotFound {
			t.Fatalf("tr-expired deveria ter sido purgado, Get devolveu %v", err)
		}
		if _, err := repo.Get(context.Background(), "user-1", "tr-not-yet"); err != nil {
			t.Fatalf("tr-not-yet não deveria ter sido purgado: %v", err)
		}
		if _, err := repo.Get(context.Background(), "user-1", "tr-active"); err != nil {
			t.Fatalf("tr-active não deveria ter sido purgado: %v", err)
		}
	})
}

func TestTranscriptionRepository_Get_NotFound(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		_, err := repo.Get(context.Background(), "user-1", "não-existe")
		if err != storage.ErrNotFound {
			t.Fatalf("esperava ErrNotFound, achou %v", err)
		}
	})
}

// TestTranscriptionRepository_OrdenaPorFracaoDeSegundo trava a escolha do
// formato de tempo em disco.
//
// A tentação é gravar com RFC3339Nano. Ele CORTA os zeros à direita da
// fração, então o texto tem largura variável e a ordem lexicográfica —
// que é a que o SQLite usa numa coluna TEXT — deixa de valer:
//
//	0,5 s      vira "....5Z"
//	0,50001 s  vira "....50001Z"
//
// Comparando caractere a caractere, 'Z' (90) é maior que '0' (48), então
// ".5Z" ordena DEPOIS de ".50001Z" — o inverso da ordem dos instantes.
//
// O teste de cursor existente não pegaria isto: ele espaça as amostras de
// minuto em minuto, e aí a fração é sempre zero.
func TestTranscriptionRepository_OrdenaPorFracaoDeSegundo(t *testing.T) {
	paraCadaRepo(t, func(t *testing.T, repo repoDeTeste) {
		base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
		cedo := base.Add(500 * time.Millisecond)     // .5
		tarde := base.Add(500010 * time.Microsecond) // .50001, DEPOIS de cedo
		if !tarde.After(cedo) {
			t.Fatal("pré-condição: tarde deveria ser depois de cedo")
		}

		if err := repo.Put(newTranscription("tr-cedo", "user-1", cedo)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Put(newTranscription("tr-tarde", "user-1", tarde)); err != nil {
			t.Fatal(err)
		}

		pagina, err := repo.List(context.Background(), storage.TranscriptionListFilter{UserID: "user-1"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(pagina.Items) != 2 {
			t.Fatalf("len(Items) = %d, want 2", len(pagina.Items))
		}
		if pagina.Items[0].ID != "tr-tarde" {
			t.Errorf("primeiro item = %q, want \"tr-tarde\" — a ordem por fração de segundo está invertida",
				pagina.Items[0].ID)
		}
	})
}
