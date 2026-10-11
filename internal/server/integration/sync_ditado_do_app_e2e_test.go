package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O ditado que o celular cria na nuvem chega ao PC como "recebido" (ADR-021,
// C1): o Store.Create o grava, e os PCs o puxam com o laço de verdade. A rota
// que chama o Create é da C5b; aqui ele é chamado direto.

// ditadoDoApp cria, no Store da nuvem de teste, um ditado do app da conta.
func (n *nuvemE2E) ditadoDoApp(texto string) string {
	n.t.Helper()
	ctx := context.Background()
	id := cloudstore.NewUUIDv7()
	sit, err := n.mem.Situacao(ctx, n.conta, id)
	if err != nil {
		n.t.Fatal(err)
	}
	modelo, dur := "gpt-4o-mini-transcribe", int64(2500)
	if _, created, err := n.mem.Create(ctx, cloudstore.Actor{UserID: n.conta}, sit.Epoch, id, cloudstore.NewDictation{
		SourcePlatform: cloudstore.SourcePlatformWeb, RawText: "bruto: " + texto, FinalText: texto, Mode: "smart",
		ASRProvider: "openai", ASRModel: &modelo, DurationMs: &dur,
	}, time.Now()); err != nil || !created {
		n.t.Fatalf("Create: created=%v err=%v", created, err)
	}
	return id
}

// Os nomes das origens da nuvem moram em dois lugares (o storage do PC não
// importa o servidor): este é o ponto onde eles têm de concordar. Uma origem
// nova do servidor precisa entrar nos dois, e em origem.go do storage, senão o
// PC a trata como "nasceu aqui".
func TestOrigensDaNuvemConferemEntreOPCEOServidor(t *testing.T) {
	if storage.PlataformaWeb != cloudstore.SourcePlatformWeb || storage.PlataformaAndroid != cloudstore.SourcePlatformAndroid {
		t.Fatalf("PC: %q %q; servidor: %q %q", storage.PlataformaWeb, storage.PlataformaAndroid,
			cloudstore.SourcePlatformWeb, cloudstore.SourcePlatformAndroid)
	}
}

func TestSyncE2E_DitadoDoAppChegaAosPCsComoRecebido(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	id := n.ditadoDoApp("o ditado que nasceu no celular")

	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		pc.rodadaOK()
		l := pc.linha(id)
		if l.SourcePlatform != "web" || l.DeviceID != nil || l.UserID != pc.dono || l.FinalText != "o ditado que nasceu no celular" ||
			l.RawText != "bruto: o ditado que nasceu no celular" || l.ASRProvider != "openai" || l.TargetApp != nil {
			t.Errorf("%s: %+v", nome, l)
		}
		if l.ServerVersion == nil || *l.ServerVersion != 1 {
			t.Errorf("%s: a versão da nuvem: %v", nome, l.ServerVersion)
		}
		if pc.naFila() != 0 {
			t.Errorf("%s: aplicar ecoou: a fila tem %d", nome, pc.naFila())
		}
	}

	// Ninguém ecoa: as rodadas seguintes não mandam push.
	n.porteiro.zerar()
	pc1.rodadaOK()
	pc2.rodadaOK()
	if pedidos, _ := n.porteiro.visto(); strings.Contains(strings.Join(pedidos, ","), "push") {
		t.Errorf("houve push sem nada mudar: %v", pedidos)
	}

	// O PC edita o que nasceu no celular: sobe sobre a versão 1, sem selo, e o
	// outro PC e o celular o veem.
	pc1.editar(id, "editado no PC1")
	if pc1.naFila() != 1 {
		t.Fatalf("a edição não entrou na fila: %d", pc1.naFila())
	}
	pc1.rodadaOK()
	if it := n.celItem(id); it.FinalText != "editado no PC1" || it.Version != 2 || it.VersaoEmConflito != nil {
		t.Fatalf("no celular: %+v", it)
	}
	pc2.rodadaOK()
	if l := pc2.linha(id); l.FinalText != "editado no PC1" || l.SourcePlatform != "web" || l.DeviceID != nil {
		t.Errorf("no PC2 depois da edição: %+v", l)
	}

	// E o celular edita de novo: os dois PCs recebem, e a origem não muda.
	n.celEditar(id, "editado no celular")
	pc1.rodadaOK()
	pc2.rodadaOK()
	for nome, pc := range map[string]*pcE2E{"PC1": pc1, "PC2": pc2} {
		if l := pc.linha(id); l.FinalText != "editado no celular" || l.SourcePlatform != "web" || l.DeviceID != nil || l.Conflito != nil {
			t.Errorf("%s depois da edição do celular: %+v", nome, l)
		}
	}
}

// Esquecer a nuvem e voltar não faz o PC tratar o ditado do app como se fosse
// dele: o envio inicial não o leva (a prévia conta só os ditados nascidos aqui)
// e o PC continua a mostrá-lo.
func TestSyncE2E_DitadoDoAppNaoEhDoPCDepoisDeEsquecer(t *testing.T) {
	n := novaNuvemE2E(t, server.Limites{})
	pc := n.novoPC()
	doApp := n.ditadoDoApp("do celular")
	doPC := pc.ditar("do PC")
	pc.rodadaOK()
	if !pc.temLinha(doApp) || pc.linha(doApp).SourcePlatform != "web" {
		t.Fatalf("o PC não recebeu o ditado do app: %+v", pc.linha(doApp))
	}

	ctx := context.Background()
	fila := pc.repo.Fila(pc.dono)
	if err := fila.EsquecerNuvem(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Sem a versão da nuvem, o do PC é histórico a enviar; o do app, não.
	previa, err := fila.Previa(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if previa != 1 {
		t.Errorf("a prévia do envio inicial = %d, quero 1 (só %s, que nasceu aqui)", previa, doPC)
	}
	if !pc.temLinha(doApp) || pc.linha(doApp).FinalText != "do celular" || pc.linha(doApp).SourcePlatform != storage.PlataformaWeb {
		t.Errorf("o ditado do app mudou no esquecer: %+v", pc.linha(doApp))
	}
}
