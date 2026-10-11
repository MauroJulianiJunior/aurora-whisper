//go:build !logindeteste

package main

import (
	"context"
	"fmt"
	"io"
)

// Sem a build tag logindeteste (o build normal e o de produção), o servidor
// de teste não existe: "teste" é um comando desconhecido, como qualquer
// outro. Ver teste.go.

const usoDoTeste = ""

func runTeste(_ context.Context, _ []string, _ func(string) string, _, stderr io.Writer) int {
	fmt.Fprintf(stderr, "%s: comando desconhecido %q\n\n", appName, "teste")
	usage(stderr)
	return 2
}
