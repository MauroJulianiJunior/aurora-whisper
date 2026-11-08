package hook

// Este arquivo não tem build tag: Hook só guarda dados (dictation.Decider,
// Translator, o canal de eventos, win32.HHOOK, win32.TickReference — todos
// tipos sem chamada de sistema embutida) e a lógica compartilhada de
// configuração. O comportamento que de fato chama para dentro do Win32
// (Start/Stop/SynthesizeTap) está em hook_windows.go / hook_other.go, no
// par exigido pelo CI multiplataforma (ver doc.go deste pacote e o doc.go
// de internal/agent/win32).

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// defaultEventBuffer é a capacidade do canal de eventos quando
// Config.EventBuffer não é informado. Pequeno de propósito: o envio no
// callback do hook é sempre não bloqueante (select/default) — um buffer
// maior não evita perda sob um consumidor lento, só atrasa quando ela
// aparece. O valor só precisa absorver rajadas curtas (ex.: keydown e
// keyup em sequência rápida antes do consumidor acordar).
const defaultEventBuffer = 16

// Config configura um Hook.
type Config struct {
	// Key é a tecla principal do ditado — a mesma usada para construir o
	// dictation.Decider e o dictation.Machine do restante do runtime.
	// Não deve ser uma tecla modificadora (dictation.Config.Validate já
	// garante isso, AC-004; Hook não revalida).
	//
	// KeyUnknown significa que o gatilho é a COMBINAÇÃO em Modifiers, sem
	// tecla principal (ADR-013).
	Key dictation.KeyCode

	// Modifiers é o conjunto exigido quando Key == KeyUnknown.
	Modifiers dictation.Modifiers

	// Nota é o conjunto do gatilho "ditar para nota" (ADR-022; padrão
	// Ctrl+Win+Alt). Zero desliga: o hook é o de antes, evento por evento. A
	// sessão do gatilho da nota chega ao runtime com RawKeyEvent.ParaNota
	// (dictation.Gatilhos). Deve ter passado por
	// dictation.ValidarGatilhoDaNota; Hook não revalida.
	Nota dictation.Modifiers

	// EventBuffer é a capacidade do canal devolvido por Events(). Zero
	// usa defaultEventBuffer.
	EventBuffer int
}

// Hook instala e opera o hook global WH_KEYBOARD_LL, alimentando o
// dictation.Decider da tecla configurada (ADR-010, escopo do Sprint 1;
// ADR-007, seção 2).
//
// Start e Stop devem ser chamados pelo mesmo goroutine "dono" do Hook,
// nunca concorrentemente entre si. Events() e SynthesizeTap() são seguros
// de qualquer goroutine depois que Start() retorna com sucesso.
type Hook struct {
	cfg        Config
	decider    *dictation.Gatilhos
	translator *Translator
	events     chan dictation.RawKeyEvent

	// pausado é escrito pela bandeja (outra thread) e lido no callback do
	// hook — por isso atômico, e nunca um lock: o callback tem orçamento
	// de ~300 ms e não pode esperar ninguém (ADR-010).
	pausado atomic.Bool

	// solta diz se a gravação solta está ligada (ADR-025, D). Escrita pelo
	// runtime (GravacaoSolta) e lida no callback, sem lock, pelo mesmo motivo
	// de pausado.
	solta atomic.Bool
	// esc decide o Esc da gravação solta. Só a thread do hook o usa, como o
	// decider.
	esc dictation.Esc
	// cliques leva ao runtime o clique esquerdo visto pelo hook do mouse.
	// Buffer 1, com envio que não bloqueia: um clique basta para parar, e os
	// seguintes não dizem nada a mais.
	cliques chan struct{}
	// erroDoMouse é a falha da última instalação do hook do mouse, escrita pela
	// thread do hook e devolvida por GravacaoSolta quando a gravação solta
	// desliga. Nil: instalou, ou nem tentou.
	erroDoMouse atomic.Pointer[error]
	// mouse e mouseProc são o hook do mouse e o trampolim dele. Só a thread do
	// hook lê e escreve: ela instala, retira e roda o callback.
	mouse     win32.HHOOK
	mouseProc uintptr

	// mu protege apenas o ciclo de vida (started/done) contra chamadas
	// concorrentes indevidas a Start/Stop. handle, threadID e tickRef são
	// escritos uma única vez, na goroutine da thread do hook, antes de
	// serem publicados ao chamador de Start através do canal ready — dali
	// em diante só são lidos pela própria goroutine do hook (callback) ou
	// por Stop (chamado, por contrato, pelo goroutine dono, depois que
	// Start já retornou — happens-before já estabelecido pelo canal
	// ready). Não precisam de proteção adicional.
	mu      sync.Mutex
	started bool
	done    chan struct{}

	handle   win32.HHOOK
	threadID uint32
	tickRef  win32.TickReference
}

// New cria um Hook para cfg.Key, ainda não instalado — chame Start.
func New(cfg Config) *Hook {
	if cfg.EventBuffer <= 0 {
		cfg.EventBuffer = defaultEventBuffer
	}
	return &Hook{
		cfg:        cfg,
		decider:    novoDecider(cfg),
		translator: NewTranslator(),
		events:     make(chan dictation.RawKeyEvent, cfg.EventBuffer),
		cliques:    make(chan struct{}, 1),
	}
}

// Events devolve o canal de dictation.RawKeyEvent que o Decider decidiu
// notificar (Decision.Notify == true) — keydown/keyup da tecla configurada,
// já filtrados de laço de auto-injeção e de atalhos com modificador
// (ADR-007, seção 3). Cabe ao runtime externo, fora da thread do hook,
// consumir este canal, atribuir AttemptID e FocusTarget, e montar o
// dictation.KeyDownEvent/KeyUpEvent que alimenta Machine.Handle — este
// pacote não conhece Machine nem FocusTarget.
//
// O canal nunca é fechado por Stop(); ele simplesmente para de receber.
func (h *Hook) Events() <-chan dictation.RawKeyEvent {
	return h.events
}

// Cliques devolve o canal do clique esquerdo durante a gravação solta
// (ADR-025, B.2). Fora dela não há hook do mouse, e nada chega aqui. O canal
// não leva posição, janela nem botão: só que houve um clique.
func (h *Hook) Cliques() <-chan struct{} {
	return h.cliques
}

// GravacaoSolta liga ou desliga o que só existe durante a gravação solta
// (ADR-025, D): o Esc que descarta e o hook do mouse. O runtime chama no
// dictation.GravacaoSoltaEffect. Não espera a thread do hook: o pedido vai
// por mensagem, e a instalação acontece lá.
//
// Ao desligar, devolve o erro da instalação do hook do mouse, se ela falhou:
// a gravação funcionou sem o clique, e o runtime registra isso.
//
// Seguro de qualquer goroutine.
func (h *Hook) GravacaoSolta(ligada bool) error {
	h.solta.Store(ligada)
	err := h.pedirMouse(ligada)
	if ligada {
		return err
	}
	if p := h.erroDoMouse.Swap(nil); p != nil && err == nil {
		err = fmt.Errorf("hook: o hook do mouse não foi instalado: %w", *p)
	}
	return err
}

// Pausar liga ou desliga o ditado sem desinstalar o hook. Pausado, a
// tecla de ditado volta a ser uma tecla comum: digita normalmente, e
// nenhum evento chega ao núcleo.
//
// A pausa vale para o PRÓXIMO toque. Um toque já em curso termina
// normalmente — ver dictation.Decider.Armado para o porquê.
//
// Seguro de qualquer goroutine.
func (h *Hook) Pausar(p bool) { h.pausado.Store(p) }

// Pausado reporta o estado atual da pausa.
func (h *Hook) Pausado() bool { return h.pausado.Load() }

// deveIgnorar decide se o callback deixa o evento passar sem consultar o
// decisor. Fora da build tag do Windows para ser testável na suíte
// normal: é a única decisão da pausa, e é onde mora o caso de borda.
//
// A pausa vale para a próxima sessão, e três coisas são sessão em curso: o
// toque armado, a gravação solta (o toque que a para tem de chegar ao
// núcleo) e o Esc engolido (a soltura dele tem de ser engolida também, ou o
// Windows fica com o Esc preso).
func (h *Hook) deveIgnorar() bool {
	return h.pausado.Load() && !h.decider.Armado() && !h.solta.Load() && !h.esc.Preso()
}

// decidir consulta o decisor e devolve, junto com a decisão, o evento como ele
// vai ao runtime: com a marca do gatilho da nota (RawKeyEvent.ParaNota), que o
// runtime não tem como saber sozinho — ele não conhece o decisor. Fora da build
// tag do Windows para ser testável: é a única coisa que o callback faz com o
// evento além de mandá-lo.
func (h *Hook) decidir(ev dictation.RawKeyEvent) (dictation.Decision, dictation.RawKeyEvent) {
	d := h.decider.Decide(ev)
	ev.ParaNota = d.Notify && d.ParaNota
	ev.Limpo = d.Notify && d.Limpo
	// O Esc da gravação solta (ADR-025, B.4) é decidido depois do gatilho, que
	// também vê o Esc: é assim que um Ctrl+Win+Esc deixa o toque sujo. Os dois
	// nunca notificam o mesmo evento. O gatilho por combinação só notifica ao
	// completar ou desfazer o conjunto, num modificador; o Esc só sem
	// modificador nenhum, e só com a gravação solta, que não existe com tecla
	// de letra.
	if e := h.esc.Decide(ev, h.solta.Load()); e.Suppress || e.Notify {
		ev.Descartar = e.Notify
		return e, ev
	}
	return d, ev
}

// novoDecider escolhe o modo do gatilho a partir da Config.
//
// Key == KeyUnknown significa gatilho por COMBINAÇÃO de modificadores
// (ADR-013): não há tecla principal, nada é suprimido, e a classe de
// defeito da tecla de letra — engolir o caractere e devolver depois, fora
// de ordem — deixa de existir.
//
// O decisor do ditado vai dentro do dos dois gatilhos (dictation.Gatilhos),
// que acrescenta só a marca do "ditar para nota": as decisões de suprimir e
// notificar continuam as do ditado.
func novoDecider(cfg Config) *dictation.Gatilhos {
	if cfg.Key == dictation.KeyUnknown {
		return dictation.NovosGatilhos(dictation.NewDeciderModificadores(cfg.Modifiers), cfg.Nota)
	}
	return dictation.NovosGatilhos(dictation.NewDecider(cfg.Key), cfg.Nota)
}
