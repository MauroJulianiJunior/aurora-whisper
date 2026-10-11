package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// loopbackHost é o único host que o whisper-server filho pode bindar.
// Não existe flag para trocar isso — CLAUDE.md do projeto é explícito:
// "API local deve bindar somente em loopback", nunca 0.0.0.0.
const loopbackHost = "127.0.0.1"

// readyPollInterval é o intervalo entre tentativas de GET / enquanto
// esperamos o whisper-server ficar pronto para aceitar POST /inference.
const readyPollInterval = 100 * time.Millisecond

// readyProbeTimeout é o timeout de cada tentativa individual de poll —
// deliberadamente curto, bem menor que -ready-timeout, para uma única
// tentativa travada não consumir o orçamento inteiro de espera.
const readyProbeTimeout = 2 * time.Second

// maxPortAttempts é o número de portas efêmeras diferentes que
// tentamos antes de desistir de iniciar o whisper-server. Existe uma
// janela pequena entre "escolher uma porta livre" (pickFreePort fecha o
// listener de sondagem) e "o whisper-server bindar essa mesma porta" —
// em outro processo pegar a porta nesse meio-tempo, tentamos de novo em
// vez de falhar direto.
const maxPortAttempts = 3

// errReadyTimeout é devolvido (encapsulado) quando o whisper-server não
// respondeu dentro de -ready-timeout.
var errReadyTimeout = errors.New("tempo esgotado esperando whisper-server ficar pronto")

// errProcessExited é devolvido (encapsulado) por waitReady quando o
// processo whisper-server já saiu sozinho antes de ficar pronto — nesse
// caso o canal procExited já foi drenado por waitReady, e o chamador
// (startWhisperServer) não deve tentar receber dele de novo nem chamar
// Kill (o processo já está morto).
var errProcessExited = errors.New("whisper-server encerrou antes de ficar pronto")

// serverProcess é o whisper-server filho já pronto para receber
// requisições em baseURL.
type serverProcess struct {
	cmd     *exec.Cmd
	baseURL string
	exited  chan error
}

// buildServerArgs monta o argv do whisper-server: função pura, sem
// nenhum I/O, para que a regra de loopback (e o resto do argv) sejam
// verificáveis por teste sem precisar subir processo nenhum.
// extraArgs repassa flags de desempenho direto para o whisper-server.
// Existem porque os defaults do binário deixam muito na mesa, e medimos:
// threads default é 4 numa máquina de 20 (12 satura e ganha 22%), e
// audio-ctx default processa a janela inteira de 30 s mesmo para uma fala
// de 1,6 s (750 corta 40% sem estragar a transcrição). Sem esse repasse, o
// modo warm mediria uma configuração pior que a do modo cold, e a
// comparação entre os dois não valeria nada.
func buildServerArgs(modelPath, lang, host string, port int, extraArgs []string) []string {
	args := []string{
		"--host", host,
		"--port", strconv.Itoa(port),
		"-m", modelPath,
		"-l", lang,
	}
	return append(args, extraArgs...)
}

// pickFreePort pede ao SO uma porta livre em loopbackHost: abre um
// listener em :0, lê a porta escolhida e fecha o listener imediatamente
// para o whisper-server (um processo externo, que não sabe herdar um
// socket já aberto deste harness) poder bindar essa mesma porta. Existe
// uma janela de corrida entre o Close() e o whisper-server bindar — por
// isso startWhisperServer tenta mais de uma porta antes de desistir.
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("whisperwarm: escolhendo porta livre em %s: %w", loopbackHost, err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// waitReady sonda baseURL até ele responder (qualquer status HTTP conta
// como "processo vivo e servindo"), até timeout se esgotar. procExited
// permite falhar rápido: se o processo já saiu (ex.: modelo inválido),
// não faz sentido esperar o timeout inteiro para descobrir.
func waitReady(baseURL string, timeout time.Duration, procExited <-chan error) error {
	client := &http.Client{Timeout: readyProbeTimeout}
	deadline := time.After(timeout)
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()

	for {
		resp, err := client.Get(baseURL + "/")
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}

		select {
		case werr := <-procExited:
			return fmt.Errorf("whisperwarm: %w: %v", errProcessExited, werr)
		case <-deadline:
			return fmt.Errorf("whisperwarm: %w (%s, timeout %s)", errReadyTimeout, baseURL, timeout)
		case <-ticker.C:
		}
	}
}

// startWhisperServer inicia serverPath como processo filho servindo
// modelPath em loopback numa porta efêmera, e só retorna depois que
// waitReady confirma que ele está aceitando requisições (ou desiste,
// devolvendo erro e garantindo que nenhum processo fique vazado).
func startWhisperServer(serverPath, modelPath, lang string, readyTimeout time.Duration, extraArgs []string) (*serverProcess, error) {
	var lastErr error
	for attempt := 0; attempt < maxPortAttempts; attempt++ {
		port, err := pickFreePort()
		if err != nil {
			return nil, err
		}

		cmd := exec.Command(serverPath, buildServerArgs(modelPath, lang, loopbackHost, port, extraArgs)...)
		// O log do whisper-server (carregamento de modelo, erros de
		// requisição) vai para o nosso stderr — nunca para stdout, que é
		// reservado exclusivamente para o protocolo de linha do harness.
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			lastErr = fmt.Errorf("whisperwarm: iniciando %s: %w", serverPath, err)
			continue
		}

		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()

		baseURL := fmt.Sprintf("http://%s:%d", loopbackHost, port)
		if err := waitReady(baseURL, readyTimeout, exited); err != nil {
			if !errors.Is(err, errProcessExited) {
				// Processo ainda vivo (só não respondeu a tempo): mata e
				// espera a goroutine de Wait() reapear antes de tentar
				// outra porta. Se for errProcessExited, o processo já
				// morreu sozinho e já drenou "exited" dentro de
				// waitReady — Kill() e um segundo receive travariam.
				_ = cmd.Process.Kill()
				<-exited
			}
			lastErr = err
			continue
		}

		return &serverProcess{cmd: cmd, baseURL: baseURL, exited: exited}, nil
	}
	return nil, fmt.Errorf("whisperwarm: não foi possível iniciar whisper-server após %d tentativa(s): %w", maxPortAttempts, lastErr)
}

// closeGraceTimeout é quanto tempo esperamos o whisper-server sair
// depois de Kill() antes de considerar Close() em si mesmo travado (não
// deveria acontecer — Kill não falha silenciosamente no Windows — mas
// Close nunca deve travar o encerramento do whisperwarm, mesma
// preocupação de WarmSession.Close em internal/benchmark/candidate).
const closeGraceTimeout = 5 * time.Second

// Close mata o processo whisper-server filho e espera ele sair. Não há
// endpoint HTTP de shutdown documentado no whisper-server, então a única
// forma confiável de encerrar é matar o processo — não é o caminho
// "gentil" de warm.go (que fecha stdin e espera saída limpa), mas o
// whisper-server não lê stdin como sinal de nada.
//
// Um processo morto por Kill() sai com código de saída não-zero por
// definição (TerminateProcess no Windows, sinal no Unix) — isso é o
// resultado ESPERADO de um encerramento deliberado, não uma falha.
// Close() por isso engole *exec.ExitError (medido: "exit status 1" toda
// vez que este Close roda com sucesso) e só devolve erro para algo
// realmente anômalo: o processo não sair a tempo, ou Wait() falhar de
// um jeito que não seja "saiu com código de erro" (ex.: I/O do próprio
// processo).
func (sp *serverProcess) Close() error {
	if sp.cmd.Process != nil {
		_ = sp.cmd.Process.Kill()
	}
	select {
	case err := <-sp.exited:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil // esperado: matamos o processo de propósito
		}
		return err
	case <-time.After(closeGraceTimeout):
		return fmt.Errorf("whisperwarm: whisper-server não encerrou %s após kill", closeGraceTimeout)
	}
}
