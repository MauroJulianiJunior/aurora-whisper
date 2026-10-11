//go:build windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/sys/windows"
)

// leitorConsole lê a chave do console do Windows com o eco desligado
// (ENABLE_ECHO_INPUT). Só o binário de verdade o usa: nenhum teste chama isto,
// porque SetConsoleMode muda o estado do console da máquina.
//
// A entrada TEM de ser um console. Se a entrada for um arquivo ou um pipe
// (echo chave | polishbench ...), a leitura recusa: a chave não passa por
// pipe, nem por redirecionamento, nem por linha de comando.
type leitorConsole struct{}

func novoLeitorConsole() LeitorChave { return leitorConsole{} }

func (leitorConsole) Ler(prompt string) (string, error) {
	h := windows.Handle(os.Stdin.Fd())
	var modo uint32
	if err := windows.GetConsoleMode(h, &modo); err != nil {
		return "", errors.New("a entrada não é um terminal: a chave só é lida com o eco desligado, digitada ou colada no terminal (nunca por pipe, arquivo ou flag)")
	}
	fmt.Fprint(os.Stderr, prompt+" ")
	if err := windows.SetConsoleMode(h, modo&^windows.ENABLE_ECHO_INPUT); err != nil {
		return "", errors.New("não consegui desligar o eco do terminal")
	}
	// O modo do console sobrevive ao processo: se ele morresse com o eco
	// desligado, o terminal da pessoa ficaria mudo. Por isso o modo volta em
	// todo caminho, inclusive no Ctrl+C.
	defer windows.SetConsoleMode(h, modo)

	interrupcao := make(chan os.Signal, 1)
	signal.Notify(interrupcao, os.Interrupt)
	defer signal.Stop(interrupcao)

	type leitura struct {
		linha string
		err   error
	}
	lida := make(chan leitura, 1)
	go func() {
		linha, err := bufio.NewReader(os.Stdin).ReadString('\n')
		lida <- leitura{linha, err}
	}()

	select {
	case l := <-lida:
		fmt.Fprintln(os.Stderr) // o Enter não foi ecoado: fecha a linha do prompt
		if l.err != nil && l.linha == "" {
			return "", errors.New("não consegui ler a chave do terminal")
		}
		return strings.TrimSpace(l.linha), nil
	case <-interrupcao:
		fmt.Fprintln(os.Stderr)
		return "", ErrInterrompido
	}
}
