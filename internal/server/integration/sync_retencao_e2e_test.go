package integration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A retenção da nuvem de ponta a ponta (F9d; ADR-019, "Decisões da F9"): o
// job de verdade (Cloud.Reter, o que o serve roda de hora em hora), o Motor
// do PC de verdade (SQLite na pasta temporária, httptest), e o relógio da
// nuvem e do Motor andando meses. Nenhuma linha do PC muda: o PC já trata o
// gone apagado (apaga a cópia) e o fora_da_janela (mantém a cópia e para de
// sincronizá-la).

const dia = 24 * time.Hour

// reter roda uma rodada do job e devolve a linha "retenção" dela.
func (n *nuvemE2E) reter() string {
	n.t.Helper()
	n.cloud.Reter(context.Background())
	linhas := strings.Split(strings.TrimSpace(n.log.String()), "\n")
	for i := len(linhas) - 1; i >= 0; i-- {
		if strings.Contains(linhas[i], "retenção") && strings.Contains(linhas[i], " usuarios=") {
			return linhas[i] + " "
		}
	}
	n.t.Fatalf("o job não registrou a linha \"retenção\":\n%s", n.log.String())
	return ""
}

func querNaLinha(t *testing.T, linha string, campos ...string) {
	t.Helper()
	for _, c := range campos {
		if !strings.Contains(linha, " "+c+" ") {
			t.Errorf("a linha da retenção não tem %s: %s", c, linha)
		}
	}
}

// andar leva o relógio da nuvem e dos PCs adiante, em passos de até 60 dias,
// com passo() em cada um (as rodadas dos PCs que continuam ligados).
func (n *nuvemE2E) andar(quanto time.Duration, passo func()) {
	n.t.Helper()
	for quanto > 0 {
		d := min(quanto, 60*dia)
		n.relogio.andar(d)
		quanto -= d
		passo()
	}
}

// manterToken usa o token do PC por fora, sem rodada: um PC de verdade que
// fica 180 dias sem falar com a nuvem sai e precisa conectar de novo (F6). Aqui
// o PC parado continua conectado para o caso provar a lápide vencida, e não a
// vida do token.
func (n *nuvemE2E) manterToken(pc *pcE2E) {
	n.t.Helper()
	if _, err := n.mem.ResolverTokenDeAparelho(context.Background(), cloudauth.HMAC(n.pepper, pc.token), n.relogio.agora()); err != nil {
		n.t.Fatalf("o token do PC parado venceu: %v", err)
	}
}

// O PC parado com uma edição pendente num ditado que o celular pôs na lixeira:
// com a lápide viva (31 dias), recebe gone apagado e apaga a cópia; com a
// lápide vencida (6 meses e um dia), mantém a cópia, marca fora_da_janela, e
// nada volta à nuvem. O ditado ativo que a nuvem apagou aos 6 meses (sem
// lápide) fica nos PCs, e a edição dele também não volta.
func TestSyncE2E_Retencao_PCParadoEOAtivoQueANuvemApagou(t *testing.T) {
	n := novaNuvemE2EComRelogio(t)
	pc1, pc2, pc3 := n.novoPC(), n.novoPC(), n.novoPC()
	lixo := pc1.ditar("vai para a lixeira no celular")
	ativo := pc1.ditar("fica no histórico")
	criado := pc1.linha(ativo).CreatedAt
	pc1.rodadaOK()
	for _, pc := range []*pcE2E{pc2, pc3} {
		pc.rodadaOK()
		if !pc.temLinha(lixo) || !pc.temLinha(ativo) {
			t.Fatal("o PC não recebeu os ditados")
		}
		// O PC para, com uma edição pendente.
		pc.editar(lixo, "editado no PC parado")
	}
	n.celLixeira(lixo)
	pc1.rodadaOK()

	// 30 dias e uma hora depois: a lixeira vence, com lápide.
	n.andar(30*dia+time.Hour, func() { pc1.rodadaOK() })
	querNaLinha(t, n.reter(), "usuarios=1", "lixeira=1", "lapides_criadas=1", "ativos=0", "falhas=0")
	pc1.rodadaOK()
	if pc1.temLinha(lixo) {
		t.Fatal("a lápide não apagou a cópia do PC 1")
	}

	// O PC 2 volta com a lápide viva: o push da edição pendente dá gone
	// apagado, e ele apaga a cópia.
	pc2.rodadaOK()
	if pc2.temLinha(lixo) || pc2.naFila() != 0 {
		t.Fatalf("o PC 2 com a lápide viva: linha %v, fila %d; quero apagado e sem fila", pc2.temLinha(lixo), pc2.naFila())
	}

	// Até os 6 meses do ditado e mais um dia (a lápide vence), com o PC 1 e o
	// PC 2 ligados e o PC 3 parado.
	ate := criado.AddDate(0, 6, 0).Add(cloudstore.FolgaDaLapide + time.Hour).Sub(n.relogio.agora())
	n.andar(ate, func() {
		pc1.rodadaOK()
		pc2.rodadaOK()
		n.manterToken(pc3)
	})
	querNaLinha(t, n.reter(), "ativos=1", "lapides_vencidas=1", "falhas=0")
	for _, id := range []string{lixo, ativo} {
		if _, err := n.mem.Get(context.Background(), n.conta, id); !errors.Is(err, cloudstore.ErrNotFound) {
			t.Fatalf("%s continua na nuvem: %v", id, err)
		}
	}

	// O ativo que a nuvem apagou fica nos PCs ligados: não há lápide.
	pc1.rodadaOK()
	pc2.rodadaOK()
	if !pc1.temLinha(ativo) || !pc2.temLinha(ativo) {
		t.Fatal("o ativo que a nuvem apagou aos 6 meses sumiu de um PC (sem lápide, o PC o mantém até a F10)")
	}
	// Uma edição dele não volta à nuvem: fora_da_janela.
	pc1.editar(ativo, "editado depois dos 6 meses")
	pc1.rodadaOK()
	if l := pc1.linha(ativo); l.SyncState != storage.SituacaoForaDaJanela || pc1.naFila() != 0 {
		t.Fatalf("o ativo editado depois dos 6 meses: situação %q, fila %d; quero fora_da_janela e sem fila", l.SyncState, pc1.naFila())
	}

	// O PC 3 volta com a lápide vencida: o push dá fora_da_janela, ele mantém
	// a cópia (com a edição) e para de sincronizá-la. E continua do cursor
	// dele (D2: a nuvem não responde 410 no mesmo epoch): nenhuma releitura
	// do 0. Com um 410, o Motor zeraria o cursor e releria do 0, e a rodada
	// voltaria sem erro, com o mesmo estado; só os pedidos mostram.
	n.porteiro.zerar()
	pc3.rodadaOK()
	pedidos, _ := n.porteiro.visto()
	puxou := false
	for _, p := range pedidos {
		if p == "pull:0" {
			t.Fatalf("o PC 3 releu do cursor 0 (um 410 no mesmo epoch?): %v", pedidos)
		}
		puxou = puxou || strings.HasPrefix(p, "pull:")
	}
	if !puxou {
		t.Fatalf("o PC 3 não puxou: %v", pedidos)
	}
	if strings.Contains(pc3.log.String(), "releituras=1") {
		t.Fatalf("o PC 3 contou uma releitura:\n%s", pc3.log.String())
	}
	l := pc3.linha(lixo)
	if l.SyncState != storage.SituacaoForaDaJanela || l.FinalText != "editado no PC parado" || pc3.naFila() != 0 {
		t.Fatalf("o PC 3 com a lápide vencida: %q, situação %q, fila %d; quero a cópia, fora_da_janela e sem fila",
			l.FinalText, l.SyncState, pc3.naFila())
	}
	if !pc3.temLinha(ativo) {
		t.Fatal("o ativo sumiu do PC 3")
	}
	// E nada voltou à nuvem.
	for _, id := range []string{lixo, ativo} {
		if _, err := n.mem.Get(context.Background(), n.conta, id); !errors.Is(err, cloudstore.ErrNotFound) {
			t.Fatalf("%s voltou à nuvem: %v", id, err)
		}
	}
	querNaLinha(t, n.reter(), "ativos=0", "lixeira=0", "lapides_criadas=0", "lapides_vencidas=0", "falhas=0")
}
