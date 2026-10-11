package cloudstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// O DepoisDeRestaurar em memória, com números exatos (o do Postgres:
// pgstore, TestDepoisDeRestaurar_Postgres), e a foto do MemoryStore que os
// testes da restauração usam como o backup: a foto não muda com o que vem
// depois, e restaurar volta tudo, no lugar, quantas vezes for.
func TestMemoria_DepoisDeRestaurarEAFoto(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s := NewMemoryStore()
	a, err := s.EntrarComGoogle(ctx, Identidade{Sub: "sa", Email: "a@exemplo.test"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EntrarComGoogle(ctx, Identidade{Sub: "sb", Email: "b@exemplo.test"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	devA, devB, revogado := NewUUIDv7(), NewUUIDv7(), NewUUIDv7()
	for _, d := range []struct{ user, dev, tok string }{{a.ID, devA, "tok-a"}, {b.ID, devB, "tok-b"}, {a.ID, revogado, "tok-r"}} {
		s.AddDevice(d.user, d.dev)
		s.AddDeviceToken(d.dev, []byte(d.tok))
	}
	if err := s.RevogarAparelho(ctx, a.ID, revogado, t0); err != nil {
		t.Fatal(err)
	}
	for i, u := range []string{a.ID, a.ID, b.ID} {
		if _, err := s.CriarSessao(ctx, NovaSessao{UserID: u, TokenHMAC: bytes.Repeat([]byte{byte(i + 1)}, 32), Rotulo: "x", Agora: t0, ExpiraEm: t0.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	id := NewUUIDv7()
	raw, _ := json.Marshal(map[string]any{
		"source_platform": "windows", "raw_text": "r", "final_text": "do backup", "mode": "smart",
		"asr_provider": "whisper-local", "status": "done", "created_at": t0.Add(-time.Hour).Format(time.RFC3339),
	})
	if _, err := s.Push(ctx, Actor{UserID: a.ID, DeviceID: devA}, []Mutation{{MutationID: NewUUIDv7(), EntityType: EntityTranscription,
		EntityID: id, Operation: OpUpsert, Payload: raw}}, t0); err != nil {
		t.Fatal(err)
	}
	epoch := func(u string) string {
		p, err := s.Pull(ctx, u, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		return p.Epoch
	}
	epochA := epoch(a.ID)

	foto := s.Fotografar()
	// Depois da foto: a edição, a lixeira e uma conta nova.
	if _, err := s.Edit(ctx, Actor{UserID: a.ID}, id, 1, "depois do backup", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EntrarComGoogle(ctx, Identidade{Sub: "sc", Email: "c@exemplo.test"}, t0); err != nil {
		t.Fatal(err)
	}
	s.Restaurar(foto)
	if got, err := s.Get(ctx, a.ID, id); err != nil || got.FinalText != "do backup" || got.Version != 1 {
		t.Fatalf("restaurado: %+v, %v; quero o texto do backup na v1", got, err)
	}
	if c := s.Contar(); c.Usuarios != 2 || c.Sessoes != 3 || c.Aparelhos != 3 || c.Tokens != 2 {
		t.Fatalf("restaurado: %+v", c)
	}
	if epoch(a.ID) != epochA {
		t.Fatal("restaurar trocou o epoch sozinho")
	}

	r, err := s.DepoisDeRestaurar(ctx, t0)
	if err != nil || r != (Restauracao{Contas: 2, Sessoes: 3, Aparelhos: 2}) {
		t.Fatalf("DepoisDeRestaurar = %+v, %v; quero 2 contas, 3 sessões e 2 aparelhos (o revogado já estava)", r, err)
	}
	if epoch(a.ID) == epochA {
		t.Error("o epoch de A não mudou")
	}
	if c := s.Contar(); c.Sessoes != 0 || c.Tokens != 0 || c.Aparelhos != 3 || c.Ditados != 1 {
		t.Errorf("depois: %+v; quero sem sessão e sem token, os aparelhos e o ditado ficam", c)
	}
	if _, err := s.ResolverTokenDeAparelho(ctx, []byte("tok-a"), t0); !errors.Is(err, ErrCredencial) {
		t.Errorf("o token de A depois: %v", err)
	}
	if l, err := s.ListarAparelhos(ctx, a.ID, t0); err != nil || len(l) != 0 {
		t.Errorf("os aparelhos de A depois: %v, %v", l, err)
	}
	// A segunda vez só troca o epoch: os aparelhos já estão revogados.
	if r2, err := s.DepoisDeRestaurar(ctx, t0); err != nil || r2 != (Restauracao{Contas: 2}) {
		t.Errorf("a segunda vez = %+v, %v; quero só as 2 contas", r2, err)
	}

	// A foto continua valendo: restaurar de novo volta as sessões e os tokens.
	s.Restaurar(foto)
	if c := s.Contar(); c.Sessoes != 3 || c.Tokens != 2 {
		t.Errorf("a segunda restauração: %+v", c)
	}
	if epoch(a.ID) != epochA {
		t.Error("a segunda restauração não voltou o epoch da foto")
	}
}
