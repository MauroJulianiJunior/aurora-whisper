package main

import (
	"strings"
	"testing"
)

// O comando que o -help e o relato entregam ao mantenedor tem de rodar: aqui ele é
// executado de verdade (contra os provedores falsos, em loopback), com as flags
// exatamente como impressas. Se uma flag mudar de nome, o comando impresso deixa
// de ser válido e este teste avisa.
func TestComandoSoLuna_RodaComAsFlagsImpressas(t *testing.T) {
	const prefixo = "go run ./cmd/polishbench "
	if !strings.HasPrefix(comandoSoLuna, prefixo) {
		t.Fatalf("comandoSoLuna = %q", comandoSoLuna)
	}
	flags := strings.Fields(strings.TrimPrefix(comandoSoLuna, prefixo))
	quero := []string{"-gastar", "-rodadas", "3", "-modelos", "openai:gpt-6-luna", "-conexao-nova"}
	if strings.Join(flags, " ") != strings.Join(quero, " ") {
		t.Fatalf("flags do comando = %v, quero %v", flags, quero)
	}
	if strings.Contains(comandoSoLuna, "&&") {
		t.Error("o PowerShell 5.1 não tem &&")
	}

	casos := datasetReal(t)
	a := novoAmbiente(t, comOraculo(casos), comOraculo(casos))
	cod := a.rodar(juntar(datasetRealFlags(), flags)...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	// Só o luna, com a conexão nova a cada chamada, sem linha de base e sem os
	// outros modelos da rodada completa.
	contem(t, rel, "-gastar -rodadas 3 -modelos openai:gpt-6-luna -conexao-nova")
	contem(t, rel, "**nova a cada chamada**")
	contem(t, rel, "`openai:gpt-6-luna` (OpenAI)")
	naoContem(t, rel, "gpt-5-nano")
	naoContem(t, rel, "claude-haiku")
	naoContem(t, rel, "linha de base: medido")
	if len(a.anthropic.Pedidos()) != 0 {
		t.Error("o comando só do luna chamou a Anthropic")
	}
	if got := a.leitor.Prompts(); len(got) != 1 || got[0] != promptOpenAI {
		t.Errorf("chaves pedidas = %v, quero só a da OpenAI", got)
	}
}

// O -help entrega o comando só do luna, ao lado do da rodada completa.
func TestRun_AjudaTrazOComandoSoDoLuna(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	if cod := run(a.ctx, []string{"-help"}, &a.stdout, &a.stderr, a.deps()); cod != 0 {
		t.Fatalf("-help saiu com %d", cod)
	}
	ajuda := a.stderr.String()
	contem(t, ajuda, "\n  "+comandoSoLuna+"\n")
	contem(t, ajuda, "maiúscula a mais")
	contem(t, ajuda, "go run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna,openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5")
}
