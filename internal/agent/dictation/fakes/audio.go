package fakes

import (
	"fmt"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// AudioFrame é uma amostra de áudio com carimbo de tempo, usada apenas
// dentro deste pacote para representar a "onda" (waveform) que um
// AudioSource real produziria. O núcleo (dictation.Machine) nunca vê este
// tipo — apenas a referência opaca dictation.AudioBufferRef.
type AudioFrame struct {
	At  time.Time
	RMS float64
}

// CapturedBuffer é o que FakeAudioSession entrega para uma tentativa
// concluída: os frames de pre-roll (alimentados antes de MarkStart) mais
// todo frame ao vivo alimentado depois, em ordem cronológica. Os testes de
// aceite inspecionam Frames para provar o AC-003 do modo "sempre pronto"
// (o início da fala não é perdido) com dado real, não com uma promessa em
// comentário. No modo padrão (ADR-015) não há pre-roll, e o AC-003 é
// provado em audio.Session, não aqui.
type CapturedBuffer struct {
	Attempt  dictation.AttemptID
	Frames   []AudioFrame
	MarkedAt time.Time
	Released bool
}

// stopOutcome descreve como FakeAudioSession.Stop deve responder a uma
// tentativa específica. respond=false modela um runtime que nunca devolve
// resposta, para exercitar o watchdog TimerAudioStopTimeout (ADR-007,
// seção 6).
type stopOutcome struct {
	respond bool
	ok      bool
	vad     dictation.VADSummary
	reason  dictation.ErrorReason
	// duracao é a duração do áudio que AudioCapturedEvent leva ao núcleo.
	// Zero, o default, é "desconhecida": o prazo da transcrição fica no
	// piso, como era antes de o prazo acompanhar o áudio.
	duracao time.Duration
}

// FakeAudioSession representa o dispositivo de captura no modo "sempre
// pronto" (ADR-007, seção 4; ADR-015): Feed alimenta um ring buffer que
// nunca para, MarkStart apenas marca um ponto de corte (sem interromper a
// captura), e Stop devolve tudo o que veio antes E depois desse corte. No
// padrão da ADR-015 o microfone só liga no keydown e não há nada antes da
// marca; quem o exercita é audio.Session, com um dispositivo falso próprio
// (internal/agent/audio/microfone_test.go).
//
// Não é seguro para uso concorrente — mesma regra de projeto do Machine e
// do clock.Fake (um único consumidor serializado).
type FakeAudioSession struct {
	ring []AudioFrame

	active   dictation.AttemptID
	preRoll  map[dictation.AttemptID][]AudioFrame
	live     map[dictation.AttemptID][]AudioFrame
	markedAt map[dictation.AttemptID]time.Time

	plans   map[dictation.AttemptID]stopOutcome
	buffers map[string]*CapturedBuffer
	seq     int

	// Allocated e Released contam quantos buffers foram criados por Stop e
	// quantos foram, em seguida, liberados por Release. É este par de
	// contadores que prova o TEST-007: ao final de qualquer tentativa,
	// Allocated deve ser igual a Released.
	Allocated int
	Released  int
}

// NewFakeAudioSession cria uma sessão de captura falsa vazia, pronta para
// receber frames via Feed antes mesmo de qualquer tentativa começar —
// exatamente como o dispositivo real no modo "sempre pronto", que já está
// gravando quando o Agent inicializa (ADR-007, seção 4; ADR-015).
func NewFakeAudioSession() *FakeAudioSession {
	return &FakeAudioSession{
		preRoll:  make(map[dictation.AttemptID][]AudioFrame),
		live:     make(map[dictation.AttemptID][]AudioFrame),
		markedAt: make(map[dictation.AttemptID]time.Time),
		plans:    make(map[dictation.AttemptID]stopOutcome),
		buffers:  make(map[string]*CapturedBuffer),
	}
}

// Feed simula um frame chegando do dispositivo (no "sempre pronto", sempre
// ligado, ADR-015). Chamado antes de MarkStart, o frame vira pre-roll;
// chamado depois, vira áudio ao vivo da tentativa ativa.
func (a *FakeAudioSession) Feed(f AudioFrame) {
	a.ring = append(a.ring, f)
	if a.active != dictation.NoAttempt {
		a.live[a.active] = append(a.live[a.active], f)
	}
}

// MarkStart tira uma fotografia do ring buffer acumulado até agora e a
// guarda como pre-roll de attempt (ADR-007, seção 4): tudo que foi
// alimentado antes desta chamada é preservado, provando o AC-003.
func (a *FakeAudioSession) MarkStart(attempt dictation.AttemptID, at time.Time) {
	snapshot := make([]AudioFrame, len(a.ring))
	copy(snapshot, a.ring)
	a.preRoll[attempt] = snapshot
	a.live[attempt] = nil
	a.markedAt[attempt] = at
	a.active = attempt
}

// HasMark reporta se MarkStart já foi chamado para attempt — usado para
// provar que um campo protegido nunca chega a marcar início de áudio
// (TEST-009/AC-036).
func (a *FakeAudioSession) HasMark(attempt dictation.AttemptID) bool {
	_, ok := a.markedAt[attempt]
	return ok
}

// ExpectStopSuccess configura Stop(attempt) para responder com sucesso,
// devolvendo vad como resumo de detecção de fala.
func (a *FakeAudioSession) ExpectStopSuccess(attempt dictation.AttemptID, vad dictation.VADSummary) {
	a.plans[attempt] = stopOutcome{respond: true, ok: true, vad: vad}
}

// ExpectStopSuccessWithDuration é ExpectStopSuccess com a duração do
// áudio capturado, que decide o prazo da transcrição
// (dictation.PrazoDeTranscricao).
func (a *FakeAudioSession) ExpectStopSuccessWithDuration(attempt dictation.AttemptID, vad dictation.VADSummary, duracao time.Duration) {
	a.plans[attempt] = stopOutcome{respond: true, ok: true, vad: vad, duracao: duracao}
}

// ExpectStopFailure configura Stop(attempt) para falhar com reason (ex.:
// microfone removido no meio da gravação).
func (a *FakeAudioSession) ExpectStopFailure(attempt dictation.AttemptID, reason dictation.ErrorReason) {
	a.plans[attempt] = stopOutcome{respond: true, ok: false, reason: reason}
}

// ExpectStopHang configura Stop(attempt) para nunca responder, exercitando
// o watchdog TimerAudioStopTimeout do próprio núcleo (ADR-007, seção 6).
func (a *FakeAudioSession) ExpectStopHang(attempt dictation.AttemptID) {
	a.plans[attempt] = stopOutcome{respond: false}
}

// Stop é chamado pelo Runtime ao aplicar um StopAudioCaptureEffect. Devolve
// o Event que a porta real acabaria entregando, ou ok=false se esta
// tentativa foi configurada para nunca responder.
func (a *FakeAudioSession) Stop(attempt dictation.AttemptID) (dictation.Event, bool) {
	if a.active == attempt {
		a.active = dictation.NoAttempt
	}

	plan, configured := a.plans[attempt]
	if !configured {
		// Default conveniente: sucesso com fala presente, o caminho feliz
		// mais comum em testes que não estão focados em áudio.
		plan = stopOutcome{respond: true, ok: true, vad: dictation.VADSummary{HasSpeech: true, SpeechDuration: 500 * time.Millisecond}}
	}
	if !plan.respond {
		return nil, false
	}
	if !plan.ok {
		return dictation.AudioFailedEvent{Attempt: attempt, Reason: plan.reason}, true
	}

	a.seq++
	id := fmt.Sprintf("buf-%d", a.seq)
	frames := make([]AudioFrame, 0, len(a.preRoll[attempt])+len(a.live[attempt]))
	frames = append(frames, a.preRoll[attempt]...)
	frames = append(frames, a.live[attempt]...)
	buf := &CapturedBuffer{Attempt: attempt, Frames: frames, MarkedAt: a.markedAt[attempt]}
	a.buffers[id] = buf
	a.Allocated++

	return dictation.AudioCapturedEvent{Attempt: attempt, Audio: dictation.AudioBufferRef{ID: id}, VAD: plan.vad, Duration: plan.duracao}, true
}

// Abort descarta qualquer rastreamento em andamento para attempt sem gerar
// buffer — é o caminho de tap, watchdog e falha (AbortAudioCaptureEffect).
func (a *FakeAudioSession) Abort(attempt dictation.AttemptID) {
	if a.active == attempt {
		a.active = dictation.NoAttempt
	}
	delete(a.preRoll, attempt)
	delete(a.live, attempt)
}

// Release é chamado pelo Runtime para todo ReleaseAudioEffect — o único
// ponto de saída (ADR-007, seção 7). Se um buffer foi alocado para
// attempt, ele é marcado como liberado e contabilizado em Released, o que
// prova o TEST-007.
func (a *FakeAudioSession) Release(attempt dictation.AttemptID) {
	for _, buf := range a.buffers {
		if buf.Attempt == attempt && !buf.Released {
			buf.Released = true
			a.Released++
		}
	}
}

// AllReleased reporta se todo buffer já alocado também já foi liberado —
// o invariante do TEST-007, verificável ao final de qualquer teste.
func (a *FakeAudioSession) AllReleased() bool {
	return a.Allocated == a.Released
}

// Buffer devolve o buffer capturado para attempt, se algum foi alocado.
func (a *FakeAudioSession) Buffer(attempt dictation.AttemptID) (*CapturedBuffer, bool) {
	for _, buf := range a.buffers {
		if buf.Attempt == attempt {
			return buf, true
		}
	}
	return nil, false
}
