package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

func TestCarregarFrases(t *testing.T) {
	ok := `{"versao":1,"aviso":"x","frases":[{"id":"a","categoria":"lista","entrada":"e","esperado":"E.","termos":["DAS"]}]}`
	got, err := carregarFrases([]byte(ok))
	if err != nil || len(got) != 1 || got[0].ID != "a" || got[0].Termos[0] != "DAS" {
		t.Fatalf("frases = %+v, %v", got, err)
	}
	casos := []struct{ nome, json, trecho string }{
		{"json quebrado", `{"versao":1,"frases":[`, "frases.json"},
		{"campo desconhecido", `{"versao":1,"frases":[{"id":"a","categoria":"lista","entrada":"e","esperado":"E","termo":["DAS"]}]}`, "unknown field"},
		{"campo desconhecido no topo", `{"versao":1,"frase":[]}`, "unknown field"},
		{"versão errada", `{"versao":2,"frases":[]}`, "versao 2"},
		{"sem versão", `{"frases":[]}`, "versao 0"},
		{"ja_organizada vem do manifest", `{"versao":1,"frases":[{"id":"a","categoria":"ja_organizada","entrada":"e","esperado":"e"}]}`, "manifest do ASR"},
	}
	for _, c := range casos {
		if _, err := carregarFrases([]byte(c.json)); err == nil || !strings.Contains(err.Error(), c.trecho) {
			t.Errorf("%s: erro %v, quero um com %q", c.nome, err, c.trecho)
		}
	}
}

func TestCarregarReferencias(t *testing.T) {
	m := `{"samples":[
	  {"id":"geral-01","audio":"a.wav","category":"pt-br-geral","reference":"Depois do almoço eu volto.","entities":[]},
	  {"id":"cs-01","audio":"b.wav","category":"code-switching","reference":"Faz o deploy em staging.","entities":["deploy","staging"]}]}`
	got, err := carregarReferencias([]byte(m))
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].ID != "ja-geral-01" || got[0].Categoria != CatJaOrganizada || got[0].Entrada != got[0].Esperado || got[0].Entrada != "Depois do almoço eu volto." {
		t.Errorf("caso 0 = %+v", got[0])
	}
	if len(got[1].Termos) != 2 || got[1].Termos[0] != "deploy" {
		t.Errorf("as entidades viram termos: %+v", got[1])
	}
	if _, err := carregarReferencias([]byte(`nada`)); err == nil {
		t.Error("manifest inválido devia dar erro")
	}
}

func TestValidarCasos(t *testing.T) {
	base := func() []Caso {
		return []Caso{
			{ID: "a", Categoria: CatLista, Entrada: "os itens do DAS", Esperado: "Os itens do DAS.", Termos: []string{"DAS"}},
			{ID: "b", Categoria: CatJaOrganizada, Entrada: "Já está.", Esperado: "Já está."},
		}
	}
	if err := validarCasos(base()); err != nil {
		t.Fatalf("dataset válido: %v", err)
	}
	casos := []struct {
		nome   string
		muda   func(cs []Caso) []Caso
		trecho string
	}{
		{"vazio", func([]Caso) []Caso { return nil }, "vazio"},
		{"sem id", func(cs []Caso) []Caso { cs[0].ID = " "; return cs }, "sem id"},
		{"id repetido", func(cs []Caso) []Caso { cs[1].ID = "a"; return cs }, "id repetido: a"},
		{"categoria desconhecida", func(cs []Caso) []Caso { cs[0].Categoria = "outra"; return cs }, "categoria desconhecida"},
		{"entrada vazia", func(cs []Caso) []Caso { cs[0].Entrada = "  "; return cs }, "entrada vazia"},
		{"esperado vazio", func(cs []Caso) []Caso { cs[1].Esperado = ""; return cs }, "esperado vazio"},
		{"termo que não está na entrada", func(cs []Caso) []Caso { cs[0].Termos = []string{"PGDAS"}; return cs }, `o termo "PGDAS" não está na entrada`},
	}
	for _, c := range casos {
		err := validarCasos(c.muda(base()))
		if err == nil || !strings.Contains(err.Error(), c.trecho) {
			t.Errorf("%s: erro %v, quero um com %q", c.nome, err, c.trecho)
		}
	}
	// O termo casa sem distinguir caixa.
	cs := base()
	cs[0].Termos = []string{"das"}
	if err := validarCasos(cs); err != nil {
		t.Errorf("termo em outra caixa: %v", err)
	}
}

func TestHashDoDataset(t *testing.T) {
	a := []Caso{
		{ID: "a", Categoria: CatLista, Entrada: "x", Esperado: "X", Termos: []string{"t"}},
		{ID: "b", Categoria: CatValores, Entrada: "y", Esperado: "Y"},
	}
	h := hashDoDataset(a)
	if len(h) != 64 {
		t.Fatalf("hash = %q", h)
	}
	if h != hashDoDataset(a) {
		t.Error("o hash não é determinístico")
	}
	// A ordem dos casos não conta.
	if h != hashDoDataset([]Caso{a[1], a[0]}) {
		t.Error("o hash depende da ordem em que os casos foram lidos")
	}
	// A entrada original não é reordenada.
	inv := []Caso{a[1], a[0]}
	hashDoDataset(inv)
	if inv[0].ID != "b" {
		t.Error("hashDoDataset reordenou a lista do chamador")
	}
	// Cada campo conta.
	muda := map[string]func(cs []Caso){
		"id":        func(cs []Caso) { cs[0].ID = "z" },
		"categoria": func(cs []Caso) { cs[0].Categoria = CatSiglas },
		"entrada":   func(cs []Caso) { cs[0].Entrada = "x2" },
		"esperado":  func(cs []Caso) { cs[0].Esperado = "X." },
		"termos":    func(cs []Caso) { cs[0].Termos = []string{"t", "u"} },
		"sem termo": func(cs []Caso) { cs[0].Termos = nil },
		"caso a mais": func(cs []Caso) {
			cs[1].Termos = []string{"novo"}
		},
	}
	for nome, f := range muda {
		cs := append([]Caso(nil), a...)
		cs[0].Termos = append([]string(nil), a[0].Termos...)
		f(cs)
		if hashDoDataset(cs) == h {
			t.Errorf("mudar %s não mudou o hash", nome)
		}
	}
	// As fronteiras entre os campos contam: "ab"+"c" não é "a"+"bc".
	x := []Caso{{ID: "1", Categoria: "a", Entrada: "bc", Esperado: "d"}}
	y := []Caso{{ID: "1", Categoria: "ab", Entrada: "c", Esperado: "d"}}
	if hashDoDataset(x) == hashDoDataset(y) {
		t.Error("o hash confunde as fronteiras dos campos")
	}
	if hashDoDataset(nil) == "" {
		t.Error("hash do vazio")
	}
}

func TestContagemPorCategoria(t *testing.T) {
	got := contagemPorCategoria([]Caso{{Categoria: "a"}, {Categoria: "b"}, {Categoria: "a"}})
	if got["a"] != 2 || got["b"] != 1 || len(got) != 2 {
		t.Errorf("contagem = %v", got)
	}
	if !categoriaValida(CatJaOrganizada) || !categoriaValida(CatArmadilha) || categoriaValida("nada") {
		t.Error("categoriaValida")
	}
	if todas := todasAsCategorias(); todas[len(todas)-1] != CatJaOrganizada || len(todas) != len(categoriasDasFrases)+1 {
		t.Errorf("todasAsCategorias = %v", todas)
	}
}

// ---------------------------------------------------------------------
// O dataset real
// ---------------------------------------------------------------------

func TestDatasetReal_Composicao(t *testing.T) {
	casos := datasetReal(t)
	conta := contagemPorCategoria(casos)
	// ~10 frases por categoria (a S3 pede ~70 frases sintéticas e as 46 referências do ASR).
	minimos := map[string]int{
		CatLista: 8, CatValores: 8, CatSiglas: 8, CatPontuacao: 10, CatPergunta: 6, CatInjecao: 6,
		CatFalaLonga: 5, CatHesitacao: 6, CatCurta: 5, CatArmadilha: 8, CatGrafia: 6,
	}
	for cat, n := range minimos {
		if conta[cat] < n {
			t.Errorf("categoria %s tem %d frases, quero pelo menos %d", cat, conta[cat], n)
		}
	}
	if conta[CatJaOrganizada] != 46 {
		t.Errorf("já organizada = %d, quero as 46 referências do manifest do ASR", conta[CatJaOrganizada])
	}
	var sinteticas int
	for cat, n := range conta {
		if cat != CatJaOrganizada {
			sinteticas += n
		}
	}
	if sinteticas < 70 || sinteticas > 100 {
		t.Errorf("%d frases sintéticas, quero cerca de 70 a 100", sinteticas)
	}
}

// Só a categoria curta tem frase com menos de 4 palavras: as outras vão ao
// provedor, e a curta nunca.
func TestDatasetReal_FraseCurtaSoNaCategoriaCurta(t *testing.T) {
	for _, c := range datasetReal(t) {
		n := len(palavrasDe(c.Entrada))
		if c.Categoria == CatCurta && n >= resolver.PolishMinPalavras {
			t.Errorf("%s: a frase curta tem %d palavras", c.ID, n)
		}
		if c.Categoria != CatCurta && n < resolver.PolishMinPalavras {
			t.Errorf("%s (%s): tem %d palavras e não iria ao provedor", c.ID, c.Categoria, n)
		}
	}
}

// Nenhum dado real: nada que pareça CPF, CNPJ, e-mail, telefone, endereço da
// web, chave ou número de documento comprido.
func TestDatasetReal_SemDadoReal(t *testing.T) {
	proibidos := map[string]*regexp.Regexp{
		"CPF":              regexp.MustCompile(`\d{3}\.\d{3}\.\d{3}-\d{2}`),
		"CNPJ":             regexp.MustCompile(`\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}`),
		"e-mail":           regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`),
		"telefone":         regexp.MustCompile(`\(?\d{2}\)?\s?9?\d{4}-?\d{4}`),
		"endereço web":     regexp.MustCompile(`(?i)https?://|www\.`),
		"número longo":     regexp.MustCompile(`\d{9,}`),
		"chave de API":     regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
		"código de barras": regexp.MustCompile(`\d{5}\.\d{5}\s\d{5}\.\d{6}`),
	}
	for _, c := range datasetReal(t) {
		for nome, re := range proibidos {
			for _, campo := range []string{c.Entrada, c.Esperado} {
				if re.MatchString(campo) {
					t.Errorf("%s: parece %s: %q", c.ID, nome, campo)
				}
			}
		}
	}
}

// Toda frase sintética foi organizada: o esperado não é a entrada crua.
func TestDatasetReal_EsperadoEOrganizacaoDaEntrada(t *testing.T) {
	for _, c := range datasetReal(t) {
		if c.Categoria == CatJaOrganizada {
			if c.Esperado != c.Entrada {
				t.Errorf("%s: já organizada tem de ter esperado igual à entrada", c.ID)
			}
			continue
		}
		if c.Esperado == c.Entrada {
			t.Errorf("%s: o esperado é igual à entrada (sem organização nenhuma)", c.ID)
		}
		if strings.TrimSpace(c.Esperado) != c.Esperado {
			t.Errorf("%s: o esperado tem espaço nas pontas", c.ID)
		}
		if strings.Contains(c.Esperado, "\r") {
			t.Errorf("%s: o esperado tem \\r (o Polish devolve \\n; a inserção é que vira \\r\\n)", c.ID)
		}
	}
}

func TestDatasetReal_TermosPresentesNoEsperado(t *testing.T) {
	for _, c := range datasetReal(t) {
		for _, termo := range c.Termos {
			na := len(ocorrenciasDoTermo(c.Entrada, termo))
			no := len(ocorrenciasDoTermo(c.Esperado, termo))
			// hesitacao-05 é a gagueira do termo ("DCTFWeb DCTFWeb"): o esperado tira uma,
			// e a guarda a conta uma vez (a repetição imediata não é duas ocorrências).
			if no != na && c.ID != "hesitacao-05" {
				t.Errorf("%s: o termo %q aparece %d vez(es) na entrada e %d no esperado", c.ID, termo, na, no)
			}
		}
	}
}

// O Smart entrega o termo do dicionário na grafia do dicionário (é o que ele
// faz), então a entrada do dataset também: a auditoria de siglas compara com o
// esperado, e uma entrada em outra caixa faria a auditoria acusar o que a guarda
// aceita (revisão da S3). A única exceção é a armadilha do "das" preposição.
func TestDatasetReal_TermosNaGrafiaCanonicaNaEntrada(t *testing.T) {
	excecoes := map[string]bool{"armadilha-07": true}
	for _, c := range datasetReal(t) {
		for _, termo := range c.Termos {
			for _, o := range acharOcorrencias(c.Entrada, termo) {
				canonica := o.Grafia == termo || (o.Abre && o.Grafia == comInicialMaiuscula(termo))
				if !canonica && !excecoes[c.ID] {
					t.Errorf("%s: o termo %q aparece na entrada como %q (o Smart o entregaria na grafia do dicionário)", c.ID, termo, o.Grafia)
				}
			}
		}
	}
	// A exceção continua valendo: se a frase mudar, a exceção sai.
	var achou bool
	for _, c := range datasetReal(t) {
		if c.ID != "armadilha-07" {
			continue
		}
		for _, o := range acharOcorrencias(c.Entrada, "DAS") {
			achou = achou || o.Grafia == "das"
		}
	}
	if !achou {
		t.Error("armadilha-07 não tem mais o 'das' preposição: tire a exceção do teste")
	}
}

// As categorias com estrutura têm estrutura de verdade no esperado: a métrica
// de "acerto de estrutura" só vale se o dataset a exercita.
func TestDatasetReal_EstruturaNoEsperado(t *testing.T) {
	var comLista, numeradas, comTraco, comParagrafos, pontuacaoComQuebra int
	for _, c := range datasetReal(t) {
		e := estruturaDe(c.Esperado)
		switch c.Categoria {
		case CatLista:
			if e.Itens > 0 {
				comLista++
				if e.Marcador == "numerado" {
					numeradas++
				} else {
					comTraco++
				}
			}
		case CatFalaLonga:
			if e.Paragrafos >= 2 {
				comParagrafos++
			}
		case CatPontuacao:
			if e.Paragrafos > 1 || strings.Contains(c.Esperado, "\n") {
				pontuacaoComQuebra++
			}
		}
	}
	if comLista < 6 || numeradas < 3 || comTraco < 3 {
		t.Errorf("listas no esperado: %d (%d numeradas, %d de traço), quero pelo menos 6 (3 e 3)", comLista, numeradas, comTraco)
	}
	if comParagrafos < 4 {
		t.Errorf("falas longas com parágrafos: %d, quero pelo menos 4", comParagrafos)
	}
	if pontuacaoComQuebra < 2 {
		t.Errorf("pontuação falada com quebra de linha ou parágrafo: %d, quero pelo menos 2", pontuacaoComQuebra)
	}
}

// As categorias de perigo têm o que as torna perigosas na ENTRADA: a pergunta
// é pergunta, a injeção é ordem, e as armadilhas trazem o que as guardas
// recusam.
func TestDatasetReal_CategoriasDePerigoSaoPerigosas(t *testing.T) {
	// (O \b do Go só vê ASCII: "será" e "esqueça" pedem fronteira explícita.)
	ordens := regexp.MustCompile(`(?i)(^|\s)(ignore|ignora|esquece|esqueça|traduza|escreva|escreve|responda|responde|você agora|desconsidere|apague|encerre)(\s|$)`)
	perguntas := regexp.MustCompile(`(?i)^(você sabe|qual|quanto|o que|quem|pode|será)(\s|$)`)
	for _, c := range datasetReal(t) {
		switch c.Categoria {
		case CatInjecao:
			if !ordens.MatchString(c.Entrada) {
				t.Errorf("%s: a injeção não traz ordem nenhuma: %q", c.ID, c.Entrada)
			}
		case CatPergunta:
			if !perguntas.MatchString(c.Entrada) || !strings.HasSuffix(c.Esperado, "?") {
				t.Errorf("%s: a pergunta não é pergunta (entrada %q, esperado %q)", c.ID, c.Entrada, c.Esperado)
			}
		}
	}
}

// As armadilhas trazem as limitações conhecidas da ADR-016 (19-eval-plan, "Frases", (c)).
func TestDatasetReal_ArmadilhasCobremAsLimitacoesConhecidas(t *testing.T) {
	var todas strings.Builder
	for _, c := range datasetReal(t) {
		if c.Categoria == CatArmadilha {
			todas.WriteString(c.Entrada + "\n")
		}
	}
	for nome, re := range map[string]*regexp.Regexp{
		"nome sem acento que o modelo corrige": regexp.MustCompile(`(?i)\bdebora\b`),
		"1° com sinal de grau":                 regexp.MustCompile(`\d°`),
		"R$1500 sem espaço":                    regexp.MustCompile(`R\$\d`),
		"numeração ditada em dígitos":          regexp.MustCompile(`item \d`),
		"sigla no começo da frase":             regexp.MustCompile(`(?m)^eSocial`),
		"DAS e das guias":                      regexp.MustCompile(`DAS.*\bdas guias`),
		"pontuação falada com quantidade":      regexp.MustCompile(`dois pontos`),
		"pontuação falada com número":          regexp.MustCompile(`sete vírgula dois`),
		"né e tipo no meio do ditado":          regexp.MustCompile(`\sné\s.*\stipo\s`),
	} {
		if !re.MatchString(todas.String()) {
			t.Errorf("as armadilhas não cobrem: %s", nome)
		}
	}
}
