package winruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/hook"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/inserter"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// pendingBuffer é a capacidade do canal interno de respostas assíncronas
// (rt.pending). Pequena de propósito: por construção do núcleo
// (Machine.pending é um único TimerKind por vez, ADR-007) há no máximo um
// timer legítimo pendente por tentativa; folga extra só absorve a corrida
// inofensiva entre CancelTimerEffect e um timer que já estava disparando
// (ver o comentário de startTimer).
const pendingBuffer = 4

// timerKey identifica um timer armado: o par (tentativa, tipo) — mesma
// convenção de internal/agent/dictation/fakes/runtime.go.timerKey.
type timerKey struct {
	Attempt dictation.AttemptID
	Kind    dictation.TimerKind
}

// focusProvider é a fonte de dictation.FocusTarget que handleRaw lê a
// cada KeyDownEvent (ADR-011, decisão 2): leitura O(1), sem I/O, nunca
// uma consulta síncrona ao caminho do teclado. Interface porque só isto —
// Current() — é o contrato que handleRaw precisa; a implementação de
// produção é *uia.Tracker (ADR-011), que satisfaz este método por
// construção. Existe como interface, em vez do tipo concreto direto
// (diferente de hook/audio/overlay abaixo), só para que a fatia de teste
// que prova AC-036 alcançável (runtime_test.go) não dependa de um
// *uia.Tracker específico nem do seu estado default — ver o comentário
// desse teste.
type focusProvider interface {
	Current() dictation.FocusTarget
}

// focoComElevacao é o que *uia.Tracker oferece além de Current: o alvo e,
// da MESMA leitura do cache, se a proteção dele é por janela de
// administrador (decisão do Mauro de 29-set-2026, item 4). É opcional: um
// focusProvider que não a implementa nunca troca a mensagem, e o ditado é
// bloqueado igual — a marca só escolhe o texto da pílula.
//
// Fica no runtime, e não em dictation.FocusTarget, porque o núcleo não
// muda (ADR-011): ele bloqueia por Protected, como sempre; a mensagem é do
// runtime e do overlay (overlay.FaseDoBloqueio).
type focoComElevacao interface {
	CurrentComElevacao() (alvo dictation.FocusTarget, janelaElevada bool)
}

// lerFoco lê o alvo do keydown e, se o provedor souber, a marca de janela
// de administrador — as duas coisas da mesma leitura.
func (rt *Runtime) lerFoco() (dictation.FocusTarget, bool) {
	if f, ok := rt.focus.(focoComElevacao); ok {
		return f.CurrentComElevacao()
	}
	return rt.focus.Current(), false
}

// Runtime liga o núcleo puro (dictation.Machine) aos colaboradores Win32
// reais: hook, áudio, overlay, rastreador de foco (uia.Tracker) e o
// inserter em camadas (internal/agent/inserter). É o equivalente de
// produção de dictation/fakes.Runtime — mesma forma (Dispatch alimenta o
// Machine, apply executa cada Effect, respostas síncronas realimentam
// recursivamente depois que o lote inteiro foi aplicado); só o outro lado
// de cada porta é real (ADR-010).
//
// Run é o único consumidor de Machine.Handle (ADR-010, "Modelo de
// threads: Runtime — único consumidor serializado de Machine.Handle").
// Nenhum outro método deste tipo chama Handle diretamente; tudo que
// precisa alimentar um Event de volta ao núcleo depois que Run já
// retornou de uma chamada (isto é, de outra goroutine — tipicamente o
// callback de um clock.System.AfterFunc) passa pelo canal pending, nunca
// por uma chamada direta.
// transcriber é o que o Runtime precisa de um ASR: transcrever um buffer.
// Interface local, e não asr.ASRProvider inteira, porque o runtime não usa
// ID, Capabilities nem Warmup — e uma interface estreita é o que permite o
// dublê dos testes existir sem implementar o que não importa aqui.
type transcriber interface {
	Transcribe(ctx context.Context, audio asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error)
}

// insertor é o que o Runtime precisa de um inserter: pôr texto no campo
// em foco.
//
// É interface, e não o *inserter.Inserter concreto, pelo mesmo motivo de
// transcriber: sem dublê não há como testar que uma inserção PRESA não
// congela o laço de Run — e esse era, até 28-set-2026, um caminho sem
// teste nenhum. Não dá para exercitar o inserter de verdade aqui:
// CLAUDE.md proíbe rodar qualquer coisa que injete entrada ou escreva no
// clipboard desta máquina.
// capturador é o que o Runtime precisa de uma sessão de áudio.
//
// Interface pelo mesmo motivo de transcriber e insertor: audio.Session
// abre um dispositivo WASAPI de verdade, e sem dublê não há como testar
// que uma parada LENTA — Stop espera a cauda calar, até 200 ms — não
// congela o laço de Run.
// historico é o que o Runtime precisa do repositório de transcrições:
// gravar. Ler é do dashboard e do cmd/dictlearn, que falam com o
// repositório direto.
type historico interface {
	Put(storage.Transcription) error
}

type capturador interface {
	Frames() <-chan audio.Frame
	MarkStart(id dictation.AttemptID, at time.Time)
	Abort(id dictation.AttemptID)
	Stop(ctx context.Context, id dictation.AttemptID) (audio.AudioBuffer, dictation.VADSummary, error)
	// MicrofoneEntregando diz se o microfone já entrega áudio para a
	// tentativa — o que segura o bipe e a pílula "Gravando" (ADR-015;
	// ver microfone.go).
	MicrofoneEntregando(id dictation.AttemptID) bool
}

type insertor interface {
	Insert(text string) inserter.Result
}

// teclado é o que o Runtime precisa do hook: os eventos que o Decider
// deixou passar e o tap sintetizado. Em produção, *hook.Hook.
//
// Interface pelo mesmo motivo de capturador e insertor: sem dublê não há
// como provar que Run continua consumindo teclas ENQUANTO a chamada de
// rede do Polish espera (ADR-016, decisão 5) — o hook de verdade só
// entrega o que o Windows mandou.
type teclado interface {
	Events() <-chan dictation.RawKeyEvent
	SynthesizeTap() error
}

// mostrador é o que o Runtime precisa do overlay: mostrar a fase e
// alimentar a onda. Em produção, *overlay.Overlay.
//
// Interface pelo mesmo motivo de transcriber e insertor: sem dublê não há
// como testar QUAL fase chega à pílula — e a mensagem de janela de
// administrador (overlay.FaseDoBloqueio, 29-set-2026) só existe nessa
// escolha; o log sozinho não prova que a pílula a recebeu.
type mostrador interface {
	SetPhase(phase dictation.OverlayPhase)
	PushAmplitude(amplitude float64)
}

// audioSlot guarda o áudio de UMA tentativa entre a parada da captura e a
// transcrição.
//
// Slot único, e não um mapa, porque o núcleo trabalha com uma tentativa por
// vez (ADR-007). Um mapa precisaria de limpeza em todo caminho de
// encerramento — sucesso, falha, abort, timeout — e esquecer um deles
// deixaria amostras de áudio retidas na memória, contra o
// 09-security-privacy.md. Com slot único, guardar um novo áudio libera o
// anterior por construção.
// capturaPronta é o resultado de audio.Session.Stop atravessando de
// volta para a goroutine de Run.
type capturaPronta struct {
	attempt dictation.AttemptID
	buf     audio.AudioBuffer
	vad     dictation.VADSummary
	err     error
}

type audioSlot struct {
	attempt dictation.AttemptID
	buf     asr.AudioBuffer
	ocupado bool
}

type Runtime struct {
	machine  *dictation.Machine
	clk      clock.Clock
	hook     teclado
	audio    capturador
	overlay  mostrador
	focus    focusProvider
	inserter insertor
	log      *slog.Logger

	asr transcriber
	som audioSlot

	// brutoDaTentativa carrega o texto do ASR entre a resolução e o
	// registro no histórico.
	//
	// O núcleo puro não leva o bruto até RecordHistoryEffect de
	// propósito: aquele efeito é comparado POR VALOR em vários testes da
	// máquina, e acrescentar campo por um motivo de ADAPTADOR faria o
	// núcleo carregar forma que ele não contrata (ADR-007). Aqui é
	// seguro sem sincronização: resolver() e recordHistory() rodam os
	// dois na goroutine de Run.
	brutoDaTentativa struct {
		attempt dictation.AttemptID
		texto   string
	}

	// originalDoASR é o texto do ASR como chegou, antes de a guarda tirar
	// um laço de repetição (repeticao.go). Vai para o raw_text do
	// histórico, e só para ele: a resolução e a inserção recebem o texto
	// sem o laço. Escrito pela guarda e consumido por brutoParaOHistorico,
	// os dois na goroutine de Run.
	originalDoASR struct {
		attempt dictation.AttemptID
		texto   string
	}

	// audioDaTentativa carrega a duração do áudio capturado até o
	// histórico (duration_ms), pelo mesmo motivo de brutoDaTentativa: o
	// núcleo não contrata esse dado. concluirCaptura escreve e
	// recordHistory lê, as duas na goroutine de Run.
	audioDaTentativa struct {
		attempt dictation.AttemptID
		duracao time.Duration
	}

	// asrMedido carrega o tempo que o provider mediu na transcrição até
	// o histórico (processing_ms).
	//
	// Ao contrário dos dois acima, quem escreve NÃO é a goroutine de Run:
	// é a goroutine de transcrever, que pode ainda estar viva quando a
	// tentativa seguinte começa (a anterior caiu no watchdog). Daí o
	// ponteiro atômico, e daí a tentativa gravada junto: uma medição de
	// outra tentativa é descartada, nunca atribuída a esta.
	asrMedido atomic.Pointer[medicaoASR]

	// emVoo são as transcrições que saíram para o ASR e não voltaram, por
	// tentativa. É o que deixa guardar no histórico a que chega depois do
	// prazo (tardio.go). Só a goroutine de Run lê e escreve; nil até a
	// primeira transcrição.
	emVoo map[dictation.AttemptID]transcricaoEmVoo

	// hist e provedorASR são o destino do histórico. Nil mantém o
	// comportamento anterior: só log, sem persistência.
	hist        historico
	provedorASR string
	modeloASR   string
	usuarioHist string

	// resolucao é a política de resolução real (internal/resolver). Nil
	// mantém o comportamento histórico: devolver o texto do ASR intocado.
	// Ver UsarResolver.
	resolucao resolver.Resolver
	usuario   string
	// literal desliga a resolução: o ditado sai como o ASR entregou (modo
	// Literal, AC-023), mesmo com resolver ligado. Só a goroutine de Run
	// lê e escreve — ver Ajustar.
	literal bool
	// polish, provedorIA e modeloIA são o modo Polish e a escolha de IA que
	// valiam no keydown (ADR-016, decisão 2). Mesma regra de literal: só a
	// goroutine de Run lê e escreve, e só mudam no keydown com a máquina
	// parada.
	polish     bool
	provedorIA string
	modeloIA   string
	// abrirPolidor monta, a cada ditado, a metade de rede do Polish. Nil:
	// o Polish escolhido cai no Smart com o motivo sem_cliente. Ver
	// UsarPolish, em polish.go.
	abrirPolidor AbrirPolidor
	// prazoDoPolish é o teto da metade de rede do Polish, contado do começo
	// da resolução. Zero usa prazoPolish, que cresce com o texto (de 3,5 s
	// a 8 s); só os testes põem um valor fixo, e curto.
	prazoDoPolish time.Duration
	// ctxRun é o ctx de Run, de onde deriva o ctx de cada chamada do
	// Polish: encerrar o Agent cancela a chamada em voo. Escrito no começo
	// de Run e lido em resolver, os dois na goroutine de Run.
	ctxRun context.Context
	// polidos traz o resultado do Polish da goroutine da chamada de volta
	// ao laço, com o que a linha de log e o histórico precisam. Canal
	// próprio, e não rt.pending, pelo motivo de capturas: o modo da
	// tentativa e o último motivo passam a ter um dono só, a goroutine de
	// Run. Buffer 1: uma tentativa por vez.
	polidos chan polidoPronto
	// polishDaTentativa é o desfecho do Polish da tentativa, a caminho do
	// histórico: "polish" só com a reescrita aceita (ADR-016, decisão 12).
	// Escrito em concluirPolish e lido em recordHistory, os dois na
	// goroutine de Run.
	polishDaTentativa struct {
		attempt   dictation.AttemptID
		reescrito bool
	}
	// ultimoPolish é o que a tela vai ler (S5, polish_ultimo_motivo).
	// Escrito pela goroutine de Run, lido pela da API local: daí o ponteiro
	// atômico. Ver UltimoPolish.
	ultimoPolish atomic.Pointer[EstadoPolish]
	// ajuste é a preferência pedida pelo painel e ainda não aplicada.
	// Escrito por Ajustar, de qualquer goroutine; consumido pela de Run.
	ajuste atomic.Pointer[Preferencias]
	// dicionario é o dicionário novo pedido pelo painel e ainda não
	// aplicado. Escrito por TrocarDicionario, de qualquer goroutine;
	// consumido pela de Run. Ver dicionario.go.
	dicionario atomic.Pointer[Dicionario]
	// vocabulario enviesa o decodificador do ASR para os termos do
	// dicionário. Vazio não envia nada. Ver UsarVocabulario. Escrito na
	// subida e por aplicarDicionario, e lido por transcrever, todos na
	// goroutine de Run.
	vocabulario string
	// dominioDoApp traduz o executável em foco no domínio do dicionário.
	// Nil, ou que devolva nil, significa "domínio desconhecido" — e
	// entrada restrita NÃO se aplica a domínio desconhecido. Ver
	// dictionary.DictionaryEntry.Restricted.
	dominioDoApp func(processName string) *string

	timers  map[timerKey]clock.Timer
	pending chan dictation.Event

	// capturas traz o áudio de volta da goroutine que esperou a cauda.
	// Buffer 1: o núcleo só tem uma tentativa por vez.
	capturas      chan capturaPronta
	pararCapturas chan struct{}

	// insercoes leva o pedido de inserção para a thread dedicada, e vaga
	// é a PERMISSÃO de pôr um ali.
	//
	// Só pode haver uma inserção em curso: se um pedido esperasse atrás
	// de outra travada, o watchdog encerraria a tentativa em 3 s e o
	// texto entraria DEPOIS — num campo que a pessoa já trocou, de uma
	// tentativa que a tela já declarou perdida.
	//
	// A primeira tentação é canal sem buffer com envio não-bloqueante:
	// ou a thread está recebendo agora, ou desiste. Está errado, e o
	// teste pegou na primeira execução. A thread não fica recebendo o
	// tempo todo — há uma janela entre ela terminar uma inserção e
	// voltar ao select, e um pedido que chegasse ali seria descartado
	// por escalonamento, não por ocupação. O ditado cairia no watchdog
	// de 3 s à toa.
	//
	// vaga torna a condição explícita: começa com uma ficha, quem
	// enfileira a toma, a thread a devolve quando termina. Ter a ficha
	// garante que insercoes (buffer 1) está vazio, então o envio nunca
	// bloqueia — mesmo que a thread ainda não tenha chegado ao select.
	insercoes chan dictation.InsertEffect
	vaga      chan struct{}

	// elevadaEm é a última tentativa cujo keydown leu um alvo protegido
	// por janela de administrador (NoAttempt se nenhuma). Escrito em
	// handleRaw e lido em apply, os dois na goroutine de Run. Só escolhe o
	// texto do bloqueio (overlay.FaseDoBloqueio), e só para a tentativa de
	// mesmo AttemptID.
	elevadaEm dictation.AttemptID

	// repeticaoEm é a última tentativa em que a guarda tirou um laço de
	// repetição do texto do ASR (repeticao.go). Escrito e lido na goroutine
	// de Run; só troca a mensagem do sucesso daquela tentativa
	// (overlay.FaseDoSucesso). Como elevadaEm, uma marca velha nunca casa
	// com uma tentativa nova.
	repeticaoEm dictation.AttemptID

	// retido e sinal são o "gravando" que espera o microfone (ADR-015).
	// Só a goroutine de Run lê e escreve. Ver microfone.go.
	retido sinalRetido
	sinal  sinalDaTentativa
	// bipar toca o bipe. Nil usa beep(); os testes trocam, porque o bipe
	// de verdade sai pela caixa de som desta máquina.
	bipar func() error
}

// New monta um Runtime pronto para Run. machine, hk, aud, ov e focus já
// devem estar prontos (dictation.NewMachine, hook.Hook.Start,
// audio.Source.Start, overlay.New, uia.Tracker.Start já chamados) —
// Runtime não é dono do ciclo de vida deles, só os opera; quem os criou é
// quem os fecha (cmd/agent). O inserter em camadas é montado aqui dentro
// (inserter.New) — ele não guarda estado de ciclo de vida próprio (ao
// contrário de hook/audio/overlay/focus), então não há nada para o
// chamador iniciar ou encerrar.
func New(machine *dictation.Machine, clk clock.Clock, hk *hook.Hook, aud *audio.Session, ov *overlay.Overlay, focus focusProvider, asrProvider transcriber, logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	// Um *hook.Hook nil numa interface não é nil: sem isto, Run chamaria
	// Events num ponteiro nulo em vez de falhar na hora de montar.
	var tec teclado
	if hk != nil {
		tec = hk
	}
	return &Runtime{
		machine:       machine,
		clk:           clk,
		hook:          tec,
		audio:         aud,
		overlay:       ov,
		focus:         focus,
		inserter:      inserter.New(logger),
		asr:           asrProvider,
		log:           logger,
		timers:        make(map[timerKey]clock.Timer),
		pending:       make(chan dictation.Event, pendingBuffer),
		insercoes:     make(chan dictation.InsertEffect, 1),
		capturas:      make(chan capturaPronta, 1),
		pararCapturas: make(chan struct{}),
		polidos:       make(chan polidoPronto, 1),
		vaga:          vagaLivre(),
	}
}

// Run consome hook.Events(), audio.Session.Frames() e o canal interno de
// respostas assíncronas (pending) até ctx ser cancelado, e é o único lugar
// deste pacote que chama Machine.Handle — direta ou indiretamente, via
// dispatch. Devolve ctx.Err() na saída por cancelamento; nunca sai por um
// Effect com falha, que o próprio núcleo já sabe tratar via timeout
// (ADR-007, seção 6).
func (rt *Runtime) Run(ctx context.Context) error {
	hookEvents := rt.hook.Events()
	var audioFrames <-chan audio.Frame
	if rt.audio != nil {
		audioFrames = rt.audio.Frames()
	}
	// A chamada do Polish deriva deste ctx: encerrar o Agent a cancela.
	rt.ctxRun = ctx

	// A thread de inserção vive enquanto Run viver. Ver threadDeInsercao:
	// ela existe para que uma inserção presa no clipboard não congele
	// este laço — que é quem consome hookEvents, audioFrames e pending.
	go rt.threadDeInsercao(ctx)
	// Libera qualquer goroutine de captura — ou do Polish — presa tentando
	// entregar resultado depois que Run saiu.
	defer close(rt.pararCapturas)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case raw, ok := <-hookEvents:
			if !ok {
				hookEvents = nil
				continue
			}
			rt.handleRaw(raw)

		case frame, ok := <-audioFrames:
			if !ok {
				audioFrames = nil
				// Com o Agent rodando, o canal só fecha quando a sessão de
				// áudio morreu de vez (audio.Session.Falha): nenhum ditado
				// funciona mais até reiniciar. Quem fecha a sessão no
				// encerramento é cmd/agent, depois de Run voltar.
				if ctx.Err() == nil {
					rt.log.Error("winruntime: o áudio parou; nenhum ditado funciona até reiniciar o Agent")
				}
				continue
			}
			rt.aoReceberFrame(frame)

		case c := <-rt.capturas:
			rt.dispatch(rt.concluirCaptura(c))

		case p := <-rt.polidos:
			rt.dispatch(rt.concluirPolish(p))

		case ev := <-rt.pending:
			// Timers e respostas de outra goroutine. A transcrição que
			// chega depois do prazo não vai ao núcleo: vai ao histórico
			// (tardio.go).
			rt.aoReceberPendente(ev)
		}
	}
}

// handleRaw traduz um dictation.RawKeyEvent já filtrado pelo Decider (via
// hook.Events, ADR-007 seção 2/3) num Event do núcleo. hook não conhece
// Machine nem FocusTarget — é esta fronteira que atribui AttemptID (o
// Machine decide o valor real em KeyDownEvent; KeyUpEvent usa o
// CurrentAttempt já corrente) e o FocusTarget do keydown.
//
// FocusTarget vem de rt.focus.Current() — leitura O(1) do cache mantido
// por uia.Tracker, nunca uma consulta UIA síncrona aqui (ADR-011, decisão
// 2: o callback do WH_KEYBOARD_LL tem orçamento de ~300ms; uma consulta
// UIA custa de milissegundos a centenas de milissegundos e travaria a
// digitação). Isto é o que torna StateBlocked (AC-036) alcançável em
// produção pela primeira vez (Sprint 2) — ver runtime_test.go.
func (rt *Runtime) handleRaw(raw dictation.RawKeyEvent) {
	if raw.Down {
		rt.aplicarAjuste()
		rt.aplicarDicionario()
		alvo, elevada := rt.lerFoco()
		antes := rt.machine.CurrentAttempt()
		rt.dispatch(dictation.KeyDownEvent{Attempt: dictation.NoAttempt, Focus: alvo})
		// A marca vale para a tentativa que ESTE keydown começou. Um keydown
		// no meio de outra tentativa é ignorado pelo núcleo, e não pode
		// remarcar a tentativa em curso com uma leitura mais nova. Uma marca
		// velha não precisa ser apagada: o AttemptID só sobe, e ela nunca
		// casa com uma tentativa nova.
		if nova := rt.machine.CurrentAttempt(); nova != antes && elevada && alvo.Protected {
			rt.elevadaEm = nova
		}
		return
	}
	attempt := rt.machine.CurrentAttempt()
	if attempt == dictation.NoAttempt {
		// Keyup sem tentativa corrente: o Decider só notifica keyup
		// quando o keydown correspondente foi armado (ADR-007, seção 3),
		// mas nada impede uma corrida onde a tentativa já terminou por
		// outro caminho (ex.: TimerMaxRecording) entre o keydown e este
		// keyup. Sem tentativa corrente não há AttemptID válido para
		// atribuir; descartar aqui é equivalente a Machine.isCurrent
		// devolver false.
		return
	}
	rt.dispatch(dictation.KeyUpEvent{Attempt: attempt})
}

// dispatch alimenta ev ao Machine e aplica os Effect devolvidos. Só é
// chamado de dentro de Run ou, recursivamente, de dentro de apply — nunca
// de outra goroutine (ADR-010, "Modelo de threads").
func (rt *Runtime) dispatch(ev dictation.Event) {
	effects := rt.machine.Handle(ev)
	rt.apply(effects)
}

// apply executa cada Effect, na ordem em que Machine.Handle os devolveu, e
// só realimenta ao núcleo as respostas produzidas pelo próprio lote depois
// que ele termina de ser aplicado inteiro — mesma disciplina, com a mesma
// justificativa, de dictation/fakes/runtime.go.apply: garante que um
// StartTimerEffect posicionado depois de um efeito com resposta, no mesmo
// lote, termine de se registrar antes que qualquer coisa reaja a ele.
//
// A única resposta realimentada aqui é a do Resolve, síncrona e local nos
// modos Literal e Smart — por isso next é preenchido e dispatch-ado dentro
// da MESMA goroutine de Run, sem quebrar o consumidor único. As que
// esperam I/O (captura, transcrição, inserção, e a rede do Polish) voltam
// por canal, de outra goroutine, e é Run quem as dispatch-a.
func (rt *Runtime) apply(effects []dictation.Effect) {
	var next []dictation.Event

	for _, eff := range effects {
		switch e := eff.(type) {
		case dictation.SuppressKeyEffect:
			// Confirmação: a supressão física já aconteceu na borda do
			// hook, dentro de Decider.Decide, antes mesmo deste Effect
			// existir. Nada a fazer aqui.

		case dictation.SynthesizeTapEffect:
			if err := rt.hook.SynthesizeTap(); err != nil {
				rt.log.Warn("winruntime: synthesize tap falhou", "attempt", e.Attempt, "err", err)
			}

		case dictation.MarkAudioStartEffect:
			// Marca o início e, no padrão, liga o microfone (ADR-015). Não
			// espera o microfone: só avisa a goroutine de áudio.
			rt.audio.MarkStart(e.Attempt, rt.clk.Now())

		case dictation.StopAudioCaptureEffect:
			rt.pedirCaptura(e.Attempt)

		case dictation.AbortAudioCaptureEffect:
			rt.audio.Abort(e.Attempt)

		case dictation.ReleaseAudioEffect:
			// audio.Session já limpa seu próprio rastreamento (o mapa
			// interno de marcas) dentro de Stop/Abort — ao contrário do
			// dublê da Fase 1, que retém os buffers capturados para as
			// asserções da suíte de aceite e por isso precisa de um
			// Release explícito. Aqui não há nada retido para liberar.

		case dictation.StartTimerEffect:
			rt.startTimer(e)

		case dictation.CancelTimerEffect:
			rt.cancelTimer(e.Attempt, e.Kind)

		case dictation.ShowOverlayEffect:
			// Logar a MUDANÇA de fase do overlay é o único sinal que
			// distingue "o gatilho não foi detectado" de "o overlay não
			// apareceu na tela". Sem isto os dois casos produzem um log
			// idêntico — silêncio — e foi exatamente o que impediu
			// diagnosticar "não consegui ver nada rodando" em
			// 26-set-2026.
			//
			// Só a fase, nunca o texto ditado nem contexto de janela
			// (CLAUDE.md, "Safety rules").
			//
			// Um bloqueio numa janela de administrador mostra a mensagem
			// própria (overlay.FaseDoBloqueio). O log continua dizendo a fase
			// do núcleo (blocked) e acrescenta o motivo.
			//
			// "Gravando" só sai com o microfone entregando áudio (ADR-015):
			// até lá o efeito fica retido. Ver microfone.go.
			if rt.reterGravando(e) {
				continue
			}
			fase := overlay.FaseDoBloqueio(e.Phase, e.Attempt != dictation.NoAttempt && e.Attempt == rt.elevadaEm)
			args := []any{"attempt", e.Attempt, "fase", e.Phase.String()}
			if fase != e.Phase {
				args = append(args, "motivo", "janela_elevada")
			}
			// Um sucesso em que a guarda tirou um laço de repetição pede para
			// conferir o texto (repeticao.go).
			if s := overlay.FaseDoSucesso(fase, e.Attempt != dictation.NoAttempt && e.Attempt == rt.repeticaoEm); s != fase {
				fase = s
				args = append(args, "motivo", "repeticao_removida")
			}
			rt.log.Info("winruntime: overlay", args...)
			rt.overlay.SetPhase(fase)

		case dictation.PlayBeepEffect:
			// O bipe diz "pode falar": só sai com o microfone entregando
			// áudio (ADR-015). Ver microfone.go.
			if rt.reterBip(e.Attempt) {
				continue
			}
			rt.tocarBip(e.Attempt)

		case dictation.TranscribeEffect:
			rt.transcrever(e)

		case dictation.ResolveEffect:
			// No Polish, a resposta não é síncrona: volta por rt.polidos
			// (polish.go), e resolver devolve nil.
			if ev := rt.resolver(e); ev != nil {
				next = append(next, ev)
			}

		case dictation.InsertEffect:
			rt.enfileirarInsercao(e)

		case dictation.RecordHistoryEffect:
			rt.recordHistory(e)
		}
	}

	for _, ev := range next {
		rt.dispatch(ev)
	}
}

// pedirCaptura dispara audio.Session.Stop FORA da goroutine de Run.
//
// Stop deixou de devolver na hora em 28-set-2026: ele espera a fala
// calar quando a tecla sobe no meio de uma palavra (audio.Session,
// margem de cauda). São até 200 ms, e só quando há o que salvar — mas
// esperá-los aqui congelaria o laço que consome hook, áudio e pending,
// que é exatamente o defeito recém-corrigido na inserção.
//
// O buffer NÃO volta por rt.pending: guardarAudio escreve em rt.som, que
// transcrever lê na goroutine de Run. Mandar a gravação por um canal
// próprio mantém esse campo com um dono só, em vez de trocar um
// congelamento por uma corrida de dados.
func (rt *Runtime) pedirCaptura(attempt dictation.AttemptID) {
	go func() {
		buf, vad, err := rt.audio.Stop(context.Background(), attempt)
		select {
		case rt.capturas <- capturaPronta{attempt: attempt, buf: buf, vad: vad, err: err}:
		case <-rt.pararCapturas:
		}
	}()
}

// concluirCaptura roda NA GOROUTINE DE RUN. É aqui que o áudio entra no
// slot e o Event nasce.
func (rt *Runtime) concluirCaptura(c capturaPronta) dictation.Event {
	attempt, buf, vad, err := c.attempt, c.buf, c.vad, c.err
	// Os números do microfone saem também quando Stop falhou: "o microfone
	// não abriu a tempo" é justamente quando eles mais importam.
	rt.registrarJanela(attempt, buf.Janela)
	if err != nil {
		rt.log.Warn("winruntime: stop audio falhou", "attempt", attempt, "err", err)
		return dictation.AudioFailedEvent{Attempt: attempt, Reason: dictation.ErrorReasonAudioCaptureFailed}
	}
	// A referência devolvida ao núcleo é só um identificador opaco
	// (dictation.AudioBufferRef, effects.go) — o Machine nunca vê os
	// bytes. Mas o runtime PRECISA guardá-los agora: com ASR real, o
	// TranscribeEffect que vem em seguida é quem lê o áudio.
	//
	// Guardar aqui é o que o 09-security-privacy.md permite (áudio em RAM,
	// liberado ao fim da tentativa) e o que o AC-028 proíbe é outra coisa
	// (áudio em DISCO). O slot único garante que só um áudio existe por
	// vez: ver audioSlot.
	rt.guardarAudio(attempt, buf)
	// A duração vem das amostras que de fato foram capturadas — é o
	// áudio que o ASR vai ouvir, pre-roll e cauda incluídos —, e não do
	// relógio entre apertar e soltar, que não é o que foi transcrito.
	duracao := duracaoDe(buf.Samples, buf.SampleRate)
	rt.audioDaTentativa.attempt, rt.audioDaTentativa.duracao = attempt, duracao

	// Sem fala, o núcleo encerra a tentativa e nada é transcrito
	// (AC-010) — e até 26-set-2026 isso acontecia em silêncio total:
	// "outcome=no_speech" não dizia se o microfone estava mudo ou se o
	// VAD estava recusando fala de verdade. São causas opostas, com
	// correções opostas.
	//
	// O que vai para o log é NÍVEL agregado — pico e RMS em dBFS e a
	// duração —, nunca amostra de áudio. "Nunca gravar áudio bruto em
	// logs" (CLAUDE.md) continua valendo: isto é a leitura de um
	// vu-meter, não o som.
	if !vad.HasSpeech {
		pico, rms := nivelDBFS(buf.Samples)
		rt.log.Warn("winruntime: nada reconhecido como fala",
			"attempt", attempt,
			"amostras", len(buf.Samples),
			"duracao", duracao,
			"pico_dbfs", pico,
			"rms_dbfs", rms,
			"fala_detectada", vad.SpeechDuration,
			"dica", dicaDeNivel(pico))
	}
	// A duração vai ao núcleo, que dá à transcrição um prazo do tamanho do
	// áudio (dictation.PrazoDeTranscricao).
	return dictation.AudioCapturedEvent{
		Attempt:  attempt,
		Audio:    dictation.AudioBufferRef{ID: fmt.Sprintf("attempt-%d", attempt)},
		VAD:      vad,
		Duration: duracao,
	}
}

// startTimer arma, no Clock real, um callback que empurra um
// TimerElapsedEvent para rt.pending quando o tempo chegar.
//
// Diferença deliberada em relação a dictation/fakes/runtime.go.startTimer:
// lá, clock.Fake dispara o callback SINCRONAMENTE dentro de Advance(), na
// mesma goroutine do teste, então Dispatch pode ser chamado direto. Aqui,
// clock.System delega para time.AfterFunc, cujo callback roda numa
// goroutine NOVA, própria do runtime do Go — chamar rt.dispatch
// diretamente dali violaria "único consumidor serializado de
// Machine.Handle" (ADR-010, "Modelo de threads"). Por isso o callback só
// enfileira em rt.pending; quem efetivamente chama Handle continua sendo
// exclusivamente a goroutine de Run.
func (rt *Runtime) startTimer(e dictation.StartTimerEffect) {
	key := timerKey{Attempt: e.Attempt, Kind: e.Kind}
	attempt, kind := e.Attempt, e.Kind
	timer := rt.clk.AfterFunc(e.Duration, func() {
		rt.pending <- dictation.TimerElapsedEvent{Attempt: attempt, Kind: kind}
	})
	rt.timers[key] = timer
}

// cancelTimer para o timer (attempt, kind), se ainda estiver armado. Uma
// corrida entre Stop() e o timer já estar disparando é possível (Stop()
// devolve false nesse caso) — o TimerElapsedEvent que já estava a
// caminho de rt.pending ainda chega e é processado por Run, mas o núcleo
// o descarta sozinho por não ser mais o TimerKind pendente da tentativa
// (ADR-007, seção "onTimerElapsed": "timer cujo Kind não seja o que a
// tentativa está aguardando é obsoleto"). Nenhuma proteção extra é
// necessária aqui.
func (rt *Runtime) cancelTimer(attempt dictation.AttemptID, kind dictation.TimerKind) {
	key := timerKey{Attempt: attempt, Kind: kind}
	if t, ok := rt.timers[key]; ok {
		t.Stop()
		delete(rt.timers, key)
	}
}

// guardarAudio coloca o áudio da tentativa no slot, liberando o que
// estiver lá. Só é chamado da goroutine de Run.
func (rt *Runtime) guardarAudio(attempt dictation.AttemptID, buf audio.AudioBuffer) {
	rt.liberarAudio()
	rt.som = audioSlot{
		attempt: attempt,
		buf:     asr.AudioBuffer{PCM: buf.Samples, SampleRateHz: buf.SampleRate},
		ocupado: true,
	}
}

// liberarAudio descarta o áudio retido. Idempotente.
func (rt *Runtime) liberarAudio() {
	if !rt.som.ocupado {
		return
	}
	rt.som = audioSlot{}
}

// transcrever atende TranscribeEffect chamando o ASR EM OUTRA GOROUTINE.
//
// Assíncrono não é preferência: a transcrição medida custa p50 de 565 ms
// (docs/benchmarks/asr-decisao-2026-09-18.md), e a goroutine de Run é a
// mesma que consome hook.Events(). Chamar o ASR aqui dentro seguraria o
// laço por meio segundo — as teclas que a pessoa digitasse nesse intervalo
// ficariam paradas na fila, e o hook, que tem orçamento de ~300 ms por
// callback, poderia ser removido pelo Windows em silêncio.
//
// O resultado volta por rt.pending, o mesmo caminho que startTimer já usa
// para respostas que chegam de outra goroutine. Quem chama Machine.Handle
// continua sendo exclusivamente a goroutine de Run (ADR-010, "Modelo de
// threads").
//
// O núcleo já arma TimerTranscribe junto com este efeito, então uma
// transcrição que nunca responda não trava a tentativa: o timeout do
// próprio núcleo a encerra (ADR-007, seção 6).
func (rt *Runtime) transcrever(e dictation.TranscribeEffect) {
	if !rt.som.ocupado || rt.som.attempt != e.Attempt {
		// Sem áudio para esta tentativa. Acontece se a captura falhou ou
		// se o slot já foi consumido — tratar como falha de transcrição é
		// melhor que transcrever silêncio, que devolveria texto vazio e
		// pareceria "não entendi nada".
		rt.log.Warn("winruntime: transcrição sem áudio retido", "attempt", e.Attempt)
		rt.pending <- dictation.TranscribeResultEvent{Attempt: e.Attempt, Reason: dictation.ErrorReasonTranscribeFailed}
		return
	}

	buf := rt.som.buf
	attempt := e.Attempt
	rt.liberarAudio() // o slot sai daqui; a goroutine é dona do buffer agora
	// Se o resultado voltar depois do prazo, é por esta anotação que ele
	// acha a linha do histórico da tentativa (tardio.go).
	rt.registrarEmVoo(attempt, buf)
	// O prompt é lido AQUI, na goroutine de Run, e não dentro da goroutine do
	// ASR: o painel troca o prompt com o Agent rodando (aplicarDicionario, na
	// goroutine de Run), e a leitura de dentro seria uma corrida com essa
	// escrita. Copiado aqui, o ditado em voo segue com o prompt com que foi.
	vocab := rt.vocabulario

	go func() {
		res, err := rt.asr.Transcribe(context.Background(), buf,
			asr.ASROptions{Language: "pt-BR", Vocabulary: vocab})
		if err != nil {
			rt.log.Warn("winruntime: transcrição falhou", "attempt", attempt, "err", err)
			rt.pending <- dictation.TranscribeResultEvent{Attempt: attempt, Reason: dictation.ErrorReasonTranscribeFailed}
			return
		}
		// runas: o TAMANHO do que o ASR devolveu, nunca o conteúdo.
		//
		// Sem este número não dá para separar "o whisper repetiu" de "a
		// inserção duplicou" sem repetir o teste várias vezes. Em
		// 28-set-2026 Mauro reportou texto duplicado e a investigação
		// passou por quatro hipóteses porque o log mostrava o tamanho no
		// FIM (text_runes) e no momento da inserção (utf16_units), mas
		// não na origem.
		//
		// Com os três, uma única execução responde onde o texto dobrou.
		//
		// audio é a duração do que foi transcrito: com ela a linha diz
		// sozinha a razão ASR/áudio, e se a janela do encoder foi a curta
		// ou a inteira (local.JanelaRecomendada), sem casar com o
		// whisper-server.log (incidente de 01-out-2026).
		rt.log.Info("winruntime: transcrição concluída",
			"attempt", attempt, "modelo", res.Model,
			"runas", len([]rune(res.Text)), "duracao", res.ProcessingTime,
			"audio", duracaoDe(buf.PCM, buf.SampleRateHz))
		// Antes do envio, e não depois: o envio por rt.pending é o que
		// faz a tentativa andar até o histórico, e a medição tem de já
		// estar lá quando ela chegar.
		rt.asrMedido.Store(&medicaoASR{attempt: attempt, processamento: res.ProcessingTime})
		rt.pending <- dictation.TranscribeResultEvent{
			Attempt: attempt,
			// Confidence fica em zero porque o provider local não fornece
			// (asr.ASRCapabilities.Confidence == false). O DTO do núcleo
			// não tem como marcar "ausente", e inventar um valor aqui
			// alimentaria a policy com um número que ninguém mediu — ver
			// internal/asr/local, Capabilities.
			Result: dictation.ASRResultDTO{Text: res.Text},
		}
	}()
}

// stubResolve atende ResolveEffect devolvendo o texto bruto do ASR sem
// nenhuma política de resolução real (Literal/Smart/Polish) — ADR-010,
// "Escopo do Sprint 1": "Resolve -> STUB nesta sprint (devolve o texto
// bruto)".
// UsarResolver liga a política de resolução real. usuario escopa a busca
// no dicionário pessoal (dictionary.DictionaryQuery.UserID) e PRECISA ser
// o mesmo do dicionário carregado — divergência faz o Search não achar
// nada, e o sintoma é "o dicionário não funciona" em vez de "o dicionário
// nem foi consultado".
//
// Deve ser chamado antes de Run. Sem isto, o Runtime devolve o texto do
// ASR intocado — que é o que ele fez do Sprint 1 até 27-set-2026.
func (rt *Runtime) UsarResolver(res resolver.Resolver, usuario string) {
	rt.resolucao = res
	rt.usuario = usuario
}

// Preferencias é o que a tela de Configurações muda com o Agent rodando.
// Só bool, Duration e string: a struct continua comparável (ADR-016,
// "Consequências").
type Preferencias struct {
	HoldThreshold time.Duration
	BeepEnabled   bool
	// Literal desliga a resolução (modo Literal). Ganha de Polish se os
	// dois vierem ligados.
	Literal bool
	// Polish liga a organização do texto do Smart pelo provedor de IA
	// (ADR-016). Provedor e Modelo são a escolha gravada na tela
	// (ai_provider, ai_model); valem os do keydown do ditado.
	Polish   bool
	Provedor string
	Modelo   string
}

// Ajustar pede que p passe a valer a partir do PRÓXIMO ditado. Seguro de
// qualquer goroutine: só guarda o pedido, e um pedido novo substitui o
// que ainda não foi aplicado.
//
// # Por que no próximo keydown, e só com a máquina parada
//
// O Machine e o modo de texto são da goroutine de Run (ADR-010, consumidor
// único); mexer neles daqui seria corrida de dados. Todo uso deles começa
// num keydown — o limiar é armado ali, o beep sai quando o hold vence, a
// resolução vem depois do ASR —, então aplicar no keydown, antes de
// despachá-lo, é aplicar antes de qualquer uso. E só com a máquina
// parada: um ditado que já começou termina com a configuração com que
// começou, sem meio-termo.
func (rt *Runtime) Ajustar(p Preferencias) { rt.ajuste.Store(&p) }

// aplicarAjuste aplica o pedido de Ajustar, se houver e se a máquina
// estiver parada. Só na goroutine de Run.
func (rt *Runtime) aplicarAjuste() {
	if rt.machine.CurrentAttempt() != dictation.NoAttempt {
		return // no meio de um ditado: o pedido espera o próximo
	}
	p := rt.ajuste.Swap(nil)
	if p == nil {
		return
	}
	if err := rt.machine.AjustarPreferencias(p.HoldThreshold, p.BeepEnabled); err != nil {
		// A API local já valida o limiar e o modo de texto antes de gravar
		// (localapi.validar); chegar aqui é defeito, e o ditado segue com
		// o que valia — TUDO o que o runtime aplica, modo de texto
		// incluído: meio pedido aplicado não é o que ninguém pediu.
		//
		// Com uma exceção, que não passa por aqui: o modo do MICROFONE
		// ("sempre pronto") vale na hora do Salvar — cmd/agent,
		// aplicarPreferencias, o leva direto à audio.Session antes de este
		// pedido chegar ao keydown. Se o núcleo recusar o limiar, o
		// microfone já mudou. É aceito porque a recusa só existe como
		// defeito, e desfazer o microfone aqui daria um segundo dono ao
		// modo dele.
		rt.log.Warn("winruntime: o núcleo recusou o limiar ou o beep; seguem as preferências anteriores", "err", err)
		return
	}
	rt.literal = p.Literal
	rt.polish, rt.provedorIA, rt.modeloIA = p.Polish, p.Provedor, p.Modelo
	// O que ficou valendo, lido do núcleo — não o que foi pedido.
	cfg := rt.machine.Config()
	args := []any{"hold_threshold", cfg.HoldThreshold, "beep", cfg.BeepEnabled, "modo", rt.modo()}
	if rt.modo() == "polish" {
		// A escolha de IA, e nunca a chave, que mora no Gerenciador de
		// Credenciais e é lida a cada ditado.
		args = append(args, "provedor", rt.provedorIA, "modelo", rt.modeloIA)
	}
	rt.log.Info("winruntime: preferências aplicadas", args...)
}

// UsarDominioDoApp liga a tradução de executável em foco para domínio do
// dicionário. Deve ser chamado antes de Run.
//
// É o que torna seguro um alias que colide com palavra comum: "comite" ->
// "commit" é correção certa num editor e ERRO num e-mail sobre o comitê.
// Sem esta função, entrada restrita nunca se aplica — o que é o default
// seguro, não uma falha.
func (rt *Runtime) UsarDominioDoApp(f func(processName string) *string) {
	rt.dominioDoApp = f
}

// UsarVocabulario define os termos que enviesam o decodificador do ASR
// (asr.ASROptions.Vocabulary). Deve ser chamado antes de Run.
func (rt *Runtime) UsarVocabulario(v string) { rt.vocabulario = v }

// limiarResolveLento é quanto tempo um resolve pode levar antes de virar
// aviso no log.
//
// Medido em 27-set-2026 (BenchmarkSmartResolve_DicionarioReal): 2,3 ms
// numa frase de 25 palavras com 43 termos no dicionário. O custo cresce
// com tokens VEZES tamanho do dicionário, e o dicionário cresce a cada
// termo aprendido — então este caminho fica mais lento com o uso, de
// forma gradual e sem causa visível, que é o pior tipo de regressão.
//
// 25 ms é uma ordem de grandeza acima do medido: passar disso significa
// que o dicionário cresceu a ponto de merecer índice, não que o ditado
// quebrou.
const limiarResolveLento = 25 * time.Millisecond

// resolver aplica a política de resolução ao texto do ASR.
//
// Roda no laço de eventos, e não em outra goroutine como a transcrição:
// o hook de teclado entrega por canal e não espera este laço (o orçamento
// de ~300 ms por callback é do Decider, não daqui), e 2,3 ms medidos não
// justificam a complexidade de um caminho assíncrono a mais.
//
// Falha de resolução NUNCA perde o ditado: o texto do ASR segue para
// inserção. Um dicionário com problema tem de degradar para "sem
// correção", nunca para "sem texto".
//
// # O Polish
//
// Com o Polish escolhido, o Smart continua rodando AQUI, e só a metade de
// rede sai do laço (polir, em polish.go): resolver devolve nil, e o
// ResolveResultEvent chega depois, por rt.polidos. Se o Smart falhar, o
// Polish nem é tentado: o texto do ASR segue, como nos outros modos.
func (rt *Runtime) resolver(e dictation.ResolveEffect) dictation.Event {
	// O começo da resolução é a âncora do prazo do Polish: o teto (3,5 s no
	// texto curto) já desconta o Smart, e o TimerResolveTimeout do núcleo,
	// que vem no mesmo lote logo depois deste efeito, só começa a contar
	// depois dele. Ver prazoPolish.
	inicio := time.Now()
	bruto := e.ASRResult.Text
	rt.brutoDaTentativa.attempt, rt.brutoDaTentativa.texto = e.Attempt, rt.brutoParaOHistorico(e.Attempt, bruto)
	if rt.resolucao == nil || rt.literal {
		return dictation.ResolveResultEvent{
			Attempt: e.Attempt,
			Result:  dictation.ResolveResultDTO{Text: bruto},
		}
	}

	// O domínio do app em foco RESTRINGE o que o dicionário pode
	// corrigir — nunca amplia. Desconhecido deixa de fora justamente as
	// entradas perigosas.
	var dominio *string
	if rt.dominioDoApp != nil {
		dominio = rt.dominioDoApp(e.Focus.ProcessName)
	}

	req := resolver.ResolveRequest{
		UserID:    rt.usuario,
		ASRResult: asr.ASRResult{Text: bruto, Language: "pt-BR"},
		Domain:    dominio,
		// CloudPolicy fica no zero value (CloudDisabled): o ditado local
		// não pode depender de rede. AC-030. O Polish é outro caminho,
		// com opt-in próprio (ADR-016), e não passa por aqui.
	}
	out, err := rt.resolucao.Resolve(context.Background(), req)
	decorrido := time.Since(inicio)

	if err != nil {
		rt.log.Warn("winruntime: resolucao falhou, seguindo com o texto do ASR",
			"attempt", e.Attempt, "err", err)
		return dictation.ResolveResultEvent{
			Attempt: e.Attempt,
			Result:  dictation.ResolveResultDTO{Text: bruto},
		}
	}
	if decorrido > limiarResolveLento {
		rt.log.Warn("winruntime: resolucao lenta — o dicionario pode ter crescido a ponto de merecer indice",
			"attempt", e.Attempt, "duracao", decorrido, "limiar", limiarResolveLento)
	}

	// Log sem o TEXTO: quantas substituições e quantos spans ficaram
	// incertos diz o que é preciso para diagnosticar, e transcript
	// completo em log é proibido (CLAUDE.md, "Safety rules"; AC-027).
	if len(out.AppliedReplacements) > 0 || len(out.Warnings) > 0 {
		// O domínio vai no log; o nome do app NÃO — nome de janela e de
		// processo são contexto, e contexto em log é proibido
		// (CLAUDE.md, "Safety rules"; AC-027). "dev" diz o que é preciso
		// para diagnosticar sem dizer onde a pessoa estava.
		rt.log.Info("winruntime: resolucao aplicada",
			"attempt", e.Attempt,
			"substituicoes", len(out.AppliedReplacements),
			"spans_incertos", len(out.UncertainSpans),
			"dominio", valorOuDesconhecido(dominio),
			"duracao", decorrido,
			"avisos", out.Warnings)
	}
	if rt.polish {
		rt.polir(e.Attempt, req, out, inicio)
		return nil
	}
	return dictation.ResolveResultEvent{
		Attempt: e.Attempt,
		Result:  dictation.ResolveResultDTO{Text: out.FinalText},
	}
}

// valorOuDesconhecido rende um *string para log sem vazar ponteiro nem
// imprimir "<nil>", que se lê como defeito em vez de como informação.
func valorOuDesconhecido(v *string) string {
	if v == nil {
		return "desconhecido"
	}
	return *v
}
