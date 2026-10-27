package syncengine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O motor com as notas e os marcadores de nota (N7), contra o SQLite de
// verdade e a nuvem de roteiro: a capacidade (nada sobe antes dela; na
// primeira vez, o cursor volta a 0), a ordem do lote (o marcador antes da
// nota, os dois antes do ditado), a lápide e a resposta aplicada na mesma
// transação.

func notasDoPC(p pcDeTeste) *storage.SQLiteNotaRepository {
	return storage.NewSQLiteNotaRepository(p.db)
}

// comNotas responde a página do pull dizendo que a nuvem sabe de notas.
func comNotas(epoch string) func(int64) (ChangePage, error) {
	return func(after int64) (ChangePage, error) {
		return ChangePage{LastSeq: after, Epoch: epoch, Entidades: []string{"transcription", "learned", "note", "label"}}, nil
	}
}

func tiposDoPush(muts []Mutation) []string {
	var out []string
	for _, m := range muts {
		out = append(out, m.EntityType+":"+m.Operation)
	}
	return out
}

func TestMotor_NotasSoSobemComACapacidade(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	notas := notasDoPC(p)
	mk, err := notas.CriarMarcador(ctx, donoDoMotor, "Mercado")
	if err != nil {
		t.Fatal(err)
	}
	n, err := notas.Criar(ctx, storage.NovaNota{UserID: donoDoMotor, Titulo: "Compras", Corpo: "- [ ] leite", Marcadores: []string{mk.ID}})
	if err != nil {
		t.Fatal(err)
	}
	p.ditar(t, "0199a000-0000-7000-8000-0000000000d1", "um ditado")

	nuvem := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
	m := novoMotor(p, nuvem)
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	for _, lote := range nuvem.pushes {
		for _, mu := range lote {
			if mu.EntityType != storage.EntidadeDitado {
				t.Fatalf("subiu %s antes da capacidade", mu.EntityType)
			}
		}
	}

	// A nuvem passa a saber de notas: a rodada lê tudo de novo (o cursor
	// volta a 0) e sobe o marcador antes da nota.
	nuvem.mu.Lock()
	nuvem.pushes, nuvem.pull = nil, comNotas("e1")
	nuvem.mu.Unlock()
	rel, err := m.Rodada(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rel.CapacidadeNova {
		t.Errorf("a rodada não viu a capacidade nova: %+v", rel)
	}
	if len(nuvem.pushes) != 1 {
		t.Fatalf("pushes: %v", nuvem.pushes)
	}
	got := tiposDoPush(nuvem.pushes[0])
	want := []string{"label:upsert", "note:upsert"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("o lote: %v, quero %v", got, want)
	}
	var corpo map[string]any
	if err := json.Unmarshal(nuvem.pushes[0][1].Payload, &corpo); err != nil {
		t.Fatal(err)
	}
	if corpo["titulo"] != "Compras" || corpo["posicao"] == "" {
		t.Errorf("o payload de criar da nota: %v", corpo)
	}
	if ms, _ := corpo["marcadores"].([]any); len(ms) != 1 || ms[0] != mk.ID {
		t.Errorf("os marcadores no payload: %v", corpo["marcadores"])
	}
	// A resposta aplicada grava a versão da nuvem e o corpo confirmado.
	depois, err := notas.Get(ctx, donoDoMotor, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if depois.ServerVersion == nil || *depois.ServerVersion != 1 {
		t.Errorf("server_version da nota: %v", depois.ServerVersion)
	}
	if depois.CorpoNuvem == nil || *depois.CorpoNuvem != "- [ ] leite" {
		t.Errorf("corpo_nuvem: %v", depois.CorpoNuvem)
	}
	ms, err := p.fila.Marcadores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("a fila não esvaziou: %+v", ms)
	}
}

func TestMotor_NotaMudadaLevaSoOsCamposEApagarDeVezSobeComoDelete(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	notas := notasDoPC(p)
	n, err := notas.Criar(ctx, storage.NovaNota{UserID: donoDoMotor, Corpo: "- [ ] a\n- [ ] b"})
	if err != nil {
		t.Fatal(err)
	}
	nuvem := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: comNotas("e1")}
	m := novoMotor(p, nuvem)
	for i := 0; i < 2; i++ { // a primeira vê a capacidade e sobe tudo
		if _, err := m.Rodada(ctx); err != nil {
			t.Fatal(err)
		}
	}
	n, _ = notas.Get(ctx, donoDoMotor, n.ID)
	corpo := "- [x] a\n- [ ] b"
	if _, err := notas.Atualizar(ctx, donoDoMotor, n.ID, n.Version, storage.MudancaDaNota{Corpo: &corpo}); err != nil {
		t.Fatal(err)
	}
	nuvem.mu.Lock()
	nuvem.pushes = nil
	nuvem.push = func(muts []Mutation) (PushResult, error) {
		r := PushResult{Epoch: "e1"}
		for _, mu := range muts {
			r.Results = append(r.Results, MutationResult{MutationID: mu.MutationID, Status: StatusApplied, EntityVersion: 2})
		}
		return r, nil
	}
	nuvem.mu.Unlock()
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if len(nuvem.pushes) != 1 || len(nuvem.pushes[0]) != 1 {
		t.Fatalf("pushes: %v", nuvem.pushes)
	}
	mu := nuvem.pushes[0][0]
	var p1 map[string]any
	_ = json.Unmarshal(mu.Payload, &p1)
	if mu.BaseVersion != 1 || p1["corpo"] != corpo || p1["corpo_base"] != "- [ ] a\n- [ ] b" || p1["cor"] != nil && len(p1) > 3 {
		t.Errorf("a mudança: base %d, payload %v", mu.BaseVersion, p1)
	}
	if _, temCor := p1["cor"]; temCor {
		t.Errorf("a mudança do corpo levou a cor: %v", p1)
	}

	// Lixeira e apagar de vez: a última mutação da nota é o delete.
	if err := notas.SoftDelete(ctx, donoDoMotor, n.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := notas.ApagarDeVez(ctx, donoDoMotor, n.ID); err != nil {
		t.Fatal(err)
	}
	nuvem.mu.Lock()
	nuvem.pushes = nil
	nuvem.mu.Unlock()
	if _, err := m.Rodada(ctx); err != nil {
		t.Fatal(err)
	}
	if len(nuvem.pushes) != 1 || len(nuvem.pushes[0]) != 1 || nuvem.pushes[0][0].Operation != OpDelete ||
		nuvem.pushes[0][0].Payload != nil {
		t.Fatalf("a lápide: %v", nuvem.pushes)
	}
	ms, _ := p.fila.Marcadores(ctx)
	if len(ms) != 0 {
		t.Errorf("a lápide ficou na fila depois do applied: %+v", ms)
	}
}

func TestMotor_PullDeNotaComMarcadorQueAindaNaoChegouEDepoisChega(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	notas := notasDoPC(p)
	notaID := "0199a000-0000-7000-8000-00000000aaa1"
	marcadorID := "0199a000-0000-7000-8000-00000000bbb1"
	agora := time.Now().UTC().Truncate(time.Second)
	nuvem := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: func(after int64) (ChangePage, error) {
		pg := ChangePage{LastSeq: after, Epoch: "e1", Entidades: []string{"note", "label"}}
		if after == 0 {
			// A nota primeiro (seq 1), o marcador numa página seguinte (seq 2).
			pg.Changes = []Change{{Seq: 1, EntityType: "note", EntityID: notaID, Operation: OpUpsert, EntityVersion: 3,
				PayloadNota: &Nota{ID: notaID, Titulo: "Da nuvem", Corpo: "x", Posicao: "V", Marcadores: []string{marcadorID},
					CreatedAt: agora, UpdatedAt: agora, Version: 3}}}
			pg.LastSeq, pg.HasMore = 1, true
		} else if after == 1 {
			pg.Changes = []Change{{Seq: 2, EntityType: "label", EntityID: marcadorID, Operation: OpUpsert, EntityVersion: 1,
				PayloadMarcador: &MarcadorDaNuvem{ID: marcadorID, Nome: "Casa", CreatedAt: agora, UpdatedAt: agora, Version: 1}}}
			pg.LastSeq = 2
		}
		return pg, nil
	}}
	m := novoMotor(p, nuvem)
	for i := 0; i < 2; i++ {
		if _, err := m.Rodada(ctx); err != nil {
			t.Fatal(err)
		}
	}
	n, err := notas.Get(ctx, donoDoMotor, notaID)
	if err != nil {
		t.Fatalf("a nota da nuvem não chegou: %v", err)
	}
	if n.Titulo != "Da nuvem" || len(n.Marcadores) != 1 || n.Marcadores[0] != marcadorID {
		t.Errorf("a nota: %+v", n)
	}
	ls, err := notas.ListarMarcadores(ctx, donoDoMotor)
	if err != nil || len(ls) != 1 || ls[0].Nome != "Casa" {
		t.Errorf("os marcadores: %+v, %v", ls, err)
	}
	// Aplicar não ecoa.
	ms, _ := p.fila.Marcadores(ctx)
	if len(ms) != 0 {
		t.Errorf("aplicar pôs marcador na fila: %+v", ms)
	}
}

func TestMotor_RecusaDeNuvemSemNotasNaoPrendeARodada(t *testing.T) {
	ctx := context.Background()
	p := novoPC(t)
	notas := notasDoPC(p)
	if _, err := notas.Criar(ctx, storage.NovaNota{UserID: donoDoMotor, Corpo: "a"}); err != nil {
		t.Fatal(err)
	}
	nuvem := &nuvemDeRoteiro{pull: comNotas("e1"), push: func(muts []Mutation) (PushResult, error) {
		r := PushResult{Epoch: "e1"}
		for _, mu := range muts {
			r.Results = append(r.Results, MutationResult{MutationID: mu.MutationID, Status: StatusRejected, Code: CodigoTipoDesconhecido})
		}
		return r, nil
	}}
	m := novoMotor(p, nuvem)
	for i := 0; i < 2; i++ {
		if _, err := m.Rodada(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(nuvem.pushes) > 2 {
		t.Errorf("a recusa do tipo repetiu o lote %d vezes", len(nuvem.pushes))
	}
	ms, _ := p.fila.Marcadores(ctx)
	if len(ms) != 1 || ms[0].EntityType != storage.EntidadeNota || ms[0].EmVoo() {
		t.Errorf("o marcador da nota devia ficar, fora de voo: %+v", ms)
	}
}
