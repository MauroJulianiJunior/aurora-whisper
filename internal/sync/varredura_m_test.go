package syncengine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O M da pergunta (ADR-019, "Decisões da F8", D3) tem dois lugares: a
// varredura o conta quando decide (ResultadoDaVarredura.Trocas, que vai ao
// CompletarEnvio), e o resumo da nuvem o lê do espelho que ela deixou
// (storage.ResumoDoEnvio.Trocas, o "previa_trocas" do GET /api/nuvem). Os dois
// têm de contar igual: senão a pergunta diria um número, e o clique soltaria
// outro. Os percursos são os da varredura (o dicionario.json e o
// historico.db de verdade, em t.TempDir()).
func TestVarredura_OMDoResumoEODaVarredura(t *testing.T) {
	ctx := context.Background()
	p := novoPCQueVarre(t)
	resumo := func() int {
		t.Helper()
		r, err := p.fila.Resumo(ctx, p.agora)
		if err != nil {
			t.Fatal(err)
		}
		return r.Trocas
	}
	conferir := func(passo string, quer int) {
		t.Helper()
		r := p.varrer()
		if r.Trocas != quer {
			t.Errorf("%s: a varredura contou %d, quero %d", passo, r.Trocas, quer)
		}
		if got := resumo(); got != r.Trocas {
			t.Errorf("%s: o resumo conta %d, a varredura %d", passo, got, r.Trocas)
		}
	}

	conferir("o arquivo de partida", 0)
	sf := p.daNuvem(1, fioDoPar("sf", "ECF", false, "", false))
	p.daNuvem(1, fioDoPar("ecede", "ECD", false, "", false))
	conferir("o que veio da nuvem", 0)
	p.aprender("LALUR", "lalu")
	conferir("o par novo", 1)
	p.desfazer("ecede")
	p.aprender("ECF", "ecede")
	conferir("a troca de termo de um par que a nuvem tem", 2)
	p.desfazer("sf")
	conferir("o desfazer de um par que a nuvem tem", 2)
	p.enviar()
	if e := p.estado(sf); e.Marcador != nil {
		t.Fatalf("o delete não subiu: %+v", e)
	}
	conferir("depois do delete", 2)
	p.aprender("ECD", "xis")
	p.desfazer("xis")
	conferir("o aprendido e desfeito antes do clique", 2)
	p.aMao(func(s *dictionary.Semente) {
		s.Entradas[0].Alias = append(s.Entradas[0].Alias, "ecedezinho")
		if s.AliasOrigem == nil {
			s.AliasOrigem = map[string]string{}
		}
		s.AliasOrigem["ecedezinho"] = dictionary.OrigemPainel
	})
	conferir("o par posto à mão com a origem", 3)

	// A linha "aplicando" (o lote gravou, e a página caiu): o resumo a lê pela
	// projeção esperada, como a varredura a confirma pelo arquivo.
	p.ateOArquivo(doPull(1, fioDoPar("ecef", "ECF", false, "", false)), true)
	if got := resumo(); got != 3 {
		t.Errorf("com a linha aplicando: o resumo conta %d, quero 3", got)
	}
	conferir("a linha aplicando confirmada", 3)

	// Depois do clique, nada mais espera: o resumo diz 0.
	if _, err := p.fila.EnviarHistorico(ctx, filepath.Join(t.TempDir(), "historico.db"), p.agora); err != nil {
		t.Fatal(err)
	}
	conferir("depois do clique", 0)
	if e := p.estado(sf); e.Presente() || e.Situacao == storage.EspelhoRecusado {
		t.Errorf("o desfeito: %+v", e)
	}
}
