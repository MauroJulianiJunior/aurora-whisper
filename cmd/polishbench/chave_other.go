//go:build !windows

package main

import "errors"

// A leitura da chave com o eco desligado só existe no Windows (o Aurora Whisper
// só roda lá). Fora dele o comando compila, para a suíte rodar no Linux, mas
// recusa pedir a chave.
type leitorIndisponivel struct{}

func novoLeitorConsole() LeitorChave { return leitorIndisponivel{} }

func (leitorIndisponivel) Ler(string) (string, error) {
	return "", errors.New("a leitura oculta da chave só existe no Windows")
}
