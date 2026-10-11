package server_test

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/authtest"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// TestAuth_Memoria roda a suíte do login, da sessão e do escopo (ADR-019, F3)
// contra o MemoryStore. A mesma suíte roda contra o Postgres em
// internal/server/pgstore (TestAuth_Postgres, com AURORA_TEST_POSTGRES_URL).
func TestAuth_Memoria(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	authtest.Run(t, authtest.Backend{
		Store:    mem,
		Accounts: mem,
		NewDevice: func(_ *testing.T, userID string, tokenHMAC []byte) string {
			id := contracttest.NewID()
			mem.AddDevice(userID, id)
			mem.AddDeviceToken(id, tokenHMAC)
			return id
		},
		RevokeDevice: func(_ *testing.T, deviceID string) { mem.RevokeDevice(deviceID) },
		DisableUser:  func(_ *testing.T, userID string) { mem.DisableUser(userID) },
		Contar: func(*testing.T) authtest.Contagem {
			c := mem.Contar()
			return authtest.Contagem{Usuarios: c.Usuarios, Contadores: c.Contadores, Sessoes: c.Sessoes,
				Pedidos: c.Pedidos, Aparelhos: c.Aparelhos, Tokens: c.Tokens}
		},
		Despejo: func(*testing.T) string { return mem.Despejo() },
	})
}
