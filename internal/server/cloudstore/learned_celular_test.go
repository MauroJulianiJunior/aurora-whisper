package cloudstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Os testes do aprendido que o celular lista e desfaz (F8d), sem banco: a
// regra pura do desfazer (UndoPair), o limite da listagem e o Store em
// memória. A suíte de contrato (internal/server/contracttest) prova o mesmo
// pela API, contra a memória e o Postgres.

// UndoPair é a regra do desfazer: undone_at, a versão mais um e updated_at no
// relógio; o texto, o domínio, a restrição e a identidade ficam. Já desfeito:
// nada muda. Não mexe na linha de quem chama.
func TestUndoPair(t *testing.T) {
	t.Run("ativa: desfaz", func(t *testing.T) {
		cur := learnedRow("sf", "ECF", 2, false)
		cur.Domain, cur.Restricted = strPtr("fiscal"), true
		before := *cur
		next, changed := UndoPair(*cur, testNow.Add(123*time.Nanosecond))
		if !changed {
			t.Fatal("changed = false, quero true")
		}
		want := before
		at := testNow
		want.UndoneAt, want.Version, want.UpdatedAt = &at, 3, testNow
		if !reflect.DeepEqual(next, want) {
			t.Fatalf("linha desfeita = %+v\nquero          %+v", next, want)
		}
		if !reflect.DeepEqual(*cur, before) {
			t.Errorf("UndoPair mudou a linha de quem chamou: %+v", *cur)
		}
	})
	t.Run("já desfeita: nada muda", func(t *testing.T) {
		cur := learnedRow("sf", "ECF", 4, true)
		next, changed := UndoPair(*cur, testNow)
		if changed || !reflect.DeepEqual(next, *cur) {
			t.Fatalf("UndoPair de uma desfeita = %+v, %v; quero a mesma linha e changed=false", next, changed)
		}
	})
	t.Run("o relógio em outro fuso vira UTC", func(t *testing.T) {
		cur := learnedRow("sf", "ECF", 1, false)
		brt := time.FixedZone("BRT", -3*60*60)
		next, _ := UndoPair(*cur, testNow.In(brt))
		if next.UndoneAt == nil || next.UndoneAt.Location() != time.UTC || !next.UndoneAt.Equal(testNow) {
			t.Fatalf("undone_at = %v, quero %v em UTC", next.UndoneAt, testNow)
		}
	})
}

// O limite da listagem: zero, negativo e acima do teto são o teto.
func TestLearnedListLimit(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{0, MaxLearnedList}, {-1, MaxLearnedList}, {MaxLearnedList + 1, MaxLearnedList}, {1 << 30, MaxLearnedList},
		{1, 1}, {7, 7}, {MaxLearnedList, MaxLearnedList},
	} {
		if got := LearnedListLimit(c.in); got != c.want {
			t.Errorf("LearnedListLimit(%d) = %d, quero %d", c.in, got, c.want)
		}
	}
	if MaxLearnedList != 500 {
		t.Errorf("MaxLearnedList = %d, a proposta da ADR-019 (escolha menor 3) é 500", MaxLearnedList)
	}
}

const (
	celUser  = "0192a000-0000-7000-8000-0000000000e2"
	celOther = "0192a000-0000-7000-8000-0000000000e3"
)

var (
	celular = Actor{UserID: celUser}
	// celPC é o PC de celUser (o aparelho testDevice, registrado em newCelularStore).
	celPC = Actor{UserID: celUser, DeviceID: testDevice}
)

// learn grava, pelo Push do PC, o aprendido de uma variante e confere que
// aplicou.
func learn(t *testing.T, s *MemoryStore, a Actor, variant, term string, base int64) {
	t.Helper()
	m := learnedUpsert(t, variant, term, base, nil)
	m.MutationID = NewUUIDv7()
	resp, err := s.Push(context.Background(), a, []Mutation{m}, testNow)
	if err != nil || len(resp.Results) != 1 || resp.Results[0].Status != StatusApplied {
		t.Fatalf("aprender %q: %+v, %v", variant, resp, err)
	}
}

func newCelularStore(t *testing.T) *MemoryStore {
	t.Helper()
	s := NewMemoryStore()
	s.AddUser(celUser)
	s.AddUser(celOther)
	s.AddDevice(celUser, testDevice)
	return s
}

func variantsOf(ls []Learned) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Variant
	}
	return out
}

// ListLearned: só os não desfeitos, do mais recente (last_seq) para o mais
// antigo, só os do usuário, nunca nulo.
func TestMemoria_ListLearned(t *testing.T) {
	ctx := context.Background()
	s := newCelularStore(t)
	page, err := s.ListLearned(ctx, celUser, 0)
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.HasMore {
		t.Fatalf("lista vazia = %+v, %v; quero uma lista não nula, vazia, sem has_more", page, err)
	}
	if page, err = s.ListLearned(ctx, "0192a000-0000-7000-8000-0000000000ff", 0); err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("usuário que não existe = %+v, %v; quero a lista vazia", page, err)
	}

	learn(t, s, celPC, "sf", "ECF", 0)   // seq 1
	learn(t, s, celPC, "nfe", "NF-e", 0) // seq 2
	learn(t, s, celPC, "ecd", "ECD", 0)  // seq 3
	learn(t, s, deviceOf(s, celOther), "sf", "SPED", 0)
	if _, err := s.UndoLearned(ctx, celular, dictionary.IDDoPar("nfe"), testNow); err != nil { // seq 5
		t.Fatal(err)
	}
	page, err = s.ListLearned(ctx, celUser, 0)
	if err != nil || fmt.Sprint(variantsOf(page.Items)) != "[ecd sf]" || page.HasMore {
		t.Fatalf("lista = %v, %v, more %v; quero [ecd sf] (o desfeito sai, o mais recente primeiro)", variantsOf(page.Items), err, page.HasMore)
	}
	// Reaprender traz de volta, no topo (o seq novo).
	learn(t, s, celPC, "nfe", "NF-e", 2)
	page, _ = s.ListLearned(ctx, celUser, 0)
	if fmt.Sprint(variantsOf(page.Items)) != "[nfe ecd sf]" {
		t.Fatalf("lista depois de reaprender = %v, quero [nfe ecd sf]", variantsOf(page.Items))
	}
	// O de outro usuário só aparece para ele.
	other, _ := s.ListLearned(ctx, celOther, 0)
	if len(other.Items) != 1 || other.Items[0].Term != "SPED" || other.Items[0].UserID != celOther {
		t.Fatalf("lista do outro usuário = %+v", other.Items)
	}

	// O limite: os mais recentes, e has_more só quando passa dele.
	for _, c := range []struct {
		limit int
		want  string
		more  bool
	}{
		{1, "[nfe]", true}, {2, "[nfe ecd]", true}, {3, "[nfe ecd sf]", false}, {4, "[nfe ecd sf]", false}, {0, "[nfe ecd sf]", false},
	} {
		page, err := s.ListLearned(ctx, celUser, c.limit)
		if err != nil || fmt.Sprint(variantsOf(page.Items)) != c.want || page.HasMore != c.more {
			t.Errorf("limit %d = %v more %v (%v); quero %s more %v", c.limit, variantsOf(page.Items), page.HasMore, err, c.want, c.more)
		}
	}
}

// deviceOf registra um aparelho novo do usuário e devolve o ator dele.
func deviceOf(s *MemoryStore, userID string) Actor {
	id := NewUUIDv7()
	s.AddDevice(userID, id)
	return Actor{UserID: userID, DeviceID: id}
}

// O teto: com mais pares do que MaxLearnedList, vêm os mais recentes e
// has_more; com exatamente o teto, não há has_more; os desfeitos não contam.
func TestMemoria_ListLearnedTeto(t *testing.T) {
	ctx := context.Background()
	s := newCelularStore(t)
	for i := range MaxLearnedList {
		learn(t, s, celPC, fmt.Sprintf("var%04d", i), "T", 0)
	}
	page, err := s.ListLearned(ctx, celUser, 0)
	if err != nil || len(page.Items) != MaxLearnedList || page.HasMore {
		t.Fatalf("com %d pares: %d itens, more %v (%v); quero todos, sem has_more", MaxLearnedList, len(page.Items), page.HasMore, err)
	}
	learn(t, s, celPC, "var-extra", "T", 0)
	page, _ = s.ListLearned(ctx, celUser, 0)
	if len(page.Items) != MaxLearnedList || !page.HasMore || page.Items[0].Variant != "var-extra" ||
		page.Items[MaxLearnedList-1].Variant != "var0001" {
		t.Fatalf("com %d pares: %d itens, more %v, do %q ao %q; quero %d, has_more, do var-extra ao var0001 (o var0000 fica de fora)",
			MaxLearnedList+1, len(page.Items), page.HasMore, page.Items[0].Variant, page.Items[len(page.Items)-1].Variant, MaxLearnedList)
	}
	if _, err := s.UndoLearned(ctx, celular, dictionary.IDDoPar("var-extra"), testNow); err != nil {
		t.Fatal(err)
	}
	page, _ = s.ListLearned(ctx, celUser, 0)
	if len(page.Items) != MaxLearnedList || page.HasMore || page.Items[0].Variant != "var0499" ||
		page.Items[MaxLearnedList-1].Variant != "var0000" {
		t.Fatalf("depois de desfazer um: %d itens, more %v; quero os %d ainda vivos, sem has_more (o desfeito não conta)", len(page.Items), page.HasMore, MaxLearnedList)
	}
}

// UndoLearned pelo ponto único de gravação: o evento é upsert, sem aparelho
// (o celular), com o seq e a versão novos; o repetido não grava nem gasta seq;
// e o desfazer do celular e o delete do PC são a mesma regra.
func TestMemoria_UndoLearned(t *testing.T) {
	ctx := context.Background()
	s := newCelularStore(t)
	learn(t, s, celPC, "sf", "ECF", 0) // seq 1
	id := dictionary.IDDoPar("sf")
	at := testNow.Add(time.Hour)

	l, err := s.UndoLearned(ctx, celular, strings.ToUpper(id), at) // o id em maiúsculas é o mesmo
	if err != nil {
		t.Fatal(err)
	}
	if l.ID != id || l.Version != 2 || l.LastSeq != 2 || l.UndoneAt == nil || !l.UndoneAt.Equal(at) || !l.UpdatedAt.Equal(at) ||
		l.Variant != "sf" || l.Term != "ECF" || l.UserID != celUser {
		t.Fatalf("o desfeito = %+v, quero v2, seq 2, undone_at %v, o texto mantido", l, at)
	}
	ev := s.Events(celUser)
	if len(ev) != 2 || ev[1].Seq != 2 || ev[1].EntityType != EntityLearned || ev[1].Operation != OpUpsert ||
		ev[1].EntityVersion != 2 || ev[1].EntityID != id || ev[1].DeviceID != nil {
		t.Fatalf("eventos = %+v, quero o segundo learned upsert v2, sem aparelho", ev)
	}

	// Repetido: o mesmo item, sem evento nem seq.
	again, err := s.UndoLearned(ctx, celular, id, at.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(again, l) {
		t.Fatalf("desfazer de novo = %+v, %v; quero a mesma linha", again, err)
	}
	if got := s.LastSeq(celUser); got != 2 || len(s.Events(celUser)) != 2 {
		t.Fatalf("contador %d, eventos %d; quero 2 e 2 (o repetido não grava)", got, len(s.Events(celUser)))
	}

	// O delete do PC sobre o par já desfeito pelo celular: applied na versão
	// atual, sem gravar.
	del := Mutation{MutationID: NewUUIDv7(), EntityType: EntityLearned, EntityID: id, Operation: OpDelete}
	resp, err := s.Push(ctx, celPC, []Mutation{del}, at)
	if err != nil || resp.Results[0].Status != StatusApplied || resp.Results[0].EntityVersion != 2 || s.LastSeq(celUser) != 2 {
		t.Fatalf("delete do PC sobre o desfeito do celular = %+v, %v; quero applied v2 sem gastar seq", resp.Results, err)
	}

	// Não existe, outro usuário e id que não é UUID: o mesmo erro, sem gastar
	// seq nem tocar a linha.
	for name, c := range map[string]struct {
		a  Actor
		id string
	}{
		"não existe":                {celular, dictionary.IDDoPar("nfe")},
		"de outro usuário":          {Actor{UserID: celOther}, id},
		"não é UUID":                {celular, "sf"},
		"vazio":                     {celular, ""},
		"UUID com lixo":             {celular, id + "x"},
		"UUID sem os hífens":        {celular, strings.ReplaceAll(id, "-", "")},
		"UUID com o caractere nulo": {celular, id[:35] + "\x00"},
	} {
		if _, err := s.UndoLearned(ctx, c.a, c.id, at); !errors.Is(err, ErrLearnedNotFound) {
			t.Errorf("%s: erro %v, quero ErrLearnedNotFound", name, err)
		}
	}
	if got := s.LastSeq(celUser); got != 2 || s.LastSeq(celOther) != 0 {
		t.Fatalf("contadores %d e %d; quero 2 e 0 (as recusas não gastam seq)", got, s.LastSeq(celOther))
	}
	if ev := s.Events(celOther); len(ev) != 0 {
		t.Fatalf("eventos do outro usuário = %+v, quero nenhum", ev)
	}

	// O usuário sem contador não grava; o aparelho que não é do usuário
	// também não.
	if _, err := s.UndoLearned(ctx, Actor{UserID: "0192a000-0000-7000-8000-0000000000ff"}, id, at); !errors.Is(err, ErrUnknownUser) {
		t.Errorf("usuário sem contador: %v, quero ErrUnknownUser", err)
	}
	if _, err := s.UndoLearned(ctx, Actor{UserID: celUser, DeviceID: NewUUIDv7()}, id, at); !errors.Is(err, ErrUnknownDevice) {
		t.Errorf("aparelho desconhecido: %v, quero ErrUnknownDevice", err)
	}

	// Reaprender com a base certa traz o par de volta (v3), limpa o undone_at
	// e a listagem o mostra.
	learn(t, s, celPC, "sf", "ECF", 2)
	page, _ := s.ListLearned(ctx, celUser, 0)
	if len(page.Items) != 1 || page.Items[0].Version != 3 || page.Items[0].UndoneAt != nil || page.Items[0].LastSeq != 3 {
		t.Fatalf("depois de reaprender: %+v, quero o par na v3, sem undone_at", page.Items)
	}
}
