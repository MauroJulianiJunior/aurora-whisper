//go:build windows

package uia

import (
	"fmt"
	"runtime"
	"time"

	"golang.org/x/sys/windows"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// eventObjectFocus é o EVENT_OBJECT_FOCUS do Win32 (winuser.h) — o único
// evento que este rastreador escuta (ADR-011, decisão 2).
const eventObjectFocus uint32 = 0x8005

const (
	// winEventOutOfContext: o callback roda na thread que chamou
	// SetWinEventHook (esta, dedicada), sem exigir que pfnWinEventProc
	// esteja numa DLL carregável por outros processos.
	winEventOutOfContext uint32 = 0x0000
	// winEventSkipOwnProcess: não notifica trocas de foco causadas pelo
	// próprio processo do Agent (ex.: o overlay, que é NOACTIVATE e não
	// deveria roubar foco de qualquer forma, mas o filtro custa nada e
	// evita ruído). O filtro vale para o evento, não para a reinspeção,
	// que pergunta à UIA pelo foco atual — ver cicloFoco.concluir,
	// "Reinspeção que cai no próprio Agent".
	winEventSkipOwnProcess uint32 = 0x0002
)

// wmQuit é o WM_QUIT do Win32. Não reexportado por internal/agent/win32
// (ver seu doc.go) — redefinido aqui como já é feito em
// internal/agent/hook/hook_windows.go.
const wmQuit uint32 = 0x0012

// Start instala a thread dedicada de rastreamento de foco (ADR-011,
// decisão 2): trava a OS thread, inicializa COM e a instância de UI
// Automation (CUIAutomation8 com TetoChamadaUIA, ou o CUIAutomation
// antigo — ver Automacao), e instala SetWinEventHook(EVENT_OBJECT_FOCUS).
// Bloqueia até a instalação terminar (com sucesso ou erro).
//
// A subida inteira passa por sysinput.Guard() antes de qualquer chamada
// de sistema — mesmo portão que gates SendInput e escrita de clipboard
// (ADR-011, decisão 6): sob "go test" ela é sempre recusada, então
// nenhum teste desta suíte cria objeto de UI Automation nem instala hook
// de sistema de verdade, mesmo que exercite este caminho. A leitura de
// propriedades da UIA que a thread faz depois de
// instalada (inspectFocusedElement) não passa pelo portão — não injeta
// entrada nem altera estado global do sistema (ver doc.go). O timer da
// reinspeção também não: é um timer da própria thread, sem janela, e só
// uma inspeção o cria — o que só acontece depois de o hook ter passado
// pelo portão.
func (t *Tracker) Start() error {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return fmt.Errorf("uia: tracker already started")
	}
	t.started = true
	t.done = make(chan struct{})
	t.mu.Unlock()

	ready := make(chan error, 1)
	go t.run(ready)

	if err := <-ready; err != nil {
		t.mu.Lock()
		t.started = false
		t.mu.Unlock()
		return err
	}
	return nil
}

// Stop pede o encerramento da thread dedicada (PostThreadMessageW com
// WM_QUIT — a única forma segura de acordar o GetMessageW de outra
// goroutine) e espera ela terminar, o que inclui o UnhookWinEvent feito
// pela própria thread antes de sair. Seguro chamar múltiplas vezes ou sem
// Start prévio (no-op) — mesmo contrato de internal/agent/hook.Hook.Stop.
func (t *Tracker) Stop() error {
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()
		return nil
	}
	t.started = false
	threadID := t.threadID
	done := t.done
	t.mu.Unlock()

	if err := win32.PostThreadMessageW(threadID, wmQuit, 0, 0); err != nil {
		return fmt.Errorf("uia: PostThreadMessageW(WM_QUIT): %w", err)
	}
	<-done
	return nil
}

// run é o corpo inteiro da thread dedicada: trava a OS thread, inicializa
// COM e a IUIAutomation, instala o hook (atrás do portão), publica
// prontidão (ou erro) em ready, e roda o loop de mensagens até WM_QUIT ou
// erro de fila — mesma forma de internal/agent/hook/hook_windows.go:run.
func (t *Tracker) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)

	// O portão vem antes de tudo — antes do COM, do objeto de UI
	// Automation (e dos tetos que se pedem a ele) e do hook. Até
	// 29-set-2026 ele vinha só antes do hook, e o objeto da UIA já tinha
	// sido criado; com o CUIAutomation8 a subida passou a fazer mais
	// chamadas COM, e nenhuma delas deve rodar sob "go test".
	if err := sysinput.Guard(); err != nil {
		ready <- fmt.Errorf("uia: %w", err)
		return
	}

	comOwned, err := coInitialize()
	if err != nil {
		ready <- fmt.Errorf("uia: %w", err)
		return
	}
	defer func() {
		if comOwned {
			windows.CoUninitialize()
		}
	}()

	auto, info, err := newAutomation()
	if err != nil {
		ready <- fmt.Errorf("uia: newAutomation: %w", err)
		return
	}
	defer auto.release()
	t.automacao = info

	t.threadID = windows.GetCurrentThreadId()

	// As chamadas de sistema da inspeção: a consulta UIA e o timer da
	// reinspeção. O que fazer com o resultado (publicar, agendar, relatar)
	// é inspecionarEPublicar, em tracker.go, sem build tag.
	//
	// # Por que um timer da thread, e não PostThreadMessageW
	//
	// Mensagem de thread (hwnd NULL) some em qualquer laço modal: o
	// DispatchMessage do laço não tem janela para onde mandá-la (Raymond
	// Chen, "Thread messages are eaten by modal loops"). Se a espera de
	// uma chamada COM desta thread bombear mensagens, a reinspeção se
	// perderia em silêncio. O WM_TIMER de SetTimer com hWnd NULL e um
	// TimerProc não tem esse problema: DispatchMessage chama o TimerProc
	// que vem no lParam, seja qual for o laço que o despachou. Sem janela
	// nenhuma, e só nesta thread.
	//
	// timerID e sis só são tocados por esta thread.
	var (
		sis     sistema
		timerID uintptr
	)
	// O nível de integridade do próprio Agent, lido uma vez: é contra ele
	// que a janela em primeiro plano é comparada depois de uma inspeção sem
	// elemento (lerPrimeiroPlano). GetCurrentProcessToken é o pseudo-handle
	// do token deste processo — não se fecha.
	ilAgent, okAgent := nivelDeIntegridade(windows.GetCurrentProcessToken())
	timerProc := windows.NewCallback(func(hwnd uintptr, msg uint32, idEvent uintptr, dwTime uint32) uintptr {
		// Um disparo só: o timer do Win32 é periódico.
		killThreadTimer(idEvent)
		if idEvent != timerID {
			// WM_TIMER de um timer já substituído: KillTimer não tira da
			// fila o que já estava lá. A reinspeção de agora é a do
			// timer atual.
			return 0
		}
		timerID = 0
		t.reinspecionar(sis)
		return 0
	})
	sis = sistema{
		inspecionar: func() Snapshot {
			return inspectFocusedElement(auto, t.cfg.InspectBudget)
		},
		agora: time.Now,
		agendar: func(espera time.Duration) bool {
			if timerID != 0 {
				killThreadTimer(timerID) // uma reinspeção pendente por vez
				timerID = 0
			}
			timerID = setThreadTimer(espera, timerProc)
			return timerID != 0
		},
		// O vigia não é chamada de sistema: é um time.AfterFunc do Go, que
		// só troca o ponteiro do cache por CAS (ver inspecionarEPublicar).
		vigiar: vigiarComTimer,
		primeiroPlano: func() primeiroPlano {
			return lerPrimeiroPlano(ilAgent, okAgent)
		},
	}

	// O callback roda nesta mesma thread (WINEVENT_OUTOFCONTEXT: o
	// próprio Win32 entrega o evento via GetMessageW/DispatchMessageW
	// desta thread) — é exatamente "fora do caminho do teclado" (ADR-011,
	// decisão 2): nenhuma chamada UIA acontece na thread do hook de
	// teclado (internal/agent/hook), só aqui.
	//
	// Cada chamada abre uma geração nova (inspecionarEPublicar). Sem
	// reentrância, os eventos são processados em ordem e a guarda por
	// geração não descarta nada; ela existe para o caso em que a espera de
	// uma chamada COM entre processos bombeia mensagens e este callback
	// roda de novo DENTRO da inspeção anterior (ver devePublicar).
	callback := windows.NewCallback(func(hWinEventHook uintptr, event uint32, hwnd uintptr, idObject, idChild int32, idEventThread, dwmsEventTime uint32) uintptr {
		if event != eventObjectFocus {
			return 0
		}
		t.inspecionarEPublicar(sis, 0, false)
		return 0
	})

	handle, err := setWinEventHook(eventObjectFocus, eventObjectFocus, 0, callback, 0, 0, winEventOutOfContext|winEventSkipOwnProcess)
	if err != nil {
		ready <- fmt.Errorf("uia: SetWinEventHook: %w", err)
		return
	}
	t.hookHandle = handle

	// Inspeciona o foco que JÁ EXISTE, antes de qualquer troca.
	//
	// O cache começa em Protected:true ("na dúvida, protege") e só era
	// atualizado por EVENT_OBJECT_FOCUS — ou seja, por uma MUDANÇA de
	// foco. Quem subisse o Agent e ditasse na janela que já estava em
	// foco pegava o valor inicial e via "Ditado desativado em campo
	// protegido", sem nunca ter chegado perto de um campo de senha.
	//
	// Isto não afrouxa a decisão 5 do ADR-011: se a inspeção falhar ou
	// estourar o orçamento, Protected() continua devolvendo true. O que
	// muda é passar a PERGUNTAR antes de assumir.
	//
	// Roda aqui, nesta thread, porque é a dona do apartment COM — a
	// mesma regra que vale para o callback. Se for dúvida, a reinspeção
	// fica agendada e sai quando o laço abaixo começar.
	t.inspecionarEPublicar(sis, 0, true)

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
		win32.TranslateMessage(&msg)
		win32.DispatchMessageW(&msg)
	}

	// A OS thread volta para o Go depois do UnlockOSThread: nada desta
	// thread fica agendado nela.
	if timerID != 0 {
		killThreadTimer(timerID)
	}
	unhookWinEvent(t.hookHandle)
}
