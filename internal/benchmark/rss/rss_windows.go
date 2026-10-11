//go:build windows

package rss

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// sample usa OpenProcess + GetProcessMemoryInfo (psapi.dll), do mesmo
// jeito que internal/agent/clipboard resolve chamadas Win32 que não têm
// wrapper em golang.org/x/sys/windows: NewLazySystemDLL/NewProc, sem CGO.
// golang.org/x/sys já é dependência do módulo (go.mod) — não é dependência
// nova.
var (
	psapi                    = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters espelha PROCESS_MEMORY_COUNTERS (psapi.h) — só os
// campos até WorkingSetSize importam aqui, mas a struct precisa do layout
// completo para o tamanho (cb) bater com o que a API espera.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
)

func sample(pid int) (int64, bool) {
	handle, err := windows.OpenProcess(processQueryInformation|processVMRead, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)

	var counters processMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))

	ret, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.cb),
	)
	if ret == 0 {
		return 0, false
	}
	return int64(counters.WorkingSetSize), true
}

// sampleTree enumera a topologia de processos do sistema uma vez
// (processChildren) e soma RSS pela árvore a partir de pid, usando a
// lógica pura de tree.go para lidar com reaproveitamento de PID e ciclos.
//
// CreateToolhelp32Snapshot/Process32First/Process32Next e GetProcessTimes
// já têm wrapper tipado em golang.org/x/sys/windows (diferente de
// GetProcessMemoryInfo, por isso sample acima usa NewLazySystemDLL/NewProc
// para psapi.dll) — o padrão de enumeração de processos abaixo é o mesmo
// que os/Getppid usa dentro do próprio x/sys/windows
// (syscall_windows.go, getProcessEntry), só que coletando todos os pares
// pid→ppid em vez de procurar um PID específico.
func sampleTree(pid int) (int64, bool) {
	children, ok := processChildren()
	if !ok {
		return 0, false
	}
	return sumTree(pid, children, processCreatedAt, sample)
}

// processChildren tira um snapshot de todos os processos do sistema e
// devolve o mapa PID do pai → PIDs dos filhos diretos, segundo o PPID que
// cada processo declara (PROCESSENTRY32.ParentProcessID). Não abre um
// handle por processo — só o handle do snapshot — por isso é barato o
// bastante para rodar a cada rssSampleInterval.
//
// PROCESSENTRY32 não carrega o instante de criação do processo, então
// este mapa por si só não distingue um PPID genuíno de um PPID
// reaproveitado (ver a documentação de sumTree em tree.go);
// processCreatedAt, chamado à parte só para os nós visitados, resolve
// isso.
func processChildren() (map[int][]int, bool) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, false
	}

	children := make(map[int][]int)
	for {
		pid := int(entry.ProcessID)
		ppid := int(entry.ParentProcessID)
		if pid != ppid { // defesa contra uma entrada absurda que se declara sua própria mãe
			children[ppid] = append(children[ppid], pid)
		}

		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break // ERROR_NO_MORE_FILES: fim da lista, não uma falha
		}
	}
	return children, true
}

// processCreatedAt devolve o instante de criação de pid via
// OpenProcess+GetProcessTimes, convertido para nanossegundos desde a
// época do Windows (windows.Filetime.Nanoseconds) — só serve para
// comparar dois processos entre si (ver sumTree), nunca é exposto como
// timestamp absoluto. ok é false se o processo não existe mais ou o
// handle não pôde ser aberto (ex.: processo protegido).
func processCreatedAt(pid int) (int64, bool) {
	handle, err := windows.OpenProcess(processQueryInformation, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return creation.Nanoseconds(), true
}
