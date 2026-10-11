package pgstore

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O aprendido que o celular lista e desfaz, no Postgres (F8d; ADR-019,
// "Decisões da F8"): o que só o banco prova. A API (a lista, o 403, o 404, o
// teto, o CSRF, o log) roda pela suíte de contrato (TestContrato_Postgres);
// aqui ficam as colunas gravadas, o contador, a concorrência e o erro do
// banco. Como os outros testes deste pacote, só rodam com
// AURORA_TEST_POSTGRES_URL.

// A listagem do celular: só os não desfeitos do usuário, do mais recente
// (last_seq) para o mais antigo, até o limite; o teto fixo e o has_more com o
// banco de verdade (as linhas do teto postas direto, para não gastar 501
// pushes); nunca a lista nula.
func TestPostgres_ListLearnedDoCelular(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user, other := e.newUser(t), e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	pcOther := cloudstore.Actor{UserID: other, DeviceID: e.newDevice(t, other)}
	variants := func(p cloudstore.LearnedPage) string {
		var v []string
		for _, l := range p.Items {
			v = append(v, l.Variant)
		}
		return fmt.Sprint(v)
	}

	if p, err := s.ListLearned(e.ctx, user, 0); err != nil || p.Items == nil || len(p.Items) != 0 || p.HasMore {
		t.Fatalf("lista vazia = %+v, %v; quero uma lista não nula, vazia, sem has_more", p, err)
	}
	e.mustApply(t, s, pc, learnMut(t, "sf", "ECF", 0), now0, 1)                                                      // seq 1
	e.mustApply(t, s, pc, learnMut(t, "nfe", "NF-e", 0), now0, 1)                                                    // seq 2
	e.mustApply(t, s, pc, learnMut(t, "ecd", "ECD", 0), now0, 1)                                                     // seq 3
	e.mustApply(t, s, pcOther, learnMut(t, "sf", "SPED", 0), now0, 1)                                                // o mesmo id, em outro usuário
	if _, err := s.UndoLearned(e.ctx, cloudstore.Actor{UserID: user}, dictionary.IDDoPar("nfe"), now0); err != nil { // seq 4
		t.Fatal(err)
	}

	p, err := s.ListLearned(e.ctx, user, 0)
	if err != nil || variants(p) != "[ecd sf]" || p.HasMore {
		t.Fatalf("lista = %s, %v, more %v; quero [ecd sf] (o desfeito sai, o mais recente primeiro, só o do usuário)", variants(p), err, p.HasMore)
	}
	if l := p.Items[1]; l.Term != "ECF" || l.UserID != user || l.Version != 1 || l.LastSeq != 1 || l.UndoneAt != nil ||
		l.ID != dictionary.IDDoPar("sf") || !l.CreatedAt.Equal(now0) {
		t.Fatalf("o par lido = %+v", l)
	}
	for _, c := range []struct {
		limit int
		want  string
		more  bool
	}{{1, "[ecd]", true}, {2, "[ecd sf]", false}, {3, "[ecd sf]", false}, {0, "[ecd sf]", false}, {-5, "[ecd sf]", false}} {
		p, err := s.ListLearned(e.ctx, user, c.limit)
		if err != nil || variants(p) != c.want || p.HasMore != c.more {
			t.Errorf("limit %d = %s more %v (%v); quero %s more %v", c.limit, variants(p), p.HasMore, err, c.want, c.more)
		}
	}
	if o, _ := s.ListLearned(e.ctx, other, 0); len(o.Items) != 1 || o.Items[0].Term != "SPED" {
		t.Fatalf("lista do outro usuário = %+v", o.Items)
	}

	// O teto, com 501 linhas vivas postas direto: vêm as 500 mais recentes e
	// has_more; com 500 vivas, sem has_more.
	big := e.newUser(t)
	e.exec(`INSERT INTO learned_pairs (id, user_id, variant, normalized_variant, term, last_seq)
		SELECT md5('x' || g)::uuid, $1, 'v' || g, 'v' || g, 'T', g FROM generate_series(1, 501) g`, big)
	e.exec("UPDATE user_sync_counters SET last_seq = 501 WHERE user_id = $1", big)
	p, err = s.ListLearned(e.ctx, big, 0)
	if err != nil || len(p.Items) != cloudstore.MaxLearnedList || !p.HasMore || p.Items[0].LastSeq != 501 || p.Items[499].LastSeq != 2 {
		t.Fatalf("com 501 pares: %d itens, more %v (%v); quero 500, has_more, do seq 501 ao 2", len(p.Items), p.HasMore, err)
	}
	e.exec("UPDATE learned_pairs SET undone_at = $2 WHERE user_id = $1 AND last_seq = 501", big, now0)
	p, err = s.ListLearned(e.ctx, big, 0)
	if err != nil || len(p.Items) != cloudstore.MaxLearnedList || p.HasMore || p.Items[0].LastSeq != 500 || p.Items[499].LastSeq != 1 {
		t.Fatalf("com 500 vivos e um desfeito: %d itens, more %v (%v); quero 500, sem has_more, do seq 500 ao 1", len(p.Items), p.HasMore, err)
	}
}

// O desfazer do celular grava pelo ponto único de gravação: a linha ganha
// undone_at e updated_at no relógio de quem chama (nunca o now() do banco),
// version + 1 e o seq novo, sem mexer no texto, na chave nem no created_at; o
// evento é learned upsert com o mesmo seq e sem aparelho; o repetido não grava
// nem gasta seq; o de outro usuário, o que não existe e o id que não é UUID
// não gravam e devolvem ErrLearnedNotFound; o aparelho de outro usuário faz o
// banco recusar (a chave estrangeira) e desfaz tudo, sem texto no erro.
func TestPostgres_UndoLearnedGravaPeloPontoUnico(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user, other := e.newUser(t), e.newUser(t)
	dev := e.newDevice(t, user)
	pc := cloudstore.Actor{UserID: user, DeviceID: dev}
	cel := cloudstore.Actor{UserID: user}
	fiscal := "fiscal"
	t1, t2, t3 := now0, now0.Add(time.Hour), now0.Add(2*time.Hour)

	m := learnMut(t, "DCTF Web", "DCTFWeb", 0)
	m.Payload = learnedPayload(t, "DCTF Web", "DCTFWeb", true, &fiscal)
	e.mustApply(t, s, pc, m, t1, 1) // seq 1
	id := dictionary.IDDoPar("DCTF Web")

	l, err := s.UndoLearned(e.ctx, cel, strings.ToUpper(id), t2)
	if err != nil {
		t.Fatal(err)
	}
	if l.ID != id || l.Version != 2 || l.LastSeq != 2 || l.UndoneAt == nil || !l.UndoneAt.Equal(t2) || !l.UpdatedAt.Equal(t2) {
		t.Fatalf("o desfeito devolvido = %+v, quero v2, seq 2, undone_at e updated_at %v", l, t2)
	}
	row := e.learnedRow(t, user, "DCTF Web")
	if row.UndoneAt == nil || !row.UndoneAt.Equal(t2) || !row.UpdatedAt.Equal(t2) || !row.CreatedAt.Equal(t1) || row.Version != 2 || row.LastSeq != 2 ||
		row.Variant != "DCTF Web" || row.NormalizedVariant != "dctfweb" || row.Term != "DCTFWeb" || !row.Restricted ||
		row.Domain == nil || *row.Domain != fiscal {
		t.Fatalf("a linha depois do desfazer = %+v, quero o texto, a chave e o created_at mantidos, undone_at e updated_at %v", row, t2)
	}
	evs := e.events(t, user)
	if len(evs) != 2 || evs[1].Seq != 2 || evs[1].EntityType != cloudstore.EntityLearned || evs[1].EntityID != id ||
		evs[1].Operation != cloudstore.OpUpsert || evs[1].EntityVersion != 2 || evs[1].DeviceID != nil {
		t.Fatalf("eventos = %+v, quero o segundo learned upsert v2, sem aparelho (o celular)", evs)
	}
	var changedAt time.Time
	e.scalar(&changedAt, "SELECT changed_at FROM sync_events WHERE user_id = $1 AND seq = 2", user)
	if !changedAt.Equal(t2) {
		t.Errorf("changed_at do desfazer = %v, quero o relógio (%v)", changedAt, t2)
	}
	// O desfazer chega ao pull como upsert com undone_at, sem aparelho.
	p, err := s.Pull(e.ctx, user, 1, 0)
	if err != nil || len(p.Changes) != 1 || p.Changes[0].Learned == nil || p.Changes[0].Learned.UndoneAt == nil ||
		p.Changes[0].Operation != cloudstore.OpUpsert || p.Changes[0].EntityVersion != 2 || p.Changes[0].DeviceID != nil {
		t.Fatalf("o pull do desfazer = %+v, %v", p, err)
	}

	// Repetido: a mesma linha, sem gravar, sem evento e sem gastar seq (o
	// número volta).
	again, err := s.UndoLearned(e.ctx, cel, id, t3)
	if err != nil || again.Version != 2 || again.LastSeq != 2 || !again.UndoneAt.Equal(t2) || !again.UpdatedAt.Equal(t2) {
		t.Fatalf("desfazer de novo = %+v, %v; quero a linha como estava (v2, undone_at %v)", again, err, t2)
	}
	if n := e.lastSeq(user); n != 2 || len(e.events(t, user)) != 2 {
		t.Fatalf("contador %d, eventos %d; quero 2 e 2 (o repetido não grava)", n, len(e.events(t, user)))
	}
	// O delete do PC sobre o desfeito do celular: applied na versão atual,
	// sem gravar.
	if r := e.push1(t, s, pc, undoMut("DCTF Web", 1), t3); r.Status != cloudstore.StatusApplied || r.EntityVersion != 2 || e.lastSeq(user) != 2 {
		t.Fatalf("o delete do PC sobre o desfeito do celular = %+v (contador %d), quero applied v2 sem gastar seq", r, e.lastSeq(user))
	}

	// Não existe, outro usuário, id que não é UUID: o mesmo erro, nada
	// gravado, nenhum número gasto.
	for name, c := range map[string]struct {
		a  cloudstore.Actor
		id string
	}{
		"não existe":       {cel, dictionary.IDDoPar("nfe")},
		"de outro usuário": {cloudstore.Actor{UserID: other}, id},
		"não é UUID":       {cel, "DCTF Web"},
		"vazio":            {cel, ""},
	} {
		if _, err := s.UndoLearned(e.ctx, c.a, c.id, t3); !errors.Is(err, cloudstore.ErrLearnedNotFound) {
			t.Errorf("%s: erro %v, quero ErrLearnedNotFound", name, err)
		}
	}
	if e.lastSeq(user) != 2 || e.lastSeq(other) != 0 || len(e.events(t, other)) != 0 {
		t.Fatalf("contadores %d e %d, eventos do outro %d; quero 2, 0 e 0 (as recusas não gastam seq)", e.lastSeq(user), e.lastSeq(other), len(e.events(t, other)))
	}

	// Reaprender com a base certa traz o par de volta, no seq seguinte.
	e.mustApply(t, s, pc, learnMut(t, "DCTF Web", "DCTFWeb", 2), t3, 3)
	if row = e.learnedRow(t, user, "DCTF Web"); row.UndoneAt != nil || row.Version != 3 || row.LastSeq != 3 {
		t.Fatalf("a linha depois de reaprender = %+v, quero v3 no seq 3, sem undone_at", row)
	}

	// O aparelho que não é do usuário: o banco recusa (a chave estrangeira do
	// evento), tudo desfaz, e o erro não traz a variante.
	devOther := e.newDevice(t, other)
	_, err = s.UndoLearned(e.ctx, cloudstore.Actor{UserID: user, DeviceID: devOther}, id, t3)
	if err == nil {
		t.Fatal("o desfazer com o aparelho de outro usuário não falhou")
	}
	for _, txt := range []string{"DCTF", "dctfweb", "DCTFWeb", fiscal} {
		if strings.Contains(err.Error(), txt) {
			t.Fatalf("o erro do Store carrega o texto do aprendido: %v", err)
		}
	}
	if row = e.learnedRow(t, user, "DCTF Web"); row.UndoneAt != nil || row.Version != 3 || e.lastSeq(user) != 3 {
		t.Fatalf("depois do erro: a linha %+v e o contador %d; quero tudo como estava (v3, seq 3)", row, e.lastSeq(user))
	}
}

// Dois desfazeres do celular ao mesmo tempo no mesmo par: a trava do contador
// os enfileira, só um grava (o seq 2), e os outros encontram o par desfeito e
// devolvem a mesma linha sem gastar seq. Sem buraco no contador.
func TestPostgres_UndoLearnedConcorrenteGravaUmaVezSo(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	e.mustApply(t, s, pc, learnMut(t, "sf", "ECF", 0), now0, 1)
	id := dictionary.IDDoPar("sf")

	const n = 8
	type result struct {
		l   cloudstore.Learned
		err error
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := s.UndoLearned(e.ctx, cloudstore.Actor{UserID: user}, id, now0.Add(time.Duration(i+1)*time.Minute))
			results <- result{l, err}
		}()
	}
	wg.Wait()
	close(results)
	var undoneAt *time.Time
	for r := range results {
		if r.err != nil || r.l.Version != 2 || r.l.LastSeq != 2 || r.l.UndoneAt == nil {
			t.Fatalf("um dos desfazeres = %+v, %v; quero todos na v2, seq 2", r.l, r.err)
		}
		if undoneAt == nil {
			undoneAt = r.l.UndoneAt
		} else if !undoneAt.Equal(*r.l.UndoneAt) {
			t.Fatalf("undone_at diferentes: %v e %v; quero o do desfazer que gravou, para todos", undoneAt, r.l.UndoneAt)
		}
	}
	if c := e.lastSeq(user); c != 2 || len(e.events(t, user)) != 2 {
		t.Fatalf("contador %d, eventos %d; quero 2 e 2 (um desfazer gravou, os outros não)", c, len(e.events(t, user)))
	}
}
