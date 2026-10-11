package learning_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
)

func TestInMemoryCorrectionRepository_Upsert_RejectsInvalid(t *testing.T) {
	repo := learning.NewInMemoryCorrectionRepository()
	cases := []learning.Correction{
		{ID: "", UserID: "u1", FromText: "a", ToText: "b"},
		{ID: "c1", UserID: "", FromText: "a", ToText: "b"},
		{ID: "c1", UserID: "u1", FromText: "  ", ToText: "b"},
		{ID: "c1", UserID: "u1", FromText: "a", ToText: "  "},
	}
	for _, c := range cases {
		if _, err := repo.Upsert(context.Background(), c); !errors.Is(err, learning.ErrInvalidCorrection) {
			t.Fatalf("Upsert(%+v) erro = %v, want ErrInvalidCorrection", c, err)
		}
	}
}

// TestInMemoryCorrectionRepository_Upsert_Idempotent prova a
// IDEMPOTÊNCIA pedida pela tarefa: gravar o mesmo par (from, to) do
// mesmo usuário duas vezes não cria duas linhas — a segunda chamada
// incrementa UseCount na mesma linha (mesmo ID).
func TestInMemoryCorrectionRepository_Upsert_Idempotent(t *testing.T) {
	repo := learning.NewInMemoryCorrectionRepository()
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)

	first, err := repo.Upsert(context.Background(), learning.Correction{
		ID: "id-1", UserID: "u1", FromText: "n oito n", ToText: "n8n",
		ConfirmedByUser: true, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("primeiro Upsert: %v", err)
	}
	if first.UseCount != 1 || first.Version != 1 {
		t.Fatalf("primeiro Upsert: UseCount/Version = %d/%d, want 1/1", first.UseCount, first.Version)
	}

	later := now.Add(time.Hour)
	second, err := repo.Upsert(context.Background(), learning.Correction{
		ID: "id-2", UserID: "u1", FromText: "n oito n", ToText: "n8n",
		ConfirmedByUser: true, CreatedAt: later, UpdatedAt: later,
	})
	if err != nil {
		t.Fatalf("segundo Upsert: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("segundo Upsert criou uma linha nova (ID=%q), esperava reaproveitar %q", second.ID, first.ID)
	}
	if second.UseCount != 2 {
		t.Fatalf("segundo Upsert: UseCount = %d, want 2", second.UseCount)
	}
	if second.Version != 2 {
		t.Fatalf("segundo Upsert: Version = %d, want 2", second.Version)
	}
	if !second.CreatedAt.Equal(now) {
		t.Fatalf("segundo Upsert: CreatedAt = %v, want preservado de %v", second.CreatedAt, now)
	}
	if !second.UpdatedAt.Equal(later) {
		t.Fatalf("segundo Upsert: UpdatedAt = %v, want %v", second.UpdatedAt, later)
	}
}

func TestInMemoryCorrectionRepository_Upsert_DifferentUsersDontMerge(t *testing.T) {
	repo := learning.NewInMemoryCorrectionRepository()
	now := time.Now().UTC()

	a, err := repo.Upsert(context.Background(), learning.Correction{
		ID: "id-a", UserID: "u1", FromText: "n oito n", ToText: "n8n", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Upsert u1: %v", err)
	}
	b, err := repo.Upsert(context.Background(), learning.Correction{
		ID: "id-b", UserID: "u2", FromText: "n oito n", ToText: "n8n", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Upsert u2: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("corrections de usuários diferentes não podem compartilhar linha (a.ID=%q, b.ID=%q)", a.ID, b.ID)
	}
}
