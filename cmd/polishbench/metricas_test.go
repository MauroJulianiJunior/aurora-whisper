package main

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// As métricas são testadas com resultados montados à mão, sem HTTP: cada número
// do relatório é conferido contra uma conta feita aqui.

func casosPequenos() []Caso {
	f, r := datasetPequeno()
	return append(f, r...)
}

// Índices do datasetPequeno nos testes abaixo.
const (
	iLista = iota
	iValores
	iPergunta
	iInjecao
	iArmadilha
	iCurta
	iJaGeral
	iJaCS
)

func cfgDe(casos []Caso) Config {
	return Config{Casos: casos, Rodadas: 1, PrazoAgente: 3500 * time.Millisecond, PrazoMax: 10 * time.Second}
}

// resultadoDe monta o Resultado de uma chamada em que o modelo devolveu `saida`,
// pelo Polir de verdade (as guardas de verdade), com a duração e os tokens dados.
func resultadoDe(caso Caso, idx, rodada int, saida string, dur time.Duration, tok ia.Tokens) Resultado {
	res, cli := polirCom(caso, saida)
	r := Resultado{Caso: idx, Rodada: rodada, Enviada: cli.chamadas > 0, Duracao: dur, Tokens: tok, Res: res}
	if cli.chamadas > 0 {
		r.Bruto, _ = json.Marshal(map[string]string{"final_text": saida})
	}
	return r
}

func perto(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCalcular_LatenciaFriaPercentisEPrazo(t *testing.T) {
	casos := casosPequenos()
	durs := []float64{9.0, 0.5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5} // a primeira é a fria
	var rs []Resultado
	for i, d := range durs {
		r := resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, time.Duration(d*float64(time.Second)), ia.Tokens{})
		r.Fria = i == 0
		rs = append(rs, r)
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})

	if m.Chamadas != 10 || m.LatenciaN != 10 || m.Latencia.N != 9 {
		t.Fatalf("Chamadas=%d LatenciaN=%d mornas=%d, quero 10, 10 e 9", m.Chamadas, m.LatenciaN, m.Latencia.N)
	}
	if !m.TemFria || !perto(m.Fria, 9.0) {
		t.Errorf("fria = %v (%v), quero 9", m.Fria, m.TemFria)
	}
	// Mornas: 0,5 a 4,5 em passos de 0,5 (9 amostras). Nearest-rank: p50 é a 5ª (2,5), p95 é a 9ª (4,5).
	if !perto(m.Latencia.P50, 2.5) || !perto(m.Latencia.P95, 4.5) {
		t.Errorf("p50=%v p95=%v, quero 2,5 e 4,5 (a fria fica fora)", m.Latencia.P50, m.Latencia.P95)
	}
	// Passam de 3,5 s (estritamente): 4, 4,5 e a fria de 9 s. O 3,5 s exato não.
	if m.AcimaDoPrazo != 3 || !perto(m.PercentualAcimaDoPrazo(), 0.3) {
		t.Errorf("AcimaDoPrazo = %d (%v), quero 3 (30%%)", m.AcimaDoPrazo, m.PercentualAcimaDoPrazo())
	}
	// A guarda aceitou tudo, mas o Agent cortaria as três lentas: fallback "prazo".
	if m.FallbackTotal != 3 || m.FallbackPorMotivo["prazo"] != 3 || m.AceitasMasLentas != 3 {
		t.Errorf("FallbackTotal=%d prazo=%d AceitasMasLentas=%d, quero 3 em cada", m.FallbackTotal, m.FallbackPorMotivo["prazo"], m.AceitasMasLentas)
	}
	if !m.Primeira.Presente || !perto(m.Primeira.Segundos, 9.0) || m.Primeira.Motivo != resolver.MotivoNenhum {
		t.Errorf("Primeira = %+v", m.Primeira)
	}
}

func TestMedeLatencia(t *testing.T) {
	resp := []byte(`{"final_text":"x"}`)
	casos := []struct {
		nome  string
		r     Resultado
		quero bool
	}{
		{"não enviada", Resultado{Enviada: false, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoCurto}}, false},
		{"respondeu", Resultado{Enviada: true, Bruto: resp}, true},
		{"respondeu e a guarda recusou", Resultado{Enviada: true, Bruto: resp, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoNumeros}}, true},
		{"prazo", Resultado{Enviada: true, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoPrazo}}, true},
		{"truncada", Resultado{Enviada: true, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoTruncada}}, true},
		{"recusa do modelo", Resultado{Enviada: true, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoRecusa}}, true},
		{"resposta inválida", Resultado{Enviada: true, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoRespostaInvalida}}, true},
	}
	for _, m := range []resolver.MotivoFallback{
		resolver.MotivoTransporte, resolver.MotivoChaveRecusada, resolver.MotivoSemCredito, resolver.MotivoLimite,
		resolver.MotivoModeloInexistente, resolver.MotivoModeloIncompativel, resolver.MotivoSobrecarga,
		resolver.MotivoPedidoRecusado, resolver.MotivoSemChave,
	} {
		casos = append(casos, struct {
			nome  string
			r     Resultado
			quero bool
		}{"erro rápido " + string(m), Resultado{Enviada: true, Res: resolver.ResolveResult{MotivoFallback: m}}, false})
	}
	for _, c := range casos {
		if got := medeLatencia(c.r); got != c.quero {
			t.Errorf("%s: %v, quero %v", c.nome, got, c.quero)
		}
	}
}

func TestCalcular_ErroRapidoNaoEntraNaLatenciaMasEntraNoFallback(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 2*time.Second, ia.Tokens{}),
		{Caso: iLista, Rodada: 1, Enviada: true, Duracao: 50 * time.Millisecond, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoSobrecarga}},
		{Caso: iLista, Rodada: 1, Enviada: true, Duracao: 10 * time.Second, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoPrazo}},
	}
	rs[0].Fria = true
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	// Latência: a fria (2 s) e o prazo (10 s); o 50 ms do erro rápido não conta.
	if m.LatenciaN != 2 || m.Latencia.N != 1 || !perto(m.Latencia.P50, 10) {
		t.Errorf("LatenciaN=%d mornas=%d p50=%v", m.LatenciaN, m.Latencia.N, m.Latencia.P50)
	}
	if m.FallbackPorMotivo["sobrecarga"] != 1 || m.FallbackPorMotivo["prazo"] != 1 || m.FallbackTotal != 2 {
		t.Errorf("fallback = %v", m.FallbackPorMotivo)
	}
	if m.Sucessos != 1 || m.Chamadas != 3 {
		t.Errorf("Sucessos=%d Chamadas=%d", m.Sucessos, m.Chamadas)
	}
}

func TestCalcular_FallbackDaRegraNaoContaAsArmadilhas(t *testing.T) {
	casos := casosPequenos()
	debora := "A Débora mandou o balancete." // a guarda recusa (palavras)
	rs := []Resultado{
		resultadoDe(casos[iArmadilha], iArmadilha, 1, debora, 0, ia.Tokens{}),
		resultadoDe(casos[iArmadilha], iArmadilha, 2, debora, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 2, casos[iLista].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iValores], iValores, 1, "O DAS vence dia 21 e custa R$ 850.", 0, ia.Tokens{}), // número trocado
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.FallbackTotal != 3 || m.Chamadas != 5 || !perto(m.FallbackTaxaTodas(), 0.6) {
		t.Errorf("todas: %d/%d (%v), quero 3/5", m.FallbackTotal, m.Chamadas, m.FallbackTaxaTodas())
	}
	if m.FallbacksSemArmadilha != 1 || m.ChamadasSemArmadilha != 3 || !perto(m.FallbackTaxa(), 1.0/3) {
		t.Errorf("da regra: %d/%d (%v), quero 1/3 (as armadilhas ficam de fora)", m.FallbacksSemArmadilha, m.ChamadasSemArmadilha, m.FallbackTaxa())
	}
	if c := m.PorCategoria[CatArmadilha]; c == nil || c.Chamadas != 2 || c.Fallbacks != 2 {
		t.Errorf("categoria armadilha = %+v", c)
	}
	if m.FallbackPorMotivo["palavras"] != 2 || m.FallbackPorMotivo["numeros"] != 1 {
		t.Errorf("por motivo = %v", m.FallbackPorMotivo)
	}
	if (Metricas{}).FallbackTaxa() != 0 || (Metricas{}).FallbackTaxaTodas() != 0 || (Metricas{}).PercentualAcimaDoPrazo() != 0 {
		t.Error("sem chamadas as taxas devem ser 0, não NaN")
	}
}

func TestF1De(t *testing.T) {
	casos := []struct {
		tp, fp, fn int
		p, r, f    float64
	}{
		{8, 2, 2, 0.8, 0.8, 0.8},
		{10, 0, 0, 1, 1, 1},
		{0, 0, 0, 1, 1, 1},
		{0, 1, 0, 0, 1, 0},
		{0, 0, 1, 1, 0, 0},
		{0, 1, 1, 0, 0, 0}, // precisão e revocação zeradas: F1 é 0, não NaN
		{3, 1, 0, 0.75, 1, 2 * 0.75 / 1.75},
		{1, 0, 3, 1, 0.25, 2 * 0.25 / 1.25},
	}
	for _, c := range casos {
		p, r, f := f1De(c.tp, c.fp, c.fn)
		if !perto(p, c.p) || !perto(r, c.r) || !perto(f, c.f) {
			t.Errorf("f1De(%d,%d,%d) = %v, %v, %v; quero %v, %v, %v", c.tp, c.fp, c.fn, p, r, f, c.p, c.r, c.f)
		}
	}
}

func TestSinaisComparados(t *testing.T) {
	casos := []struct {
		nome, esperado, saida string
		tp, fp, fn            int
	}{
		{"igual", "Olá, mundo.", "Olá, mundo.", 2, 0, 0},
		{"faltou a vírgula", "Olá, mundo.", "Olá mundo.", 1, 0, 1},
		{"vírgula a mais", "Olá mundo.", "Olá, mundo.", 1, 1, 0},
		{"sinal errado", "Olá mundo.", "Olá mundo?", 0, 1, 1},
		{"sem nenhum sinal dos dois lados", "olá mundo", "olá mundo", 0, 0, 0},
		{"maiúscula e quebra de linha não contam", "Olá\nmundo", "olá mundo", 0, 0, 0},
		{"palavra a menos: o sinal dela é erro", "Olá, mundo.", "Olá.", 0, 1, 2},
		{"palavra a mais: o sinal dela é erro", "Olá.", "Olá, mundo.", 0, 2, 1},
		{"ponto e dois pontos", "Atenção: o prazo acaba.", "Atenção, o prazo acaba.", 1, 1, 1},
		{"lista: ':' depois da frase de apresentação", "Os itens:\n- a\n- b", "Os itens:\n- a\n- b", 1, 0, 0},
		{"ponto no fim de cada item é a mais", "Os itens:\n- a\n- b", "Os itens:\n- a.\n- b.", 1, 2, 0},
		{"reticências valem ponto", "Olá.", "Olá…", 1, 0, 0},
		{"a moeda não é sinal", "Custa R$ 10.", "Custa R$ 10.", 1, 0, 0},
	}
	for _, c := range casos {
		tp, fp, fn := sinaisComparados(c.esperado, c.saida)
		if tp != c.tp || fp != c.fp || fn != c.fn {
			t.Errorf("%s: tp=%d fp=%d fn=%d, quero %d %d %d", c.nome, tp, fp, fn, c.tp, c.fp, c.fn)
		}
	}
}

func TestEdicaoPorPalavra(t *testing.T) {
	casos := []struct {
		entrada, saida string
		quero          float64
		ok             bool
	}{
		{"a b c d", "a b c d", 0, true},
		{"a b c d", "A, b. c d!", 0, true}, // pontuação e caixa não contam
		{"a b c d", "a b x d", 0.25, true},
		{"a b c d", "a b d", 0.25, true},
		{"a b c d", "a b c d e", 0.25, true},
		{"a b c d", "", 1, true},
		{"", "a", 0, false},
		{"ahn o DAS vence", "O DAS vence", 0.25, true},
	}
	for _, c := range casos {
		got, ok := edicaoPorPalavra(c.entrada, c.saida)
		if ok != c.ok || !perto(got, c.quero) {
			t.Errorf("%q -> %q: %v (%v), quero %v (%v)", c.entrada, c.saida, got, ok, c.quero, c.ok)
		}
	}
}

func TestCalcular_PreservacaoMediaEP95(t *testing.T) {
	casos := casosPequenos()
	// A frase "os itens são primeiro a guia segundo o recibo" tem 9 palavras: o
	// esperado tira 2 ordinais (2/9). Um modelo que devolve a entrada intacta tem
	// 0%; um que troca uma palavra, 1/9.
	entrada := casos[iLista].Entrada
	rs := []Resultado{
		resultadoDe(casos[iLista], iLista, 1, entrada, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 2, casos[iLista].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 3, "os itens são primeiro a guia segundo o boleto", 0, ia.Tokens{}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	// Edições: 0, 2/9 (o esperado tem 7 palavras + 2 marcadores... só 2 ordinais somem) e 1/9.
	quero := (0 + 2.0/9 + 1.0/9) / 3
	if m.Preservacao.N != 3 || !perto(m.Preservacao.Mean, quero) {
		t.Errorf("média = %v (N=%d), quero %v", m.Preservacao.Mean, m.Preservacao.N, quero)
	}
	if !perto(m.Preservacao.P95, 2.0/9) { // nearest-rank: o maior de 3
		t.Errorf("p95 = %v, quero %v", m.Preservacao.P95, 2.0/9)
	}
}

func TestReferenciaDePreservacao(t *testing.T) {
	casos := casosPequenos()
	ref := referenciaDePreservacao(casos, 0)
	// 7 frases têm 4 palavras ou mais; a curta fica de fora.
	if ref.N != 7 {
		t.Fatalf("N = %d, quero 7", ref.N)
	}
	if ref.Mean <= 0 || ref.Max < ref.Mean {
		t.Errorf("média %v, máx %v", ref.Mean, ref.Max)
	}
	// Com o mínimo em 3, a curta entra.
	if got := referenciaDePreservacao(casos, 3).N; got != 8 {
		t.Errorf("com mínimo 3, N = %d, quero 8", got)
	}
}

func TestCalcular_CustoETokens(t *testing.T) {
	casos := casosPequenos()
	tok := ia.Tokens{Entrada: 1200, Saida: 80, Raciocinio: 10}
	tok2 := ia.Tokens{Entrada: 1000, Saida: 200, Raciocinio: 30}
	rs := []Resultado{
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, tok),
		resultadoDe(casos[iLista], iLista, 2, casos[iLista].Esperado, 0, tok2),
		resultadoDe(casos[iCurta], iCurta, 1, casos[iCurta].Esperado, 0, ia.Tokens{}), // não vai ao provedor
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Preco: Preco{Entrada: 0.10, Saida: 0.50}, Resultados: rs})
	if m.Chamadas != 2 || m.NaoEnviadas != 1 {
		t.Fatalf("Chamadas=%d NaoEnviadas=%d, quero 2 e 1", m.Chamadas, m.NaoEnviadas)
	}
	if m.TokensEntrada != 2200 || m.TokensSaida != 280 || m.TokensRaciocinio != 40 || m.RaciocinioMax != 30 {
		t.Errorf("tokens = %d/%d/%d (máx %d)", m.TokensEntrada, m.TokensSaida, m.TokensRaciocinio, m.RaciocinioMax)
	}
	quero := (2200*0.10 + 280*0.50) / 1e6
	if !perto(m.CustoTotal, quero) {
		t.Errorf("custo = %v, quero %v", m.CustoTotal, quero)
	}
	// Por 1.000 ditados: o custo médio de uma chamada que foi ao provedor, vezes 1000.
	if !perto(m.CustoPor1000, quero/2*1000) {
		t.Errorf("custo por 1.000 = %v, quero %v", m.CustoPor1000, quero/2*1000)
	}
	if got := custoDe(ia.Tokens{Entrada: 1_000_000, Saida: 1_000_000}, Preco{Entrada: 1, Saida: 5}); !perto(got, 6) {
		t.Errorf("custoDe = %v, quero 6", got)
	}
}

func TestCalcular_JaOrganizadaDevolvidaIgual(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iJaGeral], iJaGeral, 1, casos[iJaGeral].Entrada, 0, ia.Tokens{}),
		resultadoDe(casos[iJaGeral], iJaGeral, 2, "  "+casos[iJaGeral].Entrada+"\n", 0, ia.Tokens{}),                 // espaço nas pontas não conta
		resultadoDe(casos[iJaCS], iJaCS, 1, "Faz o deploy em staging antes de mandar para produção", 0, ia.Tokens{}), // perdeu o ponto
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{}),                                // outra categoria
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.JaOrganizadaN != 3 || m.JaOrganizadaIgual != 2 {
		t.Errorf("já organizada igual = %d/%d, quero 2/3", m.JaOrganizadaIgual, m.JaOrganizadaN)
	}
}

func TestCalcular_InjecaoEPerguntaAntesEDepois(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iInjecao], iInjecao, 1, "Era uma vez um poema sobre contabilidade.", 0, ia.Tokens{}), // obedeceu
		resultadoDe(casos[iInjecao], iInjecao, 2, casos[iInjecao].Esperado, 0, ia.Tokens{}),                    // não obedeceu
		resultadoDe(casos[iPergunta], iPergunta, 1, "Qual o prazo da DCTFWeb? O prazo é dia 20.", 0, ia.Tokens{}),
		resultadoDe(casos[iPergunta], iPergunta, 2, "Qual o prazo da DCTFWeb? Dia vinte.", 0, ia.Tokens{}),
		resultadoDe(casos[iPergunta], iPergunta, 3, casos[iPergunta].Esperado, 0, ia.Tokens{}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.InjecaoN != 2 || m.InjecaoObedecidaAntes != 1 || m.InjecaoObedecidaDepois != 0 {
		t.Errorf("injeção = %d/%d depois %d, quero 1/2 e 0 (a guarda barrou o poema)", m.InjecaoObedecidaAntes, m.InjecaoN, m.InjecaoObedecidaDepois)
	}
	if m.PerguntaN != 3 || m.PerguntaRespondidaAntes != 2 || m.PerguntaRespondidaDepois != 0 {
		t.Errorf("pergunta = %d/%d depois %d, quero 2/3 e 0", m.PerguntaRespondidaAntes, m.PerguntaN, m.PerguntaRespondidaDepois)
	}
	if m.ChamadasCriticasAntes != 3 || m.ChamadasCriticasDepois != 0 || m.GuardaBarrouCritica != 3 {
		t.Errorf("críticas antes/depois/barradas = %d/%d/%d, quero 3/0/3", m.ChamadasCriticasAntes, m.ChamadasCriticasDepois, m.GuardaBarrouCritica)
	}
}

// Um buraco na guarda: o Polir ACEITOU uma saída com falha crítica. A auditoria
// tem de vê-lo em "depois", e o relatório mostra o caso. O Resultado é montado
// à mão (não depende do estado atual das guardas).
func TestCalcular_BuracoNaGuardaAparecemDepois(t *testing.T) {
	casos := casosPequenos()
	saida := "O DAS vence dia 21 e custa R$ 850." // número trocado
	r := resultadoDe(casos[iValores], iValores, 1, saida, 0, ia.Tokens{})
	if r.Res.Reescrito {
		t.Fatal("premissa: a guarda de verdade recusa o número trocado")
	}
	r.Res.Reescrito, r.Res.MotivoFallback, r.Res.FinalText = true, resolver.MotivoNenhum, saida // a guarda "aceita"
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: []Resultado{r}})

	if m.ChamadasCriticasAntes != 1 || m.ChamadasCriticasDepois != 1 {
		t.Fatalf("antes/depois = %d/%d, quero 1/1", m.ChamadasCriticasAntes, m.ChamadasCriticasDepois)
	}
	if m.CriticasDepois[FalhaNumero] != 1 || m.PorCategoria[CatValores].CriticasDepois != 1 {
		t.Errorf("por tipo depois = %v; por categoria = %+v", m.CriticasDepois, m.PorCategoria[CatValores])
	}
	if m.GuardaBarrouCritica != 0 {
		t.Errorf("a guarda não barrou nada, mas GuardaBarrouCritica = %d", m.GuardaBarrouCritica)
	}
	var achou bool
	for _, e := range m.Exemplos {
		if e.Tipo == ExemploCriticaDepois && e.CasoID == "valores-1" && e.Saida == saida {
			achou = true
		}
	}
	if !achou {
		t.Errorf("faltou o exemplo de falha depois da guarda: %+v", m.Exemplos)
	}
	// A regra reprova.
	v := avaliar(m, Criterios{FallbackMax: 1, P95Max: time.Hour})
	if v.Passou {
		t.Error("a regra aprovou um candidato com falha crítica depois da guarda")
	}
}

func TestCalcular_GuardaRecusouBomEBarrouCritica(t *testing.T) {
	casos := casosPequenos()
	fillers := c(CatArmadilha, "eu acho que a gente precisa né revisar tipo todos os lançamentos", "Eu acho que a gente precisa, né, revisar, tipo, todos os lançamentos.")
	casos = append(casos, fillers)
	iF := len(casos) - 1
	rs := []Resultado{
		// Sem "né" nem "tipo": a guarda recusa (palavras), a auditoria só vê omissão leve.
		resultadoDe(fillers, iF, 1, "Eu acho que a gente precisa revisar todos os lançamentos.", 0, ia.Tokens{}),
		// Número trocado: a guarda barra e a auditoria confirma a falha crítica.
		resultadoDe(casos[iValores], iValores, 1, "O DAS vence dia 21 e custa R$ 850.", 0, ia.Tokens{}),
		// Boa.
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.GuardaRecusouBom != 1 || m.GuardaRecusouBomMotivos["palavras"] != 1 {
		t.Errorf("recusou bom = %d %v, quero 1 {palavras: 1}", m.GuardaRecusouBom, m.GuardaRecusouBomMotivos)
	}
	if m.GuardaBarrouCritica != 1 || m.ChamadasCriticasAntes != 1 {
		t.Errorf("barrou crítica = %d de %d, quero 1 de 1", m.GuardaBarrouCritica, m.ChamadasCriticasAntes)
	}
	if m.LevesOmissao != 1 {
		t.Errorf("omissão leve = %d, quero 1", m.LevesOmissao)
	}
	tipos := map[string]int{}
	for _, e := range m.Exemplos {
		tipos[e.Tipo]++
	}
	if tipos[ExemploGuardaRecusouBom] != 1 || tipos[ExemploCriticaAntes] != 1 || tipos[ExemploCriticaDepois] != 0 {
		t.Errorf("exemplos por tipo = %v", tipos)
	}
}

func TestCalcular_EstruturaEMarcador(t *testing.T) {
	casos := casosPequenos()
	esperado := casos[iLista].Esperado // "Os itens são:\n1. A guia\n2. O recibo": 1 parágrafo, 2 itens numerados
	rs := []Resultado{
		resultadoDe(casos[iLista], iLista, 1, esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 2, "Os itens são:\n- A guia\n- O recibo", 0, ia.Tokens{}), // estrutura certa, marcador outro
		resultadoDe(casos[iLista], iLista, 3, "Os itens são: a guia, o recibo.", 0, ia.Tokens{}),     // virou frase
		resultadoDe(casos[iValores], iValores, 1, casos[iValores].Esperado, 0, ia.Tokens{}),          // sem lista: só entra no geral
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.EstruturaN != 4 || m.EstruturaOK != 3 {
		t.Errorf("estrutura geral = %d/%d, quero 3/4", m.EstruturaOK, m.EstruturaN)
	}
	if m.EstruturaListaN != 3 || m.EstruturaListaOK != 2 {
		t.Errorf("estrutura das frases com lista = %d/%d, quero 2/3", m.EstruturaListaOK, m.EstruturaListaN)
	}
	if m.MarcadorN != 3 || m.MarcadorOK != 1 {
		t.Errorf("marcador = %d/%d, quero 1/3", m.MarcadorOK, m.MarcadorN)
	}
}

func TestCalcular_VariacaoEntreRodadas(t *testing.T) {
	casos := casosPequenos()
	a, b := casos[iLista].Esperado, "Os itens são:\n- A guia\n- O recibo"
	rs := []Resultado{
		resultadoDe(casos[iLista], iLista, 1, a, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 2, a, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 3, b, 0, ia.Tokens{}), // mudou na terceira
		resultadoDe(casos[iValores], iValores, 1, casos[iValores].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iValores], iValores, 2, casos[iValores].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iInjecao], iInjecao, 1, casos[iInjecao].Esperado, 0, ia.Tokens{}), // uma rodada só: fica de fora
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.VariacaoN != 2 || m.VariacaoCasos != 1 {
		t.Errorf("variação = %d/%d, quero 1/2", m.VariacaoCasos, m.VariacaoN)
	}
}

func TestCalcular_ExemplosTemTetoEUmPorFrase(t *testing.T) {
	casos := casosPequenos()
	// 15 frases diferentes, cada uma com o número trocado na saída.
	for i := 0; i < 15; i++ {
		casos = append(casos, c(CatValores, fmt.Sprintf("o DAS %d vence dia 20 e custa R$ 850", i), fmt.Sprintf("O DAS %d vence dia 20 e custa R$ 850.", i)))
		casos[len(casos)-1].ID = fmt.Sprintf("v%02d", i)
	}
	var rs []Resultado
	// A mesma frase falhando em 5 rodadas, cada vez de um jeito: um exemplo só.
	for i := 0; i < 5; i++ {
		rs = append(rs, resultadoDe(casos[iPergunta], iPergunta, i+1, fmt.Sprintf("Qual o prazo da DCTFWeb? Sim %d.", i), 0, ia.Tokens{}))
	}
	for i := 8; i < len(casos); i++ {
		saida := fmt.Sprintf("O DAS %d vence dia 21 e custa R$ 850.", i-8)
		rs = append(rs, resultadoDe(casos[i], i, 1, saida, 0, ia.Tokens{}))
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	var valores, perguntas int
	for _, e := range m.Exemplos {
		if e.Tipo != ExemploCriticaAntes {
			continue
		}
		if e.Categoria == CatValores {
			valores++
		}
		if e.CasoID == "pergunta-1" {
			perguntas++
		}
	}
	if valores+perguntas != maxExemplosPorTipo {
		t.Errorf("exemplos = %d valores + %d pergunta, quero o teto de %d no total", valores, perguntas, maxExemplosPorTipo)
	}
	if perguntas != 1 {
		t.Errorf("a mesma frase falhou 5 vezes e virou %d exemplos, quero 1", perguntas)
	}
	if m.ChamadasCriticasAntes != 20 {
		t.Errorf("as 20 chamadas contam, mesmo sem exemplo: %d", m.ChamadasCriticasAntes)
	}
}

func TestCalcular_FormasEChamadasSemResposta(t *testing.T) {
	casos := casosPequenos()
	limpa := resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{})
	extra := limpa
	extra.Bruto = []byte(`{"final_text":"Os itens são:\n1. A guia\n2. O recibo","warnings":["x"]}`)
	cerca := limpa
	cerca.Bruto = []byte("```json\n{}\n```")
	erro := Resultado{Caso: iLista, Rodada: 1, Enviada: true, Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoSobrecarga}}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: []Resultado{limpa, extra, cerca, erro}})
	if m.Formas[FormaLimpa] != 1 || m.Formas[FormaCamposExtras] != 1 || m.Formas[FormaCercaDeCodigo] != 1 {
		t.Errorf("formas = %v", m.Formas)
	}
	// A forma "cerca de código" não tem final_text: não conta como sucesso.
	if m.Chamadas != 4 || m.Sucessos != 2 {
		t.Errorf("Chamadas=%d Sucessos=%d, quero 4 e 2", m.Chamadas, m.Sucessos)
	}
}

func TestCalcular_NaoEnviadasNaoContam(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iCurta], iCurta, 1, casos[iCurta].Esperado, time.Second, ia.Tokens{Entrada: 999}),
		resultadoDe(casos[iCurta], iCurta, 2, casos[iCurta].Esperado, time.Second, ia.Tokens{Entrada: 999}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.Chamadas != 0 || m.NaoEnviadas != 2 || m.TokensEntrada != 0 || m.LatenciaN != 0 || m.CustoPor1000 != 0 {
		t.Errorf("uma frase curta contou: %+v", m)
	}
}

func TestCalcular_F1PorCategoria(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iValores], iValores, 1, casos[iValores].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iValores], iValores, 2, "O DAS vence dia 20 e custa R$ 850", 0, ia.Tokens{}), // sem o ponto final
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	v := m.PorCategoria[CatValores]
	// valores: 1 ponto final certo, 1 ponto final faltando: tp=1 fn=1 fp=0.
	if v.TP != 1 || v.FN != 1 || v.FP != 0 {
		t.Errorf("valores: %+v", v)
	}
	if !perto(v.F1(), 2*1*0.5/1.5) {
		t.Errorf("F1 de valores = %v", v.F1())
	}
	// Geral: os dois de valores (tp 1, fn 1) mais o ':' certo da lista (tp 1);
	// o item "recibo" fica sem ponto no esperado.
	if m.TP != 2 || m.FN != 1 || m.FP != 0 {
		t.Errorf("geral: tp=%d fp=%d fn=%d, quero 2 0 1", m.TP, m.FP, m.FN)
	}
	_, _, f := f1De(2, 0, 1)
	if !perto(m.F1, f) {
		t.Errorf("F1 = %v, quero %v", m.F1, f)
	}
}
