package contracttest

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Os casos da arrumação do job de retenção (ADR-019, "Decisões da F9", F9c):
// os sync_events órfãos e as respostas guardadas das mutações, pelos prazos
// derivados, dentro do Reter.

// O evento órfão sai com 90 dias (1 µs antes fica); o que é o last_seq de um
// ditado ou de um aprendido vivo (o desfeito inclusive) fica, e o aparelho de
// origem continua no pull. Os de lápide e os de entidade mudada ou apagada são
// órfãos e saem; a lápide fica (o pull a lê da tabela dela).
func testRetencaoArrumacaoDosEventos(t *testing.T, w *world) {
	x, y, z := newID(), newID(), newID()
	w.mustCreate(pcA1, x, "fica")                // seq 1: o last_seq de x
	w.mustCreate(pcA1, y, "editado no outro PC") // seq 2: órfão depois da edição
	if r := w.pushOne(pcA2, change(y, 1, map[string]any{"final_text": "do PC 2"})); r.Status != "applied" {
		t.Fatalf("editar pelo PC 2: %+v", r) // seq 3: o last_seq de y
	}
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1) // seq 4: órfão depois do desfazer
	w.mustLearn(pcA1, desfazer("sf", 1), 2)        // seq 5: o last_seq do desfeito
	w.mustCreate(pcA1, z, "apagado de vez")        // seq 6
	ctx, cel := context.Background(), cloudstore.Actor{UserID: w.userA}
	if _, err := w.b.Store.Trash(ctx, cel, z, now0); err != nil {
		t.Fatal(err) // seq 7
	}
	if err := w.b.Store.Purge(ctx, cel, z, now0); err != nil {
		t.Fatal(err) // seq 8: a lápide (que fica; o evento dela é órfão)
	}
	for _, ev := range w.b.Events(t, w.userA) {
		if !ev.ChangedAt.Equal(now0) {
			t.Fatalf("o evento %d tem changed_at %s, quero o relógio de quem gravou (%s)", ev.Seq, ev.ChangedAt, now0)
		}
	}

	w.andar(now0.AddDate(0, 0, 89))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(now0.Add(cloudstore.PrazoDosEventos - us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.wantSeqs(w.userA, 1, 2, 3, 4, 5, 6, 7, 8)

	w.clk.Set(now0.Add(cloudstore.PrazoDosEventos))
	w.wantRetido(w.userA, cloudstore.Retido{Eventos: 5})
	w.wantSeqs(w.userA, 1, 3, 5)
	w.wantRetido(w.userA, cloudstore.Retido{})

	// O pull continua com o aparelho de origem de cada entidade viva, e com a
	// lápide de z.
	p := w.pullTyped(pcA1, 0, 0)
	origem := map[string]*string{}
	for _, c := range p.Changes {
		origem[c.EntityID] = c.DeviceID
	}
	if d := origem[x]; d == nil || *d != w.devA1 {
		t.Errorf("a origem de x = %v, quero o PC 1", d)
	}
	if d := origem[y]; d == nil || *d != w.devA2 {
		t.Errorf("a origem de y = %v, quero o PC 2 (a última mudança)", d)
	}
	if _, sf, ok := learnedOf(t, p, "sf"); !ok || sf.UndoneAt == nil {
		t.Errorf("o desfeito sumiu do pull: %+v %v", sf, ok)
	}
	if d, ok := origem[dictionary.IDDoPar("sf")]; !ok || d == nil || *d != w.devA1 {
		t.Errorf("a origem do desfeito = %v %v, quero o PC 1", d, ok)
	}
	if seq, ok := w.lapideNoPull(pcA1, z); !ok || seq != 8 {
		t.Errorf("a lápide de z: %d %v, quero o seq 8 (o evento dela saiu; a lápide não)", seq, ok)
	}
	// O contador não volta: a próxima gravação é o seq 9.
	w.criarAgora(pcA1, newID(), "depois da arrumação")
	w.wantSeqs(w.userA, 1, 3, 5, 9)
}

// A resposta guardada vive TokenSemUso + 10 dias. Um PC que volta dentro da
// vida do token reenvia os mesmos mutation_id e recebe duplicate (com 189
// dias, e 1 µs antes do prazo); com 190 dias a resposta some, e o reenvio é
// uma mutação nova: sem outra mudança, o aprendido responde applied com a
// versão atual, sem gravar nem dar conflito, e o ditado, que já saiu da
// janela (o prazo passa dos 6 meses), fora_da_janela.
func testRetencaoArrumacaoDasMutacoes(t *testing.T, w *world) {
	m1 := aprender("pq", "porque", 0)
	w.mustLearn(pcA1, m1, 1) // seq 1
	m2 := aprender("pq", "por que", 1)
	w.mustLearn(pcA1, m2, 2) // seq 2
	x := newID()
	m3 := create(x, "vai sair da janela")
	if r := w.pushOne(pcA1, m3); r.Status != "applied" {
		t.Fatalf("criar: %+v", r) // seq 3
	}
	resend := func(m mut) learnedResultJSON {
		t.Helper()
		r, _ := w.pushLearned(pcA1, m)
		return r
	}

	w.andar(now0.AddDate(0, 0, 189))
	// x saiu da janela (o evento dele e o primeiro do aprendido são órfãos e
	// velhos); as respostas, com 189 dias, ficam.
	w.wantRetido(w.userA, cloudstore.Retido{Ativos: 1, Eventos: 2})
	for _, m := range []mut{m1, m2, m3} {
		if r := resend(m); r.Status != "duplicate" || r.Original != "applied" {
			t.Fatalf("o reenvio de %s com 189 dias = %+v, quero duplicate do applied", m.MutationID, r)
		}
	}
	w.clk.Set(now0.Add(cloudstore.PrazoDasMutacoes - us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	if r := resend(m2); r.Status != "duplicate" {
		t.Fatalf("o reenvio 1 µs antes do prazo = %+v, quero duplicate", r)
	}

	w.clk.Set(now0.Add(cloudstore.PrazoDasMutacoes))
	w.wantRetido(w.userA, cloudstore.Retido{Mutacoes: 3})
	w.andar(now0.AddDate(0, 0, 191))
	w.wantRetido(w.userA, cloudstore.Retido{})
	if r := resend(m2); r.Status != "applied" || r.EntityVersion != 2 || r.Mesclado || r.State != nil {
		t.Fatalf("o reenvio do aprendido depois do prazo = %+v, quero applied v2, sem gravar", r)
	}
	if r := w.pushOne(pcA1, m3); r.Status != "gone" || r.Motivo != "fora_da_janela" {
		t.Fatalf("o reenvio do ditado depois do prazo = %+v, quero gone fora_da_janela", r)
	}
	// Nada gravou: só o evento do last_seq do aprendido continua.
	w.wantSeqs(w.userA, 2)
	// E a resposta nova é guardada de novo: o próximo reenvio é duplicate.
	if r := resend(m2); r.Status != "duplicate" || r.EntityVersion != 2 {
		t.Fatalf("o segundo reenvio depois do prazo = %+v, quero duplicate", r)
	}
}
