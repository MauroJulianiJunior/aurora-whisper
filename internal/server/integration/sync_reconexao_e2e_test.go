package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A volta de um PC que ficou fora (ADR-019, "Decisões da F7a/b", decisão 2,
// e a revisão de 06-out-2026): reconectar, ou receber 410, no mesmo epoch NÃO
// reenvia o histórico com a base vazia. As versões da nuvem que o PC guardou
// continuam valendo; sobe só o que mudou aqui enquanto ele estava fora, com a
// base certa, e o pull do cursor traz o resto.
//
// Cada teste é uma das sondas da revisão, que derrubavam o desenho anterior
// (o recomeço a cada reconexão):
//   - A e D: reconectar (ou o 410) sem mexer em nada criava um selo com o
//     texto velho deste PC em todo ditado que outro aparelho editou;
//   - B: a edição feita desconectado num ditado de OUTRO aparelho virava selo,
//     sem conflito nenhum;
//   - C: a lixeira antiga reenviada desfazia um restaurar mais novo;
//   - E e F: o PC desatualizado trocava o selo de verdade (a edição perdedora
//     de outro PC) pelo texto velho dele, e a edição sumia de todos.

// desconectar é o "Desconectar" do painel: revoga este aparelho na nuvem, e o
// token e o aparelho saem daqui. A conta e a URL ficam (a D3).
func (pc *pcE2E) desconectar() {
	pc.t.Helper()
	if _, err := pc.servico.Desconectar(context.Background()); err != nil {
		pc.t.Fatal(err)
	}
	if _, err := pc.servico.Credencial(context.Background()); err != nil {
		pc.t.Fatal(err)
	}
}

// reconectar é um pareamento novo da mesma instalação com a mesma conta (a D3
// garante): outro aparelho, outro token e outro nuvem_conectado_em, como a F6
// os deixa.
func (pc *pcE2E) reconectar() {
	pc.t.Helper()
	n := pc.nuvem
	pc.aparelho = cloudstore.NewUUIDv7()
	pc.token = cloudauth.NovoSegredo()
	n.mem.AddDevice(n.conta, pc.aparelho)
	n.mem.AddDeviceToken(pc.aparelho, cloudauth.HMAC(n.pepper, pc.token))
	if err := pc.cofre.Gravar(nuvem.NomeNoCofrePortatil, pc.token); err != nil {
		pc.t.Fatal(err)
	}
	if err := storage.NovaInstalacao(pc.db).Gravar(context.Background(), map[string]string{
		"nuvem_aparelho": pc.aparelho, "nuvem_nome": "PC de teste",
		"nuvem_conectado_em": time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano),
	}); err != nil {
		pc.t.Fatal(err)
	}
}

// semSelo falha se o ditado tem selo em algum dos lados.
func semSelo(t *testing.T, n *nuvemE2E, id string, pcs map[string]*pcE2E) {
	t.Helper()
	if it := n.celItem(id); it.VersaoEmConflito != nil {
		t.Errorf("a nuvem ganhou um selo sem conflito: %q", it.VersaoEmConflito.FinalText)
	}
	for nome, pc := range pcs {
		if l := pc.linha(id); l.Conflito != nil {
			t.Errorf("%s ganhou um selo sem conflito: %q", nome, l.Conflito.FinalText)
		}
	}
}

// Sondas A e D: o PC1 fica fora enquanto o celular edita um ditado dele (A) e
// um do PC2 (D). Na volta, sem nada mudado no PC1, nenhum selo nasce, e o PC1
// recebe os textos do celular. Nada sobe (nenhum push), e a versão da nuvem
// não muda. Pela reconexão e pelo 410.
func TestSyncE2E_VoltaSemMudancaNaoCriaSelo(t *testing.T) {
	for _, volta := range []string{"reconexao", "410"} {
		t.Run(volta, func(t *testing.T) {
			n := novaNuvemE2E(t, server.Limites{})
			pc1, pc2 := n.novoPC(), n.novoPC()
			doPC1 := pc1.ditar("do PC1, original")
			doPC2 := pc2.ditar("do PC2, original")
			pc1.rodadaOK()
			pc2.rodadaOK()
			pc1.rodadaOK()

			if volta == "reconexao" {
				pc1.desconectar()
			}
			n.celEditar(doPC1, "do PC1, editado no celular")
			n.celEditar(doPC2, "do PC2, editado no celular")
			versoes := map[string]int64{doPC1: n.celItem(doPC1).Version, doPC2: n.celItem(doPC2).Version}

			if volta == "reconexao" {
				pc1.reconectar()
			} else {
				n.porteiro.mexer(func(p *porteiro) { p.gone410 = 1 })
			}
			n.porteiro.zerar()
			pc1.rodadaOK()
			pc2.rodadaOK()

			if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "push") {
				t.Errorf("a volta sem mudança mandou push: %v", pedidos)
			}
			for id, quero := range map[string]string{doPC1: "do PC1, editado no celular", doPC2: "do PC2, editado no celular"} {
				semSelo(t, n, id, map[string]*pcE2E{"PC1": pc1, "PC2": pc2})
				it := n.celItem(id)
				if it.FinalText != quero || it.Version != versoes[id] {
					t.Errorf("na nuvem: %q v%d; quero %q v%d", it.FinalText, it.Version, quero, versoes[id])
				}
				if l := pc1.linha(id); l.FinalText != quero || l.ServerVersion == nil || *l.ServerVersion != it.Version {
					t.Errorf("no PC1: %q v%v", l.FinalText, l.ServerVersion)
				}
			}
			if pc1.naFila() != 0 {
				t.Errorf("a fila do PC1: %d", pc1.naFila())
			}
		})
	}
}

// Sonda B: o PC1 edita, desconectado, um ditado do PC2 que ninguém mais
// mexeu. Na volta a edição vale (não é conflito), sem selo, nos três.
func TestSyncE2E_EdicaoDesconectadoNumDitadoDeOutroPC(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	id := pc2.ditar("ditado do PC2")
	pc2.rodadaOK()
	pc1.rodadaOK()

	pc1.desconectar()
	pc1.editar(id, "editado no PC1 desconectado")
	if pc1.naFila() != 1 {
		t.Fatalf("a edição desconectado num ditado que a nuvem tem não ficou marcada: fila %d", pc1.naFila())
	}
	pc1.reconectar()
	pc1.rodadaOK()
	pc2.rodadaOK()

	semSelo(t, n, id, map[string]*pcE2E{"PC1": pc1, "PC2": pc2})
	if it := n.celItem(id); it.FinalText != "editado no PC1 desconectado" {
		t.Errorf("na nuvem: %q", it.FinalText)
	}
	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		if l := pc.linha(id); l.FinalText != "editado no PC1 desconectado" {
			t.Errorf("no %s: %q", nome, l.FinalText)
		}
	}
}

// O conflito de verdade, desconectado: o celular e o PC1 (fora) editam o
// mesmo ditado. Vale o do celular (chegou primeiro), e a edição do PC1 vai
// para o selo, nos dois lados. Nada se perde.
func TestSyncE2E_ConflitoDesconectadoDaOSelo(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	id := pc1.ditar("original")
	pc1.rodadaOK()

	pc1.desconectar()
	n.celEditar(id, "do celular")
	pc1.editar(id, "do PC1, desconectado")
	pc1.reconectar()
	pc1.rodadaOK()

	it := n.celItem(id)
	if it.FinalText != "do celular" || it.VersaoEmConflito == nil || it.VersaoEmConflito.FinalText != "do PC1, desconectado" {
		t.Fatalf("na nuvem: %+v", it)
	}
	if l := pc1.linha(id); l.FinalText != "do celular" || l.Conflito == nil || l.Conflito.FinalText != "do PC1, desconectado" {
		t.Errorf("no PC1: %q selo %+v", l.FinalText, l.Conflito)
	}
}

// Sonda C: o PC1 pôs na lixeira e subiu; desconectado, o celular restaura.
// Na volta, o ditado continua restaurado em todos: a lixeira antiga não é
// reenviada. E a lixeira feita DESCONECTADO (num ditado do PC2) sobe e vale
// (apagar vence).
func TestSyncE2E_VoltaNaoDesfazORestaurarEALixeiraDeForaSobe(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	restaurado := pc1.ditar("o PC1 pôs na lixeira; o celular restaurou")
	doPC2 := pc2.ditar("do PC2; o PC1 põe na lixeira desconectado")
	pc1.rodadaOK()
	pc2.rodadaOK()
	pc1.rodadaOK()
	pc1.lixeira(restaurado)
	pc1.rodadaOK()

	pc1.desconectar()
	n.celRestaurar(restaurado)
	pc1.lixeira(doPC2)
	pc1.reconectar()
	pc1.rodadaOK()
	pc2.rodadaOK()

	if it := n.celItem(restaurado); it.DeletedAt != nil {
		t.Error("a volta do PC1 desfez o restaurar do celular na nuvem")
	}
	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		if pc.linha(restaurado).DeletedAt != nil {
			t.Errorf("o restaurar do celular não chegou ao %s", nome)
		}
		if pc.linha(doPC2).DeletedAt == nil {
			t.Errorf("a lixeira feita desconectado não chegou ao %s", nome)
		}
	}
	if it := n.celItem(doPC2); it.DeletedAt == nil {
		t.Error("a lixeira feita desconectado não chegou à nuvem")
	}
}

// Sondas E e F: o PC2 edita e perde para o celular (o selo guarda a edição
// do PC2). O PC1, desatualizado (fora durante tudo isso), volta, pela
// reconexão (E) ou pelo 410 (F). O selo continua com a edição do PC2, na
// nuvem e nos dois PCs, e nenhum selo novo nasce.
func TestSyncE2E_VoltaDoPCDesatualizadoMantemOSelo(t *testing.T) {
	for _, volta := range []string{"reconexao", "410"} {
		t.Run(volta, func(t *testing.T) {
			n := novaNuvemE2E(t, server.Limites{})
			pc1, pc2 := n.novoPC(), n.novoPC()
			id := pc1.ditar("texto original")
			pc1.rodadaOK()
			pc2.rodadaOK()

			if volta == "reconexao" {
				pc1.desconectar()
			}
			n.celEditar(id, "o texto do celular")
			pc2.editar(id, "EDIÇÃO DO PC2 que perdeu")
			pc2.rodadaOK()
			if it := n.celItem(id); it.VersaoEmConflito == nil || it.VersaoEmConflito.FinalText != "EDIÇÃO DO PC2 que perdeu" {
				t.Fatalf("o selo não nasceu: %+v", it)
			}

			if volta == "reconexao" {
				pc1.reconectar()
			} else {
				n.porteiro.mexer(func(p *porteiro) { p.gone410 = 1 })
			}
			pc1.rodadaOK()
			pc2.rodadaOK()

			it := n.celItem(id)
			if it.FinalText != "o texto do celular" || it.VersaoEmConflito == nil || it.VersaoEmConflito.FinalText != "EDIÇÃO DO PC2 que perdeu" {
				t.Fatalf("na nuvem, depois da volta do PC1: %q selo %+v", it.FinalText, it.VersaoEmConflito)
			}
			for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
				l := pc.linha(id)
				if l.FinalText != "o texto do celular" || l.Conflito == nil || l.Conflito.FinalText != "EDIÇÃO DO PC2 que perdeu" {
					t.Errorf("no %s: %q selo %+v", nome, l.FinalText, l.Conflito)
				}
			}
		})
	}
}

// Desconectado, o ditado NOVO não entra na fila (ADR-019, "Fila no PC": o
// que a nuvem nunca teve é do envio inicial). Desde a F7c, ele sobe na volta,
// pela completação da rodada (o envio inicial já foi aceito: aqui, sozinho,
// porque na conexão não havia nada a enviar); o que foi para a lixeira
// desconectado, não.
func TestSyncE2E_DitadoNovoDesconectadoEsperaOEnvioInicial(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	pc1.ditar("subiu")
	pc1.rodadaOK()
	if h := pc1.estado().Historico; h == nil || !h.Aceito {
		t.Fatalf("sem nada a enviar na conexão, o aceite não veio sozinho: %+v", h)
	}

	pc1.desconectar()
	novo := pc1.ditar("ditado desconectado")
	pc1.editar(novo, "e editado desconectado")
	lixo := pc1.ditar("para a lixeira")
	pc1.lixeira(lixo)
	if pc1.naFila() != 0 {
		t.Fatalf("o ditado novo desconectado entrou na fila: %d", pc1.naFila())
	}
	pc1.reconectar()
	pc1.rodadaOK()
	var subiu bool
	for _, it := range n.celLista() {
		switch it.ID {
		case novo:
			subiu = it.FinalText == "e editado desconectado"
		case lixo:
			t.Error("o ditado da lixeira subiu")
		}
	}
	if !subiu {
		t.Error("o ditado novo desconectado não subiu depois do aceite")
	}
}
