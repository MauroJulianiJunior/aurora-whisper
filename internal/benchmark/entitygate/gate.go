// Package entitygate mede o portão que o Sprint 5 existe para bater:
// "Entity Accuracy ≥95% APÓS dictionary" (docs/ai-spec/19-eval-plan.md,
// "Gates local"), junto com o portão que puxa contra ele, "False
// Correction Smart ≤1%" (docs/ai-spec/02-prd.md, seção 7).
//
// # Por que um pacote separado do harness
//
// internal/benchmark/harness existe para COMPARAR CANDIDATOS de ASR —
// trabalho concluído em 18-set-2026 (docs/benchmarks/asr-decisao-*.md) e
// que não deve ser mexido para afinar dicionário. Ele mede o texto BRUTO
// do candidato e diz isso explicitamente em metrics.EntityAccuracy.
//
// Afinar dicionário é outra medição, com outra cadência: roda muitas
// vezes seguidas enquanto os alias são ajustados, e precisa do DIFF por
// entidade, não de uma média. Este pacote é a lógica pura disso; quem
// liga ASR e resolver de verdade é cmd/entitybench.
//
// # Os dois números, sempre juntos
//
// Medir só o ganho é como medir a onda pelo espalhamento e não olhar a
// imagem: sobe muito e esconde o que importa. Um dicionário que
// "conserta" 20 entidades e quebra 5 palavras que já estavam certas é
// pior que nenhum dicionário, porque o erro novo aparece em texto que a
// pessoa não estava conferindo.
//
// Por isso Medir devolve ganho E regressão na mesma estrutura, e nunca
// um sem o outro.
package entitygate

import (
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// StatusEntidade é o que aconteceu com UMA entidade esperada depois que
// o resolver passou pelo texto.
type StatusEntidade int

const (
	// JaEstavaCerta: o ASR já tinha acertado e o resolver não estragou.
	// É o caso mais comum e o que a regressão protege.
	JaEstavaCerta StatusEntidade = iota
	// Corrigida: o ASR errou e o resolver consertou. É o ganho.
	Corrigida
	// ContinuaErrada: o ASR errou e o resolver não resolveu. Não é
	// regressão — é trabalho que falta.
	ContinuaErrada
	// Regrediu: o ASR tinha ACERTADO e o resolver estragou. É o pior
	// desfecho possível e o que o portão de correção falsa proíbe.
	Regrediu
)

func (s StatusEntidade) String() string {
	switch s {
	case JaEstavaCerta:
		return "ja_estava_certa"
	case Corrigida:
		return "corrigida"
	case ContinuaErrada:
		return "continua_errada"
	case Regrediu:
		return "regrediu"
	default:
		return "desconhecido"
	}
}

// Ocorrencia é o destino de uma entidade esperada numa amostra, com o
// texto dos dois lados para o diff ser legível por quem vai ajustar o
// dicionário.
type Ocorrencia struct {
	AmostraID string
	Entidade  string
	Status    StatusEntidade
	// TextoBruto e TextoResolvido são guardados só para o relatório em
	// tela. Este pacote não persiste nada (AC-028 vale para áudio, mas o
	// espírito de "efêmero" vale aqui: é uma ferramenta de bancada).
	TextoBruto     string
	TextoResolvido string
}

// Resultado é o agregado de uma execução inteira.
type Resultado struct {
	// QualidadeBruta e QualidadeResolvida medem o texto INTEIRO — ver
	// Qualidade.
	QualidadeBruta, QualidadeResolvida Qualidade

	// Bruto e Resolvido usam a MESMA função de pontuação do harness
	// (metrics.EntityAccuracy), para os números serem comparáveis com os
	// relatórios de benchmark já publicados.
	Bruto     metrics.MatchResult
	Resolvido metrics.MatchResult

	Ganhos     int // entidades que o resolver consertou
	Regressoes int // entidades que o resolver estragou

	Ocorrencias []Ocorrencia

	// PiorouTexto lista as amostras em que o texto RESOLVIDO ficou mais
	// longe da referência que o BRUTO, medido por WER.
	//
	// # Por que existe
	//
	// TaxaCorrecaoFalsa só enxerga as palavras marcadas como entidade:
	// numerador e denominador saem de a.Entidades. Se o resolver trocar
	// uma palavra COMUM — "hoje", "sexta", "whispering" —, esse número
	// não se move. Quem vê é o WER, e o WER não entra em PassouNoPortao.
	//
	// Em 28-set-2026 uma rodada deu CER bruto 0,044 e resolvido 0,083 —
	// quase o dobro — com o portão marcando PASSOU. Parte disso pode ser
	// legítima (referência escrita diferente da forma canônica); a
	// agregação não diz qual parte. Esta lista diz, amostra por amostra.
	PiorouTexto []TextoPiorado

	// DistanciaDaIntencao é o WER do texto RESOLVIDO contra o que a
	// pessoa queria (Amostra.alvo). QualidadeResolvida mede o mesmo texto
	// contra a fala literal, que é o número certo para comparar com o
	// bruto e com os benchmarks antigos; este é o número certo para
	// saber quão longe o produto ficou da intenção. Só WER é preenchido.
	DistanciaDaIntencao Qualidade
}

// TextoPiorado é uma amostra em que o resolver aumentou o WER.
type TextoPiorado struct {
	ID                     string
	WERBruto, WERResolvido float64
	// Alvo é o texto contra o qual as duas foram medidas: a intenção
	// quando a amostra tem uma, a fala literal quando não.
	Alvo             string
	Bruto, Resolvido string
}

// TaxaCorrecaoFalsa é a fração de entidades JÁ CORRETAS que o resolver
// estragou — o portão "False Correction Smart ≤1%".
//
// O denominador é quantas estavam certas ANTES, não o total: o risco que
// o portão mede é "o resolver estraga o que já funcionava", e entidade
// que já estava errada não pode ser estragada.
//
// Zero entidades certas antes devolve 0 — não há denominador, e devolver
// 1.0 ("100% de correção falsa") seria pior que dizer nada.
func (r Resultado) TaxaCorrecaoFalsa() float64 {
	certasAntes := r.Bruto.Correct
	if certasAntes == 0 {
		return 0
	}
	return float64(r.Regressoes) / float64(certasAntes)
}

// PassouNoPortao reporta se os DOIS portões do eval-plan foram batidos.
// Nunca um sem o outro: 100% de Entity Accuracy com 5% de correção falsa
// não passa, e é exatamente o resultado que um dicionário agressivo
// produz.
func (r Resultado) PassouNoPortao() bool {
	return r.Resolvido.Score() >= MetaEntityAccuracy &&
		r.TaxaCorrecaoFalsa() <= MetaCorrecaoFalsa &&
		r.NenhumaAmostraAfastada()
}

// NenhumaAmostraAfastada é o terceiro critério do portão, desde
// 28-set-2026: o resolver não pode levar NENHUMA amostra para mais longe
// do que a pessoa queria.
//
// # Por que existe
//
// TaxaCorrecaoFalsa só enxerga palavras marcadas como entidade. Uma
// palavra comum trocada pelo resolver — "hoje" virando "ontem", uma
// preposição engolida — não a move, e passava pelo portão.
//
// # Por que zero, e não uma porcentagem
//
// A meta foi MEDIDA antes de ser fixada: com o dicionário real e o
// código de 28-set, nenhuma das 46 amostras se afasta da intenção. E a
// comparação é entre bruto e resolvido da MESMA execução do ASR, então a
// variação do ASR entre rodadas não entra — só o efeito do resolver. Uma
// amostra afastada é um defeito do resolver ou uma referência que
// discorda da forma canônica; nos dois casos, a correção é consertar, não
// tolerar.
//
// Antes das referências de intenção isto seria impossível de exigir: a
// autocorreção de fala, ACERTANDO, aparecia como piora contra a fala
// literal.
func (r Resultado) NenhumaAmostraAfastada() bool { return len(r.PiorouTexto) == 0 }

// As duas metas, de docs/ai-spec/19-eval-plan.md e 02-prd.md.
const (
	MetaEntityAccuracy = 0.95
	MetaCorrecaoFalsa  = 0.01
)

// Qualidade resume o erro de transcrição do texto INTEIRO, não só das
// entidades.
//
// Existe porque medir só entidade pode esconder um estrago: o
// vocabulário passado ao ASR (asr.ASROptions.Vocabulary) enviesa o
// decodificador para os termos do dicionário, e nada garante que isso
// não piore o resto da frase. Em 28-set-2026 Mauro ditou "o vencimento é
// no dia doze" e recebeu "Ovencimento ano dia 12" — entidade nenhuma
// envolvida.
//
// WERFalhou/CERFalhou contam amostras cujo cálculo não tinha resposta
// honesta (referência vazia após normalização) e que por isso ficaram
// FORA da média — nunca um 0,0 fingindo ser medição, a mesma regra de
// harness.ModeReport.
type Qualidade struct {
	WER, CER             float64
	Amostras             int
	WERFalhou, CERFalhou int
}

// Amostra é o mínimo que Medir precisa de cada item do dataset.
type Amostra struct {
	ID string
	// Entidades esperadas na referência (dataset.Sample.Entities).
	Entidades []string
	// Bruto é o texto como saiu do ASR; Resolvido é o mesmo texto depois
	// do resolver. Iguais significa que o resolver não mexeu.
	Bruto     string
	Resolvido string
	// Referencia é o que a pessoa realmente disse. Vazio desliga o
	// cálculo de WER/CER desta amostra.
	Referencia string
	// Intencao é o que a pessoa QUERIA produzir, quando difere do que
	// disse (dataset.Sample.Intended). Vazio significa "igual a
	// Referencia".
	//
	// A distinção só importa na autocorreção de fala: o produto remove a
	// parte retratada, e medir isso contra a fala literal puniria
	// exatamente o acerto.
	Intencao string
}

// alvo é o texto contra o qual o RESOLVIDO deve ser julgado: a intenção
// quando existe, a fala literal quando não.
func (a Amostra) alvo() string {
	if a.Intencao != "" {
		return a.Intencao
	}
	return a.Referencia
}

// Medir compara, entidade por entidade, o texto bruto do ASR contra o
// texto depois do resolver.
//
// Entidade esperada REPETIDA na mesma amostra é tratada como ocorrências
// independentes, igual a metrics.EntityAccuracy faz — uma entidade
// esperada 2x e presente 1x conta 1 acerto de 2, não 2 de 2.
func Medir(amostras []Amostra) Resultado {
	var r Resultado
	var somaWERBruto, somaCERBruto, somaWERResolvido, somaCERResolvido float64
	var somaIntencao float64
	for _, a := range amostras {
		if strings.TrimSpace(a.Referencia) != "" {
			acumularQualidade(&r.QualidadeBruta, &somaWERBruto, &somaCERBruto, a.Referencia, a.Bruto)
			acumularQualidade(&r.QualidadeResolvida, &somaWERResolvido, &somaCERResolvido, a.Referencia, a.Resolvido)

			// Contra a INTENÇÃO, não contra a fala literal: a pergunta é
			// se o resolver levou o texto para longe do que a pessoa
			// queria. Contra a fala literal, a autocorreção que acerta
			// apareceria aqui como piora — e apareceu, em 28-set-2026,
			// nas quatro amostras corr-*.
			//
			// Bruto e resolvido vêm da MESMA execução do ASR, então a
			// variação do ASR entre rodadas não entra: o que se isola é
			// o efeito do resolver.
			alvo := a.alvo()
			werB, errB := metrics.WER(alvo, a.Bruto)
			werR, errR := metrics.WER(alvo, a.Resolvido)
			if errB == nil && errR == nil && werR > werB {
				r.PiorouTexto = append(r.PiorouTexto, TextoPiorado{
					ID: a.ID, WERBruto: werB, WERResolvido: werR,
					Alvo: alvo, Bruto: a.Bruto, Resolvido: a.Resolvido,
				})
			}
			if errR == nil {
				r.DistanciaDaIntencao.Amostras++
				somaIntencao += werR
			}
		}
		brutoAmostra := metrics.EntityAccuracy(a.Entidades, a.Bruto)
		resolvidoAmostra := metrics.EntityAccuracy(a.Entidades, a.Resolvido)
		r.Bruto.Correct += brutoAmostra.Correct
		r.Bruto.Total += brutoAmostra.Total
		r.Resolvido.Correct += resolvidoAmostra.Correct
		r.Resolvido.Total += resolvidoAmostra.Total

		for _, e := range entidadesUteis(a.Entidades) {
			antes := metrics.EntityAccuracy([]string{e}, a.Bruto).Correct > 0
			depois := metrics.EntityAccuracy([]string{e}, a.Resolvido).Correct > 0

			oc := Ocorrencia{
				AmostraID: a.ID, Entidade: e,
				TextoBruto: a.Bruto, TextoResolvido: a.Resolvido,
			}
			switch {
			case antes && depois:
				oc.Status = JaEstavaCerta
			case !antes && depois:
				oc.Status = Corrigida
				r.Ganhos++
			case antes && !depois:
				oc.Status = Regrediu
				r.Regressoes++
			default:
				oc.Status = ContinuaErrada
			}
			r.Ocorrencias = append(r.Ocorrencias, oc)
		}
	}
	finalizar(&r.QualidadeBruta, somaWERBruto, somaCERBruto)
	finalizar(&r.QualidadeResolvida, somaWERResolvido, somaCERResolvido)
	if r.DistanciaDaIntencao.Amostras > 0 {
		r.DistanciaDaIntencao.WER = somaIntencao / float64(r.DistanciaDaIntencao.Amostras)
	}
	return r
}

// acumularQualidade soma WER/CER de uma amostra, contando separadamente
// as que não puderam ser calculadas.
func acumularQualidade(q *Qualidade, somaWER, somaCER *float64, referencia, hipotese string) {
	q.Amostras++
	if wer, err := metrics.WER(referencia, hipotese); err == nil {
		*somaWER += wer
	} else {
		q.WERFalhou++
	}
	if cer, err := metrics.CER(referencia, hipotese); err == nil {
		*somaCER += cer
	} else {
		q.CERFalhou++
	}
}

func finalizar(q *Qualidade, somaWER, somaCER float64) {
	if n := q.Amostras - q.WERFalhou; n > 0 {
		q.WER = somaWER / float64(n)
	}
	if n := q.Amostras - q.CERFalhou; n > 0 {
		q.CER = somaCER / float64(n)
	}
}

// entidadesUteis descarta entidade vazia ou só de espaço, do mesmo jeito
// que metrics.EntityAccuracy faz — se elas entrassem aqui e não lá, as
// contagens de Ocorrencias e de MatchResult sairiam de sincronia e o
// relatório mostraria dois totais diferentes para a mesma coisa.
func entidadesUteis(es []string) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}
