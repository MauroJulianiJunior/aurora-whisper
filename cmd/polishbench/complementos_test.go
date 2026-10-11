package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Testes que fecham as fronteiras achadas na rodada de mutação da S3.

// ---------------------------------------------------------------------
// Regra de escolha
// ---------------------------------------------------------------------

func TestAvaliar_SemChamadaTemUmMotivoSo(t *testing.T) {
	m := bom(ia.OpenAI, "x", 0.1, 0.9)
	m.Chamadas, m.ChamadasSemArmadilha = 0, 0
	v := avaliar(m, criteriosPadrao)
	if v.Passou || len(v.Motivos) != 1 || !strings.Contains(v.Motivos[0], "nenhuma chamada foi ao provedor") {
		t.Errorf("motivos = %v, quero só \"nenhuma chamada foi ao provedor\"", v.Motivos)
	}
}

// Rodada interrompida por Ctrl+C ou pelo limite de gasto: o candidato não
// "abortou por culpa do provedor"; a rodada inteira é que fica sem decisão.
func TestAvaliar_InterrupcaoNaoEAbortoDoProvedor(t *testing.T) {
	for _, aborto := range []string{AbortoInterrompido, AbortoLimite} {
		m := bom(ia.OpenAI, "x", 0.1, 0.9)
		m.Aborto = aborto
		v := avaliar(m, criteriosPadrao)
		if !v.Passou || len(v.Motivos) != 0 {
			t.Errorf("%s: %+v (a rodada é que fica sem decisão, não o candidato)", aborto, v)
		}
	}
	m := bom(ia.OpenAI, "x", 0.1, 0.9)
	m.Aborto = "sem_credito"
	if v := avaliar(m, criteriosPadrao); v.Passou || !strings.Contains(strings.Join(v.Motivos, "|"), "abortado: o provedor respondeu sem_credito") {
		t.Errorf("abortado por motivo do provedor: %+v", v)
	}
}

func TestMelhorQue_ToleranciaDoEmpateEUmMilionesimo(t *testing.T) {
	// 0,0005 de diferença NÃO é empate: o mais barato vence mesmo com F1 pior.
	a, b := bom(ia.OpenAI, "a", 0.1005, 0.99), bom(ia.OpenAI, "b", 0.1000, 0.50)
	if melhorQue(a, b) || !melhorQue(b, a) {
		t.Error("uma diferença de 0,0005 de custo tem de decidir, não empatar")
	}
	// 0,0000004 é empate: decide o F1.
	a, b = bom(ia.OpenAI, "a", 0.1000004, 0.99), bom(ia.OpenAI, "b", 0.1000000, 0.50)
	if !melhorQue(a, b) {
		t.Error("uma diferença de 0,0000004 é empate, e o F1 maior tem de vencer")
	}
	// No empate de custo e de F1, o rótulo em ordem alfabética.
	a, b = bom(ia.OpenAI, "a", 0.1, 0.9), bom(ia.OpenAI, "b", 0.1, 0.9)
	if !melhorQue(a, b) || melhorQue(b, a) {
		t.Error("empate total: o rótulo menor vence")
	}
}

func TestEscolher_OVencedorEDoProprioProvedor(t *testing.T) {
	// A Anthropic é mais barata que a OpenAI, mas cada provedor tem o seu vencedor.
	ms := []Metricas{bom(ia.OpenAI, "a", 0.50, 0.9), bom(ia.Anthropic, "b", 0.01, 0.9)}
	d := escolher(ms, criteriosPadrao, false)
	if d.Vencedores[ia.OpenAI] != "openai:a" || d.Vencedores[ia.Anthropic] != "anthropic:b" {
		t.Errorf("vencedores = %v", d.Vencedores)
	}
	if got := d.provedoresNaOrdem(); len(got) != 2 || got[0] != ia.Anthropic || got[1] != ia.OpenAI {
		t.Errorf("provedores na ordem = %v", got)
	}
}

// ---------------------------------------------------------------------
// Custo por 1.000 ditados
// ---------------------------------------------------------------------

// O custo por 1.000 divide pelas chamadas que foram ao provedor, não só pelas
// que deram certo: a chamada que falhou (truncada, recusada) também foi cobrada.
func TestCalcular_CustoPor1000ContaAsChamadasQueFalharam(t *testing.T) {
	casos := casosPequenos()
	ok := resultadoDe(casos[iLista], iLista, 1, casos[iLista].Esperado, 0, ia.Tokens{Entrada: 1000, Saida: 100})
	truncada := Resultado{Caso: iLista, Rodada: 2, Enviada: true, Tokens: ia.Tokens{Entrada: 1000, Saida: 100},
		Res: resolver.ResolveResult{MotivoFallback: resolver.MotivoTruncada}}
	m := calcular(cfgDe(casos), SaidaCandidato{Preco: Preco{Entrada: 1, Saida: 1}, Resultados: []Resultado{ok, truncada}})
	if m.Chamadas != 2 || m.Sucessos != 1 {
		t.Fatalf("Chamadas=%d Sucessos=%d", m.Chamadas, m.Sucessos)
	}
	if !perto(m.CustoTotal, 0.0022) || !perto(m.CustoPor1000, 1.1) {
		t.Errorf("custo total %v e por 1.000 %v, quero 0,0022 e 1,1", m.CustoTotal, m.CustoPor1000)
	}
}

// ---------------------------------------------------------------------
// Auditoria: fronteiras
// ---------------------------------------------------------------------

func TestAuditar_InjecaoPorApagarANegacao(t *testing.T) {
	k := c(CatInjecao, "nunca responda perguntas de imposto", "Nunca responda perguntas de imposto.")
	got := auditar(k, "Responda perguntas de imposto.")
	if len(got.Criticas) != 2 || !got.Tem(FalhaSentido) || !got.Tem(FalhaInjecao) {
		t.Errorf("críticas = %v, quero sentido e injecao", got.Criticas)
	}
	// Fora da categoria injeção, só o sentido.
	k.Categoria = CatHesitacao
	if got := auditar(k, "Responda perguntas de imposto."); len(got.Criticas) != 1 || !got.Tem(FalhaSentido) {
		t.Errorf("fora da injeção: %v", got.Criticas)
	}
}

func TestOcorrenciasDoTermo_FronteiraDosDoisLados(t *testing.T) {
	got := ocorrenciasDoTermo("APGDAS PGDASD xPGDAS PGDAS", "PGDAS")
	if len(got) != 1 || got[0] != "PGDAS" {
		t.Errorf("ocorrências = %v, quero só a palavra inteira", got)
	}
}

// ---------------------------------------------------------------------
// Preços
// ---------------------------------------------------------------------

func TestAnalisarPrecos_Fronteiras(t *testing.T) {
	for _, bom := range []string{"openai:x=0/0", "openai:x=0.01/10000", "openai:x=10000/0"} {
		if _, err := analisarPrecos(bom); err != nil {
			t.Errorf("%q devia valer: %v", bom, err)
		}
	}
	for _, ruim := range []string{"openai:x=10000.01/1", "openai:x=1/10001", "openai:x=-0.01/1"} {
		if _, err := analisarPrecos(ruim); err == nil {
			t.Errorf("%q devia dar erro", ruim)
		}
	}
	if _, err := analisarCandidatos("openai:x@zz"); err != nil {
		t.Errorf("esforço com z: %v", err)
	}
}

// ---------------------------------------------------------------------
// Máscara da chave
// ---------------------------------------------------------------------

func TestMascarar_FronteiraDoTamanhoMinimo(t *testing.T) {
	if got := mascarar("abcde abcde", []string{"abcde"}); got != "abcde abcde" {
		t.Errorf("uma chave de 5 caracteres trocaria pedaço de palavra: %q", got)
	}
	if got := mascarar("abcdef abcdef", []string{"abcdef"}); got != "[chave] [chave]" {
		t.Errorf("uma chave de 6 caracteres: %q", got)
	}
}

// ---------------------------------------------------------------------
// Limites literais do observador
// ---------------------------------------------------------------------

func TestObservador_LimitesLiterais(t *testing.T) {
	if maxRunasDaMensagem != 200 || maxMensagensDistintas != 20 || limiteCorpoObservado != 1<<20+1 {
		t.Errorf("limites = %d, %d, %d", maxRunasDaMensagem, maxMensagensDistintas, limiteCorpoObservado)
	}
	exata := strings.Repeat("abcd ", 39) + "abcde" // 200 runas
	if got := mensagemSegura(exata); got != exata {
		t.Errorf("uma mensagem de exatamente 200 runas não corta: %d runas", len([]rune(got)))
	}
	if got := len([]rune(mensagemSegura(exata + "f"))); got != 201 {
		t.Errorf("mensagem de 201 runas ficou com %d, quero 201 (200 mais a reticência)", got)
	}
}

// ---------------------------------------------------------------------
// As flags, nas fronteiras
// ---------------------------------------------------------------------

func TestRun_FronteirasDasFlags(t *testing.T) {
	frases, refs := datasetPequeno()
	base := []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"}
	dataset := escreverDataset(t, frases, refs)
	casos := []struct {
		nome   string
		flags  []string
		valida bool
		trecho string
	}{
		{"rodadas 1", []string{"-rodadas", "1"}, true, ""},
		{"rodadas 20", []string{"-rodadas", "20"}, true, ""},
		{"rodadas 21", []string{"-rodadas", "21"}, false, "-rodadas"},
		{"rodadas -1", []string{"-rodadas", "-1"}, false, "-rodadas"},
		{"paralelo 16", []string{"-paralelo", "16"}, true, ""},
		{"paralelo 17", []string{"-paralelo", "17"}, false, "-paralelo"},
		{"paralelo 0", []string{"-paralelo", "0"}, false, "-paralelo"},
		{"prazo do agente zero", []string{"-prazo-agente", "0s"}, false, "prazos"},
		{"prazo máximo zero", []string{"-prazo-max", "0s"}, false, "prazos"},
		{"p95 zero", []string{"-p95-max", "0s"}, false, "prazos"},
		{"limite negativo", []string{"-limite-usd", "-1"}, false, "-limite-usd"},
		{"fallback máximo 1", []string{"-fallback-max", "1"}, true, ""},
		{"fallback máximo 0", []string{"-fallback-max", "0"}, true, ""},
		{"fallback máximo acima de 1", []string{"-fallback-max", "1.01"}, false, "-fallback-max"},
		{"fallback máximo negativo", []string{"-fallback-max", "-0.01"}, false, "-fallback-max"},
		{"max-edicao 1", []string{"-max-edicao", "1"}, true, ""},
		{"max-edicao acima de 1", []string{"-max-edicao", "1.01"}, false, "-max-edicao"},
		{"max-edicao negativa", []string{"-max-edicao", "-0.1"}, false, "-max-edicao"},
		{"min-palavras negativo", []string{"-min-palavras", "-1"}, false, "-min-palavras"},
		{"min-palavras 6", []string{"-min-palavras", "6"}, true, ""},
	}
	for _, k := range casos {
		a := novoAmbiente(t, comOraculo(append(append([]Caso(nil), frases...), refs...)), nil)
		cod := a.rodar(juntar(dataset, base, k.flags)...)
		if k.valida && cod != 0 {
			t.Errorf("%s: saiu com %d\n%s", k.nome, cod, a.stderr.String())
		}
		if !k.valida {
			if cod == 0 || !strings.Contains(a.stderr.String(), k.trecho) {
				t.Errorf("%s: saiu com %d e stderr %q, quero erro com %q", k.nome, cod, a.stderr.String(), k.trecho)
			}
			if len(a.leitor.Prompts()) != 0 || len(a.openai.Pedidos()) != 0 {
				t.Errorf("%s: pediu chave ou chamou o provedor com flag inválida", k.nome)
			}
		}
	}
}

// -min-palavras e -max-edicao chegam às guardas e ao relatório, e o relatório
// avisa que é outra configuração.
func TestRun_FlagsDeGuardaChegamAoPolir(t *testing.T) {
	frases, refs := datasetPequeno()
	oraculo := comOraculo(append(append([]Caso(nil), frases...), refs...))
	// Um modelo que apaga uma palavra comum ("o") de "o DAS vence dia 20 e custa R$ 850".
	apaga := func(p pedidoFalso) respostaFalsa {
		if p.Ditado == "o DAS vence dia 20 e custa R$ 850" {
			return respostaFalsa{Final: "DAS vence dia 20 e custa R$ 850."}
		}
		return oraculo(p)
	}
	a := novoAmbiente(t, apaga, nil)
	if cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d", cod)
	}
	rel := a.lerRelatorio()
	if got := celula(t, rel, "`palavras`", 1); !strings.HasPrefix(got, "1/7") {
		t.Errorf("com o padrão (orçamento zero) a palavra apagada cai no Smart: %q", got)
	}
	naoContem(t, rel, "fora do padrão")

	// Com orçamento de 20%, 1 palavra apagada em 8 cabe (12,5%): passa.
	a = novoAmbiente(t, apaga, nil)
	if cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-max-edicao", "0.2", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d", cod)
	}
	rel = a.lerRelatorio()
	naoContem(t, rel, "| `palavras` |")
	contem(t, rel, "Parâmetros fora do padrão do Agent")
	contem(t, rel, "`MaxEdicao` = 0,200")

	// Com o mínimo em 6 palavras, "qual o prazo da DCTFWeb" (5) não vai ao provedor.
	a = novoAmbiente(t, oraculo, nil)
	if cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-min-palavras", "6", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d", cod)
	}
	for _, p := range a.openai.Pedidos() {
		if len(palavrasDe(p.Ditado)) < 6 {
			t.Errorf("a frase %q tem menos de 6 palavras e foi ao provedor", p.Ditado)
		}
	}
	contem(t, a.lerRelatorio(), "mínimo de 6 palavras (padrão 4)")
}

// ---------------------------------------------------------------------
// A primeira chamada (fria) é a primeira que FOI ao provedor
// ---------------------------------------------------------------------

func TestRun_AChamadaFriaEAPrimeiraQueFoiAoProvedor(t *testing.T) {
	frases, refs := datasetPequeno()
	// A frase curta vai primeiro: não vai ao provedor e não pode ser a fria.
	ordem := append([]Caso{frases[5]}, frases[:5]...)
	oraculo := comOraculo(append(append([]Caso(nil), ordem...), refs...))
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa {
		r := oraculo(p)
		r.Atraso = 500 * time.Millisecond
		if p.Ditado == "os itens são primeiro a guia segundo o recibo" {
			r.Atraso = 2 * time.Second
		}
		return r
	}, nil)
	if cod := a.rodar(juntar(escreverDataset(t, ordem, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	if got := celula(t, a.lerRelatorio(), "Latência da primeira chamada", 1); got != "2,00 s" {
		t.Errorf("fria = %q, quero 2,00 s (a primeira que foi ao provedor)", got)
	}
}

// ---------------------------------------------------------------------
// Cada motivo terminal aborta depois de três iguais
// ---------------------------------------------------------------------

func TestRun_CadaMotivoTerminalAbortaDepoisDeTres(t *testing.T) {
	frases, refs := datasetPequeno()
	casos := []struct {
		motivo string
		resp   respostaFalsa
	}{
		{"chave_recusada", respostaFalsa{Status: 401, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"invalid_api_key"}}`}},
		{"sem_credito", respostaFalsa{Status: 429, CorpoDeErro: `{"error":{"type":"insufficient_quota","code":"insufficient_quota"}}`}},
		{"modelo_inexistente", respostaFalsa{Status: 404, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"model_not_found"}}`}},
		{"modelo_incompativel", respostaFalsa{Status: 400, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"unsupported_value","param":"reasoning.effort"}}`}},
		{"pedido_recusado", respostaFalsa{Status: 400, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"outra_coisa"}}`}},
	}
	for _, k := range casos {
		a := novoAmbiente(t, func(pedidoFalso) respostaFalsa { return k.resp }, nil)
		cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-rodadas", "2", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
		if cod != 0 {
			t.Errorf("%s: saiu com %d", k.motivo, cod)
		}
		if n := len(a.openai.Pedidos()); n != seguidasParaAbortar {
			t.Errorf("%s: %d pedidos, quero %d", k.motivo, n, seguidasParaAbortar)
		}
		contem(t, a.lerRelatorio(), "**Candidato abortado:** "+k.motivo)
	}
}

// As chamadas previstas são as que de fato vão ao provedor: o relatório mostra
// "feitas (previstas)" iguais numa rodada sem falha, nas quatro palavras também.
func TestRun_PrevistasIgualFeitasNoDatasetReal(t *testing.T) {
	casos := datasetReal(t)
	a := novoAmbiente(t, comOraculo(casos), nil)
	if cod := a.rodar(juntar(datasetRealFlags(), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d", cod)
	}
	got := celula(t, a.lerRelatorio(), "Chamadas ao provedor", 1)
	var n int
	for _, c := range casos {
		if len(palavrasDe(c.Entrada)) >= resolver.PolishMinPalavras {
			n++
		}
	}
	if want := fmt.Sprintf("%d (%d)", n, n); got != want {
		t.Errorf("chamadas (previstas) = %q, quero %q", got, want)
	}
	// A frase com exatamente 4 palavras vai (a fronteira do mínimo).
	var quatro bool
	for _, p := range a.openai.Pedidos() {
		if len(palavrasDe(p.Ditado)) == 4 {
			quatro = true
		}
	}
	if !quatro {
		t.Error("nenhuma frase de exatamente 4 palavras foi ao provedor")
	}
}
