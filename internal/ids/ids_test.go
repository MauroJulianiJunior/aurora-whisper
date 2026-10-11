package ids_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ids"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNew_FormatIsUUIDv7(t *testing.T) {
	id := ids.New()
	if !uuidPattern.MatchString(id) {
		t.Fatalf("New() = %q, não bate com o formato UUID v7 esperado", id)
	}
}

func TestNew_IsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := ids.New()
		if seen[id] {
			t.Fatalf("New() gerou o mesmo id duas vezes: %q", id)
		}
		seen[id] = true
	}
}

func TestNew_OrdersLexicographicallyBySequence(t *testing.T) {
	// IDs gerados em milissegundos distintos devem ordenar na mesma ordem em
	// que foram criados, já que UUID v7 embute o timestamp nos bits altos. O
	// sleep garante milissegundos diferentes: sem ele, duas chamadas podem
	// cair no mesmo milissegundo, e aí a ordem depende só da parte aleatória —
	// o teste ficaria instável.
	first := ids.New()
	time.Sleep(2 * time.Millisecond)
	second := ids.New()
	if first >= second {
		t.Fatalf("esperava first < second (ordenação lexicográfica), achou first=%q second=%q", first, second)
	}
}
