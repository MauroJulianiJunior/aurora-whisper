//go:build windows

package conflito

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Listar enumera os nomes dos processos em execução, via
// CreateToolhelp32Snapshot.
//
// Devolve SÓ o nome do executável, nunca o caminho nem a linha de
// comando: quem chama só precisa comparar contra Conhecidos, e caminho
// de instalação diz onde a pessoa guarda as coisas — informação que não
// serve a nada aqui e que não deve circular.
//
// Um erro aqui nunca deve impedir o Agent de subir: não saber se há
// outro programa de ditado é bem menos grave que não ter ditado.
func Listar() ([]string, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("conflito: snapshot de processos: %w", err)
	}
	defer windows.CloseHandle(snap)

	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(snap, &e); err != nil {
		return nil, fmt.Errorf("conflito: primeiro processo: %w", err)
	}

	var nomes []string
	for {
		nomes = append(nomes, windows.UTF16ToString(e.ExeFile[:]))
		if err := windows.Process32Next(snap, &e); err != nil {
			// ERROR_NO_MORE_FILES encerra a varredura normalmente.
			break
		}
	}
	return nomes, nil
}
