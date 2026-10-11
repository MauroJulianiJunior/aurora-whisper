package winruntime

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/inserter"
)

// inserterFalso prende a inserção até ser liberado, para que o teste
// possa observar o que acontece ENQUANTO ela está em curso.
//
// Nunca toca o sistema: não injeta tecla, não escreve no clipboard.
// CLAUDE.md proíbe, e o que se testa aqui é o desenho de concorrência,
// não o Win32.
type inserterFalso struct {
	entrou  chan struct{}
	liberar chan struct{}
	chamou  chan string
}

func novoInserterFalso() *inserterFalso {
	return &inserterFalso{
		entrou:  make(chan struct{}, 8),
		liberar: make(chan struct{}),
		chamou:  make(chan string, 8),
	}
}

func (f *inserterFalso) Insert(text string) inserter.Result {
	f.chamou <- text
	f.entrou <- struct{}{}
	<-f.liberar
	return inserter.Result{Success: true, Method: inserter.MethodClipboard}
}

func runtimeComInserter(ins insertor) *Runtime {
	return &Runtime{
		inserter:  ins,
		pending:   make(chan dictation.Event, pendingBuffer),
		insercoes: make(chan dictation.InsertEffect, 1),
		vaga:      vagaLivre(),
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestInsercao_NaoBloqueiaOLacoDeEventos é a razão inteira desta fatia.
//
// Até 28-set-2026 a inserção rodava SÍNCRONA dentro de aplicar, que roda
// na goroutine de Run. Enquanto ela rodava, Run não consumia rt.pending —
// que é por onde o TimerElapsedEvent chega. O DefaultInsertTimeout de 3 s
// existia e não podia disparar: o watchdog da inserção estava desligado
// exatamente enquanto a inserção acontecia.
//
// Importa porque o clipboard é o recurso mais disputado do Windows. Com a
// inserção no laço, um GetClipboardData preso congelava o Agent inteiro —
// sem hook, sem overlay, sem timeout.
//
// O teste prende a inserção e exige que enfileirarInsercao RETORNE.
func TestInsercao_NaoBloqueiaOLacoDeEventos(t *testing.T) {
	ins := novoInserterFalso()
	rt := runtimeComInserter(ins)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rt.threadDeInsercao(ctx)

	voltou := make(chan struct{})
	go func() {
		rt.enfileirarInsercao(dictation.InsertEffect{Attempt: 7, Text: "olá"})
		close(voltou)
	}()

	select {
	case <-voltou:
	case <-time.After(2 * time.Second):
		t.Fatal("enfileirarInsercao() não voltou com a inserção presa — o laço de eventos ficaria travado")
	}

	// E a inserção realmente começou: voltar não foi por ter pulado o
	// trabalho.
	select {
	case <-ins.entrou:
	case <-time.After(2 * time.Second):
		t.Fatal("o inserter nunca foi chamado")
	}
	close(ins.liberar)
}

// TestInsercao_ResultadoVoltaPorPending: a resposta precisa chegar ao
// núcleo pelo mesmo canal dos outros efeitos assíncronos, senão a
// tentativa fica esperando o watchdog mesmo quando a inserção deu certo.
func TestInsercao_ResultadoVoltaPorPending(t *testing.T) {
	ins := novoInserterFalso()
	close(ins.liberar) // não prende: queremos o caminho feliz
	rt := runtimeComInserter(ins)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rt.threadDeInsercao(ctx)

	rt.enfileirarInsercao(dictation.InsertEffect{Attempt: 9, Text: "texto"})

	select {
	case ev := <-rt.pending:
		res, ok := ev.(dictation.InsertResultEvent)
		if !ok {
			t.Fatalf("pending devolveu %T, want InsertResultEvent", ev)
		}
		if res.Attempt != 9 {
			t.Errorf("Attempt = %d, want 9", res.Attempt)
		}
		if res.Reason != dictation.ErrorReasonNone {
			t.Errorf("Reason = %v, want none numa inserção bem-sucedida", res.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("o resultado da inserção nunca chegou em pending")
	}
}

// TestInsercao_ComThreadPresaNaoEnfileiraParaDepois trava a decisão de o
// canal NÃO ter buffer.
//
// Com buffer, um segundo pedido esperaria atrás da inserção travada; o
// watchdog encerraria a tentativa em 3 s; e o texto entraria no campo
// DEPOIS, numa tentativa que a tela já declarou perdida — possivelmente
// noutro campo, porque a pessoa já mudou de janela.
//
// Sem buffer, ou a thread aceita agora, ou o pedido não existe e o
// watchdog resolve.
func TestInsercao_ComThreadPresaNaoEnfileiraParaDepois(t *testing.T) {
	ins := novoInserterFalso()
	rt := runtimeComInserter(ins)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rt.threadDeInsercao(ctx)

	rt.enfileirarInsercao(dictation.InsertEffect{Attempt: 1, Text: "primeiro"})
	select {
	case <-ins.entrou:
	case <-time.After(2 * time.Second):
		t.Fatal("a primeira inserção nem começou")
	}
	// Consome o registro da PRIMEIRA chamada: sem isto, a checagem final
	// leria este mesmo valor e acusaria uma segunda inserção que não
	// aconteceu.
	if texto := <-ins.chamou; texto != "primeiro" {
		t.Fatalf("primeira chamada foi com %q, want \"primeiro\"", texto)
	}

	// Segunda tentativa enquanto a primeira está presa.
	rt.enfileirarInsercao(dictation.InsertEffect{Attempt: 2, Text: "segundo"})

	// Libera a primeira e confere que a segunda NUNCA foi executada.
	close(ins.liberar)
	select {
	case ev := <-rt.pending:
		if res := ev.(dictation.InsertResultEvent); res.Attempt != 1 {
			t.Fatalf("primeiro resultado é da tentativa %d, want 1", res.Attempt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a primeira inserção não devolveu resultado")
	}

	select {
	case texto := <-ins.chamou:
		t.Fatalf("a segunda inserção foi executada depois (%q) — o texto entraria numa tentativa já encerrada", texto)
	case ev := <-rt.pending:
		t.Fatalf("chegou um segundo evento (%#v) — a tentativa 2 devia depender do watchdog", ev)
	case <-time.After(150 * time.Millisecond):
		// Nada aconteceu: correto.
	}
}

// TestInsercao_ThreadMorreComOContexto: sem isto a goroutine (e a thread
// travada do sistema) vazaria a cada Run.
func TestInsercao_ThreadMorreComOContexto(t *testing.T) {
	ins := novoInserterFalso()
	close(ins.liberar)
	rt := runtimeComInserter(ins)

	ctx, cancel := context.WithCancel(context.Background())
	morreu := make(chan struct{})
	go func() {
		rt.threadDeInsercao(ctx)
		close(morreu)
	}()

	cancel()
	select {
	case <-morreu:
	case <-time.After(2 * time.Second):
		t.Fatal("a thread de inserção não terminou com o contexto cancelado")
	}
}
