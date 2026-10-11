package resolver

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// vocabularioAdverso mistura o que as guardas sabem tratar (pontuação
// falada, marcadores, siglas, valores, hesitações, aspas, markdown) com o
// que pode quebrá-las (espaço duplo, quebras de linha, unicode, pontuação
// solta).
var vocabularioAdverso = []string{
	"bom", "dia", "tudo", "bem", "que", "que", "né", "tipo", "pra", "então",
	"ahn", "hum", "éé", "vírgula", "ponto", "ponto final", "dois pontos", "novo parágrafo", "nova linha",
	"primeiro", "segundo", "item um", "item dois", "item 1", "1º", "sexta",
	"1.", "2)", "10.", "-", "*", "•", "1.500", "R$", "1.847,32", "20", "doze", "não", "sem", "dez",
	"DCTFWeb", "eSocial", "EFD-Reinf", "PGDAS", "OK", "C#", "Claude Code", "João", "Maria",
	"Claro,", "segue", "Aqui está", "Desculpe,",
	"“", "”", `"`, "«", "»", "#", "**", "```", "`", "~~~", "🙏", "_", "[", "]", "{", "}",
	"ação", "coração", "—", "…", "?", "!", ",", ".", ":", "  ", "\t",
}

func textoAleatorio(r *rand.Rand) string {
	n := r.Intn(40)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(vocabularioAdverso[r.Intn(len(vocabularioAdverso))])
		switch r.Intn(6) {
		case 0:
			b.WriteString("\n")
		case 1:
			b.WriteString("\n\n")
		default:
			b.WriteString(" ")
		}
	}
	return b.String()
}

// Propriedade central do "texto já organizado volta igual e é aceito": para
// QUALQUER texto, devolver exatamente a entrada nunca reprova em nenhuma
// guarda de conteúdo. Uma guarda que reprova o texto idêntico derrubaria o
// Polish em ditados perfeitamente normais.
func TestGuardas_TextoIdenticoSempreEAceito(t *testing.T) {
	r := rand.New(rand.NewSource(20260929))
	termos := []string{"DCTFWeb", "Claude Code", "C#", "eSocial", "não existe no texto"}
	for i := 0; i < 3000; i++ {
		x := textoAleatorio(r)
		if !guardaFormato(x, x) {
			t.Fatalf("guardaFormato reprovou o texto idêntico: %q", x)
		}
		if !guardaCaixa(x, x) {
			t.Fatalf("guardaCaixa reprovou o texto idêntico: %q", x)
		}
		if !guardaNumeros(x, x) {
			t.Fatalf("guardaNumeros reprovou o texto idêntico: %q", x)
		}
		if !guardaSiglasTermos(x, x, termos) {
			t.Fatalf("guardaSiglasTermos reprovou o texto idêntico: %q", x)
		}
		if m := guardaPalavras(x, x, PolishMaxEdicao); m != MotivoNenhum {
			t.Fatalf("guardaPalavras reprovou o texto idêntico (%s): %q", m, x)
		}
	}
}

// Nenhuma guarda entra em pânico com pares arbitrários, e o veredito é
// determinístico (mesma entrada, mesmo resultado).
func TestGuardas_SemPanicoEDeterministicas(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	termos := []string{"DCTFWeb", "Claude Code", "C#"}
	for i := 0; i < 3000; i++ {
		a, b := textoAleatorio(r), textoAleatorio(r)

		c1, c2 := guardaCaixa(a, b), guardaCaixa(a, b)
		f1, f2 := guardaFormato(a, b), guardaFormato(a, b)
		n1, n2 := guardaNumeros(a, b), guardaNumeros(a, b)
		s1, s2 := guardaSiglasTermos(a, b, termos), guardaSiglasTermos(a, b, termos)
		p1, p2 := guardaPalavras(a, b, PolishMaxEdicao), guardaPalavras(a, b, PolishMaxEdicao)
		if c1 != c2 || f1 != f2 || n1 != n2 || s1 != s2 || p1 != p2 {
			t.Fatalf("veredito não determinístico para %q -> %q", a, b)
		}
		_ = guardaCurto(a, PolishMinPalavras)
		_, _ = guardaJSON([]byte(b))
	}
}

// Marcadores de lista não mudam o veredito das guardas de conteúdo: o mesmo
// texto com "- " ou com "1." "2." no começo das linhas é o mesmo texto. A
// numeração só vale quando a pessoa numerou ("primeiro… segundo…").
func TestGuardas_MarcadoresDeListaNaoContam(t *testing.T) {
	entrada := "os pendentes são a DCTFWeb de agosto e o PGDAS da padaria"
	saida := "Os pendentes são:\n- A DCTFWeb de agosto\n- O PGDAS da padaria"
	if !guardaNumeros(entrada, saida) || !guardaSiglasTermos(entrada, saida, []string{"DCTFWeb"}) || !guardaFormato(entrada, saida) {
		t.Fatalf("guardas reprovaram lista legítima: %q", saida)
	}
	if m := guardaPalavras(entrada, saida, PolishMaxEdicao); m != MotivoNenhum {
		t.Fatalf("guardaPalavras reprovou lista legítima (%s): %q", m, saida)
	}

	// Ninguém numerou: a mesma lista com "1." "2." tem números que a pessoa
	// não ditou.
	numerada := "Os pendentes são:\n1. A DCTFWeb de agosto\n2. O PGDAS da padaria"
	if guardaNumeros(entrada, numerada) {
		t.Fatalf("lista numerada sem numeração ditada foi aceita: %q", numerada)
	}

	// Numerou ("primeiro… segundo…"): a lista numerada e a de traço valem.
	ditada := "os pendentes são primeiro a DCTFWeb de agosto segundo o PGDAS da padaria"
	for _, s := range []string{numerada, saida} {
		if !guardaNumeros(ditada, s) || !guardaSiglasTermos(ditada, s, []string{"DCTFWeb"}) || !guardaFormato(ditada, s) {
			t.Fatalf("guardas reprovaram lista legítima: %q", s)
		}
		if m := guardaPalavras(ditada, s, PolishMaxEdicao); m != MotivoNenhum {
			t.Fatalf("guardaPalavras reprovou lista legítima (%s): %q", m, s)
		}
	}
}

// ---------------------------------------------------------------------
// Propriedades da reprovação
// ---------------------------------------------------------------------

var vocabularioNovo = []string{"sim", "não", "obrigado", "claro", "talvez", "doze", "20", "DCTFWeb", "então", "hoje", "Segue", "pronto", "mil"}

// editarPalavras aplica n edições de PALAVRA ao texto (apaga, troca ou
// acrescenta) e devolve o resultado. Quebras de linha viram espaço.
func editarPalavras(r *rand.Rand, a string, n int) string {
	campos := strings.Fields(a)
	for k := 0; k < n && len(campos) > 0; k++ {
		i := r.Intn(len(campos))
		novo := vocabularioNovo[r.Intn(len(vocabularioNovo))]
		switch r.Intn(3) {
		case 0:
			campos = append(campos[:i], campos[i+1:]...)
		case 1:
			campos[i] = novo
		default:
			campos = append(campos[:i+1], append([]string{novo}, campos[i+1:]...)...)
		}
	}
	return strings.Join(campos, " ")
}

func ehSubsequencia(curta, longa []string) bool {
	j := 0
	for _, w := range longa {
		if j < len(curta) && curta[j] == w {
			j++
		}
	}
	return j == len(curta)
}

func contaPalavras(palavras []string) map[string]int {
	m := map[string]int{}
	for _, w := range palavras {
		m[w]++
	}
	return m
}

// O oráculo independente do alinhamento: saída aceita pela guarda de palavras
// é SEMPRE subsequência da entrada — nenhuma palavra acrescentada, nenhuma
// trocada — e nunca perde palavra protegida que não fosse estrutural.
func TestGuardaPalavras_AceitaSoSubsequenciaESemPerderProtegida(t *testing.T) {
	r := rand.New(rand.NewSource(20260930))
	aceitas := 0
	for i := 0; i < 6000; i++ {
		a := textoAleatorio(r)
		b := editarPalavras(r, a, r.Intn(4))
		// Sem orçamento (o padrão) e com 10%: as duas têm de respeitar o oráculo.
		maxEdicao := []float64{PolishMaxEdicao, 0.10}[i%2]
		if guardaPalavras(a, b, maxEdicao) != MotivoNenhum {
			continue
		}
		aceitas++
		pa, pb := palavrasNormalizadas(a), palavrasNormalizadas(b)
		if !ehSubsequencia(pb, pa) {
			t.Fatalf("aceitou saída que acrescenta ou troca palavra\nentrada: %q\nsaída:   %q", a, b)
		}
		marcas := marcarEstruturais(pa)
		estruturais := map[string]int{}
		for k, w := range pa {
			if marcas[k].Classe != classeNenhuma {
				estruturais[w]++
			}
		}
		ca, cb := contaPalavras(pa), contaPalavras(pb)
		for w, n := range ca {
			if ehProtegida(w) && cb[w] < n-estruturais[w] {
				t.Fatalf("aceitou saída que apaga a palavra protegida %q\nentrada: %q\nsaída:   %q", w, a, b)
			}
		}
	}
	if aceitas < 300 {
		t.Fatalf("só %d pares aceitos em 6000: o teste está quase vazio", aceitas)
	}
}

// Uma edição de palavra que acrescenta ou troca NUNCA passa, em texto
// nenhum: não há orçamento para isso. Vale para texto longo e para curto.
func TestGuardaPalavras_UmaPalavraAcrescentadaOuTrocadaNuncaPassa(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	casos := 0
	for i := 0; i < 3000; i++ {
		a := strings.Join(strings.Fields(textoAleatorio(r)), " ")
		campos := strings.Fields(a)
		if len(campos) < 3 {
			continue
		}
		k := r.Intn(len(campos))
		novo := "zzz" // palavra que nenhum vocabulário tem
		var b string
		if r.Intn(2) == 0 { // troca
			cp := slices.Clone(campos)
			cp[k] = novo
			b = strings.Join(cp, " ")
		} else { // acrescenta
			cp := slices.Insert(slices.Clone(campos), k, novo)
			b = strings.Join(cp, " ")
		}
		casos++
		if m := guardaPalavras(a, b, 1.0); m == MotivoNenhum {
			t.Fatalf("aceitou palavra nova mesmo com 100%% de orçamento\nentrada: %q\nsaída:   %q", a, b)
		}
	}
	if casos < 1000 {
		t.Fatalf("só %d casos", casos)
	}
}

// Números: trocar qualquer dígito de qualquer número nunca passa, e o mesmo
// texto passa.
func TestGuardaNumeros_UmDigitoTrocadoNuncaPassa(t *testing.T) {
	numericos := []string{"20", "R$", "1.847,32", "6%", "14:30", "05/10", "-500", "1º", "US$", "1500", "€", "3D"}
	fillers := []string{"vence", "dia", "pagou", "hoje", "o", "valor", "de", "guia"}
	r := rand.New(rand.NewSource(3))
	casos := 0
	for i := 0; i < 3000; i++ {
		var campos []string
		for k := 0; k < 3+r.Intn(12); k++ {
			if r.Intn(2) == 0 {
				campos = append(campos, numericos[r.Intn(len(numericos))])
			} else {
				campos = append(campos, fillers[r.Intn(len(fillers))])
			}
		}
		a := strings.Join(campos, " ")
		var posicoes []int // posições (runas) de dígitos
		runas := []rune(a)
		for p, c := range runas {
			if unicode.IsDigit(c) {
				posicoes = append(posicoes, p)
			}
		}
		if len(posicoes) == 0 {
			continue
		}
		p := posicoes[r.Intn(len(posicoes))]
		trocada := slices.Clone(runas)
		trocada[p] = '0' + (runas[p]-'0'+1+rune(r.Intn(9)))%10
		casos++
		if guardaNumeros(a, string(trocada)) {
			t.Fatalf("aceitou um dígito trocado\nentrada: %q\nsaída:   %q", a, string(trocada))
		}
		if !guardaNumeros(a, a) {
			t.Fatalf("reprovou o mesmo texto: %q", a)
		}
	}
	if casos < 1000 {
		t.Fatalf("só %d casos", casos)
	}
}

// Siglas: perder a caixa de UMA ocorrência de sigla nunca passa, mesmo com
// outra ocorrência intacta do lado.
func TestGuardaSiglasTermos_UmaOcorrenciaComACaixaPerdidaNuncaPassa(t *testing.T) {
	siglas := []string{"DCTFWeb", "PGDAS", "ECF", "eSocial", "EFD-Reinf", "DAS"}
	fillers := []string{"a", "o", "de", "e", "transmiti", "enviei", "ontem", "hoje"}
	r := rand.New(rand.NewSource(5))
	casos := 0
	for i := 0; i < 3000; i++ {
		var campos []string
		var idxSiglas []int
		for k := 0; k < 4+r.Intn(12); k++ {
			if r.Intn(3) == 0 {
				idxSiglas = append(idxSiglas, len(campos))
				campos = append(campos, siglas[r.Intn(len(siglas))])
			} else {
				campos = append(campos, fillers[r.Intn(len(fillers))])
			}
			campos = append(campos, fillers[r.Intn(len(fillers))]) // nunca duas siglas coladas
		}
		if len(idxSiglas) == 0 {
			continue
		}
		a := strings.Join(campos, " ")
		if !guardaSiglasTermos(a, a, nil) {
			t.Fatalf("reprovou o mesmo texto: %q", a)
		}
		cp := slices.Clone(campos)
		k := idxSiglas[r.Intn(len(idxSiglas))]
		cp[k] = strings.ToLower(cp[k])
		casos++
		if guardaSiglasTermos(a, strings.Join(cp, " "), nil) {
			t.Fatalf("aceitou sigla sem a caixa\nentrada: %q\nsaída:   %q", a, strings.Join(cp, " "))
		}
	}
	if casos < 1000 {
		t.Fatalf("só %d casos", casos)
	}
}

// Caixa alta: pôr UMA palavra de duas letras ou mais toda em maiúscula, num texto
// que não tinha nenhuma, nunca passa; o mesmo texto passa; e o caminho de volta
// (a entrada em maiúsculas, a saída em minúsculas) passa.
func TestGuardaCaixa_UmaPalavraEmMaiusculasNuncaPassa(t *testing.T) {
	palavras := []string{
		"o", "a", "e", "de", "cliente", "pagou", "guia", "hoje", "ok", "cnpj", "das", "não", "ação",
		"eu", "vou", "pagar", "amanhã", "extrato", "banco", "João", "Maria", "eSocial", "DCTFWeb",
	}
	r := rand.New(rand.NewSource(17))
	casos := 0
	for i := 0; i < 3000; i++ {
		var campos []string
		for k := 0; k < 3+r.Intn(12); k++ {
			campos = append(campos, palavras[r.Intn(len(palavras))])
		}
		a := strings.Join(campos, " ")
		if !guardaCaixa(a, a) {
			t.Fatalf("reprovou o mesmo texto: %q", a)
		}
		var candidatas []int // as palavras de duas letras ou mais que ainda não estão em maiúsculas
		for k, w := range campos {
			if ehCaixaAlta(strings.ToUpper(w)) && !ehCaixaAlta(w) {
				candidatas = append(candidatas, k)
			}
		}
		if len(candidatas) == 0 {
			continue
		}
		cp := slices.Clone(campos)
		k := candidatas[r.Intn(len(candidatas))]
		cp[k] = strings.ToUpper(cp[k])
		b := strings.Join(cp, " ")
		casos++
		if guardaCaixa(a, b) {
			t.Fatalf("aceitou uma palavra nova em maiúsculas\nentrada: %q\nsaída:   %q", a, b)
		}
		// A caixa que a entrada já trazia não é caixa a mais.
		if !guardaCaixa(b, b) || !guardaCaixa(b, a) {
			t.Fatalf("reprovou o que a entrada já trazia em maiúsculas: %q -> %q", b, a)
		}
	}
	if casos < 1000 {
		t.Fatalf("só %d casos", casos)
	}
}

// Gagueira do termo do dicionário: k repetições IMEDIATAS (só espaço ou pontuação
// entre elas) contam uma; k separadas por outra palavra contam k. No primeiro caso
// basta uma ocorrência na saída; no segundo, tem de ter as k.
func TestGuardaSiglasTermos_GagueiraDoTermoContaUmaESeparadasContamTodas(t *testing.T) {
	termos := []string{"DCTFWeb"}
	fillers := []string{"o", "a", "de", "venceu", "hoje", "cedo", "guia", "cliente"}
	r := rand.New(rand.NewSource(23))
	casos := 0
	for i := 0; i < 2000; i++ {
		antes := fillers[r.Intn(len(fillers))] + " " + fillers[r.Intn(len(fillers))]
		depois := fillers[r.Intn(len(fillers))] + " " + fillers[r.Intn(len(fillers))]
		k := 2 + r.Intn(3)
		sep := []string{" ", ", ", " - "}[r.Intn(3)]

		var repetidas []string
		for j := 0; j < k; j++ {
			repetidas = append(repetidas, "dctfweb")
		}

		// k repetições coladas valem uma
		entrada := antes + " " + strings.Join(repetidas, sep) + " " + depois
		uma := antes + " DCTFWeb " + depois
		nenhuma := antes + " " + depois
		if !guardaSiglasTermos(entrada, uma, termos) {
			t.Fatalf("a gagueira do termo devia contar uma\nentrada: %q\nsaída:   %q", entrada, uma)
		}
		if guardaSiglasTermos(entrada, nenhuma, termos) {
			t.Fatalf("aceitou a saída sem o termo\nentrada: %q\nsaída:   %q", entrada, nenhuma)
		}

		// k ocorrências separadas por uma palavra contam k
		separadas := antes + " " + strings.Join(repetidas, " e ") + " " + depois
		menos := antes + " " + strings.Join(repetidas[:k-1], " e ") + " e " + depois
		if !guardaSiglasTermos(separadas, separadas, termos) {
			t.Fatalf("reprovou as ocorrências separadas mantidas: %q", separadas)
		}
		if guardaSiglasTermos(separadas, menos, termos) {
			t.Fatalf("aceitou perder uma de %d ocorrências separadas\nentrada: %q\nsaída:   %q", k, separadas, menos)
		}
		casos++
	}
	if casos < 1000 {
		t.Fatalf("só %d casos", casos)
	}
}
