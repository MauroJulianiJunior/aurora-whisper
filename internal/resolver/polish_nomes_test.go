package resolver

import "testing"

// A emenda de 19-out-2026 da ADR-016: a palavra comum que o Whisper escreveu
// com maiúscula no meio da frase pode voltar à minúscula; o nome de pessoa,
// o nome que o VERO conhece e a sigla, não.
func TestGuardaSiglasTermos_MaiusculaDoWhisper(t *testing.T) {
	casos := []struct {
		nome, entrada, saida string
		passa                bool
	}{
		// O que o diagnóstico do histórico achou.
		{"Base de Conhecimento", "consulte a Base de Conhecimento antes", "Consulte a base de conhecimento antes.", true},
		{"Pesquisa", "a gente faz a Pesquisa e depois", "A gente faz a pesquisa e depois.", true},
		{"Arroz", "o preço do Arroz subiu", "O preço do arroz subiu.", true},
		{"Engenharia", "a área de Engenharia pediu", "A área de engenharia pediu.", true},
		{"com a maiúscula mantida", "a área de Engenharia pediu", "A área de Engenharia pediu.", true},

		// Nome de pessoa, mesmo que o VERO o tenha como palavra comum.
		{"João", "o João pediu a guia", "O joão pediu a guia.", false},
		{"Pedro", "falei com o Pedro ontem", "Falei com o pedro ontem.", false},
		{"Silva", "a guia do Silva venceu", "A guia do silva venceu.", false},
		{"Rosa", "a Rosa mandou o recibo", "A rosa mandou o recibo.", false},
		{"Maria", "a Maria mandou o recibo", "A maria mandou o recibo.", false},
		// Cidade que o VERO tem como palavra comum.
		{"Recife", "a filial de Recife fechou", "A filial de recife fechou.", false},
		// O nome que o VERO conhece com maiúscula, fora das listas.
		{"China", "o fornecedor da China atrasou", "O fornecedor da china atrasou.", false},
		// Nome que não existe como palavra.
		{"Anthropic", "a chave da Anthropic venceu", "A chave da anthropic venceu.", false},
		// A grafia do nome continua sem troca.
		{"nome corrigido", "a chave da Antropic venceu", "A chave da Anthropic venceu.", false},
		// Sigla continua estrita.
		{"sigla em caixa alta", "o DAS vence hoje", "O das vence hoje.", false},
		{"sigla de ênfase", "CRIE uma pasta nova", "Crie uma pasta nova.", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := guardaSiglasTermos(c.entrada, c.saida, nil); got != c.passa {
				t.Errorf("guardaSiglasTermos(%q, %q) = %v, quero %v", c.entrada, c.saida, got, c.passa)
			}
		})
	}
}
