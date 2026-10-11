package main

import "testing"

// De ponta a ponta: um modelo que capitaliza "Guia" e "Recibo" no meio do item
// aparece na métrica, e a rodada segue passando pela regra de escolha (a
// maiúscula a mais é só medida, e a guarda a deixa passar).
func TestRun_ModeloQueCapitalizaMedeMaiusculaAMaisEContinuaPassando(t *testing.T) {
	frases, refs := datasetPequeno()
	casos := append(append([]Caso{}, frases...), refs...)
	certo := comOraculo(casos)
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa {
		if p.Ditado == "os itens são primeiro a guia segundo o recibo" {
			return respostaFalsa{Final: "Os itens são:\n1. A Guia\n2. O Recibo"}
		}
		return certo(p)
	}, nil)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-rodadas", "2", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()

	// 7 frases vão ao provedor (a curta não), 2 rodadas: 14 chamadas; a de lista
	// capitaliza 2 palavras nas 2 rodadas.
	if got := celula(t, rel, "Maiúscula a mais no meio da frase", 1); got != "2/14 (14,3%) / 4" {
		t.Errorf("maiúscula a mais = %q, quero 2/14 (14,3%%) / 4", got)
	}
	contem(t, rel, "### `openai:m1`: maiúscula a mais no meio da frase (só medida)")
	contem(t, rel, "`lista-1` (lista, rodada 1) — maiúscula a mais: `Guia, Recibo`")
	contem(t, rel, "- **OpenAI:** `openai:m1`.")
	if got := celula(t, rel, "Regra de escolha", 1); got != "**passou**" {
		t.Errorf("regra de escolha = %q: a maiúscula a mais não pode reprovar", got)
	}
}
