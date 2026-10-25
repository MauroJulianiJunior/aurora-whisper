package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nenhum teste deste arquivo sai da máquina: os provedores são httptest em
// loopback, a chave é um texto de teste e o console não é tocado.

const (
	flagsRealFrases   = "-frases"
	caminhoFrasesReal = "../../testdata/polish-dataset/frases.json"
	flagsRealManifest = "-asr-manifest"
	caminhoManifest   = "../../testdata/asr-dataset/manifest-normalizado.json"
)

// datasetRealFlags aponta para o dataset da rodada do mantenedor.
func datasetRealFlags() []string {
	return []string{flagsRealFrases, caminhoFrasesReal, flagsRealManifest, caminhoManifest}
}

func juntar(partes ...[]string) []string {
	var out []string
	for _, p := range partes {
		out = append(out, p...)
	}
	return out
}

func contem(t *testing.T, texto, trecho string) {
	t.Helper()
	if !strings.Contains(texto, trecho) {
		t.Errorf("faltou %q em:\n%s", trecho, texto)
	}
}

func naoContem(t *testing.T, texto, trecho string) {
	t.Helper()
	if strings.Contains(texto, trecho) {
		t.Errorf("não devia ter %q em:\n%s", trecho, texto)
	}
}

// ---------------------------------------------------------------------
// Recusa, ajuda e argumentos
// ---------------------------------------------------------------------

// Sem -gastar o comando recusa ANTES de qualquer coisa: nenhuma chave
// pedida, nenhum arquivo lido, nenhuma conexão (nem para o provedor de teste,
// nem para fora), nenhum relatório.
func TestRun_RecusaSemGastar(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	cod := run(a.ctx, []string{"-modelos", "openai:gpt-6-luna", "-relatorio", a.relatorio}, &a.stdout, &a.stderr, a.deps())

	if cod == 0 {
		t.Fatal("rodou sem -gastar")
	}
	contem(t, a.stderr.String(), "-gastar")
	contem(t, a.stderr.String(), "recusado")
	if got := a.leitor.Prompts(); len(got) != 0 {
		t.Errorf("pediu a chave sem -gastar: %v", got)
	}
	if a.arquivosLidos != 0 {
		t.Errorf("leu %d arquivo(s) sem -gastar", a.arquivosLidos)
	}
	if n := len(a.openai.Pedidos()) + len(a.anthropic.Pedidos()); n != 0 {
		t.Errorf("abriu %d conexão(ões) sem -gastar", n)
	}
	if len(recusados.lista()) != 0 {
		t.Errorf("tentou sair da máquina: %v", recusados.lista())
	}
	if _, err := os.Stat(a.relatorio); err == nil {
		t.Error("gravou relatório sem -gastar")
	}
}

// A recusa vale mesmo com todas as outras flags certas, e mesmo sem -modelos:
// a mensagem é sempre a do -gastar.
func TestRun_RecusaSemGastar_QualquerCombinacao(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-modelos", "openai:gpt-6-luna,anthropic:claude-haiku-4-5", "-rodadas", "3"},
		{"-gastar=false", "-modelos", "openai:gpt-6-luna"},
	} {
		a := novoAmbiente(t, nil, nil)
		cod := run(a.ctx, args, &a.stdout, &a.stderr, a.deps())
		if cod == 0 {
			t.Errorf("%v: rodou sem -gastar", args)
		}
		contem(t, a.stderr.String(), "-gastar")
		if len(a.leitor.Prompts()) != 0 || a.arquivosLidos != 0 || len(a.openai.Pedidos()) != 0 {
			t.Errorf("%v: fez algo antes de recusar", args)
		}
	}
}

func TestRun_AjudaMostraOComandoEOsPrecos(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	cod := run(a.ctx, []string{"-help"}, &a.stdout, &a.stderr, a.deps())
	if cod != 0 {
		t.Fatalf("-help saiu com %d", cod)
	}
	ajuda := a.stderr.String()
	contem(t, ajuda, "go run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna,openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5")
	contem(t, ajuda, "-linha-de-base openai:gpt-5-nano@minimal")
	contem(t, ajuda, "-conexao-nova")
	contem(t, ajuda, "-f1-min")
	contem(t, ajuda, "-estrutura-min")
	contem(t, ajuda, "Chave OpenAI (Enter pula):")
	contem(t, ajuda, "openai:gpt-6-luna=0.10/0.50")
	contem(t, ajuda, "openai:gpt-5-nano=0.05/0.40")
	contem(t, ajuda, "anthropic:claude-haiku-4-5=1.00/5.00")
	contem(t, ajuda, "-gastar")
	contem(t, ajuda, "-rodadas")
	for _, l := range strings.Split(ajuda, "\n") {
		if strings.Contains(l, "go -C") && strings.Contains(l, "&&") {
			t.Errorf("o comando do -help usa && (o PowerShell 5.1 não tem): %s", l)
		}
	}
	if len(a.leitor.Prompts()) != 0 || a.arquivosLidos != 0 {
		t.Error("o -help pediu chave ou leu arquivo")
	}
}

func TestRun_ArgumentosInvalidos(t *testing.T) {
	casos := []struct {
		nome   string
		args   []string
		trecho string
	}{
		{"sem modelos", []string{"-gastar"}, "nenhum modelo"},
		{"provedor desconhecido", []string{"-gastar", "-modelos", "google:x"}, "provedor"},
		{"esforço na anthropic", []string{"-gastar", "-modelos", "anthropic:claude-haiku-4-5@low"}, "só vale na OpenAI"},
		{"sem preço", []string{"-gastar", "-modelos", "openai:gpt-desconhecido"}, "sem preço"},
		{"preço de modelo fora da lista", []string{"-gastar", "-modelos", "openai:gpt-6-luna", "-preco", "openai:gpt-6-lunar=1/1"}, "erro de digitação"},
		{"rodadas", []string{"-gastar", "-modelos", "openai:gpt-6-luna", "-rodadas", "0"}, "-rodadas"},
		{"paralelo", []string{"-gastar", "-modelos", "openai:gpt-6-luna", "-paralelo", "99"}, "-paralelo"},
		{"limite", []string{"-gastar", "-modelos", "openai:gpt-6-luna", "-limite-usd", "0"}, "-limite-usd"},
		{"argumento solto", []string{"-gastar", "-modelos", "openai:gpt-6-luna", "sk-1234567890"}, "argumento inesperado"},
		{"flag desconhecida", []string{"-gastar", "-chave", "sk-1234567890"}, "flag provided but not defined"},
	}
	for _, c := range casos {
		a := novoAmbiente(t, nil, nil)
		cod := run(a.ctx, c.args, &a.stdout, &a.stderr, a.deps())
		if cod == 0 {
			t.Errorf("%s: saiu com 0", c.nome)
		}
		contem(t, a.stderr.String(), c.trecho)
		naoContem(t, a.stderr.String(), "sk-1234567890")
		if len(a.leitor.Prompts()) != 0 || len(a.openai.Pedidos()) != 0 {
			t.Errorf("%s: pediu chave ou chamou o provedor com argumento inválido", c.nome)
		}
	}
}

// A chave NÃO tem flag: não há como passá-la pela linha de comando.
func TestRun_NaoHaFlagDeChave(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	run(a.ctx, []string{"-help"}, &a.stdout, &a.stderr, a.deps())
	ajuda := strings.ToLower(a.stderr.String())
	for _, flag := range []string{"-chave", "-key", "-token", "-api-key", "-senha"} {
		naoContem(t, ajuda, "\n  "+flag+" ")
	}
}

func TestRun_ArquivoDeFrasesInexistente(t *testing.T) {
	a := novoAmbiente(t, nil, nil)
	cod := a.rodar("-gastar", "-modelos", "openai:gpt-6-luna", "-frases", filepath.Join(t.TempDir(), "nao-existe.json"))
	if cod == 0 {
		t.Fatal("saiu com 0")
	}
	contem(t, a.stderr.String(), "lendo as frases")
	if len(a.leitor.Prompts()) != 0 {
		t.Error("pediu a chave antes de conferir o dataset")
	}
}

// ---------------------------------------------------------------------
// A chave
// ---------------------------------------------------------------------

func TestRun_PedeUmaChavePorProvedorComOPromptCombinado(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, comOraculo(append(frases, refs...)), comOraculo(append(frases, refs...)))
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1,openai:m2@minimal,anthropic:m3", "-preco", "openai:m1=1/1,openai:m2=1/1,anthropic:m3=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	got := a.leitor.Prompts()
	if len(got) != 2 || got[0] != promptOpenAI || got[1] != promptAnthropic {
		t.Errorf("prompts = %q, quero %q e %q (uma vez por provedor, na ordem)", got, promptOpenAI, promptAnthropic)
	}
	// A chave de cada provedor chegou só ao próprio provedor, no cabeçalho.
	for _, p := range a.openai.Pedidos() {
		if p.Chave != chaveOpenAI {
			t.Fatalf("a OpenAI recebeu a chave %q", p.Chave)
		}
	}
	for _, p := range a.anthropic.Pedidos() {
		if p.Chave != chaveAnthropic {
			t.Fatalf("a Anthropic recebeu a chave %q", p.Chave)
		}
	}
	if len(a.openai.Pedidos()) == 0 || len(a.anthropic.Pedidos()) == 0 {
		t.Fatal("algum provedor não recebeu pedido")
	}
}

func TestRun_EnterPulaOProvedor(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, comOraculo(append(frases, refs...)), nil)
	a.leitor.chaves[promptAnthropic] = ""
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1,anthropic:m3", "-preco", "openai:m1=1/1,anthropic:m3=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	if n := len(a.anthropic.Pedidos()); n != 0 {
		t.Errorf("a Anthropic recebeu %d pedido(s) sem chave", n)
	}
	rel := a.lerRelatorio()
	contem(t, rel, "pulado, sem chave")
	contem(t, a.stdout.String(), "anthropic:m3: sem chave, pulado")
}

func TestRun_NenhumaChaveNaoGastaNemGrava(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, nil, nil)
	a.leitor.chaves = map[string]string{}
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod == 0 {
		t.Fatal("saiu com 0 sem nenhuma chave")
	}
	contem(t, a.stderr.String(), "nenhuma chave")
	if len(a.openai.Pedidos()) != 0 {
		t.Error("chamou o provedor sem chave")
	}
	if _, err := os.Stat(a.relatorio); err == nil {
		t.Error("gravou relatório sem ter medido nada")
	}
}

func TestRun_CtrlCNaLeituraDaChave(t *testing.T) {
	frases, refs := datasetPequeno()
	a := novoAmbiente(t, nil, nil)
	a.leitor.err = ErrInterrompido
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod == 0 {
		t.Fatal("saiu com 0")
	}
	contem(t, a.stderr.String(), "interrompido antes de gastar")
	if len(a.openai.Pedidos()) != 0 {
		t.Error("chamou o provedor depois do Ctrl+C")
	}
}

// O CANÁRIO. A chave tem de ficar fora de tudo o que o programa escreve, mesmo
// que o provedor a ecoe no corpo do erro (a OpenAI faz isso no 401), num
// cabeçalho da resposta ou no texto que o "modelo" devolve.
func TestRun_ChaveNaoAparecePorLugarNenhum(t *testing.T) {
	frases, refs := datasetPequeno()
	oa := func(p pedidoFalso) respostaFalsa {
		switch {
		case strings.Contains(p.Ditado, "DAS"): // o modelo "ecoa" a chave no texto
			return respostaFalsa{Final: "O DAS vence dia 20 e custa R$ 850. " + p.Chave, Cabecalho: map[string]string{"X-Eco": p.Chave}}
		case strings.Contains(p.Ditado, "DCTFWeb"): // erro com a chave no corpo, como a OpenAI faz
			return respostaFalsa{Status: 401, CorpoDeErro: `{"error":{"message":"Incorrect API key provided: ` + p.Chave + `.","type":"invalid_request_error","code":"invalid_api_key","param":null}}`}
		}
		return respostaFalsa{Final: p.Ditado, Cabecalho: map[string]string{"X-Eco": p.Chave}}
	}
	an := func(p pedidoFalso) respostaFalsa {
		return respostaFalsa{Status: 401, CorpoDeErro: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key ` + p.Chave + `"}}`}
	}
	a := novoAmbiente(t, oa, an)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-rodadas", "2", "-modelos", "openai:m1,anthropic:m3", "-preco", "openai:m1=1/1,anthropic:m3=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	saida := a.tudoQueSaiu()
	for _, chave := range []string{chaveOpenAI, chaveAnthropic} {
		for i := 0; i+10 <= len(chave); i++ {
			if strings.Contains(saida, chave[i:i+10]) {
				t.Fatalf("um trecho da chave apareceu no que o programa escreveu: %q", chave[i:i+10])
			}
		}
	}
	// O eco no texto do modelo foi mascarado, não apagado: o relatório mostra
	// que havia algo ali.
	contem(t, a.lerRelatorio(), "[chave]")
	// E o teste é sensível: a chave chegou mesmo aos provedores e aos erros.
	if len(a.openai.Pedidos()) == 0 || len(a.anthropic.Pedidos()) == 0 {
		t.Fatal("o teste não exercitou os provedores")
	}
}

// ---------------------------------------------------------------------
// A rodada inteira
// ---------------------------------------------------------------------

// O modelo perfeito sobre o dataset REAL: tudo o que o Polir aceita chega igual
// ao esperado, sem falha crítica, com F1 1, e a regra escolhe um vencedor por
// provedor. Único fallback: o caso conhecido de gagueira sobre termo do
// dicionário.
func TestRun_ModeloPerfeitoNoDatasetReal(t *testing.T) {
	casos := datasetReal(t)
	a := novoAmbiente(t, comOraculo(casos), comOraculo(casos))
	cod := a.rodar(juntar(datasetRealFlags(), []string{"-gastar", "-rodadas", "2", "-modelos", "openai:gpt-6-luna,anthropic:claude-haiku-4-5"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	rel := a.lerRelatorio()

	contem(t, rel, "# Benchmark do Polish — 2026-09-30")
	contem(t, rel, "polish-ptbr@1.0.3")
	contem(t, rel, "6b7b2a07")
	contem(t, rel, hashDoDataset(casos))
	contem(t, rel, "**OpenAI:** `openai:gpt-6-luna`")
	contem(t, rel, "**Anthropic:** `anthropic:claude-haiku-4-5`")
	naoContem(t, rel, "BURACO NA GUARDA")
	naoContem(t, rel, "INCOMPLETA")
	contem(t, a.stdout.String(), "vencedor OpenAI: openai:gpt-6-luna")
	contem(t, a.stdout.String(), "relatório: "+a.relatorio)

	// 2 rodadas × as frases que vão ao provedor.
	var vao int
	for _, c := range casos {
		if len(palavrasDe(c.Entrada)) >= 4 {
			vao++
		}
	}
	if n := len(a.openai.Pedidos()); n != 2*vao {
		t.Errorf("a OpenAI recebeu %d pedidos, quero %d", n, 2*vao)
	}
	// As frases curtas nunca vão ao provedor.
	for _, p := range a.openai.Pedidos() {
		for _, c := range casos {
			if c.Categoria == CatCurta && p.Ditado == c.Entrada {
				t.Errorf("a frase curta %s foi ao provedor", c.ID)
			}
		}
	}
	// Nada de contexto além do ditado: o corpo tem só os campos do Agent.
	for _, chave := range []string{"context", "dictionary", "termos", "dicionario", "caso", "categoria"} {
		for _, p := range a.openai.Pedidos()[:1] {
			if _, tem := p.Corpo[chave]; tem {
				t.Errorf("o corpo do pedido tem o campo %q", chave)
			}
		}
	}
}
