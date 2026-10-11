package contracttest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Os casos da retenção na nuvem (ADR-019, "Decisões da F9", F9b): o Reter de
// cada Store, contra o Store em memória e o Postgres. O relógio anda meses,
// então os casos não usam a sessão do celular (ela vence com 30 dias sem uso):
// conferem pelo Store e pelos PCs, e um pull de cada PC no caminho (andar)
// mantém os tokens vivos, como um PC de verdade faz a cada volta do sync.

// us é um microssegundo, a precisão do Postgres.
const us = time.Microsecond

// ditadoEm é o payload de um ditado novo com a data do PC escolhida.
func ditadoEm(text string, created time.Time) map[string]any {
	d := dictation(text)
	d["created_at"] = created.UTC().Format(time.RFC3339Nano)
	return d
}

// paraALixeira é a mudança do PC que põe o ditado na lixeira em at.
func paraALixeira(id string, base int64, at time.Time) mut {
	return change(id, base, map[string]any{"deleted_at": at.UTC().Format(time.RFC3339Nano)})
}

// criarAgora cria o ditado pelo PC, ditado uma hora antes do relógio atual.
func (w *world) criarAgora(token, id, text string) {
	w.t.Helper()
	if r := w.pushOne(token, createWith(id, ditadoEm(text, w.clk.Now().Add(-time.Hour)))); r.Status != "applied" || r.EntityVersion != 1 {
		w.t.Fatalf("criar %s agora: %+v, quero applied v1", id, r)
	}
}

// andar leva o relógio até quando, com um pull de cada PC a cada 60 dias no
// caminho: o token sem uso há 180 dias deixa de valer (F6).
func (w *world) andar(quando time.Time) {
	w.t.Helper()
	for w.clk.Now().Before(quando) {
		next := w.clk.Now().AddDate(0, 0, 60)
		if next.After(quando) {
			next = quando
		}
		w.clk.Set(next)
		for _, pc := range []string{pcA1, pcA2, pcB} {
			if rec := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0&limit=1", pc, nil); rec.Code != http.StatusOK {
				w.t.Fatalf("o pull de %s em %s: %d %s", pc, next, rec.Code, rec.Body.String())
			}
		}
	}
	w.clk.Set(quando)
}

// reter roda o Reter de um usuário do caso, com o relógio da suíte.
func (w *world) reter(userID string) cloudstore.Retido {
	w.t.Helper()
	r, err := w.b.Store.Reter(context.Background(), userID, w.clk.Now())
	if err != nil {
		w.t.Fatalf("Reter em %s: %v", w.clk.Now(), err)
	}
	return r
}

func (w *world) wantRetido(userID string, want cloudstore.Retido) {
	w.t.Helper()
	if got := w.reter(userID); got != want {
		w.t.Fatalf("Reter em %s = %+v, quero %+v", w.clk.Now(), got, want)
	}
}

// existe diz se o ditado está na nuvem, pelo Store (e o devolve).
func (w *world) existe(userID, id string) (cloudstore.Transcription, bool) {
	w.t.Helper()
	got, err := w.b.Store.Get(context.Background(), userID, id)
	switch {
	case errors.Is(err, cloudstore.ErrNotFound):
		return cloudstore.Transcription{}, false
	case err != nil:
		w.t.Fatalf("Get %s: %v", id, err)
	}
	return got, true
}

// lapideNoPull diz se o pull do começo traz a lápide do ditado (e o seq).
func (w *world) lapideNoPull(token, id string) (int64, bool) {
	w.t.Helper()
	c, ok := changeOf(w.pull(token, 0, 0), id)
	if !ok {
		return 0, false
	}
	if c.Operation != "tombstone" {
		w.t.Fatalf("a mudança de %s no pull = %+v, quero a lápide ou nada", id, c)
	}
	return c.Seq, true
}

// wantSeqs confere os seq dos eventos que ficaram (a arrumação apaga os órfãos
// velhos, então a sequência pode ter buracos embaixo; o contador nunca).
func (w *world) wantSeqs(userID string, want ...int64) {
	w.t.Helper()
	got := w.seqsOf(userID)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		w.t.Fatalf("os seq dos eventos = %v, quero %v", got, want)
	}
}

func (w *world) wantGone(token string, m mut, motivo string) {
	w.t.Helper()
	if r := w.pushOne(token, m); r.Status != "gone" || r.Motivo != motivo {
		w.t.Fatalf("%s de %s com a base %d = %+v, quero gone %s", m.Operation, m.EntityID, m.BaseVersion, r, motivo)
	}
}

// O ativo com 6 meses sai sem lápide e sem evento, e 1 s antes fica. O PC
// que ainda o tem recebe fora_da_janela, e nada volta à nuvem.
func testRetencaoAtivo(t *testing.T, w *world) {
	created := now0.Add(-time.Hour)
	x, depois := newID(), newID()
	w.mustCreate(pcA1, x, "vai vencer") // seq 1
	if r := w.pushOne(pcA1, createWith(depois, ditadoEm("um segundo depois", created.Add(time.Second)))); r.Status != "applied" {
		t.Fatalf("criar o de um segundo depois: %+v", r) // seq 2
	}
	due := created.AddDate(0, 6, 0)
	w.andar(due.Add(-time.Second))
	w.wantRetido(w.userA, cloudstore.Retido{})
	if _, ok := w.existe(w.userA, x); !ok {
		t.Fatal("o ativo sumiu 1 s antes dos 6 meses")
	}

	w.clk.Set(due)
	// O evento do ativo apagado fica órfão e, com mais de 90 dias, a
	// arrumação o leva na mesma rodada (F9c).
	w.wantRetido(w.userA, cloudstore.Retido{Ativos: 1, Eventos: 1})
	if _, ok := w.existe(w.userA, x); ok {
		t.Fatal("o ativo com 6 meses continua na nuvem")
	}
	if _, ok := w.existe(w.userA, depois); !ok {
		t.Fatal("o ditado de um segundo depois saiu antes da hora")
	}
	if _, ok := changeOf(w.pull(pcA1, 0, 0), x); ok {
		t.Fatal("o ativo vencido deixou rastro no pull (lápide, quero nada: o PC mantém a cópia até a F10)")
	}
	w.wantSeqs(w.userA, 2)

	w.clk.Set(due.Add(time.Second))
	w.wantRetido(w.userA, cloudstore.Retido{Ativos: 1, Eventos: 1})

	// O PC que ainda o tem, com uma edição pendente ou mandando tudo de novo
	// (a base 0), recebe fora_da_janela: mantém a cópia, e nada volta.
	w.wantGone(pcA2, change(x, 1, map[string]any{"final_text": "editado no PC parado"}), "fora_da_janela")
	w.wantGone(pcA2, createWith(x, ditadoEm("de novo", created)), "fora_da_janela")
	if _, ok := w.existe(w.userA, x); ok {
		t.Fatal("o ativo apagado voltou à nuvem")
	}
	// Nada disso gastou seq.
	w.criarAgora(pcA1, newID(), "depois da retenção")
	w.wantSeqs(w.userA, 3)
}

// A lixeira em D+29 fica e em D+30 sai, com a lápide, o evento tombstone (sem
// aparelho, a versão seguinte) e o seq. A segunda rodada não faz nada e não
// gasta seq.
func testRetencaoLixeira(t *testing.T, w *world) {
	x := newID()
	w.mustCreate(pcA1, x, "vai para a lixeira") // seq 1
	d := now0
	if r := w.pushOne(pcA1, paraALixeira(x, 1, d)); r.Status != "applied" || r.EntityVersion != 2 {
		t.Fatalf("lixeira: %+v", r) // seq 2
	}
	w.andar(d.AddDate(0, 0, 29))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(d.AddDate(0, 0, 30).Add(-us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	if it, ok := w.existe(w.userA, x); !ok || it.DeletedAt == nil {
		t.Fatalf("1 µs antes de D+30: %+v %v, quero na lixeira", it, ok)
	}

	w.clk.Set(d.AddDate(0, 0, 30))
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1})
	if _, ok := w.existe(w.userA, x); ok {
		t.Fatal("a lixeira vencida continua na nuvem")
	}
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 3 {
		t.Fatalf("a lápide no pull: seq %d %v, quero o seq 3", seq, ok)
	}
	ev := w.b.Events(t, w.userA)
	last := ev[len(ev)-1]
	if last.Seq != 3 || last.EntityType != "transcription" || last.EntityID != x || last.Operation != "tombstone" ||
		last.EntityVersion != 3 || last.DeviceID != nil {
		t.Fatalf("o evento da retenção = %+v, quero a lápide no seq 3, v3, sem aparelho", last)
	}
	requireContiguous(t, w.seqsOf(w.userA), 3)

	// O PC parado não o traz de volta: gone apagado, e o PC apaga a cópia.
	w.wantGone(pcA2, change(x, 2, map[string]any{"final_text": "editado no PC parado"}), "apagado")
	w.wantGone(pcA2, createWith(x, dictation("de novo")), "apagado")
	if _, err := w.b.Store.Restore(context.Background(), cloudstore.Actor{UserID: w.userA}, x, w.clk.Now()); !errors.Is(err, cloudstore.ErrNotFound) {
		t.Fatalf("restaurar o apagado: %v, quero ErrNotFound", err)
	}

	// Idempotente: a segunda rodada não acha nada e não gasta seq.
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.criarAgora(pcA1, newID(), "depois da retenção")
	requireContiguous(t, w.seqsOf(w.userA), 4)
}

// A lixeira com a data antes do próprio ditado (a revisão da F9): o PC 2, com o
// relógio 40 dias atrasado, põe na lixeira o ditado que o PC 1 criou. O
// vencimento conta do created_at, e o ditado fica restaurável os 30 dias
// (AC-018), em vez de sair na rodada seguinte com a lápide que faria os PCs
// apagarem a cópia.
func testRetencaoLixeiraAntesDoDitado(t *testing.T, w *world) {
	x := newID()
	created := now0.Add(-time.Hour)
	w.mustCreate(pcA1, x, "criado no PC com o relógio certo") // seq 1
	atrasado := now0.AddDate(0, 0, -40)
	if r := w.pushOne(pcA2, paraALixeira(x, 1, atrasado)); r.Status != "applied" {
		t.Fatalf("a lixeira do PC atrasado: %+v", r) // seq 2
	}
	due := created.AddDate(0, 0, 30)
	if it, ok := w.existe(w.userA, x); !ok || it.DeletedAt == nil || it.PurgeAt == nil || !it.PurgeAt.Equal(due) {
		t.Fatalf("na lixeira: %+v %v, quero o purge_at em %s (o created_at mais 30 dias)", it, ok, due)
	}
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.andar(due.Add(-us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	if it, ok := w.existe(w.userA, x); !ok || it.DeletedAt == nil {
		t.Fatalf("1 µs antes dos 30 dias do ditado: %+v %v, quero na lixeira", it, ok)
	}
	w.clk.Set(due)
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1})
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 3 {
		t.Fatalf("a lápide: seq %d %v, quero o seq 3", seq, ok)
	}
}

// naLixeiraDoCelular diz se o ditado aparece na lixeira do app da nuvem (a
// lista do celular A), de onde a pessoa o restaura.
func (w *world) naLixeiraDoCelular(id string) bool {
	w.t.Helper()
	rec := w.do(http.MethodGet, "/api/v1/transcriptions?trash=true", celA, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("a lixeira do celular: %d %s", rec.Code, rec.Body.String())
	}
	for _, it := range decode[struct {
		Items []itemJSON `json:"items"`
	}](w.t, rec).Items {
		if it.ID == id {
			return it.DeletedAt != nil
		}
	}
	return false
}

// wantPurgeAt confere que o ditado está na lixeira, com o deleted_at que o PC
// mandou e o vencimento gravado.
func (w *world) wantPurgeAt(id string, deleted, purge time.Time) {
	w.t.Helper()
	it, ok := w.existe(w.userA, id)
	if !ok || it.DeletedAt == nil || !it.DeletedAt.Equal(deleted) || it.PurgeAt == nil || !it.PurgeAt.Equal(purge) {
		w.t.Fatalf("%s: %+v %v; quero na lixeira, com o deleted_at %s e o purge_at %s", id, it, ok, deleted, purge)
	}
}

// O piso da lixeira que chega pelo push (a decisão do Mauro de 08-out-2026,
// "Fica 24 h na lixeira"): o PC 2 ficou 40 dias sem internet com x na lixeira
// (a D4). Sem o piso, a lixeira chegaria vencida, e o job apagaria x na
// rodada seguinte, com a lápide que faz todos os PCs apagarem a cópia, sem ele
// passar pela lixeira restaurável da nuvem e do celular. Com o piso, x fica 24
// h na lixeira da nuvem e então sai, com lápide (ainda está na janela). O y,
// posto na lixeira 2 dias antes de chegar, é a lixeira de sempre: os 30 dias
// contam do deleted_at, e não da chegada.
func testRetencaoLixeiraAntigaQueChega(t *testing.T, w *world) {
	x, y := newID(), newID()
	criado := now0.AddDate(0, 0, -60)
	for _, id := range []string{x, y} {
		if r := w.pushOne(pcA1, createWith(id, ditadoEm("de dois meses", criado))); r.Status != "applied" {
			t.Fatalf("criar %s: %+v", id, r) // seq 1 e 2
		}
	}
	antiga, recente := now0.AddDate(0, 0, -40), now0.AddDate(0, 0, -2)
	for _, it := range []struct {
		id string
		at time.Time
	}{{x, antiga}, {y, recente}} {
		if r := w.pushOne(pcA2, paraALixeira(it.id, 1, it.at)); r.Status != "applied" || r.EntityVersion != 2 {
			t.Fatalf("a lixeira de %s chega: %+v", it.id, r) // seq 3 e 4
		}
	}
	piso := now0.Add(cloudstore.FolgaDaLapide)
	w.wantPurgeAt(x, antiga, piso)
	yDue := recente.AddDate(0, 0, 30)
	w.wantPurgeAt(y, recente, yDue)

	// Na chegada e até 1 µs antes das 24 h: na lixeira da nuvem e do celular.
	for _, now := range []time.Time{now0, now0.Add(time.Hour), piso.Add(-us)} {
		w.clk.Set(now)
		w.wantRetido(w.userA, cloudstore.Retido{})
		if !w.naLixeiraDoCelular(x) {
			t.Fatalf("em %s, x não está na lixeira do celular", now)
		}
	}
	if c, ok := changeOf(w.pull(pcA1, 0, 0), x); !ok || c.Operation != "upsert" || c.Payload == nil || c.Payload.DeletedAt == nil {
		t.Fatalf("x no pull antes das 24 h = %+v %v, quero o upsert na lixeira, sem lápide", c, ok)
	}

	// Nas 24 h, sai com a lápide, o evento e o seq.
	w.clk.Set(piso)
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // seq 5
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 5 {
		t.Fatalf("a lápide de x: seq %d %v, quero o seq 5", seq, ok)
	}
	w.wantGone(pcA1, change(x, 1, map[string]any{"final_text": "editado no PC 1"}), "apagado")
	if !w.naLixeiraDoCelular(y) {
		t.Fatal("y saiu da lixeira com x")
	}

	// O y: 1 µs antes dos 30 dias do deleted_at fica, e neles sai.
	w.andar(yDue.Add(-us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(yDue)
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // seq 6
	if seq, ok := w.lapideNoPull(pcA1, y); !ok || seq != 6 {
		t.Fatalf("a lápide de y: seq %d %v, quero o seq 6", seq, ok)
	}
	requireContiguous(t, w.seqsOf(w.userA), 6)
}

// O restaurar dentro das 24 h do piso traz o ditado de volta, e a lixeira
// seguinte calcula o piso de novo, com a chegada dela.
func testRetencaoRestaurarNoPiso(t *testing.T, w *world) {
	x := newID()
	criado := now0.AddDate(0, 0, -60)
	if r := w.pushOne(pcA1, createWith(x, ditadoEm("restaurado no celular", criado))); r.Status != "applied" {
		t.Fatalf("criar: %+v", r) // seq 1
	}
	antiga := now0.AddDate(0, 0, -40)
	if r := w.pushOne(pcA2, paraALixeira(x, 1, antiga)); r.Status != "applied" || r.EntityVersion != 2 {
		t.Fatalf("a lixeira antiga chega: %+v", r) // seq 2
	}
	w.wantRetido(w.userA, cloudstore.Retido{})

	// 12 h depois, a pessoa a vê na lixeira do celular e restaura.
	w.clk.Set(now0.Add(12 * time.Hour))
	if !w.naLixeiraDoCelular(x) {
		t.Fatal("x não está na lixeira do celular")
	}
	if rec := w.do(http.MethodPost, "/api/v1/transcriptions/"+x+"/restore", celA, nil); rec.Code != http.StatusOK {
		t.Fatalf("restaurar pelo celular: %d %s", rec.Code, rec.Body.String()) // seq 3
	}
	if it, ok := w.existe(w.userA, x); !ok || it.DeletedAt != nil || it.PurgeAt != nil || it.Version != 3 {
		t.Fatalf("o restaurado = %+v %v, quero fora da lixeira, sem purge_at, v3", it, ok)
	}

	// Passadas as 24 h da primeira chegada, fica: o restaurar limpou o piso.
	w.clk.Set(now0.Add(cloudstore.FolgaDaLapide))
	w.wantRetido(w.userA, cloudstore.Retido{})
	if c, ok := changeOf(w.pull(pcA1, 0, 0), x); !ok || c.Operation != "upsert" || c.Seq != 3 ||
		c.Payload == nil || c.Payload.DeletedAt != nil {
		t.Fatalf("o restaurado no pull = %+v %v, quero o upsert fora da lixeira, no seq 3", c, ok)
	}

	// O PC 2, ainda com a data antiga, põe na lixeira de novo, 30 h depois da
	// primeira chegada: o piso conta desta chegada.
	segunda := now0.Add(30 * time.Hour)
	w.clk.Set(segunda)
	outraAntiga := antiga.Add(time.Hour)
	if r := w.pushOne(pcA2, paraALixeira(x, 3, outraAntiga)); r.Status != "applied" || r.EntityVersion != 4 {
		t.Fatalf("a lixeira de novo: %+v", r) // seq 4
	}
	w.wantPurgeAt(x, outraAntiga, segunda.Add(cloudstore.FolgaDaLapide))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(segunda.Add(cloudstore.FolgaDaLapide - us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(segunda.Add(cloudstore.FolgaDaLapide))
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // seq 5
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 5 {
		t.Fatalf("a lápide: seq %d %v, quero o seq 5", seq, ok)
	}
}

// A decisão refeita sob a trava lê o purge_at gravado: x, na lixeira vencida,
// é escolhido sem trava; antes da trava, o celular o restaura e o PC 2 o põe
// na lixeira de novo, com uma data antiga, que já teria vencido pelos 30 dias.
// O piso conta da chegada desta lixeira, e x fica; sai 24 h depois.
func testRetencaoLixeiraNovaNoMeio(t *testing.T, w *world) {
	if w.b.AntesDaTrava == nil {
		t.Skip("o Backend não tem o gancho da retenção")
	}
	x := newID()
	if r := w.pushOne(pcA1, createWith(x, ditadoEm("de dois meses", now0.AddDate(0, 0, -60)))); r.Status != "applied" {
		t.Fatalf("criar: %+v", r) // seq 1
	}
	if r := w.pushOne(pcA1, paraALixeira(x, 1, now0)); r.Status != "applied" {
		t.Fatalf("lixeira: %+v", r) // seq 2
	}
	w.andar(now0.AddDate(0, 0, 30))
	antiga := now0.AddDate(0, 0, -10)
	var vistos []string
	w.b.AntesDaTrava(t, func(id string) {
		vistos = append(vistos, id)
		if _, err := w.b.Store.Restore(context.Background(), cloudstore.Actor{UserID: w.userA}, x, w.clk.Now()); err != nil {
			t.Errorf("restaurar no meio: %v", err) // seq 3
		}
		if r := w.pushOne(pcA2, paraALixeira(x, 3, antiga)); r.Status != "applied" || r.EntityVersion != 4 {
			t.Errorf("a lixeira antiga no meio: %+v", r) // seq 4
		}
	})
	w.wantRetido(w.userA, cloudstore.Retido{})
	if len(vistos) != 1 || vistos[0] != x {
		t.Fatalf("o gancho viu %v, quero só x", vistos)
	}
	piso := w.clk.Now().Add(cloudstore.FolgaDaLapide)
	w.wantPurgeAt(x, antiga, piso)
	w.b.AntesDaTrava(t, nil)

	w.clk.Set(piso.Add(-us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(piso)
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // seq 5
	requireContiguous(t, w.seqsOf(w.userA), 5)
}

// A janela de 6 meses ganha do piso: a lixeira antiga de um ditado que fecha
// os 6 meses 10 h depois da chegada vence com eles, e não 24 h depois. O x sai
// na janela, com a lápide (dentro da folga); o y, que a fecha 2 h depois e só
// é visto pelo job mais de 24 h depois dela, sai como sem o piso: sem lápide,
// sem evento e sem seq.
func testRetencaoJanelaGanhaDoPiso(t *testing.T, w *world) {
	x, y := newID(), newID()
	criadoX := now0.AddDate(0, -6, 0).Add(10 * time.Hour)
	criadoY := criadoX.Add(2 * time.Hour)
	janelaX, janelaY := criadoX.AddDate(0, 6, 0), criadoY.AddDate(0, 6, 0)
	if !janelaX.Equal(now0.Add(10*time.Hour)) || !janelaY.Equal(now0.Add(12*time.Hour)) {
		t.Fatalf("o cenário: as janelas em %s e %s", janelaX, janelaY)
	}
	for _, it := range []struct {
		id string
		at time.Time
	}{{x, criadoX}, {y, criadoY}} {
		if r := w.pushOne(pcA1, createWith(it.id, ditadoEm("perto dos 6 meses", it.at))); r.Status != "applied" {
			t.Fatalf("criar %s: %+v", it.id, r) // seq 1 e 2
		}
	}
	antiga := criadoX.AddDate(0, 0, 1)
	for _, id := range []string{x, y} {
		if r := w.pushOne(pcA2, paraALixeira(id, 1, antiga)); r.Status != "applied" {
			t.Fatalf("a lixeira antiga de %s chega: %+v", id, r) // seq 3 e 4
		}
	}
	w.wantPurgeAt(x, antiga, janelaX)
	w.wantPurgeAt(y, antiga, janelaY)
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(janelaX.Add(-us))
	w.wantRetido(w.userA, cloudstore.Retido{})

	// Na janela de x, antes do piso (24 h depois da chegada): x sai, com a
	// lápide, porque a janela acabou há menos de 24 h.
	w.clk.Set(janelaX)
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // seq 5
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 5 {
		t.Fatalf("a lápide de x: seq %d %v, quero o seq 5", seq, ok)
	}
	w.wantGone(pcA2, change(x, 2, map[string]any{"final_text": "editado no PC 2"}), "apagado")

	// O y, visto pelo job só 24 h depois da janela dele: sem lápide. A
	// lápide de x, com a janela mais um dia passados, vence na mesma rodada.
	w.clk.Set(janelaY.Add(cloudstore.FolgaDaLapide))
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesVencidas: 1})
	if _, ok := w.existe(w.userA, y); ok {
		t.Fatal("y continua na nuvem")
	}
	if _, ok := w.lapideNoPull(pcA1, y); ok {
		t.Fatal("y deixou lápide fora da janela há 24 h")
	}
	w.wantGone(pcA2, change(y, 2, map[string]any{"final_text": "editado no PC 2"}), "fora_da_janela")
	w.criarAgora(pcA1, newID(), "depois")
	requireContiguous(t, w.seqsOf(w.userA), 6)
}

// O reenvio não mexe no piso: o mesmo lote reenviado (duplicate), a lixeira
// mandada de novo sobre a versão atual (nada a gravar) e o recomeço com a
// base 0 (grava uma versão nova, mesclada) deixam o purge_at da primeira
// chegada, e o ditado sai 24 h depois dela.
func testRetencaoReenvioNaoMexeNoPiso(t *testing.T, w *world) {
	x := newID()
	criado := now0.AddDate(0, 0, -60)
	if r := w.pushOne(pcA1, createWith(x, ditadoEm("de dois meses", criado))); r.Status != "applied" {
		t.Fatalf("criar: %+v", r) // seq 1
	}
	antiga := now0.AddDate(0, 0, -40)
	m := paraALixeira(x, 1, antiga)
	if r := w.pushOne(pcA2, m); r.Status != "applied" || r.EntityVersion != 2 {
		t.Fatalf("a lixeira antiga chega: %+v", r) // seq 2
	}
	piso := now0.Add(cloudstore.FolgaDaLapide)
	w.wantPurgeAt(x, antiga, piso)

	w.clk.Set(now0.Add(12 * time.Hour))
	if r := w.pushOne(pcA2, m); r.Status != "duplicate" || r.Original != "applied" || r.EntityVersion != 2 {
		t.Fatalf("o reenvio = %+v, quero duplicate do applied v2", r)
	}
	w.wantPurgeAt(x, antiga, piso)
	if r := w.pushOne(pcA2, paraALixeira(x, 2, antiga.Add(time.Hour))); r.Status != "applied" || r.EntityVersion != 2 {
		t.Fatalf("a lixeira de novo sobre a v2 = %+v, quero applied v2, sem gravar", r)
	}
	w.wantPurgeAt(x, antiga, piso)
	tudo := ditadoEm("de dois meses", criado)
	tudo["deleted_at"] = antiga.Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	if r := w.pushOne(pcA2, createWith(x, tudo)); r.Status != "applied" || !r.Mesclado || r.EntityVersion != 3 {
		t.Fatalf("o recomeço com a base 0 = %+v, quero applied mesclado v3", r) // seq 3
	}
	w.wantPurgeAt(x, antiga, piso)

	w.clk.Set(piso.Add(-us))
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.clk.Set(piso)
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // seq 4
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 4 {
		t.Fatalf("a lápide: seq %d %v, quero o seq 4", seq, ok)
	}
	requireContiguous(t, w.seqsOf(w.userA), 4)
}

// Onde a lixeira antiga que chega aparece (a revisão da D5): na lixeira da
// nuvem e do celular, que é ordenada pelo deleted_at, e o deleted_at gravado é
// o que o PC mandou. Ela chega sempre como a mais antiga da lixeira: vem no fim
// da lista, debaixo da data antiga, depois de uma lixeira recente, mesmo tendo
// chegado por último e sendo o ditado mais novo; com uma página de um item, só
// na segunda. Pô-la no topo pediria outra ordem, por um campo do servidor, e é
// uma decisão nova do Mauro.
func testRetencaoLixeiraAntigaNoFimDaLixeira(t *testing.T, w *world) {
	x, y := newID(), newID()
	// x é o ditado mais novo: nem a ordem do created_at nem a da chegada o
	// deixariam no fim.
	for _, it := range []struct {
		id string
		at time.Time
	}{{x, now0.AddDate(0, 0, -50)}, {y, now0.AddDate(0, 0, -60)}} {
		if r := w.pushOne(pcA1, createWith(it.id, ditadoEm("de dois meses", it.at))); r.Status != "applied" {
			t.Fatalf("criar %s: %+v", it.id, r)
		}
	}
	// O y vai para a lixeira pelo celular, agora; 1 h depois chega, pelo PC 2,
	// a lixeira de x feita há 40 dias.
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+y, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("a lixeira de y pelo celular: %d %s", rec.Code, rec.Body.String())
	}
	chegada := now0.Add(time.Hour)
	w.clk.Set(chegada)
	antiga := now0.AddDate(0, 0, -40)
	if r := w.pushOne(pcA2, paraALixeira(x, 1, antiga)); r.Status != "applied" || r.EntityVersion != 2 {
		t.Fatalf("a lixeira antiga de x chega: %+v", r)
	}
	w.wantPurgeAt(x, antiga, chegada.Add(cloudstore.FolgaDaLapide))

	lixeira := func(q string) ([]itemJSON, string) {
		rec := w.do(http.MethodGet, "/api/v1/transcriptions?trash=true"+q, celA, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("a lixeira do celular%s: %d %s", q, rec.Code, rec.Body.String())
		}
		v := decode[struct {
			Items []itemJSON `json:"items"`
			Next  string     `json:"next_cursor"`
		}](t, rec)
		return v.Items, v.Next
	}
	itens, _ := lixeira("")
	if len(itens) != 2 || itens[0].ID != y || itens[1].ID != x {
		t.Fatalf("a lixeira = %+v, quero y e então x, no fim", itens)
	}
	if itens[1].DeletedAt == nil || !itens[1].DeletedAt.Equal(antiga) {
		t.Fatalf("x na lixeira com o deleted_at %v, quero o que o PC mandou (%s)", itens[1].DeletedAt, antiga)
	}
	primeira, next := lixeira("&limit=1")
	segunda, fim := lixeira("&limit=1&cursor=" + next)
	if len(primeira) != 1 || primeira[0].ID != y || next == "" || len(segunda) != 1 || segunda[0].ID != x || fim != "" {
		t.Fatalf("a lixeira em páginas de 1 = %+v (cursor %q) e %+v (cursor %q), quero y e então x", primeira, next, segunda, fim)
	}
}

// A sobreposição: posto na lixeira perto dos 6 meses, o ditado vence com eles.
// Com lápide se a janela acabou há menos de 24 h; sem ela (nem evento nem
// seq) se há mais. O "excluir definitivamente" fora da janela também não
// deixa lápide.
func testRetencaoSobreposicao(t *testing.T, w *world) {
	c := now0.Add(-time.Hour)
	x, y, z := newID(), newID(), newID()
	w.mustCreate(pcA1, x, "no limite") // seq 1
	for i, it := range []struct {
		id string
		at time.Time
	}{{y, c.AddDate(0, 0, -2)}, {z, c.AddDate(0, 0, -3)}} {
		if r := w.pushOne(pcA1, createWith(it.id, ditadoEm(fmt.Sprintf("dias antes %d", i), it.at))); r.Status != "applied" {
			t.Fatalf("criar: %+v", r) // seq 2 e 3
		}
	}
	trashAt := c.AddDate(0, 5, 25)
	w.andar(trashAt)
	for _, id := range []string{x, y, z} {
		if r := w.pushOne(pcA1, paraALixeira(id, 1, trashAt)); r.Status != "applied" {
			t.Fatalf("lixeira de %s: %+v", id, r) // seq 4 a 6
		}
	}
	end := c.AddDate(0, 6, 0) // o fim da janela de x (e o vencimento da lixeira dele)
	w.andar(end.Add(-us))
	// y e z: a janela acabou há mais de 24 h. Os eventos da criação dos três
	// (seq 1 a 3) são órfãos com mais de 90 dias: a arrumação os leva; os da
	// lixeira (seq 4 a 6) são novos e ficam.
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 2, Eventos: 3})
	if _, ok := w.lapideNoPull(pcA1, y); ok {
		t.Fatal("a lixeira vencida há mais de 24 h deixou lápide")
	}
	w.wantSeqs(w.userA, 4, 5, 6)

	// O "excluir definitivamente" fora da janela (um ditado de 3 dias antes de
	// x, posto na lixeira junto, apagado de vez no fim da janela de x): só
	// apaga, sem lápide, evento nem seq.
	old := newID()
	w.clk.Set(trashAt)
	if r := w.pushOne(pcA1, createWith(old, ditadoEm("excluir de vez", c.AddDate(0, 0, -3)))); r.Status != "applied" {
		t.Fatalf("criar o de 3 dias antes: %+v", r) // seq 7
	}
	if r := w.pushOne(pcA1, paraALixeira(old, 1, trashAt)); r.Status != "applied" {
		t.Fatalf("lixeira: %+v", r) // seq 8
	}
	w.clk.Set(end)
	if err := w.b.Store.Purge(context.Background(), cloudstore.Actor{UserID: w.userA}, old, w.clk.Now()); err != nil {
		t.Fatalf("excluir definitivamente fora da janela: %v", err)
	}
	if _, ok := w.existe(w.userA, old); ok {
		t.Fatal("o excluir definitivamente não apagou")
	}
	if _, ok := w.lapideNoPull(pcA1, old); ok {
		t.Fatal("o excluir definitivamente fora da janela deixou lápide (ela nasceria vencida)")
	}
	w.wantSeqs(w.userA, 4, 5, 6, 7, 8)

	// O x vence agora, no fim da janela: com lápide, que vive mais um dia.
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1})
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 9 {
		t.Fatalf("a lápide do x: seq %d %v, quero o seq 9", seq, ok)
	}
	w.wantSeqs(w.userA, 4, 5, 6, 7, 8, 9)
	w.wantGone(pcA2, createWith(x, dictation("de novo")), "apagado")
	for _, id := range []string{y, z, old} {
		w.wantGone(pcA2, change(id, 2, map[string]any{"final_text": "editado no PC parado"}), "fora_da_janela")
	}
}

// A lápide vive a janela inteira mais um dia, decidida linha a linha: o ditado
// de 31/ago sai da janela em 3/mar (o AddDate empurra o 31/fev), e a lápide
// dele em 4/mar. Um corte de data a apagaria em 2/mar (em Go) ou 1/mar (o
// interval do Postgres). As lápides saem fora da ordem do seq.
func testRetencaoLapideNoFimDoMes(t *testing.T, w *world) {
	ago31 := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	set10 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	w.clk.Set(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC))
	a, b := newID(), newID()
	for _, it := range []struct {
		id string
		at time.Time
	}{{a, ago31}, {b, set10}} {
		if r := w.pushOne(pcA1, createWith(it.id, ditadoEm("fim do mês", it.at))); r.Status != "applied" {
			t.Fatalf("criar: %+v", r) // seq 1 e 2
		}
	}
	// O PC 2 conhece os dois e para: o cursor dele fica antes das lápides.
	cursorAntigo := w.pull(pcA2, 0, 0).LastSeq
	if cursorAntigo != 2 {
		t.Fatalf("o cursor do PC 2 = %d, quero 2", cursorAntigo)
	}
	ctx, cel := context.Background(), cloudstore.Actor{UserID: w.userA}
	// A lápide de b (o ditado mais novo) nasce antes da de a: seq 5 e 6.
	for _, id := range []string{b, a} {
		if _, err := w.b.Store.Trash(ctx, cel, id, w.clk.Now()); err != nil {
			t.Fatal(err) // seq 3 e 4
		}
	}
	for _, id := range []string{b, a} {
		if err := w.b.Store.Purge(ctx, cel, id, w.clk.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if sa, oka := w.lapideNoPull(pcA1, a); !oka || sa != 6 {
		t.Fatalf("a lápide de a: %d %v, quero o seq 6", sa, oka)
	}

	// Em 1/mar (o corte do interval do Postgres) a lápide de a vive. Os seis
	// eventos, de 15/set e todos órfãos (os dois ditados foram apagados de
	// vez), saem pela arrumação.
	w.andar(time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC))
	w.wantRetido(w.userA, cloudstore.Retido{Eventos: 6})
	w.wantGone(pcA2, createWith(a, ditadoEm("de novo", ago31)), "apagado")
	for _, now := range []time.Time{
		time.Date(2027, 3, 2, 12, 0, 0, 0, time.UTC), // o corte calculado em Go
		time.Date(2027, 3, 3, 12, 0, 0, 0, time.UTC), // o fim da janela
		time.Date(2027, 3, 4, 12, 0, 0, 0, time.UTC).Add(-us),
	} {
		w.andar(now)
		w.wantRetido(w.userA, cloudstore.Retido{})
		w.wantGone(pcA2, createWith(a, ditadoEm("de novo", ago31)), "apagado")
	}
	w.clk.Set(time.Date(2027, 3, 4, 12, 0, 0, 0, time.UTC))
	w.wantRetido(w.userA, cloudstore.Retido{LapidesVencidas: 1})
	if _, ok := w.lapideNoPull(pcA1, a); ok {
		t.Fatal("a lápide vencida de a continua no pull")
	}
	if sb, ok := w.lapideNoPull(pcA1, b); !ok || sb != 5 {
		t.Fatalf("a lápide de b (seq menor, ditado mais novo): %d %v, quero o seq 5", sb, ok)
	}
	// D2: sem 410 no mesmo epoch. O PC 2 volta e puxa do cursor antigo, de
	// antes da lápide de a, que venceu (o pull exige 200): a de b, que ainda
	// vive, chega.
	if p := w.pull(pcA2, cursorAntigo, 0); len(p.Changes) != 1 || p.Changes[0].EntityID != b ||
		p.Changes[0].Operation != "tombstone" || p.Changes[0].Seq != 5 {
		t.Fatalf("o pull do cursor antigo = %+v, quero 200 e só a lápide de b (seq 5)", p)
	}
	w.wantGone(pcA2, createWith(a, ditadoEm("de novo", ago31)), "fora_da_janela")
	w.wantGone(pcA2, createWith(b, ditadoEm("de novo", set10)), "apagado")

	w.andar(time.Date(2027, 3, 11, 12, 0, 0, 0, time.UTC))
	w.wantRetido(w.userA, cloudstore.Retido{LapidesVencidas: 1})
	if p := w.pull(pcA1, 0, 0); len(p.Changes) != 0 {
		t.Fatalf("o pull depois das lápides vencidas = %+v, quero nada", p)
	}
	if p := w.pull(pcA2, cursorAntigo, 0); len(p.Changes) != 0 {
		t.Fatalf("o pull do cursor antigo depois das lápides vencidas = %+v, quero 200 e nada", p)
	}
	w.wantGone(pcA2, createWith(b, ditadoEm("de novo", set10)), "fora_da_janela")
	// Nada voltou à nuvem, e as lápides não gastam seq ao vencer.
	for _, id := range []string{a, b} {
		if _, ok := w.existe(w.userA, id); ok {
			t.Fatalf("%s voltou à nuvem", id)
		}
	}
	novo := newID()
	w.criarAgora(pcA1, novo, "depois das lápides")
	w.wantSeqs(w.userA, 7)
	if p := w.pull(pcA2, cursorAntigo, 0); len(p.Changes) != 1 || p.Changes[0].EntityID != novo || p.Changes[0].Seq != 7 {
		t.Fatalf("o pull do cursor antigo depois de um ditado novo = %+v, quero só o novo, no seq 7", p)
	}
}

// O mesmo id em duas contas: reter A não toca B.
func testRetencaoDuasContas(t *testing.T, w *world) {
	ativo, lixo := newID(), newID()
	for _, pc := range []string{pcA1, pcB} {
		w.mustCreate(pc, ativo, "o mesmo id")
		w.mustCreate(pc, lixo, "o mesmo id, na lixeira")
		if r := w.pushOne(pc, paraALixeira(lixo, 1, now0)); r.Status != "applied" {
			t.Fatalf("lixeira: %+v", r)
		}
	}
	users, err := w.b.Store.UsuariosDaRetencao(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, u := range users {
		found[u] = true
	}
	if !found[w.userA] || !found[w.userB] {
		t.Fatalf("UsuariosDaRetencao = %v, sem A ou B", users)
	}
	w.andar(now0.Add(-time.Hour).AddDate(0, 6, 0))
	// Os três eventos de A (seq 1 a 3) ficam órfãos e velhos: saem.
	w.wantRetido(w.userA, cloudstore.Retido{Ativos: 1, Lixeira: 1, LapidesCriadas: 1, Eventos: 3})
	w.wantSeqs(w.userA, 4)
	for _, id := range []string{ativo, lixo} {
		if _, ok := w.existe(w.userA, id); ok {
			t.Fatalf("o %s de A continua", id)
		}
		if _, ok := w.existe(w.userB, id); !ok {
			t.Fatalf("o %s de B sumiu com o Reter de A", id)
		}
	}
	if c, ok := changeOf(w.pull(pcB, 0, 0), lixo); !ok || c.Operation != "upsert" || c.Payload.DeletedAt == nil {
		t.Fatalf("o pull de B = %+v %v, quero o ditado dele, na lixeira (a lápide é de A)", c, ok)
	}
	requireContiguous(t, w.seqsOf(w.userB), 3)
	w.wantRetido(w.userB, cloudstore.Retido{Ativos: 1, Lixeira: 1, LapidesCriadas: 1, Eventos: 3})
	w.wantSeqs(w.userB, 4)
}

// O PC parado: com a lápide viva, gone apagado; com ela vencida,
// fora_da_janela; o ativo que a nuvem apagou também fora_da_janela; e nada
// volta à nuvem.
func testRetencaoPCParado(t *testing.T, w *world) {
	x, y := newID(), newID()
	w.mustCreate(pcA1, x, "vai para a lixeira em outro aparelho") // seq 1
	w.mustCreate(pcA1, y, "fica no histórico")                    // seq 2
	p0 := w.pull(pcA2, 0, 0)
	if len(p0.Changes) != 2 {
		t.Fatalf("o PC 2 conhece os dois: %+v", p0)
	}
	// O cursor do PC 2 parado: ele não puxa mais até voltar.
	cursorAntigo := p0.LastSeq
	if r := w.pushOne(pcA1, paraALixeira(x, 1, now0)); r.Status != "applied" {
		t.Fatalf("lixeira: %+v", r) // seq 3
	}
	w.andar(now0.AddDate(0, 0, 30))
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // seq 4
	// O PC 2 ficou parado, com edições pendentes sobre a versão 1.
	w.wantGone(pcA2, change(x, 1, map[string]any{"final_text": "editado no PC parado"}), "apagado")
	w.wantGone(pcA2, createWith(x, dictation("mandado de novo")), "apagado")

	w.andar(now0.Add(-time.Hour).AddDate(0, 6, 0).Add(cloudstore.FolgaDaLapide))
	// Os quatro eventos ficam órfãos e velhos (o da lápide inclusive): saem.
	w.wantRetido(w.userA, cloudstore.Retido{Ativos: 1, LapidesVencidas: 1, Eventos: 4})
	// D2: a nuvem não responde 410 no mesmo epoch. O PC 2 volta e puxa do
	// cursor antigo, de antes da lápide que venceu (o pull exige 200), e não
	// há nada a receber: o ditado e a lápide saíram.
	if p := w.pull(pcA2, cursorAntigo, 0); len(p.Changes) != 0 {
		t.Fatalf("o pull do cursor antigo = %+v, quero 200 e nada", p)
	}
	for _, m := range []mut{
		change(x, 1, map[string]any{"final_text": "editado no PC parado"}),
		createWith(x, dictation("mandado de novo")),
		change(y, 1, map[string]any{"final_text": "editado no PC parado"}),
		createWith(y, dictation("mandado de novo")),
	} {
		w.wantGone(pcA2, m, "fora_da_janela")
	}
	if p := w.pull(pcA1, 0, 0); len(p.Changes) != 0 {
		t.Fatalf("o pull = %+v, quero nada: nada volta à nuvem", p)
	}
	for _, id := range []string{x, y} {
		if _, ok := w.existe(w.userA, id); ok {
			t.Fatalf("%s voltou à nuvem", id)
		}
	}
	novo := newID()
	w.criarAgora(pcA1, novo, "depois")
	w.wantSeqs(w.userA, 5)
	// E do mesmo cursor antigo chega o que mudou depois.
	if p := w.pull(pcA2, cursorAntigo, 0); len(p.Changes) != 1 || p.Changes[0].EntityID != novo || p.LastSeq != 5 {
		t.Fatalf("o pull do cursor antigo depois de um ditado novo = %+v, quero só o novo, no seq 5", p)
	}
}

// AC-018: o ditado restaurado entre a escolha (sem trava) e a trava fica. O
// Reter decide de novo sobre a linha travada.
func testRetencaoRestauradoNoMeio(t *testing.T, w *world) {
	if w.b.AntesDaTrava == nil {
		t.Skip("o Backend não tem o gancho da retenção")
	}
	x, y := newID(), newID()
	// x é ditado antes: é o primeiro candidato.
	if r := w.pushOne(pcA1, createWith(x, ditadoEm("restaurado no meio", now0.Add(-2*time.Hour)))); r.Status != "applied" {
		t.Fatalf("criar: %+v", r) // seq 1
	}
	w.mustCreate(pcA1, y, "vai") // seq 2
	for _, id := range []string{x, y} {
		if r := w.pushOne(pcA1, paraALixeira(id, 1, now0)); r.Status != "applied" {
			t.Fatalf("lixeira: %+v", r) // seq 3 e 4
		}
	}
	w.andar(now0.AddDate(0, 0, 30))
	var vistos []string
	w.b.AntesDaTrava(t, func(id string) {
		vistos = append(vistos, id)
		if id != x {
			return
		}
		if _, err := w.b.Store.Restore(context.Background(), cloudstore.Actor{UserID: w.userA}, x, w.clk.Now()); err != nil {
			t.Errorf("restaurar no meio: %v", err) // seq 5
		}
	})
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) // y: seq 6
	if len(vistos) != 2 || vistos[0] != x || vistos[1] != y {
		t.Fatalf("o gancho viu %v, quero x e depois y", vistos)
	}
	it, ok := w.existe(w.userA, x)
	if !ok || it.DeletedAt != nil || it.Version != 3 || it.LastSeq != 5 {
		t.Fatalf("o restaurado no meio = %+v %v, quero fora da lixeira, v3 no seq 5", it, ok)
	}
	if seq, ok := w.lapideNoPull(pcA1, y); !ok || seq != 6 {
		t.Fatalf("a lápide de y: %d %v, quero o seq 6", seq, ok)
	}
	requireContiguous(t, w.seqsOf(w.userA), 6)
}

// O aprendido, o desfeito inclusive, não é tocado pela retenção (D1: fica
// enquanto a conta existir). O PC que ainda tem o par desfeito o manda de novo
// com a base 0 e perde para o desfazer, em vez de criá-lo.
func testRetencaoAprendidoDesfeito(t *testing.T, w *world) {
	w.mustLearn(pcA1, aprender("sf", "ECF", 0), 1)    // seq 1
	w.mustLearn(pcA1, desfazer("sf", 1), 2)           // seq 2
	w.mustLearn(pcA1, aprender("pq", "porque", 0), 1) // seq 3
	w.andar(now0.AddDate(1, 0, 0))
	// Só o evento do primeiro aprender (seq 1) é órfão; o do desfazer é o
	// last_seq da linha desfeita e fica (F9c). As três respostas guardadas
	// passaram de 190 dias.
	w.wantRetido(w.userA, cloudstore.Retido{Eventos: 1, Mutacoes: 3})
	w.wantSeqs(w.userA, 2, 3)
	p := w.pullTyped(pcA1, 0, 0)
	csf, sf, ok := learnedOf(t, p, "sf")
	if !ok || sf.UndoneAt == nil || sf.Term != "ECF" {
		t.Fatalf("o desfeito depois de um ano: %+v %v, quero a linha com undone_at", sf, ok)
	}
	cpq, pq, ok := learnedOf(t, p, "pq")
	if !ok || pq.UndoneAt != nil {
		t.Fatalf("o aprendido depois de um ano: %+v %v", pq, ok)
	}
	for _, c := range []typedChangeJSON{csf, cpq} {
		if c.DeviceID == nil || *c.DeviceID != w.devA1 {
			t.Fatalf("o aparelho de origem de %s = %v, quero o PC 1 (o evento do last_seq fica)", c.EntityID, c.DeviceID)
		}
	}
	if r, _ := w.pushLearned(pcA2, aprender("sf", "ECF", 0)); r.Status != "conflict" || r.State == nil || r.State.UndoneAt == nil {
		t.Fatalf("o par desfeito mandado de novo com a base 0 = %+v, quero conflict com o desfeito", r)
	}
	w.wantSeqs(w.userA, 2, 3)
}

// O seq sem buraco nem repetição com o job e o push do mesmo usuário juntos.
func testRetencaoSeqComOPushJunto(t *testing.T, w *world) {
	const n = 20
	ids := make([]string, n)
	for i := range ids {
		ids[i] = newID()
		w.mustCreate(pcA1, ids[i], fmt.Sprintf("ditado %d", i))
		if r := w.pushOne(pcA1, paraALixeira(ids[i], 1, now0)); r.Status != "applied" {
			t.Fatalf("lixeira: %+v", r)
		}
	}
	w.andar(now0.AddDate(0, 0, 30))
	var wg sync.WaitGroup
	var retido cloudstore.Retido
	var reterErr error
	falhas := make(chan string, 2*n)
	wg.Add(1)
	go func() {
		defer wg.Done()
		retido, reterErr = w.b.Store.Reter(context.Background(), w.userA, w.clk.Now())
	}()
	criado := w.clk.Now().Add(-time.Hour)
	for _, pc := range []string{pcA1, pcA2} {
		wg.Add(1)
		go func(pc string) {
			defer wg.Done()
			for i := 0; i < n/2; i++ {
				rec := w.pushRawNoFatal(pc, createWith(newID(), ditadoEm("junto com o job", criado)))
				p := decodeNoFatalPush(rec)
				if rec.Code != http.StatusOK || len(p.Results) != 1 || p.Results[0].Status != "applied" {
					falhas <- fmt.Sprintf("%d %s", rec.Code, rec.Body.String())
				}
			}
		}(pc)
	}
	wg.Wait()
	close(falhas)
	for f := range falhas {
		t.Errorf("push junto com o job: %s", f)
	}
	if reterErr != nil || retido != (cloudstore.Retido{Lixeira: n, LapidesCriadas: n}) {
		t.Fatalf("Reter junto com o push = %+v, %v", retido, reterErr)
	}
	// 2n do começo, n lápides e n ditados novos.
	requireContiguous(t, w.seqsOf(w.userA), 4*n)
	p := w.pull(pcA1, 0, 0)
	lapides := 0
	for _, c := range p.Changes {
		if c.Operation == "tombstone" {
			lapides++
		}
	}
	if lapides != n || len(p.Changes) != 2*n {
		t.Fatalf("o pull: %d mudanças, %d lápides; quero %d e %d", len(p.Changes), lapides, 2*n, n)
	}
}

// O Reter com o ctx cancelado: antes de começar, não faz nada; no meio, para
// entre um item e outro, sem deixar meia-purga, e a rodada seguinte termina.
func testRetencaoCtxCancelado(t *testing.T, w *world) {
	x, y := newID(), newID()
	if r := w.pushOne(pcA1, createWith(x, ditadoEm("primeiro", now0.Add(-2*time.Hour)))); r.Status != "applied" {
		t.Fatalf("criar: %+v", r) // seq 1
	}
	w.mustCreate(pcA1, y, "segundo") // seq 2
	for _, id := range []string{x, y} {
		if r := w.pushOne(pcA1, paraALixeira(id, 1, now0)); r.Status != "applied" {
			t.Fatalf("lixeira: %+v", r) // seq 3 e 4
		}
	}
	w.andar(now0.AddDate(0, 0, 30))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := w.b.Store.Reter(ctx, w.userA, w.clk.Now()); !errors.Is(err, context.Canceled) || r != (cloudstore.Retido{}) {
		t.Fatalf("Reter com o ctx cancelado = %+v, %v; quero nada e context.Canceled", r, err)
	}
	for _, id := range []string{x, y} {
		if it, ok := w.existe(w.userA, id); !ok || it.DeletedAt == nil {
			t.Fatalf("%s depois do Reter cancelado: %+v %v", id, it, ok)
		}
	}
	requireContiguous(t, w.seqsOf(w.userA), 4)

	if w.b.AntesDaTrava == nil {
		return
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	w.b.AntesDaTrava(t, func(id string) {
		if id == y {
			cancel()
		}
	})
	r, err := w.b.Store.Reter(ctx, w.userA, w.clk.Now())
	if !errors.Is(err, context.Canceled) || r != (cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}) {
		t.Fatalf("Reter cancelado no meio = %+v, %v; quero o primeiro item e context.Canceled", r, err)
	}
	if _, ok := w.existe(w.userA, x); ok {
		t.Fatal("o primeiro item ficou")
	}
	if seq, ok := w.lapideNoPull(pcA1, x); !ok || seq != 5 {
		t.Fatalf("a lápide do primeiro: %d %v, quero o seq 5", seq, ok)
	}
	if it, ok := w.existe(w.userA, y); !ok || it.DeletedAt == nil || it.Version != 2 {
		t.Fatalf("o segundo depois do cancelamento: %+v %v, quero inteiro, na lixeira, v2", it, ok)
	}
	requireContiguous(t, w.seqsOf(w.userA), 5)

	w.b.AntesDaTrava(t, nil)
	w.wantRetido(w.userA, cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1})
	requireContiguous(t, w.seqsOf(w.userA), 6)
}
