package fakes

import (
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// timerKey identifica um timer armado: o par (tentativa, tipo), do mesmo
// jeito que o Machine só rastreia "o timer pendente" por tentativa.
type timerKey struct {
	Attempt dictation.AttemptID
	Kind    dictation.TimerKind
}

// Runtime é a borda síncrona falsa descrita no ADR-007: recebe o []Effect
// que Machine.Handle devolve, executa cada um contra os dublês, e
// realimenta o Machine com qualquer Event resultante — tudo dentro da
// mesma pilha de chamadas, usando um clock.Fake compartilhado no lugar do
// relógio de parede. Zero goroutine, zero sleep, zero polling.
//
// Não é seguro para uso concorrente — mesma regra de projeto do Machine
// (ADR-007, "Consequências": um único consumidor serializado).
type Runtime struct {
	Machine     *dictation.Machine
	Clock       *clock.Fake
	Audio       *FakeAudioSession
	Transcriber *FakeTranscriber
	Resolver    *FakeResolver
	Inserter    *FakeInserter

	timers map[timerKey]clock.Timer

	// Efeitos observados, expostos para as asserções da suíte de aceite.
	Suppressed  []dictation.AttemptID
	Synthesized []dictation.AttemptID
	Overlays    []dictation.ShowOverlayEffect
	Beeps       []dictation.AttemptID
	History     []dictation.RecordHistoryEffect
}

// NewRuntime conecta um Machine a um conjunto de dublês e a um clock.Fake
// compartilhado.
func NewRuntime(m *dictation.Machine, c *clock.Fake, audio *FakeAudioSession, tr *FakeTranscriber, rs *FakeResolver, ins *FakeInserter) *Runtime {
	return &Runtime{
		Machine:     m,
		Clock:       c,
		Audio:       audio,
		Transcriber: tr,
		Resolver:    rs,
		Inserter:    ins,
		timers:      make(map[timerKey]clock.Timer),
	}
}

// Dispatch alimenta ev ao Machine e executa cada Effect devolvido. É o
// único ponto de entrada que os testes usam para conduzir todo o laço:
// eventos físicos de tecla passam por aqui diretamente; Effects que
// esperam resposta realimentam seu resultado por aqui também,
// recursivamente, depois que o lote que os produziu terminar (ver apply).
func (r *Runtime) Dispatch(ev dictation.Event) []dictation.Effect {
	effects := r.Machine.Handle(ev)
	r.apply(effects)
	return effects
}

// apply executa os efeitos em duas fases: primeiro cada efeito é aplicado
// na ordem em que chegou — é isso que garante que um StartTimerEffect
// posicionado depois de um efeito que espera resposta, no mesmo lote,
// termine de se registrar antes que qualquer coisa reaja a ele. Só depois
// que o lote inteiro foi aplicado é que eventuais respostas imediatas dos
// dublês são realimentadas ao Machine. Isso espelha um runtime de verdade,
// onde a resposta de um pedido de I/O nunca chega de forma síncrona dentro
// da própria chamada que o disparou.
func (r *Runtime) apply(effects []dictation.Effect) {
	var pending []dictation.Event

	for _, eff := range effects {
		switch e := eff.(type) {
		case dictation.SuppressKeyEffect:
			r.Suppressed = append(r.Suppressed, e.Attempt)

		case dictation.SynthesizeTapEffect:
			r.Synthesized = append(r.Synthesized, e.Attempt)

		case dictation.MarkAudioStartEffect:
			r.Audio.MarkStart(e.Attempt, r.Clock.Now())

		case dictation.StopAudioCaptureEffect:
			if ev, ok := r.Audio.Stop(e.Attempt); ok {
				pending = append(pending, ev)
			}

		case dictation.AbortAudioCaptureEffect:
			r.Audio.Abort(e.Attempt)

		case dictation.ReleaseAudioEffect:
			r.Audio.Release(e.Attempt)

		case dictation.StartTimerEffect:
			r.startTimer(e)

		case dictation.CancelTimerEffect:
			r.cancelTimer(e.Attempt, e.Kind)

		case dictation.ShowOverlayEffect:
			r.Overlays = append(r.Overlays, e)

		case dictation.PlayBeepEffect:
			r.Beeps = append(r.Beeps, e.Attempt)

		case dictation.TranscribeEffect:
			if ev, ok := r.Transcriber.Transcribe(e.Attempt, e.Audio); ok {
				pending = append(pending, ev)
			}

		case dictation.ResolveEffect:
			if ev, ok := r.Resolver.Resolve(e.Attempt, e.ASRResult, e.Focus); ok {
				pending = append(pending, ev)
			}

		case dictation.InsertEffect:
			if ev, ok := r.Inserter.Insert(e.Attempt, e.Text, e.Focus); ok {
				pending = append(pending, ev)
			}

		case dictation.RecordHistoryEffect:
			r.History = append(r.History, e)
		}
	}

	for _, ev := range pending {
		r.Dispatch(ev)
	}
}

// startTimer arma, no clock.Fake compartilhado, um callback que — quando o
// tempo chegar — realimenta um TimerElapsedEvent ao Machine através de
// Dispatch. O timer é removido do mapa de pendentes assim que dispara, já
// que clock.Timer não permite reagendar.
func (r *Runtime) startTimer(e dictation.StartTimerEffect) {
	key := timerKey{Attempt: e.Attempt, Kind: e.Kind}
	attempt, kind := e.Attempt, e.Kind
	timer := r.Clock.AfterFunc(e.Duration, func() {
		delete(r.timers, key)
		r.Dispatch(dictation.TimerElapsedEvent{Attempt: attempt, Kind: kind})
	})
	r.timers[key] = timer
}

// cancelTimer para o timer (attempt, kind), se ainda estiver armado.
func (r *Runtime) cancelTimer(attempt dictation.AttemptID, kind dictation.TimerKind) {
	key := timerKey{Attempt: attempt, Kind: kind}
	if t, ok := r.timers[key]; ok {
		t.Stop()
		delete(r.timers, key)
	}
}

// PendingTimers devolve quantos timers estão armados e ainda não
// dispararam nem foram cancelados — útil para confirmar que uma tentativa
// concluída não deixa nenhum watchdog correndo em segundo plano.
func (r *Runtime) PendingTimers() int {
	return len(r.timers)
}
