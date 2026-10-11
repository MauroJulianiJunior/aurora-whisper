//go:build !logindeteste

package main

import (
	"strings"
	"testing"
)

// Sem a build tag logindeteste, o servidor de teste não existe: "teste" é um
// comando desconhecido, e a ajuda nem o cita.
func TestTeste_NaoExisteSemATag(t *testing.T) {
	out, errOut, code := runCmd(t, map[string]string{envPainelDir: painelDir(t), envHTTPAddr: "127.0.0.1:0"}, "teste")
	if code != 2 || !strings.Contains(errOut, `comando desconhecido "teste"`) || out != "" {
		t.Fatalf("teste sem a tag: %d %q %q", code, out, errOut)
	}
	help, _, _ := runCmd(t, nil, "help")
	if strings.Contains(help, "teste") {
		t.Errorf("a ajuda cita o comando teste sem a tag: %s", help)
	}
}
