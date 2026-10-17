package main

import (
	"strings"
	"testing"
)

// Cada tipo de mudança que o modelo faz num ditado vira uma diferença com a
// categoria certa; pontuação e caixa não contam.
func TestDiferencas_Categorias(t *testing.T) {
	casos := []struct {
		nome, entrada, saida string
		quero                []string // categorias, na ordem
		linha                string   // trecho que a primeira linha tem de ter
	}{
		{"só pontuação e caixa", "preciso entregar o relatorio ate sexta", "Preciso entregar o relatorio ate sexta.", nil, ""},
		{"contração", "eu vou mandar pra você amanhã", "Eu vou mandar para você amanhã.", []string{DifContracao}, "[pra → para]"},
		{"acento", "o relatorio ficou pronto", "O relatório ficou pronto.", []string{DifAcento}, "[relatorio → relatório]"},
		{"filler apagado", "a gente vai né amanhã cedo", "A gente vai amanhã cedo.", []string{DifFiller}, "[- né]"},
		{"repetição em bloco", "a gente a gente foi lá", "A gente foi lá.", []string{DifRepeticao}, "[- a gente]"},
		{"hesitação", "ahn eu acho que sim", "Eu acho que sim.", []string{DifHesitacao}, "[- ahn]"},
		{"junção com hífen", "manda um e mail pra ele", "Manda um e-mail pra ele.", []string{DifJuncao}, "[e mail → e-mail]"},
		{"palavra trocada", "o resultado ficou bom demais", "O resultado ficou ótimo demais.", []string{DifTrocada}, "[bom → ótimo]"},
		{"palavra acrescentada", "ele pagou a conta ontem", "Ele pagou toda a conta ontem.", []string{DifAcrescida}, "[+ toda]"},
		{"duas mudanças", "eu vou pra lá e o relatorio fica", "Eu vou para lá, e o relatório fica.", []string{DifContracao, DifAcento}, "[pra → para]"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			difs := diferencas(c.entrada, c.saida)
			var cats []string
			for _, d := range difs {
				cats = append(cats, d.Categoria)
			}
			if strings.Join(cats, "|") != strings.Join(c.quero, "|") {
				t.Fatalf("categorias = %q, quero %q", cats, c.quero)
			}
			if c.linha != "" && !strings.Contains(difs[0].linha(), c.linha) {
				t.Errorf("linha = %q, quero que contenha %q", difs[0].linha(), c.linha)
			}
		})
	}
}

// O contexto em volta da mudança tem até 3 palavras de cada lado, da entrada.
func TestDiferencas_Contexto(t *testing.T) {
	difs := diferencas("um dois três quatro pra seis sete oito nove", "Um dois três quatro para seis sete oito nove.")
	if len(difs) != 1 {
		t.Fatalf("diferenças = %d, quero 1", len(difs))
	}
	if got := difs[0].linha(); got != "… dois três quatro [pra → para] seis sete oito …" {
		t.Errorf("linha = %q", got)
	}
}

// Uma resposta que o modelo não devolveu em JSON não gera diferença (o
// diagnóstico mostra só o desfecho).
func TestTextoDaSaida(t *testing.T) {
	if got := textoDaSaida([]byte(`{"final_text":"Olá."}`)); got != "Olá." {
		t.Errorf("textoDaSaida = %q", got)
	}
	if got := textoDaSaida([]byte(`não é json`)); got != "" {
		t.Errorf("textoDaSaida de lixo = %q, quero vazio", got)
	}
}
