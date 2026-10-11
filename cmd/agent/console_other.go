//go:build !windows

package main

import (
	"fmt"
	"os"
)

func temConsole() bool { return true }

func avisar(texto string, _ bool) { fmt.Fprintln(os.Stderr, texto) }
