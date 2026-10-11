//go:build windows

package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// loopbackHost é o único host que o whisper-server filho pode bindar. Um
// servidor de ASR escutando em 0.0.0.0 exporia transcrição do que a pessoa
// fala para a rede local — CLAUDE.md do projeto é explícito sobre isso.
const loopbackHost = "127.0.0.1"

// maxPortAttempts é quantas portas efêmeras diferentes tentamos antes de
// desistir de iniciar o whisper-server. Existe uma janela pequena entre
// "escolher uma porta livre" (pickFreePort fecha o listener de sondagem) e
// "o whisper-server bindar essa mesma porta" — outro processo pode pegar a
// porta nesse meio-tempo. Mesmo padrão de cmd/whisperwarm/server.go.
const maxPortAttempts = 3

// stopGraceTimeout é quanto Stop espera o processo sair depois de Kill()
// antes de considerar o encerramento travado. Mesma preocupação de
// cmd/whisperwarm/server.go (closeGraceTimeout): Stop nunca deve travar o
// encerramento de quem chama.
const stopGraceTimeout = 5 * time.Second

// readyPollInterval e readyProbeTimeout: mesmo padrão de
// cmd/whisperwarm/server.go — sonda / a cada readyPollInterval, cada
// tentativa individual limitada a readyProbeTimeout para uma única
// tentativa travada não consumir o orçamento inteiro de espera.
const (
	readyPollInterval = 100 * time.Millisecond
	readyProbeTimeout = 2 * time.Second
)

// errJobAssignFailed é devolvido (encapsulado) quando o Job Object não pôde
// ser criado ou o processo não pôde ser atrelado a ele.
//
// A decisão, registrada aqui porque não é óbvia: Start FALHA nesse caso, em
// vez de subir o whisper-server sem proteção contra órfão. O motivo deste
// pacote existir é justamente evitar que o servidor fique de pé segurando
// 365 MB para sempre se o Agent morrer sem passar por Stop (doc.go, "o que
// este pacote precisa resolver"). Subir mesmo assim "só para o ditado
// funcionar agora" reintroduziria em silêncio o exato defeito que este
// código existe para fechar — e falhar aqui não é grave: garantirDePe (em
// provider.go) já trata falha de Start com backoff e ErrServerDead, o
// mesmo caminho que trata qualquer outro jeito do servidor não subir.
var errJobAssignFailed = errors.New("asr/local: Job Object não pôde ser atrelado ao whisper-server — recusando subir sem proteção contra processo órfão")

func newPlatformProcess(cfg Config) serverProcess { return &windowsServerProcess{cfg: cfg} }

// windowsServerProcess implementa serverProcess subindo whisper-server.exe
// como processo filho, contido num Job Object que garante que ele nunca
// fica órfão (ver processoContido).
type windowsServerProcess struct {
	cfg Config
	pc  *processoContido
}

func (p *windowsServerProcess) Start(ctx context.Context) (string, error) {
	if p.pc != nil {
		return "", fmt.Errorf("asr/local: Start chamado mais de uma vez no mesmo processo")
	}

	var ultimo error
	for tentativa := 0; tentativa < maxPortAttempts; tentativa++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		port, err := pickFreePort()
		if err != nil {
			return "", err
		}

		pc, err := iniciarProcessoContidoCom(montarComando(p.cfg, loopbackHost, port), p.cfg.SaidaDoServidor)
		if err != nil {
			ultimo = err
			continue
		}

		baseURL := fmt.Sprintf("http://%s:%d", loopbackHost, port)
		if err := waitReady(ctx, baseURL, p.cfg.readyTimeout(), pc.done); err != nil {
			pc.matar()
			ultimo = err
			continue
		}

		p.pc = pc
		return baseURL, nil
	}
	return "", fmt.Errorf("asr/local: não foi possível iniciar whisper-server após %d tentativa(s): %w", maxPortAttempts, ultimo)
}

func (p *windowsServerProcess) Alive() bool {
	if p.pc == nil {
		return false
	}
	select {
	case <-p.pc.done:
		return false
	default:
		return true
	}
}

// Stop mata o processo filho, espera ele sair, e só então fecha o handle
// do Job — nessa ordem, Kill() é o caminho normal de encerramento, e o
// Job (com KILL_ON_JOB_CLOSE) é a rede de segurança se Kill() por algum
// motivo não bastar. Não há endpoint HTTP de shutdown documentado no
// whisper-server, mesma observação de cmd/whisperwarm/server.go.
func (p *windowsServerProcess) Stop() error {
	if p.pc == nil {
		return nil
	}
	pc := p.pc
	p.pc = nil

	if pc.cmd.Process != nil {
		_ = pc.cmd.Process.Kill()
	}

	var stopErr error
	select {
	case <-pc.done:
		// Um processo morto por Kill() sai com código de saída não-zero
		// por definição — resultado ESPERADO de um encerramento
		// deliberado, não uma falha. Engole *exec.ExitError pelo mesmo
		// motivo que cmd/whisperwarm/server.go engole.
		var exitErr *exec.ExitError
		if pc.waitErr != nil && !errors.As(pc.waitErr, &exitErr) {
			stopErr = pc.waitErr
		}
	case <-time.After(stopGraceTimeout):
		stopErr = fmt.Errorf("asr/local: whisper-server não encerrou %s após kill", stopGraceTimeout)
	}

	if pc.job != 0 {
		// KILL_ON_JOB_CLOSE: fechar o último handle do Job mata o
		// processo (e qualquer descendente) se Kill() acima não tiver
		// bastado — rede de segurança, não o caminho principal.
		_ = windows.CloseHandle(pc.job)
	}
	return stopErr
}

// processoContido é um processo filho já atrelado a um Job Object com
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, com a thread principal já retomada.
//
// Reúne exatamente a parte deste arquivo que trata do ciclo de
// vida processo/Job — a prontidão HTTP (específica de whisper-server) fica
// de fora de propósito, em waitReady, para os testes poderem exercitar
// isto com qualquer binário trivial no lugar do whisper-server, sem falar
// HTTP e sem carregar modelo nenhum.
type processoContido struct {
	cmd     *exec.Cmd
	job     windows.Handle
	done    chan struct{} // fechado quando cmd.Wait() retorna
	waitErr error         // válido só depois de done fechado — ver a goroutine em iniciarProcessoContido
}

// iniciarProcessoContido inicia caminho com args como processo filho,
// atrelado a um Job Object recém-criado com KILL_ON_JOB_CLOSE, e devolve o
// processo só depois de já estar com a thread principal rodando.
//
// # Como o filho não escapa antes de estar no Job
//
// O filho nasce com CREATE_SUSPENDED: a thread principal existe mas não
// executa nenhuma instrução própria até nós a retomarmos explicitamente lá
// embaixo (resumeMainThread), DEPOIS de AssignProcessToJobObject já ter
// retornado com sucesso. Um processo cuja única thread nunca rodou não
// pode ter criado processo nenhum — não existe código seu em execução para
// fazer isso. Portanto, entre "o processo existe" e "o processo está no
// Job", ele é estruturalmente incapaz de gerar um neto que escaparia do
// Job, não importa quanto tempo esse intervalo leve.
//
// A janela de corrida que REALMENTE sobra é outra: entre cmd.Start()
// retornar e AssignProcessToJobObject terminar, se o próprio Agent for
// morto (TerminateProcess, queda de energia, BSOD) bem nesse instante, o
// whisper-server suspenso ainda não é membro do Job e não seria morto
// junto. Ele sobraria como processo órfão — mas um órfão que nunca chegou
// a rodar main(): não carregou o modelo, não abriu a porta, não alocou os
// 365 MB que este pacote inteiro existe para não deixar escapar (doc.go).
// É um vazamento de processo vazio, não de memória de modelo — e a janela
// para isso é só os poucos microssegundos de bookkeeping do Go entre duas
// chamadas de sistema adjacentes, sem I/O nem ponto de espera no meio.
//
// # Se CreateJobObject ou AssignProcessToJobObject falhar
//
// Start (o chamador) propaga o erro e desiste dessa tentativa — ver
// errJobAssignFailed para o porquê de não haver um "modo sem proteção".
//
// # Handles
//
// O handle de processo usado em AssignProcessToJobObject vem de
// cmd.Process.WithHandle, que empresta o handle que o próprio *exec.Cmd já
// mantém — não abrimos (nem precisamos fechar) um handle novo via
// OpenProcess. O handle do Job é responsabilidade de quem recebe o
// *processoContido: matar() fecha em caso de falha aqui dentro, e
// windowsServerProcess.Stop fecha no caminho de sucesso.
func iniciarProcessoContido(caminho string, args []string) (*processoContido, error) {
	return iniciarProcessoContidoCom(comandoDoServidor{Caminho: caminho, Args: args}, nil)
}

// iniciarProcessoContidoCom é iniciarProcessoContido com a pasta de trabalho
// do comando (ADR-018, F2) e destino explícito para a saída do filho. Nil
// manda para o stderr do Agent.
func iniciarProcessoContidoCom(comando comandoDoServidor, saida io.Writer) (*processoContido, error) {
	caminho := comando.Caminho
	job, err := newKillOnCloseJob()
	if err != nil {
		return nil, err
	}

	// A pasta de trabalho (Dir) e a saída do filho (carregamento de modelo,
	// erros de requisição, para saida ou para o nosso stderr quando ela é
	// nil — mesmo padrão de cmd/whisperwarm/server.go) são ligadas em
	// execCmd, que é pura e tem teste.
	cmd := comando.execCmd(saida)
	// CREATE_NO_WINDOW desde 28-set-2026. Enquanto o Agent era um
	// programa de console, o whisper-server herdava o console dele e isso
	// não fazia diferença. Rodando como aplicativo de bandeja, sem console,
	// o Windows criaria um console NOVO para o filho — uma janela preta
	// aberta a sessão inteira na tela de quem usa. O servidor nunca precisa
	// de janela: a conversa com ele é por HTTP.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW}

	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("asr/local: iniciando %s: %w", caminho, err)
	}

	pc := &processoContido{cmd: cmd, job: job, done: make(chan struct{})}
	go func() {
		// Escrever waitErr e SÓ DEPOIS fechar done publica waitErr para
		// qualquer goroutine que observe done fechado (happens-before via
		// fechamento de canal) — sem precisar de mutex para este campo.
		pc.waitErr = cmd.Wait()
		close(pc.done)
	}()

	var assignErr error
	if err := cmd.Process.WithHandle(func(h uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(h))
	}); err != nil {
		pc.matar()
		return nil, fmt.Errorf("asr/local: sem handle de processo para atrelar ao Job: %w", err)
	}
	if assignErr != nil {
		pc.matar()
		return nil, fmt.Errorf("%w: %v", errJobAssignFailed, assignErr)
	}

	if err := resumeMainThread(uint32(cmd.Process.Pid)); err != nil {
		pc.matar()
		return nil, fmt.Errorf("asr/local: retomando processo contido: %w", err)
	}

	return pc, nil
}

// matar encerra um processoContido que não chegou a ficar pronto (falha de
// atribuição ao Job, falha ao retomar a thread, ou prontidão HTTP que não
// veio a tempo). Mata o processo diretamente — não depende de fechar o Job
// para isso, já que a atribuição ao Job pode nem ter acontecido — e SÓ
// DEPOIS fecha o handle do Job, nunca deixando handle para trás.
func (pc *processoContido) matar() {
	if pc.cmd.Process != nil {
		_ = pc.cmd.Process.Kill()
	}
	select {
	case <-pc.done:
	case <-time.After(stopGraceTimeout):
	}
	if pc.job != 0 {
		_ = windows.CloseHandle(pc.job)
		pc.job = 0
	}
}

// newKillOnCloseJob cria um Job Object configurado para matar todo membro
// quando o último handle do Job fechar (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
// — a garantia central deste arquivo: o Agent morrer por qualquer motivo,
// inclusive TerminateProcess, fecha esse handle e o Windows mata o
// whisper-server junto, sem depender de nenhum código do Agent rodar
// depois de morto.
func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("asr/local: CreateJobObject: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		job,
		uint32(windows.JobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("asr/local: SetInformationJobObject: %w", err)
	}
	return job, nil
}

// resumeMainThread retoma a execução de um processo criado com
// CREATE_SUSPENDED, dado só o PID — o handle de thread que CreateProcess
// devolve não é exposto pelo os/exec do Go (ele fecha pi.Thread
// internamente antes de retornar de Start), então achamos a thread de
// volta enumerando as threads do sistema e filtrando por
// ThreadEntry32.OwnerProcessID, o mesmo padrão de
// internal/benchmark/rss/rss_windows.go (processChildren) para enumeração
// de processos via Toolhelp32Snapshot — aqui para threads em vez de
// processos.
//
// Um processo recém-criado com CREATE_SUSPENDED tem exatamente uma thread
// (a principal): nenhuma outra pode existir ainda, porque nenhuma
// instrução do processo rodou para criar uma. Por isso paramos no primeiro
// achado, sem ambiguidade sobre qual thread retomar.
func resumeMainThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("tirando snapshot de threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ThreadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("lendo a primeira thread do snapshot: %w", err)
	}
	for {
		if entry.OwnerProcessID == pid {
			handle, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return fmt.Errorf("abrindo a thread %d: %w", entry.ThreadID, err)
			}
			_, resumeErr := windows.ResumeThread(handle)
			closeErr := windows.CloseHandle(handle)
			if resumeErr != nil {
				return fmt.Errorf("retomando a thread %d: %w", entry.ThreadID, resumeErr)
			}
			return closeErr
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			break // ERROR_NO_MORE_FILES: fim da lista, não uma falha
		}
	}
	return fmt.Errorf("nenhuma thread encontrada para o pid %d", pid)
}

// pickFreePort pede ao SO uma porta livre em loopbackHost. Mesma técnica e
// mesma janela de corrida documentadas em
// cmd/whisperwarm/server.go:pickFreePort — por isso Start tenta mais de
// uma porta (maxPortAttempts) antes de desistir.
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("asr/local: escolhendo porta livre em %s: %w", loopbackHost, err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// waitReady sonda baseURL até ele responder (qualquer status HTTP conta
// como "processo vivo e servindo"), até timeout se esgotar, até ctx
// terminar, ou até done fechar (processo morreu antes de ficar pronto).
// Mesmo padrão de cmd/whisperwarm/server.go:waitReady, adaptado para
// também respeitar ctx — o contrato de serverProcess.Start exige isso.
func waitReady(ctx context.Context, baseURL string, timeout time.Duration, done <-chan struct{}) error {
	client := &http.Client{Timeout: readyProbeTimeout}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()

	for {
		resp, err := client.Get(baseURL + "/")
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}

		select {
		case <-done:
			return fmt.Errorf("asr/local: processo encerrou antes de ficar pronto")
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("asr/local: tempo esgotado esperando processo ficar pronto (%s, timeout %s)", baseURL, timeout)
		case <-ticker.C:
		}
	}
}
