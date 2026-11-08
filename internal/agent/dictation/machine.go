package dictation

import "time"

// Machine é o redutor puro do núcleo de ditado (ADR-007, seção 1). Todo
// estímulo entra via Handle como Event; toda ação sai como []Effect.
// Machine nunca executa I/O, nunca cria goroutine, nunca chama time.Now().
//
// Handle não é thread-safe por projeto (ADR-007, "Consequências"): o
// runtime precisa garantir um único consumidor serializado.
type Machine struct {
	cfg Config

	state   State
	current AttemptID
	nextID  AttemptID

	// pending é o TimerKind que a tentativa corrente está aguardando.
	// Vale apenas enquanto current != NoAttempt.
	pending TimerKind

	focus     FocusTarget
	finalText string

	// A gravação solta (ADR-025).
	//
	// ultimoToque é a soltura do último toque limpo; ele abre a janela do
	// duplo toque. Zero, não há janela. Todo keydown que começa uma
	// tentativa o consome.
	ultimoToque time.Time
	// solta: a tentativa corrente começou pelo duplo toque. Vale até o fim
	// dela, porque o foco é relido na hora de escrever (ReleFoco).
	solta bool
	// gravandoSolta: a captura da gravação solta está em curso, com o hook
	// do mouse e o Esc ligados (GravacaoSoltaEffect). Sempre implica
	// StateRecording.
	gravandoSolta bool
	// segurando: o segundo aperto do duplo toque ainda não foi solto;
	// apertouEm é quando ele começou.
	segurando bool
	apertouEm time.Time
}

// NewMachine cria um Machine para a configuração cfg, já validada
// (Config.Validate). Devolve erro se cfg for inválida.
func NewMachine(cfg Config) (*Machine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Machine{cfg: cfg.withDefaults(), state: StateIdle}, nil
}

// AjustarPreferencias troca o limiar do hold e o beep — o que a tela de
// Configurações muda com o Agent rodando. A tecla não muda aqui: ela
// também mora no Decider do hook.
//
// Valida como NewMachine; com erro, nada muda. Não é thread-safe, pela
// mesma regra de Handle: quem chama é o consumidor único.
//
// O limiar é lido no keydown (StartTimerEffect) e o beep quando o hold
// vence, então uma troca no meio de uma tentativa não corrompe nada. O
// runtime, ainda assim, só chama com a máquina parada (ver
// winruntime.Runtime.Ajustar), para cada ditado sair inteiro com UMA
// configuração.
func (m *Machine) AjustarPreferencias(holdThreshold time.Duration, beep bool) error {
	c := m.cfg
	c.HoldThreshold, c.BeepEnabled = holdThreshold, beep
	if err := c.Validate(); err != nil {
		return err
	}
	m.cfg = c
	return nil
}

// Config devolve a configuração em vigor — o que o próximo keydown vai
// usar, já com os padrões preenchidos.
func (m *Machine) Config() Config { return m.cfg }

// State devolve o estado observável corrente, para UI e testes.
func (m *Machine) State() State { return m.state }

// CurrentAttempt devolve o AttemptID da tentativa em curso, ou NoAttempt.
func (m *Machine) CurrentAttempt() AttemptID { return m.current }

// Handle processa um Event e devolve os Effect que o runtime deve
// executar. Eventos cujo AttemptID não seja o da tentativa corrente são
// descartados (ADR-007, seção 5) — exceto KeyDownEvent, que é o único
// evento capaz de iniciar uma tentativa nova.
func (m *Machine) Handle(ev Event) []Effect {
	switch e := ev.(type) {
	case KeyDownEvent:
		return m.onKeyDown(e)
	case KeyUpEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onKeyUp(e)
	case TimerElapsedEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onTimerElapsed(e)
	case AudioCapturedEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onAudioCaptured(e)
	case AudioFailedEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onAudioFailed(e)
	case TranscribeResultEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onTranscribeResult(e)
	case ResolveResultEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onResolveResult(e)
	case InsertResultEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onInsertResult(e)
	case CliqueEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onClique()
	case EscEvent:
		if !m.isCurrent(e.Attempt) {
			return nil
		}
		return m.onEsc()
	default:
		return nil
	}
}

// isCurrent reporta se id é a tentativa em curso. Uma tentativa só existe
// enquanto m.current != NoAttempt.
func (m *Machine) isCurrent(id AttemptID) bool {
	return m.current != NoAttempt && id == m.current
}

// onKeyDown trata o início de uma possível tentativa. Ignora keydowns
// redundantes (ex.: auto-repeat do SO enquanto a tecla está segurada) —
// só aceita um novo keydown quando nenhuma tentativa está em curso.
func (m *Machine) onKeyDown(e KeyDownEvent) []Effect {
	if m.current != NoAttempt {
		return nil
	}
	duplo := m.fechaDuploToque(e.At)
	m.ultimoToque = time.Time{}

	m.nextID++
	id := m.nextID
	m.current = id
	m.focus = e.Focus

	if e.Focus.Protected {
		// AC-036/TEST-009: campo protegido bloqueia o DITADO, não a tecla.
		// Nenhum áudio é capturado em momento algum — repare que não há
		// MarkAudioStartEffect neste caminho.
		//
		// A tentativa continua viva só para preservar o tap: o Decider já
		// suprimiu a tecla na borda do hook, então se encerrássemos aqui o
		// caractere sumiria e a tecla de ditado ficaria impossível de
		// digitar dentro de qualquer campo de senha — violando o AC-001 e
		// caindo na regressão proibida de bloquear o teclado.
		m.state = StateBlocked
		m.pending = TimerHoldThreshold
		return []Effect{
			SuppressKeyEffect{Attempt: id},
			StartTimerEffect{Attempt: id, Kind: TimerHoldThreshold, Duration: m.cfg.HoldThreshold},
		}
	}

	if duplo {
		return m.ligarSolta(id, e.At)
	}

	m.state = StatePrimed
	m.pending = TimerHoldThreshold
	return []Effect{
		SuppressKeyEffect{Attempt: id},
		MarkAudioStartEffect{Attempt: id},
		StartTimerEffect{Attempt: id, Kind: TimerHoldThreshold, Duration: m.cfg.HoldThreshold},
	}
}

// onKeyUp trata a soltura da tecla de ditado.
func (m *Machine) onKeyUp(e KeyUpEvent) []Effect {
	id := m.current
	if m.gravandoSolta {
		return m.onKeyUpSolta(id, e)
	}
	switch m.state {
	case StatePrimed:
		// Tap curto (AC-001): threshold ainda não expirou. Descarta o
		// buffer e sintetiza o caractere original.
		//
		// O toque limpo abre a janela do duplo toque (ADR-025). O toque num
		// campo protegido (StateBlocked, abaixo) não abre: lá o ditado não
		// existe.
		if e.Limpo {
			m.ultimoToque = e.At
		}
		effects := []Effect{
			CancelTimerEffect{Attempt: id, Kind: TimerHoldThreshold},
			AbortAudioCaptureEffect{Attempt: id},
		}
		effects = append(effects, m.devolverToque(id)...)
		return append(effects, m.endAttempt(id, OutcomeTap, ErrorReasonNone, "")...)
	case StateBlocked:
		// Tap curto em campo protegido: o ditado está bloqueado, mas a tecla
		// precisa continuar digitando normalmente (AC-001). Nenhum áudio foi
		// capturado, então não há nada para abortar.
		effects := []Effect{
			CancelTimerEffect{Attempt: id, Kind: TimerHoldThreshold},
		}
		effects = append(effects, m.devolverToque(id)...)
		return append(effects, m.endAttempt(id, OutcomeTap, ErrorReasonNone, "")...)
	case StateRecording:
		// Hold real (AC-002/AC-007): para a captura autoritativamente.
		//
		// O overlay sai de "Gravando" AQUI, junto com a captura, e não lá
		// na frente no desfecho: é este instante que a pessoa vive como
		// "soltei a tecla". Sem isto o contador seguia correndo durante o
		// ASR inteiro e a tela afirmava estar gravando com o microfone já
		// fechado (ver OverlayProcessing).
		return m.pararCaptura(id, true, SemFim)
	default:
		// Keyup redundante durante processing/inserting: nada a fazer.
		return nil
	}
}

// devolverToque é o SynthesizeTapEffect do toque curto, e só existe no
// gatilho por tecla: lá o hook suprimiu a tecla, e o toque precisa digitá-la
// (AC-001). No gatilho por combinação (Ctrl+Win) o hook nunca suprime nada
// (NewDeciderModificadores) e não há caractere a devolver. O efeito só fazia o
// hook recusar ("no character mapping for key 0") e o runtime registrar um WARN
// a cada toque, e a cada gravação solta, que começa por um toque (ADR-025).
func (m *Machine) devolverToque(id AttemptID) []Effect {
	if m.cfg.Key == KeyUnknown {
		return nil
	}
	return []Effect{SynthesizeTapEffect{Attempt: id}}
}

// fechaDuploToque reporta se um keydown em at é o segundo aperto de um duplo
// toque: veio até JanelaDoDuploToque depois da soltura do último toque limpo
// (ADR-025, A.1). Só existe no gatilho por combinação (A.5): com tecla de
// letra, dois toques são a letra digitada duas vezes. Instante desconhecido
// (zero) não fecha nada.
func (m *Machine) fechaDuploToque(at time.Time) bool {
	if m.cfg.Key != KeyUnknown || m.ultimoToque.IsZero() || at.IsZero() {
		return false
	}
	d := at.Sub(m.ultimoToque)
	return d >= 0 && d <= JanelaDoDuploToque
}

// ligarSolta começa a gravação solta no segundo aperto do duplo toque: grava
// já, sem esperar o tempo de segurar, e a pílula aparece agora (ADR-025,
// A.3). O limite de gravação é armado aqui também: é a rede de segurança de
// sempre, e são os 2 minutos que o Mauro manteve.
func (m *Machine) ligarSolta(id AttemptID, at time.Time) []Effect {
	m.state = StateRecording
	m.pending = TimerMaxRecording
	m.solta, m.gravandoSolta = true, true
	m.segurando, m.apertouEm = true, at
	effects := []Effect{
		SuppressKeyEffect{Attempt: id},
		MarkAudioStartEffect{Attempt: id},
		GravacaoSoltaEffect{Attempt: id, Ligada: true},
		ShowOverlayEffect{Attempt: id, Phase: OverlayRecording},
		StartTimerEffect{Attempt: id, Kind: TimerMaxRecording, Duration: m.cfg.MaxRecording},
	}
	if m.cfg.BeepEnabled {
		effects = append(effects, PlayBeepEffect{Attempt: id})
	}
	return effects
}

// onKeyUpSolta trata a soltura do gatilho durante a gravação solta.
//
// A primeira é a do segundo aperto do duplo toque, e decide o que ele era:
//   - segurado além do tempo de segurar: o ditado de sempre, e soltar para;
//   - curto e limpo: a gravação fica solta;
//   - curto e com outra tecla no meio (Ctrl+Win+seta logo depois de um
//     toque): era atalho, e a gravação é descartada sem ir ao ASR.
//
// Depois dela, cada soltura é de um aperto feito com a gravação já solta: o
// toque limpo para; o que teve outra tecla no meio (trocar de área de
// trabalho) não, e a gravação segue (ADR-025, B.1).
func (m *Machine) onKeyUpSolta(id AttemptID, e KeyUpEvent) []Effect {
	if m.segurando {
		m.segurando = false
		if !e.At.IsZero() && e.At.Sub(m.apertouEm) >= m.cfg.HoldThreshold {
			return m.pararCaptura(id, true, FimSegurou)
		}
		if !e.Limpo {
			return m.descartar(id)
		}
		return nil
	}
	if !e.Limpo {
		return nil
	}
	return m.pararCaptura(id, true, FimToque)
}

// GravandoSemSegurar reporta se a gravação solta já está solta: o segundo
// aperto do duplo toque foi solto, e a gravação segue sem tecla nenhuma
// segurada. Daqui até ela parar, cada toque no gatilho é de parar, nunca uma
// sessão nova.
//
// Existe para o runtime não deixar uma dessas sessões remarcar a gravação: um
// Ctrl+Win+Alt no meio dela não para nada (não é toque limpo) e não pode
// mandar o texto para uma nota (ADR-025: a nota fica como está).
func (m *Machine) GravandoSemSegurar() bool { return m.gravandoSolta && !m.segurando }

// onClique trata o clique esquerdo da gravação solta: para e escreve, e o
// clique, que seguiu para o app, já pôs o cursor no campo (ADR-025, B.2).
// Fora da gravação solta não há hook do mouse; um clique atrasado, que
// chega depois de ela acabar, não faz nada.
func (m *Machine) onClique() []Effect {
	if !m.gravandoSolta {
		return nil
	}
	return m.pararCaptura(m.current, true, FimClique)
}

// onEsc trata o Esc da gravação solta: descarta (ADR-025, B.4).
func (m *Machine) onEsc() []Effect {
	if !m.gravandoSolta {
		return nil
	}
	return m.descartar(m.current)
}

// pararCaptura encerra a captura e começa a transcrição: é a soltura do
// hold, o toque ou o clique que param a gravação solta, e o limite de
// gravação. cancelarLimite é falso só quando quem para é o próprio limite,
// que já disparou. fim é como a gravação solta terminou, se era uma.
//
// A saída da gravação solta vem antes de tudo: o hook do mouse e o Esc
// desligam no instante em que ela para, e não no fim da transcrição.
func (m *Machine) pararCaptura(id AttemptID, cancelarLimite bool, fim FimDaSolta) []Effect {
	effects := m.sairDaSolta(id, fim)
	m.state = StateProcessing
	m.pending = TimerAudioStopTimeout
	if cancelarLimite {
		effects = append(effects, CancelTimerEffect{Attempt: id, Kind: TimerMaxRecording})
	}
	return append(effects,
		ShowOverlayEffect{Attempt: id, Phase: OverlayProcessing},
		StopAudioCaptureEffect{Attempt: id},
		StartTimerEffect{Attempt: id, Kind: TimerAudioStopTimeout, Duration: DefaultAudioStopTimeout},
	)
}

// descartar joga fora a gravação solta: a captura é abortada sem ir ao ASR,
// e a tentativa acaba sem histórico e com a pílula escondida. Quem desliga a
// gravação solta é o endAttempt.
func (m *Machine) descartar(id AttemptID) []Effect {
	effects := []Effect{
		CancelTimerEffect{Attempt: id, Kind: TimerMaxRecording},
		AbortAudioCaptureEffect{Attempt: id},
	}
	return append(effects, m.endAttempt(id, OutcomeDescartado, ErrorReasonNone, "")...)
}

// sairDaSolta desliga o que só existe durante a gravação solta. Fora dela
// não emite nada, e por isso os caminhos do ditado de sempre seguem com os
// mesmos efeitos de antes.
func (m *Machine) sairDaSolta(id AttemptID, fim FimDaSolta) []Effect {
	if !m.gravandoSolta {
		return nil
	}
	m.gravandoSolta, m.segurando = false, false
	return []Effect{GravacaoSoltaEffect{Attempt: id, Ligada: false, Fim: fim}}
}

// onTimerElapsed trata a expiração de um timer. Um timer cujo Kind não seja
// o que a tentativa está aguardando é obsoleto (já resolvido por uma
// resposta legítima) e é ignorado — é isso que resolve corretamente a
// disputa entre watchdog e resposta/keyup descrita na ADR-007.
func (m *Machine) onTimerElapsed(e TimerElapsedEvent) []Effect {
	if e.Kind != m.pending {
		return nil
	}
	id := m.current
	switch e.Kind {
	case TimerHoldThreshold:
		if m.state == StateBlocked {
			// Hold confirmado em campo protegido: avisa e encerra. Nenhum
			// áudio foi capturado em momento algum (AC-036).
			return m.endAttempt(id, OutcomeBlocked, ErrorReasonNone, "")
		}
		// Threshold expirou sem keyup: é hold (AC-002). Nenhum caractere é
		// emitido; entra em gravação com overlay e beep opcional (AC-006).
		//
		// TimerMaxRecording é armado aqui: é o que garante que a sessão volta
		// para idle mesmo se o keyup nunca chegar (ADR-007, seção 6). Sem
		// ele, uma tecla presa deixaria a máquina em StateRecording para
		// sempre e a tecla de ditado morreria até reiniciar o processo.
		m.state = StateRecording
		m.pending = TimerMaxRecording
		effects := []Effect{
			ShowOverlayEffect{Attempt: id, Phase: OverlayRecording},
			StartTimerEffect{Attempt: id, Kind: TimerMaxRecording, Duration: m.cfg.MaxRecording},
		}
		if m.cfg.BeepEnabled {
			effects = append(effects, PlayBeepEffect{Attempt: id})
		}
		return effects
	case TimerMaxRecording:
		// Limite de gravação atingido: keyup perdido, tecla presa, ou fala
		// realmente longa. Encerra a captura pelo MESMO caminho do keyup, em
		// vez de descartar: o VAD decide se havia fala de verdade (AC-010),
		// então uma tecla presa em silêncio não insere nada, e uma fala longa
		// legítima não é jogada fora.
		// Mesma saída de "Gravando" do keyup, pelo mesmo motivo: a
		// captura parou. Este caminho existe justamente para quando o
		// keyup NÃO chega (tecla presa), e é o caso em que deixar
		// "Gravando" na tela seria mais enganoso, não menos.
		//
		// Na gravação solta, o limite é o terceiro jeito de parar, e
		// escreve como os outros (ADR-025, B.3).
		return m.pararCaptura(id, false, FimLimite)
	case TimerAudioStopTimeout:
		effects := []Effect{AbortAudioCaptureEffect{Attempt: id}}
		return append(effects, m.endAttempt(id, OutcomeTimeout, ErrorReasonTimeout, "")...)
	case TimerTranscribeTimeout, TimerResolveTimeout:
		// A resolução não terminou: não existe texto final, e o histórico
		// fica sem ele. Nunca inventar um — ver endAttempt.
		return m.endAttempt(id, OutcomeTimeout, ErrorReasonTimeout, "")
	case TimerInsertTimeout:
		// A resolução terminou e o texto final existe; só a inserção não
		// respondeu. Até a revisão de 29-set-2026 este caminho passava ""
		// e a linha do histórico ficava "sem texto" justamente no ditado
		// que a pessoa mais precisa recuperar.
		return m.endAttempt(id, OutcomeTimeout, ErrorReasonTimeout, m.finalText)
	default:
		return nil
	}
}

// onAudioCaptured trata a resposta autoritativa de StopAudioCaptureEffect
// quando a captura teve sucesso. Sem fala válida, nada é transcrito nem
// inserido (AC-010).
func (m *Machine) onAudioCaptured(e AudioCapturedEvent) []Effect {
	if m.pending != TimerAudioStopTimeout {
		return nil
	}
	id := m.current
	effects := []Effect{CancelTimerEffect{Attempt: id, Kind: TimerAudioStopTimeout}}
	if !e.VAD.HasSpeech {
		return append(effects, m.endAttempt(id, OutcomeNoSpeech, ErrorReasonNoSpeech, "")...)
	}
	m.pending = TimerTranscribeTimeout
	// O prazo acompanha o tamanho do áudio: um ditado de 90 s não cabe no
	// prazo de um de 3 s (PrazoDeTranscricao).
	effects = append(effects,
		TranscribeEffect{Attempt: id, Audio: e.Audio},
		StartTimerEffect{Attempt: id, Kind: TimerTranscribeTimeout, Duration: PrazoDeTranscricao(e.Duration)},
	)
	return effects
}

// onAudioFailed trata a falha de StopAudioCaptureEffect (ex.: microfone
// perdido).
func (m *Machine) onAudioFailed(e AudioFailedEvent) []Effect {
	if m.pending != TimerAudioStopTimeout {
		return nil
	}
	id := m.current
	effects := []Effect{
		CancelTimerEffect{Attempt: id, Kind: TimerAudioStopTimeout},
		AbortAudioCaptureEffect{Attempt: id},
	}
	reason := e.Reason
	if reason == ErrorReasonNone {
		reason = ErrorReasonAudioCaptureFailed
	}
	return append(effects, m.endAttempt(id, OutcomeError, reason, "")...)
}

// onTranscribeResult trata a resposta de TranscribeEffect.
func (m *Machine) onTranscribeResult(e TranscribeResultEvent) []Effect {
	if m.pending != TimerTranscribeTimeout {
		return nil
	}
	id := m.current
	effects := []Effect{CancelTimerEffect{Attempt: id, Kind: TimerTranscribeTimeout}}
	if e.Reason != ErrorReasonNone {
		return append(effects, m.endAttempt(id, OutcomeError, e.Reason, "")...)
	}
	m.pending = TimerResolveTimeout
	// O prazo acompanha o tamanho do texto, e é medido no MESMO texto que
	// vai no ResolveEffect: o runtime calcula o prazo do Polish sobre ele,
	// e a folga entre os dois não pode depender de quem contou o quê
	// (PrazoDeResolucao).
	effects = append(effects,
		ResolveEffect{Attempt: id, ASRResult: e.Result, Focus: m.focus, ReleFoco: m.solta},
		StartTimerEffect{Attempt: id, Kind: TimerResolveTimeout, Duration: PrazoDeResolucao(e.Result.Text)},
	)
	return effects
}

// onResolveResult trata a resposta de ResolveEffect.
func (m *Machine) onResolveResult(e ResolveResultEvent) []Effect {
	if m.pending != TimerResolveTimeout {
		return nil
	}
	id := m.current
	effects := []Effect{CancelTimerEffect{Attempt: id, Kind: TimerResolveTimeout}}
	if e.Reason != ErrorReasonNone {
		return append(effects, m.endAttempt(id, OutcomeError, e.Reason, "")...)
	}
	m.state = StateInserting
	m.pending = TimerInsertTimeout
	m.finalText = e.Result.Text
	effects = append(effects,
		InsertEffect{Attempt: id, Text: e.Result.Text, Focus: m.focus, ReleFoco: m.solta},
		StartTimerEffect{Attempt: id, Kind: TimerInsertTimeout, Duration: DefaultInsertTimeout},
	)
	return effects
}

// onInsertResult trata a resposta de InsertEffect — o último passo de uma
// tentativa bem-sucedida.
func (m *Machine) onInsertResult(e InsertResultEvent) []Effect {
	if m.pending != TimerInsertTimeout {
		return nil
	}
	id := m.current
	effects := []Effect{CancelTimerEffect{Attempt: id, Kind: TimerInsertTimeout}}
	reason := e.Reason
	if reason == ErrorReasonNone && !e.Result.Success {
		reason = ErrorReasonInsertFailed
	}
	if reason != ErrorReasonNone {
		// A inserção falhou, mas o texto final existe e é exatamente o
		// que ia entrar no campo. Vai para o histórico junto com o erro.
		//
		// Até a revisão de 29-set-2026 este caminho passava "", e o
		// ditado que falhou virava "sem texto" no painel — inclusive no
		// caso ErrorReasonInsertClipboard, em que o overlay acabou de
		// dizer "está na área de transferência". O histórico é o lugar de
		// recuperar um ditado que não entrou; gravá-lo vazio apaga
		// justamente a linha que mais importa.
		return append(effects, m.endAttempt(id, OutcomeError, reason, m.finalText)...)
	}
	return append(effects, m.endAttempt(id, OutcomeSuccess, ErrorReasonNone, m.finalText)...)
}

// endAttempt é o único ponto de saída de uma tentativa (ADR-007, seção 7).
// Todo desfecho — sucesso, erro, blocked, timeout ou tap — passa por aqui,
// e ReleaseAudioEffect é sempre o último efeito emitido (TEST-007).
//
// text é o texto final da tentativa quando ele EXISTIU — sucesso, ou
// falha/timeout da inserção — e "" quando a tentativa acabou antes de a
// resolução produzir um.
func (m *Machine) endAttempt(id AttemptID, outcome Outcome, reason ErrorReason, text string) []Effect {
	// O texto final morre com a tentativa. Sem isto ele sobrevivia até a
	// próxima resolução bem-sucedida, e qualquer caminho que o lesse
	// antes disso gravaria no histórico o ditado ANTERIOR como se fosse
	// deste — pior que gravar vazio (revisão de 29-set-2026).
	m.finalText = ""
	switch outcome {
	case OutcomeSuccess:
		m.state = StateSuccess
	case OutcomeBlocked:
		m.state = StateBlocked
	case OutcomeTap, OutcomeDescartado:
		m.state = StateIdle
	default:
		m.state = StateError
	}
	m.current = NoAttempt
	m.pending = TimerNone

	// A gravação solta que acaba aqui é a descartada (descartar); a que para
	// para transcrever já foi desligada em pararCaptura. Desligar aqui também
	// garante que o hook do mouse nunca sobra ligado depois da tentativa.
	effects := m.sairDaSolta(id, FimDescartada)
	m.solta = false
	if outcome != OutcomeTap {
		effects = append(effects, ShowOverlayEffect{Attempt: id, Phase: overlayPhaseFor(outcome, reason)})
	}
	// Blocked não entra no histórico: registrar "tentou ditar aqui" carregaria
	// metadado sobre uso de campo de senha, contra a postura de privacidade da
	// spec (AC-027). O usuário já recebe o aviso pelo overlay. A gravação
	// descartada também não: a pessoa a jogou fora antes de virar texto.
	if outcome != OutcomeTap && outcome != OutcomeBlocked && outcome != OutcomeDescartado {
		effects = append(effects, RecordHistoryEffect{Attempt: id, Outcome: outcome, Reason: reason, Text: text})
	}
	effects = append(effects, ReleaseAudioEffect{Attempt: id})
	return effects
}

// overlayPhaseFor escolhe o que mostrar a partir do desfecho E do
// motivo.
//
// O motivo importa porque OutcomeError cobre coisas que pedem ações
// OPOSTAS de quem usa: "não consegui transcrever" pede ditar de novo;
// "não consegui inserir" pede colar o que já está pronto. Mostrar a
// primeira mensagem no segundo caso faz a pessoa refazer trabalho que já
// está feito — ver OverlayClipboard.
func overlayPhaseFor(outcome Outcome, reason ErrorReason) OverlayPhase {
	switch outcome {
	case OutcomeSuccess:
		return OverlaySuccess
	case OutcomeBlocked:
		return OverlayBlocked
	case OutcomeDescartado:
		// A pílula some: não houve erro nem texto a mostrar.
		return OverlayHidden
	case OutcomeError:
		switch reason {
		case ErrorReasonInsertClipboard:
			return OverlayClipboard
		case ErrorReasonInsertFailed:
			return OverlayInsertFailed
		}
		return OverlayError
	default:
		return OverlayError
	}
}
