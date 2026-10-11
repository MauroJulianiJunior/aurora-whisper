package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// tcpTransport mede o piso de qualquer protocolo sobre socket: frames
// crus (protocolo de frame.go) sobre uma conexão TCP em loopback, sem
// HTTP por cima.
type tcpTransport struct{}

func (tcpTransport) name() string { return "tcp (loopback, cru)" }

// startEchoTCPProcess sobe o subprocesso echo-tcp e lê a porta que ele
// anunciou na primeira linha de stdout (ver echo_tcp.go). Compartilhado
// entre coldRoundTrip e startWarm — o handshake de descoberta de porta é
// o mesmo nos dois modos, só muda quantas vezes a conexão resultante é
// reaproveitada.
func startEchoTCPProcess(exePath string) (cmd *exec.Cmd, stdin io.WriteCloser, port int, err error) {
	cmd = exec.Command(exePath, "echo-tcp")
	stdin, err = cmd.StdinPipe()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("transportbench: tcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("transportbench: tcp: stdout pipe: %w", err)
	}
	if err = cmd.Start(); err != nil {
		return nil, nil, 0, fmt.Errorf("transportbench: tcp: start: %w", err)
	}

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, nil, 0, fmt.Errorf("transportbench: tcp: lendo porta anunciada: %w", err)
	}
	port, err = strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, nil, 0, fmt.Errorf("transportbench: tcp: porta anunciada invalida (%q): %w", line, err)
	}

	// Nada mais é esperado em stdout depois da linha da porta; drenar em
	// segundo plano evita que o filho bloqueie se algo inesperado for
	// escrito lá.
	go func() { _, _ = io.Copy(io.Discard, reader) }()

	return cmd, stdin, port, nil
}

func (tcpTransport) coldRoundTrip(exePath string, payload []byte) (time.Duration, []byte, error) {
	start := time.Now()

	cmd, stdin, port, err := startEchoTCPProcess(exePath)
	if err != nil {
		return 0, nil, err
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return 0, nil, fmt.Errorf("transportbench: tcp cold: dial: %w", err)
	}
	if err := writeFrame(conn, payload); err != nil {
		_ = conn.Close()
		_ = stdin.Close()
		_ = cmd.Wait()
		return 0, nil, err
	}
	echoed, err := readFrame(conn)
	_ = conn.Close()
	if err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return 0, nil, err
	}

	_ = stdin.Close() // sinal de encerrar (echo_tcp.go bloqueia até isto fechar)
	if err := cmd.Wait(); err != nil {
		return 0, nil, fmt.Errorf("transportbench: tcp cold: processo de eco saiu com erro: %w", err)
	}

	return time.Since(start), echoed, nil
}

type tcpWarmSession struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	conn  net.Conn
}

func (tcpTransport) startWarm(exePath string) (warmSession, error) {
	cmd, stdin, port, err := startEchoTCPProcess(exePath)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return nil, fmt.Errorf("transportbench: tcp warm: dial: %w", err)
	}
	return &tcpWarmSession{cmd: cmd, stdin: stdin, conn: conn}, nil
}

func (s *tcpWarmSession) roundTrip(payload []byte) (time.Duration, []byte, error) {
	start := time.Now()
	if err := writeFrame(s.conn, payload); err != nil {
		return 0, nil, err
	}
	echoed, err := readFrame(s.conn)
	if err != nil {
		return 0, nil, err
	}
	return time.Since(start), echoed, nil
}

func (s *tcpWarmSession) close() error {
	_ = s.conn.Close()
	_ = s.stdin.Close()
	return s.cmd.Wait()
}
