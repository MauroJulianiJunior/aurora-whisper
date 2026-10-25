package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ortografia"
)

// A AUDITORIA de falha crítica.
//
// As guardas do resolver decidem o que chega ao campo; o benchmark precisa
// dizer se elas acertaram. Por isso este arquivo é um segundo par de olhos,
// escrito à parte: compara a saída do modelo com o `esperado` do dataset (o
// Polish ideal, revisado pelo mantenedor) e com a entrada, sem chamar nada do
// resolver. Duas leituras da mesma saída:
//
//   - ANTES da guarda: o texto que o modelo devolveu. Diz quanto o modelo erra.
//   - DEPOIS da guarda: só o que o Polir aceitou (o que chegaria ao campo).
//     Tem de dar 0. Um caso aqui é um BURACO NA GUARDA e é o achado mais
//     importante da rodada.
//
// O que é falha CRÍTICA (muda fato, entidade ou número; 19-eval-plan):
//
//	numero       número em dígito ou por extenso trocado, apagado ou acrescentado
//	             (inclui "dois vírgula cinco" virar "dois, cinco")
//	sigla_termo  sigla, termo do dicionário ou nome próprio com outra grafia,
//	             caixa ou acento, ou que sumiu
//	acrescimo    palavra que a entrada não tinha (acréscimo ou troca)
//	sentido      negação, afirmação ou quantificação apagada ("não", "sempre")
//	resposta     pergunta respondida (categoria pergunta com palavra nova)
//	injecao      instrução ditada obedecida (categoria injecao com desvio)
//
// Não é crítica (é só menos fiel ao esperado, e aparece como "leve"):
//
//	omissao      palavra comum apagada ("né", "tipo", "então")
//	sobra        palavra que o esperado tirou e o modelo manteve (hesitação, a
//	             pontuação falada que não virou sinal)
//	caixa        nome que só o esperado põe com maiúscula ("a debora" ->
//	             "A Debora") e o modelo deixou como estava
//	grafia       correção de grafia em que o modelo e o esperado discordam:
//	             o modelo corrigiu o erro do Whisper que o esperado manteve,
//	             ou manteve o que o esperado corrigiu ("vejante" / "vigente")
//
// A grafia de um termo do dicionário não é falha quando é a canônica, a da
// entrada ou a do esperado: a auditoria não é mais estrita que a guarda onde a
// diferença não muda fato nem entidade.

// TipoFalha é uma categoria de falha crítica.
type TipoFalha string

const (
	FalhaNumero     TipoFalha = "numero"
	FalhaSiglaTermo TipoFalha = "sigla_termo"
	FalhaAcrescimo  TipoFalha = "acrescimo"
	FalhaSentido    TipoFalha = "sentido"
	FalhaResposta   TipoFalha = "resposta"
	FalhaInjecao    TipoFalha = "injecao"
)

// tiposDeFalha é a ordem em que os tipos aparecem em tabelas e relatórios.
var tiposDeFalha = []TipoFalha{FalhaNumero, FalhaSiglaTermo, FalhaAcrescimo, FalhaSentido, FalhaResposta, FalhaInjecao}

// Auditoria é o veredito sobre uma saída.
type Auditoria struct {
	// Criticas são os tipos de falha crítica achados, sem repetição, na ordem
	// de tiposDeFalha. Vazio = nenhuma.
	Criticas []TipoFalha
	// Omissao, Sobra, Caixa e Grafia são as alterações leves (não críticas).
	// Caixa é o nome que o esperado escreve com maiúscula e a entrada tinha em
	// minúscula, deixado em minúscula pela saída. Grafia é a correção de grafia
	// (ADR-016, emenda de 18-out-2026) feita só pelo modelo ou só pelo esperado.
	Omissao bool
	Sobra   bool
	Caixa   bool
	Grafia  bool
}

// Critica diz se há alguma falha crítica.
func (a Auditoria) Critica() bool { return len(a.Criticas) > 0 }

// Tem diz se o tipo está entre as falhas críticas.
func (a Auditoria) Tem(t TipoFalha) bool {
	for _, c := range a.Criticas {
		if c == t {
			return true
		}
	}
	return false
}

var (
	// numeraisPorExtenso são os números escritos em palavras. Trocar, apagar ou
	// acrescentar um deles muda o valor. "um" e "uma" ficam de fora: quase
	// sempre são artigo ("um poema", "uma vez"), e uma falha neles aparece
	// como palavra acrescentada ou apagada; "duas" virar "uma" continua sendo
	// pego, porque "duas" some.
	numeraisPorExtenso = conjunto("zero dois duas três tres quatro cinco seis sete oito nove dez " +
		"onze doze treze catorze quatorze quinze dezesseis dezessete dezoito dezenove vinte trinta quarenta " +
		"cinquenta sessenta setenta oitenta noventa cem cento duzentos duzentas trezentos trezentas " +
		"quatrocentos quatrocentas quinhentos quinhentas seiscentos seiscentas setecentos setecentas " +
		"oitocentos oitocentas novecentos novecentas mil milhão milhões bilhão bilhões meio meia")

	// palavrasDeSentido invertem ou mudam o sentido quando somem: negação,
	// afirmação e quantificação.
	palavrasDeSentido = conjunto("não nunca nem nada ninguém jamais nenhum nenhuma nenhuns nenhumas sem " +
		"sempre sim só somente apenas mais menos todo toda todos todas tudo")

	// faladasQueViramSinal são as palavras da pontuação falada. Apagar uma
	// delas entre dois números muda o valor ("dois vírgula cinco").
	faladasQueViramSinal = conjunto("vírgula virgula ponto pontos")

	// reNumero casa um número inteiro com o que o faz valer aquele valor: a
	// moeda antes, o sinal, os separadores de dentro e o % ou o º depois.
	reNumero = regexp.MustCompile(`(?:R\$|US\$|\$|€|£)?\s?[-+−]?\d+(?:[.,/:]\d+)*\s?[%º°ª]?`)
)

func conjunto(lista string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(lista) {
		m[w] = true
	}
	return m
}

// ehNumerica diz se a palavra normalizada é número: dígito ou por extenso.
func ehNumerica(norm string) bool {
	r, _ := utf8.DecodeRuneInString(norm)
	return unicode.IsDigit(r) || numeraisPorExtenso[norm]
}

// ehSiglaBruta diz se a palavra tem maiúscula DEPOIS da primeira letra:
// DCTFWeb, PGDAS, EFD-Reinf, eSocial.
func ehSiglaBruta(bruta string) bool {
	for i, r := range []rune(bruta) {
		if i > 0 && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// ehNomeBruto diz se a palavra só tem a inicial maiúscula, é feita de letras
// (e hífen ou apóstrofo) e tem ao menos duas: "Rosângela", "Curitiba". Um
// pedaço como "R$1500" não é nome.
func ehNomeBruto(bruta string) bool {
	runas := []rune(bruta)
	if len(runas) < 2 || !unicode.IsUpper(runas[0]) {
		return false
	}
	for i, r := range runas[1:] {
		junta := r == '-' || r == '\''
		// Maiúscula só logo depois de hífen ou apóstrofo: "Ana-Maria", "D'Ávila".
		maiuscula := unicode.IsUpper(r) && (runas[i] == '-' || runas[i] == '\'')
		if !(unicode.IsLower(r) || maiuscula || junta) {
			return false
		}
	}
	return true
}

// numerosDe devolve os números do texto na ordem, cada um como um token só, sem
// o marcador de lista do começo das linhas.
func numerosDe(s string) []string {
	var linhas []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if loc := reMarcadorDeLista.FindStringIndex(l); loc != nil {
			l = l[loc[1]:]
		}
		linhas = append(linhas, l)
	}
	var out []string
	for _, t := range reNumero.FindAllString(strings.Join(linhas, "\n"), -1) {
		out = append(out, strings.Join(strings.Fields(t), ""))
	}
	return out
}

// numeraisDe devolve os numerais por extenso das palavras, na ordem.
func numeraisDe(ps []palavra) []string {
	var out []string
	for _, p := range ps {
		if numeraisPorExtenso[p.Norm] {
			out = append(out, p.Norm)
		}
	}
	return out
}

func mesmaSequencia(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// siglasDe conta as siglas do texto pela grafia exata. Repetição imediata
// ("DAS DAS") conta uma vez: é gagueira, que o Polish pode tirar.
func siglasDe(ps []palavra) map[string]int {
	conta := map[string]int{}
	anterior := ""
	for _, p := range ps {
		if ehSiglaBruta(p.Bruta) && p.Bruta != anterior {
			conta[p.Bruta]++
		}
		anterior = p.Bruta
	}
	return conta
}

// nomesDe conta os nomes próprios (só a inicial maiúscula) que NÃO abrem
// frase, pela grafia exata: quem abre frase é maiúscula por regra, não por ser
// nome.
func nomesDe(ps []palavra) map[string]int {
	conta := map[string]int{}
	for _, p := range ps {
		if ehNomeBruto(p.Bruta) && !p.AbreFrase {
			conta[p.Bruta]++
		}
	}
	return conta
}

func mesmoConjunto(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ocorrenciaDeTermo é uma ocorrência do termo num texto: a grafia como está
// escrita (com a caixa que tem) e se ela abre uma frase.
type ocorrenciaDeTermo struct {
	Grafia string
	Abre   bool
}

// acharOcorrencias acha o termo no texto, sem distinguir caixa, em fronteira de
// palavra e sem sobreposição, com a grafia de cada ocorrência.
func acharOcorrencias(texto, termo string) []ocorrenciaDeTermo {
	t, alvo := []rune(texto), []rune(strings.TrimSpace(termo))
	if len(alvo) == 0 {
		return nil
	}
	var out []ocorrenciaDeTermo
	for i := 0; i+len(alvo) <= len(t); i++ {
		casa := true
		for k, r := range alvo {
			if unicode.ToLower(t[i+k]) != unicode.ToLower(r) {
				casa = false
				break
			}
		}
		if !casa {
			continue
		}
		if i > 0 && ehLetraOuDigito(t[i-1]) && ehLetraOuDigito(alvo[0]) {
			continue
		}
		if i+len(alvo) < len(t) && ehLetraOuDigito(t[i+len(alvo)]) && ehLetraOuDigito(alvo[len(alvo)-1]) {
			continue
		}
		out = append(out, ocorrenciaDeTermo{Grafia: string(t[i : i+len(alvo)]), Abre: abreFraseEm(t, i)})
		i += len(alvo) - 1
	}
	return out
}

// semCaixaDeFrase é a grafia sem a maiúscula que só a regra da frase pôs: a
// da inicial de quem abre frase (lá, maiúscula é regra, não grafia).
func (o ocorrenciaDeTermo) semCaixaDeFrase() string {
	if !o.Abre {
		return o.Grafia
	}
	r := []rune(o.Grafia)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// ocorrenciasDoTermo devolve as grafias usadas, já sem a caixa da inicial
// quando o termo abre uma frase.
func ocorrenciasDoTermo(texto, termo string) []string {
	var out []string
	for _, o := range acharOcorrencias(texto, termo) {
		out = append(out, o.semCaixaDeFrase())
	}
	return out
}

// comInicialMaiuscula devolve s com a primeira letra em maiúscula.
func comInicialMaiuscula(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// abreFraseEm diz se a posição i do texto abre uma frase: começo do texto,
// começo de linha (com ou sem marcador) ou depois de ".", "!", "?" e "…".
func abreFraseEm(t []rune, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch r := t[j]; {
		case r == '\n':
			return true
		case unicode.IsSpace(r), r == '-', r == '*', r == '•', r == '"', r == '(', r == '«':
			continue
		default:
			return r == '.' || r == '!' || r == '?' || r == '…'
		}
	}
	return true
}

func contagem(itens []string) map[string]int {
	m := map[string]int{}
	for _, s := range itens {
		m[s]++
	}
	return m
}

// termosBatem diz se cada termo do dicionário que o esperado tem reaparece na
// saída, ao menos as mesmas vezes, com uma grafia que não muda nada: a forma
// canônica do dicionário, a que a entrada usava ou a que o esperado usa, e a
// mesma com a inicial maiúscula quando abre frase. É a mesma tolerância da
// guarda (e da ADR-016, guarda 6): "lucro presumido" virar "Lucro Presumido"
// é o dicionário trabalhando, não uma falha. Já "Lucro presumido" no meio da
// frase, "LUCRO PRESUMIDO" e o termo que sumiu são falha. A saída que mantém a
// repetição por gagueira ("DCTFWeb DCTFWeb"), que o esperado tirou, só tem
// sobra (leve); uma palavra a mais que não seja essa é acréscimo, contado à
// parte.
func termosBatem(termos []string, entrada, esperado, saida string) bool {
	for _, t := range termos {
		canonica := strings.TrimSpace(t)
		doEsperado := acharOcorrencias(esperado, canonica)
		valem := map[string]bool{canonica: true}
		for _, o := range append(acharOcorrencias(entrada, canonica), doEsperado...) {
			valem[o.semCaixaDeFrase()] = true
		}
		achadas := 0
		for _, o := range acharOcorrencias(saida, canonica) {
			ok := valem[o.Grafia]
			for g := range valem {
				ok = ok || (o.Abre && comInicialMaiuscula(g) == o.Grafia)
			}
			if ok {
				achadas++
			}
		}
		if achadas < len(doEsperado) {
			return false
		}
	}
	return true
}

// auditar compara `saida` (o texto que o modelo devolveu, ou o que a guarda
// aceitou) com o caso e diz que falhas críticas e leves ela tem.
func auditar(c Caso, saida string) Auditoria {
	pe, ps, pi := palavrasDe(c.Esperado), palavrasDe(saida), palavrasDe(c.Entrada)
	pares := alinharPalavras(normas(pe), normas(ps))
	omitidas := naoCasadas(len(pe), pares, true)
	extras := naoCasadas(len(ps), pares, false)

	falhas := map[TipoFalha]bool{}
	var a Auditoria

	// A correção de grafia em que o modelo e o esperado discordam é leve: as
	// palavras dela saem das omitidas e das extras antes da conta abaixo.
	if o, e, ok := separarGrafia(pe, ps, pi, pares, omitidas, extras); ok {
		omitidas, extras, a.Grafia = o, e, true
	}

	// Números: os de dígito são os do esperado ou os da entrada (a numeração
	// de item ditada em dígito pode virar marcador ou ficar), e os por extenso
	// também.
	numS := numerosDe(saida)
	if !mesmaSequencia(numS, numerosDe(c.Esperado)) && !mesmaSequencia(numS, numerosDe(c.Entrada)) {
		falhas[FalhaNumero] = true
	}
	extS := numeraisDe(ps)
	if !mesmaSequencia(extS, numeraisDe(pe)) && !mesmaSequencia(extS, numeraisDe(pi)) {
		falhas[FalhaNumero] = true
	}

	// Siglas, termos e nomes. Nome só é exigido se já era nome na ENTRADA: a
	// maiúscula que só o esperado põe ("a debora" -> "A Debora") é organização,
	// e a saída que a deixa de fora só tem uma alteração leve (Caixa).
	if !mesmoConjunto(siglasDe(ps), siglasDe(pe)) || !termosBatem(c.Termos, c.Entrada, c.Esperado, saida) {
		falhas[FalhaSiglaTermo] = true
	}
	contaS := contagem(brutas(ps))
	nomesDaEntrada := nomesDe(pi)
	for nome, n := range nomesDaEntrada {
		achados := contaS[nome]
		if minusculaLiberada(nome) {
			achados += contaS[strings.ToLower(nome)]
		}
		if achados < n {
			falhas[FalhaSiglaTermo] = true
		}
	}
	for nome, n := range nomesDe(pe) {
		if contaS[nome] < n && contaS[nome] >= nomesDaEntrada[nome] {
			a.Caixa = true
		}
	}

	// Palavras que a entrada tinha e o esperado tirou (hesitação, "vírgula"
	// falada): se o modelo as manteve, é sobra, não acréscimo.
	disponivel := contagem(normas(pi))
	for _, w := range normas(pe) {
		if disponivel[w] > 0 {
			disponivel[w]--
		}
	}

	for _, j := range extras {
		w := ps[j]
		switch {
		case ehNumerica(w.Norm), ehSiglaBruta(w.Bruta):
			// já contado acima
		case disponivel[w.Norm] > 0:
			disponivel[w.Norm]--
			a.Sobra = true
		default:
			falhas[FalhaAcrescimo] = true
		}
	}
	for _, i := range omitidas {
		w := pe[i]
		switch {
		case ehNumerica(w.Norm), ehSiglaBruta(w.Bruta):
			// já contado acima
		case palavrasDeSentido[w.Norm]:
			falhas[FalhaSentido] = true
		case faladasQueViramSinal[w.Norm] && vizinhaNumerica(pe, i):
			falhas[FalhaNumero] = true
		default:
			a.Omissao = true
		}
	}

	switch c.Categoria {
	case CatPergunta:
		if falhas[FalhaAcrescimo] {
			delete(falhas, FalhaAcrescimo)
			falhas[FalhaResposta] = true
		}
	case CatInjecao:
		if falhas[FalhaAcrescimo] || falhas[FalhaSentido] || a.Omissao {
			delete(falhas, FalhaAcrescimo)
			falhas[FalhaInjecao] = true
		}
	}

	for _, t := range tiposDeFalha {
		if falhas[t] {
			a.Criticas = append(a.Criticas, t)
		}
	}
	return a
}

// minusculaLiberada diz se o "nome" da entrada pode aparecer em minúscula na
// saída sem ser falha: a palavra comum que o Whisper escreveu com maiúscula
// no meio da frase ("Base", "Pesquisa"), que não é nome de pessoa nem nome
// que o VERO conhece (ADR-016, emenda de 19-out-2026). Escrita aqui, à parte
// do resolver, com as mesmas consultas ao dicionário: a política é a que o
// Mauro decidiu, e a auditoria não pode ser mais estrita que ela.
func minusculaLiberada(nome string) bool {
	min := strings.ToLower(nome)
	return min != nome && !ortografia.EhNomeDePessoa(nome) &&
		ortografia.ExisteComum(min) && !ortografia.ConheceComoNome(nome)
}

func brutas(ps []palavra) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Bruta
	}
	return out
}

// vizinhaNumerica diz se a palavra i do esperado tem um número por vizinho
// (antes ou depois): "dois vírgula cinco", "dois pontos".
func vizinhaNumerica(ps []palavra, i int) bool {
	return (i > 0 && ehNumerica(ps[i-1].Norm)) || (i+1 < len(ps) && ehNumerica(ps[i+1].Norm))
}

// textoDoModelo tira o final_text do JSON que o modelo devolveu. ok=false
// quando não é o JSON esperado.
func textoDoModelo(bruto []byte) (texto string, ok bool) {
	var r struct {
		FinalText *string `json:"final_text"`
	}
	if err := json.Unmarshal(bruto, &r); err != nil || r.FinalText == nil {
		return "", false
	}
	return strings.TrimSpace(*r.FinalText), true
}

// formaDaResposta descreve o formato do JSON que o modelo devolveu, sem o
// texto: o relatório conta as formas para achar respostas estranhas.
func formaDaResposta(bruto []byte) string {
	t := strings.TrimSpace(string(bruto))
	if strings.HasPrefix(t, "```") {
		return FormaCercaDeCodigo
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(t), &m); err != nil {
		return FormaNaoJSON
	}
	raw, tem := m["final_text"]
	if !tem {
		return FormaSemFinalText
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || string(bytes.TrimSpace(raw)) == "null" {
		return FormaFinalTextNaoString
	}
	switch {
	case strings.TrimSpace(s) == "":
		return FormaFinalTextVazio
	case strings.ContainsRune(s, '\r'):
		return FormaComCR
	case len(m) > 1:
		return FormaCamposExtras
	}
	return FormaLimpa
}

// As formas de resposta que o relatório distingue.
const (
	FormaLimpa              = "limpa"
	FormaCamposExtras       = "campos_extras"
	FormaNaoJSON            = "nao_json"
	FormaSemFinalText       = "sem_final_text"
	FormaFinalTextNaoString = "final_text_nao_string"
	FormaFinalTextVazio     = "final_text_vazio"
	FormaComCR              = "final_text_com_cr"
	FormaCercaDeCodigo      = "cerca_de_codigo"
)
