package integration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// O teto da resposta do cliente (syncengine.MaxCorpoDaResposta) cobre a maior
// página que a nuvem pode mandar. O orçamento da página conta o texto em bytes
// crus (cloudstore.PageTextBudget, e o último item passa dele em até 3 textos
// de MaxTextBytes: o bruto, o final e o do selo); no JSON, cada byte vira até
// 6 (o encoder escapa <, > e & e os caracteres de controle como \u00XX). O
// resto do item (os campos curtos, com os tetos do payload: 32, 35, 100 e 200
// bytes, os ids, as datas e as chaves) fica em 3 KiB mesmo escapado, para os
// 500 itens da página.
func TestSyncE2E_TetoDaRespostaCobreOPiorCaso(t *testing.T) {
	texto := 6 * (cloudstore.PageTextBudget + 3*cloudstore.MaxTextBytes)
	envelope := cloudstore.MaxPullLimit * (3 << 10)
	if pior := texto + envelope; syncengine.MaxCorpoDaResposta < pior {
		t.Fatalf("MaxCorpoDaResposta = %d; a pior página tem %d", syncengine.MaxCorpoDaResposta, pior)
	}
	if syncengine.LimiteDoPull > cloudstore.MaxPullLimit {
		t.Fatalf("o PC pede %d por página; a nuvem manda no máximo %d", syncengine.LimiteDoPull, cloudstore.MaxPullLimit)
	}
}

// Uma página de verdade com o texto que mais cresce no JSON: 17 ditados com o
// bruto e o final de 64 KiB de '<' (cada um vira 384 KiB escapado). A primeira
// página do PC2 leva 16 deles (o orçamento de 2 MiB), uns 12 MiB de JSON: com
// o teto antigo de 8 MiB, o PC2 repetia a mesma página para sempre, e o
// cursor não andava.
func TestSyncE2E_PaginaComTextoEscapado(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe uns 13 MiB de ditados")
	}
	n := novaNuvemE2E(t, server.Limites{})
	pc1, pc2 := n.novoPC(), n.novoPC()
	// A primeira rodada do PC2 vê que a nuvem sabe de notas (N7) e lê de novo do
	// 0 uma vez; com ela antes, as páginas contadas abaixo são só as do ditado.
	pc2.rodadaOK()
	enorme := strings.Repeat("<", storage.MaxTextoBytes)
	agora := time.Now().UTC()
	for i := 0; i < 17; i++ {
		if err := pc1.repo.Put(storage.Transcription{
			ID: storage.NewID(), UserID: pc1.dono, SourcePlatform: "windows", RawText: enorme, FinalText: enorme,
			Mode: "smart", ASRProvider: "whisper-local", Status: "done", CreatedAt: agora, UpdatedAt: agora,
			Version: 1, SyncState: storage.SituacaoNormal,
		}); err != nil {
			t.Fatal(err)
		}
	}
	pc1.rodadaOK()
	if pc1.naFila() != 0 {
		t.Fatalf("o PC1 não subiu tudo: fila %d", pc1.naFila())
	}
	n.porteiro.zerar()
	pc2.rodadaOK()
	if got := len(pc2.todos()); got != 17 {
		t.Errorf("o PC2 tem %d ditados; quero 17", got)
	}
	if pedidos, _ := n.porteiro.visto(); len(pedidos) != 2 {
		t.Errorf("os pedidos do PC2: %v; quero duas páginas", pedidos)
	}
}
