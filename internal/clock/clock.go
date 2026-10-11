// Package clock injeta a noção de tempo do núcleo do ditado (ver
// docs/adr/ADR-007-dictation-core.md, seção 8).
//
// O núcleo (Machine, Decider) nunca chama time.Now nem time.AfterFunc
// diretamente — ele recebe um Clock. Em produção isso é o System, que
// delega para a stdlib. Nos testes da Fase 1 isso é o Fake, cujo
// Advance dispara os timers vencidos de forma síncrona e determinista,
// sem sleep e sem polling.
package clock

import "time"

// Clock abstrai a passagem do tempo para permitir substituição por um
// relógio falso nos testes.
type Clock interface {
	// Now retorna o instante atual segundo este relógio.
	Now() time.Time

	// AfterFunc agenda f para ser executado depois que d transcorrer
	// (segundo este relógio) e retorna um Timer para cancelamento.
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer representa um disparo agendado por Clock.AfterFunc.
type Timer interface {
	// Stop cancela o timer. Retorna true se o cancelamento impediu o
	// disparo; retorna false se o timer já havia disparado ou já
	// tinha sido parado antes.
	Stop() bool
}

// Verificação em tempo de compilação de que ambas as implementações
// satisfazem Clock.
var (
	_ Clock = System{}
	_ Clock = (*Fake)(nil)
)
