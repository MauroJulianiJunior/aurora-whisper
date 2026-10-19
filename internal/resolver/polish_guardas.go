package resolver

import (
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MotivoFallback diz POR QUE o Polish não valeu num ditado e o texto do
// Smart foi mantido. É só o motivo, em forma de enum: nunca carrega texto
// do modelo, do ditado ou do provedor, então pode ir para o log e para a
// tela sem risco (docs/ai-spec/09-security-privacy.md).
//
// O valor vazio (MotivoNenhum) significa "sem fallback": a reescrita foi
// aceita, ou o Polish nem foi pedido.
type MotivoFallback string

const (
	MotivoNenhum MotivoFallback = ""

	// MotivoCurto: menos palavras que o mínimo; a chamada nem aconteceu.
	MotivoCurto MotivoFallback = "curto"
	// MotivoSemCliente: não há PolishClient configurado; nada foi chamado.
	MotivoSemCliente MotivoFallback = "sem_cliente"
	// MotivoSemChave: o cliente sinalizou (ErrPolishSemChave) que a pessoa
	// não tem chave salva para o provedor escolhido.
	MotivoSemChave MotivoFallback = "sem_chave"
	// MotivoTransporte: o cliente devolveu um erro que não sabe classificar
	// (rede fora do ar, DNS, TLS, conexão cortada) ou ficou sem contexto por
	// cancelamento. O detalhe fica com quem chamou o provedor, que o traduz
	// sem corpo e sem chave; o resolver só sabe que falhou.
	MotivoTransporte MotivoFallback = "transporte"
	// MotivoPrazo: o prazo da chamada estourou.
	MotivoPrazo MotivoFallback = "prazo"

	// Os motivos abaixo só o cliente do provedor sabe dar: ele os devolve num
	// *ErroPolish, traduzidos do que o provedor respondeu, sem o corpo e sem
	// a chave. A tela mostra o motivo ("sem crédito na OpenAI"), nunca o texto.

	// MotivoChaveRecusada: o provedor recusou a chave (401 ou 403), ou ela
	// tem caractere que nenhuma chave tem e nem foi enviada.
	MotivoChaveRecusada MotivoFallback = "chave_recusada"
	// MotivoSemCredito: acabou o crédito ou o teto de gasto da conta. Repetir
	// não adianta; a pessoa precisa pôr crédito ou mexer no limite.
	MotivoSemCredito MotivoFallback = "sem_credito"
	// MotivoLimite: limite de requisições por minuto. Passa sozinho.
	MotivoLimite MotivoFallback = "limite"
	// MotivoModeloInexistente: o modelo escolhido não existe mais ou a conta
	// não alcança; ou nenhum modelo foi escolhido.
	MotivoModeloInexistente MotivoFallback = "modelo_inexistente"
	// MotivoModeloIncompativel: o modelo existe, mas recusa um parâmetro que o
	// pedido manda: o esforço de raciocínio (o padrão "none" só vale nos
	// modelos mais novos da OpenAI; os mais antigos respondem 400 com
	// error.param "reasoning..."). Escolher outro modelo resolve; não é a
	// chave nem a conta.
	MotivoModeloIncompativel MotivoFallback = "modelo_incompativel"
	// MotivoSobrecarga: o provedor está fora do ar ou sobrecarregado (5xx).
	MotivoSobrecarga MotivoFallback = "sobrecarga"
	// MotivoPedidoRecusado: o provedor rejeitou o PEDIDO (400, 413, 422 que
	// não são crédito, modelo nem incompatibilidade de parâmetro conhecida):
	// bug de protocolo nosso, não da conta de quem usa.
	MotivoPedidoRecusado MotivoFallback = "pedido_recusado"
	// MotivoRecusa: o modelo recusou responder (refusal, filtro de conteúdo).
	MotivoRecusa MotivoFallback = "recusa"
	// MotivoTruncada: a resposta foi cortada pelo limite de tokens de saída.
	MotivoTruncada MotivoFallback = "truncada"
	// MotivoRespostaInvalida: o provedor respondeu, mas não com o que o
	// protocolo promete (sem texto, envelope estranho, redirecionamento,
	// resposta grande demais, parada por outro motivo).
	MotivoRespostaInvalida MotivoFallback = "resposta_invalida"
	// MotivoJSON: resposta que não é o JSON esperado, final_text vazio ou
	// com caractere proibido (controle, invisível, U+FFFD).
	MotivoJSON MotivoFallback = "json"
	// MotivoFormato: preâmbulo de assistente, cerca de código, markdown,
	// símbolo ou JSON que a entrada não tinha, ou texto inteiro embrulhado
	// em aspas ou parênteses.
	MotivoFormato MotivoFallback = "formato"
	// MotivoCaixa: uma palavra de duas letras ou mais saiu TODA em maiúscula e a
	// entrada não a tinha assim ("ok" -> "OK", "o cliente pagou" -> "O CLIENTE
	// PAGOU"). É o modelo obedecendo a "responda tudo em maiúsculas" ditado, ou
	// gritando por conta própria. Tem motivo próprio, e não "formato", para o
	// benchmark medir sozinho o preço desta guarda: ela também derruba o Polish
	// que só "acertou" uma sigla ("cnpj" -> "CNPJ"), e afrouxá-la é decisão nova,
	// com dados (ADR-016).
	MotivoCaixa MotivoFallback = "caixa"
	// MotivoNumeros: os números da saída não são os da entrada (valor,
	// formato, ordem, sinal, percentual ou moeda).
	MotivoNumeros MotivoFallback = "numeros"
	// MotivoSiglasTermos: uma sigla, um termo do dicionário ou um nome
	// próprio mudou de grafia, de quantidade ou sumiu.
	MotivoSiglasTermos MotivoFallback = "siglas_termos"
	// MotivoPalavras: a saída não é a entrada só com pontuação, caixa e
	// marcadores: acrescentou, trocou ou apagou palavra sem licença.
	MotivoPalavras MotivoFallback = "palavras"
	// MotivoTrechoNovo: a saída trouxe um trecho de 3 ou mais palavras que
	// não estavam na entrada.
	MotivoTrechoNovo MotivoFallback = "trecho_novo"
	// MotivoDicionario: a consulta LOCAL ao dicionário falhou, então não há
	// como conferir os termos; na dúvida, fica o Smart.
	MotivoDicionario MotivoFallback = "dicionario"
)

// Parâmetros das guardas. Os dois primeiros são PADRÕES: PolirOpcoes e
// PolishResolver os sobrescrevem, porque a calibração vem do benchmark
// (plano da etapa 2, seção 5), não de palpite.
const (
	// PolishMinPalavras: ditado com menos palavras que isto não vai ao
	// provedor. "ok, pode mandar" sai sem espera e sem gastar chamada.
	PolishMinPalavras = 4
	// PolishMaxEdicao é a fração máxima das palavras não estruturais da
	// entrada que a saída pode APAGAR além das que têm licença (hesitação,
	// gagueira, pontuação falada com o sinal, ordinal com o marcador, o "e" da
	// lista). O padrão é ZERO: o Polish legítimo não apaga mais nada, e o
	// prompt manda manter "né", "tipo", "então" e "pra"; uma palavra a menos é
	// motivo para cair no Smart. Subir esse número é decisão de calibração, com
	// o benchmark na mão (plano da etapa 2, seção 5), não de palpite: cada 10%
	// libera o modelo para apagar, sem ninguém ver, a palavra que muda o
	// sentido ("antes", "já", "ainda"). Trocar ou acrescentar palavra não tem
	// orçamento nenhum, com qualquer valor: é sempre reprovado.
	PolishMaxEdicao = 0.0

	// polishTrechoNovo: um trecho de tantas palavras NOVAS seguidas é o que
	// separa MotivoTrechoNovo de MotivoPalavras no relato do motivo.
	polishTrechoNovo = 3
	// polishTaxaDeReescrita: acima desta fração de palavras editadas o relato
	// diz "palavras" (a saída foi reescrita), mesmo que haja um trecho novo. É
	// só do relato do motivo; não decide se a saída passa.
	polishTaxaDeReescrita = 0.10

	// polishMaxCelulas limita a tabela do alinhamento (palavras da entrada
	// vezes palavras da saída). 4 milhões de células são ~16 MB; um ditado
	// real tem centenas de palavras, então passar disso é anomalia — e
	// anomalia cai para o Smart, não estoura memória.
	polishMaxCelulas = 4_000_000

	// polishInviavel é o custo de um alinhamento que não existe.
	polishInviavel int32 = 1 << 20
)

// ---------------------------------------------------------------------
// Guarda 1 — curto
// ---------------------------------------------------------------------

// guardaCurto passa (true) quando o texto tem ao menos min palavras. Com
// menos, o Polish não é chamado: não há o que organizar e cada chamada custa
// tempo e dinheiro.
func guardaCurto(texto string, min int) bool {
	return len(palavrasNormalizadas(texto)) >= min
}

// ---------------------------------------------------------------------
// Guarda 3 — JSON
// ---------------------------------------------------------------------

// ehCaractereProibido reporta se a runa não pode estar no final_text: NUL,
// U+FFFD, controle (só "\n" escapa: separa parágrafos e itens) e os
// caracteres INVISÍVEIS — formato (Cf: espaço de largura zero, marcas de
// direção, BOM), uso privado (Co) e separadores de linha e de parágrafo (Zl,
// Zp). O texto de quem dita nunca os tem, e invisível é o que passa
// despercebido no campo. (Substituto solto, Cs, nem chega aqui: o decodificador
// de JSON o troca por U+FFFD, que já é proibido.)
func ehCaractereProibido(r rune) bool {
	if r == '\n' {
		return false
	}
	return r == utf8.RuneError || unicode.IsControl(r) ||
		unicode.In(r, unicode.Cf, unicode.Co, unicode.Zl, unicode.Zp)
}

// guardaJSON decodifica a resposta do provedor e devolve o final_text já
// sem espaço nas bordas. Reprova (ok=false) quando:
//   - não é JSON, tem lixo depois do JSON, ou final_text não é string;
//   - final_text está ausente, é null ou fica vazio depois de aparado;
//   - final_text tem caractere proibido (ehCaractereProibido).
//
// Campos EXTRAS na resposta são ignorados, sem ler: o modelo pode escrever
// o que quiser neles, e nada disso é usado nem logado. O schema já os
// proíbe no provedor (additionalProperties:false); isto é o cinto além do
// suspensório, e não vale rejeitar um ditado bom por causa de um campo que
// ninguém lê.
func guardaJSON(bruto []byte) (texto string, ok bool) {
	var resp struct {
		FinalText *string `json:"final_text"`
	}
	if err := json.Unmarshal(bruto, &resp); err != nil || resp.FinalText == nil {
		return "", false
	}
	texto = strings.TrimSpace(*resp.FinalText)
	if texto == "" {
		return "", false
	}
	for _, r := range texto {
		if ehCaractereProibido(r) {
			return "", false
		}
	}
	return texto, true
}

// ---------------------------------------------------------------------
// Guarda 4 — formato
// ---------------------------------------------------------------------

// preambulos são aberturas de assistente. Só reprovam quando a ENTRADA não
// abre do mesmo jeito: "claro que sim" ditado continua "Claro que sim.".
var preambulos = [][]string{
	{"claro"},
	{"aqui", "está"}, {"aqui", "esta"}, {"aqui", "vai"},
	{"segue"},
	{"com", "certeza"}, {"certamente"},
	{"perfeito"}, {"entendido"}, {"entendi"},
	{"desculpe"}, {"sinto", "muito"},
	{"como", "assistente"}, {"como", "um", "assistente"},
	{"não", "posso"}, {"não", "consigo"},
}

func comecaCom(palavras, prefixo []string) bool {
	if len(palavras) < len(prefixo) {
		return false
	}
	for i, p := range prefixo {
		if palavras[i] != p {
			return false
		}
	}
	return true
}

// semHesitacaoNoInicio descarta as hesitações do começo, que o Polish tem
// licença para tirar: "ahn claro que sim" -> "Claro que sim." não é
// preâmbulo inventado.
func semHesitacaoNoInicio(palavras []string) []string {
	for len(palavras) > 0 && ehHesitacao(palavras[0]) {
		palavras = palavras[1:]
	}
	return palavras
}

// ehEnvolucro reporta se a runa abre ou fecha um embrulho de texto: aspas de
// qualquer tipo ou parênteses.
func ehEnvolucro(r rune) bool {
	return strings.ContainsRune(`"“”«»‘’'()`, r)
}

// ehSimboloProibido reporta se a runa é um símbolo — matemático (Sm: "+",
// "~", "<", ">", "|", "→"), modificador (Sk: a crase) ou "outro" (So: emoji,
// marca) — ou um "_", colchete ou chave, que são pontuação mas são markdown
// e JSON. O Polish não tem por que escrever nenhum deles.
func ehSimboloProibido(r rune) bool {
	return unicode.In(r, unicode.Sm, unicode.So, unicode.Sk) || strings.ContainsRune("_[]{}", r)
}

// simbolosAMais reporta se a saída tem algum símbolo proibido em quantidade
// maior que a entrada. Símbolo que a entrada já tinha ("C++", "2 + 2") fica.
func simbolosAMais(entrada, saida string) bool {
	conta := func(s string) map[rune]int {
		m := map[rune]int{}
		for _, r := range s {
			if ehSimboloProibido(r) {
				m[r]++
			}
		}
		return m
	}
	in := conta(entrada)
	for r, n := range conta(saida) {
		if n > in[r] {
			return true
		}
	}
	return false
}

// guardaFormato passa (true) quando a saída NÃO tem formato de resposta de
// assistente. Reprova:
//   - preâmbulo ("Claro", "Aqui está", "Segue"...) que a entrada não tinha;
//   - "**" ou "#" a mais do que a entrada (negrito e título de markdown);
//   - símbolo a mais do que a entrada (ehSimboloProibido): cerca de código
//     (``` e ~~~), crase, emoji, "~" do riscado, "_" do itálico, colchete do
//     link, chave e colchete do JSON, "<", ">", "|", setas;
//   - a chave "ditado" que a entrada não tinha (o envelope do JSON escrito
//     DENTRO do texto; "final_text" já cai no "_" acima);
//   - o texto inteiro entre aspas ou parênteses, quando a entrada não estava.
//
// Só pega FORMATO. Palavra acrescentada, trocada ou apagada é assunto da
// guarda de palavras.
func guardaFormato(entrada, saida string) bool {
	pIn := semHesitacaoNoInicio(palavrasNormalizadas(entrada))
	pOut := palavrasNormalizadas(saida)
	for _, p := range preambulos {
		if comecaCom(pOut, p) && !comecaCom(pIn, p) {
			return false
		}
	}

	for _, marca := range []string{"**", "#"} {
		if strings.Count(saida, marca) > strings.Count(entrada, marca) {
			return false
		}
	}

	if strings.Contains(saida, "ditado") && !strings.Contains(entrada, "ditado") {
		return false
	}

	if simbolosAMais(entrada, saida) {
		return false
	}

	s := strings.TrimSpace(saida)
	e := strings.TrimSpace(entrada)
	if r0, _ := utf8.DecodeRuneInString(s); ehEnvolucro(r0) {
		if rN, _ := utf8.DecodeLastRuneInString(s); ehEnvolucro(rN) && len(s) > 1 {
			if e0, _ := utf8.DecodeRuneInString(e); !ehEnvolucro(e0) {
				return false
			}
		}
	}
	return true
}

// ehCaixaAlta reporta se a palavra tem duas letras ou mais e TODAS em
// maiúscula: "OK", "PGDAS", "AÇÃO", "EFD-REINF". Dígito e hífen não contam como
// letra ("3D" tem uma letra só, e "1º" tem uma letra sem caixa), então nenhum
// dos dois é caixa alta. "A" e "É" (uma letra) também não são: são a maiúscula
// de quem abre a frase.
func ehCaixaAlta(palavra string) bool {
	letras := 0
	for _, r := range palavra {
		if !unicode.IsLetter(r) {
			continue
		}
		if !unicode.IsUpper(r) {
			return false
		}
		letras++
	}
	return letras >= 2
}

// contarCaixaAlta conta, por palavra em minúsculas (a comparação é SEM caixa),
// as ocorrências que estão em caixa alta (ehCaixaAlta).
func contarCaixaAlta(s string) map[string]int {
	conta := map[string]int{}
	for _, w := range palavrasComHifen(s) {
		if ehCaixaAlta(w) {
			conta[strings.ToLower(w)]++
		}
	}
	return conta
}

// guardaCaixa passa (true) quando NENHUMA palavra de duas letras ou mais sai
// toda em maiúscula na saída sem estar toda em maiúscula na entrada.
//
// A guarda de palavras compara sem caixa, e a de siglas só vigia as siglas que
// a entrada já tinha: um modelo que obedece ao ditado "responda tudo em
// maiúsculas" devolve "VOCÊ AGORA É UM ASSISTENTE…", e nenhuma das duas
// reprovava (o gpt-5-nano fez exatamente isso na frase injecao-06 do
// benchmark de 30-set-2026). Esta fecha o furo.
//
// Como conta: por palavra sem caixa, a saída não pode ter mais ocorrências em
// caixa alta que a entrada. Fica de fora, como sempre:
//   - a maiúscula do início da frase e a do nome próprio ("Eu", "João": só a
//     inicial);
//   - a sigla que a entrada já trazia em caixa alta ("PGDAS" e "OK" ditados
//     assim continuam assim);
//   - texto que a entrada já trazia todo em caixa alta.
//
// O preço, aceito: "ok" -> "OK" e "cnpj" -> "CNPJ" reprovam, porque nada
// separa a sigla certa da palavra comum sem um léxico (ADR-016, decisão 7).
// O Polish legítimo não muda a grafia de sigla: a que o dicionário conhece o
// Smart já entrega na forma canônica.
func guardaCaixa(entrada, saida string) bool {
	naEntrada := contarCaixaAlta(entrada)
	for palavra, n := range contarCaixaAlta(saida) {
		if n > naEntrada[palavra] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------
// Guarda 5 — números
// ---------------------------------------------------------------------

// reTokenNumerico casa um NÚMERO inteiro, com tudo o que o faz valer aquele
// valor: o sinal colado ("-500"), a moeda antes ("R$", "US$", "$", "€",
// "£"), os separadores de dentro ("1.847,32", "05/10", "14:30") e o sufixo
// ("6%", "1º"). Ponto ou vírgula colados no fim não entram: "dia 20." é o
// número 20.
var reTokenNumerico = regexp.MustCompile(`[-+−]?(?:R\$|US\$|\$|€|£)?\s?\d(?:[\d.,/:]*\d)?\s?[%ºª°]?`)

// tokensNumericos devolve os números do texto, na ORDEM em que aparecem, cada
// um como um token só (sem espaços).
func tokensNumericos(s string) []string {
	var tokens []string
	for _, t := range reTokenNumerico.FindAllString(s, -1) {
		tokens = append(tokens, strings.Join(strings.Fields(t), ""))
	}
	return tokens
}

// reNumeracaoDeItem casa a numeração ditada em dígitos que vira marcador de
// lista: "item 1" e "1º" (também "1ª" e "1°").
var reNumeracaoDeItem = regexp.MustCompile(`(?i:\bitem\s+(\d{1,3}))\b|\b(\d{1,3})\s?[º°ª]`)

// semNumeracaoDeItem tira do texto a numeração de item ditada em dígitos e
// devolve o texto sem ela e os números tirados, na ordem.
func semNumeracaoDeItem(s string) (string, []int) {
	var numeros []int
	texto := reNumeracaoDeItem.ReplaceAllStringFunc(s, func(m string) string {
		n := 0
		for _, r := range m {
			if unicode.IsDigit(r) {
				n = n*10 + int(r-'0')
			}
		}
		numeros = append(numeros, n)
		return ""
	})
	return texto, numeros
}

// cuesDeNumeracao conta o que autoriza a saída a numerar uma lista: as
// palavras que a pessoa disse ao numerar ("primeiro", "segundo"...; "item
// um", "item 1", "1º"). Lista numerada SÓ quando a pessoa numera (plano da
// etapa 2, decisão do Mauro): sem isto, o modelo poderia acrescentar "1." e
// "2." por conta própria.
func cuesDeNumeracao(entrada string) int {
	palavras := palavrasNormalizadas(entrada)
	cues := 0
	for i, w := range palavras {
		if ordinaisDeLista[w] {
			cues++
		}
		if w == "item" && i+1 < len(palavras) && numeraisDeItem[palavras[i+1]] {
			cues++
		}
	}
	// "item 1", "1º": a numeração em dígitos.
	cues += len(reNumeracaoDeItem.FindAllString(entrada, -1))
	return cues
}

func ehSoDigitos(w string) bool {
	for _, r := range w {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// guardaNumeros passa (true) quando os NÚMEROS da saída são os da entrada,
// como sequência ORDENADA de tokens inteiros (tokensNumericos): mesmo valor,
// mesmo formato, mesma ordem, mesmo sinal, mesmo percentual, mesma moeda.
//
// Os marcadores de lista do começo das linhas não contam, dos dois lados —
// mas só os VÁLIDOS: "1.", "2.", "3." em sequência (semMarcadoresNumerados).
// Qualquer outro "N." é número. E a saída só pode ter mais marcadores
// numerados que a entrada quando a pessoa numerou (cuesDeNumeracao).
//
// Substitui sameDigits para o Polish: aquela guarda compara a sequência
// inteira de dígitos e derrubaria TODA lista numerada, porque "1.", "2.",
// "3." são dígitos que o modelo acrescenta legitimamente. Aqui a lista passa
// e o resto continua valendo:
//
//   - "doze" -> "12" reprova (aparece um 12 que não estava);
//   - "12" -> "doze" reprova (sumiu um 12);
//   - "R$ 1500" -> "R$ 1.500" e "R$ 1.500" -> "R$ 1,500" reprovam;
//   - "05/10" -> "10/05" e "14h30" -> "14:30" reprovam;
//   - "300 ... 500" -> "500 ... 300" reprova (a ordem conta);
//   - "6%" -> "6", "500" -> "-500" e "R$" -> "US$" reprovam.
//
// Numeração ditada em dígitos ("item 1 a DCTFWeb item 2...", "1º ... 2º")
// pode virar o marcador da lista: nesse caso o "item N" some da entrada e o
// marcador N aparece na saída, na mesma ordem.
func guardaNumeros(entrada, saida string) bool {
	textoE, marcadoresE := semMarcadoresNumerados(entrada)
	textoS, marcadoresS := semMarcadoresNumerados(saida)
	if len(marcadoresS)-len(marcadoresE) > cuesDeNumeracao(entrada) {
		return false
	}
	a, b := tokensNumericos(textoE), tokensNumericos(textoS)
	if slices.Equal(a, b) {
		return true
	}
	if len(marcadoresE) > 0 {
		return false
	}
	semItem, removidos := semNumeracaoDeItem(textoE)
	return slices.Equal(removidos, marcadoresS) &&
		slices.Equal(tokensNumericos(semItem), b)
}

// ---------------------------------------------------------------------
// Guarda 6 — siglas, termos do dicionário e nomes próprios
// ---------------------------------------------------------------------

// palavrasComHifen devolve as palavras do texto na ordem, com a grafia
// original. Palavra é a sequência de letras, dígitos e hífens internos, então
// "EFD-Reinf" é uma palavra só, e a vírgula ou o ponto colados no fim não
// entram.
func palavrasComHifen(s string) []string {
	var palavras []string
	runas := []rune(s)
	for i := 0; i < len(runas); {
		if !isWordRune(runas[i]) {
			i++
			continue
		}
		ini := i
		for i < len(runas) && (isWordRune(runas[i]) || (runas[i] == '-' && i+1 < len(runas) && isWordRune(runas[i+1]))) {
			i++
		}
		palavras = append(palavras, string(runas[ini:i]))
	}
	return palavras
}

// ehSigla reporta se a palavra tem maiúscula DEPOIS da primeira letra:
// DCTFWeb, PGDAS, EFD-Reinf, eSocial, ECF, iPhone. "Nota" e "A" (só a
// inicial maiúscula) não são; "OK" é.
func ehSigla(palavra string) bool {
	for i, r := range []rune(palavra) {
		if i > 0 && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// contarSiglas conta as ocorrências de cada sigla, com a grafia exata.
// Repetição IMEDIATA conta uma vez: gagueira sobre uma sigla ("DAS DAS")
// pode virar uma só.
func contarSiglas(s string) map[string]int {
	conta := map[string]int{}
	anterior := ""
	for _, w := range palavrasComHifen(s) {
		if ehSigla(w) && w != anterior {
			conta[w]++
		}
		anterior = w
	}
	return conta
}

// ehNomeProprio reporta se a palavra tem só a inicial maiúscula e ao menos
// duas letras: "João", "Maria". ("A" não é; "DCTFWeb" é sigla, não nome.)
func ehNomeProprio(w string) bool {
	runas := []rune(w)
	if len(runas) < 2 || !unicode.IsUpper(runas[0]) {
		return false
	}
	for _, r := range runas[1:] {
		if unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// contarNomes conta, com a grafia exata, os nomes próprios da entrada: a
// palavra com só a inicial maiúscula que NÃO abre frase. Quem abre frase é
// maiúscula por regra, não por ser nome. Palavra estrutural (hesitação,
// pontuação falada, ordinal) não é nome.
func contarNomes(entrada string) map[string]int {
	tokens, _ := tokenizar(entrada)
	marcas := marcarEstruturais(palavrasDe(tokens))
	conta := map[string]int{}
	for i, t := range tokens {
		if marcas[i].Classe != classeNenhuma || !ehNomeProprio(t.Bruta) {
			continue
		}
		if i == 0 || strings.ContainsAny(t.Antes, ".!?…\n") { // abrir linha, com ou sem marcador, é abrir frase
			continue
		}
		conta[t.Bruta]++
	}
	return conta
}

func mesmaRuna(a, b rune, ignorarCaixa bool) bool {
	if a == b {
		return true
	}
	return ignorarCaixa && unicode.ToLower(a) == unicode.ToLower(b)
}

// acharRunas devolve o índice da primeira ocorrência de agulha em palheiro
// que comece e termine em FRONTEIRA DE PALAVRA (a runa vizinha, quando a
// borda da agulha é letra ou dígito, não pode ser letra nem dígito), ou -1.
// Com ignorarCaixa, maiúscula e minúscula são iguais.
//
// A fronteira impede que "PGDAS" ache "PGDAS" dentro de "PGDASD"; a caixa
// exata é o que faz "DCTFWeb" -> "DCTFweb" ser detectado.
func acharRunas(palheiro, agulha []rune, de int, ignorarCaixa bool) int {
	n := len(agulha)
	if n == 0 {
		return -1
	}
	for i := de; i+n <= len(palheiro); i++ {
		casa := true
		for k := 0; k < n; k++ {
			if !mesmaRuna(palheiro[i+k], agulha[k], ignorarCaixa) {
				casa = false
				break
			}
		}
		if !casa {
			continue
		}
		if i > 0 && isWordRune(palheiro[i-1]) && isWordRune(agulha[0]) {
			continue
		}
		if i+n < len(palheiro) && isWordRune(palheiro[i+n]) && isWordRune(agulha[n-1]) {
			continue
		}
		return i
	}
	return -1
}

// inicioDeFrase reporta se a posição i do texto abre uma frase: começo do
// texto, depois de ".", "!", "?" ou "…", ou no começo de uma linha. Espaço,
// aspas, parênteses e marcador de lista antes da posição não contam.
func inicioDeFrase(runas []rune, i int) bool {
	for j := i - 1; j >= 0; j-- {
		r := runas[j]
		switch {
		case r == '\n':
			return true
		case unicode.IsSpace(r), ehEnvolucro(r), r == '-', r == '*', r == '•':
			continue
		}
		return r == '.' || r == '!' || r == '?' || r == '…'
	}
	return true
}

// contarRunas conta as ocorrências de alvo em texto (em fronteira de
// palavra, sem se sobrepor). Com soInicioDeFrase, só as que abrem frase.
func contarRunas(texto, alvo []rune, ignorarCaixa, soInicioDeFrase bool) int {
	n := 0
	for de := 0; ; {
		i := acharRunas(texto, alvo, de, ignorarCaixa)
		if i < 0 {
			return n
		}
		de = i + len(alvo)
		if !soInicioDeFrase || inicioDeFrase(texto, i) {
			n++
		}
	}
}

// temPalavra reporta se o trecho tem alguma letra ou dígito. Um trecho só de
// espaço e pontuação ("DCTFWeb, DCTFWeb") não separa duas ocorrências.
func temPalavra(trecho []rune) bool {
	return slices.ContainsFunc(trecho, isWordRune)
}

// comInicialMaiuscula devolve s com a primeira letra em maiúscula.
func comInicialMaiuscula(s string) string {
	r, tam := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[tam:]
}

// guardaSiglasTermos passa (true) quando as siglas, os termos do dicionário e
// os nomes próprios da entrada reaparecem na saída com a grafia exata E na
// mesma quantidade.
//
//   - Siglas (DCTFWeb, PGDAS, EFD-Reinf, eSocial, ECF, OK): cada sigla da
//     entrada tem de aparecer na saída o MESMO número de vezes, com a mesma
//     grafia. Uma de duas "ECF" virar "ecf", ou "das" virar "DAS" ao lado de
//     um "DAS", reprova. Repetição imediata ("DAS DAS") conta uma vez.
//   - Termos: termos são formas canônicas do dicionário, candidatas a estar
//     no texto, que a consulta LOCAL levantou (PolishResolver.Reescrever); o
//     dicionário nunca vai ao provedor. A presença é decidida aqui, sem
//     distinguir maiúscula: um termo "DCTFWeb" está presente em "dctfweb".
//     Cada ocorrência tem de reaparecer na grafia que a entrada usou, na
//     forma canônica, ou com só a inicial maiúscula quando abre frase ("das
//     guias" -> "Das guias"). "Dctfweb venceu" no meio da frase reprova: não
//     é nenhuma das três. Repetição IMEDIATA do termo (gagueira: "a DCTFWeb
//     DCTFWeb") conta uma vez, como a da sigla; duas ocorrências separadas
//     por outra palavra contam duas.
//   - Nomes próprios (João, Maria): a palavra com só a inicial maiúscula que
//     não abre frase tem de reaparecer com a mesma grafia — maiúscula e
//     acento —, ao menos as vezes que apareceu.
func guardaSiglasTermos(entrada, saida string, termos []string) bool {
	siglasSaida := contarSiglas(saida)
	for sigla, n := range contarSiglas(entrada) {
		if siglasSaida[sigla] != n {
			return false
		}
	}

	out := []rune(saida)
	for nome, n := range contarNomes(entrada) {
		if contarRunas(out, []rune(nome), false, false) < n {
			return false
		}
	}

	in := []rune(entrada)
	for _, termo := range termos {
		canonica := strings.TrimSpace(termo)
		alvo := []rune(canonica)
		if len(alvo) == 0 {
			continue
		}
		aceitas := map[string]bool{canonica: true} // grafias que valem em qualquer lugar
		presentes := 0
		fimAnterior := -1 // onde terminou a ocorrência anterior do termo
		for de := 0; ; {
			i := acharRunas(in, alvo, de, true)
			if i < 0 {
				break
			}
			de = i + len(alvo)
			aceitas[string(in[i:de])] = true
			// Repetição IMEDIATA do termo é gagueira ("a DCTFWeb DCTFWeb"): o
			// Polish tem licença para tirá-la (guarda 7), então conta uma vez,
			// como contarSiglas. Duas ocorrências separadas por qualquer palavra
			// continuam contando duas.
			if fimAnterior < 0 || temPalavra(in[fimAnterior:i]) {
				presentes++
			}
			fimAnterior = de
		}
		iniciais := map[string]bool{} // só com a inicial maiúscula, e só abrindo frase
		for g := range aceitas {
			if c := comInicialMaiuscula(g); !aceitas[c] {
				iniciais[c] = true
			}
		}
		achadas := 0
		for g := range aceitas {
			achadas += contarRunas(out, []rune(g), false, false)
		}
		for g := range iniciais {
			achadas += contarRunas(out, []rune(g), false, true)
		}
		if achadas < presentes {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------
// Guarda 7 — palavras
// ---------------------------------------------------------------------

// classeFalada diz o que uma palavra ESTRUTURAL da entrada é. Palavra
// estrutural é a que o Polish tem licença para não devolver.
type classeFalada int

const (
	classeNenhuma classeFalada = iota
	// classeHesitacao: "ahn", "hum", "éé". Some de graça.
	classeHesitacao
	// classeGagueira: a primeira de duas palavras idênticas em sequência
	// ("que que"). Some de graça; a segunda continua valendo. Numeral repetido
	// ("vinte vinte") não é gagueira: pode ser o valor, e na dúvida fica o Smart.
	classeGagueira
	// Pontuação falada. Só some de graça quando a saída tem o sinal NO LUGAR.
	classeVirgula
	classePonto
	classeDoisPontos
	classeInterrogacao
	classeExclamacao
	classePontoVirgula
	classeNovaLinha
	classeNovoParagrafo
	// classeOrdinal: "primeiro", "segundo", "item um", "item 1", "1º". Só
	// some de graça quando a linha da saída abre com marcador de lista e a saída
	// tem ao menos dois marcadores: uma lista de um item só não é lista.
	classeOrdinal
)

// marcaEstrutural marca uma palavra da entrada: a classe e a unidade de
// que ela faz parte, a[Ini:Fim]. Uma unidade sai INTEIRA ou não sai
// ("ponto de interrogação" nunca perde só o "de").
type marcaEstrutural struct {
	Classe   classeFalada
	Ini, Fim int
}

var (
	// hesitacoes são as únicas que o Polish pode tirar ("ahn", "hum", "éé").
	// "né", "tipo", "então" e "pra" NÃO estão aqui: são o jeito de falar da
	// pessoa (plano, pergunta 2).
	hesitacoes = map[string]bool{
		"ahn": true, "ahnn": true, "ahnnn": true, "ãhn": true, "ãh": true,
		"hum": true, "humm": true, "hmm": true, "hmmm": true,
		"uhm": true, "uhmm": true,
	}

	// ordinaisDeLista são os "primeiro… segundo…" que viram itens de lista.
	// Incluem "segunda", "quarta", "quinta" e "sexta", que também são dias da
	// semana: por isso sumir com um deles só vale quando a linha da saída
	// abre com marcador de lista, e uma vez só por marcador.
	ordinaisDeLista = map[string]bool{
		"primeiro": true, "primeira": true, "segundo": true, "segunda": true,
		"terceiro": true, "terceira": true, "quarto": true, "quarta": true,
		"quinto": true, "quinta": true, "sexto": true, "sexta": true,
		"sétimo": true, "sétima": true, "oitavo": true, "oitava": true,
		"nono": true, "nona": true, "décimo": true, "décima": true,
	}

	// numeraisDeItem completam o "item um", "item dois".
	numeraisDeItem = map[string]bool{
		"um": true, "uma": true, "dois": true, "duas": true, "três": true,
		"tres": true, "quatro": true, "cinco": true, "seis": true,
		"sete": true, "oito": true, "nove": true, "dez": true,
	}

	// padroesFalados são as pontuações faladas, da mais longa para a mais
	// curta: a primeira que casa vale.
	padroesFalados = []struct {
		palavras []string
		classe   classeFalada
	}{
		{[]string{"ponto", "e", "vírgula"}, classePontoVirgula}, {[]string{"ponto", "e", "virgula"}, classePontoVirgula},
		{[]string{"ponto", "de", "interrogação"}, classeInterrogacao}, {[]string{"ponto", "de", "interrogacao"}, classeInterrogacao},
		{[]string{"ponto", "de", "exclamação"}, classeExclamacao}, {[]string{"ponto", "de", "exclamacao"}, classeExclamacao},
		{[]string{"ponto", "final"}, classePonto},
		{[]string{"dois", "pontos"}, classeDoisPontos},
		{[]string{"novo", "parágrafo"}, classeNovoParagrafo}, {[]string{"novo", "paragrafo"}, classeNovoParagrafo},
		{[]string{"nova", "linha"}, classeNovaLinha},
		{[]string{"vírgula"}, classeVirgula}, {[]string{"virgula"}, classeVirgula},
		{[]string{"ponto"}, classePonto},
	}

	// sinalDaClasse é o sinal que a saída tem de ter no lugar da palavra
	// falada. Nova linha e novo parágrafo pedem uma quebra de linha.
	sinalDaClasse = map[classeFalada]rune{
		classeVirgula: ',', classePonto: '.', classeDoisPontos: ':',
		classeInterrogacao: '?', classeExclamacao: '!', classePontoVirgula: ';',
		classeNovaLinha: '\n', classeNovoParagrafo: '\n',
	}

	// antesDeSubstantivo são as palavras que, logo antes de "ponto",
	// "vírgula", "novo parágrafo"..., mostram que a palavra é SUBSTANTIVO e
	// não comando: "esse ponto", "um novo parágrafo", "os dois pontos". Apagar
	// a palavra depois delas não é converter pontuação falada.
	antesDeSubstantivo = setDePalavras(
		"o a os as um uma uns umas do da dos das no na nos nas ao aos à às " +
			"este esta esse essa aquele aquela neste nesta nesse nessa naquele naquela " +
			"deste desta desse dessa daquele daquela outro outra outros outras cada " +
			"seu sua seus suas meu minha nosso nossa algum alguma qualquer único única " +
			"último última próximo próxima")

	// complementoDeSubstantivo são as palavras que, logo DEPOIS de "ponto" ou
	// "dois pontos", mostram um substantivo com complemento: "bom ponto de
	// venda", "dois pontos de vista". Quem dita o comando abre outra frase.
	complementoDeSubstantivo = setDePalavras("de do da dos das")

	// numeraisPorExtenso são os números escritos em palavras. Trocar, apagar
	// ou acrescentar um deles muda o valor.
	numeraisPorExtenso = setDePalavras(
		"zero um uma dois duas três tres quatro cinco seis sete oito nove dez onze doze treze " +
			"catorze quatorze quinze dezesseis dezessete dezoito dezenove vinte trinta quarenta " +
			"cinquenta sessenta setenta oitenta noventa cem cento duzentos duzentas trezentos " +
			"trezentas quatrocentos quatrocentas quinhentos quinhentas seiscentos seiscentas " +
			"setecentos setecentas oitocentos oitocentas novecentos novecentas mil milhão milhões " +
			"bilhão bilhões meio meia")

	// protegidas são as palavras de negação, afirmação e quantificação que
	// nunca podem ser apagadas (a menos que estruturais): elas invertem ou
	// mudam o sentido ("não", "nunca", "sem", "sempre", "só", "mais"). Os
	// numerais por extenso e os números em dígitos também são protegidos: ver
	// ehProtegida.
	protegidas = setDePalavras(
		"não nunca nem nada ninguém jamais nenhum nenhuma nenhuns nenhumas sem sempre " +
			"sim só somente apenas mais menos todo toda todos todas tudo")

	reOrdinalEmDigito = regexp.MustCompile(`^\d{1,3}[ºª]$`)
)

func setDePalavras(lista string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(lista) {
		m[w] = true
	}
	return m
}

// ehHesitacao reporta se a palavra normalizada é uma hesitação sem sentido:
// as da lista, mais qualquer "éé", "ééé"...
func ehHesitacao(w string) bool {
	if hesitacoes[w] {
		return true
	}
	return utf8.RuneCountInString(w) >= 2 && strings.Trim(w, "é") == ""
}

// ehNumeral reporta se a palavra é um número: por extenso ou em dígitos.
func ehNumeral(w string) bool {
	r, _ := utf8.DecodeRuneInString(w)
	return numeraisPorExtenso[w] || unicode.IsDigit(r)
}

// ehProtegida reporta se a palavra nunca pode ser apagada: negação,
// quantificação, numeral por extenso ou número em dígitos.
func ehProtegida(w string) bool {
	return protegidas[w] || ehNumeral(w)
}

// marcarEstruturais marca as palavras da ENTRADA que o Polish tem licença
// para não devolver, com a classe de cada uma (ver classeFalada):
// hesitações, o primeiro de dois idênticos em sequência (gagueira), pontuação
// falada, ordinais de lista e "item um", "item 1", "1º".
//
// Marcar é só dar a licença para o alinhamento; as contrapartidas — o sinal
// na saída, o marcador de lista — são conferidas em licencasDasApagadas.
func marcarEstruturais(a []string) []marcaEstrutural {
	marcas := make([]marcaEstrutural, len(a))
	usado := make([]bool, len(a))
	atribui := func(ini, fim int, c classeFalada) {
		for k := ini; k < fim; k++ {
			marcas[k] = marcaEstrutural{Classe: c, Ini: ini, Fim: fim}
			usado[k] = true
		}
	}
	for i := 0; i < len(a); i++ {
		if usado[i] {
			continue
		}
		w := a[i]
		switch {
		case ehHesitacao(w):
			atribui(i, i+1, classeHesitacao)
		case i+1 < len(a) && w == a[i+1] && !ehNumeral(w):
			atribui(i, i+1, classeGagueira)
		case w == "item" && i+1 < len(a) && (numeraisDeItem[a[i+1]] || ehSoDigitos(a[i+1])):
			atribui(i, i+2, classeOrdinal)
		case ordinaisDeLista[w] || reOrdinalEmDigito.MatchString(w):
			atribui(i, i+1, classeOrdinal)
		default:
			for _, p := range padroesFalados {
				if comecaCom(a[i:], p.palavras) {
					atribui(i, i+len(p.palavras), p.classe)
					break
				}
			}
		}
	}
	return marcas
}

// alinhar calcula a distância de edição por PALAVRA entre a entrada (a) e a
// saída (b), com peso: trocar ou inserir custa 1, apagar custa 1 — e 0 quando
// a palavra apagada é estrutural (estr). Devolve o custo mínimo e, para cada
// palavra da saída, se ela é NOVA (inserida ou trocada) no alinhamento.
//
// É o alinhamento FROUXO: só serve para dizer, depois que a guarda reprovou,
// se o motivo é palavra acrescentada ou trocada (motivoDaReprovacao). Quem
// decide se a saída passa é alinharApagando.
//
// Nos empates prefere casar e depois apagar, para que "nova" signifique de
// fato "não estava na entrada". ok=false quando a tabela passaria de
// polishMaxCelulas.
func alinhar(a []string, estr []bool, b []string) (custo int, novas []bool, ok bool) {
	n, m := len(a), len(b)
	if (n+1)*(m+1) > polishMaxCelulas {
		return 0, nil, false
	}
	largura := m + 1
	d := make([]int32, (n+1)*largura)
	apagar := func(i int) int32 { // custo de apagar a[i]
		if estr[i] {
			return 0
		}
		return 1
	}
	for j := 1; j <= m; j++ {
		d[j] = int32(j)
	}
	for i := 1; i <= n; i++ {
		d[i*largura] = d[(i-1)*largura] + apagar(i-1)
		for j := 1; j <= m; j++ {
			sub := d[(i-1)*largura+j-1]
			if a[i-1] != b[j-1] {
				sub++
			}
			melhor := sub
			if v := d[(i-1)*largura+j] + apagar(i-1); v < melhor {
				melhor = v
			}
			if v := d[i*largura+j-1] + 1; v < melhor {
				melhor = v
			}
			d[i*largura+j] = melhor
		}
	}

	novas = make([]bool, m)
	i, j := n, m
	for i > 0 || j > 0 {
		atual := d[i*largura+j]
		switch {
		case i > 0 && j > 0 && a[i-1] == b[j-1] && atual == d[(i-1)*largura+j-1]:
			i--
			j--
		case i > 0 && atual == d[(i-1)*largura+j]+apagar(i-1):
			i--
		case i > 0 && j > 0 && atual == d[(i-1)*largura+j-1]+1:
			novas[j-1] = true
			i--
			j--
		default: // inserção
			novas[j-1] = true
			j--
		}
	}
	return int(d[n*largura+m]), novas, true
}

// alinharApagando alinha a saída (b) à entrada (a) SÓ com casamentos e
// apagamentos: a saída tem de ser a entrada menos algumas palavras, na mesma
// ordem. Nenhuma palavra nova, nenhuma trocada.
//
// Custo de apagar a[i]: 0 se é estrutural; polishInviavel se é protegida
// (negação, quantificação, numeral); 0 se é o "e" logo antes de uma palavra
// da saída que abre linha com marcador de lista (marcadorEm[j]: o marcador
// tomou o lugar do "e"); 1 nas demais. ok=false quando não há alinhamento — a
// saída acrescenta ou troca palavra, ou apaga uma protegida — ou quando a
// tabela passaria de polishMaxCelulas.
//
// gap[i] diz, para cada palavra apagada da entrada, entre quais palavras da
// saída ela estava: é o índice j da palavra da saída que vem DEPOIS dela
// (len(b) quando é a última), e vale -1 para a palavra que ficou.
// marcadorEm tem len(b)+1 posições; a última é sempre falsa.
//
// Com corr, o alinhamento aceita também a correção de grafia (polish_grafia.go):
// a[i] casa com b[j] quando corr.troca(i, j); a[i] e a[i+1] casam com b[j]
// quando corr.junta(i, j); a[i] casa com b[j] e b[j+1] quando
// corr.separa(i, j). Com corr nil, só casa a palavra igual.
func alinharApagando(a []string, marcas []marcaEstrutural, b []string, marcadorEm []bool, corr *correcoes) (gap []int, custo int, ok bool) {
	n, m := len(a), len(b)
	if (n+1)*(m+1) > polishMaxCelulas {
		return nil, 0, false
	}
	// custoApagar é o custo de apagar a[i] quando j palavras da saída já foram
	// casadas, isto é, quando a palavra cai no vão antes de b[j].
	custoApagar := func(i, j int) int32 {
		switch {
		case marcas[i].Classe != classeNenhuma:
			return 0
		case ehProtegida(a[i]):
			return polishInviavel
		case a[i] == "e" && marcadorEm[j]:
			return 0
		}
		return 1
	}
	soma := func(x, y int32) int32 {
		if x+y >= polishInviavel {
			return polishInviavel
		}
		return x + y
	}

	// casa: a[i] fica como b[j], igual ou com a grafia corrigida.
	casa := func(i, j int) bool {
		return a[i] == b[j] || (corr != nil && corr.troca(i, j))
	}
	junta := func(i, j int) bool { return corr != nil && i >= 0 && corr.junta(i, j) }
	separa := func(i, j int) bool { return corr != nil && j >= 0 && corr.separa(i, j) }

	largura := m + 1
	d := make([]int32, (n+1)*largura)
	for j := 1; j <= m; j++ {
		d[j] = polishInviavel
	}
	for i := 1; i <= n; i++ {
		d[i*largura] = soma(d[(i-1)*largura], custoApagar(i-1, 0))
		for j := 1; j <= m; j++ {
			melhor := soma(d[(i-1)*largura+j], custoApagar(i-1, j))
			if v := d[(i-1)*largura+j-1]; v < melhor && casa(i-1, j-1) {
				melhor = v
			}
			if i >= 2 {
				if v := d[(i-2)*largura+j-1]; v < melhor && junta(i-2, j-1) {
					melhor = v
				}
			}
			if j >= 2 {
				if v := d[(i-1)*largura+j-2]; v < melhor && separa(i-1, j-2) {
					melhor = v
				}
			}
			d[i*largura+j] = melhor
		}
	}
	if d[n*largura+m] >= polishInviavel {
		return nil, 0, false
	}

	gap = make([]int, n)
	for i := range gap {
		gap[i] = -1
	}
	i, j := n, m
	for i > 0 {
		atual := d[i*largura+j]
		switch {
		case j > 0 && atual == d[(i-1)*largura+j-1] && casa(i-1, j-1):
			i--
			j--
		case i >= 2 && j > 0 && atual == d[(i-2)*largura+j-1] && junta(i-2, j-1):
			i -= 2
			j--
		case j >= 2 && atual == d[(i-1)*largura+j-2] && separa(i-1, j-2):
			i--
			j -= 2
		default:
			gap[i-1] = j
			i--
		}
	}
	return gap, int(d[n*largura+m]), true
}

// licencasDasApagadas confere a contrapartida de cada palavra ESTRUTURAL que
// a saída apagou (gap de alinharApagando). Apagar de graça só vale quando a
// palavra virou o que ela pede:
//
//   - hesitação e gagueira: nada em troca;
//   - pontuação falada: o sinal correspondente ("," "." ":" "?" "!" ";" ou
//     uma quebra de linha) no lugar exato onde a palavra estava, e nunca
//     depois de artigo, demonstrativo ou preposição ("esse ponto", "um novo
//     parágrafo" são substantivos) nem entre dois números ("dois vírgula
//     cinco", "versão dois ponto zero" são valores);
//   - ordinal: a linha da saída onde a palavra estava abre com marcador de
//     lista, um ordinal por marcador ("vence na sexta" perde o "sexta" para
//     nada), e a saída tem ao menos dois marcadores ("primeiro vamos abrir a
//     conta" virar "1. Vamos abrir a conta" é uma lista de um item só);
//   - a unidade sai inteira: "ponto de interrogação" nunca perde só o "de".
func licencasDasApagadas(a []string, marcas []marcaEstrutural, gap []int, tb []tokenTexto, cauda string) bool {
	antes := func(j int) string { // o texto da saída onde estava a palavra apagada
		if j < len(tb) {
			return tb[j].Antes
		}
		return cauda
	}
	precisa := map[int]map[rune]int{} // gap -> sinal -> quantas palavras faladas pedem
	ordinais := map[int]int{}         // gap -> ordinais apagados
	vista := map[int]bool{}           // unidades já conferidas (pela primeira palavra)
	marcadores := 0                   // linhas da saída que abrem com marcador de lista
	for _, t := range tb {
		if t.Marcador {
			marcadores++
		}
	}
	for i := range a {
		m := marcas[i]
		if gap[i] < 0 || m.Classe == classeNenhuma || m.Classe == classeHesitacao || m.Classe == classeGagueira {
			continue
		}
		for k := m.Ini; k < m.Fim; k++ {
			if gap[k] != gap[i] {
				return false // a unidade ficou pela metade
			}
		}
		if vista[m.Ini] {
			continue
		}
		vista[m.Ini] = true
		j := gap[i]

		if m.Classe == classeOrdinal {
			ordinais[j]++
			if j >= len(tb) || !tb[j].Marcador || ordinais[j] > 1 || marcadores < 2 {
				return false
			}
			continue
		}

		if m.Ini > 0 && antesDeSubstantivo[a[m.Ini-1]] {
			return false
		}
		if (m.Classe == classePonto || m.Classe == classeDoisPontos) &&
			m.Fim < len(a) && complementoDeSubstantivo[a[m.Fim]] {
			return false
		}
		if (m.Classe == classeVirgula || m.Classe == classePonto) &&
			m.Ini > 0 && m.Fim < len(a) && ehNumeral(a[m.Ini-1]) && ehNumeral(a[m.Fim]) {
			return false
		}
		if precisa[j] == nil {
			precisa[j] = map[rune]int{}
		}
		precisa[j][sinalDaClasse[m.Classe]]++
	}
	for j, sinais := range precisa {
		texto := antes(j)
		for sinal, n := range sinais {
			if strings.Count(texto, string(sinal)) < n {
				return false
			}
		}
	}
	return true
}

// limiteDeEdicao é o número máximo de palavras que maxEdicao permite apagar
// sobre efetivas palavras. O limite é INCLUSIVO: 3 em 30 palavras são 10%, e
// 10% passa.
//
// A folga de 1e-9 existe porque ponto flutuante erra por baixo: 0,29*100 dá
// 28,999999999999996, e sem a folga o limite de 29 viraria 28 e uma saída
// exatamente no orçamento seria reprovada.
func limiteDeEdicao(efetivas int, maxEdicao float64) int {
	return int(math.Floor(float64(efetivas)*maxEdicao + 1e-9))
}

// motivoDaReprovacao diz por que uma saída que não é a entrada menos
// palavras foi reprovada: MotivoTrechoNovo quando o alinhamento frouxo acha um
// trecho de 3 ou mais palavras novas seguidas e o desvio total ainda é menor
// que polishTaxaDeReescrita das palavras (a frase acrescentada num ditado
// longo), e MotivoPalavras nos demais (reescrita, resumo, resposta, palavra
// trocada). Só serve ao relato; o veredito já foi dado, e não depende do
// orçamento de palavras apagadas.
func motivoDaReprovacao(a []string, marcas []marcaEstrutural, b []string, efetivas int) MotivoFallback {
	estr := make([]bool, len(a))
	for i, m := range marcas {
		estr[i] = m.Classe != classeNenhuma
	}
	custo, novas, ok := alinhar(a, estr, b)
	if !ok || custo > limiteDeEdicao(efetivas, polishTaxaDeReescrita) {
		return MotivoPalavras
	}
	seguidas := 0
	for _, nova := range novas {
		if !nova {
			seguidas = 0
			continue
		}
		seguidas++
		if seguidas >= polishTrechoNovo {
			return MotivoTrechoNovo
		}
	}
	return MotivoPalavras
}

// guardaPalavras devolve MotivoNenhum quando a saída é a entrada só com
// pontuação, caixa e marcadores — e, no máximo, algumas palavras a menos —, ou
// o motivo da reprovação (MotivoPalavras, MotivoTrechoNovo).
//
// Como conta: as duas listas são normalizadas (sem caixa, sem pontuação de
// borda, sem marcador de lista) e a saída tem de ser uma SUBSEQUÊNCIA da
// entrada (alinharApagando):
//
//   - NENHUMA palavra acrescentada, em lugar nenhum: nem no começo ("Sim,"),
//     nem no fim ("Obrigado!"), nem no meio;
//   - NENHUMA palavra trocada, nem por sinônimo, nem por outro número, nem por
//     outro nome. Trocar e acrescentar não têm orçamento: o Polish
//     legítimo nunca faz uma coisa nem outra. A exceção única é a correção
//     de grafia da palavra que não existe em português (polish_grafia.go,
//     emenda de 18-out-2026 da ADR-016), tentada só quando a saída não passa
//     sem ela;
//   - palavra protegida (negação, quantificação, numeral por extenso) nunca é
//     apagada;
//   - palavra estrutural (hesitação, gagueira, pontuação falada, ordinal de
//     lista) é apagada de graça, mas só com a contrapartida
//     (licencasDasApagadas);
//   - o "e" que a lista trocou pelo marcador ("a, b e c" -> "- a\n- b\n- c")
//     é apagado de graça, quando a palavra da saída logo depois dele abre a
//     linha com marcador;
//   - qualquer OUTRA palavra ("né", "tipo", "então", "antes", "já") só pode ser
//     apagada dentro do orçamento maxEdicao, que é ZERO por padrão
//     (PolishMaxEdicao): o prompt manda manter o jeito de falar da pessoa, e
//     uma palavra comum a menos é motivo para cair no Smart. Com um orçamento
//     maior, vale a taxa sobre as palavras não estruturais da entrada: 10% de
//     30 palavras são 3 apagadas, e de 8 palavras nenhuma.
func guardaPalavras(entrada, saida string, maxEdicao float64) MotivoFallback {
	ta, _ := tokenizar(entrada)
	tb, cauda := tokenizar(saida)
	a, b := palavrasDe(ta), palavrasDe(tb)
	marcas := marcarEstruturais(a)

	efetivas := 0
	for _, m := range marcas {
		if m.Classe == classeNenhuma {
			efetivas++
		}
	}
	marcadorEm := make([]bool, len(b)+1) // a palavra j da saída abre linha com marcador?
	for j, t := range tb {
		marcadorEm[j] = t.Marcador
	}
	avaliar := func(corr *correcoes) MotivoFallback {
		gap, custo, ok := alinharApagando(a, marcas, b, marcadorEm, corr)
		if !ok {
			return motivoDaReprovacao(a, marcas, b, efetivas)
		}
		if custo > limiteDeEdicao(efetivas, maxEdicao) {
			return MotivoPalavras
		}
		if !licencasDasApagadas(a, marcas, gap, tb, cauda) {
			return MotivoPalavras
		}
		return MotivoNenhum
	}
	motivo := avaliar(nil)
	if motivo == MotivoNenhum {
		return MotivoNenhum
	}
	// Só a saída que não passou sem correção consulta o dicionário: a que é a
	// entrada menos palavras nunca o carrega. Se nem com a correção de grafia
	// ela passa, o motivo é o da conta sem correção, o mesmo de antes da
	// emenda de 18-out-2026.
	if avaliar(novasCorrecoes(a, ta, marcas, b, tb)) == MotivoNenhum {
		return MotivoNenhum
	}
	return motivo
}
