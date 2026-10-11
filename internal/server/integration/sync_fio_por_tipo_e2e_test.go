package integration_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// O fio por tipo (ADR-019, "Decisões da F8", F8f1) contra a nuvem de verdade,
// com o cliente de verdade do PC: o aprendido sobe (o upsert, e o delete sem
// payload), o state do conflict dele volta no Aprendido e o do ditado no
// Item, no mesmo lote, e o pull traz o par desfeito no Aprendido, ao lado do
// ditado no Item. O motor ainda não manda nem aplica o aprendido (F8f2 a
// F8f4): os lotes daqui são montados à mão, e a rodada do PC continua a da F7
// (o aprendido "não aplicado", e o cursor anda).
func TestSyncE2E_OFioPorTipo(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	c1 := syncengine.NovoCliente(n.base, pc1.token, nil)
	c2 := syncengine.NovoCliente(n.base, pc2.token, nil)
	ctx := context.Background()

	// O ditado, pelo caminho de sempre; o celular o edita (a versão 2).
	d := pc1.ditar("um ditado")
	pc1.rodadaOK()
	n.celEditar(d, "editado no celular")

	nfe, sf := dictionary.IDDoPar("nfe"), dictionary.IDDoPar("sf")
	fiscal := "fiscal"
	aprender := func(termo string, base int64) syncengine.Mutation {
		payload, err := json.Marshal(map[string]any{"variant": "nfe", "term": termo, "restricted": true, "domain": fiscal})
		if err != nil {
			t.Fatal(err)
		}
		return syncengine.Mutation{MutationID: cloudstore.NewUUIDv7(), EntityType: storage.EntidadeAprendido,
			EntityID: nfe, Operation: syncengine.OpUpsert, BaseVersion: base, Payload: payload}
	}
	desfazer := func(id string, base int64) syncengine.Mutation {
		return syncengine.Mutation{MutationID: cloudstore.NewUUIDv7(), EntityType: storage.EntidadeAprendido,
			EntityID: id, Operation: syncengine.OpDelete, BaseVersion: base}
	}

	res, err := c1.Push(ctx, []syncengine.Mutation{aprender("NF-e", 0)})
	if err != nil || res.Results[0].Status != syncengine.StatusApplied || res.Results[0].EntityVersion != 1 ||
		res.Results[0].State != nil || res.Results[0].StateAprendido != nil {
		t.Fatalf("o aprender do PC1: %+v, %v", res, err)
	}

	// O PC2 conflita nos dois, num lote só: o ditado (texto contra texto) e
	// o aprendido (outro termo, com a base 0). Cada state no tipo da mutação
	// do mesmo lugar.
	res, err = c2.Push(ctx, []syncengine.Mutation{
		{MutationID: cloudstore.NewUUIDv7(), EntityType: storage.EntidadeDitado, EntityID: d,
			Operation: syncengine.OpUpsert, BaseVersion: 1, Payload: json.RawMessage(`{"final_text":"editado no PC 2"}`)},
		aprender("Outro termo", 0),
	})
	if err != nil || len(res.Results) != 2 {
		t.Fatalf("o push do PC2: %+v, %v", res, err)
	}
	if r := res.Results[0]; r.Status != syncengine.StatusConflict || r.State == nil || r.State.ID != d ||
		r.State.FinalText != "editado no celular" || r.State.VersaoEmConflito == nil ||
		r.State.VersaoEmConflito.FinalText != "editado no PC 2" || r.StateAprendido != nil {
		t.Errorf("o conflict do ditado: %+v (state %+v)", r, r.State)
	}
	quero := syncengine.Aprendido{ID: nfe, Variant: "nfe", Term: "NF-e", Restricted: true, Domain: &fiscal}
	if r := res.Results[1]; r.Status != syncengine.StatusConflict || r.EntityVersion != 1 || r.State != nil ||
		r.StateAprendido == nil || !reflect.DeepEqual(*r.StateAprendido, quero) {
		t.Errorf("o conflict do aprendido: %+v (state %+v)", r, r.StateAprendido)
	}

	// O desfazer: o delete sem payload, e o de um par que a nuvem nunca teve.
	res, err = c1.Push(ctx, []syncengine.Mutation{desfazer(nfe, 1), desfazer(sf, 0)})
	if err != nil || len(res.Results) != 2 {
		t.Fatalf("o desfazer: %+v, %v", res, err)
	}
	if r := res.Results[0]; r.Status != syncengine.StatusApplied || r.EntityVersion != 2 || r.Mesclado {
		t.Errorf("o delete: %+v", r)
	}
	if r := res.Results[1]; r.Status != syncengine.StatusRejected || r.Code != syncengine.CodigoNadaADesfazer {
		t.Errorf("o delete do que a nuvem nunca teve: %+v", r)
	}

	// O pull do PC2 (o cliente de verdade, do cursor 0): o ditado no Item e o
	// par desfeito no Aprendido; o par que a nuvem nunca teve não aparece.
	tudo := n.tudoNaConta(pc2.token)
	if len(tudo) != 2 {
		t.Fatalf("o pull trouxe %d entidades, quero o ditado e o par: %+v", len(tudo), tudo)
	}
	if ch := tudo[d]; ch.EntityType != storage.EntidadeDitado || ch.Payload == nil || ch.PayloadAprendido != nil ||
		ch.Payload.FinalText != "editado no celular" {
		t.Errorf("o ditado do pull: %+v", ch)
	}
	ch := tudo[nfe]
	if ch.EntityType != storage.EntidadeAprendido || ch.Operation != syncengine.OpUpsert || ch.EntityVersion != 2 ||
		ch.DeviceID == nil || *ch.DeviceID != pc1.aparelho || ch.Payload != nil || ch.PayloadAprendido == nil {
		t.Fatalf("o aprendido do pull: %+v", ch)
	}
	par := *ch.PayloadAprendido
	if par.UndoneAt == nil {
		t.Errorf("o par desfeito chegou sem undone_at: %+v", par)
	}
	par.UndoneAt = nil
	if !reflect.DeepEqual(par, quero) {
		t.Errorf("o par do pull = %+v; quero %+v", par, quero)
	}

	// A rodada do PC2 continua a da F7: o ditado chega, o aprendido conta
	// como "não aplicado", e nada sobe.
	pc2.rodadaOK()
	if l := pc2.linha(d); l.FinalText != "editado no celular" || l.Conflito == nil || l.Conflito.FinalText != "editado no PC 2" {
		t.Errorf("o ditado no PC2: %+v", l)
	}
	if pc2.naFila() != 0 {
		t.Errorf("a rodada do PC2 pôs %d na fila", pc2.naFila())
	}
}
