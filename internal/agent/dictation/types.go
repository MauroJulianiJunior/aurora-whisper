// Package dictation implementa o núcleo do ditado: um redutor puro e
// síncrono que decide o que fazer a cada evento (tecla, timer, resultado
// de áudio/ASR/resolução/inserção) sem nunca executar I/O.
//
// O contrato é o ADR-007 (docs/adr/ADR-007-dictation-core.md). Este pacote
// é folha: importa apenas a stdlib. Quem executa os Effect que o Machine
// devolve é sempre um runtime externo (fake na Fase 1, Windows real no
// Sprint 1); o núcleo em si não sabe se está rodando sob teste ou em
// produção.
package dictation

import "time"

// State é o estado observável de uma tentativa de ditado, usado pela UI
// (overlay) e pelos testes para verificar transições.
type State int

const (
	// StateIdle: nenhuma tentativa em curso.
	StateIdle State = iota
	// StatePrimed: tecla rastreada foi pressionada; ainda não se sabe se
	// será tap (digitação normal) ou hold (início de ditado).
	StatePrimed
	// StateRecording: threshold de hold expirou; captura em andamento.
	StateRecording
	// StateProcessing: tecla foi solta; aguardando parada de captura,
	// transcrição ou resolução.
	StateProcessing
	// StateInserting: texto resolvido; aguardando confirmação de inserção.
	StateInserting
	// StateSuccess: tentativa concluída com texto inserido.
	StateSuccess
	// StateError: tentativa terminou em falha (áudio, ASR, resolver,
	// inserção ou timeout).
	StateError
	// StateBlocked: campo focado é protegido (ex.: senha); ditado recusado.
	StateBlocked
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StatePrimed:
		return "primed"
	case StateRecording:
		return "recording"
	case StateProcessing:
		return "processing"
	case StateInserting:
		return "inserting"
	case StateSuccess:
		return "success"
	case StateError:
		return "error"
	case StateBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// KeyCode identifica uma tecla física, independente de plataforma. Os
// valores concretos (mapeamento para VK_* do Win32) são responsabilidade
// do runtime do Sprint 1; aqui é só um identificador opaco e comparável.
type KeyCode uint16

const (
	KeyUnknown KeyCode = iota

	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ

	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9

	KeySpace
	KeyEnter
	KeyTab
	KeyEscape
	KeyBackspace

	// Variantes L/R de cada tecla modificadora pura (AC-004: nenhuma delas
	// pode ser configurada como tecla principal do ditado).
	ShiftLeft
	ShiftRight
	ControlLeft
	ControlRight
	AltLeft
	AltRight
	MetaLeft
	MetaRight
)

// IsModifier reporta se k é uma tecla modificadora pura (Shift/Ctrl/Alt/Win,
// em qualquer variante L/R). Usado pela validação de Config (AC-004).
func (k KeyCode) IsModifier() bool {
	switch k {
	case ShiftLeft, ShiftRight, ControlLeft, ControlRight, AltLeft, AltRight, MetaLeft, MetaRight:
		return true
	default:
		return false
	}
}

// Modifiers é um bitmask das teclas modificadoras seguradas junto de um
// evento de tecla.
type Modifiers uint8

const (
	ModShift Modifiers = 1 << iota
	ModControl
	ModAlt
	ModMeta
)

// None devolve o conjunto vazio de modificadores.
func None() Modifiers { return 0 }

// Has reporta se todos os bits de bit estão presentes em m.
func (m Modifiers) Has(bit Modifiers) bool { return m&bit == bit }

// IsNone reporta se nenhum modificador está presente.
func (m Modifiers) IsNone() bool { return m == 0 }

// FocusTarget descreve o controle com foco no momento do KeyDown. É dado
// já pronto entregue pelo runtime (ADR-007, seção "Rejeitado" — nenhuma
// interface de localização de foco é declarada aqui).
type FocusTarget struct {
	ProcessName string
	ControlType string
	// Protected indica campo sensível (ex.: senha). AC-036/TEST-009: com
	// Protected == true, o ditado é bloqueado antes de capturar qualquer
	// áudio ou contexto.
	Protected bool
}

// AttemptID identifica uma tentativa de ditado. NoAttempt (zero) significa
// "nenhuma tentativa em curso" ou, num Event de KeyDown que ainda não
// iniciou uma tentativa, "deixe o Machine atribuir o próximo ID".
type AttemptID uint64

// NoAttempt é o valor de AttemptID que representa ausência de tentativa.
const NoAttempt AttemptID = 0

// RawKeyEvent é o evento físico entregue pelo hook de teclado, antes de
// qualquer interpretação semântica. Ver ADR-007, seção 3.
type RawKeyEvent struct {
	Key       KeyCode
	Modifiers Modifiers
	Down      bool
	At        time.Time
	// Injected indica que o evento veio de SendInput (de qualquer processo).
	Injected bool
	// FromThisProcess indica que fomos nós (Aurora Whisper) que injetamos
	// o evento, tipicamente via SynthesizeTapEffect. Usado para evitar o
	// laço infinito descrito no ADR-007, seção 3.
	FromThisProcess bool
	// ParaNota não vem do teclado: o hook a copia da decisão (Decision.ParaNota)
	// antes de mandar o evento ao runtime. Verdadeira, a sessão é do gatilho
	// "ditar para nota" (ADR-022): o texto vai para uma nota, não para o campo.
	ParaNota bool
}

// AudioBufferRef é uma referência opaca a um buffer de áudio mantido pelo
// runtime. O núcleo nunca vê ou persiste os bytes reais — apenas repassa
// esta referência de volta ao runtime via TranscribeEffect.
type AudioBufferRef struct {
	ID string
}

// VADSummary é o resultado mínimo de detecção de atividade de voz para o
// buffer de uma tentativa (ADR-007, "O que foi rejeitado": sem streaming,
// só o resumo final).
type VADSummary struct {
	HasSpeech      bool
	SpeechDuration time.Duration
}

// ASRResultDTO é o formato mínimo de resultado de transcrição que o núcleo
// consome. O runtime traduz internal/asr.ASRResult para este DTO.
type ASRResultDTO struct {
	Text       string
	Confidence float64
}

// ResolveResultDTO é o formato mínimo de resultado de resolução (Literal /
// Smart / Polish) que o núcleo consome.
type ResolveResultDTO struct {
	Text string
}

// InsertResultDTO é o formato mínimo de resultado de inserção de texto.
type InsertResultDTO struct {
	Success bool
}

// ErrorReason classifica por que uma tentativa terminou em erro.
type ErrorReason int

const (
	// ErrorReasonNone indica sucesso (nenhum erro).
	ErrorReasonNone ErrorReason = iota
	ErrorReasonAudioCaptureFailed
	ErrorReasonNoSpeech
	ErrorReasonTranscribeFailed
	ErrorReasonResolveFailed
	ErrorReasonInsertFailed
	// ErrorReasonInsertClipboard: a inserção falhou MAS o texto ficou na
	// área de transferência, retido de propósito (ADR-011, decisão 3).
	//
	// Separado de ErrorReasonInsertFailed porque as duas situações pedem
	// ações diferentes de quem usa: uma é "cole"; a outra é "não deu,
	// tente de novo". Mandar colar quando não há nada colado faz a
	// pessoa colar o que ela tinha copiado ANTES.
	ErrorReasonInsertClipboard
	// ErrorReasonTimeout indica que o watchdog interno (StartTimerEffect
	// de timeout) expirou antes de o runtime responder.
	ErrorReasonTimeout
)

func (r ErrorReason) String() string {
	switch r {
	case ErrorReasonNone:
		return "none"
	case ErrorReasonAudioCaptureFailed:
		return "audio_capture_failed"
	case ErrorReasonNoSpeech:
		return "no_speech"
	case ErrorReasonTranscribeFailed:
		return "transcribe_failed"
	case ErrorReasonResolveFailed:
		return "resolve_failed"
	case ErrorReasonInsertFailed:
		return "insert_failed"
	case ErrorReasonTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// Outcome classifica como uma tentativa terminou, para fins de
// RecordHistoryEffect.
type Outcome int

const (
	OutcomeSuccess Outcome = iota
	// OutcomeTap: tecla foi solta antes do threshold (digitação normal).
	OutcomeTap
	// OutcomeBlocked: campo focado era protegido; nada foi capturado.
	OutcomeBlocked
	// OutcomeNoSpeech: áudio capturado não continha fala válida (AC-010).
	OutcomeNoSpeech
	OutcomeError
	// OutcomeTimeout: watchdog interno expirou (mic perdido/travado etc.).
	OutcomeTimeout
)

func (o Outcome) String() string {
	switch o {
	case OutcomeSuccess:
		return "success"
	case OutcomeTap:
		return "tap"
	case OutcomeBlocked:
		return "blocked"
	case OutcomeNoSpeech:
		return "no_speech"
	case OutcomeError:
		return "error"
	case OutcomeTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// OverlayPhase descreve o que o overlay visual deve mostrar.
type OverlayPhase int

const (
	OverlayHidden OverlayPhase = iota
	OverlayRecording
	// OverlayProcessing: a tecla foi solta e a captura parou; o áudio
	// está sendo transcrito, resolvido e inserido (08-ui-ux-spec.md,
	// estado Processing).
	//
	// Estava na spec desde o começo e faltava no núcleo — overlay/phase.go
	// registrava a lacuna por escrito, sem corrigi-la por conta própria,
	// porque Transcribe e Resolve eram stub no Sprint 1 e não havia o que
	// mostrar. Com ASR real passou a haver: em 28-set-2026 Mauro relatou
	// "tem um delayzinho no final... tenho que esperar um segundo pra ele
	// parar de gravar", e a observação seguinte fechou o diagnóstico:
	// "depois que eu solto a tecla ele chega a marcar um segundo a mais no
	// contador na tela".
	//
	// O contador não estava atrasado. Ele estava certo: sem esta fase, o
	// overlay permanecia em OverlayRecording durante a parada do áudio, o
	// ASR (665 ms medidos), o resolver e a inserção inteira. A tela dizia
	// "Gravando" enquanto o microfone já estava fechado — e quem lê isso
	// conclui, corretamente, que o sistema ainda está gravando.
	//
	// Não é fase terminal: não esconde o overlay sozinho, porque a
	// tentativa não acabou. Quem a encerra é sempre endAttempt, por um
	// dos desfechos abaixo.
	OverlayProcessing
	OverlaySuccess
	OverlayError
	// OverlayBlocked: "Ditado desativado em campo protegido"
	// (08-ui-ux-spec.md, estado Blocked).
	OverlayBlocked
	// OverlayInsertFailed: a transcrição deu certo e a inserção falhou
	// SEM deixar o texto na área de transferência — inclusive o caso de
	// injeção parcial, em que parte do texto já está na tela e insistir
	// duplicaria (ver inserter.Result.Partial).
	OverlayInsertFailed
	// OverlayClipboard: a transcrição DEU CERTO e só a inserção falhou —
	// o texto está na área de transferência, retido de propósito
	// (ADR-011, decisão 3: não perder o texto ditado).
	//
	// Precisa ser uma fase própria porque OverlayError diz "Não consegui
	// transcrever. Tente novamente.", e nos dois casos o desfecho é
	// OutcomeError. Mauro encontrou isso usando o produto em
	// 27-set-2026: ditou sem um campo de texto em foco, o texto ficou no
	// clipboard, e a tela mandou ditar de novo — quando a ação certa era
	// clicar num campo e colar.
	//
	// Uma mensagem de erro que descreve o erro ERRADO é pior que
	// nenhuma: ela faz a pessoa refazer trabalho que já está pronto.
	OverlayClipboard
)

func (p OverlayPhase) String() string {
	switch p {
	case OverlayHidden:
		return "hidden"
	case OverlayRecording:
		return "recording"
	case OverlayProcessing:
		return "processing"
	case OverlaySuccess:
		return "success"
	case OverlayError:
		return "error"
	case OverlayBlocked:
		return "blocked"
	case OverlayInsertFailed:
		return "insert_failed"
	case OverlayClipboard:
		return "clipboard"
	default:
		return "unknown"
	}
}

// TimerKind identifica qual timer disparou/deve ser armado ou cancelado.
// Cada fase do Machine que aguarda uma resposta assíncrona tem seu próprio
// TimerKind (ADR-007, seção 6).
type TimerKind int

const (
	// TimerNone é o valor zero de TimerKind e significa "nenhum timer
	// pendente". Existe para que o zero value NÃO colida com um timer real:
	// enquanto TimerHoldThreshold valia zero, o guard de obsolescência da
	// ADR-007 (seção 5) não filtrava nada durante StateRecording, e um
	// TimerElapsed duplicado reprocessava o threshold.
	TimerNone TimerKind = iota
	// TimerHoldThreshold: decide tap vs hold (Config.HoldThreshold).
	TimerHoldThreshold
	// TimerMaxRecording: limite máximo de gravação (Config.MaxRecording).
	// É a rede de segurança para keyup perdido ou tecla presa — sem ele a
	// sessão ficaria em StateRecording para sempre (ADR-007, seção 6).
	TimerMaxRecording
	// TimerAudioStopTimeout: watchdog para StopAudioCaptureEffect.
	TimerAudioStopTimeout
	// TimerTranscribeTimeout: watchdog para TranscribeEffect.
	TimerTranscribeTimeout
	// TimerResolveTimeout: watchdog para ResolveEffect.
	TimerResolveTimeout
	// TimerInsertTimeout: watchdog para InsertEffect.
	TimerInsertTimeout
)

func (k TimerKind) String() string {
	switch k {
	case TimerNone:
		return "none"
	case TimerHoldThreshold:
		return "hold_threshold"
	case TimerMaxRecording:
		return "max_recording"
	case TimerAudioStopTimeout:
		return "audio_stop_timeout"
	case TimerTranscribeTimeout:
		return "transcribe_timeout"
	case TimerResolveTimeout:
		return "resolve_timeout"
	case TimerInsertTimeout:
		return "insert_timeout"
	default:
		return "unknown"
	}
}
