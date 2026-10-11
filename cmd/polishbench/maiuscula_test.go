package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// A métrica "maiúscula a mais" é só medida. Estes testes provocam cada jeito de
// a saída pôr maiúscula onde o esperado não põe, e cada jeito de NÃO contar:
// um alarme falso contaminaria o número que o mantenedor vai ler para saber se a
// regra de maiúscula do prompt funcionou.

func TestMaiusculasAMais(t *testing.T) {
	casos := []struct {
		nome     string
		esperado string
		saida    string
		quero    []string
	}{
		{
			// O caso real de 30-set-2026, com frase sintética.
			"mês e substantivo comum no meio da frase",
			"Os pendentes são a DCTFWeb de agosto, o PGDAS da padaria e o recibo da ECF.",
			"Os pendentes são 1º DCTFWeb de Agosto, 2º PGDAS da Padaria e 3º Recibo da ECF.",
			[]string{"Agosto", "Padaria", "Recibo"},
		},
		{
			"item de lista abre com maiúscula: não conta; o mês no meio do item conta",
			"Os pendentes da semana são:\n1. A DCTFWeb de agosto\n2. O PGDAS da padaria",
			"Os pendentes da semana são:\n1. A DCTFWeb de Agosto\n2. O PGDAS da Padaria",
			[]string{"Agosto", "Padaria"},
		},
		{
			"lista com traço",
			"Preciso de:\n- O contrato social\n- A folha de pagamento",
			"Preciso de:\n- O Contrato Social\n- A Folha de Pagamento",
			[]string{"Contrato", "Social", "Folha", "Pagamento"},
		},
		{
			"tudo certo: nada conta",
			"Os pendentes são a DCTFWeb de agosto, o PGDAS da padaria e o recibo da ECF.",
			"Os pendentes são a DCTFWeb de agosto, o PGDAS da padaria e o recibo da ECF.",
			nil,
		},
		{
			"primeira palavra do texto não conta, mesmo que o esperado a tenha em minúscula",
			"agosto vence hoje",
			"Agosto vence hoje",
			nil,
		},
		{
			"depois de ponto, exclamação, interrogação e reticências não conta",
			"fechei o balancete. mandei? sim! depois… volto",
			"Fechei o balancete. Mandei? Sim! Depois… Volto",
			nil,
		},
		{
			"depois de dois pontos, na mesma linha, não conta",
			"ele disse: sim, pode mandar",
			"Ele disse: Sim, pode mandar",
			nil,
		},
		{
			"depois de quebra de linha e de parágrafo não conta",
			"primeiro assunto\n\nsegundo assunto agora",
			"Primeiro assunto\n\nSegundo assunto Agora",
			[]string{"Agora"},
		},
		{
			"nome próprio, sigla e termo que o esperado já capitaliza não contam",
			"Liga para a Débora e confirma o DAS do Simples Nacional em Curitiba.",
			"Liga para a Débora e confirma o DAS do Simples Nacional em Curitiba.",
			nil,
		},
		{
			"o contrário (minúscula onde o esperado capitaliza) não é esta métrica",
			"Liga para a Débora e confirma o DAS.",
			"Liga para a débora e confirma o das.",
			nil,
		},
		{
			"palavra repetida: o alinhamento casa cada uma com a sua",
			"o recibo e o contrato do cliente",
			"O recibo e o Contrato do Cliente",
			[]string{"Contrato", "Cliente"},
		},
		{
			"acento na inicial",
			"ele achou o último lançamento",
			"Ele achou o Último lançamento",
			[]string{"Último"},
		},
		{
			"palavra toda em maiúscula no meio também conta (inicial maiúscula)",
			"a guia vence em agosto e vale muito",
			"A guia vence em AGOSTO e vale muito",
			[]string{"AGOSTO"},
		},
		{
			"número e ordinal não são maiúscula",
			"pagou 12 guias no 1º mês de 2026",
			"Pagou 12 guias no 1º mês de 2026",
			nil,
		},
		{
			"sem par, mas o esperado tem a palavra sempre em minúscula: conta",
			"vence em agosto",
			"vence em Agosto Agosto",
			[]string{"Agosto", "Agosto"},
		},
		{
			"sem par e a palavra não está no esperado: é acréscimo, não conta aqui",
			"vence em agosto",
			"vence em Agosto Nome",
			[]string{"Agosto"},
		},
		{
			"sem par e o esperado a tem também capitalizada: incerto, não conta",
			"a Rosa comprou a rosa hoje",
			"a Rosa comprou a rosa hoje Rosa",
			nil,
		},
		{
			"depois de dois pontos logo na primeira palavra",
			"atenção: hoje vence",
			"Atenção: Hoje vence",
			nil,
		},
		{
			"a mesma palavra com e sem maiúscula no esperado: o alinhamento decide qual é qual",
			"a Rosa comprou a rosa hoje",
			"a Rosa comprou a Rosa hoje",
			[]string{"Rosa"},
		},
		{
			"palavra da saída casada com a primeira palavra do esperado",
			"rosa comprou a Rosa",
			"a Rosa comprou a Rosa",
			[]string{"Rosa"},
		},
		{"esperado vazio", "", "Agosto vence", nil},
		{"saída vazia", "agosto vence", "", nil},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := maiusculasAMais(c.esperado, c.saida)
			if !reflect.DeepEqual(got, c.quero) {
				t.Errorf("maiusculasAMais =\n %q\nquero\n %q\nesperado: %q\nsaída:    %q", got, c.quero, c.esperado, c.saida)
			}
		})
	}
}

// casosDeCaixa é um dataset de três frases: uma de lista, uma de valores e uma
// já organizada (que vem do manifest do ASR, frase ditada de verdade).
func casosDeCaixa() []Caso {
	return []Caso{
		{ID: "lista-a", Categoria: CatLista, Entrada: "os pendentes são primeiro a DCTFWeb de agosto segundo o PGDAS da padaria", Esperado: "Os pendentes são:\n1. A DCTFWeb de agosto\n2. O PGDAS da padaria", Termos: []string{"DCTFWeb", "PGDAS"}},
		{ID: "valores-b", Categoria: CatValores, Entrada: "o recibo do DAS vence dia 20 e custa R$ 850", Esperado: "O recibo do DAS vence dia 20 e custa R$ 850.", Termos: []string{"DAS"}},
		{ID: prefixoJaOrganiza + "c", Categoria: CatJaOrganizada, Entrada: "Depois do almoço eu volto e a gente termina isso.", Esperado: "Depois do almoço eu volto e a gente termina isso."},
	}
}

func TestCalcular_MaiusculaAMais(t *testing.T) {
	casos := casosDeCaixa()
	rs := []Resultado{
		resultadoDe(casos[0], 0, 1, "Os pendentes são:\n1. A DCTFWeb de Agosto\n2. O PGDAS da Padaria", time.Second, ia.Tokens{}), // 2 palavras
		resultadoDe(casos[0], 0, 2, casos[0].Esperado, time.Second, ia.Tokens{}),                                                  // certa
		resultadoDe(casos[1], 1, 1, "O Recibo do DAS vence dia 20 e custa R$ 850.", time.Second, ia.Tokens{}),                     // 1 palavra
		resultadoDe(casos[2], 2, 1, "Depois do Almoço eu volto e a gente termina isso.", time.Second, ia.Tokens{}),                // 1 palavra, já organizada
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})

	if m.Sucessos != 4 {
		t.Fatalf("Sucessos = %d, quero 4", m.Sucessos)
	}
	if m.CaixaAMaisChamadas != 3 || m.CaixaAMaisPalavras != 4 {
		t.Errorf("maiúscula a mais = %d chamadas / %d palavras, quero 3 e 4", m.CaixaAMaisChamadas, m.CaixaAMaisPalavras)
	}
	for cat, quero := range map[string][2]int{CatLista: {1, 2}, CatValores: {1, 1}, CatJaOrganizada: {1, 1}} {
		c := m.PorCategoria[cat]
		if c == nil || c.CaixaAMaisChamadas != quero[0] || c.CaixaAMaisPalavras != quero[1] {
			t.Errorf("categoria %s: %+v, quero %d chamada(s) e %d palavra(s)", cat, c, quero[0], quero[1])
		}
	}

	// Exemplos: as duas frases sintéticas, uma vez cada; a já organizada conta na
	// métrica mas não vira exemplo (é frase ditada de verdade).
	var xs []Exemplo
	for _, e := range m.Exemplos {
		if e.Tipo == ExemploMaiusculaAMais {
			xs = append(xs, e)
		}
	}
	if len(xs) != 2 {
		t.Fatalf("exemplos de maiúscula a mais = %d, quero 2: %+v", len(xs), xs)
	}
	if xs[0].CasoID != "lista-a" || !reflect.DeepEqual(xs[0].Palavras, []string{"Agosto", "Padaria"}) {
		t.Errorf("primeiro exemplo = %+v", xs[0])
	}
	if xs[1].CasoID != "valores-b" || !reflect.DeepEqual(xs[1].Palavras, []string{"Recibo"}) {
		t.Errorf("segundo exemplo = %+v", xs[1])
	}
	for _, e := range m.Exemplos {
		if e.Categoria == CatJaOrganizada && e.Tipo == ExemploMaiusculaAMais {
			t.Errorf("exemplo de frase ja_organizada (real) no relatório: %+v", e)
		}
	}
}

// A métrica mede o texto do MODELO, antes da guarda: uma saída que a guarda
// recusa (aqui, o número trocado) e que o Agent trocaria pelo texto do Smart,
// em minúscula, conta do mesmo jeito. O que interessa é se o prompt fez o modelo
// acertar a maiúscula.
func TestCalcular_MaiusculaAMaisMedeOTextoDoModeloMesmoRecusadoPelaGuarda(t *testing.T) {
	casos := casosDeCaixa()
	r := resultadoDe(casos[1], 1, 1, "O Recibo do DAS vence dia 21 e custa R$ 850.", time.Second, ia.Tokens{})
	if r.Res.Reescrito {
		t.Fatalf("a guarda devia recusar o número trocado: %+v", r.Res)
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: []Resultado{r}})
	if m.CaixaAMaisChamadas != 1 || m.CaixaAMaisPalavras != 1 {
		t.Errorf("maiúscula a mais = %d/%d, quero 1/1 (o texto do modelo, não o do Smart)", m.CaixaAMaisChamadas, m.CaixaAMaisPalavras)
	}
}

// Nome próprio que o esperado capitaliza e a entrada (whisper) escreve em
// minúscula: o modelo que o capitaliza acertou, e não é maiúscula a mais.
func TestCalcular_MaiusculaAMaisNomeProprioCertoNaoConta(t *testing.T) {
	casos := []Caso{{ID: "nome", Categoria: CatArmadilha, Entrada: "liga para a debora e confirma o balancete", Esperado: "Liga para a Debora e confirma o balancete."}}
	r := resultadoDe(casos[0], 0, 1, casos[0].Esperado, time.Second, ia.Tokens{})
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: []Resultado{r}})
	if m.Sucessos != 1 || m.CaixaAMaisChamadas != 0 || m.CaixaAMaisPalavras != 0 {
		t.Errorf("Sucessos=%d maiúscula a mais=%d/%d, quero 1 e 0/0", m.Sucessos, m.CaixaAMaisChamadas, m.CaixaAMaisPalavras)
	}
}

// Texto estranho (vazio, só pontuação, moeda solta, marcador sem item, emoji,
// tudo em maiúscula) nunca derruba a métrica, e o que ela devolve são sempre
// palavras da saída.
func TestMaiusculasAMais_TextoEstranhoNaoQuebra(t *testing.T) {
	textos := []string{
		"", " ", ":", "...", "R$", "R$ 1500", "\n\n\n", "- ", "1. ", "É", "ÁGUA ÁGUA", "a:b c:D", "😀 Ola mundo",
		"- item\n- Item", "o Dia 5: Sim", "a  b   C", "Nome. Outro nome? Sim!",
	}
	for _, esperado := range textos {
		for _, saida := range textos {
			palavras := map[string]bool{}
			for _, p := range palavrasDe(saida) {
				palavras[p.Bruta] = true
			}
			for _, w := range maiusculasAMais(esperado, saida) {
				if !palavras[w] || !inicialMaiuscula(w) {
					t.Errorf("maiusculasAMais(%q, %q) devolveu %q, que não é palavra da saída com inicial maiúscula", esperado, saida, w)
				}
			}
		}
	}
}

// A métrica mede o que o MODELO devolveu, antes da guarda, e só nas chamadas em
// que houve final_text: a que não foi ao provedor ou não devolveu texto não
// conta.
func TestCalcular_MaiusculaAMaisSoNasChamadasComTexto(t *testing.T) {
	casos := casosDeCaixa()
	rs := []Resultado{
		resultadoDe(casos[1], 1, 1, "O Recibo do DAS vence dia 20 e custa R$ 850.", time.Second, ia.Tokens{}),
		{Caso: 1, Rodada: 2, Enviada: true, Duracao: 50 * time.Millisecond}, // erro sem resposta
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.Chamadas != 2 || m.Sucessos != 1 || m.CaixaAMaisChamadas != 1 || m.CaixaAMaisPalavras != 1 {
		t.Errorf("Chamadas=%d Sucessos=%d CaixaAMais=%d/%d, quero 2, 1 e 1/1", m.Chamadas, m.Sucessos, m.CaixaAMaisChamadas, m.CaixaAMaisPalavras)
	}
}

// A métrica é só medida: um modelo que capitaliza mês e substantivo comum em
// todas as respostas, com a guarda aceitando (ela não barra isso), passa na
// regra de escolha do mesmo jeito que passaria sem a maiúscula.
func TestMaiusculaAMais_NaoEntraNaRegraDeEscolha(t *testing.T) {
	casos := casosDeCaixa()
	cfg := cfgDe(casos)
	saidaCaixa := "O Recibo do DAS vence dia 20 e custa R$ 850."
	comCaixa := []Resultado{
		resultadoDe(casos[1], 1, 1, saidaCaixa, time.Second, ia.Tokens{Entrada: 1000, Saida: 50}),
		resultadoDe(casos[1], 1, 2, saidaCaixa, time.Second, ia.Tokens{Entrada: 1000, Saida: 50}),
	}
	semCaixa := []Resultado{
		resultadoDe(casos[1], 1, 1, casos[1].Esperado, time.Second, ia.Tokens{Entrada: 1000, Saida: 50}),
		resultadoDe(casos[1], 1, 2, casos[1].Esperado, time.Second, ia.Tokens{Entrada: 1000, Saida: 50}),
	}
	cand := Candidato{Provedor: "openai", Modelo: "m"}
	crit := Criterios{FallbackMax: 0.05, P95Max: 2500 * time.Millisecond}

	mCaixa := calcular(cfg, SaidaCandidato{Cand: cand, Preco: Preco{1, 1}, Resultados: comCaixa})
	mLimpa := calcular(cfg, SaidaCandidato{Cand: cand, Preco: Preco{1, 1}, Resultados: semCaixa})
	if mCaixa.CaixaAMaisChamadas != 2 || mLimpa.CaixaAMaisChamadas != 0 {
		t.Fatalf("CaixaAMaisChamadas = %d (com) e %d (sem), quero 2 e 0", mCaixa.CaixaAMaisChamadas, mLimpa.CaixaAMaisChamadas)
	}
	dCaixa, dLimpa := escolher([]Metricas{mCaixa}, crit, false), escolher([]Metricas{mLimpa}, crit, false)
	if !dLimpa.Vereditos[0].Passou {
		t.Fatalf("o controle (sem a maiúscula) devia passar: %+v", dLimpa.Vereditos[0])
	}
	if !reflect.DeepEqual(dCaixa, dLimpa) {
		t.Errorf("a maiúscula a mais mudou a decisão:\n com: %+v\n sem: %+v", dCaixa, dLimpa)
	}
}

func TestGerarRelatorio_MaiusculaAMais(t *testing.T) {
	r := rodadaDeTeste(t)
	// Um modelo que capitaliza: acrescenta as métricas de um terceiro candidato.
	casos := casosDeCaixa()
	cfg := cfgDe(casos)
	rs := []Resultado{
		resultadoDe(casos[0], 0, 1, "Os pendentes são:\n1. A DCTFWeb de Agosto\n2. O PGDAS da Padaria", time.Second, ia.Tokens{}),
		resultadoDe(casos[1], 1, 1, casos[1].Esperado, time.Second, ia.Tokens{}),
		resultadoDe(casos[2], 2, 1, "Depois do Almoço eu volto e a gente termina isso.", time.Second, ia.Tokens{}),
		// Uma chamada sem final_text (erro do provedor): entra nas chamadas, mas não
		// no denominador da métrica, que só olha o que o modelo escreveu.
		{Caso: 1, Rodada: 2, Enviada: true, Duracao: 50 * time.Millisecond, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoSobrecarga}},
	}
	cand := Candidato{Provedor: "openai", Modelo: "caixa-alta"}
	r.Candidatos = append(r.Candidatos, cand)
	r.Metricas = append(r.Metricas, calcular(cfg, SaidaCandidato{Cand: cand, Preco: Preco{1, 1}, Resultados: rs}))
	rel := gerarRelatorio(r)

	contem(t, rel, "## Maiúscula a mais")
	contem(t, rel, "**Só medida: não entra na regra de escolha.**")
	// Linha da tabela por modelo: chamadas com alguma (das 3 com texto) / palavras.
	if got := celula(t, rel, "Maiúscula a mais no meio da frase", 3); got != "2/3 (66,7%) / 3" {
		t.Errorf("linha da tabela por modelo = %q, quero 2/3 (66,7%%) / 3", got)
	}
	// Os modelos que não maiusculam mostram 0.
	if got := celula(t, rel, "Maiúscula a mais no meio da frase", 1); !strings.HasPrefix(got, "0/") {
		t.Errorf("modelo sem maiúscula a mais = %q, quero 0/...", got)
	}
	// Por categoria (seção própria).
	if got := celula(t, rel, "Todas as categorias", 3); got != "2/3 (66,7%) / 3" {
		t.Errorf("seção, todas as categorias = %q", got)
	}
	// "lista" também aparece em outra linha da tabela por modelo: procura só
	// dentro da seção da métrica.
	secao := rel[strings.Index(rel, "## Maiúscula a mais"):]
	secao = secao[:strings.Index(secao[3:], "\n## ")+3]
	if got := celula(t, secao, "lista", 3); !strings.HasPrefix(got, "1 / 2 (1)") {
		t.Errorf("seção, categoria lista = %q, quero 1 / 2 (1)", got)
	}
	// Exemplos: só a frase sintética, com as palavras contadas; a já organizada
	// (frase real) não vira exemplo.
	contem(t, rel, "### `openai:caixa-alta`: maiúscula a mais no meio da frase (só medida)")
	contem(t, rel, "maiúscula a mais: `Agosto, Padaria`")
	contem(t, rel, "saída: `Os pendentes são:⏎1. A DCTFWeb de Agosto⏎2. O PGDAS da Padaria`")
	naoContem(t, rel, "Depois do Almoço")
	// O relatório sem nenhum modelo com maiúscula a mais mantém a frase antiga
	// do "nenhum exemplo" ampliada.
	naoContem(t, gerarRelatorio(rodadaDeTeste(t)), "maiúscula a mais: `")
}

// O dataset é a régua da métrica: se o `esperado` tivesse mês, dia da semana ou
// substantivo comum com maiúscula no meio da frase, o modelo que acertasse seria
// contado como errado. Nas frases sintéticas, toda palavra em maiúscula no meio
// da frase do `esperado` tem de ser nome próprio (pessoa, cidade, país), sigla ou
// nome de tributo (IRPJ, PIS, Cofins, CSLL) ou termo do dicionário da própria
// frase (campo `termos`, onde entram "Simples Nacional" e "Lucro Presumido",
// os nomes dos regimes, na grafia canônica do dicionário). As da categoria
// ja_organizada vêm do manifest do ASR (frases ditadas de verdade, com nomes de
// pessoas, de empresas e de produtos, e uma frase em inglês) e ficam de fora.
func TestDataset_EsperadoSemMaiusculaNoMeioForaDeNomeSiglaETermo(t *testing.T) {
	nomesESiglas := map[string]bool{
		"Rosângela": true, "Thiago": true, "Débora": true, "Debora": true, "Curitiba": true, "França": true,
		"IRPJ": true, "PIS": true, "Cofins": true, "CSLL": true,
	}
	mesesEDias := []string{
		"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
		"segunda", "terça", "quarta", "quinta", "sexta", "sábado", "domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira",
	}
	var conferidos int
	for _, c := range datasetReal(t) {
		if c.Categoria == CatJaOrganizada {
			continue
		}
		termos := map[string]bool{}
		for _, termo := range c.Termos {
			for _, p := range strings.Fields(termo) {
				termos[p] = true
			}
		}
		ps := palavrasDe(c.Esperado)
		meio := noMeioDaFrase(ps)
		for i, p := range ps {
			if !meio[i] || !inicialMaiuscula(p.Bruta) {
				continue
			}
			conferidos++
			for _, mes := range mesesEDias {
				if strings.EqualFold(p.Bruta, mes) {
					t.Errorf("%s: o esperado escreve %q com maiúscula no meio da frase (mês ou dia da semana vai em minúscula)\n esperado: %q", c.ID, p.Bruta, c.Esperado)
				}
			}
			if !nomesESiglas[p.Bruta] && !termos[p.Bruta] {
				t.Errorf("%s: o esperado escreve %q com maiúscula no meio da frase e a palavra não é nome próprio conhecido nem termo da frase (`termos`); se for substantivo comum, o esperado está errado\n esperado: %q", c.ID, p.Bruta, c.Esperado)
			}
		}
	}
	if conferidos < 30 {
		t.Fatalf("conferi só %d palavras: o teste perdeu o dataset?", conferidos)
	}
}
