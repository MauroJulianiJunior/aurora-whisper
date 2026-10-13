//go:build windows

package local

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Estes testes usam "ping" como processo trivial no lugar do
// whisper-server: já vem com o Windows, não lê stdin (ao contrário de
// "cmd /c timeout"), não gasta CPU relevante nem carrega modelo nenhum. O
// que importa aqui é o ciclo de vida do processo/Job (iniciarProcessoContido,
// resumeMainThread, o Job matando o filho) — não HTTP nem whisper-server de
// verdade, que este pacote não tem disponível em teste (ver relatório da
// tarefa para o que não deu para testar).

// TestIniciarProcessoContido_RodaERetomaSozinho prova que resumeMainThread
// de fato retoma a thread principal — não só "não voltou erro". Se a
// retomada não funcionasse, o processo ficaria suspenso para sempre e
// nunca terminaria sozinho; aqui damos a ele um trabalho curto (dois
// pings) e esperamos que ele SAIA sozinho dentro de um prazo bem menor que
// "para sempre".
func TestIniciarProcessoContido_RodaERetomaSozinho(t *testing.T) {
	pc, err := iniciarProcessoContido("ping", []string{"-n", "2", "127.0.0.1"})
	if err != nil {
		t.Fatalf("iniciarProcessoContido: %v", err)
	}
	t.Cleanup(pc.matar)

	select {
	case <-pc.done:
		if pc.waitErr != nil {
			t.Errorf("waitErr = %v, want nil — ping -n 2 127.0.0.1 devia sair com sucesso", pc.waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("o processo não terminou sozinho em 5s — se a thread principal não foi retomada, ele fica suspenso para sempre")
	}
}

// TestIniciarProcessoContido_JobMataFilhoQuandoFecha é o teste que mais
// importa: prova que a proteção contra órfão funciona de verdade, e não é
// só uma afirmação em comentário. Simula exatamente o cenário do doc.go —
// o dono do processo (o Agent) morre sem passar por Stop — fechando o
// handle do Job diretamente, e confirma que o Windows mata o filho junto
// via JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE.
func TestIniciarProcessoContido_JobMataFilhoQuandoFecha(t *testing.T) {
	pc, err := iniciarProcessoContido("ping", []string{"-n", "60", "127.0.0.1"})
	if err != nil {
		t.Fatalf("iniciarProcessoContido: %v", err)
	}

	select {
	case <-pc.done:
		t.Fatal("o processo já tinha saído sozinho antes do teste fechar o Job — teste inválido, aumente -n")
	default:
	}

	// Fecha o handle do Job diretamente — SEM passar por matar()/Stop —
	// para simular o Agent sendo morto (TerminateProcess, queda, panic)
	// sem chance de rodar nenhum código de encerramento próprio.
	if err := windows.CloseHandle(pc.job); err != nil {
		t.Fatalf("CloseHandle(job): %v", err)
	}

	select {
	case <-pc.done:
		// esperado: fechar o último handle do Job matou o processo.
	case <-time.After(5 * time.Second):
		_ = pc.cmd.Process.Kill() // não deixa o processo vazar do teste mesmo se a asserção falhar
		t.Fatal("o processo continuou vivo depois do Job fechar — a proteção contra órfão NÃO funcionou")
	}
}

// TestIniciarProcessoContido_CaminhoInexistente: se o executável não
// existe, cmd.Start() falha e nada deve ficar de pé — nem processo, nem
// Job. Não há como este teste verificar diretamente "nenhum handle
// vazou" (Go não expõe contagem de handles), mas exercita o caminho de
// erro mais comum de iniciarProcessoContido.
func TestIniciarProcessoContido_CaminhoInexistente(t *testing.T) {
	pc, err := iniciarProcessoContido("caminho-que-nao-existe-de-verdade.exe", nil)
	if err == nil {
		t.Cleanup(pc.matar)
		t.Fatal("iniciarProcessoContido com caminho inexistente = nil, want erro")
	}
	if pc != nil {
		t.Errorf("iniciarProcessoContido = %+v, want nil no caminho de erro", pc)
	}
}

// TestWindowsServerProcess_Start_CtxJaCancelado: Start não deve nem tentar
// subir nada se o contexto já morreu — o contrato de serverProcess.Start
// (process.go) é "espera até ele aceitar requisição, ou até ctx terminar".
func TestWindowsServerProcess_Start_CtxJaCancelado(t *testing.T) {
	p := &windowsServerProcess{cfg: Config{ServerPath: "não importa, nunca deveria ser usado"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start com ctx cancelado: err = %v, want context.Canceled", err)
	}
}

// TestWindowsServerProcess_Start_ChamadoDuasVezes: Start não é reentrante —
// chamar de novo no mesmo processo vazaria o processo/Job da primeira
// chamada.
func TestWindowsServerProcess_Start_ChamadoDuasVezes(t *testing.T) {
	p := &windowsServerProcess{cfg: Config{}, pc: &processoContido{cmd: nil, done: make(chan struct{})}}

	if _, err := p.Start(context.Background()); err == nil {
		t.Fatal("segunda chamada a Start = nil, want erro")
	}
}

// TestWindowsServerProcess_Start_ProcessoMorreAntesDePronto_TentaEDesiste
// usa "ping" no lugar do ServerPath: ping não entende os flags
// --host/--port/-m que serverArgs monta, sai quase imediatamente com erro
// de uso, e nunca abre a porta. Isto exercita o laço de tentativas
// completo (maxPortAttempts) e o caminho "o processo morreu antes de
// ficar pronto" de waitReady — sem precisar de whisper-server.exe nem de
// um servidor HTTP de verdade.
func TestWindowsServerProcess_Start_ProcessoMorreAntesDePronto_TentaEDesiste(t *testing.T) {
	p := &windowsServerProcess{cfg: Config{
		ServerPath:   "ping",
		ModelPath:    "modelo-que-nao-existe.bin",
		ReadyTimeout: 3 * time.Second, // limite defensivo; o esperado é falhar bem antes disso
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	end, err := p.Start(ctx)
	if err == nil {
		t.Fatalf("Start = (%q, nil), want erro — ping nunca serve HTTP", end.base)
	}
	if !strings.Contains(err.Error(), "não foi possível iniciar") {
		t.Errorf("err = %v, want mensagem de esgotamento de tentativas", err)
	}
	if p.Alive() {
		t.Error("Alive() = true depois de Start falhar em todas as tentativas")
	}
}

// TestWindowsServerProcess_AliveEStop_DelegamParaProcessoContido verifica
// que os métodos públicos (o que Provider realmente chama) encerram o
// processo de verdade, sem precisar de HTTP: monta um processoContido à
// mão com iniciarProcessoContido (mesmo caminho de produção) e embrulha
// num windowsServerProcess, pulando só a espera de prontidão HTTP que é
// específica de whisper-server.
func TestWindowsServerProcess_AliveEStop_DelegamParaProcessoContido(t *testing.T) {
	pc, err := iniciarProcessoContido("ping", []string{"-n", "60", "127.0.0.1"})
	if err != nil {
		t.Fatalf("iniciarProcessoContido: %v", err)
	}
	p := &windowsServerProcess{cfg: Config{}, pc: pc}

	if !p.Alive() {
		t.Fatal("Alive() = false logo após subir — o processo devia estar rodando")
	}

	if err := p.Stop(); err != nil {
		t.Errorf("Stop() = %v, want nil (Kill() é um encerramento esperado)", err)
	}
	if p.Alive() {
		t.Error("Alive() = true depois de Stop")
	}

	select {
	case <-pc.done:
		// esperado: Stop matou o processo de verdade, não só marcou um
		// campo local.
	case <-time.After(2 * time.Second):
		t.Error("o processo continuou vivo depois de Stop()")
	}
}

// TestPickFreePort_ELoopback: a porta devolvida tem de estar livre e, mais
// importante, pickFreePort nunca pode pedir nada fora de 127.0.0.1 — um
// servidor de ASR na rede exporia transcrição (CLAUDE.md do projeto).
func TestPickFreePort_ELoopback(t *testing.T) {
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port = %d, fora da faixa válida", port)
	}
}

// TestWaitReady_RetornaAoFicarPronto: caminho feliz — assim que o endereço
// responde (qualquer status conta, conforme o comentário de waitReady),
// devolve nil sem esperar o timeout inteiro.
func TestWaitReady_RetornaAoFicarPronto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	done := make(chan struct{}) // nunca fecha: o processo "continua vivo"
	err := waitReady(context.Background(), endereco{base: srv.URL}, 5*time.Second, done)
	if err != nil {
		t.Fatalf("waitReady: %v", err)
	}
}

// TestWaitReady_RespeitaCtx: se o contexto morre antes do endereço
// responder, waitReady tem de devolver o erro do contexto e não esperar o
// timeout inteiro (que aqui é propositalmente enorme para a asserção de
// tempo abaixo só passar se ctx.Done() foi respeitado de verdade).
func TestWaitReady_RespeitaCtx(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	inicio := time.Now()
	err := waitReady(ctx, endereco{base: "http://127.0.0.1:1"}, time.Minute, done)
	decorrido := time.Since(inicio)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if decorrido > 2*time.Second {
		t.Errorf("waitReady levou %s para respeitar ctx.Done() — esperava retorno quase imediato após 200ms", decorrido)
	}
}

// TestWaitReady_SondaPeloCaminhoSecreto: a sonda de prontidão também vai pelo
// caminho da subida. O servidor de teste só responde nele; fora dele, corta a
// conexão sem resposta, e waitReady não pode dar o servidor por pronto.
func TestWaitReady_SondaPeloCaminhoSecreto(t *testing.T) {
	var mu sync.Mutex
	var chegaram []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		chegaram = append(chegaram, r.URL.Path)
		mu.Unlock()
		if r.URL.Path != caminhoDeTeste+"/" {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := waitReady(context.Background(), endereco{base: srv.URL, caminho: caminhoDeTeste}, 5*time.Second, make(chan struct{})); err != nil {
		t.Fatalf("waitReady: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(chegaram) != 1 || chegaram[0] != caminhoDeTeste+"/" {
		t.Errorf("a sonda chegou em %q, quero só %s/", chegaram, caminhoDeTeste)
	}
}

// TestWaitReady_ErroNaoLevaOCaminho: o tempo esgotado diz a base (ajuda a ler
// o log), e não o caminho.
func TestWaitReady_ErroNaoLevaOCaminho(t *testing.T) {
	err := waitReady(context.Background(), endereco{base: "http://127.0.0.1:1", caminho: caminhoDeTeste}, 300*time.Millisecond, make(chan struct{}))
	if err == nil {
		t.Fatal("waitReady = nil, quero tempo esgotado")
	}
	semPedacoDoCaminho(t, "o erro de waitReady", fmt.Sprintf("%v %+v %#v", err, err, err), caminhoDeTeste)
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("err = %v: quero a base no erro", err)
	}
}
