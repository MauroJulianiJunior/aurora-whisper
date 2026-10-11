package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Mais fronteiras achadas na rodada de mutação da S3.

// "Aceita mas lenta" é a chamada que a guarda ACEITOU e passou do prazo. A que a
// guarda recusou e também demorou é só um fallback da guarda.
func TestCalcular_AceitaMasLentaExigeAGuardaTerAceitado(t *testing.T) {
	casos := casosPequenos()
	lentaAceita := resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 5*time.Second, ia.Tokens{})
	lentaRecusada := resultadoDe(casos[iValores], iValores, 1, "O DAS vence dia 21 e custa R$ 850.", 5*time.Second, ia.Tokens{})
	rapidaRecusada := resultadoDe(casos[iValores], iValores, 2, "O DAS vence dia 21 e custa R$ 850.", time.Second, ia.Tokens{})
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: []Resultado{lentaAceita, lentaRecusada, rapidaRecusada}})
	if m.AceitasMasLentas != 1 {
		t.Errorf("AceitasMasLentas = %d, quero 1 (a recusada e lenta não é 'aceita')", m.AceitasMasLentas)
	}
	// As duas lentas viram "prazo" para o Agent; a rápida recusada fica com o motivo da guarda.
	if m.FallbackPorMotivo["prazo"] != 2 || m.FallbackPorMotivo["numeros"] != 1 {
		t.Errorf("por motivo = %v", m.FallbackPorMotivo)
	}
}

// O desfecho da primeira chamada (fria) vem no relatório mesmo quando ela falhou.
func TestCalcular_PrimeiraChamadaFalhadaTemOMotivo(t *testing.T) {
	casos := casosPequenos()
	fria := Resultado{Caso: iLista, Rodada: 1, Fria: true, Enviada: true, Duracao: 1500 * time.Millisecond,
		Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoChaveRecusada}}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: []Resultado{fria}})
	if !m.Primeira.Presente || m.Primeira.Motivo != resolver.MotivoChaveRecusada || !perto(m.Primeira.Segundos, 1.5) {
		t.Errorf("Primeira = %+v", m.Primeira)
	}
	// Erro rápido não entra na latência, mas a primeira chamada continua à mostra.
	if m.TemFria || m.LatenciaN != 0 {
		t.Errorf("TemFria=%v LatenciaN=%d: o erro rápido não mede o modelo", m.TemFria, m.LatenciaN)
	}
	if got := calcular(cfgDe(casos), SaidaCandidato{}).Primeira; got.Presente {
		t.Errorf("sem chamada nenhuma, Primeira = %+v", got)
	}
}

func TestCalcular_RaciocinioMaximoEDaMaiorChamada(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{Entrada: 1, Saida: 1, Raciocinio: 30}),
		resultadoDe(casos[iLista], iLista, 2, casos[iLista].Esperado, 0, ia.Tokens{Entrada: 1, Saida: 1, Raciocinio: 10}),
		resultadoDe(casos[iLista], iLista, 3, casos[iLista].Esperado, 0, ia.Tokens{Entrada: 1, Saida: 1, Raciocinio: 20}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.RaciocinioMax != 30 || m.TokensRaciocinio != 60 {
		t.Errorf("máx = %d, soma = %d; quero 30 e 60", m.RaciocinioMax, m.TokensRaciocinio)
	}
}

// ---------------------------------------------------------------------
// Alinhamento: acima do limite de células, nada casa
// ---------------------------------------------------------------------

func TestAlinharPalavras_AcimaDoLimiteNaoAlinha(t *testing.T) {
	grande := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprint("p", i%7)
		}
		return out
	}
	// 2.001 x 2.001 células passam de 4 milhões: devolve nil sem alocar a tabela.
	if got := alinharPalavras(grande(2001), grande(2001)); got != nil {
		t.Errorf("acima do limite alinhou %d pares", len(got))
	}
	// Logo abaixo do limite (1.999 x 1.999) alinha.
	if got := alinharPalavras(grande(1500), grande(1500)); len(got) != 1500 {
		t.Errorf("abaixo do limite alinhou %d pares, quero 1500", len(got))
	}
	// E com um lado vazio nunca alinha.
	if got := alinharPalavras(nil, grande(3)); got != nil {
		t.Errorf("um lado vazio: %v", got)
	}
	if got := alinharPalavras(grande(3), nil); got != nil {
		t.Errorf("o outro lado vazio: %v", got)
	}
}

func TestAlinharPalavras_EmpatesFicamComAsPrimeiras(t *testing.T) {
	// a = x y x, b = x: casa o primeiro x.
	got := alinharPalavras([]string{"x", "y", "x"}, []string{"x"})
	if len(got) != 1 || got[0] != (par{0, 0}) {
		t.Errorf("pares = %v, quero o primeiro x", got)
	}
	// a = x, b = y x x: casa o primeiro x de b.
	got = alinharPalavras([]string{"x"}, []string{"y", "x", "x"})
	if len(got) != 1 || got[0] != (par{0, 1}) {
		t.Errorf("pares = %v, quero o x da posição 1 de b", got)
	}
	// Ordem preservada nos dois lados.
	got = alinharPalavras([]string{"a", "b", "c", "d", "e"}, []string{"b", "d", "e", "a"})
	for i := 1; i < len(got); i++ {
		if got[i].A <= got[i-1].A || got[i].B <= got[i-1].B {
			t.Errorf("os pares saíram fora de ordem: %v", got)
		}
	}
	if len(got) != 3 {
		t.Errorf("pares = %v, quero 3 (b d e)", got)
	}
}

func TestDistanciaDePalavras_VaziosCustamOTamanhoDoOutro(t *testing.T) {
	if got := distanciaDePalavras(nil, []string{"a", "b", "c"}); got != 3 {
		t.Errorf("de vazio para 3 = %d", got)
	}
	if got := distanciaDePalavras([]string{"a", "b"}, nil); got != 2 {
		t.Errorf("de 2 para vazio = %d", got)
	}
	if got := distanciaDePalavras([]string{"a"}, []string{"b"}); got != 1 {
		t.Errorf("trocar uma palavra = %d", got)
	}
	if got := distanciaDePalavras([]string{"a", "b"}, []string{"b", "a"}); got != 2 {
		t.Errorf("trocar a ordem de duas palavras = %d, quero 2", got)
	}
}

// ---------------------------------------------------------------------
// A começo de frase, com marcadores e parênteses
// ---------------------------------------------------------------------

func TestAbreFraseEm_PulaMarcadorAspasEParenteses(t *testing.T) {
	casos := []struct {
		nome, texto, palavra string
		quero                bool
	}{
		{"depois de ponto e parêntese", "Fim. (Quatro) fim", "Quatro", true},
		{"depois de ponto e aspas", "Fim. \"Cinco\" fim", "Cinco", true},
		{"depois de ponto e «", "Fim. «Seis» fim", "Seis", true},
		{"depois de marcador de traço", "Fim.\n- Sete", "Sete", true},
		{"depois de marcador de asterisco", "Fim.\n* Oito", "Oito", true},
		{"depois de bolinha", "Fim.\n• Nove", "Nove", true},
		{"no meio da frase depois de parêntese", "Um (dois) três", "dois", false},
		{"depois de vírgula", "Um, dois", "dois", false},
		{"depois de exclamação", "Um! Dois", "Dois", true},
		{"depois de interrogação", "Um? Dois", "Dois", true},
		{"depois de reticências", "Um… Dois", "Dois", true},
		{"o começo do texto", "Um dois", "Um", true},
		{"o começo do texto com espaço", "   Um dois", "Um", true},
	}
	for _, c := range casos {
		texto := []rune(c.texto)
		i := len([]rune(c.texto[:strings.Index(c.texto, c.palavra)]))
		if got := abreFraseEm(texto, i); got != c.quero {
			t.Errorf("%s: abreFraseEm = %v, quero %v", c.nome, got, c.quero)
		}
	}
}

// ---------------------------------------------------------------------
// O transporte injetado é o que fala com o provedor
// ---------------------------------------------------------------------

type transporteContador struct {
	base http.RoundTripper
	n    atomic.Int32
}

func (c *transporteContador) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.base.RoundTrip(r)
}

func TestRun_UsaOTransporteInjetadoESoEle(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, comOraculo(append(append([]Caso(nil), frases...), refs...)), nil)
	cont := &transporteContador{base: transporteDeTeste()}
	d := a.deps()
	d.Transporte = cont
	cod := run(a.ctx, append(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"}), "-relatorio", a.relatorio), &a.stdout, &a.stderr, d)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	if got, quero := int(cont.n.Load()), len(a.openai.Pedidos()); got != quero || quero == 0 {
		t.Errorf("o transporte injetado viu %d pedidos e o provedor recebeu %d: quero iguais e não zero", got, quero)
	}
}

// ---------------------------------------------------------------------
// Falha ao gravar o relatório
// ---------------------------------------------------------------------

func TestRun_FalhaAoGravarORelatorioAindaMostraOResumo(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, comOraculo(append(append([]Caso(nil), frases...), refs...)), nil)
	d := a.deps()
	d.GravarRelatorio = func(string, time.Time, []byte) (string, error) { return "", errors.New("disco cheio") }
	cod := run(a.ctx, append(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"}), "-relatorio", a.relatorio), &a.stdout, &a.stderr, d)
	if cod == 0 {
		t.Fatal("saiu com 0 sem ter gravado o relatório")
	}
	contem(t, a.stderr.String(), "gravando o relatório: disco cheio")
	contem(t, a.stdout.String(), "gasto da rodada")
	contem(t, a.stdout.String(), "PASSOU")
	naoContem(t, a.stdout.String(), "relatório: ")
}

// O resumo do terminal.
func TestResumo_TabelaEVencedor(t *testing.T) {
	r := rodadaDeTeste(t)
	res := resumo(r)
	contem(t, res, "modelo")
	contem(t, res, "falha crítica depois")
	contem(t, res, "openai:bom")
	contem(t, res, "PASSOU")
	contem(t, res, "não passou")
	contem(t, res, "vencedor OpenAI: openai:bom")
	contem(t, res, "gasto da rodada: US$")
	contem(t, res, "anthropic:ruim não passou:")
	naoContem(t, res, "INCOMPLETA")
	r.Metricas[1].Aborto = "sem_credito"
	contem(t, resumo(r), "anthropic:ruim: parou (sem_credito)")
}

// Os caracteres invisíveis (que não são de controle) somem da mensagem do provedor.
func TestMensagemSegura_CaractereInvisivelViraEspaco(t *testing.T) {
	if got := mensagemSegura("a​b­c"); got != "a b c" {
		t.Errorf("mensagem = %q, quero \"a b c\" (espaço de largura zero e hífen mole viram espaço)", got)
	}
}

// Já organizada: a comparação ignora o espaço nas pontas da ENTRADA também.
func TestCalcular_JaOrganizadaIgnoraEspacoNaEntrada(t *testing.T) {
	k := Caso{ID: "ja-x", Categoria: CatJaOrganizada, Entrada: "Depois do almoço eu volto e a gente termina isso.  ", Esperado: "Depois do almoço eu volto e a gente termina isso."}
	casos := []Caso{k}
	rs := []Resultado{resultadoDe(k, 0, 1, "Depois do almoço eu volto e a gente termina isso.", 0, ia.Tokens{})}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.JaOrganizadaN != 1 || m.JaOrganizadaIgual != 1 {
		t.Errorf("já organizada igual = %d/%d, quero 1/1", m.JaOrganizadaIgual, m.JaOrganizadaN)
	}
}

// As falhas críticas são contadas por categoria (a tabela "Por categoria").
func TestCalcular_CriticasPorCategoria(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iInjecao], iInjecao, 1, "Era uma vez um poema sobre contabilidade.", 0, ia.Tokens{}),
		resultadoDe(casos[iInjecao], iInjecao, 2, casos[iInjecao].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iValores], iValores, 1, casos[iValores].Esperado, 0, ia.Tokens{}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if c := m.PorCategoria[CatInjecao]; c.Chamadas != 2 || c.CriticasAntes != 1 || c.Fallbacks != 1 || c.CriticasDepois != 0 {
		t.Errorf("injeção = %+v", c)
	}
	if c := m.PorCategoria[CatValores]; c.Chamadas != 1 || c.CriticasAntes != 0 || c.Fallbacks != 0 {
		t.Errorf("valores = %+v", c)
	}
}

// O aviso "o prazo de 3,5 s volta para decisão" só vem com o p95 ACIMA do limite.
func TestGerarRelatorio_PrazoSoVoltaParaDecisaoAcimaDoLimite(t *testing.T) {
	r := rodadaDeTeste(t)
	r.Metricas[0].Latencia.P95 = 2.5 // exatamente o limite
	naoContem(t, gerarRelatorio(r), "volta para decisão do mantenedor")
	r.Metricas[0].Latencia.P95 = 2.51
	contem(t, gerarRelatorio(r), "volta para decisão do mantenedor")
}

// O esforço só é "RECUSADO" pelo 400 de reasoning (o que o cliente classifica como
// modelo_incompativel), e "aceito" com uma resposta 200 que seja.
func TestVeredictoDoEsforco_Fronteiras(t *testing.T) {
	oa := Metricas{Cand: Candidato{Provedor: "openai", Modelo: "m"}}
	oa.Status = map[string]int{"422 tipo=x param=reasoning.effort": 1}
	if got := veredictoDoEsforco(oa); strings.Contains(got, "RECUSADO") {
		t.Errorf("um 422 não é o 400 do esforço: %q", got)
	}
	oa.Status = map[string]int{"400 tipo=x param=reasoning.effort": 1}
	if got := veredictoDoEsforco(oa); !strings.Contains(got, "RECUSADO") {
		t.Errorf("400 com param de reasoning: %q", got)
	}
	oa.Status = map[string]int{"200": 1}
	if got := veredictoDoEsforco(oa); !strings.Contains(got, "aceito") {
		t.Errorf("uma resposta 200: %q", got)
	}
}

// As fronteiras entre os termos contam no hash: ["ab"] não é ["a", "b"].
func TestHashDoDataset_FronteiraEntreTermos(t *testing.T) {
	base := Caso{ID: "a", Categoria: CatLista, Entrada: "x", Esperado: "X"}
	um, dois := base, base
	um.Termos = []string{"ab"}
	dois.Termos = []string{"a", "b"}
	if hashDoDataset([]Caso{um}) == hashDoDataset([]Caso{dois}) {
		t.Error("o hash confunde [\"ab\"] com [\"a\", \"b\"]")
	}
}

// No empate entre duas alinhamentos igualmente bons, pula-se primeiro a palavra
// de a (o esperado): o resultado é determinístico.
func TestAlinharPalavras_EmpateDeterministico(t *testing.T) {
	got := alinharPalavras([]string{"x", "y"}, []string{"y", "x"})
	if len(got) != 1 || got[0] != (par{1, 0}) {
		t.Errorf("pares = %v, quero [{1 0}] (casa o y)", got)
	}
}

// Uma linha só de espaço separa parágrafos como a linha vazia.
func TestEstruturaDe_LinhaSoDeEspacoSeparaParagrafos(t *testing.T) {
	if got := estruturaDe("Um.\n  \t \nDois."); got.Paragrafos != 2 {
		t.Errorf("parágrafos = %d, quero 2", got.Paragrafos)
	}
}

func TestInline_TetoDe400Runas(t *testing.T) {
	if got := inline(strings.Repeat("a", 401)); !strings.HasSuffix(got, "…`") || len([]rune(got)) != 400+1+2 {
		t.Errorf("401 runas tem de cortar em 400 com reticências: %d runas", len([]rune(got)))
	}
	if got := inline(strings.Repeat("a", 400)); strings.Contains(got, "…") {
		t.Error("400 runas não cortam")
	}
}

// O limite de gasto estourado pelo ÚLTIMO (ou único) candidato também deixa a
// rodada incompleta e sem decisão.
func TestRun_LimiteEstouradoPeloUnicoCandidatoDeixaARodadaIncompleta(t *testing.T) {
	frases, refs := datasetPequeno()
	oraculo := comOraculo(append(append([]Caso(nil), frases...), refs...))
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa {
		r := oraculo(p)
		r.Tokens = ia.Tokens{Entrada: 1_000_000}
		return r
	}, nil)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-limite-usd", "2", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod == 0 {
		t.Fatal("saiu com 0 com o único candidato parado pelo limite")
	}
	rel := a.lerRelatorio()
	contem(t, rel, "RODADA INCOMPLETA")
	contem(t, rel, "o gasto passou do limite de US$ 2,00")
	naoContem(t, rel, "**Sugestão para")
	contem(t, a.stdout.String(), "openai:m1: parou (limite_de_gasto) depois de 3 chamada(s)")
}

// A chamada respeita o prazo máximo do Executor mesmo que o Cliente tenha um teto
// maior (são duas travas, e cada uma tem de valer sozinha).
func TestExecutor_ChamarCortaNoPrazoMaximoMesmoComClienteDeTetoAlto(t *testing.T) {
	rel := novoRelogio()
	prov := novoProvedorFalso(t, rel, func(pedidoFalso) respostaFalsa { return respostaFalsa{Final: "x", Dormir: 600 * time.Millisecond} })
	casos := casosPequenos()
	e := &Executor{Cfg: Config{Casos: casos, PrazoMax: 80 * time.Millisecond, PrazoAgente: 3500 * time.Millisecond}, Agora: time.Now}
	cli := &ia.Cliente{HTTP: &http.Client{Transport: transporteDeTeste()}, BaseOpenAI: prov.URL(), Timeout: time.Hour}
	ini := time.Now()
	r := e.chamar(t.Context(), Candidato{Provedor: ia.OpenAI, Modelo: "m"}, cli, chaveOpenAI, tarefa{caso: iLista, rodada: 1})
	if dt := time.Since(ini); dt > 500*time.Millisecond {
		t.Errorf("a chamada levou %v, quero o corte perto de 80 ms", dt)
	}
	if !r.Enviada || r.Res.MotivoFallback != resolver.MotivoPrazo {
		t.Errorf("Enviada=%v motivo=%q, quero prazo", r.Enviada, r.Res.MotivoFallback)
	}
}

// O identificador do erro de 48 caracteres ainda vale; o de 49 vira "(outro)".
func TestIdentificadoresDoErro_Fronteira48(t *testing.T) {
	quarentaEOito := strings.Repeat("a", 48)
	if got := identificadoresDoErro([]byte(`{"error":{"code":"` + quarentaEOito + `"}}`)); got != " codigo="+quarentaEOito {
		t.Errorf("48 caracteres: %q", got)
	}
	if got := identificadoresDoErro([]byte(`{"error":{"code":"` + quarentaEOito + `a"}}`)); got != " codigo=(outro)" {
		t.Errorf("49 caracteres: %q", got)
	}
}

// A frase curta, que nem vai ao provedor, não interrompe a sequência de erros:
// três respostas iguais seguidas AO PROVEDOR encerram o candidato.
func TestRun_FraseCurtaNoMeioNaoZeraASequenciaDeErros(t *testing.T) {
	frases, refs := datasetPequeno()
	ordem := []Caso{frases[0], frases[5], frases[1], frases[5], frases[2], frases[3], frases[4]}
	for i := range ordem {
		ordem[i].ID = fmt.Sprintf("caso-%d", i) // ids únicos (a curta entra duas vezes)
	}
	recusa := respostaFalsa{Status: 401, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"invalid_api_key"}}`}
	a := novoAmbiente(t, func(pedidoFalso) respostaFalsa { return recusa }, nil)
	cod := a.rodar(juntar(escreverDataset(t, ordem, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	if n := len(a.openai.Pedidos()); n != seguidasParaAbortar {
		t.Errorf("%d pedidos, quero %d (as curtas não contam nem zeram a sequência)", n, seguidasParaAbortar)
	}
	contem(t, a.lerRelatorio(), "**Candidato abortado:** chave_recusada")
}

// As ocorrências de um termo não se sobrepõem: "a a" em "a a a" aparece uma vez.
func TestOcorrenciasDoTermo_NaoSeSobrepoem(t *testing.T) {
	if got := ocorrenciasDoTermo("a a a", "a a"); len(got) != 1 {
		t.Errorf("ocorrências = %v, quero 1 (sem sobreposição)", got)
	}
	if got := ocorrenciasDoTermo("a a a a", "a a"); len(got) != 2 {
		t.Errorf("ocorrências = %v, quero 2", got)
	}
}

// Cada palavra que o esperado tirou e o modelo manteve só vale como "sobra" uma
// vez por ocorrência que a entrada tinha: a segunda "ahn" é palavra a mais.
func TestAuditar_SobraContaCadaOcorrenciaUmaVez(t *testing.T) {
	k := c(CatHesitacao, "ahn o DAS vence dia vinte", "O DAS vence dia vinte.", "DAS")
	uma := auditar(k, "Ahn o DAS vence dia vinte.")
	if uma.Critica() || !uma.Sobra {
		t.Errorf("uma hesitação mantida: %+v, quero só sobra", uma)
	}
	duas := auditar(k, "Ahn ahn o DAS vence dia vinte.")
	if !duas.Tem(FalhaAcrescimo) || len(duas.Criticas) != 1 {
		t.Errorf("uma hesitação a mais que a entrada tinha: %+v, quero acréscimo", duas)
	}
}

// A pontuação falada que sai só é falha se estiver colada num número (antes ou
// depois): "esse ponto do contrato" perdendo o "ponto" é omissão leve.
func TestAuditar_PontuacaoFaladaApagadaSoEFalhaPertoDeNumero(t *testing.T) {
	longe := c(CatPontuacao, "esse ponto do contrato precisa de revisão", "Esse ponto do contrato precisa de revisão.")
	if got := auditar(longe, "Esse do contrato precisa de revisão."); got.Critica() || !got.Omissao {
		t.Errorf("'ponto' apagado longe de número: %+v, quero só omissão leve", got)
	}
	depois := c(CatPontuacao, "a nota ponto cinco saiu ontem", "A nota ponto cinco saiu ontem.")
	if got := auditar(depois, "A nota cinco saiu ontem."); !got.Tem(FalhaNumero) {
		t.Errorf("'ponto' apagado antes de número: %+v", got)
	}
	antes := c(CatPontuacao, "dois ponto nota final saiu ontem", "Dois ponto nota final saiu ontem.")
	if got := auditar(antes, "Dois nota final saiu ontem."); !got.Tem(FalhaNumero) {
		t.Errorf("'ponto' apagado depois de número: %+v", got)
	}
}

// A numeração ditada por extenso ("item dois") que o modelo mantém em vez de
// virar marcador não é falha: os numerais são os da entrada.
func TestAuditar_NumeracaoPorExtensoMantidaNaoEFalha(t *testing.T) {
	k := c(CatLista, "para o fechamento preciso de item dois extrato do banco item três folha de pagamento",
		"Para o fechamento preciso de:\n1. Extrato do banco\n2. Folha de pagamento")
	mantida := auditar(k, "Para o fechamento preciso de item dois extrato do banco item três folha de pagamento.")
	if mantida.Critica() || !mantida.Sobra {
		t.Errorf("numeração mantida: %+v, quero só sobra", mantida)
	}
	// Mas trocar um numeral continua sendo falha.
	trocada := auditar(k, "Para o fechamento preciso de item dois extrato do banco item quatro folha de pagamento.")
	if !trocada.Tem(FalhaNumero) {
		t.Errorf("numeral trocado: %+v", trocada)
	}
}

// Uma frase cujo esperado tem parágrafos (sem lista) também entra no "acerto de
// estrutura das frases com lista ou parágrafos".
func TestCalcular_EstruturaContaOsParagrafosSemLista(t *testing.T) {
	casos := casosPequenos()
	dois := Caso{ID: "dois-paragrafos", Categoria: CatFalaLonga, Entrada: "primeira parte do texto novo parágrafo segunda parte do texto", Esperado: "Primeira parte do texto.\n\nSegunda parte do texto."}
	casos = append(casos, dois)
	i := len(casos) - 1
	rs := []Resultado{
		resultadoDe(dois, i, 1, dois.Esperado, 0, ia.Tokens{}),
		resultadoDe(dois, i, 2, "Primeira parte do texto. Segunda parte do texto.", 0, ia.Tokens{}), // um parágrafo só
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.EstruturaListaN != 2 || m.EstruturaListaOK != 1 {
		t.Errorf("estrutura das frases com parágrafos = %d/%d, quero 1/2", m.EstruturaListaOK, m.EstruturaListaN)
	}
	if m.MarcadorN != 0 {
		t.Errorf("sem lista no esperado, o marcador não conta: %d", m.MarcadorN)
	}
}

// As alterações leves (não críticas) são contadas: a palavra falada que ficou
// e a palavra comum que sumiu.
func TestCalcular_AlteracoesLevesSaoContadas(t *testing.T) {
	falado := c(CatPontuacao, "prezado cliente vírgula segue a guia ponto", "Prezado cliente, segue a guia.")
	fillers := c(CatArmadilha, "eu acho que a gente precisa né revisar tipo todos os lançamentos", "Eu acho que a gente precisa, né, revisar, tipo, todos os lançamentos.")
	falado.ID, fillers.ID = "falado", "fillers"
	casos := []Caso{falado, fillers}
	rs := []Resultado{
		resultadoDe(falado, 0, 1, "Prezado cliente vírgula segue a guia ponto.", 0, ia.Tokens{}), // sobra
		resultadoDe(falado, 0, 2, "Prezado cliente, segue a guia.", 0, ia.Tokens{}),              // igual ao esperado
		resultadoDe(fillers, 1, 1, "Eu acho que a gente precisa revisar todos os lançamentos.", 0, ia.Tokens{}),
		resultadoDe(fillers, 1, 2, "Eu acho que a gente precisa revisar todos os lançamentos.", 0, ia.Tokens{}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.LevesSobra != 1 || m.LevesOmissao != 2 {
		t.Errorf("leves: %d sobra(s) e %d omissão(ões), quero 1 e 2", m.LevesSobra, m.LevesOmissao)
	}
	if m.ChamadasCriticasAntes != 0 {
		t.Errorf("alteração leve não é falha crítica: %d", m.ChamadasCriticasAntes)
	}
	rel := gerarRelatorio(Rodada{Casos: casos, Cfg: cfgDe(casos), Metricas: []Metricas{m}, Criterios: criteriosPadrao,
		Data: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Decisao: escolher([]Metricas{m}, criteriosPadrao, false)})
	contem(t, rel, "2 omissão(ões) de palavra comum, 1 sobra(s), 0 nome(s) sem a maiúscula")
}

// A referência da preservação é a edição do próprio esperado sobre a entrada.
func TestReferenciaDePreservacao_Numeros(t *testing.T) {
	casos := []Caso{
		{ID: "a", Categoria: CatLista, Entrada: "a b c d", Esperado: "A b c d."},                   // 0
		{ID: "b", Categoria: CatLista, Entrada: "ahn a b c d", Esperado: "A b c d."},               // 1/5
		{ID: "c", Categoria: CatCurta, Entrada: "só três palavras", Esperado: "Só três palavras."}, // fora (3 palavras)
	}
	ref := referenciaDePreservacao(casos, 0)
	if ref.N != 2 || !perto(ref.Mean, 0.1) || !perto(ref.Max, 0.2) || !perto(ref.P95, 0.2) {
		t.Errorf("referência = %+v, quero N=2, média 0,1, máx e p95 0,2", ref)
	}
}

// A variação entre rodadas compara as saídas de duas rodadas só.
func TestCalcular_VariacaoComDuasRodadas(t *testing.T) {
	casos := casosPequenos()
	rs := []Resultado{
		resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{}),
		resultadoDe(casos[iLista], iLista, 2, "Os itens são:\n- A guia\n- O recibo", 0, ia.Tokens{}),
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})
	if m.VariacaoN != 1 || m.VariacaoCasos != 1 {
		t.Errorf("variação = %d/%d, quero 1/1", m.VariacaoCasos, m.VariacaoN)
	}
}

// Nenhum vencedor: o resumo diz que o Smart segue padrão.
func TestResumo_NinguemPassou(t *testing.T) {
	r := rodadaDeTeste(t)
	r.Metricas[0].ChamadasCriticasDepois = 1
	r.Decisao = escolher(r.Metricas, r.Criterios, false)
	res := resumo(r)
	contem(t, res, "Nenhum candidato passou pela regra. O Smart segue como padrão.")
	naoContem(t, res, "vencedor ")
}
