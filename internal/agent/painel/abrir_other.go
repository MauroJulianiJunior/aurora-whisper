//go:build !windows

package painel

import "errors"

// AbrirNoNavegador só existe no Windows, onde o Agent roda.
func AbrirNoNavegador(string) error {
	return errors.New("painel: abrir no navegador só existe no Windows")
}
