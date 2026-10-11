//go:build windows

package local

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestCaminhoDoModelo_NoWindows: o Windows não diferencia maiúscula de
// minúscula num caminho, e outro disco nunca é "relativo" a este. Só conta
// caminhos como texto: nada é criado nem aberto.
func TestCaminhoDoModelo_NoWindows(t *testing.T) {
	const arquivo = "ggml-small-q4_0.bin"
	casos := []struct {
		nome string
		cfg  Config
		quer string
	}{
		{
			"caixa de letras diferente no caminho",
			Config{Dir: `C:\Users\João\AppData\Local\Programs\Aurora Whisper`, ModelPath: `c:\users\JOÃO\appdata\local\programs\aurora whisper\` + arquivo},
			arquivo,
		},
		{
			"outro disco fica absoluto",
			Config{Dir: `C:\Programas\Aurora Whisper`, ModelPath: `D:\Programas\Aurora Whisper\` + arquivo},
			`D:\Programas\Aurora Whisper\` + arquivo,
		},
		{
			"caminho com acento e espaço",
			Config{Dir: `C:\Users\José Çedilha\AppData\Local\Programs\Aurora Whisper`, ModelPath: `C:\Users\José Çedilha\AppData\Local\Programs\Aurora Whisper\` + arquivo},
			arquivo,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := caminhoDoModelo(c.cfg); got != c.quer {
				t.Errorf("caminhoDoModelo = %q, quero %q", got, c.quer)
			}
		})
	}
}

// TestIniciarProcessoContido_RodaNaPastaDeTrabalho prova, com um processo de
// verdade e inofensivo (cmd /c cd, que só imprime a pasta atual), que o filho
// nasce com a pasta de trabalho pedida e não com a do teste. É a metade que a
// montagem pura não prova: que Dir chega ao CreateProcess.
func TestIniciarProcessoContido_RodaNaPastaDeTrabalho(t *testing.T) {
	pasta := t.TempDir()
	var saida bytes.Buffer
	pc, err := iniciarProcessoContidoCom(comandoDoServidor{Caminho: "cmd", Args: []string{"/c", "cd"}, Dir: pasta}, &saida)
	if err != nil {
		t.Fatalf("iniciarProcessoContidoCom: %v", err)
	}
	t.Cleanup(pc.matar)

	select {
	case <-pc.done:
	case <-time.After(10 * time.Second):
		t.Fatal("cmd /c cd não terminou em 10 s")
	}
	if pc.waitErr != nil {
		t.Fatalf("waitErr = %v", pc.waitErr)
	}
	if got := strings.TrimSpace(saida.String()); !strings.EqualFold(got, pasta) {
		t.Errorf("o filho rodou em %q, quero a pasta pedida %q", got, pasta)
	}
}
