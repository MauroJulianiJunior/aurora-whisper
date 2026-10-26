package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// O modo -notas-lista (ADR-022, N5) sem sair da máquina: o dataset real
// conferido com a guarda, as métricas sobre medidas montadas à mão e o comando
// inteiro contra o provedor falso em loopback.

const caminhoNotasListaReal = "../../testdata/polish-dataset/notas-lista.json"

func casosDaListaReais(t *testing.T) []CasoLista {
	t.Helper()
	dados, err := os.ReadFile(caminhoNotasListaReal)
	if err != nil {
		t.Fatal(err)
	}
	casos, err := carregarCasosDaLista(dados)
	if err != nil {
		t.Fatal(err)
	}
	return casos
}

// TestNotasLista_DatasetReal: o dataset passa na validação (o esperado de cada
// caso é o que a guarda devolve) e cobre o que a N5 pede: com e sem vírgula,
// número com vírgula, unidade, sigla, injeção e o item único que não se separa.
func TestNotasLista_DatasetReal(t *testing.T) {
	casos := casosDaListaReais(t)
	if err := validarCasosDaLista(casos); err != nil {
		t.Fatal(err)
	}
	if len(casos) < 20 {
		t.Fatalf("só %d casos; a N5 pede cerca de 20", len(casos))
	}
	porTipo := map[string]int{}
	for _, c := range casos {
		porTipo[c.Tipo]++
	}
	for _, tipo := range tiposDaLista {
		if porTipo[tipo] == 0 {
			t.Errorf("nenhum caso do tipo %s", tipo)
		}
	}
	// Nenhum trecho repete um exemplo do prompt (o modelo só copiaria).
	for _, c := range casos {
		if strings.Contains(resolver.NotasListaPrompt, `"trecho":"`+c.Trecho+`"`) {
			t.Errorf("%s repete um exemplo do prompt", c.ID)
		}
	}
}

func TestNotasLista_ValidacaoRecusaDatasetIncoerente(t *testing.T) {
	bom := CasoLista{ID: "a", Categoria: CatNotasLista, Tipo: "virgula", Trecho: "arroz, feijão", Esperado: []string{"Arroz", "Feijão"}}
	casos := []struct {
		nome string
		muda func(c *CasoLista)
	}{
		{"categoria", func(c *CasoLista) { c.Categoria = "lista" }},
		{"tipo", func(c *CasoLista) { c.Tipo = "outro" }},
		{"várias linhas", func(c *CasoLista) { c.Trecho = "arroz\nfeijão" }},
		{"esperado com palavra a mais", func(c *CasoLista) { c.Esperado = []string{"Arroz", "Feijão preto"} }},
		{"um item que não é o trecho", func(c *CasoLista) { c.Esperado = []string{"arroz"} }},
		{"esperado vazio", func(c *CasoLista) { c.Esperado = nil }},
	}
	if err := validarCasosDaLista([]CasoLista{bom}); err != nil {
		t.Fatalf("o caso bom foi recusado: %v", err)
	}
	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			c := bom
			c.Esperado = slices.Clone(bom.Esperado)
			tc.muda(&c)
			if err := validarCasosDaLista([]CasoLista{c}); err == nil {
				t.Fatal("aceitou")
			}
		})
	}
	if err := validarCasosDaLista([]CasoLista{bom, bom}); err == nil {
		t.Fatal("aceitou id repetido")
	}
	if _, err := carregarCasosDaLista([]byte(`{"versao":1,"casos":[{"id":"x","campo_novo":1}]}`)); err == nil {
		t.Fatal("aceitou campo desconhecido")
	}
}

// TestNotasLista_Metricas: as contas sobre medidas montadas à mão.
func TestNotasLista_Metricas(t *testing.T) {
	casos := []CasoLista{
		{ID: "a", Tipo: "sem_virgula", Trecho: "leite pão café", Esperado: []string{"Leite", "Pão", "Café"}},
		{ID: "b", Tipo: "um_item", Trecho: "ver a padaria", Esperado: []string{"ver a padaria"}},
		{ID: "c", Tipo: "virgula", Trecho: "arroz, feijão", Esperado: []string{"Arroz", "Feijão"}},
	}
	s := SaidaDaLista{Cand: Candidato{Provedor: ia.OpenAI, Modelo: "m"}, Custo: 0.01, Medidas: []MedidaDaLista{
		// A IA acertou.
		{Caso: 0, Res: resolver.ResultadoDaLista{Itens: []string{"Leite", "Pão", "Café"}, Como: resolver.SeparouComIA}, Chamou: true, Duracao: time.Second},
		// A IA errou o corte (um item junto) e a guarda aceitou: F1 baixa.
		{Caso: 0, Res: resolver.ResultadoDaLista{Itens: []string{"Leite pão", "Café"}, Como: resolver.SeparouComIA}, Chamou: true, Duracao: 2 * time.Second},
		// O modelo disse um item só: nada separou, e o esperado era isso.
		{Caso: 1, Res: resolver.ResultadoDaLista{Como: resolver.SeparouNenhum, Motivo: resolver.ListaUmItem}, Chamou: true, Duracao: time.Second},
		// A guarda recusou e a regra acertou.
		{Caso: 2, Res: resolver.ResultadoDaLista{Itens: []string{"Arroz", "Feijão"}, Como: resolver.SeparouPorRegra, Motivo: resolver.MotivoDaLista(resolver.MotivoPalavras)}, Chamou: true, Duracao: 9 * time.Second},
		// Sem rede.
		{Caso: 2, Res: resolver.ResultadoDaLista{Itens: []string{"Arroz", "Feijão"}, Como: resolver.SeparouPorRegra, Motivo: resolver.MotivoDaLista(resolver.MotivoTransporte)}, Chamou: true},
	}}
	m := calcularDaLista(casos, s)
	if m.Medidas != 5 || m.ComIA != 2 || m.PorRegra != 2 || m.Nenhum != 1 {
		t.Fatalf("contagens: %+v", m)
	}
	if m.Recusas["palavras"] != 1 || m.UmItem != 1 || m.Erros["transporte"] != 1 {
		t.Fatalf("recusas %v, um item %d, erros %v", m.Recusas, m.UmItem, m.Erros)
	}
	if m.Exatas != 4 || m.PorTipo["sem_virgula"] != [2]int{1, 2} || m.PorTipo["um_item"] != [2]int{1, 1} {
		t.Fatalf("exatas %d, por tipo %v", m.Exatas, m.PorTipo)
	}
	// Itens: 3+0+1+2+2 acertos; "Leite pão" a mais; "leite" e "pão" faltando.
	if m.TP != 9 || m.FP != 1 || m.FN != 2 || m.TPIA != 4 || m.FPIA != 1 || m.FNIA != 2 {
		t.Fatalf("TP/FP/FN = %d/%d/%d, IA %d/%d/%d", m.TP, m.FP, m.FN, m.TPIA, m.FPIA, m.FNIA)
	}
	if m.ForaDoTrecho != 0 || m.AcimaDoPrazo != 1 || m.FallbackTaxa() != 0.6 {
		t.Fatalf("fora %d, acima do prazo %d, fallback %v", m.ForaDoTrecho, m.AcimaDoPrazo, m.FallbackTaxa())
	}

	// Um item que não é pedaço do trecho é o buraco na guarda, e o relatório grita.
	s.Medidas = append(s.Medidas, MedidaDaLista{Caso: 0, Res: resolver.ResultadoDaLista{Itens: []string{"Leite", "Chocolate"}, Como: resolver.SeparouComIA}})
	m = calcularDaLista(casos, s)
	if m.ForaDoTrecho != 1 {
		t.Fatalf("fora do trecho = %d", m.ForaDoTrecho)
	}
	rel := relatorioDaLista(relatorioDaListaDados{Data: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Casos: casos, Hash: "abc", Rodadas: 1, Metricas: []MetricasDaLista{m}})
	contem(t, rel, "BURACO NA GUARDA")
	contem(t, rel, "- `a` (ia): esperado `Leite` · `Pão` · `Café`; saiu `Leite` · `Chocolate`")
}

// trechoDoPedido lê o trecho da mensagem que o comando mandou.
func trechoDoPedido(p pedidoFalso) string {
	var conteudo string
	for _, campo := range []string{"input", "messages"} {
		if ms, ok := p.Corpo[campo].([]any); ok && len(ms) > 0 {
			conteudo, _ = ms[0].(map[string]any)["content"].(string)
		}
	}
	var msg struct {
		Trecho string `json:"trecho"`
	}
	_ = json.Unmarshal([]byte(conteudo), &msg)
	return msg.Trecho
}

// oraculoDaLista é o modelo perfeito: devolve o esperado de cada trecho.
func oraculoDaLista(casos []CasoLista, mexer func([]string) []string) comportamento {
	m := map[string][]string{}
	for _, c := range casos {
		m[c.Trecho] = c.Esperado
	}
	return func(p pedidoFalso) respostaFalsa {
		itens := slices.Clone(m[trechoDoPedido(p)])
		if mexer != nil {
			itens = mexer(itens)
		}
		b, _ := json.Marshal(map[string]any{"itens": itens})
		return respostaFalsa{TextoDoModelo: string(b), Tokens: ia.Tokens{Entrada: 900, Saida: 40}, Atraso: 800 * time.Millisecond}
	}
}

func TestRun_NotasListaComOModeloPerfeito(t *testing.T) {
	casos := casosDaListaReais(t)
	a := novoAmbiente(t, oraculoDaLista(casos, nil), nil)
	cod := a.rodar("-gastar", "-notas-lista", "-notas-lista-frases", caminhoNotasListaReal, "-rodadas", "2", "-modelos", "openai:gpt-6-luna")
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "# Benchmark da separação em itens das notas — 2026-09-30")
	contem(t, rel, resolver.NotasListaPromptRef)
	contem(t, rel, resolver.NotasListaPromptHash())
	contem(t, rel, hashDosCasosDaLista(casos))
	contem(t, rel, "-gastar -rodadas 2 -modelos openai:gpt-6-luna -notas-lista")
	contem(t, rel, "Nada: toda lista saiu como a esperada.")
	naoContem(t, rel, "BURACO NA GUARDA")
	contem(t, a.stdout.String(), "listas exatas 100,0%; fora do trecho 0")
	// Todo trecho tem duas palavras ou mais: todos vão ao provedor, nas duas rodadas.
	if n := len(a.openai.Pedidos()); n != 2*len(casos) {
		t.Fatalf("a OpenAI recebeu %d pedidos, quero %d", n, 2*len(casos))
	}
	p := a.openai.Pedidos()[0]
	formato := p.Corpo["text"].(map[string]any)["format"].(map[string]any)
	if formato["name"] != resolver.NotasListaSchemaNome || p.Corpo["instructions"] != resolver.NotasListaPrompt || trechoDoPedido(p) != casos[0].Trecho {
		t.Fatalf("o pedido não é o da separação em itens: %v", formato["name"])
	}
	if strings.Contains(a.tudoQueSaiu(), chaveOpenAI) {
		t.Fatal("a chave apareceu na saída")
	}
}

// TestRun_NotasListaModeloQueInventa: o modelo acrescenta um item que não
// estava no trecho. A guarda recusa todas, a regra assume onde há vírgula, e
// nenhum item fora do trecho chega à pessoa.
func TestRun_NotasListaModeloQueInventa(t *testing.T) {
	casos := casosDaListaReais(t)
	inventa := func(itens []string) []string { return append(itens, "chocolate") }
	a := novoAmbiente(t, oraculoDaLista(casos, inventa), nil)
	cod := a.rodar("-gastar", "-notas-lista", "-notas-lista-frases", caminhoNotasListaReal, "-modelos", "openai:gpt-6-luna")
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	rel := a.lerRelatorio()
	contem(t, rel, "palavras ")
	naoContem(t, rel, "BURACO NA GUARDA")
	contem(t, a.stdout.String(), "IA valeu em 0,0%")
	contem(t, a.stdout.String(), "fora do trecho 0")
}

// TestRun_NotasListaParaNaChaveRecusada: 401 é motivo terminal; o candidato
// para na primeira chamada, e o relatório diz.
func TestRun_NotasListaParaNaChaveRecusada(t *testing.T) {
	a := novoAmbiente(t, func(pedidoFalso) respostaFalsa {
		return respostaFalsa{Status: 401, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"invalid_api_key"}}`}
	}, nil)
	cod := a.rodar("-gastar", "-notas-lista", "-notas-lista-frases", caminhoNotasListaReal, "-modelos", "openai:gpt-6-luna")
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	if n := len(a.openai.Pedidos()); n != 1 {
		t.Fatalf("%d pedidos depois da chave recusada", n)
	}
	contem(t, a.lerRelatorio(), "(parou: chave_recusada)")
}

func TestRun_NotasListaRecusaEstimativaAcimaDoLimite(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	cod := a.rodar("-gastar", "-notas-lista", "-notas-lista-frases", caminhoNotasListaReal, "-rodadas", "20",
		"-modelos", "openai:gpt-6-luna", "-preco", "openai:gpt-6-luna=500/500", "-limite-usd", "0.01")
	if cod != 2 || len(a.leitor.Prompts()) != 0 || len(a.openai.Pedidos()) != 0 {
		t.Fatalf("cod %d, chaves pedidas %v, pedidos %d", cod, a.leitor.Prompts(), len(a.openai.Pedidos()))
	}
}

// TestComandoNotasLista_TemSoFlagsQueExistem: o comando do roteiro manual e da
// spec roda contra o dublê (o -gastar e o -notas-lista existem).
func TestComandoNotasLista_TemSoFlagsQueExistem(t *testing.T) {
	casos := casosDaListaReais(t)
	a := novoAmbiente(t, oraculoDaLista(casos, nil), nil)
	args := strings.Fields(strings.TrimPrefix(comandoNotasLista, "go run ./cmd/polishbench "))
	args = append(args, "-notas-lista-frases", caminhoNotasListaReal)
	if cod := a.rodar(args...); cod != 0 {
		t.Fatalf("o comando impresso não roda: %d\n%s", cod, a.stderr.String())
	}
}
