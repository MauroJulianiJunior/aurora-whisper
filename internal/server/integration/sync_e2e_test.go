package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// A ponta a ponta do sync (ADR-019, F7a e F7b): dois PCs e o celular contra a
// nuvem de verdade. O palco está em sync_harness_test.go.

// Ditar no PC1 aparece no celular e no PC2, com o aparelho de origem; e
// ninguém ecoa: a rodada seguinte dos dois PCs não manda nada.
func TestSyncE2E_DitarNoPC1ChegaAoCelularEAoPC2(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	id := pc1.ditar("o balancete de setembro")
	if pc1.naFila() != 1 {
		t.Fatalf("o ditado não entrou na fila: %d", pc1.naFila())
	}
	pc1.rodadaOK()
	if pc1.naFila() != 0 {
		t.Errorf("a fila do PC1 ficou com %d", pc1.naFila())
	}
	if sv := pc1.linha(id).ServerVersion; sv == nil || *sv != 1 {
		t.Errorf("a versão da nuvem no PC1: %v", sv)
	}

	it := n.celItem(id)
	if it.FinalText != "o balancete de setembro" || it.RawText != "bruto: o balancete de setembro" ||
		it.DeviceID == nil || *it.DeviceID != pc1.aparelho {
		t.Errorf("no celular: %+v", it)
	}

	pc2.rodadaOK()
	l := pc2.linha(id)
	if l.FinalText != "o balancete de setembro" || l.UserID != pc2.dono || l.DeviceID == nil || *l.DeviceID != pc1.aparelho ||
		l.ServerVersion == nil || *l.ServerVersion != 1 || l.TargetApp != nil {
		t.Errorf("no PC2: %+v", l)
	}
	if pc2.naFila() != 0 {
		t.Errorf("aplicar ecoou: a fila do PC2 tem %d", pc2.naFila())
	}

	// Ninguém ecoa: as rodadas seguintes não mandam push.
	n.porteiro.zerar()
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "push") {
		t.Errorf("houve push sem nada mudar: %v", pedidos)
	}
}

// Editar no celular volta aos dois PCs; a versão sobe aqui (a aba aberta no
// painel recebe 409), e nada volta à nuvem.
func TestSyncE2E_EditarNoCelularVoltaAosPCs(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	id := pc1.ditar("reunião às três")
	pc1.rodadaOK()
	pc2.rodadaOK()
	versaoAntes := pc1.linha(id).Version

	n.celEditar(id, "reunião às quatro")
	n.porteiro.zerar()
	pc1.rodadaOK()
	pc2.rodadaOK()
	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		l := pc.linha(id)
		if l.FinalText != "reunião às quatro" || l.ServerVersion == nil || *l.ServerVersion != 2 {
			t.Errorf("%s: %q v%v", nome, l.FinalText, l.ServerVersion)
		}
		if pc.naFila() != 0 {
			t.Errorf("%s ecoou a edição do celular", nome)
		}
	}
	if pc1.linha(id).Version != versaoAntes+1 {
		t.Errorf("a versão local não subiu: %d -> %d", versaoAntes, pc1.linha(id).Version)
	}
	// A aba aberta com a versão de antes recebe 409.
	if _, err := pc1.repo.UpdateFinalText(context.Background(), pc1.dono, id, versaoAntes, "da aba velha"); !errors.Is(err, storage.ErrVersionConflict) {
		t.Errorf("a aba velha gravou: %v", err)
	}
	if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "push") {
		t.Errorf("a edição do celular voltou à nuvem: %v", pedidos)
	}
}

// O mesmo ditado editado nos dois lados: vale o que chegou primeiro (o
// celular), e o texto do PC vai para o selo, nos três. Nenhum texto se perde.
func TestSyncE2E_EditarNosDoisLadosDaOSelo(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	id := pc1.ditar("texto original")
	pc1.rodadaOK()
	pc2.rodadaOK()

	n.celEditar(id, "o texto do celular")
	pc1.editar(id, "o texto do PC")
	pc1.rodadaOK()
	pc2.rodadaOK()

	it := n.celItem(id)
	if it.FinalText != "o texto do celular" || it.VersaoEmConflito == nil || it.VersaoEmConflito.FinalText != "o texto do PC" ||
		it.VersaoEmConflito.DeviceID == nil || *it.VersaoEmConflito.DeviceID != pc1.aparelho {
		t.Fatalf("no celular: %+v", it)
	}
	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		l := pc.linha(id)
		if l.FinalText != "o texto do celular" || l.Conflito == nil || l.Conflito.FinalText != "o texto do PC" ||
			l.ServerVersion == nil || *l.ServerVersion != it.Version {
			t.Errorf("%s: %q selo %+v v%v (a nuvem está na v%d)", nome, l.FinalText, l.Conflito, l.ServerVersion, it.Version)
		}
		if pc.naFila() != 0 {
			t.Errorf("%s ficou com %d na fila", nome, pc.naFila())
		}
	}

	// A escolha no celular limpa o selo, e chega aos PCs.
	if st, b := n.celular(http.MethodPatch, "/api/v1/transcriptions/"+id, map[string]any{"base_version": it.Version, "resolver_conflito": "trocar"}); st != http.StatusOK {
		t.Fatalf("escolher = %d %s", st, b)
	}
	pc1.rodadaOK()
	if l := pc1.linha(id); l.Conflito != nil || l.FinalText != "o texto do PC" {
		t.Errorf("depois da escolha: %q selo %+v", l.FinalText, l.Conflito)
	}
}

// puxarNoMeio é um pull sozinho, com o marcador pendente: o aplicador tem de
// PULAR o ditado.
func puxarNoMeio(t *testing.T, n *nuvemE2E, pc *pcE2E) {
	t.Helper()
	m := &syncengine.Motor{Fila: pc.repo.Fila(pc.dono), Cliente: syncengine.NovoCliente(n.base, pc.token, nil),
		Aparelho: pc.aparelho, PareadoEm: pc.credencial(t).PareadoEm, NovoID: storage.NewID}
	rel := syncengine.Relatorio{Push: map[string]int{}, Pull: map[string]int{}, Recusados: map[string]int{}}
	if err := m.Receber(context.Background(), &rel); err != nil {
		t.Fatal(err)
	}
	if rel.Pull[syncengine.TipoPulado] != 1 {
		t.Fatalf("o pull do meio não pulou o ditado com marcador: %+v", rel.Pull)
	}
}

// Campos diferentes, com um pull no meio: o celular edita o texto, o PC1 põe
// na lixeira e puxa ANTES de subir (o pull pula o ditado, que tem marcador);
// o push mescla (applied, mesclado), o PC não guarda essa versão, e o pull
// seguinte traz o estado inteiro: o texto do celular e a lixeira do PC. Sem
// a regra do eco pela versão, o PC tomaria o estado mesclado (que leva o
// device_id dele) por eco, e o texto do celular nunca chegaria (ADR-019, "Eco
// e mescla"; a F7a prova o lado do PC).
func TestSyncE2E_CamposDiferentesComUmPullNoMeio(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	id := pc1.ditar("antes")
	pc1.rodadaOK()

	n.celEditar(id, "editado no celular")
	pc1.lixeira(id)
	puxarNoMeio(t, n, pc1)
	if l := pc1.linha(id); l.DeletedAt == nil || l.FinalText != "antes" {
		t.Fatalf("o pull do meio sobrescreveu a pendência: %+v", l)
	}

	pc1.rodadaOK()
	l := pc1.linha(id)
	if l.DeletedAt == nil || l.FinalText != "editado no celular" || l.Conflito != nil {
		t.Errorf("no PC1: lixeira %v, texto %q, selo %+v", l.DeletedAt, l.FinalText, l.Conflito)
	}
	it := n.celItem(id)
	if it.DeletedAt == nil || it.FinalText != "editado no celular" || it.VersaoEmConflito != nil || *l.ServerVersion != it.Version {
		t.Errorf("no celular: %+v (o PC está na v%v)", it, l.ServerVersion)
	}
	if pc1.naFila() != 0 {
		t.Errorf("a fila: %d", pc1.naFila())
	}
}

// A ordem inversa é a "edição antiga" (ADR-019, "Decisões da F2"): o celular
// põe na lixeira, o PC edita sobre a base de antes. Apagar vence: o ditado
// fica na lixeira, com o texto de antes, e a edição do PC vai para o selo,
// nos dois lados. Com um pull no meio também.
func TestSyncE2E_EdicaoAntigaContraALixeiraVaiParaOSelo(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	id := pc1.ditar("antes")
	pc1.rodadaOK()

	n.celLixeira(id)
	pc1.editar(id, "editado no PC")
	puxarNoMeio(t, n, pc1)
	pc1.rodadaOK()
	l := pc1.linha(id)
	if l.DeletedAt == nil || l.FinalText != "antes" || l.Conflito == nil || l.Conflito.FinalText != "editado no PC" {
		t.Errorf("no PC1: lixeira %v, texto %q, selo %+v", l.DeletedAt, l.FinalText, l.Conflito)
	}
	it := n.celItem(id)
	if it.DeletedAt == nil || it.VersaoEmConflito == nil || it.VersaoEmConflito.FinalText != "editado no PC" || *l.ServerVersion != it.Version {
		t.Errorf("no celular: %+v", it)
	}
}

func (pc *pcE2E) credencial(t *testing.T) syncengine.Credencial {
	t.Helper()
	c, err := conexaoDoTeste{pc.servico}.Credencial(context.Background())
	if err != nil || !c.Conectado {
		t.Fatalf("credencial: %+v, %v", c, err)
	}
	return c
}

// A lixeira e o restaurar, nos dois sentidos; e o "excluir definitivamente"
// do celular apaga a cópia dos PCs (a lápide).
func TestSyncE2E_LixeiraERestaurarNosDoisSentidos(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	a := pc1.ditar("vai para a lixeira no PC")
	b := pc1.ditar("vai para a lixeira no celular")
	pc1.rodadaOK()
	pc2.rodadaOK()

	// PC -> celular e PC2.
	pc1.lixeira(a)
	pc1.rodadaOK()
	if it := n.celItem(a); it.DeletedAt == nil {
		t.Error("a lixeira do PC não chegou ao celular")
	}
	pc2.rodadaOK()
	if pc2.linha(a).DeletedAt == nil {
		t.Error("a lixeira do PC1 não chegou ao PC2")
	}
	// Celular -> PCs.
	n.celLixeira(b)
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pc1.linha(b).DeletedAt == nil || pc2.linha(b).DeletedAt == nil {
		t.Error("a lixeira do celular não chegou aos PCs")
	}
	if l := pc1.linha(b); l.PurgeAt == nil || !l.PurgeAt.Equal(l.DeletedAt.Add(storage.PrazoDaLixeira)) {
		t.Errorf("o prazo do expurgo: %v, %v", l.DeletedAt, l.PurgeAt)
	}
	// Restaurar no celular: chega aos PCs.
	n.celRestaurar(b)
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pc1.linha(b).DeletedAt != nil || pc2.linha(b).DeletedAt != nil {
		t.Error("o restaurar do celular não chegou aos PCs")
	}
	// O PC1 restaura o que ele mesmo apagou.
	pc1.restaurar(a)
	pc1.rodadaOK()
	if it := n.celItem(a); it.DeletedAt != nil {
		t.Error("o restaurar do PC não chegou ao celular")
	}
	pc2.rodadaOK()
	if pc2.linha(a).DeletedAt != nil {
		t.Error("o restaurar do PC1 não chegou ao PC2")
	}

	// Excluir de vez no celular (da lixeira): a lápide apaga a cópia dos PCs.
	n.celLixeira(a)
	n.celExcluirDeVez(a)
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pc1.temLinha(a) || pc2.temLinha(a) {
		t.Error("o excluído de vez ficou num PC")
	}
	// E o PC que tinha uma mudança pendente nele recebe o gone e apaga.
	c := pc1.ditar("vai ser excluído com uma edição pendente")
	pc1.rodadaOK()
	n.celLixeira(c)
	n.celExcluirDeVez(c)
	pc1.editar(c, "edição que chega tarde")
	pc1.rodadaOK()
	if pc1.temLinha(c) || pc1.naFila() != 0 {
		t.Errorf("o gone apagado não apagou: linha %v, fila %d", pc1.temLinha(c), pc1.naFila())
	}
}

// O PC sem rede acumula e envia quando ela volta, sem duplicar; durante a
// queda, nenhum pull (o push sem resposta encerra a rodada).
func TestSyncE2E_OfflineAcumulaEEnviaQuandoVolta(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	velho := pc1.ditar("de antes da queda")
	pc1.rodadaOK()

	n.porteiro.mexer(func(p *porteiro) { p.fora = true })
	novos := []string{pc1.ditar("um"), pc1.ditar("dois"), pc1.ditar("três")}
	pc1.editar(novos[0], "um, editado")
	pc1.lixeira(velho)
	n.porteiro.zerar()
	for i := 0; i < 3; i++ {
		if err := pc1.rodada(); !errors.Is(err, syncengine.ErrTransitorio) {
			t.Fatalf("rodada sem rede = %v", err)
		}
	}
	if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "pull") {
		t.Errorf("houve pull depois de push sem resposta: %v", pedidos)
	}
	if pc1.laco.Resumo() != "Nuvem: sem conexão" {
		t.Errorf("a bandeja: %q", pc1.laco.Resumo())
	}
	if pc1.naFila() != 4 {
		t.Errorf("a fila sem rede: %d; quero 4", pc1.naFila())
	}

	n.porteiro.mexer(func(p *porteiro) { p.fora = false })
	pc1.rodadaOK()
	if pc1.naFila() != 0 || pc1.laco.Resumo() != "Nuvem: em dia" {
		t.Errorf("depois da volta: fila %d, %q", pc1.naFila(), pc1.laco.Resumo())
	}
	cel := n.celLista()
	semRepetidos(t, "celular", cel, func(i itemDoCelular) string { return i.ID })
	if len(cel) != 4 {
		t.Fatalf("o celular tem %d ditados; quero 4", len(cel))
	}
	if it := n.celItem(novos[0]); it.FinalText != "um, editado" {
		t.Errorf("a edição offline: %q", it.FinalText)
	}
	if it := n.celItem(velho); it.DeletedAt == nil {
		t.Error("a lixeira offline não chegou")
	}
}

// A resposta perdida: a nuvem aplica, o PC não sabe. O Agent reinicia; o
// primeiro pedido depois de subir é o REENVIO, com os mesmos mutation_id,
// antes de qualquer pull, e a nuvem responde duplicate. Nada duplica.
func TestSyncE2E_RespostaPerdidaEReinicio(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	pc1.rodadaOK()
	ids := []string{pc1.ditar("um"), pc1.ditar("dois")}
	n.porteiro.mexer(func(p *porteiro) { p.perderRespostas = 1 })
	n.porteiro.zerar()
	if err := pc1.rodada(); !errors.Is(err, syncengine.ErrTransitorio) {
		t.Fatalf("rodada com a resposta perdida = %v", err)
	}
	if len(n.celLista()) != 2 {
		t.Fatal("a nuvem não aplicou o push cuja resposta se perdeu")
	}

	pc1.reiniciar()
	pc1.rodadaOK()
	pedidos, pushes := n.porteiro.visto()
	if len(pedidos) < 3 || pedidos[0] != "push" || pedidos[1] != "push" || !strings.HasPrefix(pedidos[2], "pull") {
		t.Fatalf("pedidos = %v; quero o push perdido, o reenvio e só então o pull", pedidos)
	}
	if strings.Join(pushes[0], ",") != strings.Join(pushes[1], ",") {
		t.Errorf("o reenvio trocou os ids: %v e %v", pushes[0], pushes[1])
	}
	cel := n.celLista()
	semRepetidos(t, "celular", cel, func(i itemDoCelular) string { return i.ID })
	if len(cel) != 2 || pc1.naFila() != 0 {
		t.Errorf("celular %d, fila %d", len(cel), pc1.naFila())
	}
	for _, id := range ids {
		if l := pc1.linha(id); l.ServerVersion == nil || *l.ServerVersion != 1 {
			t.Errorf("%s: a versão depois do duplicate: %v", id, l.ServerVersion)
		}
	}
}

// Revogar no celular: o PC recebe 401 na rodada seguinte, fica desconectado
// (o token sai do Cofre, o aparelho da instalação), a bandeja diz
// "desconectada", o ditado segue local e não entra mais na fila, e nenhum
// pedido sai depois.
func TestSyncE2E_RevogarDesconecta(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	pc1.ditar("antes de revogar")
	pc1.rodadaOK()

	n.celRevogar(pc1.aparelho)
	pc1.ditar("depois de revogar, antes de o PC saber")
	if err := pc1.rodada(); !errors.Is(err, syncengine.ErrNaoAutorizado) {
		t.Fatalf("rodada depois de revogar = %v", err)
	}
	if _, err := pc1.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
		t.Error("o token revogado ficou no Cofre")
	}
	e, err := pc1.servico.Estado(context.Background())
	if err != nil || e.Estado != nuvem.Desconectado || e.Aviso != nuvem.AvisoRevogado {
		t.Errorf("o estado: %+v, %v", e, err)
	}
	if pc1.laco.Resumo() != "Nuvem: desconectada" {
		t.Errorf("a bandeja: %q", pc1.laco.Resumo())
	}
	antes := pc1.naFila()
	id := pc1.ditar("desconectado")
	if pc1.naFila() != antes {
		t.Error("o ditado desconectado entrou na fila")
	}
	if pc1.linha(id).FinalText != "desconectado" {
		t.Error("o ditado não gravou local")
	}
	n.porteiro.zerar()
	pc1.rodadaOK()
	if pedidos, _ := n.porteiro.visto(); len(pedidos) != 0 {
		t.Errorf("desconectado, saíram pedidos: %v", pedidos)
	}
}

// O 410 (o envelope de teste, mandado pelo porteiro: o servidor não o manda no
// mesmo epoch, a D2 da F9; o PC o trata assim mesmo) relê do cursor 0
// SEM reenviar nada (as versões da nuvem continuam valendo no mesmo epoch), e
// nenhuma versão muda na nuvem. O epoch novo (um backup restaurado) recomeça:
// tudo de novo, com a base vazia. Nos dois, nada duplica, nem aqui nem na
// nuvem.
func TestSyncE2E_410ReleDoZeroEEpochNovoRecomeca(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	pc1.ditar("um")
	pc1.ditar("dois")
	pc1.rodadaOK()
	pc2.ditar("três")
	pc2.rodadaOK()
	pc1.rodadaOK()
	versoes := map[string]int64{}
	for _, it := range n.celLista() {
		versoes[it.ID] = it.Version
	}

	n.porteiro.mexer(func(p *porteiro) { p.gone410 = 1 })
	n.porteiro.zerar()
	pc1.rodadaOK()
	pedidos, _ := n.porteiro.visto()
	// O pull do cursor recebe 410; o PC lê do 0 e não manda nada.
	if len(pedidos) != 2 || pedidos[0] == "pull:0" || !strings.HasPrefix(pedidos[0], "pull:") || pedidos[1] != "pull:0" {
		t.Errorf("os pedidos do 410: %v; quero o pull do cursor e o do 0, sem push", pedidos)
	}
	for _, it := range n.celLista() {
		if it.Version != versoes[it.ID] {
			t.Errorf("o 410 mudou a versão de %s na nuvem: %d -> %d", it.ID, versoes[it.ID], it.Version)
		}
	}
	verificarSemDuplicar(t, n, pc1, 3)

	// O epoch novo: a nuvem "restaurou um backup" (os mesmos dados, outro
	// epoch). O PC2 descarta a resposta e recomeça.
	n.mem.TrocarEpoch(n.conta)
	pc2.ditar("quatro")
	pc2.rodadaOK()
	verificarSemDuplicar(t, n, pc2, 4)
	pc1.rodadaOK()
	verificarSemDuplicar(t, n, pc1, 4)
}

func verificarSemDuplicar(t *testing.T, n *nuvemE2E, pc *pcE2E, quero int) {
	t.Helper()
	cel := n.celLista()
	semRepetidos(t, "celular", cel, func(i itemDoCelular) string { return i.ID })
	todos := pc.todos()
	semRepetidos(t, "PC", todos, func(l storage.Transcription) string { return l.ID })
	if len(cel) != quero || len(todos) != quero || pc.naFila() != 0 {
		t.Errorf("celular %d, PC %d, fila %d; quero %d e fila vazia", len(cel), len(todos), pc.naFila(), quero)
	}
	for _, l := range todos {
		if l.ServerVersion == nil {
			t.Errorf("%s ficou sem a versão da nuvem depois do recomeço", l.ID)
		}
	}
}

// Reconectar continua do cursor (decisão 2 da F7b, refeita na revisão de
// 06-out-2026): a lixeira e a edição feitas desconectado, em ditados que já
// tinham subido, ficam marcadas na fila e sobem na volta, com a base certa
// (apagar vence; a edição sem conflito vale), sem selo e sem trazer nada de
// volta da lixeira. As sondas da revisão estão em sync_reconexao_e2e_test.go.
func TestSyncE2E_ReconectarNaoTiraDaLixeira(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	id := pc1.ditar("vai para a lixeira desconectado")
	editado := pc1.ditar("vai ser editado desconectado")
	pc1.rodadaOK()

	// Desconecta (a instalação sem o aparelho), mexe, e reconecta (outro
	// nuvem_conectado_em).
	inst := storage.NovaInstalacao(pc1.db)
	if err := inst.Apagar(context.Background(), "nuvem_aparelho"); err != nil {
		t.Fatal(err)
	}
	pc1.lixeira(id)
	pc1.editar(editado, "editado desconectado")
	if pc1.naFila() != 2 {
		t.Fatalf("as mudanças desconectado em ditados que a nuvem tem: fila %d; quero 2", pc1.naFila())
	}
	if err := inst.Gravar(context.Background(), map[string]string{
		"nuvem_aparelho": pc1.aparelho, "nuvem_conectado_em": time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	n.porteiro.zerar()
	pc1.rodadaOK()
	if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "pull:0") {
		t.Errorf("a reconexão leu do 0: %v", pedidos)
	}
	if l := pc1.linha(id); l.DeletedAt == nil {
		t.Error("a volta tirou o ditado da lixeira")
	}
	if it := n.celItem(id); it.DeletedAt == nil {
		t.Error("a lixeira feita desconectado não chegou à nuvem")
	}
	if it := n.celItem(editado); it.FinalText != "editado desconectado" || it.VersaoEmConflito != nil {
		t.Errorf("a edição feita desconectado: %q selo %+v", it.FinalText, it.VersaoEmConflito)
	}
	if pc1.naFila() != 0 {
		t.Errorf("a fila depois da volta: %d", pc1.naFila())
	}
}

// O 429 do limite de verdade do servidor: a rodada volta com a espera pedida,
// e a bandeja diz "aguardando" (a nuvem respondeu; não é falta de conexão).
func TestSyncE2E_429(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{PushPedidos: 1, PushMutacoes: 1 << 30})
	pc1 := n.novoPC()
	pc1.ditar("um")
	pc1.rodadaOK()
	pc1.ditar("dois")
	err := pc1.rodada()
	var e *syncengine.ErrEspere
	if !errors.As(err, &e) || e.Espera <= 0 || e.Espera > syncengine.MaxEsperaPedida {
		t.Fatalf("rodada no limite = %v", err)
	}
	if pc1.naFila() != 1 || pc1.laco.Resumo() != "Nuvem: aguardando" {
		t.Errorf("fila %d, %q", pc1.naFila(), pc1.laco.Resumo())
	}
}

// O redirecionamento não é seguido: o token nunca chega a outro endereço.
func TestSyncE2E_RedirecionamentoNaoLevaOToken(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	var comToken atomic.Int32
	outro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			comToken.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer outro.Close()
	pc1 := n.novoPC()
	pc1.ditar("um")
	n.porteiro.mexer(func(p *porteiro) { p.redirecionar = outro.URL })
	if err := pc1.rodada(); !errors.Is(err, syncengine.ErrTransitorio) {
		t.Fatalf("rodada com redirecionamento = %v", err)
	}
	if comToken.Load() != 0 {
		t.Error("o token foi a outro endereço")
	}
}

// Desconectado (o PC que nunca conectou, e o que desconectou): zero pedidos.
func TestSyncE2E_DesconectadoZeroPedidos(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	if _, err := pc1.servico.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	n.porteiro.zerar()
	pc1.ditar("desconectado")
	pc1.rodadaOK()
	pc1.rodadaOK()
	if pedidos, _ := n.porteiro.visto(); len(pedidos) != 0 {
		t.Errorf("pedidos desconectado: %v", pedidos)
	}
	if pc1.naFila() != 0 {
		t.Error("o ditado desconectado entrou na fila")
	}
}

// O ditado nunca espera a rede: com um push travado no meio do caminho, gravar
// um ditado termina na hora.
func TestSyncE2E_PutNaoEsperaOPushTravado(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	pc1.ditar("um")
	solta := make(chan struct{})
	n.porteiro.mexer(func(p *porteiro) { p.travarPush = solta })
	feito := make(chan error, 1)
	go func() { feito <- pc1.rodada() }()
	// Espera o push chegar à nuvem (e travar lá).
	limite := time.Now().Add(5 * time.Second)
	for {
		if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "push") {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("o push não saiu")
		}
		time.Sleep(5 * time.Millisecond)
	}
	inicio := time.Now()
	id := pc1.ditar("ditado durante o push travado")
	pc1.editar(id, "e editado")
	if d := time.Since(inicio); d > 500*time.Millisecond {
		t.Errorf("gravar durante o push travado levou %v", d)
	}
	close(solta)
	if err := <-feito; err != nil {
		t.Fatal(err)
	}
	pc1.rodadaOK()
	if it := n.celItem(id); it.FinalText != "e editado" {
		t.Errorf("o ditado gravado durante o push: %q", it.FinalText)
	}
}

// O laço de verdade, em segundo plano: o ditado só o acorda, e ele sobe
// sozinho.
func TestSyncE2E_LacoEmSegundoPlano(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1 := n.novoPC()
	pc1.repo.AoEnfileirar(pc1.laco.Avisar)
	ctx, cancel := context.WithCancel(context.Background())
	feito := make(chan struct{})
	go func() { pc1.laco.Rodar(ctx); close(feito) }()
	defer func() { cancel(); <-feito }()

	id := pc1.ditar("sobe sozinho")
	limite := time.Now().Add(10 * time.Second)
	for {
		if st, _ := n.celular(http.MethodGet, "/api/v1/transcriptions/"+id, nil); st == http.StatusOK {
			break
		}
		if time.Now().After(limite) {
			t.Fatalf("o ditado não subiu sozinho\n%s", pc1.log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Os logs dos dois lados, depois de tudo: nenhum texto ditado, token, id de
// ditado, URL ou e-mail.
func TestSyncE2E_LogsSoComContagens(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	id := pc1.ditar("segredo do cliente Fulano")
	pc1.rodadaOK()
	n.celEditar(id, "segredo editado no celular")
	pc1.editar(id, "segredo editado no PC")
	pc1.rodadaOK()
	pc2.rodadaOK()
	n.porteiro.mexer(func(p *porteiro) { p.fora = true })
	pc1.ditar("segredo sem rede")
	_ = pc1.rodada()
	n.porteiro.mexer(func(p *porteiro) { p.fora = false })
	pc1.rodadaOK()
	n.celRevogar(pc2.aparelho)
	_ = pc2.rodada()

	for nome, log := range map[string]string{"PC1": pc1.log.String(), "PC2": pc2.log.String(), "nuvem": n.log.String()} {
		if log == "" {
			t.Fatalf("o log do %s está vazio: o teste não mede nada", nome)
		}
		for _, proibido := range []string{"segredo", "Fulano", pc1.token, pc2.token, id, n.base, "localhost:", emailDoE2E, n.sessao} {
			if strings.Contains(log, proibido) {
				t.Errorf("o log do %s tem %q", nome, proibido)
			}
		}
	}
	if !strings.Contains(pc1.log.String(), "enviados=") {
		t.Error("o log do PC1 não tem as contagens da rodada")
	}
}
