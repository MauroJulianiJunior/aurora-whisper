package winruntime

import (
	"context"
	"runtime"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// insert aplica InsertEffect através do inserter em camadas (ADR-011,
// decisão 3: SendInput unicode para texto curto; clipboard + Ctrl+V para
// texto longo ou quando o SendInput falha). Substitui a inserção mínima
// caractere-a-caractere do Sprint 1 — ver doc.go.
//
// e.Focus não é usado aqui: internal/agent/inserter não faz UI Automation
// nem decide COM BASE no controle alvo (ADR-011, decisão 1 — UIA é só
// para inspeção; a inserção em si não sabe nem precisa saber qual
// controle tem foco, só que o foco de teclado atual é onde SendInput/
// Ctrl+V vão parar). O bloqueio em campo protegido já aconteceu antes
// disso, dentro do núcleo (Machine.onKeyDown, ADR-011 decisão 5) — um
// InsertEffect só é emitido para uma tentativa que já passou por essa
// checagem.
func (rt *Runtime) insert(e dictation.InsertEffect) dictation.Event {
	result := rt.inserter.Insert(e.Text)

	if !result.Success {
		rt.log.Warn("winruntime: insercao falhou em ambos os caminhos (unicode e clipboard)",
			"attempt", e.Attempt,
			"text_runes", len([]rune(e.Text)),
			"clipboard_retained", result.ClipboardRetained,
		)
		if result.ClipboardRetained {
			// ADR-011, decisão 3 / 04-system-design.md "Inserção" item 3:
			// não perder o texto. O clipboard já ficou, de propósito, com
			// o texto ditado (ver internal/agent/inserter, backend_windows.go)
			// — só registramos aqui que essa é a situação, nunca o
			// conteúdo em si (NFR-010).
			rt.log.Info("winruntime: texto ditado permanece no clipboard para colagem manual (insercao automatica falhou)",
				"attempt", e.Attempt)
		}
	}

	// O motivo distingue "cole" de "não deu": só quando o texto ficou
	// retido no clipboard a tela pode mandar colar. Na injeção parcial
	// (result.Partial) o clipboard nem foi tocado.
	reason := dictation.ErrorReasonNone
	if !result.Success {
		reason = dictation.ErrorReasonInsertFailed
		if result.ClipboardRetained {
			reason = dictation.ErrorReasonInsertClipboard
		}
	}
	return dictation.InsertResultEvent{
		Attempt: e.Attempt,
		Reason:  reason,
		Result:  dictation.InsertResultDTO{Success: result.Success},
	}
}

// threadDeInsercao roda as inserções numa goroutine própria, travada numa
// thread do sistema.
//
// # Por que fora do laço de Run
//
// Até 28-set-2026 a inserção rodava SÍNCRONA dentro de aplicar, que roda
// na goroutine de Run. Enquanto ela rodava, Run não consumia rt.pending —
// e rt.pending é por onde o TimerElapsedEvent chega. O resultado é que o
// DefaultInsertTimeout de 3 s era decorativo NESTE caminho: o watchdog
// que deveria socorrer a inserção não podia disparar justamente enquanto
// a inserção estava em curso.
//
// Isso importa porque o clipboard é o recurso mais disputado do Windows.
// Um GetClipboardData pode ficar preso esperando renderização adiada de
// outro processo, e o Histórico da Área de Transferência mexe no mesmo
// recurso (ver internal/agent/clipboard, policy.go). Com a inserção no
// laço, um clipboard preso congelava o Agent inteiro: sem hook, sem
// overlay — a pílula nem redesenhava, porque quem a acorda também é Run.
//
// # Por que a thread é travada
//
// OpenClipboard(0) associa o clipboard à THREAD que chamou, e
// SetClipboardData/CloseClipboard precisam vir da mesma. O Preserver faz
// open -> ler -> escrever -> close em chamadas separadas, e uma goroutine
// Go pode mudar de thread entre syscalls. O pacote clipboard nunca
// travou a thread; o laço de Run também não. Era contrato do Win32 sendo
// respeitado por sorte de escalonamento.
//
// É o mesmo remédio que internal/agent/audio e internal/agent/hook já
// usam pelo mesmo motivo — ali para COM e para o loop de mensagens.
//
// # O que fica pior, honestamente
//
// A inserção agora pode terminar DEPOIS de o watchdog ter encerrado a
// tentativa. Nesse caso o texto entra no campo e a tela já disse que deu
// timeout: a mensagem fica errada por uma janela. É a mesma troca que
// transcrever já aceita, e é melhor que a anterior — antes, o Agent
// inteiro congelava e a mensagem não chegava nunca.
func (rt *Runtime) threadDeInsercao(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for {
		select {
		case <-ctx.Done():
			return
		case e := <-rt.insercoes:
			ev := rt.insert(e)
			// A ficha volta ANTES de entregar o resultado: entregar pode
			// esperar por Run, e segurar a vaga nesse meio-tempo
			// recusaria a próxima inserção por um motivo que já passou.
			rt.devolverVaga()
			select {
			case rt.pending <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}

// enfileirarInsercao entrega o pedido à thread dedicada, ou desiste.
//
// Roda na goroutine de Run, então NÃO pode bloquear: é o laço que
// consome rt.pending, e esperar aqui seria recriar, com outra roupa, o
// congelamento que esta fatia existe para acabar.
//
// Quando a thread está ocupada — só acontece se uma inserção anterior
// travou — nenhum evento é forçado. O TimerInsertTimeout que acompanha
// este InsertEffect encerra a tentativa em 3 s, e ele VOLTOU a poder
// disparar. Forçar um InsertResultEvent daqui exigiria mandar para
// rt.pending de dentro de Run, que é exatamente o bloqueio que se está
// evitando.
func (rt *Runtime) enfileirarInsercao(e dictation.InsertEffect) {
	select {
	case <-rt.vaga:
		// Com a ficha em mãos, insercoes está vazio por construção: o
		// envio não bloqueia nem que a thread ainda não esteja no select.
		rt.insercoes <- e
	default:
		rt.log.Warn("winruntime: inserção anterior ainda em curso; esta tentativa depende do watchdog",
			"attempt", e.Attempt)
	}
}

// vagaLivre cria o canal de permissão já com a ficha dentro.
func vagaLivre() chan struct{} {
	v := make(chan struct{}, 1)
	v <- struct{}{}
	return v
}

// devolverVaga repõe a ficha. Nunca bloqueia: só quem a tomou devolve, e
// a capacidade é 1.
func (rt *Runtime) devolverVaga() {
	select {
	case rt.vaga <- struct{}{}:
	default:
	}
}
