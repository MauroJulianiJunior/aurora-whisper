package main

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// ---------------------------------------------------------------------
// Achado médio da revisão: a regra não tinha piso de organização nem linha de
// base, e um modelo "eco" (devolve a entrada) passava e vencia por ser barato.
// ---------------------------------------------------------------------

// metOrg monta as métricas de um candidato que respondeu, só com o que a
// organização precisa: F1 fora de ja_organizada por TP/FP/FN da categoria lista
// e o acerto de estrutura das listas.
func metOrg(prov, modelo string, custo1000 float64, tp, fp, fn, okLista, nLista int) Metricas {
	m := bom(prov, modelo, custo1000, 0.9)
	m.Sucessos = 100
	m.PorCategoria = map[string]*MetCategoria{
		CatLista:        {Chamadas: 50, TP: tp, FP: fp, FN: fn},
		CatJaOrganizada: {Chamadas: 50, TP: 500, FP: 0, FN: 0}, // o esperado é a entrada: todo modelo acerta
	}
	m.EstruturaListaOK, m.EstruturaListaN = okLista, nLista
	return m
}

func TestF1Organizando_NaoContaJaOrganizada(t *testing.T) {
	m := metOrg(ia.OpenAI, "x", 0.1, 0, 0, 40, 0, 10) // não acerta nenhum sinal fora de ja_organizada
	if got := m.F1Organizando(); got != 0 {
		t.Errorf("F1 fora de ja_organizada = %v, quero 0 (o sinal de ja_organizada não conta)", got)
	}
	m = metOrg(ia.OpenAI, "x", 0.1, 30, 10, 10, 0, 10) // precisão 0,75, revocação 0,75
	if got := m.F1Organizando(); !perto(got, 0.75) {
		t.Errorf("F1 fora de ja_organizada = %v, quero 0,75", got)
	}
	// Sem nenhuma categoria a medir, vale o "nada a acertar" da f1De (1).
	if got := (Metricas{PorCategoria: map[string]*MetCategoria{CatJaOrganizada: {TP: 9}}}).F1Organizando(); got != 1 {
		t.Errorf("só ja_organizada: F1 = %v, quero 1", got)
	}
}

func TestEstruturaListaTaxa(t *testing.T) {
	if _, ok := (Metricas{}).EstruturaListaTaxa(); ok {
		t.Error("sem frase de lista medida a taxa não existe")
	}
	m := Metricas{EstruturaListaOK: 3, EstruturaListaN: 4}
	if got, ok := m.EstruturaListaTaxa(); !ok || !perto(got, 0.75) {
		t.Errorf("taxa = %v (%v), quero 0,75", got, ok)
	}
}

func TestAvaliar_PisoDeOrganizacao(t *testing.T) {
	crit := func(f1, est float64) Criterios {
		return Criterios{FallbackMax: 0.05, P95Max: 2500 * time.Millisecond, F1Min: f1, EstruturaMin: est}
	}
	// F1 fora de ja_organizada = 2*30/(2*30+10+10) = 0,75; estrutura 3/4.
	m := metOrg(ia.OpenAI, "x", 0.1, 30, 10, 10, 3, 4)
	casos := []struct {
		nome   string
		c      Criterios
		passou bool
		motivo string
	}{
		{"sem piso (a regra aprovada) passa", crit(0, 0), true, ""},
		{"F1 exatamente no piso passa", crit(0.75, 0), true, ""},
		{"F1 abaixo do piso reprova", crit(0.76, 0), false, "F1 de pontuação, fora de já organizada, de 0,750 (mínimo 0,760)"},
		{"estrutura exatamente no piso passa", crit(0, 0.75), true, ""},
		{"estrutura abaixo do piso reprova", crit(0, 0.76), false, "acerto de estrutura das listas de 75,0% (mínimo 76,0%)"},
		{"os dois pisos, os dois cumpridos", crit(0.7, 0.7), true, ""},
	}
	for _, k := range casos {
		v := avaliar(m, k.c)
		if v.Passou != k.passou {
			t.Errorf("%s: Passou = %v (%v), quero %v", k.nome, v.Passou, v.Motivos, k.passou)
		}
		if k.motivo != "" && !strings.Contains(strings.Join(v.Motivos, "|"), k.motivo) {
			t.Errorf("%s: motivos %v não têm %q", k.nome, v.Motivos, k.motivo)
		}
	}
	// Sem frase de lista medida, o piso de estrutura não tem dado: reprova.
	sem := metOrg(ia.OpenAI, "x", 0.1, 30, 10, 10, 0, 0)
	if v := avaliar(sem, crit(0, 0.5)); v.Passou || !strings.Contains(strings.Join(v.Motivos, "|"), "nenhuma frase com lista") {
		t.Errorf("sem dado de estrutura: %+v", v)
	}
	if !avaliar(sem, crit(0, 0)).Passou {
		t.Error("sem piso, a falta de frase de lista não reprova")
	}
}

func TestEscolher_LinhaDeBaseNuncaVence(t *testing.T) {
	base := bom(ia.OpenAI, "nano", 0.05, 0.9)
	base.Cand.LinhaDeBase = true
	barato := bom(ia.OpenAI, "luna", 0.10, 0.9)
	ms := []Metricas{base, barato, bom(ia.Anthropic, "haiku", 1.0, 0.9)}
	d := escolher(ms, criteriosPadrao, false)
	if d.Vencedores[ia.OpenAI] != "openai:luna" {
		t.Errorf("vencedor OpenAI = %q, quero o mais barato que NÃO é linha de base", d.Vencedores[ia.OpenAI])
	}
	if !d.Vereditos[0].Passou {
		t.Error("a linha de base é julgada pela regra como os outros: devia constar como aprovada")
	}
	// Se só a linha de base passa, o provedor fica sem vencedor.
	barato.ChamadasCriticasDepois = 1
	d = escolher([]Metricas{base, barato}, criteriosPadrao, false)
	if v, tem := d.Vencedores[ia.OpenAI]; tem {
		t.Errorf("só a linha de base passou e mesmo assim venceu %q", v)
	}
	if !d.Vereditos[0].Passou || d.Vereditos[1].Passou {
		t.Errorf("vereditos = %+v", d.Vereditos)
	}
}

func TestMarcarLinhaDeBase(t *testing.T) {
	cands, _ := analisarCandidatos("openai:luna,openai:nano@minimal,anthropic:haiku")
	got, err := marcarLinhaDeBase("openai:nano@minimal", cands)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].LinhaDeBase || !got[1].LinhaDeBase || got[2].LinhaDeBase {
		t.Errorf("marcas = %v %v %v, quero só a do meio", got[0].LinhaDeBase, got[1].LinhaDeBase, got[2].LinhaDeBase)
	}
	if cands[1].LinhaDeBase {
		t.Error("a lista original foi alterada")
	}
	// Vazio não marca ninguém; mais de um, com espaços.
	if got, err := marcarLinhaDeBase("  ", cands); err != nil || got[0].LinhaDeBase || got[1].LinhaDeBase {
		t.Errorf("vazio: %v %v", got, err)
	}
	if got, err := marcarLinhaDeBase("openai:luna, anthropic:haiku", cands); err != nil || !got[0].LinhaDeBase || !got[2].LinhaDeBase || got[1].LinhaDeBase {
		t.Errorf("dois: %v %v", got, err)
	}
	// O rótulo tem de casar inteiro (o esforço conta) e estar em -modelos.
	for _, ruim := range []string{"openai:nano", "openai:nano@low", "openai:outro", "gemini:x", "openai:", "openai:nano@Minimal"} {
		if _, err := marcarLinhaDeBase(ruim, cands); err == nil || !strings.Contains(err.Error(), "-linha-de-base") {
			t.Errorf("%q devia ser recusado com -linha-de-base no erro: %v", ruim, err)
		}
	}
}

func TestReferenciaDoEco(t *testing.T) {
	casos := []Caso{
		// com sinais e lista no esperado: o eco erra tudo
		{ID: "a", Categoria: CatLista, Entrada: "as tarefas são conciliar o banco e fechar o caixa", Esperado: "As tarefas são:\n- Conciliar o banco\n- Fechar o caixa"},
		// sem lista, com um sinal (o ponto final): o eco não acerta o sinal
		{ID: "b", Categoria: CatValores, Entrada: "o DAS vence dia 20 e custa 850", Esperado: "O DAS vence dia 20 e custa 850."},
		// já organizada: o eco acerta, mas não conta no F1 nem na estrutura de lista
		{ID: "c", Categoria: CatJaOrganizada, Entrada: "Depois do almoço eu volto e a gente termina.", Esperado: "Depois do almoço eu volto e a gente termina."},
		// curta: não vai ao provedor
		{ID: "d", Categoria: CatCurta, Entrada: "ok pode mandar", Esperado: "Ok:\n- pode\n- mandar"},
	}
	ref := referenciaDoEco(casos, 0)
	if ref.F1 != 0 {
		t.Errorf("F1 do eco = %v, quero 0 (nenhum sinal acertado fora de ja_organizada)", ref.F1)
	}
	if ref.EstruturaN != 1 || ref.Estrutura != 0 {
		t.Errorf("estrutura do eco = %v em %d frase(s), quero 0 em 1", ref.Estrutura, ref.EstruturaN)
	}
	// Uma frase que o eco já entrega organizada (esperado igual à entrada, lista
	// já formatada) conta como acerto.
	casos = append(casos, Caso{ID: "e", Categoria: CatLista, Entrada: "As tarefas são:\n- Conciliar o banco\n- Fechar o caixa hoje", Esperado: "As tarefas são:\n- Conciliar o banco\n- Fechar o caixa hoje"})
	ref = referenciaDoEco(casos, 0)
	if ref.EstruturaN != 2 || !perto(ref.Estrutura, 0.5) {
		t.Errorf("estrutura do eco = %v em %d, quero 0,5 em 2", ref.Estrutura, ref.EstruturaN)
	}
	if ref.F1 == 0 {
		t.Errorf("a frase e acerta sinais: o F1 do eco devia subir de 0, é %v", ref.F1)
	}
	// O mínimo de palavras vem do padrão do Agent (4) e da flag.
	if r := referenciaDoEco(casos[:2], 20); r.EstruturaN != 0 || r.F1 != 1 {
		t.Errorf("com -min-palavras 20 nada vai ao provedor: %+v", r)
	}
}

func TestAvisosDeOrganizacao(t *testing.T) {
	eco := RefOrganizacao{F1: 0, Estrutura: 0, EstruturaN: 10}
	// TP/FP/FN da categoria lista dão o F1: 2TP/(2TP+FP+FN).
	bomOrg := metOrg(ia.Anthropic, "haiku", 1.0, 95, 0, 10, 10, 10)         // F1 0,95; estrutura 100%
	fraco := metOrg(ia.OpenAI, "fraco", 0.05, 60, 0, 40, 10, 10)            // F1 0,75; estrutura 100%
	quaseEco := metOrg(ia.OpenAI, "eco", 0.02, 4, 0, 96, 0, 10)             // F1 ~0,08; estrutura 0
	estruturaRuim := metOrg(ia.OpenAI, "sem-lista", 0.05, 95, 0, 10, 6, 10) // F1 0,95; estrutura 60%
	aviso := func(r Rodada) string { return strings.Join(avisosDeOrganizacao(r), "\n") }
	rodada := func(ms ...Metricas) Rodada {
		return Rodada{Metricas: ms, Criterios: criteriosPadrao, RefEco: eco, Decisao: escolher(ms, criteriosPadrao, false)}
	}

	// Vencedor que só devolve a entrada: avisa nos dois eixos.
	a := aviso(rodada(quaseEco, bomOrg))
	for _, quero := range []string{"`openai:eco` venceu OpenAI, mas organiza pouco",
		"perto dos 0,000 de um modelo que só devolve a entrada",
		"perto dos 0,0% de um modelo que só devolve a entrada",
		"contra 0,950 de `anthropic:haiku`", "contra 100,0% de `anthropic:haiku`", "-f1-min"} {
		contem(t, a, quero)
	}

	// F1 bem abaixo do melhor da rodada, mesmo acima do eco: avisa.
	contem(t, aviso(rodada(fraco, bomOrg)), "F1 de pontuação de 0,750, contra 0,950 de `anthropic:haiku`")
	naoContem(t, aviso(rodada(fraco, bomOrg)), "devolve a entrada")
	// Estrutura das listas bem abaixo do melhor: avisa, sem mexer no F1.
	e := aviso(rodada(estruturaRuim, bomOrg))
	contem(t, e, "acerto de estrutura das listas de 60,0%, contra 100,0% de `anthropic:haiku`")
	naoContem(t, e, "F1 de pontuação")
	// Os outros candidatos incluem a linha de base e os reprovados.
	base := bomOrg
	base.Cand.LinhaDeBase = true
	contem(t, aviso(rodada(fraco, base)), "contra 0,950 de `anthropic:haiku`")
	reprovado := bomOrg
	reprovado.ChamadasCriticasDepois = 1
	contem(t, aviso(rodada(fraco, reprovado)), "contra 0,950 de `anthropic:haiku`")

	// Vencedor que organiza tanto quanto o melhor: nenhum aviso.
	if a := aviso(rodada(bomOrg)); a != "" {
		t.Errorf("um vencedor que organiza bem não avisa: %s", a)
	}
	semelhante := metOrg(ia.OpenAI, "quase", 0.05, 90, 0, 10, 10, 10) // F1 0,947; ~ o do melhor
	if a := aviso(rodada(semelhante, bomOrg)); a != "" {
		t.Errorf("F1 e estrutura a 0,01 do melhor: sem aviso, veio %s", a)
	}
	// A estrutura entre o eco e a folga (10%) está perto do eco, mesmo abaixo do melhor.
	perto := metOrg(ia.OpenAI, "perto", 0.05, 95, 0, 10, 1, 10) // F1 0,95; estrutura 10%
	contem(t, aviso(rodada(perto, bomOrg)), "acerto de estrutura das listas de 10,0%, perto dos 0,0% de um modelo que só devolve a entrada")
	// Sem frase de lista no dataset, o eco não tem estrutura medida: sem "perto do eco".
	semListaNoEco := rodada(perto)
	semListaNoEco.RefEco = RefOrganizacao{F1: 0}
	if a := aviso(semListaNoEco); a != "" {
		t.Errorf("sem estrutura de referência do eco, não há o que comparar: %s", a)
	}
	// A diferença de estrutura abaixo da folga (10% contra 20%) não avisa.
	estrutura90 := metOrg(ia.OpenAI, "e90", 0.05, 95, 0, 10, 9, 10) // F1 0,95; estrutura 90%
	if a := aviso(rodada(estrutura90, bomOrg)); a != "" {
		t.Errorf("estrutura 90%% contra 100%% está dentro da folga, veio: %s", a)
	}
	// Candidato que não respondeu nada não é o "melhor F1" (sem dado, a F1 vale 1).
	fraco85 := metOrg(ia.OpenAI, "f85", 0.05, 85, 0, 30, 10, 10) // F1 0,85
	semResposta := Metricas{Cand: Candidato{Provedor: ia.Anthropic, Modelo: "mudo"}, Aborto: "chave_recusada"}
	if a := aviso(rodada(fraco85, semResposta)); a != "" {
		t.Errorf("o candidato sem resposta não é referência: %s", a)
	}
	// A linha de base fraca não gera aviso: ela não venceu.
	linhaFraca := quaseEco
	linhaFraca.Cand.LinhaDeBase = true
	if a := aviso(rodada(linhaFraca, bomOrg)); a != "" {
		t.Errorf("linha de base fraca não vence, não avisa: %s", a)
	}
	// Rodada incompleta não avisa.
	r := rodada(quaseEco, bomOrg)
	r.Incompleta = true
	if a := aviso(r); a != "" {
		t.Errorf("rodada incompleta não avisa: %s", a)
	}
	// O aviso vem no resumo e na decisão do relatório e no terminal.
	r = rodada(quaseEco, bomOrg)
	r.Data = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	rel := gerarRelatorio(r)
	if n := strings.Count(rel, "> **Atenção:** `openai:eco` venceu OpenAI"); n != 2 {
		t.Errorf("o aviso devia sair no Resumo e na Decisão, saiu %d vez(es)", n)
	}
	contem(t, resumo(r), "ATENÇÃO: `openai:eco` venceu OpenAI, mas organiza pouco")
}

// No comando inteiro: um modelo "eco", que devolve o que recebeu, e um bom. O
// eco é o mais barato e passa na regra sem piso.
func comboEcoEBom(t *testing.T) (*ambiente, []string) {
	t.Helper()
	frases, refs := datasetPequeno()
	oraculo := comOraculo(append(frases, refs...))
	modelo := func(p pedidoFalso) respostaFalsa {
		if p.Modelo == "eco" {
			return respostaFalsa{Final: p.Ditado}
		}
		return oraculo(p)
	}
	a := novoAmbiente(t, modelo, nil)
	return a, juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-rodadas", "2", "-modelos", "openai:bom,openai:eco", "-preco", "openai:bom=1/1,openai:eco=0.1/0.1"})
}

func TestTextoDoPiso(t *testing.T) {
	contem(t, textoDoPiso(Criterios{}), "**Sem piso de organização**")
	so1 := textoDoPiso(Criterios{F1Min: 0.5})
	contem(t, so1, "**Piso de organização**")
	contem(t, so1, "F1 de pontuação (fora de já organizada) ≥ 0,500")
	naoContem(t, so1, "acerto de estrutura")
	soEst := textoDoPiso(Criterios{EstruturaMin: 0.25})
	contem(t, soEst, "acerto de estrutura das listas ≥ 25,0%")
	naoContem(t, soEst, "F1 de pontuação")
	naoContem(t, soEst, "Sem piso")
}

// A nota "só a linha de base passou" é do provedor da linha de base: o outro
// provedor, sem vencedor por outro motivo, não a leva.
func TestRun_NotaDaLinhaDeBaseNaoVazaParaOutroProvedor(t *testing.T) {
	frases, refs := datasetPequeno()
	eco := func(p pedidoFalso) respostaFalsa { return respostaFalsa{Final: p.Ditado} }
	erra := func(p pedidoFalso) respostaFalsa {
		return respostaFalsa{Final: "Resposta que não tem nada a ver com o ditado."}
	}
	a := novoAmbiente(t, eco, erra)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:eco,anthropic:ruim", "-linha-de-base", "openai:eco", "-preco", "openai:eco=1/1,anthropic:ruim=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "OpenAI: nenhum candidato passou. Só a linha de base passou, e ela não concorre.")
	contem(t, rel, "Anthropic: nenhum candidato passou.\n")
}

func TestRun_ModeloQueSoDevolveAEntradaVenceSemPisoEORelatorioAvisa(t *testing.T) {
	a, args := comboEcoEBom(t)
	if cod := a.rodar(args...); cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "- **OpenAI:** `openai:eco`.") // a regra aprovada, sem piso, o deixa vencer...
	contem(t, rel, "**Sem piso de organização**")
	contem(t, rel, "> **Atenção:** `openai:eco` venceu OpenAI, mas organiza pouco") // ...e o relatório avisa
	contem(t, a.stdout.String(), "ATENÇÃO: `openai:eco` venceu OpenAI, mas organiza pouco")
	if got := celula(t, rel, "F1 de pontuação, fora de", 2); got != "0,000" {
		t.Errorf("F1 do eco fora de ja_organizada = %q, quero 0,000", got)
	}
	if got := celula(t, rel, "F1 de pontuação, fora de", 1); got != "1,000" {
		t.Errorf("F1 do bom fora de ja_organizada = %q, quero 1,000", got)
	}
}

func TestRun_LinhaDeBaseMedidaMasNaoVence(t *testing.T) {
	a, args := comboEcoEBom(t)
	if cod := a.rodar(append(args, "-linha-de-base", "openai:eco")...); cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "- **OpenAI:** `openai:bom`.")
	naoContem(t, rel, "Atenção:")
	contem(t, rel, "`openai:eco` (OpenAI), esforço de raciocínio: none — **linha de base: medido, nunca vence**")
	contem(t, rel, "`openai:eco`: **passou** (custo US$ 0,1280 por 1.000 ditados, F1 ")
	contem(t, rel, "É linha de base: não concorre a vencedor.")
	contem(t, a.stdout.String(), "modelo: openai:eco (esforço: none) [linha de base: medido, nunca vence]")
	contem(t, a.stdout.String(), "passou (linha de base: não vence)")
	contem(t, a.stdout.String(), "vencedor OpenAI: openai:bom")
	if got := celula(t, rel, "**Regra de escolha**", 2); got != "passou (linha de base: não vence)" {
		t.Errorf("célula da regra do eco = %q", got)
	}
	contem(t, rel, "-linha-de-base openai:eco") // o comando fica registrado
	// Os dois foram medidos: o eco fez as mesmas chamadas.
	if n1, n2 := len(a.openai.Pedidos()), 2*2*7; n1 != n2 {
		t.Errorf("a OpenAI recebeu %d pedidos, quero %d (os dois modelos, 2 rodadas, 7 frases)", n1, n2)
	}
}

func TestRun_SoALinhaDeBasePassa_ProvedorFicaSemVencedor(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa { return respostaFalsa{Final: p.Ditado} }, nil)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:eco", "-linha-de-base", "openai:eco", "-preco", "openai:eco=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "**Nenhum candidato passou pela regra de escolha.** Só a linha de base passou, e ela não concorre.")
	contem(t, rel, "OpenAI: nenhum candidato passou. Só a linha de base passou, e ela não concorre.")
	contem(t, a.stdout.String(), "Nenhum candidato passou pela regra. Só a linha de base passou, e ela não concorre. O Smart segue como padrão.")
	naoContem(t, rel, "**Sugestão para")
}

func TestRun_PisoDeOrganizacaoReprovaOEco(t *testing.T) {
	a, args := comboEcoEBom(t)
	if cod := a.rodar(append(args, "-f1-min", "0.5", "-estrutura-min", "0.5")...); cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "- **OpenAI:** `openai:bom`.")
	contem(t, rel, "`openai:eco`: não passou: F1 de pontuação, fora de já organizada, de 0,000 (mínimo 0,500); acerto de estrutura das listas de 0,0% (mínimo 50,0%).")
	contem(t, rel, "**Piso de organização** (decisão do mantenedor, fora da regra aprovada): F1 de pontuação (fora de já organizada) ≥ 0,500; acerto de estrutura das listas ≥ 50,0%.")
	naoContem(t, rel, "Sem piso de organização")
	contem(t, rel, "-f1-min 0.5 -estrutura-min 0.5")
}

func TestRun_FlagsDoPisoELinhaDeBaseInvalidas(t *testing.T) {
	casos := []struct {
		nome  string
		flags []string
		erro  string
	}{
		{"f1 acima de 1", []string{"-f1-min", "1.5"}, "-f1-min e -estrutura-min"},
		{"f1 negativo", []string{"-f1-min", "-0.1"}, "-f1-min e -estrutura-min"},
		{"estrutura acima de 1", []string{"-estrutura-min", "2"}, "-f1-min e -estrutura-min"},
		{"estrutura NaN", []string{"-estrutura-min", "NaN"}, "-f1-min e -estrutura-min"},
		{"f1 NaN", []string{"-f1-min", "NaN"}, "-f1-min e -estrutura-min"},
		{"linha de base fora de -modelos", []string{"-linha-de-base", "openai:outro"}, "não está em -modelos"},
		{"linha de base mal escrita", []string{"-linha-de-base", "semprovedor"}, "-linha-de-base"},
	}
	for _, k := range casos {
		a := novoAmbiente(t, nil, nil)
		cod := a.rodar(append([]string{"-gastar", "-modelos", "openai:x", "-preco", "openai:x=1/1"}, k.flags...)...)
		if cod != 2 {
			t.Errorf("%s: código %d, quero 2\n%s", k.nome, cod, a.stderr.String())
		}
		contem(t, a.stderr.String(), k.erro)
		if len(a.leitor.Prompts()) != 0 || len(a.openai.Pedidos()) != 0 {
			t.Errorf("%s: pediu chave ou chamou o provedor antes de recusar", k.nome)
		}
	}
}

// Os limites do piso valem inclusive: 0 e 1 são aceitos.
func TestRun_PisoNosExtremosEAceito(t *testing.T) {
	for _, v := range []string{"0", "1"} {
		a, args := comboEcoEBom(t)
		a.rodar(append(args, "-f1-min", v)...)
		if strings.Contains(a.stderr.String(), "entre 0 e 1") {
			t.Errorf("-f1-min %s foi recusado: %s", v, a.stderr.String())
		}
	}
}

func TestGerarRelatorio_ReferenciaDaOrganizacaoDoEco(t *testing.T) {
	r := rodadaDeTeste(t)
	r.RefEco = referenciaDoEco(r.Casos, 0)
	rel := gerarRelatorio(r)
	contem(t, rel, "Referência da organização: um modelo que só devolve a entrada marcaria, neste dataset, F1 de pontuação de 0,000 (fora de \"já organizada\") e acerto de estrutura de 0,0% nas 1 frase(s) com lista ou parágrafos.")
	// Sem frase de lista no dataset, a estrutura do eco não entra.
	r.RefEco = RefOrganizacao{F1: 0.25}
	rel = gerarRelatorio(r)
	contem(t, rel, "F1 de pontuação de 0,250 (fora de \"já organizada\"). Um modelo que organiza")
	naoContem(t, rel, "acerto de estrutura de")
}

// As fronteiras do aviso, em valores que o double representa sem sobra: F1 de
// 0,1 é 2/20; 0,2 é 2/10; a taxa de estrutura 0,3 é 3/10 e 0,5 é 5/10, e as
// diferenças 0,2-0,1 e 0,5-0,3 dão exatamente a folga (0,10 e 0,20).
func TestAvisosDeOrganizacao_FronteirasExatas(t *testing.T) {
	aviso := func(ms ...Metricas) string {
		r := Rodada{Metricas: ms, Criterios: criteriosPadrao, RefEco: RefOrganizacao{F1: 0, Estrutura: 0, EstruturaN: 10}, Decisao: escolher(ms, criteriosPadrao, false)}
		return strings.Join(avisosDeOrganizacao(r), "\n")
	}
	// F1 exatamente na folga do eco (0 + 0,10): ainda é "perto do eco".
	f01 := metOrg(ia.OpenAI, "f01", 0.05, 1, 0, 18, 10, 10) // F1 0,1
	contem(t, aviso(f01), "F1 de pontuação (fora de já organizada) de 0,100, perto dos 0,000")
	// A diferença exata de 0,10 para o melhor NÃO passa da folga: 0,3 - 0,19999999999999998
	// dá 0,1 no double (f1De(1,0,8) não é 0,2 exato, e f1De(3,0,14) é 0,3).
	f02 := metOrg(ia.OpenAI, "f02", 0.05, 1, 0, 8, 10, 10)    // F1 0,19999999999999998
	f03 := metOrg(ia.Anthropic, "f03", 1.0, 3, 0, 14, 10, 10) // F1 0,3
	if a := aviso(f02, f03); a != "" {
		t.Errorf("a diferença de F1 exatamente igual à folga não avisa, veio: %s", a)
	}
	// Além da folga (0,3 contra 0,18): avisa.
	f0195 := metOrg(ia.OpenAI, "f0195", 0.05, 1, 0, 9, 10, 10) // F1 0,18
	contem(t, aviso(f0195, f03), "contra 0,300 de `anthropic:f03`")
	// Um fio acima da folga do eco não é "perto".
	f0105 := metOrg(ia.OpenAI, "f0105", 0.05, 1, 0, 17, 10, 10) // F1 0,1053
	naoContem(t, aviso(f0105), "perto dos 0,000 de um modelo")

	// Estrutura exatamente na folga do eco (0 + 0,20): ainda é "perto do eco".
	e02 := metOrg(ia.OpenAI, "e02", 0.05, 95, 0, 10, 2, 10)
	contem(t, aviso(e02), "acerto de estrutura das listas de 20,0%, perto dos 0,0%")
	e03 := metOrg(ia.OpenAI, "e03", 0.05, 95, 0, 10, 3, 10)
	if a := aviso(e03); a != "" {
		t.Errorf("30%% está acima da folga do eco (20%%) e sozinho não tem o que comparar: %s", a)
	}
	// Diferença exata de 0,20 para o melhor não passa da folga.
	e05 := metOrg(ia.Anthropic, "e05", 1.0, 95, 0, 10, 5, 10)
	if a := aviso(e03, e05); a != "" {
		t.Errorf("0,5 - 0,3 é exatamente a folga: sem aviso, veio %s", a)
	}
}
