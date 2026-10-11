//go:build windows

package local

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Um "whisper-server" de mentira, para provar de ponta a ponta que Start sobe
// o servidor na pasta de trabalho certa e com o modelo pelo nome relativo (F2
// da ADR-018): o próprio binário de teste, reexecutado como processo filho, que
// escuta na porta pedida em 127.0.0.1, diz onde está e com que argumentos foi
// chamado, e espera ser morto. Nenhum whisper-server de verdade, nenhum modelo.
//
// O modo só liga com a variável abaixo, que apenas este teste põe (t.Setenv),
// então uma execução normal de "go test" nunca cai nele.

const envServidorFalso = "AURORA_TESTE_SERVIDOR_FALSO"

func TestMain(m *testing.M) {
	if os.Getenv(envServidorFalso) == "1" {
		servidorFalsoComoProcesso()
		return
	}
	os.Exit(m.Run())
}

// servidorFalsoComoProcesso é o que o processo filho faz: lê --port do argv,
// escreve a pasta atual e cada argumento (um por linha) na saída, e serve 200
// em / até ser morto.
func servidorFalsoComoProcesso() {
	cwd, _ := os.Getwd()
	fmt.Printf("cwd=%s\n", cwd)
	porta := ""
	for i, a := range os.Args[1:] {
		fmt.Printf("arg=%s\n", a)
		if a == "--port" && i+2 < len(os.Args) {
			porta = os.Args[i+2]
		}
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", porta))
	if err != nil {
		fmt.Printf("erro=%v\n", err)
		os.Exit(3)
	}
	_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

// bufferSeguro é a saída do filho: o os/exec escreve nela de outra goroutine
// enquanto o teste lê.
type bufferSeguro struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *bufferSeguro) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *bufferSeguro) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestStart_SobeNaPastaDoProgramaComOModeloRelativo é a ligação que as
// montagens puras não provam: Start monta o comando com a Config e o processo
// nasce, de verdade, na pasta pedida e com o -m relativo.
func TestStart_SobeNaPastaDoProgramaComOModeloRelativo(t *testing.T) {
	t.Setenv(envServidorFalso, "1")

	prog := filepath.Join(t.TempDir(), "João Çedilha", "Aurora Whisper")
	if err := os.MkdirAll(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	modelo := filepath.Join(prog, "ggml-small-q4_0.bin")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	saida := &bufferSeguro{}
	p := &windowsServerProcess{cfg: Config{
		ServerPath:      exe,
		Dir:             prog,
		ModelPath:       modelo,
		Language:        "pt",
		ReadyTimeout:    20 * time.Second,
		SaidaDoServidor: saida,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	base, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v\nsaída do filho:\n%s", err, saida.String())
	}
	defer p.Stop()
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Errorf("baseURL = %q, quero loopback", base)
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(base, "http://127.0.0.1:")); err != nil {
		t.Errorf("baseURL = %q sem porta numérica", base)
	}

	// O filho já respondeu HTTP, então já escreveu o que sabe de si; só falta
	// o os/exec copiar a saída do cano para o buffer (o último argumento, o
	// idioma, é a última linha que ele escreve antes de escutar).
	for fim := time.Now().Add(5 * time.Second); !strings.Contains(saida.String(), "arg=pt") && time.Now().Before(fim); {
		time.Sleep(20 * time.Millisecond)
	}
	var cwd string
	var args []string
	for _, l := range strings.Split(saida.String(), "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, "cwd="):
			cwd = strings.TrimPrefix(l, "cwd=")
		case strings.HasPrefix(l, "arg="):
			args = append(args, strings.TrimPrefix(l, "arg="))
		}
	}
	if !strings.EqualFold(cwd, prog) {
		t.Errorf("o servidor rodou em %q, quero a pasta do programa %q", cwd, prog)
	}
	i := indice(args, "-m")
	if i < 0 || i+1 >= len(args) || args[i+1] != "ggml-small-q4_0.bin" {
		t.Errorf("argv do servidor = %q, quero -m com o nome do arquivo, sem pasta", args)
	}
	for _, a := range args {
		if strings.Contains(a, "João") {
			t.Errorf("o argumento %q leva o nome do usuário", a)
		}
	}
}
