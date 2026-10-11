//go:build windows

package uia

// Bindings de UI Automation (COM) e dos utilitários Win32 auxiliares que o
// rastreador de foco precisa: mesma disciplina do WASAPI do Sprint 1
// (ADR-010 D1) — syscall puro sobre vtable COM manual, runtime.
// LockOSThread()+CoInitializeEx na mesma thread das chamadas (garantido
// por quem chama, tracker_windows.go:run()), Release() com defer em todo
// objeto obtido.
//
// Interfaces usadas: IUIAutomation2 (CLSID_CUIAutomation8, com
// ConnectionTimeout e TransactionTimeout em TetoChamadaUIA) — ou, se ele
// não existir, IUIAutomation (CLSID_CUIAutomation) — e
// IUIAutomationElement. Propriedades lidas: ControlType, IsPassword,
// ProcessId e NativeWindowHandle. Só IsPassword e NativeWindowHandle (para
// o fallback ES_PASSWORD) vêm da ADR-011, decisão 5; ControlType e
// ProcessId são descritivos — ver readElement.
//
// Nota sobre unsafe.Pointer/go vet: mesma disciplina documentada em
// internal/agent/audio/wasapi_windows.go — todo ponteiro de objeto/
// interface COM é mantido como unsafe.Pointer de ponta a ponta; uintptr só
// aparece no limite exato de syscall.SyscallN/LazyProc.Call.

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GUIDs dos objetos e das interfaces COM da UI Automation, conferidos
// contra UIAutomationClient.h. Um byte errado aqui devolve E_NOINTERFACE
// ou REGDB_E_CLASSNOTREG em runtime, não um erro de compilação — mesma
// ressalva de wasapi_windows.go.
//
// Fonte da conferência de 29-set-2026 (CUIAutomation8 e IUIAutomation2):
// o uiautomationclient.h do mingw-w64 (C:\msys64\ucrt64\include), gerado
// do UIAutomationClient.idl da Microsoft:
//
//	DEFINE_GUID(CLSID_CUIAutomation8, 0xe22ad333, 0xb25f, 0x460c, 0x83,0xd0, 0x05,0x81,0x10,0x73,0x95,0xc9)
//	DEFINE_GUID(IID_IUIAutomation2,   0x34723aff, 0x0c9d, 0x49d0, 0x98,0x96, 0x7a,0xb5,0x2d,0xf8,0xcd,0x8a)
//	DEFINE_GUID(CLSID_CUIAutomation,  0xff48dba4, 0x60ef, 0x4201, 0xaa,0x87, 0x54,0x10,0x3e,0xef,0x59,0x4e)
//	DEFINE_GUID(IID_IUIAutomation,    0x30cbe57d, 0xd9d0, 0x452a, 0xab,0x13, 0x7a,0xc5,0xac,0x48,0x25,0xee)
//
// Os dois últimos já estavam aqui e conferem com o mesmo arquivo.
var (
	clsidCUIAutomation  = windows.GUID{Data1: 0xFF48DBA4, Data2: 0x60EF, Data3: 0x4201, Data4: [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation    = windows.GUID{Data1: 0x30CBE57D, Data2: 0xD9D0, Data3: 0x452A, Data4: [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
	clsidCUIAutomation8 = windows.GUID{Data1: 0xE22AD333, Data2: 0xB25F, Data3: 0x460C, Data4: [8]byte{0x83, 0xD0, 0x05, 0x81, 0x10, 0x73, 0x95, 0xC9}}
	iidIUIAutomation2   = windows.GUID{Data1: 0x34723AFF, Data2: 0x0C9D, Data3: 0x49D0, Data4: [8]byte{0x98, 0x96, 0x7A, 0xB5, 0x2D, 0xF8, 0xCD, 0x8A}}
)

// Índices de vtable de IUIAutomation e IUIAutomation2
// (UIAutomationClient.h), contando os 3 métodos de IUnknown primeiro. Só
// o que este pacote usa está listado; os demais métodos existem na vtable
// real mas nunca são chamados daqui.
//
// IUIAutomation2 herda de IUIAutomation: a vtable dele é a de
// IUIAutomation inteira (58 entradas, 0 a 57) e mais seis
// (get/put_AutoSetFocus, get/put_ConnectionTimeout,
// get/put_TransactionTimeout, 58 a 63). Por isso o mesmo ponteiro serve
// para GetFocusedElement (8) com os dois objetos. Conferido em 29-set-2026
// contando as entradas de IUIAutomationVtbl (58) e IUIAutomation2Vtbl (64)
// no uiautomationclient.h do mingw-w64: GetFocusedElement na 8,
// put_ConnectionTimeout na 61, put_TransactionTimeout na 63 — os dois
// recebem um DWORD em milissegundos (Microsoft Learn, "IUIAutomation2::
// put_ConnectionTimeout" e "put_TransactionTimeout").
const (
	midAutomationGetFocusedElement      = 8
	midAutomation2PutConnectionTimeout  = 61
	midAutomation2PutTransactionTimeout = 63
)

// Índices de vtable de IUIAutomationElement (UIAutomationClient.h), mesma
// contagem. Só as propriedades que readElement lê — ver lá quais são
// exigidas pela ADR-011, decisão 5, e quais são descritivas.
const (
	midElementGetCurrentProcessId          = 20
	midElementGetCurrentControlType        = 21
	midElementGetCurrentIsPassword         = 35
	midElementGetCurrentNativeWindowHandle = 36
)

const (
	clsctxInprocServer  = 0x1
	clsctxInprocHandler = 0x2
	clsctxLocalServer   = 0x4
	clsctxAll           = clsctxInprocServer | clsctxInprocHandler | clsctxLocalServer
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	user32              = windows.NewLazySystemDLL("user32.dll")
	procGetWindowLongW  = user32.NewProc("GetWindowLongW")
	procSetWinEventHook = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent  = user32.NewProc("UnhookWinEvent")
	procSetTimer        = user32.NewProc("SetTimer")
	procKillTimer       = user32.NewProc("KillTimer")
)

// hresult é o valor de retorno de toda chamada COM: um int32 assinado —
// negativo é falha. Mesma forma de audio/wasapi_windows.go.
type hresult int32

func (h hresult) failed() bool { return h < 0 }

// comObject é um ponteiro para um objeto COM (ponteiro para o ponteiro de
// vtable). Cópia independente do helper de audio/wasapi_windows.go —
// pacotes deliberadamente isolados nesta fatia (ver doc.go: "SEU ESCOPO
// EXCLUSIVO"), mesma explicação de por que o campo fica em
// unsafe.Pointer, nunca em uintptr, do início ao fim da sua vida.
type comObject struct{ p unsafe.Pointer }

func asComObject(p unsafe.Pointer) comObject { return comObject{p: p} }

func (o comObject) valid() bool { return o.p != nil }

func (o comObject) vtable() unsafe.Pointer {
	return *(*unsafe.Pointer)(o.p)
}

func (o comObject) method(index int) uintptr {
	slot := unsafe.Add(o.vtable(), uintptr(index)*unsafe.Sizeof(uintptr(0)))
	return *(*uintptr)(slot)
}

func (o comObject) call(index int, args ...uintptr) uintptr {
	fn := o.method(index)
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(o.p))
	all = append(all, args...)
	r1, _, _ := syscall.SyscallN(fn, all...)
	runtime.KeepAlive(o)
	return r1
}

func (o comObject) callHR(index int, args ...uintptr) hresult {
	return hresult(int32(o.call(index, args...)))
}

func (o comObject) release() {
	if o.valid() {
		o.call(2) // IUnknown::Release
	}
}

// coInitialize chama CoInitializeEx nesta thread e reporta se esta
// chamada de fato tomou posse do apartment — mesma lógica e mesma
// justificativa de audio/wasapi_windows.go:coInitialize.
func coInitialize() (owned bool, err error) {
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		const rpcEChangedMode = 0x80010106
		if errno, ok := err.(windows.Errno); ok && uint32(errno) == rpcEChangedMode {
			return false, nil
		}
		return false, fmt.Errorf("CoInitializeEx: %w", err)
	}
	return true, nil
}

// coCreateInstance chama ole32!CoCreateInstance para o CLSID e a IID
// dados, com CLSCTX_ALL. Devolve o HRESULT cru: quem decide o que fazer
// com uma falha é montarAutomacao.
func coCreateInstance(clsid, iid windows.GUID) (comObject, hresult) {
	var out unsafe.Pointer
	r1, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)),
		0, // pUnkOuter: NULL
		uintptr(clsctxAll),
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(clsid)
	runtime.KeepAlive(iid)
	if hr := hresult(int32(r1)); hr.failed() {
		return comObject{}, hr
	}
	return asComObject(out), hresult(int32(r1))
}

// fabricaWindows é a fabricaUIA de produção: as chamadas COM de verdade,
// na thread dedicada (a dona do apartment). Guarda o objeto criado.
type fabricaWindows struct {
	obj comObject
}

func (f *fabricaWindows) criar8() int32 {
	obj, hr := coCreateInstance(clsidCUIAutomation8, iidIUIAutomation2)
	if !hr.failed() {
		f.obj = obj
	}
	return int32(hr)
}

func (f *fabricaWindows) aplicarTetos(ms uint32) (hrConexao, hrTransacao int32) {
	hc := f.obj.callHR(midAutomation2PutConnectionTimeout, uintptr(ms))
	ht := f.obj.callHR(midAutomation2PutTransactionTimeout, uintptr(ms))
	return int32(hc), int32(ht)
}

func (f *fabricaWindows) criarAntigo() int32 {
	obj, hr := coCreateInstance(clsidCUIAutomation, iidIUIAutomation)
	if !hr.failed() {
		f.obj = obj
	}
	return int32(hr)
}

// newAutomation cria a instância única de UI Automation usada pela thread
// dedicada do Tracker durante toda a sua vida (ADR-010, "COM tem
// apartment threading": criada e usada sempre na mesma thread): o
// CUIAutomation8 com TetoChamadaUIA, ou o CUIAutomation antigo se o 8 não
// existir — ver montarAutomacao, que decide, e Automacao, que diz o que
// aconteceu.
func newAutomation() (comObject, Automacao, error) {
	var f fabricaWindows
	info, err := montarAutomacao(&f, TetoChamadaUIA)
	if err != nil {
		return comObject{}, Automacao{}, fmt.Errorf("uia: %w", err)
	}
	return f.obj, info, nil
}

// getFocusedElement chama IUIAutomation::GetFocusedElement. Devolve um
// comObject inválido (valid() == false) quando a UIA reporta sucesso sem
// elemento nenhum (InspectNilElement no chamador) — não é, por si só, uma
// falha de HRESULT.
func (auto comObject) getFocusedElement() (comObject, hresult) {
	var elem unsafe.Pointer
	hr := auto.callHR(midAutomationGetFocusedElement, uintptr(unsafe.Pointer(&elem)))
	return asComObject(elem), hr
}

// getInt32Property chama um get_Current* que devolve um valor int32 puro
// (ControlType, IsPassword como BOOL, ProcessId).
func (elem comObject) getInt32Property(index int) (int32, hresult) {
	var out int32
	hr := elem.callHR(index, uintptr(unsafe.Pointer(&out)))
	return out, hr
}

// getNativeWindowHandle chama IUIAutomationElement::
// get_CurrentNativeWindowHandle. O tipo Win32 (UIA_HWND) é do tamanho de
// um ponteiro; devolve 0 quando o elemento não é apoiado por um HWND
// clássico próprio (controle moderno/navegador) — não é, por si só, uma
// falha.
func (elem comObject) getNativeWindowHandle() (uintptr, hresult) {
	var out uintptr
	hr := elem.callHR(midElementGetCurrentNativeWindowHandle, uintptr(unsafe.Pointer(&out)))
	return out, hr
}

// readElement lê do elemento com foco:
//
//   - IsPassword e o HWND nativo (para o fallback ES_PASSWORD): os dois
//     sinais da ADR-011, decisão 5 — a única coisa que a decisão pede;
//   - ControlType: descritivo, atravessa para dictation.FocusTarget;
//   - ProcessId, resolvido para o nome do executável: descritivo também,
//     mas não é enfeite — ProcessName escolhe o domínio do dicionário
//     (winruntime/runtime.go, dominioDoApp).
//
// Name e ClassName eram lidos até 29-set-2026 e atribuídos à decisão 5,
// que não os lista. Ninguém os lia (grep), e cada um é uma ida e volta
// entre processos a mais dentro do orçamento da inspeção. Saíram. O HWND
// nativo e o ProcessId NÃO podem sair pelo mesmo motivo: o primeiro é o
// segundo sinal de senha, o segundo é o domínio do dicionário.
//
// ControlType e IsPassword são tratados como essenciais: falhar em
// qualquer um dos dois aborta a leitura inteira, porque IsPassword é
// literalmente o sinal primário de segurança — o chamador trata isso como
// InspectFailed e, por Snapshot.Protected(), como campo protegido (na
// dúvida, protege). ProcessId e o HWND nativo são melhor-esforço: uma
// falha em qualquer um deles não aborta a leitura, só deixa o campo
// correspondente no zero value.
//
// Devolve QUAL das duas essenciais falhou, e com que HRESULT, em vez de
// um error: o chamador joga o texto fora e o log só dizia "failed", o que
// deixou três recusas de ditado indistinguíveis de um campo de senha de
// verdade em 28-set-2026. Ver FalhaEm.
//
// Cada chamada é cronometrada em lenta (ver ChamadaUIA), inclusive a que
// falhou.
func readElement(elem comObject, lenta *maisLenta) (Snapshot, FalhaEm, hresult) {
	inicio := time.Now()
	controlType, hr := elem.getInt32Property(midElementGetCurrentControlType)
	lenta.anotar(ChamadaControlType, time.Since(inicio))
	if hr.failed() {
		return Snapshot{}, FalhaControlType, hr
	}

	inicio = time.Now()
	isPasswordRaw, hr := elem.getInt32Property(midElementGetCurrentIsPassword)
	lenta.anotar(ChamadaIsPassword, time.Since(inicio))
	if hr.failed() {
		return Snapshot{}, FalhaIsPassword, hr
	}

	snap := Snapshot{
		ControlType: controlTypeName(controlType),
		IsPassword:  isPasswordRaw != 0,
	}

	inicio = time.Now()
	pid, hr := elem.getInt32Property(midElementGetCurrentProcessId)
	lenta.anotar(ChamadaProcessID, time.Since(inicio))
	if !hr.failed() {
		snap.ProcessID = uint32(pid)
		inicio = time.Now()
		snap.ProcessName = processName(snap.ProcessID)
		lenta.anotar(ChamadaNomeDoProcesso, time.Since(inicio))
	}

	inicio = time.Now()
	hwnd, hr := elem.getNativeWindowHandle()
	lenta.anotar(ChamadaNativeWindowHandle, time.Since(inicio))
	// hwnd 0 com sucesso também conta como lido: o fallback não se aplica
	// (controle moderno/navegador), e o sinal da UIA basta. Uma FALHA aqui
	// não conta — é o que impede uma leitura atrasada de valer sem o
	// segundo sinal (classificarLeitura, condição (b)).
	snap.EstiloLido = !hr.failed()
	if !hr.failed() && hwnd != 0 {
		inicio = time.Now()
		snap.StyleHasPassword = hasPasswordStyle(hwnd)
		lenta.anotar(ChamadaEstiloDaJanela, time.Since(inicio))
	}

	return snap, FalhaNenhuma, 0
}

// inspectFocusedElement é o fluxo completo de uma inspeção: pede o
// elemento com foco a auto, lê suas propriedades e classifica o
// resultado em InspectOutcome. budget <= 0 desativa a checagem de
// timeout (usado só em teste; Config.withDefaults garante um valor
// positivo em produção).
//
// Em qualquer desfecho, o Snapshot sai com a chamada mais lenta e a
// duração dela (LentaEm, LentaDuracao).
func inspectFocusedElement(auto comObject, budget time.Duration) Snapshot {
	var lenta maisLenta
	snap := inspecionarMedindo(auto, budget, &lenta)
	snap.LentaEm, snap.LentaDuracao = lenta.chamada, lenta.duracao
	return snap
}

func inspecionarMedindo(auto comObject, budget time.Duration, lenta *maisLenta) Snapshot {
	start := time.Now()

	elem, hr := auto.getFocusedElement()
	lenta.anotar(ChamadaGetFocusedElement, time.Since(start))
	if hr.failed() {
		return Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: int32(hr)}
	}
	if !elem.valid() {
		return Snapshot{Outcome: InspectNilElement}
	}
	defer elem.release()

	snap, falha, hrLeitura := readElement(elem, lenta)
	if falha != FalhaNenhuma {
		return Snapshot{Outcome: InspectFailed, Falha: falha, HRESULT: int32(hrLeitura)}
	}

	// Chegou aqui: GetFocusedElement, ControlType e IsPassword responderam.
	// Se passou do orçamento, classificarLeitura decide se ainda vale.
	snap.Outcome, snap.Atrasada = classificarLeitura(snap.EstiloLido, time.Since(start), budget)
	return snap
}

// processName resolve pid para o nome do executável (sem o caminho
// completo — CLAUDE.md, "Nunca gravar contexto de janela ... em logs":
// o nome do processo é metadado operacional, não conteúdo de janela, mas
// mesmo assim não há motivo para reter mais do que o necessário). Uma
// falha em qualquer etapa (processo já encerrado, sem permissão, etc.)
// devolve string vazia — ProcessName não entra na decisão de Protected(),
// só é descritivo.
func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	return nomeDaImagem(h)
}

// nomeDaImagem devolve o nome-base do executável do processo h
// (QueryFullProcessImageNameW, cortado por nomeBase), ou "" se não deu
// para ler — inclusive um caminho maior que MAX_PATH, como antes.
func nomeDaImagem(h windows.Handle) string {
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return nomeBase(windows.UTF16ToString(buf[:size]))
}

// lerPrimeiroPlano lê, só leitura, o processo da janela em primeiro plano
// e compara o nível de integridade dele com o do Agent (ilAgent, lido se
// okAgent) — a "janela de administrador" (decisão do Mauro de 29-set-2026,
// item 4). Só roda depois de uma inspeção sem elemento
// (deveOlharPrimeiroPlano); o que concluir é classificarElevacao, puro.
//
// Nada aqui manda mensagem a janela nenhuma nem espera a thread de
// interface de outro processo: GetForegroundWindow e
// GetWindowThreadProcessId só leem o objeto de janela; OpenProcess
// (PROCESS_QUERY_LIMITED_INFORMATION), OpenProcessToken (TOKEN_QUERY) e
// GetTokenInformation(TokenIntegrityLevel) são consultas ao kernel. Não
// injeta entrada nem altera estado global — como a leitura de propriedades
// da UIA, não passa pelo portão; e só roda na thread dedicada, que só
// existe depois de a subida ter passado por ele.
//
// A Microsoft documenta que um processo não elevado abre o token de um
// elevado se os dois forem da mesma conta (OpenProcessToken, Remarks) —
// o caso do Gerenciador de Tarefas de quem é administrador. De outra
// conta, o token não abre e o resultado é ElevacaoDesconhecida: a
// mensagem fica a de sempre.
func lerPrimeiroPlano(ilAgent uint32, okAgent bool) primeiroPlano {
	desconhecida := primeiroPlano{elevacao: ElevacaoDesconhecida}

	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return desconhecida
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
		return desconhecida
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return desconhecida
	}
	defer windows.CloseHandle(h)

	var (
		tok windows.Token
		il  uint32
		ok  bool
	)
	if windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok) == nil {
		il, ok = nivelDeIntegridade(tok)
		tok.Close()
	}
	return primeiroPlano{
		elevacao: classificarElevacao(il, ok, ilAgent, okAgent),
		// Só o nome-base, e só para o aviso (executavelDoAviso).
		executavel: nomeDaImagem(h),
	}
}

// nivelDeIntegridade lê o nível de integridade de tok: o último
// sub-authority do SID do rótulo obrigatório (TOKEN_MANDATORY_LABEL,
// GetTokenInformation com TokenIntegrityLevel) — 0x2000 médio, 0x3000
// alto (winnt.h, SECURITY_MANDATORY_*_RID). ok == false se não deu para
// ler.
func nivelDeIntegridade(tok windows.Token) (il uint32, ok bool) {
	var n uint32
	// A primeira chamada só diz o tamanho: falha com
	// ERROR_INSUFFICIENT_BUFFER por contrato, e o erro não interessa.
	_ = windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, nil, 0, &n)
	if n == 0 {
		return 0, false
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, &buf[0], n, &n); err != nil {
		return 0, false
	}
	// O SID aponta para dentro do próprio buf, que fica vivo até o fim da
	// função.
	rotulo := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0]))
	sid := rotulo.Label.Sid
	if sid == nil {
		return 0, false
	}
	k := sid.SubAuthorityCount()
	if k == 0 {
		return 0, false
	}
	il = sid.SubAuthority(uint32(k - 1))
	runtime.KeepAlive(buf)
	return il, true
}

// hasPasswordStyle é o fallback de estilo ES_PASSWORD (ADR-011, decisão
// 5): GetWindowLongW(hwnd, GWL_STYLE) & ES_PASSWORD, para controles Edit
// clássicos onde a UIA não respondeu IsPassword corretamente.
func hasPasswordStyle(hwnd uintptr) bool {
	// GWL_STYLE = -16. GetWindowLongW recebe nIndex como int (32 bits);
	// a extensão de sinal para uintptr (64 bits, o tipo que LazyProc.Call
	// exige) reproduz o mesmo padrão de bits que um chamador C passaria —
	// GetWindowLongW só lê os 32 bits baixos de qualquer forma.
	const (
		gwlStyle   = uintptr(0xFFFFFFFFFFFFFFF0) // int32(-16) estendido em complemento de dois
		esPassword = 0x0020
	)
	r1, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
	return int32(r1)&esPassword != 0
}

// setWinEventHook instala um SetWinEventHook OUT_OF_CONTEXT (sem DLL:
// hmodWinEventProc = 0) para eventMin..eventMax, chamando callback (criado
// por windows.NewCallback pelo chamador) em TODOS os processos/threads
// (idProcess=idThread=0), com flags. Devolve o HWINEVENTHOOK como
// uintptr — ver o comentário de Tracker.hookHandle.
func setWinEventHook(eventMin, eventMax uint32, hmodWinEventProc uintptr, callback uintptr, idProcess, idThread uint32, flags uint32) (uintptr, error) {
	r1, _, err := procSetWinEventHook.Call(
		uintptr(eventMin), uintptr(eventMax),
		hmodWinEventProc, callback,
		uintptr(idProcess), uintptr(idThread),
		uintptr(flags),
	)
	if r1 == 0 {
		return 0, err
	}
	return r1, nil
}

// unhookWinEvent remove um hook instalado por setWinEventHook.
func unhookWinEvent(hook uintptr) {
	procUnhookWinEvent.Call(hook)
}

// setThreadTimer cria um timer DA THREAD que chama (SetTimer com hWnd
// NULL): nenhuma janela, nenhum estado global — o WM_TIMER cai na fila
// desta thread, e o DispatchMessageW do laço dela chama timerProc
// (criado por windows.NewCallback pelo chamador). Periódico, como todo
// timer do Win32: quem quer um disparo só mata o timer no callback.
// Devolve o id do timer, ou 0 se falhou.
func setThreadTimer(espera time.Duration, timerProc uintptr) uintptr {
	ms := espera.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	id, _, _ := procSetTimer.Call(0, 0, uintptr(ms), timerProc)
	return id
}

// killThreadTimer destrói um timer criado por setThreadTimer.
func killThreadTimer(id uintptr) {
	procKillTimer.Call(0, id)
}
