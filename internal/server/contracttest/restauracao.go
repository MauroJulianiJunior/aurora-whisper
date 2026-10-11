package contracttest

import (
	"context"
	"net/http"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Depois de restaurar um backup (ADR-019, "Decisões da F11", F11d;
// cloudstore.Restaurador), contra o Store em memória e o Postgres. O backup é
// MAIS VELHO que o estado da nuvem: depois dele, o celular edita, põe na
// lixeira e exclui de vez, o PC cria um ditado novo, um aparelho é revogado e
// outro é pareado. Restaurar volta o banco ao backup; o DepoisDeRestaurar
// troca o epoch de todas as contas e revoga todas as sessões e todos os
// aparelhos.
//
// O caso afirma o que tem de valer e o que o Mauro aceitou em 09-out-2026
// ("aceitar e deixar escrito"): o excluído de vez depois do backup volta
// vivo no pull do 0, e a lixeira feita depois do backup volta fora da
// lixeira. Nada é escondido.

func testRestauracaoBackupMaisVelho(t *testing.T, w *world) {
	if w.b.Fotografar == nil {
		t.Skip("o Backend não fotografa o banco")
	}
	rest, ok := w.b.Store.(cloudstore.Restaurador)
	if !ok {
		t.Fatal("o Store não é um cloudstore.Restaurador")
	}
	ctx := context.Background()
	editado, lixo, excluido, intocado, novo := newID(), newID(), newID(), newID(), newID()
	for _, id := range []string{editado, lixo, excluido, intocado} {
		w.mustCreate(pcA1, id, "antes do backup: "+id[:8])
	}
	w.mustCreate(pcB, newID(), "da outra conta")
	antes := w.pull(pcA1, 0, 0)
	epochA, epochB := antes.SyncEpoch, w.pull(pcB, 0, 0).SyncEpoch

	restaurar := w.b.Fotografar(t)

	// Depois do backup.
	if rec := w.edit(celA, editado, 1, "editado depois do backup"); rec.Code != http.StatusOK {
		t.Fatalf("editar: %d %s", rec.Code, rec.Body.String())
	}
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+lixo, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("lixeira: %d %s", rec.Code, rec.Body.String())
	}
	if rec := w.do(http.MethodDelete, "/api/v1/transcriptions/"+excluido, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("lixeira do excluído: %d %s", rec.Code, rec.Body.String())
	}
	if rec := w.do(http.MethodPost, "/api/v1/transcriptions/"+excluido+"/purge", celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("excluir de vez: %d %s", rec.Code, rec.Body.String())
	}
	w.mustCreate(pcA1, novo, "depois do backup: novo")
	if rec := w.do(http.MethodDelete, "/api/v1/devices/"+w.devA2, celA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revogar o PC A2: %d %s", rec.Code, rec.Body.String())
	}
	const pareadoDepois = "pc-a3-pareado-depois-do-backup"
	w.newDevice(w.userA, pareadoDepois)
	if rec := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", pcA2, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("o PC revogado depois do backup, antes de restaurar: %d", rec.Code)
	}

	restaurar(t)

	// O risco que o DepoisDeRestaurar fecha: o banco restaurado tem o
	// revoked_at antigo, e o aparelho revogado depois do backup volta a valer.
	if rec := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", pcA2, nil); rec.Code != http.StatusOK {
		t.Fatalf("restaurado, o PC revogado depois do backup = %d; o caso supõe que ele volta a valer", rec.Code)
	}
	if p := w.pull(pcA1, 0, 0); p.SyncEpoch != epochA {
		t.Fatalf("restaurado, o epoch mudou sozinho: %s -> %s", epochA, p.SyncEpoch)
	}

	r, err := rest.DepoisDeRestaurar(ctx, w.clk.Now())
	if err != nil {
		t.Fatalf("DepoisDeRestaurar: %v", err)
	}
	// O Backend é o mesmo para todos os casos: as contagens incluem as contas
	// dos casos anteriores. As deste: 2 contas, 2 sessões e os 3 aparelhos
	// (A1, A2 e B; o A3 não existe no backup).
	if r.Contas < 2 || r.Sessoes < 2 || r.Aparelhos < 3 {
		t.Errorf("as contagens = %+v; quero ao menos 2 contas, 2 sessões e 3 aparelhos", r)
	}

	// Todas as credenciais de antes recebem 401: os PCs, o pareado depois do
	// backup (que o backup nem tem) e as sessões.
	for _, pc := range []string{pcA1, pcA2, pcB, pareadoDepois} {
		if rec := w.do(http.MethodGet, "/api/v1/sync/pull?after_seq=0", pc, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("o pull de %s depois da restauração: %d, quero 401", pc, rec.Code)
		}
	}
	for _, cel := range []string{celA, celB} {
		if rec := w.do(http.MethodGet, "/api/v1/transcriptions", cel, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("a sessão %s depois da restauração: %d, quero 401", cel, rec.Code)
		}
	}
	// O DepoisDeRestaurar de novo não revoga nada (já estão), e troca o epoch
	// outra vez.
	if r2, err := rest.DepoisDeRestaurar(ctx, w.clk.Now()); err != nil || r2.Aparelhos != 0 || r2.Sessoes != 0 || r2.Contas != r.Contas {
		t.Errorf("a segunda vez = %+v, %v; quero as mesmas contas e nada a revogar", r2, err)
	}

	// O PC que conecta de novo (um aparelho novo, pelo pareamento) lê o epoch
	// novo e o estado do backup.
	const deNovo = "pc-a1-conectado-de-novo"
	w.newDevice(w.userA, deNovo)
	p := w.pull(deNovo, 0, 0)
	if p.SyncEpoch == "" || p.SyncEpoch == epochA {
		t.Fatalf("o epoch de A = %q; quero um novo (antes era %s)", p.SyncEpoch, epochA)
	}
	if _, ok := changeOf(p, novo); ok {
		t.Error("o ditado criado depois do backup está no banco restaurado")
	}
	for _, id := range []string{editado, lixo, excluido, intocado} {
		c, ok := changeOf(p, id)
		if !ok || c.Payload == nil {
			t.Errorf("%s não veio no pull do 0: %+v", id, c)
			continue
		}
		if c.Payload.FinalText != "antes do backup: "+id[:8] || c.Payload.DeletedAt != nil || c.EntityVersion != 1 {
			t.Errorf("%s no pull do 0: %q, lixeira %v, v%d; quero o estado do backup", id, c.Payload.FinalText,
				c.Payload.DeletedAt != nil, c.EntityVersion)
		}
	}
	// O aceito (09-out-2026), escrito: o excluído de vez depois do backup
	// volta vivo, sem lápide; a lixeira de depois do backup volta fora dela.
	if c, _ := changeOf(p, excluido); c.Operation == "delete" {
		t.Errorf("o excluído de vez depois do backup veio como lápide: %+v", c)
	}
	// B também ganhou o epoch novo, com os dados dela intactos.
	const bDeNovo = "pc-b-conectado-de-novo"
	w.newDevice(w.userB, bDeNovo)
	if pb := w.pull(bDeNovo, 0, 0); pb.SyncEpoch == "" || pb.SyncEpoch == epochB || len(pb.Changes) != 1 {
		t.Errorf("a conta B: epoch %q (antes %s), %d mudanças; quero epoch novo e o ditado dela", pb.SyncEpoch, epochB, len(pb.Changes))
	}
}
