//go:build windows

package instancia

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// Garantir toma a instância chamada nome. Devolve a função que a libera,
// ou ErrJaRodando se outro processo a detém.
//
// windows.CreateMutex devolve ERROR_ALREADY_EXISTS com um handle VÁLIDO
// quando o mutex já existe — é assim que o Win32 sinaliza, e não como
// falha. O handle é fechado aqui para não segurar a instância do outro.
func Garantir(nome string) (func(), error) {
	p, err := windows.UTF16PtrFromString(nome)
	if err != nil {
		return nil, fmt.Errorf("instancia: nome inválido %q: %w", nome, err)
	}
	h, err := windows.CreateMutex(nil, false, p)
	if jaExiste(err) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return nil, ErrJaRodando
	}
	if err != nil {
		return nil, fmt.Errorf("instancia: CreateMutex: %w", err)
	}
	return func() { windows.CloseHandle(h) }, nil
}

// jaExiste diz se a falha do CreateMutex significa "outro processo tem a
// instância".
//
// ERROR_ACCESS_DENIED também significa: quando o Agent que criou o mutex
// roda elevado, o descritor de segurança padrão do objeto não dá acesso a
// um processo comum, e o CreateMutex do segundo falha com acesso negado
// em vez de ALREADY_EXISTS. A primeira versão tratava isso como falha do
// mecanismo e seguia sem a garantia — dois hooks, texto duplicado, o
// defeito que este pacote existe para impedir.
func jaExiste(err error) bool {
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
