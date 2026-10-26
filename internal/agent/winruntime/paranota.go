package winruntime

import (
	"context"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/paranota"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Este arquivo é o lado do runtime do gatilho "ditar para nota" (ADR-022,
// "Acrescentar por voz sem abrir a nota", fase N9).
//
// # O que muda, e o que não muda
//
// A sessão do gatilho da nota é um ditado como outro qualquer até o fim da
// resolução: o mesmo limiar, o mesmo bipe, a mesma pílula de gravação, o mesmo
// ASR, o mesmo Smart e o mesmo Polish. O núcleo nem sabe dela. Muda só o
// destino do texto: no InsertEffect, em vez de ir à thread de inserção (sem
// SendInput, sem clipboard), o texto vai para uma nota (paranota.Gravar), e a
// resposta volta ao núcleo como a de uma inserção. A pílula do desfecho e a
// linha do histórico são trocadas aqui, pela tentativa.
//
// O Ctrl+Win sem Alt não passa por nada disto: a marca da sessão é falsa e o
// InsertEffect segue para enfileirarInsercao, como sempre.

// gravarNotaPorVoz é o que o runtime precisa das notas: levar o texto ditado a
// uma nota (paranota.Gravar, com o repositório e o dono já ligados).
type gravarNotaPorVoz func(ctx context.Context, ditadoID, texto string) (paranota.Resultado, error)

// prazoDaNota é o teto da gravação na nota. É SQLite local, na goroutine de
// Run: milissegundos. O teto só existe para um banco preso não segurar o laço.
const prazoDaNota = 2 * time.Second

// UsarNotas liga o destino do gatilho "ditar para nota". usuario é o dono das
// notas, o mesmo do histórico. Deve ser chamado antes de Run. Sem isto, a
// sessão do gatilho da nota é inserida no campo como um ditado comum (o hook
// continua marcando, e o runtime ignora a marca).
func (rt *Runtime) UsarNotas(repo paranota.Repositorio, usuario string) {
	rt.notas = func(ctx context.Context, ditadoID, texto string) (paranota.Resultado, error) {
		return paranota.Gravar(ctx, repo, usuario, ditadoID, texto)
	}
}

// sessaoParaNota é a marca do gatilho da nota de uma tentativa.
type sessaoParaNota struct {
	attempt dictation.AttemptID
	sim     bool
}

// notaDaTentativa é o que aconteceu com o texto da tentativa que foi para a
// nota, a caminho da pílula e do histórico.
type notaDaTentativa struct {
	attempt dictation.AttemptID
	// ditadoID é o id que a linha do histórico vai ter: a nota nova o guarda
	// em ditado_de_origem.
	ditadoID string
	res      paranota.Resultado
	falhou   bool
}

// marcarParaNota anota a marca do gatilho da nota que veio com o evento. No
// keydown, vale para a tentativa que ele começou; na soltura, a marca dela é a
// final (o pico da sessão, dictation.Gatilhos), e só sobe: uma soltura sem
// marca nunca desfaz a do keydown. Só na goroutine de Run.
func (rt *Runtime) marcarParaNota(attempt dictation.AttemptID, raw dictation.RawKeyEvent) {
	if attempt == dictation.NoAttempt {
		return
	}
	if rt.paraNota.attempt != attempt {
		rt.paraNota = sessaoParaNota{attempt: attempt}
	}
	if raw.ParaNota {
		rt.paraNota.sim = true
	}
}

// vaiParaNota diz se o texto da tentativa vai para uma nota: a sessão é do
// gatilho da nota, ele está ligado nas Configurações e as notas estão ligadas.
func (rt *Runtime) vaiParaNota(attempt dictation.AttemptID) bool {
	return rt.notas != nil && !rt.semGatilhoDaNota &&
		rt.paraNota.attempt == attempt && rt.paraNota.sim
}

// gravarNaNota atende o InsertEffect da sessão do gatilho da nota: grava o
// texto na nota e devolve a resposta que o núcleo espera de uma inserção.
// Falhar aqui é "a inserção falhou": o núcleo grava o ditado no histórico COM
// o texto, e à vista (recordHistory não o esconde), e a pílula diz onde ele
// está. Nada some calado.
//
// O log leva só números e a forma do desfecho — nunca o texto nem o título.
func (rt *Runtime) gravarNaNota(e dictation.InsertEffect) dictation.Event {
	pai := rt.ctxRun
	if pai == nil {
		pai = context.Background()
	}
	ctx, cancelar := context.WithTimeout(pai, prazoDaNota)
	defer cancelar()

	n := notaDaTentativa{attempt: e.Attempt, ditadoID: storage.NewID()}
	res, err := rt.notas(ctx, n.ditadoID, e.Text)
	n.res, n.falhou = res, err != nil
	rt.notaFeita = n

	if err != nil {
		rt.log.Warn("winruntime: não consegui gravar o ditado na nota; ele fica no histórico",
			"attempt", e.Attempt, "text_runes", len([]rune(e.Text)), "err", err)
		return dictation.InsertResultEvent{Attempt: e.Attempt, Reason: dictation.ErrorReasonInsertFailed}
	}
	rt.log.Info("winruntime: ditado gravado na nota",
		"attempt", e.Attempt, "nota_nova", res.Nova, "linhas", res.Linhas, "text_runes", len([]rune(e.Text)))
	return dictation.InsertResultEvent{Attempt: e.Attempt, Result: dictation.InsertResultDTO{Success: true}}
}

// FaseDaNota escolhe a pílula do desfecho de uma tentativa que foi para a nota,
// a partir da fase que o núcleo emitiu: o sucesso vira FaseNaNota com o texto
// ("Lista de compras +3") ou FaseNotaNova; a falha de "inserção" (a nota não
// foi gravada) vira FaseNotaNaoSalvou. Qualquer outra fase passa intacta, sem
// texto. Pura.
func FaseDaNota(fase dictation.OverlayPhase, res paranota.Resultado, falhou bool) (dictation.OverlayPhase, string) {
	switch {
	case fase == dictation.OverlaySuccess && !falhou && res.Nova:
		return overlay.FaseNotaNova, ""
	case fase == dictation.OverlaySuccess && !falhou:
		return overlay.FaseNaNota, res.Pilula()
	case fase == dictation.OverlayInsertFailed && falhou:
		return overlay.FaseNotaNaoSalvou, ""
	default:
		return fase, ""
	}
}

// mostradorComTexto é o que *overlay.Overlay oferece além de SetPhase: a fase
// com o texto da mensagem. Opcional, como focoComElevacao: um mostrador que não
// o implementa mostra a fase com o Label dela.
type mostradorComTexto interface {
	SetPhaseComTexto(phase dictation.OverlayPhase, texto string)
}

// mostrarFase manda a fase (e o texto, se houver) à pílula.
func (rt *Runtime) mostrarFase(fase dictation.OverlayPhase, texto string) {
	if m, ok := rt.overlay.(mostradorComTexto); ok && texto != "" {
		m.SetPhaseComTexto(fase, texto)
		return
	}
	rt.overlay.SetPhase(fase)
}

// virouNotaPorVoz devolve, para a linha do histórico da tentativa, o id que ela
// deve ter e o virou_nota, e se a tentativa foi mesmo para a nota. Consome a
// anotação: uma tentativa nova nunca herda a de outra.
func (rt *Runtime) virouNotaPorVoz(attempt dictation.AttemptID) (ditadoID string, virou *string, foi bool) {
	n := rt.notaFeita
	if n.attempt != attempt || attempt == dictation.NoAttempt {
		return "", nil, false
	}
	rt.notaFeita = notaDaTentativa{}
	if n.falhou {
		// À vista no histórico: é o lugar onde o texto está.
		return n.ditadoID, nil, true
	}
	id := n.res.NotaID
	return n.ditadoID, &id, true
}
