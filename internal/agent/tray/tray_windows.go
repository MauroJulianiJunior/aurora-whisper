//go:build windows

package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// Procs próprios deste pacote, no mesmo padrão de
// internal/agent/clipboard/backend_windows.go: o que só a bandeja usa não
// entra no pacote win32 compartilhado.
var (
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procSetTimer               = user32.NewProc("SetTimer")
	procKillTimer              = user32.NewProc("KillTimer")
	procCreateBitmap           = gdi32.NewProc("CreateBitmap")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")

	procChangeWindowMessageFilterEx  = user32.NewProc("ChangeWindowMessageFilterEx")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
)

// Constantes publicadas em shellapi.h e winuser.h. Nenhuma inventada.
const (
	nimAdd    = 0x0
	nimModify = 0x1
	nimDelete = 0x2

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	// wmClose, wmSysCommand e scClose ficam em janela.go, junto da
	// decisão que os usa.
	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmSettingChange = 0x001A
	wmTimer         = 0x0113
	wmContextMenu   = 0x007B
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmApp           = 0x8000

	// wmNotifica é o que o Windows manda quando a pessoa clica no ícone;
	// wmAtualiza é o nosso pedido de redesenho vindo de outra thread.
	wmNotifica = wmApp + 1
	wmAtualiza = wmApp + 2
	// wmParar é o pedido de encerramento vindo de Parar. Não é WM_CLOSE:
	// WM_CLOSE também chega de FORA (ver wndProc), e os dois pedem coisas
	// diferentes.
	wmParar = wmApp + 3

	idTimerIcone = 1

	// MSGFLT_ALLOW (winuser.h).
	msgfltAllow = 1
	// DPI_AWARENESS_CONTEXT_SYSTEM_AWARE (windef.h): ((DPI_AWARENESS_CONTEXT)-2).
	dpiContextoDoSistema = ^uintptr(1)

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	smCxSmIcon   = 49
	biRGB        = 0
	dibRGBColors = 0

	nomeDaClasse = "AuroraWhisperTray"
)

// notifyIconData espelha NOTIFYICONDATAW (shellapi.h), versão completa. Os
// handles são uintptr, e o alinhamento natural do Go reproduz o padding
// que o compilador C insere no x64 — ver o comentário de
// internal/agent/win32/types.go.
type notifyIconData struct {
	CbSize           uint32
	HWnd             win32.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            win32.HICON
	SzTip            [maxDica]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     win32.HICON
}

// iconInfo espelha ICONINFO (winuser.h).
type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  win32.HBITMAP
	HbmColor win32.HBITMAP
}

// Tray é o ícone na área de notificação. Um por processo.
type Tray struct {
	acoes Acoes

	hwnd                     win32.HWND
	iconeAtivo, iconePausado win32.HICON
	taskbarCriada            uint32
	// tam e tema são os dos ícones de agora: tam é fixo desde a subida, e
	// tema acompanha a barra de tarefas (acompanharTema).
	tam  int
	tema Tema

	// sis são as chamadas de sistema do caminho do ícone — ver sistema.
	sis sistema

	// Estado da subida, tocado só pela thread da bandeja.
	pronto     chan<- error
	avisou     bool
	tentativas int
	// foraNoLog diz que a última palavra da bandeja no log é "o ícone foi
	// recusado" — a volta dele precisa ser registrada também (ver
	// Acoes.AoVoltar).
	foraNoLog bool

	done chan struct{}
}

// defWindowProc é win32.DefWindowProcW. É variável só para o teste do
// wndProc pôr um dublê no lugar e verificar que uma mensagem NÃO chegou
// ao Windows — sem chamada de sistema nenhuma.
var defWindowProc = win32.DefWindowProcW

// sistema reúne as chamadas de sistema do caminho do ícone: pôr e tirar o
// ícone, o timer das tentativas, o fim da janela, e criar, destruir e
// escolher a variante dos ícones.
//
// Em produção são as de verdade (sistemaReal), e só Iniciar as liga —
// depois do portão. O teste liga um dublê que só anota, e o caminho
// inteiro fica exercitável sem ícone na bandeja nem timer de verdade: a
// desistência tirando o ícone antes de destruir a janela, o SetTimer que
// falha, a série nova depois do TaskbarCreated. Esses três passos foram
// corrigidos pela revisão de 29-set-2026 e ficavam sem teste, porque cada
// um era uma chamada de sistema no meio da decisão. Um Tray montado sem
// sis entra em pânico no primeiro uso — nunca cai no Windows por engano.
type sistema struct {
	notifyIcon    func(op uintptr, d *notifyIconData) error
	setTimer      func(hwnd win32.HWND) error
	killTimer     func(hwnd win32.HWND)
	destroyWindow func(hwnd win32.HWND)

	lerTema       func() Tema
	criarIcone    func(pix []byte, tam int) (win32.HICON, error)
	destruirIcone func(h win32.HICON)
}

var sistemaReal = sistema{
	notifyIcon: func(op uintptr, d *notifyIconData) error {
		r, _, err := procShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(d)))
		if r == 0 {
			return fmt.Errorf("tray: Shell_NotifyIconW(%d): %w", op, err)
		}
		return nil
	},
	setTimer: func(hwnd win32.HWND) error {
		r, _, err := procSetTimer.Call(uintptr(hwnd), idTimerIcone, uintptr(IntervaloEntreTentativas.Milliseconds()), 0)
		if r == 0 {
			return fmt.Errorf("SetTimer: %w", err)
		}
		return nil
	},
	killTimer:     func(hwnd win32.HWND) { procKillTimer.Call(uintptr(hwnd), idTimerIcone) },
	destroyWindow: func(hwnd win32.HWND) { win32.DestroyWindow(hwnd) },

	lerTema:       lerTemaDaBarra,
	criarIcone:    criarIcone,
	destruirIcone: func(h win32.HICON) { procDestroyIcon.Call(uintptr(h)) },
}

// lerTemaDaBarra lê de que cor está a barra de tarefas. Só lê o registro,
// não muda nada nele — por isso não passa pelo portão, como
// autostart.Ligado.
func lerTemaDaBarra() Tema {
	k, err := registry.OpenKey(registry.CURRENT_USER, chaveDoTema, registry.QUERY_VALUE)
	if err != nil {
		return TemaDaBarra(0, err)
	}
	defer k.Close()
	// GetIntegerValue aceita DWORD e QWORD; outro tipo volta como erro, e
	// TemaDaBarra fica com o escuro.
	v, _, err := k.GetIntegerValue(valorDoTema)
	return TemaDaBarra(v, err)
}

// criada garante uma bandeja por processo: win32.NewWndProc consome, para
// sempre, uma posição de uma tabela de tamanho fixo do runtime, e a
// classe de janela só pode ser registrada uma vez. Mesmo motivo do
// overlay (window_windows.go).
var criada int32

// Iniciar cria o ícone e devolve quando ele já está na bandeja — ou
// quando a barra de tarefas o recusou em todas as tentativas
// (tentativas.go), o que pode levar alguns segundos no logon.
//
// Passa pelo portão de internal/agent/sysinput: pôr um ícone na área de
// notificação é agir na tela de quem usa, e isso nunca acontece sob go
// test.
func Iniciar(acoes Acoes) (*Tray, error) {
	if err := sysinput.Guard(); err != nil {
		return nil, fmt.Errorf("tray: %w", err)
	}
	if acoes.Estado == nil || acoes.Pausar == nil || acoes.IniciarComWindows == nil || acoes.Sair == nil {
		return nil, errors.New("tray: Acoes incompletas")
	}
	if !atomic.CompareAndSwapInt32(&criada, 0, 1) {
		return nil, errors.New("tray: já iniciada neste processo")
	}

	t := &Tray{acoes: acoes, sis: sistemaReal, done: make(chan struct{})}
	pronto := make(chan error, 1)
	go t.rodar(pronto)
	if err := <-pronto; err != nil {
		return nil, err
	}
	return t, nil
}

// Atualizar redesenha ícone e dica. Seguro de qualquer goroutine: só
// POSTA uma mensagem, e quem redesenha é a thread da bandeja.
func (t *Tray) Atualizar() {
	procPostMessageW.Call(uintptr(t.hwnd), wmAtualiza, 0, 0)
}

// Parar tira o ícone da bandeja e espera a thread terminar.
func (t *Tray) Parar() {
	procPostMessageW.Call(uintptr(t.hwnd), wmParar, 0, 0)
	<-t.done
}

// rodar é a thread da bandeja. Travada na thread do sistema porque a
// janela, o ícone e o menu pertencem à thread que os criou — mesmo motivo
// do hook e do tracker de foco.
func (t *Tray) rodar(pronto chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)
	t.pronto = pronto

	// O processo não declara ciência de DPI, e numa thread assim o
	// GetSystemMetrics devolve tudo virtualizado a 96 DPI: o ícone sairia
	// com 16 px em qualquer escala, e o Explorer o esticaria borrado — o
	// mesmo com o menu. Só ESTA thread passa a saber o DPI do sistema; o
	// overlay, em outra thread, continua exatamente como foi aprovado.
	// Ausente antes do Windows 10 1607: aí fica como estava.
	if procSetThreadDpiAwarenessContext.Find() == nil {
		procSetThreadDpiAwarenessContext.Call(dpiContextoDoSistema)
	}

	if err := t.criarJanela(); err != nil {
		pronto <- err
		return
	}

	t.tam = TamanhoDoIcone(int(win32.GetSystemMetrics(smCxSmIcon)))
	t.tema = t.sis.lerTema()
	var err error
	if t.iconeAtivo, t.iconePausado, err = t.montarIcones(t.tema); err != nil {
		win32.DestroyWindow(t.hwnd)
		pronto <- err
		return
	}
	// Numa função, e não com os handles como argumento do defer:
	// acompanharTema troca os dois, e um defer com argumento guardaria os
	// da subida — destruiria de novo os já destruídos e deixaria os novos
	// para trás.
	defer func() {
		t.sis.destruirIcone(t.iconeAtivo)
		t.sis.destruirIcone(t.iconePausado)
	}()

	// O Explorer manda "TaskbarCreated" a toda janela de nível superior
	// quando ele reinicia. Sem tratar isto, qualquer travamento do
	// Explorer apagaria o ícone PARA SEMPRE, e o Agent seguiria rodando
	// sem ninguém ver — nem ter como sair a não ser pelo Gerenciador de
	// Tarefas.
	if nome, err := windows.UTF16PtrFromString("TaskbarCreated"); err == nil {
		r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(nome)))
		t.taskbarCriada = uint32(r)
	}
	// Com o Agent elevado, o isolamento de privilégio (UIPI) barra o
	// TaskbarCreated que vem do Explorer, que não é elevado — e o ícone
	// não voltaria depois de o Explorer reiniciar. Liberar só esta
	// mensagem, só para esta janela.
	if t.taskbarCriada != 0 && procChangeWindowMessageFilterEx.Find() == nil {
		procChangeWindowMessageFilterEx.Call(uintptr(t.hwnd), uintptr(t.taskbarCriada), msgfltAllow, 0)
	}

	// A primeira tentativa. Se falhar, as próximas vêm pelo timer, com a
	// janela viva e o laço de mensagens rodando — é ele que recebe o
	// TaskbarCreated, e parar o laço para esperar travaria quem manda
	// mensagem a todas as janelas.
	t.tentarPorOIcone()

	var msg win32.MSG
	for {
		r, _ := win32.GetMessageW(&msg, 0, 0, 0)
		if r == 0 || r == -1 {
			return
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessageW(&msg)
	}
}

func (t *Tray) criarJanela() error {
	nome, err := windows.UTF16PtrFromString(nomeDaClasse)
	if err != nil {
		return fmt.Errorf("tray: nome de classe: %w", err)
	}
	inst, _, _ := procGetModuleHandleW.Call(0)
	wc := win32.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win32.WNDCLASSEX{})),
		LpfnWndProc:   win32.NewWndProc(t.wndProc),
		HInstance:     win32.HINSTANCE(inst),
		LpszClassName: nome,
	}
	if _, err := win32.RegisterClassExW(&wc); err != nil {
		return fmt.Errorf("tray: RegisterClassExW: %w", err)
	}
	// Janela de nível superior OCULTA, e não uma janela só de mensagens
	// (HWND_MESSAGE): TrackPopupMenu e SetForegroundWindow exigem uma
	// janela de verdade, e o "TaskbarCreated" só é mandado a janelas de
	// nível superior. Ela nunca é mostrada — ShowWindow não é chamado.
	hwnd, err := win32.CreateWindowExW(0, nome, nome, 0, 0, 0, 0, 0, 0, 0, win32.HINSTANCE(inst), nil)
	if err != nil {
		return fmt.Errorf("tray: CreateWindowExW: %w", err)
	}
	t.hwnd = hwnd
	return nil
}

func (t *Tray) wndProc(hwnd win32.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch {
	case msg == wmNotifica:
		switch uint32(lParam) & 0xFFFF {
		case wmLButtonUp, wmRButtonUp, wmContextMenu:
			t.mostrarMenu()
		}
		return 0
	case msg == wmAtualiza:
		t.notificar(nimModify)
		return 0
	case t.taskbarCriada != 0 && msg == t.taskbarCriada:
		// O Explorer (re)criou a barra, e o ícone precisa entrar de novo.
		// Na subida, é mais uma tentativa da mesma série. Depois dela, é
		// uma série NOVA, com o mesmo timer — e que não desiste: tentar
		// uma vez só deixava o ícone sumir pelo resto da sessão se o
		// Explorer, ocupado com o NIM_ADD de todo mundo, recusasse o nosso
		// (revisão de 29-set-2026; ver tentativas.go).
		if t.avisou {
			t.tentativas = 0
		}
		t.tentarPorOIcone()
		return 0
	case msg == wmTimer && wParam == idTimerIcone:
		t.tentarPorOIcone()
		return 0
	case msg == wmParar:
		t.notificar(nimDelete)
		t.sis.destroyWindow(hwnd)
		return 0
	case ehAltF4(msg, wParam):
		// Alt+F4 com esta janela ativa — ela fica em primeiro plano depois
		// do menu. Engolido aqui, antes de o DefWindowProc transformá-lo em
		// WM_CLOSE: ninguém vê esta janela, então ninguém pediu para
		// fechá-la (revisão de 29-set-2026; ver ehAltF4).
		return 0
	case msg == wmClose:
		// Nosso código nunca manda WM_CLOSE: Parar usa wmParar. A premissa
		// de antes era "então este veio de fora", e ela era incompleta: o
		// próprio DefWindowProc gera WM_CLOSE a partir do Alt+F4, e esse
		// caminho encerrava o Agent sem aviso. Ele agora morre no caso
		// acima, e o que chega aqui vem de fora, postado direto nesta
		// janela.
		//
		// Não é o caminho do "taskkill" sem /F, ao contrário do que este
		// comentário dizia: ele manda um WM_CLOSE só, à primeira janela sem
		// dono do processo, e essa é a pílula do overlay, que é topmost. A
		// pílula repassa o pedido ao mesmo encerramento (ver
		// overlay.AoPedirFechar; revisão de 29-set-2026).
		//
		// Tratar como o Parar tirava o ícone e deixava o Agent ditando
		// invisível, com o mutex impedindo abrir outro. Quem pede para
		// fechar a janela da bandeja está pedindo para fechar o Agent: o
		// mesmo caminho do item Sair.
		t.acoes.Sair()
		return 0
	case msg == wmDestroy:
		win32.PostQuitMessage(0)
		return 0
	case msg == wmSettingChange:
		// A barra de tarefas trocou de claro para escuro, ou o contrário:
		// o ícone não tem fundo próprio, e a barra dele sumiria na barra
		// de tarefas nova. Segue também para o DefWindowProc — é um aviso,
		// e outras áreas dele não são desta janela.
		if mudouOTema(textoDoLParam(lParam)) {
			t.acompanharTema()
		}
	}
	return defWindowProc(hwnd, msg, wParam, lParam)
}

// textoDoLParam lê o texto que o WM_SETTINGCHANGE traz no lParam: o nome
// da área que mudou, ou nada.
//
// O lParam é o endereço de um texto do Windows, não do runtime do Go —
// mesmo caso do KBDLLHOOKSTRUCT em win32.NewLowLevelKeyboardHookProc, e a
// mesma forma de reconstruir o ponteiro sem o aviso do go vet.
func textoDoLParam(lParam uintptr) string {
	if lParam == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Add(unsafe.Pointer(nil), lParam)))
}

// montarIcones desenha e cria os dois ícones, ativo e pausado, para o
// tema. Se o segundo falha, o primeiro é destruído antes de voltar: quem
// chama fica com os dois ou com nenhum.
func (t *Tray) montarIcones(tema Tema) (ativo, pausado win32.HICON, err error) {
	if ativo, err = t.sis.criarIcone(PixelsDoIcone(t.tam, tema, false), t.tam); err != nil {
		return 0, 0, err
	}
	if pausado, err = t.sis.criarIcone(PixelsDoIcone(t.tam, tema, true), t.tam); err != nil {
		t.sis.destruirIcone(ativo)
		return 0, 0, err
	}
	return ativo, pausado, nil
}

// acompanharTema põe na bandeja a variante do tema de agora.
//
// O Windows manda "ImmersiveColorSet" também quando só a cor de destaque
// muda; sem troca de tema, nada é recriado. Se os ícones novos não saem,
// os velhos ficam — um ícone de contraste errado ainda é melhor que
// nenhum —, e o tema guardado continua o velho, para o próximo aviso
// tentar de novo.
func (t *Tray) acompanharTema() {
	tema := t.sis.lerTema()
	if tema == t.tema {
		return
	}
	ativo, pausado, err := t.montarIcones(tema)
	if err != nil {
		if t.acoes.AoFalhar != nil {
			t.acoes.AoFalhar(fmt.Errorf("tray: a barra de tarefas mudou de tema e o ícone não foi redesenhado: %w", err))
		}
		return
	}
	velhoAtivo, velhoPausado := t.iconeAtivo, t.iconePausado
	t.iconeAtivo, t.iconePausado, t.tema = ativo, pausado, tema
	// Se o ícone está fora da bandeja — uma série de tentativas em
	// andamento —, este NIM_MODIFY falha, e tudo bem: a próxima tentativa
	// já põe os ícones novos. Os velhos só são destruídos depois, para a
	// bandeja nunca apontar para um ícone que não existe mais.
	t.notificar(nimModify)
	t.sis.destruirIcone(velhoAtivo)
	t.sis.destruirIcone(velhoPausado)
}

// tentarPorOIcone é uma tentativa de pôr o ícone — na subida ou depois de
// um TaskbarCreated; DepoisDaTentativa decide o resto.
func (t *Tray) tentarPorOIcone() {
	t.tentativas++
	err := t.porIcone()
	switch DepoisDaTentativa(t.tentativas, err == nil, t.avisou) {
	case PassoPronto:
		t.sis.killTimer(t.hwnd)
		t.avisar(nil)
		if t.foraNoLog {
			t.foraNoLog = false
			if t.acoes.AoVoltar != nil {
				t.acoes.AoVoltar()
			}
		}
	case PassoTentarDeNovo:
		if t.tentativas > 1 {
			return // o timer da série já está armado
		}
		// Sem timer, nenhuma tentativa seguinte chega, e Iniciar ficaria
		// esperando para sempre — com o hook já instalado e nenhum ícone.
		// O retorno era ignorado até a revisão de 29-set-2026.
		if errTimer := t.sis.setTimer(t.hwnd); errTimer != nil {
			errSemTimer := fmt.Errorf("tray: o ícone foi recusado e não consegui armar as próximas tentativas (%v): %w", errTimer, err)
			if !t.avisou {
				t.desistir(errSemTimer)
				return
			}
			// Depois da subida não há Iniciar esperando, e destruir a
			// janela perderia o próximo TaskbarCreated — a única chance
			// que sobra de o ícone voltar. Fica registrado.
			t.registrarIconeFora(errSemTimer)
			return
		}
		if t.avisou {
			// Uma linha por série, não uma por segundo: o Explorer pode
			// recusar por um bom tempo, e o log não pode virar ruído.
			t.registrarIconeFora(fmt.Errorf("tray: o Explorer reiniciou e recusou o ícone; tentando de novo a cada %v até conseguir: %w", IntervaloEntreTentativas, err))
		}
	case PassoDesistir:
		t.desistir(fmt.Errorf("tray: a barra de tarefas recusou o ícone %d vezes: %w", t.tentativas, err))
	}
}

// desistir encerra a subida da bandeja com erro.
//
// O NIM_DELETE vem antes de tudo porque um NIM_ADD que "falhou" por tempo
// esgotado às vezes pôs o ícone mesmo assim (ver porIcone). Sem ele, o
// ícone ficava na bandeja, morto, ao lado da janela de erro dizendo que o
// Agent não está rodando (revisão de 29-set-2026). Se o ícone não existe,
// o NIM_DELETE só falha, sem efeito — e o erro dele não interessa aqui.
func (t *Tray) desistir(err error) {
	t.sis.killTimer(t.hwnd)
	t.notificar(nimDelete)
	t.avisar(err)
	t.sis.destroyWindow(t.hwnd)
}

// registrarIconeFora entrega a AoFalhar a recusa do ícone depois da
// subida, quando não há mais Iniciar para devolver o erro, e anota que a
// volta dele também precisa ir para o log.
func (t *Tray) registrarIconeFora(err error) {
	t.foraNoLog = true
	if t.acoes.AoFalhar != nil {
		t.acoes.AoFalhar(err)
	}
}

// porIcone põe o ícone na bandeja. Um NIM_ADD que "falhou" por tempo
// esgotado às vezes pôs o ícone mesmo assim, e aí todo NIM_ADD seguinte
// falha porque ele já existe. Se o NIM_MODIFY funciona, o ícone está lá.
func (t *Tray) porIcone() error {
	err := t.notificar(nimAdd)
	if err == nil || t.notificar(nimModify) == nil {
		return nil
	}
	return err
}

// avisar responde a Iniciar, uma vez só.
func (t *Tray) avisar(err error) {
	if t.avisou {
		return
	}
	t.avisou = true
	t.pronto <- err
}

// notificar adiciona, atualiza ou remove o ícone, sempre a partir do
// estado ATUAL — a fonte da verdade é quem Acoes.Estado consulta.
func (t *Tray) notificar(op uintptr) error {
	e := t.acoes.Estado()
	d := notifyIconData{
		HWnd:             t.hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmNotifica,
		HIcon:            t.iconeAtivo,
		SzTip:            dicaUTF16(Dica(e)),
	}
	if e.Pausado {
		d.HIcon = t.iconePausado
	}
	d.CbSize = uint32(unsafe.Sizeof(d))
	return t.sis.notifyIcon(op, &d)
}

func (t *Tray) mostrarMenu() {
	e := t.acoes.Estado()

	h, _, _ := procCreatePopupMenu.Call()
	if h == 0 {
		return
	}
	defer procDestroyMenu.Call(h)

	for _, it := range Menu(e) {
		if it.Separador {
			procAppendMenuW.Call(h, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if !it.Habilitado {
			flags |= mfGrayed
		}
		if it.Marcado {
			flags |= mfChecked
		}
		texto, err := windows.UTF16PtrFromString(it.Texto)
		if err != nil {
			continue
		}
		procAppendMenuW.Call(h, flags, uintptr(it.ID), uintptr(unsafe.Pointer(texto)))
	}

	var pt win32.POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// Comportamento documentado do TrackPopupMenu para ícones de bandeja:
	// sem pôr a janela dona em primeiro plano, o menu não fecha quando a
	// pessoa clica fora dele. O WM_NULL depois é a segunda metade da
	// mesma receita.
	procSetForegroundWindow.Call(uintptr(t.hwnd))
	cmd, _, _ := procTrackPopupMenu.Call(h,
		tpmReturnCmd|tpmNoNotify|tpmRightButton|tpmBottomAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(t.hwnd), 0)
	procPostMessageW.Call(uintptr(t.hwnd), wmNull, 0, 0)

	t.executar(ID(cmd), e)
}

// executar age sobre o item escolhido. e é o estado de quando o menu
// ABRIU, que é o que a pessoa viu ao clicar: "Pausar ditado" desmarcado
// significa "pausar", e é isso que ela pediu.
func (t *Tray) executar(id ID, e Estado) {
	switch id {
	case IDPausar:
		t.acoes.Pausar(!e.Pausado)
		t.notificar(nimModify)
	case IDIniciarComWindows:
		if err := t.acoes.IniciarComWindows(!e.IniciarComWindows); err != nil && t.acoes.AoFalhar != nil {
			t.acoes.AoFalhar(err)
		}
	case IDSair:
		t.acoes.Sair()
	case IDAbrir:
		if t.acoes.Abrir == nil {
			return
		}
		if err := t.acoes.Abrir(); err != nil && t.acoes.AoFalhar != nil {
			t.acoes.AoFalhar(err)
		}
	}
}

// criarIcone monta um HICON a partir de pixels BGRA tam x tam.
func criarIcone(pix []byte, tam int) (win32.HICON, error) {
	// O copy abaixo escreve len(pix) bytes num bitmap de tam x tam: com
	// tamanhos desencontrados, escreveria fora dele.
	if tam <= 0 || len(pix) != tam*tam*4 {
		return 0, fmt.Errorf("tray: %d bytes não são um ícone de %d x %d", len(pix), tam, tam)
	}
	bmi := win32.BITMAPINFO{Header: win32.BITMAPINFOHEADER{
		Size:        uint32(unsafe.Sizeof(win32.BITMAPINFOHEADER{})),
		Width:       int32(tam),
		Height:      -int32(tam), // negativo: linhas de cima para baixo, igual ao Canvas
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	cor, bits, err := win32.CreateDIBSection(0, &bmi, dibRGBColors)
	if err != nil {
		return 0, fmt.Errorf("tray: CreateDIBSection: %w", err)
	}
	// CreateIconIndirect COPIA os bitmaps; os originais podem ir embora.
	defer win32.DeleteObject(win32.HGDIOBJ(cor))
	copy(unsafe.Slice((*byte)(bits), len(pix)), pix)

	// Máscara monocromática vazia: com um bitmap de cor de 32 bits, o
	// Windows usa o canal alfa dele e ignora a máscara — mas ela tem de
	// existir, ou CreateIconIndirect recusa.
	mascara, _, err := procCreateBitmap.Call(uintptr(tam), uintptr(tam), 1, 1, 0)
	if mascara == 0 {
		return 0, fmt.Errorf("tray: CreateBitmap: %w", err)
	}
	defer win32.DeleteObject(win32.HGDIOBJ(mascara))

	ii := iconInfo{FIcon: 1, HbmMask: win32.HBITMAP(mascara), HbmColor: cor}
	h, _, err := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if h == 0 {
		return 0, fmt.Errorf("tray: CreateIconIndirect: %w", err)
	}
	return win32.HICON(h), nil
}
