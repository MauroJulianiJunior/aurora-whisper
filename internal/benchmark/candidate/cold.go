package candidate

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/rss"
)

// rssSampleInterval é o intervalo entre amostras de RSS durante uma
// chamada cold ou warm. Curto o bastante para capturar o pico de um
// modelo carregando, sem virar carga perceptível sobre o próprio
// candidato que está sendo medido.
const rssSampleInterval = 20 * time.Millisecond

// RunCold roda cfg em modo cold: inicia um processo novo, com o caminho
// de audioPath substituído em ColdArgs, espera terminar, e lê a
// transcrição do stdout (trimado). É exatamente o custo de "nada fica
// vivo entre transcrições" que a tabela do ADR-012 chama de cold.
//
// Se cfg.Executable estiver vazio ou ColdArgs não contiver
// AudioPlaceholder, devolve erro sem tentar rodar nada. Se o processo não
// terminar dentro de cfg.timeout(), é morto e RunCold devolve ErrTimeout.
// Um processo que termina com código de saída não-zero é reportado como
// erro com o conteúdo de stderr, nunca como sucesso silencioso.
func RunCold(ctx context.Context, cfg Config, audioPath string) (Result, error) {
	if cfg.Executable == "" {
		return Result{}, ErrEmptyExecutable
	}
	args, err := substitutePlaceholder(cfg.ColdArgs, audioPath)
	if err != nil {
		return Result{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, cfg.timeout())
	defer cancel()

	cmd := exec.CommandContext(callCtx, cfg.Executable, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("candidate: iniciando %s: %w", cfg.Name, err)
	}

	stopWatch := startRSSWatcher(cmd.Process.Pid)

	start := time.Now()
	err = cmd.Wait()
	elapsed := time.Since(start)
	peakBytes, measured := stopWatch()

	if callCtx.Err() != nil {
		return Result{}, fmt.Errorf("candidate: %s: %w", cfg.Name, ErrTimeout)
	}
	if err != nil {
		return Result{}, fmt.Errorf("candidate: %s saiu com erro: %w (stderr: %s)",
			cfg.Name, err, strings.TrimSpace(stderr.String()))
	}

	return Result{
		Text:         strings.TrimSpace(stdout.String()),
		WallTime:     elapsed,
		PeakRSSBytes: peakBytes,
		RSSMeasured:  measured,
	}, nil
}

// startRSSWatcher dispara uma goroutine que amostra internal/benchmark/rss
// (a árvore inteira do processo — rss.SampleTree, não só o PID que o
// harness iniciou: um candidato "warm com adaptador", como
// cmd/whisperwarm, sobe o runtime de verdade como processo filho, e é o
// filho quem segura o modelo) para pid em intervalos de
// rssSampleInterval, e devolve uma função stop que sinaliza a goroutine
// para parar, espera ela terminar e entrega o maior valor observado.
// Devolve (0, false) se nenhuma amostra bem-sucedida ocorreu — processo
// curto demais para uma amostra, ou plataforma sem suporte de medição
// (internal/benchmark/rss). Para um candidato sem filhos (cold, ou warm
// sem adaptador), rss.SampleTree devolve o mesmo valor que rss.Sample
// devolveria — ver rss.TestSumTree_SingleNode.
//
// stop() só deve ser chamada uma vez, depois que o processo monitorado já
// terminou (cmd.Wait() retornou) — chamar antes não é incorreto (a
// goroutine simplesmente para de amostrar mais cedo), mas mediria menos
// do que o processo realmente usou.
func startRSSWatcher(pid int) (stop func() (int64, bool)) {
	stopCh := make(chan struct{})
	done := make(chan struct{})
	var maxBytes int64
	var anyOK bool

	go func() {
		defer close(done)
		ticker := time.NewTicker(rssSampleInterval)
		defer ticker.Stop()
		for {
			if b, ok := rss.SampleTree(pid); ok {
				anyOK = true
				if b > maxBytes {
					maxBytes = b
				}
			}
			select {
			case <-stopCh:
				return
			case <-ticker.C:
			}
		}
	}()

	return func() (int64, bool) {
		close(stopCh)
		<-done // aguarda a última escrita em maxBytes/anyOK antes de ler
		return maxBytes, anyOK
	}
}
