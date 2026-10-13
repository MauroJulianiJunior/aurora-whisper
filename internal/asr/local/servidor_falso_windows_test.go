//go:build windows

package local

import (
	"bytes"
	"context"
	"encoding/hex"
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

// servidorFalsoComoProcesso é o que o processo filho faz: lê --port e
// --request-path do argv, escreve a pasta atual e cada argumento (um por linha)
// na saída, e, até ser morto, serve 200 só debaixo do caminho pedido. Fora dele
// corta a conexão sem resposta, que é mais estrito que o whisper-server (ele
// responde 404): um Start que sondasse sem o caminho nunca daria o servidor por
// pronto.
func servidorFalsoComoProcesso() {
	cwd, _ := os.Getwd()
	fmt.Printf("cwd=%s\n", cwd)
	porta, caminho := "", ""
	for i, a := range os.Args[1:] {
		fmt.Printf("arg=%s\n", a)
		if i+2 < len(os.Args) {
			switch a {
			case "--port":
				porta = os.Args[i+2]
			case "--request-path":
				caminho = os.Args[i+2]
			}
		}
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", porta))
	if err != nil {
		fmt.Printf("erro=%v\n", err)
		os.Exit(3)
	}
	_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caminho == "" || !strings.HasPrefix(r.URL.Path, caminho+"/") {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
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

	end, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v\nsaída do filho:\n%s", err, saida.String())
	}
	defer p.Stop()
	if !strings.HasPrefix(end.base, "http://127.0.0.1:") {
		t.Errorf("base = %q, quero loopback", end.base)
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(end.base, "http://127.0.0.1:")); err != nil {
		t.Errorf("base = %q sem porta numérica", end.base)
	}

	cwd, args := saidaDoFilho(t, saida, end.caminho)
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

// saidaDoFilho espera o filho ter escrito o último argumento (o caminho
// secreto, que vai por último no argv) e devolve a pasta atual e os argumentos
// que ele recebeu. O filho já respondeu HTTP, então já escreveu tudo; só falta
// o os/exec copiar a saída do cano para o buffer.
func saidaDoFilho(t *testing.T, saida *bufferSeguro, ultimo string) (cwd string, args []string) {
	t.Helper()
	for fim := time.Now().Add(5 * time.Second); !strings.Contains(saida.String(), "arg="+ultimo) && time.Now().Before(fim); {
		time.Sleep(20 * time.Millisecond)
	}
	for _, l := range strings.Split(saida.String(), "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, "cwd="):
			cwd = strings.TrimPrefix(l, "cwd=")
		case strings.HasPrefix(l, "arg="):
			args = append(args, strings.TrimPrefix(l, "arg="))
		}
	}
	return cwd, args
}

// TestStart_SobeComOCaminhoSecretoEFalaPorEle é a ligação que as montagens
// puras não provam: Start sobe o servidor com um --request-path de 128 bits,
// sonda e devolve o endereço com o MESMO caminho (o servidor de mentira só
// responde debaixo dele), e a subida seguinte usa outro. O argv que o processo
// recebeu também não traz nenhuma flag que exporia o ditado.
func TestStart_SobeComOCaminhoSecretoEFalaPorEle(t *testing.T) {
	t.Setenv(envServidorFalso, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	subir := func() (endereco, []string) {
		t.Helper()
		saida := &bufferSeguro{}
		p := &windowsServerProcess{cfg: Config{
			ServerPath: exe, ModelPath: "ggml-small-q4_0.bin", Language: "pt", SemFlashAttention: true,
			ReadyTimeout: 20 * time.Second, SaidaDoServidor: saida,
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		end, err := p.Start(ctx)
		if err != nil {
			t.Fatalf("Start: %v\nsaída do filho:\n%s", err, saida.String())
		}
		t.Cleanup(func() { _ = p.Stop() })
		_, args := saidaDoFilho(t, saida, end.caminho)

		// Pelo caminho, o servidor responde; sem ele, não.
		cli := &http.Client{Timeout: 5 * time.Second}
		if resp, err := cli.Get(end.url("/")); err != nil {
			t.Errorf("GET pelo caminho: %v", err)
		} else {
			resp.Body.Close()
		}
		if resp, err := cli.Get(end.base + "/"); err == nil {
			resp.Body.Close()
			t.Errorf("GET sem o caminho respondeu %s: o servidor de mentira devia cortar", resp.Status)
		}
		return end, args
	}

	end1, args1 := subir()
	if got, ok := valorDoArgumento(args1, "--request-path"); !ok || got != end1.caminho {
		t.Errorf("o servidor recebeu --request-path %q (veio: %v), e o Agent fala com %q", got, ok, end1.caminho)
	}
	if b, err := hex.DecodeString(strings.TrimPrefix(end1.caminho, "/")); err != nil || len(b) != 16 || end1.caminho[0] != '/' {
		t.Errorf("caminho = %q, quero uma barra e 128 bits em hexadecimal", end1.caminho)
	}
	for _, a := range args1 {
		for _, proibida := range []string{"-pr", "--print-realtime", "-debug", "--debug-mode", "--convert"} {
			if a == proibida {
				t.Errorf("o servidor recebeu %q: o ditado iria para o log", a)
			}
		}
	}

	end2, _ := subir()
	if end2.caminho == end1.caminho {
		t.Errorf("duas subidas com o mesmo caminho %q", end1.caminho)
	}
}
