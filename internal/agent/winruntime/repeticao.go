package winruntime

import (
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/repeticao"
)

// guardaDeRepeticao tira do texto do ASR o laço de repetição (a mesma frase
// várias vezes seguidas no lugar da fala), deixando uma ocorrência, antes de
// o texto ir à resolução. Roda na goroutine de Run.
//
// No incidente de 01-out-2026 (docs/defeito-ditado-longo.md), a frase "e
// como cada um deles vai ser utilizado na hora de récu," entrou doze vezes
// no campo. A regra do que conta como laço, e do que não conta ("não, não,
// não", listas, números), está em internal/asr/repeticao.
//
// O aviso vai a dois lugares, e o texto a nenhum: o log, só com contagens; e
// a pílula do sucesso desta tentativa, que vira "Removi uma repetição.
// Confira o texto." (overlay.FaseDoSucesso, por rt.repeticaoEm), porque o
// laço costuma tomar o lugar de parte da fala e quem ditou precisa
// conferir.
//
// # O texto original fica no histórico
//
// A resolução, a inserção e o final_text recebem o texto sem o laço. O
// raw_text recebe o texto do ASR como chegou, com o laço
// (brutoParaOHistorico): raw_text é o texto do ASR sem modificação
// (06-api-contract, resolver.ResolveResult.RawText), e é o único lugar de
// onde um falso positivo da guarda — repetição dita de propósito — pode ser
// recuperado. O editor do painel mostra a diferença entre o bruto e o
// final, e com ela as cópias que saíram.
//
// O laço de aprendizado lê o bruto para achar a forma que o ASR escreveu (o
// "de" de uma sugestão). As cópias a mais não criam forma nova: o final se
// alinha com uma delas. Um bruto grande demais para alinhar só faz a
// edição sair sem sugestão (learning, maxCelulas), como qualquer ditado
// enorme.
func (rt *Runtime) guardaDeRepeticao(e dictation.TranscribeResultEvent) dictation.TranscribeResultEvent {
	if e.Reason != dictation.ErrorReasonNone {
		return e
	}
	r := repeticao.Colapsar(e.Result.Text)
	if !r.Colapsou() {
		return e
	}
	maiorUnidade, repeticoes := 0, 0
	for _, c := range r.Colapsos {
		maiorUnidade = max(maiorUnidade, c.Palavras)
		repeticoes += c.Repeticoes - 1
	}
	rt.log.Warn("winruntime: laço de repetição do ASR removido",
		"attempt", e.Attempt,
		"lacos", len(r.Colapsos),
		"copias_removidas", repeticoes,
		"palavras_da_unidade", maiorUnidade,
		"palavras_removidas", r.PalavrasRemovidas,
		"runas_antes", len([]rune(e.Result.Text)),
		"runas_depois", len([]rune(r.Texto)))
	rt.repeticaoEm = e.Attempt
	rt.originalDoASR.attempt, rt.originalDoASR.texto = e.Attempt, e.Result.Text
	e.Result.Text = r.Texto
	return e
}

// brutoParaOHistorico devolve o raw_text da tentativa: o texto do ASR como
// chegou, se a guarda tirou um laço dele, ou texto (o que seguiu para a
// resolução), se não tirou. Consome o original: ele só vale uma vez, e
// nunca para outra tentativa.
func (rt *Runtime) brutoParaOHistorico(attempt dictation.AttemptID, texto string) string {
	o := rt.originalDoASR
	if o.attempt == dictation.NoAttempt || o.attempt != attempt {
		return texto
	}
	rt.originalDoASR.attempt, rt.originalDoASR.texto = dictation.NoAttempt, ""
	return o.texto
}
