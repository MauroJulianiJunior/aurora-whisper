package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// httpTransport mede o protocolo que whisper-server e nemo-speech serve
// usam: POST multipart em loopback, com o net/http.Client padrão da
// stdlib (que já reaproveita conexão em modo warm — keep-alive é o
// comportamento default de http.Transport).
type httpTransport struct{}

func (httpTransport) name() string { return "http POST multipart (loopback)" }

// startEchoHTTPProcess sobe o subprocesso echo-http e lê a porta
// anunciada, mesmo handshake de startEchoTCPProcess (transport_tcp.go).
func startEchoHTTPProcess(exePath string) (cmd *exec.Cmd, stdin io.WriteCloser, port int, err error) {
	cmd = exec.Command(exePath, "echo-http")
	stdin, err = cmd.StdinPipe()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("transportbench: http: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("transportbench: http: stdout pipe: %w", err)
	}
	if err = cmd.Start(); err != nil {
		return nil, nil, 0, fmt.Errorf("transportbench: http: start: %w", err)
	}

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, nil, 0, fmt.Errorf("transportbench: http: lendo porta anunciada: %w", err)
	}
	port, err = strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, nil, 0, fmt.Errorf("transportbench: http: porta anunciada invalida (%q): %w", line, err)
	}

	go func() { _, _ = io.Copy(io.Discard, reader) }()

	return cmd, stdin, port, nil
}

// postMultipart monta e envia um POST multipart com payload como o
// único campo de arquivo, e devolve o corpo da resposta cru. Chamado
// tanto pelo cold (client descartável) quanto pelo warm (client
// reaproveitado entre chamadas — é o que testa o custo de conexão já
// estabelecida).
func postMultipart(client *http.Client, port int, payload []byte) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("audio", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("transportbench: http: montando multipart: %w", err)
	}
	if _, err := part.Write(payload); err != nil {
		return nil, fmt.Errorf("transportbench: http: escrevendo multipart: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("transportbench: http: fechando multipart: %w", err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("transportbench: http: montando requisicao: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transportbench: http: requisicao: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("transportbench: http: status %s", resp.Status)
	}
	echoed, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("transportbench: http: lendo resposta: %w", err)
	}
	return echoed, nil
}

func (httpTransport) coldRoundTrip(exePath string, payload []byte) (time.Duration, []byte, error) {
	start := time.Now()

	cmd, stdin, port, err := startEchoHTTPProcess(exePath)
	if err != nil {
		return 0, nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	echoed, postErr := postMultipart(client, port, payload)
	client.CloseIdleConnections()
	if postErr != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return 0, nil, postErr
	}

	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		return 0, nil, fmt.Errorf("transportbench: http cold: processo de eco saiu com erro: %w", err)
	}

	return time.Since(start), echoed, nil
}

type httpWarmSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	client *http.Client
	port   int
}

func (httpTransport) startWarm(exePath string) (warmSession, error) {
	cmd, stdin, port, err := startEchoHTTPProcess(exePath)
	if err != nil {
		return nil, err
	}
	return &httpWarmSession{
		cmd:    cmd,
		stdin:  stdin,
		client: &http.Client{Timeout: 30 * time.Second},
		port:   port,
	}, nil
}

func (s *httpWarmSession) roundTrip(payload []byte) (time.Duration, []byte, error) {
	start := time.Now()
	echoed, err := postMultipart(s.client, s.port, payload)
	if err != nil {
		return 0, nil, err
	}
	return time.Since(start), echoed, nil
}

func (s *httpWarmSession) close() error {
	s.client.CloseIdleConnections()
	_ = s.stdin.Close()
	return s.cmd.Wait()
}
