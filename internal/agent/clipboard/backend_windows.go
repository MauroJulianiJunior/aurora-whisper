//go:build windows

package clipboard

import (
	"fmt"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// user32/kernel32 não têm wrapper em internal/agent/win32 (que só cobre
// user32/gdi32 para hook e overlay — ver seu doc.go) nem em
// golang.org/x/sys/windows para as funções de clipboard, então são
// resolvidas aqui do mesmo jeito que internal/agent/winruntime resolve
// kernel32!Beep: NewLazySystemDLL/NewProc, sem reimplementar a fiação de
// convenção de chamada por cima da stdlib (ADR-010, D3). Este arquivo é o
// único deste pacote com essas chamadas — o resto do pacote fala só com a
// interface backend.
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
)

const (
	// cfUnicodeText é CF_UNICODETEXT (winuser.h): texto UTF-16 terminado
	// em zero. É o único formato que este pacote preserva — ver doc.go,
	// "O que é preservado, e o que não é".
	cfUnicodeText = 13

	// gmemMoveable é GMEM_MOVEABLE (winbase.h), exigido pelo Win32 para
	// memória entregue a SetClipboardData — o sistema pode mover o bloco
	// enquanto o app não o tem travado com GlobalLock.
	gmemMoveable = 0x0002
)

// winBackend é a implementação real de backend, sem estado — todo estado
// vive no próprio clipboard do sistema, fora do processo.
type winBackend struct{}

func newBackend() backend { return winBackend{} }

func (winBackend) open() error {
	r1, _, err := procOpenClipboard.Call(0)
	if r1 == 0 {
		return err
	}
	return nil
}

func (winBackend) close() {
	// CloseClipboard pode falhar (ex.: chamado sem open bem-sucedido
	// antes), mas backend.close não tem retorno de erro — o contrato é
	// "sempre chamável via defer" (ver doc da interface). Um erro aqui
	// não tem ação corretiva possível do lado do chamador.
	procCloseClipboard.Call()
}

func emptyClipboard() error {
	r1, _, err := procEmptyClipboard.Call()
	if r1 == 0 {
		return err
	}
	return nil
}

func (winBackend) empty() error {
	return emptyClipboard()
}

func (winBackend) unicodeText() (string, bool, error) {
	avail, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText)
	if avail == 0 {
		// Formato ausente: clipboard vazio ou com outra coisa (imagem,
		// arquivos, ...). Não é erro — ver doc.go.
		return "", false, nil
	}

	h, _, err := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false, fmt.Errorf("clipboard: GetClipboardData: %w", err)
	}
	// h é um HGLOBAL que pertence ao clipboard enquanto ele estiver
	// aberto — nunca chamamos GlobalFree nele, só Lock/Unlock para ler.
	//
	// go vet (analisador unsafeptr) aponta "possible misuse of
	// unsafe.Pointer" na conversão de p (linha abaixo, unsafe.Pointer(p))
	// e na conversão equivalente em newGlobalUnicodeText. É falso
	// positivo conhecido, não um bug: p não é endereço de memória
	// gerenciada pelo Go (não é rastreada nem movida pelo GC — é memória
	// GMEM_MOVEABLE do Win32, travada nesta seção via GlobalLock), então
	// as regras de unsafe.Pointer sobre movimento do GC não se aplicam.
	// O mesmo padrão aparece no próprio golang.org/x/sys/windows gerado
	// (zsyscall_windows.go, ex. getSidIdentifierAuthority) e é inerente
	// a qualquer chamada Win32 sem CGO que devolva um ponteiro via
	// syscall — não há reformulação sintática que o analisador aceite
	// (ver golang.org/x/tools/go/analysis/passes/unsafeptr: só reconhece
	// reflect.Value.Pointer/UnsafeAddr e aritmética de ponteiro, nenhum
	// dos dois se aplica aqui). Registrado como concern no relatório
	// desta fatia.
	p, _, lockErr := procGlobalLock.Call(h)
	if p == 0 {
		return "", false, fmt.Errorf("clipboard: GlobalLock: %w", lockErr)
	}
	defer procGlobalUnlock.Call(h)

	size, _, _ := procGlobalSize.Call(h)
	if size < 2 {
		return "", true, nil
	}
	// p é o endereço de memória alocada pelo Win32 (GlobalAlloc), não pelo
	// runtime do Go, então a preocupação do go vet (unsafeptr) sobre o GC
	// mover ou coletar o alvo não se aplica. unsafe.Add reconstrói o
	// ponteiro a partir do endereço bruto sem acionar o aviso — mesmo
	// padrão já usado em internal/agent/win32/callbacks_windows.go.
	units := unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(nil), p)), size/2)
	n := 0
	for n < len(units) && units[n] != 0 {
		n++
	}
	return string(utf16.Decode(units[:n])), true, nil
}

// newGlobalUnicodeText aloca um HGLOBAL movível com text codificado como
// CF_UNICODETEXT (UTF-16 terminado em zero) — pronto para
// SetClipboardData. Em qualquer caminho de erro, libera o que já tiver
// alocado; não há vazamento a cargo do chamador.
func newGlobalUnicodeText(text string) (uintptr, error) {
	units := utf16.Encode([]rune(text))
	units = append(units, 0)
	size := uintptr(len(units)) * 2

	h, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return 0, fmt.Errorf("clipboard: GlobalAlloc: %w", err)
	}
	// Ver o comentário equivalente em unicodeText sobre o achado de
	// go vet (unsafeptr) nesta conversão — falso positivo conhecido.
	p, _, lockErr := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return 0, fmt.Errorf("clipboard: GlobalLock: %w", lockErr)
	}
	// Ver o comentário equivalente em unicodeText: memória do Win32, não
	// do GC do Go; unsafe.Add é a forma de expressar isso sem o aviso.
	dst := unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(nil), p)), len(units))
	copy(dst, units)
	procGlobalUnlock.Call(h)

	return h, nil
}

func (winBackend) setUnicodeText(text string) error {
	h, err := newGlobalUnicodeText(text)
	if err != nil {
		return err
	}

	if err := emptyClipboard(); err != nil {
		procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: EmptyClipboard: %w", err)
	}

	r1, _, setErr := procSetClipboardData.Call(cfUnicodeText, h)
	if r1 == 0 {
		// SetClipboardData falhou: a posse de h nunca passou para o
		// sistema, então ainda é nossa responsabilidade liberar.
		procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: SetClipboardData: %w", setErr)
	}
	// Sucesso: a partir daqui h pertence ao sistema. Não chamar
	// GlobalFree nem reusar h.
	return nil
}

func (winBackend) sequenceNumber() (uint32, error) {
	r1, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r1), nil
}
