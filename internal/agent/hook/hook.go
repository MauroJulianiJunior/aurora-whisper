package hook

// Este arquivo não tem build tag: Hook só guarda dados (dictation.Decider,
// Translator, o canal de eventos, win32.HHOOK, win32.TickReference — todos
// tipos sem chamada de sistema embutida) e a lógica compartilhada de
// configuração. O comportamento que de fato chama para dentro do Win32
// (Start/Stop/SynthesizeTap) está em hook_windows.go / hook_other.go, no
// par exigido pelo CI multiplataforma (ver doc.go deste pacote e o doc.go
// de internal/agent/win32).

import (
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
	decider    *dictation.Decider
	translator *Translator
	events     chan dictation.RawKeyEvent

	// pausado é escrito pela bandeja (outra thread) e lido no callback do
	// hook — por isso atômico, e nunca um lock: o callback tem orçamento
	// de ~300 ms e não pode esperar ninguém (ADR-010).
	pausado atomic.Bool

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
func (h *Hook) deveIgnorar() bool {
	return h.pausado.Load() && !h.decider.Armado()
}

// novoDecider escolhe o modo do gatilho a partir da Config.
//
// Key == KeyUnknown significa gatilho por COMBINAÇÃO de modificadores
// (ADR-013): não há tecla principal, nada é suprimido, e a classe de
// defeito da tecla de letra — engolir o caractere e devolver depois, fora
// de ordem — deixa de existir.
func novoDecider(cfg Config) *dictation.Decider {
	if cfg.Key == dictation.KeyUnknown {
		return dictation.NewDeciderModificadores(cfg.Modifiers)
	}
	return dictation.NewDecider(cfg.Key)
}
