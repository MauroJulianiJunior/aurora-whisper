//go:build windows

package painel

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

var procShellExecuteW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")

const swShowNormal = 1

// AbrirNoNavegador abre url no navegador padrão de quem usa.
//
// Passa pelo portão de internal/agent/sysinput: abrir uma janela na tela
// de quem usa é agir no sistema real, e sob go test isso nunca acontece.
//
// url carrega o bilhete — NUNCA vai para log, nem aqui nem em quem chama.
func AbrirNoNavegador(url string) error {
	if err := sysinput.Guard(); err != nil {
		return fmt.Errorf("painel: %w", err)
	}
	verbo, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	alvo, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	// ShellExecute devolve um valor maior que 32 em sucesso; de 0 a 32 é
	// código de erro (documentação do ShellExecuteW).
	r, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verbo)), uintptr(unsafe.Pointer(alvo)), 0, 0, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("painel: ShellExecute falhou (código %d)", r)
	}
	return nil
}
