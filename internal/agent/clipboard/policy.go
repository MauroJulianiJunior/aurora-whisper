package clipboard

import (
	"errors"
	"fmt"
	"time"
)

// Este arquivo não tem build tag: é lógica pura, sem chamada de sistema,
// testada contra dublês (CLAUDE.md, "Testes que agem no sistema real").

// RetryPolicy governa quantas vezes openWithRetry tenta abrir o clipboard
// antes de desistir, e quanto esperar entre tentativas.
type RetryPolicy struct {
	// MaxAttempts é o número máximo de tentativas de abrir. Valores < 1
	// são tratados como 1 (uma tentativa, sem retry).
	MaxAttempts int

	// Backoff devolve quanto esperar depois da tentativa número attempt
	// (1-based) antes de tentar de novo. Não é chamado depois da última
	// tentativa. nil equivale a "sem espera".
	Backoff func(attempt int) time.Duration
}

// DefaultRetryPolicy é a política usada em produção: até 5 tentativas,
// com backoff curto (10ms, 20ms, ... até um teto de 100ms). O clipboard
// aberto por outro processo costuma liberar em poucos milissegundos —
// não há motivo para esperar mais que isso antes de desistir de forma
// limpa (ver doc.go, "Armadilhas tratadas").
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 5,
		Backoff: func(attempt int) time.Duration {
			d := time.Duration(attempt) * 10 * time.Millisecond
			const cap = 100 * time.Millisecond
			if d > cap {
				d = cap
			}
			return d
		},
	}
}

// ErrOpenTimeout é o erro base devolvido por openWithRetry quando todas
// as tentativas de abrir o clipboard falharam. errors.Is reconhece este
// valor mesmo com a causa concreta encadeada via %w.
var ErrOpenTimeout = errors.New("clipboard: OpenClipboard falhou após todas as tentativas (recurso global ocupado por outro processo?)")

// openWithRetry chama open até ela devolver nil ou a política de
// tentativas se esgotar, esperando via sleep entre tentativas segundo
// policy.Backoff. Nunca entra em laço infinito: o número de tentativas é
// sempre limitado por policy.MaxAttempts.
//
// Pura o bastante para teste sem tocar o sistema: open e sleep são
// injetados, então um teste usa dublês que nunca abrem um clipboard de
// verdade nem esperam tempo real.
func openWithRetry(open func() error, policy RetryPolicy, sleep func(time.Duration)) error {
	maxAttempts := policy.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := open()
		if err == nil {
			return nil
		}
		lastErr = err

		if attempt == maxAttempts {
			break
		}
		if policy.Backoff != nil && sleep != nil {
			sleep(policy.Backoff(attempt))
		}
	}
	return fmt.Errorf("%w: %v", ErrOpenTimeout, lastErr)
}

// shouldRestore decide se ainda é seguro escrever o conteúdo preservado de
// volta. A pergunta é "o clipboard ainda contém o que EU escrevi?".
//
// # Por que não o número de sequência
//
// A primeira versão comparava GetClipboardSequenceNumber antes e depois, e
// isso não sobrevive ao Windows real. O Histórico da Área de Transferência
// (Win+V), ligado por padrão no Windows 11, lê e reescreve o clipboard logo
// após cada escrita, incrementando a sequência várias vezes. Observado numa
// máquina de verdade: a nossa escrita registrou 1765 e, na hora de
// restaurar, a sequência já estava em 1768.
//
// O efeito era pior que um bug ruidoso: em toda máquina com esse recurso
// ativo a sequência SEMPRE mudava, o restore era SEMPRE pulado, o AC-009
// nunca acontecia — e o log dizia "pulado" como se fosse prudência. A
// pessoa perdia o que tinha copiado, e nada denunciava.
//
// Comparar o CONTEÚDO distingue os dois casos que a sequência confunde:
//
//   - o serviço de histórico reescrevendo o NOSSO próprio texto — o
//     conteúdo continua sendo o nosso, e restaurar é seguro;
//   - a pessoa copiando algo novo no meio do caminho — o conteúdo é outro,
//     e restaurar destruiria a cópia mais recente dela.
//
// readOK falso significa que não conseguimos ler o clipboard para comparar.
// Nesse caso restauramos assim mesmo: perder o conteúdo original é certeza,
// enquanto sobrescrever uma mudança externa é só risco.
func shouldRestore(currentText, ourText string, readOK bool) bool {
	if !readOK {
		return true
	}
	return currentText == ourText
}
