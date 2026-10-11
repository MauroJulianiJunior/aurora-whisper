package retention

import (
	"testing"
	"time"
)

var refNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

// TestEffectiveTrashPurgeAt cobre a regra de sobreposição isoladamente,
// como função pura, antes de testar o Job por cima dela.
func TestEffectiveTrashPurgeAt(t *testing.T) {
	cases := []struct {
		name      string
		createdAt time.Time
		deletedAt time.Time
		want      time.Time
	}{
		{
			name: "item quase no fim dos 6 meses vence antes dos 30 dias de lixeira",
			// criado a 5 meses e 29 dias de deletedAt: falta 1 dia para
			// os 6 meses de criado.
			createdAt: refNow.AddDate(0, -6, 1),
			deletedAt: refNow,
			want:      refNow.AddDate(0, 0, 1), // = createdAt + 6 meses
		},
		{
			name:      "item recém-criado usa a janela normal de 30 dias de lixeira",
			createdAt: refNow,
			deletedAt: refNow,
			want:      refNow.AddDate(0, 0, 30), // = deletedAt + 30 dias
		},
		{
			name: "empate exato entre as duas datas: qualquer uma serve (mesmo instante)",
			// deletado exatamente quando completaria os 6 meses de
			// criado, então createdAt+6mo == deletedAt; a regra de
			// sobreposição sempre vale (>= 0 dias restantes de trash).
			createdAt: refNow.AddDate(0, -6, 0),
			deletedAt: refNow,
			want:      refNow, // == createdAt + 6 meses
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectiveTrashPurgeAt(tc.createdAt, tc.deletedAt)
			if !got.Equal(tc.want) {
				t.Fatalf("EffectiveTrashPurgeAt(%v, %v) = %v, want %v", tc.createdAt, tc.deletedAt, got, tc.want)
			}
		})
	}
}

func TestActiveExpiryAt(t *testing.T) {
	got := ActiveExpiryAt(refNow)
	want := refNow.AddDate(0, 6, 0)
	if !got.Equal(want) {
		t.Fatalf("ActiveExpiryAt(%v) = %v, want %v", refNow, got, want)
	}
}

func TestTrashExpiryAt(t *testing.T) {
	got := TrashExpiryAt(refNow)
	want := refNow.AddDate(0, 0, 30)
	if !got.Equal(want) {
		t.Fatalf("TrashExpiryAt(%v) = %v, want %v", refNow, got, want)
	}
}
