package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run contém toda a lógica de main, parametrizada por argumentos e
// streams — permite testar o parsing de flags e o fluxo de erro sem
// tocar os.Args/os.Stdin/os.Stdout/os.Stderr reais.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("whisperwarm", flag.ContinueOnError)
	fs.SetOutput(stderr)

	serverPath := fs.String("server", "", "caminho do whisper-server.exe (obrigatório)")
	modelPath := fs.String("model", "", "caminho do modelo .bin (obrigatório)")
	lang := fs.String("lang", "pt", "idioma passado ao whisper-server (-l)")
	readyTimeout := fs.Duration("ready-timeout", 60*time.Second, "tempo máximo esperando o whisper-server ficar pronto")

	// Flags de desempenho do whisper-server (-ac, -t, -nfa...) vao DEPOIS
	// de "--". Sem o separador, o pacote flag tenta parsea-los como flags
	// deste adaptador e aborta — e quem chama ve so "46 falhas" sem causa.
	if err := fs.Parse(args); err != nil {
		return 2 // flag já escreveu o erro/usage em stderr
	}
	if *serverPath == "" || *modelPath == "" {
		fmt.Fprintln(stderr, "whisperwarm: -server e -model são obrigatórios")
		fs.Usage()
		return 2
	}

	sp, err := startWhisperServer(*serverPath, *modelPath, *lang, *readyTimeout, fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "whisperwarm: %v\n", err)
		return 1
	}
	defer func() {
		if err := sp.Close(); err != nil {
			fmt.Fprintf(stderr, "whisperwarm: encerrando whisper-server: %v\n", err)
		}
	}()

	c := newClient(sp.baseURL)
	transcribe := func(audioPath string) (string, error) {
		return c.Transcribe(context.Background(), audioPath)
	}

	if err := runProtocol(stdin, stdout, stderr, transcribe); err != nil {
		fmt.Fprintf(stderr, "whisperwarm: %v\n", err)
		return 1
	}
	return 0
}
