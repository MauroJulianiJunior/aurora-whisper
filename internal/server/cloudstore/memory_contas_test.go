package cloudstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A sessão em memória com os mesmos prazos do Postgres (pgstore,
// TestPostgres_Sessao_SemUsoETeto): o vencimento anda com o uso sem passar do
// teto, e o teto vale pela política da hora (encurtá-lo corta as sessões que
// já existem). O resto do comportamento está na suíte internal/server/authtest.
func TestMemoria_SessaoSemUsoETeto(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	p := PoliticaDeSessao{SemUso: 30 * 24 * time.Hour, Teto: 90 * 24 * time.Hour}
	s := NewMemoryStore()
	u, err := s.EntrarComGoogle(ctx, Identidade{Sub: "s", Email: "s@exemplo.test"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	h := []byte("0123456789abcdef0123456789abcdef")
	if _, err := s.CriarSessao(ctx, NovaSessao{UserID: u.ID, TokenHMAC: h, Rotulo: "x", Agora: t0, ExpiraEm: t0.Add(p.SemUso)}); err != nil {
		t.Fatal(err)
	}
	at := t0
	for range 3 {
		at = at.Add(29 * 24 * time.Hour)
		r, err := s.ResolverSessao(ctx, h, at, p)
		if err != nil || !r.ExpiraEm.Equal(p.ExpiracaoDaSessao(t0, at)) {
			t.Fatalf("resolver em %v: %+v, %v", at, r, err)
		}
	}
	if _, err := s.ResolverSessao(ctx, h, t0.Add(p.Teto), p); !errors.Is(err, ErrCredencial) {
		t.Fatalf("depois do teto: %v", err)
	}

	h2 := []byte("fedcba9876543210fedcba9876543210")
	if _, err := s.CriarSessao(ctx, NovaSessao{UserID: u.ID, TokenHMAC: h2, Rotulo: "x", Agora: t0, ExpiraEm: t0.Add(p.SemUso)}); err != nil {
		t.Fatal(err)
	}
	curta := PoliticaDeSessao{SemUso: p.SemUso, Teto: 10 * 24 * time.Hour}
	if _, err := s.ResolverSessao(ctx, h2, t0.Add(20*24*time.Hour), curta); !errors.Is(err, ErrCredencial) {
		t.Fatalf("teto encurtado: %v, quero ErrCredencial", err)
	}
}

// AddConta (o servidor de teste, com a conta de id fixo): o login depois a
// acha pelo sub, com o mesmo id; um id, um sub ou um e-mail repetidos (ou
// vazios) são recusados, sem mexer no que existe.
func TestMemoria_AddConta(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	const id = "00000000-7e57-7000-8000-000000000001"
	ident := Identidade{Sub: "teste:voce@teste.local", Email: "voce@teste.local", Nome: "Conta de teste"}
	s := NewMemoryStore()
	if err := s.AddConta(id, ident); err != nil {
		t.Fatal(err)
	}
	u, err := s.EntrarComGoogle(ctx, ident, t0)
	if err != nil || u.ID != id || u.Email != ident.Email || u.Nome != ident.Nome {
		t.Fatalf("o login depois do AddConta = %+v, %v", u, err)
	}
	for nome, caso := range map[string]struct {
		id    string
		ident Identidade
	}{
		"o mesmo id":     {id, Identidade{Sub: "outro", Email: "outro@teste.local"}},
		"o mesmo sub":    {"00000000-7e57-7000-8000-000000000002", Identidade{Sub: ident.Sub, Email: "outro@teste.local"}},
		"o mesmo e-mail": {"00000000-7e57-7000-8000-000000000002", Identidade{Sub: "outro", Email: ident.Email}},
		"sem id":         {"", Identidade{Sub: "outro", Email: "outro@teste.local"}},
		"sem sub":        {"00000000-7e57-7000-8000-000000000002", Identidade{Email: "outro@teste.local"}},
		"sem e-mail":     {"00000000-7e57-7000-8000-000000000002", Identidade{Sub: "outro"}},
	} {
		if err := s.AddConta(caso.id, caso.ident); err == nil {
			t.Errorf("%s: AddConta aceitou", nome)
		}
	}
	if c := s.Contar(); c.Usuarios != 1 {
		t.Errorf("%d contas; quero 1", c.Usuarios)
	}
	if u, err := s.EntrarComGoogle(ctx, ident, t0); err != nil || u.ID != id {
		t.Errorf("a conta mudou: %+v, %v", u, err)
	}
}
