package server_test

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// TestContrato_StoreEmMemoria roda a suíte de contrato da etapa 5 contra o
// Store em memória. A mesma suíte roda contra o Postgres em
// internal/server/pgstore (com AURORA_TEST_POSTGRES_URL).
func TestContrato_StoreEmMemoria(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	contracttest.Run(t, contracttest.Backend{
		Store:    mem,
		Accounts: mem,
		NewDevice: func(_ *testing.T, userID string, tokenHMAC []byte) string {
			id := contracttest.NewID()
			mem.AddDevice(userID, id)
			mem.AddDeviceToken(id, tokenHMAC)
			return id
		},
		Events:     func(_ *testing.T, userID string) []cloudstore.Event { return mem.Events(userID) },
		NoteEvents: func(_ *testing.T, userID string) []cloudstore.Event { return mem.NoteEvents(userID) },
		AntesDaTrava: func(t *testing.T, f func(id string)) {
			mem.AntesDaTravaDaRetencao(f)
			t.Cleanup(func() { mem.AntesDaTravaDaRetencao(nil) })
		},
		Fotografar: func(*testing.T) func(*testing.T) {
			foto := mem.Fotografar()
			return func(*testing.T) { mem.Restaurar(foto) }
		},
	})
}
