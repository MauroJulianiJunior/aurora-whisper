//go:build !windows

package tray

// Tray fora do Windows não existe. Os stubs existem para cmd/agent e os
// testes de outros pacotes compilarem no runner Linux do CI.
type Tray struct{}

func Iniciar(Acoes) (*Tray, error) { return nil, ErrNaoSuportado }
func (*Tray) Atualizar()           {}
func (*Tray) Parar()               {}
