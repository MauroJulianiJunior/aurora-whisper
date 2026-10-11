package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
)

// TestSync_MutationIDIdempotent — a base da idempotência (ver sync.go,
// comentário do arquivo): reenviar a MESMA mutation_id nunca cria um
// segundo registro nem reaplica o efeito. Isso é a garantia direta por
// trás da meta "sync confirmado perdido: 0" — um retry de rede depois
// de um "applied" já confirmado não pode virar dois registros.
func TestSync_MutationIDIdempotent(t *testing.T) {
	engine := server.NewSyncEngine()
	ctx := context.Background()

	mutation := server.Mutation{
		MutationID: "mut-1", EntityType: "transcription", EntityID: "ent-1",
		Operation: "upsert", BaseVersion: 0, Payload: json.RawMessage(`{"final_text":"primeira vez"}`),
	}

	first := engine.Push(ctx, "user-1", []server.Mutation{mutation})
	if len(first) != 1 || first[0].Status != server.MutationApplied || first[0].EntityVersion != 1 {
		t.Fatalf("primeiro push = %+v, esperava applied/version=1", first)
	}

	// Retry EXATO (mesma mutation_id, mesmo payload) — simula um
	// timeout de rede em que a resposta do primeiro push se perdeu mas
	// o servidor já tinha aplicado.
	second := engine.Push(ctx, "user-1", []server.Mutation{mutation})
	if len(second) != 1 || second[0].Status != server.MutationDuplicate {
		t.Fatalf("retry do mesmo mutation_id = %+v, esperava duplicate", second)
	}
	if second[0].EntityVersion != first[0].EntityVersion {
		t.Fatalf("duplicate devolveu entity_version diferente do applied original: %d != %d",
			second[0].EntityVersion, first[0].EntityVersion)
	}

	// Confirma, via Pull, que só existe UM evento — nunca dois
	// registros para a mesma mutation_id.
	pull := engine.Pull(ctx, "user-1", 0, 100)
	if len(pull.Changes) != 1 {
		t.Fatalf("eventos de sync = %d, esperava exatamente 1 (retry não pode duplicar)", len(pull.Changes))
	}
}

// TestSync_MutationIDIdempotent_HTTP é o mesmo cenário, mas batendo na
// rota HTTP POST /sync/push, para provar que a montagem (handler +
// engine) preserva a garantia, não só o motor isolado.
func TestSync_MutationIDIdempotent_HTTP(t *testing.T) {
	h := newHarness(t)

	push := map[string]any{
		"device_id": "dev-a",
		"mutations": []map[string]any{
			{"mutation_id": "mut-http-1", "entity_type": "dictionary_entry", "entity_id": "ent-1",
				"operation": "upsert", "base_version": 0, "payload": map[string]any{"canonical_form": "DCTFWeb"}},
		},
	}

	rec1 := h.do(http.MethodPost, "/api/v1/sync/push", h.tokenA, push)
	res1 := decodeBody[struct {
		Results []struct {
			MutationID string `json:"mutation_id"`
			Status     string `json:"status"`
		} `json:"results"`
	}](t, rec1)
	if len(res1.Results) != 1 || res1.Results[0].Status != "applied" {
		t.Fatalf("primeiro push HTTP = %+v, esperava applied", res1.Results)
	}

	rec2 := h.do(http.MethodPost, "/api/v1/sync/push", h.tokenA, push)
	res2 := decodeBody[struct {
		Results []struct {
			MutationID string `json:"mutation_id"`
			Status     string `json:"status"`
		} `json:"results"`
	}](t, rec2)
	if len(res2.Results) != 1 || res2.Results[0].Status != "duplicate" {
		t.Fatalf("retry via HTTP = %+v, esperava duplicate", res2.Results)
	}
}

// TestSync_StaleBaseVersionConflicts — base_version desatualizado
// devolve conflict, nunca aplica silenciosamente sobre uma versão que o
// cliente não viu.
func TestSync_StaleBaseVersionConflicts(t *testing.T) {
	engine := server.NewSyncEngine()
	ctx := context.Background()

	// v0 -> v1.
	first := engine.Push(ctx, "user-1", []server.Mutation{{
		MutationID: "m1", EntityType: "transcription", EntityID: "e1",
		Operation: "upsert", BaseVersion: 0, Payload: json.RawMessage(`{"v":1}`),
	}})
	if first[0].Status != server.MutationApplied {
		t.Fatalf("setup: %+v", first)
	}

	// Uma segunda mutation, com base_version=0 (desatualizado — já foi
	// para 1), tenta aplicar.
	stale := engine.Push(ctx, "user-1", []server.Mutation{{
		MutationID: "m2", EntityType: "transcription", EntityID: "e1",
		Operation: "upsert", BaseVersion: 0, Payload: json.RawMessage(`{"v":2}`),
	}})
	if stale[0].Status != server.MutationConflict {
		t.Fatalf("upsert com base_version desatualizado = %+v, esperava conflict", stale[0])
	}
	if stale[0].EntityVersion != 1 {
		t.Fatalf("conflict devolveu entity_version = %d, esperava a versão corrente (1)", stale[0].EntityVersion)
	}

	// A versão corrente continua 1 — o conflict não teve efeito.
	pull := engine.Pull(ctx, "user-1", 0, 100)
	if len(pull.Changes) != 1 || pull.Changes[0].EntityVersion != 1 {
		t.Fatalf("estado após conflict = %+v, esperava só o evento v1 original", pull.Changes)
	}
}

// TestSync_DeleteWinsOverStaleEdit — "delete aceito vence edit antiga"
// (docs/ai-spec/04-system-design.md, "Sync"), nos dois sentidos de
// ordem de chegada.
func TestSync_DeleteWinsOverStaleEdit(t *testing.T) {
	t.Run("delete chega depois de um edit mais novo, com o mesmo base_version original", func(t *testing.T) {
		engine := server.NewSyncEngine()
		ctx := context.Background()

		// Cria a entidade em v1.
		create := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "create", EntityType: "transcription", EntityID: "e1",
			Operation: "upsert", BaseVersion: 0, Payload: json.RawMessage(`{"v":1}`),
		}})
		if create[0].Status != server.MutationApplied || create[0].EntityVersion != 1 {
			t.Fatalf("setup create: %+v", create[0])
		}

		// Device X edita, offline, baseado em v1 -> aplica, vira v2.
		edit := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "edit-x", EntityType: "transcription", EntityID: "e1",
			Operation: "upsert", BaseVersion: 1, Payload: json.RawMessage(`{"v":"edit-x"}`),
		}})
		if edit[0].Status != server.MutationApplied || edit[0].EntityVersion != 2 {
			t.Fatalf("setup edit: %+v", edit[0])
		}

		// Device Y, que também partiu de v1 (não viu a edição de X),
		// manda um DELETE com base_version=1 — mais antigo que a
		// versão corrente (2), mas ainda assim deve VENCER.
		del := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "delete-y", EntityType: "transcription", EntityID: "e1",
			Operation: "delete", BaseVersion: 1,
		}})
		if del[0].Status != server.MutationApplied {
			t.Fatalf("delete com base_version antigo (vencendo edit) = %+v, esperava applied", del[0])
		}

		// Uma edição posterior que ainda tentar se basear em v1 (a
		// mesma versão que o delete usou) bate em conflict — o
		// tombstone não pode ser revivido por uma edit antiga.
		lateEdit := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "edit-late", EntityType: "transcription", EntityID: "e1",
			Operation: "upsert", BaseVersion: 1, Payload: json.RawMessage(`{"v":"tarde-demais"}`),
		}})
		if lateEdit[0].Status != server.MutationConflict {
			t.Fatalf("edit antiga depois do delete = %+v, esperava conflict (delete deve permanecer vencedor)", lateEdit[0])
		}
	})

	t.Run("delete aplica primeiro, edit antiga chega depois com o mesmo base_version", func(t *testing.T) {
		engine := server.NewSyncEngine()
		ctx := context.Background()

		create := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "create", EntityType: "transcription", EntityID: "e1",
			Operation: "upsert", BaseVersion: 0, Payload: json.RawMessage(`{"v":1}`),
		}})
		if create[0].Status != server.MutationApplied {
			t.Fatalf("setup: %+v", create[0])
		}

		del := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "delete-first", EntityType: "transcription", EntityID: "e1",
			Operation: "delete", BaseVersion: 1,
		}})
		if del[0].Status != server.MutationApplied {
			t.Fatalf("delete: %+v", del[0])
		}

		// Edit atrasada, baseada na mesma v1 que o delete usou — o
		// delete já bumpou a versão para 2, então essa edit está
		// desatualizada e não pode reviver a entidade.
		edit := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "edit-after-delete", EntityType: "transcription", EntityID: "e1",
			Operation: "upsert", BaseVersion: 1, Payload: json.RawMessage(`{"v":"reviver"}`),
		}})
		if edit[0].Status != server.MutationConflict {
			t.Fatalf("edit antiga após delete já aplicado = %+v, esperava conflict", edit[0])
		}
	})
}

// TestSync_DeleteOfUnknownEntityFromTheFutureConflicts — um delete cujo
// base_version é MAIOR que a versão corrente (delete "do futuro", o
// cliente viu uma versão que o servidor não tem) não é aceito
// silenciosamente.
func TestSync_DeleteFromFutureConflicts(t *testing.T) {
	engine := server.NewSyncEngine()
	ctx := context.Background()

	del := engine.Push(ctx, "user-1", []server.Mutation{{
		MutationID: "d1", EntityType: "transcription", EntityID: "never-existed",
		Operation: "delete", BaseVersion: 5,
	}})
	if del[0].Status != server.MutationConflict {
		t.Fatalf("delete do futuro (base_version=5 contra entidade inexistente, version=0) = %+v, esperava conflict", del[0])
	}
}

// TestSync_PushRejectsMalformedMutation — mutation malformada (sem os
// campos obrigatórios, ou operation inválida) é "rejected", nunca
// tratada como applied nem derruba o resto do batch.
func TestSync_PushRejectsMalformedMutation(t *testing.T) {
	engine := server.NewSyncEngine()
	ctx := context.Background()

	results := engine.Push(ctx, "user-1", []server.Mutation{
		{MutationID: "", EntityType: "transcription", EntityID: "e1", Operation: "upsert"},
		{MutationID: "m2", EntityType: "transcription", EntityID: "e1", Operation: "bogus"},
		{MutationID: "m3", EntityType: "transcription", EntityID: "e2", Operation: "upsert", BaseVersion: 0},
	})
	if results[0].Status != server.MutationRejected {
		t.Fatalf("mutation sem mutation_id = %+v, esperava rejected", results[0])
	}
	if results[1].Status != server.MutationRejected {
		t.Fatalf("mutation com operation inválida = %+v, esperava rejected", results[1])
	}
	if results[2].Status != server.MutationApplied {
		t.Fatalf("mutation válida no mesmo batch = %+v, esperava applied (mutations malformadas não derrubam o batch)", results[2])
	}
}

// TestSync_PullCursorMonotonic — o cursor de Pull não pula nem repete
// evento, mesmo paginando com limit pequeno, e last_seq/has_more
// refletem exatamente a posição.
func TestSync_PullCursorMonotonic(t *testing.T) {
	engine := server.NewSyncEngine()
	ctx := context.Background()

	const total = 7
	for i := 0; i < total; i++ {
		res := engine.Push(ctx, "user-1", []server.Mutation{{
			MutationID: "m" + string(rune('a'+i)), EntityType: "transcription",
			EntityID: "e" + string(rune('a'+i)), Operation: "upsert", BaseVersion: 0,
			Payload: json.RawMessage(`{}`),
		}})
		if res[0].Status != server.MutationApplied {
			t.Fatalf("setup mutation %d: %+v", i, res[0])
		}
	}

	var seen []int64
	after := int64(0)
	for {
		page := engine.Pull(ctx, "user-1", after, 3)
		for _, c := range page.Changes {
			seen = append(seen, c.Seq)
		}
		if !page.HasMore {
			if page.LastSeq != int64(total) {
				t.Fatalf("last_seq final = %d, esperava %d", page.LastSeq, total)
			}
			break
		}
		after = page.LastSeq
	}

	if len(seen) != total {
		t.Fatalf("total de eventos vistos = %d, esperava %d (seq não pode pular nem repetir): %v", len(seen), total, seen)
	}
	for i, seq := range seen {
		if seq != int64(i+1) {
			t.Fatalf("seq na posição %d = %d, esperava %d (sequência estrita, sem gaps nem repetição): %v", i, seq, i+1, seen)
		}
	}

	// Repetir a MESMA página (mesmo after_seq) devolve exatamente os
	// mesmos eventos — o cursor é determinístico, não avança sozinho.
	repeat := engine.Pull(ctx, "user-1", 0, 3)
	if len(repeat.Changes) != 3 || repeat.Changes[0].Seq != 1 || repeat.Changes[2].Seq != 3 {
		t.Fatalf("repetir Pull(after_seq=0, limit=3) = %+v, esperava sempre seq 1..3", repeat.Changes)
	}
}

// TestSync_PullCursorMonotonic_HTTP prova o mesmo via HTTP, incluindo
// que has_more some quando a última página é exata.
func TestSync_PullCursorMonotonic_HTTP(t *testing.T) {
	h := newHarness(t)

	push := map[string]any{
		"device_id": "dev-a",
		"mutations": []map[string]any{
			{"mutation_id": "hm1", "entity_type": "transcription", "entity_id": "e1", "operation": "upsert", "base_version": 0, "payload": map[string]any{}},
			{"mutation_id": "hm2", "entity_type": "transcription", "entity_id": "e2", "operation": "upsert", "base_version": 0, "payload": map[string]any{}},
		},
	}
	if rec := h.do(http.MethodPost, "/api/v1/sync/push", h.tokenA, push); rec.Code != http.StatusOK {
		t.Fatalf("push: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0&limit=500", h.tokenA, nil)
	pull := decodeBody[struct {
		Changes []struct {
			Seq int64 `json:"seq"`
		} `json:"changes"`
		LastSeq int64 `json:"last_seq"`
		HasMore bool  `json:"has_more"`
	}](t, rec)
	if len(pull.Changes) != 2 || pull.Changes[0].Seq != 1 || pull.Changes[1].Seq != 2 {
		t.Fatalf("pull HTTP changes = %+v", pull.Changes)
	}
	if pull.LastSeq != 2 || pull.HasMore {
		t.Fatalf("pull HTTP last_seq/has_more = %d/%v, esperava 2/false", pull.LastSeq, pull.HasMore)
	}
}
