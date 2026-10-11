//go:build !windows

package autostart

func Ligado(string) (bool, error) { return false, ErrNaoSuportado }
func Ligar(string) error          { return ErrNaoSuportado }
func Desligar() error             { return ErrNaoSuportado }
