package dictation

import "time"

// Event é implementado por todo estímulo que Machine.Handle consegue
// processar. Todo Event carrega o AttemptID da tentativa a que se refere
// (ADR-007, seção 5).
//
// Para KeyDownEvent, que pode iniciar uma tentativa nova, o chamador deve
// usar NoAttempt — é o Machine quem atribui o AttemptID real. Para os
// demais eventos, o AttemptID deve ser o que o runtime recebeu no Effect
// correspondente (StartTimerEffect, StopAudioCaptureEffect, TranscribeEffect,
// ResolveEffect ou InsertEffect); um AttemptID que não seja mais o corrente
// é descartado silenciosamente pelo Machine.
type Event interface {
	AttemptID() AttemptID
}

// KeyDownEvent sinaliza que a tecla de ditado foi pressionada (já filtrada
// pelo Decider: sem modificadores, não injetada por nós mesmos). Focus é o
// controle com foco no instante do keydown.
type KeyDownEvent struct {
	Attempt AttemptID
	Focus   FocusTarget
}

func (e KeyDownEvent) AttemptID() AttemptID { return e.Attempt }

// KeyUpEvent sinaliza que a tecla de ditado foi solta.
type KeyUpEvent struct {
	Attempt AttemptID
}

func (e KeyUpEvent) AttemptID() AttemptID { return e.Attempt }

// TimerElapsedEvent sinaliza que um timer armado por StartTimerEffect
// expirou sem ter sido cancelado a tempo.
type TimerElapsedEvent struct {
	Attempt AttemptID
	Kind    TimerKind
}

func (e TimerElapsedEvent) AttemptID() AttemptID { return e.Attempt }

// AudioCapturedEvent é a resposta autoritativa de StopAudioCaptureEffect
// quando a captura termina com sucesso.
type AudioCapturedEvent struct {
	Attempt AttemptID
	Audio   AudioBufferRef
	VAD     VADSummary
	// Duration é a duração do áudio capturado, das amostras, e não do
	// relógio entre apertar e soltar: é o que o ASR vai ouvir. Decide o
	// prazo da transcrição (PrazoDeTranscricao). Zero quer dizer
	// desconhecida, e o prazo fica no piso.
	Duration time.Duration
}

func (e AudioCapturedEvent) AttemptID() AttemptID { return e.Attempt }

// AudioFailedEvent é a resposta de StopAudioCaptureEffect quando a captura
// falha (ex.: microfone removido).
type AudioFailedEvent struct {
	Attempt AttemptID
	Reason  ErrorReason
}

func (e AudioFailedEvent) AttemptID() AttemptID { return e.Attempt }

// TranscribeResultEvent é a resposta de TranscribeEffect. Reason ==
// ErrorReasonNone indica sucesso.
//
// O tamanho de Result.Text decide o prazo da resolução
// (PrazoDeResolucao), como AudioCapturedEvent.Duration decide o da
// transcrição. Não há campo à parte com o tamanho: o texto já está aqui, e
// é ele que segue no ResolveEffect. Texto vazio dá o piso.
type TranscribeResultEvent struct {
	Attempt AttemptID
	Result  ASRResultDTO
	Reason  ErrorReason
}

func (e TranscribeResultEvent) AttemptID() AttemptID { return e.Attempt }

// ResolveResultEvent é a resposta de ResolveEffect. Reason ==
// ErrorReasonNone indica sucesso.
type ResolveResultEvent struct {
	Attempt AttemptID
	Result  ResolveResultDTO
	Reason  ErrorReason
}

func (e ResolveResultEvent) AttemptID() AttemptID { return e.Attempt }

// InsertResultEvent é a resposta de InsertEffect. Reason == ErrorReasonNone
// e Result.Success == true indicam sucesso.
type InsertResultEvent struct {
	Attempt AttemptID
	Result  InsertResultDTO
	Reason  ErrorReason
}

func (e InsertResultEvent) AttemptID() AttemptID { return e.Attempt }
