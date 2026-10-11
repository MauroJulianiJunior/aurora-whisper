package dictionary

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Vocabulario monta a lista de termos que vai enviesar o decodificador do
// ASR (asr.ASROptions.Vocabulary), a partir das entradas do dicionário
// pessoal, LIMITADA por um orçamento de tamanho.
//
// # Por que limitado, e não "todos os termos"
//
// Porque cada termo custa latência. Medido em 26-set-2026 na máquina de
// referência, sobre as 46 amostras do dataset:
//
//	13 termos  ->  +40 ms   e Entity Accuracy de 61,2% para 73,5%
//	36 termos  -> +146 ms   e Entity Accuracy de 61,2% para 79,6%
//
// O termo 14º em diante rende 0,13 ponto por milissegundo; os 13
// primeiros rendem 0,31. O retorno é decrescente e o custo não é — e o
// p50 do produto já estava 13% acima da meta ANTES disto
// (docs/benchmarks/asr-decisao-2026-09-18.md).
//
// Um dicionário pessoal cresce sem limite com o uso. Sem orçamento, o
// ditado ficaria mais lento a cada termo aprendido, e ninguém ligaria uma
// coisa à outra — a degradação seria gradual e sem causa visível.
//
// # Como escolhe quem entra
//
// Primeiro as dicas que a pessoa ligou no "Adicionar termo" (EDicaDaPessoa,
// ADR-020), na ordem em que estão. Depois o resto, por Priority
// decrescente, que é o mesmo critério que Search usa para desempatar
// candidatos (repository_memory.go): o que importa mais para quem usa
// entra primeiro. Empate de prioridade resolve por ordem alfabética da
// forma canônica — arbitrário, mas DETERMINÍSTICO, para o mesmo dicionário
// produzir sempre o mesmo prompt e a medição ser reproduzível.
//
// Quem disputa o prompt é a regra de DisputaOPrompt: fica fora a entrada
// inativa, a AliasOnly (que o painel criou ao aprender uma variante) e o
// termo adicionado sem a dica.
//
// # O orçamento é em runas, não em termos
//
// Porque o custo do decodificador acompanha o TAMANHO do prompt, não a
// contagem de itens: "Metalúrgica Toffanetti" custa o que três siglas
// curtas custam. maxRunes <= 0 devolve vazio — "sem orçamento" é
// "sem vocabulário", nunca "vocabulário ilimitado".
//
// O corte acontece em termo INTEIRO: meio termo enviesaria o
// decodificador para uma palavra que não existe.
func Vocabulario(entradas []DictionaryEntry, maxRunes int) string {
	termos := VocabularioTermos(entradas, maxRunes)
	nomes := make([]string, 0, len(termos))
	for _, e := range termos {
		nomes = append(nomes, strings.TrimSpace(e.CanonicalForm))
	}
	return strings.Join(nomes, separadorVocabulario)
}

const separadorVocabulario = ", "

// VocabularioTermos é Vocabulario devolvendo as entradas ESCOLHIDAS em vez
// da string montada — mesma seleção, mesma ordem, mesmo corte.
//
// Existe porque o corte era INVISÍVEL. O Agent logava "termos=44" ao lado
// de "vocabulario_runas=140" e a linha se lia como "mandei 44 termos",
// quando foram 17: o resto não coube no orçamento e sumiu sem deixar
// rastro. Um termo do dicionário pessoal que nunca chega ao ASR é
// indistinguível, no log, de um termo que chegou e o modelo ignorou — e
// são problemas opostos, com correções opostas.
//
// Descobrir isso custou uma medição em 28-set-2026. Devia ter custado uma
// linha de log.
func VocabularioTermos(entradas []DictionaryEntry, maxRunes int) []DictionaryEntry {
	if maxRunes <= 0 {
		return nil
	}

	// As dicas da pessoa vêm primeiro, na ordem em que estão (a do arquivo, que
	// é a ordem em que ela as adicionou), e o resto depois, como sempre. Como o
	// painel recusa a dica que não cabe (ErrSemEspaco), cada dica ligada está
	// de fato no prompt, e a conta "sobram N de 140" que a tela mostra é a
	// verdade. Sem esta ordem, o desempate alfabético decidiria qual dica dela
	// sai quando duas disputam o último lugar.
	dicas := make([]DictionaryEntry, 0, len(entradas))
	resto := make([]DictionaryEntry, 0, len(entradas))
	for _, e := range entradas {
		switch {
		case !DisputaOPrompt(e):
		case EDicaDaPessoa(e):
			dicas = append(dicas, e)
		default:
			resto = append(resto, e)
		}
	}
	sort.SliceStable(resto, func(i, j int) bool {
		if resto[i].Priority != resto[j].Priority {
			return resto[i].Priority > resto[j].Priority
		}
		return resto[i].CanonicalForm < resto[j].CanonicalForm
	})
	ativas := append(dicas, resto...)

	escolhidas := make([]DictionaryEntry, 0, len(ativas))
	usado := 0
	for _, e := range ativas {
		custo := utf8.RuneCountInString(strings.TrimSpace(e.CanonicalForm))
		if len(escolhidas) > 0 {
			custo += utf8.RuneCountInString(separadorVocabulario)
		}
		if usado+custo > maxRunes {
			// Não para: um termo longo demais não pode impedir que os
			// seguintes, mais curtos, entrem no que ainda cabe.
			continue
		}
		escolhidas = append(escolhidas, e)
		usado += custo
	}
	return escolhidas
}

// DisputaOPrompt diz se a entrada disputa um lugar no prompt do ASR. É a regra
// única que Vocabulario e o log da subida do Agent (a conta das "ativas", que
// diz quantas o orçamento cortou) usam, para os dois não divergirem.
//
// Fica fora:
//   - a entrada inativa: desativar um termo tem de tirá-lo também do viés do
//     ASR, senão "desativado" não significaria nada;
//   - a entrada AliasOnly, que o painel criou ao aprender uma variante: o
//     termo que o painel inventou não vai ao ASR por acaso. Num dicionário
//     que cabe no orçamento, um termo de prioridade 50 entraria no prompt;
//   - o termo que a pessoa adicionou SEM a dica (Adicionado, abaixo de
//     PrioridadeDica): ela disse que não o quer no prompt, e "sem dica" tem de
//     valer também num dicionário com espaço sobrando (ADR-020). Um Agent
//     anterior a esta regra só ordena por prioridade, e o termo de 50 pode
//     entrar no prompt dele: uma degradação inofensiva;
//   - a entrada sem forma canônica.
func DisputaOPrompt(e DictionaryEntry) bool {
	if !e.Active || e.AliasOnly || strings.TrimSpace(e.CanonicalForm) == "" {
		return false
	}
	return !e.Adicionado || e.Priority >= PrioridadeDica
}

// EDicaDaPessoa diz se a entrada é uma dica que a pessoa ligou no "Adicionar
// termo" (ADR-020): um termo adicionado, ativo, com prioridade PrioridadeDica
// ou mais. As dicas dela entram no prompt antes de tudo (VocabularioTermos).
func EDicaDaPessoa(e DictionaryEntry) bool {
	return e.Adicionado && DisputaOPrompt(e)
}

// runasDasDicas é quanto do orçamento as dicas da pessoa ocupam: as runas de
// cada uma, mais o separador entre elas. Como elas vêm primeiro no prompt, é
// exatamente o que elas gastam lá.
func runasDasDicas(entradas []DictionaryEntry) int {
	usadas, n := 0, 0
	for _, e := range entradas {
		if !EDicaDaPessoa(e) {
			continue
		}
		if n > 0 {
			usadas += utf8.RuneCountInString(separadorVocabulario)
		}
		usadas += utf8.RuneCountInString(strings.TrimSpace(e.CanonicalForm))
		n++
	}
	return usadas
}

// OrcamentoRunasPadrao é o teto de tamanho do prompt de vocabulário,
// escolhido para ficar perto do prompt curto medido em 26-set-2026 —
// cerca de +40 ms na máquina de referência, contra +146 ms do longo.
//
// # A medição está em TERMOS e o orçamento cobra em RUNAS
//
// Os dois pontos medidos são "13 termos" e "36 termos". O orçamento não
// conta termos, conta runas — e quantos termos cabem em 140 runas depende
// inteiramente de QUAIS termos, ou seja, do dicionário de cada pessoa.
// Conferido em 28-set-2026: 140 runas davam 11 termos no dicionário de
// 26-set e dão 17 no de hoje. A constante não reproduz "o prompt de 13
// termos" — em nenhum estado do repositório ela reproduziu.
//
// Isso não invalida o teto, que é o que protege a latência de crescer com
// o dicionário. Invalida a equivalência: não dá para dizer "140 runas =
// +40 ms" sem uma medição feita em runas, e essa medição não existe.
// Converter os pontos existentes dá por volta de 0,5 ms/runa nas duas
// reconstruções, mas NENHUMA das duas foi cronometrada — é aritmética
// sobre uma medição de outra unidade, não dado.
//
// É um número afinado numa máquina só, igual ao `-t 12` do ASR
// (docs/benchmarks/asr-decisao-2026-09-18.md, "afinado NESTA máquina"), e
// carrega a mesma ressalva: em hardware diferente o custo por runa muda e
// este valor precisa ser remedido, não herdado.
const OrcamentoRunasPadrao = 140
