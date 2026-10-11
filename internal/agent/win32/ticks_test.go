package win32

import (
	"math"
	"testing"
	"time"
)

// Teste de mesa da aritmética de ancoragem de ticks (ADR-007, pendência
// registrada; ADR-010, ponto 2 do sprint). Roda em qualquer GOOS —
// nenhum destes casos toca Win32.
func TestTickReference_ToTime(t *testing.T) {
	ref := NewTickReference(1_000, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))

	cases := []struct {
		name string
		tick uint32
		want time.Time
	}{
		{
			name: "mesmo tick da referência",
			tick: 1_000,
			want: ref.Wall,
		},
		{
			name: "tick posterior, caso comum",
			tick: 1_500,
			want: ref.Wall.Add(500 * time.Millisecond),
		},
		{
			name: "tick anterior à referência",
			tick: 500,
			want: ref.Wall.Add(-500 * time.Millisecond),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ref.ToTime(tc.tick)
			if !got.Equal(tc.want) {
				t.Fatalf("ToTime(%d) = %v, want %v", tc.tick, got, tc.want)
			}
		})
	}

	t.Run("overflow: contador estoura entre a referência e o evento", func(t *testing.T) {
		refTop := NewTickReference(math.MaxUint32-200, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
		// 300ms depois de refTop.Tick, o contador já deu a volta:
		// (MaxUint32-200) + 300, módulo 2^32, é 99.
		tick := uint32(99)
		want := refTop.Wall.Add(300 * time.Millisecond)

		got := refTop.ToTime(tick)
		if !got.Equal(want) {
			t.Fatalf("ToTime(%d) = %v, want %v (referência perto do overflow)", tick, got, want)
		}
	})

	t.Run("overflow: evento pouco antes do wraparound, referência pouco depois", func(t *testing.T) {
		refAfterWrap := NewTickReference(50, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
		// O evento aconteceu 100ms antes da referência, do lado de cá do
		// wraparound: numericamente isso é MaxUint32-49.
		tick := uint32(math.MaxUint32 - 49)
		want := refAfterWrap.Wall.Add(-100 * time.Millisecond)

		got := refAfterWrap.ToTime(tick)
		if !got.Equal(want) {
			t.Fatalf("ToTime(%d) = %v, want %v (evento antes do wraparound)", tick, got, want)
		}
	})

	t.Run("delta zero em ambos os extremos do range de uint32", func(t *testing.T) {
		refZero := NewTickReference(0, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
		got := refZero.ToTime(0)
		if !got.Equal(refZero.Wall) {
			t.Fatalf("ToTime(0) com ref.Tick=0 = %v, want %v", got, refZero.Wall)
		}
	})
}

func TestNewTickReference(t *testing.T) {
	wall := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ref := NewTickReference(42, wall)

	if ref.Tick != 42 {
		t.Errorf("Tick = %d, want 42", ref.Tick)
	}
	if !ref.Wall.Equal(wall) {
		t.Errorf("Wall = %v, want %v", ref.Wall, wall)
	}
}
