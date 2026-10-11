//go:build !windows

package instancia

func Garantir(string) (func(), error) { return nil, ErrNaoSuportado }
