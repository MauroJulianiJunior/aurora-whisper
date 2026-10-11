package dictation

import (
	"time"
	"unicode/utf8"
)

// Effect é implementado por toda ação que Machine.Handle pede ao runtime
// para executar. O núcleo nunca executa I/O: um Effect é puramente dado,
// interpretado por um runtime externo (fake na Fase 1, Windows real no
// Sprint 1).
type Effect interface {
	isEffect()
}

// Timeouts padrão para os efeitos que aguardam resposta (ADR-007, seção 6).
// A ADR não fixa valores; estes são defaults conservadores do núcleo,
// ajustáveis futuramente sem mudar o contrato dos Effect.
//
// DefaultTranscribeTimeout é o prazo da transcrição quando a duração do
// áudio não é conhecida, e o piso de PrazoDeTranscricao: o prazo de
// verdade acompanha o tamanho do áudio. DefaultResolveTimeout é, do mesmo
// jeito, o piso de PrazoDeResolucao, que acompanha o tamanho do texto.
const (
	DefaultAudioStopTimeout  = 5 * time.Second
	DefaultTranscribeTimeout = 10 * time.Second
	DefaultResolveTimeout    = 5 * time.Second
	DefaultInsertTimeout     = 3 * time.Second
)

// O prazo da transcrição cresce com o áudio (incidente de 01-out-2026,
// docs/defeito-ditado-longo.md).
//
// Até então ele era fixo em 10 s, e um ditado de 37 s e outro de 89 s
// terminaram em timeout com a transcrição chegando 8,8 s e 6,1 s depois.
// O texto ficou pronto e foi jogado fora. O tempo de ASR não é
// proporcional à fala com folga constante: no mesmo dia, a razão ASR/áudio
// foi de 0,09 a 0,51.
const (
	// PisoPrazoTranscricao é o menor prazo, para áudio curto ou de duração
	// desconhecida. É o prazo fixo de antes, que nunca cortou um ditado
	// abaixo de 15 s.
	PisoPrazoTranscricao = DefaultTranscribeTimeout
	// TetoPrazoTranscricao é o maior prazo. Com DefaultMaxRecording (120 s)
	// a fórmula daria 95 s; na pior razão medida (0,51) 120 s de áudio levam
	// 61 s, e 90 s deixam 1,47× de folga. Mais que isso só prende a tecla:
	// enquanto a transcrição não volta, o ditado seguinte é ignorado.
	TetoPrazoTranscricao = 90 * time.Second
	// basePrazoTranscricao é o custo fixo: subir a requisição, o servidor
	// esperar a vez dele, e a CPU disputada com o Meet. Numa chamada de
	// 30-set-2026, 19,9 s de áudio levaram 3,75 s.
	basePrazoTranscricao = 5 * time.Second
	// fatorPrazoTranscricao é o prazo por segundo de áudio: a pior razão
	// medida no incidente (0,51) com 1,5× de folga.
	fatorPrazoTranscricao = 0.75
)

// PrazoDeTranscricao devolve quanto esperar a transcrição de um áudio de
// duração audio: 5 s + 0,75 × audio, entre PisoPrazoTranscricao e
// TetoPrazoTranscricao. Zero ou negativo (duração desconhecida) dá o piso.
//
// Nos ditados do incidente: 27,5 s de áudio dão 25,6 s de prazo (o ASR
// levou 2,4 s); 37,2 s dão 32,9 s (levou 18,8 s, e estourava os 10 s);
// 88,9 s dão 71,7 s (levou 16,1 s, e estourava).
func PrazoDeTranscricao(audio time.Duration) time.Duration {
	if audio <= 0 {
		return PisoPrazoTranscricao
	}
	if audio >= TetoPrazoTranscricao*2 {
		// Já passou do teto, e multiplicar uma duração absurda poderia
		// transbordar.
		return TetoPrazoTranscricao
	}
	p := basePrazoTranscricao + time.Duration(float64(audio)*fatorPrazoTranscricao)
	return min(max(p, PisoPrazoTranscricao), TetoPrazoTranscricao)
}

// O prazo da resolução cresce com o texto (decisão 5 de
// docs/defeito-ditado-longo.md, do Mauro, em 01-out-2026: "o prazo cresce
// com o texto, até uns 8 s num ditado de 1 minuto").
//
// Quem precisa do tempo é o Polish (ADR-016): a chamada ao provedor de IA
// leva uma base (TLS, fila, primeiro token) mais o tempo de gerar a saída,
// e a saída cresce com o texto. Até aqui o prazo do Polish era fixo em
// 3,5 s, dentro dos 5 s deste watchdog, e no incidente de 01-out-2026 um
// ditado de 802 caracteres caiu no Smart por 1 ms (3469 ms contra 3468).
//
// O núcleo não conhece o Polish, mas o watchdog dele tem de ficar acima do
// prazo do Polish com a folga de sempre (1,5 s): se o watchdog vencer
// antes, a tentativa acaba SEM texto, e o ditado inteiro se perde. Por
// isso o acréscimo pelo texto mora aqui, e os dois prazos o somam ao seu
// piso: o watchdog é 5 s + acréscimo; o Polish (winruntime, prazoPolish),
// 3,5 s + acréscimo. A folga fica constante por construção, em qualquer
// tamanho.
const (
	// PisoPrazoResolucao é o prazo do texto curto: o de antes, que a fala
	// curta continua tendo, ao milissegundo.
	PisoPrazoResolucao = DefaultResolveTimeout
	// TetoPrazoResolucao é o maior prazo: os 8 s do Polish que o Mauro
	// decidiu, mais a folga de 1,5 s. É também quanto a tecla de ditado
	// fica ignorada, no pior caso, se a resolução nunca responder.
	TetoPrazoResolucao = 9500 * time.Millisecond
	// CaracteresSemAcrescimo é até onde o texto não ganha tempo: uns 17 s
	// de fala (13 a 15 caracteres por segundo nos ditados do incidente). É
	// a faixa em que os 3,5 s do Polish foram medidos com folga: o
	// benchmark de 30-set-2026 (p95 de 2,35 s) e, no log de 01-out-2026,
	// 254 caracteres em 2,07 s. Acima dela o log já aperta: 359 caracteres
	// levaram 3,14 s, 90% do prazo.
	CaracteresSemAcrescimo = 250
	// AcrescimoPorCaractere é o tempo a mais por caractere acima de
	// CaracteresSemAcrescimo. No log, acima de 250 caracteres cada caractere
	// de entrada rende de 0,21 a 0,32 token de saída (254 caracteres e 80
	// tokens, 359 e 96, 907 e 195: a razão cai com o tamanho, porque a saída
	// tem um custo fixo), então 6 ms por caractere são de 19 a 29 ms por
	// token: cobrem a geração a partir de 35 a 53 tokens/s, abaixo dos 80
	// tokens/s do ritmo mais lento que a ADR-016 estimou. É uma conta com
	// nove linhas de log de um dia e um modelo; a faixa logo depois dos 250
	// caracteres é a mais apertada, e o passo L4 do roteiro manual confere.
	// O acréscimo chega ao teto aos 1000 caracteres, o fim de um ditado de 1
	// minuto.
	AcrescimoPorCaractere = 6 * time.Millisecond
	// tetoAcrescimo é o acréscimo máximo, 4,5 s, alcançado aos 1000
	// caracteres.
	tetoAcrescimo = TetoPrazoResolucao - PisoPrazoResolucao
)

// AcrescimoPeloTexto devolve o tempo que a resolução de texto ganha pelo
// tamanho: zero até CaracteresSemAcrescimo caracteres (runas, não bytes),
// e AcrescimoPorCaractere por caractere acima disso, até 4,5 s.
//
// É a parte comum dos dois prazos da resolução: o watchdog do núcleo
// (PrazoDeResolucao) e o teto do Polish no runtime. Os dois a calculam
// sobre o MESMO texto, o que entra na resolução, para a folga entre eles
// não depender de quem contou o quê.
//
// Nos ditados do incidente: 359 caracteres ganham 654 ms; 802, 3,312 s;
// 907, 3,942 s.
func AcrescimoPeloTexto(texto string) time.Duration {
	n := utf8.RuneCountInString(texto)
	if n <= CaracteresSemAcrescimo {
		return 0
	}
	acima := n - CaracteresSemAcrescimo
	if acima >= int(tetoAcrescimo/AcrescimoPorCaractere) {
		return tetoAcrescimo
	}
	return time.Duration(acima) * AcrescimoPorCaractere
}

// PrazoDeResolucao devolve quanto o núcleo espera a resolução de texto:
// PisoPrazoResolucao mais AcrescimoPeloTexto(texto), de 5 s a 9,5 s. Texto
// vazio ou curto dá o piso, que é o prazo de antes.
func PrazoDeResolucao(texto string) time.Duration {
	return PisoPrazoResolucao + AcrescimoPeloTexto(texto)
}

// SuppressKeyEffect confirma ao runtime que o evento físico da tentativa
// Attempt deve ser (ou já foi) suprimido. Complementa a decisão rápida do
// Decider na borda do hook.
type SuppressKeyEffect struct{ Attempt AttemptID }

func (SuppressKeyEffect) isEffect() {}

// SynthesizeTapEffect pede ao runtime para sintetizar (via SendInput) o
// caractere da tecla original, já que o keydown físico foi suprimido
// (AC-001). O runtime deve marcar o evento sintetizado com
// FromThisProcess=true para não realimentar o Decider.
type SynthesizeTapEffect struct{ Attempt AttemptID }

func (SynthesizeTapEffect) isEffect() {}

// MarkAudioStartEffect pede ao runtime para marcar o ponto de corte de
// início desta tentativa no áudio capturado (ADR-007, seção 4). Emitido
// uma vez por tentativa, no keydown, e nunca em campo protegido.
//
// O que existe antes da marca depende do modo do microfone (ADR-015). No
// padrão, sob demanda, o microfone liga nesta marca e não há áudio
// anterior a ela (pre-roll 0). Só no modo "sempre pronto" há um ring
// buffer de pre-roll já em execução, e o corte começa até 300 ms antes.
type MarkAudioStartEffect struct{ Attempt AttemptID }

func (MarkAudioStartEffect) isEffect() {}

// StopAudioCaptureEffect pede a parada autoritativa da captura de áudio da
// tentativa Attempt. Espera resposta (AudioCapturedEvent ou
// AudioFailedEvent); sempre acompanhado de um StartTimerEffect.
type StopAudioCaptureEffect struct{ Attempt AttemptID }

func (StopAudioCaptureEffect) isEffect() {}

// AbortAudioCaptureEffect pede para descartar, sem esperar resposta, o
// áudio em andamento de uma captura que não será usada (tap curto, falha
// ou timeout de StopAudioCaptureEffect).
type AbortAudioCaptureEffect struct{ Attempt AttemptID }

func (AbortAudioCaptureEffect) isEffect() {}

// ReleaseAudioEffect libera definitivamente qualquer recurso de áudio
// associado à tentativa Attempt. É emitido exatamente uma vez, sempre, ao
// final de toda tentativa — o único ponto de saída da ADR-007, seção 7
// (TEST-007).
type ReleaseAudioEffect struct{ Attempt AttemptID }

func (ReleaseAudioEffect) isEffect() {}

// StartTimerEffect arma um timer de duração Duration para a tentativa
// Attempt, com o propósito Kind. Todo Effect que espera resposta
// (StopAudioCaptureEffect, TranscribeEffect, ResolveEffect, InsertEffect)
// vem sempre acompanhado de um StartTimerEffect de timeout (ADR-007,
// seção 6); TimerHoldThreshold é a exceção que decide tap vs hold.
type StartTimerEffect struct {
	Attempt  AttemptID
	Kind     TimerKind
	Duration time.Duration
}

func (StartTimerEffect) isEffect() {}

// CancelTimerEffect cancela um timer de propósito Kind armado
// anteriormente para Attempt, tipicamente porque a resposta esperada já
// chegou.
type CancelTimerEffect struct {
	Attempt AttemptID
	Kind    TimerKind
}

func (CancelTimerEffect) isEffect() {}

// ShowOverlayEffect pede ao runtime para atualizar o overlay visual da
// tentativa Attempt para a fase Phase.
type ShowOverlayEffect struct {
	Attempt AttemptID
	Phase   OverlayPhase
}

func (ShowOverlayEffect) isEffect() {}

// PlayBeepEffect pede um beep de confirmação ao entrar em gravação.
// Emitido apenas quando Config.BeepEnabled (AC-006).
type PlayBeepEffect struct{ Attempt AttemptID }

func (PlayBeepEffect) isEffect() {}

// TranscribeEffect pede a transcrição do áudio referenciado por Audio.
// Espera resposta (TranscribeResultEvent); sempre acompanhado de um
// StartTimerEffect.
type TranscribeEffect struct {
	Attempt AttemptID
	Audio   AudioBufferRef
}

func (TranscribeEffect) isEffect() {}

// ResolveEffect pede a resolução (Literal/Smart/Polish) do resultado bruto
// de ASR, no contexto de Focus. Espera resposta (ResolveResultEvent);
// sempre acompanhado de um StartTimerEffect.
type ResolveEffect struct {
	Attempt   AttemptID
	ASRResult ASRResultDTO
	Focus     FocusTarget
}

func (ResolveEffect) isEffect() {}

// InsertEffect pede a inserção de Text no controle Focus. Espera resposta
// (InsertResultEvent); sempre acompanhado de um StartTimerEffect.
type InsertEffect struct {
	Attempt AttemptID
	Text    string
	Focus   FocusTarget
}

func (InsertEffect) isEffect() {}

// RecordHistoryEffect pede o registro em histórico do desfecho de uma
// tentativa. Não é emitido para OutcomeTap (digitação normal não gera
// histórico de ditado).
type RecordHistoryEffect struct {
	Attempt AttemptID
	Outcome Outcome
	Reason  ErrorReason
	// Text é o texto final — o que foi, ou ia ser, inserido no campo.
	// Vem preenchido no sucesso E na falha ou timeout da inserção, em que
	// o texto existia e só não entrou; vem vazio quando a tentativa
	// acabou antes de a resolução produzir um. Vazio quer dizer "não
	// existiu", nunca "perdi" (revisão de 29-set-2026).
	Text string
}

func (RecordHistoryEffect) isEffect() {}
