package main

import (
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// ---------------------------------------------------------------------
// A estimativa de custo não é um teto: são 1.200 tokens de entrada e 150 de saída
// por chamada, sem raciocínio. O texto diz "cerca de", a linha é conferida, e uma
// estimativa que já passa do limite da rodada recusa antes de pedir a chave (a
// rodada gastaria o limite e pararia sem decisão).
// ---------------------------------------------------------------------

// Preço só para tornar a estimativa fácil de conferir à mão: por chamada,
// (1.200 × 1.000 + 150 × 10.000) / 1e6 = US$ 2,70; datasetPequeno tem 7
// frases que vão ao provedor e a rodada tem 2 rodadas: 14 chamadas = US$ 37,80.
const precoDaEstimativa = "openai:m1=1000/10000"

func flagsDaEstimativa(t *testing.T, extra ...string) (*ambiente, []string) {
	t.Helper()
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, nil, nil)
	flags := append([]string{"-gastar", "-rodadas", "2", "-modelos", "openai:m1", "-preco", precoDaEstimativa}, extra...)
	return a, juntar(escreverDataset(t, frases, refs), flags)
}

func TestRun_LinhaDaEstimativaDeCusto(t *testing.T) {
	a, args := flagsDaEstimativa(t, "-limite-usd", "100")
	if cod := a.rodar(args...); cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	contem(t, a.stdout.String(), "2 rodada(s); 14 chamadas previstas por modelo, 14 no total; estimativa de custo cerca de US$ 37,80, sem contar tokens de raciocínio (limite da rodada: US$ 100,00)")
	naoContem(t, a.stdout.String(), "estimativa de custo até")
}

func TestRun_EstimativaDoisModelosSomaOsDois(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, nil, nil)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-rodadas", "1", "-modelos", "openai:m1,anthropic:m2",
		"-preco", "openai:m1=1000/10000,anthropic:m2=0/10000", "-limite-usd", "100"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	// 7 chamadas por modelo: m1 = 7 × 2,70 = 18,90; m2 = 7 × 1,50 = 10,50; total 29,40.
	contem(t, a.stdout.String(), "1 rodada(s); 7 chamadas previstas por modelo, 14 no total; estimativa de custo cerca de US$ 29,40")
}

func TestRun_EstimativaAcimaDoLimiteRecusaAntesDePedirAChave(t *testing.T) {
	a, args := flagsDaEstimativa(t, "-limite-usd", "37")
	cod := a.rodar(args...)
	if cod != 2 {
		t.Fatalf("saiu com %d, quero 2\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	contem(t, a.stderr.String(), "recusado: a estimativa (US$ 37,80) já passa do limite da rodada (US$ 37,00)")
	contem(t, a.stderr.String(), "Reduza -rodadas")
	if len(a.leitor.Prompts()) != 0 {
		t.Errorf("pediu a chave antes de recusar: %v", a.leitor.Prompts())
	}
	if n := len(a.openai.Pedidos()); n != 0 {
		t.Errorf("chamou o provedor %d vez(es) antes de recusar", n)
	}
	if _, err := os.Stat(a.relatorio); err == nil {
		t.Error("gravou relatório de uma rodada recusada")
	}
}

// A estimativa exatamente igual ao limite não recusa; um fio abaixo, sim.
func TestRun_EstimativaIgualAoLimitePassa(t *testing.T) {
	est := 14 * custoDe(ia.Tokens{Entrada: tokensEntradaEstimados, Saida: tokensSaidaEstimados}, Preco{Entrada: 1000, Saida: 10000})
	igual := strconv.FormatFloat(est, 'g', -1, 64)
	a, args := flagsDaEstimativa(t, "-limite-usd", igual)
	if cod := a.rodar(args...); cod == 2 {
		t.Fatalf("recusou com a estimativa (%s) igual ao limite\n%s", igual, a.stderr.String())
	}
	abaixo := strconv.FormatFloat(math.Nextafter(est, 0), 'g', -1, 64)
	a, args = flagsDaEstimativa(t, "-limite-usd", abaixo)
	if cod := a.rodar(args...); cod != 2 {
		t.Fatalf("não recusou com o limite (%s) um fio abaixo da estimativa (%s): código %d", abaixo, igual, cod)
	}
}

func TestRun_LimiteAltoLiberaAEstimativaAlta(t *testing.T) {
	a, args := flagsDaEstimativa(t, "-limite-usd", "38")
	if cod := a.rodar(args...); cod == 2 {
		t.Fatalf("recusou com o limite acima da estimativa\n%s", a.stderr.String())
	}
}
