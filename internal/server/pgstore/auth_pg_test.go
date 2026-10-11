package pgstore

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/authtest"
)

// TestAuth_Postgres roda a suíte do login, da sessão e do escopo (ADR-019,
// F3) contra o Postgres: a mesma de internal/server (TestAuth_Memoria).
func TestAuth_Postgres(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	authtest.Run(t, authtest.Backend{
		Store:     s,
		Accounts:  s,
		NewDevice: e.newDeviceWithToken,
		RevokeDevice: func(t *testing.T, deviceID string) {
			e.exec("UPDATE devices SET revoked_at = now() WHERE id = $1", deviceID)
		},
		DisableUser: func(t *testing.T, userID string) {
			e.exec("UPDATE users SET disabled_at = now() WHERE id = $1", userID)
		},
		Contar: func(t *testing.T) authtest.Contagem {
			return authtest.Contagem{
				Usuarios:   e.count("SELECT count(*) FROM users"),
				Contadores: e.count("SELECT count(*) FROM user_sync_counters"),
				Sessoes:    e.count("SELECT count(*) FROM web_sessions"),
				Pedidos:    e.count("SELECT count(*) FROM pairing_requests"),
				Aparelhos:  e.count("SELECT count(*) FROM devices"),
				Tokens:     e.count("SELECT count(*) FROM device_tokens"),
			}
		},
		Despejo: e.despejo,
	})
}

// despejo devolve o texto de todas as linhas das tabelas que guardam conta,
// sessão, login, aparelho e ditado (o bytea sai em hex, \x...).
func (e *pgEnv) despejo(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, table := range []string{"users", "user_sync_counters", "web_sessions", "pending_logins", "devices",
		"device_tokens", "pairing_requests", "transcriptions", "sync_events", "applied_mutations", "tombstones"} {
		var dump *string
		e.scalar(&dump, "SELECT string_agg(t::text, E'\\n') FROM "+table+" t")
		if dump != nil {
			b.WriteString(*dump)
			b.WriteString("\n")
		}
	}
	return b.String()
}
