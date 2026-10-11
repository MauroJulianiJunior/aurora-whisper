package smartconta

import (
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// DicaLigadaPorPadrao diz se a dica do aprendido vem ligada nas preferências do
// celular. Vem DESLIGADA até a C8 medir (ADR-021, escolha menor 8): o ganho de
// 61,2% para 79,6% de Entity Accuracy foi medido no initial prompt do
// whisper.cpp local, e não nos modelos da OpenAI nem no whisper da Groq, e a
// própria OpenAI avisa que o prompt pode induzir o termo. A função Dica existe e
// é testada; quem decide ligá-la é a pessoa, nas preferências.
const DicaLigadaPorPadrao = false

// OrcamentoDaDica é o tamanho máximo da dica, em runas: o mesmo orçamento do
// prompt de vocabulário do PC (dictionary.OrcamentoRunasPadrao, 140), que
// protege a latência do decodificador de crescer com o aprendido.
const OrcamentoDaDica = dictionary.OrcamentoRunasPadrao

// Dica monta a dica que vai ao provedor de transcrição como o prompt: os termos
// distintos do aprendido da conta, do mais recente ao mais antigo, até
// OrcamentoDaDica runas, separados por vírgula.
//
// pares vêm na ordem do ListLearned (do mais recente ao mais antigo), e o
// primeiro a citar um termo fixa a grafia dele. Ficam de fora os pares presos a
// um domínio ou restritos (a dica não pode levar um termo que só vale num
// programa, e o PWA não sabe em que programa o texto vai entrar) e os
// desfeitos. O corte é em termo inteiro e segue a regra do PC
// (dictionary.Vocabulario): o termo que não cabe no que sobra fica de fora, e
// os seguintes, mais curtos, ainda podem entrar.
//
// Não é o prompt do PC. No PC o termo aprendido é AliasOnly e fica fora do
// prompt (ADR-017; ADR-020, decisão 3); na nuvem não há outra fonte de dica, e
// por isso ela diverge (ADR-021, decisão E). Os termos podem ser nomes de
// clientes ou de terceiros e vão ao provedor a cada ditado, qualquer que seja o
// conteúdo: a folha do aceite e o dossiê dizem isso.
//
// Não loga nada: os termos são texto da pessoa.
func Dica(pares []dictionary.ParDaNuvem) string {
	vistos := map[string]bool{}
	var entradas []dictionary.DictionaryEntry
	for _, p := range pares {
		if p.Desfeito || p.Dominio != "" || p.Restrito {
			continue
		}
		termo := p.Termo
		chave := chaveNoDicionario(termo)
		if chave == "" || vistos[chave] {
			continue
		}
		vistos[chave] = true
		entradas = append(entradas, dictionary.DictionaryEntry{CanonicalForm: termo, Active: true})
	}
	// Vocabulario ordena por prioridade decrescente: a ordem em que os termos
	// chegaram (o mais recente primeiro) vira a prioridade, e o corte é o do PC.
	for i := range entradas {
		entradas[i].Priority = len(entradas) - i
	}
	return dictionary.Vocabulario(entradas, OrcamentoDaDica)
}
