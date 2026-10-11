package main

import (
	"fmt"
	"io"
	"os/exec"
	"time"
)

// pipeTransport mede o transporte que whisper-cli usa (`-f -`): dados
// crus por stdin, resposta crua por stdout, do mesmo processo — sem
// porta, sem handshake de rede.
type pipeTransport struct{}

func (pipeTransport) name() string { return "pipe (stdin/stdout)" }

func (pipeTransport) coldRoundTrip(exePath string, payload []byte) (time.Duration, []byte, error) {
	start := time.Now()

	cmd := exec.Command(exePath, "echo-pipe")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("transportbench: pipe cold: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("transportbench: pipe cold: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("transportbench: pipe cold: start: %w", err)
	}

	if err := writeFrame(stdin, payload); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, nil, err
	}
	echoed, err := readFrame(stdout)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, nil, err
	}

	_ = stdin.Close()                  // sinal de EOF para echoPipeLoop encerrar
	_, _ = io.Copy(io.Discard, stdout) // drena o que sobrar (nada esperado) até o filho fechar stdout
	if err := cmd.Wait(); err != nil {
		return 0, nil, fmt.Errorf("transportbench: pipe cold: processo de eco saiu com erro: %w", err)
	}

	return time.Since(start), echoed, nil
}

type pipeWarmSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (pipeTransport) startWarm(exePath string) (warmSession, error) {
	cmd := exec.Command(exePath, "echo-pipe")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("transportbench: pipe warm: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("transportbench: pipe warm: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("transportbench: pipe warm: start: %w", err)
	}
	return &pipeWarmSession{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

func (s *pipeWarmSession) roundTrip(payload []byte) (time.Duration, []byte, error) {
	start := time.Now()
	if err := writeFrame(s.stdin, payload); err != nil {
		return 0, nil, err
	}
	echoed, err := readFrame(s.stdout)
	if err != nil {
		return 0, nil, err
	}
	return time.Since(start), echoed, nil
}

func (s *pipeWarmSession) close() error {
	_ = s.stdin.Close()
	_, _ = io.Copy(io.Discard, s.stdout)
	return s.cmd.Wait()
}
