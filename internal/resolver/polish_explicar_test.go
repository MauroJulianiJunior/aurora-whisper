package resolver

import (
	"strings"
	"testing"
)

func TestExplicarSiglasTermos(t *testing.T) {
	casos := []struct {
		nome, entrada, saida string
		termos               []string
		quero                []string
	}{
		{"passa", "o PGDAS vence hoje e a Ana manda", "O PGDAS vence hoje, e a Ana manda.", nil, nil},
		{"sigla em minúscula", "o PGDAS vence hoje", "O pgdas vence hoje.", nil,
			[]string{`sigla "PGDAS": 1 na entrada, 0 na saída; na saída, "pgdas" 1x`}},
		{"sigla a mais", "o DAS do DAS anterior", "O DAS do DAS anterior e o DAS novo.", nil,
			[]string{`sigla "DAS": 2 na entrada, 3 na saída`}},
		{"nome com acento", "falei com a Debora ontem", "Falei com a Débora ontem.", nil,
			[]string{`nome "Debora": 1 na entrada, 0 na saída`}},
		{"termo", "a dctfweb vence amanhã cedo", "A DCTFweb vence amanhã cedo.", []string{"DCTFWeb"},
			[]string{`termo "DCTFWeb": não reaparece com a grafia da entrada`}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := ExplicarSiglasTermos(c.entrada, c.saida, c.termos)
			if strings.Join(got, "|") != strings.Join(c.quero, "|") {
				t.Errorf("ExplicarSiglasTermos = %q, quero %q", got, c.quero)
			}
			// A explicação vazia é a guarda passando, e vice-versa.
			if (len(got) == 0) != guardaSiglasTermos(c.entrada, c.saida, c.termos) {
				t.Errorf("explicação %q discorda da guarda", got)
			}
		})
	}
}

func TestCorrecaoDeGrafiaAceita(t *testing.T) {
	casos := []struct {
		saiu, entrou []string
		quero        bool
	}{
		{[]string{"relatorio"}, []string{"relatório"}, true},
		{[]string{"e", "mail"}, []string{"e-mail"}, true},
		{[]string{"derepente"}, []string{"de", "repente"}, true},
		{[]string{"pagou"}, []string{"pegou"}, false},
		{[]string{"logo", "marca"}, []string{"logomarca"}, false},
		{[]string{"promptes"}, []string{"prompts"}, false},
		{[]string{"um", "relatorio"}, []string{"relatório"}, false},
	}
	for _, c := range casos {
		if got := CorrecaoDeGrafiaAceita(c.saiu, c.entrou); got != c.quero {
			t.Errorf("CorrecaoDeGrafiaAceita(%q, %q) = %v, quero %v", c.saiu, c.entrou, got, c.quero)
		}
	}
}
