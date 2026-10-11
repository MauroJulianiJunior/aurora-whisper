package candidate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// WarmSession é um processo candidato residente, medido em modo warm
// (ADR-012): iniciado uma vez, reutilizado para todas as transcrições. O
// protocolo é uma linha de stdin (caminho do áudio) por uma linha de
// stdout (transcrição) — o mínimo que qualquer runtime "modo servidor"
// consegue implementar sem que este harness precise conhecer nada
// específico do candidato.
//
// Um Config sem WarmArgs não tem WarmSession — StartWarmSession devolve
// ErrWarmNotSupported, e quem monta o relatório registra warm como "não
// medido" para esse candidato, em vez de inventar um número (ADR-012).
type WarmSession struct {
	cfg    Config
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	mu      sync.Mutex // serializa Transcribe: um processo, uma requisição por vez
	rssPeak int64
	rssOK   bool
}

// ErrWarmNotSupported é devolvido por StartWarmSession quando
// Config.WarmArgs está vazio.
var ErrWarmNotSupported = errors.New("candidate: candidato não declara WarmArgs (warm não suportado)")

// StartWarmSession inicia o processo warm de cfg e espera até
// cfg.startupTimeout() por ele ficar pronto (processo iniciado com
// sucesso; não há handshake de "pronto" no protocolo — a primeira
// Transcribe é quem paga o custo de carregamento se o candidato não
// sinalizar prontidão de outra forma, o que é medido como parte do
// primeiro WallTime, nunca escondido).
func StartWarmSession(ctx context.Context, cfg Config) (*WarmSession, error) {
	if cfg.Executable == "" {
		return nil, ErrEmptyExecutable
	}
	if len(cfg.WarmArgs) == 0 {
		return nil, ErrWarmNotSupported
	}

	cmd := exec.Command(cfg.Executable, cfg.WarmArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("candidate: %s: stdin pipe: %w", cfg.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("candidate: %s: stdout pipe: %w", cfg.Name, err)
	}

	startCtx, cancel := context.WithTimeout(ctx, cfg.startupTimeout())
	defer cancel()

	started := make(chan error, 1)
	go func() { started <- cmd.Start() }()

	select {
	case err := <-started:
		if err != nil {
			return nil, fmt.Errorf("candidate: iniciando %s (warm): %w", cfg.Name, err)
		}
	case <-startCtx.Done():
		return nil, fmt.Errorf("candidate: %s: %w ao iniciar processo warm", cfg.Name, ErrTimeout)
	}

	ws := &WarmSession{
		cfg:    cfg,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdout),
	}
	return ws, nil
}

// Transcribe envia audioPath como uma linha para o processo warm e espera
// uma linha de resposta, com timeout cfg.timeout(). Chamadas concorrentes
// na mesma WarmSession são serializadas — o protocolo de linha não tem
// como correlacionar requisição e resposta fora de ordem.
func (ws *WarmSession) Transcribe(ctx context.Context, audioPath string) (Result, error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	stopWatch := startRSSWatcher(ws.cmd.Process.Pid)

	type lineResult struct {
		text string
		err  error
	}
	respCh := make(chan lineResult, 1)

	start := time.Now()
	if _, err := io.WriteString(ws.stdin, audioPath+"\n"); err != nil {
		stopWatch()
		return Result{}, fmt.Errorf("candidate: %s (warm): escrevendo requisição: %w", ws.cfg.Name, err)
	}

	go func() {
		if ws.stdout.Scan() {
			respCh <- lineResult{text: ws.stdout.Text()}
			return
		}
		err := ws.stdout.Err()
		if err == nil {
			err = io.EOF
		}
		respCh <- lineResult{err: fmt.Errorf("candidate: %s (warm): processo fechou stdout: %w", ws.cfg.Name, err)}
	}()

	callCtx, cancel := context.WithTimeout(ctx, ws.cfg.timeout())
	defer cancel()

	select {
	case r := <-respCh:
		elapsed := time.Since(start)
		peakBytes, measured := stopWatch()
		if r.err != nil {
			return Result{}, r.err
		}
		return Result{
			Text:         strings.TrimSpace(r.text),
			WallTime:     elapsed,
			PeakRSSBytes: peakBytes,
			RSSMeasured:  measured,
		}, nil
	case <-callCtx.Done():
		stopWatch()
		return Result{}, fmt.Errorf("candidate: %s (warm): %w", ws.cfg.Name, ErrTimeout)
	}
}

// Close encerra o processo warm: fecha stdin (sinal de fim para
// candidatos bem-comportados), espera até cfg.timeout() por saída
// limpa e, se não sair a tempo, mata o processo. Sempre tenta matar
// como último recurso — uma WarmSession nunca deve vazar um processo
// travado na máquina do usuário (docs/CLAUDE.md, "Testes que agem no
// sistema real": esta é a mesma preocupação, aplicada ao processo
// candidato em vez do foco do desktop).
func (ws *WarmSession) Close() error {
	_ = ws.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- ws.cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(ws.cfg.timeout()):
		_ = ws.cmd.Process.Kill()
		<-done
		return fmt.Errorf("candidate: %s (warm): não encerrou a tempo, processo morto", ws.cfg.Name)
	}
}
