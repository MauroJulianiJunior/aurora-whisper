//go:build windows

package hook

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// wmQuit é o WM_QUIT do Win32. Não é reexportado por internal/agent/win32
// (que só expõe as mensagens de teclado que o hook precisa decodificar),
// então é definido aqui: é este pacote quem posta WM_QUIT para encerrar a
// thread do hook (ver Stop).
const wmQuit uint32 = 0x0012

// wmMouse é a mensagem de thread com que GravacaoSolta pede à thread do hook
// que instale (wParam 1) ou retire (wParam 0) o hook do mouse. Na faixa
// WM_APP (0x8000 em diante), que o Windows deixa para o próprio programa.
const wmMouse uint32 = 0x8000 + 1

// kernel32!GetTickCount não tem wrapper em golang.org/x/sys/windows nem em
// internal/agent/win32 (que só cobre user32/gdi32 — ver seu doc.go), então
// é resolvido aqui do mesmo jeito que win32 resolve user32/gdi32: por
// NewLazySystemDLL/NewProc, sem reimplementar a fiação de convenção de
// chamada por cima da stdlib (ADR-010, D3).
var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procGetTickCount = kernel32.NewProc("GetTickCount")
)

// getTickCount lê o contador de ticks do sistema (millisegundos desde o
// boot, 32 bits, com overflow — ver win32.TickReference).
func getTickCount() uint32 {
	r1, _, _ := procGetTickCount.Call()
	return uint32(r1)
}

// Start instala o hook global WH_KEYBOARD_LL numa thread dedicada,
// travada com runtime.LockOSThread(), rodando o loop
// GetMessageW/TranslateMessage/DispatchMessageW que o WH_KEYBOARD_LL
// exige (ADR-010, "Modelo de threads": mensagem só chega na thread que
// instalou o hook). Bloqueia até o hook estar instalado ou a instalação
// falhar.
func (h *Hook) Start() error {
	// Instalar um WH_KEYBOARD_LL é ação no sistema real: o hook passa a ver
	// e a poder suprimir cada tecla que a pessoa digita, em qualquer
	// aplicativo. Nenhum teste tem o que fazer com isso (ADR-011, decisão 6).
	if err := sysinput.Guard(); err != nil {
		return fmt.Errorf("hook: %w", err)
	}

	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return fmt.Errorf("hook: already started")
	}
	h.started = true
	h.done = make(chan struct{})
	h.mu.Unlock()

	ready := make(chan error, 1)
	go h.run(ready)

	if err := <-ready; err != nil {
		h.mu.Lock()
		h.started = false
		h.mu.Unlock()
		return err
	}
	return nil
}

// Stop pede o encerramento da thread do hook (PostThreadMessageW com
// WM_QUIT — a única forma segura de acordar o GetMessageW de outra
// goroutine) e espera ela terminar, o que inclui o
// UnhookWindowsHookEx feito pela própria thread do hook antes de sair.
// É seguro chamar Stop múltiplas vezes ou sem Start prévio (no-op).
func (h *Hook) Stop() error {
	h.mu.Lock()
	if !h.started {
		h.mu.Unlock()
		return nil
	}
	h.started = false
	threadID := h.threadID
	done := h.done
	h.mu.Unlock()

	if err := win32.PostThreadMessageW(threadID, wmQuit, 0, 0); err != nil {
		return fmt.Errorf("hook: PostThreadMessageW(WM_QUIT): %w", err)
	}
	<-done
	return nil
}

// run é o corpo inteiro da thread do hook: trava a OS thread, ancora o
// relógio de ticks, instala o hook, publica prontidão (ou erro) em ready,
// e roda o loop de mensagens até WM_QUIT ou erro de fila. Escreve
// h.threadID, h.tickRef e h.handle antes de mandar algo em ready — o
// receive de ready em Start() estabelece happens-before para quem lê
// esses campos depois (callback, na própria goroutine; Stop, cujo
// contrato exige ser chamado depois que Start retornou).
func (h *Hook) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(h.done)

	h.threadID = windows.GetCurrentThreadId()
	h.tickRef = win32.NewTickReference(getTickCount(), time.Now())

	proc := win32.NewLowLevelKeyboardHookProc(h.callback)
	handle, err := win32.SetWindowsHookExW(win32.WHKeyboardLL, proc, 0, 0)
	if err != nil {
		ready <- fmt.Errorf("hook: SetWindowsHookExW: %w", err)
		return
	}
	h.handle = handle
	ready <- nil

	var msg win32.MSG
	for {
		r, err := win32.GetMessageW(&msg, 0, 0, 0)
		if r == 0 {
			break // WM_QUIT
		}
		if r == -1 {
			_ = err // fila de mensagens com erro: encerra o loop de qualquer forma
			break
		}
		if msg.HWnd == 0 && msg.Message == wmMouse {
			h.trocarMouse(msg.WParam != 0)
			continue
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessageW(&msg)
	}

	// O hook do mouse nunca sobra: se o Agent fecha no meio de uma gravação
	// solta, ele sai junto com o de teclado.
	h.trocarMouse(false)
	win32.UnhookWindowsHookEx(h.handle)
}

// pedirMouse pede à thread do hook que instale ou retire o hook do mouse. Só
// manda a mensagem; quem instala é trocarMouse, lá. Sem hook de teclado
// rodando (os testes nunca o iniciam), não há thread para pedir, e nada
// acontece.
func (h *Hook) pedirMouse(ligar bool) error {
	h.mu.Lock()
	started, threadID := h.started, h.threadID
	h.mu.Unlock()
	if !started {
		return nil
	}
	var w uintptr
	if ligar {
		w = 1
	}
	if err := win32.PostThreadMessageW(threadID, wmMouse, w, 0); err != nil {
		return fmt.Errorf("hook: PostThreadMessageW(wmMouse): %w", err)
	}
	return nil
}

// trocarMouse instala ou retira o hook do mouse (WH_MOUSE_LL) da gravação
// solta (ADR-025, D.1). Roda na thread do hook de teclado, que já tem o laço
// de mensagens que um hook de baixo nível exige, e por isso os dois callbacks
// nunca rodam ao mesmo tempo.
//
// O trampolim é criado uma vez só, na primeira instalação: windows.NewCallback
// nunca libera o que aloca (ver win32.NewLowLevelKeyboardHookProc), e criar um
// por gravação solta esgotaria a tabela.
func (h *Hook) trocarMouse(ligar bool) {
	if ligar == (h.mouse != 0) {
		return
	}
	if !ligar {
		_ = win32.UnhookWindowsHookEx(h.mouse)
		h.mouse = 0
		return
	}
	// Instalar um WH_MOUSE_LL é ação no sistema real: o hook passa a ver cada
	// clique da máquina. Mesmo portão do de teclado (ADR-025, D.3).
	if err := sysinput.Guard(); err != nil {
		h.erroDoMouse.Store(&err)
		return
	}
	if h.mouseProc == 0 {
		h.mouseProc = win32.NewLowLevelMouseHookProc(h.callbackDoMouse)
	}
	m, err := win32.SetWindowsHookExW(win32.WHMouseLL, h.mouseProc, 0, 0)
	if err != nil {
		h.erroDoMouse.Store(&err)
		return
	}
	h.mouse = m
}

// callbackDoMouse é o LowLevelMouseProc da gravação solta. Faz o mínimo, como
// o de teclado (ADR-010): olha só se o botão esquerdo desceu, avisa por canal
// sem bloquear e repassa. Nunca suprime: o clique segue para o app, que põe o
// cursor no campo (ADR-025, B.2). Nunca lê lParam: a posição, a janela e o
// que foi clicado não passam por aqui (ADR-025, F).
func (h *Hook) callbackDoMouse(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode >= 0 && wParam == uintptr(win32.WMLButtonDown) {
		select {
		case h.cliques <- struct{}{}:
		default:
			// Já há um clique esperando o runtime: este não diz nada a mais.
		}
	}
	return win32.CallNextHookEx(h.mouse, nCode, wParam, lParam)
}

// callback é o LowLevelKeyboardProc instalado por run(). Roda inteiramente
// na thread do hook, dentro do orçamento de ~300ms do
// LowLevelHooksTimeout (ADR-010): só traduz, decide e manda por canal de
// forma não bloqueante — nunca I/O, nunca alocação pesada, nunca lock,
// nunca UI Automation.
func (h *Hook) callback(nCode int32, wParam uintptr, info *win32.KBDLLHOOKSTRUCT) uintptr {
	// lParam original, reconstruído a partir do ponteiro já decodificado
	// por win32.NewLowLevelKeyboardHookProc, para poder repassar a
	// CallNextHookEx sem reinterpretar a struct outra vez.
	lParam := uintptr(unsafe.Pointer(info))

	if nCode < 0 {
		// Regra do próprio Win32 (ver o comentário de
		// win32.LowLevelKeyboardProc): nCode < 0 nunca inspeciona info,
		// só repassa.
		return win32.CallNextHookEx(h.handle, nCode, wParam, lParam)
	}

	down := wParam == uintptr(win32.WMKeyDown) || wParam == uintptr(win32.WMSysKeyDown)
	at := h.tickRef.ToTime(info.Time)
	ev := h.translator.Translate(info.VkCode, down, info.Injected(), info.DwExtraInfo, at)

	// Pausado: a tecla passa direto. Traduzir continua acontecendo ACIMA,
	// de propósito — é o Translator que acompanha Ctrl/Win/Shift, e
	// pular a tradução deixaria o estado dos modificadores errado quando a
	// pausa acabasse.
	if h.deveIgnorar() {
		return win32.CallNextHookEx(h.handle, nCode, wParam, lParam)
	}

	decision, ev := h.decidir(ev)
	if decision.Notify {
		select {
		case h.events <- ev:
		default:
			// Canal cheio: o consumidor está atrasado. Descartar aqui é
			// a única opção que respeita o orçamento de ~300ms — bloquear
			// derrubaria o hook (ADR-010, "Modelo de threads").
		}
	}

	if decision.Suppress {
		return 1
	}
	return win32.CallNextHookEx(h.handle, nCode, wParam, lParam)
}

// SynthesizeTap injeta, via SendInput(KEYEVENTF_UNICODE), o caractere
// correspondente à tecla configurada (keydown seguido de keyup),
// marcando ambos os eventos com a marca deste processo (marker.go) ANTES
// de injetar — para que, quando voltarem pelo próprio hook, o Translator
// os reconheça como FromThisProcess e o Decider nunca os reinterprete
// como um novo keydown/keyup físico da tecla de ditado (ADR-007, seção 3;
// AC-001: tap curto digita a tecla normalmente).
func (h *Hook) SynthesizeTap() error {
	// Injeta tecla no sistema real: passa pelo portão (ADR-011, decisão 6).
	// Este caminho é do Sprint 1 e ficou de fora quando o portão foi criado
	// — a auditoria do Sprint 2 pegou. Enquanto ele existisse sem guarda, a
	// afirmação do pacote sysinput de ser "o portão único para SendInput"
	// era simplesmente falsa.
	if err := sysinput.Guard(); err != nil {
		return fmt.Errorf("hook: %w", err)
	}

	char, ok := keyToChar[h.cfg.Key]
	if !ok {
		return fmt.Errorf("hook: no character mapping for key %d", h.cfg.Key)
	}

	down := win32.INPUT{
		Type: win32.InputKeyboard,
		Ki: win32.KEYBDINPUT{
			WScan:       char,
			DwFlags:     win32.KeyEventFUnicode,
			DwExtraInfo: markerExtraInfo,
		},
	}
	up := down
	up.Ki.DwFlags |= win32.KeyEventFKeyUp

	n, err := win32.SendInput([]win32.INPUT{down, up})
	if n != 2 {
		if err == nil {
			err = fmt.Errorf("hook: SendInput injected %d/2 events", n)
		}
		return err
	}
	return nil
}
