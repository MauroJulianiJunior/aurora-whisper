package main

import (
	"reflect"
	"testing"
)

// A correção de grafia na auditoria (ADR-016, emenda de 18-out-2026): a
// correção que a guarda aceita não pode sair como "buraco na guarda", e a troca
// que a guarda recusa continua crítica.
func TestAuditar_CorrecaoDeGrafia(t *testing.T) {
	corrigido := c(CatGrafia, "a alíquota vejante do serviço continua a mesma", "A alíquota vigente do serviço continua a mesma.")
	literal := c(CatGrafia, "a alíquota vejante do serviço continua a mesma", "A alíquota vejante do serviço continua a mesma.")
	separa := c(CatGrafia, "derepente o cliente mandou as notas", "Derepente, o cliente mandou as notas.")
	duasPalavras := c(CatGrafia, "vou mandar o e mail com a planilha", "Vou mandar o e mail com a planilha.")
	existe := c(CatGrafia, "os lançamentos contáveis da filial fecham amanhã", "Os lançamentos contáveis da filial fecham amanhã.")
	esocial := c(CatGrafia, "o esocial do cliente caiu de novo", "O esocial do cliente caiu de novo.")
	nunka := c(CatGrafia, "o cliente nunka mandou o extrato", "O cliente nunka mandou o extrato.")
	nome := c(CatGrafia, "liga para a debora e confirma", "Liga para a debora e confirma.")
	tres := c(CatGrafia, "antes do fechamente a gente confere as despezas do mês",
		"Antes do fechamento, a gente confere as despesas do mês.")

	casos := []struct {
		nome     string
		caso     Caso
		saida    string
		criticas []TipoFalha
		grafia   bool
	}{
		{"o modelo e o esperado corrigem igual", corrigido, corrigido.Esperado, nil, false},
		{"o esperado corrigiu e o modelo manteve o texto do Smart", corrigido, "A alíquota vejante do serviço continua a mesma.", nil, true},
		{"o modelo corrigiu o que o esperado manteve (a guarda aceita)", literal, "A alíquota vigente do serviço continua a mesma.", nil, true},
		{"uma palavra que não existe vira duas", separa, "De repente, o cliente mandou as notas.", nil, true},
		{"duas palavras viram uma com as mesmas letras", duasPalavras, "Vou mandar o e-mail com a planilha.", nil, true},
		{"três correções na mesma frase", tres, "Antes do fechamente, a gente confere as despezas do mês.", nil, true},

		{"a correção para outra palavra que não a do esperado é acréscimo", corrigido, "A alíquota vidente do serviço continua a mesma.", []TipoFalha{FalhaAcrescimo}, false},
		{"a correção longe demais na escrita é acréscimo", literal, "A alíquota vidente do serviço continua a mesma.", []TipoFalha{FalhaAcrescimo}, false},
		{"duas palavras que existem não viram uma", c(CatGrafia, "depois a gente confere o extrato", "Depois a gente confere o extrato."), "Depois agente confere o extrato.", []TipoFalha{FalhaAcrescimo}, false},
		{"a palavra que existe não vira duas", c(CatGrafia, "o agente do banco ligou ontem", "O agente do banco ligou ontem."), "O a gente do banco ligou ontem.", []TipoFalha{FalhaAcrescimo}, false},
		{"a palavra que existe não é corrigida", existe, "Os lançamentos contábeis da filial fecham amanhã.", []TipoFalha{FalhaAcrescimo}, false},
		{"a correção que muda a primeira letra é outra palavra", esocial, "O social do cliente caiu de novo.", []TipoFalha{FalhaAcrescimo}, false},
		{"a correção que acrescenta negação é acréscimo", nunka, "O cliente nunca mandou o extrato.", []TipoFalha{FalhaAcrescimo}, false},
		{"o nome não é grafia", nome, "Liga para a Débora e confirma.", []TipoFalha{FalhaAcrescimo}, false},
		{"a palavra certa tem de existir", literal, "A alíquota vejantte do serviço continua a mesma.", []TipoFalha{FalhaAcrescimo}, false},
		{"a palavra apagada num lugar não vira a acrescentada em outro", literal, "A alíquota do serviço continua a mesma vigente.", []TipoFalha{FalhaAcrescimo}, false},
		{"a palavra errada tem de vir da entrada", corrigido, "A alíquota vigentte do serviço continua a mesma.", []TipoFalha{FalhaAcrescimo}, false},
		{"palavra curta: duas letras de diferença é outra palavra", c(CatGrafia, "o bulito venceu ontem à tarde", "O bulito venceu ontem à tarde."), "O boleto venceu ontem à tarde.", []TipoFalha{FalhaAcrescimo}, false},
		{"a palavra que se separa não ganha negação", c(CatGrafia, "o cliente nuncamais mandou o extrato", "O cliente nuncamais mandou o extrato."), "O cliente nunca mais mandou o extrato.", []TipoFalha{FalhaAcrescimo}, false},
		{"duas palavras não viram uma outra qualquer", duasPalavras, "Vou mandar o anexo com a planilha.", []TipoFalha{FalhaAcrescimo}, false},
		{"o acento que faltava na negação passa", c(CatGrafia, "o cliente ainda nao mandou o extrato", "O cliente ainda nao mandou o extrato."), "O cliente ainda não mandou o extrato.", nil, true},
		{"o numeral não é grafia", c(CatGrafia, "o cliente mandou tres notas fiscais", "O cliente mandou tres notas fiscais."), "O cliente mandou três notas fiscais.", []TipoFalha{FalhaNumero}, false},
	}
	for _, k := range casos {
		// A guarda de verdade tem de concordar: a saída sem falha crítica passa
		// nela, e a com falha crítica cai no Smart.
		if res, _ := polirCom(k.caso, k.saida); res.Reescrito != (len(k.criticas) == 0) {
			t.Errorf("%s: a guarda e a auditoria discordam (a guarda aceitou = %v, motivo %q, críticas %v)", k.nome, res.Reescrito, res.MotivoFallback, k.criticas)
		}
		got := auditar(k.caso, k.saida)
		if !reflect.DeepEqual(got.Criticas, k.criticas) && !(len(got.Criticas) == 0 && len(k.criticas) == 0) {
			t.Errorf("%s:\n saída:  %q\n críticas = %v, quero %v", k.nome, k.saida, got.Criticas, k.criticas)
		}
		if got.Grafia != k.grafia {
			t.Errorf("%s:\n saída:  %q\n Grafia = %v, quero %v", k.nome, k.saida, got.Grafia, k.grafia)
		}
		if k.grafia && (got.Sobra || got.Omissao) {
			t.Errorf("%s: a correção de grafia saiu também como sobra (%v) ou omissão (%v)", k.nome, got.Sobra, got.Omissao)
		}
	}
}

// Nas frases da categoria grafia, o modelo que devolve o texto do Smart sem
// corrigir não tem falha crítica, só a correção que faltou (leve); a
// grafia-09, em que o esperado mantém a palavra que existe, nem isso.
func TestDatasetReal_GrafiaSemCorrigirELeve(t *testing.T) {
	vistas := 0
	for _, k := range datasetReal(t) {
		if k.Categoria != CatGrafia {
			continue
		}
		vistas++
		got := auditar(k, k.Entrada)
		if got.Critica() {
			t.Errorf("%s: a entrada sem correção saiu com falha crítica %v", k.ID, got.Criticas)
		}
		querGrafia := k.ID != "grafia-09"
		if got.Grafia != querGrafia {
			t.Errorf("%s: Grafia = %v, quero %v", k.ID, got.Grafia, querGrafia)
		}
		if got := auditar(k, k.Esperado); got.Critica() || got.Grafia {
			t.Errorf("%s: o próprio esperado saiu com %v (Grafia %v)", k.ID, got.Criticas, got.Grafia)
		}
	}
	if vistas == 0 {
		t.Fatal("o dataset não tem a categoria grafia")
	}
}
