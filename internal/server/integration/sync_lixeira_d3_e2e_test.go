package integration_test

import (
	"context"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// A D3 da F10 de ponta a ponta (ADR-019, "Decisões da F10"; a correção da D4
// da F9): a lixeira feita com o PC desconectado não sai de vez do PC aos 30
// dias, porque a nuvem ainda não soube dela. Na volta, o push a leva, a nuvem
// a guarda com o piso de 24 h (a D5 da F9), e só então o PC a expurga.
//
// A nuvem de verdade (o MemoryStore atrás do httptest), o Motor do PC de
// verdade (SQLite na pasta temporária), e o mesmo relógio para a nuvem e o
// Motor (novaNuvemE2EComRelogio). O expurgo do PC é o PurgeExpired que o
// ciclo do Agent chama, com o instante desse relógio.
func TestSyncE2E_D3_ALixeiraDesconectadaEsperaANuvem(t *testing.T) {
	ctx := context.Background()
	n := novaNuvemE2EComRelogio(t)
	pc := n.novoPC()
	id := pc.ditar("vai para a lixeira com o PC desconectado")
	pc.rodadaOK()
	if pc.linha(id).ServerVersion == nil {
		t.Fatal("o ditado não subiu")
	}

	// Desconectado, a lixeira de um ditado que a nuvem tem entra na fila (o
	// Desconectar mantém o server_version) e espera.
	pc.desconectar()
	if err := pc.repo.SoftDelete(ctx, pc.dono, id, n.relogio.agora()); err != nil {
		t.Fatal(err)
	}
	if pc.naFila() != 1 {
		t.Fatalf("a lixeira desconectada não entrou na fila: %d", pc.naFila())
	}

	// 31 dias depois: o prazo da lixeira venceu, e o expurgo do PC a mantém.
	n.relogio.andar(31 * dia)
	r, err := pc.repo.PurgeExpired(ctx, n.relogio.agora())
	if err != nil {
		t.Fatal(err)
	}
	if r.Apagados != 0 || r.EsperandoANuvem != 1 || !pc.temLinha(id) {
		t.Fatalf("aos 31 dias desconectado: %+v, a linha ficou %v; quero a lixeira esperando a nuvem", r, pc.temLinha(id))
	}
	if it, err := n.mem.Get(ctx, n.conta, id); err != nil || it.DeletedAt != nil {
		t.Fatalf("a nuvem soube da lixeira sem o PC conectado: %+v %v", it, err)
	}

	// A volta: o push leva a lixeira, applied, e o marcador sai.
	pc.reconectar()
	antes := n.relogio.agora()
	pc.rodadaOK()
	depois := n.relogio.agora()
	if pc.naFila() != 0 {
		t.Fatalf("o push não levou a lixeira: %d na fila\n%s", pc.naFila(), pc.log.String())
	}
	it, err := n.mem.Get(ctx, n.conta, id)
	if err != nil || it.DeletedAt == nil || it.PurgeAt == nil {
		t.Fatalf("a nuvem não tem a lixeira: %+v %v", it, err)
	}
	// O piso de 24 h da lixeira que chega (a D5 da F9): o deleted_at é o do
	// PC, de 31 dias atrás, e o vencimento conta da chegada.
	if p := *it.PurgeAt; p.Before(antes.Add(cloudstore.FolgaDaLapide)) || p.After(depois.Add(cloudstore.FolgaDaLapide)) {
		t.Fatalf("o purge_at da nuvem = %s; quero a chegada (%s a %s) mais 24 h", p, antes, depois)
	}

	// Só agora o PC a expurga.
	r, err = pc.repo.PurgeExpired(ctx, n.relogio.agora())
	if err != nil {
		t.Fatal(err)
	}
	if r.Apagados != 1 || r.EsperandoANuvem != 0 || pc.temLinha(id) {
		t.Fatalf("depois do push confirmado: %+v, a linha ficou %v; quero a lixeira expurgada", r, pc.temLinha(id))
	}
	// E a nuvem continua com ela, na lixeira, até o piso.
	if it, err := n.mem.Get(ctx, n.conta, id); err != nil || it.DeletedAt == nil {
		t.Fatalf("a lixeira sumiu da nuvem com o expurgo do PC: %+v %v", it, err)
	}
}
