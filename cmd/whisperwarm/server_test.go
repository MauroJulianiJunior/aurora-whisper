package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildServerArgs_NeverBindsWildcard(t *testing.T) {
	args := buildServerArgs("models/ggml-small.bin", "pt", loopbackHost, 54321, nil)

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "0.0.0.0") {
		t.Fatalf("buildServerArgs produziu argv com 0.0.0.0 (regra do projeto: só loopback): %v", args)
	}

	want := []string{"--host", "127.0.0.1", "--port", "54321", "-m", "models/ggml-small.bin", "-l", "pt"}
	if len(args) != len(want) {
		t.Fatalf("argv = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q (argv completo: %v)", i, args[i], want[i], args)
		}
	}
}

func TestBuildServerArgs_HostIsAlwaysWhatCallerPasses(t *testing.T) {
	// buildServerArgs em si é uma função pura: quem decide loopback é
	// startWhisperServer, que só chama com loopbackHost. Este teste
	// prova que loopbackHost É "127.0.0.1" (a constante que
	// startWhisperServer efetivamente usa) — se alguém mudar a
	// constante para algo diferente de loopback, este teste quebra.
	if loopbackHost != "127.0.0.1" {
		t.Fatalf("loopbackHost = %q, want 127.0.0.1", loopbackHost)
	}
}

func TestPickFreePort_ReturnsUsablePortOnLoopback(t *testing.T) {
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("pickFreePort devolveu porta fora de faixa: %d", port)
	}

	// Prova que a porta realmente ficou livre depois do pickFreePort
	// fechar o listener de sondagem: consegue bindar de novo nela.
	ln, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("porta %d de pickFreePort não ficou livre: %v", port, err)
	}
	ln.Close()
}

func TestWaitReady_SucceedsAsSoonAsServerResponds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	never := make(chan error) // nunca dispara: processo não saiu

	start := time.Now()
	err := waitReady(srv.URL, 5*time.Second, never)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("waitReady: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("waitReady demorou %v para reconhecer um servidor já respondendo (timeout configurado: 5s)", elapsed)
	}
}

func TestWaitReady_TimesOutWhenNothingListens(t *testing.T) {
	// Porta que quase certamente não tem ninguém escutando: pega uma
	// porta livre e nunca sobe servidor nela.
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	baseURL := fmt.Sprintf("http://%s:%d", loopbackHost, port)
	never := make(chan error)

	start := time.Now()
	err = waitReady(baseURL, 500*time.Millisecond, never)
	elapsed := time.Since(start)

	if !errors.Is(err, errReadyTimeout) {
		t.Fatalf("err = %v, want wrapping errReadyTimeout", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("waitReady demorou %v para desistir (timeout configurado: 500ms)", elapsed)
	}
}

func TestWaitReady_FailsFastWhenProcessAlreadyExited(t *testing.T) {
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	baseURL := fmt.Sprintf("http://%s:%d", loopbackHost, port)

	exited := make(chan error, 1)
	exited <- fmt.Errorf("exit status 1")

	start := time.Now()
	// Timeout longo de propósito: se waitReady só desistisse por
	// timeout (ignorando procExited), este teste estouraria os 2s de
	// verificação abaixo mesmo passando no fim das contas.
	err = waitReady(baseURL, 10*time.Second, exited)
	elapsed := time.Since(start)

	if !errors.Is(err, errProcessExited) {
		t.Fatalf("err = %v, want wrapping errProcessExited", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("waitReady demorou %v para reconhecer processo já morto (deveria ser quase instantâneo, timeout era 10s)", elapsed)
	}
}

// TestBuildServerArgs_RepassaFlagsDeDesempenho trava o repasse de -ac e -t
// para o whisper-server.
//
// Sem ele, o modo warm mediria uma configuração PIOR que a do modo cold —
// threads default 4 numa máquina de 20, e janela de encoder de 30 s inteira
// para uma fala de 1,6 s — e a comparação entre cold e warm, que é o ponto
// do ADR-012, compararia coisas diferentes achando que são a mesma.
func TestBuildServerArgs_RepassaFlagsDeDesempenho(t *testing.T) {
	extra := []string{"-ac", "750", "-t", "12"}
	args := buildServerArgs("models/quant/small-q4_0.bin", "pt", loopbackHost, 54321, extra)

	linha := strings.Join(args, " ")
	for _, esperado := range []string{"-ac 750", "-t 12", "-m models/quant/small-q4_0.bin", "--host " + loopbackHost} {
		if !strings.Contains(linha, esperado) {
			t.Errorf("argv = %q, falta %q", linha, esperado)
		}
	}
	// O host NUNCA pode deixar de ser loopback, nem por flag extra.
	if strings.Contains(linha, "0.0.0.0") {
		t.Errorf("argv = %q — bind fora de loopback", linha)
	}
}
